import { readFileSync } from 'node:fs';

import { expect, type Page } from '@playwright/test';
import { zEnvironment, zOrg, zProject, zServiceAccount } from '@hikyo/zod';
import { z } from 'zod';

import { expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import {
  BASE_URL,
  establishSession,
  nextTotpCode,
  readSeed,
} from '../fixtures/instance.ts';
import { test } from '../fixtures/passkey.ts';
import { surfacesForFlow } from '../registry.ts';

const COLOR_SCHEMES: readonly ['dark', 'light'] = ['dark', 'light'];

/**
 * Flow: machine access (registry surface `machine-access`) — mvp-boundary S3's
 * "all three tabs + row expansion + display-once mint", against the locked
 * prototype #31 iteration 3.
 *
 * What this flow proves, in the ADRs' own terms:
 *
 *  - the inventory is a TABBED one — service accounts, federation, Kubernetes
 *    targets — and each tab renders the state this build actually holds;
 *  - a credential row is METADATA ONLY: prefix hint, kind, expiry in words,
 *    last used, and never a value;
 *  - expanding a service account shows credentials and federated bindings on
 *    the left, delivery targets and actions on the right, and the five-step
 *    setup journey full-width below;
 *  - the mint is DISPLAY-ONCE: the step-up names the post-state formula, the
 *    value appears exactly once, a stored-confirmation checkbox gates dismiss,
 *    and after dismissal the value is nowhere on the page while the new row is;
 *  - a federated binding renders byte-exactly, and the form REFUSES a
 *    pull-request event until it is deliberately bound.
 *
 * The passkey machinery is here for the session, not for the mint: a
 * `manage-members` read of the grant rows is MFA-mandatory, so the surface's
 * scope column needs a stepped-up session to exist at all.
 */

const seed = readSeed();
const PATH = `/orgs/${seed.org}/projects/${seed.project}/machine-access`;

/** accountRow is the disclosure button that expands one service account. */
function accountRow(page: Page, name: string) {
  return page.getByRole('button', { name, exact: false }).first();
}

/**
 * revokeMinted retires everything this flow minted, and it is not tidiness.
 *
 * The instance caps concurrent live credentials per service account at five,
 * and this file mints three times per Playwright project across two viewport
 * projects — so without revoking, the sixth mint is refused by the cap and the
 * failure reads as a broken mint rather than as a test that littered. Revoking
 * is also the second half of the rotation the ADR describes: mint, distribute,
 * then revoke.
 */
async function revokeMinted(page: Page) {
  const expansion = page.locator('.machine__sub');
  const buttons = expansion.getByRole('button', { name: /^Revoke hik_1_wl_/ });
  for (let remaining = await buttons.count(); remaining > 0; remaining--) {
    await buttons.first().click();
    await expect(expansion.locator('.cred')).toHaveCount(remaining - 1);
  }
  await expect(expansion.locator('.cred')).toHaveCount(0);
}

test.describe('machine access', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ permissions: ['clipboard-read', 'clipboard-write'] });

  let page: Page;

  test.beforeEach(async ({ passkeyPage }) => {
    page = passkeyPage;
    await page.context().clearCookies();
    await page.goto(PATH);
    await establishSession(page);
    await page.goto(PATH);
    await expect(page.getByRole('heading', { name: 'Machine access', level: 1 })).toBeVisible();
  });

  test('the inventory has five tabs, and every one of them says what it holds', async () => {
    const tabs = page.getByRole('tab');
    await expect(tabs).toHaveCount(5);
    await expect(tabs.nth(0)).toHaveText(/Service accounts \(3\)/);
    await expect(tabs.nth(1)).toHaveText(/Federation \(1\)/);
    await expect(tabs.nth(2)).toHaveText(/Kubernetes targets \(0\)/);
    await expect(tabs.nth(3)).toHaveText(/Providers \(0\)/);
    await expect(tabs.nth(4)).toHaveText(/Leases \(0\)/);

    // The policy strip: the per-project opt-in is stated, not offered as a
    // control whose only outcome would be a refusal.
    const policy = page.locator('.machine__policy');
    await expect(policy).toContainText('per-project opt-in): off.');
    await expectStatusIsTextAndAria(page, policy);

    // The inventory itself: every seeded account, with its immutable kind.
    for (const name of [seed.machine.workload, seed.machine.automation, seed.machine.mintable]) {
      await expect(page.getByRole('button', { name, exact: false })).toBeVisible();
    }
    await expect(page.getByRole('row').filter({ hasText: seed.machine.workload })).toContainText(
      'development',
    );
    await expect(page.getByRole('row').filter({ hasText: seed.machine.automation })).toContainText(
      'automation',
    );

    // The federation tab renders the binding BYTE-EXACTLY. Nothing folds case,
    // resolves the URL or strips a slash, so what was seeded is what is here.
    await page.getByRole('tab', { name: 'Federation' }).click();
    await expect(page.getByText(seed.machine.issuer, { exact: true })).toBeVisible();
    await expect(page.getByText(seed.machine.subject, { exact: true })).toBeVisible();
    await expect(page.getByText(seed.machine.audience, { exact: true })).toBeVisible();

    // The Kubernetes tab is EMPTY and says why. An empty list here must not
    // read as "everything is healthy".
    await page.getByRole('tab', { name: 'Kubernetes targets' }).click();
    const empty = page.getByRole('status').filter({ hasText: 'No delivery targets are reported' });
    await expect(empty).toContainText('never that everything is healthy');
    await expect(empty).toContainText('Check HikyoSecret conditions with kubectl');
    await expect(empty).not.toContainText('not part of this build');
    await expectStatusIsTextAndAria(page, empty);

    // The Providers tab is empty on a fresh project and says so.
    await page.getByRole('tab', { name: 'Providers' }).click();
    const providersEmpty = page
      .getByRole('status')
      .filter({ hasText: 'No dynamic-secret providers on this project yet' });
    await expect(providersEmpty).toBeVisible();
    await expectStatusIsTextAndAria(page, providersEmpty);
    // Configuring a provider is offered, since a live session admits it.
    await expect(page.getByRole('button', { name: 'Configure provider' })).toBeEnabled();

    // The Leases tab is status-only and empty on a fresh project; it never shows
    // a secret. With no provider yet, minting is held back and says why.
    await page.getByRole('tab', { name: 'Leases' }).click();
    const leasesEmpty = page
      .getByRole('status')
      .filter({ hasText: 'No dynamic-secret leases on this project yet' });
    await expect(leasesEmpty).toBeVisible();
    await expectStatusIsTextAndAria(page, leasesEmpty);
    await expect(page.getByRole('button', { name: 'Mint lease' })).toBeDisabled();
    await expect(
      page.getByText('Configure a provider with a credential first', { exact: false }),
    ).toBeVisible();
  });

  test('configuring a provider validates its inputs before it dials anything', async () => {
    await page.getByRole('tab', { name: 'Providers' }).click();
    await page.getByRole('button', { name: 'Configure provider' }).click();

    const dialog = page.getByRole('dialog');
    await expect(
      dialog.getByRole('heading', { name: 'Configure dynamic-secret provider' }),
    ).toBeVisible();

    // Submitting with empty fields is refused CLIENT-side: no origin is dialled
    // until the inputs are complete, so the operator meets a form refusal, not a
    // probe failure.
    await dialog.getByRole('button', { name: 'Configure provider' }).click();
    await expect(
      dialog.getByRole('alert').filter({ hasText: 'are all required' }),
    ).toBeVisible();

    // The admin credential is a write-only password field — never rendered back.
    await expect(dialog.getByLabel('Admin credential (write-only)')).toHaveAttribute(
      'type',
      'password',
    );

    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
  });

  test('expanding a row shows credentials, bindings, targets and the journey below', async ({}, testInfo) => {
    const toggle = accountRow(page, seed.machine.workload);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');

    const expansion = page.locator('.machine__sub');
    await expect(expansion.getByRole('heading', { name: 'Credentials' })).toBeVisible();
    await expect(expansion.getByRole('heading', { name: 'Federated bindings' })).toBeVisible();
    await expect(expansion.getByRole('heading', { name: 'Delivery targets' })).toBeVisible();
    await expect(expansion.getByRole('heading', { name: 'Setup journey' })).toBeVisible();

    // The journey is five steps. With the project's machine-reveal opt-in off
    // (the seeded default) step 4 is the next act and step 5 says the grant
    // API refuses until it is on, in words rather than a dead control.
    const steps = expansion.locator('.journey__step');
    await expect(steps).toHaveCount(5);
    await expect(steps.nth(1)).toContainText(`read granted — development`);
    await expect(steps.nth(3)).toContainText('Enable the project machine-reveal opt-in');
    await expect(steps.nth(3)).toContainText('next');
    await expect(steps.nth(4)).toContainText('refused by the grant API until the opt-in above is on');
    await expect(steps.nth(4).locator('.journey__state')).toHaveText('blocked');
    await expect(steps.nth(2)).toContainText('Read grant permits configuration delivery');
    await expect(steps.nth(2)).toContainText('a successful fetch has not been verified here');
    await expect(expansion).not.toContainText('not in this build');
    const overflowingClaimNames = await expansion.locator('.kv dt').evaluateAll((terms) =>
      terms.filter((term) => term.scrollWidth > term.clientWidth).map((term) => term.textContent),
    );
    expect(overflowingClaimNames, 'federation claim names must not overlap their values').toEqual([]);
    for (const colorScheme of COLOR_SCHEMES) {
      await page.emulateMedia({ colorScheme });
      await page.evaluate((theme) => {
        localStorage.setItem('hikyo.theme', theme);
        window.dispatchEvent(new StorageEvent('storage', { key: 'hikyo.theme' }));
      }, colorScheme);
      await expansion.locator('.journey').scrollIntoViewIfNeeded();
      await page.screenshot({ path: testInfo.outputPath(`machine-journey-${colorScheme}.png`) });
    }

    // An automation principal has no journey at all: it never delivers to a
    // workload.
    await toggle.click();
    await accountRow(page, seed.machine.automation).click();
    await expect(page.locator('.machine__sub')).toContainText('has no setup journey');
    await expect(page.locator('.journey__step')).toHaveCount(0);
  });

  test('the mint shows the value exactly once, and the confirmation gates dismiss', async () => {
    await accountRow(page, seed.machine.mintable).click();
    await page
      .getByRole('button', { name: `Mint credential for ${seed.machine.mintable}` })
      .click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('heading', { level: 2 })).toHaveText(
      `mint credential · ${seed.machine.mintable}`,
    );
    // The step-up names the POST-STATE formula, not what the mint adds — and
    // says honestly that this account reaches no plaintext, so nothing is
    // reauthenticated for a disclosure that cannot happen.
    await expect(dialog).toContainText('resulting post-state');
    await expect(dialog).toContainText('reaches no plaintext');

    await dialog.getByRole('button', { name: 'Mint credential' }).click();

    // The value, exactly once, in the credential grammar.
    const token = dialog.locator('.machine__token');
    await expect(token).toHaveText(/^hik_1_wl_/);
    const value = (await token.textContent()) ?? '';
    expect(value.length, 'the minted value').toBeGreaterThan(20);
    await expect(dialog).toContainText('never retrievable again');

    // Dismissal is GATED. Pressing Done without the confirmation refuses, in
    // words, and the value stays on screen rather than being lost.
    await dialog.getByRole('button', { name: 'Done' }).click();
    await expect(dialog.getByRole('alert')).toContainText('there is no second look');
    await expect(token).toBeVisible();

    await dialog.getByRole('checkbox').check();
    await dialog.getByRole('button', { name: 'Done' }).click();
    await expect(dialog).toBeHidden();

    // And it is gone: nothing on this page can return it, which is the whole
    // point of display-once. The row shows the PREFIX HINT instead.
    await expect(page.getByText(value)).toHaveCount(0);
    const expansion = page.locator('.machine__sub');
    await expect(expansion.locator('.cred')).toHaveCount(1);
    await expect(expansion.locator('.cred code')).toHaveText(/^hik_1_wl_.*…$/);
    await expect(expansion.locator('.cred')).toContainText('expires in');
    await expect(expansion.locator('.cred')).toContainText('never used');
    // The row shows a PREFIX, not the value: what is on screen is a strict,
    // short prefix of what was minted and nothing more.
    const hint = ((await expansion.locator('.cred code').textContent()) ?? '').replace('…', '');
    expect(value.startsWith(hint), 'the row shows a prefix of the minted value').toBe(true);
    expect(hint.length, 'the hint is far shorter than the value').toBeLessThan(value.length / 2);

    // Reopening creates a fresh reviewing lifecycle. The disclosed component
    // above was unmounted, and its old plaintext cannot be revived by the new
    // review before another credential is deliberately submitted.
    await expansion
      .getByRole('button', { name: `Mint credential for ${seed.machine.mintable}` })
      .click();
    const freshDialog = page.getByRole('dialog');
    await expect(freshDialog).toBeVisible();
    await expect(freshDialog.getByText(value, { exact: true })).toHaveCount(0);
    await freshDialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(freshDialog).toBeHidden();

    // Revoking is the other half of rotation, and it bites at the next request
    // rather than at expiry.
    await revokeMinted(page);
    await expect(
      page.getByRole('status').filter({ hasText: 'stops authenticating at the next request' }),
    ).toBeVisible();
  });

  test('escape does not throw away a value nothing can return', async () => {
    await accountRow(page, seed.machine.mintable).click();
    await page
      .getByRole('button', { name: `Mint credential for ${seed.machine.mintable}` })
      .click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('button', { name: 'Mint credential' }).click();
    await expect(dialog.locator('.machine__token')).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('alert')).toContainText('there is no second look');

    await dialog.getByRole('checkbox').check();
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await revokeMinted(page);
  });

  test('escape while the mint is in flight does not unmount the value', async () => {
    // The window this closes: Escape reaches a native <dialog> even when Cancel
    // is disabled, so a dismissal mid-flight would unmount the component the
    // server is about to hand a credential to — losing a value nothing can
    // return while leaving a live credential behind.
    //
    // The mint is delayed rather than stubbed: the request, the commit and the
    // response are all real, only slower, so what is asserted is the real
    // ordering rather than a fixture's.
    await page.route('**/service-accounts/*/credentials', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.fallback();
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, 1_500));
      await route.continue();
    });
    try {
      await accountRow(page, seed.machine.mintable).click();
      await page
        .getByRole('button', { name: `Mint credential for ${seed.machine.mintable}` })
        .click();
      const dialog = page.getByRole('dialog');
      await dialog.getByRole('button', { name: 'Mint credential' }).click();
      await expect(dialog.getByRole('button', { name: 'Minting…' })).toBeVisible();

      await page.keyboard.press('Escape');
      await expect(dialog).toBeVisible();

      // And the value still arrives, at the component that is still there.
      await expect(dialog.locator('.machine__token')).toHaveText(/^hik_1_wl_/, { timeout: 10_000 });
    } finally {
      await page.unroute('**/service-accounts/*/credentials');
    }

    const dialog = page.getByRole('dialog');
    await dialog.getByRole('checkbox').check();
    await dialog.getByRole('button', { name: 'Done' }).click();
    await expect(dialog).toBeHidden();
    await revokeMinted(page);
  });

  test('browser Back is a dismissal attempt, not a way to lose the value', async () => {
    // Escape goes through the dialog's own cancel event; Back pops the ROUTE,
    // which would unmount the component and everything it holds. The guard
    // turns the pop into the same gated dismissal attempt: mid-flight it is
    // ignored, with an unstored value it holds back, and the URL never moves.
    await page.route('**/service-accounts/*/credentials', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.fallback();
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, 1_500));
      await route.continue();
    });
    try {
      await accountRow(page, seed.machine.mintable).click();
      await page
        .getByRole('button', { name: `Mint credential for ${seed.machine.mintable}` })
        .click();
      const dialog = page.getByRole('dialog');
      await dialog.getByRole('button', { name: 'Mint credential' }).click();
      await expect(dialog.getByRole('button', { name: 'Minting…' })).toBeVisible();

      // Back mid-flight: ignored, the mint completes where it started.
      await page.goBack();
      await expect(dialog).toBeVisible();
      await expect(dialog.locator('.machine__token')).toHaveText(/^hik_1_wl_/, { timeout: 10_000 });
      expect(new URL(page.url()).pathname).toBe(PATH);

      // Back with the unstored value on screen: held back, in words.
      await page.goBack();
      await expect(dialog).toBeVisible();
      await expect(dialog.getByRole('alert')).toContainText('there is no second look');
      expect(new URL(page.url()).pathname).toBe(PATH);

      await dialog.getByRole('checkbox').check();
      await dialog.getByRole('button', { name: 'Done' }).click();
      await expect(dialog).toBeHidden();
    } finally {
      await page.unroute('**/service-accounts/*/credentials');
    }
    await revokeMinted(page);
  });

  test('the binding form refuses a pull-request event until it is deliberately bound', async () => {
    await page.getByRole('tab', { name: 'Federation' }).click();
    await page.getByRole('button', { name: 'New binding' }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    let createRequests = 0;
    page.on('request', (request) => {
      if (
        request.method() === 'POST' &&
        /\/service-accounts\/[^/]+\/bindings$/.test(new URL(request.url()).pathname)
      ) {
        createRequests += 1;
      }
    });
    // The Kubernetes preset fills the byte-exact issuer and the UID pin the
    // server refuses a binding without.
    await expect(dialog.getByLabel('Issuer')).toHaveValue(seed.machine.issuer);
    await expect(dialog.getByLabel(/ServiceAccount UID/)).toBeVisible();

    // A binding expires on the same terms as a bearer credential, and the
    // indefinite option is present-and-disabled with its reason rather than
    // absent — the instance opt-in that admits it is off by default.
    const lifetime = dialog.getByLabel('Binding lifetime');
    await expect(lifetime).toHaveValue('default');
    await expect(lifetime.getByRole('option', { name: /Indefinite/ })).toBeDisabled();
    await lifetime.selectOption('90d');

    await dialog.getByRole('button', { name: 'GitHub Actions' }).click();
    await expect(dialog.getByLabel(/Repository id/)).toBeVisible();
    // `push` is not a refusal.
    await expect(dialog.getByRole('alert')).toHaveCount(0);

    // A numeric pin is refused BEFORE the request when it is not a whole number
    // the issuer could have minted: an empty field would bind repository 0, and
    // anything past 2^53 rounds to a neighbouring repository id.
    await dialog.getByLabel(/Repository id \(repository_id\)/).fill('4242.7');
    await dialog.getByLabel(/Repository owner id/).fill('99');
    await dialog.getByLabel('Audience').fill(seed.machine.audience);
    await dialog.getByRole('button', { name: 'Bind this identity' }).click();
    await expect(dialog.getByRole('alert')).toContainText('must be a whole number');
    await dialog.getByLabel(/Repository id \(repository_id\)/).fill('4242');

    // The audience is mandatory, and refused here rather than as a 400.
    await dialog.getByLabel('Audience').fill('');
    await dialog.getByRole('button', { name: 'Bind this identity' }).click();
    await expect(dialog.getByRole('alert')).toContainText('An audience is mandatory');
    await dialog.getByLabel('Audience').fill(seed.machine.audience);

    // Issuer and subject carry the byte-exact identity. Empty values are
    // refused locally, before the create mutation can reach the server.
    const issuer = dialog.getByLabel('Issuer');
    const validIssuer = await issuer.inputValue();
    await issuer.fill('');
    await dialog.getByRole('button', { name: 'Bind this identity' }).click();
    await expect(dialog.getByRole('alert')).toContainText('An issuer is mandatory');
    expect(createRequests).toBe(0);
    await issuer.fill(validIssuer);

    const subject = dialog.getByLabel('Subject, matched byte-for-byte');
    const validSubject = await subject.inputValue();
    await subject.fill('');
    await dialog.getByRole('button', { name: 'Bind this identity' }).click();
    await expect(dialog.getByRole('alert')).toContainText('A subject is mandatory');
    expect(createRequests).toBe(0);
    await subject.fill(validSubject);

    await dialog.getByLabel('Event name').selectOption('pull_request_target');
    const refusal = dialog.getByRole('alert');
    await expect(refusal).toContainText('pull_request_target');
    await expect(refusal).toContainText("this service account's fetch authority");
    await expectStatusIsTextAndAria(page, refusal.first());

    // The refusal HOLDS: pressing bind without the acknowledgement is refused
    // by the surface, before the server is asked anything.
    await dialog.getByRole('button', { name: 'Bind this identity' }).click();
    await expect(dialog).toContainText('Acknowledge deliberately below');

    // And the acknowledgement is a deliberate act, not a default.
    const deliberate = dialog.getByRole('checkbox');
    await expect(deliberate).not.toBeChecked();
    await deliberate.check();
    await expect(deliberate).toBeChecked();

    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
  });

  test('the grant warning names the live credentials and the newly reachable keys', async () => {
    await accountRow(page, seed.machine.workload).click();
    await page
      .getByRole('button', { name: `Add environment grant to ${seed.machine.workload}` })
      .click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('Grants attach to the service account, never to a credential');
    // The two numbers nothing else on the surface says: how many credentials
    // this re-scopes, and exactly what becomes reachable. The count is the
    // SERVER's `live_credentials`, so an expired credential is not counted as
    // one this grant re-scopes.
    await expect(dialog).toContainText('re-scopes every credential already in circulation');
    await expect(dialog).toContainText('1 live credential');

    // The formula, and the honest statement that its disclosure conjunct is
    // vacuous here — the same sentence the mint makes, for the same reason.
    await expect(dialog).toContainText('the delta, not the whole post-state');
    await expect(dialog).toContainText('newly decrypts nothing');

    // What a `read` grant actually delivers: the whole key catalogue by name,
    // classification and presence — config keys included, unset keys included
    // — and no value of any classification.
    const keys = dialog.getByRole('list', { name: 'Keys this grant makes reachable' });
    for (const key of [...seed.secrets, seed.config]) {
      await expect(keys).toContainText(key);
    }
    await expect(keys).toContainText('config');
    await expect(keys).toContainText('secret');
    await expect(dialog).toContainText('No value of any classification is delivered');

    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
  });

  test('replaces a binding atomically, then revokes it', async () => {
    // A binding is matched globally on (issuer, subject), so a subject unique to
    // this Playwright project keeps the desktop and mobile runs from colliding
    // on the same identity. The test revokes what it minted at the end.
    const subject = `system:serviceaccount:hikyo-system:e2e-replace-${test.info().project.name}`;
    const account = seed.machine.mintable;

    await page.getByRole('tab', { name: 'Federation' }).click();
    await page.getByRole('button', { name: 'New binding' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Bind on the mintable account — it has no grants, so the replacement's
    // post-state reach is empty and no reauthentication ceremony runs. The
    // Kubernetes preset fills the configured issuer and the required UID pin.
    await dialog.getByLabel('Service account').selectOption({ label: account });
    await expect(dialog.getByLabel('Issuer')).toHaveValue(seed.machine.issuer);
    await dialog.getByLabel('Subject, matched byte-for-byte').fill(subject);
    await dialog.getByLabel(/ServiceAccount UID/).fill('e2e-replace-uid');
    await dialog.getByLabel('Audience').fill(seed.machine.audience);
    await dialog.getByRole('button', { name: 'Bind this identity' }).click();
    await expect(dialog).toBeHidden();
    await expect(page.locator('.notice').filter({ hasText: 'Bound' })).toBeVisible();

    const card = page.locator('.bindrow', { hasText: subject });
    await expect(card).toBeVisible();

    // Replace: the dialog is retitled, the account is LOCKED to the
    // predecessor's, and the fields are seeded from it. The server revokes the
    // predecessor and inserts the successor in one transaction.
    await card.getByRole('button', { name: `Replace binding on ${account}` }).click();
    const replace = page.getByRole('dialog');
    await expect(
      replace.getByRole('heading', { name: 'Replace federated binding' }),
    ).toBeVisible();
    await expect(replace.getByLabel('Service account')).toBeDisabled();
    await expect(replace.getByLabel('Issuer')).toHaveValue(seed.machine.issuer);
    await expect(replace.getByLabel('Subject, matched byte-for-byte')).toHaveValue(subject);
    await replace.getByLabel('Binding lifetime').selectOption('90d');
    // Hold the real post-mutation listing to expose the stale-cache window.
    // The replacement must remain pending until its successor is rendered.
    let signalListing: () => void = () => { throw new Error('listing gate not initialized'); };
    const listingStarted = new Promise<void>((resolve) => { signalListing = resolve; });
    let releaseListing: () => void = () => { throw new Error('release gate not initialized'); };
    const listingReleased = new Promise<void>((resolve) => { releaseListing = resolve; });
    let finishListing: () => void = () => { throw new Error('finish gate not initialized'); };
    const listingFinished = new Promise<void>((resolve) => { finishListing = resolve; });
    const credentialsPattern = '**/service-accounts/*/credentials';
    await page.route(credentialsPattern, async (route) => {
      if (route.request().method() === 'GET') {
        signalListing();
        await listingReleased;
      }
      try {
        await route.continue();
      } finally {
        finishListing();
      }
    });
    try {
      await replace.getByRole('button', { name: 'Replace this binding' }).click();
      await listingStarted;
      await expect(replace).toBeVisible();
      await expect(replace.getByRole('button', { name: 'Replacing…' })).toBeDisabled();
    } finally {
      releaseListing();
      await listingFinished;
      await page.unroute(credentialsPattern);
    }
    await expect(replace).toBeHidden();
    await expect(page.locator('.notice').filter({ hasText: 'Replaced' })).toBeVisible();

    // Exactly one live binding carries the subject now — the predecessor was
    // revoked, so it is gone from the list. Revoke the successor to leave the
    // seed inventory as it was.
    const successor = page.locator('.bindrow', { hasText: subject });
    await expect(successor).toHaveCount(1);
    await successor.getByRole('button', { name: `Revoke binding on ${account}` }).click();
    await expect(page.locator('.bindrow', { hasText: subject })).toHaveCount(0);
  });

  for (const scheme of COLOR_SCHEMES) {
    test(`meets the pinned assertion set on the inventory (${scheme})`, async () => {
      await page.emulateMedia({ colorScheme: scheme });
      try {
        await page.reload();
        await expect(page.getByRole('heading', { name: 'Machine access', level: 1 })).toBeVisible();
        // Expanded, because the expansion is most of this surface: the
        // credential rows, the binding card and the journey rail are all only
        // reachable through it.
        await accountRow(page, seed.machine.workload).click();

        const heading = page.getByRole('heading', { name: 'Machine access', level: 1 });
        const well = page.locator('.card');
        const badge = page.locator('.badge').first();
        const mint = page.getByRole('button', {
          name: `Mint credential for ${seed.machine.workload}`,
        });

        await expectPinnedAssertionSet(page, {
          flow: 'machine-access',
          surface: 'machine-access',
          theme: scheme,
          text: [heading, page.locator('.machine__policy'), page.locator('.journey__step').first()],
          radii: [
            [well, 'container'],
            [mint, 'control'],
            [badge, 'badge'],
          ],
          fonts: [
            [heading, 'ui'],
            [page.locator('.kv dd').first(), 'mono'],
          ],
          colours: [
            [heading, 'color', '--tx'],
            [well, 'backgroundColor', '--bg-panel'],
            [well, 'borderTopColor', '--line'],
          ],
          hairlines: [well],
          density: [[mint, '--touch']],
        });
      } finally {
        await page.emulateMedia({ colorScheme: null });
      }
    });
  }

  test('a browser operator creates an account, mints, revokes, and deletes it — no CLI', async () => {
    // The whole point of #464: a fresh project must no longer present an inert
    // inventory that only a CLI/API seed can fill. The name is unique per run so
    // a retry (or the second viewport project) never collides with a live
    // sibling and reads a 409 as a broken create.
    const name = `e2e-sa-${test.info().project.name}-${Date.now().toString(36)}`;

    // CREATE. The empty-tab primary action is always present; here the tab is
    // seeded, so the same action sits above the table.
    await page.getByRole('button', { name: 'Create service account', exact: true }).first().click();
    const createDialog = page.getByRole('dialog');
    await expect(createDialog.getByRole('heading', { level: 2 })).toHaveText(
      'Create service account',
    );
    await createDialog.getByLabel('Name').fill(name);
    // Kind defaults to workload — a fresh workload's empty reach is what makes
    // the mint below take the no-ceremony branch.
    await createDialog.getByRole('button', { name: 'Create service account' }).click();
    await expect(createDialog).toBeHidden();
    await expect(
      page.getByRole('status').filter({ hasText: `Created ${name} (workload)` }),
    ).toBeVisible();

    // It is immediately in the inventory and usable — no reload, no CLI.
    const row = accountRow(page, name);
    await expect(row).toBeVisible();
    await row.click();
    const expansion = page.locator('.machine__sub');

    // MINT. A fresh account reaches no plaintext, so the mint is the
    // no-passkey branch: the button reads "Mint credential", not "Use a passkey
    // and mint".
    await expansion.getByRole('button', { name: `Mint credential for ${name}` }).click();
    const mintDialog = page.getByRole('dialog');
    await expect(mintDialog).toContainText('reaches no plaintext');
    await mintDialog.getByRole('button', { name: 'Mint credential' }).click();
    await expect(mintDialog.locator('.machine__token')).toHaveText(/^hik_1_wl_/);
    await mintDialog.getByRole('checkbox').check();
    await mintDialog.getByRole('button', { name: 'Done' }).click();
    await expect(mintDialog).toBeHidden();
    await expect(expansion.locator('.cred')).toHaveCount(1);

    // REVOKE. It bites at the next request, and the account survives it.
    await expansion.getByRole('button', { name: /^Revoke hik_1_wl_/ }).click();
    await expect(expansion.locator('.cred')).toHaveCount(0);
    await expect(
      page.getByRole('status').filter({ hasText: 'stops authenticating at the next request' }),
    ).toBeVisible();

    // DELETE, behind a typed-name confirmation. The dialog states the cascade —
    // the credentials revoked and the grants released — never a refusal, because
    // the server delete is atomic and does not refuse on dependency.
    await expansion.getByRole('button', { name: `Delete ${name}` }).click();
    const deleteDialog = page.getByRole('dialog');
    await expect(deleteDialog.getByRole('heading', { level: 2 })).toHaveText(
      `Delete service account · ${name}`,
    );
    await expect(deleteDialog).toContainText('cannot be undone');

    // The destructive button is dead until the exact name is typed.
    const confirm = deleteDialog.getByRole('button', { name: 'Delete service account' });
    await expect(confirm).toBeDisabled();
    await deleteDialog.getByLabel('Confirm the account name to delete it').fill(name);
    await expect(confirm).toBeEnabled();
    await confirm.click();
    await expect(deleteDialog).toBeHidden();
    await expect(page.getByRole('status').filter({ hasText: `Deleted ${name}` })).toBeVisible();

    // And it is gone from the inventory — the surface returns to what it was.
    await expect(accountRow(page, name)).toHaveCount(0);
  });

  test('a duplicate name is refused actionably, without a stale account', async () => {
    // The create 409 is a name already in use among live siblings — a distinct
    // sentence from the mint's ceiling 409, and it must leave the operator able
    // to pick another name rather than staring at a dead form.
    await page.getByRole('button', { name: 'Create service account', exact: true }).first().click();
    const dialog = page.getByRole('dialog');
    // A seeded account name is guaranteed to already be live.
    await dialog.getByLabel('Name').fill(seed.machine.workload);
    await dialog.getByRole('button', { name: 'Create service account' }).click();
    await expect(dialog.getByRole('alert')).toContainText('already used');
    // The dialog stays open and editable: editing clears the refusal.
    await dialog.getByLabel('Name').fill(`${seed.machine.workload}-x`);
    await expect(dialog.getByRole('alert')).toHaveCount(0);
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
  });

  test('meets the pinned assertion set with the display-once value on screen', async () => {
    // The mint dialog is the component this ticket exists for, and it is the
    // only place in the SPA a credential value is ever rendered. Asserting only
    // the resting surface would leave it, its confirmation checkbox and its
    // refusal unchecked.
    await accountRow(page, seed.machine.mintable).click();
    await page
      .getByRole('button', { name: `Mint credential for ${seed.machine.mintable}` })
      .click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('button', { name: 'Mint credential' }).click();
    await expect(dialog.locator('.machine__token')).toBeVisible();

    await expectPinnedAssertionSet(page, {
      flow: 'machine-access',
      surface: 'machine-access',
      theme: 'dark',
      text: [dialog.getByRole('heading', { level: 2 }), dialog.locator('.machine__token')],
      radii: [
        [dialog, 'container'],
        [dialog.locator('.machine__token'), 'control'],
      ],
      fonts: [[dialog.locator('.machine__token'), 'mono']],
      colours: [[dialog, 'backgroundColor', '--bg-panel']],
      hairlines: [dialog],
      density: [[dialog.getByRole('button', { name: 'Done' }), '--touch']],
    });

    await dialog.getByRole('checkbox').check();
    await dialog.getByRole('button', { name: 'Done' }).click();
    await expect(dialog).toBeHidden();
    await revokeMinted(page);
  });
});

/**
 * Flow: deployment adapters (registry surface `adapters`, #157).
 *
 * It rides this file for the reason the registry states: the merge gate loads
 * `ci.yml` from the base branch, so a spec a PR adds to a group never runs on
 * that PR. machine-access.spec.ts is already in group 3 and is the
 * project-scoped sibling, so the surface's pinned set runs from PR-checked-out
 * content today.
 *
 * What this flow proves, in the ADRs' own terms:
 *
 *  - an adapter and its first target are created from the browser behind the
 *    adapter reauthentication ceremony, with the credential write-only;
 *  - the key subset is explicit: the ticked key is echoed back by name;
 *  - the target's health moves through the real outbox to `Healthy` against
 *    the --dev fake provider (ceremony, outbox, ledger and audit are real);
 *  - pause is instant and needs no ceremony; resume runs the ceremony and
 *    names the revision it catches up to;
 *  - removal is an explicit retain-or-prune decision, and retaining lists the
 *    orphaned names loudly.
 */
const ADAPTERS_PATH = `/orgs/${seed.org}/projects/${seed.project}/adapters`;
const ADAPTER_ORIGIN = 'https://forgejo-e2e.example';
const ADAPTER_ORIGIN_MOVED = 'https://forgejo-e2e-moved.example';
/** The one key the flow ticks; the seed always creates at least one secret. */
const ADAPTER_KEY = seed.secrets[0] ?? '';

async function authoriseAdapterCeremony(page: Page): Promise<void> {
  // The target lives in the protected `prod` environment, whose window is
  // capped at 0, so the adapter ceremony is a passkey decision: the virtual
  // authenticator the passkey fixture installed satisfies it, and no shared
  // TOTP step file is touched (desktop and mobile run in parallel projects).
  const dialog = page.getByRole('dialog', { name: 'Confirm it is you' });
  await expect(dialog).toBeVisible();
  // The dialog reads each environment's policy first and only then decides
  // between a code field and a passkey decision; Authorise stays disabled
  // until it has. Deciding before that would type nothing into a field that
  // does not exist yet, and the required field would then block the submit.
  const authorise = dialog.getByRole('button', { name: 'Authorise' });
  await expect(authorise).toBeEnabled();
  const code = dialog.getByLabel('Authenticator code');
  if (await code.isVisible().catch(() => false)) {
    await code.fill(await nextTotpCode());
  }
  await authorise.click();
  await expect(dialog).toBeHidden();
}

test.describe('deployment adapters', () => {
  test.describe.configure({ mode: 'serial' });

  let page: Page;

  test.beforeEach(async ({ passkeyPage }) => {
    page = passkeyPage;
    await page.context().clearCookies();
    await page.goto(ADAPTERS_PATH);
    await establishSession(page);
    await page.goto(ADAPTERS_PATH);
    await expect(page.getByRole('heading', { name: 'Deployment adapters', level: 1 })).toBeVisible();
  });

  test('creates an adapter behind the ceremony, watches it converge, pauses, resumes and removes', async () => {
    expect(ADAPTER_KEY).not.toBe('');

    // Create: provider, origin, a write-only credential, and the first target
    // with one ticked key. The save opens the adapter-purpose ceremony over
    // exactly the target's environment; the code authorises it.
    await page.getByRole('button', { name: 'Add adapter' }).click();
    const form = page.getByRole('region', { name: 'New adapter' });
    await form.getByLabel('Origin').fill(ADAPTER_ORIGIN);
    await form.getByLabel('Credential').fill('e2e-provider-token');
    await form.getByRole('combobox', { name: 'Environment', exact: true }).selectOption(seed.prod);
    await form.getByRole('textbox', { name: 'Owner', exact: true }).fill('acme');
    await form.getByRole('textbox', { name: 'Repository', exact: true }).fill('app');
    await form.getByLabel('Name prefix').fill('E2E_');
    await form.getByRole('checkbox', { name: ADAPTER_KEY, exact: true }).check();
    await form.getByRole('button', { name: 'Save' }).click();
    await authoriseAdapterCeremony(page);
    await expect(page.locator('.adapters__done')).toContainText('Adapter created');

    // The credential never comes back: the panel says only that one is set.
    const panel = page.getByRole('region', { name: `Adapter ${ADAPTER_ORIGIN}` });
    await expect(panel).toBeVisible();
    await expect(panel.getByText('credential set')).toBeVisible();
    await expect(page.getByText('e2e-provider-token')).toHaveCount(0);

    // The target row opens its detail; the ticked key is echoed by name and
    // the real outbox converges it against the fake provider.
    const row = panel.locator('.adapters__target').first();
    await row.click();
    const detail = page.getByRole('complementary', { name: 'Target detail' });
    await expect(detail).toBeVisible();
    await expect(detail.getByRole('list', { name: 'Member keys' })).toContainText(ADAPTER_KEY);
    // The environment published before the adapter existed, so a fresh target
    // starts `never`; a resync drives its first converge against the fake
    // provider. Resync pushes, so it runs the ceremony.
    await expect(detail.locator('.adapters__health')).toHaveText(/Never synced/);
    await detail.getByRole('button', { name: 'Resync' }).click();
    await authoriseAdapterCeremony(page);
    await expect(detail.locator('.adapters__health')).toHaveText(/Healthy/, { timeout: 20_000 });
    await expect(detail.locator('.adapters__facts')).toContainText('rev ');
    // The workflow mapping is names only, prefixed, and carries no value.
    await expect(detail.locator('.adapters__workflow')).toContainText(`E2E_${ADAPTER_KEY}`);

    // Pause needs no ceremony; the chip says so in words.
    await detail.getByRole('button', { name: 'Pause' }).click();
    await expect(detail.locator('.adapters__health')).toHaveText(/Paused/);
    await expect(detail.getByRole('button', { name: 'Resync' })).toBeDisabled();

    // Resume pushes again, so it runs the ceremony and names its revision.
    await detail.getByRole('button', { name: 'Resume' }).click();
    await authoriseAdapterCeremony(page);
    await expect(page.locator('.adapters__done')).toContainText(/Catching up to revision \d+/);
    await expect(detail.locator('.adapters__health')).toHaveText(/Healthy/, { timeout: 20_000 });

    // Removal is an explicit decision: neither option is preselected, and
    // retaining lists the names left behind.
    await detail.getByRole('button', { name: 'Remove' }).click();
    const remove = page.getByRole('dialog', { name: /Remove target/ });
    await expect(remove.getByRole('button', { name: 'Remove target' })).toBeDisabled();
    await remove.getByRole('radio', { name: /Retain/ }).check();
    await remove.getByRole('button', { name: 'Remove target' }).click();
    await expect(page.locator('.adapters__done')).toContainText(`E2E_${ADAPTER_KEY}`);
    await expect(panel.locator('.adapters__target')).toHaveCount(0);
  });

  for (const scheme of COLOR_SCHEMES) {
    for (const surface of surfacesForFlow('adapters')) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({}, testInfo) => {
        await page.emulateMedia({ colorScheme: scheme });
        try {
          const heading = page.getByRole('heading', { name: 'Deployment adapters', level: 1 });
          await expect(heading).toBeVisible();
          const panel = page.getByRole('region', { name: `Adapter ${ADAPTER_ORIGIN}` });
          await expect(panel).toBeVisible();
          const origin = panel.locator('.adapters__origin');
          const badge = panel.locator('.chip').first();
          const addTarget = panel.getByRole('button', { name: 'Add target' });
          const rowDensity = testInfo.project.name === 'mobile' ? '--touch' : '--row';

          await expectPinnedAssertionSet(page, {
            flow: 'adapters',
            surface: surface.id,
            theme: scheme,
            text: [heading, origin],
            radii: [
              [panel, 'container'],
              [addTarget, 'control'],
              [badge, 'badge'],
            ],
            fonts: [
              [heading, 'ui'],
              [origin, 'mono'],
            ],
            colours: [
              [heading, 'color', '--tx'],
              [panel, 'backgroundColor', '--bg-panel'],
              [panel, 'borderTopColor', '--panel-line'],
            ],
            hairlines: [panel],
            density: [[addTarget, rowDensity]],
          });
        } finally {
          await page.emulateMedia({ colorScheme: null });
        }
      });
    }
  }

  // The adapter lifecycle the registry attributed to #504: probe and plan a
  // target without a value or a write; replace the write-only credential;
  // move the origin as a durable scrub-before-switch job, exercising BOTH
  // exits of an attention-required move (cancel reconverges the old route,
  // resume with a working credential completes the switch); revoke custody;
  // and tear the adapter down behind the same retain-or-prune decision a
  // single target takes. The --dev fake provider refuses the literal
  // credential `revoked`, which is what makes the attention path deterministic.
  test('probes, plans, replaces the credential, moves the origin both ways, revokes, and deletes', async () => {
    const panel = page.getByRole('region', { name: `Adapter ${ADAPTER_ORIGIN}` });
    await expect(panel).toBeVisible();

    // A target again: the first test removed its target, and a move spans
    // every target the adapter has. A route is durable identity even after
    // removal (the (adapter, destination, environment) row stays unique across
    // tombstones), so this is a different repository; a fresh prefix keeps the
    // retained names of the first test out of the way, so the plan below is a
    // clean create.
    await panel.getByRole('button', { name: 'Add target' }).click();
    const form = page.getByRole('form', { name: 'Add target' });
    await form.getByRole('combobox', { name: 'Environment', exact: true }).selectOption(seed.prod);
    await form.getByRole('textbox', { name: 'Owner', exact: true }).fill('acme');
    await form.getByRole('textbox', { name: 'Repository', exact: true }).fill('app-lifecycle');
    await form.getByLabel('Name prefix').fill('LC_');
    await form.getByRole('checkbox', { name: ADAPTER_KEY, exact: true }).check();
    await form.getByRole('button', { name: 'Save' }).click();
    await authoriseAdapterCeremony(page);
    await expect(page.locator('.adapters__done')).toContainText('Target added');
    await panel.locator('.adapters__target').first().click();
    const detail = page.getByRole('complementary', { name: 'Target detail' });
    await expect(detail).toBeVisible();

    // PROBE: identity and expiry, no ceremony, no value.
    await detail.getByRole('button', { name: 'Test connection' }).click();
    const connection = detail.locator('dl[aria-label="Connection"]');
    await expect(connection).toBeVisible();
    await expect(connection).toContainText('dev-fake');
    await expect(connection).toContainText('never');

    // PLAN: the value-blind name plan names the prefixed create and nothing else.
    await detail.getByRole('button', { name: 'Plan' }).click();
    const planned = detail.getByRole('list', { name: 'Planned changes' });
    await expect(planned).toContainText(`create secret LC_${ADAPTER_KEY}`);
    await expect(page.getByText('e2e-provider-token')).toHaveCount(0);

    // REPLACE the credential behind the credential-set ceremony. It is
    // write-only: the form empties and the panel still says only "set".
    await panel.getByRole('button', { name: 'Replace credential' }).click();
    const credentialForm = page.getByRole('form', { name: 'Replace credential' });
    await credentialForm.getByLabel('Credential').fill('e2e-provider-token-2');
    await credentialForm.getByRole('button', { name: 'Replace' }).click();
    await authoriseAdapterCeremony(page);
    await expect(page.locator('.adapters__done')).toContainText('Credential replaced');
    await expect(panel.getByText('credential set')).toBeVisible();
    await expect(page.getByText('e2e-provider-token-2')).toHaveCount(0);

    // MOVE, exit one: the new origin's credential is refused, the move needs
    // attention, and cancel reconverges the old route. The move id is carried
    // in the URL (an id, never a secret) so the follow-up survives a reload.
    const startMove = async (credential: string) => {
      await panel.getByRole('button', { name: 'Change origin' }).click();
      const moveForm = page.getByRole('form', { name: 'Change origin' });
      await moveForm.getByLabel('New origin').fill(ADAPTER_ORIGIN_MOVED);
      await moveForm.getByLabel('New credential').fill(credential);
      await moveForm.getByRole('button', { name: 'Start move' }).click();
      await authoriseAdapterCeremony(page);
      await expect(page.locator('.adapters__done')).toContainText('Origin move started');
      await expect(page).toHaveURL(/[?&]move=/);
    };
    await startMove('revoked');
    const move = page.getByRole('complementary', { name: 'Route move' });
    await expect(move).toBeVisible();
    await expect(move.locator('.adapters__move-state')).toHaveText(/attention required/, { timeout: 30_000 });
    await expect(move).toContainText('Resume with a working credential, or cancel');
    await page.reload();
    await expect(move.locator('.adapters__move-state')).toHaveText(/attention required/);
    await move.getByRole('button', { name: 'Cancel move' }).click();
    await authoriseAdapterCeremony(page);
    await expect(page.locator('.adapters__done')).toContainText('Move canceled');
    await expect(move.locator('.adapters__move-state')).toHaveText(/canceled/, { timeout: 30_000 });
    await move.getByRole('button', { name: 'Close' }).click();
    await expect(panel).toBeVisible();

    // MOVE, exit two: attention again, then resume with a working credential
    // completes the switch and the adapter answers to the new origin.
    await startMove('revoked');
    await expect(move.locator('.adapters__move-state')).toHaveText(/attention required/, { timeout: 30_000 });
    await move.getByRole('button', { name: 'Resume with a new credential' }).click();
    const resumeForm = page.getByRole('form', { name: 'Resume move' });
    await expect(resumeForm.getByLabel('New origin')).toHaveValue(ADAPTER_ORIGIN_MOVED);
    await resumeForm.getByLabel('New credential').fill('e2e-provider-token-3');
    await resumeForm.getByRole('button', { name: 'Resume move' }).click();
    await authoriseAdapterCeremony(page);
    await expect(page.locator('.adapters__done')).toContainText('Move resumed');
    await expect(move.locator('.adapters__move-state')).toHaveText(/completed/, { timeout: 30_000 });
    await move.getByRole('button', { name: 'Close' }).click();
    const moved = page.getByRole('region', { name: `Adapter ${ADAPTER_ORIGIN_MOVED}` });
    await expect(moved).toBeVisible();
    await expect(page.getByRole('region', { name: `Adapter ${ADAPTER_ORIGIN}` })).toHaveCount(0);
    await expect(page.getByText('e2e-provider-token-3')).toHaveCount(0);

    // REVOKE custody: the consequence is stated first; afterwards the panel
    // says "absent" and the revoke action is gone until a credential is set.
    await moved.getByRole('button', { name: 'Revoke credential' }).click();
    const revoke = page.getByRole('dialog', { name: /Revoke credential for/ });
    await expect(revoke).toContainText('remote scrub may then be impossible');
    await revoke.getByRole('button', { name: 'Revoke credential' }).click();
    await expect(page.locator('.adapters__done')).toContainText('Credential revoked');
    await expect(moved.getByText('credential absent')).toBeVisible();
    await expect(moved.getByRole('button', { name: 'Revoke credential' })).toBeDisabled();

    // DELETE behind the explicit decision; retain lists the orphaned names.
    await moved.getByRole('button', { name: 'Delete adapter' }).click();
    const remove = page.getByRole('dialog', { name: /Delete adapter/ });
    await expect(remove.getByRole('button', { name: 'Delete adapter' })).toBeDisabled();
    await remove.getByRole('radio', { name: /Retain/ }).check();
    await remove.getByRole('button', { name: 'Delete adapter' }).click();
    await expect(page.locator('.adapters__done')).toContainText(`LC_${ADAPTER_KEY}`);
    await expect(page.getByRole('region', { name: /^Adapter / })).toHaveCount(0);
  });
});

