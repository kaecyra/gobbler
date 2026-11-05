import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { portFromEnv } from '../fixtures/ports.mjs';
import { expectTapTargets } from '../fixtures/tap';
import { userCopy } from '../fixtures/copy';

// The editor needs a recipe in the database, which the shared server of the
// suite does not have. This spec seeds its own data directory through the real
// stores (seed/main.go), starts its own gobbler on it in dev-auth mode, and
// drives the editor through a browser. Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_EDITOR_PORT');
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let id = 0;

test.use({ baseURL: origin });
// One server and one recipe per file, and the tests build on each other's data.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_EDITOR_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-editor-'));
  const out = execFileSync('go', ['run', './e2e/editor/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
  ({ recipe: id } = JSON.parse(out));
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
    if (Date.now() > deadline) throw new Error('the editor server did not start');
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

const save = (page: import('@playwright/test').Page) => page.getByRole('button', { name: 'Save recipe' }).first().click();

// A stored step is named by the key the store gave it (ADR-00035): read it from
// the form rather than assuming one.
const stepKey = (page: import('@playwright/test').Page, c: number, s: number) =>
  page.locator(`[name="c.${c}.s.${s}.id"]`).inputValue();

async function revisions(page: import('@playwright/test').Page): Promise<number> {
  await page.goto(`/recipes/${id}/history`);
  return page.getByTestId('revision').count();
}

test('loads the stored recipe into the form', async ({ page }) => {
  const res = await page.goto(`/recipes/${id}/edit`);
  expect(res?.status()).toBe(200);
  await expect(page.locator('[name="name"]')).toHaveValue('E2E Editor Loaf');
  await expect(page.locator('[name="c.0.name"]')).toHaveValue('Dough');
  await expect(page.locator('[name="c.1.name"]')).toHaveValue('Glaze');
  await expect(page.locator('[name="c.0.l.0.qty"]')).toHaveValue('2');
  await expect(page.locator('[name="c.0.l.0.unit"]')).toHaveValue('cup');
  await expect(page.locator('[name="c.0.s.1.text"]')).toHaveValue('Let the dough rest.');
  await expect(page.locator('[name="c.0.s.1.dur"]')).toHaveValue('60 min');
  const [mix, rest] = [await stepKey(page, 0, 0), await stepKey(page, 0, 1)];
  expect(mix).toMatch(/^stp-[0-9a-hjkmnp-tv-z]{8}$/);
  await expect(page.locator(`[name="c.0.s.1.dep"][value="${mix}"]`)).toBeChecked();
  await expect(page.locator(`[name="c.1.s.0.dep"][value="${rest}"]`)).toBeChecked();
  expect(await revisions(page)).toBe(0);
});

test('an invalid quantity is rejected inline, keeps the input and saves nothing', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('Never Saved');
  await page.locator('[name="c.0.l.0.qty"]').fill('lots');
  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'is not a quantity' })).toBeVisible();
  await expect(page.locator('[name="c.0.l.0.qty"]')).toHaveValue('lots');
  await expect(page.locator('[name="name"]')).toHaveValue('Never Saved');
  expect(await revisions(page)).toBe(0);
  await page.goto(`/recipes/${id}`);
  await expect(page.getByRole('heading', { name: 'E2E Editor Loaf' })).toBeVisible();
});

test('the video field takes a YouTube link or ID; anything else is rejected inline and saves nothing', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('[name="video"]')).toHaveValue('');
  await page.locator('[name="name"]').fill('Never Saved Either');
  await page.locator('[name="video"]').fill('https://vimeo.com/12345');
  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'is not a YouTube video' })).toBeVisible();
  await expect(page.locator('[name="video"]')).toHaveValue('https://vimeo.com/12345');
  await expect(page.locator('[name="name"]')).toHaveValue('Never Saved Either');
  expect(await revisions(page)).toBe(0);
  await page.goto(`/recipes/${id}`);
  await expect(page.getByRole('heading', { name: 'E2E Editor Loaf' })).toBeVisible();
  await expect(page.getByTestId('video')).toHaveCount(0);
});

test('a step dependency cycle is rejected naming the steps and saves nothing', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  // mix would depend on glaze, which depends on rest, which depends on mix.
  const [mix, rest, glaze] = [await stepKey(page, 0, 0), await stepKey(page, 0, 1), await stepKey(page, 1, 0)];
  await page.locator('li.step').nth(0).locator('summary').click();
  await page.locator(`[name="c.0.s.0.dep"][value="${glaze}"]`).check();
  await save(page);
  const alert = page.getByRole('alert').filter({ hasText: 'depend on each other in a circle' });
  await expect(alert).toBeVisible();
  await expect(alert).toContainText(`step 1 (${mix})`);
  await expect(alert).toContainText(`step 2 (${rest})`);
  await expect(alert).toContainText(`step 1 (${glaze})`);
  await expect(page.locator(`[name="c.0.s.0.dep"][value="${glaze}"]`)).toBeChecked();
  expect(await revisions(page)).toBe(0);
});

