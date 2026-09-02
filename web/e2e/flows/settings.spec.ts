import { readFileSync } from 'node:fs';

import { expect, type Page } from '@playwright/test';
import {
  zDefinitionsSettings,
  zEnvironment,
  zEnvironmentSettings,
  zOrg,
  zProject,
  zProjectRetentionPolicy,
  zRetentionPolicy,
} from '@hikyo/zod';
import { generatePath } from 'react-router';
import { z } from 'zod';

import { expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import { BrowserApiError, browserApi } from '../fixtures/api.ts';
import {
  BASE_URL,
  establishSession,
  readSeed,
  STORAGE_STATE,
} from '../fixtures/instance.ts';
import { test, withPasskeyPage } from '../fixtures/passkey.ts';
import { surfacesForFlow } from '../registry.ts';

/**
 * Flow: organisation and project settings (registry surfaces `org-settings`
 * and `project-settings`) — mvp-boundary S3's "project/org settings incl.
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
    // complete end — the interrupted-then-resumed recovery.
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
          'Definitions for this project are managed in Git — changes arrive through `definitions plan` / `definitions apply`.',
      });
      await expect(gitNotice.locator('span').last()).toHaveText(
        'Definitions for this project are managed in Git — changes arrive through `definitions plan` / `definitions apply`.',
      );
      await expect(source).toHaveValue('git');

      await page.reload();
      const persistedPolicy = page.locator('#project-policy');
      const persistedSource = persistedPolicy.getByLabel('Definitions source');
      await expect(persistedSource).toHaveValue('git');
      const persistedNotice = persistedPolicy.getByRole('alert').filter({
        hasText:
          'Definitions for this project are managed in Git — changes arrive through `definitions plan` / `definitions apply`.',
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

/**
 * Flow: project- and environment-scoped audit (registry surface `project-audit`,
 * #572). It rides this file for the reason the registry spells out: the merge
 * gate loads `ci.yml` from the base branch, so a spec a PR adds to a group never
 * runs on that PR. settings.spec.ts is already in group 2 and is the
 * project-scoped sibling surface, so the surface's pinned set runs from
 * PR-checked-out content today. The bootstrap administrator holds `audit-read`
 * (seeded break-glass at instance scope, inheriting down to this project), so
 * the project and environment trails are readable.
 */
const PROJECT_AUDIT_PATH = `/orgs/${seed.org}/projects/${seed.project}/audit`;
const PROJECT_AUDIT_SURFACES = surfacesForFlow('project-audit');

test.describe('project audit trail', () => {
  test.use({ storageState: STORAGE_STATE });

  test('reads the project trail, scopes to an environment, and exports both', async ({ page }) => {
    await page.goto(PROJECT_AUDIT_PATH);
    await expect(page.getByRole('heading', { name: 'Audit', level: 1 })).toBeVisible();
    await expect(page.getByText('Every recorded event in this project')).toBeVisible();

    // The seed created this project's keys and published values, so the trail
    // is not empty. A project-scoped holder reaches it without an org proof.
    await expect(page.locator('.audit__row').first()).toBeVisible();

    // The project export is a same-origin GET under the session cookie; the
    // browser streams JSONL to disk.
    const projectDownload = page.waitForEvent('download');
    await page.getByRole('link', { name: 'Export JSONL' }).click();
    const projectFile = await projectDownload;
    expect(projectFile.suggestedFilename()).toMatch(/\.jsonl$/);
    expect(readFileSync(await projectFile.path(), 'utf8').trim().length).toBeGreaterThan(0);

    // Narrowing to an environment switches to the environment operations and
    // carries the id in the URL, so a reload resolves the same environment.
    await page.getByLabel('Environment').selectOption(seed.dev);
    await expect(page).toHaveURL(new RegExp(`environment=${seed.dev}`));
    await expect(page.getByText('Every recorded event in this environment')).toBeVisible();
    await expect(page.locator('.audit__row').first()).toBeVisible();

    // The export link now points at the environment operation's path.
    const exportLink = page.getByRole('link', { name: 'Export JSONL' });
    await expect(exportLink).toHaveAttribute(
      'href',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/environments/${seed.dev}/audit/export`,
    );
    const envDownload = page.waitForEvent('download');
    await exportLink.click();
    const envFile = await envDownload;
    expect(envFile.suggestedFilename()).toMatch(/\.jsonl$/);
  });

  test('renders the scoped forbidden state instead of a blank table on 403', async ({ page }) => {
    // A project holder who may NOT read this trail must see the refusal, never
    // an empty table read as "no events".
    const auditRead = new RegExp(
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/audit(\\?|$)`,
    );
    await page.route(auditRead, (route) =>
      route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'forbidden', message: 'forbidden' } }),
      }),
    );
    try {
      await page.goto(PROJECT_AUDIT_PATH);
      await expect(page.locator('.audit__empty[role="alert"]')).toContainText(
        'This trail is not available, or you may not read it.',
      );
      await expect(page.locator('.audit__row')).toHaveCount(0);
    } finally {
      await page.unroute(auditRead);
    }
  });

  test('keeps the trail and the scope escape when the environment list cannot be read', async ({
    page,
  }) => {
    // The picker's list load is independent of the trail: a holder who cannot
    // read the environment list still reads the project trail, and the picker
    // stays usable so a deep link that pinned an environment is not a trap.
    const envList = new RegExp(
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/environments(\\?|$)`,
    );
    await page.route(envList, (route) =>
      route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'forbidden', message: 'forbidden' } }),
      }),
    );
    try {
      // Land already pinned to an environment: the list read fails, but the
      // trail must still load and the "All environments" escape must remain.
      await page.goto(`${PROJECT_AUDIT_PATH}?environment=${seed.dev}`);
      const picker = page.getByLabel('Environment');
      await expect(picker).toBeEnabled();
      await expect(page.getByText('The environment list could not be read')).toBeVisible();
      await expect(page.locator('.audit__row').first()).toBeVisible();

      // Switching back to the project trail is reachable, so the deep link is
      // recoverable rather than a dead end.
      await picker.selectOption('');
      await expect(page).not.toHaveURL(/environment=/);
      await expect(page.getByText('Every recorded event in this project')).toBeVisible();
      await expect(page.locator('.audit__row').first()).toBeVisible();
    } finally {
      await page.unroute(envList);
    }
  });

  test('resets the open detail and filter when the project changes under the same surface', async ({
    page,
  }) => {
    // `audit` and `project-audit` share one keyed element, so a sidebar hop
    // between two projects reuses the component. An event or a filter from one
    // project's trail must not carry into another's.
    await page.goto(PROJECT_AUDIT_PATH);
    await page.getByLabel('Operation').fill('settings.key_created');
    await page.getByRole('button', { name: 'Apply filter' }).click();
    await page.locator('.audit__row').first().click();
    await expect(page.getByRole('complementary', { name: 'Event detail' })).toBeVisible();

    // Client-side navigation to a DIFFERENT project's audit under the SAME
    // keyed element (not a reload, which would remount and reset trivially):
    // the component is reused, so the reset must come from the effect.
    await page.evaluate((path) => {
      history.pushState(null, '', path);
      dispatchEvent(new PopStateEvent('popstate'));
    }, `/orgs/${seed.org}/projects/${seed.history.project}/audit`);
    await expect(page).toHaveURL(new RegExp(`${seed.history.project}/audit$`));
    await expect(page.getByRole('complementary', { name: 'Event detail' })).toBeHidden();
    await expect(page.getByLabel('Operation')).toHaveValue('');
  });

  test('closes the open detail when the environment scope changes by URL', async ({ page }) => {
    // An environment scope switch can arrive by URL (a sidebar link clearing
    // ?environment, or the back button), bypassing the picker's own path, so
    // the open detail must still close: it belongs to the prior scope's trail.
    await page.goto(`${PROJECT_AUDIT_PATH}?environment=${seed.dev}`);
    await expect(page.getByText('Every recorded event in this environment')).toBeVisible();
    await page.locator('.audit__row').first().click();
    await expect(page.getByRole('complementary', { name: 'Event detail' })).toBeVisible();

    // Clear ?environment by client-side navigation, not the picker: the reused
    // component must still drop the detail from the environment trail.
    await page.evaluate((path) => {
      history.pushState(null, '', path);
      dispatchEvent(new PopStateEvent('popstate'));
    }, PROJECT_AUDIT_PATH);
    await expect(page.getByText('Every recorded event in this project')).toBeVisible();
    await expect(page.getByRole('complementary', { name: 'Event detail' })).toBeHidden();
  });

  test('refuses a malformed filter at the project boundary', async ({ page }) => {
    const response = await page.request.get(
      `${BASE_URL}/api/v1/orgs/${seed.org}/projects/${seed.project}/audit?outcome=not-an-outcome`,
    );
    expect(response.status()).toBe(400);
  });

  for (const scheme of ['dark', 'light'] as const) {
    for (const surface of PROJECT_AUDIT_SURFACES) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({
        page,
      }, testInfo) => {
        await page.emulateMedia({ colorScheme: scheme });
        try {
          await page.goto(PROJECT_AUDIT_PATH);
          // Narrow before the sweep: the pinned set focuses, measures and
          // contrast-checks EVERY interactive element, so a full page of rows
          // would blow the per-test budget. `settings.key_created` is a small,
          // always-present set: the seed created this project's keys (the audit
          // event type, not the authz operation `key.create`).
          await page.getByLabel('Operation').fill('settings.key_created');
          await page.getByRole('button', { name: 'Apply filter' }).click();
          await expect(page.locator('.audit__row').first()).toBeVisible();

          const heading = page.getByRole('heading', { name: 'Audit', level: 1 });
          // The project page carries a scope panel before the filter, so target
          // the filter panel by name rather than "first panel".
          const panel = page.locator('.audit__filter');
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
