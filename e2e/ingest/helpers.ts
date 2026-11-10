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
// draft that was saved (ADR-00049). On a shared server that draft is another
// spec's, and its teardown may discard it before it is read, leaving a 404 with
// no message to follow. The saved recipe is therefore read from the import
// page, which carries the same message and depends on no other draft. Every
// confirm that expects the recipe page goes through here.
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
