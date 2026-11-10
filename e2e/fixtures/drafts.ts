import { test as base } from '@playwright/test';

// The specs on the shared server share one household, and a household holds at
// most 20 waiting drafts (store.MaxPendingDrafts). A test that reads a recipe
// and stops leaves its draft waiting, and once the suite had left 19 the next
// test to read one in CI was refused with 409 (#596). This fixture discards,
// after each test, the drafts that test created: those named by a 303 to
// /ingest/<token> that answered one of its POSTs under /ingest. A confirm or a
// discard can open a draft another test made (ADR-00049), but that redirect
// carries ?done=, so the draft is never taken from the test still using it.
export const test = base.extend<{ discardDrafts: void }>({
  discardDrafts: [
    async ({ context }, use) => {
      const made = new Set<string>();
      context.on('response', (res) => {
        if (res.request().method() !== 'POST' || res.status() !== 303) return;
        if (!new URL(res.url()).pathname.startsWith('/ingest')) return;
        const to = res.headers()['location'];
        const token = to && /^\/ingest\/([0-9a-f]+)$/.exec(to)?.[1];
        if (token) made.add(token);
      });
      await use();
      for (const token of made) {
        // A confirmed or discarded draft is gone; cancelling it again answers
        // 303 as well, so every answer but a redirect is a failure.
        const res = await context.request.post(`/ingest/${token}/cancel`, { maxRedirects: 0 });
        if (res.status() !== 303) throw new Error(`discard draft ${token}: ${res.status()}`);
      }
    },
    { auto: true },
  ],
});

export { expect } from '@playwright/test';
