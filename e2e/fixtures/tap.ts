import { expect, type Page } from '@playwright/test';

/**
 * Asserts every control inside scope is at least --tap (44px) tall
 * (docs/ui/design.md): links, buttons, text inputs, selects, textareas,
 * disclosure summaries and the labels that carry a checkbox. A visually
 * hidden input (.sr), such as a picture picker's radio or file input, is
 * pressed through the label around it, so the label is measured in its place.
 * It fails when it finds none, so a scope that holds no controls cannot pass
 * by measuring nothing, and returns how many it measured.
 */
export async function expectTapTargets(page: Page, scope = 'main'): Promise<number> {
  const result = await page.locator(scope).evaluate((root) => {
    const controls = root.querySelectorAll(
      'a[href], button, input:not([type=hidden]):not([type=checkbox]), select, textarea, summary, label.check, label.inline',
    );
    const tooSmall: string[] = [];
    let measured = 0;
    for (const control of controls) {
      const el = control.matches('input.sr') ? (control.closest('label') ?? control) : control;
      const box = el.getBoundingClientRect();
      if (box.width === 0 && box.height === 0) continue; // not rendered
      measured++;
      if (box.height < 44) {
        tooSmall.push(`${el.tagName.toLowerCase()} "${(el.textContent ?? '').trim().slice(0, 30)}" is ${box.height}px tall`);
      }
    }
    return { measured, tooSmall };
  });
  expect(result.measured, `no controls found in ${scope}`).toBeGreaterThan(0);
  expect(result.tooSmall, 'controls shorter than --tap').toEqual([]);
  return result.measured;
}
