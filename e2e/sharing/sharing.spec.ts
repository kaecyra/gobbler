import { test, expect, type BrowserContext } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { DEV, OTHER, STRANGER, OPERATOR } from '../fixtures/identity';
import { as, startHousehold, uniqueEmail } from '../household/support';

// Sending a recipe copy to another household (ADR-00018, Sharing). The sender
// recipe needs data the shared server does not have, so this spec seeds its own
// data directory through the real store (seed/main.go) and starts its own
// gobbler on it in dev-auth mode. DEV and OTHER are the households the dev
// bypass seeds; OPERATOR starts the extra households some tests need.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = Number(process.env.GOBBLER_E2E_SHARING_PORT ?? 38451);
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let recipeId = 0;

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_SHARING_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-sharing-'));
  const out = execFileSync('go', ['run', './e2e/sharing/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
  ({ recipe: recipeId } = JSON.parse(out));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_OPERATOR_EMAILS: OPERATOR,
    },
    stdio: 'inherit',
  });
  const deadline = Date.now() + 20_000;
  for (;;) {
    try {
      if ((await fetch(`${origin}/healthz`)).ok) break;
    } catch {
      // not listening yet
    }
    if (Date.now() > deadline) throw new Error('the sharing server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

const SENT = 'If that address belongs to a member of a household';

/** Sends the seeded recipe to email as DEV and returns the page text it lands on. */
async function sendTo(ctx: BrowserContext, email: string): Promise<{ status: number; url: string; text: string }> {
  const res = await ctx.request.post(`/shares/new/${recipeId}`, { form: { email } });
  return { status: res.status(), url: new URL(res.url()).pathname, text: await res.text() };
}

/** The accept path of every share in the inbox of ctx. */
async function inboxActions(ctx: BrowserContext): Promise<string[]> {
  const page = await ctx.newPage();
  await page.goto('/shares');
  const actions = await page.locator('form[action$="/accept"]').evaluateAll((fs) => fs.map((f) => f.getAttribute('action')!));
  await page.close();
  return actions;
}

test('the sender sees one confirmation for a member, a stranger and an unknown address', async ({ browser, baseURL }) => {
  const dev = await as(browser, baseURL, DEV);
  const page = await dev.newPage();
  await page.goto(`/shares/new/${recipeId}`);
  await expect(page.getByTestId('recipe-name')).toHaveText('E2E Shared Pie');
  await page.getByLabel('Email address').fill(OTHER);
  await page.getByRole('button', { name: 'Send a copy' }).click();
  await expect(page).toHaveURL(/\/shares\/sent$/);
  await expect(page.getByTestId('sent')).toContainText(SENT);

  const member = await sendTo(dev, OTHER);
  const nobody = await sendTo(dev, `nobody-${Date.now()}@e2e.example`);
  const own = await sendTo(dev, DEV);
  const known = await sendTo(dev, OPERATOR);
  for (const r of [member, nobody, own, known]) {
    expect(r.status).toBe(200);
    expect(r.url).toBe('/shares/sent');
    expect(r.text).toBe(member.text);
  }

  // A mistyped address is refused the same way for everyone.
  const bad = await dev.request.post(`/shares/new/${recipeId}`, { form: { email: 'not an email' } });
  expect(bad.status()).toBe(400);
  await dev.close();
});

test('the recipient household sees the share pending and declines it', async ({ browser, baseURL }) => {
  const other = await as(browser, baseURL, OTHER);
  const page = await other.newPage();
  await page.goto('/shares');
  const shares = page.getByTestId('share');
  await expect(shares.first()).toContainText('E2E Shared Pie');
  await expect(shares.first()).toContainText('Dev household');
  // Nothing is in the recipes until a member accepts.
  await page.goto('/recipes');
  await expect(page.locator('body')).not.toContainText('E2E Shared Pie');

  const before = await inboxActions(other);
  await page.goto('/shares');
  await shares.first().getByRole('button', { name: 'Decline E2E Shared Pie' }).click();
  await expect(page).toHaveURL(/\/shares$/);
  expect((await inboxActions(other)).length).toBe(before.length - 1);
  await page.goto('/recipes');
  await expect(page.locator('body')).not.toContainText('E2E Shared Pie');
  await other.close();
});

test('accepting makes an independent copy without the sender private data or overlay', async ({ browser, baseURL }) => {
  const dev = await as(browser, baseURL, DEV);
  await sendTo(dev, OTHER);
  const other = await as(browser, baseURL, OTHER);
  const page = await other.newPage();
  const [accept] = await inboxActions(other);
  await page.goto('/shares');
  await page.getByTestId('share').first().getByRole('button', { name: 'Accept E2E Shared Pie' }).click();

  await expect(page).toHaveURL(/\/recipes\/\d+$/);
  const copyId = page.url().split('/').pop();
  await expect(page.getByRole('heading', { name: 'E2E Shared Pie', level: 1 })).toBeVisible();
  await expect(page.getByText('3 e2e sender fruit')).toBeVisible();
  expect(copyId).not.toBe(String(recipeId));

  // The copy says where it came from, and carries none of the sender's journal.
  await page.goto(`/recipes/${copyId}/journal`);
  await expect(page.locator('body')).toContainText('Shared by Dev household on');
  await expect(page.locator('body')).not.toContainText('e2e sender private note');
  await expect(page.locator('body')).not.toContainText('e2e sender private comment');

  // The sender's ingredient lands in the recipient's own catalog as unreviewed,
  // without the sender's alias.
  await page.goto('/catalog');
  await expect(page.getByTestId('unreviewed')).toContainText('e2e sender fruit');
  await expect(page.locator('body')).not.toContainText('e2e sender alias');

  // The sender still has its own recipe, with its own journal, and no copy of the other's.
  const mine = await dev.newPage();
  await mine.goto(`/recipes/${recipeId}/journal`);
  await expect(mine.locator('body')).toContainText('e2e sender private note');
  await mine.goto('/catalog');
  await expect(mine.locator('body')).toContainText('e2e sender alias');

  // A share is usable once: accepting it again finds nothing.
  expect((await other.request.post(accept)).status()).toBe(404);
  expect(await inboxActions(other)).not.toContain(accept);
  await dev.close();
  await other.close();
});

test.describe('the sharing routes, as a non-member and as a member of another household', () => {
  test('a non-member reaches nothing', async ({ browser, baseURL }) => {
    const ctx = await as(browser, baseURL, STRANGER);
    expect((await ctx.request.get('/shares')).status()).toBe(403);
    expect((await ctx.request.get('/shares/sent')).status()).toBe(403);
    expect((await ctx.request.get(`/shares/new/${recipeId}`)).status()).toBe(403);
    for (const path of [`/shares/new/${recipeId}`, '/shares/1/accept', '/shares/1/decline']) {
      const res = await ctx.request.post(path, { form: { email: OTHER } });
      expect(res.status(), path).toBe(403);
      expect(await res.text(), path).not.toContain('E2E Shared Pie');
    }
    await ctx.close();
  });

  test('a member of a third household can neither send the recipe nor act on its share', async ({ browser, baseURL }) => {
    const dev = await as(browser, baseURL, DEV);
    await sendTo(dev, OTHER);
    const other = await as(browser, baseURL, OTHER);
    const [accept] = await inboxActions(other);
    expect(accept).toMatch(/^\/shares\/\d+\/accept$/);
    const id = accept.split('/')[2];

    const third = await startHousehold(browser, baseURL, 'Third kitchen');
    const page = await third.ctx.newPage();
    await page.goto('/shares');
    await expect(page.getByTestId('no-shares')).toBeVisible();
    await expect(page.locator('body')).not.toContainText('E2E Shared Pie');

    expect((await third.ctx.request.get(`/shares/new/${recipeId}`)).status()).toBe(404);
    expect((await third.ctx.request.post(`/shares/new/${recipeId}`, { form: { email: OTHER } })).status()).toBe(404);
    expect((await third.ctx.request.post(`/shares/${id}/accept`)).status()).toBe(404);
    expect((await third.ctx.request.post(`/shares/${id}/decline`)).status()).toBe(404);
    // The sender cannot accept or decline what it sent either.
    expect((await dev.request.post(`/shares/${id}/accept`)).status()).toBe(404);
    expect((await dev.request.post(`/shares/${id}/decline`)).status()).toBe(404);

    // None of that touched the share, which the recipient can still see.
    expect((await inboxActions(other)).includes(accept)).toBe(true);
    await page.goto('/recipes');
    await expect(page.locator('body')).not.toContainText('E2E Shared Pie');
    await third.ctx.close();
    await dev.close();
    await other.close();
  });

  test('the sender is told the same whether the address is a member or not, and a member receives the share', async ({ browser, baseURL }) => {
    const { email, ctx } = await startHousehold(browser, baseURL, 'Fourth kitchen');
    const dev = await as(browser, baseURL, DEV);
    const known = await sendTo(dev, email);
    const unknown = await sendTo(dev, uniqueEmail('nobody'));
    expect(known.text).toBe(unknown.text);
    expect((await inboxActions(ctx)).length).toBe(1);
    await ctx.close();
    await dev.close();
  });
});
