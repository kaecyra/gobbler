import { test, expect, type Page } from '@playwright/test';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { portFromEnv } from '../fixtures/ports.mjs';
import { openSaved, pasteText, png } from '../ingest/helpers';

// The picture picker at the top of the edit page (ADR-00051). The source page
// is served by a local fixture the spec changes between tests; the server runs
// with the dev auth bypass, so it may fetch loopback addresses.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_EDIT_PICTURE_PORT');
const origin = `http://127.0.0.1:${port}`;
test.use({ baseURL: origin });
// Each test builds on the recipe the one before left; one worker, in order.
test.describe.configure({ mode: 'serial' });

let server: ChildProcess | undefined;
let dataDir = '';
let fixture: Server;
let fixtureURL = '';
// What the source page does now.
let empty = false;
let delayMs = 0;

const PICTURES: Record<string, Buffer> = {
  '/pics/one.png': png(1200, 400, [200, 90, 60]),
  '/pics/two.png': png(1600, 900, [60, 130, 200]),
};
const UPLOAD = png(800, 600, [90, 160, 90]);

const sourcePage = () => `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Picture Toast",
 ${empty ? '' : `"image":["${fixtureURL}/pics/one.png","${fixtureURL}/pics/two.png"],`}
 "recipeIngredient":["2 slices bread","1 tbsp butter"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Toast the bread."},{"@type":"HowToStep","text":"Butter it."}]}
</script></head><body><h1>Picture Toast</h1></body></html>`;

