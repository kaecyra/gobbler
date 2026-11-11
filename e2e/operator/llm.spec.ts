import { test, expect, type Page } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { portFromEnv } from '../fixtures/ports.mjs';
import { DEV_OPERATOR, ENCRYPTION_KEY, NO_OPENROUTER, pasteText } from '../ingest/helpers';
import { expectTapTargets } from '../fixtures/tap';

// The LLM section of the Deployment page (ADR-00056, ADR-00057). This spec
// starts its own gobbler with an encryption key, and points the Claude and
// Gemini SDKs at local fakes of both APIs through their base-URL variables, so
// no real API is called and no real key is used. Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_LLM_PORT');
const origin = `http://127.0.0.1:${port}`;

const CLAUDE_KEY = 'sk-ant-e2e-distinct-key';
const GEMINI_KEY = 'AIza-e2e-distinct-key';

const claudeModel = (id: string, name: string, lifecycle: string, structured: boolean, image: boolean) => ({
  type: 'model',
  id,
  display_name: name,
  lifecycle,
  capabilities: { structured_outputs: { supported: structured }, image_input: { supported: image } },
});

// A recorded-shape Models API page: only the first two pass the text task, and
// only the first passes the vision task.
const CLAUDE_MODELS = JSON.stringify({
  data: [
    claudeModel('e2e-claude-large', 'E2E Claude Large', 'active', true, true),
    claudeModel('e2e-claude-text', 'E2E Claude Text', 'active', true, false),
    claudeModel('e2e-claude-old', 'E2E Claude Old', 'deprecated', true, true),
    claudeModel('e2e-claude-plain', 'E2E Claude Plain', 'active', false, true),
  ],
  has_more: false,
  first_id: 'e2e-claude-large',
  last_id: 'e2e-claude-plain',
});

const GEMINI_MODELS = JSON.stringify({
  models: [
    { name: 'models/e2e-gemini-flash', displayName: 'E2E Gemini Flash', supportedGenerationMethods: ['generateContent'] },
    { name: 'models/e2e-gemini-embed', displayName: 'E2E Gemini Embed', supportedGenerationMethods: ['embedContent'] },
  ],
});

// The gap-fill answer for MESSY: one ingredient entry for its single weak line.
const ANSWER = { name: 'Mystery Stew', components: [{ ingredients: ['1 tsp dragonfruit zest'], steps: ['Stir.'] }] };

const MESSY = `Mystery Stew

Ingredients:
- ?? a good amount of glorp, roughly #@! to taste-ish

Instructions:
Stir it all together and simmer until it looks right.
`;

let listingFails = false;
let claudeCalls = 0;
let geminiCalls = 0;
let claudeKeySeen = '';
let geminiKeySeen = '';
let fakeClaude: Server;
let fakeGemini: Server;
let server: ChildProcess | undefined;
let dataDir = '';

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

const listen = (s: Server) => new Promise<string>((resolve) => s.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(s.address() as AddressInfo).port}`)));

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_LLM_PORT`);

  fakeClaude = createServer((req, res) => {
    req.resume();
    req.on('end', () => {
      claudeKeySeen = String(req.headers['x-api-key'] ?? '');
      if (req.method === 'GET' && req.url?.startsWith('/v1/models')) {
        if (listingFails) {
          res.writeHead(503, { 'content-type': 'application/json' });
          res.end('{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}');
          return;
        }
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(CLAUDE_MODELS);
        return;
      }
      claudeCalls++;
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(
        JSON.stringify({
          id: 'msg_e2e',
          type: 'message',
          role: 'assistant',
          model: 'e2e',
          content: [{ type: 'tool_use', id: 'toolu_e2e', name: 'record_recipe', input: ANSWER }],
          stop_reason: 'tool_use',
          stop_sequence: null,
          usage: { input_tokens: 1, output_tokens: 1 },
        }),
      );
    });
  });
  fakeGemini = createServer((req, res) => {
    req.resume();
    req.on('end', () => {
      geminiKeySeen = String(req.headers['x-goog-api-key'] ?? '');
      if (req.method === 'GET' && req.url?.includes('/models')) {
        res.writeHead(listingFails ? 503 : 200, { 'content-type': 'application/json' });
        res.end(listingFails ? '{"error":{"code":503,"message":"overloaded","status":"UNAVAILABLE"}}' : GEMINI_MODELS);
        return;
      }
      geminiCalls++;
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(
        JSON.stringify({
          candidates: [{ content: { role: 'model', parts: [{ text: JSON.stringify(ANSWER) }] }, finishReason: 'STOP' }],
          usageMetadata: { promptTokenCount: 1, candidatesTokenCount: 1 },
        }),
      );
    });
  });
  const claudeURL = await listen(fakeClaude);
  const geminiURL = await listen(fakeGemini);

  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-llm-'));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_ENCRYPTION_KEY: ENCRYPTION_KEY,
      GOBBLER_OPERATOR_EMAILS: DEV_OPERATOR,
      GOBBLER_OPENROUTER_URL: NO_OPENROUTER,
      ANTHROPIC_BASE_URL: claudeURL,
      GOOGLE_GEMINI_BASE_URL: geminiURL,
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
    if (Date.now() > deadline) throw new Error('the LLM server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
  await new Promise<void>((resolve) => fakeClaude?.close(() => resolve()));
  await new Promise<void>((resolve) => fakeGemini?.close(() => resolve()));
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

const llm = (page: Page) => page.getByTestId('integration-llm');
// Clicks Save and waits for the answer, so a reload after it cannot cancel the post.
async function save(page: Page) {
  const answered = page.waitForResponse((r) => r.url().endsWith('/operator/settings/values/llm') && r.request().method() === 'POST');
  await llm(page).locator('form[data-testid="values-llm"]').getByRole('button', { name: 'Save' }).click();
  await answered;
}
const optionValues = (page: Page, field: string) =>
  llm(page).getByTestId(`value-${field}`).locator('option').evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));

