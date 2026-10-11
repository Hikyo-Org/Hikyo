import { expect, test } from '@playwright/test';

// Exercise browser enforcement, not just the presence of a policy string.
// The route suite separately rejects console errors during normal interactions.
test('the static demo blocks injected inline scripts', async ({ page }) => {
  await page.goto('./');
  await expect(page.getByRole('heading', { name: 'Environment matrix', exact: true })).toBeVisible();
  const violation = await page.evaluate(() => new Promise<string>((resolve) => {
    document.addEventListener('securitypolicyviolation', (event) => resolve(event.violatedDirective), { once: true });
    const script = document.createElement('script');
    script.textContent = "document.body.dataset.demoInlineExecuted = 'yes'";
    document.body.append(script);
  }));
  expect(violation).toBe('script-src-elem');
  expect(await page.locator('body').getAttribute('data-demo-inline-executed')).toBeNull();
});