/**
 * Project audit (#572, registry surface `project-audit`): the project's own
 * trail behind `audit-read@project`, with the environment as a filter. The
 * export is a same-origin GET under the session cookie, one literal route per
 * scope, so the parity registry can see all three export operations reached.
 */
test.describe('project audit', () => {
  test.describe.configure({ mode: 'serial' });

  let page: Page;
  const AUDIT_PATH = `/orgs/${seed.org}/projects/${seed.project}/audit`;

  test.beforeEach(async ({ passkeyPage }) => {
    page = passkeyPage;
    await page.context().clearCookies();
    await page.goto(AUDIT_PATH);
    await establishSession(page);
    await page.goto(AUDIT_PATH);
    await expect(page.getByRole('heading', { name: 'Project audit', level: 1 })).toBeVisible();
  });

  test('reads the project trail, narrows it to one environment, and exports each scope', async () => {
    const rows = page.locator('.audit__row');
    await expect(rows.first()).toBeVisible();
    const exportLink = page.getByRole('link', { name: 'Export JSONL' });
    await expect(exportLink).toHaveAttribute(
      'href',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/audit/export`,
    );

    // The environment picker narrows the trail to that environment's slice and
    // the export follows it; the seeded value publishes make it non-empty.
    await page.getByLabel('Environment').selectOption(seed.dev);
    await page.getByRole('button', { name: 'Apply filter' }).click();
    await expect(rows.first()).toBeVisible();
    await expect(exportLink).toHaveAttribute(
      'href',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/environments/${seed.dev}/audit/export`,
    );
    await rows.first().click();
    const detail = page.getByRole('complementary', { name: 'Event detail' });
    await expect(detail).toContainText(seed.dev);

    const download = page.waitForEvent('download');
    await exportLink.click();
    const file = await download;
    expect(file.suggestedFilename()).toMatch(/\.jsonl$/);
    const body = readFileSync(await file.path(), 'utf8').trim();
    expect(body.length).toBeGreaterThan(0);
    for (const line of body.split('\n')) {
      const event = z.object({ seq: z.number(), env_id: z.string().optional() }).parse(JSON.parse(line));
      expect(event.env_id).toBe(seed.dev);
    }

    // Clear restores the whole project and the project export route.
    await page.getByRole('button', { name: 'Clear' }).click();
    await expect(page.getByLabel('Environment')).toHaveValue('');
    await expect(exportLink).toHaveAttribute(
      'href',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/audit/export`,
    );
  });

  for (const scheme of COLOR_SCHEMES) {
    for (const surface of surfacesForFlow('project-audit')) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({}, testInfo) => {
        await page.emulateMedia({ colorScheme: scheme });
        try {
          // Narrow before the sweep, as the org audit flow does: the pinned set
          // focuses and measures every interactive element on the page.
          await page.getByLabel('Operation').fill('grant.created');
          await page.getByRole('button', { name: 'Apply filter' }).click();
          await expect(page.locator('.audit__row').first()).toBeVisible();

          const heading = page.getByRole('heading', { name: 'Project audit', level: 1 });
          const panel = page.locator('.panel').first();
          const op = page.locator('.audit__row-op').first();
          const badge = page.locator('.chip').first();
          const apply = page.getByRole('button', { name: 'Apply filter' });
          const rowDensity = testInfo.project.name === 'mobile' ? '--touch' : '--row';

          await expectPinnedAssertionSet(page, {
            flow: 'project-audit',
            surface: surface.id,
            theme: scheme,
            text: [heading, op],
            radii: [
              [panel, 'container'],
              [apply, 'control'],
              [badge, 'badge'],
            ],
            fonts: [
              [heading, 'ui'],
              [op, 'mono'],
            ],
            colours: [
              [heading, 'color', '--tx'],
              [panel, 'backgroundColor', '--bg-panel'],
              [panel, 'borderTopColor', '--panel-line'],
            ],
            hairlines: [panel],
            density: [[apply, rowDensity]],
          });
        } finally {
          await page.emulateMedia({ colorScheme: null });
        }
      });
    }
  }
});

/**
 * Browser-only lifecycle acceptance (#504).
 *
 * One operator, one browser, no CLI: from a fresh organisation to a
 * Kubernetes-ready delivery. The instance is the suite's shared one, bootstrapped
 * with `hikyo admin create` (host-local authority, the one accepted exception at
 * the start of the journey); the seeded fixture tenant exists beside the
 * organisation this flow creates and is never touched by it, so what is proved
 * is the organisation-scoped lifecycle from nothing, not instance-level
 * defaults on an otherwise empty database. Every Hikyo-side prerequisite the operator
 * needs (`kubernetes-operator.mdx` § The five-step journey) is done from the
 * SPA, and the delivery wire is then exercised the way the operator's
 * controller does it — a bearer fetch of the environment projection — so the
 * proof is the delivered projection, not a screen that says "configured".
 *
 * The journey, in order: organisation, project, environments, declared config
 * and secret with first values, publish, a human invitation, a workload
 * service account with a display-once credential and its read grant, the
 * per-project machine-reveal opt-in, the reveal grant, the delivery fetch, a
 * CI adapter target converging to Healthy, the audit trail, and two supported
 * recoveries: rotating a leaked machine credential (the old one is refused at
 * the next fetch) and rolling a bad publish back through the history drawer.
 */
const zDelivered = z.object({
  current: z.boolean(),
  keys: z.array(
    z.object({
      name: z.string(),
      classification: z.string(),
      value: z.string().optional(),
    }),
  ),
});

/** captureCreated resolves the id of the row a POST to `pathname` created. */
async function captureCreated<T>(
  page: Page,
  pathname: string,
  schema: z.ZodType<T>,
  act: () => Promise<void>,
): Promise<T> {
  const response = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      new URL(candidate.url()).pathname === pathname &&
      candidate.status() === 201,
  );
  await act();
  return schema.parse(await (await response).json());
}

/**
 * Secret-bearing assertions are made on booleans so a regression prints
 * "false", never the value it was checking for.
 */
async function expectAbsentFromPage(page: Page, value: string, what: string): Promise<void> {
  expect(value).not.toBe('');
  expect((await page.content()).includes(value), `${what} is present in the page`).toBe(false);
}

test.describe('browser-only lifecycle', () => {
  test('takes an empty organisation to Kubernetes-ready delivery without the CLI', async ({
    passkeyPage: page,
    playwright,
  }, testInfo) => {
    test.setTimeout(420_000);
    const tag = `${testInfo.project.name}-${Date.now().toString(36)}`;
    const orgName = `Lifecycle ${tag}`;
    const peer = await playwright.request.newContext();
    try {
      // 1. ORGANISATION, from instance administration. Creation grants the
      // creator organisation admin and ends the session; sign in again.
      await page.context().clearCookies();
      await page.goto('/login');
      await establishSession(page);
      await page.goto('/instance');
      await expect(page.getByRole('heading', { name: 'Instance settings', level: 1 })).toBeVisible();
      await page.getByRole('button', { name: 'Open create organisation form' }).click();
      await page.getByLabel('New organisation name').fill(orgName);
      const org = await captureCreated(page, '/api/v1/orgs', zOrg, async () => {
        await page.getByRole('button', { name: 'Create organisation', exact: true }).click();
      });
      await expect(page.getByRole('heading', { name: 'Sign in to Hikyo', level: 1 })).toBeVisible();
      await establishSession(page);

      // 2. PROJECT. The Projects page is unscoped: it creates under the rail's
      // active organisation, so choose the new one there (the desktop rail's
      // avatar, or the phone drawer's switcher) and read the choice back from
      // the breadcrumb before creating anything.
      await page.goto('/projects');
      const railChoice = page.getByRole('button', { name: `Organisation ${orgName}`, exact: true });
      const menu = page.getByRole('button', { name: 'Menu' });
      await expect
        .poll(async () => (await railChoice.isVisible()) || (await menu.isVisible()))
        .toBe(true);
      if (await railChoice.isVisible()) {
        await railChoice.click();
      } else {
        await menu.click();
        await page.getByRole('button', { name: orgName }).click();
      }
      await expect(page.getByLabel('Breadcrumb')).toContainText(orgName);
      const projectName = `payments-${tag}`;
      await page.getByLabel('Project name').fill(projectName);
      const project = await captureCreated(page, `/api/v1/orgs/${org.id}/projects`, zProject, async () => {
        await page.getByRole('button', { name: 'Create project' }).click();
      });
      await expect(page.getByRole('status').filter({ hasText: `Project ${projectName} created` })).toBeVisible();

      // 3. ENVIRONMENTS, from project settings.
      const base = `/orgs/${org.id}/projects/${project.id}`;
      const api = `/api/v1/orgs/${org.id}/projects/${project.id}`;
      await page.goto(`${base}/settings`);
      const environments: Record<string, string> = {};
      for (const name of ['development', 'production']) {
        await page.getByLabel('New environment name').fill(name);
        const created = await captureCreated(page, `${api}/environments`, zEnvironment, async () => {
          await page.getByRole('button', { name: 'Create', exact: true }).click();
        });
        environments[name] = created.id;
        await expect(page.getByLabel('New environment name')).toHaveValue('');
      }
      const dev = environments['development'] ?? '';
      expect(dev).not.toBe('');

      // 4. DECLARE a config key and a secret key, each with a first value in
      // development, then PUBLISH the two drafts. Development is unprotected,
      // so the publish is plain: no ceremony, no confirmation.
      await page.goto(`${base}/matrix`);
      await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
      const declare = async (name: string, value: string, secret: boolean) => {
        await page.getByRole('button', { name: '+ New key' }).click();
        const modal = page.getByRole('dialog');
        await modal.getByLabel('Group').fill('app');
        await modal.getByLabel('Key name').fill(name);
        if (secret) {
          await modal.getByRole('checkbox', { name: /secret/ }).check();
        }
        await modal.getByLabel('First value (optional)').fill(value);
        const targets = modal.getByRole('group', { name: 'Set that value in' });
        const development = targets.getByRole('checkbox', { name: 'development' });
        if (!(await development.isChecked())) {
          await development.check();
        }
        const production = targets.getByRole('checkbox', { name: 'production' });
        if (await production.isChecked()) {
          await production.uncheck();
        }
        await modal.getByRole('button', { name: 'Declare' }).click();
        await expect(page.locator('.notice')).toContainText(`Declared ${name} with a draft value in 1 environment`);
      };
      await declare('LOG_LEVEL', 'info', false);
      const secretValue = `hunter2-${tag}`;
      await declare('DB_PASSWORD', secretValue, true);
      await page.getByRole('button', { name: /unpublished edit/ }).click();
      const publishSheet = page.getByRole('region', { name: 'Publish drafts' });
      await expect(publishSheet).toContainText('LOG_LEVEL');
      // The secret's plaintext never reaches the sheet, the notice, or the DOM.
      await expectAbsentFromPage(page, secretValue, 'the secret value');
      // Development is unprotected: no confirmation, no ceremony; the notice
      // is the proof the publish went straight through.
      await publishSheet.getByRole('button', { name: /Publish selected/ }).click();
      await expect(page.locator('.notice')).toContainText('Published atomically: development');
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await expectAbsentFromPage(page, secretValue, 'the secret value');

      // 5. HUMAN ACCESS: invite a member with a template. The display-once
      // authority is shown in the dialog and nowhere else.
      await page.goto(`/orgs/${org.id}/members`);
      await page.getByRole('button', { name: 'Invite' }).click();
      const invite = page.getByRole('dialog');
      await invite.getByLabel('Username').fill(`lc-viewer-${tag}`);
      await invite.getByLabel('Role template').selectOption('viewer');
      await invite.getByRole('button', { name: 'Invite', exact: true }).click();
      await expect(invite.getByRole('heading', { level: 2 })).toContainText('Invitation for');
      const authority = (await invite.getByTestId('issued-authority').textContent()) ?? '';
      expect(authority.length).toBeGreaterThan(16);
      await invite.getByRole('button', { name: 'Close' }).click();
      await expect(page.getByRole('dialog')).toBeHidden();
      await expectAbsentFromPage(page, authority, 'the invitation authority');

      // 6. WORKLOAD ACCESS: service account, display-once credential, read
      // grant on development. Minting first keeps the mint on the no-plaintext
      // branch; the grant then re-scopes that one live credential, and says so.
      await page.goto(`${base}/machine-access`);
      await page.getByRole('button', { name: 'Create service account', exact: true }).first().click();
      const createDialog = page.getByRole('dialog');
      const accountName = `k8s-payments-${tag}`;
      await createDialog.getByLabel('Name').fill(accountName);
      const account = await captureCreated(page, `${api}/service-accounts`, zServiceAccount, async () => {
        await createDialog.getByRole('button', { name: 'Create service account' }).click();
      });
      await expect(createDialog).toBeHidden();
      const row = accountRow(page, accountName);
      await row.click();
      const expansion = page.locator('.machine__sub');
      const mint = async (): Promise<string> => {
        await expansion.getByRole('button', { name: `Mint credential for ${accountName}` }).click();
        const dialog = page.getByRole('dialog');
        await dialog.getByRole('button', { name: /Mint credential|Use a passkey and mint/ }).click();
        await expect(dialog.locator('.machine__token')).toHaveText(/^hik_1_wl_/);
        const token = (await dialog.locator('.machine__token').textContent())?.trim() ?? '';
        await dialog.getByRole('checkbox').check();
        await dialog.getByRole('button', { name: 'Done' }).click();
        await expect(dialog).toBeHidden();
        return token;
      };
      const firstToken = await mint();
      await expansion.getByRole('button', { name: `Add environment grant to ${accountName}` }).click();
      const grant = page.getByRole('dialog');
      await expect(grant).toContainText('1 live credential');
      await grant.locator('#grant-environment').selectOption(dev);
      // `read` newly decrypts nothing for a workload, so the dialog says the
      // reauthentication conjunct is vacuous and no ceremony runs.
      await expect(grant).toContainText('no reauthentication is required');
      await grant.getByRole('button', { name: 'Grant read' }).click();
      await expect(page.getByRole('status').filter({ hasText: /Grant result for development/ })).toBeVisible();

      const delivery = `${BASE_URL}${api}/environments/${dev}/delivery`;
      const fetchAs = async (token: string) =>
        peer.get(delivery, { headers: { Authorization: `Bearer ${token}` } });
      // Control: the wire is closed without a credential.
      expect((await peer.get(delivery)).status()).toBe(401);
      // Read alone delivers configuration plaintext and secret PRESENCE only.
      const presence = await fetchAs(firstToken);
      expect(presence.status()).toBe(200);
      const presenceBody = zDelivered.parse(await presence.json());
      expect(presenceBody.keys.find((key) => key.name === 'LOG_LEVEL')?.value).toBe('info');
      expect(presenceBody.keys.find((key) => key.name === 'DB_PASSWORD')?.value).toBeUndefined();

      // 7. SECRET DELIVERY needs the per-project opt-in AND a reveal grant on
      // the workload (the opt-in alone grants nothing; the dialog says so).
      await page.getByRole('button', { name: /Enable the opt-in/ }).click();
      const optIn = page.getByRole('dialog', { name: 'Enable machine secret delivery' });
      await expect(optIn).toContainText('Nothing is granted by this act alone');
      await optIn.getByRole('checkbox').check();
      await optIn.getByRole('button', { name: 'Enable the opt-in' }).click();
      await expect(page.getByText('Machine secret delivery (per-project opt-in): on')).toBeVisible();

      // The reveal grant WIDENS what the workload decrypts, so the server
      // refuses the first attempt until the session has reauthenticated over
      // development, and the dialog answers with the mint-purpose passkey
      // ceremony and retries. The wire proves it: one refused grant, one
      // reauthentication finish, one accepted grant, in that order.
      const grantPath = `${api}/environments/${dev}/grants`;
      const wire: string[] = [];
      const onResponse = (response: import('@playwright/test').Response) => {
        const pathname = new URL(response.url()).pathname;
        const method = response.request().method();
        // Refusal versus acceptance is the fact; the exact success code is
        // the contract's (a created line answers 200 or 201 by outcome).
        const status = response.status();
        const outcome = status >= 200 && status < 300 ? 'ok' : String(status);
        if (method === 'POST' && pathname === grantPath) {
          wire.push(`grant:${outcome}`);
        } else if (method === 'POST' && pathname === '/api/v1/auth/webauthn/reauth/finish') {
          wire.push(`reauth:${outcome}`);
        }
      };
      page.on('response', onResponse);
      await page.goto(`/orgs/${org.id}/members`);
      await page.getByRole('button', { name: 'New grant' }).click();
      const reveal = page.getByRole('dialog');
      await reveal.getByLabel('Principal').fill(account.principal_id);
      await reveal.getByRole('checkbox', { name: 'reveal', exact: true }).check();
      await reveal.getByLabel('Scope').selectOption(`env:${project.id}:${dev}`);
      await reveal.getByRole('button', { name: 'Grant', exact: true }).click();
      await expect(page.locator('.notice').filter({ hasText: 'Grant results' })).toContainText('Created: reveal');
      page.off('response', onResponse);
      expect(wire).toEqual(['grant:409', 'reauth:ok', 'grant:ok']);

      // The controller's fetch now carries the secret: a Kubernetes-ready
      // projection, obtained with a credential minted in the browser.
      const full = await fetchAs(firstToken);
      expect(full.status()).toBe(200);
      const fullBody = zDelivered.parse(await full.json());
      expect(fullBody.keys.find((key) => key.name === 'DB_PASSWORD')?.value).toBe(secretValue);

      // 8. CI DELIVERY through an adapter target on development, converging
      // to Healthy against the --dev fake provider; health is read in place.
      await page.goto(`${base}/adapters`);
      await page.getByRole('button', { name: 'Add adapter' }).click();
      const adapterForm = page.getByRole('region', { name: 'New adapter' });
      const origin = `https://forgejo-lifecycle-${tag}.example`;
      await adapterForm.getByLabel('Origin').fill(origin);
      await adapterForm.getByLabel('Credential').fill('lifecycle-provider-token');
      await adapterForm.getByRole('combobox', { name: 'Environment', exact: true }).selectOption(dev);
      await adapterForm.getByRole('textbox', { name: 'Owner', exact: true }).fill('acme');
      await adapterForm.getByRole('textbox', { name: 'Repository', exact: true }).fill('payments');
      await adapterForm.getByRole('checkbox', { name: 'DB_PASSWORD', exact: true }).check();
      await adapterForm.getByRole('button', { name: 'Save' }).click();
      await authoriseAdapterCeremony(page);
      await expect(page.locator('.adapters__done')).toContainText('Adapter created');
      const panel = page.getByRole('region', { name: `Adapter ${origin}` });
      await panel.locator('.adapters__target').first().click();
      const detail = page.getByRole('complementary', { name: 'Target detail' });
      await expect(detail.locator('.adapters__health')).toHaveText(/Healthy|Never synced|Converging/);
      if (!/Healthy/.test((await detail.locator('.adapters__health').textContent()) ?? '')) {
        await detail.getByRole('button', { name: 'Resync' }).click();
        await authoriseAdapterCeremony(page);
      }
      await expect(detail.locator('.adapters__health')).toHaveText(/Healthy/, { timeout: 30_000 });
      await expectAbsentFromPage(page, secretValue, 'the secret value');

      // 9. AUDIT: the organisation's trail is readable and names the publish.
      await page.goto(`/orgs/${org.id}/audit`);
      await expect(page.locator('.audit__row').first()).toBeVisible();
      await page.getByLabel('Operation').fill('revision.published');
      await page.getByRole('button', { name: 'Apply filter' }).click();
      await expect(page.locator('.audit__row').first()).toBeVisible();
      await expectAbsentFromPage(page, secretValue, 'the secret value');

      // 10. RECOVERY, credential leak: revoke and re-mint. The old value is
      // refused at the very next fetch; the new one delivers.
      await page.goto(`${base}/machine-access`);
      await accountRow(page, accountName).click();
      await expansion.getByRole('button', { name: /^Revoke hik_1_wl_/ }).click();
      await expect(expansion.locator('.cred')).toHaveCount(0);
      expect((await fetchAs(firstToken)).status()).toBe(401);
      const secondToken = await mint();
      expect(secondToken === firstToken, 'the re-mint returned the revoked value').toBe(false);
      expect((await fetchAs(secondToken)).status()).toBe(200);

      // 11. RECOVERY, bad publish: change LOG_LEVEL, publish, then restore the
      // earlier revision from the history drawer and publish the restore.
      await page.goto(`${base}/matrix`);
      await page.getByRole('button', { name: /^LOG_LEVEL in development:/ }).click();
      const editor = page.getByRole('dialog');
      await editor.getByLabel('development value').fill('debug');
      await editor.getByRole('button', { name: 'Save 1 draft' }).click();
      await page.getByRole('button', { name: /unpublished edit/ }).click();
      await page.getByRole('region', { name: 'Publish drafts' }).getByRole('button', { name: /Publish selected/ }).click();
      await expect(page.locator('.notice')).toContainText('Published atomically: development');
      expect(zDelivered.parse(await (await fetchAs(secondToken)).json()).keys.find((key) => key.name === 'LOG_LEVEL')?.value).toBe('debug');

      await page.goto(`${base}/matrix/history?env=${dev}`);
      const drawer = page.getByRole('complementary', { name: 'Revision history' });
      await expect(drawer).toBeVisible();
      const revisions = await drawer.locator('[data-history-revision]').evaluateAll((nodes) =>
        nodes.map((node) => Number(node.getAttribute('data-history-revision'))),
      );
      // The earliest revision that changed LOG_LEVEL is the one that set it to
      // `info`; an environment may open with an empty revision before it.
      const restoreKey = drawer.getByRole('button', { name: 'Restore LOG_LEVEL…' });
      for (const revision of [...revisions].sort((a, b) => a - b)) {
        const back = page.locator('#history-detail-back');
        if (await back.isVisible()) {
          await back.click();
        }
        await drawer.locator(`[data-history-revision="${String(revision)}"]`).click();
        await expect(page.locator('#history-detail-title')).toContainText(`r${String(revision)}`);
        if ((await restoreKey.count()) > 0) {
          break;
        }
      }
      // One key from the changed-key row: a config-only restore opens no
      // secret plaintext, so it takes no ceremony and stages an ordinary draft.
      await restoreKey.click();
      const restore = page.getByRole('dialog');
      await restore.getByRole('button', { name: /^Stage the restore from r/ }).click();
      await expect(restore.locator('.history__impact')).toContainText('debug → info');
      await restore.locator('#history-restore-back').click();
      await expect(page.getByRole('button', { name: /LOG_LEVEL in development:.*draft set/ })).toBeVisible();
      await page.getByRole('button', { name: /unpublished edit/ }).click();
      await page.getByRole('region', { name: 'Publish drafts' }).getByRole('button', { name: /Publish selected/ }).click();
      await expect(page.locator('.notice')).toContainText('Published atomically: development');
      expect(zDelivered.parse(await (await fetchAs(secondToken)).json()).keys.find((key) => key.name === 'LOG_LEVEL')?.value).toBe('info');
      await expectAbsentFromPage(page, secretValue, 'the secret value');
    } finally {
      await peer.dispose();
    }
  });
});
