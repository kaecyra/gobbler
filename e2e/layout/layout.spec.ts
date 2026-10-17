import { test, expect, type Page } from '@playwright/test';
import { OPERATOR, OTHER, asIdentity } from '../fixtures/identity';

// The tests below, up to the identity blocks, run as the default identity DEV,
// which the shared e2e server (e2e/fixtures/serve.mjs) makes an operator. For an
// operator every section is reachable from the header: three in the nav itself
// and the rest behind its menu button (docs/ui/design.md, Layout and chrome).
const primary = [
  { name: 'Library', path: '/recipes' },
  { name: 'Shopping list', path: '/shopping' },
  { name: 'Add recipe', path: '/ingest' },
];
const menu = [
  { name: 'Catalog', path: '/catalog' },
  { name: 'Shared', path: '/shares' },
  { name: 'Household', path: '/household' },
  { name: 'Invitations', path: '/operator' },
  { name: 'Settings', path: '/settings' },
];

const nav = (page: Page) => page.getByRole('navigation', { name: 'Primary' });
const openMenu = async (page: Page) => {
  await nav(page).getByRole('button', { name: 'More sections' }).click();
};

test('the home page links to every section', async ({ page }) => {
  await page.goto('/');
  for (const p of primary) {
    await expect(nav(page).getByRole('link', { name: p.name })).toHaveAttribute('href', p.path);
  }
  await openMenu(page);
  for (const p of menu) {
    await expect(nav(page).getByRole('link', { name: p.name })).toHaveAttribute('href', p.path);
  }
  await expect(nav(page).locator('[aria-current="page"]')).toHaveCount(0);
});

for (const p of primary) {
  test(`${p.name} is reached from the nav and marked current`, async ({ page }) => {
    await page.goto('/');
    await nav(page).getByRole('link', { name: p.name }).click();
    await expect(page).toHaveURL(new RegExp(`${p.path}$`));
    const current = nav(page).locator('[aria-current="page"]');
    await expect(current).toHaveCount(1);
    await expect(current).toHaveAccessibleName(p.name);
  });
}

for (const p of menu) {
  test(`${p.name} is reached from the menu and marked current`, async ({ page }) => {
    await page.goto('/');
    await openMenu(page);
    await nav(page).getByRole('link', { name: p.name }).click();
    await expect(page).toHaveURL(new RegExp(`${p.path}$`));
    await expect(nav(page).locator('[aria-current="page"]')).toHaveCount(1);
    // The menu button itself shows that the current page lives behind it.
    await expect(nav(page).getByRole('button', { name: 'More sections' })).toHaveAttribute('data-current', 'true');
    await openMenu(page);
    await expect(nav(page).getByRole('link', { name: p.name })).toHaveAttribute('aria-current', 'page');
  });
}