// The save swaps the section in place, so poll until the options are the new ones.
const expectOptions = (page: Page, field: string, want: string[]) =>
  expect(async () => expect(await optionValues(page, field)).toEqual(want)).toPass();

// Types a key into its field; the section's Save sends it with the rest.
const typeKey = (page: Page, field: string, value: string) =>
  llm(page).getByTestId(`secret-llm-${field}`).getByTestId('secret-input').fill(value);

test('the LLM section lists a provider and a model per task, a timeout and both keys', async ({ page }) => {
  const res = await page.goto('/operator/settings');
  expect(res?.status()).toBe(200);
  await expect(llm(page).getByRole('heading', { name: 'LLM' })).toBeVisible();
  for (const f of ['text_provider', 'text_model', 'vision_provider', 'vision_model']) {
    await expect(llm(page).getByTestId(`value-${f}`)).toBeVisible();
  }
  await expect(llm(page).getByTestId('value-text_provider')).toHaveValue('');
  await expect(llm(page).getByTestId('value-timeout')).toHaveValue('');
  await expect(llm(page).getByTestId('secret-llm-anthropic_key').getByTestId('secret-state')).toHaveText('Not set');
  await expect(llm(page).getByTestId('secret-llm-gemini_key').getByTestId('secret-state')).toHaveText('Not set');
});

test('a model is chosen from the provider\'s narrowed listing, per task, and never typed', async ({ page }) => {
  await page.goto('/operator/settings');
  // The key and the providers go in one save; the page then lists the models.
  await typeKey(page, 'anthropic_key', CLAUDE_KEY);
  await llm(page).getByTestId('value-text_provider').selectOption('claude');
  await llm(page).getByTestId('value-vision_provider').selectOption('claude');
  await save(page);
  await expect(page.getByTestId('integrations-problem')).toHaveCount(0);
  await expect(llm(page).getByTestId('secret-llm-anthropic_key').getByTestId('secret-state')).toHaveText('Saved');

  // Active models with structured output for text; also image input for vision.
  await expectOptions(page, 'text_model', ['', 'e2e-claude-large', 'e2e-claude-text']);
  await expectOptions(page, 'vision_model', ['', 'e2e-claude-large']);
  await expect(llm(page).locator('input[name="text_model"], input[name="vision_model"]')).toHaveCount(0);

  await llm(page).getByTestId('value-text_model').selectOption('e2e-claude-text');
  await llm(page).getByTestId('value-vision_model').selectOption('e2e-claude-large');
  await save(page);
  await page.reload();
  await expect(llm(page).getByTestId('value-text_model')).toHaveValue('e2e-claude-text');
  await expect(llm(page).getByTestId('value-vision_model')).toHaveValue('e2e-claude-large');
  expect(claudeKeySeen).toBe(CLAUDE_KEY);
  // The key shows only that it is saved, and a save that left it blank kept it.
  await expect(llm(page).getByTestId('secret-llm-anthropic_key').getByTestId('secret-state')).toHaveText('Saved');
  await expect(llm(page).getByTestId('secret-llm-anthropic_key').getByTestId('secret-input')).toHaveValue('');
  await expect(page.locator('body')).not.toContainText(CLAUDE_KEY);
});

