import type { Locator, Page } from '@playwright/test';

// dragRow drags a row by its handle onto a target and releases at frac of the
// target's height: another row, or a whole part's box. A row that leaves one
// part moves everything below it up, so the target is measured again at the end,
// with the row already previewed in its new place.
export async function dragRow(page: Page, from: Locator, to: Locator, frac: number) {
  await to.scrollIntoViewIfNeeded();
  const grip = (await from.locator('.grip').first().boundingBox())!;
  const x = grip.x + grip.width / 2;
  const y0 = grip.y + grip.height / 2;
  // The element itself, not the locator: a locator such as nth(1) names whatever
  // is second once the row has moved.
  const target = (await to.elementHandle())!;
  const t = (await target.boundingBox())!;
  const y1 = t.y + t.height * frac;
  await page.mouse.move(x, y0);
  await page.mouse.down();
  for (let i = 1; i <= 10; i++) await page.mouse.move(x, y0 + ((y1 - y0) * i) / 10);
  for (let i = 0; i < 3; i++) {
    const live = (await target.boundingBox())!;
    await page.mouse.move(x, live.y + live.height * frac);
  }
  await page.mouse.up();
}

// watchScroll samples the scroll position on every frame for ms, so a jump that
// is scrolled back afterwards still fails (ADR-00038).
export async function watchScroll(page: Page, ms: number) {
  await page.evaluate((limit) => {
    const w = window as unknown as { ys: number[] };
    w.ys = [];
    const t0 = performance.now();
    const f = () => { w.ys.push(window.scrollY); if (performance.now() - t0 < limit) requestAnimationFrame(f); };
    requestAnimationFrame(f);
  }, ms);
}

// scrollDrift is the furthest the page moved from before while watchScroll ran.
export async function scrollDrift(page: Page, before: number): Promise<number> {
  const ys = await page.evaluate(() => (window as unknown as { ys: number[] }).ys);
  return Math.max(...ys.map((y) => Math.abs(y - before)));
}
