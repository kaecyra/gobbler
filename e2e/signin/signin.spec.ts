import { test, expect } from '@playwright/test';

// Sign-in over OpenID Connect (ADR-00030) against the sign-in server instance:
// the dev bypass with sign-in configured against the local fake issuer
// (fixtures/issuer.mjs), never a real provider. The bypass still decides every
// request's identity there, so these tests drive the sign-in routes themselves:
// the redirect to the issuer with PKCE, state and nonce, the callback, the
// cookies it sets, the return target and sign-out. That the session cookie is
// then the identity is proved against the real router in Go
// (internal/web/signin_flow_test.go). Requests go through fetch with redirects
// off, so every hop and every Set-Cookie is seen as the browser would see it.
const signinURL = process.env.GOBBLER_E2E_SIGNIN_URL;
if (!signinURL) throw new Error('GOBBLER_E2E_SIGNIN_URL is not set; run through playwright.config.ts');
test.use({ baseURL: signinURL });

const PENDING = '__Host-gobbler-signin';
const SESSION = '__Host-gobbler-session';

interface SetCookie {
  value: string;
  attrs: string[];
}

function setCookie(res: Response, name: string): SetCookie | undefined {
  for (const line of res.headers.getSetCookie()) {
    const [pair, ...attrs] = line.split(';').map((s) => s.trim());
    const eq = pair.indexOf('=');
    if (pair.slice(0, eq) === name) return { value: pair.slice(eq + 1), attrs };
  }
  return undefined;
}

function hop(url: string, init: RequestInit = {}): Promise<Response> {
  return fetch(url, { redirect: 'manual', ...init });
}

/** Starts a sign-in for next and follows it to the issuer and back, stopping before the callback. */
async function toCallback(next: string): Promise<{ back: URL; pending: SetCookie; authorize: URL }> {
  const start = await hop(`${signinURL}/login?next=${encodeURIComponent(next)}`);
  expect(start.status).toBe(303);
  const pending = setCookie(start, PENDING);
  expect(pending, 'the sign-in cookie').toBeDefined();
  const authorize = new URL(start.headers.get('location') ?? '');
  const approved = await hop(authorize.href);
  expect(approved.status).toBe(302);
  return { back: new URL(approved.headers.get('location') ?? ''), pending: pending!, authorize };
}

test('sign-in goes to the issuer with PKCE, state and nonce, and the callback starts a session', async () => {
  const { back, pending, authorize } = await toCallback('/recipes?text=pie');
  const q = authorize.searchParams;
  expect(q.get('code_challenge_method')).toBe('S256');
  expect(q.get('code_challenge')).toBeTruthy();
  expect(q.get('state')).toBeTruthy();
  expect(q.get('nonce')).toBeTruthy();
  expect(q.get('scope')).toBe('openid email');
  expect(q.get('redirect_uri')).toBe(`${signinURL}/auth/callback`);
  expect(pending.attrs).toEqual(expect.arrayContaining(['Path=/', 'Max-Age=600', 'HttpOnly', 'Secure', 'SameSite=Lax']));

  expect(back.pathname).toBe('/auth/callback');
  expect(back.searchParams.get('state')).toBe(q.get('state'));
  const done = await hop(back.href, { headers: { cookie: `${PENDING}=${pending.value}` } });
  expect(done.status).toBe(303);
  expect(done.headers.get('location')).toBe('/recipes?text=pie');
  const session = setCookie(done, SESSION);
  expect(session?.value, 'the session cookie').toBeTruthy();
  expect(session!.attrs).toEqual(expect.arrayContaining(['Path=/', 'Max-Age=2592000', 'HttpOnly', 'Secure', 'SameSite=Lax']));
  expect(setCookie(done, PENDING)?.attrs, 'the sign-in cookie is cleared').toContain('Max-Age=0');

  // The code was used once and cannot be used again.
  const replay = await hop(back.href, { headers: { cookie: `${PENDING}=${pending.value}` } });
  expect(replay.status).toBe(400);

  const out = await hop(`${signinURL}/logout`, { method: 'POST', headers: { cookie: `${SESSION}=${session!.value}` } });
  expect(out.status).toBe(303);
  expect(out.headers.get('location')).toBe('/');
  expect(setCookie(out, SESSION)?.attrs).toContain('Max-Age=0');
});

test('a callback whose state does not match is refused without a session', async () => {
  const { back, pending } = await toCallback('/');
  back.searchParams.set('state', 'forged');
  const res = await hop(back.href, { headers: { cookie: `${PENDING}=${pending.value}` } });
  expect(res.status).toBe(400);
  expect(setCookie(res, SESSION)).toBeUndefined();
});

test('a callback without the sign-in cookie is refused', async () => {
  const { back } = await toCallback('/');
  const res = await hop(back.href);
  expect(res.status).toBe(400);
  expect(setCookie(res, SESSION)).toBeUndefined();
});

test('the return target cannot leave the site', async () => {
  for (const next of ['https://evil.example/', '//evil.example/', '/\\evil.example']) {
    const { back, pending } = await toCallback(next);
    const res = await hop(back.href, { headers: { cookie: `${PENDING}=${pending.value}` } });
    expect(res.status, next).toBe(303);
    expect(res.headers.get('location'), next).toBe('/');
  }
});

test('a cross-site sign-out is refused', async () => {
  const res = await hop(`${signinURL}/logout`, { method: 'POST', headers: { 'Sec-Fetch-Site': 'cross-site' } });
  expect(res.status).toBe(403);
});

test('the nav menu offers sign-out as a form button', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'More sections' }).click();
  const signOut = page.getByRole('button', { name: 'Sign out' });
  await expect(signOut).toBeVisible();
  await signOut.click();
  await expect(page).toHaveURL(`${signinURL}/`);
});
