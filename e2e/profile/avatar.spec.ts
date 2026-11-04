import { test, expect, type Page } from '@playwright/test';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdirSync } from 'node:fs';
import { OTHER, asIdentity } from '../fixtures/identity';
import { expectTapTargets } from '../fixtures/tap';

// Avatar upload, crop, background processing and serving (ADR-00033). Each test
// signs in as an email of its own: the server is shared with specs that run in
// parallel.

let counter = 0;
function freshEmail(label: string): string {
  counter += 1;
  return `${label}-${process.pid}-${Date.now()}-${counter}@e2e.example`;
}

const fixture = (name: string) => fileURLToPath(new URL(`./avatar-fixtures/${name}`, import.meta.url));
const header = (page: Page) => page.getByRole('banner');

/** Opens the picture dialog from the avatar. */
async function openDialog(page: Page) {
  await page.getByTestId('profile-avatar').click();
  await expect(page.getByTestId('profile-avatar-form')).toBeVisible();
}

/** Chooses a file in the picture dialog and presses Upload. */
async function upload(page: Page, file: string | { name: string; mimeType: string; buffer: Buffer }) {
  await page.goto('/profile');
  await openDialog(page);
  await page.locator('input[name=avatar]').setInputFiles(file);
  await page.getByRole('button', { name: 'Upload' }).click();
}

/** Waits, in the dialog, for Cropper.js to draw, then keeps the default square. */
async function cropAndSave(page: Page) {
  await waitForCrop(page);
  await page.getByTestId('crop-save').click();
}

/** Waits for the crop step in the dialog to be drawn. */
async function waitForCrop(page: Page) {
  await expect(dialogOf(page).locator('[data-crop-form][data-crop-ready=true]')).toBeAttached();
  await expect(dialogOf(page).locator('.cropper-container')).toBeVisible();
}

const dialogOf = (page: Page) => page.getByTestId('profile-avatar-form');

test.describe('uploading an avatar', () => {
  const me = freshEmail('avatar-me');
  test.use(asIdentity(me));

  test('crops in the browser, processes in the background and serves a stripped WebP from the app', async ({ page, baseURL }) => {
    const origins = new Set<string>();
    page.on('request', (r) => {
      if (r.url().startsWith('http')) origins.add(new URL(r.url()).origin);
    });

    await upload(page, fixture('gps-tagged.jpg'));
    await cropAndSave(page);

    // The status fragment polls while the job runs; the page then reloads on its own.
    const img = page.getByTestId('profile-avatar-img');
    await expect(img).toBeVisible({ timeout: 30_000 });
    const src = (await img.getAttribute('src'))!;
    expect(src).toMatch(/^\/profile\/[A-Za-z0-9_-]{22}\/avatar\?v=/);

    // 400x300 with the default square crop: 300x300, never the whole picture.
    const size = await img.evaluate((el: HTMLImageElement) => [el.naturalWidth, el.naturalHeight]);
    expect(size).toEqual([300, 300]);
    // Red left, blue right: the crop is the middle of the picture.
    const colours = await img.evaluate((el: HTMLImageElement) => {
      const c = document.createElement('canvas');
      c.width = el.naturalWidth;
      c.height = el.naturalHeight;
      const g = c.getContext('2d')!;
      g.drawImage(el, 0, 0);
      return [g.getImageData(10, 150, 1, 1).data, g.getImageData(290, 150, 1, 1).data].map((d) => [d[0], d[2]]);
    });
    expect(colours[0][0]).toBeGreaterThan(150);
    expect(colours[1][1]).toBeGreaterThan(150);

    // The header shows it too.
    await expect(header(page).getByTestId('header-avatar-img')).toBeVisible();
    expect(await header(page).getByTestId('header-avatar-img').getAttribute('src')).toBe(src);

    // What the app serves is lossy WebP with no EXIF (GPS), XMP or ICC.
    const res = await page.request.get(src);
    expect(res.status()).toBe(200);
    expect(res.headers()['content-type']).toBe('image/webp');
    const body = await res.body();
    expect(body.subarray(0, 4).toString('latin1')).toBe('RIFF');
    expect(body.subarray(8, 12).toString('latin1')).toBe('WEBP');
    const text = body.toString('latin1');
    for (const leak of ['EXIF', 'Exif', 'XMP ', 'xmpmeta', 'ICCP', 'ICC_PROFILE', 'GPS', 'SECRETPLACE']) {
      expect(text, `stored avatar contains ${leak}`).not.toContain(leak);
    }

    // Nothing was loaded from another origin, and no URL names a storage server.
    await page.goto('/profile');
    await page.waitForLoadState('networkidle');
    expect([...origins]).toEqual([new URL(baseURL!).origin]);
  });

  test('refuses what it cannot use, each with a message that says what to do', async ({ page }) => {
    const cases: [string, { name: string; mimeType: string; buffer: Buffer }, RegExp][] = [
      ['over 5 MB', { name: 'big.jpg', mimeType: 'image/jpeg', buffer: Buffer.alloc(5 * 1024 * 1024 + 2048, 1) }, /over 5 MB/],
      ['text called a jpeg', { name: 'notes.jpg', mimeType: 'image/jpeg', buffer: Buffer.from('hello, not a picture') }, /not a JPEG, PNG, GIF, WebP, HEIC or AVIF/],
      ['a cut-off jpeg', { name: 'cut.jpg', mimeType: 'image/jpeg', buffer: Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 16, 74, 70, 73, 70, 0, 1, 1, 0, 0, 1, 0, 1, 0, 0]) }, /could not be read|not a JPEG/],
    ];
    for (const [name, file, message] of cases) {
      await upload(page, file);
      await expect(page.getByText(message), name).toBeVisible();
    }
  });

  test('uploads a HEIC, which the browser cannot show, by cropping against a preview made by the server', async ({ page }) => {
    await upload(page, fixture('sample.heic'));
    // Cropper.js hides the original image and draws its own; the preview itself loaded.
    await expect(page.locator('.cropper-container')).toBeVisible();
    expect(await page.getByTestId('crop-image').evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0);
    await cropAndSave(page);
    await expect(page.getByTestId('profile-avatar-img')).toBeVisible({ timeout: 60_000 });
  });

  test('has controls at least 44px tall on the profile and in the crop step, on a laptop and on a phone', async ({ page }) => {
    for (const viewport of [{ width: 1024, height: 768 }, { width: 390, height: 844 }]) {
      await page.setViewportSize(viewport);
      await page.goto('/profile');
      await expectTapTargets(page, 'main');
      await openDialog(page);
      await expectTapTargets(page, 'dialog');
      await page.locator('input[name=avatar]').setInputFiles(fixture('gps-tagged.jpg'));
      await page.getByRole('button', { name: 'Upload' }).click();
      await waitForCrop(page);
      await expectTapTargets(page, 'dialog');
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, `${viewport.width}px`).toBeLessThanOrEqual(0);
    }
  });
});

