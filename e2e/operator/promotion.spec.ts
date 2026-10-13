import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { OPERATOR, OTHER, STRANGER, asIdentity } from '../fixtures/identity';

// Promotion (ADR-00018, Catalog): the operator reviews the overlay entries
// households suggested for the central catalog and promotes one, which merges
// other households' exact duplicates into it. The shared server has no overlay
// entries, so this spec seeds its own data directory (promotion/seed/main.go)
// and starts its own gobbler on it, as the catalog spec does. Loopback only,
// throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = Number(process.env.GOBBLER_E2E_PROMOTION_PORT ?? 38522);
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let ids: { ghee: number; duplicate: number; private: number };

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_PROMOTION_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-promotion-'));
  ids = JSON.parse(execFileSync('go', ['run', './e2e/operator/promotion/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' }));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_OPERATOR_EMAILS: OPERATOR,
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
    if (Date.now() > deadline) throw new Error('the promotion server did not start');
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
    test(`${who} reaches the promotion pages`, async ({ browser }) => {
      const ctx = await browser.newContext({ baseURL: origin, ...asIdentity(who) });
      try {
        for (const path of ['/operator/promotions', `/operator/promotions/${ids.ghee}`]) {
          const res = await ctx.request.get(path);
          expect(res.status(), path).toBe(403);
          const body = await res.text();
          expect(body, path).not.toContain('e2e ghee');
          expect(body, path).not.toContain('e2e private sauce');
        }
        expect((await ctx.request.post(`/operator/promotions/${ids.ghee}`)).status()).toBe(403);
      } finally {
        await ctx.close();
      }
    });
  }
});

test.describe('the operator', () => {
  test.use(asIdentity(OPERATOR));

  test('sees what was suggested, how many duplicates a promotion merges, and no household', async ({ page }) => {
    await page.goto('/operator/promotions');
    const row = page.getByTestId('suggested-ingredient');
    await expect(row).toHaveCount(1);
    await expect(row).toContainText('e2e ghee');
    await expect(page.locator('body')).not.toContainText('e2e private sauce');
    await expect(page.locator('body')).not.toContainText('Dev household');
    await expect(page.locator('body')).not.toContainText('Other household');

    await row.getByRole('link', { name: 'e2e ghee' }).click();
    await expect(page.getByTestId('suggested-name')).toHaveText('e2e ghee');
    await expect(page.getByTestId('duplicate-count')).toHaveText('1');
    await expect(page.locator('body')).not.toContainText('Other household');
  });

  test('cannot open an entry nobody suggested', async ({ page }) => {
    const res = await page.goto(`/operator/promotions/${ids.private}`);
    expect(res?.status()).toBe(404);
    await expect(page.locator('body')).not.toContainText('e2e private sauce');
  });

  test('promotes it: every household reads it, and the duplicate is merged', async ({ page, browser }) => {
    await page.goto(`/operator/promotions/${ids.ghee}`);
    await page.getByRole('button', { name: 'Promote to the central catalog' }).click();
    await expect(page).toHaveURL(/\/operator\/promotions$/);
    await expect(page.getByTestId('none-suggested')).toBeVisible();

    // The other household had its own "e2e clarified butter"; it now reads the
    // central entry, which keeps answering to the household's own names.
    const other = await browser.newContext({ baseURL: origin, ...asIdentity(OTHER) });
    try {
      const p = await other.newPage();
      await p.goto('/catalog?q=e2e');
      const rows = p.getByTestId('all-row');
      await expect(rows).toHaveCount(1);
      await expect(rows).toContainText('e2e ghee');
      await expect(rows.getByTestId('central-badge')).toBeVisible();
      await expect(rows).toContainText('e2e other ghee');
      expect((await other.request.get(`/catalog/${ids.duplicate}`)).status()).toBe(404);
    } finally {
      await other.close();
    }
  });
});
