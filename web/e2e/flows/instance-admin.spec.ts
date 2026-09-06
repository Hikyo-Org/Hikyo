import { expect, type Browser, type Page } from '@playwright/test';
import {
  zAuthMethods,
  zInstanceConfigStatus,
  zUpdateStatus,
  zGrantList,
  zOrg,
  zSamlProviderMutationResult,
  zSamlSpKeyList,
  zScimBinding,
  zScimMappingResult,
  zScimMintResult,
} from '@hikyo/zod';
import { z } from 'zod';

import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expectNoSeriousAxeViolations, expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import { browserApi } from '../fixtures/api.ts';
import {
  ADMIN,
  BASE_URL,
  BASE_URL_B,
  establishSession,
  INSTANCE_GRANT_TARGET,
  nextTotpCode,
  OIDC_PROVIDER,
  readSeed,
  readServing,
  STORAGE_STATE,
  WEBUI_OIDC,
} from '../fixtures/instance.ts';
import { test } from '../fixtures/passkey.ts';
import { totpCode } from '../fixtures/seed.ts';

/**
 * Flow: instance administration (registry surface `instance-admin`) , 
 * mvp-boundary S3's "instance administration", against the locked prototype
 * #29 iteration 15.
 *
 * Three properties this flow exists to hold:
 *
 *  - the organisation enumeration is the OPERATOR's, and it is MFA-mandatory.
 *    A password-only session gets the honest "second factor required" state,
 *    never an empty list, the empty list would be the UI answering a question
 *    it was refused;
 *  - the instance grant listing shows origins, so a break-glass grant is
 *    distinguishable from an ordinary one after an incident. The fixture's own
 *    seeding grants are break-glass, which is what makes this assertable;
 *  - the genuinely host-only SystemProof set (init, migrate, restore
 *    reconciliation, break-glass, host-file custody, startup-only key material)
 *    is stated as absent rather than drawn as disabled controls, while every
 *    remotely operable rotation and re-encryption job (#503) warns before it
 *    runs and the two content-invisible ones (DEK rotation, re-encryption) run
 *    for real, resuming across a reload.
 */

const seed = readSeed();

