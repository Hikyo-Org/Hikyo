import { expect, test } from '@playwright/test';
import { z } from 'zod';

import { expectPinnedAssertionSet } from '../fixtures/assertions.ts';
import { browserApi } from '../fixtures/api.ts';
import { readSeed, STORAGE_STATE } from '../fixtures/instance.ts';
import { surfacesForFlow } from '../registry.ts';

/**
 * Flow: secret-scanning Surface-1 warn dialog on the matrix editing surface
 * (#74, SS2/SS4 [UI]; secret-scanning ADR §§2,4).
 *
 * A config-classified value carrying a credential-shaped string SAVES, the
 * warn never blocks, and the redacted finding rides back into a dialog naming
 * the rule and the key, never the matched text. The two named resolutions are
 * exercised: keep-as-config (a sticky dismissal the identical value no longer
 * trips, while a distinct offending value still does) and reclassify-as-secret
 * (routing the key through secret handling). SS4's non-disclosure invariant is
 * asserted where it is easiest to violate: the planted canary reaches neither
 * the dialog DOM nor the browser console.
 *
 * The canaries are AWS's own documented EXAMPLE access-key ids, syntactically
 * valid, matched by the `aws-access-token` rule, and non-live by construction.
 */

const seed = readSeed();
const MATRIX_PATH = `/orgs/${seed.org}/projects/${seed.project}/matrix`;
const SCHEMES: readonly ('dark' | 'light')[] = ['dark', 'light'];

// Two distinct, syntactically valid, non-live AWS access-key ids.
const CANARY = 'AKIAIOSFODNN7EXAMPLE';
const DISTINCT = 'AKIAI44QH8DHBEXAMPLE';

const zCreatedKey = z.object({ id: z.string() });

