import { test, expect, type Page } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { portFromEnv } from '../fixtures/ports.mjs';

// Cook mode needs recipes in the database, which the shared server of the suite
// does not have. This spec seeds its own data directory through the real store
// (seed/main.go), starts its own gobbler on it in dev-auth mode, and drives the
// page in a browser. Loopback only, throwaway data. Steps of the seeded pizza:
// 1 mix, 2 rise (passive, 1 h), 3 bake, 4 chop, 5 simmer, 6 serve (1 min).
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');
const accessURL = process.env.GOBBLER_E2E_ACCESS_URL;
if (!accessURL) throw new Error('GOBBLER_E2E_ACCESS_URL is not set; run through playwright.config.ts');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_COOK_PORT');
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let pizza = 0;
let empty = 0;

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_COOK_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-cook-'));
  const out = execFileSync('go', ['run', './e2e/cook/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
  ({ pizza, empty } = JSON.parse(out));
  server = spawn(bin, ['serve'], {
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
    if (Date.now() > deadline) throw new Error('the cook-mode server did not start');
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

/** Replaces AudioContext with a recorder so the test can see that a sound was made. */
async function stubAudio(page: Page) {
  await page.addInitScript(() => {
    (window as any).__beeps = 0;
    (window as any).AudioContext = class {
      state = 'running';
      currentTime = 0;
      destination = {};
      resume() {}
      createGain() {
        return { gain: { value: 0 }, connect() {} };
      }
      createOscillator() {
        return {
          frequency: { value: 0 },
          connect() {},
          start() {
            (window as any).__beeps++;
          },
          stop() {},
        };
      }
    };
  });
}

/**
 * Replaces the Screen Wake Lock API with a recorder. `mode` picks the
 * behaviour: granted, denied, or absent. Hiding the page drops the lock the way
 * browsers do, by firing the sentinel's release event.
 */
async function stubWakeLock(page: Page, mode: 'granted' | 'denied' | 'absent') {
  await page.addInitScript((m) => {
    const w = window as any;
    w.__wake = { requests: [] as string[], released: 0, held: 0 };
    let visibility = 'visible';
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility });
    w.__setVisibility = (v: string) => {
      visibility = v;
      if (v === 'hidden') w.__sentinels.forEach((s: any) => s.drop());
      document.dispatchEvent(new Event('visibilitychange'));
    };
    w.__sentinels = [] as any[];
    if (m === 'absent') {
      Object.defineProperty(navigator, 'wakeLock', { configurable: true, value: undefined });
      return;
    }
    Object.defineProperty(navigator, 'wakeLock', {
      configurable: true,
      value: {
        request: async (type: string) => {
          w.__wake.requests.push(type);
          if (m === 'denied') throw new DOMException('denied', 'NotAllowedError');
          const listeners: Array<() => void> = [];
          w.__wake.held++;
          const s = {
            release: async () => {
              w.__wake.released++;
              w.__wake.held--;
            },
            addEventListener: (_: string, f: () => void) => listeners.push(f),
            drop: () => {
              w.__wake.held--;
              listeners.forEach((f) => f());
            },
          };
          w.__sentinels.push(s);
          return s;
        },
      },
    });
  }, mode);
}

test('shows one step at a time with its ingredients and equipment', async ({ page }) => {
  const res = await page.goto(`/recipes/${pizza}/cook`);
  expect(res?.status()).toBe(200);
  await expect(page.getByTestId('progress')).toContainText('Step 1 of 6');
  await expect(page.getByTestId('step-text')).toHaveText('Mix the dough.');
  await expect(page.getByTestId('cook-ingredients')).toContainText('1 1/3 cup e2e flour');
  await expect(page.getByTestId('cook-ingredients')).not.toContainText('tomato');
  await expect(page.getByTestId('cook-equipment')).toContainText('e2e bowl');
  await expect(page.getByTestId('step-text')).toHaveCount(1);
});

test('step equipment shows as chips with an icon', async ({ page }) => {
  await page.goto(`/recipes/${pizza}/cook`);
  const chip = page.getByTestId('cook-equipment').locator('li').first();
  await expect(chip).toContainText('e2e bowl');
  await expect(chip.locator('svg[aria-hidden="true"]')).toHaveCount(1);
});

test('a step with a title shows it above the text, and up next names the following step', async ({ page }) => {
  await page.goto(`/recipes/${pizza}/cook`);
  await expect(page.getByTestId('step-title')).toHaveText('Make the dough');
  await expect(page.getByTestId('up-next')).toContainText('Let the dough rise.');
  await page.getByTestId('up-next').getByRole('link').click();
  await expect(page.getByTestId('progress')).toContainText('Step 2 of 6');
  // Steps with no title have no heading.
  await expect(page.getByTestId('step-title')).toHaveCount(0);
  await page.goto(`/recipes/${pizza}/cook?step=6`);
  await expect(page.getByTestId('up-next')).toHaveCount(0);
});

test('every control on the cook page is at least 44px tall', async ({ page }) => {
  await page.goto(`/recipes/${pizza}/cook?step=2`);
  await page.getByTestId('timer-start').click();
  await expect(page.getByTestId('timer')).toHaveCount(1);
  const controls: Record<string, ReturnType<Page['locator']>> = {
    'back to recipe': page.getByTestId('cook-exit'),
    'timer start': page.getByTestId('timer-start'),
    'timer dismiss': page.getByTestId('timer').getByRole('button', { name: 'Dismiss' }),
    'timer pause': page.getByTestId('timer-toggle'),
    'meanwhile link': page.getByTestId('meanwhile').getByRole('link'),
    'up next link': page.getByTestId('up-next').getByRole('link'),
    'back': page.getByTestId('prev'),
    'next': page.getByTestId('next'),
    'all steps link': page.getByTestId('step-list').getByRole('link'),
  };
  for (const [name, loc] of Object.entries(controls)) {
    const n = await loc.count();
    expect(n, `${name} present`).toBeGreaterThan(0);
    for (let i = 0; i < n; i++) {
      const box = await loc.nth(i).boundingBox();
      expect(box?.height ?? 0, `${name} #${i} height`).toBeGreaterThanOrEqual(44);
    }
  }
});

test('steps forward and back, and jumps from the step list', async ({ page }) => {
  await page.goto(`/recipes/${pizza}/cook`);
  await page.getByTestId('next').click();
  await expect(page.getByTestId('step-text')).toHaveText('Let the dough rise.');
  await expect(page).toHaveURL(`/recipes/${pizza}/cook?step=2`);
  await page.getByTestId('next').click();
  await expect(page.getByTestId('step-text')).toHaveText('Bake the pizza.');
  await page.getByTestId('prev').click();
  await expect(page.getByTestId('step-text')).toHaveText('Let the dough rise.');
  await page.getByTestId('step-list').getByRole('link', { name: 'Chop the tomatoes.' }).click();
  await expect(page.getByTestId('progress')).toContainText('Step 4 of 6');
  await expect(page.getByTestId('cook-ingredients')).toContainText('2 e2e tomato');
  await expect(page.getByTestId('step-list').getByRole('link', { name: 'Chop the tomatoes.' })).toHaveAttribute('aria-current', 'step');
  // The URL alone reproduces the step.
  await page.reload();
  await expect(page.getByTestId('progress')).toContainText('Step 4 of 6');
});

test('a passive step shows what to do meanwhile', async ({ page }) => {
  await page.goto(`/recipes/${pizza}/cook?step=2`);
  const guidance = page.getByTestId('meanwhile');
  await expect(guidance).toContainText('step 4');
  await expect(guidance).toContainText('Chop the tomatoes.');
  await expect(guidance).not.toContainText('Simmer the sauce.');
  await guidance.getByRole('link', { name: 'step 4' }).click();
  await expect(page.getByTestId('step-text')).toHaveText('Chop the tomatoes.');
  await expect(page.getByTestId('meanwhile')).toHaveCount(0);
});

test('the recipe scale and units carry into cook mode', async ({ page }) => {
  await page.goto(`/recipes/${pizza}/cook?scale=2`);
  await expect(page.getByTestId('cook-ingredients')).toContainText('2 2/3 cup e2e flour');
  await page.getByTestId('next').click();
  await expect(page).toHaveURL(`/recipes/${pizza}/cook?scale=2&step=2`);
  await page.getByTestId('cook-exit').click();
  await expect(page).toHaveURL(`/recipes/${pizza}?scale=2`);
});

test('the recipe view links into cook mode', async ({ page }) => {
  await page.goto(`/recipes/${pizza}?scale=2`);
  await page.getByTestId('cook-link').click();
  await expect(page).toHaveURL(`/recipes/${pizza}/cook?scale=2`);
  await expect(page.getByTestId('step-text')).toHaveText('Mix the dough.');
});

test('a recipe without steps shows an empty state', async ({ page }) => {
  const res = await page.goto(`/recipes/${empty}/cook`);
  expect(res?.status()).toBe(200);
  await expect(page.getByTestId('cook-empty')).toContainText('no steps');
  await expect(page.getByTestId('next')).toHaveCount(0);
});

test('an unknown recipe is a 404 page and a step past the end is a 400 page', async ({ page }) => {
  expect((await page.goto(`/recipes/${pizza + 1000}/cook`))?.status()).toBe(404);
  expect((await page.goto(`/recipes/${pizza}/cook?step=7`))?.status()).toBe(400);
  expect((await page.goto(`/recipes/${pizza}/cook?step=0`))?.status()).toBe(400);
});

test('cook mode is refused without an Access identity', async ({ request }) => {
  const res = await request.get(`${accessURL}/recipes/${pizza}/cook`);
  expect(res.status()).toBe(403);
});

test('timers run side by side, keep their steps, and finish by the clock', async ({ page }) => {
  await stubAudio(page);
  await page.clock.install();
  await page.goto(`/recipes/${pizza}/cook`);
  await expect(page.getByTestId('timers-empty')).toBeVisible();

  // Step 1: 10 minutes.
  await page.getByTestId('timer-start').click();
  // Stepping must not lose a running timer.
  await page.getByTestId('step-list').getByRole('link', { name: 'Top and serve.' }).click();
  await expect(page.getByTestId('step-text')).toHaveText('Top and serve.');
  await page.getByTestId('timer-start').click();

  const rows = page.getByTestId('timer');
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toContainText('Step 1: Mix the dough.');
  await expect(rows.nth(1)).toContainText('Step 6: Top and serve.');
  await expect(rows.nth(1)).toContainText('1:00');

  // Jump the clock a minute in one go, as a backgrounded page would see it:
  // only an end timestamp gets this right.
  await page.clock.fastForward(60_000);
  await expect(rows.nth(1)).toContainText('Done');
  await expect(page.getByTestId('timer-alert')).toContainText('Step 6: Top and serve.');
  expect(await page.evaluate(() => (window as any).__beeps)).toBeGreaterThan(0);
  await expect(rows.nth(0)).not.toContainText('Done');
  await expect(rows.nth(0)).toContainText('9:');

  await page.clock.fastForward(9 * 60_000);
  await expect(rows.nth(0)).toContainText('Done');

  await rows.nth(1).getByRole('button', { name: 'Dismiss' }).click();
  await expect(rows).toHaveCount(1);
});

test('a paused timer holds its time, resumes from it, and keeps that across steps and a reload', async ({ page }) => {
  await page.clock.install();
  await page.goto(`/recipes/${pizza}/cook`);
  await page.getByTestId('timer-start').click(); // step 1: 10 minutes
  const row = page.getByTestId('timer');
  const toggle = page.getByRole('button', { name: 'Pause Step 1: Mix the dough. timer' });
  await page.clock.fastForward(120_000);
  await toggle.click();
  await expect(row).toContainText('8:00');
  await expect(row.getByTestId('timer-toggle')).toHaveText('Resume');

  // Time passing, stepping and reloading change nothing while paused.
  await page.clock.fastForward(300_000);
  await expect(row).toContainText('8:00');
  await page.getByTestId('next').click();
  await expect(page.getByTestId('progress')).toContainText('Step 2 of 6');
  await expect(row).toContainText('8:00');
  await page.reload();
  await expect(row).toContainText('8:00');
  await expect(row.getByTestId('timer-toggle')).toHaveText('Resume');

  // Resume continues from what was left.
  await page.getByRole('button', { name: 'Resume Step 1: Mix the dough. timer' }).click();
  await page.clock.fastForward(60_000);
  await expect(row).toContainText('7:00');
  await expect(row.getByTestId('timer-toggle')).toHaveText('Pause');
});

test('the timers mascot is shown whole, above the box, inside the cook card', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' }); // the bob would move the sampled box
  for (const size of [{ width: 1024, height: 768 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(size);
    await page.goto(`/recipes/${pizza}/cook`);
    const img = await page.locator('.cook-peek').boundingBox();
    const card = await page.locator('#cook').boundingBox();
    const box = await page.locator('.cook-timers-box').boundingBox();
    expect(img && card && box).toBeTruthy();
    expect(img!.y, `${size.width}px top inside the card`).toBeGreaterThanOrEqual(card!.y);
    expect(img!.y + img!.height, `${size.width}px stands on the box`).toBeLessThanOrEqual(box!.y + 5);
  }
});

test('running timers survive a reload', async ({ page }) => {
  await page.clock.install();
  await page.goto(`/recipes/${pizza}/cook`);
  await page.getByTestId('timer-start').click();
  await page.clock.fastForward(120_000);
  await page.reload();
  const row = page.getByTestId('timer');
  await expect(row).toHaveCount(1);
  await expect(row).toContainText('Step 1: Mix the dough.');
  await expect(row).toContainText('8:');
});

test('the wake lock is requested on open, taken back when the page returns', async ({ page }) => {
  await stubWakeLock(page, 'granted');
  await page.goto(`/recipes/${pizza}/cook`);
  await expect.poll(() => page.evaluate(() => (window as any).__wake.requests)).toEqual(['screen']);
  await expect(page.getByTestId('wake-status')).toBeHidden();

  // The browser drops the lock when the page is hidden; coming back re-acquires it.
  await page.evaluate(() => (window as any).__setVisibility('hidden'));
  await page.evaluate(() => (window as any).__setVisibility('visible'));
  await expect.poll(() => page.evaluate(() => (window as any).__wake.requests.length)).toBe(2);
  await expect.poll(() => page.evaluate(() => (window as any).__wake.held)).toBe(1);

});

test('exiting cook mode releases the lock', async ({ page }) => {
  await stubWakeLock(page, 'granted');
  await page.goto(`/recipes/${pizza}/cook`);
  await expect.poll(() => page.evaluate(() => (window as any).__wake.held)).toBe(1);
  // Stop the navigation so the page (and its counters) stays inspectable.
  await page.evaluate(() => document.querySelector('[data-cook-exit]')!.addEventListener('click', (e) => e.preventDefault()));
  await page.getByTestId('cook-exit').click();
  await expect.poll(() => page.evaluate(() => (window as any).__wake.held)).toBe(0);
  expect(await page.evaluate(() => (window as any).__wake.released)).toBe(1);
});

test('without the Wake Lock API the page still works and says so', async ({ page }) => {
  await stubWakeLock(page, 'absent');
  await page.goto(`/recipes/${pizza}/cook`);
  await expect(page.getByTestId('wake-status')).toContainText('cannot keep the screen awake');
  await page.getByTestId('next').click();
  await expect(page.getByTestId('step-text')).toHaveText('Let the dough rise.');
});

test('a refused wake lock shows a notice and the page keeps working', async ({ page }) => {
  await stubWakeLock(page, 'denied');
  await page.goto(`/recipes/${pizza}/cook`);
  await expect(page.getByTestId('wake-status')).toContainText('could not be kept awake');
  await page.getByTestId('timer-start').click();
  await expect(page.getByTestId('timer')).toHaveCount(1);
});

test('going Back after stepping runs one set of timers: one beep, no dismissed timer back', async ({ page }) => {
  await stubAudio(page);
  await page.clock.install();
  await page.goto(`/recipes/${pizza}/cook?step=6`);
  await page.getByTestId('timer-start').click();
  await page.getByTestId('prev').click(); // htmx swap, pushes history
  await expect(page.getByTestId('progress')).toContainText('Step 5 of 6');
  await page.goBack(); // htmx restores the page and runs its scripts again
  await expect(page.getByTestId('progress')).toContainText('Step 6 of 6');
  await page.clock.fastForward(60_000);
  await expect(page.getByTestId('timer')).toContainText('Done');
  // One announcement is three beeps; a second live copy of the script would double it.
  expect(await page.evaluate(() => (window as any).__beeps)).toBe(3);

  // A dismissed timer stays dismissed across the same trip.
  await page.getByTestId('timer').getByRole('button', { name: 'Dismiss' }).click();
  await page.getByTestId('prev').click();
  await expect(page.getByTestId('progress')).toContainText('Step 5 of 6');
  await page.goBack();
  await expect(page.getByTestId('progress')).toContainText('Step 6 of 6');
  await page.clock.fastForward(1_000);
  await expect(page.getByTestId('timer')).toHaveCount(0);
});

test('a page restored after leaving, or a cancelled exit, asks for the wake lock again', async ({ page }) => {
  await stubWakeLock(page, 'granted');
  await page.goto(`/recipes/${pizza}/cook`);
  await expect.poll(() => page.evaluate(() => (window as any).__wake.held)).toBe(1);

  // Back/forward cache: pagehide releases, pageshow brings the page back.
  await page.evaluate(() => window.dispatchEvent(new Event('pagehide')));
  await expect.poll(() => page.evaluate(() => (window as any).__wake.held)).toBe(0);
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })));
  await expect.poll(() => page.evaluate(() => (window as any).__wake.held)).toBe(1);

  // An exit that does not happen (the navigation is cancelled) must not leave the screen free to sleep.
  await page.evaluate(() => document.querySelector('[data-cook-exit]')!.addEventListener('click', (e) => e.preventDefault()));
  await page.getByTestId('cook-exit').click();
  await expect.poll(() => page.evaluate(() => (window as any).__wake.held)).toBe(0);
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: false })));
  await expect.poll(() => page.evaluate(() => (window as any).__wake.held)).toBe(1);
});

test('a running timer\'s button offers a restart, not a silent second start', async ({ page }) => {
  await page.clock.install();
  await page.goto(`/recipes/${pizza}/cook?step=6`);
  const button = page.getByTestId('timer-start');
  await expect(button).toHaveText('Start timer (1 min)');
  await button.click();
  await expect(button).toHaveText('Restart timer (1 min)');
  await page.clock.fastForward(60_000);
  await expect(button).toHaveText('Start timer (1 min)');
});
