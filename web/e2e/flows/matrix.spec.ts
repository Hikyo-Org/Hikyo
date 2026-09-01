import { expect, type Locator, type Page } from '@playwright/test';
import { zEnvironmentList } from '@hikyo/zod';

import {
  expectPinnedAssertionSet,
  expectStatusIsTextAndAria,
} from '../fixtures/assertions.ts';
import { browserApi } from '../fixtures/api.ts';
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
    // Required everywhere, including environments created later — the symbolic
    // mode, not "tick every box".
    await modal.getByRole('radio', { name: 'all (current & future)' }).check();

    await modal.getByRole('button', { name: 'Declare' }).click();

    await expect(page.locator('.notice')).toContainText(
      'Declared FEATURE_ENABLED with a draft value in 1 environment',
    );
    await expect(page.getByRole('button', { name: /features\/.*1 key/ })).toBeVisible();
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
});
