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
const anonymousURL = process.env.GOBBLER_E2E_ANONYMOUS_URL;
if (!anonymousURL) throw new Error('GOBBLER_E2E_ANONYMOUS_URL is not set; run through playwright.config.ts');

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
let doomedId = 0;
let doomedCloneId = 0;
let listedId = 0;
let notesId = 0;
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
  ({ recipe: id, meanwhile: meanwhileId, clone: cloneId, bare_video: bareVideoId, video_later: laterId, styled: styledId, doomed: doomedId, doomed_clone: doomedCloneId, listed: listedId, notes: notesId } = JSON.parse(out));
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

test('qualifiers show as muted italic text set off by spaces, not commas', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  const row = page.locator('.ing li', { hasText: 'e2e whole milk' });
  const quals = row.getByTestId('qualifier');
  await expect(quals).toHaveText(['warmed', 'divided']);
  await expect(quals.first()).toHaveCSS('font-style', 'italic');
  const plain = await row.getByText('e2e whole milk').first().evaluate((el) => getComputedStyle(el).color);
  const muted = await quals.first().evaluate((el) => getComputedStyle(el).color);
  expect(muted).not.toBe(plain);
  expect((await row.locator('span').first().innerText()).trim()).toBe('1 cup e2e whole milk warmed divided');
  // The ribs line has no qualifiers and so no qualifier element.
  await expect(page.locator('.ing li', { hasText: 'e2e short ribs' }).getByTestId('qualifier')).toHaveCount(0);
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

test('the recipe is refused without a session', async ({ request }) => {
  const res = await request.get(`${anonymousURL}/recipes/${id}`, { maxRedirects: 0 });
  expect(res.status()).toBe(303);
  expect(res.headers()['location']).toMatch(/^\/login\?next=/);
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

// ADR-00024: equipment is a chip with an icon, then the name.
test('equipment shows as chips with an icon', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  const chips = page.getByTestId('equipment-item');
  await expect(chips).toHaveCount(2);
  for (const chip of await chips.all()) {
    await expect(chip.locator('svg[aria-hidden="true"]')).toHaveCount(1);
    const box = await chip.locator('svg').boundingBox();
    expect(box?.width).toBeGreaterThan(0);
  }
  await expect(chips.filter({ hasText: 'e2e whisk' })).toBeVisible();
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
  await expect(page.getByTestId('cuisine')).toHaveText('Korean');
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
  await expect(page.getByTestId('cuisine')).toHaveCount(0);
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
  'shopping button': '.recipe-controls button[data-testid="shopping-add"]',
  'cook link': '.recipe-controls a.btn',
  'source link': 'a.srclink',
  'journal fields': '#journal input[type="date"], #journal input[type="text"], #journal textarea',
  'journal spoon pickers': '#journal .spoon-pick',
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

test('steps carry their time tag, dependency tag and the passive number mark', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  const steps = page.locator('.step');
  await expect(steps.nth(0).locator('.stag')).toHaveText(['20 min hands-on']);
  await expect(steps.nth(1).locator('.stag')).toHaveText(['Timer · 3 h', 'after step 1']);
  // The timer tag is the waiting colour, the hands-on tag is not.
  const bg = (i: number) => steps.nth(i).locator('.stag').first().evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(await bg(1)).not.toBe(await bg(0));
  // A passive step's number is dashed; an active one's is solid.
  const border = (i: number) => steps.nth(i).locator('.sn').evaluate((el) => getComputedStyle(el).borderTopStyle);
  expect(await border(0)).toBe('solid');
  expect(await border(1)).toBe('dashed');
});

test('a step offered while a passive step waits is tagged with that step', async ({ page }) => {
  await page.goto(`/recipes/${meanwhileId}`);
  const chop = page.locator('.step', { hasText: 'Chop the tomatoes.' });
  await expect(chop.getByTestId('during')).toHaveText('during step 2');
  // The step that waits for the chop is not offered, so it has no such tag.
  await expect(page.locator('.step', { hasText: 'Simmer the sauce.' }).getByTestId('during')).toHaveCount(0);
});

// Recipe pictures (ADR-00039, Showing): a short header with the title over the
// fade, or the placeholder band of the same height.
test('the header shows the recipe picture short, with the title over it', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  const hero = page.getByTestId('recipe-picture');
  const img = hero.locator('img');
  await expect(img).toHaveAttribute('src', new RegExp(`^/recipes/${id}/picture\\?v=`));
  await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0)).toBe(true);
  const box = (await hero.boundingBox())!;
  expect(box.height).toBe(210);
  // The title is inside the picture, near its bottom, and is still the page's h1.
  const title = page.getByRole('heading', { name: 'E2E Sponge', level: 1 });
  const t = (await title.boundingBox())!;
  expect(t.y).toBeGreaterThanOrEqual(box.y);
  expect(t.y + t.height).toBeLessThanOrEqual(box.y + box.height + 1);
  expect(t.y).toBeGreaterThan(box.y + box.height / 2);
  await expect(hero.locator('.phmark')).toBeHidden();
});

