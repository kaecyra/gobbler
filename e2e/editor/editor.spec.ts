import { test, expect, type Locator, type Page } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { portFromEnv } from '../fixtures/ports.mjs';
import { expectTapTargets } from '../fixtures/tap';
import { userCopy } from '../fixtures/copy';
import { dragRow, scrollDrift, watchScroll } from './drag';

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

// The suggestion lists (issue #488). A native datalist offers only the entries
// that match the text already in the box, so a unit set to "cup" listed "cup"
// alone under a dropdown arrow. The editor draws its own list, which shows
// every suggestion when it opens and narrows as the person types.
test.describe('suggestion lists', () => {
  const list = (page: Page) => page.getByRole('listbox');
  const options = (page: Page) => list(page).getByRole('option');
  const texts = async (page: Page) => (await options(page).allTextContents()).map((t) => t.trim());

  test('opening a filled field lists every unit, not only the matches for its text', async ({ page }) => {
    await page.goto(`/recipes/${id}/edit`);
    const unit = page.locator('[name="c.0.l.0.unit"]');
    await expect(unit).toHaveValue('cup');
    await unit.click();
    await expect(page.getByRole('listbox', { name: 'Unit' })).toBeVisible();
    const all = await texts(page);
    expect(all.length).toBeGreaterThan(1);
    expect(all).toContain('cup');
    expect(all.some((t) => !t.includes('cup'))).toBe(true);
    const sizes = [await options(page).count()];

    // Typing narrows, case-insensitively, and an empty box lists everything again.
    await unit.fill('CUP');
    const narrowed = await texts(page);
    expect(narrowed.length).toBeLessThan(sizes[0]);
    expect(narrowed.every((t) => t.toLowerCase().includes('cup'))).toBe(true);
    await unit.fill('');
    await expect(options(page)).toHaveCount(sizes[0]);

    // Escape closes it without changing the value; choosing one fills the field.
    await unit.fill('cu');
    await page.keyboard.press('Escape');
    await expect(list(page)).toBeHidden();
    await expect(unit).toHaveValue('cu');
    await unit.click();
    await options(page).filter({ hasText: /^tbsp$/ }).click();
    await expect(unit).toHaveValue('tbsp');
    await expect(list(page)).toBeHidden();
  });

  test('the keyboard walks the list; Enter picks without saving the form', async ({ page }) => {
    // The form posts through htmx, so the URL never changes: what proves Enter
    // did not save is that no request was sent.
    const posts: string[] = [];
    page.on('request', (r) => { if (r.method() === 'POST') posts.push(r.url()); });
    await page.goto(`/recipes/${id}/edit`);
    const unit = page.locator('[name="c.0.l.0.unit"]');
    await unit.focus();
    await expect(list(page)).toBeVisible();
    await expect(unit).toHaveAttribute('role', 'combobox');
    await expect(unit).toHaveAttribute('aria-expanded', 'true');
    await page.keyboard.press('ArrowDown');
    const first = (await texts(page))[0];
    await expect(unit).toHaveAttribute('aria-activedescendant', (await options(page).first().getAttribute('id'))!);
    await page.keyboard.press('ArrowDown');
    await expect(unit).toHaveAttribute('aria-activedescendant', (await options(page).nth(1).getAttribute('id'))!);
    await page.keyboard.press('ArrowUp');
    await page.keyboard.press('Enter');
    await expect(unit).toHaveValue(first);
    await expect(unit).toHaveAttribute('aria-expanded', 'false');
    await page.waitForTimeout(500);
    expect(posts, 'Enter on an option must not post the form').toEqual([]);
  });

  test('Enter on an equipment suggestion picks it and does not press Add equipment', async ({ page }) => {
    const posts: string[] = [];
    page.on('request', (r) => { if (r.method() === 'POST') posts.push(r.url()); });
    await page.goto(`/recipes/${id}/edit`);
    const rows = await page.locator('[name$=".name"][name^="e."]').count();
    const equip = page.getByPlaceholder('Add equipment');
    await equip.focus();
    await expect(options(page).first()).toBeVisible();
    await page.keyboard.press('ArrowDown');
    const first = (await texts(page))[0];
    await page.keyboard.press('Enter');
    await expect(equip).toHaveValue(first);
    await page.waitForTimeout(500);
    expect(posts, 'Add equipment must not be pressed').toEqual([]);
    expect(await page.locator('[name$=".name"][name^="e."]').count()).toBe(rows);
  });

  test('a line added after a morph has the full unit list too', async ({ page }) => {
    await page.goto(`/recipes/${id}/edit`);
    const part = page.locator('#ed-ingredients .component').first();
    const units = part.locator('input[name^="c.0.l."][name$=".unit"]');
    const before = await units.count();
    await part.getByRole('button', { name: 'Add ingredient' }).click();
    await expect(units).toHaveCount(before + 1);
    const unit = units.last();
    await unit.fill('cup');
    await page.locator('[name="name"]').click();
    await unit.click();
    await expect(list(page)).toBeVisible();
    const all = await texts(page);
    expect(all.length).toBeGreaterThan(1);
    expect(all.some((t) => !t.includes('cup'))).toBe(true);
  });

  test('ingredient and equipment fields list every suggestion too', async ({ page }) => {
    await page.goto(`/recipes/${id}/edit`);
    const ing = page.locator('[name="c.0.l.0.ing"]');
    const was = await ing.inputValue();
    expect(was).not.toBe('');
    await ing.click();
    const names = await texts(page);
    expect(names).toContain(was);
    expect(names.some((t) => !t.includes(was))).toBe(true);
    await ing.fill(was.slice(0, 4));
    const near = await texts(page);
    expect(near.length).toBeLessThan(names.length);
    expect(near.every((t) => t.toLowerCase().includes(was.slice(0, 4).toLowerCase()))).toBe(true);

    const equip = page.getByPlaceholder('Add equipment');
    await equip.click();
    await expect(options(page).first()).toBeVisible();
    const gear = await texts(page);
    expect(gear.length).toBeGreaterThan(1);
    await equip.fill(gear[0].slice(0, 3));
    expect((await texts(page)).length).toBeLessThan(gear.length);
  });

  test('the package unit lists every unit', async ({ page }) => {
    await page.goto(`/recipes/${id}/edit`);
    const pkg = page.locator('[name="c.0.l.0.pkgunit"]');
    await pkg.evaluate((el) => el.closest('details')?.setAttribute('open', ''));
    await pkg.fill('cup');
    await page.locator('[name="name"]').click();
    await expect(list(page)).toBeHidden();
    await pkg.click();
    expect((await texts(page)).length).toBeGreaterThan(1);
  });

  test('a tap opens the list on a tablet, in reach of a finger', async ({ browser }) => {
    const context = await browser.newContext({ baseURL: origin, hasTouch: true, viewport: { width: 820, height: 1180 } });
    const page = await context.newPage();
    await page.goto(`/recipes/${id}/edit`);
    const unit = page.locator('[name="c.0.l.0.unit"]');
    await unit.tap();
    await expect(list(page)).toBeVisible();
    const tall = await options(page).evaluateAll((els) => els.filter((e) => e.getBoundingClientRect().height < 44).length);
    expect(tall, 'options shorter than --tap').toBe(0);
    const box = (await list(page).boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(820);
    expect(box.y + box.height).toBeLessThanOrEqual(1180);
    await options(page).nth(1).tap();
    await expect(list(page)).toBeHidden();
    await expect(unit).not.toHaveValue('');
    await context.close();
  });
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
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
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

test('a step has no part control: dragging is the only way to move it between parts', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('li.step').nth(0).locator('summary').click();
  await expect(page.locator('li.step').nth(0).getByLabel('Part', { exact: true })).toHaveCount(0);
  await expect(page.locator('[name$=".to"]')).toHaveCount(0);
});

test('a stale save is rejected rather than overwriting a newer edit', async ({ browser, page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const other = await browser.newPage({ baseURL: origin });
  await other.goto(`/recipes/${id}/edit`);
  await other.locator('[name="name"]').fill('Edited in another tab');
  await save(other);
  await expect(other).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
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
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
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
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
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
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
  await expect(page.getByTestId('cuisine')).toHaveText('Middle Eastern');
  await page.goto(`/recipes/${id}/edit`);
  await expect(select).toHaveValue('middle_eastern');
  await select.selectOption('');
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
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
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
});

test('Enter in a text field saves, with the bar in place', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const before = await revisions(page);
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('E2E Enter Loaf');
  await page.locator('[name="name"]').press('Enter');
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
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
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
  await expect(page.getByRole('heading', { name: 'E2E No Script Loaf' })).toBeVisible();
  await ctx.close();
});

// The name box under Steps is a script's convenience: it has no name, so
// typing in it without a script would be lost. It is hidden then, and the
// read-only title shows instead.
test('without JavaScript, the Steps name box is hidden and the part title shows', async ({ browser }) => {
  const ctx = await browser.newContext({ javaScriptEnabled: false, baseURL: origin });
  const page = await ctx.newPage();
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('#ed-steps input[data-part-name]').first()).toBeHidden();
  await expect(page.locator('#ed-steps .grp-title').first()).toBeVisible();
  await expect(page.locator('[name="c.0.name"]')).toBeVisible();
  await ctx.close();
});

test('without JavaScript, Enter in a field saves', async ({ browser }) => {
  const ctx = await browser.newContext({ javaScriptEnabled: false, baseURL: origin });
  const page = await ctx.newPage();
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('E2E No Script Enter Loaf');
  await page.locator('[name="name"]').press('Enter');
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
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
  // Sample the scroll position on every frame, so a jump that is scrolled
  // back afterwards still fails.
  await watchScroll(page, 1500);
  await add.click();
  await expect(page.locator(`[name="c.0.l.0.q.${n}.text"]`)).toBeFocused();
  await page.waitForTimeout(1600);
  const worst = await scrollDrift(page, before);
  expect(worst, `scrollY moved up to ${worst}px from ${before}`).toBeLessThanOrEqual(4);
});

// Dragging a step by its handle posts a move, and the server reorders.
test('dragging a step by its handle reorders the steps', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 768 });
  await page.goto(`/recipes/${id}/edit`);
  const steps = page.locator('ol[data-drag-list="move-step:0"] > li.step');
  await expect(steps).toHaveCount(2);
  const first = await page.locator('[name="c.0.s.0.text"]').inputValue();
  const second = await page.locator('[name="c.0.s.1.text"]').inputValue();
  await dragRow(page, steps.nth(0), steps.nth(1), 0.8);
  await expect(page.locator('[name="c.0.s.0.text"]')).toHaveValue(second);
  await expect(page.locator('[name="c.0.s.1.text"]')).toHaveValue(first);
});

