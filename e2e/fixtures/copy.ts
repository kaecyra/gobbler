import type { Page } from '@playwright/test';

/**
 * Every word a person can read or hear on the page: the text of main, closed
 * disclosures and screen-reader-only text included, plus the attributes that
 * carry words (aria-label, title, placeholder, alt). Style and script bodies
 * are left out. For checks on wording, such as the editor never saying
 * "component" (docs/ui/design.md).
 */
export async function userCopy(page: Page): Promise<string> {
  return page.locator('main').evaluate((root) => {
    const clone = root.cloneNode(true) as HTMLElement;
    clone.querySelectorAll('style, script, datalist').forEach((el) => el.remove());
    const words = [clone.textContent ?? ''];
    root.querySelectorAll('[aria-label], [title], [placeholder], [alt]').forEach((el) => {
      for (const a of ['aria-label', 'title', 'placeholder', 'alt']) {
        const v = el.getAttribute(a);
        if (v) words.push(v);
      }
    });
    return words.join('\n');
  });
}
