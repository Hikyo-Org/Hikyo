import { expect, test, type Page } from '@playwright/test';
import { zRetentionHealth } from '@hikyo/zod';

import type { RetentionHealth } from '../../src/api/retention.ts';
import {
  expectNoSeriousAxeViolations,
  expectPinnedAssertionSet,
  expectStatusIsTextAndAria,
} from '../fixtures/assertions.ts';
import { ADMIN, STORAGE_STATE } from '../fixtures/instance.ts';
import { surfacesForFlow } from '../registry.ts';


/**
 * Flow: the application chrome (registry surfaces `overview`, `projects`,
 * `settings`).
 *
 * The skeleton's contract is navigation, not content: every section is
 * reachable, the account entry works, the theme is switchable, and the whole
 * thing survives a phone viewport. The pinned assertion set runs over every
 * control the flow touches.
 *
 * These tests start from a session minted once in global setup rather than
 * driving the login form each time. Signing in is the login flow's subject,
 * and the instance's per-source throttle (ten attempts a minute) means a suite
 * that re-authenticates per test would eventually be measuring the throttle.
 */

/** openNav reveals the sidebar, which is a disclosure on a phone. */
async function openNav(page: Page): Promise<void> {
  const navigation = page.getByRole('navigation', { name: 'Sections', exact: true });
  const toggle = page.getByRole('button', { name: 'Menu' });
  await expect
    .poll(async () => (await navigation.isVisible()) || (await toggle.isVisible()))
    .toBe(true);
  if (await navigation.isVisible()) {
    return;
  }
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') {
    await toggle.click();
  }
  await expect(navigation).toBeVisible();
}

/** Override only scenario facts while preserving the live generated response contract. */
async function mockRetentionHealth(
  page: Page,
  overrides: Partial<RetentionHealth>,
): Promise<void> {
  await page.route('**/api/v1/instance/retention-health', async (route) => {
    const response = await route.fetch();
    const health = zRetentionHealth.parse(await response.json());
    await route.fulfill({ response, json: { ...health, ...overrides } });
  });
}

