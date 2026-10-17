// Starts the gobbler binary for the Playwright suite on loopback with data in a
// fresh temp directory removed on exit. Run by the webServer entries in
// playwright.config.ts; never with real credentials.
//
//   serve.mjs            dev-auth bypass on (a request is the dev identity unless it
//                        sends X-Gobbler-Dev-Identity; the dev households are seeded)
//   serve.mjs --access   Access verification ON with a fake team domain and AUD,
//                        so a request without a valid token is refused (ADR-00003)
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { startThumbnailHost, THUMBNAIL_HOST_ENV } from './thumbnails.mjs';

const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) {
  console.error('GOBBLER_E2E_BIN is not set; run the suite through `make e2e`, which builds the binary.');
  process.exit(1);
}
const access = process.argv.includes('--access');
const port = access ? process.env.GOBBLER_E2E_ACCESS_PORT : process.env.GOBBLER_E2E_PORT;
if (!port) {
  console.error('the listen port is not set; run the suite through playwright.config.ts');
  process.exit(1);
}
// Video thumbnails come from a local fixture, never from i.ytimg.com.
const thumbnails = await startThumbnailHost();
const dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-'));

// Only what the server needs plus the fixed settings: nothing from the
// developer's environment (API keys, real Access settings) reaches the binary.
const env = {
  PATH: process.env.PATH ?? '',
  GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
  GOBBLER_DATA_DIR: dataDir,
  GOBBLER_LOG_LEVEL: 'warn',
  [THUMBNAIL_HOST_ENV]: thumbnails.origin,
  // The operators of the e2e deployment: one who is a member of no household
  // and the default dev identity, so suites that open /settings as dev (the
  // layout nav) keep working. Keep them in step with OPERATOR and DEV in
  // identity.ts. OTHER is a member who is not an operator.
  GOBBLER_OPERATOR_EMAILS: 'operator@e2e.example,dev@localhost',
  ...(access
    ? { GOBBLER_ACCESS_TEAM_DOMAIN: 'e2e.invalid', GOBBLER_ACCESS_AUD: 'e2e-fake-aud' }
    : { GOBBLER_DEV_AUTH_BYPASS: 'true' }),
};

const child = spawn(bin, ['serve'], { env, stdio: 'inherit' });

let stopping = false;
let cleaned = false;
function cleanup() {
  if (cleaned) return;
  cleaned = true;
  rmSync(dataDir, { recursive: true, force: true });
  void thumbnails.close();
}

for (const sig of ['SIGTERM', 'SIGINT']) {
  process.on(sig, () => {
    stopping = true;
    child.kill(sig);
  });
}
child.on('error', (err) => {
  console.error(`start ${bin}: ${err.message}`);
  cleanup();
  process.exit(1);
});
child.on('exit', (code, signal) => {
  cleanup();
  if (signal && !stopping) {
    console.error(`${bin} died from signal ${signal}`);
    process.exit(1);
  }
  process.exit(code ?? 0);
});
