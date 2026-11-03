import { test, expect, type Playwright } from '@playwright/test';
import { DEV } from '../fixtures/identity';
import { expectTapTargets } from '../fixtures/tap';
import { REDIRECT, VERIFIER, authParams, codeForm, connectApp, decide, registerClient, resourceOf, startCallback } from './support';

// The OAuth authorization server for MCP clients (ADR-00034), run against the
// real binary: discovery, registration, consent in a browser, the token
// endpoint, and revocation from the profile. Only a member can approve a client,
// so these tests act as DEV, the default identity of the dev server. The server
// is shared with the other specs, which run in parallel, so each test names its
// client for itself and looks only at the rows that carry the name.

let counter = 0;
function uniqueName(label: string): string {
  counter += 1;
  return `${label} ${process.pid}-${Date.now()}-${counter}`;
}

test.describe('discovery', () => {
  test('the documents describe the server and name the /mcp resource', async ({ request, baseURL }) => {
    for (const path of ['/.well-known/oauth-protected-resource', '/.well-known/oauth-protected-resource/mcp']) {
      const res = await request.get(path);
      expect(res.status()).toBe(200);
      const doc = await res.json();
      expect(doc.resource).toBe(`${baseURL}/mcp`);
      expect(doc.authorization_servers).toEqual([baseURL]);
    }
    const res = await request.get('/.well-known/oauth-authorization-server');
    expect(res.status()).toBe(200);
    const doc = await res.json();
    expect(doc).toMatchObject({
      issuer: baseURL,
      authorization_endpoint: `${baseURL}/oauth/authorize`,
      token_endpoint: `${baseURL}/oauth/token`,
      registration_endpoint: `${baseURL}/oauth/register`,
      code_challenge_methods_supported: ['S256'],
      client_id_metadata_document_supported: true,
    });
  });
});

test.describe('connecting an app', () => {
  test('a client registers, the person approves it on the consent page, and a reused code revokes the grant', async ({ page, request }) => {
    const resource = await resourceOf(request);
    const name = uniqueName('Kitchen agent');
    const callback = await startCallback();
    const clientId = await registerClient(request, name, callback.redirect);
    const params = authParams(clientId, resource, callback.redirect);

    await page.goto(`/oauth/authorize?${new URLSearchParams(params)}`);
    await expect(page.getByTestId('consent-client')).toHaveText(name);
    await expect(page.getByTestId('consent-return-host')).toHaveText(callback.host);
    await expect(page.getByTestId('consent-email')).toHaveText(DEV);
    await expectTapTargets(page);

    await page.getByTestId('consent-allow').click();
    await page.waitForURL(`${callback.redirect}?**`);
    const back = new URL(page.url());
    expect(back.searchParams.get('state')).toBe('st4te');
    expect(back.searchParams.get('iss')).toBeTruthy();
    const code = back.searchParams.get('code')!;
    await callback.close();

    const tokenRes = await request.post('/oauth/token', { form: codeForm(clientId, code, resource, callback.redirect) });
    expect(tokenRes.status()).toBe(200);
    expect(tokenRes.headers()['cache-control']).toBe('no-store');
    const tokens = await tokenRes.json();
    expect(tokens.token_type).toBe('Bearer');
    expect(tokens.access_token).toMatch(/^goa_/);

    // The code works once; using it again revokes the grant it made.
    const replay = await request.post('/oauth/token', { form: codeForm(clientId, code, resource, callback.redirect), failOnStatusCode: false });
    expect(replay.status()).toBe(400);
    expect((await replay.json()).error).toBe('invalid_grant');
    await page.goto('/profile');
    await page.getByRole('link', { name: 'Manage connected apps' }).click();
    // The replay revoked the grant, so the app is not in the list.
    await expect(page.getByTestId('grant').filter({ hasText: name })).toHaveCount(0);
  });

  test('denying sends the client back with access_denied and issues no code', async ({ page, request }) => {
    const resource = await resourceOf(request);
    const callback = await startCallback();
    const clientId = await registerClient(request, 'Denied agent', callback.redirect);
    await page.goto(`/oauth/authorize?${new URLSearchParams(authParams(clientId, resource, callback.redirect))}`);
    await page.getByTestId('consent-deny').click();
    await page.waitForURL(`${callback.redirect}?**`);
    const back = new URL(page.url());
    await callback.close();
    expect(back.searchParams.get('error')).toBe('access_denied');
    expect(back.searchParams.get('state')).toBe('st4te');
    expect(back.searchParams.has('code')).toBe(false);
  });

  test('a refresh token rotates, and presenting a retired one revokes the grant', async ({ request }) => {
    const { clientId, resource, tokens } = await connectApp(request);
    const refresh = async (token: string) =>
      request.post('/oauth/token', {
        form: { grant_type: 'refresh_token', client_id: clientId, refresh_token: token, resource },
        failOnStatusCode: false,
      });
    const next = await refresh(tokens.refresh_token);
    expect(next.status()).toBe(200);
    const nextTokens = await next.json();
    expect(nextTokens.refresh_token).not.toBe(tokens.refresh_token);

    const reuse = await refresh(tokens.refresh_token);
    expect(reuse.status()).toBe(400);
    expect((await reuse.json()).error).toBe('invalid_grant');
    // The grant died with it: the newest refresh token is refused too.
    const after = await refresh(nextTokens.refresh_token);
    expect(after.status()).toBe(400);
  });

  test('the token endpoint refuses a wrong PKCE verifier, a missing resource and a wrong resource', async ({ request }) => {
    const resource = await resourceOf(request);
    const clientId = await registerClient(request);
    const fresh = async () => (await decide(request, authParams(clientId, resource), 'allow')).searchParams.get('code')!;
    const post = (form: Record<string, string>) => request.post('/oauth/token', { form, failOnStatusCode: false });

    let res = await post({ ...codeForm(clientId, await fresh(), resource), code_verifier: VERIFIER.replace(/^./, 'x') });
    expect([res.status(), (await res.json()).error]).toEqual([400, 'invalid_grant']);
    const { resource: _dropped, ...noResource } = codeForm(clientId, await fresh(), resource);
    res = await post(noResource);
    expect([res.status(), (await res.json()).error]).toEqual([400, 'invalid_request']);
    res = await post({ ...codeForm(clientId, await fresh(), resource), resource: 'https://other.example/mcp' });
    expect([res.status(), (await res.json()).error]).toEqual([400, 'invalid_target']);
  });

  test('the authorization endpoint refuses a request with no PKCE challenge or the wrong resource at the redirect URI', async ({ request }) => {
    const resource = await resourceOf(request);
    const clientId = await registerClient(request);
    const noPkce = authParams(clientId, resource);
    delete (noPkce as Record<string, string | undefined>).code_challenge;
    let res = await request.get(`/oauth/authorize?${new URLSearchParams(noPkce)}`, { maxRedirects: 0 });
    expect(res.status()).toBe(303);
    expect(new URL(res.headers()['location']).searchParams.get('error')).toBe('invalid_request');
    res = await request.get(`/oauth/authorize?${new URLSearchParams({ ...authParams(clientId, resource), resource: 'https://other.example/mcp' })}`, { maxRedirects: 0 });
    expect(new URL(res.headers()['location']).searchParams.get('error')).toBe('invalid_target');
  });

  test('a redirect URI the client did not register is shown to the person, never followed', async ({ request }) => {
    const resource = await resourceOf(request);
    const clientId = await registerClient(request);
    const res = await request.get(`/oauth/authorize?${new URLSearchParams({ ...authParams(clientId, resource), redirect_uri: 'https://evil.example/cb' })}`, {
      maxRedirects: 0,
      failOnStatusCode: false,
    });
    expect(res.status()).toBe(400);
    expect(res.headers()['location']).toBeUndefined();
  });

  test('a client metadata URL that is not https, or is a private address, is refused', async ({ request }) => {
    const resource = await resourceOf(request);
    for (const id of ['http://agent.example/client.json', 'https://127.0.0.1/client.json', 'https://169.254.169.254/latest/meta-data']) {
      const res = await request.get(`/oauth/authorize?${new URLSearchParams(authParams(id, resource))}`, { maxRedirects: 0, failOnStatusCode: false });
      expect(res.status(), id).toBe(400);
      expect(res.headers()['location']).toBeUndefined();
    }
  });

  test('registration refuses a redirect URI that is plain http to a public host', async ({ request }) => {
    const res = await request.post('/oauth/register', { data: { redirect_uris: ['http://app.example/cb'] }, failOnStatusCode: false });
    expect(res.status()).toBe(400);
    expect((await res.json()).error).toBe('invalid_redirect_uri');
  });
});

