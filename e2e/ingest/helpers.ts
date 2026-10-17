import type { Page } from '@playwright/test';

// The add page shows one source field at a time (docs/ui/design.md): choose
// the paste toggle, then fill the text.
export async function pasteText(page: Page, text: string): Promise<void> {
  await page.locator('label[for=src-text]').click();
  await page.getByLabel('Recipe text').fill(text);
}
