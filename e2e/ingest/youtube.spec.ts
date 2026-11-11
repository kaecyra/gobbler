import { test, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { portFromEnv } from '../fixtures/ports.mjs';
import { DEV_OPERATOR, ENCRYPTION_KEY, NO_OPENROUTER, openSaved } from './helpers';

// A YouTube link's recipe comes from the video's description, or from a recipe
// page it links (ADR-00054). This spec starts its own gobbler with an
// encryption key, saves the Data API key on the Deployment page the way an
// operator does, and points the lookup (GOBBLER_VIDEO_API_HOST) at a local
// fake of videos.list that also serves the linked blog page. Nothing reaches
// YouTube; loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_YOUTUBE_PORT');
const origin = `http://127.0.0.1:${port}`;

const API_KEY = 'AIza-e2e-youtube-key';
const watch = (id: string) => `https://www.youtube.com/watch?v=${id}`;
const ID_TEXT = 'aaaaaaaaaaa';
const ID_BLOG = 'bbbbbbbbbbb';
const ID_QUOTA = 'ccccccccccc';
const ID_GONE = 'ddddddddddd';

const DESCRIPTION = `Garlic spaghetti in ten minutes.

Ingredients
8 oz spaghetti
3 cloves garlic, minced
1/4 cup olive oil
Salt to taste

Directions
1. Boil the spaghetti for 10 minutes.
2. Fry the garlic in the oil until golden, about 2 minutes.
3. Toss together and serve.
`;

const BLOG = `<!doctype html><html><head><title>Blog Toast</title>
<script type="application/ld+json">${JSON.stringify({
  '@context': 'https://schema.org',
  '@type': 'Recipe',
  name: 'Blog Toast',
  recipeIngredient: ['2 slices bread', '1 tbsp butter'],
  recipeInstructions: [{ '@type': 'HowToStep', text: 'Toast the bread and butter it.' }],
})}</script></head><body><h1>Blog Toast</h1></body></html>`;

let fake: Server;
let blogURL = '';
let asked: { id: string; key: string }[] = [];
let server: ChildProcess | undefined;
let dataDir = '';

const snippet = (title: string, description: string) => JSON.stringify({ items: [{ snippet: { title, description } }] });

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_YOUTUBE_PORT`);

  fake = createServer((req, res) => {
    const u = new URL(req.url ?? '/', 'http://fake');
    if (u.pathname === '/blog') {
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      res.end(BLOG);
      return;
    }
    if (u.pathname !== '/youtube/v3/videos') {
      res.writeHead(404).end();
      return;
    }
    const id = u.searchParams.get('id') ?? '';
    asked.push({ id, key: u.searchParams.get('key') ?? '' });
    const ok = (body: string) => {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(body);
    };
    if (id === ID_TEXT) return ok(snippet('Garlic Spaghetti', DESCRIPTION));
    if (id === ID_BLOG) return ok(snippet('Toast', `Recipe on my blog: ${blogURL}\n`));
    if (id === ID_GONE) return ok('{"items":[]}');
    res.writeHead(403, { 'content-type': 'application/json' });
    res.end('{"error":{"code":403,"message":"quota","errors":[{"domain":"youtube.quota","reason":"quotaExceeded"}]}}');
  });
  await new Promise<void>((r) => fake.listen(0, '127.0.0.1', r));
  const fakeURL = `http://127.0.0.1:${(fake.address() as AddressInfo).port}`;
  blogURL = `${fakeURL}/blog`;

  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-youtube-'));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_ENCRYPTION_KEY: ENCRYPTION_KEY,
      GOBBLER_OPERATOR_EMAILS: DEV_OPERATOR,
      GOBBLER_OPENROUTER_URL: NO_OPENROUTER,
      GOBBLER_VIDEO_API_HOST: fakeURL,
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
    if (Date.now() > deadline) throw new Error('the youtube server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
  const res = await fetch(`${origin}/operator/settings/secrets/youtube/api_key`, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded', 'sec-fetch-site': 'same-origin' },
    body: new URLSearchParams({ value: API_KEY }),
    redirect: 'manual',
  });
  if (res.status !== 303) throw new Error(`saving the YouTube key: ${res.status}`);
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((r) => server!.once('exit', r));
    server.kill('SIGTERM');
    await exited;
  }
  await new Promise<void>((r) => fake?.close(() => r()));
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

test.beforeEach(() => {
  asked = [];
});

async function readLink(page: import('@playwright/test').Page, id: string) {
  await page.goto('/ingest');
  await page.getByLabel('Recipe URL').fill(watch(id));
  await page.getByRole('button', { name: 'Read recipe' }).click();
}

test('the Deployment page shows the saved key as set and never shows its value', async ({ page }) => {
  await page.goto('/operator/settings');
  const section = page.getByTestId('integration-youtube');
  await expect(section.getByRole('heading', { name: 'YouTube' })).toBeVisible();
  await expect(section.getByTestId('secret-state')).toHaveText('Set');
  expect(await page.content()).not.toContain(API_KEY);
});

test('a description with the recipe in it is drafted, and the video stays on the recipe', async ({ page }) => {
  await readLink(page, ID_TEXT);
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  expect(asked).toEqual([{ id: ID_TEXT, key: API_KEY }]);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Garlic Spaghetti');
  await expect(page.locator('[name="c.0.l.0.raw"]')).toHaveValue('8 oz spaghetti');
  await expect(page.getByLabel('YouTube video (link or ID, optional)')).toHaveValue(ID_TEXT);

  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await openSaved(page);
  await expect(page).toHaveURL(/\/recipes\/\d+(?:-[a-z0-9-]*)?$/);
  await expect(page.getByTestId('video')).toHaveCount(1);
});

test('a recipe page the description links wins over the description text', async ({ page }) => {
  await readLink(page, ID_BLOG);
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Blog Toast');
  await expect(page.locator('[name="c.0.l.0.raw"]')).toHaveValue('2 slices bread');
  await expect(page.getByLabel('YouTube video (link or ID, optional)')).toHaveValue(ID_BLOG);
});

test('a used-up quota is said so on the form, with no draft and no key shown', async ({ page }) => {
  await readLink(page, ID_QUOTA);
  const alert = page.getByRole('alert');
  await expect(alert).toContainText("YouTube's daily limit");
  await expect(alert).not.toContainText(API_KEY);
  await expect(page).toHaveURL(/\/ingest$/);
});

test('a private or removed video is said so on the form', async ({ page }) => {
  await readLink(page, ID_GONE);
  await expect(page.getByRole('alert')).toContainText('private or has been removed');
});

for (const host of ['instagram.com', 'www.instagram.com', 'm.instagram.com']) {
  test(`an Instagram link on ${host} is refused with the paste-the-caption message`, async ({ page }) => {
    await page.goto('/ingest');
    await page.getByLabel('Recipe URL').fill(`https://${host}/p/Cabc123/`);
    await page.getByRole('button', { name: 'Read recipe' }).click();
    await expect(page.getByRole('alert')).toContainText('Instagram links cannot be read. Paste the caption as text instead.');
    await expect(page).toHaveURL(/\/ingest$/);
    expect(asked).toEqual([]);
  });
}
