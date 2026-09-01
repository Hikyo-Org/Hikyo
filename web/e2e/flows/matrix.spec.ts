import { expect, type Locator, type Page } from '@playwright/test';
import { zEnvironmentList } from '@hikyo/zod';
import { z } from 'zod';

import {
  expectPinnedAssertionSet,
  expectStatusIsTextAndAria,
} from '../fixtures/assertions.ts';
import { browserApi, fixtureApiCall, fixtureBearer } from '../fixtures/api.ts';
import {
  readSeed,
  STORAGE_STATE,
} from '../fixtures/instance.ts';
import { test } from '../fixtures/passkey.ts';
import { surfacesForFlow } from '../registry.ts';

/**
 * Flow: whole-project environment matrix (registry surface `matrix`).
 *
 * Each project first stages clean development and unsafe production drafts,
 * proves selective publish, then repairs production through its protected
 * ceremony. The copy leg proves the same protected guard and secret routing.
 * Mobile narrows to the acceptance viewport, 375px, before its final edit.
 */

const seed = readSeed();
const MATRIX_PATH = `/orgs/${seed.org}/projects/${seed.project}/matrix`;
const SCHEMES: readonly ('dark' | 'light')[] = ['dark', 'light'];

/**
 * The project navigation is a fixed drawer on a phone and a column on a desktop.
 *
 * Closing it is NOT "click Menu again": the open drawer is `position: fixed` and
 * 300px wide, so it sits on top of the toggle that opened it and swallows the
 * click. The app's own ways out are Escape and the drawer's close button; these
 * use Escape, which shell.spec also proves restores focus to the toggle.
 */
async function withProjectNavigation(
  page: Page,
  read: (navigation: Locator) => Promise<void>,
): Promise<void> {
  const menu = page.getByRole('button', { name: 'Menu' });
  const drawer = await menu.isVisible();
  if (drawer) await menu.click();
  await read(page.getByRole('navigation', { name: 'Project' }));
  if (drawer) {
    await page.keyboard.press('Escape');
    await expect(page.getByRole('navigation', { name: 'Sections', exact: true })).toBeHidden();
  }
}

