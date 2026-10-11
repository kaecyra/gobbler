import { defineConfig } from '@playwright/test';

// The suite runs against a real gobbler binary in dev-auth mode on loopback
// with a throwaway database (ADR-00013, ADR-00003). `make e2e` builds the
// binary and passes its path in GOBBLER_E2E_BIN.
const port = Number(process.env.GOBBLER_E2E_PORT ?? 38417);
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    command: 'node fixtures/serve.mjs',
    // Ready when the unauthenticated health route answers.
    url: `${baseURL}/healthz`,
    reuseExistingServer: false,
    timeout: 30_000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5_000 },
    env: {
      GOBBLER_E2E_PORT: String(port),
      GOBBLER_E2E_BIN: process.env.GOBBLER_E2E_BIN ?? '',
    },
  },
});
