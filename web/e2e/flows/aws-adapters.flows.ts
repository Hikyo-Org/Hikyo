import { expect, type Page, type Response, type TestInfo } from '@playwright/test';
import { zAdapter, zAdapterConnection, zAdapterJob, zAdapterList, zAdapterPlan, zAdapterTargetDetail, zError } from '@hikyo/zod';
import { z } from 'zod';

import { BrowserApiError, browserApi } from '../fixtures/api.ts';
import { nextTotpCode, readSeed } from '../fixtures/instance.ts';
import { test } from '../fixtures/passkey.ts';

/**
 * SOURCE DRAFT, NOT EXECUTED: #158 browser product-surface coverage.
 *
 * The flow fixture selects the development provider. For AWS it runs the real
 * awssm.Module over an in-memory, metadata-aware awssm.API. Browser, API,
 * ceremonies, persisted configuration, ownership and module decisions are
 * subjects here. Descriptor syntax is parsed, but authentication, STS, IAM,
 * egress and remote AWS behavior remain simulated. No AWS HTTP client runs.
 *
 * These tests deliberately assert lifecycle acceptance and persisted pause
 * state, not real AWS convergence, retention, or completed remote pruning.
 * The invented .example endpoints, account and access fields have no cloud
 * authority. Never point this draft at a real-provider fixture unchanged.
 *
 * Registered by machine-access.spec.ts so the base-branch CI spec list loads
 * these tests for desktop and mobile. No additional registry/pinned-surface
 * claim is made; the existing adapters flow retains that responsibility.
 */
const seed = readSeed();
const PROJECT_API = `/api/v1/orgs/${seed.org}/projects/${seed.project}`;
const ADAPTERS_API = `${PROJECT_API}/adapters`;
const ADAPTERS_PATH = `/orgs/${seed.org}/projects/${seed.project}/adapters`;
function seededSecretKey(): string {
  const key = seed.secrets[0];
  if (key === undefined) throw new Error('AWS browser draft requires a seeded secret key');
  return key;
}
const KEY = seededSecretKey();
const ACCOUNT = '123456789012';
const ACCESS_KEY_ID = 'AKIAHIKYOTEST0000001';
const SECRET_CANARY = 'not-an-aws-secret-browser-canary';
const UNOWNED_FIXTURE = 'e2e-aws-unowned';
const PREFIX = 'AWS_UI_';

function fixtureOrigin(kind: string, purpose: string, testInfo: TestInfo): string {
  // The current setup boots fresh instances for each viewport invocation.
  // Also isolate repeat/retry attempts, whose retained resources and one-time
  // external seed must never become the next attempt's initial state.
  const identity = `${testInfo.project.name}-${testInfo.workerIndex}-${testInfo.repeatEachIndex}-${testInfo.retry}`.toLowerCase().replace(/[^a-z0-9-]/g, '-');
  return `https://aws-${kind}-${purpose}-${identity}.example`;
}

function expectRedacted(raw: string): void {
  // Inspect the unparsed wire body: response schemas may strip extra fields,
  // which would otherwise hide a server accidentally returning a credential.
  expect(raw).not.toContain(SECRET_CANARY);
  expect(raw).not.toContain(UNOWNED_FIXTURE);
  expect(raw).not.toContain('external-fixture-value');
  expect(raw).not.toContain(ACCESS_KEY_ID);
  expect(raw).not.toMatch(/"(?:credential|secret_access_key|value)"\s*:/);
}

async function authorise(page: Page): Promise<void> {
  const dialog = page.getByRole('dialog', { name: 'Confirm it is you' });
  await expect(dialog).toBeVisible();
  const confirm = dialog.getByRole('button', { name: 'Authorise' });
  await expect(confirm).toBeEnabled();
  const code = dialog.getByLabel('Authenticator code');
  if (await code.isVisible()) await code.fill(await nextTotpCode());
  await confirm.click();
  await expect(dialog).toBeHidden();
}

function createdResponse(page: Page): Promise<Response> {
  return page.waitForResponse((response) =>
    response.request().method() === 'POST' &&
    new URL(response.url()).pathname === ADAPTERS_API,
  );
}

