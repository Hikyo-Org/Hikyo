import { readFileSync } from 'node:fs';

import { expect, test, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { zScimBinding, zScimBindingList, zServiceAccountList } from '@hikyo/zod';
import { z } from 'zod';

import { expectPinnedAssertionSet, expectStatusIsTextAndAria } from '../fixtures/assertions.ts';
import { browserApi } from '../fixtures/api.ts';
import {
  BASE_URL,
  nextTotpCode,
  readSeed,
  STORAGE_STATE,
} from '../fixtures/instance.ts';
import { surfacesForFlow } from '../registry.ts';

/**
 * Flow: members & grants (registry surface `members`) — mvp-boundary S3's
 * "members (grant modal incl. blast warning + staging default)", against the
 * locked prototype #29 iteration 18.
 *
 * What it proves, in the permission ADR's own terms:
 *
 *  - the membership listing is one line PER CAPABILITY, each carrying its
 *    origin chips, grouped by principal and scope;
 *  - "who can…?" answers by inspection and counts a grant ABOVE the scope
 *    asked about, because grants inherit downward;
 *  - the grant modal's scope select runs narrow to wide, preselects an
 *    environment confirmed unprotected, and puts the protected one last and
 *    never selected;
 *  - an organisation-scoped grant shows its blast radius ENUMERATED — every
 *    project and environment, plus the future ones — and "back, change scope"
 *    keeps the composition, so the warning can be answered rather than only
 *    dismissed;
 *  - each checked capability lands as its own line, and revoking one is
 *    one click with feedback that stays on the page;
 *  - the org rail drives the breadcrumb and the org-scoped surfaces.
 *
 * The session is the shared administrator's: `manage-members` is MFA-mandatory
 * and the shared session is stepped up, and its `manage-members` is held at
 * INSTANCE scope, which reaches this organisation by ordinary downward
 * inheritance.
 */

const seed = readSeed();
/** The seed's safest scope: its one confirmed-unprotected environment named staging. */
const DEFAULT_SCOPE = `env:${seed.history.project}:${seed.history.staging}`;
const PATH = `/orgs/${seed.org}/members`;
const PROJECT_PATH = `${PATH}?project=${seed.project}`;

/**
 * automationPrincipal is the grant target: the seeded `automation` service
 * account.
 *
 * Not the administrator: granting to yourself advances your OWN session
 * generation and kills the session the flow is running in. Not the workload
 * either: its normative allowlist admits `read` alone, so a two-capability
 * composition would be refused for a reason this flow is not about. The
 * automation class may hold read, edit, publish and definitions-edit at
 * project scope or below, which is exactly the composition the modal is for.
 */
async function automationPrincipal(page: Page): Promise<string> {
  const accounts = await browserApi(
    page,
    'GET',
    `/api/v1/orgs/${seed.org}/projects/${seed.project}/service-accounts`,
    zServiceAccountList,
  );
  const account = accounts.items.find((item) => item.name === seed.machine.automation);
  if (account === undefined) {
    throw new Error(`the fixture's ${seed.machine.automation} service account is missing`);
  }
  return account.principal_id;
}

/** revokeAll takes back everything this flow granted, whatever happened. */
async function revokeAll(page: Page, principal: string, capabilities: readonly string[]) {
  for (const capability of capabilities) {
    const query = `principal=${encodeURIComponent(principal)}&capability=${capability}`;
    try {
      await browserApi(
        page,
        'DELETE',
        `/api/v1/orgs/${seed.org}/projects/${seed.project}/environments/${seed.dev}/grants?${query}`,
        z.null(),
      );
    } catch (error) {
      if (error instanceof Error && error.message.includes('answered 404:')) {
        continue;
      }
      throw error;
    }
  }
}

async function freshPage(browser: Browser): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ storageState: STORAGE_STATE });
  return { context, page: await context.newPage() };
}