test.describe('the picture dialog', () => {
  test.describe.configure({ mode: 'serial' });
  const me = freshEmail('avatar-dialog');
  test.use(asIdentity(me));

  test('takes the upload, the crop and the save without leaving the profile page', async ({ page }) => {
    await page.goto('/profile');
    const profileUrl = page.url();
    await openDialog(page);
    await page.locator('input[name=avatar]').setInputFiles(fixture('gps-tagged.jpg'));
    await page.getByRole('button', { name: 'Upload' }).click();

    // The crop is the dialog's next step: same page, same dialog, Cropper drawn in it.
    await waitForCrop(page);
    expect(page.url()).toBe(profileUrl);
    await expect(dialogOf(page).getByTestId('crop-image')).toBeAttached();
    await expect(dialogOf(page)).toBeVisible();

    await page.getByTestId('crop-save').click();
    await expect(dialogOf(page).getByTestId('avatar-status')).toHaveAttribute('data-state', 'queued');
    expect(page.url()).toBe(profileUrl);
    await expect(page.getByTestId('profile-avatar-img')).toBeVisible({ timeout: 30_000 });
  });

  test('closing in the middle of a crop saves nothing, keeps the old avatar and the page, and returns focus', async ({ page }) => {
    await page.goto('/profile');
    const before = (await page.getByTestId('profile-avatar-img').getAttribute('src'))!;
    const served = await (await page.request.get(before)).body();
    const avatar = page.getByTestId('profile-avatar');
    // Something typed elsewhere on the page and not saved: closing must not throw it away.
    const name = page.getByTestId('profile-name-form').getByRole('textbox', { name: 'Name' });
    await name.fill('Unsaved name');

    await openDialog(page);
    await page.locator('input[name=avatar]').setInputFiles(fixture('sample.heic'));
    await page.getByRole('button', { name: 'Upload' }).click();
    await waitForCrop(page);
    await page.keyboard.press('Escape');
    await expect(dialogOf(page)).toBeHidden();
    await expect(avatar).toBeFocused();
    await expect(name).toHaveValue('Unsaved name');

    // Nothing was saved: the avatar is the old one, byte for byte, and the dialog starts over.
    await expect(page.getByTestId('profile-avatar-img')).toHaveAttribute('src', before);
    expect(Buffer.compare(served, await (await page.request.get(before)).body())).toBe(0);
    await expect(page.getByTestId('avatar-status')).toHaveAttribute('data-state', 'uploaded');
    await avatar.click();
    await expect(dialogOf(page).locator('input[name=avatar]')).toBeVisible();
    await expect(dialogOf(page).getByTestId('crop-save')).toHaveCount(0);

    // Cancel in the crop step closes it the same way.
    await dialogOf(page).getByTestId('avatar-status').getByRole('link', { name: 'Choose the square to keep' }).click();
    await waitForCrop(page);
    // Only the step arrives, not a whole page nested in the dialog.
    await expect(dialogOf(page).locator('header, main, h1')).toHaveCount(0);
    await dialogOf(page).getByTestId('crop-cancel').click();
    await expect(dialogOf(page)).toBeHidden();
    await expect(avatar).toBeFocused();
    await expect(name).toHaveValue('Unsaved name');
    await expect(page.getByTestId('profile-avatar-img')).toHaveAttribute('src', before);
  });

  test('moves focus to the new step each time one replaces the last', async ({ page }) => {
    await page.goto('/profile');
    await page.getByTestId('profile-avatar').click();
    await page.locator('input[name=avatar]').setInputFiles(fixture('gps-tagged.jpg'));
    await page.getByRole('button', { name: 'Upload' }).click();
    await waitForCrop(page);
    await expect(dialogOf(page).getByRole('heading', { name: 'Choose the square to keep' })).toBeFocused();
    await page.getByTestId('crop-save').click();
    await expect(dialogOf(page).getByRole('heading', { name: 'Saving your picture' })).toBeFocused();
  });

  test('shows the reason in the dialog when a file is refused', async ({ page }) => {
    await page.goto('/profile');
    await openDialog(page);
    await page.locator('input[name=avatar]').setInputFiles({ name: 'notes.jpg', mimeType: 'image/jpeg', buffer: Buffer.from('hello, not a picture') });
    await page.getByRole('button', { name: 'Upload' }).click();
    await expect(dialogOf(page).getByTestId('avatar-message')).toContainText('not a JPEG, PNG, GIF, WebP, HEIC or AVIF');
    await expect(dialogOf(page)).toBeVisible();
    await expect(dialogOf(page).locator('input[name=avatar]')).toBeVisible();
    await expect(dialogOf(page).getByTestId('avatar-error')).toBeHidden();
  });

  test('says so in the dialog when a request fails, and offers to try again', async ({ page }) => {
    await page.goto('/profile');
    await openDialog(page);
    const upload_ = () => page.locator('input[name=avatar]').setInputFiles(fixture('gps-tagged.jpg'));

    // A server error, a dropped connection and a signed-out session each show a message.
    const cases: [string, (r: import('@playwright/test').Route) => Promise<void>, RegExp][] = [
      ['500', (r) => r.fulfill({ status: 500, body: 'boom' }), /went wrong on our side/],
      ['dropped connection', (r) => r.abort('connectionfailed'), /connection failed/],
      ['401', (r) => r.fulfill({ status: 401, body: 'no' }), /signed out/],
    ];
    for (const [name, fail, message] of cases) {
      await page.route(/\/avatar$/, async (route) => (route.request().method() === 'POST' ? fail(route) : route.continue()));
      await upload_();
      await page.getByRole('button', { name: 'Upload' }).click();
      await expect(dialogOf(page).getByTestId('avatar-error'), name).toBeVisible();
      await expect(dialogOf(page).getByTestId('avatar-error'), name).toContainText(message);
      await page.unroute(/\/avatar$/);
      // Try again starts over at the first step with the message gone.
      await dialogOf(page).getByTestId('avatar-retry').click();
      await expect(dialogOf(page).getByTestId('avatar-error')).toBeHidden();
      await expect(dialogOf(page).locator('input[name=avatar]')).toBeVisible();
    }
    // Close is always there.
    await page.route(/\/avatar$/, (route) => route.fulfill({ status: 500, body: 'boom' }));
    await upload_();
    await page.getByRole('button', { name: 'Upload' }).click();
    await expect(dialogOf(page).getByTestId('avatar-error')).toBeVisible();
    await dialogOf(page).getByTestId('avatar-close').click();
    await expect(dialogOf(page)).toBeHidden();
  });
});

