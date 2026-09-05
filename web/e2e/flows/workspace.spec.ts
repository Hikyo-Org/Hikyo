import { expect, test, type Page } from '@playwright/test';
import type { WorkspaceHandoffEstablishment, WorkspaceHandoffStepUp } from '@hikyo/client';

import { expectNoSeriousAxeViolations, expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import {
  ADMIN,
  BASE_URL,
  BASE_URL_B,
  HOST_B,
  readServing,
  REMOTE_NAME,
  STORAGE_STATE,
} from '../fixtures/instance.ts';
import { surfacesForFlow } from '../registry.ts';

/**
 * Flow: the multi-instance surfaces (registry surfaces `remotes`,
 * `workspace-approve`, `workspace-callback`).
 *
 * This is M6's [UI] deliverable, "workspace popup ceremony + kill switch" , 
 * and it runs against TWO REAL INSTANCES on two loopback origins, not against a
 * mock. What it proves, in the order it proves it:
 *
 *   1. The directory card renders a real entry, its state, and the last-known
 *      listing it holds.
 *   2. The serving instance's administrator allowlists the viewing origin
 *      THROUGH THE UI, the consent surface, exercised rather than seeded.
 *   3. The origin-labelled workspace action opens a popup ON THE REMOTE'S ORIGIN, the human
 *      authorizes there, the code returns through this origin's own callback
 *      page over a BroadcastChannel, and the shell redeems it cross-origin.
 *      There is no server in the middle at any point.
 *   4. Removing the allowlist entry kills the workspace, and the shell says so.
 *   5. Revoking the session in the remote's own active-session list kills it
 *      the same way, criterion 5, seen from the browser.
 *
 * The two instances differ by HOSTNAME (A is `localhost`, B is `127.0.0.1`)
 * and not only by port, because cookies are not partitioned by port: one
 * hostname would mean one cookie jar and B's session would destroy A's. A
 * takes the NAME because a WebAuthn relying-party id must be a registrable
 * domain and an IP literal is not one, so the passkey ceremonies have to run
 * there.
 */

const VIEWING_ORIGIN = BASE_URL;

/** onB opens a page against the serving instance in the same context. */
async function onB(page: Page, path: string): Promise<void> {
  await page.goto(BASE_URL_B + path);
}

/** Ensure consent only after the initial read can no longer overwrite a mutation. */
async function allowOrigin(page: Page): Promise<void> {
  const allowed = page.getByText(VIEWING_ORIGIN, { exact: true });
  const empty = page.getByText(
    'No origins allowlisted. No browser can operate this instance remotely.',
  );
  await expect(allowed.or(empty)).toBeVisible();
  if (await allowed.isVisible()) return;

  await page.getByRole('textbox', { name: 'Origin' }).fill(VIEWING_ORIGIN);
  await page.getByRole('button', { name: 'Allow origin' }).click();
  await expect(allowed).toBeVisible();
}

/** card is the directory card for the seeded remote entry. */
function card(page: Page) {
  return page.locator('.remote').filter({ hasText: REMOTE_NAME });
}

/**
 * revokeConnectionByLabel is best-effort test cleanup: it retires any live
 * connection credential carrying `label`, from inside B's own origin so the
 * session cookie and its synchronizer token are both present. It never throws , 
 * a failed cleanup must not mask the assertion that actually failed.
 */
async function revokeConnectionByLabel(page: Page, label: string): Promise<void> {
  await page
    .evaluate(async (wanted) => {
      const csrf =
        document.cookie
          .split(';')
          .map((part) => part.trim())
          .find((part) => part.startsWith('__Host-hikyo-csrf='))
          ?.slice('__Host-hikyo-csrf='.length) ?? '';
      const listed = await fetch('/api/v1/instance/connections');
      if (!listed.ok) return;
      const body: unknown = await listed.json();
      const items =
        typeof body === 'object' && body !== null && 'items' in body
          ? (body as { items: { id: string; label: string; revoked_at?: string }[] }).items
          : [];
      for (const item of items) {
        if (item.label === wanted && item.revoked_at === undefined) {
          await fetch(`/api/v1/instance/connections/${item.id}`, {
            method: 'DELETE',
            headers: { 'X-Hikyo-CSRF': csrf },
          });
        }
      }
    }, label)
    .catch(() => undefined);
}

test.describe('multi-instance', () => {
  test.use({ storageState: STORAGE_STATE });

  test('This instance directory shows identity, metadata and scoped refusal', async ({ page }, testInfo) => {
    await page.goto('/remotes');
    const panel = page.getByRole('region', { name: 'This instance', exact: true });
    await expect(panel.getByText('Identity', { exact: true })).toBeVisible();
    await expect(panel.locator('dd').first()).not.toBeEmpty();
    await expect(panel.getByText('Version', { exact: true })).toBeVisible();
    await expect(panel.getByText('Organisations', { exact: true })).toBeVisible();
    await expect(panel.getByText('Projects', { exact: true })).toBeVisible();
    await panel.scrollIntoViewIfNeeded();
    expect(await panel.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
    for (const colorScheme of ['dark', 'light']) {
      await page.emulateMedia({ colorScheme: colorScheme === 'dark' ? 'dark' : 'light', reducedMotion: 'reduce' });
      await expectNoSeriousAxeViolations(page);
      await page.screenshot({ path: testInfo.outputPath(`this-instance-${colorScheme}.png`), fullPage: true });
    }
    await page.screenshot({ path: testInfo.outputPath('this-instance.png'), fullPage: true });
    await page.route('**/api/v1/instance/directory', (route) => route.fulfill({ status: 403, json: { error: { code: 'forbidden', message: 'forbidden' } } }));
    await page.reload();
    await expect(panel.getByRole('alert')).toContainText('You do not hold instance-directory');
    await expect(panel.locator('dd')).toHaveCount(0);
  });

  test('keeps anonymous ceremony URLs intact and outside shell chrome', async ({ page, context }) => {
    await context.clearCookies();

    for (const path of [
      '/reauth/cli?transaction=hik_1_test',
      '/workspace/approve?state=hik_1_test',
    ]) {
      await page.goto(path);
      await expect(page).toHaveURL(new RegExp(`${path.replace('?', '\\?')}$`));
      await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
      await expect(page.getByRole('navigation', { name: 'Organisations' })).toHaveCount(0);
    }

    await page.goto('/workspace/callback');
    await expect(page.getByRole('heading', { name: 'Returning to your workspace' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Organisations' })).toHaveCount(0);
  });

  test('the directory card carries state, identity and the last-known listing', async ({ page }) => {
    await page.goto('/remotes');
    const entry = card(page);
    await expect(entry).toBeVisible();
    await expect(entry).toContainText(BASE_URL_B);

    // The state is a SENTENCE, announced, never a colour. The entry is
    // deliberately unreachable over plaintext, the server refuses to fetch a
    // remote URL that is not https, so the card is in exactly the state the
    // ADR names: last known, with its age.
    await expectStatusIsTextAndAria(page, entry.getByRole('status').first());
    await expect(entry).toContainText('Showing the last known directory');

    // The last-known listing came from a real authenticated directory fetch of
    // the other instance, so its identity is present and is not this
    // instance's own.
    await expect(entry.getByText('Identity')).toBeVisible();
    await expect(entry).not.toContainText('not yet observed');
  });

  /**
   * The receiving side, end to end (#498). The serving instance's operator
   * mints a connection credential IN THE UI, a peer presents it at the one
   * endpoint it may reach, the directory fetch, and it authenticates.
   * Revoking it through the UI, after the consequence is stated, refuses the
   * very next presentation.
   *
   * It mints its OWN uniquely-labelled credential and never touches the seeded
   * one every sibling test depends on. The connect-and-refuse legs run through
   * an EXPLICITLY cookie-less request context, so the credential, not an
   * ambient session, is provably what authenticates: a no-auth control against
   * the same endpoint is refused, and only the bearer turns that into a 200,
   * which is exactly what a peer instance's server-side directory fetch does.
   *
   * A `finally` revokes the credential by label whatever happens, so a failure
   * before the UI revoke neither leaves a live credential nor a usable one in a
   * retained trace: a revoked value is inert.
   */
  test('mints a connection credential in the UI, a peer connects with it, and revocation refuses the next fetch', async ({
    page,
    playwright,
  }) => {
    await onB(page, '/remotes');
    const section = page.locator('#connection-credentials');
    await expect(section).toBeVisible();

    const label = `e2e connection ${Date.now()}`;
    // A context with no storage state carries no cookies, the only credential
    // it can present is the bearer set per request.
    const peer = await playwright.request.newContext();
    const directory = `${BASE_URL_B}/api/v1/instance/directory`;
    try {
      await section.getByLabel('Label').fill(label);
      await section.getByRole('button', { name: 'Mint credential' }).click();

      const mintDialog = page.getByRole('dialog', { name: /shown exactly once/ });
      await expect(mintDialog).toBeVisible();
      const value = (await mintDialog.locator('.machine__token').textContent())?.trim() ?? '';
      expect(value).not.toBe('');

      // Control: without the credential the same request is refused, proving the
      // 200 below is the bearer's doing and not an ambient cookie.
      const anonymous = await peer.get(directory);
      expect(anonymous.status(), 'the directory fetch is not open without a credential').toBe(401);

      // A peer presents the minted value at the directory fetch, the one
      // operation an instance-connection credential may reach, and connects.
      const connected = await peer.get(directory, {
        headers: { Authorization: `Bearer ${value}` },
      });
      expect(connected.status()).toBe(200);

      // The value is display-once: confirm storage, dismiss, and it is gone from
      // the surface, the inventory shows metadata only.
      await mintDialog.getByLabel(/I have stored this credential/).check();
      await mintDialog.getByRole('button', { name: 'Done' }).click();
      await expect(mintDialog).toBeHidden();

      const row = section.locator('.connection').filter({ hasText: label });
      await expect(row.getByText('live', { exact: true })).toBeVisible();
      // The full plaintext must be absent (its prefix_hint legitimately is not).
      // Asserting on a boolean keeps the value out of any failure diagnostic.
      const surfaceText = (await section.textContent()) ?? '';
      expect(surfaceText.includes(value), 'the plaintext value is present in the inventory').toBe(
        false,
      );

      // Revocation states the consequence before it commits (AC#4).
      await row.getByRole('button', { name: `Revoke ${label}` }).click();
      const revokeDialog = page.getByRole('dialog', { name: new RegExp(`Revoke ${label}`) });
      await expect(revokeDialog).toContainText('credential rejected');
      await expect(revokeDialog).toContainText('Active workspace sessions');
      await expect(revokeDialog).toContainText('unaffected');
      await revokeDialog.getByRole('button', { name: 'Revoke credential' }).click();
      await expect(revokeDialog).toBeHidden();

      await expect(row.getByText('revoked', { exact: true })).toBeVisible();

      // The same presentation is now refused, revocation bit at the next fetch.
      const refused = await peer.get(directory, {
        headers: { Authorization: `Bearer ${value}` },
      });
      expect(refused.status()).toBe(401);
    } finally {
      // Revoke by label from within B's own origin (cookies + CSRF available),
      // whatever state the assertions left the credential in.
      await revokeConnectionByLabel(page, label);
      await peer.dispose();
    }
  });

  test('the popup ceremony opens a workspace, and both kill switches close it', async ({
    page,
    context,
  }) => {
    // --- consent, through the serving instance's own UI ---------------------
    const b = await context.newPage();
    await onB(b, '/remotes');
    await allowOrigin(b);

    // --- the ceremony -------------------------------------------------------
    await page.goto('/remotes');
    const entry = card(page);
    // Preparation is eager; the enabled action proves the network work has
    // completed before the click that must synchronously open the popup.
    const proceed = entry.getByRole('button', { name: /^Continue to / });
    await expect(proceed).toBeVisible({ timeout: 30_000 });
    const popupOpened = context.waitForEvent('page');
    // The redemption response is where the bearer exists on the wire exactly
    // once. Capturing it here is what turns the persistence assertion below
    // from "no string that looks like a bearer is stored" into "THIS bearer is
    // stored nowhere".
    const redemption = page.waitForResponse(
      (r) => r.url().includes('/api/v1/auth/workspace/redeem') && r.ok(),
    );
    const liveness = page.waitForRequest((r) => r.url().includes('/api/v1/me/sessions'));
    await proceed.click();
    await expect(entry.getByRole('button', { name: 'Waiting for sign-in…' })).toBeDisabled();

    const popup = await popupOpened;
    await popup.waitForLoadState();
    await expect(popup.locator('.workspace-consent__origin')).toContainText(VIEWING_ORIGIN);
    // `noopener` ASSERTED, not claimed. Without it the remote's page keeps a
    // handle on the viewing shell and can navigate it to phishing content;
    // removing the flag would otherwise have changed no assertion in this file.
    expect(
      await popup.evaluate(() => globalThis.opener === null),
      'the popup can reach back into the viewing shell, window.opener is not null',
    ).toBe(true);
    // The ceremony is on the REMOTE'S origin. This assertion is the whole
    // architecture in one line: nothing about authenticating to B happens on
    // A, and no code path exists by which A's server could.
    expect(new URL(popup.url()).origin, 'the popup is not on the remote origin').toBe(
      new URL(BASE_URL_B).origin,
    );
    await expect(popup.getByRole('heading', { name: 'Authorize this workspace' })).toBeVisible();
    // The popup's OWN tab-scoped stores, inspected at the last moment the tab
    // is on B's origin. This is the only browsing context that ever holds
    // B-origin sessionStorage during the ceremony, and the only ceremony
    // artifact it has seen by now is the handoff state, assert B's ceremony
    // pages stash no workspace-grammar artifact (ws bearer, hc code, hs state;
    // B's own script-readable CSRF token is legitimately present and is not a
    // ceremony artifact). The bearer itself CANNOT appear in this tab's
    // B-origin storage at any later moment either: it is minted by the shell's
    // redemption call after the popup has left B for the callback origin
    // (openPrepared in web/src/api/workspace.ts, "the artifact never crosses
    // a redirect"), so this pre-authorization snapshot plus the origin-scoped
    // localStorage check below on page `b` close every store B can write.
    const popupStores = await popup.evaluate(() => ({
      local: Object.entries(globalThis.localStorage),
      session: Object.entries(globalThis.sessionStorage),
      cookie: document.cookie,
    }));
    for (const artifact of ['hik_1_ws_', 'hik_1_hc_', 'hik_1_hs_']) {
      expect(
        JSON.stringify(popupStores),
        `B's ceremony pages persisted a ${artifact} artifact in the popup tab`,
      ).not.toContain(artifact);
    }
    await popup.getByRole('button', { name: 'Authorize' }).click();

    // The popup lands on THIS origin's callback page and closes itself, it was
    // opened with `noopener`, so there is no `window.opener` to talk back
    // through and the return path is a BroadcastChannel only this origin can
    // open.
    await expect(entry.getByText('Workspace open')).toBeVisible({ timeout: 30_000 });

    // --- the bearer is in JS MEMORY ONLY, proven against the real value -----
    const redeemed: unknown = await (await redemption).json();
    const value =
      typeof redeemed === 'object' && redeemed !== null && 'value' in redeemed
        ? String(redeemed.value)
        : '';
    expect(value, 'the redemption returned no bearer to check').not.toBe('');

    const stored = await page.evaluate(() => ({
      local: Object.entries(globalThis.localStorage),
      session: Object.entries(globalThis.sessionStorage),
      cookie: document.cookie,
    }));
    expect(JSON.stringify(stored), 'the workspace bearer was persisted on the viewing origin').not.toContain(
      value,
    );
    // document.cookie sees only script-readable cookies on THIS origin. The
    // cookie jar is where an HttpOnly cookie would be, and B's jar is where the
    // remote would have set one, both are checked, because "memory only" is a
    // claim about every store either origin can write.
    const jar = await context.cookies([VIEWING_ORIGIN, BASE_URL_B]);
    expect(JSON.stringify(jar), 'the workspace bearer reached a cookie jar').not.toContain(value);
    // The SERVING origin's script-visible stores get the same inspection.
    // localStorage and document.cookie are ORIGIN-scoped, so this tab sees any
    // write the popup's B pages made. sessionStorage is tab-scoped and this
    // tab's says nothing about the popup's, that gap is closed structurally
    // by the pre-authorization popup snapshot above: the bearer is minted only
    // after the popup has left B's origin, so no B-tab sessionStorage moment
    // exists in which it could have been stored.
    const storedB = await b.evaluate(() => ({
      local: Object.entries(globalThis.localStorage),
      cookie: document.cookie,
    }));
    expect(
      JSON.stringify(storedB),
      'the workspace bearer was persisted on the serving origin',
    ).not.toContain(value);

    // And the transport is what the ADR requires: the bearer rides an
    // Authorization header, and nothing ambient travels with it.
    const probe = await liveness;
    const headers = await probe.allHeaders();
    expect(headers['authorization'], 'the liveness probe carries no bearer').toBe(`Bearer ${value}`);
    expect(headers['cookie'], 'the cross-origin probe carried cookies').toBeUndefined();

    // --- kill switch 1: the remote withdraws consent ------------------------
    await onB(b, '/remotes');
    await b
      .getByRole('button', { name: `Remove ${VIEWING_ORIGIN} and kill its workspace sessions` })
      .click();
    await expect(b.getByText('revoked 1 workspace session')).toBeVisible();

    // And the shell notices, on its own, within one liveness poll.
    await expect(entry.getByText('Workspace session ended')).toBeVisible({ timeout: 30_000 });
    // Eager preparation correctly fails while this origin is no longer
    // allowlisted; the launcher must expose a truthful retry, not claim ready.
    await expect(entry.getByRole('button', { name: 'Try again' })).toBeVisible({ timeout: 30_000 });

    // --- kill switch 2: revoked from the remote's active-session list -------
    await onB(b, '/remotes');
    await allowOrigin(b);

    await entry.getByRole('button', { name: 'Try again' }).click();
    const proceedAgain = entry.getByRole('button', { name: /^Continue to / });
    await expect(proceedAgain).toBeVisible({ timeout: 30_000 });
    const second = context.waitForEvent('page');
    await proceedAgain.click();
    const popup2 = await second;
    await popup2.waitForLoadState();
    await popup2.getByRole('button', { name: 'Authorize' }).click();
    await expect(entry.getByText('Workspace open')).toBeVisible({ timeout: 30_000 });

    // The workspace session appears in the REMOTE'S own list as its own
    // artifact type, carrying the origin it was issued to, criterion 5.
    await onB(b, '/settings');
    const workspaceRow = b.locator('.session').filter({ hasText: 'workspace' });
    await expect(workspaceRow).toBeVisible();
    await expect(workspaceRow).toContainText(VIEWING_ORIGIN);
    await workspaceRow.getByRole('button', { name: /^Revoke the workspace session/ }).click();
    await expect(workspaceRow).toHaveCount(0);

    // Mid-flight: the shell finds out at its next request, which is what
    // "bites at the next presentation" means from out here.
    await expect(entry.getByText('Workspace session ended')).toBeVisible({ timeout: 30_000 });

    await b.close();
  });

  /**
   * OPERATING the remote, the reopen's core: a live workspace must route
   * matrix/values reads and edits to the REMOTE, and NEVER to the viewing
   * instance's server. This is the criterion the badge-only version failed.
   */
  test("A opens and operates B's project, and no operation touches A's server", async ({
    page,
    context,
  }) => {
    const b = readServing();

    // Consent, through B's own UI.
    const bPage = await context.newPage();
    await onB(bPage, '/remotes');
    await allowOrigin(bPage);
    await bPage.close();

    // Open the workspace with the popup ceremony.
    await page.goto('/remotes');
    const entry = card(page);
    const proceed = entry.getByRole('button', { name: /^Continue to / });
    await expect(proceed).toBeVisible({ timeout: 30_000 });
    const popupOpened = context.waitForEvent('page');
    await proceed.click();
    const popup = await popupOpened;
    await popup.waitForLoadState();
    await popup.getByRole('button', { name: 'Authorize' }).click();
    await expect(entry.getByText('Workspace open')).toBeVisible({ timeout: 30_000 });

    // THE TRIPWIRE. Fail if any product-data call reaches THIS instance's server
    // while the workspace is open: a missed transport thread would send that one
    // call here, same-origin, with cookies, and render A's data as B's. The
    // shell's own reads (its rail's `me/orgs`, retention health) are elsewhere;
    // `/api/v1/orgs/**` is the project-data surface, and inside a workspace every
    // one of those must be spoken to B, never here.
    const leaked: string[] = [];
    await page.route(`${BASE_URL}/api/v1/orgs/**`, async (route) => {
      leaked.push(route.request().method() + ' ' + new URL(route.request().url()).pathname);
      await route.abort('failed');
    });

    // Navigate into B's project through the picker, a CLIENT-SIDE navigation,
    // because the bearer lives in memory and a full load would drop it. The
    // picker read B's own orgs and projects over the bearer to build this link.
    const picker = entry.locator('.remote__picker');
    await expect(picker.getByRole('heading', { name: 'Open a project' })).toBeVisible({
      timeout: 30_000,
    });
    await picker.getByText('serving-co').waitFor();
    await picker.getByRole('link', { name: 'vault' }).click();

    // B's config value is delivered in plaintext and rendered from the remote.
    // Its presence proves the matrix read reached B; the banner proves the shell
    // knows whose data it is showing.
    await expect(page.getByText(b.value)).toBeVisible({ timeout: 30_000 });
    await expect(page.locator('.workspace-banner')).toContainText(BASE_URL_B);

    // And nothing leaked to A. This is the no-proxy claim, checked at runtime
    // rather than argued: `api/noproxy_test.go` proves the
    // server grew no proxy endpoint; this proves the browser used none.
    expect(leaked, `product-data calls reached the viewing server: ${leaked.join(', ')}`).toEqual(
      [],
    );

    // Clean up the session this test opened. Both Playwright projects run
    // against the SAME pair of instances and the specs run in order, so a
    // workspace session left behind here is one a later test's kill-switch
    // assertion would count, its "revoked 1 workspace session" is a real
    // assertion and must not be loosened to absorb this one's litter.
    const cleanup = await context.newPage();
    await onB(cleanup, '/remotes');
    await cleanup
      .getByRole('button', { name: `Remove ${VIEWING_ORIGIN} and kill its workspace sessions` })
      .click();
    await expect(cleanup.getByText('revoked 1 workspace session')).toBeVisible();
    await cleanup.close();
  });

  /**
   * THE SIGNED-OUT ARRIVAL, which is the FIRST establishment's real shape.
   *
   * A popup opened at a remote the human has never signed into on this device
   * lands with no session for that instance. Bouncing it to /login would throw
   * away the `state` the transaction is addressed by, so the approve page
   * renders the login itself and the URL survives, a small piece of routing
   * with nothing else asserting it. The suite's shared storage state carries
   * sessions for BOTH instances, so the happy path above never reaches this
   * branch at all: breaking the public signed-out route, the state-preserving
   * login, or the post-login approval would have left it green.
   */
  test('a popup arriving signed out logs in in place and still approves', async ({
    page,
    context,
  }) => {
    const b = await context.newPage();
    await onB(b, '/remotes');
    await allowOrigin(b);
    await b.close();

    // Sign the context OUT of the SERVING instance only. The viewing shell must
    // stay signed in, this is the state a human is in the first time they open
    // a workspace at a remote. B's cookies are kept so the fully stepped-up
    // administrator session can be restored for the cleanup at the end: the
    // password login this test performs in the popup is single-factor, and
    // every instance-scope surface on B is MFA-mandatory, so it cannot reach
    // B's own remotes page afterwards.
    // Captured by DOMAIN, not by URL. `context.cookies(url)` applies the
    // browser's own delivery rules, and a `Secure` cookie is not delivered to
    // an `http://` URL whose host is an IP LITERAL, Chrome's plaintext
    // carve-out for secure cookies is `localhost` by name. B is the address
    // literal (A holds `localhost`, which WebAuthn requires of the instance
    // that runs passkey ceremonies), so the URL form silently returns nothing
    // here and the restore below would put back an empty jar.
    const bSession = (await context.cookies()).filter((c) => c.domain === HOST_B);
    await context.clearCookies({ domain: HOST_B });

    await page.goto('/remotes');
    const entry = card(page);
    const proceed = entry.getByRole('button', { name: /^Continue to / });
    await expect(proceed).toBeVisible({ timeout: 30_000 });
    const popupOpened = context.waitForEvent('page');
    await proceed.click();

    const popup = await popupOpened;
    await popup.waitForLoadState();
    const arrivedAt = popup.url();
    expect(new URL(arrivedAt).origin).toBe(new URL(BASE_URL_B).origin);

    // The login is rendered IN PLACE, on the approve route, not at /login.
    await expect(popup.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    expect(new URL(popup.url()).pathname, 'the popup was bounced off the approve route').toBe(
      new URL(arrivedAt).pathname,
    );
    expect(new URL(popup.url()).search, 'the transaction state was lost at the login').toBe(
      new URL(arrivedAt).search,
    );

    await popup.getByLabel('Username').fill(ADMIN.username);
    await popup.getByLabel('Password').fill(ADMIN.password);
    await popup.getByRole('button', { name: 'Sign in' }).click();

    // And the transaction the popup arrived holding is the one it approves.
    await popup.getByRole('button', { name: 'Authorize' }).click();
    await expect(entry.getByText('Workspace open')).toBeVisible({ timeout: 30_000 });

    // Clean up the session this test opened. Both Playwright projects run
    // against the SAME pair of instances, so a workspace session left behind
    // here is one the other project's kill-switch test would find and count , 
    // its "revoked 1 workspace session" assertion is a real assertion and must
    // not be loosened to absorb this one's litter.
    await context.clearCookies({ domain: HOST_B });
    await context.addCookies(bSession);
    const cleanup = await context.newPage();
    await onB(cleanup, '/remotes');
    await cleanup
      .getByRole('button', { name: `Remove ${VIEWING_ORIGIN} and kill its workspace sessions` })
      .click();
    await expect(cleanup.getByText('revoked 1 workspace session')).toBeVisible();
    await cleanup.close();
  });

  // The matrix is DERIVED from the registry, not re-listed beside it: this flow
  // asserts exactly the surfaces it claims. Both themes, because the palette is
  // a dual-theme palette and half of it going unchecked is half a claim.
  for (const surface of surfacesForFlow('workspace')) {
    for (const scheme of ['dark', 'light'] as const) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        const workspaceTransactionState = 'hik_1_test';
        const workspaceTransactionPath =
          `/api/v1/auth/workspace/transactions/${workspaceTransactionState}`;
        let workspaceTransactionReads = 0;
        const cliTransactionState = 'hik_1_test';
        const cliTransactionPath =
          `/api/v1/auth/cli-reauth/transactions/${cliTransactionState}`;
        let cliTransactionReads = 0;

        if (surface.id === 'workspace-approve') {
          // The approval page now derives its ceremony exclusively from the
          // server-owned transaction purpose. Stub that exact read so this
          // visual matrix exercises the real establishment branch instead of
          // rendering the fail-closed missing-transaction state.
          await page.route(`${BASE_URL}${workspaceTransactionPath}`, async (route) => {
            const request = route.request();
            const requestURL = new URL(request.url());
            expect(request.method()).toBe('GET');
            expect(requestURL.pathname).toBe(workspaceTransactionPath);
            expect(requestURL.search).toBe('');
            workspaceTransactionReads += 1;
            await route.fulfill({
              status: 200,
              contentType: 'application/json',
              body: JSON.stringify({
                state: workspaceTransactionState,
                purpose: 'establishment',
                requesting_origin: VIEWING_ORIGIN,
                key_ids: [],
                expires_at: '2099-01-01T00:00:00Z',
              } satisfies WorkspaceHandoffEstablishment),
            });
          });
        }

        if (surface.id === 'cli-reauth') {
          // This matrix addresses a visual surface, not a durable handoff
          // fixture. Stub only its exact display-policy read so the real page
          // renders the interactive state that the registry claims. The
          // response intentionally contains no handoff id, bearer, verifier,
          // code, or credential.
          await page.route(`${BASE_URL}${cliTransactionPath}`, async (route) => {
            const request = route.request();
            const requestURL = new URL(request.url());
            expect(request.method()).toBe('GET');
            expect(requestURL.pathname).toBe(cliTransactionPath);
            expect(requestURL.search).toBe('');
            cliTransactionReads += 1;
            await route.fulfill({
              status: 200,
              contentType: 'application/json',
              body: JSON.stringify({
                state: cliTransactionState,
                purpose: 'adapter',
                operation: 'adapter.sync',
                key_ids: [],
                environments: [
                  {
                    environment_id: 'env_00000000-0000-4000-8000-000000000001',
                    effective_window_seconds: 300,
                    requires_webauthn: false,
                  },
                ],
                redirect_uri: 'http://127.0.0.1:40123/callback',
                expires_at: '2099-01-01T00:00:00Z',
              }),
            });
          });
        }

        // The two ceremony pages are reached by a redirect in life, so they are
        // visited with the query they are addressed by, the approve page with
        // a state to consent to, the callback page with none, which is its own
        // refusal state and the one that renders without closing the window.
        const addressedPath =
          surface.id === 'workspace-approve'
            ? `${surface.path}?state=${workspaceTransactionState}`
            : surface.id === 'cli-reauth'
              ? `${surface.path}?transaction=${cliTransactionState}`
              : surface.path;
        await page.goto(addressedPath);

        const heading = page.getByRole('heading', { level: 1 }).first();
        await expect(heading).toBeVisible();
        if (surface.id === 'workspace-approve') {
          await expect(page.locator('.workspace-consent__origin')).toContainText(VIEWING_ORIGIN);
          await expect(page.getByRole('button', { name: 'Authorize' })).toBeVisible();
          await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible();
          expect(workspaceTransactionReads).toBe(1);
        }
        if (surface.id === 'cli-reauth') {
          await expect(page.getByLabel('Authenticator code')).toBeVisible();
          await expect(page.getByRole('button', { name: 'Authorize CLI' })).toBeVisible();
          await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible();
          expect(cliTransactionReads).toBe(1);
        }
        // `remotes` renders in the shell, where a card is chrome (`--bg-panel`);
        // the three ceremonies render on a bare page, where the card is a
        // raised sheet (`--bg-raise`). One selector, two roles.
        const inChrome = surface.id === 'remotes';
        const container = page.locator(inChrome ? '.card' : '.login__card').first();

        await expectPinnedAssertionSet(page, {
          flow: 'workspace',
          surface: surface.id,
          theme: scheme,
          text: [heading],
          radii: [[container, 'container']],
          fonts: [[heading, 'ui']],
          colours: [
            [heading, 'color', '--tx'],
            [container, 'backgroundColor', inChrome ? '--bg-panel' : '--bg-raise'],
            [container, 'borderTopColor', '--line'],
          ],
          hairlines: [container],
          density: [],
        });
      });
    }
  }
});

// The live application and authenticated session render the real consent
// component; only its metadata response is varied to exercise worst-case
// layout. The genuine two-instance popup above separately proves issuance.
test.describe('consent summary resilience', () => {
  test.use({ storageState: STORAGE_STATE });

  test('consent recipient and every key remain readable on a narrow popup', async ({ page }, testInfo) => {
    const id = (prefix: string, n: number) => `${prefix}_00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
    const origin = `https://${'a'.repeat(60)}.${'b'.repeat(60)}.${'c'.repeat(60)}.xn--bcher-kva.example:8443`;
    const summary: WorkspaceHandoffStepUp = {
      state: 'layout-state', purpose: 'step-up', requesting_origin: origin,
      operation: 'reveal', environment: id('env', 1),
      key_ids: Array.from({ length: 200 }, (_, n) => id('key', n)),
      expires_at: new Date(Date.now() + 60_000).toISOString(),
    };
    await page.route('**/api/v1/auth/workspace/transactions/layout-state', route => route.fulfill({ json: summary }));
    await page.goto('/workspace/approve?state=layout-state&origin=https://attacker.example');
    await expect(page.locator('.workspace-consent__origin')).toHaveText(`Requesting origin: ${origin}`);
    await expect(page.getByText('attacker.example')).toHaveCount(0);
    const scope = page.getByRole('region', { name: 'Requested scope' });
    await expect(scope.locator('li')).toHaveCount(200);
    const bounds = await scope.boundingBox();
    if (bounds === null) throw new Error('missing scope geometry');
    const viewport = page.viewportSize();
    if (viewport === null) throw new Error('missing viewport');
    expect(bounds.height).toBeLessThanOrEqual(viewport.height * 0.35 + 1);
    await expect(page.getByRole('button', { name: 'Use a passkey' })).toBeInViewport();
    await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toBeInViewport();
    await scope.focus();
    await page.keyboard.press('End');
    await expect(scope.locator('li').last()).toBeInViewport();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await expectNoSeriousAxeViolations(page);
    await page.screenshot({ path: testInfo.outputPath('consent-full-scope.png'), fullPage: true });
  });

  test('consent expired metadata offers no authorization action', async ({ page }) => {
    await page.route('**/api/v1/auth/workspace/transactions/expired-state', route => route.fulfill({ json: {
      state: 'expired-state', purpose: 'establishment', requesting_origin: 'https://xn--bcher-kva.example',
      key_ids: [], expires_at: '2020-01-01T00:00:00Z',
    } }));
    await page.goto('/workspace/approve?state=expired-state');
    await expect(page.getByRole('heading', { name: 'Authorization could not be completed' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Authorize', exact: true })).toHaveCount(0);
  });
});
