import { test, expect, type Page } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { portFromEnv } from '../fixtures/ports.mjs';
import { expectTapTargets } from '../fixtures/tap';

// An agent's proposed edit to a recipe (ADR-00035): shown inline on the recipe
// page, reviewed in the editor, recorded in history. The data is seeded through
// the real stores (seed-proposal/main.go): each test uses a recipe of its own,
// each with a pending proposal that renames it, raises the flour to 3 cups, drops
// the salt, rewrites the first step, removes the second and adds a third.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_PROPOSAL_PORT');
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

interface Seeded {
  recipe: number;
  token: string;
}
let seeded: Record<'marks' | 'reject' | 'accept' | 'stale' | 'race' | 'restore', Seeded>;
let dataDir = '';
let server: ChildProcess | undefined;

const REASON = 'Less salt and a longer rest make a better crumb.';

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_PROPOSAL_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-proposal-'));
  seeded = JSON.parse(execFileSync('go', ['run', './e2e/recipeview/seed-proposal', dataDir], { cwd: repoRoot, encoding: 'utf8' }));
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
    if (Date.now() > deadline) throw new Error('the proposal server did not start');
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

const save = (page: Page) => page.getByRole('button', { name: 'Save recipe' }).first().click();

async function revisionCount(page: Page, id: number): Promise<number> {
  await page.goto(`/recipes/${id}/history`);
  return page.getByTestId('revision').count();
}

// An ordinary editor save: the recipe moves on to a new version.
async function editInEditor(page: Page, id: number, description: string) {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="description"]').fill(description);
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
}

test('a pending proposal is marked in place with its old and new values', async ({ page }) => {
  const { recipe: id } = seeded.marks;
  await page.goto(`/recipes/${id}`);
  const banner = page.getByTestId('proposal-banner');
  await expect(banner).toBeVisible();
  await expect(banner.getByTestId('proposal-reason')).toContainText(REASON);
  await expect(banner.getByTestId('proposal-by')).toContainText('API key gobk_e2e7');
  await expect(banner.getByTestId('proposal-review')).toHaveAttribute('href', new RegExp(`/recipes/${id}/edit\\?proposal=${seeded.marks.token}`));

  // The recipe itself is the live one, unchanged.
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('E2E Marks Loaf');
  const fields = page.getByTestId('proposal-field');
  await expect(fields).toHaveCount(1);
  await expect(fields.first()).toContainText('E2E Marks Loaf');
  await expect(fields.first()).toContainText('E2E Marks Loaf Revised');

  // A changed line keeps the live text and shows the proposed one; a removed
  // line says so.
  await expect(page.getByTestId('proposal-line-new')).toContainText(/3 .*flour/);
  // The proposed line shows its qualifiers in the same style as a live line.
  const proposed = page.getByTestId('proposal-line-new');
  await expect(proposed.getByTestId('qualifier')).toHaveText(['sifted', 'warmed']);
  await expect(proposed.getByTestId('qualifier').first()).toHaveCSS('font-style', 'italic');
  expect((await proposed.innerText()).trim()).toMatch(/flour sifted warmed \(optional\)$/);
  await expect(page.getByTestId('proposal-line-removed')).toHaveCount(1);
  // A changed step shows both texts; a removed one and an added one are noted.
  const changed = page.getByTestId('proposal-step-changed');
  await expect(changed).toContainText('Mix the dough.');
  await expect(changed).toContainText('Mix the dough and let it rest.');
  await expect(page.getByTestId('proposal-step-removed')).toContainText('Bake the loaf.');
  await expect(page.getByTestId('proposal-step-added')).toContainText('Shape the loaf.');
  await expectTapTargets(page, '[data-testid="proposal-banner"]');
});

