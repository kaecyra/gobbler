import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
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
  { name: 'Operator', path: '/operator' },
];

const nav = (page: Page) => page.getByRole('navigation', { name: 'Primary' });
const header = (page: Page) => page.getByRole('banner');
const account = (page: Page) => header(page).getByRole('button', { name: 'Account menu' });
// The light and dark toggle lives in the account menu (ADR-00032), so the menu is opened first.
const openAccount = async (page: Page) => {
  await account(page).click();
};
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
  await expect(footer.getByRole('link', { name: 'kaecyra', exact: true })).toHaveAttribute('href', 'https://resume.timgunter.ca');
  await expect(footer.getByRole('link', { name: 'Canada', exact: true })).toHaveAttribute('href', '/canada');
});

test('the footer shows the version the binary was built from, beside the repository link', async ({ page }) => {
  const version = readFileSync(fileURLToPath(new URL('../../VERSION', import.meta.url)), 'utf8').trim();
  await page.goto('/');
  const footer = page.getByRole('contentinfo');
  await expect(footer.getByTestId('app-version')).toHaveText(`v${version}`);
  // Beside the link, in the same group.
  const source = footer.getByTestId('footer-source');
  await expect(source.getByRole('link', { name: /github\.com\/kaecyra\/gobbler/ })).toBeVisible();
  await expect(source.getByTestId('app-version')).toBeVisible();
});

test('every mascot drawing is served as an SVG revalidated by its ETag', async ({ request }) => {
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
    expect(res.headers()['cache-control'], name).toBe('no-cache');
    expect(res.headers()['etag'], name).toBeTruthy();
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
    await openAccount(page);
    await page.getByRole('button', { name: 'Switch to dark' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');
    await expect(page.getByRole('button', { name: 'Switch to light' })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.getByRole('button', { name: 'Switch to light' })).toContainText('Light mode');
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');
    await openAccount(page);
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
    await openAccount(page);
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
    const avatar = await account(page).boundingBox();
    expect(avatar!.height).toBeGreaterThanOrEqual(44);
    expect(avatar!.width).toBeGreaterThanOrEqual(44);
    await openAccount(page);
    for (const name of ['Switch to dark', 'Profile']) {
      const box = await header(page).getByRole(name === 'Profile' ? 'link' : 'button', { name }).boundingBox();
      expect(box!.height, name).toBeGreaterThanOrEqual(44);
    }
  });

  test('the account menu opens downward from the top of the page, not up over the nav bar', async ({ page }) => {
    await page.goto('/');
    await openAccount(page);
    const list = await header(page).locator('.avatar-menu .menu-list').boundingBox();
    const button = await account(page).boundingBox();
    expect(list!.y).toBeGreaterThan(button!.y + button!.height);
    expect(list!.x + list!.width).toBeLessThanOrEqual(390);
  });
});

// ADR-00032 obligation 6: the avatar replaces the old light and dark button, and its
// menu holds the toggle, the profile link and (where people sign in) Sign out.
test.describe('the account menu', () => {
  test('is the signed-in person\'s avatar, drawn in the page, in place of the old button', async ({ page }) => {
    await page.goto('/');
    await expect(header(page).locator('.modebtn')).toHaveCount(0);
    const avatar = account(page).locator('svg.avatar');
    await expect(avatar).toBeVisible();
    // Drawn from the email by the app: an inline SVG path, no image request.
    await expect(avatar.locator('path')).toHaveAttribute('d', /^M/);
    await expect(account(page).locator('img')).toHaveCount(0);
  });

  test('holds the toggle and the profile link, and sends the person to their profile', async ({ page }) => {
    await page.goto('/');
    await openAccount(page);
    await expect(header(page).getByRole('button', { name: 'Switch to dark' })).toBeVisible();
    const profile = header(page).getByRole('link', { name: 'Profile' });
    await expect(profile).toHaveAttribute('href', '/profile');
    await expect(profile.locator('svg circle, svg path').first()).toBeAttached();
    await profile.click();
    await expect(page).toHaveURL(/\/profile\/[A-Za-z0-9_-]{22}$/);
    await openAccount(page);
    // Never marked current: the page may be someone else's profile.
    await expect(header(page).getByRole('link', { name: 'Profile' })).not.toHaveAttribute('aria-current');
  });

  test('closes on Escape and on a click elsewhere, and leaves the sections menu alone', async ({ page }) => {
    await page.goto('/');
    const toggle = header(page).getByRole('button', { name: 'Switch to dark' });
    await openAccount(page);
    await expect(toggle).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(toggle).toBeHidden();
    await openAccount(page);
    await page.locator('main').click({ position: { x: 5, y: 5 } });
    await expect(toggle).toBeHidden();
  });

  test('does not offer sign-out where people do not sign in to the app', async ({ page }) => {
    await page.goto('/');
    await openAccount(page);
    await expect(header(page).getByRole('button', { name: 'Sign out' })).toHaveCount(0);
  });
});

