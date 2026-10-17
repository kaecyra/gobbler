import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { startThumbnailHost, THUMBNAIL_HOST_ENV, THUMBNAIL_JPEG } from '../fixtures/thumbnails.mjs';
import { portFromEnv } from '../fixtures/ports.mjs';

// The recipe view needs a recipe in the database, which the shared server of
// the suite does not have. This spec seeds its own data directory through the
// real store (seed/main.go), starts its own gobbler on it in dev-auth mode, and
// reads the page through a browser. Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');
const accessURL = process.env.GOBBLER_E2E_ACCESS_URL;
if (!accessURL) throw new Error('GOBBLER_E2E_ACCESS_URL is not set; run through playwright.config.ts');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_RECIPEVIEW_PORT');
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let id = 0;
let meanwhileId = 0;
let cloneId = 0;
let bareVideoId = 0;
let laterId = 0;
let styledId = 0;
let thumbnails: Awaited<ReturnType<typeof startThumbnailHost>> | undefined;

test.use({ baseURL: origin });
// One server per file: fullyParallel would otherwise run beforeAll in every worker.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  // A leftover server on the port would answer with someone else's data.
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_RECIPEVIEW_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-recipeview-'));
  const out = execFileSync('go', ['run', './e2e/recipeview/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
  ({ recipe: id, meanwhile: meanwhileId, clone: cloneId, bare_video: bareVideoId, video_later: laterId, styled: styledId } = JSON.parse(out));
  thumbnails = await startThumbnailHost();
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      [THUMBNAIL_HOST_ENV]: thumbnails.origin,
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
    if (Date.now() > deadline) throw new Error('the recipe-view server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
  await thumbnails?.close();
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

test('shows the recipe as written', async ({ page }) => {
  const res = await page.goto(`/recipes/${id}`);
  expect(res?.status()).toBe(200);
  await expect(page).toHaveTitle(/E2E Sponge/);
  await expect(page.getByRole('heading', { name: 'E2E Sponge', level: 1 })).toBeVisible();
  await expect(page.getByText('1 1/3 cup e2e flour')).toBeVisible();
  await expect(page.getByText('1/3 cup e2e sugar')).toBeVisible();
  await expect(page.getByText('Bake until golden.')).toBeVisible();
  await expect(page.getByTestId('equipment')).toContainText('e2e 9x13 pan');
  await expect(page.getByTestId('total')).toContainText('55 min');
});

test('the Edit link opens the editor form for that recipe', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByTestId('edit-link').click();
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}/edit$`));
  await expect(page.locator('form#editor')).toBeVisible();
});

test('the Share link opens the send form for that recipe', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByTestId('share-link').click();
  await expect(page).toHaveURL(new RegExp(`/shares/new/${id}$`));
  await expect(page.getByTestId('recipe-name')).toBeVisible();
});

test('a passive step shows the work that can be done meanwhile', async ({ page }) => {
  await page.goto(`/recipes/${meanwhileId}`);
  const guidance = page.getByTestId('meanwhile');
  // Only the dough rise has guidance; the sauce's second step waits for the first.
  await expect(guidance).toHaveCount(1);
  await expect(guidance).toContainText('step 4: Chop the tomatoes.');
  await expect(guidance).not.toContainText('Simmer the sauce.');
  await expect(page.locator('li', { hasText: 'Let the dough rise.' }).first()).toContainText('Meanwhile');
});

test('a recipe without anything to do meanwhile shows no guidance', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await expect(page.getByText('Bake until golden.')).toBeVisible();
  await expect(page.getByTestId('meanwhile')).toHaveCount(0);
});

test('scaling by a preset swaps the recipe in place and keeps the URL shareable', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByRole('link', { name: '3×' }).click();
  // 1/3 cup x 3 is exactly 1 cup; 1 1/3 x 3 is exactly 4; the bay leaf is fixed.
  await expect(page.getByText('1 cup e2e sugar')).toBeVisible();
  await expect(page.getByText('4 cup e2e flour')).toBeVisible();
  await expect(page.getByText('6 e2e egg')).toBeVisible();
  await expect(page.getByText('1 e2e bay leaf')).toBeVisible();
  await expect(page.getByTestId('servings')).toHaveText('12');
  await expect(page).toHaveURL(`/recipes/${id}?scale=3`);

  // The URL alone reproduces the state.
  await page.reload();
  await expect(page.getByText('1 cup e2e sugar')).toBeVisible();
});

test('scaling to a serving count', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByLabel('Servings').fill('2');
  await page.getByRole('button', { name: 'Scale' }).click();
  await expect(page.getByTestId('servings')).toHaveText('2');
  await expect(page.getByText('2/3 cup e2e flour')).toBeVisible();
});

test('unit modes convert the view and combine with scaling', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByRole('link', { name: 'Metric' }).click();
  await expect(page.getByText('315 ml e2e flour')).toBeVisible();
  await page.getByRole('link', { name: 'Weight' }).click();
  // flour has a density, sugar has none and stays a volume.
  await expect(page.getByText('160 g e2e flour')).toBeVisible();
  await expect(page.getByText('79 ml e2e sugar')).toBeVisible();
  await page.getByRole('link', { name: '2×' }).click();
  await expect(page).toHaveURL(`/recipes/${id}?scale=2&units=weight`);
  await expect(page.getByText('315 g e2e flour')).toBeVisible();
  await page.getByRole('link', { name: 'As written' }).click();
  await expect(page.getByText('2 2/3 cup e2e flour')).toBeVisible();
});

test('a servings value that cannot be used shows a message in place', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByLabel('Servings').fill('0');
  await page.getByRole('button', { name: 'Scale' }).click();
  await expect(page.getByTestId('view-error')).toContainText('cannot be used');
  // The recipe stays on screen and the bad value does not reach the address bar.
  await expect(page.getByText('1 1/3 cup e2e flour')).toBeVisible();
  await expect(page).toHaveURL(`/recipes/${id}`);
});

test('a missing recipe is a 404 page', async ({ page }) => {
  const res = await page.goto(`/recipes/${id + 1000}`);
  expect(res?.status()).toBe(404);
});

test('a scale that cannot be shown is a 400 page', async ({ page }) => {
  const res = await page.goto(`/recipes/${id}?scale=0`);
  expect(res?.status()).toBe(400);
});

test('the recipe is refused without an Access identity', async ({ request }) => {
  const res = await request.get(`${accessURL}/recipes/${id}`);
  expect(res.status()).toBe(403);
});

test('the journal loads into the recipe page', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await expect(page.locator('#journal')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Journal', level: 2 })).toBeVisible();
});

test('the journal survives scaling', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByLabel('Add a note').fill('half typed');
  await page.getByRole('link', { name: '2×' }).click();
  await expect(page.getByText('2/3 cup e2e sugar')).toBeVisible();
  await expect(page.getByLabel('Add a note')).toHaveValue('half typed');
});

test('the History link opens the history of that recipe', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByTestId('history-link').click();
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}/history$`));
  await expect(page.getByTestId('current-version')).toBeVisible();
});

