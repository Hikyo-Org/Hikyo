import { expect, test, type Page, type Route } from '@playwright/test';

import {
  expectBoundaryContrast,
  expectContrast,
  expectNoSeriousAxeViolations,
  expectPinnedAssertionSet,
  expectStatusIsTextAndAria,
  measureSurfaceLuminance,
} from '../fixtures/assertions.ts';
import { z } from 'zod';

import { fixtureApiCall, fixtureBearer } from '../fixtures/api.ts';
import { ADMIN, BASE_URL, OIDC_PROVIDER, nextTotpCode, readSeed } from '../fixtures/instance.ts';

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

  const card = page.locator('.login__card');
  const submit = page.getByRole('button', { name: 'Sign in' });
  const username = page.getByLabel('Username');
  const password = page.getByLabel('Password');
  const heading = page.getByRole('heading', { name: 'Sign in to Hikyo' });
  const lede = page.getByText('Use the credential you established');

  await expectBoundaryContrast(page, username);
  await expectBoundaryContrast(page, password);

  await expectPinnedAssertionSet(page, {
    flow: 'login',
    surface: 'login',
    theme,
    text: [heading, lede],
    radii: [
      [card, 'container'],
      [submit, 'control'],
      [username, 'control'],
      [password, 'control'],
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
    density: [[submit, '--touch']],
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
    density: [[close, '--touch']],
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
    density: [[submit, '--touch']],
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
    density: [[submit, '--touch']],
  });
}

/**
 * Flow: login (registry surface `login`).
 *
 * Covers the surface's whole job — refusal and success — and runs the pinned
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

    await page.getByLabel('Username').fill(ADMIN.username);
    await page.getByLabel('Password').fill('not the password at all');
    await page.getByRole('button', { name: 'Sign in' }).click();

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

  test('redirects an anonymous authenticated-route deep link to login', async ({ page }) => {
    await page.goto('/projects');

    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Organisations' })).toHaveCount(0);
  });

  test('signs in and establishes a browser session on cookies alone', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Username').fill(ADMIN.username);
    await page.getByLabel('Password').fill(ADMIN.password);
    await page.getByRole('button', { name: 'Sign in' }).click();

    // The org rail is desktop chrome — a phone reaches organisations through
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

      const controls = page.locator('input, button');
      await expect(controls).toHaveCount(5);
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
  // The happy path — a real invitation claimed here and signed in with — is
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

    // Invitation takes a stepped-up administrator session; the invite carries
    // no template, so the account can sign in and see nothing.
    const admin = await fixtureBearer('the recovery fixture');
    const stepped = await fixtureApiCall(
      admin,
      'POST',
      '/api/v1/auth/totp/step-up',
      z.object({ session_token: z.string() }),
      { code: await nextTotpCode() },
    );
    const invitation = await fixtureApiCall(
      stepped.session_token,
      'POST',
      `/api/v1/orgs/${seed.org}/invitations`,
      z.object({ authority: z.string(), principal_id: z.string() }),
      { username },
    );
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
      await page.getByLabel('Username').fill(username);
      await page.getByLabel('Password').fill(firstPassword);
      await page.getByRole('button', { name: 'Sign in' }).click();
      await expect(page.locator('.login__card').getByRole('alert')).toBeVisible();
      await page.getByLabel('Password').fill(newPassword);
      await page.getByRole('button', { name: 'Sign in' }).click();
      await expect(page.getByRole('list', { name: 'Breadcrumb' })).toBeVisible();
      expect((await page.content()).includes(code ?? ''), 'the recovery code outlived the ceremony').toBe(false);
    } finally {
      await context.close();
    }
  });

  test('is dark by default and follows the platform preference', async ({ page }) => {
    await page.goto('/login');
    // No explicit choice has been made — no attribute, no stored value — and
    // no script decides the theme, which is what lets the CSP forbid inline
    // script without a first-paint flash.
    await expect(page.locator('html')).not.toHaveAttribute('data-theme', /.+/);

    await page.emulateMedia({ colorScheme: 'dark' });
    const dark = await measureSurfaceLuminance(page);
    expect(dark.luminance, `the dark surface is not dark (${dark.colour})`).toBeLessThan(0.1);

    // Chromium never reports `no-preference`, so "dark default" is asserted
    // where it is observable — the declared default in the stylesheet, before
    // the light override — via the document's own colour-scheme order.
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
      await expectContrast(page, page.getByText('Use the credential you established'));
      await expectContrast(page, page.getByText('Username'));
    }
  });
});