// The server without the dev bypass, as in production (ADR-00030, ADR-00029).
test.describe('without a session', () => {
  async function anonymous(playwright: Playwright) {
    return playwright.request.newContext({ baseURL: process.env.GOBBLER_E2E_ANONYMOUS_URL });
  }

  test('discovery, registration and the token endpoint answer, and the authorization endpoint sends the person to sign in', async ({ playwright }) => {
    const ctx = await anonymous(playwright);
    try {
      expect((await ctx.get('/.well-known/oauth-protected-resource')).status()).toBe(200);
      expect((await ctx.get('/.well-known/oauth-protected-resource/mcp')).status()).toBe(200);
      const meta = await ctx.get('/.well-known/oauth-authorization-server');
      expect(meta.status()).toBe(200);
      expect((await meta.json()).issuer).toBe('https://gobbler.e2e.invalid');

      const reg = await ctx.post('/oauth/register', { data: { client_name: 'Anon', redirect_uris: [REDIRECT] } });
      expect(reg.status()).toBe(201);
      const clientId = (await reg.json()).client_id as string;
      const token = await ctx.post('/oauth/token', { form: { grant_type: 'authorization_code', client_id: clientId, code: 'goc_nope' }, failOnStatusCode: false });
      expect(token.status()).toBe(400);

      const query = new URLSearchParams(authParams(clientId, 'https://gobbler.e2e.invalid/mcp'));
      const authz = await ctx.get(`/oauth/authorize?${query}`, { maxRedirects: 0, failOnStatusCode: false });
      expect(authz.status()).toBe(303);
      const location = authz.headers()['location'];
      expect(location.startsWith('/login?next=')).toBe(true);
      expect(decodeURIComponent(location)).toContain(`/oauth/authorize?response_type=code&client_id=${clientId}`);

      const post = await ctx.post('/oauth/authorize', { form: { ...authParams(clientId, 'x'), decision: 'allow' }, failOnStatusCode: false });
      expect(post.status()).toBe(401);
    } finally {
      await ctx.dispose();
    }
  });
});
