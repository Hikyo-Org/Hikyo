import { expect, type Page } from '@playwright/test';
import { zGrantResult, zPublishResult, zReauthResult } from '@hikyo/zod';
import { z } from 'zod';

import { expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import { BrowserApiError, browserApi } from '../fixtures/api.ts';
import {
  ADMIN,
  countDisclosureEvents,
  establishSession,
  nextTotpCode,
  OIDC_PROVIDER,
  readSeed,
} from '../fixtures/instance.ts';
import { test } from '../fixtures/passkey.ts';

/**
 * Flow: the reveal, copy and publish-into-protected ceremonies (registry
 * surface `values`) — mvp-boundary A5 and the S3 ceremony row.
 *
 * What this flow is here to prove, in the ADR's own terms:
 *
 *  - a disclosure runs a purpose-bound ceremony over an ENUMERATED key set;
 *  - the window gates the PROMPT: a second disclosure inside a live window
 *    needs no second ceremony, and the countdown chip says how long is left;
 *  - a revealed value re-masks on a visible countdown;
 *  - clipboard copy of a secret is a disclosure and is recorded as one, even
 *    from the masked state;
 *  - copy into another environment runs the SAME enumerated-key ceremony, and
 *    there is no `confirm()` anywhere;
 *  - one audit row per disclosed key, never one row for the batch;
 *  - **at an effective window of 0 (a protected environment) the ceremony
 *    offers no TOTP option and the server refuses a code**, so a passkey is
 *    required per disclosure.
 *
 * The passkey legs run against Chromium's virtual authenticator over CDP.
 * That is the only way to exercise a WebAuthn ceremony end to end, and the
 * criterion is specifically about WebAuthn.
 */

const seed = readSeed();
const VALUES_PATH = `/orgs/${seed.org}/projects/${seed.project}/environments/${seed.dev}/values`;
const PROD_PATH = `/orgs/${seed.org}/projects/${seed.project}/environments/${seed.prod}/values`;

function instanceGrantPath(principal: string, capability: string): string {
  const query = new URLSearchParams({ principal, capability });
  return `/api/v1/instance/grants?${query.toString()}`;
}

function orgGrantPath(principal: string, capability: string): string {
  const query = new URLSearchParams({ principal, capability });
  return `/api/v1/orgs/${seed.org}/grants?${query.toString()}`;
}

/** ROTATED is what the blind replacement writes, and what the readback expects. */
const ROTATED = 'rotated-blind';

/**
 * valueUpdatedAt reads a cell's `updated_at` through the masked listing, which
 * needs no `reveal` at all — write-presence is `read`-class. It is how a
 * fire-and-forget mutation is turned into something waitable without giving the
 * test capabilities the principal under test does not have.
 */
async function valueUpdatedAt(page: Page, environment: string, key: string): Promise<string> {
  return page.evaluate(
    async (input: { org: string; project: string; environment: string; key: string }) => {
      const resp = await fetch(
        `/api/v1/orgs/${input.org}/projects/${input.project}/environments/${input.environment}/values/${input.key}`,
        { credentials: 'same-origin' },
      );
      if (!resp.ok) {
        return `error ${String(resp.status)}`;
      }
      const body: unknown = await resp.json();
      if (typeof body !== 'object' || body === null) {
        return 'not an object';
      }
      const record: Record<string, unknown> = { ...body };
      const updated = record['updated_at'];
      return typeof updated === 'string' ? updated : 'absent';
    },
    { org: seed.org, project: seed.project, environment, key },
  );
}

/**
 * publishOwnDraft bridges the staging model until the matrix grows its publish
 * affordance (#56): since #51 a save only STAGES a pending change, so a flow
 * that asserts on DELIVERED state publishes its own draft through the API —
 * the same seam the CLI demo uses. It reads the caller's own
 * `pending_version_id` off the signals endpoint and publishes exactly that.
 */
async function publishOwnDraft(page: Page, environment: string, key: string): Promise<void> {
  const pending = await page.evaluate(
    async (
      input: { org: string; project: string; environment: string; key: string },
    ): Promise<{ versionID: string | null; error: string | null }> => {
      const base = `/api/v1/orgs/${input.org}/projects/${input.project}/environments/${input.environment}`;
      // The save is fire-and-forget from the DOM's point of view, so the
      // draft may not have committed by the time this runs. Poll the signals
      // endpoint until the caller's own pending version appears; the bound
      // exists so a genuinely lost write still fails loudly rather than
      // spinning.
      let versionID: string | undefined;
      for (let attempt = 0; attempt < 50 && versionID === undefined; attempt++) {
        const signals = await fetch(`${base}/signals`, { credentials: 'same-origin' });
        if (!signals.ok) {
          return { versionID: null, error: `signals ${String(signals.status)}` };
        }
        const body: unknown = await signals.json();
        if (typeof body !== 'object' || body === null) {
          return { versionID: null, error: 'signals: not a cells object' };
        }
        const cells: unknown = Object(body)['cells'];
        if (!Array.isArray(cells)) {
          return { versionID: null, error: 'signals: not a cells object' };
        }
        const cell = cells.find((candidate: unknown) => {
          if (typeof candidate !== 'object' || candidate === null) {
            return false;
          }
          return Object(candidate)['name'] === input.key;
        });
        const pendingVersion = cell === undefined ? undefined : Object(cell)['pending_version_id'];
        versionID = typeof pendingVersion === 'string' ? pendingVersion : undefined;
        if (versionID === undefined) {
          await new Promise((resolve) => setTimeout(resolve, 100));
        }
      }
      if (versionID === undefined) {
        return { versionID: null, error: 'no pending draft for key' };
      }
      return { versionID, error: null };
    },
    { org: seed.org, project: seed.project, environment, key },
  );
  if (pending.error !== null || pending.versionID === null) {
    throw new Error(`publishing the staged draft: ${pending.error ?? 'missing version id'}`);
  }
  const base = `/api/v1/orgs/${seed.org}/projects/${seed.project}/environments/${environment}`;
  await browserApi(page, 'POST', `${base}/publish`, zPublishResult, {
    version_ids: [pending.versionID],
  });
}

/** auditLines is the surface's per-key disclosure record. */
function auditLines(page: Page) {
  return page.getByRole('region', { name: 'Disclosure records' }).getByRole('listitem');
}

/**
 * A fresh page, authenticator, and session per test. The passkey fixture writes
 * the advanced counter before Playwright closes each page, so the next test
 * starts where the previous authenticator stopped instead of looking cloned.
 *
 * A fresh session matters because a reauthentication window is a property
 *    of the SESSION. Carrying one over would mean the second test's first
 *    disclosure quietly skipped the ceremony it exists to assert.
 *
 * Signing in mints a new session and touches no other, so neither the shared
 * storage state nor any other flow is disturbed.
 */
test.describe('reveal ceremonies', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ permissions: ['clipboard-read', 'clipboard-write'] });

  let page: Page;

  test.beforeEach(async ({ passkeyPage }) => {
    page = passkeyPage;
    // An empty jar guarantees this test starts with no reauthentication window.
    await page.context().clearCookies();
    await page.goto(VALUES_PATH);
    await establishSession(page);
    await page.goto(VALUES_PATH);
    await expect(page.getByRole('heading', { name: 'Values', level: 1 })).toBeVisible();
  });

  test('a reveal takes a purpose-bound ceremony over the enumerated keys', async () => {
    const secret = seed.secrets[0] ?? '';
    const trailBefore = countDisclosureEvents();
    // Masked before anything happens: write-presence, never plaintext.
    await expect(page.getByLabel(`${secret} is masked`)).toBeVisible();

    await page.getByRole('button', { name: `Reveal ${secret}` }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    // Purpose-bound: the act and the environment, not "authenticate".
    // Purpose-bound means the ACT and the ENVIRONMENT BY NAME — `reveal ·
    // development`, never an opaque id, which is the modal's headline feature.
    await expect(dialog.getByRole('heading', { level: 2 })).toHaveText('reveal · development');
    // Disclosure reauth is not account step-up, and the modal says so.
    await expect(dialog).toContainText('disclosure');
    // The enumerated set: one key, listed.
    await expect(dialog).toContainText('One decision over exactly the 1 key below');
    await expect(dialog.getByRole('list', { name: 'Keys this decision covers' })).toContainText(
      secret,
    );

    await dialog.getByRole('button', { name: 'Use a passkey' }).click();
    await expect(dialog).toBeHidden();

    // The value is on screen, and the remask countdown is visible.
    await expect(page.getByText('hunter2-development')).toBeVisible();
    await expect(page.getByText(/re-masks in \d+s/)).toBeVisible();

    // One audit line, for one key. Never "revealed N secrets".
    await expect(auditLines(page)).toHaveCount(1);
    await expect(auditLines(page).first()).toContainText(secret);
    // And the SERVER agrees. The list above is the UI's belief; per-key
    // cardinality is a property of the trail, so it is asserted against the
    // trail — a server that aggregated would pass the first check and fail
    // this one, which is the whole point of having both.
    expect(countDisclosureEvents() - trailBefore, 'server-side disclosure rows').toBe(1);
  });

  test('the value re-masks on its own', async () => {
    const secret = seed.secrets[0] ?? '';
    // The clock has to be patched before the document loads, or the surface's
    // ticker is a real one and the fast-forward moves nothing.
    await page.clock.install();
    await page.goto(VALUES_PATH);
    await page.getByRole('button', { name: `Reveal ${secret}` }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Use a passkey' }).click();
    await expect(page.getByText('hunter2-development')).toBeVisible();

    // The clock, not a real wait: a suite that slept ten seconds per assertion
    // would be measuring patience.
    await page.clock.fastForward(11_000);
    await expect(page.getByText('hunter2-development')).toBeHidden();
    await expect(page.getByLabel(`${secret} is masked`)).toBeVisible();
  });

  test('the window gates the prompt, so a second reveal does not ask again', async () => {
    const [first = '', second = ''] = seed.secrets;
    const trailBefore = countDisclosureEvents();
    await page.getByRole('button', { name: `Reveal ${first}` }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Use a passkey' }).click();
    await expect(page.getByRole('dialog')).toBeHidden();

    // The chip is the window made visible, and it counts down.
    const chip = page.getByText(/Reveal window · \d+s/);
    await expect(chip).toBeVisible();
    await expectStatusIsTextAndAria(page, chip);

    // Inside the window: no modal, and the disclosure still audits.
    await page.getByRole('button', { name: `Reveal ${second}` }).click();
    await expect(page.getByText('sk_test_development')).toBeVisible();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(auditLines(page)).toHaveCount(2);
    expect(countDisclosureEvents() - trailBefore, 'server-side disclosure rows').toBe(2);
  });

  test('copying a secret is a disclosure, and copying a config value is free', async () => {
    const secret = seed.secrets[0] ?? '';

    // Copy WITHOUT display: the cell is still masked when the copy is asked
    // for, and the ceremony runs anyway.
    await expect(page.getByLabel(`${secret} is masked`)).toBeVisible();
    await page.getByRole('button', { name: `Copy ${secret}` }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Use a passkey' }).click();

    const notice = page.getByRole('status').filter({ hasText: 'recorded as a disclosure' });
    await expect(notice).toBeVisible();
    // The honest caveat, verbatim: the OS may keep clipboard history.
    await expect(notice).toContainText('the OS may keep clipboard history');
    await expect(auditLines(page)).toHaveCount(1);
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('hunter2-development');

    // A config value is not a secret, so no ceremony and no disclosure record.
    await page.getByRole('button', { name: `Copy ${seed.config}` }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(
      page.getByRole('status').filter({ hasText: 'no disclosure was recorded' }),
    ).toBeVisible();
    await expect(auditLines(page)).toHaveCount(1);
  });

  test('a live source window does not stand in for the protected destination', async () => {
    // The hole this asserts: reveal in development (which opens development's
    // sliding window), then publish into production. A surface that consulted
    // the SOURCE window would skip the ceremony entirely, and the protected
    // environment's "a passkey per decision" would never be asked for.
    const secret = seed.secrets[0] ?? '';
    await page.getByRole('button', { name: `Reveal ${secret}` }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Use a passkey' }).click();
    await expect(page.getByRole('dialog')).toBeHidden();
    await expect(page.getByText(/Reveal window · \d+s/)).toBeVisible();

    await page.getByLabel('Publish into').selectOption(seed.prod);
    await page.getByRole('button', { name: 'Publish into environment' }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    // The source window IS live, so the source leg passes without a prompt and
    // the one decision left is the destination's — named as such.
    await expect(dialog.getByRole('heading', { level: 2 })).toHaveText('publish into · production');
    // Protected, so no code option — the destination's cap, not the source's.
    await expect(dialog.getByLabel('Or a code from your authenticator')).toHaveCount(0);
    await dialog.getByRole('button', { name: 'Use a passkey' }).click();
    await expect(dialog).toBeHidden();
    await expect(auditLines(page).first()).toContainText(`Copied into ${seed.prod}`);
  });

  test('publishing into another environment runs the same enumerated ceremony', async () => {
    // A browser confirm() would deadlock this test rather than pass it: the
    // surface must not have one, and nothing here handles a dialog event.
    let nativeDialog = false;
    page.on('dialog', () => {
      nativeDialog = true;
    });

    await page.getByLabel('Publish into').selectOption(seed.prod);
    await page.getByRole('button', { name: 'Publish into environment' }).click();

    const dialog = page.getByRole('dialog');
    // TWO decisions, because a copy has two ends: the material leaves
    // development (a disclosure) and lands in production (protected). Each is
    // its own enumerated-key ceremony over the same key set.
    for (const title of ['copy · development', 'publish into · production']) {
      await expect(dialog).toBeVisible();
      await expect(dialog.getByRole('heading', { level: 2 })).toHaveText(title);
      await expect(dialog).toContainText(
        `One decision over exactly the ${seed.secrets.length} keys below`,
      );
      for (const secret of seed.secrets) {
        await expect(dialog.getByRole('list', { name: 'Keys this decision covers' })).toContainText(
          secret,
        );
      }
      await dialog.getByRole('button', { name: 'Use a passkey' }).click();
    }
    await expect(dialog).toBeHidden();

    // One line per key per destination, again never a batch summary.
    await expect(auditLines(page)).toHaveCount(seed.secrets.length);
    expect(nativeDialog, 'the surface used a native confirm()').toBe(false);
  });

  test('a non-protected ceremony can be answered with a code, not only a passkey', async () => {
    // The TOTP SUCCESS path. Every other ceremony here is answered with a
    // passkey and every other TOTP assertion in the suite is a refusal, so a
    // code path that opened a window nothing could spend would ship green.
    const secret = seed.secrets[0] ?? '';
    const trailBefore = countDisclosureEvents();
    await page.getByRole('button', { name: `Reveal ${secret}` }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    const code = dialog.getByLabel('Or a code from your authenticator');
    // Offered at all only because this environment's window is not capped at 0.
    await expect(code).toBeVisible();

    // A step nothing has spent: every code is single-use per (account, step),
    // and seeding burned several on its way to a stepped-up session.
    await code.fill(await nextTotpCode());
    await dialog.getByRole('button', { name: 'Authorise with a code' }).click();
    await expect(dialog).toBeHidden();

    await expect(page.getByText('hunter2-development')).toBeVisible();
    // A code opens a SLIDING window, so the chip counts down rather than
    // announcing a single decision.
    await expect(page.getByText(/Reveal window · \d+s/)).toBeVisible();
    expect(countDisclosureEvents() - trailBefore, 'server-side disclosure rows').toBe(1);
  });

  test('navigating to another environment re-masks', async () => {
    // React Router reuses this component when only the route parameters
    // change. Without an explicit reset the plaintext disclosed in development
    // is still in state a moment later — and key names repeat across
    // environments, so it would render in PRODUCTION's row for the same key.
    const secret = seed.secrets[0] ?? '';
    await page.getByRole('button', { name: `Reveal ${secret}` }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Use a passkey' }).click();
    await expect(page.getByText('hunter2-development')).toBeVisible();

    // A client-side navigation, not a reload: a reload would clear everything
    // for a reason that has nothing to do with the fix.
    await page.evaluate((path: string) => {
      window.history.pushState({}, '', path);
      window.dispatchEvent(new PopStateEvent('popstate'));
    }, PROD_PATH);

    await expect(page).toHaveURL(new RegExp(`${seed.prod}/values$`));
    await expect(page.getByText('hunter2-development')).toHaveCount(0);
    await expect(page.getByLabel(`${secret} is masked`)).toBeVisible();
    // The session record goes with it: those disclosures were about somewhere
    // else, and leaving them under a production heading is a lie.
    await expect(auditLines(page)).toHaveCount(0);
  });

  test('meets the pinned assertion set with the ceremony open', async () => {
    // S3 asks for the pinned set "for the components touched", and the
    // ceremony is the component this ticket exists for — asserting only the
    // resting surface would leave the modal, its key list and its code form
    // unchecked. The dialog is native, so this also proves the focus ring and
    // contrast hold inside the top layer.
    const secret = seed.secrets[0] ?? '';
    await page.getByRole('button', { name: `Reveal ${secret}` }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel('Or a code from your authenticator')).toBeVisible();

    await expectPinnedAssertionSet(page, {
      flow: 'reveal',
      surface: 'values',
      theme: 'dark',
      text: [
        dialog.getByRole('heading', { level: 2 }),
        dialog.getByRole('list', { name: 'Keys this decision covers' }),
      ],
      radii: [[dialog, 'container']],
      fonts: [[dialog.getByRole('heading', { level: 2 }), 'ui']],
      colours: [[dialog, 'backgroundColor', '--bg-panel']],
      hairlines: [dialog],
      density: [[dialog.getByRole('button', { name: 'Use a passkey' }), '--touch']],
    });
  });

  test('a write-only key offers replacement, never a disabled field', async () => {
    const secret = seed.secrets[0] ?? '';
    await page.getByRole('button', { name: secret, exact: true }).click();
    const field = page.getByLabel(`New value for ${secret}`);
    await expect(field).toBeVisible();
    await expect(field).toBeEnabled();
    // This principal HOLDS reveal here, so the field says the other thing:
    // empty means unchanged, and there is no per-row clear — the prototype's
    // resolution removed it. The blind-replacement wording is asserted in the
    // write-only flow below, where the capability is genuinely absent.
    await expect(field).toHaveAttribute('placeholder', 'Leave empty to keep unchanged');
    await expect(field).toHaveAttribute('data-write-only', 'false');

    // The pinned set again, over the editor and a POPULATED audit region —
    // both are components this flow touches and neither is on the resting
    // surface.
    await page.getByRole('button', { name: `Reveal ${secret}` }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Use a passkey' }).click();
    await expect(auditLines(page).first()).toBeVisible();

    await expectPinnedAssertionSet(page, {
      flow: 'reveal',
      surface: 'values',
      theme: 'dark',
      text: [auditLines(page).first(), page.getByText(/re-masks in \d+s/).first()],
      radii: [[field, 'control']],
      fonts: [[field, 'mono']],
      colours: [],
      hairlines: [],
      density: [[field, '--touch']],
    });
  });

  /**
   * The protected environment: mvp-boundary A5's [E2E] line.
   *
   * A protected environment caps the window at 0, so TOTP cannot honour the gate
   * and the ceremony must not offer it. The refusal is asserted at BOTH levels —
   * the modal has no code field, and the server answers 409 to a code presented
   * directly — because a UI that merely hides the option is a convention, and
   * the criterion is about what the server will accept.
   */
  test('the ceremony offers no code option, and the server refuses one', async () => {
    await page.context().clearCookies();
    await page.goto(PROD_PATH);
    await establishSession(page);
    await page.goto(PROD_PATH);

    // The chip states the cap in words, before anything is attempted.
    const chip = page.getByText(/Protected · a passkey per disclosure/);
    await expect(chip).toBeVisible();
    await expectStatusIsTextAndAria(page, chip);

    await page.getByRole('button', { name: 'Reveal every secret' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('every disclosure takes its own passkey ceremony');
    // Absent, not disabled.
    await expect(dialog.getByLabel('Or a code from your authenticator')).toHaveCount(0);

    // And the server agrees: a code presented against this environment is
    // refused by the ENVIRONMENT's state (409), not as a bad code (401).
    try {
      await browserApi(page, 'POST', '/api/v1/auth/reauth/totp', zReauthResult, {
        // The code's VALUE is irrelevant and that is the point: the window
        // check runs before any code is verified, so a 409 here proves the
        // environment refused the factor rather than the code being wrong.
        environment_id: seed.prod,
        code: '000000',
      });
      throw new Error('a TOTP reauth against a 0-window environment unexpectedly succeeded');
    } catch (error) {
      if (!(error instanceof BrowserApiError)) {
        throw error;
      }
      expect(error.status, 'a TOTP reauth against a 0-window environment').toBe(409);
    }

    // And the positive half of "per disclosure": the passkey ceremony
    // authorises ONE, and the next disclosure asks again. A window that
    // survived would make "per disclosure" a description of the first one only.
    await dialog.getByRole('button', { name: 'Use a passkey' }).click();
    await expect(dialog).toBeHidden();
    // Disclosed, asserted by the remask countdown rather than by a literal
    // value: an earlier test in this file publishes INTO production, so the
    // material here is whatever the last authorised copy put there — which is
    // the honest state of a real environment and not something a flow should
    // pin.
    await expect(page.getByText(/re-masks in \d+s/).first()).toBeVisible();
    await expect(auditLines(page)).toHaveCount(seed.secrets.length);
    // No standing countdown: a single decision is not a window.
    await expect(page.getByText(/Reveal window · \d+s/)).toHaveCount(0);

    await page.getByRole('button', { name: 'Reveal every secret' }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog')).toBeHidden();

    // CLIPBOARD COPY IN A PROTECTED ENVIRONMENT. This is where a ceremony that
    // signed the wrong operation shows up: the assertion is bound
    // byte-exactly, so a copy that signed `copy` and then took the reveal
    // route would be refused here every single time — and a sliding
    // environment would never reveal it, because sliding windows are
    // deliberately not purpose-scoped.
    const trailBefore = countDisclosureEvents();
    const secret = seed.secrets[0] ?? '';
    await page.getByRole('button', { name: `Copy ${secret}` }).click();
    const copyDialog = page.getByRole('dialog');
    await expect(copyDialog).toBeVisible();
    // Told the truth about what it is, while signing the route it takes.
    await expect(copyDialog.getByRole('heading', { level: 2 })).toHaveText(
      'copy to clipboard · production',
    );
    await copyDialog.getByRole('button', { name: 'Use a passkey' }).click();
    await expect(copyDialog).toBeHidden();
    await expect(
      page.getByRole('status').filter({ hasText: 'recorded as a disclosure' }),
    ).toBeVisible();
    expect(countDisclosureEvents() - trailBefore, 'server-side disclosure rows').toBe(1);
  });
});

/** The complete popup return leg against the browser-drivable fake IdP. */
test.describe('OIDC disclosure reauthentication', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: { cookies: [], origins: [] } });

  async function signInWithOIDC(page: Page): Promise<void> {
    await page.goto('/login');
    const oidcSignIn = page.getByRole('button', {
      name: `Continue with ${OIDC_PROVIDER.displayName}`,
    });
    // Global setup configures and links the provider before this test starts.
    // A missing button is therefore a failed discovery read, not a provider
    // seed race; fail on that signal instead of hiding it behind reloads.
    await expect(oidcSignIn).toBeVisible({ timeout: 15_000 });
    await oidcSignIn.click();
    // The org rail is desktop chrome — a phone reaches organisations through
    // the drawer, so the rail is `display:none` there. What proves the shell
    // came up at BOTH widths is the breadcrumb, which only the authenticated
    // chrome renders.
    await expect(page.getByRole('list', { name: 'Breadcrumb' })).toBeVisible({ timeout: 15_000 });
  }

  test('reveals after the IdP popup returns through the SPA done page', async ({ page }) => {
    await signInWithOIDC(page);
    await page.goto(VALUES_PATH);
    const secret = seed.secrets[0] ?? '';
    await page.getByRole('button', { name: `Reveal ${secret}` }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    // The reauth dialog offers the OIDC option once its provider query settles.
    const reauth = dialog.getByRole('button', {
      name: `Re-authenticate with ${OIDC_PROVIDER.displayName}`,
    });
    await expect(reauth).toBeVisible({ timeout: 15_000 });

    const popupOpened = page.waitForEvent('popup');
    await reauth.click();
    await popupOpened;
    await expect(dialog).toBeHidden();
    await expect(page.getByText('hunter2-development')).toBeVisible();
  });

  test('keeps a protected zero-window environment passkey-only', async ({ page }) => {
    await signInWithOIDC(page);
    await page.goto(PROD_PATH);
    await page.getByRole('button', { name: 'Reveal every secret' }).click();
    const dialog = page.getByRole('dialog');

    await expect(
      dialog.getByRole('button', { name: `Re-authenticate with ${OIDC_PROVIDER.displayName}` }),
    ).toHaveCount(0);
    await expect(dialog).toContainText(
      'Your identity provider cannot satisfy a per-disclosure gate; use a passkey.',
    );
    await expect(dialog.getByRole('button', { name: 'Use a passkey' })).toBeVisible();
  });
});

/**
 * Write-only editing, with the capability genuinely absent (permission-model
 * ADR § `edit` and `publish` are separate: "`edit` without `reveal` is a valid,
 * supported state — write-only replacement (blind rotation)… the UI MUST
 * support the write-only editing path").
 *
 * It takes the administrator's `reveal` away for the duration rather than
 * inventing a second account, because this product has no second-account path:
 * `admin create` mints the FIRST administrator and refuses a second. Revoking
 * a grant advances the principal's session generation and kills every session
 * they hold, so the grant is restored and the suite's shared session re-minted
 * afterwards — leaving the instance as this file found it.
 */
test.describe('write-only editing', () => {
  test.describe.configure({ mode: 'serial' });

  test('replaces a value the principal may not read', async ({ passkeyPage: page }) => {
    await page.context().clearCookies();
    await page.goto(VALUES_PATH);
    await establishSession(page);

    // Take both inherited `reveal` lines away: the original instance grant and
    // the creator-admin grant now installed at org scope.
    await browserApi(page, 'DELETE', instanceGrantPath(seed.principal, 'reveal'), z.null());
    await page.context().clearCookies();
    await page.goto(VALUES_PATH);
    await establishSession(page);
    await browserApi(page, 'DELETE', orgGrantPath(seed.principal, 'reveal'), z.null());

    try {
      // The revoke killed that session with it, which is the deprovisioning
      // rule working; sign in again.
      await page.context().clearCookies();
      await page.goto(VALUES_PATH);
      await establishSession(page);
      await page.goto(VALUES_PATH);

      const secret = seed.rotatable;
      const before = await valueUpdatedAt(page, seed.dev, secret);
      // No reveal affordance at all — there is nothing to offer.
      await expect(page.getByRole('button', { name: `Reveal ${secret}` })).toHaveCount(0);
      await expect(page.getByLabel(`${secret} is masked`)).toBeVisible();

      await page.getByRole('button', { name: secret, exact: true }).click();
      const field = page.getByLabel(`New value for ${secret}`);
      // Enabled, not disabled: the capability is `edit`, and refusing to render
      // a field would invent a prerequisite the permission model rejects.
      await expect(field).toBeEnabled();
      await expect(field).toHaveAttribute('data-write-only', 'true');
      await expect(field).toHaveAttribute(
        'placeholder',
        'Replace without seeing the current value',
      );
      await expect(page.getByText('You may replace this value but not read it')).toBeVisible();

      // And the blind replacement actually LANDS. A masked cell and no alert
      // prove nothing — the mutation is fire-and-forget from the DOM's point
      // of view — so the value is read back through the API afterwards, under
      // the reveal this principal is about to get again.
      await field.fill(ROTATED);
      await page.getByRole('button', { name: 'Save draft' }).click();
      // No error may surface. The app-level assertive announcer holds
      // role="alert" for its whole lifetime — it must exist empty before an
      // announcement lands — so "no alert fired" is pinned as an absent error
      // toast plus an announcer that stayed empty.
      await expect(page.locator('.toast--error')).toHaveCount(0);
      await expect(page.locator('.visually-hidden[role="alert"]')).toHaveText('');
      await expect(page.getByLabel(`${secret} is masked`)).toBeVisible();
      // A save STAGES (#51); the blind edit only lands on delivery when its
      // draft is published. `edit` staged it, `publish` commits it — and
      // neither needs `reveal`, which is the point of the write-only path.
      await publishOwnDraft(page, seed.dev, secret);
      // Wait for the publish to have reached the server before restoring the
      // grant, so the readback below cannot race it.
      await expect
        .poll(async () => valueUpdatedAt(page, seed.dev, secret), { timeout: 5_000 })
        .not.toBe(before);
    } finally {
      await page.context().clearCookies();
      await page.goto(VALUES_PATH);
      await establishSession(page);
      await browserApi(page, 'POST', '/api/v1/instance/grants', zGrantResult, {
        principal: seed.principal,
        capability: 'reveal',
      });
      await page.context().clearCookies();
      await page.goto(VALUES_PATH);
      await establishSession(page);
      await browserApi(page, 'POST', `/api/v1/orgs/${seed.org}/grants`, zGrantResult, {
        principal: seed.principal,
        capability: 'reveal',
      });
    }

    // Restored, so read it back: the blind rotation stored exactly what was
    // typed, which is the only thing that proves a write-only edit is a real
    // edit rather than a form that looked like one.
    await page.context().clearCookies();
    await page.goto(VALUES_PATH);
    await establishSession(page);
    await page.goto(VALUES_PATH);
    await page.getByRole('button', { name: `Reveal ${seed.rotatable}` }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Use a passkey' }).click();
    await expect(page.getByText(ROTATED)).toBeVisible();
  });
});

/**
 * The pinned assertion set over the surface this flow claims, in both themes.
 * The matrix is derived from the registry, so claiming a surface and asserting
 * it are the same act.
 */
test.describe('pinned assertion set', () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  for (const scheme of ['dark', 'light'] as const) {
    test(`meets the pinned assertion set on Values (${scheme})`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(VALUES_PATH);
      // No ceremony here, so no second factor and no authenticator: the
      // surface itself is `read`, and asking for more would make this assert
      // the login path instead of the design tokens.
      await establishSession(page, false);
      await page.goto(VALUES_PATH);
      await expect(page.getByRole('heading', { name: 'Values', level: 1 })).toBeVisible();

      const heading = page.getByRole('heading', { name: 'Values', level: 1 });
      const well = page.locator('.card');
      const chip = page.locator('.chip').first();
      const revealAll = page.getByRole('button', {
        name: 'Reveal every secret',
      });

      await expectPinnedAssertionSet(page, {
        flow: 'reveal',
        surface: 'values',
        theme: scheme,
        text: [heading, chip],
        radii: [
          [well, 'container'],
          [revealAll, 'control'],
          [chip, 'badge'],
        ],
        fonts: [
          [heading, 'ui'],
          [page.locator('.values__masked').first(), 'mono'],
        ],
        colours: [
          [heading, 'color', '--tx'],
          [well, 'backgroundColor', '--bg-panel'],
          [well, 'borderTopColor', '--line'],
        ],
        hairlines: [well],
        density: [[revealAll, '--touch']],
      });
    });
  }
});