test.describe('app chrome', () => {
  test.use({ storageState: STORAGE_STATE });

  test.beforeEach(async ({ page }, testInfo) => {
    await page.goto('/');
    if (testInfo.project.name === 'mobile') {
      await expect(page.getByRole('button', { name: 'Menu' })).toBeVisible();
    } else {
      await expect(page.getByRole('navigation', { name: 'Organisations' })).toBeVisible();
    }
  });

  test('matches the locked desktop chrome composition', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'desktop chrome geometry');

    const rail = page.getByRole('navigation', { name: 'Organisations' });
    const sidebar = page.getByRole('navigation', { name: 'Sections', exact: true });
    const header = page.locator('.header');

    await rail.getByRole('button', { name: /^Project / }).first().click();

    await expect(rail).toHaveCSS('width', '56px');
    await expect(sidebar).toHaveCSS('width', '218px');
    await expect(header).toHaveCSS('height', '61px');
    await expect(header.getByText('Signed in as')).toBeVisible();
    await expect(rail.getByRole('link', { name: 'Instance settings' })).toBeVisible();

    const projectNav = sidebar.getByRole('navigation', { name: 'Project' });
    const matrixLink = projectNav.getByRole('link', { name: 'Environment matrix' });
    await expect(matrixLink).toHaveCSS('min-height', '38px');
    await expect(matrixLink).toHaveCSS('font-size', '13px');
    await expect(sidebar.locator('.context-sidebar__org-avatar')).toHaveCSS('width', '28px');
    await expect(sidebar.locator('.context-sidebar__org small')).toHaveText(
      'Organisation member',
    );
    await expect(sidebar.locator('.context-sidebar__group').first()).toHaveCSS('height', '38px');
    await expect(sidebar.getByRole('heading', { name: 'Organisation' })).toBeVisible();
    await expect(projectNav.getByRole('link', { name: 'Version history' })).toHaveCount(0);
    await expect(projectNav.getByRole('link', { name: 'Machine access' })).toBeVisible();
    await expect(projectNav.getByRole('link', { name: 'Members' })).toBeVisible();
    // The organisation block is never hidden: every destination stays
    // reachable in project mode (#567).
    // Exact: the project block's `Project audit` (#572) also contains the word.
    await expect(sidebar.getByRole('link', { name: 'Audit', exact: true })).toBeVisible();
    // Account and instance live in the rail on desktop, not in the sidebar.
    await expect(sidebar.getByRole('link', { name: 'Account & security' })).toHaveCount(0);
    await expect(sidebar.getByRole('link', { name: 'Instance settings' })).toHaveCount(0);
  });

  test('stacks the instance context above the organisation block on instance routes', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'desktop sidebar composition');
    await page.goto('/instance/members');
    const sidebar = page.getByRole('navigation', { name: 'Sections', exact: true });
    const instanceNav = sidebar.getByRole('navigation', { name: 'Instance' });
    await expect(instanceNav.getByRole('link', { name: 'Instance members' })).toHaveAttribute(
      'aria-current',
      'page',
    );
    // Exact matching: the settings link must not light up under its sibling.
    await expect(instanceNav.getByRole('link', { name: 'Instance settings' })).not.toHaveAttribute(
      'aria-current',
      'page',
    );
    await expect(sidebar.getByRole('heading', { name: 'Organisation' })).toBeVisible();
    await expect(page.getByLabel('Breadcrumb')).toContainText('Instance');
  });

  test('keeps every chrome destination in the fixed mobile drawer', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile', 'mobile drawer contract');

    const toggle = page.getByRole('button', { name: 'Menu' });
    await toggle.click();

    const drawer = page.getByRole('navigation', { name: 'Sections', exact: true });
    await expect(drawer).toBeVisible();
    await expect(drawer).toHaveCSS('position', 'fixed');
    await expect(drawer).toHaveCSS('width', '300px');
    await expect(drawer.locator('.sidebar__mobile-organisations button')).not.toHaveCount(0);
    await expect(drawer.locator('.sidebar__mobile-projects button')).not.toHaveCount(0);
    await expect(drawer.getByRole('link', { name: 'Account & security' })).toBeVisible();
    await expect(drawer.getByRole('link', { name: 'Instance settings' })).toBeVisible();
    await expect(drawer.getByRole('link', { name: 'Instance members' })).toBeVisible();
    await expect(drawer.locator(':focus')).toBeVisible();

    await page.keyboard.press('Shift+Tab');
    await expect(page.getByRole('button', { name: 'Close navigation' })).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(drawer.locator(':focus')).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(drawer).toBeHidden();
    await expect(toggle).toBeFocused();

    await toggle.click();
    await page.locator('.nav-scrim').click();
    await expect(drawer).toBeHidden();
    await expect(toggle).toBeFocused();
  });

  test('releases the mobile drawer when the viewport becomes desktop', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile', 'mobile-to-desktop transition');

    await page.getByRole('button', { name: 'Menu' }).click();
    await expect(page.locator('.chrome')).toHaveAttribute('data-nav', 'open');

    await page.setViewportSize({ width: 1024, height: 720 });

    await expect(page.locator('.chrome')).toHaveAttribute('data-nav', 'closed');
    await expect(page.getByRole('navigation', { name: 'Organisations' })).not.toHaveAttribute(
      'inert',
      '',
    );
    await expect(page.getByRole('button', { name: /^Account:/ })).toBeFocused();
  });

  test('reaches every section of the skeleton', async ({ page }) => {
    for (const surface of surfacesForFlow('shell')) {
      await openNav(page);
      await page.getByRole('link', { name: surface.label, exact: true }).click();
      await expect(page.getByRole('heading', { name: surface.label, level: 1 })).toBeVisible();
      // The breadcrumb is the "where am I" answer and must follow.
      await expect(page.getByLabel('Breadcrumb')).toContainText(surface.label);
    }
  });

  test('a deep link is served by the instance, not just by the router', async ({ page }) => {
    // The SPA-fallback rule seen from the outside: a full page load of an
    // application route must return the document, uncached, not a 404.
    const response = await page.goto('/projects');
    expect(response?.status(), 'a deep link did not fall back to the document').toBe(200);
    expect(response?.headers()['cache-control'], 'the document was served cacheable').toBe(
      'no-cache',
    );
    await expect(page.getByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
  });

  test('redirects the public login route to projects with a live session', async ({ page }, testInfo) => {
    await page.goto('/login');

    await expect(page).toHaveURL(/\/projects$/);
    if (testInfo.project.name === 'mobile') {
      await expect(page.getByRole('button', { name: 'Menu' })).toBeVisible();
    } else {
      await expect(page.getByRole('navigation', { name: 'Organisations' })).toBeVisible();
    }
    await expect(page.getByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
  });

  test('the binary toggle flips theme and keeps the choice explicit', async ({ page }) => {
    // The header control is a two-state sun/moon; `system` lives in the account
    // Preferences panel. Each click writes an explicit theme opposite the one
    // currently painted, so two clicks land on opposite attributes.
    const toggle = page.getByRole('button', { name: /theme/i });
    await toggle.click();
    const first = await page.locator('html').getAttribute('data-theme');
    expect(first === 'light' || first === 'dark', 'first click set an explicit theme').toBeTruthy();
    await toggle.click();
    const second = await page.locator('html').getAttribute('data-theme');
    expect(second, 'the second click flipped to the other theme').not.toBe(first);
    // The choice survives a reload: it is a decision, not a session mood.
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', second ?? '');
  });

  test('Escape dismisses the account menu and restores focus', async ({ page }) => {
    const account = page.getByRole('button', { name: /^Account:/ });
    await account.click();

    await expect(account).toHaveAttribute('aria-expanded', 'true');
    await expect(page.getByRole('menuitem', { name: 'Account & security' })).toBeFocused();
    await expectNoSeriousAxeViolations(page);

    await page.keyboard.press('Escape');

    await expect(account).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByRole('menu', { name: 'Account' })).toHaveCount(0);
    await expect(account).toBeFocused();
  });

  // The rail's zero state remains a real supported state even though the
  // fixture's creator now has automatic access to its organisations. Reach it
  // by controlling only this projection read; the interaction under test is
  // the empty-state rendering, while the real creator-membership path is
  // covered by the instance-administration flow.
  test('shows the zero-organisation state rather than an empty rail', async ({ page }) => {
    await page.route('**/api/v1/auth/whoami', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 100));
      await route.continue();
    });
    await page.route('**/api/v1/me/orgs', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], count: 0 }),
      }),
    );
    await page.goto('/login');
    await openNav(page);
    const notice = page.getByRole('status').filter({ hasText: 'No organisations yet' });
    await expectStatusIsTextAndAria(page, notice);
    await expect(notice).toContainText('No organisations yet');
    await expect(page.getByText(/choose a project/i)).toHaveCount(0);
    // And no step-up wall: nothing on the navigation surface asks for one.
    await expect(page.getByText(/second factor/i)).toHaveCount(0);
  });

  test('shows stale pruning health in the persistent app chrome', async ({ page }) => {
    const lastSuccess = '2026-08-14T10:00:00Z';
    await mockRetentionHealth(page, { last_prune_success: lastSuccess, stale: true });
    await page.reload();

    const warning = page.locator('.retention-warning');
    await expect(warning).toHaveAttribute('role', 'alert');
    await expect(warning).toContainText('Payload pruning has not succeeded since');
    await expect(warning).toContainText('retention bounds are not being enforced.');
    await expect(warning.locator('time')).toHaveAttribute('datetime', lastSuccess);
  });

  test('warns when a project reaches the storage high-water', async ({ page }) => {
    await mockRetentionHealth(page, {
      last_prune_success: '2026-08-20T10:00:00Z',
      stale: false,
      peak_project_bytes: 1_500_000_000,
      storage_warn: true,
    });
    await page.reload();

    const warning = page.locator('.retention-warning');
    await expect(warning).toHaveAttribute('role', 'alert');
    await expect(warning).toContainText('1.40 GiB of stored payload');
    await expect(warning).toContainText('new publishes are refused at 4 GiB');
  });

  for (const status of [403, 404]) {
    // An operator whose health endpoint refuses the read still keeps the
    // administration chrome — that is gated on the whoami capability, not on
    // this poll — but the pruning banner has no health to show, so it is silent.
    test(`silently hides pruning health when the endpoint returns ${status}`, async ({ page }) => {
      await page.route('**/api/v1/instance/retention-health', (route) =>
        route.fulfill({ status, contentType: 'application/json', body: '{}' }),
      );
      await page.reload();
      await expect(page.locator('.retention-warning')).toHaveCount(0);
    });
  }

  // The administration chrome and its background polls are gated on the whoami
  // `instance_operator` capability: an ordinary member neither sees the surface
  // nor fires the operator-only reads it would only be refused.
  test('gates instance administration on the operator capability', async ({ page }, testInfo) => {
    await page.route('**/api/v1/auth/whoami', async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      body.capabilities = { ...body.capabilities, instance_operator: false };
      await route.fulfill({ response, json: body });
    });
    let retentionCalled = false;
    await page.route('**/api/v1/instance/retention-health', (route) => {
      retentionCalled = true;
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
    });
    await page.reload();
    if (testInfo.project.name === 'mobile') {
      await openNav(page);
    }
    const instanceAdmin =
      testInfo.project.name === 'mobile'
        ? page
            .getByRole('navigation', { name: 'Sections', exact: true })
            .getByRole('link', { name: 'Instance settings' })
        : page.locator('.rail__action[aria-label="Instance settings"]');
    await expect(instanceAdmin).toHaveCount(0);
    expect(retentionCalled).toBe(false);
    // whoami is re-read on load, focus and hydrate, so drop the async override
    // before the page tears down — a route.fetch still in flight at test end
    // would otherwise error against the closing context.
    await page.unrouteAll({ behavior: 'ignoreErrors' });
  });

  // A still-valid session must survive a transient background revalidation
  // outage: the server briefly going unreachable for a whoami re-read must not
  // latch the global reload wall over the working UI (#440).
  test('holds a still-valid session through a background revalidation outage', async ({ page }) => {
    await page.goto('/projects');
    const heading = page.getByRole('heading', { name: 'Projects', level: 1 });
    const wall = page.getByText('Could not reach the server. Reload once it is back.');
    await expect(heading).toBeVisible();

    let whoamiDown = true;
    await page.route('**/api/v1/auth/whoami', async (route) => {
      if (whoamiDown) {
        await route.fulfill({ status: 503, contentType: 'application/json', body: '{}' });
        return;
      }
      await route.continue();
    });

    // A peer-tab "session changed" broadcast forces a BLOCKING revalidate — the
    // path that used to promote the transport error into the latching wall.
    const failed = page.waitForResponse('**/api/v1/auth/whoami');
    await page.evaluate(() =>
      new BroadcastChannel('hikyo-root-auth').postMessage('session-changed'),
    );
    await failed;

    // The last-known-good session keeps painting; no reload wall. `heading`
    // returns only once the failed blocking check has committed back to the
    // authenticated shell — a reverted fix latches the wall, the heading never
    // comes back, and this visibility poll times out.
    await expect(heading).toBeVisible();
    await expect(wall).toHaveCount(0);

    // When the server returns, the next revalidation recovers cleanly.
    whoamiDown = false;
    await page.evaluate(() =>
      new BroadcastChannel('hikyo-root-auth').postMessage('session-changed'),
    );
    await expect(heading).toBeVisible();
    await expect(wall).toHaveCount(0);
    await page.unrouteAll({ behavior: 'ignoreErrors' });
  });

  test('fails loud when pruning health cannot be checked', async ({ page }) => {
    await page.route('**/api/v1/instance/retention-health', (route) =>
      route.fulfill({ status: 500, contentType: 'application/json', body: '{}' }),
    );
    await page.reload();
    await expect(page.locator('.retention-warning')).toContainText(
      'Retention health could not be checked. Reload to try again.',
    );
  });

  // The matrix is DERIVED from the registry, not re-listed beside it: this
  // flow asserts exactly the surfaces it claims, so claiming a fourth is the
  // same act as asserting it. Both themes, because the palette is a dual-theme
  // palette and half of it going unchecked is half a claim.
  for (const surface of surfacesForFlow('shell')) {
    for (const scheme of ['dark', 'light'] as const) {
      test(`meets the pinned assertion set on ${surface.label} (${scheme})`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        await page.goto(surface.path);
        await openNav(page);

        const account = page.getByRole('button', { name: /^Account:/ });
        const theme = page.getByRole('button', { name: /theme/i });
        const heading = page.getByRole('heading', { name: surface.label, level: 1 });
        const well = page.locator('.card');
        const crumbs = page.getByLabel('Breadcrumb');
        const active = page.getByRole('link', { name: surface.label, exact: true });

        await expectPinnedAssertionSet(page, {
          flow: 'shell',
          surface: surface.id,
          theme: scheme,
          text: [heading, crumbs, active],
          radii: [
            // The identity circle is one of the three things allowed to be a pill.
            [account, 'pill'],
            [theme, 'control'],
            [well, 'container'],
          ],
          fonts: [
            [heading, 'ui'],
            [crumbs, 'ui'],
          ],
          colours: [
            [heading, 'color', '--tx'],
            // Cards are persistent-chrome surfaces, which is `--bg-panel`, not
            // the `--bg-raise` of a raised row. The two are a rounding apart in
            // dark and opposite sides of the page in light.
            [well, 'backgroundColor', '--bg-panel'],
            [well, 'borderTopColor', '--line'],
            // Treatment e's hairline rule belongs to each ROW, so that the
            // current row can own its segment of it and turn it accent.
            // Not `.first()`: the first link on this surface IS the current one,
            // and the current row's whole point is that its segment of the rule
            // turns accent. Assert the rule on a row that is not current.
            [
              page.locator('.sidebar__link:not([aria-current="page"])').first(),
              'borderLeftColor',
              '--chrome-line',
            ],
            // ...and the current row owns its segment of that rule in accent.
            // Asserting only the hairline would pass just as well with the rule
            // on the container and a second line drawn inside the row.
            [active, 'borderLeftColor', '--accent'],
          ],
          hairlines: [well],
          density: [[theme, '--touch']],
        });
      });
    }
  }

  test('the skip link is the first tab stop and becomes visible', async ({ page }) => {
    await page.keyboard.press('Tab');
    const skip = page.getByRole('link', { name: 'Skip to content' });
    await expect(skip).toBeFocused();
    await expect(skip).toBeInViewport();
  });
});

