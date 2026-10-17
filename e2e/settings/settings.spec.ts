import { test, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { portFromEnv } from '../fixtures/ports.mjs';

// The settings page shows the live backup job, whose state depends on how the
// server is configured, so this spec starts its own gobbler per state on a
// throwaway data directory in dev-auth mode (loopback only): one with no S3
// endpoint (snapshots stay local) and one whose S3 endpoint refuses
// connections (every upload fails). Both listen on the same port, one after
// the other.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_SETTINGS_PORT');
const origin = `http://127.0.0.1:${port}`;

test.use({ baseURL: origin });
// The servers share a port and the tests build on each other's backups.
test.describe.configure({ mode: 'serial' });

let dataDir = '';
let server: ChildProcess | undefined;

async function start(extraEnv: Record<string, string>) {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_SETTINGS_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-settings-'));
  server = spawn(bin!, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      // /settings is operator-only (ADR-00018); the dev identity is the operator here.
      GOBBLER_OPERATOR_EMAILS: 'dev@localhost',
      ...extraEnv,
    },
    stdio: 'inherit',
  });
  const deadline = Date.now() + 20_000;
  for (;;) {
    try {
      if ((await fetch(`${origin}/healthz`)).ok) return;
    } catch {
      // not listening yet
    }
    if (Date.now() > deadline) throw new Error('the settings server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
}

async function stop() {
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
  server = undefined;
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
  dataDir = '';
}

async function health(): Promise<Record<string, string>> {
  const res = await fetch(`${origin}/healthz`);
  return (await res.json()).details;
}

test.describe('local only', () => {
  test.beforeAll(() => start({}));
  test.afterAll(stop);

  test('shows never backed up and that snapshots stay local', async ({ page }) => {
    const res = await page.goto('/settings');
    expect(res?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible();
    await expect(page.getByTestId('backup-never')).toBeVisible();
    await expect(page.getByTestId('backup-destination')).toContainText('Local only');
    await expect(page.getByTestId('backup-error')).toHaveCount(0);
    expect((await health()).backup).toBe('never');
  });

  test('back up now succeeds without a full reload and shows the time', async ({ page }) => {
    await page.goto('/settings');
    await page.evaluate(() => ((window as unknown as { marker: string }).marker = 'same-document'));
    const done = page.waitForResponse((r) => r.url().endsWith('/settings/backup') && r.request().method() === 'POST');
    await page.getByRole('button', { name: 'Back up now' }).click();
    expect((await done).status()).toBe(200);
    await expect(page.getByTestId('backup-outcome')).toHaveText('Backup finished.');
    await expect(page.getByTestId('backup-last-success')).toBeVisible();
    await expect(page.getByTestId('backup-never')).toHaveCount(0);
    expect(await page.evaluate(() => (window as unknown as { marker: string }).marker)).toBe('same-document');

    const details = await health();
    expect(details.backup).toBe('ok');
    expect(details.backup_last_success).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/);
    // The page and the health check agree after a reload.
    await page.reload();
    await expect(page.getByTestId('backup-last-success')).toBeVisible();
  });
});

test.describe('failing upload', () => {
  test.beforeAll(() =>
    start({
      // Nothing listens on port 1, so every upload is refused.
      GOBBLER_S3_ENDPOINT: 'http://127.0.0.1:1',
      GOBBLER_S3_REGION: 'us-east-1',
      // The bucket and prefix are chosen to equal the credentials so the
      // failed upload's error text (which names both) contains them: only the
      // page's redaction keeps them out of the response.
      GOBBLER_S3_BUCKET: 'e2e-secret-key',
      GOBBLER_S3_PREFIX: 'e2e-key-id',
      GOBBLER_S3_ACCESS_KEY_ID: 'e2e-key-id',
      GOBBLER_S3_SECRET_ACCESS_KEY: 'e2e-secret-key',
    }),
  );
  test.afterAll(stop);

  test('shows that snapshots are uploaded, with no credentials', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByTestId('backup-destination')).toContainText('uploaded to S3');
    const html = await page.content();
    for (const secret of ['e2e-key-id', 'e2e-secret-key']) {
      expect(html).not.toContain(secret);
    }
  });

  test('back up now reports the failure and the health check says only failed', async ({ page }) => {
    await page.goto('/settings');
    await page.getByRole('button', { name: 'Back up now' }).click();
    await expect(page.getByTestId('backup-outcome')).toHaveText('Backup failed.', { timeout: 60_000 });
    await expect(page.getByTestId('backup-error')).toContainText('Last backup failed');
    const html = await page.content();
    for (const secret of ['e2e-key-id', 'e2e-secret-key']) {
      expect(html).not.toContain(secret);
    }
    await expect(page.getByTestId('backup-error')).toContainText('[redacted]');

    const res = await fetch(`${origin}/healthz`);
    const text = await res.text();
    expect((JSON.parse(text).details as Record<string, string>).backup).toBe('failed');
    for (const leak of ['127.0.0.1:1', 'e2e-secret-key', 'e2e-key-id', 'upload', 'backups']) {
      expect(text).not.toContain(leak);
    }
  });
});