test('an ingredient the catalog does not know is refused until it is proposed', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="c.0.l.0.ing"]').fill('zzyzx root');
  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'No ingredient in the catalog' })).toBeVisible();
  await expect(page.locator('[name="c.0.l.0.propose"]')).not.toBeChecked();
  expect(await revisions(page)).toBe(0);
});

test('edits and saves a recipe: new part, line with qualifier, step with dependency', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('E2E Edited Loaf');
  await page.locator('[name="c.0.l.0.qty"]').fill('1½');

  await page.getByRole('button', { name: 'Add a part' }).click();
  await expect(page.locator('[name="c.2.name"]')).toBeVisible();
  await page.locator('[name="c.2.name"]').fill('Topping');

  // Ingredients and steps are separate sections, each grouped by component.
  const topping = page.locator('#ed-ingredients .component').nth(2);
  const toppingSteps = page.locator('#ed-steps .component').nth(2);
  await topping.getByRole('button', { name: 'Add ingredient' }).click();
  await expect(page.locator('[name="c.2.l.0.qty"]')).toBeVisible();
  await page.locator('[name="c.2.l.0.qty"]').fill('2-3');
  await page.locator('[name="c.2.l.0.unit"]').fill('tbsp');
  await page.locator('[name="c.2.l.0.ing"]').fill('butter');
  await page.locator('[name="c.2.l.0.optional"]').check();
  await topping.getByRole('button', { name: 'Add qualifier' }).click();
  await expect(page.locator('[name="c.2.l.0.q.0.text"]')).toBeVisible();
  await page.locator('[name="c.2.l.0.q.0.kind"]').selectOption('preparation');
  await page.locator('[name="c.2.l.0.q.0.text"]').fill('melted');

  // A step just added comes back with its details open.
  await toppingSteps.getByRole('button', { name: 'Add step' }).click();
  await expect(page.locator('[name="c.2.s.0.text"]')).toBeVisible();
  await page.locator('[name="c.2.s.0.text"]').fill('Brush on the melted butter.');
  await page.locator('[name="c.2.s.0.dur"]').fill('5 min');
  const glaze = await stepKey(page, 1, 0);
  await page.locator(`[name="c.2.s.0.dep"][value="${glaze}"]`).check();

  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByRole('heading', { name: 'E2E Edited Loaf' })).toBeVisible();

  // The edit is stored: the editor loads it back.
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('[name="c.0.l.0.qty"]')).toHaveValue('1 1/2');
  await expect(page.locator('[name="c.2.name"]')).toHaveValue('Topping');
  await expect(page.locator('[name="c.2.l.0.qty"]')).toHaveValue('2-3');
  await expect(page.locator('[name="c.2.l.0.q.0.text"]')).toHaveValue('melted');
  await expect(page.locator('[name="c.2.l.0.optional"]')).toBeChecked();
  await expect(page.locator(`[name="c.2.s.0.dep"][value="${glaze}"]`)).toBeChecked();
  // The new step was given a key by the store; the old one kept its key.
  expect(await stepKey(page, 1, 0)).toBe(glaze);
  expect(await stepKey(page, 2, 0)).toMatch(/^stp-[0-9a-hjkmnp-tv-z]{8}$/);
  // Exactly one revision, written by the save.
  expect(await revisions(page)).toBe(1);
});

test('moves a step to another part and reorders steps', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('li.step').nth(0).locator('summary').click();
  await page.locator('[name="c.0.s.0.to"]').selectOption('1');
  // Moves apply on the next post of the form; any action will do.
  await page.locator('#ed-steps .component').nth(1).getByRole('button', { name: 'Add step' }).click();
  await expect(page.locator('[name="c.1.s.2.text"]')).toBeVisible();
  await expect(page.locator('[name="c.0.s.0.text"]')).toHaveValue('Let the dough rest.');
  await expect(page.locator('[name="c.1.s.1.text"]')).toHaveValue('Mix the dough.');
  // Open its details; they may be open already, since editor.js keeps the rows a person opened.
  await page.locator('li.step').nth(2).locator('details.more').evaluate((d) => { (d as HTMLDetailsElement).open = true; });
  await page.locator('li.step').nth(2).getByRole('button', { name: 'Move step 3 up' }).click();
  await expect(page.locator('[name="c.1.s.0.text"]')).toHaveValue('Mix the dough.');
  await expect(page.locator('[name="c.1.s.1.text"]')).toHaveValue('Glaze the loaf.');
});

