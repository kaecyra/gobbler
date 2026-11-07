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
  // Nothing to show in the side, so there is no side.
  await expect(page.locator('aside.side')).toHaveCount(0);
  await expectTappable(page.getByTestId('empty-library').getByRole('link'));
});

test.describe('with recipes', () => {
  let ids: Record<string, number> = {};
  const cards = (page: import('@playwright/test').Page) => page.locator('.cards .rcard');
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
    await expect(soup.getByTestId('card-time')).toHaveText('35m');
    await expect(soup.locator('[data-rib]')).toHaveCount(0);

    const cake = card(page, 'Lemon Cake');
    await expect(cake.getByRole('img', { name: 'Make it again' })).toBeVisible();
    await expect(cake.locator('.spoon.on')).toHaveCount(4);
    await expect(cake.locator('.heat')).toHaveCount(0);
    await expect(cake.locator('.rfoot')).toContainText('Made 1×');

    const knots = card(page, 'Cardamom Knots');
    await expect(knots.locator('.mtype')).toContainText('Baked');
    await expect(knots.locator('.rfoot')).toContainText('Not made yet');
    await expect(knots.locator('.rfoot')).toContainText('New');
  });

  test('cards are grouped by meal, and the grouping moves to time to table', async ({ page }) => {
    await page.goto('/recipes');
    const headings = page.locator('.group h2');
    await expect(headings).toHaveText([heading('Mains', 3), heading('Baked', 1), heading('Desserts', 1)]);
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

  test('a baked recipe shows Baked on its page, and the library groups it after the mains', async ({ page }) => {
    await page.goto(`/recipes/${ids['Cardamom Knots']}`);
    await expect(page.getByTestId('meal-type')).toContainText('Baked');
    await expect(page.getByTestId('meal-type').locator('svg')).toHaveCount(1);
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
    await expect(forgot.getByRole('img', { name: 'Make it again' })).toBeVisible();
    await expect(forgot.getByRole('link', { name: 'Lemon Cake' })).toHaveAttribute('href', `/recipes/${ids['Lemon Cake']}`);
  });

  test('the library has the suggestion, and Another idea stays on the library page', async ({ page }) => {
    await page.goto('/recipes?q=a&group=time');
    const suggest = page.locator('#suggest');
    // Never made first. Lemon Cake is the forgot panel's, so it is not also
    // suggested here.
    await expect(suggest.locator('.rcard h3')).toHaveText('Beef Stew');
    // A library card, the same element and classes as one in the grid, at the panel's width.
    await expect(suggest.locator('article.rcard')).toHaveCount(1);
    await expect(suggest.getByTestId('card-picture')).toHaveClass(/\brpic\b/);
    const panel = (await page.locator('aside.side').boundingBox())!;
    expect(Math.abs((await suggest.locator('.rcard').boundingBox())!.width - panel.width)).toBeLessThanOrEqual(2);
    // The voice line is above the card and "Another idea" below it.
    const voice = (await suggest.locator('.sg-voice').boundingBox())!;
    const cardBox = (await suggest.locator('.rcard').boundingBox())!;
    const nextBox = (await suggest.getByRole('link', { name: 'Another idea' }).boundingBox())!;
    expect(voice.y + voice.height).toBeLessThanOrEqual(cardBox.y);
    expect(cardBox.y + cardBox.height).toBeLessThanOrEqual(nextBox.y);
    // The suggestion draws on the whole library, not on the search.
    const next = suggest.getByRole('link', { name: 'Another idea' });
    await expect(next).toHaveAttribute('href', '/recipes?group=time&idea=1&q=a');
    await next.click();
    await expect(suggest.locator('.rcard h3')).toHaveText('Cardamom Knots');
    await expect(page).toHaveURL(/\/recipes\?q=a&group=time$/);
    await expect(page.locator('.group h2').first()).toBeVisible();
    await page.goto('/recipes?idea=1');
    await expect(suggest.locator('.rcard h3')).toHaveText('Cardamom Knots');
    await expectTappable(suggest.locator('.rcard h3 a'));
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

  test('a card shows its picture full-bleed at the top, and the placeholder band when it has none', async ({ page }) => {
    await page.goto('/recipes');
    const soup = card(page, 'Tomato Soup');
    const img = soup.locator('.rpic img');
    await expect(img).toHaveAttribute('src', new RegExp(`^/recipes/${ids['Tomato Soup']}/picture\\?v=`));
    await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0)).toBe(true);
    // The picture sits above the meal type and spans the card's width.
    const cardBox = (await soup.boundingBox())!;
    const picBox = (await soup.getByTestId('card-picture').boundingBox())!;
    expect(picBox.y - cardBox.y).toBeLessThanOrEqual(2);
    expect(picBox.width).toBeGreaterThanOrEqual(cardBox.width - 4);
    expect(picBox.height).toBe(150);
    await expect(soup.locator('.rpic .phmark')).toBeHidden();

    // No picture: the band, same height, with the meal-type picture and the variant of the id.
    for (const name of ['Lemon Cake', 'Beef Stew']) {
      const band = card(page, name).getByTestId('card-picture');
      await expect(band.locator('img')).toHaveCount(0);
      await expect(band).toHaveClass(new RegExp(`\\bph${ids[name]! % 6}\\b`));
      await expect(band.locator('.phmark svg')).toBeVisible();
      expect((await band.boundingBox())!.height).toBe(150);
    }
    expect(ids['Lemon Cake']! % 6).not.toBe(ids['Beef Stew']! % 6);
  });

  test('search results show the pictures too', async ({ page }) => {
    await page.goto('/recipes');
    await searchbox(page).fill('tomato');
    await expect(cards(page)).toHaveCount(1);
    await expect(cards(page).locator('.rpic img')).toHaveCount(1);
  });

  test('the suggestion in the side shows the recipe picture, and the other panels show none', async ({ page }) => {
    await page.goto('/recipes?idea=2');
    const side = page.locator('aside.side');
    // Slow Ribs is the third idea and has a picture.
    await expect(side.locator('#suggest').getByTestId('card-picture').locator('img')).toHaveAttribute('src', new RegExp(`^/recipes/${ids['Slow Ribs']}/picture\\?v=`));
    await expect(side.locator('.panel .rpic, .panel img[src*="/picture"]')).toHaveCount(0);
  });

  test('a recipe whose picture object is missing shows its placeholder band, not a broken image', async ({ page }) => {
    const ribs = (p: import('@playwright/test').Page) => card(p, 'Slow Ribs').getByTestId('card-picture');
    // The picture is there until the object goes. (Asked outside the page, which would keep the
    // answer in its cache and never ask again.)
    const src = (await (await page.request.get('/recipes')).text()).match(new RegExp(`/recipes/${ids['Slow Ribs']}/picture\\?v=[\\w-]+`))?.[0];
    expect(src, 'the library links the picture').toBeTruthy();
    expect((await page.request.get(src!)).status()).toBe(200);

    srv.dropPicture(ids['Slow Ribs']!);
    const missing = page.waitForResponse((r) => r.url().includes(`/recipes/${ids['Slow Ribs']}/picture`));
    await page.goto('/recipes');
    expect((await missing).status()).toBe(404);
    await expect(ribs(page).locator('img')).toHaveCount(0);
    await expect(ribs(page)).not.toHaveAttribute('data-pic', '');
    await expect(ribs(page).locator('.phmark svg')).toBeVisible();
    await expect(ribs(page)).toHaveClass(new RegExp(`\\bph${ids['Slow Ribs']! % 6}\\b`));
    expect((await ribs(page).boundingBox())!.height).toBe(150);
  });

  test.describe('total time', () => {
    const time = (page: import('@playwright/test').Page, name: string) => card(page, name).getByTestId('card-time');

    test('a card shows its total time with a clock, and no ribbon', async ({ page }) => {
      await page.goto('/recipes');
      await expect(page.locator('[data-rib]')).toHaveCount(0);
      // Lemon Cake: 15m of prep and 40m of baking.
      await expect(time(page, 'Lemon Cake')).toHaveText('55m');
      await expect(time(page, 'Lemon Cake')).toHaveAttribute('title', '55 minutes');
      await expect(time(page, 'Lemon Cake').locator('svg')).toHaveCount(1);
      // Cardamom Knots: 20 + 90 + 40 + 30 + 20 minutes.
      await expect(time(page, 'Cardamom Knots')).toHaveText('3h 20m');
    });

    test('the time sits in the card footer, small, not edge to edge', async ({ page }) => {
      await page.goto('/recipes');
      const box = (await time(page, 'Lemon Cake').boundingBox())!;
      const cardBox = (await card(page, 'Lemon Cake').boundingBox())!;
      expect(box.width).toBeLessThan(cardBox.width / 3);
      expect(box.y + box.height).toBeLessThan(cardBox.y + cardBox.height - 8);
      expect(await time(page, 'Lemon Cake').locator('xpath=..').getAttribute('class')).toBe('rfoot');
    });

    test('a card sits under the time group its total belongs to', async ({ page }) => {
      await page.goto('/recipes?group=time');
      const group = page.locator('section.group[aria-label="Up to 3 hours"]');
      await expect(group.locator('.rcard', { hasText: 'Lemon Cake' })).toHaveCount(1);
      await expect(group.locator('.rcard', { hasText: 'Lemon Cake' }).getByTestId('card-time')).toHaveText('55m');
      await expect(group.locator('.rcard', { hasText: 'Cardamom Knots' })).toHaveCount(0);
    });
  });
});