// Parts are dragged between by the same handle as within one. The page ends
// with Dough, Glaze and Topping from the tests above; these rows are read from
// the page rather than assumed.
test('dragging an ingredient into another part saves it there at the dropped position, with every field', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 1400 });
  await page.goto(`/recipes/${id}/edit`);
  const lines = (ci: number) => page.locator(`ol[data-drag-list="move-line:${ci}"] > li.line`);
  const get = (ci: number, li: number, f: string) => page.locator(`[name="c.${ci}.l.${li}.${f}"]`).inputValue();
  const was = { qty: await get(0, 0, 'qty'), unit: await get(0, 0, 'unit'), ing: await get(0, 0, 'ing') };
  const before0 = await lines(0).count();
  const before1 = await lines(1).count();
  expect(before1, 'part 2 has a line to drop onto').toBeGreaterThan(0);
  await dragRow(page, lines(0).nth(0), lines(1).nth(0), 0.8);
  await expect(lines(0)).toHaveCount(before0 - 1);
  await expect(lines(1)).toHaveCount(before1 + 1);
  const at = async () => ({ qty: await get(1, 1, 'qty'), unit: await get(1, 1, 'unit'), ing: await get(1, 1, 'ing') });
  expect(await at()).toEqual(was);
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
  await page.goto(`/recipes/${id}/edit`);
  await expect(lines(1)).toHaveCount(before1 + 1);
  expect(await at()).toEqual(was);
});

