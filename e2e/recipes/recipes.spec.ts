import { test, expect } from '@playwright/test';
import { expectTappable, seededServer } from './support';

const srv = await seededServer('GOBBLER_E2E_RECIPES_PORT', 'gobbler-e2e-recipes-');

test.use({ baseURL: srv.origin });
// One server per file; the empty-library test must run before the seeding.
test.describe.configure({ mode: 'serial' });

test.beforeAll(srv.start);
test.afterAll(srv.stop);

/** The text of a group heading: its title and the number of cards. */
const heading = (title: string, count: number) => new RegExp(`^\\s*${title}\\s*${count}$`);
const searchbox = (page: import('@playwright/test').Page) => page.getByRole('searchbox', { name: 'Search recipes' });

test('an empty library shows an empty state, not an error', async ({ page }) => {
  const res = await page.goto('/recipes');
  expect(res?.status()).toBe(200);
  await expect(page.getByTestId('empty-library')).toBeVisible();
  await expect(page.getByTestId('recently-made')).toContainText('Nothing made yet');
  await expect(page.getByTestId('forgot')).toHaveCount(0);
  await expectTappable(page.getByTestId('empty-library').getByRole('link'));
});

test.describe('with recipes', () => {
  let ids: Record<string, number> = {};
  const cards = (page: import('@playwright/test').Page) => page.locator('.rcard');
  const card = (page: import('@playwright/test').Page, name: string) => cards(page).filter({ has: page.getByRole('heading', { name, level: 3 }) });

  test.beforeAll(() => {
    ids = srv.seed();
  });

  test('cards carry meal type, chilis, source, parts, key ingredients, spoons and the made-it line', async ({ page }) => {
    await page.goto('/recipes');
    await expect(cards(page)).toHaveCount(5);

    const soup = card(page, 'Tomato Soup');
    await expect(soup.getByRole('link', { name: 'Tomato Soup' })).toHaveAttribute('href', `/recipes/${ids['Tomato Soup']}`);
    await expect(soup.locator('.mtype')).toContainText('Main');
    await expect(soup.getByTestId('card-cuisine')).toHaveText('Italian');
    await expect(card(page, 'Beef Stew').getByTestId('card-cuisine')).toHaveCount(0);
    await expect(soup.getByRole('img', { name: 'Medium heat' })).toBeVisible();
    await expect(soup.locator('.heat svg')).toHaveCount(2);
    await expect(soup).toContainText('example.test');
    await expect(soup.getByRole('list', { name: 'Parts' })).toContainText('Main');
    await expect(soup.locator('.key em')).toHaveText('tomato');
    // Unrated says New; three entries in the log, the newest two days ago.
    await expect(soup.locator('.rfoot')).toContainText('New');
    await expect(soup.locator('.rfoot')).toContainText(/Made 3× · /);
    await expect(soup.locator('[data-rib]')).toHaveAttribute('role', 'img');

    const cake = card(page, 'Lemon Cake');
    await expect(cake.getByRole('img', { name: 'Make it again' })).toBeVisible();
    await expect(cake.locator('.spoon.on')).toHaveCount(4);
    await expect(cake.locator('.heat')).toHaveCount(0);
    await expect(cake.locator('.rfoot')).toContainText('Made 1×');

    const knots = card(page, 'Cardamom Knots');
    await expect(knots.locator('.rfoot')).toContainText('Not made yet');
    await expect(knots.locator('.rfoot')).toContainText('New');
  });

  test('cards are grouped by meal, and the grouping moves to time to table', async ({ page }) => {
    await page.goto('/recipes');
    const headings = page.locator('.group h2');
    await expect(headings).toHaveText([heading('Breakfast', 1), heading('Mains', 3), heading('Desserts', 1)]);
    await expect(page.locator('.group', { hasText: 'Mains' }).locator('.rcard')).toHaveCount(3);
    await expect(page.getByRole('link', { name: 'Meal' })).toHaveAttribute('aria-current', 'true');

    await page.getByRole('link', { name: 'Time to table' }).click();
    await expect(page).toHaveURL(/group=time/);
    await expect(headings).toHaveText([heading('Under 45 minutes', 1), heading('Up to 3 hours', 2), heading('Plan ahead', 2)]);
    await expect(page.locator('.group', { hasText: 'Under 45 minutes' })).toContainText('Tomato Soup');
    await expect(page.getByRole('link', { name: 'Time to table' })).toHaveAttribute('aria-current', 'true');

    // The grouping is in the URL: it survives a reload and a new search.
    await page.reload();
    await expect(headings).toHaveText([heading('Under 45 minutes', 1), heading('Up to 3 hours', 2), heading('Plan ahead', 2)]);
    await searchbox(page).fill('dinner');
    await expect(page).toHaveURL(/q=dinner/);
    await expect(page).toHaveURL(/group=time/);
    await expect(headings).toHaveText([heading('Under 45 minutes', 1), heading('Up to 3 hours', 1)]);
    await page.getByRole('link', { name: 'Meal' }).click();
    await expect(page).not.toHaveURL(/group=/);
    await expect(headings).toHaveText([heading('Mains', 2)]);
  });

  test('the side panels read the made-it log', async ({ page }) => {
    await page.goto('/recipes');
    const recent = page.getByTestId('recently-made');
    const entries = recent.locator('.made li');
    await expect(entries).toHaveCount(4);
    await expect(entries.nth(0)).toContainText('Tomato Soup');
    await expect(entries.nth(0)).toContainText('Made it');
    await expect(entries.nth(1)).toContainText('“Added basil”');
    await expect(entries.nth(3)).toContainText('Lemon Cake');
    await expect(entries.nth(3)).toContainText('“Too sweet”');
    const forgot = page.getByTestId('forgot');
    await expect(forgot).toContainText('Last made in');
    await expect(forgot).toContainText('You gave it four spoons.');
    await expect(forgot.getByRole('link', { name: 'Lemon Cake' })).toHaveAttribute('href', `/recipes/${ids['Lemon Cake']}`);
  });

  test('the library has the suggestion, and Another idea stays on the library page', async ({ page }) => {
    await page.goto('/recipes?q=a&group=time');
    const suggest = page.locator('#suggest');
    await expect(suggest.getByRole('heading', { level: 2 })).toHaveText('Lemon Cake');
    // The suggestion draws on the whole library, not on the search.
    const next = suggest.getByRole('link', { name: 'Another idea' });
    await expect(next).toHaveAttribute('href', '/recipes?group=time&idea=1&q=a');
    await next.click();
    await expect(suggest.getByRole('heading', { level: 2 })).toHaveText('Tomato Soup');
    await expect(page).toHaveURL(/\/recipes\?q=a&group=time$/);
    await expect(page.locator('.group h2').first()).toBeVisible();
    await page.goto('/recipes?idea=1');
    await expect(suggest.getByRole('heading', { level: 2 })).toHaveText('Tomato Soup');
    await expectTappable(suggest.locator('.btn'));
    await expectTappable(suggest.locator('.sg-next'));
  });

  test('search narrows the results, updates the URL and survives a reload', async ({ page }) => {
    await page.goto('/recipes');
    await searchbox(page).fill('lemon');
    await expect(cards(page)).toHaveCount(1);
    await expect(cards(page)).toContainText('Lemon Cake');
    await expect(page.locator('.res-h')).toContainText('1 recipe for “lemon”');
    await expect(page).toHaveURL(/\/recipes\?q=lemon/);
    await page.reload();
    await expect(cards(page)).toHaveCount(1);
    await expect(searchbox(page)).toHaveValue('lemon');
  });

  test('search reaches ingredients, descriptions and notes', async ({ page }) => {
    for (const [text, name] of [['tomatoes', 'Tomato Soup'], ['zesty', 'Lemon Cake'], ['thyme', 'Beef Stew']]) {
      await page.goto(`/recipes?q=${text}`);
      await expect(cards(page)).toHaveCount(1);
      await expect(cards(page)).toContainText(name);
    }
  });

  test('there are no tag filters', async ({ page }) => {
    await page.goto('/recipes');
    await expect(page.getByRole('list', { name: 'Filter by tag' })).toHaveCount(0);
    // A tag in the URL from an older link is ignored, not an error.
    const res = await page.goto('/recipes?tag=dinner');
    expect(res?.status()).toBe(200);
    await expect(cards(page)).toHaveCount(5);
  });

  test('a search with no match shows the empty state, and odd input is not an error', async ({ page }) => {
    await page.goto('/recipes?q=zebra');
    await expect(page.getByTestId('no-match')).toBeVisible();
    for (const q of ['"', 'AND', 'name:lemon', '(', '*']) {
      const res = await page.goto(`/recipes?q=${encodeURIComponent(q)}`);
      expect(res?.status()).toBe(200);
    }
  });

  test('the "/" key focuses the search box', async ({ page }) => {
    await page.goto('/recipes');
    await expect(searchbox(page)).not.toBeFocused();
    await page.keyboard.press('/');
    await expect(searchbox(page)).toBeFocused();
    // Typed into the box, "/" is just a character.
    await page.keyboard.type('a/b');
    await expect(searchbox(page)).toHaveValue('a/b');
  });

  test('every control is at least 44px tall', async ({ page }) => {
    await page.goto('/recipes');
    await expectTappable(searchbox(page));
    await expectTappable(page.locator('.grpby .seg a'));
    await expectTappable(page.locator('.rcard h3 a'));
    await expectTappable(page.locator('.made a'));
    await expectTappable(page.getByTestId('forgot').getByRole('link'));
    await page.setViewportSize({ width: 390, height: 844 });
    await expectTappable(page.locator('.rcard h3 a'));
    await expectTappable(page.locator('.grpby .seg a'));
  });

  test('the page loads nothing from another origin', async ({ page }) => {
    const foreign: string[] = [];
    page.on('request', (req) => {
      if (!req.url().startsWith(srv.origin)) foreign.push(req.url());
    });
    await page.goto('/recipes?q=lemon');
    await page.waitForLoadState('networkidle');
    expect(foreign).toEqual([]);
  });

  test.describe('time ribbon', () => {
    const knotsRibbon = (page: import('@playwright/test').Page) => card(page, 'Cardamom Knots').locator('[data-rib]');
    // Sets the width of a card, so the ribbon refits as it would in a narrower grid cell.
    const widen = (page: import('@playwright/test').Page, px: number) =>
      card(page, 'Cardamom Knots').evaluate((el, w) => ((el as HTMLElement).style.width = `${w}px`), px);

    test('a short ribbon shows level 0 with a tooltip and a label', async ({ page }) => {
      await page.goto('/recipes');
      const rib = card(page, 'Lemon Cake').locator('[data-rib]');
      await expect(rib).toHaveAttribute('data-level', '0');
      await expect(rib).toHaveAttribute('aria-label', '15 minutes prep, then 40 minutes bake');
      await expect(rib.locator('.rib-full .rib-seg').first()).toHaveAttribute('title', '15m prep');
      await expect(rib.locator('.rib-full .rib-seg').nth(1)).toHaveAttribute('title', '40m bake');
    });

    test('a long ribbon moves to a merged level as the card narrows, and back', async ({ page }) => {
      await page.setViewportSize({ width: 1400, height: 900 });
      await page.goto('/recipes');
      await widen(page, 900);
      await expect(knotsRibbon(page)).toHaveAttribute('data-level', '0');
      await widen(page, 230);
      await expect.poll(() => knotsRibbon(page).getAttribute('data-level')).toMatch(/^[12]$/);
      await widen(page, 900);
      await expect(knotsRibbon(page)).toHaveAttribute('data-level', '0');
    });

    test('without the script a narrow ribbon wraps and nothing is clipped', async ({ page }) => {
      await page.route('**/static/ribbon.js', (route) => route.abort());
      await page.goto('/recipes');
      await widen(page, 230);
      const rib = knotsRibbon(page);
      await expect(rib).toHaveAttribute('data-level', '0');
      await expect(rib).not.toHaveAttribute('data-fit', '');
      const box = (await rib.boundingBox())!;
      // More than one line of segments, every one inside the ribbon.
      expect(box.height).toBeGreaterThan(34);
      const edges = await rib.locator('.rib-full .rib-seg').evaluateAll((segs) => segs.map((s) => s.getBoundingClientRect().right));
      for (const right of edges) expect(right).toBeLessThanOrEqual(box.x + box.width + 1);
      expect(await rib.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    });
  });
});