test.describe('environment matrix', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({
    storageState: STORAGE_STATE,
    permissions: ['clipboard-read', 'clipboard-write'],
  });

  test.beforeEach(async ({ page }) => {
    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
    await withProjectNavigation(page, async (projectNavigation) => {
      await expect(
        page.locator('.project-sidebar').getByRole('heading', { name: 'payments' }),
      ).toBeVisible();
      await expect(
        projectNavigation.getByRole('link', { name: 'Environment matrix', exact: true }),
      ).toHaveAttribute('aria-current', 'page');
      await expect(projectNavigation.getByRole('button', { name: /app\/.*1 key/ })).toBeVisible();
    });
    await expect(page.getByRole('button', { name: /LOG_LEVEL in development:/ })).toBeVisible();
  });

  test('keeps problems visible and publishes only selected clean environments', async ({ passkeyPage: page }, testInfo) => {
      await page.getByRole('button', { name: /LOG_LEVEL in development:/ }).click();
      await page.getByRole('dialog').getByLabel('development value').fill(`selective-${testInfo.project.name}`);
      await page.getByRole('dialog').getByRole('button', { name: 'Save 1 draft' }).click();

      await page.getByRole('button', { name: new RegExp(`${seed.matrixRequired} in production:`) }).click();
      await page.getByRole('dialog').getByRole('button', { name: 'Clear production to absent' }).click();
      await page.getByRole('dialog').getByRole('button', { name: 'Save 1 draft' }).click();
      await expect(page.locator('.notice')).toContainText(`1 draft updated for ${seed.matrixRequired}`);

      await page.getByRole('button', { name: /unpublished edit/ }).click();
      const publishSheet = page.getByRole('region', { name: 'Publish drafts' });
      const development = publishSheet.getByRole('checkbox', { name: /development/ });
      const production = publishSheet.getByRole('checkbox', { name: /production/ });
      const publish = publishSheet.getByRole('button', { name: /Publish selected/ });
      await expect(development).toBeChecked();
      await expect(publishSheet).toContainText(`selective-${testInfo.project.name}`);
      await expect(production).not.toBeChecked();
      await expect(production).toBeDisabled();
      await expect(publish).toBeEnabled();
      await expect(publishSheet.getByRole('alert')).toContainText(
        `Publish blocked: ${seed.matrixRequired} in production`,
      );

      const menu = page.getByRole('button', { name: 'Menu' });
      if (await menu.isVisible()) await menu.click();
      // Anchored: a group that HAS problems carries "problems" in its own
      // aria-label ("app/ · 1 key · 2 problems"), so an unanchored match is
      // ambiguous the moment the filter has anything to show.
      await page.getByRole('button', { name: /^problems/ }).click();
      const bar = page.locator('.matrix__filter');
      await expectStatusIsTextAndAria(page, bar);
      await expect(bar).toContainText('filter active: problems');
      await expect(bar).toContainText('showing 1 of 5 keys');

      await expect(page.getByRole('rowheader', { name: new RegExp(seed.matrixRequired) })).toBeVisible();
      await expect(page.getByRole('rowheader', { name: /LOG_LEVEL/ })).toHaveCount(0);

      const hiddenApp = page.locator('.matrix__group-link[title="hidden by the problems filter"]', {
        hasText: 'app/',
      });
      await expect(hiddenApp).toBeDisabled();
      await expect(hiddenApp).toHaveAttribute('title', 'hidden by the problems filter');

      if (await menu.isVisible()) await menu.click();
      await page.locator('.matrix__group-link', { hasText: 'ops/' }).click();
      await expect(bar).toBeVisible();
      await expect(bar).toContainText('filter active: problems');

      await page.getByRole('button', { name: '✕ show all keys' }).click();
      await expect(page.getByRole('rowheader', { name: /LOG_LEVEL/ })).toBeVisible();

      await publish.click();
      await expect(page.locator('.notice')).toContainText('Published atomically: development');

      await page.getByRole('button', { name: new RegExp(`${seed.matrixRequired} in production:`) }).click();
      const editor = page.getByRole('dialog');
      await editor.getByLabel('production value').fill(`required-${testInfo.project.name}`);
      await editor.getByRole('button', { name: 'Save 1 draft' }).click();

      await page.getByRole('button', { name: /unpublished edit/ }).click();
      const repairedSheet = page.getByRole('region', { name: 'Publish drafts' });
      await expect(repairedSheet.getByText('PROTECTED — confirms before publish')).toBeVisible();
      const protectedConfirmation = repairedSheet.getByRole('checkbox', {
        name: 'I confirm publishing to protected production.',
      });
      const protectedPublish = repairedSheet.getByRole('button', { name: /Publish selected/ });
      await expect(protectedPublish).toBeDisabled();
      await protectedConfirmation.check();
      await protectedPublish.click();
      await expect(page.getByRole('heading', { name: 'publish into · production' })).toBeVisible();
      await expect(page.getByRole('list', { name: 'Keys this decision covers' })).toContainText(
        seed.matrixRequired,
      );
      await page.getByRole('button', { name: 'Use a passkey' }).click();
      await expect(page.locator('.notice')).toContainText('Published atomically: production');
  });

  test('keeps values on their environment IDs after display reorder', async ({ page }) => {
    const environmentsPath =
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/environments`;
    const orderPath = `${environmentsPath}/order`;
    await browserApi(page, 'PUT', orderPath, zEnvironmentList, {
      environment_ids: [seed.prod, seed.dev],
    });

    const readResources = new Set<string>();
    const recordMatrixRead = (request: { method: () => string; url: () => string }) => {
      if (request.method() !== 'GET') return;
      const path = new URL(request.url()).pathname;
      if (
        path === environmentsPath ||
        path === `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys` ||
        path === `/api/v1/orgs/${seed.org}/projects/${seed.project}/key-groups` ||
        path.startsWith(`${environmentsPath}/`)
      ) {
        readResources.add(path);
      }
    };
    page.on('request', recordMatrixRead);

    try {
      await page.reload();
      await expect(page.getByRole('columnheader').nth(1)).toContainText('production');
      await expect(page.getByRole('columnheader').nth(2)).toContainText('development');
      await expect(
        page.getByRole('button', { name: new RegExp(`${seed.matrixRequired} in production:`) }),
      ).toBeVisible();
      await expect(
        page.getByRole('button', { name: new RegExp(`${seed.matrixRequired} in development:`) }),
      ).toBeVisible();

      await page
        .getByRole('button', { name: new RegExp(`${seed.matrixRequired} in production:`) })
        .click();
      const editor = page.getByRole('dialog');
      await editor.getByLabel('production value').fill('identity-check-not-saved');
      await expect(editor.getByLabel('production value')).toHaveValue('identity-check-not-saved');
      await expect(editor.getByLabel('development value')).toHaveCount(0);
      await editor.getByRole('button', { name: 'Edit all environments' }).click();
      await expect(editor.getByLabel('development value')).not.toHaveValue(
        'identity-check-not-saved',
      );
      await editor.getByRole('button', { name: 'Close row editor' }).click();

      // Three project reads plus four existing query families per environment.
      // Config cells do not need a secret-disclosure capability request.
      // Re-keying changes representation, not observer/query count.
      await expect.poll(() => readResources.size).toBe(3 + 4 * 2);
    } finally {
      page.off('request', recordMatrixRead);
      await browserApi(page, 'PUT', orderPath, zEnvironmentList, {
        environment_ids: [seed.dev, seed.prod],
      });
    }
  });

  test('uses environment visibility and collapsible-group density valves', async ({ page }) => {
    const chooser = page.locator('.matrix__environment-picker');
    await chooser.locator('summary').click();
    await chooser.getByText('production', { exact: true }).click();
    await expect(chooser.locator('summary')).toContainText('envs 1/2');
    await expect(page.getByRole('columnheader', { name: 'production' })).toHaveCount(0);
    await expect(chooser.getByRole('checkbox', { name: 'development' })).toBeDisabled();

    await chooser.getByText('production', { exact: true }).click();
    await expect(chooser.locator('summary')).toContainText('envs 2/2');
    await chooser.locator('summary').click();

    const group = page.locator('.matrix__group-row button', { hasText: 'app' });
    await group.click();
    await expect(group).toHaveAttribute('aria-expanded', 'false');
    await expect(group).toContainText('LOG_LEVEL');
    await group.click();
    await expect(group).toHaveAttribute('aria-expanded', 'true');
  });

  test('refreshes a mounted copy destination and keeps secret work in the cell modal', async ({ passkeyPage: page }, testInfo) => {
      // Both cells are already mounted. The destination starts warm with its
      // pre-copy value, so only successful destination invalidation can replace
      // it inside React Query's five-second freshness window.
      await expect(
        page.getByRole('button', { name: new RegExp(`${seed.config} in production:`) }),
      ).toBeVisible();
      await page
        .getByRole('button', { name: new RegExp(`${seed.config} in development:`) })
        .click();
      const editor = page.getByRole('dialog');
      await editor.getByRole('button', { name: 'Copy published development value to…' }).click();
      await editor.getByRole('checkbox', { name: 'production · protected' }).check();

      const copy = editor.getByRole('button', { name: 'Copy to 1 environment' });
      await expect(copy).toBeDisabled();
      await editor
        .getByRole('checkbox', { name: 'I confirm copying into protected production.' })
        .check();
      await expect(copy).toBeEnabled();
      await copy.click();
      await expect(
        page.getByRole('heading', { name: 'publish into · production' }),
      ).toBeVisible();
      await expect(page.getByRole('list', { name: 'Keys this decision covers' })).toContainText(
        seed.config,
      );
      await page.getByRole('button', { name: 'Use a passkey' }).click();
      await expect(page.locator('.notice')).toContainText(
        `${seed.config} copied to 1 environment`,
      );
      await expect(
        page.getByRole('button', {
          name: `${seed.config} in production: selective-${testInfo.project.name}`,
        }),
      ).toBeVisible();

      const secret = seed.secrets[0] ?? '';
      await page
        .getByRole('button', { name: new RegExp(`${secret} in development:`) })
        .click();
      const secretEditor = page.getByRole('dialog');
      await expect(secretEditor.getByRole('button', { name: `Reveal ${secret}` })).toBeVisible();
      await expect(secretEditor.getByRole('button', { name: `Copy ${secret}` })).toBeVisible();
      await expect(secretEditor.getByRole('button', { name: 'Edit all environments' })).toBeVisible();
      await expect(secretEditor.getByRole('link', { name: 'Open Values' })).toHaveCount(0);
      await secretEditor.getByRole('button', { name: `Reveal ${secret}` }).click();
      await expect(page.getByRole('heading', { name: 'reveal · development' })).toBeVisible();
      await expect(page.getByRole('list', { name: 'Keys this decision covers' })).toContainText(secret);
      await page.getByRole('button', { name: 'Use a passkey' }).click();
      const revealed = secretEditor.getByLabel(`${secret} revealed`);
      await expect(revealed).toBeVisible();
      await expect(revealed).toHaveCount(0, { timeout: 12_000 });
      await secretEditor.getByRole('button', { name: 'Close row editor' }).click();

      await page
        .getByRole('button', { name: new RegExp(`${secret} in production:`) })
        .click();
      const productionSecretEditor = page.getByRole('dialog');
      await productionSecretEditor.getByRole('button', { name: `Copy ${secret}` }).click();
      await expect(page.getByRole('heading', { name: 'copy to clipboard · production' })).toBeVisible();
      await page.getByRole('button', { name: 'Use a passkey' }).click();
      await expect(productionSecretEditor.getByRole('status')).toContainText(
        'Copied, and recorded as a disclosure',
      );
  });

  test('edits and publishes from the matrix at desktop and 375px mobile', async ({ passkeyPage: page }, testInfo) => {
      if (testInfo.project.name === 'mobile') {
        await page.setViewportSize({ width: 375, height: 812 });
      }

      await page.getByRole('button', { name: /^LOG_LEVEL in development:/ }).click();
      const editor = page.getByRole('dialog');
      await expect(editor).toBeVisible();
      await expect(editor).toContainText('Updated by');
      await expect(editor).toContainText('Revision');
      await expect(editor.getByText('PROTECTED', { exact: true })).toHaveCount(0);
      await editor.getByRole('button', { name: 'Edit all environments' }).click();
      await expect(editor.getByText('PROTECTED', { exact: true })).toBeVisible();
      const firstEditorRow = editor.locator('.matrix-row-editor__row').first();
      await expectPinnedAssertionSet(page, {
        flow: 'matrix',
        surface: 'matrix',
        theme: `${testInfo.project.name}-row-editor`,
        text: [editor.getByRole('heading', { name: 'LOG_LEVEL' }), firstEditorRow],
        radii: [
          [editor, 'container'],
          [firstEditorRow, 'control'],
          [editor.getByLabel('development value'), 'control'],
        ],
        fonts: [
          [editor.getByRole('heading', { name: 'LOG_LEVEL' }), 'mono'],
          [editor.getByLabel('development value'), 'mono'],
        ],
        colours: [
          [editor.getByRole('heading', { name: 'LOG_LEVEL' }), 'color', '--tx'],
          [firstEditorRow, 'borderTopColor', '--line'],
        ],
        hairlines: [firstEditorRow],
        density: [[editor.getByRole('button', { name: 'Close row editor' }), '--touch']],
      });

      const value = `matrix-${testInfo.project.name}`;
      await editor.getByLabel('Fill all environments').fill(`  ${value}  `);
      await editor.getByRole('button', { name: 'Fill all', exact: true }).click();
      await expect(editor.getByLabel('development value')).toHaveValue(`  ${value}  `);
      await editor.getByLabel('production value').fill(`\t${value}-production `);
      if (testInfo.project.name === 'mobile') {
        const box = await editor.boundingBox();
        expect(box).not.toBeNull();
        expect(Math.abs((box?.y ?? 0) + (box?.height ?? 0) / 2 - 812 / 2)).toBeLessThan(8);
        expect(
          await editor.evaluate((element) =>
            Number.parseFloat(getComputedStyle(element).borderBottomLeftRadius),
          ),
        ).toBeGreaterThan(0);
      }
      await editor.getByRole('button', { name: 'Save 2 drafts' }).click();
      await expect(page.locator('.notice')).toContainText('2 drafts updated for LOG_LEVEL');
      await expect(page.locator('.notice')).toContainText('whitespace was removed from 2 values');
      await expect(page.getByRole('button', { name: /LOG_LEVEL in development:.*draft set/ })).toBeVisible();

      await page.getByRole('button', { name: /^LOG_LEVEL in development:/ }).click();
      const reopened = page.getByRole('dialog');
      await expect(reopened.getByLabel('development value')).toHaveValue(value);
      await reopened.getByRole('button', { name: 'Edit all environments' }).click();
      await expect(reopened.getByLabel('production value')).toHaveValue(`${value}-production`);
      await reopened.getByRole('button', { name: 'Close row editor' }).click();

      await page.reload();

      const review = page.getByRole('button', { name: /unpublished edit/ });
      await review.click();
      const publishSheet = page.getByRole('region', { name: 'Publish drafts' });
      await expect(publishSheet).toContainText(value);
      await expect(publishSheet).toContainText(`${value}-production`);
      const atomicPublish = publishSheet.getByRole('button', { name: /Publish selected/ });
      await expect(atomicPublish).toBeDisabled();
      await publishSheet.getByRole('checkbox', {
        name: 'I confirm publishing to protected production.',
      }).check();
      await atomicPublish.click();
      await expect(page.getByRole('heading', { name: 'publish into · production' })).toBeVisible();
      await page.getByRole('button', { name: 'Use a passkey' }).click();
      await expect(page.locator('.notice')).toContainText(/Published atomically: .*Signals updated/);
      await expect(page.getByRole('button', { name: /LOG_LEVEL in development:/ })).not.toHaveAccessibleName(/draft set/);
      // env-matrix 31: the changed state is a bare `Δ` mark; its revision moved
      // off the row text and into the cell's accessible name.
      await expect(page.getByRole('button', { name: /LOG_LEVEL in development:/ })).toHaveAccessibleName(/changed in r/);
  });

  for (const surface of surfacesForFlow('matrix')) {
    for (const scheme of SCHEMES) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        await page.goto(MATRIX_PATH);

        const heading = page.getByRole('heading', { name: 'Environment matrix', level: 1 });
        const sidebar = page.locator('.sidebar');
        const groupRow = page.locator('.project-sidebar__group').first();
        const chooser = page.locator('.matrix__environment-picker summary');
        const key = page.locator('.matrix__key').first();
        const cell = page.locator('.matrix-cell').first();

        await expectPinnedAssertionSet(page, {
          flow: 'matrix',
          surface: surface.id,
          theme: scheme,
          text: [heading, key, cell],
          radii: [
            [chooser, 'control'],
            [cell, 'control'],
          ],
          fonts: [
            [heading, 'ui'],
            [key, 'mono'],
            [cell, 'mono'],
          ],
          colours: [
            [heading, 'color', '--tx'],
            [sidebar, 'backgroundColor', '--bg-panel'],
          ],
          hairlines: [],
          density: [[chooser, '--touch']],
        });
        // Sidebar treatment e draws the hairline on each ROW, so the row is
        // where the rule has to be — a border on the list around them would
        // satisfy a container assertion while the active row could not own its
        // own segment of the line.
        expect(await groupRow.evaluate((element) => getComputedStyle(element).borderLeftWidth)).toBe('1px');
      });
    }
  }

  /**
   * The advisory stream (#510). These sit after the edit/publish legs so the
   * fixture tenant is in its settled shape: one healthy stream replaces the
   * 2s-per-environment signals poll, the poll returns only while the stream
   * is not healthy, and a publish on a SECOND tab reaches the first without a
   * reload — which is the whole point of demoting the poll.
   */

  test('holds one advisory stream, drops the fallback poll while healthy, and closes it on route leave', async ({ page }) => {
    const eventsPath = `/api/v1/orgs/${seed.org}/projects/${seed.project}/events`;
    const signals: string[] = [];
    let eventsResponses = 0;
    let eventsFailedCount = 0;
    page.on('request', (request) => {
      if (new URL(request.url()).pathname.endsWith('/signals')) {
        signals.push(request.url());
      }
    });
    page.on('response', (response) => {
      if (new URL(response.url()).pathname === eventsPath && response.status() === 200) {
        eventsResponses += 1;
      }
    });
    page.on('requestfailed', (request) => {
      if (new URL(request.url()).pathname === eventsPath) {
        eventsFailedCount += 1;
      }
    });

    // A fresh navigation under THESE listeners: the stream this test reasons
    // about is the one it can see open.
    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
    // Exactly one stream, and it actually opened: the 200 carries the retry
    // hint the client's reconnection cadence then honours.
    await expect
      .poll(() => eventsResponses, { timeout: 15_000 })
      .toBe(1);

    // Idle window longer than three fallback cadences: a healthy stream holds
    // ZERO periodic signals requests — the failure mode #510 exists to fix.
    const before = signals.length;
    await page.waitForTimeout(7_000);
    expect(signals.slice(before).length).toBe(0);

    // Leaving the matrix is the subscription's whole teardown: the stream's
    // request is aborted, not left open behind the route. A client-side link,
    // so this proves the React cleanup and not just a document teardown —
    // matrix/history renders the same Matrix with its drawer open and
    // deliberately keeps the stream, so the target is another surface.
    const menu = page.getByRole('button', { name: 'Menu' });
    if (await menu.isVisible()) {
      await menu.click();
    }
    await page.getByRole('link', { name: 'Members & access', exact: true }).click();
    await expect(page).toHaveURL(/\/members/);
    await expect
      .poll(() => eventsFailedCount, { timeout: 10_000 })
      .toBeGreaterThan(0);
  });

  test('activates the 2s fallback poll when the stream cannot connect and stops it on recovery', async ({ page }) => {
    const eventsPath = `/api/v1/orgs/${seed.org}/projects/${seed.project}/events`;
    await page.route('**/api/v1/orgs/*/projects/*/events', (route) => route.abort());
    // The reload is what puts the block in front of THIS page's stream: the
    // beforeEach navigation already opened one before the route existed.
    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
    await page.waitForTimeout(1_500); // the mount-time signals fetches land

    // With the stream refused, the fallback owns freshness: signals requests
    // keep arriving at the fallback cadence.
    let signalsCount = 0;
    page.on('request', (request) => {
      if (new URL(request.url()).pathname.endsWith('/signals')) {
        signalsCount += 1;
      }
    });
    await page.waitForTimeout(6_000);
    expect(signalsCount).toBeGreaterThanOrEqual(2);

    // Recovery: unblock the stream; the client's retry loop reconnects, the
    // stream goes healthy, and the fallback falls silent again.
    await page.unroute('**/api/v1/orgs/*/projects/*/events');
    await page.waitForResponse(
      (response) => new URL(response.url()).pathname === eventsPath && response.status() === 200,
      { timeout: 20_000 },
    );
    await page.waitForTimeout(1_000); // let the healthy transition land
    const atRecovery = signalsCount;
    await page.waitForTimeout(6_000);
    expect(signalsCount).toBe(atRecovery);
  });

  test('delivers another session\u2019s publish to an idle matrix without a reload', async ({ passkeyPage: page }, testInfo) => {
    const eventsPath = `/api/v1/orgs/${seed.org}/projects/${seed.project}/events`;
    // Register the listener BEFORE the navigation: the stream can open before
    // the heading settles, and the 200 below must be THIS page's own stream —
    // the idle assertion at the end is about live delivery, not a stale poll.
    let streamOpen = false;
    page.on('response', (response) => {
      if (new URL(response.url()).pathname === eventsPath && response.status() === 200) {
        streamOpen = true;
      }
    });
    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
    await expect
      .poll(() => streamOpen, { timeout: 15_000 })
      .toBe(true);

    // A second page in the same context is a second browser session over the
    // same signed-in identity. It stages and publishes through the ordinary
    // matrix UI — development only, so no protected ceremony is involved —
    // while the first page sits idle on the same surface.
    const observer = await page.context().newPage();
    try {
      await observer.goto(MATRIX_PATH);
      await expect(observer.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();

      const value = `live-${testInfo.project.name}`;
      await observer.getByRole('button', { name: /^LOG_LEVEL in development:/ }).click();
      const editor = observer.getByRole('dialog');
      await editor.getByLabel('development value').fill(value);
      await editor.getByRole('button', { name: 'Save 1 draft' }).click();
      await expect(observer.locator('.notice')).toContainText(`1 draft updated for LOG_LEVEL`);

      await observer.getByRole('button', { name: /unpublished edit/ }).click();
      const publishSheet = observer.getByRole('region', { name: 'Publish drafts' });
      const publish = publishSheet.getByRole('button', { name: /Publish selected/ });
      await expect(publish).toBeEnabled();
      await publish.click();
      await expect(observer.locator('.notice')).toContainText(/Published atomically/);

      // The idle page repaints the published cell from the advisory events —
      // the signals fallback poll is not running, so nothing else could have
      // brought the new value in.
      await expect(page.getByRole('button', { name: /LOG_LEVEL in development:/ })).toContainText(
        value,
        { timeout: 10_000 },
      );
    } finally {
      await observer.close();
    }
  });
});

/**
 * Flow: catalogue declaration detail (registry surface `key-detail`, #491).
 *
 * It rides THIS spec file rather than its own by necessity, not preference: the
 * merge gate loads `ci.yml` from the base branch (`ci-control.yml` is
 * `pull_request_target`), so the per-group spec lists it runs are the base
 * branch's, and a spec a PR adds to a group never executes on that PR — its
 * pinned claims would then be forever unmet and web-closure would fail. The
 * matrix spec is already in a group, and its FILE content comes from the PR
 * checkout, so the surface's pinned set runs today. See e2e/registry.ts.
 *
 * Clicking a key name opens the routable, reload-safe declaration detail; it
 * renders the whole declaration without a value, edits its metadata through the
 * shared mutation path, survives a reload, and keeps a stale link recoverable.
 * A dedicated config key per viewport project keeps the seeded catalogue the
 * other flows assert on untouched.
 */
const zCreatedKey = z.object({ id: z.string() });
// AWS's own documented EXAMPLE access-key id: matched by the aws-access-token
// rule and non-live by construction (same canary the S1 scanning flow uses).
const CANARY = 'AKIAIOSFODNN7EXAMPLE';
const zKeyRead = z.object({
  name: z.string(),
  group_id: z.string(),
  declaration: z.object({
    rule: z
      .object({ min_length: z.number().optional(), pattern: z.string().optional() })
      .optional(),
  }),
  presence: z.object({
    required_in: z.object({ mode: z.string() }),
    forbidden_in: z.object({ mode: z.string() }),
  }),
});
function keyDetailKeyName(projectName: string): string {
  return `CATALOGUE_${projectName.toUpperCase()}`;
}

test.describe('catalogue declaration detail', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  let keyId = '';

  test.beforeAll(async ({}, testInfo) => {
    const token = await fixtureBearer('key-detail fixture');
    const created = await fixtureApiCall(
      token,
      'POST',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys`,
      zCreatedKey,
      {
        name: keyDetailKeyName(testInfo.project.name),
        classification: 'config',
        folder_path: 'catalogue',
        description: '',
        declaration: { rule: { type: 'string' } },
      },
    );
    keyId = created.id;
  });

  test.afterAll(async () => {
    if (keyId === '') return;
    const token = await fixtureBearer('key-detail cleanup');
    await fixtureApiCall(
      token,
      'DELETE',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys/${keyId}`,
      z.object({}),
    );
  });

  test('opens from the key name, inspects, edits, and survives reload', async ({
    page,
  }, testInfo) => {
    const keyName = keyDetailKeyName(testInfo.project.name);
    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();

    // The key name opens the declaration detail (not the history it used to).
    await page.getByRole('link', { name: `Declaration of ${keyName}` }).click();
    await expect(page).toHaveURL(new RegExp(`/matrix/keys/${keyId}$`));
    await expect(page.getByRole('heading', { name: keyName, level: 2 })).toBeVisible();

    // Every organisation and declaration field is legible; no value appears.
    const panel = page.locator('.key-detail');
    await expect(panel).toContainText('config');
    await expect(panel).toContainText('catalogue');
    await expect(panel).toContainText('Value rules');
    await expect(panel).toContainText('Presence');

    // The one write: edit the description, save, and see it survive a reload —
    // the surface is addressed by the key id, not its name.
    const description = 'Feature flag catalogue entry';
    await panel.getByLabel('Description').fill(description);
    await panel.getByRole('button', { name: 'Save declaration' }).click();
    await expectStatusIsTextAndAria(page, page.getByRole('status').filter({ hasText: 'Saved.' }));

    await page.reload();
    await expect(page.locator('.key-detail')).toContainText(description);
    await expect(page.getByLabel('Description')).toHaveValue(description);

    // Per-key revision history stays one gesture deeper; Close returns to the
    // matrix.
    await expect(page.getByRole('link', { name: /revision history/i })).toBeVisible();
    await page.getByRole('link', { name: 'Close key declaration' }).click();
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
  });

  test('edits value rules and presence, previews impact, and blocks a scanned pattern', async ({
    page,
  }) => {
    const keyPath = `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys/${keyId}`;
    const token = await fixtureBearer('rules edit');
    await page.goto(`/orgs/${seed.org}/projects/${seed.project}/matrix/keys/${keyId}`);
    const panel = page.locator('.key-detail');
    await expect(panel.getByRole('heading', { name: 'Edit value rules & presence' })).toBeVisible();

    // A valid rule edit commits and is durable.
    await panel.getByLabel('Minimum length').fill('3');
    await panel.getByRole('button', { name: 'Save value rules & presence' }).click();
    await expect(panel.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
    expect((await fixtureApiCall(token, 'GET', keyPath, zKeyRead)).declaration.rule?.min_length).toBe(3);

    // Criterion 3: the before/after impact names the affected environments.
    await panel.getByLabel('Required in').selectOption('all');
    await expect(panel.locator('.key-detail__presence-impact')).toContainText('Newly required in');
    await panel.getByLabel('Required in').selectOption('none');

    // Commit a value-safe presence change (the key holds no values, so
    // forbidding it everywhere is satisfiable) and verify it persisted through
    // the API — the client preview alone would not prove the write carried it.
    await panel.getByLabel('Forbidden in').selectOption('all');
    await panel.getByRole('button', { name: 'Save value rules & presence' }).click();
    await expect(panel.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
    expect((await fixtureApiCall(token, 'GET', keyPath, zKeyRead)).presence.forbidden_in.mode).toBe('all');
    await panel.getByLabel('Forbidden in').selectOption('none');
    await panel.getByRole('button', { name: 'Save value rules & presence' }).click();
    await expect(panel.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();

    // #183/#493: a credential-shaped pattern is blocked; overriding commits it.
    const consoleLines: string[] = [];
    page.on('console', (message) => consoleLines.push(message.text()));
    page.on('pageerror', (error) => consoleLines.push(String(error)));
    await panel.getByLabel('Pattern (RE2, anchored)').fill(CANARY);
    await panel.getByRole('button', { name: 'Save value rules & presence' }).click();
    const block = page.locator('dialog.scan-block');
    await expect(block).toBeVisible();
    await expect(block.getByText('aws-access-token')).toBeVisible();
    // SS4: the canary reaches neither the dialog markup nor the console.
    expect(await block.evaluate((el) => el.outerHTML)).not.toContain(CANARY);
    expect(consoleLines.filter((line) => line.includes(CANARY))).toEqual([]);
    await block.getByRole('button', { name: 'Acknowledge and continue' }).click();
    await expect(block).toHaveCount(0);
    expect((await fixtureApiCall(token, 'GET', keyPath, zKeyRead)).declaration.rule?.pattern).toBe(CANARY);
  });

  test('sets and clears the key’s group membership', async ({ page }, testInfo) => {
    const token = await fixtureBearer('group membership');
    const group = await fixtureApiCall(
      token,
      'POST',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/key-groups`,
      zCreatedKey,
      { name: `catgrp-${testInfo.project.name}` },
    );
    const keyPath = `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys/${keyId}`;
    try {
      await page.goto(`/orgs/${seed.org}/projects/${seed.project}/matrix/keys/${keyId}`);
      const groupSelect = page.getByLabel('Key group');
      await groupSelect.selectOption(group.id);
      await expect(page.getByRole('status').filter({ hasText: 'Group updated.' })).toBeVisible();
      expect((await fixtureApiCall(token, 'GET', keyPath, zKeyRead)).group_id).toBe(group.id);

      await groupSelect.selectOption('');
      await expect(page.getByRole('status').filter({ hasText: 'Group updated.' })).toBeVisible();
      expect((await fixtureApiCall(token, 'GET', keyPath, zKeyRead)).group_id).toBe('');
    } finally {
      await fixtureApiCall(
        token,
        'DELETE',
        `/api/v1/orgs/${seed.org}/projects/${seed.project}/key-groups/${group.id}`,
        z.object({}),
      );
    }
  });

  test('manages folder and key-group lifecycle from the matrix', async ({ page }, testInfo) => {
    const suffix = testInfo.project.name.toLowerCase();
    const folderPath = `catflow-${suffix}`;
    const groupName = `catflow ${suffix}`;
    await page.goto(MATRIX_PATH);
    await page.getByRole('button', { name: 'Folders & groups' }).click();
    const dialog = page.locator('dialog.catalogue-manage');
    await expect(dialog.getByRole('heading', { name: 'Folders & groups' })).toBeVisible();

    await dialog.getByLabel('New folder path').fill(folderPath);
    await dialog.getByRole('button', { name: 'Add folder' }).click();
    await expect(dialog.getByText(`Folder ${folderPath} created.`)).toBeVisible();

    await dialog.getByLabel('New group name').fill(groupName);
    await dialog.getByRole('button', { name: 'Add group' }).click();
    await expect(dialog.getByText(`Group ${groupName} created.`)).toBeVisible();

    // Clean up through the same lifecycle controls; a row is addressed by its
    // input's accessible name (its identity lives in an input value).
    const folderInput = dialog.getByLabel(`Folder path for ${folderPath}`);
    await folderInput.locator('xpath=ancestor::li').getByRole('button', { name: 'Delete' }).click();
    await folderInput
      .locator('xpath=ancestor::li')
      .getByRole('button', { name: 'Confirm delete' })
      .click();
    await expect(folderInput).toHaveCount(0);

    const groupInput = dialog.getByLabel(`Name for group ${groupName}`);
    await groupInput.locator('xpath=ancestor::li').getByRole('button', { name: 'Delete' }).click();
    await groupInput
      .locator('xpath=ancestor::li')
      .getByRole('button', { name: 'Confirm delete' })
      .click();
    await expect(groupInput).toHaveCount(0);
    await expect(dialog).toBeVisible();
  });

  test('keeps a stale or missing key recoverable', async ({ page }) => {
    // A well-formed key id that does not exist — a link that outlived its key.
    const missing = 'key_01890000-0000-7000-8000-000000000000';
    await page.goto(`/orgs/${seed.org}/projects/${seed.project}/matrix/keys/${missing}`);

    const alert = page.locator('.key-detail .alert');
    await expect(alert).toBeVisible();
    await expect(alert).toHaveAttribute('role', 'alert');

    await page.getByRole('link', { name: 'Back to the matrix' }).click();
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
  });

  const keyDetailSurfaces = surfacesForFlow('key-detail');
  if (keyDetailSurfaces.length !== 1 || keyDetailSurfaces[0] === undefined) {
    throw new Error(
      `the key-detail flow must claim exactly one surface, got ${String(keyDetailSurfaces.length)}`,
    );
  }
  const keyDetailSurface = keyDetailSurfaces[0];

  for (const scheme of SCHEMES) {
    test(`meets the pinned assertion set on Key declaration (${scheme})`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(`/orgs/${seed.org}/projects/${seed.project}/matrix/keys/${keyId}`);

      const heading = page.locator('#key-detail-title');
      await expect(heading).toBeVisible();
      const panel = page.locator('.key-detail');
      const factName = page.locator('.key-detail__fact dt').first();
      const factValue = page.locator('.key-detail__fact dd.mono').first();
      const close = page.locator('.key-detail__close');
      const save = page.getByRole('button', { name: 'Save declaration' });

      await expectPinnedAssertionSet(page, {
        flow: 'key-detail',
        surface: keyDetailSurface.id,
        theme: scheme,
        text: [heading, factName, factValue],
        radii: [
          [close, 'control'],
          [save, 'control'],
        ],
        fonts: [
          [heading, 'mono'],
          [factName, 'ui'],
        ],
        colours: [
          [heading, 'color', '--tx'],
          [panel, 'backgroundColor', '--bg-panel'],
        ],
        hairlines: [],
        density: [[close, '--touch']],
      });
    });
  }
});

