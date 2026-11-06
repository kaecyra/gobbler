import { test, expect } from '@playwright/test';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { deflateSync } from 'node:zlib';
import { portFromEnv } from '../fixtures/ports.mjs';

// The picture picker on the review screen (ADR-00039, Choosing). The page is
// fetched from a local fixture server, which also serves the page's pictures;
// the e2e server runs with the dev auth bypass, so it may fetch loopback
// addresses (ADR-00040). The spec starts its own gobbler: a household holds at
// most 20 drafts (ADR-00035), and the shared server's are used by the other
// specs.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_PICTURES_PORT');
const origin = `http://127.0.0.1:${port}`;
test.use({ baseURL: origin });

let server: ChildProcess | undefined;
let dataDir = '';

// width x height RGB PNG, banded so the thumbnail is not a flat colour. 1200 by
// 400 clears the candidate minimum of 1066 by 300; the stored candidate is
// scaled to 1024 wide, which is the size the picker shows.
function png(width: number, height: number, rgb: [number, number, number]): Buffer {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const crc = (b: Buffer) => {
    let c = 0xffffffff;
    for (const x of b) c = crcTable[(c ^ x) & 0xff]! ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  };
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type), data]);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc(body));
    return Buffer.concat([len, body, sum]);
  };
  const head = Buffer.alloc(13);
  head.writeUInt32BE(width, 0);
  head.writeUInt32BE(height, 4);
  head[8] = 8; // bit depth
  head[9] = 2; // RGB
  const raw = Buffer.alloc((width * 3 + 1) * height);
  for (let y = 0; y < height; y++) {
    const row = y * (width * 3 + 1);
    const shade = 0.55 + 0.45 * (Math.floor(y / 40) % 2);
    for (let x = 0; x < width; x++) {
      for (let i = 0; i < 3; i++) raw[row + 1 + x * 3 + i] = Math.round(rgb[i]! * shade);
    }
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', head),
    chunk('IDAT', deflateSync(raw)),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

const PICTURES: Record<string, Buffer> = {
  '/pics/one.png': png(1200, 400, [200, 90, 60]),
  '/pics/two.png': png(1600, 900, [60, 130, 200]),
};

const page = (name: string, images: string[]) => `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"${name}",
 "image":${JSON.stringify(images)},
 "recipeIngredient":["2 slices bread","1 tbsp butter"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Toast the bread."},{"@type":"HowToStep","text":"Butter it."}]}
</script></head><body><h1>${name}</h1></body></html>`;

let fixture: Server;
let fixtureURL: string;

test.beforeAll(async () => {
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-pictures-'));
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
    if (Date.now() > deadline) throw new Error('the pictures server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
  fixture = createServer((req, res) => {
    const pic = PICTURES[req.url ?? ''];
    if (pic) {
      res.writeHead(200, { 'content-type': 'image/png' });
      res.end(pic);
    } else if (req.url === '/with-pictures' || req.url === '/with-pictures-b' || req.url === '/with-pictures-c') {
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      res.end(page(`Picture Toast ${req.url!.slice(-1)}`, [`${fixtureURL}/pics/one.png`, `${fixtureURL}/pics/two.png`]));
    } else if (req.url === '/without-pictures') {
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      res.end(page('Bare Toast', []));
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

async function review(p: import('@playwright/test').Page, path: string) {
  await p.goto('/ingest');
  await p.getByLabel('Recipe URL').fill(`${fixtureURL}${path}`);
  await p.getByRole('button', { name: 'Read recipe' }).click();
  await expect(p).toHaveURL(/\/ingest\/[0-9a-f]+$/);
}

const picker = (p: import('@playwright/test').Page) => p.getByRole('radiogroup', { name: 'Picture for this recipe' });

test('the review screen offers the page pictures and No picture, none chosen', async ({ page: p }) => {
  await review(p, '/with-pictures');
  const group = picker(p);
  const radios = group.getByRole('radio');
  await expect(radios).toHaveCount(3);
  await expect(group.getByText('1024 × 341')).toBeVisible();
  await expect(group.getByText('1024 × 576')).toBeVisible();
  await expect(group.getByText('Shows a coloured band')).toBeVisible();
  for (const r of await radios.all()) await expect(r).not.toBeChecked();
  await expect(p.getByText('Pick a picture, or No picture, before saving.')).toBeVisible();
  // The candidates are real images, served after admission.
  for (const img of await group.locator('img').all()) {
    await expect(img).toBeVisible();
    expect(await img.evaluate((i: HTMLImageElement) => i.naturalWidth)).toBeGreaterThan(0);
  }
});

test('confirm is refused with no choice, and the draft is kept', async ({ page: p }) => {
  await review(p, '/with-pictures-b');
  await p.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(p.getByText(/choose a picture, or No picture/i)).toBeVisible();
  await expect(p).toHaveURL(/\/ingest\/[0-9a-f]+$/);
});

test('a draft confirmed with a chosen picture saves it as the recipe picture', async ({ page: p }) => {
  await review(p, '/with-pictures-c');
  const group = picker(p);
  await group.locator('.pick', { hasText: '1024 × 576' }).click();
  await expect(group.getByRole('radio', { name: /1024 × 576/ })).toBeChecked();
  await expect(p.getByText('Pick a picture, or No picture, before saving.')).toBeHidden();
  await p.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(p).toHaveURL(/\/recipes\/\d+$/);
  const id = p.url().match(/\/recipes\/(\d+)$/)![1];
  const res = await p.request.get(`/recipes/${id}/picture`);
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toMatch(/^image\//);
});

test('a draft confirmed with No picture saves a recipe with no picture', async ({ page: p }) => {
  await review(p, '/with-pictures');
  await picker(p).locator('.pick.none').click();
  await p.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(p).toHaveURL(/\/recipes\/\d+$/);
  const id = p.url().match(/\/recipes\/(\d+)$/)![1];
  expect((await p.request.get(`/recipes/${id}/picture`)).status()).toBe(404);
});

test('the choice survives the form being shown again', async ({ page: p }) => {
  await review(p, '/with-pictures');
  await picker(p).locator('.pick', { hasText: '1024 × 341' }).click();
  // A refused confirm re-renders the form: blank the name so the editor rejects it.
  await p.getByLabel('Name', { exact: true }).fill('');
  // The confirm is an htmx request: wait for it, or the reload can abort it
  // before the choice is recorded.
  await Promise.all([
    p.waitForResponse((r) => r.url().endsWith('/confirm')),
    p.getByRole('button', { name: 'Confirm and save' }).first().click(),
  ]);
  await p.reload();
  await expect(picker(p).getByRole('radio', { name: /1024 × 341/ })).toBeChecked();
});

test('a picture position the draft does not have is refused as a bad request', async ({ page: p }) => {
  await review(p, '/with-pictures');
  await picker(p).locator('.pick').first().click();
  await picker(p).getByRole('radio').first().evaluate((r: HTMLInputElement) => (r.value = '99'));
  const [res] = await Promise.all([
    p.waitForResponse((r) => r.url().endsWith('/confirm')),
    p.getByRole('button', { name: 'Confirm and save' }).first().click(),
  ]);
  expect(res.status()).toBe(400);
});

test('a page with no pictures offers only No picture, and it saves', async ({ page: p }) => {
  await review(p, '/without-pictures');
  const radios = picker(p).getByRole('radio');
  await expect(radios).toHaveCount(1);
  await expect(radios.first()).toBeChecked();
  await expect(p.getByText('This source had no usable pictures.')).toBeVisible();
  await p.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(p).toHaveURL(/\/recipes\/\d+$/);
});

test('the picker matches the mockup at its width', async ({ page: p }, info) => {
  await p.setViewportSize({ width: 1100, height: 900 });
  await review(p, '/with-pictures');
  await picker(p).locator('.pick', { hasText: '1024 × 341' }).click();
  const pick = picker(p).locator('.pick').first();
  const box = (await pick.boundingBox())!;
  expect(Math.round(box.width)).toBe(132);
  expect((await pick.locator('.th').boundingBox())!.height).toBe(84);
  await p.locator('.picker').screenshot({ path: info.outputPath('picker.png') });
});

test('the tick and the No picture frame are drawings from the icon set', async ({ page: p }) => {
  await review(p, '/with-pictures');
  const none = picker(p).locator('.pick.none');
  // The frame is an svg, not a box drawn in CSS.
  await expect(none.locator('.th > svg')).toHaveCount(1);
  await expect(none.locator('.tick svg')).toHaveCount(1);
  await expect(none.locator('.tick')).toBeHidden();
  await none.click();
  await expect(none.locator('.tick')).toBeVisible();
  await expect(none.locator('.tick svg path')).toBeVisible();
});