test('a recipe without a picture shows the placeholder band at the same height, with its meal type', async ({ page }) => {
  await page.goto(`/recipes/${styledId}`);
  const hero = page.getByTestId('recipe-picture');
  await expect(hero.locator('img')).toHaveCount(0);
  await expect(hero).toHaveClass(new RegExp(`\\bph${styledId % 6}\\b`));
  await expect(hero.locator('.phmark svg')).toBeVisible();
  expect((await hero.boundingBox())!.height).toBe(210);
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 800 });
  expect((await hero.boundingBox())!.height).toBe(160);
});

test('a recipe whose picture object is missing shows the placeholder band, not a broken image', async ({ page }) => {
  rmSync(join(dataDir, 'media', 'recipes', String(meanwhileId)), { recursive: true, force: true });
  const missing = page.waitForResponse((r) => r.url().includes(`/recipes/${meanwhileId}/picture`));
  await page.goto(`/recipes/${meanwhileId}`);
  expect((await missing).status()).toBe(404);
  const hero = page.getByTestId('recipe-picture');
  await expect(hero.locator('img')).toHaveCount(0);
  await expect(hero).toHaveClass(new RegExp(`\\bph${meanwhileId % 6}\\b`));
  expect((await hero.boundingBox())!.height).toBe(210);
});

test.describe('deleting a recipe', () => {
  test('the Delete recipe button opens a confirmation that names the recipe and what goes with it', async ({ page }) => {
    await page.goto(`/recipes/${doomedId}`);
    await page.getByTestId('delete-link').scrollIntoViewIfNeeded();
    await page.getByTestId('delete-link').click();
    await expect(page).toHaveURL(new RegExp(`/recipes/${doomedId}/delete$`));
    const box = page.getByTestId('delete-confirm');
    await expect(box).toContainText('E2E Doomed Stew');
    for (const word of ['history', 'notes', 'made-it', 'picture']) await expect(box).toContainText(word);
    await expect(page.getByTestId('delete-submit')).toBeDisabled();
  });

  test('the final button stays disabled until DELETE is typed', async ({ page }) => {
    await page.goto(`/recipes/${doomedId}/delete`);
    const input = page.getByTestId('delete-input');
    await input.fill('delete');
    await expect(page.getByTestId('delete-submit')).toBeDisabled();
    await input.fill('DELETE');
    await expect(page.getByTestId('delete-submit')).toBeEnabled();
  });

  test('a POST without DELETE is refused and deletes nothing', async ({ page }) => {
    const res = await page.request.post(`/recipes/${doomedId}/delete`, { form: { confirm: 'delete' } });
    expect(res.status()).toBe(400);
    const none = await page.request.post(`/recipes/${doomedId}/delete`);
    expect(none.status()).toBe(400);
    expect((await page.goto(`/recipes/${doomedId}`))?.status()).toBe(200);
  });

  test('typing DELETE removes the recipe, its picture and its history, and a clone survives', async ({ page }) => {
    await page.goto(`/recipes/${doomedId}/delete`);
    await page.getByTestId('delete-input').fill('DELETE');
    await page.getByTestId('delete-submit').click();
    await expect(page).toHaveURL(/\/recipes$/);
    for (const path of ['', '/picture', '/history', '/journal']) {
      const res = await page.request.get(`/recipes/${doomedId}${path}`);
      expect(res.status(), path).toBe(404);
    }
    expect((await page.goto(`/recipes/${doomedCloneId}`))?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'E2E Doomed Stew', level: 1 })).toBeVisible();
    await expect(page.getByTestId('origin')).toHaveCount(0);
  });

  test('a recipe on a shopping list says so and stays until it is taken off', async ({ page }) => {
    await page.goto(`/recipes/${listedId}/delete`);
    await page.getByTestId('delete-input').fill('DELETE');
    const [res] = await Promise.all([
      page.waitForResponse((r) => r.url().endsWith(`/recipes/${listedId}/delete`) && r.request().method() === 'POST'),
      page.getByTestId('delete-submit').click(),
    ]);
    expect(res.status()).toBe(409);
    await expect(page.getByText('this recipe is on a shopping list and must be removed from it before it can be deleted')).toBeVisible();
    expect((await page.request.get(`/recipes/${listedId}`)).status()).toBe(200);
  });
});