test.beforeAll(async () => {
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-edit-picture-'));
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
    if (Date.now() > deadline) throw new Error('the edit picture server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
  fixture = createServer((req, res) => {
    const pic = PICTURES[req.url ?? ''];
    if (pic) {
      res.writeHead(200, { 'content-type': 'image/png' });
      res.end(pic);
    } else if (req.url === '/toast') {
      setTimeout(() => {
        res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
        res.end(sourcePage());
      }, delayMs);
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

const picker = (p: Page) => p.getByTestId('edit-picture');
// An offered picture's ID (ADR-00051).
const OFFER_ID = /^[A-Za-z0-9_-]{22}$/;
const tiles = (p: Page) => picker(p).getByRole('radiogroup', { name: 'Picture for this recipe' });
const searchLink = (p: Page) => p.getByTestId('picture-search');
const save = (p: Page) => p.getByRole('button', { name: 'Save recipe' }).first();

let recipeId = '';

async function pictureSrc(p: Page): Promise<string> {
  await p.goto(`/recipes/${recipeId}`);
  return (await p.getByTestId('recipe-picture').locator('img').getAttribute('src')) ?? '';
}

test('a recipe imported with No picture', async ({ page: p }) => {
  await p.goto('/ingest');
  await p.getByLabel('Recipe URL').fill(`${fixtureURL}/toast`);
  await p.getByRole('button', { name: 'Read recipe' }).click();
  await expect(p).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  await p.getByRole('radiogroup', { name: 'Picture for this recipe' }).locator('.pick.none').click();
  await p.getByRole('button', { name: 'Confirm and save' }).first().click();
  await openSaved(p);
  recipeId = p.url().match(/\/recipes\/(\d+)(?:-[a-z0-9-]*)?$/)![1]!;
  expect((await p.request.get(`/recipes/${recipeId}/picture`)).status()).toBe(404);
});

test('opening the edit page of a recipe with no picture searches on its own', async ({ page: p }) => {
  delayMs = 1500;
  await p.goto(`/recipes/${recipeId}/edit`);
  // The page is drawn at once, holding two shimmering places for the results.
  await expect(picker(p).locator('.pick.ghost')).toHaveCount(2);
  await expect(picker(p).locator('.pick.ghost').first()).toBeVisible();
  await expect(picker(p).getByRole('button', { name: 'Searching the source page' })).toBeVisible();
  await expect(searchLink(p)).toBeDisabled();
  await expect(tiles(p).getByRole('radio', { name: /No picture/ })).toBeChecked();
  await expect(tiles(p).getByText('Shows a coloured band')).toBeVisible();
  // Then the finds replace them.
  await expect(tiles(p).getByText('Recipe image')).toHaveCount(2);
  await expect(picker(p).locator('.pick.ghost').first()).toBeHidden();
  await expect(picker(p).getByRole('button', { name: 'Search the source page again' })).toBeVisible();
  await expect(tiles(p).getByRole('radio', { name: /No picture/ })).toBeChecked();
  await expect(tiles(p).getByText('Current picture')).toHaveCount(0);
  await expect(picker(p).getByText('Upload a picture')).toBeVisible();
  delayMs = 0;
});

test('choosing a found picture applies it on Save', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}/edit`);
  // What was found is kept: the page offers it again without a search.
  await expect(picker(p).getByRole('button', { name: 'Search the source page again' })).toBeVisible();
  const chosen = tiles(p).locator('.pick', { hasText: '1024 × 576' });
  const offer = await chosen.getByRole('radio').getAttribute('value');
  expect(offer).toMatch(OFFER_ID);
  await chosen.click();
  await save(p).click();
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  const src = await pictureSrc(p);
  expect(src).toMatch(new RegExp(`^/recipes/${recipeId}/picture\\?v=`));
  expect((await p.request.get(src)).status()).toBe(200);
  // Saving cleared what was offered.
  expect((await p.request.get(`/recipes/${recipeId}/picture/offered/${offer}`)).status()).toBe(404);
});

test('with a picture the page offers it, searches only when asked, and Back changes nothing', async ({ page: p }) => {
  const before = await pictureSrc(p);
  await p.goto(`/recipes/${recipeId}/edit`);
  await expect(tiles(p).getByRole('radio', { name: /Current picture/ })).toBeChecked();
  await expect(tiles(p).getByText('Left as it is')).toBeVisible();
  await expect(tiles(p).getByText('Removes it')).toBeVisible();
  await expect(tiles(p).getByText('Recipe image')).toHaveCount(0);
  await searchLink(p).click();
  await expect(tiles(p).getByText('Recipe image')).toHaveCount(2);
  await expect(tiles(p).getByRole('radio', { name: /Current picture/ })).toBeChecked();
  await tiles(p).locator('.pick', { hasText: '1024 × 341' }).click();
  await p.getByRole('link', { name: 'Back to the recipe without saving' }).click();
  expect(await pictureSrc(p)).toBe(before);
});

test('searching again keeps the choice made in the picker', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}/edit`);
  await tiles(p).locator('.pick.none').click();
  await searchLink(p).click();
  await expect(tiles(p).getByText('Recipe image')).toHaveCount(2);
  await expect(tiles(p).getByRole('radio', { name: /No picture/ })).toBeChecked();
  // A chosen found picture the search replaces falls back to the current one.
  await tiles(p).locator('.pick', { hasText: '1024 × 341' }).click();
  await searchLink(p).click();
  await expect(tiles(p).getByRole('radio', { name: /Current picture/ })).toBeChecked();
});

test('an uploaded picture becomes a chosen tile and applies on Save', async ({ page: p }) => {
  const before = await pictureSrc(p);
  await p.goto(`/recipes/${recipeId}/edit`);
  await p.getByTestId('picture-upload').locator('input[type=file]').setInputFiles({ name: 'mine.png', mimeType: 'image/png', buffer: UPLOAD });
  await expect(tiles(p).getByText('Uploaded picture')).toBeVisible();
  await expect(tiles(p).getByRole('radio', { name: /Uploaded picture/ })).toBeChecked();
  const upload = await tiles(p).getByRole('radio', { name: /Uploaded picture/ }).getAttribute('value');
  expect(upload).toMatch(OFFER_ID);
  expect((await p.request.get(`/recipes/${recipeId}/picture/offered/${upload}`)).status()).toBe(200);
  expect(await pictureSrc(p)).toBe(before);
  await p.goto(`/recipes/${recipeId}/edit`);
  await expect(tiles(p).getByRole('radio', { name: /Uploaded picture/ })).toHaveCount(1);
  await tiles(p).locator('.pick', { hasText: 'Uploaded picture' }).click();
  await save(p).click();
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  const after = await pictureSrc(p);
  expect(after).not.toBe(before);
  expect((await p.request.get(after)).status()).toBe(200);
});

test('a file that is not a picture is refused in the picker', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}/edit`);
  await p.getByTestId('picture-upload').locator('input[type=file]').setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') });
  await expect(picker(p).locator('.pick-note')).toBeVisible();
  await expect(tiles(p).getByRole('radio', { name: /Current picture/ })).toBeChecked();
  // The refusal keeps a choice made in the picker.
  await tiles(p).locator('.pick.none').click();
  await p.getByTestId('picture-upload').locator('input[type=file]').setInputFiles({ name: 'more.txt', mimeType: 'text/plain', buffer: Buffer.from('still not') });
  await expect(picker(p).locator('.pick-note')).toBeVisible();
  await expect(tiles(p).getByRole('radio', { name: /No picture/ })).toBeChecked();
  // So does one over the size limit, whose body is not read to the end: the
  // choice, made on a fresh page, goes ahead of the file.
  await p.goto(`/recipes/${recipeId}/edit`);
  await expect(tiles(p).getByRole('radio', { name: /Current picture/ })).toBeChecked();
  await tiles(p).locator('.pick.none').click();
  await p.getByTestId('picture-upload').locator('input[type=file]').setInputFiles({ name: 'huge.png', mimeType: 'image/png', buffer: Buffer.alloc(6 * 1024 * 1024, 7) });
  await expect(picker(p).locator('.pick-note')).toContainText('That file is over 5 MB.');
  await expect(tiles(p).getByRole('radio', { name: /No picture/ })).toBeChecked();
});

test('a search that finds nothing says so', async ({ page: p }) => {
  empty = true;
  await p.goto(`/recipes/${recipeId}/edit`);
  await searchLink(p).click();
  await expect(picker(p).getByText('The source page had no usable pictures.')).toBeVisible();
  await expect(tiles(p).getByText('Recipe image')).toHaveCount(0);
  empty = false;
});

test('No picture removes the picture on Save', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}/edit`);
  await tiles(p).locator('.pick.none').click();
  await save(p).click();
  await expect(p).toHaveURL(new RegExp(`/recipes/${recipeId}(?:-[a-z0-9-]*)?$`));
  expect((await p.request.get(`/recipes/${recipeId}/picture`)).status()).toBe(404);
});