test.describe('the avatar without scripts', () => {
  const me = freshEmail('avatar-noscript');
  test.use({ ...asIdentity(me), javaScriptEnabled: false });

  test('links to an upload page, then the crop page, then back to the profile', async ({ page }) => {
    await page.goto('/profile');
    await page.getByTestId('profile-avatar').click();
    await expect(page).toHaveURL(/\/profile\/[A-Za-z0-9_-]{22}\/avatar\/upload$/);
    await page.locator('input[name=avatar]').setInputFiles(fixture('gps-tagged.jpg'));
    await page.getByRole('button', { name: 'Upload' }).click();
    await expect(page).toHaveURL(/\/avatar\/crop$/);
    await page.getByTestId('crop-save').click();
    await expect(page).toHaveURL(/\/profile\/[A-Za-z0-9_-]{22}$/);
  });
});

test.describe('who may fetch an avatar', () => {
  const owner = freshEmail('avatar-owner');
  let path = '';
  let src = '';

  test.beforeAll(async ({ browser, baseURL }) => {
    test.setTimeout(90_000);
    const ctx = await browser.newContext({ baseURL, ...asIdentity(owner) });
    const page = await ctx.newPage();
    await upload(page, fixture('gps-tagged.jpg'));
    await cropAndSave(page);
    path = new URL(page.url()).pathname;
    await expect(page.getByTestId('profile-avatar-img')).toBeVisible({ timeout: 30_000 });
    src = (await page.getByTestId('profile-avatar-img').getAttribute('src'))!;
    await ctx.close();
  });

  test('a member of another household gets it by the profile address, where the visibility rule allows', async ({ playwright, baseURL }) => {
    const ctx = await playwright.request.newContext({ baseURL, ...asIdentity(OTHER) });
    const res = await ctx.get(src);
    expect(res.status()).toBe(200);
    expect(res.headers()['content-type']).toBe('image/webp');
    // By address alone: the storage key and a guessed address give nothing.
    expect((await ctx.get(`/profile/${'A'.repeat(22)}/avatar`)).status()).toBe(404);
    expect((await ctx.get('/media/avatars/x.webp')).status()).toBe(404);
    await ctx.dispose();
  });

  test('only the owner can reach the upload, upload page, crop and preview', async ({ playwright, baseURL }) => {
    const ctx = await playwright.request.newContext({ baseURL, ...asIdentity(OTHER) });
    expect((await ctx.get(`${path}/avatar/upload`)).status()).toBe(404);
    expect((await ctx.get(`${path}/avatar/crop`)).status()).toBe(404);
    expect((await ctx.get(`${path}/avatar/status`)).status()).toBe(404);
    expect((await ctx.get(`${path}/avatar/preview/${'A'.repeat(22)}`)).status()).toBe(404);
    expect((await ctx.post(`${path}/avatar`, { multipart: { avatar: { name: 'a.jpg', mimeType: 'image/jpeg', buffer: Buffer.from('x') } } })).status()).toBe(404);
    await ctx.dispose();
  });

  test('a visitor with no session is refused', async ({ playwright }) => {
    const ctx = await playwright.request.newContext({ baseURL: process.env.GOBBLER_E2E_ANONYMOUS_URL });
    const res = await ctx.get(`${path}/avatar`, { maxRedirects: 0 });
    expect(res.status()).not.toBe(200);
    await ctx.dispose();
  });
});