test('the toggle hides the marks and leaves a compact banner, and the next visit shows them again', async ({ page }) => {
  const { recipe: id } = seeded.marks;
  await page.goto(`/recipes/${id}`);
  const toggle = page.getByTestId('proposal-marks-toggle');
  await expect(toggle).toBeChecked();
  await toggle.uncheck();
  for (const marked of ['proposal-field', 'proposal-line-new', 'proposal-line-removed', 'proposal-step-changed', 'proposal-step-removed', 'proposal-step-added']) {
    await expect(page.getByTestId(marked)).toBeHidden();
  }
  // The banner stays, with its two actions; its detail is gone.
  await expect(page.getByTestId('proposal-banner')).toBeVisible();
  await expect(page.getByTestId('proposal-review')).toBeVisible();
  await expect(page.getByTestId('proposal-reject')).toBeVisible();
  await expect(page.getByTestId('proposal-reason')).toBeHidden();
  // Scaling swaps the recipe in place; the choice survives it.
  await page.getByRole('link', { name: '2×' }).click();
  await expect(page.getByTestId('scaled-note')).toBeVisible();
  await expect(page.getByTestId('proposal-step-changed')).toBeHidden();
  await expect(toggle).not.toBeChecked();

  await page.goto(`/recipes/${id}`);
  await expect(page.getByTestId('proposal-marks-toggle')).toBeChecked();
  await expect(page.getByTestId('proposal-step-changed')).toBeVisible();
});

test('Reject turns the proposal down and leaves the recipe unchanged', async ({ page }) => {
  const { recipe: id } = seeded.reject;
  await page.goto(`/recipes/${id}`);
  await expect(page.getByTestId('proposal-reject').locator('xpath=ancestor::form')).toHaveAttribute('action', `/recipes/${id}/proposal/reject`);
  await page.getByTestId('proposal-reject').click();
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByTestId('proposal-banner')).toHaveCount(0);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('E2E Reject Loaf');
  await expect(page.getByText('Mix the dough.')).toBeVisible();
  await expect(page.getByText('Bake the loaf.')).toBeVisible();
  expect(await revisionCount(page, id)).toBe(0);
  // It is no longer open for review.
  const res = await page.goto(`/recipes/${id}/edit?proposal=${seeded.reject.token}`);
  expect(res?.status()).toBe(409);
  await expect(page.getByText('This proposal was rejected.')).toBeVisible();
});

test('cook mode and the share form show no proposal', async ({ page }) => {
  const { recipe: id } = seeded.marks;
  for (const path of [`/recipes/${id}/cook`, `/shares/new/${id}`]) {
    const res = await page.goto(path);
    expect(res?.status()).toBe(200);
    await expect(page.getByTestId('proposal-banner')).toHaveCount(0);
    const text = await page.locator('body').innerText();
    expect(text).not.toContain('Revised');
    expect(text).not.toContain('Shape the loaf.');
    expect(text).not.toContain(REASON);
  }
  // Cook mode shows the live steps.
  await page.goto(`/recipes/${id}/cook`);
  await expect(page.locator('body')).toContainText('Bake the loaf.');
});

test('the editor opens the proposal pre-filled with a summary, and saving it records the proposal in history', async ({ page }) => {
  const { recipe: id, token } = seeded.accept;
  await page.goto(`/recipes/${id}/edit?proposal=${token}`);
  const summary = page.getByTestId('proposal-summary');
  await expect(summary).toContainText('API key gobk_e2e7');
  await expect(summary.getByTestId('proposal-reason')).toContainText(REASON);
  const changes = summary.getByTestId('proposal-changes');
  await expect(changes).toContainText('Name: E2E Accept Loaf, now E2E Accept Loaf Revised');
  await expect(changes).toContainText('Ingredient changed: 2 cup all-purpose flour, now 3 cup all-purpose flour');
  await expect(changes).toContainText('Ingredient removed: 1 cup salt');
  await expect(changes).toContainText('Step 1 changed: Mix the dough., now Mix the dough and let it rest.');
  await expect(changes).toContainText('Step 2 removed: Bake the loaf.');
  await expect(changes).toContainText('Step added at 2: Shape the loaf.');
  await expect(page.locator('[name="name"]')).toHaveValue('E2E Accept Loaf Revised');
  await expect(page.locator('[name="c.0.l.0.qty"]')).toHaveValue('3');
  await expect(page.locator('[name="c.0.s.0.text"]')).toHaveValue('Mix the dough and let it rest.');

  // The person changes the proposal before saving; saving is an ordinary save.
  await page.locator('[name="description"]').fill('Edited by the person.');
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByTestId('proposal-banner')).toHaveCount(0);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('E2E Accept Loaf Revised');
  await expect(page.getByText('Edited by the person.')).toBeVisible();

  await page.goto(`/recipes/${id}/history`);
  const rows = page.getByTestId('revision');
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText('replaced by a saved proposal');
  await expect(rows.first()).toContainText('by dev@localhost');
  const note = rows.first().getByTestId('revision-proposal');
  await expect(note).toContainText('API key gobk_e2e7');
  await expect(note).toContainText(REASON);
  // The current version is the one the proposal produced, and says so too.
  await expect(page.getByTestId('current-proposal')).toContainText(REASON);

  // A saved proposal cannot be opened again.
  const again = await page.goto(`/recipes/${id}/edit?proposal=${token}`);
  expect(again?.status()).toBe(409);
});

