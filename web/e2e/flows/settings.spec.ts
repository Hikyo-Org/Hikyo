import { readFile } from 'node:fs/promises';
import { expect, type Page } from '@playwright/test';
import {
  zDefinitionsSettings,
  zDefinitionsBundle,
  zEnvironment,
  zEnvironmentList,
  zEnvironmentSettings,
  zOrg,
  zProject,
  zProjectRetentionPolicy,
  zRetentionPolicy,
} from '@hikyo/zod';
import { generatePath } from 'react-router';
import { z } from 'zod';

import { expectNoSeriousAxeViolations, expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import { BrowserApiError, browserApi } from '../fixtures/api.ts';
import {
  establishSession,
  readSeed,
  STORAGE_STATE,
} from '../fixtures/instance.ts';
import { test, withPasskeyPage } from '../fixtures/passkey.ts';
import { surfacesForFlow } from '../registry.ts';

/**
 * Flow: organisation and project settings (registry surfaces `org-settings`
 * and `project-settings`), mvp-boundary S3's "project/org settings incl.
 * retention + danger zones", against the locked prototype #29 iterations 14
 * (org) and 15/16 (project, retention).
 *
 * Everything destructive here happens to objects this flow CREATES. The
 * seeded tenant is the reveal, matrix and machine-access flows' subject: its
 * environment policy is what their ceremonies depend on, and a settings drill
 * that flipped production's protected flag would break three other flows from
 * a cause several files away. So the drills run on a throwaway organisation
 * and a throwaway project, named per Playwright project so a run that dies
 * halfway cannot collide with the next viewport's run.
 *
 * The pinned assertion set runs on the SEEDED organisation and project, where
 * the retention panels and the environment policy have real state to render.
 */

const seed = readSeed();
const SETTINGS_SURFACES = surfacesForFlow('chrome-settings');

function isBrowserApiStatus(error: unknown, status: number): boolean {
  return error instanceof BrowserApiError && error.status === status;
}

test.describe('organisation settings', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  let page: Page;
  let drillOrg = '';
  let drillName = '';

  test.beforeAll(async ({}, testInfo) => {
    drillName = `Settings drill ${testInfo.project.name}`;
  });

  test.beforeEach(async ({ passkeyPage }) => {
    page = passkeyPage;
    await page.goto('/');
    if (drillOrg === '') {
      const created = await browserApi(page, 'POST', '/api/v1/orgs', zOrg, { name: drillName });
      drillOrg = created.id;
      await establishSession(page);
    }
  });

  test.afterAll(async ({ browser }) => {
    if (drillOrg === '') {
      return;
    }
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const cleanupPage = await context.newPage();
    try {
      await withPasskeyPage(cleanupPage, 'shared', async (page) => {
        try {
          await browserApi(page, 'DELETE', `/api/v1/orgs/${drillOrg}`, z.null());
        } catch (error) {
          if (!isBrowserApiStatus(error, 404)) {
            throw error;
          }
        }
        drillOrg = '';
      });
    } finally {
      await context.close();
    }
  });

  test('saves the compact organisation revision default', async () => {
    const original = await browserApi(
      page,
      'GET',
      `/api/v1/orgs/${seed.org}/retention`,
      zRetentionPolicy,
    );
    await page.goto(`/orgs/${seed.org}/settings`);
    await expect(
      page.getByRole('heading', { name: /Organisation settings ·/, level: 1 }),
    ).toBeVisible();

    const retention = page.locator('#org-retention');
    const revisions = retention.getByLabel('Org default revisions kept');
    const next = original.last_revisions === 8 ? 7 : 8;
    try {
      await revisions.fill(String(next));
      await revisions.press('Tab');
      const saved = page.locator('.notice').filter({ hasText: 'Retention saved' });
      await expectStatusIsTextAndAria(page, saved);
      await expect(saved).toContainText(`last ${String(next)}`);
      await expect(revisions).toHaveValue(String(next));
    } finally {
      await browserApi(
        page,
        'PUT',
        `/api/v1/orgs/${seed.org}/retention`,
        zRetentionPolicy,
        original,
      );
    }
  });

  test('renames the organisation and says who may do it', async () => {
    await page.goto(`/orgs/${drillOrg}/settings`);
    await expect(page.getByLabel('Name', { exact: true })).toHaveValue(drillName);
    await page.getByLabel('Name', { exact: true }).fill(`${drillName} renamed`);
    await page.getByLabel('Name', { exact: true }).press('Tab');
    const done = page.locator('.notice').filter({ hasText: 'Renamed to' });
    await expectStatusIsTextAndAria(page, done);
    drillName = `${drillName} renamed`;
  });

  test('does not claim an organisation identity when it is unreadable', async () => {
    const orgRead = new RegExp(`/api/v1/orgs/${drillOrg}$`);
    await page.route(orgRead, (route) =>
      route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'internal', message: 'internal error' } }),
      }),
    );
    try {
      await page.goto(`/orgs/${drillOrg}/settings`);
      // Scoped to the inline page alert: the app-level toast announcer holds a
      // second, always-present role="alert", so a page-wide query resolves two.
      await expect(page.locator('.alert[role="alert"]')).toContainText(
        'This organisation could not be read.',
      );
      await expect(page.getByLabel('Name', { exact: true })).toBeDisabled();
      await expect(page.locator('#org-identity')).not.toContainText('active');
    } finally {
      await page.unroute(orgRead);
    }
  });

  test('disarms organisation deletion while the route identity is pending', async () => {
    const targetName = `Pending org ${Date.now()}`;
    const target = await browserApi(page, 'POST', '/api/v1/orgs', zOrg, { name: targetName });
    await establishSession(page);
    let release: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const targetRead = new RegExp(`/api/v1/orgs/${target.id}$`);
    await page.route(targetRead, async (route) => {
      await gate;
      await route.continue();
    });
    try {
      await page.goto(`/orgs/${drillOrg}/settings`);
      const danger = page.locator('#org-danger');
      await danger.getByLabel('Delete this organisation').fill(drillName);
      await expect(danger.getByRole('button', { name: 'Delete organisation' })).toBeEnabled();

      await page.evaluate((path) => {
        history.pushState(null, '', path);
        dispatchEvent(new PopStateEvent('popstate'));
      }, `/orgs/${target.id}/settings`);
      await expect(page).toHaveURL(new RegExp(`${target.id}/settings$`));
      await expect(page.getByLabel('Name', { exact: true })).toHaveValue('');
      const pendingDanger = page.locator('#org-danger');
      await expect(pendingDanger.getByLabel('Delete this organisation')).toHaveValue('');
      await expect(pendingDanger.getByRole('button', { name: 'Delete organisation' })).toBeDisabled();
      if (release === undefined) {
        throw new Error('the organisation-read gate was not installed');
      }
      release();
      await expect(page.getByLabel('Name', { exact: true })).toHaveValue(targetName);
    } finally {
      if (release !== undefined) {
        release();
      }
      await page.unroute(targetRead);
      await browserApi(page, 'DELETE', `/api/v1/orgs/${target.id}`, z.null());
      // Org deletion removes its contained creator grants and therefore kills
      // this principal's sessions. Re-mint before the next serial drill.
      await page.context().clearCookies();
      await page.goto('/');
      await establishSession(page);
    }
  });

  test('deletes the organisation only behind the typed name', async () => {
    await page.goto(`/orgs/${drillOrg}/settings`);
    const danger = page.locator('#org-danger');
    const remove = danger.getByRole('button', { name: 'Delete organisation' });
    await expect(remove).toBeDisabled();
    // The armed state is text, not only a disabled attribute: "why is this
    // button dead" is answerable without seeing it.
    await expect(danger.getByRole('status')).toContainText('Type');

    await danger.getByLabel('Delete this organisation').fill('not the name');
    await expect(remove).toBeDisabled();

    await danger.getByLabel('Delete this organisation').fill(drillName);
    await expect(danger.getByRole('status')).toContainText('The name matches');
    await expect(remove).toBeEnabled();
    const responsePromise = page.waitForResponse(
      (response) =>
        response.request().method() === 'DELETE' &&
        new URL(response.url()).pathname === `/api/v1/orgs/${drillOrg}`,
    );
    await remove.click();
    const response = await responsePromise;
    expect(response.status()).toBe(204);
    drillOrg = '';
    await expect(page.locator('.toast').filter({ hasText: 'Organisation deleted' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo', level: 1 })).toBeVisible();
  });

});

test.describe('project settings', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  let page: Page;
  let drillProject = '';
  let drillEnv = '';
  let drillName = '';

  const base = () => `/api/v1/orgs/${seed.org}/projects/${drillProject}`;

  test.beforeAll(async ({}, testInfo) => {
    drillName = `drill-${testInfo.project.name}`;
  });

  test.beforeEach(async ({ passkeyPage }) => {
    page = passkeyPage;
    await page.goto('/');
    if (drillProject === '') {
      const project = await browserApi(
        page,
        'POST',
        `/api/v1/orgs/${seed.org}/projects`,
        zProject,
        { name: drillName },
      );
      drillProject = project.id;
      const environment = await browserApi(
        page,
        'POST',
        `${base()}/environments`,
        zEnvironment,
        { name: 'staging' },
      );
      drillEnv = environment.id;
    }
  });

  test.afterAll(async ({ browser }) => {
    if (drillProject === '') {
      return;
    }
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const cleanupPage = await context.newPage();
    try {
      await withPasskeyPage(cleanupPage, 'shared', async (page) => {
        try {
          if (drillEnv !== '') {
            await browserApi(page, 'DELETE', `${base()}/environments/${drillEnv}`, z.null());
          }
          await browserApi(page, 'DELETE', base(), z.null());
        } catch (error) {
          if (!isBrowserApiStatus(error, 404)) {
            throw error;
          }
        }
        drillProject = '';
        drillEnv = '';
      });
    } finally {
      await context.close();
    }
  });

  test('definitions bundle exports, checks, plans and applies without the CLI', async ({}, testInfo) => {
    await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
    const downloadPromise = page.waitForEvent('download');
    await page.getByRole('link', { name: 'Download definitions bundle', exact: true }).click();
    const download = await downloadPromise;
    const path = await download.path();
    if (path === null) throw new Error('No bundle was downloaded');
    const content = await readFile(path, 'utf8');
    const exported = zDefinitionsBundle.parse(JSON.parse(content));
    expect(exported.environments.some((env) => env.name === 'staging')).toBe(true);
    const portable = page.getByRole('link', { name: 'Download portable bundle', exact: true });
    await expect(portable).toHaveAttribute('href', `${base()}/definitions/export?portable=true`);
    await page.getByRole('button', { name: 'Check a bundle', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Definitions bundle', exact: true });
    const upload = dialog.getByLabel('Definitions bundle file (JSON, up to 1 MiB)');
    await upload.setInputFiles({ name: 'definitions.json', mimeType: 'application/json', buffer: Buffer.from(content) });
    await dialog.getByRole('button', { name: 'Check bundle', exact: true }).click();
    await expect(dialog.getByRole('status').filter({ hasText: 'Equal:' })).toBeVisible();
    // New additive environment is an observable schema publish, not a no-op.
    await upload.setInputFiles({ name: 'portable.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify({ format_version: 1, environments: [{ name: 'bundle-preview' }], key_groups: [], keys: [] })) });
    await dialog.getByRole('button', { name: 'Check bundle', exact: true }).click();
    await expect(dialog.getByRole('status').filter({ hasText: 'File ahead:' })).toBeVisible();
    await dialog.getByRole('button', { name: 'Create impact plan' }).click();
    await expect(dialog.getByRole('heading', { name: 'Immutable impact plan' })).toBeVisible();
    expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
    await dialog.getByRole('button', { name: 'Review and apply' }).scrollIntoViewIfNeeded();
    for (const colorScheme of ['dark', 'light']) {
      await page.emulateMedia({ colorScheme: colorScheme === 'dark' ? 'dark' : 'light', reducedMotion: 'reduce' });
      await expectNoSeriousAxeViolations(page);
      await page.screenshot({ path: testInfo.outputPath(`definitions-plan-${colorScheme}.png`), fullPage: true });
    }
    await page.screenshot({ path: testInfo.outputPath('definitions-plan.png'), fullPage: true });
    await dialog.getByRole('button', { name: 'Review and apply' }).click();
    await page.getByRole('dialog', { name: 'Apply definitions and publish' }).getByRole('button', { name: 'Apply and publish' }).click();
    await expect(dialog.getByRole('status').filter({ hasText: 'Definitions applied at revision' })).toBeVisible();
    await dialog.getByRole('button', { name: 'Close definitions bundle' }).click();
    await expect(page.locator('#project-environments').getByText('bundle-preview', { exact: true })).toBeVisible();
    const environments = await browserApi(page, 'GET', `${base()}/environments`, zEnvironmentList);
    const created = environments.items.find((environment) => environment.name === 'bundle-preview');
    if (created === undefined) throw new Error('Applied environment is missing');
    await browserApi(page, 'DELETE', `${base()}/environments/${created.id}`, z.null());
  });

  test('definitions bundle shows stale-base, Git governance and publish refusals', async () => {
    const makeFile = (baseRevision?: number) => ({ name: 'definitions.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify({ format_version: 1, base_revision: baseRevision, environments: [], key_groups: [], keys: [] })) });
    await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
    await page.getByRole('button', { name: 'Check a bundle', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Definitions bundle', exact: true });
    const upload = dialog.getByLabel('Definitions bundle file (JSON, up to 1 MiB)');
    await upload.setInputFiles(makeFile(0));
    await dialog.getByRole('button', { name: 'Check bundle', exact: true }).click();
    await dialog.getByRole('button', { name: 'Create impact plan' }).click();
    await expect(dialog.getByRole('alert').filter({ hasText: 're-export and rebase' })).toBeVisible();
    await upload.setInputFiles(makeFile());
    await dialog.getByRole('button', { name: 'Check bundle', exact: true }).click();
    await dialog.getByRole('button', { name: 'Create impact plan' }).click();
    await page.route(`**${base()}/definitions/plans/*/apply`, (route) => route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'not found' } } }));
    await dialog.getByRole('button', { name: 'Review and apply' }).click();
    await page.getByRole('dialog', { name: 'Apply definitions and publish' }).getByRole('button', { name: 'Apply and publish' }).click();
    await expect(dialog.getByRole('alert').filter({ hasText: 'publish on every affected environment' })).toBeVisible();
    await page.unroute(`**${base()}/definitions/plans/*/apply`);
    await dialog.getByRole('button', { name: 'Close definitions bundle' }).click();
    try {
      await browserApi(page, 'PUT', `${base()}/definitions/settings`, zDefinitionsSettings, { definitions_source: 'git' });
      await page.reload();
      await page.getByRole('button', { name: 'Check a bundle', exact: true }).click();
      await expect(dialog.getByRole('alert').filter({ hasText: 'Browser apply is refused' })).toBeVisible();
      await upload.setInputFiles(makeFile());
      await dialog.getByRole('button', { name: 'Check bundle', exact: true }).click();
      await dialog.getByRole('button', { name: 'Create impact plan' }).click();
      await expect(dialog.getByRole('button', { name: 'Review and apply' })).toBeDisabled();
    } finally {
      await browserApi(page, 'PUT', `${base()}/definitions/settings`, zDefinitionsSettings, { definitions_source: 'db' });
    }
  });

  test('renames the project and leaves the identifier alone', async () => {
    await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
    await expect(
      page.getByRole('heading', { name: `Project settings · ${drillName}`, level: 1 }),
    ).toBeVisible();
    const before = page.url();

    await page.getByLabel('Name', { exact: true }).fill(`${drillName}-renamed`);
    await page.getByLabel('Name', { exact: true }).press('Tab');
    await expect(page.locator('.notice').filter({ hasText: 'Renamed to' })).toBeVisible();
    drillName = `${drillName}-renamed`;
    await expect(page).toHaveURL(before);
  });

  test('creates, renames, clones, reorders, and deletes an environment through the SPA', async () => {
    // The four lifecycle operations driven entirely through the rendered
    // "Manage" disclosure, against the real server (#623 restores this surface).
    // Every environment here is created and destroyed within the test, so the
    // shared `staging` drill environment the other tests depend on is untouched.
    await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
    const panel = page.locator('#project-environments');
    const created = 'lifecycle';
    const renamed = 'lifecycle-renamed';
    const clone = 'lifecycle-clone';

    // Create through the SPA form.
    await panel.getByLabel('New environment name').fill(created);
    await panel.getByRole('button', { name: 'Create' }).click();
    await expect(
      page.locator('.notice').filter({ hasText: `Environment ${created} created.` }),
    ).toBeVisible();

    // Open its disclosure once. The id is stable across rename and clone, so the
    // same <details> stays open for all three actions below the summary.
    await panel.locator('summary', { hasText: `Manage ${created}` }).click();
    const open = panel.locator('details.environment-lifecycle[open]');

    await open.getByLabel(`New name for ${created}`).fill(renamed);
    await open.getByRole('button', { name: 'Rename environment' }).click();
    await expect(
      page.locator('.notice').filter({ hasText: `renamed to ${renamed}` }),
    ).toBeVisible();

    await open.getByLabel(`Clone ${renamed} into`).fill(clone);
    await open.getByRole('button', { name: 'Clone environment' }).click();
    await expect(page.locator('.notice').filter({ hasText: `cloned to ${clone}` })).toBeVisible();

    // Reorder: the renamed row is not first (staging precedes it), so Move up is
    // live and sends the whole ordered set.
    await open.getByRole('button', { name: `Move ${renamed} up` }).click();
    await expect(
      page.locator('.notice').filter({ hasText: `Environment ${renamed} moved up` }),
    ).toBeVisible();

    // Typed-name delete of both environments this test created, through the SPA.
    // The renamed row's disclosure is still open from the actions above, so
    // delete it in place; the clone's is closed, so open it first.
    await open.getByLabel(`Delete ${renamed}`).fill(renamed);
    await open.getByRole('button', { name: 'Delete environment' }).click();
    await expect(
      page.locator('.notice').filter({ hasText: `Environment ${renamed} deleted.` }),
    ).toBeVisible();

    await panel.locator('summary', { hasText: `Manage ${clone}` }).click();
    const cloneRow = panel.locator('details.environment-lifecycle[open]');
    await cloneRow.getByLabel(`Delete ${clone}`).fill(clone);
    await cloneRow.getByRole('button', { name: 'Delete environment' }).click();
    await expect(
      page.locator('.notice').filter({ hasText: `Environment ${clone} deleted.` }),
    ).toBeVisible();
  });

  test('rotates the project DEK and re-encrypts, resuming across a reload', async () => {
    // The project-scoped half of remote cryptographic maintenance (#503),
    // against the real server. Both jobs are content-invisible: they re-wrap
    // this project's ciphertext without touching its plaintext.
    await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
    const keys = page.locator('#project-keys');

    await keys.getByRole('button', { name: 'Rotate the project DEK' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('incomplete until');
    await dialog.getByRole('button', { name: 'Rotate the DEK' }).click();
    await expect(page.getByRole('dialog')).toBeHidden();
    await expect(page.locator('.notice')).toContainText("This project's DEK was rotated");

    // Reload BEFORE re-encrypting: the pending walk lives only in the server's
    // cursor now. A fresh page with no client state must resume it to a clean
    // complete end, the interrupted-then-resumed recovery.
    await page.reload();
    await keys.getByRole('button', { name: 'Re-encrypt the project' }).click();
    await expect(page.locator('.notice')).toContainText('Project re-encryption complete');

    // Idempotent: a further run moves nothing.
    await keys.getByRole('button', { name: 'Re-encrypt the project' }).click();
    await expect(page.locator('.notice')).toContainText(
      'nothing to move; all ciphertext is already on the active DEK version',
    );
  });

  test('protects an environment through the compact policy controls', async () => {
    const settingsPath = `${base()}/environments/${drillEnv}/settings`;
    const original = await browserApi(page, 'GET', settingsPath, zEnvironmentSettings);
    await browserApi(
      page,
      'PUT',
      settingsPath,
      zEnvironmentSettings,
      {
        protected: false,
        reauth_window_seconds: 300,
      },
    );
    try {
      await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
      const policy = page.locator('#project-policy');
      const environment = policy.getByRole('button', { name: 'staging' });
      await expect(environment).toHaveAttribute('aria-pressed', 'false');
      await expect(policy.getByLabel('Reveal reauth window')).toHaveValue('900');

      await environment.click();
      const protectedNotice = page.locator('.notice').filter({ hasText: 'staging is now protected' });
      await expectStatusIsTextAndAria(page, protectedNotice);
      await expect(policy.getByRole('button', { name: /staging/ })).toHaveAttribute(
        'aria-pressed',
        'true',
      );

      await policy.getByRole('button', { name: /staging/ }).click();
      await expect(
        page.locator('.notice').filter({ hasText: 'staging is no longer protected' }),
      ).toBeVisible();
      await policy.getByLabel('Reveal reauth window').selectOption('300');
      await expect(
        page.locator('.notice').filter({ hasText: 'changed to 300 seconds' }),
      ).toBeVisible();
    } finally {
      await browserApi(page, 'PUT', settingsPath, zEnvironmentSettings, original);
    }
  });

  test('persists Git-governed definitions and explains the read-only boundary', async () => {
    await page.emulateMedia({ colorScheme: 'dark' });
    try {
      await browserApi(page, 'PUT', `${base()}/definitions/settings`, zDefinitionsSettings, {
        definitions_source: 'db',
      });
      await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
      const policy = page.locator('#project-policy');
      const source = policy.getByLabel('Definitions source');
      await expect(source).toHaveValue('db');

      // Select by contract value: labels are presentation and may be localised.
      await source.selectOption('git');
      const gitNotice = policy.getByRole('alert').filter({
        hasText:
          'Definitions for this project are managed in Git: changes arrive through `definitions plan` / `definitions apply`.',
      });
      await expect(gitNotice.locator('span').last()).toHaveText(
        'Definitions for this project are managed in Git: changes arrive through `definitions plan` / `definitions apply`.',
      );
      await expect(source).toHaveValue('git');

      await page.reload();
      const persistedPolicy = page.locator('#project-policy');
      const persistedSource = persistedPolicy.getByLabel('Definitions source');
      await expect(persistedSource).toHaveValue('git');
      const persistedNotice = persistedPolicy.getByRole('alert').filter({
        hasText:
          'Definitions for this project are managed in Git: changes arrive through `definitions plan` / `definitions apply`.',
      });
      await expect(persistedNotice).toBeVisible();

      const heading = page.getByRole('heading', {
        name: `Project settings · ${drillName}`,
        level: 1,
      });
      await expectPinnedAssertionSet(page, {
        flow: 'chrome-settings',
        surface: 'project-settings',
        theme: 'dark',
        text: [heading, persistedNotice, persistedPolicy.locator('.settings-row__detail').first()],
        radii: [
          [persistedPolicy, 'container'],
          [persistedSource, 'control'],
        ],
        fonts: [
          [heading, 'ui'],
          [persistedPolicy.locator('.settings-row__detail').first(), 'mono'],
        ],
        colours: [
          [heading, 'color', '--tx'],
          [persistedPolicy, 'backgroundColor', '--bg-panel'],
          [persistedPolicy, 'borderTopColor', '--panel-line'],
        ],
        hairlines: [persistedPolicy],
        density: [[page.locator('#project-metadata input').first(), '--touch']],
      });

      // project-settings remains capable of changing its own governance mode.
      await persistedSource.selectOption('db');
      await expect(persistedSource).toHaveValue('db');
      await expect(persistedNotice).toHaveCount(0);
      await page.reload();
      await expect(page.locator('#project-policy').getByLabel('Definitions source')).toHaveValue(
        'db',
      );
    } finally {
      await browserApi(page, 'PUT', `${base()}/definitions/settings`, zDefinitionsSettings, {
        definitions_source: 'db',
      });
      await page.emulateMedia({ colorScheme: null });
    }
  });

  test('keeps compact policy controls disabled while an environment is unreadable', async () => {
    const settingsPath = `${base()}/environments/${drillEnv}/settings`;
    await page.route(`**${settingsPath}`, async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({ error: { code: 'not_found', message: 'not found' } }),
        });
        return;
      }
      await route.continue();
    });
    try {
      await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
      const policy = page.locator('#project-policy');
      await expect(policy.getByText("This environment's policy could not be read.")).toBeVisible();
      await expect(policy.getByRole('button', { name: 'staging' })).toBeDisabled();
    } finally {
      await page.unroute(`**${settingsPath}`);
    }
  });

  for (const refusal of [
    { status: 403, code: 'forbidden', text: 'not permitted to read' },
    { status: 500, code: 'internal', text: 'failed to load' },
  ]) {
    test(`surfaces a ${refusal.status} environment-policy read as an alert`, async () => {
      const settingsPath = `${base()}/environments/${drillEnv}/settings`;
      await page.route(`**${settingsPath}`, async (route) => {
        if (route.request().method() === 'GET') {
          await route.fulfill({
            status: refusal.status,
            contentType: 'application/json',
            body: JSON.stringify({ error: { code: refusal.code, message: refusal.code } }),
          });
          return;
        }
        await route.continue();
      });
      try {
        await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
        const policy = page.locator('#project-policy');
        await expect(policy.getByRole('alert').filter({ hasText: refusal.text })).toBeVisible();
        await expect(policy.getByRole('button', { name: 'staging' })).toBeDisabled();
      } finally {
        await page.unroute(`**${settingsPath}`);
      }
    });
  }

  test('does not turn failed dependencies into empty or generic retention state', async () => {
    const environmentsRead = new RegExp(`${base()}/environments$`);
    const orgRetentionRead = new RegExp(`/api/v1/orgs/${seed.org}/retention$`);
    const fault = {
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'internal', message: 'internal error' } }),
    };
    await page.route(environmentsRead, (route) => route.fulfill(fault));
    await page.route(orgRetentionRead, (route) => route.fulfill(fault));
    try {
      await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
      await expect(
        page.locator('#project-policy').getByRole('alert').filter({
          hasText: 'environments could not be read',
        }),
      ).toBeVisible();
      await expect(page.getByText('This project holds no environments yet.')).toHaveCount(0);
      await expect(page.locator('#project-retention').getByRole('alert')).toContainText(
        'organisation retention cap could not be read',
      );
      await expect(
        page.getByText('The organisation cap decides what this project may keep.'),
      ).toHaveCount(0);
    } finally {
      await page.unroute(environmentsRead);
      await page.unroute(orgRetentionRead);
    }
  });

  test('saves and clears compact project retention', async () => {
    const retentionPath = `${base()}/retention`;
    const original = await browserApi(page, 'GET', retentionPath, zProjectRetentionPolicy);
    try {
      await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
      const retention = page.locator('#project-retention');
      const mode = retention.getByLabel('Retention mode');
      await mode.selectOption('custom');
      await expect(page.locator('.notice').filter({ hasText: 'Revision retention saved' })).toBeVisible();

      const revisions = retention.getByLabel('Revisions kept per environment');
      await revisions.fill('2');
      await revisions.press('Tab');
      await expect(page.locator('.notice').filter({ hasText: 'Revision retention saved' })).toBeVisible();
      await expect(retention).toContainText('custom');

      await mode.selectOption('inherit');
      await expect(page.locator('.notice').filter({ hasText: 'Revision retention saved' })).toBeVisible();
      await expect(revisions).toHaveCount(0);
    } finally {
      await browserApi(page, 'PUT', retentionPath, zProjectRetentionPolicy, {
        inherited: original.inherited,
        max_age_seconds: original.inherited ? null : original.max_age_seconds,
        last_revisions: original.inherited ? null : original.last_revisions,
      });
    }
  });

  test('disarms typed-name deletion when route identity changes', async () => {
    await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
    await expect(page.getByLabel('Name', { exact: true })).toHaveValue(drillName);
    const danger = page.locator('#project-danger');
    await danger.getByLabel('Delete this project').fill(drillName);
    await expect(danger.getByRole('button', { name: 'Delete project' })).toBeEnabled();

    const target = `/orgs/${seed.org}/projects/${seed.project}/settings`;
    const projectRead = new RegExp(
      `/api/v1/orgs/${seed.org}/projects/${seed.project}$`,
    );
    await page.route(projectRead, (route) =>
      route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'internal', message: 'internal error' } }),
      }),
    );
    try {
      await page.evaluate((path) => {
        history.pushState(null, '', path);
        dispatchEvent(new PopStateEvent('popstate'));
      }, target);
      await expect(page).toHaveURL(new RegExp(`${seed.project}/settings$`));
      await expect(page.getByLabel('Name', { exact: true })).toHaveValue('');
      await expect(page.getByLabel('Name', { exact: true })).toBeDisabled();
      const nextDanger = page.locator('#project-danger');
      await expect(nextDanger.getByLabel('Delete this project')).toHaveValue('');
      await expect(nextDanger.getByRole('button', { name: 'Delete project' })).toBeDisabled();
    } finally {
      await page.unroute(projectRead);
    }
  });

  test('disarms typed-name deletion between successfully loaded projects with the same name', async () => {
    const otherOrg = await browserApi(page, 'POST', '/api/v1/orgs', zOrg, {
      name: `Same-name holder ${Date.now()}`,
    });
    await establishSession(page);
    const otherProject = await browserApi(
      page,
      'POST',
      `/api/v1/orgs/${otherOrg.id}/projects`,
      zProject,
      { name: drillName },
    );
    try {
      await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
      const danger = page.locator('#project-danger');
      await danger.getByLabel('Delete this project').fill(drillName);
      await expect(danger.getByRole('button', { name: 'Delete project' })).toBeEnabled();

      await page.goto(`/orgs/${otherOrg.id}/projects/${otherProject.id}/settings`);
      await expect(page.getByLabel('Name', { exact: true })).toHaveValue(drillName);
      const switched = page.locator('#project-danger');
      await expect(switched.getByLabel('Delete this project')).toHaveValue('');
      await expect(switched.getByRole('button', { name: 'Delete project' })).toBeDisabled();
    } finally {
      await browserApi(
        page,
        'DELETE',
        `/api/v1/orgs/${otherOrg.id}/projects/${otherProject.id}`,
        z.null(),
      );
      await browserApi(page, 'DELETE', `/api/v1/orgs/${otherOrg.id}`, z.null());
      await page.context().clearCookies();
      await page.goto('/');
      await establishSession(page);
    }
  });

  test('refuses to delete a project while it still contains an environment', async () => {
    await page.goto(`/orgs/${seed.org}/projects/${drillProject}/settings`);
    const danger = page.locator('#project-danger');
    await danger.getByLabel('Delete this project').fill(drillName);
    await danger.getByRole('button', { name: 'Delete project' }).click();
    const refusal = page.getByRole('alert').filter({ hasText: 'never cascades' });
    await expect(refusal).toBeVisible();
  });

});