test.describe('secret scanning warn dialog', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test('warns, dismisses stickily, re-fires, and reclassifies (SS2/SS4 [UI])', async ({
    page,
  }, testInfo) => {
    // A fresh config key per viewport project: the two projects share one
    // instance, and reclassifying a seeded key to secret would break the
    // sibling matrix flow. The name matches the KeyName grammar (^[A-Z_]…).
    const keyName = `SCAN_${testInfo.project.name.toUpperCase()}`;

    // Capture the console BEFORE anything navigates, so the SS4 sweep sees every
    // message the save could have logged.
    const consoleLines: string[] = [];
    page.on('console', (message) => consoleLines.push(message.text()));
    page.on('pageerror', (error) => consoleLines.push(String(error)));

    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();

    // The matrix has no create-key control; a surface's fixture work goes
    // through the page's own session (browserApi), exactly as other flows do.
    const created = await browserApi(
      page,
      'POST',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys`,
      zCreatedKey,
      { name: keyName, classification: 'config', declaration: { rule: { type: 'string' } } },
    );
    try {
      await page.reload();
      const cell = page.getByRole('button', { name: new RegExp(`${keyName} in development:`) });
      await expect(cell).toBeVisible();

      const warn = page.locator('dialog.scan-warn');

    // --- SS2: plant the credential; the save succeeds and the warn fires -----
      await plantValue(page, cell, CANARY);

      await expect(
        warn.getByRole('heading', { name: 'Possible secret in a config value' }),
      ).toBeVisible();
    // The finding names its rule and offers both first-class resolutions.
      await expect(warn.getByText('aws-access-token')).toBeVisible();
      await expect(
        warn.getByRole('button', { name: `Reclassify ${keyName} as secret` }),
      ).toBeVisible();
      await expect(warn.getByRole('button', { name: 'Keep as config' })).toBeVisible();
    // No blanket ignore-all input exists on the dialog (ADR §4).
      await expect(warn.getByRole('button', { name: /ignore all/i })).toHaveCount(0);

    // --- SS4 [UI]: the canary is nowhere in the dialog, nowhere in the console
      await expect(warn).not.toContainText(CANARY);
      expect(consoleLines.filter((line) => line.includes(CANARY))).toEqual([]);

    // --- SS2: keep-as-config is the sticky dismissal. It re-submits the
    // identical value with the finding's token; the server re-scans that exact
    // value under the fresh dismissal and returns no finding, so the dialog
    // closes. The dialog closing IS "the identical value no longer warns",
    // proven through the real dismissal path rather than a second editor round.
      await warn.getByRole('button', { name: 'Keep as config' }).click();
      await expect(warn).toHaveCount(0);
    // Let the dismissal's re-save cascade (values/signals/pending) settle before
    // reopening the editor: the dev cell now carries a staged draft.
      await expect(cell).toHaveAccessibleName(/draft set/);

    // --- SS2: a distinct offending value re-fires -----------------------------
      await plantValue(page, cell, DISTINCT);
      await expect(
        warn.getByRole('heading', { name: 'Possible secret in a config value' }),
      ).toBeVisible();

    // --- SS2: reclassify-as-secret completes ----------------------------------
      await warn.getByRole('button', { name: `Reclassify ${keyName} as secret` }).click();
      await expect(warn).toHaveCount(0);
    // The key is now secret: the matrix re-fetches and the key row shows the
    // lock. That is reclassification completing, observed end to end.
      await expect(page.locator('.matrix__key').filter({ hasText: keyName })).toContainText('🔒');
    } finally {
      await browserApi(
        page,
        'DELETE',
        `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys/${created.id}`,
        z.null(),
      );
    }
  });

  for (const surface of surfacesForFlow('scanning')) {
    for (const scheme of SCHEMES) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        await page.goto(MATRIX_PATH);

        const heading = page.getByRole('heading', { name: 'Environment matrix', level: 1 });
        const sidebar = page.locator('.sidebar');
        const groupRow = page.locator('.context-sidebar__group').first();
        const chooser = page.locator('.matrix__environment-picker summary');
        const key = page.locator('.matrix__key').first();
        const cell = page.locator('.matrix-cell').first();

        await expectPinnedAssertionSet(page, {
          flow: 'scanning',
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
        // where the rule has to be, a border on the list around them would
        // satisfy a container assertion while the active row could not own its
        // own segment of the line.
        expect(await groupRow.evaluate((element) => getComputedStyle(element).borderLeftWidth)).toBe('1px');
      });
    }
  }
});

/**
 * Flow: secret-scanning Surface-2 BLOCK dialog on the catalogue declaration
 * editor (#183, SS3 [UI]; secret-scanning ADR §4).
 *
 * Unlike the Surface-1 warn, a declaration free-text field is exported to Git
 * and treated as public, so a credential-shaped value in it is REFUSED before
 * any state persists. The block dialog states that consequence, renders only the
 * redacted finding (rule id + locator, never the matched text), and offers one
 * content-bound acknowledgement per finding through a single audited override , 
 * no blanket ignore-all. The planted canary reaches neither the dialog DOM nor
 * the browser console. The named stale/skew/surplus refusals are proven at the
 * unit layer (ScanBlockDialog, KeyDeclarationDetail) and server-side in Go; the
 * modal freezes content at submit, so they are not reachable through this UI.
 */
test.describe('secret scanning block dialog', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test('blocks a public declaration field, acknowledges, and resubmits (SS3 [UI])', async ({
    page,
  }, testInfo) => {
    const keyName = `SCANBLOCK_${testInfo.project.name.toUpperCase()}`;

    const consoleLines: string[] = [];
    page.on('console', (message) => consoleLines.push(message.text()));
    page.on('pageerror', (error) => consoleLines.push(String(error)));

    await page.goto(MATRIX_PATH);
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();

    const created = await browserApi(
      page,
      'POST',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys`,
      zCreatedKey,
      { name: keyName, classification: 'config', description: '', declaration: { rule: { type: 'string' } } },
    );
    try {
      // The declaration detail is routable and reload-safe, address it directly.
      await page.goto(`/orgs/${seed.org}/projects/${seed.project}/matrix/keys/${created.id}`);
      const panel = page.locator('.key-detail');
      await expect(panel.getByRole('heading', { name: keyName, level: 2 })).toBeVisible();

      const block = page.locator('dialog.scan-block');

      // --- SS3: a credential-shaped description is refused, and the block dialog
      // states the exported-as-public consequence --------------------------------
      await panel.getByLabel('Description').fill(CANARY);
      await panel.getByRole('button', { name: 'Save declaration' }).click();

      await expect(
        block.getByRole('heading', { name: 'Declaration blocked by secret scanning' }),
      ).toBeVisible();
      await expect(block).toContainText('exported to Git and treated as public');
      // The finding names its rule; the override is offered once.
      await expect(block.getByText('aws-access-token')).toBeVisible();
      await expect(block.getByRole('button', { name: 'Acknowledge and continue' })).toBeVisible();
      // No blanket ignore-all input exists on the dialog (ADR §4).
      await expect(block.getByRole('button', { name: /ignore all/i })).toHaveCount(0);

      // --- SS4 [UI]: the canary is nowhere in the dialog, nowhere in the console -
      await expect(block).not.toContainText(CANARY);
      expect(consoleLines.filter((line) => line.includes(CANARY))).toEqual([]);

      // --- keyboard: Escape closes the block without submitting ------------------
      await page.keyboard.press('Escape');
      await expect(block).toHaveCount(0);

      // --- SS3: the single audited override acknowledges the finding and the
      // resubmit succeeds --------------------------------------------------------
      await panel.getByRole('button', { name: 'Save declaration' }).click();
      await expect(block).toBeVisible();
      await block.getByRole('button', { name: 'Acknowledge and continue' }).click();
      await expect(block).toHaveCount(0);
      await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();

      // The acknowledged description now stands recorded; the canary is the
      // operator's own input, exactly as the warn test carves out.
      await page.reload();
      await expect(page.getByLabel('Description')).toHaveValue(CANARY);
    } finally {
      await browserApi(
        page,
        'DELETE',
        `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys/${created.id}`,
        z.null(),
      );
    }
  });
});

/** plantValue opens the row editor, stages one development value, and saves. */
async function plantValue(
  page: import('@playwright/test').Page,
  cell: import('@playwright/test').Locator,
  value: string,
): Promise<void> {
  await cell.click();
  const editor = page.locator('dialog.matrix-row-editor');
  await expect(editor).toBeVisible();
  await editor.getByLabel('development value').fill(value);
  const save = editor.getByRole('button', { name: /^Save \d+ draft/ });
  await expect(save).toBeEnabled();
  await save.click();
  // A successful save closes the editor (its findings, if any, open the warn).
  await expect(editor).toHaveCount(0);
}
