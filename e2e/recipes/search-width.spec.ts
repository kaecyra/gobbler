import { test, expect } from '@playwright/test';
import { seededServer } from './support';

const srv = await seededServer('GOBBLER_E2E_SEARCHWIDTH_PORT', 'gobbler-e2e-searchwidth-');

test.use({ baseURL: srv.origin });

test.beforeAll(srv.start);
test.afterAll(srv.stop);

// The library's search box spans the row of results below it, not some
// width in between (the old 640px cap).
for (const width of [1280, 390]) {
  test(`the library search box is as wide as the library at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/recipes');
    const search = await page.locator('label.search').boundingBox();
    const lib = await page.locator('.lib').boundingBox();
    expect(search, 'no search box').not.toBeNull();
    expect(lib, 'no library row').not.toBeNull();
    expect(Math.abs(search!.width - lib!.width)).toBeLessThanOrEqual(1);
  });
}