test('a clone shows where it came from, and the original does not', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await expect(page.getByTestId('origin')).toHaveCount(0);
  await page.goto(`/recipes/${cloneId}`);
  await expect(page.getByTestId('origin')).toContainText(`recipe ${id}`);
  await page.getByTestId('origin').getByRole('link', { name: /^recipe / }).click();
  await expect(page).toHaveURL(`/recipes/${id}`);
});

test('the Clone button makes a new recipe that points back at this one', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByTestId('clone-button').click();
  await expect(page).not.toHaveURL(`/recipes/${id}`);
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
  await expect(page.getByTestId('origin')).toContainText(`recipe ${id}`);
});

// ADR-00019 (amended): the larger-pan hint is on sized equipment of a view scaled up only.
// The seed's pan is sized and its whisk is not.
test('a view scaled up hints at a larger pan on sized equipment only', async ({ page }) => {
  await page.goto(`/recipes/${id}?scale=2`);
  const items = page.getByTestId('equipment-item');
  await expect(items).toHaveCount(2);
  await expect(items.filter({ hasText: 'e2e 9x13 pan' })).toContainText('may need a larger pan');
  await expect(items.filter({ hasText: 'e2e whisk' })).not.toContainText('larger pan');
});

test('a view scaled down has no larger-pan hint', async ({ page }) => {
  await page.goto(`/recipes/${id}?scale=1/2`);
  await expect(page.getByTestId('scaled-note')).toBeVisible();
  await expect(page.getByTestId('equipment-item')).toHaveCount(2);
  await expect(page.getByTestId('equipment-hint')).toHaveCount(0);
});

