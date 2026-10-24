import { createHash, randomBytes } from 'node:crypto';
import { createServer } from 'node:http';
import { expect, test, type Page, type Route } from '@playwright/test';

import {
  expectBoundaryContrast,
  expectContrast,
  expectNoSeriousAxeViolations,
  expectPinnedAssertionSet,
  expectStatusIsTextAndAria,
  measureSurfaceLuminance,
} from '../fixtures/assertions.ts';
import { zAuthMethods, zError, zLoginResult, zOrgList, zRegistrationPolicy, zTotpEnrolStartResult, zWhoAmI } from '@hikyo/zod';
import { z } from 'zod';

import { browserApi, fixtureApiCall } from '../fixtures/api.ts';
import {
  ADMIN,
  BASE_URL,
  OIDC_PROVIDER,
  WEBUI_OIDC,
  nextTotpCode,
  passEnrolmentGate,
  readSeed,
  STORAGE_STATE,
} from '../fixtures/instance.ts';
import { enrolledAccount, TotpLedger, TOTP_STEP_MS, withSharedAdmin } from '../fixtures/accounts.ts';
import { withPasskeyPage } from '../fixtures/passkey.ts';
import { readMail } from '../fixtures/mail.ts';

/** publicPost is an unauthenticated JSON POST, parsed at the boundary. */
async function publicPost<T>(path: string, body: unknown, schema: z.ZodType<T>): Promise<T> {
  const response = await fetch(`${BASE_URL}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw new Error(`POST ${path} answered ${String(response.status)}: ${await response.text()}`);
  }
  return schema.parse(response.status === 204 ? {} : await response.json());
}

async function expectLoginSurface(page: Page, theme: 'dark' | 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.goto('/login');

  // Step one of the staged entry (#587 locked, spec section 6): one row per
  // way in, no credential field yet. The e2e browser can assert, so the
  // passkey row stands beside the password row and the seeded provider. The
  // Password row is matched by prefix: after a sign-in its name carries the
  // "Last used" badge.
  const card = page.locator('.login__card');
  const heading = page.getByRole('heading', { name: 'Sign in to Hikyo' });
  const lede = page.getByText('Choose how you sign in.');
  const passwordRow = card.getByRole('button', { name: /^Password\b/ });
  const provider = card.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` });
  await expect(heading).toBeVisible();
  await expect(lede).toBeVisible();
  await expect(passwordRow).toBeVisible();
  await expect(card.getByRole('button', { name: 'Passkey', exact: true })).toBeVisible();
  await expect(provider).toBeVisible();
  await expect(card.getByLabel('Username')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Create an account' })).toHaveCount(0);

  await expectPinnedAssertionSet(page, {
    flow: 'login',
    surface: 'login',
    theme,
    text: [heading, lede],
    radii: [
      [card, 'container'],
      [passwordRow, 'control'],
      [provider, 'control'],
    ],
    fonts: [
      [heading, 'ui'],
      [lede, 'ui'],
    ],
    colours: [
      [heading, 'color', '--tx'],
      [lede, 'color', '--tx-dim'],
      [card, 'backgroundColor', '--bg-raise'],
      [card, 'borderTopColor', '--line'],
      // A generic provider row is the house secondary button.
      [provider, 'backgroundColor', '--bg-panel'],
    ],
    hairlines: [card, provider],
    // Method rows are buttons: `--control`, the touch floor on a coarse pointer.
    density: [[passwordRow, '--control'], [provider, '--control']],
  });

  // Step two: only the chosen method's form.
  await passwordRow.click();
  const submit = page.getByRole('button', { name: 'Sign in', exact: true });
  const username = page.getByLabel('Username');
  const password = page.getByLabel('Password');
  const stepHeading = page.getByRole('heading', { name: 'Sign in with a password' });
  const stepLede = page.getByText('Use the credential you established');
  // The way back is on the card, and it returns to the rows without a reload.
  await expect(card.getByRole('button', { name: 'Other ways to sign in' })).toBeVisible();

  await expectBoundaryContrast(page, username);
  await expectBoundaryContrast(page, password);

  await expectPinnedAssertionSet(page, {
    flow: 'login',
    surface: 'login',
    theme,
    text: [stepHeading, stepLede],
    radii: [
      [card, 'container'],
      [submit, 'control'],
      [username, 'control'],
      [password, 'control'],
    ],
    fonts: [
      [stepHeading, 'ui'],
      [stepLede, 'ui'],
    ],
    colours: [
      [stepHeading, 'color', '--tx'],
      [stepLede, 'color', '--tx-dim'],
      [card, 'backgroundColor', '--bg-raise'],
      [card, 'borderTopColor', '--line'],
      [submit, 'backgroundColor', '--accent'],
      [submit, 'color', '--on-accent'],
    ],
    hairlines: [card, username],
    density: [[submit, '--control']],
  });
}

async function expectOIDCDoneSurface(page: Page, theme: 'dark' | 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.goto('/auth/oidc/done?purpose=reauth');

  const card = page.locator('.login__card');
  const heading = page.getByRole('heading', { name: 'Returning from your identity provider' });
  // Scoped to the card: the app-level toast announcer holds role="alert" for
  // its whole lifetime (it must exist empty before an announcement lands), so
  // a page-wide alert query resolves two elements.
  const refusal = card.getByRole('alert');
  const close = page.getByRole('button', { name: 'Close this window' });
  await expect(refusal).toContainText('without an OIDC transaction');

  await expectPinnedAssertionSet(page, {
    flow: 'login',
    surface: 'oidc-done',
    theme,
    text: [heading, refusal],
    radii: [[card, 'container'], [close, 'control']],
    fonts: [[heading, 'ui']],
    colours: [
      [card, 'backgroundColor', '--bg-raise'],
      [card, 'borderTopColor', '--line'],
    ],
    hairlines: [card],
    density: [[close, '--control']],
  });
}

async function expectSAMLDoneSurface(page: Page, theme: 'dark' | 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.goto('/auth/saml/done?state=not-started');
  const card = page.locator('.login__card');
  const heading = page.getByRole('heading', { name: 'Returning from your identity provider' });
  const refusal = card.getByRole('alert');
  const back = card.getByRole('link', { name: 'Return to sign in' });
  await expect(refusal).toContainText('transaction is missing or already completed');
  await expect(back).toHaveAttribute('href', '/login');
  await expectPinnedAssertionSet(page, {
    flow: 'login', surface: 'saml-done', theme,
    text: [heading, refusal],
    radii: [[card, 'container'], [back, 'control']],
    fonts: [[heading, 'ui']],
    colours: [[card, 'backgroundColor', '--bg-raise'], [card, 'borderTopColor', '--line']],
    hairlines: [card],
    density: [[back, '--control']],
  });
}