async function fillAwsForm(page: Page, kind: string, origin: string, destination: string, secretValue = SECRET_CANARY): Promise<void> {
  await page.getByRole('button', { name: 'Add adapter' }).click();
  const form = page.getByRole('complementary', { name: 'New adapter' });
  await form.getByLabel('Provider', { exact: true }).selectOption('aws-secrets-manager');
  await form.getByLabel('Secrets Manager endpoint').fill(origin);
  await form.getByLabel('Authentication', { exact: true }).selectOption('static');
  await form.getByLabel('Access key id', { exact: true }).fill(ACCESS_KEY_ID);
  const secret = form.getByLabel('Secret access key', { exact: true });
  await expect(secret).toHaveAttribute('type', 'password');
  await secret.fill(secretValue);
  await form.getByLabel('Region', { exact: true }).fill('eu-west-1');
  await form.getByLabel('Environment', { exact: true }).selectOption(seed.prod);
  await form.getByLabel('Destination kind', { exact: true }).selectOption(kind);
  await form.getByLabel('AWS account id', { exact: true }).fill(ACCOUNT);
  await form.getByLabel(kind === 'json-object' ? 'Secret name' : 'Path prefix', { exact: true }).fill(destination);
  await form.getByLabel('KMS key (optional)', { exact: true }).fill('alias/hikyo-browser-fixture');
  await form.getByLabel('Name prefix', { exact: true }).fill(PREFIX);
  await form.getByRole('checkbox', { name: KEY, exact: true }).check();
}

