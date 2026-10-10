import { expect, test } from '@playwright/test';

import { chooseTheme, expectTheme, openStorybook, storyFrame } from './helpers.ts';

test('toolbar themes reach Button Canvas and inline Docs, with live controls and navigation cleanup', async ({ page }) => {
  const canvas = await openStorybook(page, '/story/ui-button--secondary');
  const button = canvas.getByRole('button', { name: 'Save changes', exact: true });
  await expect(button).toBeVisible();
  await chooseTheme(page, 'light');
  await expectTheme(canvas, 'light');
  const light = await button.evaluate((element) => getComputedStyle(element).backgroundColor);
  await chooseTheme(page, 'dark');
  await expectTheme(canvas, 'dark');
  expect(await button.evaluate((element) => getComputedStyle(element).backgroundColor)).not.toBe(light);

  const docs = await openStorybook(page, '/docs/ui-button--docs');
  await expect(docs.getByRole('heading', { name: 'Button', exact: true }).first()).toBeVisible();
  const example = docs.locator('#story--ui-button--secondary--primary');
  await expect(example.getByRole('button', { name: 'Save changes', exact: true })).toBeVisible();
  await chooseTheme(page, 'light');
  await expectTheme(docs, 'light');
  await expect(docs.locator('html')).toHaveAttribute('data-storybook-docs-theme', 'light');
  await chooseTheme(page, 'dark');
  await expectTheme(docs, 'dark');
  const control = docs.locator('tr').filter({ has: docs.getByText('children', { exact: true }) }).getByRole('textbox');
  await control.fill('Theme contract button');
  await expect(example.getByRole('button', { name: 'Theme contract button', exact: true })).toBeVisible();

  // The same preview document retires its Docs owner before rendering Canvas.
  await page.getByRole('link', { name: 'Secondary', exact: true }).click();
  await expect(page).toHaveURL(/path=\/story\/ui-button--secondary/);
  await expect(docs.locator('html')).not.toHaveAttribute('data-storybook-docs-theme');
  await chooseTheme(page, 'light');
  await expectTheme(docs, 'light');
  await expect(docs.getByRole('button', { name: 'Theme contract button', exact: true })).toBeVisible();
});

test('API Docs keep independent fixtures, frames and unsaved form state through toolbar roundtrips', async ({ page }) => {
  const docs = await openStorybook(page, '/docs/routes-projects--docs');
  await expect(docs.locator('iframe[id^="iframe--routes-projects--"]')).toHaveCount(4);
  const empty = await storyFrame(docs, 'routes-projects--empty');
  const populated = await storyFrame(docs, 'routes-projects--populated');
  const failure = await storyFrame(docs, 'routes-projects--load-error');
  const input = empty.getByRole('textbox', { name: 'Project name', exact: true });
  await expect(input).toBeVisible();
  await expect(populated.getByText('billing', { exact: true })).toBeVisible();
  await expect(failure.getByText(/projects could not be loaded/i)).toBeVisible();
  await expect(empty.getByText(/no projects yet/i)).toBeVisible();
  await expect.poll(() => docs.evaluate(() => [...document.querySelectorAll('iframe[id^="iframe--routes-projects--"]')]
    .map((frame) => frame instanceof HTMLIFrameElement ? frame.contentDocument?.documentElement.dataset.theme : undefined)))
    .toEqual(['dark', 'dark', 'dark', 'dark']);
  await input.fill('Unsaved theme contract draft');
  await input.focus();
  await expect(input).toBeFocused();
  const identities = await docs.evaluateHandle(() => [...document.querySelectorAll('iframe')].map((frame) => ({
    frame,
    input: frame.contentDocument?.querySelector('input'),
    fetch: frame.contentWindow?.fetch,
  })));
  for (const palette of ['light', 'dark', 'light']) {
    if (palette !== 'light' && palette !== 'dark') throw new Error('Unsupported palette');
    await chooseTheme(page, palette);
    await expectTheme(docs, palette);
    await expectTheme(empty, palette);
    await expectTheme(populated, palette);
    await expectTheme(failure, palette);
    await expect.poll(() => docs.evaluate(() => [...document.querySelectorAll('iframe[id^="iframe--routes-projects--"]')]
      .map((frame) => frame instanceof HTMLIFrameElement ? frame.contentDocument?.documentElement.dataset.theme : undefined)))
      .toEqual([palette, palette, palette, palette]);
    await expect(input).toHaveValue('Unsaved theme contract draft');
    expect(await identities.evaluate((before) => before.every((entry, index) => {
      const frame = document.querySelectorAll('iframe')[index];
      return frame === entry.frame && frame?.contentDocument?.querySelector('input') === entry.input && frame?.contentWindow?.fetch === entry.fetch;
    }))).toBe(true);
    await expect(populated.getByText('billing', { exact: true })).toBeVisible();
    await expect(empty.getByText(/no projects yet/i)).toBeVisible();
    // Toolbar interaction moves focus to the manager. The original input stays
    // reachable and can regain focus without reinitializing its form.
    await input.focus();
    await expect(input).toBeFocused();
  }
  await identities.dispose();
});