test('restoring the version a proposal replaced round-trips the whole recipe', async ({ page }) => {
  const { recipe: id, token } = seeded.restore;
  await page.goto(`/recipes/${id}/edit?proposal=${token}`);
  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('E2E Restore Loaf Revised');
  // The current version came from the proposal, and history says so.
  await page.goto(`/recipes/${id}/history`);
  await expect(page.getByTestId('current-proposal')).toContainText(REASON);

  await page.goto(`/recipes/${id}/history/1`);
  await page.getByText('Restore this version').click();
  await page.locator('form', { hasText: 'Yes, restore version 1' }).getByRole('button').click();
  await expect(page).toHaveURL(`/recipes/${id}/history`);
  await page.goto(`/recipes/${id}`);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('E2E Restore Loaf');
  await expect(page.getByText('A plain loaf.')).toBeVisible();
  await expect(page.getByText(/2 .*flour/)).toBeVisible();
  await expect(page.getByText(/1 .*salt/)).toBeVisible();
  await expect(page.getByText('Mix the dough.', { exact: true })).toBeVisible();
  await expect(page.getByText('Bake the loaf.', { exact: true })).toBeVisible();
  await expect(page.getByText('Shape the loaf.')).toHaveCount(0);
});

test('a proposal goes stale after an editor save: out-of-date banner with Reject only, and it cannot be opened', async ({ page }) => {
  const { recipe: id, token } = seeded.stale;
  await editInEditor(page, id, 'Changed by a person first.');

  const banner = page.getByTestId('proposal-banner');
  await expect(banner.getByTestId('proposal-stale')).toContainText('out of date');
  await expect(banner.getByTestId('proposal-reject')).toBeVisible();
  await expect(banner.getByTestId('proposal-review')).toHaveCount(0);
  for (const marked of ['proposal-field', 'proposal-line-new', 'proposal-step-changed', 'proposal-step-added']) {
    await expect(page.getByTestId(marked)).toHaveCount(0);
  }
  // The live recipe is the person's.
  await expect(page.getByText('Changed by a person first.')).toBeVisible();

  const res = await page.goto(`/recipes/${id}/edit?proposal=${token}`);
  expect(res?.status()).toBe(409);
  await expect(page.getByText('out of date')).toBeVisible();
  await expect(page.locator('[name="name"]')).toHaveCount(0);
});

test('a review save that races an editor save is refused and the recipe is left as the editor save made it', async ({ page, browser }) => {
  const { recipe: id, token } = seeded.race;
  // The reviewer has the proposal open, at version 1.
  await page.goto(`/recipes/${id}/edit?proposal=${token}`);
  await expect(page.getByTestId('proposal-summary')).toBeVisible();

  // Meanwhile someone saves the recipe in the editor.
  const other = await browser.newPage({ baseURL: origin });
  await editInEditor(other, id, 'Saved first.');
  await other.close();

  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'out of date' })).toBeVisible();
  await page.goto(`/recipes/${id}`);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('E2E Race Loaf');
  await expect(page.getByText('Saved first.')).toBeVisible();
  await expect(page.getByText('Shape the loaf.')).toHaveCount(0);
  // Only the editor save left a revision, and it is an ordinary edit.
  await page.goto(`/recipes/${id}/history`);
  await expect(page.getByTestId('revision')).toHaveCount(1);
  await expect(page.getByTestId('revision').first()).toContainText('replaced by an edit');
});