test('without scripts a search and an upload land back on the edit page', async ({ request }) => {
  const headers = { 'Sec-Fetch-Site': 'same-origin' };
  const search = await request.post(`/recipes/${recipeId}/edit/picture/search`, { headers, maxRedirects: 0 });
  expect(search.status()).toBe(303);
  expect(search.headers()['location']).toBe(`/recipes/${recipeId}/edit`);
  const upload = await request.post(`/recipes/${recipeId}/edit/picture/upload`, {
    headers, maxRedirects: 0, multipart: { file: { name: 'mine.png', mimeType: 'image/png', buffer: UPLOAD } },
  });
  expect(upload.status()).toBe(303);
  // The edit page now offers it, under an ID of its own.
  const edit = await (await request.get(`/recipes/${recipeId}/edit`)).text();
  const offer = edit.match(/name="picture" value="([A-Za-z0-9_-]{22})"/)?.[1];
  expect(offer, 'the uploaded picture offered on the edit page').toBeTruthy();
  expect((await request.get(`/recipes/${recipeId}/picture/offered/${offer}`)).status()).toBe(200);
});

test('a choice a later search replaced is refused and nothing is saved', async ({ page: p }) => {
  await p.goto(`/recipes/${recipeId}/edit`);
  await searchLink(p).click();
  await expect(tiles(p).getByText('Recipe image')).toHaveCount(2);
  await tiles(p).locator('.pick', { hasText: '1024 × 341' }).click();
  // Another tab, or an agent's find_recipe_pictures, searches again meanwhile.
  const again = await p.request.post(`/recipes/${recipeId}/edit/picture/search`, { headers: { 'Sec-Fetch-Site': 'same-origin' }, maxRedirects: 0 });
  expect(again.status()).toBe(303);
  await p.getByLabel('Name', { exact: true }).fill('Renamed Toast');
  await save(p).click();
  await expect(p.getByText('The picture you chose is no longer offered')).toBeVisible();
  expect((await p.request.get(`/recipes/${recipeId}/picture`)).status()).toBe(404);
  await p.goto(`/recipes/${recipeId}`);
  await expect(p.getByRole('heading', { name: 'Picture Toast' })).toBeVisible();
});

test('a recipe with no source address offers no search', async ({ page: p }) => {
  await p.goto('/ingest');
  await pasteText(p, 'Typed Toast\n\nIngredients\n- 2 slices bread\n\nInstructions\n1. Toast it.\n');
  await p.getByRole('button', { name: 'Read recipe' }).click();
  await p.getByRole('button', { name: 'Confirm and save' }).first().click();
  await openSaved(p);
  const typed = p.url().match(/\/recipes\/(\d+)(?:-[a-z0-9-]*)?$/)![1]!;
  await p.goto(`/recipes/${typed}/edit`);
  await expect(searchLink(p)).toHaveCount(0);
  await expect(picker(p).locator('.pick.ghost').first()).toBeHidden();
  await expect(picker(p).getByText('Upload a picture')).toBeVisible();
});
