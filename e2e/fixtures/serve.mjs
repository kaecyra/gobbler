// Starts the gobbler binary for the Playwright suite: dev-auth bypass on a
// loopback address, data in a fresh temp directory removed on exit. Run by the
// webServer entry in playwright.config.ts; never with real credentials.
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) {
  console.error('GOBBLER_E2E_BIN is not set; run the suite through `make e2e`, which builds the binary.');
  process.exit(1);
}
const port = process.env.GOBBLER_E2E_PORT ?? '38417';
const dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-'));

// Only what the server needs, plus the fixed bypass settings: nothing from the
// developer's environment (API keys, Access settings) reaches the binary.
const env = {
  PATH: process.env.PATH ?? '',
  GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
  GOBBLER_DATA_DIR: dataDir,
  GOBBLER_DEV_AUTH_BYPASS: 'true',
  GOBBLER_LOG_LEVEL: 'warn',
};

const child = spawn(bin, ['serve'], { env, stdio: 'inherit' });

let cleaned = false;
function cleanup() {
  if (cleaned) return;
  cleaned = true;
  rmSync(dataDir, { recursive: true, force: true });
}

for (const sig of ['SIGTERM', 'SIGINT']) {
  process.on(sig, () => child.kill(sig));
}
child.on('error', (err) => {
  console.error(`start ${bin}: ${err.message}`);
  cleanup();
  process.exit(1);
});
child.on('exit', (code, signal) => {
  cleanup();
  process.exit(code ?? (signal ? 0 : 1));
});
