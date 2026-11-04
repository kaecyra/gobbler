// Starts the gobbler binary for the Playwright suite on loopback with data in a
// fresh temp directory removed on exit. Run by the webServer entries in
// playwright.config.ts; never with real credentials.
//
//   serve.mjs            dev-auth bypass on (a request is the dev identity unless it
//                        sends X-Gobbler-Dev-Identity; the dev households are seeded)
//   serve.mjs --anonymous  dev-auth bypass OFF, as in production: sign-in is
//                        configured against an issuer that does not exist and is
//                        never contacted, so the identity middleware judges every
//                        request and one without a session is refused (ADR-00030)
//   serve.mjs --signin   dev-auth bypass on, with sign-in configured against the
//                        local fake issuer (fixtures/issuer.mjs), so the sign-in
//                        routes run their whole flow (ADR-00030). The bypass still
//                        decides every request's identity.
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { issuerEnv, startIssuer } from './issuer.mjs';
import { startThumbnailHost, THUMBNAIL_HOST_ENV } from './thumbnails.mjs';

const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) {
  console.error('GOBBLER_E2E_BIN is not set; run the suite through `make e2e`, which builds the binary.');
  process.exit(1);
}
const anonymous = process.argv.includes('--anonymous');
const signin = process.argv.includes('--signin');
const port = anonymous
  ? process.env.GOBBLER_E2E_ANONYMOUS_PORT
  : signin
    ? process.env.GOBBLER_E2E_SIGNIN_PORT
    : process.env.GOBBLER_E2E_PORT;
if (!port) {
  console.error('the listen port is not set; run the suite through playwright.config.ts');
  process.exit(1);
}
// Video thumbnails come from a local fixture, never from i.ytimg.com.
const thumbnails = await startThumbnailHost();
// Sign-in goes to a local fake issuer, never to a real provider.
const issuer = signin ? await startIssuer() : undefined;
const dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-'));

// Only what the server needs plus the fixed settings: nothing from the
// developer's environment (API keys, real sign-in settings) reaches the binary.
const env = {
  PATH: process.env.PATH ?? '',
  GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
  GOBBLER_DATA_DIR: dataDir,
  GOBBLER_LOG_LEVEL: 'warn',
  [THUMBNAIL_HOST_ENV]: thumbnails.origin,
  // The suite's requests arrive from loopback, standing in for the tunnel, so
  // the sessions specs can set CF-Connecting-IP (ADR-00036).
  GOBBLER_TRUSTED_PROXIES: '127.0.0.1,::1',
  // Every spec registers its clients from this one address, in parallel, far
  // past what a household needs, so the per-address OAuth limits are lifted
  // here; the limits themselves are covered by Go tests.
  GOBBLER_OAUTH_REGISTER_RATE: '1000',
  GOBBLER_OAUTH_REGISTER_BURST: '100000',
  GOBBLER_OAUTH_TOKEN_RATE: '1000',
  GOBBLER_OAUTH_TOKEN_BURST: '100000',
  // The operators of the e2e deployment: one who is a member of no household
  // and the default dev identity, so suites that open /settings as dev (the
  // layout nav) keep working. Keep them in step with OPERATOR and DEV in
  // identity.ts. OTHER is a member who is not an operator.
  GOBBLER_OPERATOR_EMAILS: 'operator@e2e.example,dev@localhost',
  // Without the bypass the issuer and base URL must be https. Nothing in the
  // suite starts a sign-in here, so the .invalid issuer is never contacted.
  ...(anonymous
    ? {
        GOBBLER_OIDC_ISSUER: 'https://issuer.e2e.invalid',
        GOBBLER_OIDC_CLIENT_ID: 'gobbler-e2e-anonymous',
        GOBBLER_OIDC_CLIENT_SECRET: 'e2e-fake-client-secret',
        GOBBLER_PUBLIC_BASE_URL: 'https://gobbler.e2e.invalid',
      }
    : { GOBBLER_DEV_AUTH_BYPASS: 'true' }),
  ...(issuer ? issuerEnv(issuer.origin, `http://127.0.0.1:${port}`) : {}),
};

const child = spawn(bin, ['serve'], { env, stdio: 'inherit' });

let stopping = false;
let cleaned = false;
function cleanup() {
  if (cleaned) return;
  cleaned = true;
  rmSync(dataDir, { recursive: true, force: true });
  void thumbnails.close();
  void issuer?.close();
}

for (const sig of ['SIGTERM', 'SIGINT']) {
  process.on(sig, () => {
    stopping = true;
    child.kill(sig);
  });
}
child.on('error', (err) => {
  console.error(`start ${bin}: ${err.message}`);
  cleanup();
  process.exit(1);
});
child.on('exit', (code, signal) => {
  cleanup();
  if (signal && !stopping) {
    console.error(`${bin} died from signal ${signal}`);
    process.exit(1);
  }
  process.exit(code ?? 0);
});
