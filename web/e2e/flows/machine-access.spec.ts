import { expect, type Page } from '@playwright/test';

import { expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import {
  establishSession,
  nextTotpCode,
  readSeed,
} from '../fixtures/instance.ts';
import { test } from '../fixtures/passkey.ts';
import { surfacesForFlow } from '../registry.ts';

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

  test('the inventory has four tabs, and every one of them says what it holds', async () => {
    const tabs = page.getByRole('tab');
    await expect(tabs).toHaveCount(4);
    await expect(tabs.nth(0)).toHaveText(/Service accounts \(3\)/);
    await expect(tabs.nth(1)).toHaveText(/Federation \(1\)/);
    await expect(tabs.nth(2)).toHaveText(/Kubernetes targets \(0\)/);
    await expect(tabs.nth(3)).toHaveText(/Leases \(0\)/);

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
    await expectStatusIsTextAndAria(page, empty);

    // The Leases tab is status-only and empty on a fresh project; it never shows
    // a secret and points mint/lifecycle at the CLI.
    await page.getByRole('tab', { name: 'Leases' }).click();
    const leasesEmpty = page
      .getByRole('status')
      .filter({ hasText: 'No dynamic-secret leases on this project yet' });
    await expect(leasesEmpty).toBeVisible();
    await expectStatusIsTextAndAria(page, leasesEmpty);
  });

  test('expanding a row shows credentials, bindings, targets and the journey below', async () => {
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
    await replace.getByRole('button', { name: 'Replace this binding' }).click();
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

  for (const scheme of ['dark', 'light'] as const) {
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
/** The one key the flow ticks; the seed always creates at least one secret. */
const ADAPTER_KEY = seed.secrets[0] ?? '';

async function authoriseAdapterCeremony(page: Page): Promise<void> {
  // The target lives in the protected `prod` environment, whose window is
  // capped at 0, so the adapter ceremony is a passkey decision: the virtual
  // authenticator the passkey fixture installed satisfies it, and no shared
  // TOTP step file is touched (desktop and mobile run in parallel projects).
  const dialog = page.getByRole('dialog', { name: 'Confirm it is you' });
  await expect(dialog).toBeVisible();
  const code = dialog.getByLabel('Authenticator code');
  if (await code.isVisible().catch(() => false)) {
    await code.fill(await nextTotpCode());
  }
  await dialog.getByRole('button', { name: 'Authorise' }).click();
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

  for (const scheme of ['dark', 'light'] as const) {
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
});
