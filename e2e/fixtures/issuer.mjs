// A local OpenID Connect issuer for the e2e servers (ADR-00030), so no test
// signs in with a real provider. A server under test is pointed at it with the
// GOBBLER_OIDC_* settings that issuerEnv returns. It serves discovery, an
// authorization endpoint that approves at once and sends the browser back with
// a code, a token endpoint that checks the client and the PKCE verifier and
// answers with an RS256 ID token, and its signing key. Loopback only; its key
// is made at start and never leaves the process.
import { createHash, createSign, generateKeyPairSync, randomBytes } from 'node:crypto';
import { createServer } from 'node:http';

export const ISSUER_CLIENT_ID = 'gobbler-e2e';
export const ISSUER_CLIENT_SECRET = 'e2e-fake-client-secret';
// The identity the issuer signs in as: the dev identity, a member of the dev
// household on a bypass server.
export const ISSUER_EMAIL = 'dev@localhost';

const KID = 'e2e-key';

function b64url(buf) {
  return Buffer.from(buf).toString('base64url');
}

function json(res, status, body) {
  const text = JSON.stringify(body);
  res.writeHead(status, { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(text) });
  res.end(text);
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on('data', (c) => chunks.push(c));
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
    req.on('error', reject);
  });
}

// startIssuer listens on an OS-chosen loopback port. It resolves to the
// issuer's origin, which is also its issuer identifier, and a close function.
export async function startIssuer() {
  const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
  const jwk = publicKey.export({ format: 'jwk' });
  const codes = new Map();
  let origin = '';

  function sign(claims) {
    const signing = `${b64url(JSON.stringify({ alg: 'RS256', kid: KID, typ: 'JWT' }))}.${b64url(JSON.stringify(claims))}`;
    const sig = createSign('RSA-SHA256').update(signing).sign(privateKey);
    return `${signing}.${b64url(sig)}`;
  }

  const server = createServer(async (req, res) => {
    const url = new URL(req.url ?? '/', origin);
    if (req.method === 'GET' && url.pathname === '/.well-known/openid-configuration') {
      json(res, 200, {
        issuer: origin,
        authorization_endpoint: `${origin}/authorize`,
        token_endpoint: `${origin}/token`,
        jwks_uri: `${origin}/jwks`,
        response_types_supported: ['code'],
        subject_types_supported: ['public'],
        id_token_signing_alg_values_supported: ['RS256'],
        code_challenge_methods_supported: ['S256'],
      });
      return;
    }
    if (req.method === 'GET' && url.pathname === '/jwks') {
      json(res, 200, { keys: [{ ...jwk, kid: KID, use: 'sig', alg: 'RS256' }] });
      return;
    }
    if (req.method === 'GET' && url.pathname === '/authorize') {
      const q = url.searchParams;
      const scopes = (q.get('scope') ?? '').split(' ');
      if (q.get('client_id') !== ISSUER_CLIENT_ID || q.get('response_type') !== 'code' || q.get('code_challenge_method') !== 'S256' ||
          !q.get('code_challenge') || !q.get('state') || !q.get('nonce') || !scopes.includes('openid') || !q.get('redirect_uri')) {
        res.writeHead(400).end('bad authorization request');
        return;
      }
      const code = b64url(randomBytes(24));
      codes.set(code, { challenge: q.get('code_challenge'), nonce: q.get('nonce'), redirect: q.get('redirect_uri') });
      const back = new URL(q.get('redirect_uri'));
      back.searchParams.set('code', code);
      back.searchParams.set('state', q.get('state'));
      res.writeHead(302, { Location: back.toString() }).end();
      return;
    }
    if (req.method === 'POST' && url.pathname === '/token') {
      const form = new URLSearchParams(await readBody(req));
      let id = form.get('client_id');
      let secret = form.get('client_secret');
      const basic = /^Basic (.+)$/.exec(req.headers.authorization ?? '');
      if (basic) {
        const [u, p] = Buffer.from(basic[1], 'base64').toString('utf8').split(':');
        id = decodeURIComponent(u);
        secret = decodeURIComponent(p ?? '');
      }
      if (id !== ISSUER_CLIENT_ID || secret !== ISSUER_CLIENT_SECRET) {
        json(res, 401, { error: 'invalid_client' });
        return;
      }
      const code = form.get('code') ?? '';
      const grant = codes.get(code);
      codes.delete(code); // one use only
      const challenge = b64url(createHash('sha256').update(form.get('code_verifier') ?? '').digest());
      if (!grant || form.get('grant_type') !== 'authorization_code' || form.get('redirect_uri') !== grant.redirect || challenge !== grant.challenge) {
        json(res, 400, { error: 'invalid_grant' });
        return;
      }
      const now = Math.floor(Date.now() / 1000);
      const idToken = sign({
        iss: origin, aud: ISSUER_CLIENT_ID, sub: 'e2e-subject', email: ISSUER_EMAIL, email_verified: true,
        iat: now, exp: now + 3600, nonce: grant.nonce,
      });
      json(res, 200, { access_token: b64url(randomBytes(24)), token_type: 'Bearer', expires_in: 3600, id_token: idToken });
      return;
    }
    res.writeHead(404).end();
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  origin = `http://127.0.0.1:${server.address().port}`;
  return {
    origin,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}

// issuerEnv is the server environment that signs in with the issuer at origin
// for a server reached at publicBaseURL. Plain http is accepted only with the
// loopback-only dev bypass on.
export function issuerEnv(origin, publicBaseURL) {
  return {
    GOBBLER_OIDC_ISSUER: origin,
    GOBBLER_OIDC_CLIENT_ID: ISSUER_CLIENT_ID,
    GOBBLER_OIDC_CLIENT_SECRET: ISSUER_CLIENT_SECRET,
    GOBBLER_PUBLIC_BASE_URL: publicBaseURL,
  };
}