test.describe('settings flow visual contract', () => {
  test.use({ storageState: STORAGE_STATE });

  // Registry-derived: adding a claimed surface adds its pinned checks here.
  for (const surface of SETTINGS_SURFACES) {
    for (const scheme of ['dark', 'light'] as const) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        try {
          const path = generatePath(surface.path, { org: seed.org, project: seed.project });
          await page.goto(path);
          const heading = page.getByRole('heading', {
            name: surface.id === 'org-settings'
              ? /^Organisation settings · /
              : /^Project settings · /,
            level: 1,
          });
          const well = page.locator('.panel').first();
          const control = surface.id === 'org-settings'
            ? page.locator('#org-identity input:not([type="range"]):not([type="file"])')
            : page.locator('#project-metadata input').first();
          const metadata = well.locator('.settings-row__detail').first();

          await expectPinnedAssertionSet(page, {
            flow: 'chrome-settings',
            surface: surface.id,
            theme: scheme,
            text: [
              heading,
              page.locator('.page__lede'),
              metadata,
            ],
            radii: [
              [well, 'container'],
              [control, 'control'],
            ],
            fonts: [
              [heading, 'ui'],
              [metadata, 'mono'],
            ],
            colours: [
              [heading, 'color', '--tx'],
              [well, 'backgroundColor', '--bg-panel'],
              [well, 'borderTopColor', '--panel-line'],
            ],
            hairlines: [well],
            density: [[control, '--touch']],
          });
        } finally {
          await page.emulateMedia({ colorScheme: null });
        }
      });
    }
  }
});