test('a timeout above the largest allowed is refused and nothing is saved', async ({ page }) => {
  await page.goto('/operator/settings');
  await llm(page).getByTestId('value-timeout').fill('10m');
  await save(page);
  await expect(page.getByTestId('integrations-problem')).toContainText('Timeout must be a duration such as 60s, up to 2m0s');
  await page.reload();
  await expect(llm(page).getByTestId('value-timeout')).toHaveValue('');
});

test('a setting saved on the page is used by the next ingest, with no restart', async ({ page }) => {
  // Text is on Claude (saved above): a below-threshold paste calls Claude only.
  claudeCalls = 0;
  geminiCalls = 0;
  await page.goto('/ingest');
  await pasteText(page, MESSY);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  expect([claudeCalls, geminiCalls]).toEqual([1, 0]);

  // Move the text task to Gemini; the very next ingest reaches Gemini.
  await page.goto('/operator/settings');
  await typeKey(page, 'gemini_key', GEMINI_KEY);
  await llm(page).getByTestId('value-text_provider').selectOption('gemini');
  await save(page);
  await expectOptions(page, 'text_model', ['', 'e2e-gemini-flash']);
  await llm(page).getByTestId('value-text_model').selectOption('e2e-gemini-flash');
  await save(page);
  await expect(llm(page).getByTestId('value-text_model')).toHaveValue('e2e-gemini-flash');
  expect(geminiKeySeen).toBe(GEMINI_KEY);

  claudeCalls = 0;
  geminiCalls = 0;
  await page.goto('/ingest');
  await pasteText(page, MESSY);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  expect([claudeCalls, geminiCalls]).toEqual([0, 1]);
});

test('changing a task\'s provider clears its model', async ({ page }) => {
  await page.goto('/operator/settings');
  await llm(page).getByTestId('value-vision_provider').selectOption('gemini');
  await save(page);
  await expect(llm(page).getByTestId('value-vision_provider')).toHaveValue('gemini');
  await expect(llm(page).getByTestId('value-vision_model')).toHaveValue('');
  // The text task is untouched by the vision change.
  await expect(llm(page).getByTestId('value-text_model')).toHaveValue('e2e-gemini-flash');
});

test('a listing that cannot be fetched is explained and leaves the saved model alone', async ({ page }) => {
  listingFails = true;
  try {
    await page.goto('/operator/settings');
    await expect(llm(page).getByTestId('problem-text_model')).toContainText('The models could not be listed');
    await expect(llm(page).getByTestId('saved-text_model')).toHaveText('e2e-gemini-flash');
    await expect(llm(page).locator('select[name="text_model"]')).toHaveCount(0);
    // Saving the rest of the form posts no model, and must not clear it.
    await llm(page).getByTestId('value-timeout').fill('45s');
    await save(page);
    await page.reload();
    await expect(llm(page).getByTestId('value-timeout')).toHaveValue('45s');
    await expect(llm(page).getByTestId('saved-text_model')).toHaveText('e2e-gemini-flash');
  } finally {
    listingFails = false;
  }
  // The listing is back: the model that was never cleared is the selected one.
  await page.reload();
  await expect(llm(page).getByTestId('value-text_model')).toHaveValue('e2e-gemini-flash');
});

test('a saved key is cleared from its label line, and the other saved settings stay', async ({ page }) => {
  await page.goto('/operator/settings');
  const key = llm(page).getByTestId('secret-llm-anthropic_key');
  await expect(key.getByTestId('secret-state')).toHaveText('Saved');
  // Clear sits on the label line, a full tap target although it reads as a link.
  await expectTapTargets(page);
  await key.getByRole('button', { name: 'Clear Claude API key' }).click();
  await expect(llm(page).getByTestId('secret-llm-anthropic_key').getByTestId('secret-state')).toHaveText('Not set');
  await expect(llm(page).getByTestId('secret-llm-anthropic_key').getByRole('button', { name: /Clear/ })).toHaveCount(0);
  await expect(llm(page).getByTestId('secret-llm-gemini_key').getByTestId('secret-state')).toHaveText('Saved');
  await expect(llm(page).getByTestId('value-timeout')).toHaveValue('45s');
});

test('the values route answers 404 for an integration nothing registered', async ({ request }) => {
  const res = await request.post('/operator/settings/values/nope', { form: { x: 'y' }, headers: { 'Sec-Fetch-Site': 'same-origin' }, maxRedirects: 0 });
  expect(res.status()).toBe(404);
});