test('an unscaled view has no larger-pan hint', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await expect(page.getByTestId('equipment-item')).toHaveCount(2);
  await expect(page.getByTestId('equipment-hint')).toHaveCount(0);
});

// ADR-00021 obligation 1. The video is click to play: the page makes no request
// to a YouTube origin before the button is pressed, and loads the
// youtube-nocookie.com player after. The requests are intercepted, so the test
// never reaches YouTube whatever the network does.
const YOUTUBE_HOST = /(^|\.)(youtube\.com|youtube-nocookie\.com|ytimg\.com|googlevideo\.com|youtu\.be)$/;
const VIDEO = 'dQw4w9WgXcQ';

async function stubYouTube(page: import('@playwright/test').Page): Promise<string[]> {
  const asked: string[] = [];
  await page.route((url) => YOUTUBE_HOST.test(url.hostname), async (route) => {
    asked.push(route.request().url());
    await route.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>stub player</title>' });
  });
  return asked;
}

test('a recipe video makes no request to YouTube until Play is pressed, then loads the nocookie player', async ({ page }) => {
  const asked = await stubYouTube(page);
  await page.goto(`/recipes/${id}`);
  const thumb = page.getByTestId('video-thumbnail');
  await expect(thumb).toBeVisible();
  // The preview is the application's own copy, and it decoded.
  await expect(thumb).toHaveAttribute('src', `/recipes/${id}/video/thumbnail`);
  await expect.poll(() => thumb.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0)).toBe(true);
  await page.waitForLoadState('networkidle');
  expect(asked, 'requests to a YouTube origin before Play').toEqual([]);
  await expect(page.locator('iframe')).toHaveCount(0);

  await page.getByTestId('video-play').click();
  const frame = page.getByTestId('video-frame');
  await expect(frame).toBeVisible();
  const src = (await frame.getAttribute('src')) ?? '';
  expect(src.startsWith(`https://www.youtube-nocookie.com/embed/${VIDEO}?`)).toBe(true);
  expect(new URL(src).searchParams.get('autoplay')).toBe('1');
  await expect(page.getByTestId('video-play')).toHaveCount(0);
  await expect.poll(() => asked.some((u) => new URL(u).hostname === 'www.youtube-nocookie.com')).toBe(true);
  expect(asked.filter((u) => new URL(u).hostname !== 'www.youtube-nocookie.com'), 'other YouTube origins after Play').toEqual([]);
});

test('scaling the recipe does not reset a playing video', async ({ page }) => {
  await stubYouTube(page);
  await page.goto(`/recipes/${id}`);
  await page.getByTestId('video-play').click();
  await expect(page.getByTestId('video-frame')).toBeVisible();
  await page.getByRole('link', { name: '2×' }).click();
  await expect(page.getByTestId('servings')).toHaveText('8');
  await expect(page.getByTestId('video-frame')).toBeVisible();
});

