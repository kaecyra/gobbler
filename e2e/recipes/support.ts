import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, type Locator } from '@playwright/test';
import { portFromEnv } from '../fixtures/ports.mjs';

// The library and the home page need recipes, which the shared server of the
// suite does not have. A spec that uses this starts its own gobbler on a
// throwaway data directory in dev-auth mode, checks the empty library first,
// then seeds recipes through the real store (seed/main.go) while the server
// runs.
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

export async function seededServer(portVar: string, prefix: string) {
  const bin = process.env.GOBBLER_E2E_BIN;
  if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');
  const port = await portFromEnv(process.env, portVar);
  const origin = `http://127.0.0.1:${port}`;
  let dataDir = '';
  let server: ChildProcess | undefined;

  return {
    origin,
    async start() {
      const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
      if (stale) throw new Error(`something already listens on ${origin}; set ${portVar}`);
      dataDir = mkdtempSync(join(tmpdir(), prefix));
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
        if (Date.now() > deadline) throw new Error('the server did not start');
        await new Promise((r) => setTimeout(r, 100));
      }
    },
    async stop() {
      if (server && server.exitCode === null) {
        const exited = new Promise((resolve) => server!.once('exit', resolve));
        server.kill('SIGTERM');
        await exited;
      }
      if (dataDir) rmSync(dataDir, { recursive: true, force: true });
    },
    /** Adds the fixture recipes; returns their ids by name. */
    seed(): Record<string, number> {
      const out = execFileSync('go', ['run', './e2e/recipes/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
      return JSON.parse(out);
    },
  };
}

/** Every control of a class is at least --tap (44px) tall (docs/ui/design.md). */
export async function expectTappable(controls: Locator) {
  expect(await controls.count(), 'no controls to measure').toBeGreaterThan(0);
  for (const control of await controls.all()) {
    const box = await control.boundingBox();
    expect(box, 'control has no box').not.toBeNull();
    expect(box!.height).toBeGreaterThanOrEqual(44);
  }
}
