import type { Page } from '@playwright/test';
import { deflateSync } from 'node:zlib';

// The add page shows one source field at a time (docs/ui/design.md): choose
// the paste toggle, then fill the text.
export async function pasteText(page: Page, text: string): Promise<void> {
  await page.locator('label[for=src-text]').click();
  await page.getByLabel('Recipe text').fill(text);
}

// After a draft is confirmed: the saved recipe's page. When other drafts are
// waiting, a confirm opens the oldest of them instead, with ?done= naming the
// draft that was saved (ADR-00049). On a shared server the draft opened is
// often another spec's, and its teardown may discard it before the page loads,
// leaving a 404 with no message to follow. The saved recipe is therefore read
// from the import page, which carries the same message and depends on no other
// draft. Every confirm that expects the recipe page goes through here.
export async function openSaved(page: Page): Promise<void> {
  await page.waitForURL(
    (u) => /^\/recipes\/\d+/.test(u.pathname) || (/^\/ingest\/[0-9a-f]+$/.test(u.pathname) && u.searchParams.has('done')),
  );
  const landed = new URL(page.url());
  if (landed.pathname.startsWith('/ingest/')) {
    await page.goto(`/ingest?done=${landed.searchParams.get('done')}`);
    await page.getByTestId('draft-done').getByRole('link').click();
    await page.waitForURL(/\/recipes\/\d+/);
  }
}

// After a draft is discarded with no other waiting, the import page; with
// others waiting, the oldest of them. Either says what was discarded.
export const discardedURL = /\/ingest(\/[0-9a-f]+)?\?done=[0-9a-f]+$/;

// width x height RGB PNG, banded so the thumbnail is not a flat colour. 1200 by
// 400 clears the candidate minimum of 1066 by 300; the stored candidate is
// scaled to 1024 wide, which is the size the picker shows.
export function png(width: number, height: number, rgb: [number, number, number]): Buffer {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const crc = (b: Buffer) => {
    let c = 0xffffffff;
    for (const x of b) c = crcTable[(c ^ x) & 0xff]! ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  };
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type), data]);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc(body));
    return Buffer.concat([len, body, sum]);
  };
  const head = Buffer.alloc(13);
  head.writeUInt32BE(width, 0);
  head.writeUInt32BE(height, 4);
  head[8] = 8; // bit depth
  head[9] = 2; // RGB
  const raw = Buffer.alloc((width * 3 + 1) * height);
  for (let y = 0; y < height; y++) {
    const row = y * (width * 3 + 1);
    const shade = 0.55 + 0.45 * (Math.floor(y / 40) % 2);
    for (let x = 0; x < width; x++) {
      for (let i = 0; i < 3; i++) raw[row + 1 + x * 3 + i] = Math.round(rgb[i]! * shade);
    }
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', head),
    chunk('IDAT', deflateSync(raw)),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

// The LLM settings live on the Deployment page, not in the environment
// (ADR-00056, ADR-00057). A spec that runs its own gobbler against a fake
// Messages API starts it with ENCRYPTION_KEY and GOBBLER_OPERATOR_EMAILS set to
// DEV_OPERATOR, answers MODELS_LISTING on GET /v1/models, and then calls
// configureClaude, which saves the settings the way an operator does.
export const ENCRYPTION_KEY = Buffer.alloc(32, 7).toString('base64');
export const DEV_OPERATOR = 'dev@localhost';

const listed = (id: string, name: string) => ({
  type: 'model',
  id,
  display_name: name,
  lifecycle: 'active',
  capabilities: { structured_outputs: { supported: true }, image_input: { supported: true } },
});

// The models the fake lists: the text and vision models the specs save.
export const MODELS_LISTING = JSON.stringify({
  data: [listed('e2e-text', 'E2E Text'), listed('e2e-vision', 'E2E Vision')],
  has_more: false,
  first_id: 'e2e-text',
  last_id: 'e2e-vision',
});

async function postForm(origin: string, path: string, form: Record<string, string>): Promise<void> {
  const res = await fetch(`${origin}${path}`, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded', 'sec-fetch-site': 'same-origin' },
    body: new URLSearchParams(form),
    redirect: 'manual',
  });
  // A save is a 303 back to the page; a refusal is a 422 with the reason.
  if (res.status !== 303) throw new Error(`POST ${path}: ${res.status} ${(await res.text()).slice(0, 300)}`);
}

// Saves Claude for both tasks on the Deployment page: the key, then the
// providers, then the models, which the page offers only once it can list them.
export async function configureClaude(origin: string, timeout = '5s'): Promise<void> {
  await postForm(origin, '/operator/settings/secrets/llm/anthropic_key', { value: 'e2e-fake-key' });
  const providers = { text_provider: 'claude', vision_provider: 'claude', timeout };
  await postForm(origin, '/operator/settings/values/llm', providers);
  await postForm(origin, '/operator/settings/values/llm', { ...providers, text_model: 'e2e-text', vision_model: 'e2e-vision' });
}
