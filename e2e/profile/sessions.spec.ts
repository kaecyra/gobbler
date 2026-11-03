import { test, expect, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { OTHER, asIdentity } from '../fixtures/identity';
import { expectTapTargets } from '../fixtures/tap';

// The sessions section of the profile (ADR-00032). It runs against the sign-in
// server, whose fake issuer signs in as DEV, so real sessions exist to list and
// end. That server keeps the dev bypass, which decides every request's identity,
// so what happens to an ended session's next request is proved against the real
// router in Go (internal/web/profile/sessions_test.go); here we prove what the
// person sees and does. The sessions belong to one email shared with the
// sign-in spec, so every test finds its own sessions by an address it made up
// and the tests run one after another.
const signinURL = process.env.GOBBLER_E2E_SIGNIN_URL;
if (!signinURL) throw new Error('GOBBLER_E2E_SIGNIN_URL is not set; run through playwright.config.ts');
test.use({ baseURL: signinURL });
test.describe.configure({ mode: 'serial' });

const SESSION = '__Host-gobbler-session';
const PENDING = '__Host-gobbler-signin';
const FIREFOX = 'Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0';
const SAFARI = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15';
const ANDROID = 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36';

let counter = 0;
/** An address in the documentation range that no other test in this run uses. */
function freshIP(): string {
  counter += 1;
  return `198.51.${(process.pid % 100) + 100}.${counter}`;
}

function cookieValue(res: Response, name: string): string | undefined {
  for (const line of res.headers.getSetCookie()) {
    const pair = line.split(';')[0].trim();
    const eq = pair.indexOf('=');
    if (pair.slice(0, eq) === name) return pair.slice(eq + 1);
  }
  return undefined;
}

/** Signs in through the fake issuer as the browser ua behind the address ip, and returns the session cookie. */
async function signIn(ua: string, ip: string): Promise<string> {
  const headers = { 'user-agent': ua, 'cf-connecting-ip': ip };
  const start = await fetch(`${signinURL}/login?next=%2F`, { redirect: 'manual', headers });
  const pending = cookieValue(start, PENDING);
  const approved = await fetch(start.headers.get('location') ?? '', { redirect: 'manual' });
  const done = await fetch(approved.headers.get('location') ?? '', {
    redirect: 'manual',
    headers: { ...headers, cookie: `${PENDING}=${pending}` },
  });
  expect(done.status).toBe(303);
  const token = cookieValue(done, SESSION);
  expect(token, 'the session cookie').toBeTruthy();
  return token!;
}

/** Opens the profile of DEV as the browser whose session is token and waits for the list. */
async function openProfile(page: Page, token: string): Promise<string> {
  await page.context().setExtraHTTPHeaders({ cookie: `${SESSION}=${token}` });
  await page.goto('/profile');
  await expect(page.getByTestId('sessions-list')).toBeVisible();
  return new URL(page.url()).pathname;
}

const row = (page: Page, ip: string) => page.getByTestId('session').filter({ hasText: ip });

test('lists each session with its browser, system, address and times, and marks the current one', async ({ page, request }) => {
  const [ipA, ipB, ipC] = [freshIP(), freshIP(), freshIP()];
  const a = await signIn(FIREFOX, ipA);
  await signIn(SAFARI, ipB);
  await signIn(ANDROID, ipC);
  const path = await openProfile(page, a);

  const mine = row(page, ipA);
  await expect(mine).toHaveAttribute('data-current', 'true');
  await expect(mine.getByTestId('session-device')).toHaveText('Firefox on Linux');
  await expect(mine.getByTestId('session-current')).toBeVisible();
  await expect(mine.getByTestId('session-end')).toHaveCount(0);
  await expect(mine).toContainText('started');
  await expect(mine).toContainText('last used');
  await expect(mine.locator('time').first()).toHaveAttribute('datetime', /^\d{4}-\d\d-\d\dT/);

  await expect(row(page, ipB)).toHaveAttribute('data-current', 'false');
  await expect(row(page, ipB).getByTestId('session-device')).toHaveText('Safari on macOS');
  await expect(row(page, ipB).getByTestId('session-end')).toBeVisible();
  await expect(row(page, ipC).getByTestId('session-device')).toHaveText('Chrome on Android');
  await expect(page.getByTestId('session-current')).toHaveCount(1);

  // A browser without scripts opens the list as a page of its own.
  const plain = await request.get(`/profile/${path.split('/')[2]}/sessions`, { headers: { cookie: `${SESSION}=${a}` } });
  expect(plain.status()).toBe(200);
  expect(await plain.text()).toContain(ipB);
});

test('ends one session, and only that one', async ({ page }) => {
  const [ipA, ipB, ipC] = [freshIP(), freshIP(), freshIP()];
  const a = await signIn(FIREFOX, ipA);
  await signIn(SAFARI, ipB);
  await signIn(ANDROID, ipC);
  await openProfile(page, a);

  await row(page, ipB).getByRole('button', { name: 'End this session' }).click();
  await expect(page.getByTestId('sessions-list')).toBeVisible();
  await expect(row(page, ipB)).toHaveCount(0);
  await expect(row(page, ipC)).toHaveCount(1);
  await expect(row(page, ipA)).toHaveAttribute('data-current', 'true');
});

test('ends every other session and keeps the current one', async ({ page }) => {
  const [ipA, ipB, ipC] = [freshIP(), freshIP(), freshIP()];
  await signIn(SAFARI, ipB);
  const a = await signIn(FIREFOX, ipA);
  await signIn(ANDROID, ipC);
  await openProfile(page, a);

  await page.getByRole('button', { name: 'End every other session' }).click();
  await expect(page.getByTestId('sessions-list')).toBeVisible();
  await expect(row(page, ipB)).toHaveCount(0);
  await expect(row(page, ipC)).toHaveCount(0);
  await expect(row(page, ipA)).toHaveAttribute('data-current', 'true');
  await expect(page.getByTestId('sessions-end-others')).toHaveCount(0);
});

test('its controls are at least 44px tall', async ({ page }) => {
  const a = await signIn(FIREFOX, freshIP());
  await signIn(SAFARI, freshIP());
  await openProfile(page, a);
  await expect(page.getByTestId('sessions-end-others')).toBeVisible();
  await expectTapTargets(page, '[data-testid=profile-sessions]');
});

test('another member sees no sessions on the profile and finds no session routes', async ({ page, playwright }) => {
  const a = await signIn(FIREFOX, freshIP());
  const path = await openProfile(page, a);
  const address = path.split('/')[2];

  const other = await playwright.request.newContext({ baseURL: signinURL, ...asIdentity(OTHER) });
  const view = await other.get(`/profile/${address}`);
  expect(view.status()).toBe(200);
  const html = await view.text();
  expect(html).not.toContain('Sessions');
  expect(html).not.toContain('sessions-slot');
  expect((await other.get(`/profile/${address}/sessions`)).status()).toBe(404);
  expect((await other.post(`/profile/${address}/sessions/end`, { form: { id: 'a'.repeat(64) } })).status()).toBe(404);
  expect((await other.post(`/profile/${address}/sessions/end-others`)).status()).toBe(404);
  await other.dispose();
});

// Screenshots for review: GOBBLER_E2E_SHOTS=<dir> writes them, nothing otherwise.
const shots = process.env.GOBBLER_E2E_SHOTS;
for (const [w, h] of [[1024, 768], [390, 844]]) {
  for (const mode of ['light', 'dark']) {
    test(`screenshot ${w}x${h} ${mode}`, async ({ page }) => {
      test.skip(!shots, 'GOBBLER_E2E_SHOTS is not set');
      mkdirSync(shots!, { recursive: true });
      await page.setViewportSize({ width: w, height: h });
      await page.addInitScript((m) => localStorage.setItem('gob-mode', m), mode);
      const a = await signIn(FIREFOX, freshIP());
      await signIn(ANDROID, freshIP());
      await signIn(SAFARI, freshIP());
      await openProfile(page, a);
      await page.getByTestId('profile-sessions').scrollIntoViewIfNeeded();
      await page.screenshot({ path: `${shots}/sessions-${w}x${h}-${mode}.png`, fullPage: true });
    });
  }
}