test('dragging a step into another part saves it there with its title, kind, duration, dependencies and equipment', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 1400 });
  await page.goto(`/recipes/${id}/edit`);
  const steps = (ci: number) => page.locator(`ol[data-drag-list="move-step:${ci}"] > li.step`);
  const field = (ci: number, si: number, f: string) => page.locator(`[name="c.${ci}.s.${si}.${f}"]`);
  const moving = await steps(0).count() - 1;
  const key = await field(0, moving, 'id').inputValue();
  const was = {
    text: await field(0, moving, 'text').inputValue(),
    dur: await field(0, moving, 'dur').inputValue(),
    title: await field(0, moving, 'title').inputValue(),
    kind: await field(0, moving, 'kind').inputValue(),
    deps: await page.locator(`[name="c.0.s.${moving}.dep"]:checked`).evaluateAll((els) => els.map((e) => (e as HTMLInputElement).value)),
  };
  expect(was.deps.length, 'the step depends on another').toBeGreaterThan(0);
  // Link a piece of equipment to the step, so the move has a link to keep.
  await page.locator('#ed-equipment input[placeholder="Add equipment"]').fill('whisk');
  await page.getByRole('button', { name: 'Add equipment' }).click();
  // The answer focuses the next empty slot, and may scroll to it; let that settle.
  await expect(page.locator('#ed-equipment input[placeholder="Add equipment"]')).toBeFocused();
  await page.locator('li.equipment').first().locator('summary').click();
  const link = page.locator(`li.equipment [name="e.0.step"][value="${key}"]`);
  await link.check();
  await page.evaluate(() => window.scrollTo(0, 0));
  const n1 = await steps(1).count();
  await dragRow(page, steps(0).nth(moving), steps(1).nth(0), 0.2);
  await expect(steps(1)).toHaveCount(n1 + 1);
  await expect(field(1, 0, 'id')).toHaveValue(key);
  const now = async () => ({
    text: await field(1, 0, 'text').inputValue(),
    dur: await field(1, 0, 'dur').inputValue(),
    title: await field(1, 0, 'title').inputValue(),
    kind: await field(1, 0, 'kind').inputValue(),
    deps: await page.locator('[name="c.1.s.0.dep"]:checked').evaluateAll((els) => els.map((e) => (e as HTMLInputElement).value)),
  });
  expect(await now()).toEqual(was);
  await expect(link).toBeChecked();
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
  await page.goto(`/recipes/${id}/edit`);
  await expect(field(1, 0, 'id')).toHaveValue(key);
  expect(await now()).toEqual(was);
  await page.locator('li.equipment').first().locator('summary').click();
  await expect(link).toBeChecked();
});