test('a stale save is rejected rather than overwriting a newer edit', async ({ browser, page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const other = await browser.newPage({ baseURL: origin });
  await other.goto(`/recipes/${id}/edit`);
  await other.locator('[name="name"]').fill('Edited in another tab');
  await save(other);
  await expect(other).toHaveURL(new RegExp(`/recipes/${id}$`));
  await other.close();

  await page.locator('[name="name"]').fill('Edited from the old form');
  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'was changed after you opened the editor' })).toBeVisible();
  await expect(page.locator('[name="name"]')).toHaveValue('Edited from the old form');
  await page.goto(`/recipes/${id}`);
  await expect(page.getByRole('heading', { name: 'Edited in another tab' })).toBeVisible();
});

// The UI calls a recipe component a "part" (docs/ui/design.md); the recipe
// here has several, so every per-part control is on the page.
test('the editor never says component, and lists the parts with links to them', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const panel = page.locator('#parts-panel');
  await expect(panel).toContainText(/This recipe has \w+ parts\./);
  await expect(panel.getByRole('link', { name: 'Go to part 1' })).toHaveAttribute('href', '#part-1');
  await expect(page.locator('#part-1 [name="c.0.name"]')).toHaveCount(1);
  expect(await userCopy(page)).not.toMatch(/\bcomponents?\b/i);
});

test('a recipe that does not exist is a 404', async ({ page }) => {
  const res = await page.goto('/recipes/999999/edit');
  expect(res?.status()).toBe(404);
});

test('meal type, spice level, stage kind and step title are set in the editor and persist', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('[name="meal_type"]')).toHaveValue('');
  await expect(page.locator('[name="spice"]')).toHaveValue('');
  await page.locator('[name="meal_type"]').selectOption('dessert');
  await page.locator('[name="spice"]').selectOption('0');
  await page.locator('li.step').nth(0).locator('summary').click();
  await page.locator('[name="c.0.s.0.kind"]').selectOption('braise');
  await page.locator('[name="c.0.s.0.title"]').fill('Start the braise');
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('[name="meal_type"]')).toHaveValue('dessert');
  // Spice 0 is a recipe that is not spicy, which is not the same as unset.
  await expect(page.locator('[name="spice"]')).toHaveValue('0');
  await expect(page.locator('[name="c.0.s.0.kind"]')).toHaveValue('braise');
  await expect(page.locator('[name="c.0.s.0.title"]')).toHaveValue('Start the braise');

  await page.locator('[name="meal_type"]').selectOption('');
  await page.locator('[name="spice"]').selectOption('');
  await page.locator('li.step').nth(0).locator('summary').click();
  await page.locator('[name="c.0.s.0.kind"]').selectOption('');
  await page.locator('[name="c.0.s.0.title"]').fill('');
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('[name="meal_type"]')).toHaveValue('');
  await expect(page.locator('[name="spice"]')).toHaveValue('');
  await expect(page.locator('[name="c.0.s.0.kind"]')).toHaveValue('');
  await expect(page.locator('[name="c.0.s.0.title"]')).toHaveValue('');
});

test('the cuisine is set from the fixed list in the editor, shown on the recipe page, and cleared', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const select = page.locator('[name="cuisine"]');
  await expect(select).toHaveValue('');
  await expect(select.locator('option').last()).toHaveText('Other');
  await select.selectOption('middle_eastern');
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByTestId('cuisine')).toHaveText('Middle Eastern');
  await page.goto(`/recipes/${id}/edit`);
  await expect(select).toHaveValue('middle_eastern');
  await select.selectOption('');
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByTestId('cuisine')).toHaveCount(0);
});

