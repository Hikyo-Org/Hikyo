import assert from 'node:assert/strict';

/**
 * Check settings placement and consent controls in the built site across themes
 * and viewport sizes. Analytics-disabled builds skip these checks.
 */
export async function checkAnalyticsLayout(browser, origin) {
  for (const width of [320, 390, 768, 1280]) {
    for (const theme of ['dark', 'light']) {
      const context = await browser.newContext({ viewport: { width, height: 844 } });
      try {
        await context.addInitScript((theme) => {
          localStorage.setItem('hikyo.analytics.consent', 'denied');
          localStorage.setItem('hikyo-theme', theme);
        }, theme);
        const page = await context.newPage();
        for (const route of ['/docs/getting-started/', '/']) {
          await page.goto(`${origin}${route}`, { waitUntil: 'networkidle' });
          const settings = page.getByRole('button', { name: 'Analytics settings', exact: true });
          if (await settings.count() === 0) {
            assert.notEqual(process.env.POSTHOG_REQUIRED, 'true', 'required analytics control is missing');
            console.log('analytics layout: skipped because this build disables analytics');
            return;
          }
          assert.equal(await page.locator('html').evaluate((element) => element.classList.contains('dark')),
            theme === 'dark', 'requested theme must be active');

          await settings.scrollIntoViewIfNeeded();
          const settingsBox = await settings.boundingBox();
          const contentBox = await page.locator('main').boundingBox();
          assert.ok(settingsBox && contentBox, 'settings and page content must have layout boxes');
          assert.ok(settingsBox.y >= contentBox.y + contentBox.height,
            `${route} ${width}px ${theme}: analytics settings must follow page content`);
          assert.ok(settingsBox.height >= 44, 'analytics settings must have a 44px touch target');
          assert.ok(await settings.evaluate((element) => {
            const box = element.getBoundingClientRect();
            return element.contains(document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2));
          }), 'analytics settings must be reachable at the end of the page');
          await settings.click();
          const consent = page.getByRole('complementary', { name: 'Optional site analytics' });
          await consent.waitFor({ state: 'visible' });
          await page.evaluate(() => localStorage.removeItem('hikyo.analytics.consent'));
          await consent.getByRole('button', { name: 'No thanks', exact: true }).click();
          await consent.waitFor({ state: 'hidden' });
          assert.equal(await page.evaluate(() => localStorage.getItem('hikyo.analytics.consent')), 'denied',
            'refusal must be saved');

          if (route.startsWith('/docs/') && width < 768) {
            await page.evaluate(() => window.scrollTo(0, 0));
            await page.getByRole('button', { name: 'Open Sidebar', exact: true }).click();
            const sidebar = page.locator('#nd-sidebar-mobile');
            await sidebar.waitFor({ state: 'visible' });
            const drawerBox = await sidebar.boundingBox();
            const openSettingsBox = await settings.boundingBox();
            assert.ok(drawerBox && openSettingsBox, 'drawer and settings must have layout boxes');
            assert.ok(openSettingsBox.y >= drawerBox.y + drawerBox.height
              || openSettingsBox.y + openSettingsBox.height <= drawerBox.y
              || openSettingsBox.x >= drawerBox.x + drawerBox.width
              || openSettingsBox.x + openSettingsBox.width <= drawerBox.x,
            'analytics settings must not cover the mobile sidebar');
            await sidebar.getByRole('button', { name: 'Toggle Theme', exact: true }).click();
            await page.waitForFunction((dark) => document.documentElement.classList.contains('dark') === dark,
              theme !== 'dark');
            await sidebar.getByRole('button', { name: 'Close Sidebar', exact: true }).click();
            await sidebar.waitFor({ state: 'hidden' });
          }
        }
      } finally {
        await context.close();
      }
    }
  }
  console.log('analytics layout: page footer, consent controls and mobile navigation passed at 320/390/768/1280px in both themes');
}