test('dragging a row into another part does not jump the page', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.goto(`/recipes/${id}/edit`);
  const steps = (ci: number) => page.locator(`ol[data-drag-list="move-step:${ci}"] > li.step`);
  const n0 = await steps(0).count();
  const n1 = await steps(1).count();
  // Put the source a little below the header, so the target is on screen too.
  const top = (await steps(0).nth(n0 - 1).boundingBox())!.y;
  await page.evaluate((dy) => window.scrollBy(0, dy), top - 220);
  expect(await page.evaluate(() => window.scrollY), 'the rows are mid-page').toBeGreaterThan(100);
  // The source is above the target, so it moves down into the next part.
  // Both rows must be on screen, clear of the edge where a drag scrolls the page.
  const source = steps(0).nth(n0 - 1);
  const target = steps(1).nth(0);
  expect((await source.boundingBox())!.y, 'the dragged row is on screen').toBeGreaterThan(100);
  const t = (await target.boundingBox())!;
  expect(t.y + t.height, 'the target row is on screen').toBeLessThan(900 - 100);
  const before = await page.evaluate(() => window.scrollY);
  await watchScroll(page, 2500);
  await dragRow(page, source, target, 0.8);
  await page.waitForTimeout(2600);
  const worst = await scrollDrift(page, before);
  await expect(steps(1)).toHaveCount(n1 + 1);
  expect(worst, `scrollY moved up to ${worst}px`).toBeLessThanOrEqual(4);
});

// One name, two inputs: the one under Steps has no name, so the form posts the
// one under Ingredients.
test('a part is named from Steps as well as Ingredients, the two kept in step, and the typed name is saved', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const ingredients = page.locator('[name="c.1.name"]');
  const steps = page.locator('#ed-steps input[data-part-name="1"]');
  await expect(steps).toHaveValue(await ingredients.inputValue());
  await expect(steps).not.toHaveAttribute('name', /.*/);
  await steps.fill('Icing');
  await expect(ingredients).toHaveValue('Icing');
  await ingredients.fill('Frosting');
  await expect(steps).toHaveValue('Frosting');
  await steps.fill('Sugar glaze');
  expect(await page.locator('form#editor [name="c.1.name"]').count(), 'one input posts the name').toBe(1);
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
  await page.goto(`/recipes/${id}/edit`);
  await expect(ingredients).toHaveValue('Sugar glaze');
  await expect(steps).toHaveValue('Sugar glaze');
});

