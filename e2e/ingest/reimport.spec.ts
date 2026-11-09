import { test, expect, type Page } from '@playwright/test';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { portFromEnv } from '../fixtures/ports.mjs';
import { openSaved, pasteText, png } from './helpers';

// Re-importing a saved recipe from its source (ADR-00042). The page is fetched
// from a local fixture server that the spec changes between imports; the e2e
// server runs with the dev auth bypass, so it may fetch loopback addresses. The
// spec starts its own gobbler: a household holds at most 20 drafts and the
// shared server's are used by the other specs.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_REIMPORT_PORT');
const origin = `http://127.0.0.1:${port}`;
test.use({ baseURL: origin });
// Each test builds on a recipe the one before left; one worker, in order.
test.describe.configure({ mode: 'serial' });

let server: ChildProcess | undefined;
let dataDir = '';
let fixture: Server;
let fixtureURL = '';
let fetches = 0;
let down = false;
// What the source says now; the spec changes it to see the re-import read it.
let title = 'Reimport Toast';

const PICTURES: Record<string, Buffer> = {
  '/pics/one.png': png(1200, 400, [200, 90, 60]),
  '/pics/two.png': png(1600, 900, [60, 130, 200]),
};

const page = (name: string) => `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"${name}",
 "image":["${fixtureURL}/pics/one.png","${fixtureURL}/pics/two.png"],
 "recipeIngredient":["2 slices bread","1 tbsp butter"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Toast the bread."},{"@type":"HowToStep","text":"Butter it."}]}
</script></head><body><h1>${name}</h1></body></html>`;