// Sign-out revokes the session it uses, so it gets its own — sharing the
// suite's would leave every later test holding a dead cookie.
test.describe('sign out', () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test('signs out through the account entry and clears both cookies', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Username').fill(ADMIN.username);
    await page.getByLabel('Password').fill(ADMIN.password);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('button', { name: /^Account:/ })).toBeVisible();

    await page.getByRole('button', { name: /^Account:/ }).click();
    await page.getByRole('menuitem', { name: 'Sign out' }).click();

    // Sign-out is a cookie-authenticated POST, so it only succeeds if the SPA
    // echoed the synchronizer token — reaching the login page proves the whole
    // CSRF contract end to end, through the real server.
    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    const names = (await page.context().cookies()).map((c) => c.name);
    expect(names).not.toContain('__Host-hikyo');
    expect(names).not.toContain('__Host-hikyo-csrf');
  });

  test('moves two tabs through one login and logout state machine', async ({ context, page }) => {
    const other = await context.newPage();
    await page.goto('/login');
    await other.goto('/login');
    await expect(other.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();

    await page.getByLabel('Username').fill(ADMIN.username);
    await page.getByLabel('Password').fill(ADMIN.password);
    await page.getByRole('button', { name: 'Sign in' }).click();

    await expect(page.getByRole('button', { name: /^Account:/ })).toBeVisible();
    await expect(other.getByRole('button', { name: /^Account:/ })).toBeVisible();

    await page.getByRole('button', { name: /^Account:/ }).click();
    await page.getByRole('menuitem', { name: 'Sign out' }).click();

    await expect(page.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    await expect(other.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
  });
});