// Screenshots for review, written only when SHOTS_DIR is set.
test.describe('screenshots', () => {
  test.skip(!process.env.SHOTS_DIR, 'set SHOTS_DIR to write screenshots');
  const me = freshEmail('avatar-shots');
  test.use(asIdentity(me));

  test('profile and crop pages, light and dark, laptop and phone', async ({ page }) => {
    test.setTimeout(120_000);
    const dir = process.env.SHOTS_DIR!;
    mkdirSync(dir, { recursive: true });
    const sizes = [
      ['1024x768', { width: 1024, height: 768 }],
      ['390x844', { width: 390, height: 844 }],
    ] as const;
    const shoot = async (name: string, prepare?: () => Promise<void>) => {
      for (const [label, viewport] of sizes) {
        await page.setViewportSize(viewport);
        for (const mode of ['light', 'dark']) {
          await page.evaluate((m) => localStorage.setItem('gob-mode', m), mode);
          await page.reload();
          await page.waitForTimeout(300);
          if (prepare) await prepare();
          await page.screenshot({ path: join(dir, `avatar-${name}-${label}-${mode}.png`), fullPage: true });
        }
      }
    };
    await page.goto('/profile');
    await openDialog(page);
    await page.locator('input[name=avatar]').setInputFiles(fixture('gps-tagged.jpg'));
    await page.getByRole('button', { name: 'Upload' }).click();
    await waitForCrop(page);
    // The crop step is in the dialog, which a reload closes: each shot opens it again.
    const resumeCrop = async () => {
      await openDialog(page);
      await dialogOf(page).getByTestId('avatar-status').getByRole('link', { name: 'Choose the square to keep' }).click();
      await waitForCrop(page);
    };
    await shoot('crop', resumeCrop);
    await page.reload();
    await resumeCrop();
    await page.getByTestId('crop-save').click();
    await expect(page.getByTestId('profile-avatar-img')).toBeVisible({ timeout: 30_000 });
    await shoot('profile');
  });
});