function samlMetadata(slug: string): string {
  const dir = mkdtempSync(join(tmpdir(), 'hikyo-saml-e2e-'));
  const key = join(dir, 'idp.key');
  const cert = join(dir, 'idp.crt');
  try {
    execFileSync('openssl', [
      'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
      '-keyout', key, '-out', cert, '-days', '365',
      '-subj', `/CN=${slug}`,
    ], { stdio: 'ignore' });
    const certificate = readFileSync(cert, 'utf8')
      .split('\n')
      .filter((line) => !line.startsWith('-----'))
      .join('');
    return (
      '<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" ' +
      'xmlns:ds="http://www.w3.org/2000/09/xmldsig#" ' +
      `entityID="https://idp.example/${slug}">` +
      '<md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">' +
      '<md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data><ds:X509Certificate>' +
      certificate +
      '</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>' +
      `<md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example/${slug}/sso"/>` +
      '</md:IDPSSODescriptor></md:EntityDescriptor>'
    );
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test.describe('instance administration', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test('opens owner-local configuration from instance settings and applies a published revision', async ({ passkeyPage: page }, testInfo) => {
    test.setTimeout(60_000);
    await page.goto('/instance');
    await page.getByRole('link', { name: 'Manage Hikyo configuration', exact: true }).click();
    await expect(page).toHaveURL(/\/instance\/config$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Hikyo configuration' })).toBeVisible();
    await expect(page.getByText('Independent instances', { exact: true })).toBeVisible();
    await expect(page.getByText('Ordinary settings reload live. Bootstrap source changes use an enrolled controlled rollout.', { exact: false })).toBeVisible();
    const status = await browserApi(page, 'GET', '/api/v1/instance/config', zInstanceConfigStatus);
    expect(status.owner_instance_id).not.toBe('');
    expect(status.managed).toBe(true);
    await page.getByRole('link', { name: 'Edit configuration project', exact: true }).click();
    await expect(page.getByText('Hikyo system configuration', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Declare key', exact: true })).toHaveCount(0);
    // The expanded catalogue virtualizes rows; scroll to the channel at the end.
    await page.locator('.matrix__scroll').evaluate((element) => { element.scrollTop = element.scrollHeight; });
    await page.getByRole('button', { name: /^HIKYO_UPDATE_CHANNEL in Production:/ }).click();
    const editor = page.getByRole('dialog');
    await editor.getByLabel('Production value').fill('off');
    await editor.getByRole('button', { name: 'Save 1 draft' }).click();
    await page.getByRole('button', { name: /unpublished edit/ }).click();
    const publish = page.getByRole('region', { name: 'Publish drafts' });
    const protectedConfirmation = publish.getByRole('checkbox', { name: 'I confirm publishing to protected Production.' });
    if (await protectedConfirmation.count() !== 0) await protectedConfirmation.check();
    await publish.getByRole('button', { name: /Publish selected/ }).click();
    const publishPasskey = page.getByRole('button', { name: 'Use a passkey', exact: true });
    if (await publishPasskey.isVisible()) await publishPasskey.click();
    await expect(page.locator('.notice')).toContainText('Published');
    const pending = await browserApi(page, 'GET', '/api/v1/instance/config', zInstanceConfigStatus);
    expect(pending.generation).toBe(status.generation);
    expect(pending.desired_revision).toBe(status.desired_revision);
    expect(pending.latest_revision).not.toBe(status.latest_revision);
    await page.getByRole('link', { name: 'Review and apply', exact: true }).click();
    await expect(page.getByLabel('Published revision to apply or test')).toHaveValue(String(pending.latest_revision));
    await page.getByRole('button', { name: 'Apply selected revision', exact: true }).click();
    await expect(page.getByRole('dialog').getByText('Reload live', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Authorize with passkey', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect.poll(async () => (await browserApi(page, 'GET', '/api/v1/instance/config', zInstanceConfigStatus)).state).toBe('active');
    const applied = await browserApi(page, 'GET', '/api/v1/instance/config', zInstanceConfigStatus);
    expect(applied.desired_revision).toBe(pending.latest_revision);
    expect(applied.generation).toBe(status.generation + 1n);
    const updates = await browserApi(page, 'GET', '/api/v1/instance/update-status', zUpdateStatus);
    expect(updates.channel).toBe('off');
    expect(await page.locator('main').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
    await expectNoSeriousAxeViolations(page);
    await page.locator('#configuration-owner').screenshot({ path: testInfo.outputPath('managed-configuration-applied.png') });
    await page.getByRole('heading', { name: 'Nodes on this instance', exact: true }).scrollIntoViewIfNeeded();
    await page.locator('#configuration-nodes').screenshot({ path: testInfo.outputPath('managed-configuration-nodes.png') });
  });

  test('applies an independent owner without changing the viewing instance', async ({ page }, testInfo) => {
    test.setTimeout(60_000);
    const viewing = await browserApi(page, 'GET', '/api/v1/instance/config', zInstanceConfigStatus);
    await page.goto(`${BASE_URL_B}/instance/config`);
    const readRemote = async () => {
      const response = await page.evaluate(async () => {
        const result = await fetch('/api/v1/instance/config');
        const body: unknown = await result.json();
        return { status: result.status, body };
      });
      expect(response.status).toBe(200);
      return zInstanceConfigStatus.parse(response.body);
    };
    const remote = await readRemote();
    expect(remote.owner_instance_id).not.toBe(viewing.owner_instance_id);
    expect(remote.binding?.project_id).not.toBe(viewing.binding?.project_id);
    expect(remote.generation).not.toBe(viewing.generation);
    await page.getByRole('link', { name: 'Edit configuration project', exact: true }).click();
    // The expanded catalogue virtualizes rows; scroll to the channel at the end.
    await page.locator('.matrix__scroll').evaluate((element) => { element.scrollTop = element.scrollHeight; });
    await page.getByRole('button', { name: /^HIKYO_UPDATE_CHANNEL in Production:/ }).click();
    const editor = page.getByRole('dialog');
    await editor.getByLabel('Production value').fill('nightly');
    await editor.getByRole('button', { name: 'Save 1 draft' }).click();
    await page.getByRole('button', { name: /unpublished edit/ }).click();
    await page.getByRole('region', { name: 'Publish drafts' }).getByRole('button', { name: /Publish selected/ }).click();
    await expect(page.locator('.notice')).toContainText('Published');
    const published = await readRemote();
    expect(published.generation).toBe(remote.generation);
    await page.getByRole('link', { name: 'Review and apply', exact: true }).click();
    await expect(page.getByLabel('Published revision to apply or test')).toHaveValue(String(published.latest_revision));
    await page.getByRole('button', { name: 'Apply selected revision', exact: true }).click();
    await expect(page.getByRole('dialog').getByText('Reload live', { exact: true })).toBeVisible();
    await page.getByLabel('Fresh authenticator code').fill(totpCode(readServing().otpauth, new Date(Date.now() + 30_000)));
    await page.getByRole('button', { name: 'Authorize with code', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect.poll(async () => (await readRemote()).state).toBe('active');
    const applied = await readRemote();
    expect(applied.desired_revision).toBe(published.latest_revision);
    expect(applied.generation).toBe(remote.generation + 1n);
    const remoteUpdates: unknown = await page.evaluate(async () => (await fetch('/api/v1/instance/update-status')).json());
    expect(zUpdateStatus.parse(remoteUpdates).channel).toBe('nightly');
    const unchanged = await browserApi(page, 'GET', '/api/v1/instance/config', zInstanceConfigStatus);
    expect(unchanged.generation).toBe(viewing.generation);
    expect(unchanged.desired_revision).toBe(viewing.desired_revision);
    expect((await browserApi(page, 'GET', '/api/v1/instance/update-status', zUpdateStatus)).channel).toBe('off');
    await page.locator('#configuration-owner').screenshot({ path: testInfo.outputPath('managed-configuration-independent-owner.png') });
  });


  test.beforeEach(async ({ page }) => {
    await page.goto('/instance');
    await expect(
      page.getByRole('heading', { name: 'Instance settings', level: 1 }),
    ).toBeVisible();
  });

  test('enumerates the organisations on the instance', async ({ page }) => {
    const orgs = page.locator('#instance-orgs');
    // Setup creates the protected configuration root; the fixture adds its
    // tenant and a second empty organisation.
    await expect(orgs.locator(':scope > .settings-row')).toHaveCount(3);
    await expect(orgs).toContainText(seed.orgName);
    await expect(orgs).toContainText(seed.orgBName);
    await expect(orgs.getByRole('link', { name: seed.orgName })).toBeVisible();
  });

  test('shows instance grants with the origin that holds them', async ({ page }) => {
    await page.goto('/instance/members');
    const grants = page.locator('#members-list');
    // The seeding grants are written by the host-local `admin grant` verb, so
    // they carry the break-glass origin, the one distinction the membership
    // surface exists to preserve.
    await expect(grants.getByText('break-glass').first()).toBeVisible();
    await expect(grants.locator(`.member-name[title="${seed.principal}"]`).first()).toHaveText(ADMIN.displayName);
    await expect(grants).toContainText('manage-projects');
    await expect(grants).toContainText('inherit downward into every organisation');
  });

  test('states the pruner health and where else the same fact lives', async ({ page }) => {
    const health = page.locator('#instance-retention');
    await expectStatusIsTextAndAria(page, health.getByRole('status'));
    await expect(health).toContainText('Payload pruning');
    await expect(health).toContainText('hikyo doctor');
  });

  test('warns before rotating the change-token key, and does not rotate on cancel', async ({
    page,
  }) => {
    const keys = page.locator('#instance-keys');
    // The genuinely host-only set is named as absent, not drawn as disabled
    // buttons: init, migrate, restore reconciliation and break-glass have no
    // network surface at all, by ADR.
    await expect(keys).toContainText('local host authority');
    await expect(keys).toContainText('CLI-at-the-box, not CLI-over-network');
    await expect(keys).toContainText('init');
    await expect(keys).toContainText('break-glass');

    await keys.getByRole('button', { name: 'Rotate the change-token key' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Every conditional-fetch cursor in circulation stops');
    await expect(dialog).toContainText('cannot be undone');
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(page.getByRole('dialog')).toBeHidden();
    await expect(keys.getByRole('button', { name: 'Rotate the change-token key' })).toBeFocused();
  });

  test('names the consequences of the master-key and root-key jobs before running', async ({
    page,
  }) => {
    const keys = page.locator('#instance-keys');

    await keys.getByRole('button', { name: 'Rotate the master key' }).click();
    let dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('re-wrapped under it');
    await expect(dialog).toContainText('finalize the root rotation first');
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(page.getByRole('dialog')).toBeHidden();

    // The root-key rotation is a three-phase job; prepare names the host step
    // and the crash-safety fact before anything is sealed.
    await keys.getByRole('button', { name: 'Prepare' }).click();
    dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('No key material crosses the wire');
    await expect(dialog).toContainText('install the new root at the primary source');
    await expect(dialog).toContainText('bootable under either root');
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(page.getByRole('dialog')).toBeHidden();
  });

  test('rotates the instance DEK and re-encrypts, resuming across a reload', async ({ page }) => {
    const keys = page.locator('#instance-keys');

    // Rotate the instance DEK for real. It appends a version and is
    // content-invisible: no other flow's plaintext moves. This leaves the
    // instance's credential ciphertext PENDING re-encryption onto the new
    // version, an incomplete rotation.
    await keys.getByRole('button', { name: 'Rotate the instance DEK' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('incomplete until');
    await dialog.getByRole('button', { name: 'Rotate the DEK' }).click();
    await expect(page.getByRole('dialog')).toBeHidden();
    await expect(page.locator('.notice')).toContainText('The instance DEK was rotated');

    // Reload BEFORE any re-encryption: the pending walk now lives only in the
    // server's cursor, with no client state to carry it. A fresh page must be
    // able to pick it up and drive it to a clean, complete end, the real
    // interrupted-then-resumed recovery, not a re-run of an already-finished job.
    await page.reload();
    await expect(
      page.getByRole('heading', { name: 'Instance settings', level: 1 }),
    ).toBeVisible();
    await keys.getByRole('button', { name: 'Re-encrypt the instance' }).click();
    await expect(page.locator('.notice')).toContainText('Instance re-encryption complete');

    // Idempotent: everything now sits on the active version, so a further run
    // moves nothing and says so.
    await keys.getByRole('button', { name: 'Re-encrypt the instance' }).click();
    await expect(page.locator('.notice')).toContainText(
      'nothing to move; all ciphertext is already on the active DEK version',
    );
  });

  test('reads and saves the machine-credential ceiling', async ({ page }) => {
    const settings = page.locator('#instance-settings');
    await settings.getByRole('button', { name: 'edit' }).last().click();
    const lifetime = settings.getByLabel('Maximum finite lifetime (seconds)');
    const live = settings.getByLabel('Maximum live credentials per service account');
    await expect(lifetime).not.toHaveValue('');
    await expect(live).not.toHaveValue('');
    await settings.getByRole('button', { name: 'Save credential policy' }).click();
    const done = page.locator('.notice').filter({ hasText: 'Credential policy updated' });
    await expectStatusIsTextAndAria(page, done);
  });

  test('clears an affected-credential preview on edit and confirms its exact snapshot', async ({
    page,
  }) => {
    const requestSchema = z.object({
      max_finite_lifetime_seconds: z.number().int().positive(),
      allow_indefinite: z.boolean(),
      max_live_credentials: z.number().int().positive(),
      confirm: z.boolean().optional(),
    });
    type PolicyRequest = z.infer<typeof requestSchema>;
    const requests: PolicyRequest[] = [];
    await page.route('**/api/v1/instance/credential-policy', async (route) => {
      if (route.request().method() !== 'PUT') {
        await route.continue();
        return;
      }
      const body = requestSchema.parse(route.request().postDataJSON());
      requests.push(body);
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          applied: body.confirm === true,
          policy: {
            max_finite_lifetime_seconds: body.max_finite_lifetime_seconds,
            allow_indefinite: body.allow_indefinite,
            max_live_credentials: body.max_live_credentials,
          },
          affected: [
            {
              id: 'mcr_00000000-0000-4000-8000-000000000001',
              service_account_id: 'svc_00000000-0000-4000-8000-000000000002',
              reason: 'clamped',
            },
          ],
          clamped_count: body.confirm === true ? 1 : 0,
        }),
      });
    });

    const settings = page.locator('#instance-settings');
    await settings.getByRole('button', { name: 'edit' }).last().click();
    const lifetime = settings.getByLabel('Maximum finite lifetime (seconds)');
    const live = settings.getByLabel('Maximum live credentials per service account');
    await lifetime.fill(String(Math.max(1, Number(await lifetime.inputValue()) - 1)));
    await settings.getByRole('button', { name: 'Save credential policy' }).click();
    await expect(settings.getByRole('alert')).toContainText('Nothing has changed yet');

    await live.fill(String(Number(await live.inputValue()) + 1));
    await expect(settings.getByRole('alert')).toHaveCount(0);

    await settings.getByRole('button', { name: 'Save credential policy' }).click();
    await expect(settings.getByRole('alert')).toContainText('Nothing has changed yet');
    const previewed = requests[requests.length - 1];
    if (previewed === undefined) {
      throw new Error('the credential-policy preview request was not captured');
    }
    await settings.getByRole('button', { name: 'Apply and affect these credentials' }).click();
    await expect(page.locator('.notice').filter({ hasText: 'Credential policy updated' })).toBeVisible();
    expect(requests[requests.length - 1]).toEqual({ ...previewed, confirm: true });
  });

  test('creates and revokes an instance grant with visible provenance', async ({ page }) => {
    await page.goto('/instance/members');
    await page.getByRole('button', { name: 'New grant' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('button', { name: 'Enter an ID for another principal' }).click();
    await dialog.getByLabel('Principal').fill(INSTANCE_GRANT_TARGET);
    await expect(dialog.getByLabel('Scope')).toHaveValue('instance');
    await dialog.getByRole('checkbox', { name: 'read', exact: true }).check();
    await dialog.getByRole('button', { name: 'Grant', exact: true }).click();
    const granted = page.locator('.notice').filter({ hasText: `Grant results for ${INSTANCE_GRANT_TARGET}` });
    await expectStatusIsTextAndAria(page, granted);
    const row = page.getByRole('row').filter({ has: page.locator(`.member-name[title="${INSTANCE_GRANT_TARGET}"]`) });
    await expect(row).toContainText(`manual: ${seed.principal}`);
    await row
      .getByRole('button', { name: `Revoke read on instance · everything for ${INSTANCE_GRANT_TARGET}` })
      .click();
    const revoked = page.locator('.notice').filter({ hasText: 'Revoked read' });
    await expectStatusIsTextAndAria(page, revoked);
    await expect(row).toHaveCount(0);
  });

  // Member invitation at instance scope (#568): how a second operator comes
  // to exist without host access. The invitee's lines are revoked afterwards;
  // the account itself stays (there is no account deletion), under a unique
  // name.
  test('invites an operator at instance scope and refuses the same username twice', async ({
    page,
  }, testInfo) => {
    const username = `operator-${testInfo.project.name}-${Date.now()}`;
    await page.goto('/instance/members');
    await page.getByRole('button', { name: 'Invite', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByRole('heading', { level: 2 })).toHaveText('Invite a member to Instance');
    // Instance scope admits exactly one template.
    await expect(dialog.getByLabel('Role template').locator('option')).toHaveText([
      'No initial grants',
      'operator',
    ]);
    await dialog.getByLabel('Username').fill(username);
    await dialog.getByLabel('Display name (optional)').fill('Second Operator');
    await dialog.getByLabel('Role template').selectOption('operator');
    await dialog.getByRole('button', { name: 'Invite', exact: true }).click();
    const principal = (await dialog.getByTestId('issued-principal').textContent()) ?? '';
    expect(principal).toMatch(/^(prn|usr)_/);
    await expect(dialog.getByTestId('issued-authority')).not.toBeEmpty();
    try {
      await dialog.getByRole('button', { name: 'Close' }).click();
      await expect(page.getByRole('dialog')).toBeHidden();
      await expectStatusIsTextAndAria(
        page,
        page.locator('.notice').filter({ hasText: `Invited ${username} at Instance as operator` }),
      );
      // Every expanded line is the inviter's, at instance scope.
      const row = page.getByRole('row').filter({ has: page.locator(`.member-name[title="${principal}"]`) });
      await expect(row.locator('.member-name')).toHaveText('Second Operator');
      await expect(row).toContainText('manage-members');
      await expect(row).toContainText(`manual: ${seed.principal}`);
      const listed = await browserApi(page, 'GET', '/api/v1/instance/grants', zGrantList);
      expect(listed.items.filter((grant) => grant.principal_id === principal).length).toBeGreaterThan(1);

      // The same username again is a conflict, said inline, with the form kept.
      await page.getByRole('button', { name: 'Invite', exact: true }).click();
      const again = page.getByRole('dialog');
      await again.getByLabel('Username').fill(username);
      await again.getByRole('button', { name: 'Invite', exact: true }).click();
      const refusal = again.getByRole('alert');
      await expectStatusIsTextAndAria(page, refusal);
      await expect(refusal).toContainText('already taken');
      await expect(again.getByLabel('Username')).toHaveValue(username);
      await again.getByRole('button', { name: 'Cancel' }).click();
      await expect(page.getByRole('dialog')).toBeHidden();
    } finally {
      const listed = await browserApi(page, 'GET', '/api/v1/instance/grants', zGrantList);
      for (const grant of listed.items.filter((item) => item.principal_id === principal)) {
        const query = `principal=${encodeURIComponent(principal)}&capability=${encodeURIComponent(grant.capability)}`;
        await browserApi(page, 'DELETE', `/api/v1/instance/grants?${query}`, z.null());
      }
    }
  });

  test('applies the instance role template through the UI and revokes every created line', async ({
    page,
  }, testInfo) => {
    testInfo.setTimeout(60_000);
    const slug = `instance-template-${testInfo.project.name}`;
    const metadata = samlMetadata(slug);
    const providerInput = {
      display_name: slug,
      entity_id: `https://idp.example/${slug}`,
      metadata_source: 'file',
      metadata_document: metadata,
      metadata_url: null,
      assurance_policy: null,
      allow_email_nameid: false,
      force_sign_requests: false,
      enabled: true,
    } as const;
    const preview = await browserApi(
      page,
      'PUT',
      `/api/v1/instance/saml-providers/${slug}`,
      zSamlProviderMutationResult,
      providerInput,
    );
    expect(preview.applied).toBe(false);
    const applied = await browserApi(
      page,
      'PUT',
      `/api/v1/instance/saml-providers/${slug}`,
      zSamlProviderMutationResult,
      {
        ...providerInput,
        confirmed_fingerprints: preview.required_fingerprints,
        confirmed_endpoints: preview.required_endpoints,
      },
    );
    if (!applied.applied || applied.provider === null || applied.provider === undefined) {
      throw new Error('the throwaway SAML provider did not apply after its trust diff was confirmed');
    }
    const provider = applied.provider;
    const binding = await browserApi(
      page,
      'POST',
      `/api/v1/orgs/${seed.org}/scim-bindings`,
      zScimBinding,
      {
        provider_kind: 'saml',
        provider_slug: provider.slug,
        subject_source: 'externalId',
        nameid_format: 'urn:oasis:names:tc:SAML:2.0:nameid-format:persistent',
        nameid_qualifier_present: false,
        nameid_sp_qualifier_present: false,
      },
    );
    const minted = await browserApi(
      page,
      'POST',
      `/api/v1/orgs/${seed.org}/scim-bindings/${binding.id}/credentials`,
      zScimMintResult,
      { proof: await nextTotpCode() },
    );
    const userResponse = await fetch(
      `${BASE_URL}/api/v1/orgs/${seed.org}/scim/v2/${binding.id}/Users`,
      {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${minted.token}`,
          'Content-Type': 'application/scim+json',
        },
        body: JSON.stringify({
          schemas: ['urn:ietf:params:scim:schemas:core:2.0:User'],
          userName: `${slug}@example.invalid`,
          externalId: `${slug}-subject`,
          active: true,
        }),
      },
    );
    const userResponseText = await userResponse.text();
    expect(userResponse.status, userResponseText).toBe(201);
    const userBody: unknown = JSON.parse(userResponseText);
    const user = z.object({ id: z.string() }).parse(userBody);
    const groupResponse = await fetch(
      `${BASE_URL}/api/v1/orgs/${seed.org}/scim/v2/${binding.id}/Groups`,
      {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${minted.token}`,
          'Content-Type': 'application/scim+json',
        },
        body: JSON.stringify({
          schemas: ['urn:ietf:params:scim:schemas:core:2.0:Group'],
          displayName: `${slug}-group`,
          members: [{ value: user.id }],
        }),
      },
    );
    const groupResponseText = await groupResponse.text();
    expect(groupResponse.status, groupResponseText).toBe(201);
    const groupBody: unknown = JSON.parse(groupResponseText);
    const group = z.object({ id: z.string() }).parse(groupBody);
    const mapping = await browserApi(
      page,
      'POST',
      `/api/v1/orgs/${seed.org}/scim-bindings/${binding.id}/mappings`,
      zScimMappingResult,
      { group_id: group.id, template: 'viewer' },
    );
    const orgGrants = await browserApi(
      page,
      'GET',
      `/api/v1/orgs/${seed.org}/grants`,
      zGrantList,
    );
    const seededGrant = orgGrants.items.find((grant) =>
      grant.origins.some(
        (origin) => origin.kind === 'scim' && origin.subject.includes(mapping.mapping.id),
      ),
    );
    if (seededGrant === undefined) {
      throw new Error('the throwaway SCIM mapping did not expose its human principal in grants');
    }
    const principal = seededGrant.principal_id;
    try {
      await page.goto('/instance/members');
      await page.getByRole('button', { name: 'New grant' }).click();
      const dialog = page.getByRole('dialog');
      await dialog.getByRole('button', { name: 'Enter an ID for another principal' }).click();
      await dialog.getByLabel('Principal').fill(principal);
      await expect(dialog.getByLabel('Scope')).toHaveValue('instance');
      await dialog.getByRole('radio', { name: 'Apply a role template' }).check();
      await dialog.getByLabel('Role template', { exact: true }).selectOption('operator');
      await dialog.getByRole('button', { name: 'Grant', exact: true }).click();
      await expect(page.locator('.notice').filter({ hasText: 'Applied operator to' })).toContainText(
        principal,
      );
      const listed = await browserApi(page, 'GET', '/api/v1/instance/grants', zGrantList);
      expect(listed.items.filter((grant) => grant.principal_id === principal).length).toBeGreaterThan(0);
    } finally {
      const listed = await browserApi(page, 'GET', '/api/v1/instance/grants', zGrantList);
      for (const grant of listed.items.filter((item) => item.principal_id === principal)) {
        const query = `principal=${encodeURIComponent(principal)}&capability=${encodeURIComponent(grant.capability)}`;
        await browserApi(page, 'DELETE', `/api/v1/instance/grants?${query}`, z.null());
      }
      await browserApi(
        page,
        'DELETE',
        `/api/v1/orgs/${seed.org}/scim-bindings/${binding.id}/mappings?group=${encodeURIComponent(group.id)}`,
        zScimMappingResult,
      );
      const deletedGroup = await fetch(
        `${BASE_URL}/api/v1/orgs/${seed.org}/scim/v2/${binding.id}/Groups/${group.id}`,
        { method: 'DELETE', headers: { Authorization: `Bearer ${minted.token}` } },
      );
      expect(deletedGroup.status).toBe(204);
      const deleted = await fetch(
        `${BASE_URL}/api/v1/orgs/${seed.org}/scim/v2/${binding.id}/Users/${user.id}`,
        { method: 'DELETE', headers: { Authorization: `Bearer ${minted.token}` } },
      );
      expect(deleted.status).toBe(204);
      await browserApi(page, 'DELETE', `/api/v1/orgs/${seed.org}/scim-bindings/${binding.id}`, z.null());
      await browserApi(page, 'DELETE', `/api/v1/instance/saml-providers/${provider.slug}`, z.null());
    }
  });

  test('creates an organisation, grants its creator admin access, and requires a fresh login', async ({ passkeyPage: page }, testInfo) => {
    const name = `Instance drill ${testInfo.project.name}`;
      await page.getByRole('button', { name: 'Open create organisation form' }).click();
      await page.getByLabel('New organisation name').fill(name);
      const responsePromise = page.waitForResponse(
        (response) =>
          response.request().method() === 'POST' &&
          new URL(response.url()).pathname === '/api/v1/orgs',
      );
      await page.getByRole('button', { name: 'Create organisation', exact: true }).click();
      const response = await responsePromise;
      expect(response.status()).toBe(201);
      const created = zOrg.parse(await response.json());

      const toast = page.locator('.toast').filter({ hasText: `Created ${name}` });
      await expect(toast).toBeInViewport();
      await expect(toast).toContainText('granted you organisation admin access');
      await expect(page.getByRole('heading', { name: 'Sign in to Hikyo', level: 1 })).toBeVisible();

      await establishSession(page);
      await page.goto(`/orgs/${created.id}/settings`);
      await expect(
        page.getByRole('heading', { name: `Organisation settings · ${name}`, level: 1 }),
      ).toBeVisible();
      await expect(page.getByLabel('Name')).toHaveValue(name);
  });

  test('configures a SAML provider, refreshes its metadata, rotates and retires SP keys, and gates login availability', async ({
    page,
    browser,
  }, testInfo) => {
    testInfo.setTimeout(90_000);
    const slug = `saml-admin-${testInfo.project.name}`;
    const entityId = `https://idp.example/${slug}`;
    const providersPanel = page.locator('#instance-saml-providers');
    const spPanel = page.locator('#instance-saml-sp-keys');

    // A fresh, unauthenticated context asks the public auth-methods endpoint
    // whether the provider is advertised for sign-in. Enabling a provider makes
    // it appear; deleting it makes it vanish, that is the login-availability
    // property this journey exists to hold.
    const advertised = async (): Promise<boolean> => {
      const anon = await browser.newContext({ storageState: { cookies: [], origins: [] } });
      try {
        const response = await anon.request.get(`${BASE_URL}/api/v1/auth/methods`);
        expect(response.ok(), await response.text()).toBe(true);
        const methods = zAuthMethods.parse(await response.json());
        return methods.providers.some((provider) => provider.slug === slug && provider.kind === 'saml');
      } finally {
        await anon.close();
      }
    };

    // Defensive: a prior failed run can leave an overlap-retiring SP key that
    // would 409 the rotate. Retire any before starting so the rotate is clean.
    for (const key of (await browserApi(page, 'GET', '/api/v1/instance/saml-sp-keys', zSamlSpKeyList)).keys) {
      if (key.state === 'retiring') {
        await browserApi(page, 'DELETE', `/api/v1/instance/saml-sp-keys/${key.fingerprint}`, z.null());
      }
    }

    try {
      // Create through the diff-and-confirm ceremony.
      await providersPanel.getByRole('button', { name: 'Configure a new SAML provider' }).click();
      await providersPanel.getByLabel('Slug (immutable; addresses this provider)').fill(slug);
      await providersPanel.getByLabel('Display name').fill(slug);
      await providersPanel
        .getByLabel('IdP entityID (byte-exact; immutable after create)')
        .fill(entityId);
      await providersPanel.getByLabel('Metadata XML').fill(samlMetadata(slug));
      await providersPanel.getByRole('button', { name: 'Preview and configure' }).click();
      await expect(providersPanel.getByText('changes trust state')).toBeVisible();
      await providersPanel.getByRole('button', { name: 'Confirm trust and configure provider' }).click();
      await expect(providersPanel.getByText(`Configured SAML provider ${slug}`)).toBeVisible();

      const providerRow = providersPanel.locator(`[data-saml-provider="${slug}"]`);
      await expect(providerRow).toContainText('enabled');
      expect(await advertised()).toBe(true);

      // Replace the pinned metadata through the same ceremony. A fresh
      // self-signed certificate makes this a real trust change.
      await providerRow.getByRole('button', { name: 'Refresh metadata' }).click();
      await providerRow.getByLabel('Replacement metadata XML').fill(samlMetadata(slug));
      await providerRow.getByRole('button', { name: 'Preview metadata change' }).click();
      await expect(providerRow.getByText('changes trust state')).toBeVisible();
      await providerRow
        .getByRole('button', { name: 'Confirm and apply the trust change' })
        .click();
      await expect(providersPanel.getByText(`Refreshed the metadata for ${slug}`)).toBeVisible();

      // Rotate the SP signing key: the old active key becomes retiring, a new
      // one is published.
      await spPanel.getByRole('button', { name: 'Rotate the active signing key' }).click();
      await expect(spPanel.getByText('Rotated the SP signing key')).toBeVisible();

      // Ordinary retirement erases the overlap-retiring key.
      const retiringRow = spPanel.locator('[data-sp-key]').filter({ hasText: 'retiring' });
      await expect(retiringRow).toHaveCount(1);
      const retiringFingerprint = await retiringRow.getAttribute('data-sp-key');
      expect(retiringFingerprint).not.toBeNull();
      await retiringRow.getByRole('button', { name: 'Retire', exact: true }).click();
      await retiringRow
        .getByLabel('Type the fingerprint to erase this retiring key')
        .fill(retiringFingerprint ?? '');
      await retiringRow.getByRole('button', { name: 'Retire key' }).click();
      await expect(spPanel.getByText('is erased and gone from SP metadata')).toBeVisible();
      await expect(spPanel.locator('[data-sp-key]').filter({ hasText: 'retiring' })).toHaveCount(0);

      // Compromise retirement erases and replaces the active key with no overlap.
      const activeRow = spPanel.locator('[data-sp-key]').filter({ hasText: 'active' });
      await expect(activeRow).toHaveCount(1);
      const compromisedFingerprint = await activeRow.getAttribute('data-sp-key');
      expect(compromisedFingerprint).not.toBeNull();
      await activeRow.getByRole('button', { name: 'Compromise-retire' }).click();
      await activeRow
        .getByLabel('Type the fingerprint to erase and replace the compromised key')
        .fill(compromisedFingerprint ?? '');
      await activeRow.getByRole('button', { name: 'Compromise-retire key' }).click();
      await expect(spPanel.getByText('minted a replacement')).toBeVisible();
      // Exactly one active key remains and it is a new one.
      const keysAfter = await browserApi(page, 'GET', '/api/v1/instance/saml-sp-keys', zSamlSpKeyList);
      expect(keysAfter.keys.filter((key) => key.state === 'active')).toHaveLength(1);
      expect(keysAfter.keys.some((key) => key.fingerprint === compromisedFingerprint)).toBe(false);

      // Remove the provider through its typed-name danger gate.
      await providerRow.getByRole('button', { name: 'Remove', exact: true }).click();
      await providerRow.getByLabel(`Type ${slug} to remove this provider`).fill(slug);
      await providerRow.getByRole('button', { name: 'Remove provider' }).click();
      await expect(providersPanel.getByText(`Removed SAML provider ${slug}`)).toBeVisible();
      expect(await advertised()).toBe(false);
    } finally {
      // The provider is gone on the happy path; sweep it if the run failed midway.
      await browserApi(page, 'DELETE', `/api/v1/instance/saml-providers/${slug}`, z.null()).catch(
        () => undefined,
      );
    }
  });

  test('answers a password-only session with the second-factor state, not an empty list', async ({
    browser,
  }) => {
    // Its own context, with an EMPTY jar: `browser.newContext()` still picks
    // up the describe's `storageState`, and a live session cookie on a login
    // POST is refused 401 by the CSRF gate before the handler ever sees it , 
    // which looks exactly like a wrong password. This session is deliberately
    // weaker than the suite's, and it must not replace it.
    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    try {
      const page = await context.newPage();
      await page.goto('/');
      await establishSession(page, false);
      await page.goto('/instance');

      const panels = [
        ['#instance-orgs', 'needs a second factor', 'organisations on this instance'],
        ['#instance-retention', 'requires a second factor', 'Payload pruning'],
        ['#instance-settings', 'requires a second factor', 'Maximum finite lifetime'],
        ['#instance-oidc', 'needs a second factor', '+ add identity provider'],
        ['#instance-saml-providers', 'needs a second factor', 'No SAML providers are configured'],
        ['#instance-saml-sp-keys', 'needs a second factor', 'signs every AuthnRequest'],
        ['#instance-federation', 'needs a second factor', 'Configure issuer'],
      ] as const;
      for (const [selector, refusalText, forbiddenText] of panels) {
        const panel = page.locator(selector);
        await expect(panel.getByRole('alert')).toContainText(refusalText);
        await expect(panel).not.toContainText(forbiddenText);
      }
      // The members pair answers the same session the same way (#567).
      await page.goto('/instance/members');
      // Scoped to the page: the step-up banner above the well is an alert too.
      await expect(page.locator('.page--members').getByRole('alert')).toContainText(
        'Instance grants require a second factor',
      );
      await expect(page.locator('#members-list')).not.toContainText('No instance-scope grants');
    } finally {
      await context.close();
    }
  });

  /** Open a fresh, unauthenticated /login and settle it on a known button. */
  async function withFreshLogin(
    browser: Browser,
    body: (page: Page) => Promise<void>,
  ): Promise<void> {
    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    try {
      const fresh = await context.newPage();
      await fresh.goto('/login');
      // The fixture's own provider is always advertised, waiting on it is the
      // settle point that makes a later "absent" assertion trustworthy.
      await expect(
        fresh.getByRole('button', { name: `Continue with ${OIDC_PROVIDER.displayName}` }),
      ).toBeVisible();
      await body(fresh);
    } finally {
      await context.close();
    }
  }

  test('configures a provider, advertises it on login, then disables and deletes it', async ({
    page,
    browser,
  }, testInfo) => {
    testInfo.setTimeout(60_000);
    try {
    const panel = page.locator('#instance-oidc');
    await expect(panel.getByRole('heading', { name: 'Identity providers' })).toBeVisible();
    // The fixture's linked provider is listed alongside any this flow adds.
    await expect(panel).toContainText(OIDC_PROVIDER.displayName);

    // Configure a new provider entirely through the browser. Its issuer is the
    // second fixture IdP, so discovery succeeds server-side at create time.
    await panel.getByRole('button', { name: '+ add identity provider' }).click();
    const editor = panel.locator('.oidc-editor');
    await editor.getByLabel('Slug').fill(WEBUI_OIDC.slug);
    await editor.getByLabel('Display name').fill(WEBUI_OIDC.displayName);
    await editor.getByLabel('Issuer URL').fill(WEBUI_OIDC.issuer);
    await editor.getByLabel('Client ID').fill('e2e-webui-client');
    await editor.getByLabel('Client secret').fill('e2e-webui-secret');
    await editor.getByLabel('Scopes').fill('openid');
    await editor.getByRole('button', { name: 'Configure provider' }).click();

    const row = panel.locator('.settings-row').filter({ hasText: WEBUI_OIDC.displayName });
    await expect(row).toContainText('enabled');
    await expect(
      page.locator('.notice').filter({ hasText: 'advertised on the sign-in page' }),
    ).toBeVisible();

    // Advertised login availability, verified in a fresh unauthenticated context.
    await withFreshLogin(browser, async (login) => {
      await expect(
        login.getByRole('button', { name: `Continue with ${WEBUI_OIDC.displayName}` }),
      ).toBeVisible();
    });

    // Disable it: a reconfigure with the write-only secret re-entered and the
    // Enabled box cleared. The secret is required even to disable.
    await row.getByRole('button', { name: `Reconfigure ${WEBUI_OIDC.displayName}` }).click();
    const reconfigure = panel.locator('.oidc-editor');
    await reconfigure.getByLabel('Enabled (advertised on the sign-in page)').uncheck();
    await expect(reconfigure.getByRole('alert')).toContainText(
      'Local password and second-factor sign-in is unaffected',
    );
    await reconfigure.getByRole('button', { name: 'Save provider' }).click();
    // The panel now shows two role=alert nodes at once (the disable-consequence
    // note and the field refusal), so scope the assertion to the panel text.
    await expect(panel).toContainText('entered on every save');
    await reconfigure.getByLabel('Client secret').fill('e2e-webui-secret');
    await reconfigure.getByRole('button', { name: 'Save provider' }).click();
    await expect(
      panel.locator('.settings-row').filter({ hasText: WEBUI_OIDC.displayName }),
    ).toContainText('disabled');

    // No longer advertised.
    await withFreshLogin(browser, async (login) => {
      await expect(
        login.getByRole('button', { name: `Continue with ${WEBUI_OIDC.displayName}` }),
      ).toHaveCount(0);
    });

    // Delete it, gated on the immutable slug, with the consequence stated.
    const disabledRow = panel.locator('.settings-row').filter({ hasText: WEBUI_OIDC.displayName });
    await disabledRow.getByRole('button', { name: `Delete ${WEBUI_OIDC.displayName}` }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('cannot be undone');
    // Keyboard operable: Escape closes the native dialog without deleting.
    await dialog.getByLabel('Type the provider slug to confirm').press('Escape');
    await expect(page.getByRole('dialog')).toBeHidden();
    await expect(
      panel.locator('.settings-row').filter({ hasText: WEBUI_OIDC.displayName }),
    ).toHaveCount(1);

    await disabledRow.getByRole('button', { name: `Delete ${WEBUI_OIDC.displayName}` }).click();
    const confirm = page.getByRole('dialog');
    await confirm.getByLabel('Type the provider slug to confirm').fill(WEBUI_OIDC.slug);
    await confirm.getByRole('button', { name: 'Delete provider' }).click();
    await expect(
      panel.locator('.settings-row').filter({ hasText: WEBUI_OIDC.displayName }),
    ).toHaveCount(0);
    } finally {
      // Failure-safe cleanup: a mid-test failure must not leave the throwaway
      // provider behind to collide (by slug or enabled issuer) on a later run.
      await browserApi(page, 'DELETE', `/api/v1/instance/oidc-providers/${WEBUI_OIDC.slug}`, z.null()).catch(
        () => undefined,
      );
    }
  });

  test('keeps the issuer immutable and refuses a secretless reconfigure before any request', async ({
    page,
  }) => {
    const panel = page.locator('#instance-oidc');
    let putSent = false;
    await page.route('**/api/v1/instance/oidc-providers/**', async (route) => {
      if (route.request().method() === 'PUT') {
        putSent = true;
      }
      await route.continue();
    });

    await panel
      .locator('.settings-row')
      .filter({ hasText: OIDC_PROVIDER.displayName })
      .getByRole('button', { name: `Reconfigure ${OIDC_PROVIDER.displayName}` })
      .click();
    const editor = panel.locator('.oidc-editor');
    // The issuer field is disabled on reconfigure, so a changed issuer is not
    // even expressible in the UI, the field-error path for it is unit-tested.
    // Here the blank-secret guard refuses the save with no request reaching the
    // server, which is the write-only-secret contract enforced client-side.
    await expect(editor.getByLabel('Issuer URL')).toBeDisabled();
    await editor.getByLabel('Client secret').fill('');
    await editor.getByRole('button', { name: 'Save provider' }).click();
    await expect(panel.getByRole('alert')).toContainText('entered on every save');
    expect(putSent).toBe(false);
  });

  test('surfaces a stale-state conflict as a reload prompt', async ({ page }) => {
    const panel = page.locator('#instance-oidc');
    await page.route('**/api/v1/instance/oidc-providers/**', async (route) => {
      if (route.request().method() !== 'PUT') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'conflict' } }),
      });
    });

    await panel
      .locator('.settings-row')
      .filter({ hasText: OIDC_PROVIDER.displayName })
      .getByRole('button', { name: `Reconfigure ${OIDC_PROVIDER.displayName}` })
      .click();
    const editor = panel.locator('.oidc-editor');
    await editor.getByLabel('Client secret').fill('whatever-secret');
    await editor.getByRole('button', { name: 'Save provider' }).click();
    await expect(panel.getByRole('alert')).toContainText('changed underneath you');
  });

  test('configures, edits and deletes a federation issuer, and fails closed while one is bound', async ({
    page,
  }) => {
    const federation = page.locator('#instance-federation');

    // The seeded issuer is listed with the census that fails a delete closed:
    // web-api is bound to it, so at least one binding names it.
    await expect(federation).toContainText(seed.machine.issuer);
    const seedRow = federation.locator('li.settings-row', { hasText: seed.machine.issuer });
    await expect(seedRow).toContainText('binding');

    // Configure a NEW, never-bound issuer. Discovery mode needs no JWKS
    // document, and the create runs no outbound fetch, so an unreachable host
    // is fine here. The host carries the project name so the desktop and mobile
    // runs never name the same issuer.
    const newIssuer = `https://e2e-federation-${test.info().project.name}.example.org`;
    await federation.getByRole('button', { name: 'Configure issuer' }).click();
    const createForm = federation.locator('fieldset', {
      hasText: 'Configure a federation issuer',
    });
    await createForm.getByLabel('Issuer (https URL, matched byte-for-byte)').fill(newIssuer);
    await createForm.getByLabel('Platform type').selectOption('github-actions');
    await createForm.getByLabel('Refused audiences, one per line').fill('example-owner');
    await createForm.getByRole('button', { name: 'Configure issuer' }).click();

    await expect(federation.locator('.notice')).toContainText('Configured');
    const newRow = federation.locator('li.settings-row', { hasText: newIssuer });
    await expect(newRow).toContainText('no bindings name it');

    // Edit the mutable half: the issuer string and platform type are shown
    // read-only, and only the refused audiences move.
    await newRow.getByRole('button', { name: 'Edit' }).click();
    const editForm = federation.locator('fieldset', { hasText: `Edit ${newIssuer}` });
    await expect(editForm.getByRole('textbox', { name: 'Issuer' })).toHaveCount(0);
    await editForm.getByLabel('Refused audiences, one per line').fill('example-owner\nsecond-owner');
    await editForm.getByRole('button', { name: 'Save issuer' }).click();
    await expect(federation.locator('.notice')).toContainText('Updated');

    // The seeded issuer FAILS CLOSED: a binding names it, so the confirmation
    // explains deletion is permanently unavailable and offers NO destructive
    // action. A revoked binding would still count, so this can never reach zero.
    await seedRow.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(seedRow).toContainText('cannot be deleted');
    await expect(seedRow.getByRole('button', { name: 'Delete issuer' })).toHaveCount(0);
    await seedRow.getByRole('button', { name: 'Close' }).click();
    await expect(federation).toContainText(seed.machine.issuer);

    // The never-bound issuer deletes cleanly, the destructive action is
    // present only because its census is zero.
    await newRow.getByRole('button', { name: 'Delete', exact: true }).click();
    await newRow.getByRole('button', { name: 'Delete issuer' }).click();
    await expect(federation.locator('.notice')).toContainText('Deleted');
    await expect(federation.locator('li.settings-row', { hasText: newIssuer })).toHaveCount(0);
  });

  for (const scheme of ['dark', 'light'] as const) {
    test(`meets the pinned assertion set on instance administration (${scheme})`, async ({
      page,
    }) => {
      await page.emulateMedia({ colorScheme: scheme });
      try {
        const heading = page.getByRole('heading', {
          name: 'Instance settings',
          level: 1,
        });
        const well = page.locator('.panel').first();
        const create = page.getByRole('button', { name: 'Open create organisation form' });
        const tag = page.locator('.settings-tag').first();
        const cli = page.locator('.instance-cli').first();

        await expectPinnedAssertionSet(page, {
          flow: 'instance-admin',
          surface: 'instance-admin',
          theme: scheme,
          text: [heading, page.locator('.settings-row__detail').first(), page.locator('.page__lede')],
          radii: [
            [well, 'container'],
            [create, 'control'],
            [tag, 'badge'],
          ],
          fonts: [
            [heading, 'ui'],
            [cli, 'mono'],
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

  for (const scheme of ['dark', 'light'] as const) {
    test(`meets the pinned assertion set on instance configuration (${scheme})`, async ({
      page,
    }, testInfo) => {
      await page.emulateMedia({ colorScheme: scheme });
      try {
        await page.goto('/instance/config');
        const heading = page.getByRole('heading', { name: 'Hikyo configuration', level: 1 });
        const owner = page.locator('#configuration-owner');
        const apply = page.getByRole('button', { name: 'Apply selected revision', exact: true });
        const revision = page.getByLabel('Published revision to apply or test');
        const edit = page.getByRole('link', { name: 'Edit configuration project', exact: true });
        const rowDensity = testInfo.project.name === 'mobile' ? '--touch' : '--row';
        await expect(apply).toBeEnabled();
        await expectPinnedAssertionSet(page, {
          flow: 'instance-admin',
          surface: 'instance-config',
          theme: scheme,
          text: [heading, page.locator('.page__lede'), owner.locator('.settings-row__detail')],
          radii: [[owner, 'container'], [apply, 'control'], [revision, 'control']],
          fonts: [[heading, 'ui'], [owner.locator('code'), 'mono']],
          colours: [
            [heading, 'color', '--tx'],
            [owner, 'backgroundColor', '--bg-panel'],
            [owner, 'borderTopColor', '--panel-line'],
          ],
          hairlines: [owner],
          density: [[apply, rowDensity], [edit, rowDensity]],
        });
      } finally {
        await page.emulateMedia({ colorScheme: null });
      }
    });
  }

  // The instance members surface (#567) rides this spec: the closure demands a
  // pinned run per theme, and both themes are half the palette each.
  for (const scheme of ['dark', 'light'] as const) {
    test(`meets the pinned assertion set on instance members (${scheme})`, async ({
      page,
    }, testInfo) => {
      await page.emulateMedia({ colorScheme: scheme });
      try {
        await page.goto('/instance/members');
        const rowDensity = testInfo.project.name === 'mobile' ? '--touch' : '--row';
        const heading = page.getByRole('heading', { name: 'Members · Instance', level: 1 });
        const well = page.locator('.panel').first();
        const jump = page.getByRole('link', { name: 'Who can…?' });
        const chip = page.locator('.chip').first();
        const newGrant = page.getByRole('button', { name: 'New grant' });

        await expectPinnedAssertionSet(page, {
          flow: 'instance-admin',
          surface: 'instance-members',
          theme: scheme,
          text: [heading, page.locator('.inspect__answer'), page.locator('.capability__name').first()],
          radii: [
            [well, 'container'],
            [newGrant, 'control'],
            [chip, 'badge'],
          ],
          fonts: [
            [heading, 'ui'],
            [page.locator('.capability__name').first(), 'mono'],
          ],
          colours: [
            [heading, 'color', '--tx'],
            [well, 'backgroundColor', '--bg-panel'],
            [well, 'borderTopColor', '--panel-line'],
          ],
          hairlines: [well],
          density: [
            [newGrant, rowDensity],
            [jump, rowDensity],
          ],
        });
      } finally {
        await page.emulateMedia({ colorScheme: null });
      }
    });
  }
});