// The author's notes (ADR-00041): rows with an optional title and a text of one
// paragraph, added, dragged, removed and saved like the other lists.
test('author notes are added, reordered by their handle, paragraph-limited, removed and saved', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.getByRole('heading', { name: "Author's Notes" })).toBeVisible();
  const rows = page.locator('ol[data-drag-list="move-note"] > li.note');
  await expect(rows).toHaveCount(0);

  await page.getByRole('button', { name: 'Add note' }).click();
  await expect(page.locator('[name="n.0.text"]')).toBeFocused();
  await page.locator('[name="n.0.title"]').fill('Flour');
  await page.locator('[name="n.0.text"]').fill('Bread flour gives a chewier crust.');
  await page.getByRole('button', { name: 'Add note' }).click();
  await page.locator('[name="n.1.text"]').fill('Keeps three days.');
  await expect(rows).toHaveCount(2);
  // What was typed survives the structural button.
  await expect(page.locator('[name="n.0.title"]')).toHaveValue('Flour');

  // Dragging by the handle posts a move, and the server reorders.
  await dragRow(page, rows.nth(0), rows.nth(1), 0.8);
  await expect(page.locator('[name="n.0.text"]')).toHaveValue('Keeps three days.');
  await expect(page.locator('[name="n.1.title"]')).toHaveValue('Flour');

  // Only the paragraph rule refuses a note, on its row, nothing saved.
  await page.locator('[name="n.0.text"]').fill('Keeps three days.\n\nA second paragraph.');
  await save(page);
  await expect(rows.nth(0).getByRole('alert')).toContainText('one paragraph');
  await page.locator('[name="n.0.text"]').fill('x'.repeat(1000));
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));

  // Saved: the recipe page shows both after the last step, the 1000-character one first.
  const notes = page.getByTestId('author-note');
  await expect(notes).toHaveCount(2);
  await expect(notes.nth(1)).toContainText('Flour: Bread flour gives a chewier crust.');

  // Remove one and save.
  await page.goto(`/recipes/${id}/edit`);
  await expect(rows).toHaveCount(2);
  await page.getByRole('button', { name: 'Remove note 1' }).click();
  await expect(rows).toHaveCount(1);
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
  await expect(page.getByTestId('author-note')).toHaveCount(1);
  await expect(page.getByTestId('author-note-title')).toHaveText('Flour:');

});