/**
 * Flow: the browser key lifecycle — rename, reclassify, delete (#494).
 *
 * Rides this spec for the same merge-gate reason the detail surface does (it
 * reuses the `key-detail` surface). One serial journey over one throwaway config
 * key: rename it (identity survives, the heading follows the new name),
 * reclassify config → secret through its confirm ceremony (tightening needs no
 * reveal), then delete it behind the typed-name confirm and land back on the
 * matrix with no route left pointing at the key. Declassification's reveal gate
 * and its Surface-1 warnings are covered by the component tests, which can drive
 * the refusal shapes deterministically.
 */
test.describe('catalogue declaration lifecycle', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test('renames, reclassifies, and deletes a key from the detail surface', async ({
    page,
  }, testInfo) => {
    const startName = `LIFECYCLE_${testInfo.project.name.toUpperCase()}`;
    const renamed = `${startName}_RENAMED`;
    const token = await fixtureBearer('lifecycle fixture');
    const created = await fixtureApiCall(
      token,
      'POST',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys`,
      zCreatedKey,
      {
        name: startName,
        classification: 'config',
        folder_path: 'lifecycle',
        description: '',
        declaration: { rule: { type: 'string' } },
      },
    );
    const keyId = created.id;
    let deleted = false;
    try {
      await page.goto(`/orgs/${seed.org}/projects/${seed.project}/matrix/keys/${keyId}`);
      const panel = page.locator('.key-detail');
      await expect(page.getByRole('heading', { name: startName, level: 2 })).toBeVisible();

      // Rename: identity is the id, so the URL is unchanged and the heading
      // follows the new name once the query is invalidated. `exact` because
      // getByLabel is case-insensitive substring — plain 'Name' also matches the
      // delete surface's "Confirm the key name to delete it".
      await panel.getByLabel('Name', { exact: true }).fill(renamed);
      await panel.getByRole('button', { name: 'Rename key' }).click();
      await expectStatusIsTextAndAria(
        page,
        page.getByRole('status').filter({ hasText: 'Renamed.' }),
      );
      await expect(page.getByRole('heading', { name: renamed, level: 2 })).toBeVisible();

      // Reclassify config → secret through the confirm ceremony. Tightening
      // discloses nothing and needs no reveal.
      await panel.getByRole('button', { name: 'Reclassify as secret…' }).click();
      const dialog = page.locator('dialog.matrix-editor');
      await expect(dialog).toBeVisible();
      await dialog.getByRole('button', { name: 'Reclassify as secret', exact: true }).click();
      await expectStatusIsTextAndAria(
        page,
        page.getByRole('status').filter({ hasText: 'Reclassified as secret.' }),
      );
      await expect(panel).toContainText('🔒 secret');

      // Delete behind the typed-name confirm, then land on the matrix with the
      // key route gone.
      await panel.getByLabel('Confirm the key name to delete it').fill(renamed);
      await panel.getByRole('button', { name: 'Delete key' }).click();
      await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
      await expect(page).not.toHaveURL(new RegExp(`/matrix/keys/${keyId}$`));
      deleted = true;
    } finally {
      if (!deleted) {
        const cleanup = await fixtureBearer('lifecycle cleanup');
        await fixtureApiCall(
          cleanup,
          'DELETE',
          `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys/${keyId}`,
          z.object({}),
        ).catch(() => undefined);
      }
    }
  });
});

/**
 * Flow: declaring a new key from the browser (#492).
 *
 * Runs after the edit/publish suite and as the last describe in the file, so
 * the persistent catalogue write it makes cannot perturb an earlier
 * assertion — the leg's other specs run before matrix.spec, and this block runs
 * after every `environment matrix` test. It declares a config key with a value
 * rule and presence, opens a first value, and proves the value entered the
 * draft workflow with the declaration.
 */
test.describe('environment matrix declaration', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test('declares a key with a rule and first value, and stages that value as a draft', async ({
    passkeyPage: page,
  }) => {
    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();

    await page.getByRole('button', { name: '+ New key' }).click();
    const modal = page.getByRole('dialog');
    await expect(modal).toBeVisible();

    await modal.getByLabel('Group').fill('features');
    await modal.getByLabel('Key name').fill('FEATURE_ENABLED');
    await modal.getByLabel('Type').selectOption('boolean');
    await modal.getByLabel('First value (optional)').fill('true');
    // Presence stays at the default `none`: declaring `required_in` an
    // environment that has no value yet is a server veto (required + absent),
    // which the modal surfaces recoverably — its own test below. This journey
    // proves the declaration + first-value draft path succeeds end to end.

    await modal.getByRole('button', { name: 'Declare' }).click();

    await expect(page.locator('.notice')).toContainText(
      'Declared FEATURE_ENABLED with a draft value in 1 environment',
    );
    // The declared key's cell now carries its opening draft — proof the value
    // entered the draft workflow with the declaration.
    await expect(
      page.getByRole('button', { name: /FEATURE_ENABLED in development:.*draft set/ }),
    ).toBeVisible();
  });

  test('rejects a first value that cannot be the declared type before any write', async ({
    passkeyPage: page,
  }) => {
    await page.goto(MATRIX_PATH);
    await page.getByRole('button', { name: '+ New key' }).click();
    const modal = page.getByRole('dialog');
    await expect(modal).toBeVisible();

    await modal.getByLabel('Group').fill('features');
    await modal.getByLabel('Key name').fill('RETRY_LIMIT');
    await modal.getByLabel('Type').selectOption('integer');
    await modal.getByLabel('First value (optional)').fill('not-a-number');
    await modal.getByRole('button', { name: 'Declare' }).click();

    await expect(modal.getByRole('alert')).toContainText('Enter a base-10 integer.');
    await expect(modal).toBeVisible();
  });

  test('surfaces a required-presence veto recoverably and keeps the form open', async ({
    passkeyPage: page,
  }) => {
    await page.goto(MATRIX_PATH);
    await page.getByRole('button', { name: '+ New key' }).click();
    const modal = page.getByRole('dialog');
    await expect(modal).toBeVisible();

    await modal.getByLabel('Group').fill('features');
    await modal.getByLabel('Key name').fill('REQUIRED_EVERYWHERE');
    await modal.getByLabel('Type').selectOption('string');
    // Symbolic all — required in every environment, current and future — on a
    // key with no values yet: the server vetoes (required + absent). The block
    // is a phase-1 failure, so the intact form stays open for the operator to
    // relax the rule or add values.
    await modal
      .getByRole('group', { name: 'Required in' })
      .getByRole('radio', { name: 'all (current & future)' })
      .check();
    await modal.getByRole('button', { name: 'Declare' }).click();

    await expect(modal.getByRole('alert')).toContainText('required_in');
    await expect(modal).toBeVisible();
    await expect(modal.getByLabel('Key name')).toHaveValue('REQUIRED_EVERYWHERE');
  });
});

/**
 * Flow tail: the browser dotenv import wizard (#495).
 *
 * A local .env file is read in the browser, its one new key is classified and
 * typed explicitly, and — targeting only the unprotected development
 * environment so no step-up is needed — the reviewed import declares the key
 * and publishes its value. The result names what landed, and the value is a
 * live (published) matrix cell, not a draft.
 */
test.describe('environment matrix dotenv import', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test('imports a new config key into development and publishes its value', async ({
    passkeyPage: page,
  }) => {
    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();

    await page.getByRole('button', { name: 'Import', exact: true }).click();
    const modal = page.getByRole('dialog');
    await expect(modal).toBeVisible();

    // The wizard opens on the source picker (#496); choose the .env journey.
    await modal.getByRole('button', { name: /\.env file/ }).click();

    // The file is read locally; nothing is sent yet.
    await modal.getByLabel('Dotenv file').setInputFiles({
      name: 'app.env',
      mimeType: 'text/plain',
      buffer: Buffer.from('IMPORTED_FLAG=true\n'),
    });
    await expect(modal.getByText('1 value read')).toBeVisible();

    // Target only development; production is protected and would need step-up.
    await modal.getByRole('checkbox', { name: 'production' }).uncheck();
    await modal.getByRole('button', { name: 'Review', exact: true }).click();

    // Classify the new key: config (not secret) so its value shows in the cell.
    await modal.getByRole('checkbox', { name: 'secret' }).uncheck();
    await expect(modal.getByText('Suggested: boolean')).toBeVisible();
    await modal.getByLabel('Type').selectOption('boolean');
    await modal.getByRole('button', { name: 'Review changes' }).click();

    await expect(modal.getByText('1 to import, 1 new key declared')).toBeVisible();
    await modal.getByRole('button', { name: 'Import', exact: true }).click();

    await expect(modal.getByText('Declared 1 new key: IMPORTED_FLAG')).toBeVisible();
    await expect(modal.getByRole('list', { name: 'Import results' })).toContainText('imported 1');
    await modal.getByRole('button', { name: 'Done' }).click();
    await expect(modal).toBeHidden();

    // The imported value is a live published cell, not a draft.
    await expect(
      page.getByRole('button', { name: /IMPORTED_FLAG in development:/ }),
    ).toBeVisible();
  });
});

/**
 * Flow tail: a browser file-mode connector import (#496).
 *
 * A local Kubernetes Secret manifest is read and parsed in the browser (the same
 * strict mapping the Go connector uses), its one new key classified and declared,
 * and its base64-decoded value published into the unprotected development
 * environment through the SAME review/apply flow as the .env journey. This is the
 * connector half of the shared UI acceptance criterion.
 */
test.describe('environment matrix kubernetes import', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test('imports a Kubernetes Secret entry into development and publishes its value', async ({
    passkeyPage: page,
  }) => {
    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();

    await page.getByRole('button', { name: 'Import', exact: true }).click();
    const modal = page.getByRole('dialog');
    await expect(modal).toBeVisible();

    await modal.getByRole('button', { name: /Kubernetes Secret manifest/ }).click();

    // A single-Secret manifest maps its entries onto the environment root; the
    // value is `console` (base64 `Y29uc29sZQ==`). Read locally, nothing sent yet.
    await modal.getByLabel('Kubernetes Secret manifest').setInputFiles({
      name: 'webapp.yaml',
      mimeType: 'text/plain',
      buffer: Buffer.from(
        'apiVersion: v1\nkind: Secret\nmetadata:\n  name: webapp\ndata:\n  K8S_TOKEN: Y29uc29sZQ==\n',
      ),
    });
    await expect(modal.getByText('1 value read')).toBeVisible();

    await modal.getByRole('checkbox', { name: 'production' }).uncheck();
    await modal.getByRole('button', { name: 'Review', exact: true }).click();

    // Classify config so the value shows in the cell (K8s defaults to secret).
    await modal.getByRole('checkbox', { name: 'secret' }).uncheck();
    await modal.getByRole('button', { name: 'Review changes' }).click();

    await expect(modal.getByText('1 to import, 1 new key declared')).toBeVisible();
    await modal.getByRole('button', { name: 'Import', exact: true }).click();

    await expect(modal.getByText('Declared 1 new key: K8S_TOKEN')).toBeVisible();
    await expect(modal.getByRole('list', { name: 'Import results' })).toContainText('imported 1');
    await modal.getByRole('button', { name: 'Done' }).click();
    await expect(modal).toBeHidden();

    await expect(page.getByRole('button', { name: /K8S_TOKEN in development:/ })).toBeVisible();
  });
});
