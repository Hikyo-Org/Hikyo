import { expect, test } from '@playwright/test';
import { z } from 'zod';

import { expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import { fixtureApiCall, fixtureBearer } from '../fixtures/api.ts';
import { readSeed, STORAGE_STATE } from '../fixtures/instance.ts';
import { surfacesForFlow } from '../registry.ts';

/**
 * Flow: catalogue declaration detail (registry surface `key-detail`, #491).
 *
 * Clicking a key name opens a routable, reload-safe surface that renders the
 * whole declaration without a value, edits its metadata through the shared
 * mutation path, survives a reload, and keeps a stale link recoverable. A
 * dedicated config key per viewport project keeps the seeded catalogue the
 * other flows assert on untouched.
 */

const seed = readSeed();
const MATRIX_PATH = `/orgs/${seed.org}/projects/${seed.project}/matrix`;
const SCHEMES: readonly ('dark' | 'light')[] = ['dark', 'light'];

const zCreatedKey = z.object({ id: z.string() });

/** Per-viewport-project key: the two projects share one instance. */
function keyNameFor(projectName: string): string {
  return `CATALOGUE_${projectName.toUpperCase()}`;
}

let keyId = '';

test.describe('catalogue declaration detail', () => {
  test.describe.configure({ mode: 'serial' });
  test.use({ storageState: STORAGE_STATE });

  test.beforeAll(async ({}, testInfo) => {
    const token = await fixtureBearer('key-detail fixture');
    const created = await fixtureApiCall(
      token,
      'POST',
      `/api/v1/orgs/${seed.org}/projects/${seed.project}/keys`,
      zCreatedKey,
      {
        name: keyNameFor(testInfo.project.name),
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
    const keyName = keyNameFor(testInfo.project.name);
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

    // The one write: edit the description, save, and see the persisted state
    // survive a reload — the surface is addressed by the key id, not its name.
    const description = 'Feature flag catalogue entry';
    await panel.getByLabel('Description').fill(description);
    await panel.getByRole('button', { name: 'Save declaration' }).click();
    await expectStatusIsTextAndAria(page, page.getByRole('status').filter({ hasText: 'Saved.' }));

    await page.reload();
    await expect(page.locator('.key-detail')).toContainText(description);
    await expect(page.getByLabel('Description')).toHaveValue(description);

    // Per-key revision history stays one gesture deeper, and Close returns to
    // the matrix.
    await expect(page.getByRole('link', { name: /revision history/i })).toBeVisible();
    await page.getByRole('link', { name: 'Close key declaration' }).click();
    await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
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

  const surfaces = surfacesForFlow('key-detail');
  if (surfaces.length !== 1 || surfaces[0] === undefined) {
    throw new Error(
      `the key-detail flow must claim exactly one surface, got ${String(surfaces.length)}`,
    );
  }
  const keyDetailSurface = surfaces[0];

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