// Issue #496. A qualifier chip is one pill: grip, kind, text and remove stay
// inside it, and the chips wrap onto the next row instead of running past the
// panel. The three qualifiers are what the parser makes of "1 cup (240ml) whole
// milk, warmed to about 110°F (43°C)": a form, a short note and a long note.
test.describe('qualifier chips', () => {
  const quals = [
    { kind: 'form', text: 'whole' },
    { kind: 'note', text: '240ml' },
    { kind: 'note', text: 'warmed to about 110°F (43°C)' },
    // The widest kind label, so the narrowest phone still has to fit it.
    { kind: 'preparation', text: 'diced' },
  ];

  for (const viewport of [{ width: 1280, height: 900 }, { width: 820, height: 1180 }, { width: 390, height: 844 }, { width: 360, height: 780 }]) {
    test(`keep every control inside its pill and inside the panel at ${viewport.width}px`, async ({ page }) => {
      await page.setViewportSize(viewport);
      await page.goto(`/recipes/${id}/edit`);
      const line = page.locator('li.line').first();
      await line.locator('summary').click();
      const have = await line.locator('.qchip').count();
      for (let i = have; i < quals.length; i++) {
        await line.getByRole('button', { name: 'Add qualifier' }).click();
        await expect(line.locator('.qchip').nth(i).locator('input[type=text]')).toBeFocused();
      }
      for (const [i, q] of quals.entries()) {
        await line.locator('.qchip').nth(i).locator('select').selectOption(q.kind);
        await line.locator('.qchip').nth(i).locator('input[type=text]').fill(q.text);
      }
      const chips = line.locator('.qchip');
      await expect(chips).toHaveCount(quals.length);
      const panel = (await line.locator('.more-body').boundingBox())!;
      for (let i = 0; i < quals.length; i++) {
        const chip = chips.nth(i);
        await expect(chip, `chip ${i} is visible`).toBeVisible();
        const box = (await chip.boundingBox())!;
        expect(box.x, `chip ${i} starts inside the panel`).toBeGreaterThanOrEqual(panel.x - 0.5);
        expect(box.x + box.width, `chip ${i} ends inside the panel`).toBeLessThanOrEqual(panel.x + panel.width + 0.5);
        for (const part of ['.grip', 'select', 'input[type=text]', '.ibtn']) {
          const where = `chip ${i} ${part}`;
          const el = chip.locator(part);
          // The drag grip shows only once the editor script has run.
          await expect(el, `${where} is visible`).toBeVisible();
          const inner = (await el.boundingBox())!;
          expect(inner.x, `${where} left`).toBeGreaterThanOrEqual(box.x - 0.5);
          expect(inner.x + inner.width, `${where} right`).toBeLessThanOrEqual(box.x + box.width + 0.5);
          expect(inner.y, `${where} top`).toBeGreaterThanOrEqual(box.y - 0.5);
          expect(inner.y + inner.height, `${where} bottom`).toBeLessThanOrEqual(box.y + box.height + 0.5);
        }
        // The kind select shows its whole label: it is as wide as the label's
        // text, with room for its padding and arrow, and no narrower than a tap target.
        const sel = await chip.locator('select').evaluate((el: HTMLSelectElement) => {
          const cs = getComputedStyle(el);
          const ctx = document.createElement('canvas').getContext('2d')!;
          ctx.font = `${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
          return { width: el.getBoundingClientRect().width, label: ctx.measureText(el.selectedOptions[0].text).width };
        });
        expect(sel.width, `chip ${i} kind select shows its label`).toBeGreaterThanOrEqual(sel.label + 24);
        expect(sel.width, `chip ${i} kind select is a tap target wide`).toBeGreaterThanOrEqual(44);
        // The text is not cut off where the row has room for it. A phone is
        // narrower than the long note, and a one-line field scrolls it there.
        // On the narrowest phones the widest kind label leaves the text box a
        // few characters, and it scrolls there too.
        if (viewport.width < 820 && (quals[i].text.length > 10 || (viewport.width <= 400 && quals[i].kind === 'preparation'))) continue;
        const clipped = await chip.locator('input[type=text]').evaluate((el: HTMLInputElement) => el.scrollWidth > el.clientWidth);
        expect(clipped, `chip ${i} text is cut off`).toBe(false);
      }
    });
  }
});

// Saves, so it runs last. Not part of the describe above: it edits the recipe.
test('suggestion lists: a value that is not in the list still saves', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const ing = page.locator('input[name$=".l.0.ing"]').first();
  const name = await ing.getAttribute('name');
  await ing.click();
  await ing.fill('e2e saffron');
  await expect(page.getByRole('listbox').getByRole('option')).toHaveCount(0);
  // The server asks before it adds an unknown ingredient to the catalog.
  await save(page);
  await page.getByRole('checkbox', { name: 'Add this ingredient to the catalog as new and unreviewed' }).check();
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}(?:-[a-z0-9-]*)?$`));
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator(`[name="${name}"]`)).toHaveValue('e2e saffron');
});

// A refused save comes back as a 200 with the messages inline, which once left
// "Save" looking dead: the error sat far below the fold. The page says so at
// the top, brings the first error into view and counts errors on the save bar.
test('a refused save shows the not-saved callout and the first error in view, and the bar counts it', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto(`/recipes/${id}/edit`);
  await page.getByRole('button', { name: 'Add note' }).last().click();
  const text = page.locator('[name^="n."][name$=".text"]').last();
  await text.fill('One paragraph.\n\nAnother paragraph.');
  await page.evaluate(() => window.scrollTo(0, 0));
  await save(page);
  const callout = page.getByTestId('not-saved');
  await expect(callout).toContainText('Not saved. 1 field needs fixing.');
  await expect(callout).toBeInViewport();
  const error = page.getByRole('alert').filter({ hasText: 'one paragraph' });
  await expect(error).toBeInViewport();
  await expect(error).toBeFocused();
  await expect(page.getByTestId('bar-errors')).toContainText('1 field stops this being saved');
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}/edit$`));
});
