import { expect } from '@playwright/test';
import { zLoginResult, zWhoAmI } from '@hikyo/zod';

import { expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import {
  ADMIN,
  BASE_URL,
  nextTotpCode,
  OIDC_PROVIDER,
  STORAGE_STATE,
} from '../fixtures/instance.ts';
import { test } from '../fixtures/passkey.ts';

/**
 * Flow: account & security (registry surface `settings`), mvp-boundary S3's
 * "account & security", against the locked prototype #29 iteration 15
 * (Profile · Sign-in factors · Recovery · Active sessions · Linked identities
 * · Preferences).
 *
 * The surface absorbed #71's standalone session list, so the kill switch is
 * asserted here as one panel of the account rather than as a page of its own.
 *
 * What this flow does NOT do, deliberately: enrol or remove a factor. Every
 * one of those is an account-security mutation that advances the principal's
 * session generation and deletes every other session the account holds, and
 * this suite has exactly ONE administrator per instance, whose TOTP seed and
 * whose passkey are the fixture every other flow's ceremonies stand on.
 * Removing the authenticator would break the TOTP ledger for the whole run.
 * The affordances are asserted as affordances, the proof rule is asserted
 * through a real refusal, and the ONE mutation that is safe to make, a
 * recovery-code batch, which nothing else in the suite reads, is made last
 * and followed by re-minting the shared session it invalidates.
 */

test.describe('account and security', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test.beforeEach(async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: 'Account & security', level: 1 })).toBeVisible();
  });

  test('shows the account as it is, including whether a factor stands', async ({ page }) => {
    // Profile is read-only because nothing writes it: no operation anywhere in
    // the contract sets a display name or an email.
    await expect(page.locator('#account-profile input')).toHaveCount(2);
    for (const field of await page.locator('#account-profile input').all()) {
      await expect(field).toHaveJSProperty('readOnly', true);
    }

    // Passkeys are listable, so they are listed. The authenticator factor is
    // now readable too: this suite's administrator has a confirmed one, and the
    // panel reports it rather than disclaiming knowledge.
    const factors = page.locator('#account-factors');
    await expect(factors.getByRole('button', { name: 'Add a passkey' })).toBeVisible();
    await expect(factors.getByRole('button', { name: 'Remove the authenticator' })).toContainText(
      'enrolled',
    );
    await expect(factors.locator('.account-passkey').first()).toContainText('added');

    // The kill switch, absorbed from #71: this session is in the list.
    const sessions = page.locator('#account-sessions');
    await expect(sessions.getByRole('listitem').first()).toContainText('browser');
    await expect(
      sessions.getByRole('button', { name: /^Revoke the browser session/ }).first(),
    ).toBeVisible();

    // The OIDC disclosure fixture configures and links one real provider. The
    // account surface must report that live state and retain the link affordance
    // for the configured provider instead of rendering the old empty claim.
    const identities = page.locator('#account-identities');
    await expect(identities.getByRole('listitem').first()).toContainText('subject user');
    await expect(
      identities.getByRole('button', { name: `Link ${OIDC_PROVIDER.displayName}` }),
    ).toBeVisible();
  });

  test('keeps factor and provider empty claims hidden while their reads are pending', async ({
    page,
  }) => {
    let release: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    for (const path of ['/api/v1/auth/webauthn/credentials', '/api/v1/auth/methods']) {
      await page.route(
        (url) => url.pathname === path,
        async (route) => {
          await gate;
          await route.continue();
        },
      );
    }
    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(page.getByRole('status').filter({ hasText: 'Loading passkeys' })).toBeVisible();
    await expect(page.getByRole('status').filter({ hasText: 'Loading configured identity providers' })).toBeVisible();
    await expect(page.locator('#account-factors')).not.toContainText('No passkey is enrolled');
    await expect(page.locator('#account-identities')).not.toContainText('none enabled');
    if (release === undefined) {
      throw new Error('the account-factor gate was not installed');
    }
    release();
    await expect(page.getByRole('status').filter({ hasText: 'Loading passkeys' })).toHaveCount(0);
  });

  test('asks for the credential you already have before an account change', async ({ page }) => {
    await page.getByRole('button', { name: 'Add a passkey' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByRole('heading', { name: 'Confirm it is you' })).toBeVisible();
    await expect(dialog).toContainText('the new one never authorises itself');
    // Escape is the platform's, and it must reach the caller rather than
    // leaving the page with an invisible open dialog.
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog')).toBeHidden();
  });

  test('reports a refused proof honestly, and changes nothing', async ({ page }) => {
    // One attempt, against the real server: removing the authenticator is
    // proved by the PASSWORD, never by the code, so a wrong password is
    // refused and the factor stands. There is no per-account lockout to trip.
    await page.getByRole('button', { name: 'Remove the authenticator' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Password').fill(`${ADMIN.password}-wrong`);
    await dialog.getByRole('button', { name: 'Confirm' }).click();

    const inlineAlert = page
      .locator('#content')
      .getByRole('alert')
      .filter({ hasText: 'did not authorise the change' });
    await expect(inlineAlert).toContainText('did not authorise the change');
    // The toast node is presentational; the announcement lives in the
    // app-level visually-hidden assertive region, so both halves are pinned.
    const toast = page.locator('.toast').filter({ hasText: 'did not authorise the change' });
    await expect(toast).toBeInViewport();
    await expect(
      page
        .locator('.visually-hidden[role="alert"]')
        .filter({ hasText: 'did not authorise the change' }),
    ).toContainText('did not authorise the change');
    // Never presented as a bare "wrong password": a 401 here is either a
    // refused proof or an ended session, and the sentence covers both without
    // guessing which.
    await expect(inlineAlert).toContainText('or this session has ended');
  });

  test('keeps the theme choice explicit and local', async ({ page }) => {
    const theme = page.locator('#account-preferences').getByLabel('Theme');
    const headerToggle = page.getByRole('button', { name: /theme/i });
    await theme.selectOption('light');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    // The choice is one piece of shared state: choosing it in Preferences moves
    // the header toggle in the same document, with no reload. Painting light
    // makes the toggle offer dark.
    await expect(headerToggle).toHaveAccessibleName('Switch to dark theme');
    await theme.selectOption('dark');
    await expect(headerToggle).toHaveAccessibleName('Switch to light theme');
    await page.reload();
    await expect(theme).toHaveValue('dark');
    await theme.selectOption('system');
    await expect(page.locator('html')).not.toHaveAttribute('data-theme', 'dark');
  });

  test('revokes a second browser session without killing the current one', async ({ browser, page }) => {
    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    try {
      const second = await context.newPage();
      await second.goto(BASE_URL);
      const login = await second.request.post(`${BASE_URL}/api/v1/auth/local/login`, {
        data: { username: ADMIN.username, password: ADMIN.password, artifact: 'browser' },
      });
      expect(login.status()).toBe(200);
      const minted = zLoginResult.parse(await login.json());

      await page.reload();
      const row = page.locator('.session').filter({ hasText: minted.session.id });
      await expect(row).toBeVisible();
      await row.getByRole('button', { name: new RegExp(minted.session.id) }).click();
      await expect(
        page.getByRole('status').filter({ hasText: `Revoked the browser session ${minted.session.id}` }),
      ).toBeVisible();
      await expect(row).toHaveCount(0);

      const refused = await second.request.get(`${BASE_URL}/api/v1/auth/whoami`);
      expect(refused.status()).toBe(401);
      const current = await page.request.get(`${BASE_URL}/api/v1/auth/whoami`);
      expect(current.status()).toBe(200);
      zWhoAmI.parse(await current.json());
    } finally {
      await context.close();
    }
  });

  for (const scheme of ['dark', 'light'] as const) {
    test(`meets the pinned assertion set on account & security (${scheme})`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      try {
        const heading = page.getByRole('heading', { name: 'Account & security', level: 1 });
        const well = page.locator('.panel').first();
        const add = page.getByRole('button', { name: 'Add a passkey' });
        const badge = page.locator('.badge').first();

        await expectPinnedAssertionSet(page, {
          flow: 'account',
          surface: 'settings',
          theme: scheme,
          text: [heading, page.locator('.factor__meta').first(), page.locator('.page__lede')],
          radii: [
            [well, 'container'],
            [add, 'control'],
            [badge, 'badge'],
          ],
          fonts: [
            [heading, 'ui'],
            [page.locator('.factor__meta').first(), 'mono'],
          ],
          colours: [
            [heading, 'color', '--tx'],
            [well, 'backgroundColor', '--bg-panel'],
            [well, 'borderTopColor', '--panel-line'],
          ],
          hairlines: [well],
          density: [],
        });
      } finally {
        await page.emulateMedia({ colorScheme: null });
      }
    });
  }

  /**
   * LAST except recovery codes: factor mutations advance the account session
   * generation. The refresh restores the shared suite state before the final
   * display-once drill.
   */
  test.describe('passkey mutation', () => {
    test.use({ passkeyCredential: 'empty' });

    test('reports the existing TOTP factor and enrols then removes an additional passkey', async ({ passkeyPage: page }) => {
      // The factor state is now readable: the suite's administrator has a
      // confirmed authenticator, and the panel reports it rather than
      // disclaiming knowledge.
      await expect(
        page.getByRole('button', { name: 'Remove the authenticator' }),
      ).toContainText('enrolled');
      await expect(page.getByRole('button', { name: 'enrol' })).toHaveCount(0);
      await expect(page.locator('.enrolment')).toHaveCount(0);

      const rows = page.locator('#account-factors .account-passkey');
      const before = await rows.count();
      await page.getByRole('button', { name: 'Add a passkey' }).click();
      const addProof = page.getByRole('dialog');
      await addProof.getByLabel('Password').fill(ADMIN.password);
      await addProof.getByRole('button', { name: 'Confirm' }).click();
      await expect(rows).toHaveCount(before + 1);
      await expect(page.getByRole('status').filter({ hasText: 'Passkey enrolled' })).toBeVisible();

      const added = rows.last();
      await expect(added).toContainText('added');
      await added.getByRole('button', { name: /Remove passkey/ }).click();
      const removeProof = page.getByRole('dialog');
      await removeProof.getByLabel('Password').fill(ADMIN.password);
      await removeProof.getByRole('button', { name: 'Confirm' }).click();
      await expect(rows).toHaveCount(before);
      await expect(page.getByRole('status').filter({ hasText: 'Passkey removed' })).toBeVisible();
    });
  });

  /**
   * LAST in this file, and it must stay last.
   *
   * Regenerating the batch is an account-security mutation: it reissues the
   * acting session and deletes every other session this principal holds, the
   * suite's shared storage state included. The re-mint at the end is what
   * leaves the run as it was found; every later test builds its context from
   * that file.
   */
  test('replaces the recovery codes and shows them exactly once', async ({ passkeyPage: page }) => {
      await page.getByRole('button', { name: 'Replace recovery codes' }).click();
      const proof = page.getByRole('dialog');
      await expect(proof).toContainText('never authorise their own regeneration');
      // The proof is a code from the authenticator where one stands, which it
      // does, and the ledger hands out a step nothing has spent.
      await proof.getByLabel('Code or password').fill(await nextTotpCode());
      await proof.getByRole('button', { name: 'Confirm' }).click();

      const codes = page.getByRole('dialog');
      await expect(codes.getByRole('heading', { name: 'Your new recovery codes' })).toBeVisible();
      await expect(codes).toContainText('Shown once');
      const listed = codes.getByRole('list', { name: 'Recovery codes' }).getByRole('listitem');
      expect(await listed.count()).toBeGreaterThan(0);
      const captured = await listed.allTextContents();

      await page.keyboard.press('Escape');
      await expect(codes).toBeVisible();
      await expect(codes.getByRole('alert')).toContainText('cannot be dismissed yet');

      // The dismiss is gated on the acknowledgement: these exist in exactly
      // this response and nowhere else, hashes afterwards.
      const done = codes.getByRole('button', { name: 'Done' });
      await expect(done).toBeDisabled();
      await codes.getByRole('checkbox').check();
      await done.click();
      await expect(page.getByRole('dialog')).toBeHidden();

      const status = page.locator('.notice').filter({ hasText: 'shown exactly once' });
      await expectStatusIsTextAndAria(page, status);

      await page.reload();
      for (const code of captured) {
        await expect(page.locator('body')).not.toContainText(code.trim());
      }
  });
});