test('a video whose thumbnail could not be downloaded shows a plain placeholder that still plays', async ({ page }) => {
  const asked = await stubYouTube(page);
  await page.goto(`/recipes/${bareVideoId}`);
  await expect(page.getByTestId('video-play')).toBeVisible();
  await expect(page.getByTestId('video-thumbnail')).toHaveCount(0);
  await page.waitForLoadState('networkidle');
  expect(asked).toEqual([]);
  await page.getByTestId('video-play').click();
  await expect(page.getByTestId('video-frame')).toBeVisible();
});

test('a recipe without a video has no player and loads no video script', async ({ page }) => {
  const scripts: string[] = [];
  page.on('request', (r) => {
    if (r.url().includes('/static/video')) scripts.push(r.url());
  });
  await page.goto(`/recipes/${meanwhileId}`);
  await expect(page.getByText('Mix the dough.')).toBeVisible();
  await expect(page.getByTestId('video')).toHaveCount(0);
  expect(scripts).toEqual([]);
});

test('the thumbnail is served as an image through the recipe', async ({ request }) => {
  const res = await request.get(`/recipes/${id}/video/thumbnail`);
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toBe('image/png');
  const none = await request.get(`/recipes/${bareVideoId}/video/thumbnail`);
  expect(none.status()).toBe(404);
});

// ADR-00021 decision 3: a save downloads the thumbnail of a new video. The
// server is pointed at the fixture host, so no request leaves the machine.
test('saving a recipe with a video stores the thumbnail the image host served', async ({ page, request }) => {
  const before = await request.get(`/recipes/${laterId}/video/thumbnail`);
  expect(before.status()).toBe(404);
  await page.goto(`/recipes/${laterId}/edit`);
  await page.locator('[name="video"]').fill('https://youtu.be/aqz-KE-bpKQ');
  await page.getByRole('button', { name: 'Save recipe' }).first().click();
  await expect(page).toHaveURL(new RegExp(`/recipes/${laterId}$`));
  const res = await request.get(`/recipes/${laterId}/video/thumbnail`);
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toBe('image/jpeg');
  expect(Buffer.compare(await res.body(), THUMBNAIL_JPEG)).toBe(0);
});

// The mockup design (ADR-00022, docs/ui/design.md).

test('a pinned note shows as a tip with no box, tint or coloured edge, and an unpinned note does not', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  const tip = page.getByTestId('tip');
  await expect(tip).toHaveCount(1);
  await expect(tip).toContainText('Salt the ribs the night before.');
  await expect(page.locator('#recipe')).not.toContainText('Private remark');
  const style = await tip.evaluate((el) => {
    const c = getComputedStyle(el);
    return { left: c.borderLeftWidth, top: c.borderTopWidth, bg: c.backgroundColor, size: parseFloat(getComputedStyle(el.querySelector('p')!).fontSize) };
  });
  expect(style.left).toBe('0px');
  expect(style.top).toBe('0px');
  expect(style.bg).toBe('rgba(0, 0, 0, 0)');
  // Full recipe-size text, not a footnote.
  expect(style.size).toBeGreaterThanOrEqual(19);
});

test('a recipe without a pinned note shows no tip', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await expect(page.getByTestId('tip')).toHaveCount(0);
});

test('a step title heads its step', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  const titles = page.getByTestId('step-title');
  await expect(titles).toHaveText(['Brown the ribs', 'Braise']);
  await expect(page.getByText('Brown the ribs in two batches.')).toBeVisible();
});

test('Cook mode sits in the controls row beside the scaler, and Start cooking is nowhere', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  const row = page.locator('.recipe-controls');
  const cook = row.getByRole('link', { name: 'Cook mode' });
  await expect(cook).toBeVisible();
  await expect(row.getByRole('link', { name: '2×' })).toBeVisible();
  await expect(page.getByText('Start cooking')).toHaveCount(0);
  await cook.click();
  await expect(page).toHaveURL(new RegExp(`/recipes/${styledId}/cook$`));
});