test('framed dialogs and keyboard popovers remain owned and reachable while themes change', async ({ page }) => {
  const docs = await openStorybook(page, '/docs/ui-dialog--docs');
  const decision = await storyFrame(docs, 'ui-dialog--decision');
  const dialog = decision.getByRole('dialog', { name: 'Revoke this connection?', exact: true });
  await expect(dialog).toBeVisible();
  await expect(docs.getByRole('dialog')).toHaveCount(0);
  await chooseTheme(page, 'light');
  await expectTheme(decision, 'light');
  await expect(dialog.getByRole('button', { name: 'Keep it', exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Keep it', exact: true }).focus();
  await expect(dialog.getByRole('button', { name: 'Keep it', exact: true })).toBeFocused();
  await chooseTheme(page, 'dark');
  await expectTheme(decision, 'dark');
  await expect(dialog).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(dialog.getByRole('button', { name: 'Revoke connection', exact: true })).toBeVisible();
  const bounds = await dialog.boundingBox();
  if (bounds === null) throw new Error('Narrow dialog did not render');
  expect(bounds.width).toBeLessThanOrEqual(390);
  await page.setViewportSize({ width: 1280, height: 900 });

  const menuDocs = await openStorybook(page, '/docs/ui-menu--docs');
  const menu = await storyFrame(menuDocs, 'ui-menu--default');
  const sibling = await storyFrame(menuDocs, 'ui-menu--disabled-skipped');
  const opens = await storyFrame(menuDocs, 'ui-menu--opens');
  await expect(opens.getByRole('button', { name: 'Row actions', exact: true })).toHaveAttribute('aria-expanded', 'false');
  await expect(opens.getByRole('menu')).toBeHidden();
  await expect(sibling.getByRole('button', { name: 'Row actions', exact: true })).toBeVisible();
  const trigger = menu.getByRole('button', { name: 'Row actions', exact: true });
  await trigger.focus();
  await trigger.press('Enter');
  await expect(menu.getByRole('menuitem', { name: 'Rename', exact: true })).toBeFocused();
  await chooseTheme(page, 'light');
  await expectTheme(menu, 'light');
  await expect(menu.getByRole('menuitem', { name: 'Rename', exact: true })).toBeVisible();
  await expect(sibling.getByRole('button', { name: 'Row actions', exact: true })).toHaveAttribute('aria-expanded', 'false');
  await expect(sibling.getByRole('menu')).toBeHidden();
  await menu.getByRole('menuitem', { name: 'Rename', exact: true }).focus();
  await menu.getByRole('menuitem', { name: 'Rename', exact: true }).press('Escape');
  await expect(trigger).toBeFocused();
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
});

test('production ThemeToggle writes its shared persistent choice and fixture teardown clears it', async ({ page }) => {
  const canvas = await openStorybook(page, '/story/routes-themetoggle--default');
  const toggle = canvas.getByRole('button', { name: /switch to (dark|light) theme/i });
  await expect(toggle).toBeVisible();
  const label = await toggle.getAttribute('aria-label');
  const choice = label?.includes('light') ? 'light' : 'dark';
  await toggle.click();
  await expectTheme(canvas, choice);
  expect(await canvas.evaluate(() => localStorage.getItem('hikyo.theme'))).toBe(choice);
  await toggle.click();
  const opposite = choice === 'light' ? 'dark' : 'light';
  await expectTheme(canvas, opposite);
  expect(await canvas.evaluate(() => localStorage.getItem('hikyo.theme'))).toBe(opposite);
  await page.getByText('Button', { exact: true }).click();
  await page.getByRole('link', { name: 'Secondary', exact: true }).click();
  await expect(page).toHaveURL(/path=\/story\/ui-button--secondary/);
  await expect(canvas.getByRole('button', { name: 'Save changes', exact: true })).toBeVisible();
  await expect.poll(() => canvas.evaluate(() => localStorage.getItem('hikyo.theme'))).toBeNull();
});