test('the editor keeps every control at least 44px tall and does not scroll sideways, on a tablet and a phone', async ({ page }) => {
  for (const viewport of [{ width: 1024, height: 768 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto(`/recipes/${id}/edit`);
    expect(await expectTapTargets(page), `${viewport.width}px`).toBeGreaterThan(20);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow, `${viewport.width}px page scrolls sideways`).toBeLessThanOrEqual(0);
  }
});

test('Save and Cancel stay in the viewport at the top and bottom of a long editor, clear of the tab bar; Enter and Cancel work', async ({ page }) => {
  for (const viewport of [{ width: 1024, height: 768 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto(`/recipes/${id}/edit`);
    const bar = page.locator('.savebar');
    const saveBtn = bar.getByRole('button', { name: 'Save recipe' });
    const cancel = bar.getByRole('link', { name: 'Cancel' });
    const where = (label: string) => `${viewport.width}px ${label}`;
    for (const label of ['top', 'bottom']) {
      if (label === 'bottom') await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
      else await page.evaluate(() => window.scrollTo(0, 0));
      await expect(saveBtn, where(label)).toBeInViewport({ ratio: 1 });
      await expect(cancel, where(label)).toBeInViewport({ ratio: 1 });
      const box = (await saveBtn.boundingBox())!;
      expect(box.height, where(label)).toBeGreaterThanOrEqual(44);
      expect((await cancel.boundingBox())!.height, where(label)).toBeGreaterThanOrEqual(44);
      const tabs = page.locator('nav .tabs, .tabs').first();
      const tabBox = await tabs.boundingBox();
      if (tabBox && (await tabs.evaluate((el) => getComputedStyle(el).position)) === 'fixed') {
        expect(box.y + box.height, where(`${label} above tab bar`)).toBeLessThanOrEqual(tabBox.y);
      }
      // The last field is not hidden behind the bar.
      if (label === 'bottom') {
        const last = page.locator('.editor input:visible, .editor textarea:visible, .editor select:visible').last();
        const lastBox = (await last.boundingBox())!;
        const barBox = (await bar.boundingBox())!;
        expect(lastBox.y + lastBox.height, where('last field')).toBeLessThanOrEqual(barBox.y + 1);
      }
    }
  }
  // Cancel leaves for the recipe without saving.
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('.savebar').getByRole('link', { name: 'Cancel' }).click();
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
});

test('Enter in a text field saves, with the bar in place', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const before = await revisions(page);
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('E2E Enter Loaf');
  await page.locator('[name="name"]').press('Enter');
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByRole('heading', { name: 'E2E Enter Loaf' })).toBeVisible();
  expect(await revisions(page)).toBe(before + 1);
});

// Saving is a plain form post: the bar works without script (#288).
test('without JavaScript, Save in the bar saves', async ({ browser }) => {
  const ctx = await browser.newContext({ javaScriptEnabled: false, baseURL: origin });
  const page = await ctx.newPage();
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('E2E No Script Loaf');
  await page.locator('.savebar').getByRole('button', { name: 'Save recipe' }).click();
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByRole('heading', { name: 'E2E No Script Loaf' })).toBeVisible();
  await ctx.close();
});

test('without JavaScript, Enter in a field saves', async ({ browser }) => {
  const ctx = await browser.newContext({ javaScriptEnabled: false, baseURL: origin });
  const page = await ctx.newPage();
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('E2E No Script Enter Loaf');
  await page.locator('[name="name"]').press('Enter');
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByRole('heading', { name: 'E2E No Script Enter Loaf' })).toBeVisible();
  await ctx.close();
});

// A structural action swaps the whole form (HTMX). editor.js keeps the page
// where it was and focuses what the action made, so Add qualifier halfway down
// the page does not jump.
test('Add qualifier keeps the page in place and focuses the new qualifier', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 768 });
  await page.goto(`/recipes/${id}/edit`);
  const line = page.locator('li.line').first();
  await line.locator('summary').click();
  const add = line.getByRole('button', { name: 'Add qualifier' });
  await add.evaluate((el) => el.scrollIntoView({ block: 'center' }));
  const before = await page.evaluate(() => window.scrollY);
  expect(before, 'the button is mid-page, not at the top').toBeGreaterThan(100);
  const n = await page.locator('[name^="c.0.l.0.q."][name$=".text"]').count();
  await add.click();
  await expect(page.locator(`[name="c.0.l.0.q.${n}.text"]`)).toBeFocused();
  const after = await page.evaluate(() => window.scrollY);
  expect(Math.abs(after - before), `scrolled from ${before} to ${after}`).toBeLessThanOrEqual(4);
});

// Dragging a step by its handle posts a move, and the server reorders.
test('dragging a step by its handle reorders the steps', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 768 });
  await page.goto(`/recipes/${id}/edit`);
  const steps = page.locator('ol[data-drag-list="move-step:0"] > li.step');
  await expect(steps).toHaveCount(2);
  const first = await page.locator('[name="c.0.s.0.text"]').inputValue();
  const second = await page.locator('[name="c.0.s.1.text"]').inputValue();
  await steps.nth(1).scrollIntoViewIfNeeded();
  const grip = (await steps.nth(0).locator('.grip').boundingBox())!;
  const target = (await steps.nth(1).boundingBox())!;
  const x = grip.x + grip.width / 2;
  const y0 = grip.y + grip.height / 2;
  const y1 = target.y + target.height * 0.8;
  await page.mouse.move(x, y0);
  await page.mouse.down();
  for (let i = 1; i <= 10; i++) await page.mouse.move(x, y0 + ((y1 - y0) * i) / 10);
  await page.mouse.up();
  await expect(page.locator('[name="c.0.s.0.text"]')).toHaveValue(second);
  await expect(page.locator('[name="c.0.s.1.text"]')).toHaveValue(first);
});