async function expectEstablishSurface(page: Page, theme: 'dark' | 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.goto('/establish');

  const card = page.locator('.login__card');
  const submit = page.getByRole('button', { name: 'Establish password' });
  const authority = page.getByLabel('Setup authority');
  const password = page.getByLabel('New password');
  const repeat = page.getByLabel('Repeat the password');
  const heading = page.getByRole('heading', { name: 'Establish your credential' });
  const lede = page.getByText('Paste the setup authority you were handed');
  // The fixture's enabled OIDC provider (#610): token first, so it is barred
  // until an authority is pasted, but it is measured like any other row.
  const provider = page.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` });
  await expect(provider).toBeDisabled();

  await expectBoundaryContrast(page, authority);
  await expectBoundaryContrast(page, password);

  await expectPinnedAssertionSet(page, {
    flow: 'login',
    surface: 'establish-credential',
    theme,
    text: [heading, lede],
    radii: [
      [card, 'container'],
      [submit, 'control'],
      [authority, 'control'],
      [password, 'control'],
      [repeat, 'control'],
      [provider, 'control'],
    ],
    fonts: [
      [heading, 'ui'],
      [lede, 'ui'],
    ],
    colours: [
      [heading, 'color', '--tx'],
      [lede, 'color', '--tx-dim'],
      [card, 'backgroundColor', '--bg-raise'],
      [card, 'borderTopColor', '--line'],
      [submit, 'backgroundColor', '--accent'],
      [submit, 'color', '--on-accent'],
    ],
    hairlines: [card, authority, provider],
    density: [[submit, '--control'], [provider, '--control']],
  });
}

async function expectRecoverySurface(page: Page, theme: 'dark' | 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.goto('/establish?mode=recover');

  const card = page.locator('.login__card');
  const submit = page.getByRole('button', { name: 'Continue' });
  const username = page.getByLabel('Username');
  const code = page.getByLabel('Recovery code');
  const heading = page.getByRole('heading', { name: 'Recover your account' });
  const lede = page.getByText('One unused recovery code sets a new password');

  await expectBoundaryContrast(page, username);
  await expectBoundaryContrast(page, code);

  await expectPinnedAssertionSet(page, {
    flow: 'login',
    surface: 'establish-credential',
    theme,
    text: [heading, lede],
    radii: [
      [card, 'container'],
      [submit, 'control'],
      [username, 'control'],
      [code, 'control'],
    ],
    fonts: [
      [heading, 'ui'],
      [lede, 'ui'],
    ],
    colours: [
      [heading, 'color', '--tx'],
      [lede, 'color', '--tx-dim'],
      [card, 'backgroundColor', '--bg-raise'],
      [card, 'borderTopColor', '--line'],
      [submit, 'backgroundColor', '--accent'],
      [submit, 'color', '--on-accent'],
    ],
    hairlines: [card, username],
    density: [[submit, '--control']],
  });
}

/**
 * Flow: login (registry surface `login`).
 *
 * Covers the surface's whole job, refusal and success, and runs the pinned
 * assertion set over everything it touches.
 */

test.describe('login', () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`OIDC done page meets the pinned assertion set in ${theme} mode`, async ({ page }) => {
      await expectOIDCDoneSurface(page, theme);
    });
    test(`SAML done page meets the pinned assertion set in ${theme} mode`, async ({ page }) => {
      await expectSAMLDoneSurface(page, theme);
    });
  }
  test.beforeEach(async ({ context }) => {
    await context.clearCookies();
  });

  test('refuses a wrong credential in text and ARIA, not colour', async ({ page }) => {
    await page.goto('/login');
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();

    await page.getByRole('button', { name: /^Password\b/ }).click();
    await page.getByLabel('Username').fill(ADMIN.username);
    await page.getByLabel('Password').fill('not the password at all');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();

    // Scoped to the card for the same reason as the OIDC-done surface above:
    // the toast announcer is a second, always-present role="alert".
    const alert = page.locator('.login__card').getByRole('alert');
    await expectStatusIsTextAndAria(page, alert);
    // The refusal must not name which half was wrong: the server closes that
    // oracle deliberately and the UI must not reopen it.
    await expect(alert).toContainText('username and password');
    await expect(alert).not.toContainText(/unknown|no such|does not exist/i);

    // Still on the login page, cookie-free.
    await expect(page).toHaveURL(/\/login$/);
    expect(await page.context().cookies()).toEqual([]);
  });

  // An inactive registration policy (#606, #587 d3): the public page says
  // only "Sign-up is paused." and nothing about why. Real state: an instance
  // policy is opened on a provider seeded for the purpose, then the provider
  // is disabled, which makes the policy inactive (a failing precondition).
  // With registration closed there is no line at all.
  //
  // Its own instance operator, like the sign-up door's: three codes (step-up,
  // two proofs) on the shared administrator leave its ledger a step ahead of
  // the clock, and the next flow to draw from it waits out that step inside a
  // default budget. The fresh account spends four (enrol, step-up, two
  // proofs); the last two may each wait one boundary, which the budget below
  // covers.
  test('says only "Sign-up is paused." while registration is inactive', async ({ page, browser }, testInfo) => {
    testInfo.setTimeout(180_000);
    const provider = { slug: 'e2e-reg-paused', displayName: 'Registration Paused' };
    const policyPath = '/api/v1/instance/registration-policy';
    const providerPath = `/api/v1/instance/oidc-providers/${provider.slug}`;
    const providerBody = (enabled: boolean, rowVersion?: number) => ({
      display_name: provider.displayName,
      issuer: WEBUI_OIDC.issuer,
      client_id: 'e2e-reg-client',
      client_secret: 'e2e-reg-secret',
      scopes: 'openid email',
      enabled,
      row_version: rowVersion,
    });
    const operator = await enrolledAccount(browser, 'reg-paused-operator', 'instance');
    const admin = operator.bearer;
    const methods = async () => {
      const response = await fetch(`${BASE_URL}/api/v1/auth/methods`);
      return zAuthMethods.parse(await response.json());
    };

    await page.goto('/login');
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    await expect(page.getByText('Sign-up is paused.')).toHaveCount(0);
    try {
      const created = await fixtureApiCall(
        admin,
        'PUT',
        providerPath,
        z.object({ row_version: z.number().int().positive() }).passthrough(),
        providerBody(true),
      );
      await fixtureApiCall(admin, 'PUT', policyPath, zRegistrationPolicy, {
        external: [{ provider: { kind: 'oidc', slug: provider.slug } }],
        landing: { kind: 'none' },
        proof: await operator.ledger.next(),
      });
      expect((await methods()).signup_open).toBe(true);
      await fixtureApiCall(admin, 'PUT', providerPath, z.unknown(), providerBody(false, created.row_version));
      const door = await methods();
      expect(door.signup_open).toBe(false);
      expect(door.signup_paused).toBe(true);

      await page.reload();
      await expect(page.getByText('Sign-up is paused.', { exact: true })).toBeVisible();
      await expect(page.locator('main')).not.toContainText(/authority-lost|no longer holds|mailer|precondition|inactive|disabled/i);
    } finally {
      await fixtureApiCall(admin, 'DELETE', policyPath, z.unknown(), { proof: await operator.ledger.next() }).catch((error: unknown) => {
        if (!(error instanceof Error && error.message.includes('answered 404:'))) throw error;
      });
      await fixtureApiCall(admin, 'DELETE', providerPath, z.unknown()).catch((error: unknown) => {
        if (!(error instanceof Error && error.message.includes('answered 404:'))) throw error;
      });
    }
  });

  test('redirects an anonymous authenticated-route deep link to login', async ({ page }) => {
    await page.goto('/projects');

    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Organisations' })).toHaveCount(0);
  });

  test('presents the second factor after a password and establishes a browser session on cookies alone', async ({
    page,
  }) => {
    await page.goto('/login');
    await page.getByRole('button', { name: /^Password\b/ }).click();
    await page.getByLabel('Username').fill(ADMIN.username);
    await page.getByLabel('Password').fill(ADMIN.password);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();

    // ADMIN carries an enrolled authenticator, so the password answers a login
    // challenge, not a session (#760). The factor is never skippable: the code
    // field is presented and the session mints only once it is satisfied. This
    // is the `PasswordThenAuthenticator` flow the story asserts, end to end.
    await expect(page.getByRole('heading', { name: 'Present your second factor' })).toBeVisible();
    await page.getByLabel('Authenticator code').fill(await nextTotpCode());
    await page.getByRole('button', { name: 'Present code' }).click();

    // The org rail is desktop chrome, a phone reaches organisations through
    // the drawer, so the rail is `display:none` there. What proves the shell
    // came up at BOTH widths is the breadcrumb, which only the authenticated
    // chrome renders.
    await expect(page.getByRole('list', { name: 'Breadcrumb' })).toBeVisible();

    const cookies = await page.context().cookies();
    const session = cookies.find((c) => c.name === '__Host-hikyo');
    const csrf = cookies.find((c) => c.name === '__Host-hikyo-csrf');
    expect(session, 'no browser session cookie').toBeDefined();
    expect(session?.httpOnly, 'the session token is readable by script').toBe(true);
    expect(csrf, 'no synchronizer-token cookie').toBeDefined();
    expect(csrf?.httpOnly, 'the synchronizer token is unreachable to the SPA').toBe(false);

    // Nothing about the session is in storage: the whole point of the cookie
    // pair is that JavaScript holds no replayable credential.
    const stored = await page.evaluate(() => ({
      local: Object.entries(globalThis.localStorage),
      session: Object.entries(globalThis.sessionStorage),
    }));
    expect(JSON.stringify(stored)).not.toContain('hik_1_');
  });

  test('signs in with a passkey alone, the discoverable credential selecting the account', async ({
    page,
  }) => {
    // The shared passkey was enrolled in global setup through the same
    // credProps round trip the SPA runs: without it the server records the
    // credential as non-discoverable and refuses it at sign-in with a 401.
    await withPasskeyPage(page, 'shared', async (passkeyPage) => {
      await passkeyPage.goto('/login');
      await passkeyPage.getByRole('button', { name: 'Passkey', exact: true }).click();

      // A passkey is multi-factor on its own: no second step is presented.
      await expect(passkeyPage.getByRole('list', { name: 'Breadcrumb' })).toBeVisible();
      await expect(passkeyPage.getByRole('heading', { name: 'Present your second factor' })).toHaveCount(0);
      const cookies = await passkeyPage.context().cookies();
      expect(cookies.find((c) => c.name === '__Host-hikyo'), 'no browser session cookie').toBeDefined();
    });
  });

  test('keeps every login control accessible while an OIDC ceremony is pending', async ({
    page,
  }) => {
    const startPath = `**/api/v1/auth/oidc/${OIDC_PROVIDER.slug}/start`;
    let releaseStart: () => void = () => undefined;
    let startHandlerActive = false;
    let finishStartHandler: () => void = () => undefined;
    const pendingStart = new Promise<void>((resolve) => {
      releaseStart = resolve;
    });
    const startHandlerFinished = new Promise<void>((resolve) => {
      finishStartHandler = resolve;
    });
    const holdStart = async (route: Route) => {
      startHandlerActive = true;
      await pendingStart;
      try {
        await route.abort();
      } finally {
        finishStartHandler();
      }
    };
    await page.route(startPath, holdStart);

    try {
      await page.goto('/login');
      const oidc = page.getByRole('button', {
        name: `Continue with ${OIDC_PROVIDER.displayName}`,
      });
      await oidc.click();
      await expect(
        page.getByRole('button', { name: 'Contacting identity provider…' }),
      ).toBeDisabled();

      // Step one of the staged entry: the password row, the passkey row and
      // the seeded provider's row; no field until a method is chosen.
      const controls = page.locator('input, button');
      await expect(controls).toHaveCount(3);
      for (const control of await controls.all()) {
        await expect(control).toBeDisabled();
      }
      await expectNoSeriousAxeViolations(page);
    } finally {
      releaseStart();
      if (startHandlerActive) await startHandlerFinished;
      await page.unroute(startPath, holdStart);
    }
  });

  // The palette is a dual-theme palette, so conformance is a dual-theme claim:
  // the pinned set runs on the surface in both schemes.
  for (const scheme of ['dark', 'light'] as const) {
    test(`meets the pinned assertion set (${scheme})`, async ({ page }) => {
      await expectLoginSurface(page, scheme);
    });
  }

  // Credential establishment (#568, registry surface `establish-credential`):
  // the public page where an invitation or reset authority becomes a password.
  // The happy path, a real invitation claimed here and signed in with, is
  // the members flow's; this flow owns the page's own contract.
  test('reaches the establish page from the login card', async ({ page }) => {
    await page.goto('/login');
    await page.getByRole('link', { name: 'Have a setup authority? Establish your credential' }).click();
    await expect(page).toHaveURL(/\/establish$/);
    await expect(page.getByRole('heading', { name: 'Establish your credential' })).toBeVisible();
    await page.getByRole('link', { name: 'Back to sign in' }).click();
    await expect(page).toHaveURL(/\/login$/);
  });

  test('refuses mismatched passwords locally, before any request', async ({ page }) => {
    let requests = 0;
    const establishPath = '**/api/v1/auth/credential/establish';
    const count = async (route: Route) => {
      requests += 1;
      await route.continue();
    };
    await page.route(establishPath, count);
    try {
      await page.goto('/establish');
      await page.getByLabel('Setup authority').fill('hik_cea_not_a_real_authority_value');
      await page.getByLabel('New password').fill('a first password long enough');
      await page.getByLabel('Repeat the password').fill('a first password long enough, but not this');
      await page.getByRole('button', { name: 'Establish password' }).click();
      const alert = page.locator('.login__card').getByRole('alert');
      await expectStatusIsTextAndAria(page, alert);
      await expect(alert).toContainText('differ');
      expect(requests).toBe(0);
    } finally {
      await page.unroute(establishPath, count);
    }
  });

  test('answers an unknown authority uniformly', async ({ page }) => {
    await page.goto('/establish');
    await page.getByLabel('Setup authority').fill('hik_cea_not_a_real_authority_value');
    await page.getByLabel('New password').fill('a first password long enough');
    await page.getByLabel('Repeat the password').fill('a first password long enough');
    await page.getByRole('button', { name: 'Establish password' }).click();
    const alert = page.locator('.login__card').getByRole('alert');
    await expectStatusIsTextAndAria(page, alert);
    // Unknown, expired and spent are one sentence: the server closes that
    // oracle and the page must not reopen it.
    await expect(alert).toContainText("That authority can't be used.");
    await expect(alert).not.toContainText(/unknown|no such|does not exist|spent/i);
    expect(await page.context().cookies()).toEqual([]);
  });

  for (const scheme of ['dark', 'light'] as const) {
    test(`establish page meets the pinned assertion set (${scheme})`, async ({ page }) => {
      await expectEstablishSurface(page, scheme);
    });
    test(`recovery entry meets the pinned assertion set (${scheme})`, async ({ page }) => {
      await expectRecoverySurface(page, scheme);
    });
  }

  // Recovery-code sign-in (#571): a human who lost their second factor spends
  // one recovery code in the browser for a display-once authority, which is
  // handed straight into the establish form. The account is a fresh invitee
  // prepared over the API (invite, establish, sign in, regenerate codes with
  // the password as proof, since no factor stands yet) so the shared fixture
  // administrator is never touched.
  test('recovers an account with a code, sets a new password, and signs in', async ({ browser }, testInfo) => {
    const seed = readSeed();
    const username = `recover-${testInfo.project.name}-${Date.now().toString(36)}`;
    const firstPassword = 'the password that was lost with the phone';
    const newPassword = 'a brand new password chosen after recovery';

    // Invitation takes a stepped-up administrator session: the shared one,
    // because minting a fresh one spends a TOTP step (see the gate flow below).
    // The invite carries no template, so the account can sign in and see nothing.
    const adminContext = await browser.newContext({ storageState: STORAGE_STATE });
    const invitation = await browserApi(
      await adminContext.newPage(),
      'POST',
      `/api/v1/orgs/${seed.org}/invitations`,
      z.object({ authority: z.string(), principal_id: z.string() }),
      { username },
    ).finally(() => adminContext.close());
    await publicPost(
      '/api/v1/auth/credential/establish',
      { authority: invitation.authority, password: firstPassword },
      z.object({}),
    );
    const invitee = await publicPost(
      '/api/v1/auth/local/login',
      { username, password: firstPassword },
      z.object({ session_token: z.string() }),
    );
    const codes = await fixtureApiCall(
      invitee.session_token,
      'POST',
      '/api/v1/auth/recovery-codes/regenerate',
      z.object({ recovery_codes: z.array(z.string()).min(2) }),
      { proof: firstPassword },
    );
    const [code, spare] = codes.recovery_codes;
    expect(code).toBeDefined();
    expect(spare).toBeDefined();

    const context = await browser.newContext();
    try {
      const page = await context.newPage();
      await page.goto('/login');
      await page.getByRole('link', { name: 'Lost your second factor? Recover with a code' }).click();
      await expect(page).toHaveURL(/\/establish\?mode=recover$/);
      await expect(page.getByRole('heading', { name: 'Recover your account' })).toBeVisible();

      // Refusal is one sentence, whoever and whatever was wrong: a wrong code
      // for a real user and any code for an unknown user read the same.
      await page.getByLabel('Username').fill(username);
      await page.getByLabel('Recovery code').fill('not-a-code');
      await page.getByRole('button', { name: 'Continue' }).click();
      const alert = page.locator('.login__card').getByRole('alert');
      await expectStatusIsTextAndAria(page, alert);
      await expect(alert).toContainText('was not accepted');
      await expect(alert).not.toContainText(/unknown|no such|stale|epoch|batch/i);
      const refusedText = (await alert.textContent()) ?? '';
      await page.getByLabel('Username').fill(`${username}-nobody`);
      await page.getByLabel('Recovery code').fill(spare ?? '');
      await page.getByRole('button', { name: 'Continue' }).click();
      await expect(alert).toHaveText(refusedText);

      // The real code hands the authority into the establish form, in state.
      await page.getByLabel('Username').fill(username);
      await page.getByLabel('Recovery code').fill(code ?? '');
      await page.getByRole('button', { name: 'Continue' }).click();
      await expect(page.getByRole('heading', { name: 'Establish your credential' })).toBeVisible();
      await expect(
        page.getByRole('status').filter({ hasText: 'Your recovery code was accepted' }),
      ).toBeVisible();
      // The authority is held in component state only: no field renders it,
      // and neither the URL nor the document carries the recovery code.
      await expect(page.getByLabel('Setup authority')).toHaveCount(0);
      expect(page.url().includes(code ?? ''), 'the recovery code reached the URL').toBe(false);
      expect((await page.content()).includes(code ?? ''), 'the recovery code reached the page').toBe(false);
      await page.getByLabel('New password').fill(newPassword);
      await page.getByLabel('Repeat the password').fill(newPassword);
      await page.getByRole('button', { name: 'Establish password' }).click();
      await expect(page.getByRole('heading', { name: 'Credential established' })).toBeVisible();

      // The old password is gone and the new one signs in. Nothing about the
      // authority or the code survives in the page.
      await page.getByRole('link', { name: 'Sign in' }).click();
      await page.getByRole('button', { name: /^Password\b/ }).click();
      await page.getByLabel('Username').fill(username);
      await page.getByLabel('Password').fill(firstPassword);
      await page.getByRole('button', { name: 'Sign in', exact: true }).click();
      await expect(page.locator('.login__card').getByRole('alert')).toBeVisible();
      await page.getByLabel('Password').fill(newPassword);
      await page.getByRole('button', { name: 'Sign in', exact: true }).click();
      // No factor stands on this account, so the product-default `required`
      // policy gates the new session into enrolment (#785) before the shell.
      await passEnrolmentGate(page, newPassword);
      await expect(page.getByRole('list', { name: 'Breadcrumb' })).toBeVisible();
      expect((await page.content()).includes(code ?? ''), 'the recovery code outlived the ceremony').toBe(false);
    } finally {
      await context.close();
    }
  });

  // The sign-in enrolment gate and the passkey second factor (#785), under the
  // product-default `required` policy: an unenrolled invitee signs in, is
  // confined to the gate whatever path it asks for, re-proves its password,
  // stores its recovery codes and enrols a passkey; its next sign-in answers
  // the #760 challenge with that passkey through the button, not the code.
  test('gates an unenrolled account into enrolment, then signs in with its passkey as the second factor', async ({ browser }, testInfo) => {
    const seed = readSeed();
    const username = `gated-${testInfo.project.name}-${Date.now().toString(36)}`;
    const password = 'a password for an account with no factor yet';
    // The invitation rides the shared, already stepped-up administrator
    // session: minting a fresh one would spend a TOTP step, and waiting one out
    // on the shared ledger can eat the whole test budget.
    const adminContext = await browser.newContext({ storageState: STORAGE_STATE });
    const invitation = await browserApi(
      await adminContext.newPage(),
      'POST',
      `/api/v1/orgs/${seed.org}/invitations`,
      z.object({ authority: z.string(), principal_id: z.string() }),
      { username },
    ).finally(() => adminContext.close());
    await publicPost(
      '/api/v1/auth/credential/establish',
      { authority: invitation.authority, password },
      z.object({}),
    );

    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    try {
      // An empty authenticator of its own: the shared admin passkey must not
      // be replaced by this account's resident credential.
      await withPasskeyPage(await context.newPage(), 'empty', async (page) => {
        const signIn = async () => {
          await page.goto('/login');
          await page.getByRole('button', { name: /^Password\b/ }).click();
          await page.getByLabel('Username').fill(username);
          await page.getByLabel('Password').fill(password);
          await page.getByRole('button', { name: 'Sign in', exact: true }).click();
        };

        await signIn();
        await expect(page.getByRole('heading', { name: 'Set up a second factor' })).toBeVisible();
        await expect(page.getByRole('button', { name: /skip|later|not now|without/i })).toHaveCount(0);
        // Confined: a deep link lands back on the gate, no shell renders.
        await page.goto('/projects');
        await expect(page).toHaveURL(/\/login$/);
        await expect(page.getByRole('heading', { name: 'Set up a second factor' })).toBeVisible();
        await expect(page.getByRole('list', { name: 'Breadcrumb' })).toHaveCount(0);

        await page.getByLabel('Password').fill(password);
        await page.getByRole('button', { name: 'Continue' }).click();
        await expect(page.getByRole('heading', { name: 'Store your recovery codes' })).toBeVisible();
        await expect(page.getByRole('list', { name: 'Recovery codes' }).getByRole('listitem')).not.toHaveCount(0);
        const proceed = page.getByRole('button', { name: 'Continue' });
        await expect(proceed).toBeDisabled();
        await page.getByRole('checkbox').check();
        await proceed.click();
        await page.getByRole('button', { name: 'Create a passkey' }).click();
        await expect(page.getByRole('list', { name: 'Breadcrumb' })).toBeVisible();

        // A fresh sign-in: the passkey now stands, so the password answers a
        // challenge, and the passkey button completes it.
        await context.clearCookies();
        await signIn();
        await expect(page.getByRole('heading', { name: 'Present your second factor' })).toBeVisible();
        await expect(page.getByLabel('Authenticator code')).toHaveCount(0);
        await page.getByRole('button', { name: 'Use a passkey' }).click();
        await expect(page.getByRole('list', { name: 'Breadcrumb' })).toBeVisible();
      });
    } finally {
      await context.close();
    }
  });

  test('is dark by default and follows the platform preference', async ({ page }) => {
    await page.goto('/login');
    // No explicit choice has been made, no attribute, no stored value, and
    // no script decides the theme, which is what lets the CSP forbid inline
    // script without a first-paint flash.
    await expect(page.locator('html')).not.toHaveAttribute('data-theme', /.+/);

    await page.emulateMedia({ colorScheme: 'dark' });
    const dark = await measureSurfaceLuminance(page);
    expect(dark.luminance, `the dark surface is not dark (${dark.colour})`).toBeLessThan(0.1);

    // Chromium never reports `no-preference`, so "dark default" is asserted
    // where it is observable, the declared default in the stylesheet, before
    // the light override, via the document's own colour-scheme order.
    const declared = await page.evaluate(
      () => document.querySelector('meta[name="color-scheme"]')?.getAttribute('content') ?? '',
    );
    expect(declared.trim().split(/\s+/)[0], 'the document does not declare dark first').toBe(
      'dark',
    );

    await page.emulateMedia({ colorScheme: 'light' });
    const light = await measureSurfaceLuminance(page);
    expect(
      light.luminance,
      `a light platform preference was not respected (${light.colour})`,
    ).toBeGreaterThan(0.7);
  });

  test('meets the pinned contrast floor in both themes', async ({ page }) => {
    await page.goto('/login');
    for (const scheme of ['dark', 'light'] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      await expectContrast(page, page.getByRole('heading', { name: 'Sign in to Hikyo' }));
      await expectContrast(page, page.getByText('Choose how you sign in.'));
    }
    // Step two, once: the theme is re-emulated in place, the step holds.
    await page.getByRole('button', { name: /^Password\b/ }).click();
    for (const scheme of ['dark', 'light'] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      await expectContrast(page, page.getByRole('heading', { name: 'Sign in with a password' }));
      await expectContrast(page, page.getByText('Use the credential you established'));
      await expectContrast(page, page.getByText('Username'));
    }
  });
});

/**
 * Flow: login, registry surfaces `establish-credential` and `oidc-done`
 * (#610): an invitation claimed by an external identity. An invitee is minted
 * over the API; the login page offers no claim; the establish page holds its
 * provider row until the authority is pasted, and the real round-trip on the
 * fixture provider (purpose `claim`, the authority as the proof) spends the
 * authority, binds a subject no account holds and lands signed in on Account
 * & security. The spent authority then reads the one refusal sentence from a
 * cookie-less browser. The only interception is the fake IdP's authorize
 * request, given that fresh subject; the Hikyo server is never substituted.
 */
test.describe('invitation claim', () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test('clears sensitive inputs on provider departure and Back before a fresh claim', async ({ page, browser }, testInfo) => {
    const seed = readSeed();
    const stamp = `${testInfo.project.name}-${Date.now().toString(36)}`;
    const adminContext = await browser.newContext({ storageState: STORAGE_STATE });
    const adminPage = await adminContext.newPage();
    const invitationSchema = z.object({ authority: z.string(), principal_id: z.string() });
    const invite = (suffix: string) => browserApi(
      adminPage, 'POST', `/api/v1/orgs/${seed.org}/invitations`, invitationSchema,
      { username: `claim-back-${stamp}-${suffix}` },
    );
    const [invitation, freshInvitation] = await Promise.all([invite('depart'), invite('return')])
      .finally(() => adminContext.close());

    await page.goto('/establish');
    const provider = page.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` });
    await expect(provider).toBeDisabled();
    await page.getByLabel('Setup authority').fill(invitation.authority);
    await page.getByLabel('New password', { exact: true }).fill('a browser departure password');
    await page.getByLabel('Repeat the password').fill('a browser departure password');
    await expect(provider).toBeEnabled();
    // Console events can be lost during document teardown. Retain only three
    // numeric lengths in test storage, never authority/password plaintext.
    await page.evaluate(() => {
      sessionStorage.removeItem('claim-departure-lengths');
      globalThis.addEventListener('pagehide', (event) => {
        if (!event.isTrusted) return;
        const lengths = ['authority', 'password', 'repeat'].map((name) =>
          document.querySelector<HTMLInputElement>(`input[name="${name}"]`)?.value.length ?? -1);
        sessionStorage.setItem('claim-departure-lengths', JSON.stringify(lengths));
      }, { once: true });
    });
    await page.route(/\/authorize\?/, async (route) => {
      await route.fulfill({ contentType: 'text/html', body: '<h1>Claim provider paused</h1>' });
    });
    await provider.click();
    await expect(page.getByRole('heading', { name: 'Claim provider paused' })).toBeVisible();

    // Playwright Chromium can reload on Back; the unit test separately proves
    // synchronous clearing at persisted pagehide without claiming BFCache here.
    await page.goBack();
    await expect(page.getByRole('heading', { name: 'Establish your credential' })).toBeVisible();
    const departure = await page.evaluate(() => {
      const lengths = sessionStorage.getItem('claim-departure-lengths');
      sessionStorage.removeItem('claim-departure-lengths');
      return lengths;
    });
    expect(z.array(z.number()).length(3).parse(JSON.parse(departure ?? 'null'))).toEqual([0, 0, 0]);
    for (const label of ['Setup authority', 'New password', 'Repeat the password']) {
      await expect(page.getByLabel(label, { exact: true })).toHaveValue('');
    }
    await expect(provider).toBeDisabled();
    await expect(page.getByText('Contacting identity provider…')).toHaveCount(0);
    await page.unroute(/\/authorize\?/);
    await page.route(/\/authorize\?/, async (route) => {
      await route.continue({ url: `${route.request().url()}&sub=${encodeURIComponent(`claim-back-${stamp}`)}` });
    });
    await page.getByLabel('Setup authority').fill(freshInvitation.authority);
    await expect(provider).toBeEnabled();
    const start = page.waitForRequest((request) => request.method() === 'POST'
      && new URL(request.url()).pathname.endsWith('/start'));
    await provider.click();
    expect((await start).postDataJSON()).toEqual({ purpose: 'claim', proof: freshInvitation.authority, browser: true });
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.getByRole('heading', { name: 'Account & security' })).toBeVisible();
    expect((await page.context().cookies()).find((cookie) => cookie.name === '__Host-hikyo')).toBeDefined();
  });

  test('claims an invitation with a provider and lands signed in; the login page never offers a claim', async ({ page, browser }, testInfo) => {
    const seed = readSeed();
    const stamp = `${testInfo.project.name}-${Date.now().toString(36)}`;
    const adminContext = await browser.newContext({ storageState: STORAGE_STATE });
    const invitation = await browserApi(
      await adminContext.newPage(),
      'POST',
      `/api/v1/orgs/${seed.org}/invitations`,
      z.object({ authority: z.string(), principal_id: z.string() }),
      { username: `claim-${stamp}` },
    ).finally(() => adminContext.close());

    const claimStarts: string[] = [];
    page.on('request', (request) => {
      if (request.method() === 'POST' && /\/start$/.test(new URL(request.url()).pathname)
        && (request.postData() ?? '').includes('"claim"')) claimStarts.push(request.url());
    });

    // The login page has no claim affordance: no authority field, no claim start.
    await page.goto('/login');
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    await expect(page.getByLabel('Setup authority')).toHaveCount(0);

    // Token first, then the provider beside the password form.
    await page.goto('/establish');
    const provider = page.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` });
    await expect(provider).toBeDisabled();
    await page.getByLabel('Setup authority').fill(invitation.authority);
    await expect(provider).toBeEnabled();
    await page.route(/\/authorize\?/, async (route) => {
      await route.continue({ url: `${route.request().url()}&sub=${encodeURIComponent(`claim-${stamp}`)}` });
    });
    await provider.click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.getByRole('heading', { name: 'Account & security' })).toBeVisible();
    expect((await page.context().cookies()).find((c) => c.name === '__Host-hikyo')).toBeDefined();
    expect(claimStarts).toHaveLength(1);

    // Spent: the same authority reads the one sentence, from a fresh browser.
    const fresh = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    try {
      const again = await fresh.newPage();
      await again.goto('/establish');
      await again.getByLabel('Setup authority').fill(invitation.authority);
      await again.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` }).click();
      const alert = again.locator('.login__card').getByRole('alert');
      await expectStatusIsTextAndAria(again, alert);
      await expect(alert).toContainText("That authority can't be used. Ask whoever invited you for a new one.");
      await expect(again).toHaveURL(/\/establish$/);
      await expectNoSeriousAxeViolations(again);
    } finally {
      await fresh.close();
    }
  });
});

