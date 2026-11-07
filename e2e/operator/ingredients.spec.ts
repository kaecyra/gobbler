import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { DEV, OPERATOR, OTHER, STRANGER, asIdentity } from '../fixtures/identity';
import { portFromEnv } from '../fixtures/ports.mjs';
import { expectTapTargets } from '../fixtures/tap';

// Central ingredients (ADR-00018, Catalog): the operator is the only curator of
// the central layer and edits a central entry's name, aliases, aisle and
// density. This spec seeds its own data directory (ingredients/seed/main.go)
// and starts its own gobbler on it, as the promotion spec does. DEV is an
// operator here as well as a member of the dev household, so the edits run as
// an operator whose household is on the request: they must change the central
// entry every household reads, not the dev household's own aisle and aliases.
// Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_INGREDIENTS_PORT');
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let ids: { saffron: number; turmeric: number; pesto: number };

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_INGREDIENTS_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-ingredients-'));
  ids = JSON.parse(execFileSync('go', ['run', './e2e/operator/ingredients/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' }));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_OPERATOR_EMAILS: `${OPERATOR},${DEV}`,
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
    if (Date.now() > deadline) throw new Error('the central ingredients server did not start');
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

test.describe('nobody but the operator', () => {
  for (const who of [STRANGER, OTHER]) {
    test(`${who} reaches the central ingredient pages`, async ({ browser }) => {
      const ctx = await browser.newContext({ baseURL: origin, ...asIdentity(who) });
      try {
        for (const path of ['/operator/ingredients', `/operator/ingredients/${ids.saffron}`]) {
          const res = await ctx.request.get(path);
          expect(res.status(), path).toBe(403);
          expect(await res.text(), path).not.toContain('e2e saffron');
        }
        const save = await ctx.request.post(`/operator/ingredients/${ids.saffron}`, {
          form: { name: 'e2e saffron', aisle: 'intruded' },
        });
        expect(save.status()).toBe(403);
      } finally {
        await ctx.close();
      }
    });
  }
});

test.describe('the operator', () => {
  test.use(asIdentity(DEV));

  test('reaches the page from the operator page', async ({ browser }) => {
    const ctx = await browser.newContext({ baseURL: origin, ...asIdentity(OPERATOR) });
    try {
      const page = await ctx.newPage();
      await page.goto('/operator');
      await page.getByRole('navigation', { name: 'Operator' }).getByRole('link', { name: 'Central ingredients' }).click();
      await expect(page).toHaveURL(/\/operator\/ingredients$/);
      await expect(page.getByRole('heading', { name: 'Central ingredients', level: 1 })).toBeVisible();
    } finally {
      await ctx.close();
    }
  });

  test('lists the central entries and none of a household', async ({ page }) => {
    await page.goto('/operator/ingredients?q=e2e');
    const rows = page.getByTestId('central-row');
    await expect(rows).toHaveCount(2);
    await expect(rows.filter({ hasText: 'e2e saffron' })).toContainText('e2e saffron threads');
    await expect(page.locator('body')).not.toContainText('e2e dev pesto');
    const res = await page.goto(`/operator/ingredients/${ids.pesto}`);
    expect(res?.status()).toBe(404);
  });

  test('a name or alias another entry holds is shown on the form', async ({ page }) => {
    await page.goto(`/operator/ingredients/${ids.saffron}`);
    await page.locator('#name').fill('e2e turmeric');
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByTestId('form-error')).toContainText('already used by');
    await expect(page.locator('#name')).toHaveValue('e2e turmeric');
  });

  test('the central ingredient pages keep every control at least 44px tall', async ({ page }) => {
    await page.goto('/operator/ingredients');
    expect(await expectTapTargets(page)).toBeGreaterThan(1);
    await page.goto(`/operator/ingredients/${ids.saffron}`);
    expect(await expectTapTargets(page)).toBeGreaterThan(1);
  });

  test('edits the central entry, and every household reads the change', async ({ page, browser }) => {
    await page.goto('/operator/ingredients?q=saffron');
    await page.getByRole('link', { name: 'e2e saffron' }).click();
    await expect(page.locator('#usda_id')).toHaveValue('990001');
    await expect(page.locator('#usda_id')).not.toBeEditable();
    await page.locator('#name').fill('e2e saffron strands');
    await page.locator('#aliases').fill('e2e saffron threads\ne2e kesar');
    await page.locator('#aisle').fill('baking');
    await page.keyboard.press('Escape');
    await page.locator('#density').fill('0.2');
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page).toHaveURL(/\/operator\/ingredients$/);
    const row = page.getByTestId('central-row').filter({ hasText: 'e2e saffron strands' });
    await expect(row).toContainText('baking');
    await expect(row).toContainText('e2e kesar');

    // The operator's own household and another household both read the
    // edited central entry.
    for (const who of [DEV, OTHER]) {
      const ctx = await browser.newContext({ baseURL: origin, ...asIdentity(who) });
      try {
        const p = await ctx.newPage();
        await p.goto('/catalog?q=saffron');
        const rows = p.getByTestId('all-row');
        await expect(rows, who).toHaveCount(1);
        await expect(rows, who).toContainText('e2e saffron strands');
        await expect(rows, who).toContainText('baking');
        await expect(rows, who).toContainText('e2e kesar');
        await expect(rows.getByTestId('central-badge'), who).toBeVisible();
      } finally {
        await ctx.close();
      }
    }
  });
});