test('the meta row shows meal type, chilis, spoons, made count and the source host', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  await expect(page.getByTestId('meal-type')).toContainText('Main');
  await expect(page.getByTestId('spice').locator('svg')).toHaveCount(2);
  await expect(page.getByTestId('spice')).toHaveAttribute('aria-label', 'Medium heat');
  await expect(page.getByTestId('rating').locator('svg')).toHaveCount(5);
  await expect(page.getByTestId('rating').locator('svg.on')).toHaveCount(4);
  await expect(page.getByTestId('made-count')).toHaveText('Made 2×');
  const source = page.getByTestId('source-link');
  await expect(source).toContainText('example.com');
  await expect(source).toHaveAttribute('target', '_blank');
  await expect(source).toHaveAttribute('rel', /noopener/);
  // An unrated recipe with no spice or meal type says New and shows nothing else.
  await page.goto(`/recipes/${id}`);
  await expect(page.getByTestId('rating-new')).toHaveText('New');
  await expect(page.getByTestId('spice')).toHaveCount(0);
  await expect(page.getByTestId('meal-type')).toHaveCount(0);
});

test('the large ribbon sits under Ready in about, and survives scaling', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  await expect(page.locator('.rtime .eyebrow')).toContainText('Ready in about');
  const ribbon = page.locator('.rib-big');
  await expect(ribbon).toBeVisible();
  await page.getByRole('link', { name: '2×' }).click();
  await expect(page.getByTestId('servings')).toHaveText('8');
  await expect(page.locator('.rib-big')).toBeVisible();
});

test('the meanwhile branch is a tinted callout, not a coloured edge', async ({ page }) => {
  await page.goto(`/recipes/${meanwhileId}`);
  const callout = page.getByTestId('meanwhile');
  const style = await callout.evaluate((el) => {
    const c = getComputedStyle(el);
    return { left: c.borderLeftWidth, bg: c.backgroundColor };
  });
  expect(style.left).toBe('0px');
  expect(style.bg).not.toBe('rgba(0, 0, 0, 0)');
});

// docs/ui/design.md: every interactive control is at least --tap (44px) tall.
// One assertion per control class, so a failure names the class.
const TAP = 44;
const controlClasses: Record<string, string> = {
  'breadcrumb link': '.crumbs a',
  'scale and unit links': '.recipe-controls .seg a',
  'servings field': '.recipe-controls input[name="servings"]',
  'scale button': '.recipe-controls button',
  'shopping and cook links': '.recipe-controls a.btn',
  'source link': 'a.srclink',
  'journal fields': '#journal select, #journal input[type="text"], #journal textarea',
  'journal buttons': '#journal button',
  'recipe tool links': '.tool-links a',
  'clone button': '.tools button',
};

test('every control on the recipe page is at least 44px tall', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  await expect(page.locator('#journal')).toBeVisible();
  for (const [name, selector] of Object.entries(controlClasses)) {
    const items = page.locator(selector);
    const n = await items.count();
    expect(n, `${name} (${selector}) present`).toBeGreaterThan(0);
    for (let i = 0; i < n; i++) {
      const box = await items.nth(i).boundingBox();
      expect(box?.height ?? 0, `${name} #${i} height`).toBeGreaterThanOrEqual(TAP - 0.5);
    }
  }
});

test('every control on a clone page with its origin link is at least 44px tall', async ({ page }) => {
  await page.goto(`/recipes/${cloneId}`);
  const links = page.locator('[data-testid="origin"] a');
  await expect(links).toHaveCount(2);
  for (let i = 0; i < 2; i++) {
    const box = await links.nth(i).boundingBox();
    expect(box?.height ?? 0, `origin link #${i} height`).toBeGreaterThanOrEqual(TAP - 0.5);
  }
});
