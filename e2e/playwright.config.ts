import { defineConfig } from '@playwright/test';
import { portsFromEnv } from './fixtures/ports.mjs';

// The suite runs against a real gobbler binary in dev-auth mode on loopback
// with a throwaway database (ADR-00013, ADR-00003). `make e2e` builds the
// binary and passes its path in GOBBLER_E2E_BIN.
// Free ports from the kernel, not fixed ones in its ephemeral range (#257).
// Written back to the environment so a worker that loads this file again, and
// the specs, see the same ports.
const [port, accessPort] = await portsFromEnv(process.env, ['GOBBLER_E2E_PORT', 'GOBBLER_E2E_ACCESS_PORT']);
process.env.GOBBLER_E2E_PORT = String(port);
process.env.GOBBLER_E2E_ACCESS_PORT = String(accessPort);
const baseURL = `http://127.0.0.1:${port}`;
const accessURL = `http://127.0.0.1:${accessPort}`;
// Read by specs that talk to the Access-on instance.
process.env.GOBBLER_E2E_ACCESS_URL = accessURL;
const serverEnv = { GOBBLER_E2E_BIN: process.env.GOBBLER_E2E_BIN ?? '' };

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
  webServer: [
    {
      // Dev-auth bypass on: every request carries the dev identity.
      command: 'node fixtures/serve.mjs',
      // Ready when the unauthenticated health route answers.
      url: `${baseURL}/healthz`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 5_000 },
      env: { ...serverEnv, GOBBLER_E2E_PORT: String(port) },
    },
    {
      // Access verification on with a fake team domain and AUD: the real
      // ADR-00003 behaviour for requests without a token.
      command: 'node fixtures/serve.mjs --access',
      url: `${accessURL}/healthz`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 5_000 },
      env: { ...serverEnv, GOBBLER_E2E_ACCESS_PORT: String(accessPort) },
    },
  ],
});