test.beforeAll(async () => {
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-reimport-'));
  server = spawn(bin!, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
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
    if (Date.now() > deadline) throw new Error('the re-import server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
  fixture = createServer((req, res) => {
    const pic = PICTURES[req.url ?? ''];
    if (pic) {
      res.writeHead(200, { 'content-type': 'image/png' });
      res.end(pic);
    } else if (req.url === '/toast') {
      fetches++;
      if (down) {
        res.writeHead(503, { 'content-type': 'text/plain' });
        res.end('down for maintenance');
        return;
      }
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      res.end(page(title));
    } else {
      res.writeHead(404, { 'content-type': 'text/plain' });
      res.end('no such page');
    }
  });
  await new Promise<void>((resolve) => fixture.listen(0, '127.0.0.1', resolve));
  fixtureURL = `http://127.0.0.1:${(fixture.address() as AddressInfo).port}`;
});

test.afterAll(async () => {
  await new Promise<void>((resolve) => fixture.close(() => resolve()));
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

const picker = (p: Page) => p.getByRole('radiogroup', { name: 'Picture for this recipe' });
const confirm = (p: Page) => p.getByRole('button', { name: 'Confirm and save' }).first();
const callout = (p: Page) => p.getByTestId('reimport-callout');
const CALLOUT = 'Saving replaces the current recipe. Your earlier edits are kept in History.';

let recipeId = '';
let typedId = '';

async function pictureSrc(p: Page): Promise<string> {
  return (await p.getByTestId('recipe-picture').locator('img').getAttribute('src')) ?? '';
}

test('an import carries no re-import callout and no Keep current picture; its recipe offers the button', async ({ page: p }) => {
  await p.goto('/ingest');
  await p.getByLabel('Recipe URL').fill(`${fixtureURL}/toast`);
  await p.getByRole('button', { name: 'Read recipe' }).click();
  await expect(p).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  await expect(callout(p)).toHaveCount(0);
  await expect(p.getByText(CALLOUT)).toHaveCount(0);
  await expect(picker(p).getByText('Keep current picture')).toHaveCount(0);
  await picker(p).locator('.pick', { hasText: '1024 × 341' }).click();
  await confirm(p).click();
  await openSaved(p);
  await expect(p).toHaveURL(/\/recipes\/\d+(?:-[a-z0-9-]*)?$/);
  recipeId = p.url().match(/\/recipes\/(\d+)(?:-[a-z0-9-]*)?$/)![1]!;
  // ADR-00042 obligation 9: the button is among the action links, after Share and above Delete.
  const button = p.getByTestId('reimport-button');
  await expect(button).toHaveText('Re-import from source');
  await expect(p.getByTestId('reimport-banner')).toHaveCount(0);
  const links = await p.locator('aside .tool-links li').allInnerTexts();
  expect(links.map((l) => l.trim())).toEqual(['History', 'Edit', 'Share', 'Re-import from source']);
  const order = await p.locator('aside').evaluate((aside) => {
    const all = [...aside.querySelectorAll('[data-testid=share-link], [data-testid=reimport-button], [data-testid=delete-link]')];
    return all.map((e) => e.getAttribute('data-testid'));
  });
  expect(order).toEqual(['share-link', 'reimport-button', 'delete-link']);
  expect(await button.evaluate((b) => b.closest('form')?.method)).toBe('post');
  expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(44);
});

test('a recipe with no source URL has no button', async ({ page: p }) => {
  await p.goto('/ingest');
  await pasteText(p, 'Typed Toast\n\nIngredients\n- 2 slices bread\n\nInstructions\n1. Toast it.\n');
  await p.getByRole('button', { name: 'Read recipe' }).click();
  await expect(p).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  await confirm(p).click();
  await openSaved(p);
  await expect(p).toHaveURL(/\/recipes\/\d+(?:-[a-z0-9-]*)?$/);
  typedId = p.url().match(/\/recipes\/(\d+)(?:-[a-z0-9-]*)?$/)![1]!;
  await expect(p.getByTestId('reimport-button')).toHaveCount(0);
  await expect(p.getByText('Re-import from source')).toHaveCount(0);
});

test('a failed fetch shows its message, holds no draft and leaves the recipe', async ({ page: p }) => {
  down = true;
  await p.goto(`/recipes/${recipeId}`);
  await p.getByTestId('reimport-button').click();
  await expect(p.getByText(/Could not fetch that page/)).toBeVisible();
  await expect(p.getByText('The recipe is unchanged.')).toBeVisible();
  down = false;
  await p.goto(`/recipes/${recipeId}`);
  await expect(p.getByTestId('reimport-banner')).toHaveCount(0);
  await expect(p.getByRole('heading', { name: 'Reimport Toast' })).toBeVisible();
});

test('starting a re-import lands on the review with the callout, and the recipe page shows its banner', async ({ page: p }) => {
  title = 'Reimport Toast, read again';
  const before = fetches;
  await p.goto(`/recipes/${recipeId}`);
  await p.getByTestId('reimport-button').click();
  await expect(p).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  expect(fetches).toBe(before + 1);
  const reviewURL = p.url();
  await expect(callout(p)).toHaveText(CALLOUT);
  // "Keep current picture" is offered beside the candidates and No picture, and chosen.
  const group = picker(p);
  await expect(group.getByRole('radio')).toHaveCount(4);
  await expect(group.getByRole('radio', { name: /Keep current picture/ })).toBeChecked();
  await expect(p.getByText('Pick a picture, or No picture, before saving.')).toBeHidden();
  // The draft's own confidence flags are on the screen, as for an import.
  await expect(p.getByLabel('Name', { exact: true })).toHaveValue('Reimport Toast, read again');

  // ADR-00042 obligation 12: the recipe page announces it, with no inline marks.
  await p.goto(`/recipes/${recipeId}`);
  const banner = p.getByTestId('reimport-banner');
  await expect(banner).toContainText('A re-import is waiting for review');
  await expect(p.getByTestId('reimport-review')).toHaveAttribute('href', new URL(reviewURL).pathname);
  await expect(p.getByTestId('proposal-banner')).toHaveCount(0);
  await expect(p.getByTestId('proposal-fields')).toHaveCount(0);
  await expect(p.getByRole('heading', { name: 'Reimport Toast' })).toBeVisible();
  await p.getByTestId('reimport-review').click();
  await expect(p).toHaveURL(reviewURL);
});

test('after the recipe is edited the banner says it changed and offers Discard; discarding returns to the recipe', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}/edit`);
  await p.getByLabel('Name', { exact: true }).fill('Edited Toast');
  await p.getByRole('button', { name: 'Save recipe' }).first().click();
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  await expect(p.getByTestId('reimport-stale')).toContainText('The recipe changed since the re-import');
  await expect(p.getByTestId('reimport-review')).toHaveCount(0);
  await p.getByTestId('reimport-discard').click();
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  await expect(p.getByTestId('reimport-banner')).toHaveCount(0);
  await expect(p.getByRole('heading', { name: 'Edited Toast' })).toBeVisible();
});

test('saving a re-import with the default replaces the recipe, keeps its picture and says so in History', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}`);
  const before = await pictureSrc(p);
  expect(before).toMatch(new RegExp(`^/recipes/${recipeId}/picture\\?v=`));
  await p.getByTestId('reimport-button').click();
  await expect(p).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  await confirm(p).click();
  await openSaved(p);
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  await expect(p.getByRole('heading', { name: 'Reimport Toast, read again' })).toBeVisible();
  expect(await pictureSrc(p)).toBe(before);
  await expect(p.getByTestId('reimport-banner')).toHaveCount(0);

  // The earlier edit is in History, and History says where this version came from.
  await p.goto(`/recipes/${recipeId}/history`);
  await expect(p.getByTestId('current-proposal')).toHaveText(/^Re-imported from 127\.0\.0\.1\.$/);
  await expect(p.getByTestId('revision')).toHaveCount(2);
  await p.getByTestId('revision').first().getByRole('link', { name: /Version/ }).click();
  await expect(p.getByRole('heading', { name: 'Edited Toast' })).toBeVisible();
  // The library still holds two recipes, the imported one and the typed one: a re-import made no second.
  await p.goto('/recipes');
  const hrefs = await p.locator('main a[href^="/recipes/"]').evaluateAll((as) =>
    [...new Set(as.map((a) => a.getAttribute('href')).filter((h) => /^\/recipes\/\d+(?:-[a-z0-9-]*)?$/.test(h ?? '')).map((h) => h!.replace(/^(\/recipes\/\d+).*$/, '$1')))]);
  expect(hrefs.sort()).toEqual([`/recipes/${recipeId}`, `/recipes/${typedId}`].sort());
});

test('choosing a candidate replaces the picture', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}`);
  const before = await pictureSrc(p);
  await p.getByTestId('reimport-button').click();
  await picker(p).locator('.pick', { hasText: '1024 × 576' }).click();
  await confirm(p).click();
  await openSaved(p);
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  const after = await pictureSrc(p);
  expect(after).not.toBe(before);
  expect((await p.request.get(after)).status()).toBe(200);
});

test('No picture removes the picture', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}`);
  await p.getByTestId('reimport-button').click();
  await picker(p).locator('.pick.none').click();
  await confirm(p).click();
  await openSaved(p);
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  expect((await p.request.get(`/recipes/${recipeId}/picture`)).status()).toBe(404);
});

test('cancelling a re-import returns to the recipe and leaves it as it was', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}`);
  await p.getByTestId('reimport-button').click();
  await p.getByRole('button', { name: 'Cancel' }).first().click();
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  await expect(p.getByTestId('reimport-banner')).toHaveCount(0);
});

test('starting a re-import of a recipe with no source address is refused', async ({ page: p, request }) => {
  const res = await request.post(`/ingest/reimport/${typedId}`, { headers: { 'Sec-Fetch-Site': 'same-origin' } });
  expect(res.status()).toBe(409);
  expect(await res.text()).toContain('no source address');
  expect((await request.get(`/ingest/reimport/${typedId}`)).status()).toBe(405);
  void p;
});
