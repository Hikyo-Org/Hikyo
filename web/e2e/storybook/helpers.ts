import { expect, type Frame, type Page } from '@playwright/test';

export type Palette = 'dark' | 'light';

declare global {
  interface Window {
    // Readiness only: the installed Storybook preview exposes its active
    // renders here. Fail visibly if that surface changes on an upgrade.
    __STORYBOOK_PREVIEW__?: { storyRenders: readonly { id: string; phase: string }[] };
  }
}

export async function waitStoryReady(frame: Frame, storyId: string): Promise<void> {
  await expect.poll(() => frame.evaluate((id) => window.__STORYBOOK_PREVIEW__?.storyRenders
    .find((render) => render.id === id)?.phase, storyId)).toBe('finished');
  await frame.evaluate(() => document.fonts.ready);
}

export async function openStorybook(page: Page, path: string): Promise<Frame> {
  await page.goto(`?path=${path}`);
  const preview = page.locator('#storybook-preview-iframe');
  await expect(preview).toBeVisible();
  const element = await preview.elementHandle();
  const frame = await element?.contentFrame();
  if (!frame) throw new Error('Storybook preview frame did not mount');
  await frame.waitForLoadState('domcontentloaded');
  if (path.startsWith('/story/')) await waitStoryReady(frame, path.slice('/story/'.length));
  else await frame.evaluate(() => document.fonts.ready);
  return frame;
}

export async function chooseTheme(page: Page, palette: Palette): Promise<void> {
  const toolbar = page.getByRole('button', { name: /^App theme / });
  const label = palette === 'light' ? 'Light' : 'Dark';
  if (await toolbar.getAttribute('aria-label') === `App theme ${label}`) return;
  await toolbar.click();
  await page.getByRole('listbox', { name: 'App theme', exact: true }).getByRole('option').filter({ hasText: label }).click();
  await expect(toolbar).toHaveAttribute('aria-label', `App theme ${label}`);
}

export async function expectTheme(frame: Frame, palette: Palette): Promise<void> {
  await expect(frame.locator('html')).toHaveAttribute('data-theme', palette);
  // Attribute propagation alone misses competing Storybook background styles.
  // Resolve the actual app token using this frame's cascade, then compare paint.
  await expect.poll(() => frame.evaluate(() => {
    const probe = document.createElement('span');
    probe.style.backgroundColor = 'var(--bg)';
    document.body.append(probe);
    const expected = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return getComputedStyle(document.body).backgroundColor === expected;
  })).toBe(true);
}

export async function storyFrame(docs: Frame, storyId: string): Promise<Frame> {
  const locator = docs.locator(`iframe[id="iframe--${storyId}"]`).first();
  await expect(locator).toBeVisible();
  // Docs frames use native lazy loading. Visibility alone includes examples
  // below the viewport; bring this example into view before awaiting its load.
  await locator.scrollIntoViewIfNeeded();
  const element = await locator.elementHandle();
  const frame = await element?.contentFrame();
  if (!frame) throw new Error(`Docs story frame missing: ${storyId}`);
  await frame.waitForLoadState('domcontentloaded');
  await waitStoryReady(frame, storyId);
  return frame;
}
