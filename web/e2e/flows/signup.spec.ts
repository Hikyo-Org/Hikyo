import { expect, test } from '@playwright/test';
import { zSignupRequest, zSignupVerifyRequest } from '@hikyo/zod';

import { expectNoSeriousAxeViolations, expectPinnedAssertionSet } from '../fixtures/assertions.ts';

/** UI contract exercise. Mail delivery and token authority are covered by Go lifecycle tests. */
test.describe('local sign-up UI contract', () => {
  test.use({ storageState: { cookies: [], origins: [] } });
  test('requests, resends and finishes fresh-org signup without a browser session', async ({ page }) => {
    const requests: { email: string; org?: string }[] = [];
    let currentToken = '';
    let verified = false;
    await page.route('**/api/v1/auth/methods*', (route) => route.fulfill({ json: {
      local_login_enabled: true, providers: [], signup_open: true, signup_paused: false,
      signup_methods: ['local'], signup_landing: 'fresh-org',
    } }));
    await page.route('**/api/v1/auth/signup', async (route) => {
      requests.push(zSignupRequest.parse(route.request().postDataJSON()));
      currentToken = `su_ui_${String(requests.length)}`;
      await route.fulfill({ status: 202, body: '' });
    });
    await page.route('**/api/v1/auth/signup/verify', async (route) => {
      const body = zSignupVerifyRequest.parse(route.request().postDataJSON());
      expect(body.token).toBe(currentToken);
      expect(body.display_name).toBe('Alex');
      expect(body.org_name).toBe('My organisation');
      expect(body.landing).toBe('fresh-org');
      verified = true;
      await route.fulfill({ status: 204, body: '' });
    });
    await page.goto('/signup');
    await page.getByRole('button', { name: 'Email', exact: true }).click();
    await page.getByLabel('Email', { exact: true }).fill('alex@example.com');
    await page.getByRole('button', { name: 'Send sign-up link' }).click();
    await expect(page.getByRole('heading', { name: 'Check your mail' })).toBeVisible();
    await expect(page.locator('.login__card')).toContainText('If this address can sign up');
    await page.getByRole('button', { name: 'Send it again' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Requested another link' })).toBeVisible();
    expect(requests).toEqual([{ email: 'alex@example.com' }, { email: 'alex@example.com' }]);

    for (const theme of ['dark', 'light'] as const) {
      await page.emulateMedia({ colorScheme: theme });
      await page.goto(`/signup/verify#token=${currentToken}&email=alex%40example.com&landing=fresh-org`);
      await expect(page).toHaveURL(/\/signup\/verify$/);
      const card = page.locator('.login__card');
      const heading = page.getByRole('heading', { name: 'Finish creating your account' });
      const email = page.getByLabel('Email', { exact: true });
      const submit = page.getByRole('button', { name: 'Create account', exact: true });
      await expect(email).toHaveValue('alex@example.com');
      await expect(email).toHaveAttribute('readonly', '');
      await expectPinnedAssertionSet(page, {
        flow: 'signup', surface: 'signup-verify', theme,
        text: [heading, page.locator('.login__landing')],
        radii: [[card, 'container'], [submit, 'control'], [email, 'control']],
        fonts: [[heading, 'ui']],
        colours: [[heading, 'color', '--tx'], [card, 'backgroundColor', '--bg-raise']],
        hairlines: [card, email], density: [[submit, '--control']],
      });
      await expectNoSeriousAxeViolations(page);
    }
    await page.getByLabel('Display name').fill('Alex');
    await page.getByLabel('Organisation name').fill('My organisation');
    await page.getByLabel('Password', { exact: true }).fill('twelvecharacters');
    await page.getByLabel('Repeat password').fill('differentpassword');
    await page.getByRole('button', { name: 'Create account', exact: true }).click();
    await expect(page.locator('.login__card').getByRole('alert')).toContainText('The two passwords differ');
    expect(verified).toBe(false);
    await page.getByLabel('Repeat password').fill('twelvecharacters');
    await page.getByRole('button', { name: 'Create account', exact: true }).click();
    await expect(page).toHaveURL(/\/login$/);
    expect(verified).toBe(true);
    expect((await page.context().cookies()).some((cookie) => cookie.name === '__Host-hikyo')).toBe(false);
  });
});