export function registerAwsAdapterFlows(): void {
  test.describe('AWS adapter product surface with the development fake', () => {
    test.describe.configure({ mode: 'serial' });
    test.use({ sharedSession: true });

    for (const scenario of [
      { kind: 'json-object', destination: 'browser/config', decision: 'Retain', keepRemote: true },
      { kind: 'per-key', destination: 'browser/keys/', decision: 'Prune', keepRemote: false },
    ]) {
      test(`${scenario.kind}: creates, inspects, pauses, resumes and requests ${scenario.decision.toLowerCase()}`, async ({ passkeyPage: page }, testInfo) => {
        const origin = fixtureOrigin(scenario.kind, 'lifecycle', testInfo);
        let adapterId = '';
        try {
          await page.goto(ADAPTERS_PATH);
          await fillAwsForm(page, scenario.kind, origin, scenario.destination);
          const creation = createdResponse(page);
          await page.getByRole('complementary', { name: 'New adapter' }).getByRole('button', { name: 'Save', exact: true }).click();
          await authorise(page);
          const response = await creation;
          expect(response.status()).toBe(201);
          const raw = await response.text();
          // Capture identity first so cleanup also runs if redaction fails.
          const created = zAdapter.parse(JSON.parse(raw));
          adapterId = created.id;
          expectRedacted(raw);
          expect(created.provider).toBe('aws-secrets-manager');
          expect(created.credential_present).toBe(true);
          expect(created.targets).toHaveLength(1);
          const target = created.targets[0];
          if (target === undefined) throw new Error('AWS adapter has no first target');
          expect(target).toMatchObject({
            environment_id: seed.prod,
            destination_kind: scenario.kind,
            destination_owner: ACCOUNT,
            destination_name: scenario.destination,
            destination_environment: 'alias/hikyo-browser-fixture',
            name_prefix: PREFIX,
          });

          await expect(page.getByRole('complementary', { name: 'New adapter' })).toBeHidden();
          await page.reload();
          const panel = page.getByRole('region', { name: `Adapter ${origin}`, exact: true });
          await expect(panel.getByRole('heading', { name: 'AWS Secrets Manager' })).toBeVisible();
          await expect(panel.getByText('credential set', { exact: true })).toBeVisible();
          await expect(page.getByLabel('Secret access key', { exact: true })).toHaveCount(0);
          expectRedacted(await page.locator('body').innerText());

          for (const path of [ADAPTERS_API, `${ADAPTERS_API}/${adapterId}`, `${PROJECT_API}/adapter-targets/${target.id}`]) {
            const inspected = await page.request.get(path);
            expect(inspected.status()).toBe(200);
            const wire = await inspected.text();
            expectRedacted(wire);
            if (path === ADAPTERS_API) zAdapterList.parse(JSON.parse(wire));
            else if (path === `${ADAPTERS_API}/${adapterId}`) zAdapter.parse(JSON.parse(wire));
            else zAdapterTargetDetail.parse(JSON.parse(wire));
          }

          // The dev seam parses descriptors without checking node policy or
          // STS. Assert form serialization and write-only replacement only.
          for (const mode of ['ambient', 'assume-role', 'web-identity', 'static']) {
            await panel.getByRole('button', { name: 'Replace credential', exact: true }).click();
            const replacement = page.getByRole('complementary', { name: 'Replace credential', exact: true });
            await replacement.getByLabel('Authentication', { exact: true }).selectOption(mode);
            await replacement.getByLabel('Region', { exact: true }).fill('eu-west-1');
            if (mode === 'static') {
              await replacement.getByLabel('Access key id', { exact: true }).fill(ACCESS_KEY_ID);
              const secret = replacement.getByLabel('Secret access key', { exact: true });
              await expect(secret).toHaveValue('');
              await expect(secret).toHaveAttribute('type', 'password');
              await secret.fill(SECRET_CANARY);
            } else {
              await expect(replacement.getByText(/The node operator must allow it/)).toBeVisible();
              await expect(replacement.getByLabel('Secret access key', { exact: true })).toHaveCount(0);
            }
            if (mode === 'assume-role' || mode === 'web-identity') {
              await replacement.getByLabel('Role ARN', { exact: true }).fill(`arn:aws:iam::${ACCOUNT}:role/browser-fixture`);
              await replacement.getByLabel('Session duration (seconds)', { exact: true }).fill('900');
            }
            if (mode === 'assume-role') await replacement.getByLabel('External id', { exact: true }).fill('browser-fixture');
            const replacing = page.waitForResponse((result) =>
              result.request().method() === 'PUT' &&
              new URL(result.url()).pathname === `${ADAPTERS_API}/${adapterId}/credential`,
            );
            await replacement.getByRole('button', { name: 'Replace', exact: true }).click();
            await authorise(page);
            const replaced = await replacing;
            expect(replaced.status()).toBe(204);
            expect(await replaced.text()).toBe('');
            const request = z.object({ credential: z.string() }).strict().parse(replaced.request().postDataJSON());
            const descriptor = z.object({
              mode: z.string(), region: z.string(), role_arn: z.string().optional(),
              external_id: z.string().optional(), session_seconds: z.number().optional(),
              access_key_id: z.string().optional(), secret_access_key: z.string().optional(),
            }).strict().parse(JSON.parse(request.credential));
            expect(descriptor.mode).toBe(mode);
            expect(descriptor.region).toBe('eu-west-1');
            expect(descriptor.secret_access_key).toBe(mode === 'static' ? SECRET_CANARY : undefined);
            expect(descriptor.access_key_id).toBe(mode === 'static' ? ACCESS_KEY_ID : undefined);
            expect(descriptor.external_id).toBe(mode === 'assume-role' ? 'browser-fixture' : undefined);
            expect(descriptor.role_arn).toBe(mode === 'assume-role' || mode === 'web-identity' ? `arn:aws:iam::${ACCOUNT}:role/browser-fixture` : undefined);
            expect(descriptor.session_seconds).toBe(mode === 'assume-role' || mode === 'web-identity' ? 900 : undefined);
            await expect(replacement).toBeHidden();
            expectRedacted(await page.locator('body').innerText());
          }

          await panel.locator('.adapters__target').click();
          const detail = page.getByRole('complementary', { name: 'Target detail' });
          await expect(detail.locator('.adapters__facts')).toContainText(scenario.destination);
          await expect(detail.getByRole('list', { name: 'Member keys' })).toContainText(KEY);
          await expect(detail.locator('.adapters__workflow')).toContainText(`${PREFIX}${KEY}`);

          // The product deliberately delegates destination moves to the CLI.
          // Prove AWS routing cannot accidentally change through Edit keys.
          await detail.getByRole('button', { name: 'Edit keys', exact: true }).click();
          await expect(detail).toContainText('move it with the CLI move ceremony');
          for (const label of ['Environment', 'Destination kind', 'AWS account id', 'KMS key (optional)', scenario.kind === 'json-object' ? 'Secret name' : 'Path prefix']) {
            await expect(detail.getByLabel(label, { exact: true })).toBeDisabled();
          }
          await detail.getByRole('button', { name: 'Stop editing', exact: true }).click();

          for (const operation of [
            { button: 'Plan', suffix: 'plan', schema: zAdapterPlan },
            { button: 'Test connection', suffix: 'test', schema: zAdapterConnection },
          ]) {
            const result = page.waitForResponse((response) =>
              response.request().method() === 'POST' &&
              new URL(response.url()).pathname === `${PROJECT_API}/adapter-targets/${target.id}/${operation.suffix}`,
            );
            await detail.getByRole('button', { name: operation.button, exact: true }).click();
            const response = await result;
            expect(response.status()).toBe(200);
            const wire = await response.text();
            expectRedacted(wire);
            operation.schema.parse(JSON.parse(wire));
            await expect(page.getByRole('dialog', { name: 'Confirm it is you' })).toBeHidden();
          }
          await expect(detail.getByRole('region', { name: 'Plan', exact: true })).toBeVisible();
          expectRedacted(await detail.innerText());

          await detail.getByRole('button', { name: 'Pause', exact: true }).click();
          await expect(detail.locator('.adapters__health')).toHaveText(/Paused/);
          await expect(detail.getByRole('button', { name: 'Resync', exact: true })).toBeDisabled();
          await expect(page.getByRole('dialog', { name: 'Confirm it is you' })).toBeHidden();
          await page.reload();
          await expect(detail.locator('.adapters__health')).toHaveText(/Paused/);
          const paused = await browserApi(page, 'GET', `${PROJECT_API}/adapter-targets/${target.id}`, zAdapterTargetDetail);
          expect(paused.target.paused_at).not.toBeNull();

          await detail.getByRole('button', { name: 'Resume', exact: true }).click();
          await authorise(page);
          await expect(page.locator('.adapters__done')).toContainText(/Catching up to revision \d+/);
          await expect(detail.getByRole('button', { name: 'Pause', exact: true })).toBeVisible();
          const resumed = await browserApi(page, 'GET', `${PROJECT_API}/adapter-targets/${target.id}`, zAdapterTargetDetail);
          expect(resumed.target.paused_at).toBeNull();
          // Fence the fake worker before checking teardown choices. No Healthy
          // assertion here could establish AWS destination behavior.
          await detail.getByRole('button', { name: 'Pause', exact: true }).click();
          await expect(detail.locator('.adapters__health')).toHaveText(/Paused/);

          await detail.getByRole('button', { name: 'Remove', exact: true }).click();
          const remove = page.getByRole('dialog', { name: /^Remove target / });
          const confirm = remove.getByRole('button', { name: 'Remove target', exact: true });
          await expect(confirm).toBeDisabled();
          await expect(remove.getByRole('radio', { name: /^Retain/ })).not.toBeChecked();
          await expect(remove.getByRole('radio', { name: /^Prune/ })).not.toBeChecked();
          await remove.getByRole('radio', { name: new RegExp(`^${scenario.decision}`) }).check();
          const teardown = page.waitForResponse((result) =>
            result.request().method() === 'DELETE' &&
            new URL(result.url()).pathname === `${PROJECT_API}/adapter-targets/${target.id}`,
          );
          await confirm.click();
          const removed = await teardown;
          expect(removed.status()).toBe(200);
          expect(new URL(removed.url()).searchParams.get('keep_remote')).toBe(String(scenario.keepRemote));
          await expect(remove).toBeHidden();
          await expect(panel.locator('.adapters__target')).toHaveCount(0);

          // Retain has no asynchronous scrub requiring this credential.
          if (scenario.keepRemote) {
            await panel.getByRole('button', { name: 'Revoke credential', exact: true }).click();
            const revoke = page.getByRole('dialog', { name: /Revoke credential for/ });
            await expect(revoke).toContainText('remote scrub may then be impossible');
            const revoking = page.waitForResponse((result) =>
              result.request().method() === 'DELETE' &&
              new URL(result.url()).pathname === `${ADAPTERS_API}/${adapterId}/credential`,
            );
            await revoke.getByRole('button', { name: 'Revoke credential', exact: true }).click();
            const revoked = await revoking;
            expect(revoked.status()).toBe(204);
            expect(await revoked.text()).toBe('');
            await expect(panel.getByText('credential absent', { exact: true })).toBeVisible();
            await expect(panel.getByRole('button', { name: 'Revoke credential', exact: true })).toBeDisabled();
            await expect(page.getByRole('dialog', { name: 'Confirm it is you' })).toBeHidden();
            await page.reload();
            const afterRevoke = await browserApi(page, 'GET', `${ADAPTERS_API}/${adapterId}`, zAdapter);
            expect(afterRevoke.credential_present).toBe(false);
            expectRedacted(await page.locator('body').innerText());
          }
        } finally {
          if (adapterId !== '') {
            try {
              await browserApi(page, 'DELETE', `${ADAPTERS_API}/${adapterId}?keep_remote=true`, z.json());
            } catch (error) {
              if (!(error instanceof BrowserApiError && error.status === 404)) throw error;
            }
          }
        }
      });
    }

    for (const kind of ['json-object', 'per-key']) {
      test(`${kind}: adopts exactly the reviewed unowned resource through the ceremony`, async ({ passkeyPage: page }, testInfo) => {
        // This explicit dev-only credential marker seeds one foreign resource
        // with no Hikyo ownership tag, once in this endpoint's namespace.
        // Actual module.Plan derives its resource/version witness; Sync must
        // validate it and perform the real journal protocol to converge.
        const origin = fixtureOrigin(kind, 'adoption', testInfo);
        const destination = kind === 'json-object' ? 'browser/adopt-config' : 'browser/adopt-keys/';
        let adapterId = '';
        try {
          await page.goto(ADAPTERS_PATH);
          await fillAwsForm(page, kind, origin, destination, UNOWNED_FIXTURE);
          const creation = createdResponse(page);
          await page.getByRole('complementary', { name: 'New adapter' }).getByRole('button', { name: 'Save', exact: true }).click();
          await authorise(page);
          const createdResponseBody = await creation;
          expect(createdResponseBody.status()).toBe(201);
          const wire = await createdResponseBody.text();
          const created = zAdapter.parse(JSON.parse(wire));
          adapterId = created.id;
          expectRedacted(wire);
          const target = created.targets[0];
          if (target === undefined) throw new Error('AWS adoption draft requires a first target');
          const panel = page.getByRole('region', { name: `Adapter ${origin}`, exact: true });
          await panel.locator('.adapters__target').click();
          const detail = page.getByRole('complementary', { name: 'Target detail' });
          await detail.getByRole('button', { name: 'Plan', exact: true }).click();
          const conflicts = detail.getByRole('region', { name: 'Conflicts', exact: true });
          await expect(conflicts).toBeVisible();
          const before = await browserApi(page, 'GET', `${PROJECT_API}/adapter-targets/${target.id}`, zAdapterTargetDetail);
          expect(before.conflicts).toHaveLength(1);
          const artifact = before.conflicts[0];
          if (artifact === undefined) throw new Error('AWS adoption draft requires a conflict artifact');
          const effectiveName = kind === 'json-object' ? destination : `${destination}${PREFIX}${KEY}`;
          const selected = artifact.entries.find((entry) => entry.effective_name === effectiveName && entry.surface === 'secret');
          if (selected === undefined) throw new Error('AWS conflict did not preserve the configured destination name');
          const list = conflicts.getByRole('list', { name: `Conflict artifact ${artifact.id}` });
          const adopt = conflicts.getByRole('button', { name: 'Adopt 0 selected', exact: true });
          await expect(adopt).toBeDisabled();
          const choice = list.getByRole('checkbox', { name: `${selected.surface}:${selected.effective_name}`, exact: true });
          await expect(choice).not.toBeChecked();
          await choice.check();
          const adoption = page.waitForResponse((result) =>
            result.request().method() === 'POST' &&
            new URL(result.url()).pathname === `${PROJECT_API}/adapter-targets/${target.id}/adoptions`,
          );
          await conflicts.getByRole('button', { name: 'Adopt 1 selected', exact: true }).click();
          await authorise(page);
          const accepted = await adoption;
          expect(accepted.status()).toBe(202);
          const adoptionWire = await accepted.text();
          expectRedacted(adoptionWire);
          const job = zAdapterJob.parse(JSON.parse(adoptionWire));
          expect(job.generation).toBe(before.target.generation + 1n);
          const request = z.object({
            artifact_id: z.string(), target_generation: z.number(),
            destination_id: z.number(), repository_id: z.number(),
            entries: z.array(z.object({ surface: z.string(), effective_name: z.string() }).strict()),
          }).strict().parse(accepted.request().postDataJSON());
          expect(request.artifact_id).toBe(artifact.id);
          expect(BigInt(request.target_generation)).toBe(before.target.generation);
          expect(BigInt(request.destination_id)).toBe(artifact.destination_id);
          expect(BigInt(request.repository_id)).toBe(artifact.repository_id);
          expect(request.entries).toEqual([selected]);
          await expect(page.locator('.adapters__done')).toContainText('Adopted. A converge is queued.');
          await expect(detail.locator('.adapters__health')).toHaveText(/Healthy/, { timeout: 20_000 });
          const after = await browserApi(page, 'GET', `${PROJECT_API}/adapter-targets/${target.id}`, zAdapterTargetDetail);
          expect(after.target.generation).toBe(job.generation);
          expect(after.conflicts).toHaveLength(0);
          await page.reload();
          await expect(detail.locator('.adapters__health')).toHaveText(/Healthy/);
          await expect(detail.getByRole('region', { name: 'Conflicts', exact: true })).toHaveCount(0);
          // Reusing consumed consent must fail through the real server even
          // when the exact old names and artifact are replayed.
          try {
            await browserApi(page, 'POST', `${PROJECT_API}/adapter-targets/${target.id}/adoptions`, zAdapterJob, request);
            throw new Error('Consumed AWS adoption artifact was accepted twice');
          } catch (error) {
            if (!(error instanceof BrowserApiError && error.status === 409)) throw error;
            expectRedacted(error.body);
            expect(zError.parse(JSON.parse(error.body)).error.code).toBe('conflict');
          }
          await detail.getByRole('button', { name: 'Pause', exact: true }).click();
          await expect(detail.locator('.adapters__health')).toHaveText(/Paused/);
          expectRedacted(await detail.innerText());
        } finally {
          if (adapterId !== '') await browserApi(page, 'DELETE', `${ADAPTERS_API}/${adapterId}?keep_remote=true`, z.json());
        }
      });
    }

    test('shows a safe server refusal for an invalid per-key path without creating an adapter', async ({ passkeyPage: page }) => {
      // Repository routing validation and the actual module run against the
      // dev-only peer; this refusal does not establish cloud reachability.
      const origin = 'https://aws-invalid-path-browser.example';
      await page.goto(ADAPTERS_PATH);
      await fillAwsForm(page, 'per-key', origin, 'missing-trailing-slash');
      const creation = createdResponse(page);
      await page.getByRole('complementary', { name: 'New adapter' }).getByRole('button', { name: 'Save', exact: true }).click();
      await authorise(page);
      const response = await creation;
      expect(response.status()).toBe(400);
      const raw = await response.text();
      expectRedacted(raw);
      const failure = zError.parse(JSON.parse(raw));
      expect(failure.error.code).toBe('bad_request');
      expect(failure.error.detail).toContain('must end in /');
      const form = page.getByRole('complementary', { name: 'New adapter' });
      await expect(form.getByRole('alert')).toContainText('must end in /');
      await expect(form.getByLabel('Secret access key', { exact: true })).toHaveCount(0);
      const inventory = await browserApi(page, 'GET', ADAPTERS_API, zAdapterList);
      expect(inventory.items.some((adapter) => adapter.origin === origin)).toBe(false);
    });
  });
}
