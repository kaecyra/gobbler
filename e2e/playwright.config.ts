import { defineConfig } from '@playwright/test';
import { portsFromEnv } from './fixtures/ports.mjs';

// The suite runs against a real gobbler binary in dev-auth mode on loopback
// with a throwaway database (ADR-00013, ADR-00003); a second instance runs
// without the bypass and a third with sign-in (ADR-00030). `make e2e` builds the
// binary and passes its path in GOBBLER_E2E_BIN.
// Free ports from the kernel, not fixed ones in its ephemeral range (#257).
// Written back to the environment so a worker that loads this file again, and
// the specs, see the same ports.
const [port, anonymousPort, signinPort] = await portsFromEnv(process.env, ['GOBBLER_E2E_PORT', 'GOBBLER_E2E_ANONYMOUS_PORT', 'GOBBLER_E2E_SIGNIN_PORT']);
process.env.GOBBLER_E2E_PORT = String(port);
process.env.GOBBLER_E2E_ANONYMOUS_PORT = String(anonymousPort);
process.env.GOBBLER_E2E_SIGNIN_PORT = String(signinPort);
const baseURL = `http://127.0.0.1:${port}`;
const anonymousURL = `http://127.0.0.1:${anonymousPort}`;
const signinURL = `http://127.0.0.1:${signinPort}`;
// Read by specs that talk to the anonymous and sign-in instances.
process.env.GOBBLER_E2E_ANONYMOUS_URL = anonymousURL;
process.env.GOBBLER_E2E_SIGNIN_URL = signinURL;
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
      // No dev bypass, as in production: the real ADR-00030 behaviour for
      // requests without a session.
      command: 'node fixtures/serve.mjs --anonymous',
      url: `${anonymousURL}/healthz`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 5_000 },
      env: { ...serverEnv, GOBBLER_E2E_ANONYMOUS_PORT: String(anonymousPort) },
    },
    {
      // Sign-in configured against a local fake issuer (ADR-00030), with the
      // dev bypass still deciding identity.
      command: 'node fixtures/serve.mjs --signin',
      url: `${signinURL}/healthz`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 5_000 },
      env: { ...serverEnv, GOBBLER_E2E_SIGNIN_PORT: String(signinPort) },
    },
  ],
});