// Signed out: a public page served without an identity (ADR-00029) shows the figure
// in the same place, with the toggle and Sign in. The anonymous server runs without the
// dev bypass, as production does.
test.describe('the signed-out figure', () => {
  const anonymousURL = process.env.GOBBLER_E2E_ANONYMOUS_URL;
  if (!anonymousURL) throw new Error('GOBBLER_E2E_ANONYMOUS_URL is not set; run through playwright.config.ts');

  test('stands in the account button\'s place, with the toggle and Sign in', async ({ page }) => {
    await page.goto(`${anonymousURL}/canada`);
    const figure = header(page).getByRole('button', { name: 'Signed out menu' });
    await expect(figure).toBeVisible();
    await expect(header(page).getByRole('button', { name: 'Account menu' })).toHaveCount(0);
    const box = await figure.boundingBox();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);
    await expect(figure.locator('svg circle, svg path').first()).toBeVisible();
    await figure.click();
    await expect(header(page).getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    await expect(header(page).getByRole('link', { name: 'Profile' })).toHaveCount(0);
    await expect(header(page).getByRole('button', { name: 'Sign out' })).toHaveCount(0);
  });

  test.describe('with the system preferring dark', () => {
    test.use({ colorScheme: 'dark' });

    test('toggles the theme, remembers it, and is light until asked', async ({ page }) => {
      await page.goto(`${anonymousURL}/canada`);
      await expect(page.locator('html')).not.toHaveAttribute('data-mode', 'dark');
      await header(page).getByRole('button', { name: 'Signed out menu' }).click();
      await header(page).getByRole('button', { name: 'Switch to dark' }).click();
      await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');
      await page.reload();
      await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');
      await header(page).getByRole('button', { name: 'Signed out menu' }).click();
      await header(page).getByRole('button', { name: 'Switch to light' }).click();
      await expect(page.locator('html')).not.toHaveAttribute('data-mode', 'dark');
    });
  });
});

// The header's right-hand items are spaced by one token at every width (the issue's
// nav spacing): the bar's own gap, the gap to the account button and the coral action's
// margin used to be three different numbers.
test.describe('header spacing', () => {
  const NAV_GAP = 8;

  for (const [name, width, height] of [
    ['laptop', 1440, 900],
    ['tablet', 1024, 768],
    ['small tablet', 700, 900],
  ] as const) {
    test(`separates the nav entries, the sections menu and the account button by one gap at ${name} width`, async ({ page }) => {
      await page.setViewportSize({ width, height });
      await page.goto('/');
      const items = [
        nav(page).getByRole('link', { name: 'Library' }),
        nav(page).getByRole('link', { name: 'Shopping list' }),
        nav(page).getByRole('link', { name: 'Add recipe' }),
        nav(page).locator('details.menu'),
        header(page).locator('details.avatar-menu'),
      ];
      const boxes = [];
      for (const item of items) boxes.push((await item.boundingBox())!);
      for (let i = 1; i < boxes.length; i++) {
        const gap = boxes[i].x - (boxes[i - 1].x + boxes[i - 1].width);
        expect(gap, `gap before item ${i}`).toBeCloseTo(NAV_GAP, 0);
      }
    });
  }

  for (const [name, width, height] of [
    ['laptop', 1440, 900],
    ['tablet', 1024, 768],
    ['phone', 390, 844],
  ] as const) {
    test(`uses the token for the bar and the header, with no margin on the coral action, at ${name} width`, async ({ page }) => {
      await page.setViewportSize({ width, height });
      await page.goto('/');
      const gaps = await page.evaluate(() => {
        const px = (el: Element, prop: string) => getComputedStyle(el).getPropertyValue(prop);
        return {
          token: px(document.documentElement, '--nav-gap').trim(),
          tabs: px(document.querySelector('.tabs')!, 'column-gap'),
          wrap: px(document.querySelector('.top .wrap')!, 'column-gap'),
          cta: px(document.querySelector('.tab.cta')!, 'margin-left'),
        };
      });
      expect(gaps).toEqual({ token: '8px', tabs: '8px', wrap: '8px', cta: '0px' });
    });
  }
});

// The Operator section holds the operator pages. DEV is a member and an operator (the default
// identity, as stated at the top of this file); OTHER is a member who is not. The server refuses OTHER
// either way, so this is about what the nav offers.
test.describe('a member who is not an operator', () => {
  test.use(asIdentity(OTHER));

  test('has no Operator link in the menu', async ({ page }) => {
    await page.goto('/');
    await expect(nav(page).getByRole('link', { name: 'Library' })).toBeVisible();
    await openMenu(page);
    await expect(nav(page).getByRole('link', { name: 'Operator' })).toHaveCount(0);
    await expect(nav(page).getByRole('link', { name: 'Household' })).toHaveAttribute('href', '/household');
  });
});

test.describe('an operator who belongs to no household', () => {
  test.use(asIdentity(OPERATOR));

  test('sees Operator in the menu on an operator page', async ({ page }) => {
    await page.goto('/operator/settings');
    await openMenu(page);
    await expect(nav(page).getByRole('link', { name: 'Operator' })).toHaveAttribute('href', '/operator');
  });

  test('is offered only the operator pages', async ({ page }) => {
    await page.goto('/operator');
    await openMenu(page);
    const hrefs = await nav(page).getByRole('link').evaluateAll((as) => as.map((a) => a.getAttribute('href')));
    expect(hrefs).toEqual(['/operator']);
  });
});
