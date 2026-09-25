import { expect, test, type Page, type Route } from '@playwright/test';

import {
  expectBoundaryContrast,
  expectContrast,
  expectNoSeriousAxeViolations,
  expectPinnedAssertionSet,
  expectStatusIsTextAndAria,
  measureSurfaceLuminance,
} from '../fixtures/assertions.ts';
import { zAuthMethods, zOrgList, zRegistrationPolicy } from '@hikyo/zod';
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
import { enrolledAccount, TOTP_STEP_MS } from '../fixtures/accounts.ts';
import { withPasskeyPage } from '../fixtures/passkey.ts';

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
    density: [[passwordRow, '--touch'], [provider, '--touch']],
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

async function expectEstablishSurface(page: Page, theme: 'dark' | 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.goto('/establish');

  const card = page.locator('.login__card');
  const submit = page.getByRole('button', { name: 'Establish credential' });
  const authority = page.getByLabel('Setup authority');
  const password = page.getByLabel('New password');
  const repeat = page.getByLabel('Repeat the password');
  const heading = page.getByRole('heading', { name: 'Establish your credential' });
  const lede = page.getByText('Paste the setup authority you were handed');

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
    hairlines: [card, authority],
    density: [[submit, '--control']],
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
    const providerBody = (enabled: boolean) => ({
      display_name: provider.displayName,
      issuer: WEBUI_OIDC.issuer,
      client_id: 'e2e-reg-client',
      client_secret: 'e2e-reg-secret',
      scopes: 'openid email',
      enabled,
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
      await fixtureApiCall(admin, 'PUT', providerPath, z.unknown(), providerBody(true));
      await fixtureApiCall(admin, 'PUT', policyPath, zRegistrationPolicy, {
        external: [{ provider: { kind: 'oidc', slug: provider.slug } }],
        landing: { kind: 'none' },
        proof: await operator.ledger.next(),
      });
      expect((await methods()).signup_open).toBe(true);
      await fixtureApiCall(admin, 'PUT', providerPath, z.unknown(), providerBody(false));
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
      await page.getByRole('button', { name: 'Establish credential' }).click();
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
    await page.getByRole('button', { name: 'Establish credential' }).click();
    const alert = page.locator('.login__card').getByRole('alert');
    await expectStatusIsTextAndAria(page, alert);
    // Unknown, expired and spent are one sentence: the server closes that
    // oracle and the page must not reopen it.
    await expect(alert).toContainText('was not accepted');
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
      await page.getByRole('button', { name: 'Establish credential' }).click();
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
          density: [[provider, '--touch']],
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
        const orgs = operatorPage.locator('#instance-orgs');
        await orgs.getByLabel('Show').selectOption('registration');
        await expect(orgs.getByRole('link', { name: `org-${created ?? ''}` })).toBeVisible();
        await expect(orgs.getByText('self-serve', { exact: true }).first()).toBeVisible();
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
