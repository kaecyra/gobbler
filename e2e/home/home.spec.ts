import { test, expect } from '@playwright/test';
import { expectTappable, seededServer } from '../recipes/support';

const srv = await seededServer('GOBBLER_E2E_HOME_PORT', 'gobbler-e2e-home-');

test.use({ baseURL: srv.origin });
// One server per file; the empty-library test must run before the seeding.
test.describe.configure({ mode: 'serial' });

test.beforeAll(srv.start);
test.afterAll(srv.stop);

const searchbox = (page: import('@playwright/test').Page) => page.getByRole('searchbox', { name: 'Search recipes' });

test('an empty library shows the hero and an empty state', async ({ page }) => {
  const res = await page.goto('/');
  expect(res?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'What are we making?', level: 1 })).toBeVisible();
  await expect(page.getByTestId('empty-library')).toBeVisible();
  await expect(page.locator('#suggest')).toHaveCount(0);
  await expect(page.getByTestId('recently-made')).toContainText('Nothing made yet');
});

test.describe('with recipes', () => {
  let ids: Record<string, number> = {};

  test.beforeAll(() => {
    ids = srv.seed();
  });

  test('the hero search is big, centred and focused with "/"', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'What are we making?', level: 1 })).toBeVisible();
    const box = (await searchbox(page).boundingBox())!;
    expect(box.width).toBeGreaterThan(400);
    const viewport = page.viewportSize()!;
    // Centred: the same room on both sides of the box, within a few pixels.
    const label = (await page.locator('label.search').boundingBox())!;
    expect(Math.abs(label.x - (viewport.width - label.x - label.width))).toBeLessThan(24);
    await expect(searchbox(page)).not.toBeFocused();
    await page.keyboard.press('/');
    await expect(searchbox(page)).toBeFocused();
  });

  test('typing moves to /recipes?q= and Back returns home', async ({ page }) => {
    await page.goto('/');
    await searchbox(page).click();
    await searchbox(page).pressSequentially('lemon', { delay: 40 });
    await expect(page).toHaveURL(/\/recipes\?q=lemon$/);
    await expect(page.locator('.rcard')).toHaveCount(1);
    await expect(page.locator('.rcard')).toContainText('Lemon Cake');
    // The box was kept through the swap: still focused, nothing lost.
    await expect(searchbox(page)).toBeFocused();
    await expect(searchbox(page)).toHaveValue('lemon');
    // The page title and header follow the library page.
    await expect(page).toHaveTitle(/Recipes/);

    await page.goBack();
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole('heading', { name: 'What are we making?', level: 1 })).toBeVisible();
    await expect(page.locator('#suggest')).toBeVisible();
    await expect(page).toHaveTitle(/Home/);
  });

  test('submitting the hero search goes to the library', async ({ page }) => {
    await page.goto('/');
    await searchbox(page).fill('stew');
    await searchbox(page).press('Enter');
    await expect(page).toHaveURL(/\/recipes\?q=stew/);
    await expect(page.locator('.rcard')).toContainText('Beef Stew');
  });

  test('the suggestion is the recipe made longest ago, and "Another idea" cycles', async ({ page }) => {
    await page.goto('/');
    const suggest = page.locator('#suggest');
    await expect(suggest.getByRole('heading', { level: 2 })).toHaveText('Lemon Cake');
    await expect(suggest).toContainText('Haven’t made this in a while');
    await expect(suggest).toContainText(/Last made \d+ months ago/);
    await expect(suggest.getByRole('img', { name: 'Make it again' })).toBeVisible();
    await expect(suggest.locator('[data-rib]')).toHaveAttribute('role', 'img');
    await expect(suggest.getByRole('link', { name: 'Open recipe' })).toHaveAttribute('href', `/recipes/${ids['Lemon Cake']}`);

    await suggest.getByRole('link', { name: 'Another idea' }).click();
    await expect(suggest.getByRole('heading', { level: 2 })).toHaveText('Tomato Soup');
    await expect(suggest).toContainText(/Last made 2 days ago/);
    // A swap, not a navigation: the address is still home, the hero still there.
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole('heading', { name: 'What are we making?' })).toBeVisible();

    await suggest.getByRole('link', { name: 'Another idea' }).click();
    // Never made: by name after the ones that were.
    await expect(suggest.getByRole('heading', { level: 2 })).toHaveText('Beef Stew');
    await expect(suggest).toContainText('Haven’t tried this one yet');
    await expect(suggest).toContainText('Not made yet');
  });

  test('the idea is in the URL, so it can be reloaded', async ({ page }) => {
    await page.goto('/?idea=1');
    await expect(page.locator('#suggest').getByRole('heading', { level: 2 })).toHaveText('Tomato Soup');
    expect((await page.goto('/?idea=-1'))?.status()).toBe(400);
    expect((await page.goto('/?idea=x'))?.status()).toBe(400);
  });

  test('recently made lists the newest entries with their notes', async ({ page }) => {
    await page.goto('/');
    const entries = page.getByTestId('recently-made').locator('.made li');
    await expect(entries).toHaveCount(4);
    await expect(entries.nth(0)).toContainText('Tomato Soup');
    await expect(entries.nth(0)).toContainText('Made it');
    await expect(entries.nth(1)).toContainText('“Added basil”');
    await expect(entries.nth(0).getByRole('link', { name: 'Tomato Soup' })).toHaveAttribute('href', `/recipes/${ids['Tomato Soup']}`);
  });

  test('every control is at least 44px tall', async ({ page }) => {
    for (const size of [{ width: 1024, height: 768 }, { width: 390, height: 844 }]) {
      await page.setViewportSize(size);
      await page.goto('/');
      await expectTappable(searchbox(page));
      await expectTappable(page.locator('#suggest .btn'));
      await expectTappable(page.locator('#suggest .sg-next'));
      await expectTappable(page.locator('.made a'));
    }
  });

  test('the page loads nothing from another origin', async ({ page }) => {
    const foreign: string[] = [];
    page.on('request', (req) => {
      if (!req.url().startsWith(srv.origin)) foreign.push(req.url());
    });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    expect(foreign).toEqual([]);
  });
});
