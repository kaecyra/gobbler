import { test, expect } from '@playwright/test';
import { DEV, asIdentity } from '../fixtures/identity';

// The profile page's layout (ADR-00032, #369): the avatar as the way to change
// the picture, the Profile section, household membership and the order of the
// sections. Each test that changes a profile signs in as an email of its own.

let counter = 0;
function freshEmail(label: string): string {
  counter += 1;
  return `${label}-${process.pid}-${Date.now()}-${counter}@e2e.example`;
}

const editIcon = (page: import('@playwright/test').Page) => page.getByTestId('avatar-edit');
const dialog = (page: import('@playwright/test').Page) => page.getByTestId('profile-avatar-form');

test.describe('editing the avatar from the picture', () => {
  const me = freshEmail('layout-avatar');
  test.use(asIdentity(me));

  test('the avatar is a button whose edit icon shows on hover and on focus', async ({ page }) => {
    await page.goto('/profile');
    const avatar = page.getByRole('button', { name: 'Change your picture' });
    await expect(avatar).toBeVisible();
    await expect(editIcon(page)).toHaveCSS('opacity', '0');
    await avatar.hover();
    await expect(editIcon(page)).not.toHaveCSS('opacity', '0');
    await page.mouse.move(0, 0);
    await expect(editIcon(page)).toHaveCSS('opacity', '0');
    await avatar.focus();
    await expect(avatar).toBeFocused();
    await expect(editIcon(page)).not.toHaveCSS('opacity', '0');
  });

  test('the avatar opens the file chooser, and the page never shows an upload form', async ({ page }) => {
    await page.goto('/profile');
    await expect(dialog(page)).toBeHidden();
    await expect(page.locator('input[name=avatar]')).toBeHidden();
    const chooser = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Change your picture' }).click();
    expect((await chooser).isMultiple()).toBe(false);
    // Nothing opens until a file is chosen.
    await expect(dialog(page)).toBeHidden();
    await expect(page.locator('input[name=avatar]')).toBeHidden();
  });

  test('Escape and the close button dismiss the dialog, and closing it keeps nothing', async ({ page }) => {
    await page.goto('/profile');
    const avatar = page.getByRole('button', { name: 'Change your picture' });
    const before = await page.getByTestId('profile-avatar').innerHTML();

    // A file that is refused leaves the dialog open with the reason.
    let chooser = page.waitForEvent('filechooser');
    await avatar.click();
    await (await chooser).setFiles({ name: 'a.png', mimeType: 'image/png', buffer: Buffer.from('x') });
    await expect(dialog(page).getByTestId('avatar-message')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(dialog(page)).toBeHidden();
    await expect(avatar).toBeFocused();

    // The refusal and the chosen file are gone, and nothing was uploaded.
    expect(await page.locator('input[name=avatar]').evaluate((el: HTMLInputElement) => el.files!.length)).toBe(0);
    await expect(page.getByTestId('avatar-status')).toHaveAttribute('data-state', 'none');
    await expect(dialog(page).getByTestId('avatar-message')).toHaveCount(0);

    chooser = page.waitForEvent('filechooser');
    await avatar.click();
    await (await chooser).setFiles({ name: 'b.png', mimeType: 'image/png', buffer: Buffer.from('y') });
    await expect(dialog(page)).toBeVisible();
    await page.getByTestId('avatar-close').click();
    await expect(dialog(page)).toBeHidden();
    expect(await page.getByTestId('profile-avatar').innerHTML()).toBe(before);
    await expect(page.getByTestId('profile-avatar-img')).toHaveCount(0);
  });
});

test.describe('on a touch device', () => {
  test.use({ ...asIdentity(freshEmail('layout-touch')), hasTouch: true, isMobile: true });

  test('the edit icon is always visible', async ({ page }) => {
    await page.goto('/profile');
    await expect(editIcon(page)).not.toHaveCSS('opacity', '0');
  });
});

test.describe('the Profile section', () => {
  const me = freshEmail('layout-profile');
  test.use(asIdentity(me));

  test('is headed Profile and still sets the display name', async ({ page }) => {
    await page.goto('/profile');
    const section = page.getByTestId('profile-name-form');
    await expect(section.getByRole('heading', { name: 'Profile', exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Display name' })).toHaveCount(0);
    await section.getByRole('textbox', { name: 'Name' }).fill('Layout Cook');
    await section.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Layout Cook');
  });
});

test.describe('household membership', () => {
  test.describe('of a member', () => {
    test.use(asIdentity(DEV));

    test('names the household and links to it', async ({ page }) => {
      await page.goto('/profile');
      const section = page.getByTestId('profile-household');
      await expect(section).toContainText('You are a member of Dev household.');
      await section.getByRole('link', { name: 'Dev household' }).click();
      await expect(page).toHaveURL(/\/household$/);
    });
  });

  test.describe('of someone with none', () => {
    test.use(asIdentity(freshEmail('layout-nohousehold')));

    test('says so plainly', async ({ page }) => {
      await page.goto('/profile');
      await expect(page.getByTestId('profile-household')).toContainText('You are not in a household.');
      await expect(page.getByTestId('profile-household').getByRole('link')).toHaveCount(0);
    });
  });
});

test.describe('the order of the sections', () => {
  test.use(asIdentity(DEV));

  test('is profile, household, API keys, connected apps, sessions', async ({ page }) => {
    await page.goto('/profile');
    const headings = await page.locator('main section h2').allTextContents();
    const order = ['Profile', 'Household', 'API keys', 'Connected apps', 'Sessions'];
    const found = headings.filter((h) => order.includes(h.trim())).map((h) => h.trim());
    expect(found).toEqual(order);
  });
});

test.describe('spacing', () => {
  test.use(asIdentity(freshEmail('layout-gap')));

  test('the profile box and the first section have the gap every panel has', async ({ page }) => {
    await page.goto('/profile');
    const head = (await page.getByTestId('profile-identity').boundingBox())!;
    const next = (await page.locator('.pg.profile > .panel').nth(1).boundingBox())!;
    expect(next.y - (head.y + head.height)).toBeGreaterThanOrEqual(16);
  });
});