// The author's notes end the step chain (ADR-00041), drawn after variant A of
// the recipe screen in docs/ui/mockup.html.
test('author notes continue the step chain after the last step, each credited, with no heading', async ({ page }) => {
  await page.setViewportSize({ width: 1100, height: 1000 });
  await page.goto(`/recipes/${notesId}`);
  const notes = page.getByTestId('author-note');
  await expect(notes).toHaveCount(3);
  // They sit in the same list as the last step, after it, in order.
  const last = page.locator('.method ol').last();
  await expect(last.locator('> li').last()).toHaveAttribute('data-testid', 'author-note');
  await expect(last.locator('> li.step:not(.anote)').last()).toContainText('Reduce the sauce');
  await expect(notes.nth(0)).toContainText('Wine: Any dry red you would drink works.');
  await expect(notes.nth(1).getByTestId('author-note-title')).toHaveText('Make ahead:');
  await expect(notes.nth(2).getByTestId('author-note-title')).toHaveCount(0);
  for (let i = 0; i < 3; i++) await expect(notes.nth(i).locator('small')).toHaveText('From the author');
  await expect(page.getByRole('heading', { name: /author/i })).toHaveCount(0);
  // Each note is credited, so a note read alone carries whose it is, and the
  // page names them nowhere else.
  await expect(page.locator('.method')).not.toContainText("Author's Notes");
});

test('an author note is a bulb in a soft circle on a dotted rail, its text smaller and lighter than a step, with no coloured edge', async ({ page }) => {
  await page.setViewportSize({ width: 1100, height: 1000 });
  await page.goto(`/recipes/${notesId}`);
  const note = page.getByTestId('author-note').first();
  const step = page.locator('.method li.step:not(.anote)').first();

  const circle = note.locator('.an > span');
  await expect(circle.locator('svg')).toBeVisible();
  const box = (await circle.boundingBox())!;
  expect(Math.round(box.width)).toBe(30);
  expect(Math.round(box.height)).toBe(30);
  expect(await circle.evaluate((el) => getComputedStyle(el).borderRadius)).toBe('50%');
  expect(await circle.evaluate((el) => getComputedStyle(el).backgroundColor), 'a soft fill, not transparent').not.toBe('rgba(0, 0, 0, 0)');

  // The rail runs on from the last step: dotted, 2px, and no thicker edge anywhere else.
  const rail = await note.evaluate((el) => {
    const s = getComputedStyle(el, '::before');
    return { style: s.borderLeftStyle, width: s.borderLeftWidth };
  });
  expect(rail).toEqual({ style: 'dotted', width: '2px' });
  expect(await note.evaluate((el) => getComputedStyle(el).borderLeftWidth)).toBe('0px');

  const size = async (loc: typeof note) => loc.locator('p').first().evaluate((el) => parseFloat(getComputedStyle(el).fontSize));
  expect(await size(note)).toBeLessThan(await size(step));
  const credit = await note.locator('small').evaluate((el) => ({ size: parseFloat(getComputedStyle(el).fontSize), color: getComputedStyle(el).color }));
  expect(credit.size).toBeLessThan(await size(note));
  const bodyColor = await note.locator('p').evaluate((el) => getComputedStyle(el).color);
  expect(credit.color, 'the credit is lighter than the note text').not.toBe(bodyColor);
});

test('a recipe without author notes shows none, and the notes survive scaling', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await expect(page.getByTestId('author-note')).toHaveCount(0);
  await page.goto(`/recipes/${notesId}`);
  await page.getByRole('link', { name: '2×' }).first().click();
  await expect(page.getByTestId('scaled-note')).toBeVisible();
  await expect(page.getByTestId('author-note')).toHaveCount(3);
});