test('the menu closes on Escape and on a click elsewhere', async ({ page }) => {
  await page.goto('/');
  const catalog = nav(page).getByRole('link', { name: 'Catalog' });
  await openMenu(page);
  await expect(catalog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(catalog).toBeHidden();
  await openMenu(page);
  await expect(catalog).toBeVisible();
  await page.locator('main').click({ position: { x: 5, y: 5 } });
  await expect(catalog).toBeHidden();
});

test('the header shows the mascot, wordmark and tagline', async ({ page }) => {
  await page.goto('/');
  const header = page.getByRole('banner');
  await expect(header.getByRole('link', { name: 'gobbler' })).toHaveAttribute('href', '/');
  await expect(header.getByText('Find it. Cook it. Gobble it.')).toBeVisible();
  const mascot = header.locator('img.standing');
  await expect(mascot).toBeVisible();
  expect(await mascot.evaluate((i: HTMLImageElement) => i.naturalWidth)).toBeGreaterThan(0);
});

test('the footer links the repository and credits the maker', async ({ page }) => {
  await page.goto('/');
  const footer = page.getByRole('contentinfo');
  await expect(footer.getByRole('link', { name: /github\.com\/kaecyra\/gobbler/ })).toHaveAttribute(
    'href',
    'https://github.com/kaecyra/gobbler',
  );
  await expect(footer.getByText('Made by kaecyra in Canada')).toBeVisible();
});

test('every mascot drawing is served as a cached SVG', async ({ request }) => {
  for (const name of [
    'gob',
    'gob-full-rolling-pin',
    'gob-full-mixing-bowl',
    'gob-whisk',
    'gob-herbs',
    'gob-full-pie',
    'gob-tasting',
  ]) {
    const res = await request.get(`/static/img/${name}.svg`);
    expect(res.status(), name).toBe(200);
    expect(res.headers()['content-type'], name).toContain('image/svg+xml');
    expect(res.headers()['cache-control'], name).toContain('max-age');
    expect(await res.text(), name).toContain('<svg');
  }
});

// Light always (docs/ui/design.md): the OS preference never flips the theme.
test.describe('theme', () => {
  test.use({ colorScheme: 'dark' });

  test('is light even when the system prefers dark', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('html')).not.toHaveAttribute('data-mode', 'dark');
    const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
    expect(bg).toBe('rgb(252, 250, 245)');
  });

  test('the header toggle switches to dark and the choice survives a reload', async ({ page }) => {
    await page.goto('/');
    const toggle = page.getByRole('button', { name: 'Switch to dark' });
    await toggle.click();
    await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');
    await expect(page.getByRole('button', { name: 'Switch to light' })).toHaveAttribute('aria-pressed', 'true');
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');
    await page.getByRole('button', { name: 'Switch to light' }).click();
    await expect(page.locator('html')).not.toHaveAttribute('data-mode', 'dark');
    await page.reload();
    await expect(page.locator('html')).not.toHaveAttribute('data-mode', 'dark');
  });

  test('still toggles when storage is unavailable', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window, 'localStorage', {
        get() {
          throw new Error('storage blocked');
        },
      });
    });
    await page.goto('/');
    await page.getByRole('button', { name: 'Switch to dark' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');
  });
});

// The header and footer never push the page wider than the screen: tablet,
// laptop and phone.
for (const [name, width, height] of [
  ['tablet', 1024, 768],
  ['laptop', 1440, 900],
  ['phone', 390, 844],
] as const) {
  test.describe(`at ${name} width`, () => {
    test.use({ viewport: { width, height } });

    test('the page does not scroll sideways', async ({ page }) => {
      for (const path of ['/', '/recipes']) {
        await page.goto(path);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        expect(overflow, path).toBeLessThanOrEqual(0);
      }
    });
  });
}

test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('the nav is a fixed bar along the bottom and the page does not scroll sideways', async ({ page }) => {
    await page.goto('/');
    const box = await nav(page).boundingBox();
    expect(box).not.toBeNull();
    expect(Math.round(box!.y + box!.height)).toBe(844);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test('every nav target is at least 44px', async ({ page }) => {
    await page.goto('/');
    for (const p of primary) {
      const box = await nav(page).getByRole('link', { name: p.name }).boundingBox();
      expect(box!.height, p.name).toBeGreaterThanOrEqual(44);
      expect(box!.width, p.name).toBeGreaterThanOrEqual(44);
    }
    const toggle = await page.getByRole('button', { name: 'Switch to dark' }).boundingBox();
    expect(toggle!.height).toBeGreaterThanOrEqual(44);
  });
});

// Settings and Invitations are operator pages. DEV is a member and an operator (the default
// identity, as stated at the top of this file); OTHER is a member who is not. The server refuses OTHER
// either way, so this is about what the nav offers.
test.describe('a member who is not an operator', () => {
  test.use(asIdentity(OTHER));

  test('has no Settings link in the menu', async ({ page }) => {
    await page.goto('/');
    await expect(nav(page).getByRole('link', { name: 'Library' })).toBeVisible();
    await openMenu(page);
    await expect(nav(page).getByRole('link', { name: 'Settings' })).toHaveCount(0);
    await expect(nav(page).getByRole('link', { name: 'Invitations' })).toHaveCount(0);
    await expect(nav(page).getByRole('link', { name: 'Household' })).toHaveAttribute('href', '/household');
  });
});

test.describe('an operator who belongs to no household', () => {
  test.use(asIdentity(OPERATOR));

  test('sees Settings in the menu on the operator page', async ({ page }) => {
    await page.goto('/settings');
    await openMenu(page);
    await expect(nav(page).getByRole('link', { name: 'Settings' })).toHaveAttribute('href', '/settings');
    await expect(nav(page).getByRole('link', { name: 'Invitations' })).toHaveAttribute('href', '/operator');
  });
});