test.describe('members and grants', () => {
  test.use({ storageState: STORAGE_STATE });

  test.beforeEach(async ({ page }) => {
    await page.goto(PATH);
    await expect(page.getByRole('heading', { name: 'Members', level: 1 })).toBeVisible();
  });

  test('lists one line per capability with its origin chips', async ({ page }) => {
    const table = page.getByRole('table');
    // The fixture's workload holds `read` on development, granted through the
    // API — so its origin is `manual`, not the break-glass kind the seeding
    // CLI writes at instance scope.
    await expect(table).toContainText('read');
    await expect(table).toContainText('payments · development');
    await expect(table.getByText('manual').first()).toBeVisible();
    // Instance-scope grants reach this org by inheritance and are absent by
    // design; the surface says so rather than leaving a hole.
    await expect(page.getByText('Instance-scope grants reach this organisation')).toBeVisible();
  });

  test('keeps project chrome and narrows the members projection from a project link', async ({
    page,
  }) => {
    await page.goto(`/orgs/${seed.org}/projects/${seed.project}/matrix`);
    const menu = page.getByRole('button', { name: 'Menu' });
    if (await menu.isVisible()) await menu.click();
    const projectNav = page.getByRole('navigation', { name: 'Project' });
    await projectNav.getByRole('link', { name: 'Members & access' }).click();

    await expect(page).toHaveURL(PROJECT_PATH);
    if (await menu.isVisible()) await menu.click();
    await expect(page.getByRole('heading', { name: 'Project · payments' })).toBeVisible();
    await expect(
      page.getByRole('heading', { name: 'Members & access · payments', level: 1 }),
    ).toBeVisible();
    await expect(projectNav.getByRole('link', { name: 'Members & access' })).toHaveAttribute(
      'aria-current',
      'page',
    );
    if (await menu.isVisible()) await page.keyboard.press('Escape');

    const scope = page.getByLabel('On scope', { exact: true });
    await expect(scope.locator(`option[value="project:${seed.project}"]`)).toHaveCount(1);
    await expect(scope.locator(`option[value="project:${seed.history.project}"]`)).toHaveCount(0);
    await expect(scope.locator(`option[value="org:${seed.org}"]`)).toHaveCount(0);
    await expect(page.getByLabel('Capability').locator('option')).toHaveText([
      'reveal',
      'read',
      'publish',
      'edit',
      'manage-members',
      'audit-read',
    ]);

    await page.getByRole('button', { name: '+ new grant' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByText('Apply a role template', { exact: true })).toHaveCount(0);
    await expect(dialog.getByLabel('Scope').locator(`option[value="org:${seed.org}"]`)).toHaveCount(1);
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  });

  test('answers "who can…?" by inspection, counting the grants above the scope', async ({
    page,
  }) => {
    await page.getByLabel('Capability').selectOption('read');
    // By VALUE: the seed holds two projects with a `development` environment
    // (#59's history project), so a label is ambiguous.
    await page.getByLabel('On', { exact: true }).selectOption(`env:${seed.project}:${seed.dev}`);
    const answer = page.locator('.inspect__answer');
    await expect(answer).toContainText('grant line');

    // The creator's org-admin template is org-scoped, so it covers production
    // even though production holds no narrower `read` line of its own.
    await page.getByLabel('On', { exact: true }).selectOption(`env:${seed.project}:${seed.prod}`);
    await expect(answer).toContainText('1 grant line');
    await expect(answer).toContainText(seed.principal);
  });

  test('does not answer Nobody before the grant listing succeeds', async ({ page }) => {
    let release: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route(
      (url) => url.pathname === `/api/v1/orgs/${seed.org}/grants`,
      async (route) => {
        await gate;
        await route.continue();
      },
    );
    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(page.getByRole('status').filter({ hasText: 'Loading grants before answering' })).toBeVisible();
    await expect(page.locator('#members-inspect')).not.toContainText('Nobody');
    if (release === undefined) {
      throw new Error('the grant-list gate was not installed');
    }
    release();
    await expect(page.getByRole('status').filter({ hasText: 'Loading grants before answering' })).toHaveCount(0);
  });

  test('opens the grant modal with the safest scope preselected', async ({ page }) => {
    await page.getByRole('button', { name: 'New grant' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByRole('heading', { name: 'New grant' })).toBeVisible();

    const scope = dialog.getByLabel('Scope');
    // The safest default: the seed's one confirmed-unprotected environment
    // NAMED staging (the history project's), preferred over `payments`'
    // development even though that project sorts first.
    await expect(scope).toHaveValue(DEFAULT_SCOPE);

    const values = await scope.locator('option').evaluateAll((options) =>
      options.map((option) => (option instanceof HTMLOptionElement ? option.value : '')),
    );
    const dev = values.indexOf(`env:${seed.project}:${seed.dev}`);
    const prod = values.indexOf(`env:${seed.project}:${seed.prod}`);
    const project = values.indexOf(`project:${seed.project}`);
    const org = values.indexOf(`org:${seed.org}`);
    expect(dev, 'development is offered').toBeGreaterThan(0);
    // Narrow to wide, with the protected environment last inside its project.
    expect(prod).toBeGreaterThan(dev);
    expect(project).toBeGreaterThan(prod);
    expect(org).toBeGreaterThan(project);

    // The capability explanations are the permission ADR's own wording, one
    // (?) toggle each, closed until asked for.
    const explain = dialog.locator('summary[aria-label="Explain reveal"]');
    await expect(explain.locator('..')).not.toHaveAttribute('open', '');
    await explain.click();
    await expect(explain.locator('..')).toHaveAttribute('open', '');
    await expect(dialog.getByText('current secret plaintext, by any route')).toBeVisible();
  });

  test('prefers a confirmed-unprotected staging environment by name', async ({ page }) => {
    // `payments` (development · production) sorts before the history project
    // (development · staging); position order alone would pick
    // payments/development. The rule prefers the confirmed-unprotected
    // environment named staging, wherever it sits.
    await page.getByRole('button', { name: 'New grant' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByLabel('Scope')).toHaveValue(DEFAULT_SCOPE);
    await expect(dialog.getByLabel('Scope').locator('option:checked')).toHaveText('staging');
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  });

  test('resets a cancelled composition when a fresh grant dialog opens', async ({ page }) => {
    const principal = await automationPrincipal(page);
    const newGrant = page.getByRole('button', { name: 'New grant' });
    await newGrant.click();
    let dialog = page.getByRole('dialog');
    await dialog.getByLabel('Principal').fill(principal);
    await dialog.getByRole('checkbox', { name: 'read', exact: true }).check();
    await dialog.getByLabel('Scope').selectOption(`env:${seed.project}:${seed.prod}`);
    await dialog.getByRole('button', { name: 'Cancel' }).click();

    await newGrant.click();
    dialog = page.getByRole('dialog');
    await expect(dialog.getByLabel('Principal')).toHaveValue('');
    await expect(dialog.getByRole('checkbox', { name: 'read', exact: true })).not.toBeChecked();
    await expect(dialog.getByLabel('Scope')).toHaveValue(DEFAULT_SCOPE);
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  });

  test('waits for the complete topology before enabling a new grant', async ({ page }) => {
    let release: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route(
      (url) => url.pathname === `/api/v1/orgs/${seed.org}/projects`,
      async (route) => {
        await gate;
        await route.continue();
      },
    );
    await page.reload({ waitUntil: 'domcontentloaded' });
    const newGrant = page.getByRole('button', { name: 'New grant' });
    await expect(newGrant).toBeDisabled();
    await expect(page.getByText(/Loading the complete organisation topology/)).toBeVisible();
    if (release === undefined) {
      throw new Error('the project-list gate was not installed');
    }
    release();
    await expect(newGrant).toBeEnabled();
    await newGrant.click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  });

  test('warns on an org-scoped grant and keeps the composition on the way back', async ({
    page,
  }) => {
    const principal = await automationPrincipal(page);
    await page.getByRole('button', { name: 'New grant' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Principal').fill(principal);
    await dialog.getByRole('checkbox', { name: 'read', exact: true }).check();
    await dialog.getByRole('checkbox', { name: 'edit', exact: true }).check();
    await dialog.getByLabel('Scope').selectOption({ label: `${seed.orgName} (every project and environment)` });
    await dialog.getByRole('button', { name: 'Grant', exact: true }).click();

    const blast = page.getByRole('dialog');
    await expect(
      blast.getByRole('heading', { name: /blast radius/i }),
    ).toBeVisible();
    // The capabilities being handed over, named.
    await expect(blast).toContainText('read, edit');
    // Every project and every environment, enumerated rather than summarised
    // — including the protected one, and including the ones that do not exist.
    const list = blast.getByRole('list', { name: /organisation-scoped grant reaches/i });
    await expect(list).toContainText('payments');
    await expect(list).toContainText('development');
    await expect(list).toContainText('production (protected)');
    await expect(list).toContainText('any project created later');

    await blast.getByRole('button', { name: 'Back, change scope' }).click();
    // Composition preserved: a warning that threw the work away would train
    // people to click through it.
    const again = page.getByRole('dialog');
    await expect(again.getByLabel('Principal')).toHaveValue(principal);
    await expect(again.getByRole('checkbox', { name: 'read', exact: true })).toBeChecked();
    await expect(again.getByRole('checkbox', { name: 'edit', exact: true })).toBeChecked();
    await again.getByRole('button', { name: 'Cancel' }).click();
    await expect(page.getByRole('dialog')).toBeHidden();
  });

  test('marks unreadable environment protection and never preselects it', async ({ page }) => {
    // Every environment's protection becomes unreadable, so nothing is
    // CONFIRMED unprotected and nothing may be preselected — the history
    // project's staging included.
    const settingsPath = /\/api\/v1\/orgs\/[^/]+\/projects\/[^/]+\/environments\/[^/]+\/settings$/;
    await page.route(
      (url) => settingsPath.test(url.pathname) && url.search === '',
      (route) => route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'not_found', message: 'not found' } }),
      }),
    );
    await page.reload();
    await page.getByRole('button', { name: 'New grant' }).click();
    const scope = page.getByRole('dialog').getByLabel('Scope');
    await expect(
      scope.locator(`option[value="env:${seed.project}:${seed.dev}"]`),
    ).toHaveText('development (protection unreadable)');
    await expect(scope).toHaveValue('');
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  });

  test('reports a real later-capability refusal while keeping the earlier grant live', async ({ page }) => {
    const principal = await automationPrincipal(page);
    const attempted: string[] = [];
    const grantPath = `/api/v1/orgs/${seed.org}/projects/${seed.project}/environments/${seed.dev}/grants`;
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname === grantPath) {
        attempted.push(z.object({ capability: z.string() }).parse(request.postDataJSON()).capability);
      }
    });
    try {
      await page.getByRole('button', { name: 'New grant' }).click();
      const dialog = page.getByRole('dialog');
      await dialog.getByLabel('Principal').fill(principal);
      await dialog.getByRole('checkbox', { name: 'read', exact: true }).check();
      // `pin` is an environment atom, but the automation principal's normative
      // allowlist refuses it. `read` before it is a real successful first write.
      await dialog.getByRole('checkbox', { name: 'edit', exact: true }).check();
      await dialog.getByRole('checkbox', { name: 'pin', exact: true }).check();
      await dialog.getByLabel('Scope').selectOption(`env:${seed.project}:${seed.dev}`);
      await dialog.getByRole('button', { name: 'Grant', exact: true }).click();

      const partial = page.getByRole('alert').filter({ hasText: 'Completed 2 of 3' });
      await expect(partial).toContainText('2 of 3');
      await expect(partial).toContainText('pin was refused');
      await expect(partial).toContainText('live and listed below');
      const row = page.getByRole('row').filter({ hasText: principal });
      await expect(row).toContainText('read');
      await expect(row).toContainText('edit');
      expect(attempted).toEqual(['read', 'edit', 'pin']);
    } finally {
      await revokeAll(page, principal, ['read', 'edit', 'pin']);
    }
  });

  test('grants each capability as its own line and revokes one back', async ({ page }) => {
    const principal = await automationPrincipal(page);
    try {
      await page.getByRole('button', { name: 'New grant' }).click();
      const dialog = page.getByRole('dialog');
      await dialog.getByLabel('Principal').fill(principal);
      await dialog.getByRole('checkbox', { name: 'read', exact: true }).check();
      await dialog.getByRole('checkbox', { name: 'edit', exact: true }).check();
      await dialog.getByLabel('Scope').selectOption(`env:${seed.project}:${seed.dev}`);
      await dialog.getByRole('button', { name: 'Grant', exact: true }).click();

      const feedback = page.locator('.notice').filter({ hasText: 'Grant results' });
      await expectStatusIsTextAndAria(page, feedback);
      await expect(feedback).toContainText('Created: read, edit');
      await expect(feedback).toContainText('Origin added: none');
      await expect(feedback).toContainText('Unchanged: none');
      await expect(feedback).toContainText('independently revocable');

      // Two independent rows, not a bundle.
      const row = page.getByRole('row').filter({ hasText: principal });
      await expect(row).toContainText('read');
      await expect(row).toContainText('edit');

      await page
        .getByRole('button', { name: `Revoke edit on payments · development for ${principal}` })
        .click();
      const revoked = page.locator('.notice').filter({ hasText: 'Revoked edit' });
      await expectStatusIsTextAndAria(page, revoked);
      await expect(page.getByRole('row').filter({ hasText: principal })).not.toContainText('edit');
      await expect(page.getByRole('row').filter({ hasText: principal })).toContainText('read');
    } finally {
      await revokeAll(page, principal, ['read', 'edit']);
    }
  });

  test('renders membership and organisation server failures honestly', async ({ page }) => {
    await page.route(
      (url) => url.pathname === `/api/v1/orgs/${seed.org}/grants`,
      (route) => route.fulfill({ status: 500, contentType: 'application/json', body: '{}' }),
    );
    await page.route(
      (url) => url.pathname === `/api/v1/orgs/${seed.org}`,
      (route) => route.fulfill({ status: 500, contentType: 'application/json', body: '{}' }),
    );
    await page.reload();
    await expect(page.getByRole('alert').filter({ hasText: 'server failed while reading memberships' })).toBeVisible();
    await expect(page.locator('#members-inspect')).not.toContainText('Nobody');
    await expect(page.getByRole('alert').filter({ hasText: 'organisation could not be read' })).toBeVisible();
    await expect(page.getByRole('alert').filter({ hasText: 'second factor' })).toHaveCount(0);
  });

  test('switches creator membership from a project route without leaking the old org', async ({
    browser,
  }) => {
    const view = await freshPage(browser);
    try {
        await view.page.goto(`/orgs/${seed.org}/projects/${seed.project}/matrix`);
        const menu = view.page.getByRole('button', { name: 'Menu' });
        const mobile = await menu.isVisible();
        if (mobile) {
          await menu.click();
        }
        const rail = view.page.getByRole('navigation', { name: 'Organisations' });
        const mobileOrganizations = view.page.getByRole('region', { name: 'Organizations' });
        const orgButton = (name: string) => mobile
          ? mobileOrganizations.getByRole('button').filter({ hasText: name })
          : rail.getByRole('button', { name: `Organisation ${name}` });
        await expect(orgButton(seed.orgName)).toHaveAttribute(
          'aria-current',
          mobile ? 'page' : 'true',
        );
        await orgButton(seed.orgBName).click();
        await expect(view.page).toHaveURL(/\/projects$/);
        await expect(view.page.getByLabel('Breadcrumb')).toContainText(seed.orgBName);
        if (await menu.isVisible()) {
          await menu.click();
        }
        await expect(view.page.getByRole('link', { name: 'Members' })).toHaveAttribute(
          'href',
          `/orgs/${seed.orgB}/members`,
        );
        await expect(view.page.getByText('payments', { exact: true })).toHaveCount(0);

        // A deep link into the second organisation must stay selected when the
        // human moves to an unscoped surface: the route named the org, and
        // `/projects` must not silently fall back to the first circle.
        await view.page.goto(`/orgs/${seed.orgB}/members`);
        await expect(view.page.getByLabel('Breadcrumb')).toContainText(seed.orgBName);
        const menuAgain = view.page.getByRole('button', { name: 'Menu' });
        if (await menuAgain.isVisible()) {
          await menuAgain.click();
        }
        await view.page.getByRole('link', { name: 'Projects', exact: true }).click();
        await expect(view.page).toHaveURL(/\/projects$/);
        await expect(view.page.getByLabel('Breadcrumb')).toContainText(seed.orgBName);
        if (mobile) {
          await menu.click();
        }
        await expect(orgButton(seed.orgBName)).toHaveAttribute(
          'aria-current',
          mobile ? 'page' : 'true',
        );
    } finally {
      await view.context.close();
    }
  });

  for (const scheme of ['dark', 'light'] as const) {
    test(`meets the pinned assertion set on members (${scheme})`, async ({ page }, testInfo) => {
      await page.emulateMedia({ colorScheme: scheme });
      try {
        // Chrome surfaces are two-tier by design: DESIGN.md's 36px `--row` on a
        // mouse-driven grid, lifted to the 44px `--touch` floor on a phone.
        // `expectDensity` is an exact match and is not pointer-gated the way
        // `expectTouchTargets` is, so the claim has to name which tier it is on.
        const rowDensity = testInfo.project.name === 'mobile' ? '--touch' : '--row';
        const heading = page.getByRole('heading', { name: 'Members', level: 1 });
        const well = page.locator('.panel').first();
        const jump = page.getByRole('link', { name: 'Who can…?' });
        const chip = page.locator('.chip').first();
        const newGrant = page.getByRole('button', { name: 'New grant' });

        await expectPinnedAssertionSet(page, {
          flow: 'members',
          surface: 'members',
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
            // A settings panel's boundary is `--panel-line` (DESIGN.md); `--line`
            // is the control boundary, and a panel is not a control.
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

  for (const scheme of ['dark', 'light'] as const) {
    test(`meets the pinned assertion set on both grant dialogs (${scheme})`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      try {
        const principal = await automationPrincipal(page);
        const opener = page.getByRole('button', { name: 'New grant' });
        await opener.click();
        const composition = page.getByRole('dialog');
        await expect(composition).toBeVisible();

        await expectPinnedAssertionSet(page, {
          flow: 'members',
          surface: 'members',
          theme: scheme,
          text: [
            composition.getByRole('heading', { level: 2 }),
            composition.locator('.field__hint').first(),
          ],
          radii: [
            [composition, 'container'],
            [composition.getByRole('button', { name: 'Cancel' }), 'control'],
          ],
          fonts: [[composition.locator('.mono').first(), 'mono']],
          colours: [[composition, 'backgroundColor', '--bg-panel']],
          hairlines: [composition],
          density: [[composition.getByRole('button', { name: 'Cancel' }), '--touch']],
        });

        await composition.getByLabel('Principal').fill(principal);
        await composition.getByRole('checkbox', { name: 'read', exact: true }).check();
        await composition
          .getByLabel('Scope')
          .selectOption({ label: `${seed.orgName} (every project and environment)` });
        await composition.getByRole('button', { name: 'Grant', exact: true }).click();

        const blast = page.getByRole('dialog');
        const blastRow = blast.locator('.blast__list li').first();
        await expectPinnedAssertionSet(page, {
          flow: 'members',
          surface: 'members',
          theme: scheme,
          text: [blast.getByRole('heading', { level: 2 }), blast.locator('.blast__envs').first()],
          radii: [
            [blast, 'container'],
            [blastRow, 'container'],
            [blast.getByRole('button', { name: 'Back, change scope' }), 'control'],
          ],
          fonts: [[blast.locator('.mono').first(), 'mono']],
          colours: [[blast, 'backgroundColor', '--bg-panel']],
          hairlines: [blast, blastRow],
          density: [[blast.getByRole('button', { name: 'Back, change scope' }), '--touch']],
        });

        await page.keyboard.press('Escape');
        await expect(page.getByRole('dialog')).toBeHidden();
        await expect(opener).toBeFocused();
      } finally {
        await page.emulateMedia({ colorScheme: null });
      }
    });
  }
});

/**
 * Flow: SCIM provisioning administration (registry surface `scim`, #501).
 *
 * It rides this file because the merge gate loads `ci.yml` from the base branch
 * and a spec a PR adds to a group never runs on that PR — members.spec.ts is
 * already in group 1 and is the org-scoped `manage-members` sibling, so the
 * surface's pinned set runs from PR-checked-out content today (see
 * e2e/registry.ts). The session is the shared administrator's: `manage-members`
 * is MFA-mandatory and this session is stepped up, and minting additionally
 * reauthenticates with a fresh TOTP proof.
 */
const SCIM_PATH = `/orgs/${seed.org}/scim`;
const SCIM_MEDIA = 'application/scim+json';
const SCHEMA_GROUP = 'urn:ietf:params:scim:schemas:core:2.0:Group';
const SCHEMA_USER = 'urn:ietf:params:scim:schemas:core:2.0:User';
const SCHEMA_PATCHOP = 'urn:ietf:params:scim:api:messages:2.0:PatchOp';
const SCIM_PROVIDER_SLUG = 'e2e-oidc';

/** Create the binding once if absent; the pinned-set tests reuse it. */
async function ensureScimBinding(page: Page): Promise<string> {
  const list = await browserApi(
    page,
    'GET',
    `/api/v1/orgs/${seed.org}/scim-bindings`,
    zScimBindingList,
  );
  const existing = list.items.find((binding) => binding.provider_slug === SCIM_PROVIDER_SLUG);
  if (existing !== undefined) {
    return existing.id;
  }
  const created = await browserApi(page, 'POST', `/api/v1/orgs/${seed.org}/scim-bindings`, zScimBinding, {
    provider_kind: 'oidc',
    provider_slug: SCIM_PROVIDER_SLUG,
    subject_source: 'externalId',
  });
  return created.id;
}

test.describe('scim provisioning', () => {
  test.use({ storageState: STORAGE_STATE });

  test('administers a binding, credential, mapping and directory end to end', async ({ page }) => {
    await page.goto(SCIM_PATH);
    await expect(page.getByRole('heading', { name: 'SCIM provisioning', level: 1 })).toBeVisible();

    // Bind the fixture's OIDC provider. Subject source defaults to externalId.
    await page.getByLabel('Provider slug').fill(SCIM_PROVIDER_SLUG);
    await page.getByRole('button', { name: 'Create binding' }).click();
    await expect(page.getByText(new RegExp(`Bound ${SCIM_PROVIDER_SLUG}`))).toBeVisible();

    // Administer it — the binding id lands in the query string as route data.
    const card = page.locator('.scim-binding', { hasText: SCIM_PROVIDER_SLUG });
    await card.getByRole('button', { name: 'Administer' }).click();
    await expect(page.getByRole('heading', { name: 'Provisioning credentials' })).toBeVisible();
    const bindingId = new URL(page.url()).searchParams.get('binding') ?? '';
    expect(bindingId).not.toBe('');

    // Mint a credential with a fresh reauthentication proof, and capture the
    // display-once token from the dialog. The token is read here (it is in the
    // dialog by design) and used as the wire bearer below.
    await page.getByLabel('Reauthentication proof').fill(await nextTotpCode());
    await page.getByRole('button', { name: 'Mint credential' }).click();
    const mintDialog = page.getByRole('dialog', { name: /shown exactly once/ });
    await expect(mintDialog).toBeVisible();
    const token = ((await mintDialog.locator('.machine__token').textContent()) ?? '').trim();
    expect(token).not.toBe('');
    await mintDialog.getByLabel(/configured this credential/).check();
    await mintDialog.getByRole('button', { name: 'Done' }).click();
    await expect(mintDialog).toBeHidden();

    // The token never reaches a URL or durable browser storage. Assert on a
    // boolean, never the raw token: a `not.toContain(token)` failure prints the
    // expected substring into the CI log, leaking the credential.
    expect(page.url().includes(token)).toBe(false);
    const tokenInStorage = await page.evaluate((secret) => {
      const scan = (store: Storage) =>
        Object.keys(store).some((key) => key.includes(secret) || (store.getItem(key) ?? '').includes(secret));
      return scan(localStorage) || scan(sessionStorage);
    }, token);
    expect(tokenInStorage).toBe(false);

    // The identity provider's own wire, with that credential: provision a group
    // and a user the way a connector does.
    const wire = (method: string, path: string, body?: unknown) =>
      fetch(`${BASE_URL}/api/v1/orgs/${seed.org}/scim/v2/${bindingId}${path}`, {
        method,
        headers: {
          Authorization: `Bearer ${token}`,
          ...(body === undefined ? {} : { 'Content-Type': SCIM_MEDIA }),
        },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });

    const groupResponse = await wire('POST', '/Groups', {
      schemas: [SCHEMA_GROUP],
      displayName: 'E2E Engineers',
    });
    expect(groupResponse.status).toBe(201);
    const group = z.object({ id: z.string() }).parse(await groupResponse.json());
    expect(group.id).not.toBe('');

    const userResponse = await wire('POST', '/Users', {
      schemas: [SCHEMA_USER],
      userName: 'e2e-scim@idp.test',
      externalId: 'e2e-scim-sub',
      active: true,
    });
    expect(userResponse.status).toBe(201);
    const user = z.object({ id: z.string() }).parse(await userResponse.json());

    // Put the user IN the group so the mapping below actually grants someone —
    // otherwise the "members affected" count is zero and the assertions pass
    // vacuously.
    const patchResponse = await wire('PATCH', `/Groups/${group.id}`, {
      schemas: [SCHEMA_PATCHOP],
      Operations: [{ op: 'add', path: 'members', value: [{ value: user.id }] }],
    });
    expect(patchResponse.status).toBe(200);

    // A reload re-reads the directory the wire just populated; the binding stays
    // selected because it is in the URL.
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Directory' })).toBeVisible();
    await expect(page.locator('.scim-directory-group', { hasText: 'E2E Engineers' })).toBeVisible();
    await expect(
      page.locator('.scim-directory-user', { hasText: 'e2e-scim@idp.test' }),
    ).toBeVisible();

    // Map the provisioned group to a template at organisation scope — the widest
    // reach — so the server's consequence language is returned and rendered. The
    // one member is granted immediately, so the applied count is nonzero.
    await page.getByLabel('Provisioned group').selectOption(group.id);
    await page.getByLabel('Template').selectOption('viewer');
    await page.getByRole('button', { name: 'Add mapping' }).click();
    await expect(page.getByText(/Applied to 1 member/)).toBeVisible();
    await expect(page.getByText(/[1-9]\d* grants created/)).toBeVisible();
    await expect(page.locator('.scim-warnings')).toBeVisible();
    const mappingRow = page.locator('.scim-mapping', { hasText: 'E2E Engineers' });
    await expect(mappingRow).toBeVisible();

    // Delete the mapping — it releases every origin it held. The row goes, and
    // the release count is reported ABOVE the list so it outlives the row.
    await mappingRow.getByRole('button', { name: /Delete mapping/ }).click();
    await expect(page.locator('.scim-mapping', { hasText: 'E2E Engineers' })).toHaveCount(0);
    await expect(page.getByText(/Deleted\. [1-9]\d* origins released/)).toBeVisible();

    // Revoke the credential; it bites at the wire's very next request.
    const credentialRow = page.locator('.scim-credential').first();
    await credentialRow.getByRole('button', { name: /Revoke credential/ }).click();
    await expect(credentialRow.locator('.badge', { hasText: 'revoked' })).toBeVisible();
    const afterRevoke = await wire('GET', '/ServiceProviderConfig');
    expect(afterRevoke.status).toBe(401);

    // Delete the binding through its typed-name gate; the teardown runs and the
    // card is gone.
    await card.getByLabel('Confirm by typing the provider slug').fill(SCIM_PROVIDER_SLUG);
    await card.getByRole('button', { name: 'Delete binding' }).click();
    await expect(page.locator('.scim-binding', { hasText: SCIM_PROVIDER_SLUG })).toHaveCount(0);
  });

  for (const scheme of ['dark', 'light'] as const) {
    for (const surface of surfacesForFlow('scim')) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({
        page,
      }, testInfo) => {
        await page.emulateMedia({ colorScheme: scheme });
        try {
          const bindingId = await ensureScimBinding(page);
          await page.goto(`${SCIM_PATH}?binding=${bindingId}`);

          const heading = page.getByRole('heading', { name: 'SCIM provisioning', level: 1 });
          const panel = page.locator('.panel').first();
          const slug = page.locator('.scim-binding__slug .mono').first();
          const badge = page.locator('.scim-binding .badge').first();
          const administer = page.getByRole('button', { name: 'Administering' }).first();
          const rowDensity = testInfo.project.name === 'mobile' ? '--touch' : '--row';

          await expectPinnedAssertionSet(page, {
            flow: 'scim',
            surface: surface.id,
            theme: scheme,
            text: [heading, slug],
            radii: [
              [panel, 'container'],
              [administer, 'control'],
              [badge, 'badge'],
            ],
            fonts: [
              [heading, 'ui'],
              [slug, 'mono'],
            ],
            colours: [
              [heading, 'color', '--tx'],
              [panel, 'backgroundColor', '--bg-panel'],
              [panel, 'borderTopColor', '--panel-line'],
            ],
            hairlines: [panel],
            density: [[administer, rowDensity]],
          });
        } finally {
          await page.emulateMedia({ colorScheme: null });
        }
      });
    }
  }
});

/**
 * Flow: audit trail query and export (registry surface `audit`, #502).
 *
 * It rides this file for the same reason `scim` does: the merge gate loads
 * `ci.yml` from the base branch, so a spec a PR adds to a group never runs on
 * that PR. members.spec.ts is already in group 1 and is the org-scoped sibling,
 * so the surface's pinned set runs from PR-checked-out content today. The
 * bootstrap administrator holds `audit-read` (seeded break-glass at instance
 * scope, inheriting down to this org), so the org trail is readable.
 */
const AUDIT_PATH = `/orgs/${seed.org}/audit`;

test.describe('audit trail', () => {
  test.use({ storageState: STORAGE_STATE });

  test('pages the org trail, inspects an event and exports it', async ({ page }) => {
    await page.goto(AUDIT_PATH);
    await expect(page.getByRole('heading', { name: 'Audit', level: 1 })).toBeVisible();

    // The setup wrote many events (org creation, the break-glass grants, the
    // seeded project/keys/values), so the first page is not empty.
    const rows = page.locator('.audit__row');
    await expect(rows.first()).toBeVisible();

    // Inspecting an event opens the detail with its payload; no value material
    // is ever in the trail, so the payload is metadata only.
    await rows.first().click();
    const detail = page.getByRole('complementary', { name: 'Event detail' });
    await expect(detail).toBeVisible();
    await expect(detail.getByRole('heading', { level: 3, name: 'Payload' })).toBeVisible();

    // A filter that cannot match anything is an explicit empty state, never a
    // silent blank. `operation` is a free string, so an unknown one is not a
    // 400 — it simply matches nothing (contract: unknown type returns empty).
    await page.getByLabel('Operation').fill('nonexistent.operation.e2e');
    await page.getByRole('button', { name: 'Apply filter' }).click();
    await expect(page.locator('.audit__row')).toHaveCount(0);
    await expect(page.locator('.audit__empty')).toBeVisible();

    // Clear, then export the current filter. The download is a same-origin GET
    // under the session cookie; the browser streams JSONL to disk.
    await page.getByRole('button', { name: 'Clear' }).click();
    await expect(page.locator('.audit__row').first()).toBeVisible();
    const download = page.waitForEvent('download');
    await page.getByRole('link', { name: 'Export JSONL' }).click();
    const file = await download;
    expect(file.suggestedFilename()).toMatch(/\.jsonl$/);
    const path = await file.path();
    const body = readFileSync(path, 'utf8').trim();
    expect(body.length).toBeGreaterThan(0);
    // Every line is a JSON object carrying the immutable envelope fields.
    for (const line of body.split('\n')) {
      const event = z
        .object({ seq: z.number(), id: z.string(), type: z.string(), outcome: z.string() })
        .parse(JSON.parse(line));
      expect(event.id).not.toBe('');
    }
  });

  test('refuses a malformed filter at the boundary', async ({ page }) => {
    // A well-formed but empty request succeeds; the malformed-filter path is a
    // format the contract rejects before tenant resolution. An out-of-enum
    // outcome cannot be typed through the select, so drive the query directly:
    // the server answers 400, never a partial page.
    const response = await page.request.get(
      `${BASE_URL}/api/v1/orgs/${seed.org}/audit?outcome=not-an-outcome`,
    );
    expect(response.status()).toBe(400);
  });

  for (const scheme of ['dark', 'light'] as const) {
    for (const surface of surfacesForFlow('audit')) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({
        page,
      }, testInfo) => {
        await page.emulateMedia({ colorScheme: scheme });
        try {
          await page.goto(AUDIT_PATH);
          await expect(page.locator('.audit__row').first()).toBeVisible();

          const heading = page.getByRole('heading', { name: 'Audit', level: 1 });
          const panel = page.locator('.panel').first();
          const op = page.locator('.audit__row-op').first();
          const badge = page.locator('.chip').first();
          const apply = page.getByRole('button', { name: 'Apply filter' });
          const rowDensity = testInfo.project.name === 'mobile' ? '--touch' : '--row';

          await expectPinnedAssertionSet(page, {
            flow: 'audit',
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