/**
 * Flow: login, registry surface `signup` (#607): the staged entry's sign-up
 * door, against real server state. An instance registration policy is opened
 * over the API on the fixture provider (fresh-org landing), the door renders
 * on `/login` and on `/signup`, the confirmation step names the provider and
 * the landing, and the round-trip with a subject no account holds creates the
 * account and its self-served org and lands signed in. Before the door opens,
 * the same unknown subject on the sign-in door is refused: sign-in never
 * creates an account (#604). The only interception is the fake IdP's
 * authorize request, given a fresh subject the way a different person would
 * bring one; the Hikyo server is never substituted.
 */
test.describe('sign-up door', () => {
  test.use({ storageState: { cookies: [], origins: [] } });
  const policyPath = '/api/v1/instance/registration-policy';

  /** Give the fake IdP's authorize request a subject no account holds. */
  async function freshSubject(page: Page, subject: string) {
    await page.route(/\/authorize\?/, async (route) => {
      await route.continue({ url: `${route.request().url()}&sub=${encodeURIComponent(subject)}` });
    });
  }

  test('opens only while registration is open, confirms, and lands a new account in its own org', async ({ page, browser }, testInfo) => {
    const subject = `signup-${testInfo.project.name}-${Date.now().toString(36)}`;
    // Its own instance operators, with their own authenticators: two codes
    // enrol and step each up, a third proves its write. A fresh account holds
    // two codes per TOTP step, so the opening proof may wait for one step
    // boundary; the closer is enrolled now, so its proof, drawn at the end,
    // is due by then. The budget is the default plus that one step.
    testInfo.setTimeout(testInfo.timeout + TOTP_STEP_MS);
    const [operator, closer] = await Promise.all([
      enrolledAccount(browser, 'signup-operator', 'instance'),
      enrolledAccount(browser, 'signup-closer', 'instance'),
    ]);
    const admin = operator.bearer;
    await freshSubject(page, subject);

    // Closed: nothing hints at sign-up, and the sign-in door refuses an
    // unknown identity instead of creating one.
    await page.goto('/login');
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    await expect(page.getByText('New here?')).toHaveCount(0);
    await page.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` }).click();
    await expect(page).toHaveURL(/error=unauthenticated/);
    expect((await page.context().cookies()).find((c) => c.name === '__Host-hikyo')).toBeUndefined();

    let created: string | undefined;
    try {
      await fixtureApiCall(admin, 'PUT', policyPath, zRegistrationPolicy, {
        external: [{ provider: { kind: 'oidc', slug: OIDC_PROVIDER.slug } }],
        landing: { kind: 'fresh-org', cap: 5 },
        proof: await operator.ledger.next(),
      });

      // The door on /login, and the pinned set on /signup in both schemes.
      await page.goto('/login');
      await page.getByRole('button', { name: 'Create an account' }).click();
      await expect(page.getByRole('heading', { name: 'Create an account' })).toBeVisible();
      for (const scheme of ['dark', 'light'] as const) {
        await page.emulateMedia({ colorScheme: scheme });
        await page.goto('/signup');
        const card = page.locator('.login__card');
        const heading = page.getByRole('heading', { name: 'Create an account' });
        const landing = page.locator('.login__landing');
        const provider = page.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` });
        await expect(landing).toHaveText('You’ll get your own organisation, with you as its first administrator.');
        await expectPinnedAssertionSet(page, {
          flow: 'login',
          surface: 'signup',
          theme: scheme,
          text: [heading, landing],
          radii: [[card, 'container'], [landing, 'container'], [provider, 'control']],
          fonts: [[heading, 'ui'], [landing, 'ui']],
          colours: [
            [heading, 'color', '--tx'],
            [landing, 'color', '--tx'],
            [landing, 'borderTopColor', '--accent'],
            [card, 'backgroundColor', '--bg-raise'],
          ],
          hairlines: [card, landing, provider],
          density: [[provider, '--control']],
        });
      }

      // The confirmation step, then the real round-trip under `sign-up`.
      await page.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` }).click();
      await expect(page.getByRole('heading', { name: `Create an account with ${OIDC_PROVIDER.displayName}` })).toBeVisible();
      await expect(page.locator('.login__card')).toContainText(
        `This creates a new account. Already have one? Sign in with it first, then add ${OIDC_PROVIDER.displayName} under Settings › Security.`,
      );
      await page.getByRole('button', { name: `Continue to ${OIDC_PROVIDER.displayName}` }).click();
      await expect(page.getByRole('list', { name: 'Breadcrumb' })).toBeVisible();
      expect((await page.context().cookies()).find((c) => c.name === '__Host-hikyo')).toBeDefined();

      // The self-served org the sign-up minted, on the operator's origin filter.
      const selfServed = await fixtureApiCall(admin, 'GET', '/api/v1/orgs?origin=registration', zOrgList);
      const minted = selfServed.items.filter((org) => org.origin === 'registration' && org.name === `org-${org.id}`);
      expect(minted.length).toBeGreaterThan(0);
      created = minted.at(-1)?.id;
      const adminView = await browser.newContext({ storageState: STORAGE_STATE });
      try {
        const operatorPage = await adminView.newPage();
        await operatorPage.goto('/instance');
        await operatorPage.getByRole('tab', { name: 'Organisations' }).click();
        const orgs = operatorPage.locator('#instance-orgs');
        await orgs.getByLabel('Show').selectOption('registration');
        await expect(orgs.getByRole('link', { name: `org-${created ?? ''}` })).toBeVisible();
        await expect(orgs.getByText('self-serve', { exact: true }).first()).toBeVisible();
        for (const theme of ['dark', 'light'] as const) {
          await operatorPage.emulateMedia({ colorScheme: theme });
          const heading = orgs.getByRole('heading', { name: 'Organisations', exact: true });
          const filter = orgs.getByLabel('Show');
          await expectPinnedAssertionSet(operatorPage, {
            flow: 'instance-admin', surface: 'instance-admin', theme,
            text: [heading, orgs.getByRole('link', { name: `org-${created ?? ''}` })],
            radii: [[orgs, 'container'], [filter, 'control']], fonts: [[heading, 'ui']],
            colours: [[heading, 'color', '--tx']], hairlines: [orgs, filter], density: [[filter, '--control']],
          });
          await expectNoSeriousAxeViolations(operatorPage);
          await operatorPage.screenshot({ path: testInfo.outputPath(`org-origin-${theme}.png`), fullPage: true });
        }
        await orgs.getByLabel('Show').selectOption('manual');
        await expect(orgs.getByRole('link', { name: `org-${created ?? ''}` })).toHaveCount(0);
      } finally {
        await adminView.close();
      }
    } finally {
      const toleratingGone = (error: unknown) => {
        if (!(error instanceof Error && error.message.includes('answered 404:'))) throw error;
      };
      if (created !== undefined) {
        await fixtureApiCall(admin, 'DELETE', `/api/v1/orgs/${created}`, z.unknown()).catch(toleratingGone);
      }
      await fixtureApiCall(closer.bearer, 'DELETE', policyPath, z.unknown(), { proof: await closer.ledger.next() }).catch(toleratingGone);
    }
    // Closed again: the door is gone (a fresh, signed-out page).
    await page.context().clearCookies();
    await page.goto('/login');
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    await expect(page.getByText('New here?')).toHaveCount(0);
  });
});

/** Real local registration: browser request, trusted TLS SMTP, single-use link and fresh org. */
test.describe('local sign-up', () => {
  test.use({ storageState: { cookies: [], origins: [] } });
  test('requests, resends and finishes fresh-org signup without a browser session', async ({ page, browser }, testInfo) => {
    testInfo.setTimeout(testInfo.timeout + TOTP_STEP_MS);
    const [operator, closer] = await Promise.all([
      enrolledAccount(browser, 'local-signup-operator', 'instance'),
      enrolledAccount(browser, 'local-signup-closer', 'instance'),
    ]);
    const emailAddress = `signup-${testInfo.project.name}-${Date.now().toString(36)}@hikyo.test`;
    const organisation = `Browser signup ${testInfo.project.name}`;
    const policyPath = '/api/v1/instance/registration-policy';
    let created: string | undefined;
    let opened = false;
    try {
      await fixtureApiCall(operator.bearer, 'PUT', policyPath, zRegistrationPolicy, {
        local: {}, external: [], landing: { kind: 'fresh-org', cap: 5 }, proof: await operator.ledger.next(),
      });
      opened = true;
      await page.goto('/signup');
      await page.getByRole('button', { name: 'Email', exact: true }).click();
      await page.getByLabel('Email', { exact: true }).fill(emailAddress);
      await page.getByRole('button', { name: 'Send sign-up link' }).click();
      await expect(page.getByRole('heading', { name: 'Check your mail' })).toBeVisible();
      await expect(page.locator('.login__card')).toContainText('If this address can sign up');
      await expect.poll(() => readMail(emailAddress).length).toBe(1);
      await page.getByRole('button', { name: 'Send it again' }).click();
      await expect(page.getByRole('status').filter({ hasText: 'Requested another link' })).toBeVisible();
      await expect.poll(() => readMail(emailAddress).length).toBe(2);
      const messages = readMail(emailAddress);
      const links = messages.map((message) => {
        expect(message.from).toBe('signup@hikyo.test');
        // go-mail emits quoted-printable text: unwrap soft lines before reading the URL.
        const body = message.data.replace(/=\r\n/g, '').replace(/=([0-9A-F]{2})/gi, (_, hex: string) => String.fromCharCode(Number.parseInt(hex, 16)));
        const link = body.match(/https?:\/\/[^\s]+\/signup\/verify#[^\s]+/)?.[0];
        if (link === undefined) throw new Error('accepted sign-up mail has no verification link');
        expect(new URL(link).origin).toBe(BASE_URL);
        return link;
      });
      expect(links[1]).not.toBe(links[0]);
      const currentLink = links[1];
      if (currentLink === undefined) throw new Error('resend mail is missing');
      for (const theme of ['dark', 'light'] as const) {
        await page.emulateMedia({ colorScheme: theme });
        await page.goto(currentLink);
        await expect(page).toHaveURL(/\/signup\/verify$/);
        const card = page.locator('.login__card');
        const heading = page.getByRole('heading', { name: 'Finish creating your account' });
        const email = page.getByLabel('Email', { exact: true });
        const submit = page.getByRole('button', { name: 'Create account', exact: true });
        await expect(email).toHaveValue(emailAddress);
        await expect(email).toHaveAttribute('readonly', '');
        await expect(page.getByLabel('Password', { exact: true })).toHaveAttribute('autocomplete', 'new-password');
        await expectPinnedAssertionSet(page, {
          flow: 'signup', surface: 'signup-verify', theme,
          text: [heading, page.locator('.login__landing')],
          radii: [[card, 'container'], [submit, 'control'], [email, 'control']],
          fonts: [[heading, 'ui']],
          colours: [[heading, 'color', '--tx'], [card, 'backgroundColor', '--bg-raise']],
          hairlines: [card, email], density: [[submit, '--control']],
        });
        await expectNoSeriousAxeViolations(page);
        await page.screenshot({ path: testInfo.outputPath(`signup-verify-${theme}.png`), fullPage: true });
      }
      await page.getByLabel('Display name').fill('Alex');
      await page.getByLabel('Organisation name').fill(organisation);
      await page.getByLabel('Password', { exact: true }).fill('twelvecharacters');
      await page.getByLabel('Repeat password').fill('differentpassword');
      await page.getByRole('button', { name: 'Create account', exact: true }).click();
      await expect(page.locator('.login__card').getByRole('alert')).toContainText('The two passwords differ');
      await page.getByLabel('Repeat password').fill('twelvecharacters');
      const verified = page.waitForResponse((response) => response.url().endsWith('/auth/signup/verify'));
      await page.getByRole('button', { name: 'Create account', exact: true }).click();
      expect((await verified).status()).toBe(204);
      await expect(page).toHaveURL(/\/login$/);
      expect((await page.context().cookies()).some((cookie) => cookie.name === '__Host-hikyo')).toBe(false);
      const orgs = await fixtureApiCall(operator.bearer, 'GET', '/api/v1/orgs?origin=registration', zOrgList);
      const minted = orgs.items.find((org) => org.name === organisation);
      expect(minted?.origin).toBe('registration');
      created = minted?.id;
      await page.goto(currentLink);
      await page.getByLabel('Display name').fill('Alex replay');
      await page.getByLabel('Organisation name').fill('Replay must not create this');
      await page.getByLabel('Password', { exact: true }).fill('twelvecharacters');
      await page.getByLabel('Repeat password').fill('twelvecharacters');
      await page.getByRole('button', { name: 'Create account', exact: true }).click();
      await expect(page.getByRole('link', { name: 'Go to sign-up' })).toBeVisible();
      expect((await page.context().cookies()).some((cookie) => cookie.name === '__Host-hikyo')).toBe(false);
    } finally {
      if (opened) await fixtureApiCall(closer.bearer, 'DELETE', policyPath, z.object({}), { proof: await closer.ledger.next() });
      if (created !== undefined) await fixtureApiCall(operator.bearer, 'DELETE', `/api/v1/orgs/${created}`, z.object({}));
    }
  });
});

/** #612 rides the existing login CI shard, including both viewport projects. */
test.describe('CLI login handoff', () => {
  test.beforeEach(async ({ context }) => { await context.clearCookies(); });

  test('password-only browser recovers through enrolled MFA before explicitly approving the same CLI request', async ({ page, browser }, testInfo) => {
    const username = `cli-login-recovery-${randomBytes(8).toString('hex')}`;
    const password = randomBytes(24).toString('base64url');
    const invitation = await withSharedAdmin(browser, (admin) => browserApi(
      admin, 'POST', `/api/v1/orgs/${readSeed().org}/invitations`,
      z.object({ authority: z.string(), principal_id: z.string() }), { username },
    ));
    await publicPost('/api/v1/auth/credential/establish', { authority: invitation.authority, password }, z.object({}));
    const signedIn = await page.request.post(`${BASE_URL}/api/v1/auth/local/login`, {
      data: { username, password, artifact: 'browser' },
    });
    expect(signedIn.status()).toBe(200);
    const initial = zLoginResult.parse(await signedIn.json());
    expect(initial.session.assurance.factors).toEqual(['password']);
    const enrolment = await browserApi(page, 'POST', '/api/v1/auth/totp/enrol/start', zTotpEnrolStartResult, { password });
    const ledger = new TotpLedger(enrolment.otpauth_uri, new Date());
    // Enrolment reissues the acting browser with password proof only. The
    // confirming code enrols a factor; it does not authenticate this session.
    const weak = await browserApi(page, 'POST', '/api/v1/auth/totp/enrol/confirm', zLoginResult, { code: await ledger.next() });
    expect(weak.session.artifact).toBe('browser');
    expect(weak.session.assurance.factors).toEqual(['password']);
    expect(weak.session.id).not.toBe(initial.session.id);

    const flow = await startLoopbackLogin();
    const returnTo = `/login/cli?state=${flow.state}&provider=oidc%3A${OIDC_PROVIDER.slug}`;
    try {
      await page.goto(returnTo);
      const approve = page.getByRole('button', { name: 'Authorize CLI sign-in', exact: true });
      await expect(approve).toBeVisible();
      const refused = page.waitForResponse((response) => response.url().endsWith('/auth/cli-login/approve'));
      await approve.click();
      const refusal = await refused;
      expect(refusal.status()).toBe(400);
      const detail = 'Complete your enrolled authenticator or passkey before authorizing the CLI. Sign out and sign in again.';
      expect(zError.parse(await refusal.json()).error.detail).toBe(detail);
      await expect(page.getByRole('heading', { name: 'Complete your sign-in' })).toBeVisible();
      await expect(page.locator('.login__card').getByRole('alert')).toHaveText(detail);
      await page.screenshot({ path: testInfo.outputPath('mfa-required.png') });

      const loggedOut = page.waitForResponse((response) => response.url().endsWith('/auth/logout'));
      await page.getByRole('button', { name: 'Sign out and sign in again', exact: true }).click();
      expect((await loggedOut).status()).toBe(204);
      await expect(page).toHaveURL(`${BASE_URL}${returnTo}`);
      await expect(page.getByRole('heading', { name: `Sign in with ${OIDC_PROVIDER.displayName}` })).toBeVisible();
      await expect(page.getByRole('button', { name: 'Create an account' })).toHaveCount(0);
      await page.getByRole('button', { name: 'Other ways to sign in' }).click();
      await page.getByRole('button', { name: /^Password\b/ }).click();
      await page.getByLabel('Username').fill(username);
      await page.getByLabel('Password', { exact: true }).fill(password);
      await page.getByRole('button', { name: 'Sign in', exact: true }).click();
      await expect(page.getByRole('heading', { name: 'Present your second factor' })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath('second-factor.png') });
      await page.getByLabel('Authenticator code').fill(await ledger.next());
      await page.getByRole('button', { name: 'Present code' }).click();

      await expect(page).toHaveURL(`${BASE_URL}${returnTo}`);
      await expect(approve).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath('consent-after-mfa.png') });
      const browserIdentity = await browserApi(page, 'GET', '/api/v1/auth/whoami', zWhoAmI);
      expect(browserIdentity.session.id).not.toBe(weak.session.id);
      expect(browserIdentity.session.assurance.factors).toEqual(['password', 'totp']);
      // Reaching consent did not approve the request: the same live transaction
      // remains readable, and only this second explicit click authorizes it.
      const pending = await browserApi(page, 'GET', `/api/v1/auth/cli-login/transactions/${flow.state}`, z.object({ reference: z.string() }));
      expect(pending.reference).toBe(flow.state);
      await approve.click();
      await expect(page).toHaveURL(new RegExp(`^${flow.redirect.replaceAll('.', '\\.')}`));
      const callback = new URL(page.url());
      expect(callback.searchParams.get('state')).toBe(flow.state);
      const redeemed = await publicPost('/api/v1/auth/cli-login/redeem', {
        code: callback.searchParams.get('code'), pkce_verifier: flow.verifier, redirect_uri: flow.redirect,
      }, zLoginResult);
      expect(redeemed.session_token).toBeTruthy();
      expect(redeemed.session.artifact).toBe('cli');
      expect(redeemed.session.id).not.toBe(browserIdentity.session.id);
      expect(redeemed.principal.id).toBe(invitation.principal_id);
      expect(redeemed.session.assurance.factors).toEqual(['password', 'totp']);
      const stillSignedIn = await browserApi(page, 'GET', '/api/v1/auth/whoami', zWhoAmI);
      expect(stillSignedIn.session.id).toBe(browserIdentity.session.id);
    } finally { await flow.close(); }
  });

  test('device code entry preserves provider preselect and always starts sign-in', async ({ page }) => {
    await page.goto(`/login/cli?device=1&provider=oidc:${OIDC_PROVIDER.slug}`);
    await page.getByLabel('Device code').fill('abcd-efgh');
    await page.getByRole('button', { name: 'Continue', exact: true }).click();
    await expect(page).toHaveURL(/user_code=ABCDEFGH.*provider=oidc%3Ae2e-oidc/);
    await expect(page.getByRole('heading', { name: `Sign in with ${OIDC_PROVIDER.displayName}` })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create an account' })).toHaveCount(0);
    const start = page.waitForRequest((request) => request.url().endsWith(`/auth/oidc/${OIDC_PROVIDER.slug}/start`));
    await page.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` }).click();
    const request = await start;
    expect(z.object({ intent: z.literal('sign-in'), browser: z.literal(true) }).parse(request.postDataJSON()).intent).toBe('sign-in');
    // The deliberately unknown user code is only resolved after the ordinary
    // browser ceremony; the return URL must still carry it and the provider.
    await expect(page).toHaveURL(/\/login\/cli\?user_code=ABCDEFGH.*provider=oidc%3Ae2e-oidc/);
    await expect(page.getByRole('heading', { name: 'CLI sign-in could not be completed' })).toBeVisible();
  });

  test('OAuth2 UI contract hides signup, starts sign-in and retains the CLI return after refusal', async ({ page }) => {
    const state = 'A'.repeat(43);
    const returnTo = `/login/cli?state=${state}&provider=oauth2%3Agithub`;
    await page.route('**/api/v1/auth/methods*', (route) => route.fulfill({ json: {
      local_login_enabled: true,
      providers: [{ kind: 'oauth2', slug: 'github', display_name: 'GitHub', profile: 'github', brand: 'github' }],
      signup_open: true, signup_paused: false, signup_methods: ['local', { kind: 'oauth2', slug: 'github' }],
    } }));
    await page.route('**/api/v1/auth/oauth2/github/start', async (route) => {
      expect(z.object({ purpose: z.literal('login'), intent: z.literal('sign-in'), browser: z.literal(true) }).parse(route.request().postDataJSON()).intent).toBe('sign-in');
      await route.fulfill({ json: { authorization_url: `${BASE_URL}/auth/oidc/done?purpose=login&state=oauth2-ui&error=unknown-identity` } });
    });
    await page.goto(returnTo);
    await expect(page.getByRole('heading', { name: 'Sign in with GitHub' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create an account' })).toHaveCount(0);
    await expect(page.getByRole('link', { name: 'Have a setup authority? Establish your credential' })).toHaveCount(0);
    await page.getByRole('button', { name: 'Continue with GitHub' }).click();
    const back = page.getByRole('link', { name: 'Return to sign in' });
    await expect(back).toHaveAttribute('href', returnTo);
    await back.click();
    await expect(page).toHaveURL(`${BASE_URL}${returnTo}`);
    await expect(page.getByRole('heading', { name: 'Sign in with GitHub' })).toBeVisible();
  });

  const schemes: Array<'dark' | 'light'> = ['dark', 'light'];
  for (const scheme of schemes) {
    test(`device consent requires explicit approval and meets the pinned set (${scheme})`, async ({ browser }, testInfo) => {
      const context = await browser.newContext({ storageState: STORAGE_STATE });
      const page = await context.newPage();
      try {
        const started = await startDeviceLogin();
        let interval = started.interval;
        let nextPollAt = Date.now() + interval * 1000;
        const poll = () => fetch(`${BASE_URL}/api/v1/auth/cli-login/device/token`, {
          method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          body: new URLSearchParams({ device_code: started.device_code, client_id: 'hikyo-cli', grant_type: 'urn:ietf:params:oauth:grant-type:device_code' }),
        });
        if (scheme === 'dark') {
          // Deliberately early real protocol polls exercise RFC 8628's cumulative
          // slow_down. The next token request waits the server-directed interval.
          for (const expectedInterval of [10, 15]) {
            const early = await poll();
            expect(early.status).toBe(400);
            interval = z.object({ error: z.literal('slow_down'), interval: z.literal(expectedInterval) }).parse(await early.json()).interval;
            nextPollAt = Date.now() + interval * 1000;
          }
        }
        await page.emulateMedia({ colorScheme: scheme });
        await page.goto(`/login/cli?user_code=${started.user_code}`);
        const heading = page.getByRole('heading', { name: 'Authorize CLI sign-in' });
        const card = page.locator('.login__card');
        const approve = page.getByRole('button', { name: 'Authorize CLI sign-in', exact: true });
        await expect(heading).toBeVisible();
        await expect(card).toContainText(started.user_code);
        await expect(card).toContainText('Hikyo CLI');
        await expect(card).toContainText(BASE_URL);
        await expect(card).toContainText('Separate CLI session');
        await expectPinnedAssertionSet(page, {
          flow: 'cli-login', surface: 'cli-login', theme: scheme,
          text: [heading], radii: [[card, 'container'], [approve, 'control']],
          fonts: [[heading, 'ui']],
          colours: [[heading, 'color', '--tx'], [card, 'backgroundColor', '--bg-raise'], [approve, 'backgroundColor', '--accent']],
          hairlines: [card], density: [[approve, '--control']],
        });
        await page.screenshot({ path: `/tmp/hikyo-612-cli-${testInfo.project.name}-${scheme}.png` });
        await approve.click();
        await expect(page.getByRole('heading', { name: 'CLI sign-in approved' })).toBeVisible();
        await new Promise((resolve) => setTimeout(resolve, Math.max(0, nextPollAt - Date.now())));
        const completed = await poll();
        const completedBody = await completed.json();
        expect(completed.status).toBe(200);
        expect(z.object({ access_token: z.string().min(1), token_type: z.literal('Bearer') }).parse(completedBody).access_token).toBeTruthy();
        const whoami = await context.request.get(`${BASE_URL}/api/v1/auth/whoami`);
        expect(whoami.status()).toBe(200);
        expect(await page.evaluate(() => JSON.stringify({ local: Object.entries(localStorage), session: Object.entries(sessionStorage) }))).not.toContain(started.device_code);
      } finally { await context.close(); }
    });
  }

  test('server authority accepts a live request with a fast browser clock and refuses its reuse', async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    try {
      await context.addInitScript(() => {
        const now = Date.now.bind(Date);
        Date.now = () => now() + 20 * 60 * 1000;
      });
      const page = await context.newPage();
      const started = await startDeviceLogin();
      const path = `/login/cli?user_code=${started.user_code}`;
      await page.goto(path);
      await page.getByRole('button', { name: 'Authorize CLI sign-in', exact: true }).click();
      await expect(page.getByRole('heading', { name: 'CLI sign-in approved' })).toBeVisible();
      // A document reload drops cached consent and asks the real server again.
      const refusal = page.waitForResponse((response) =>
        response.url().endsWith(`/auth/cli-login/transactions/${started.user_code}`),
      );
      await page.reload();
      expect((await refusal).status()).toBe(403);
      await expect(page.getByRole('heading', { name: 'CLI sign-in could not be completed' })).toBeVisible();
      await expect(page.getByRole('button', { name: 'Authorize CLI sign-in', exact: true })).toHaveCount(0);
    } finally {
      await context.close();
    }
  });

  test('cancel works in a normal tab and a new device request requires new approval', async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    try {
      const page = await context.newPage();
      const first = await startDeviceLogin();
      await page.goto(`/login/cli?user_code=${first.user_code}`);
      await page.getByRole('button', { name: 'Cancel', exact: true }).click();
      await expect(page.getByRole('heading', { name: 'CLI sign-in cancelled' })).toBeVisible();
      const second = await startDeviceLogin();
      await page.goto(`/login/cli?user_code=${second.user_code}`);
      await expect(page.getByRole('button', { name: 'Authorize CLI sign-in', exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'Authorize CLI sign-in', exact: true }).click();
      await expect(page.getByRole('heading', { name: 'CLI sign-in approved' })).toBeVisible();
    } finally { await context.close(); }
  });

  for (const method of ['password', 'passkey', 'oidc']) {
    test(`loopback ${method} sign-in preserves the browser session and redeems a separate CLI session`, async ({ page, browser }) => {
      const flow = await startLoopbackLogin();
      try {
        const run = async (loginPage: Page) => {
          await loginPage.goto(`/login/cli?state=${flow.state}`);
          await expect(loginPage.getByRole('button', { name: 'Create an account' })).toHaveCount(0);
          if (method === 'password') {
            const account = await enrolledAccount(browser, 'cli-login-password', 'org');
            await loginPage.getByRole('button', { name: /^Password\b/ }).click();
            await loginPage.getByLabel('Username').fill(account.username);
            await loginPage.getByLabel('Password', { exact: true }).fill(account.password);
            await loginPage.getByRole('button', { name: 'Sign in', exact: true }).click();
            await expect(loginPage.getByRole('heading', { name: 'Present your second factor' })).toBeVisible();
            await loginPage.getByLabel('Authenticator code').fill(await account.ledger.next());
            await loginPage.getByRole('button', { name: 'Present code' }).click();
          } else if (method === 'passkey') {
            await loginPage.getByRole('button', { name: 'Passkey', exact: true }).click();
          } else {
            await loginPage.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` }).click();
          }
          await expect(loginPage.getByRole('heading', { name: 'Authorize CLI sign-in' })).toBeVisible();
          await expect(loginPage).toHaveURL(new RegExp(`/login/cli\\?state=${flow.state}`));
          await loginPage.getByRole('button', { name: 'Authorize CLI sign-in', exact: true }).click();
          await expect(loginPage).toHaveURL(new RegExp(`^${flow.redirect.replaceAll('.', '\\.')}`));
          const callback = new URL(loginPage.url());
          expect(callback.searchParams.get('state')).toBe(flow.state);
          const redeemed = await publicPost('/api/v1/auth/cli-login/redeem', {
            code: callback.searchParams.get('code'), pkce_verifier: flow.verifier, redirect_uri: flow.redirect,
          }, z.object({ session_token: z.string().min(1) }));
          expect(redeemed.session_token).toBeTruthy();
          expect((await loginPage.context().request.get(`${BASE_URL}/api/v1/auth/whoami`)).status()).toBe(200);
        };
        if (method === 'passkey') await withPasskeyPage(page, 'shared', run);
        else await run(page);
      } finally { await flow.close(); }
    });
  }
});

async function startDeviceLogin() {
  const response = await fetch(`${BASE_URL}/api/v1/auth/cli-login/device/start`, {
    method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ client_id: 'hikyo-cli' }),
  });
  if (!response.ok) throw new Error(`device start answered ${response.status}: ${await response.text()}`);
  return z.object({ device_code: z.string(), user_code: z.string(), interval: z.number() }).parse(await response.json());
}
async function startLoopbackLogin() {
  const server = createServer((_request, response) => { response.end('CLI sign-in complete. Return to your terminal.'); });
  await new Promise<void>((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('loopback listener has no TCP address');
  const redirect = `http://127.0.0.1:${address.port}/callback`;
  const state = randomBytes(32).toString('base64url');
  const verifier = randomBytes(32).toString('base64url');
  try {
    await publicPost('/api/v1/auth/cli-login/start', {
      transport: 'loopback', state, pkce_challenge: createHash('sha256').update(verifier).digest('base64url'), redirect_uri: redirect,
    }, z.object({ state: z.string() }));
  } catch (error) { server.close(); throw error; }
  return { state, verifier, redirect, close: () => new Promise<void>((resolve, reject) => server.close((error) => error === undefined ? resolve() : reject(error))) };
}
