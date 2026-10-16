import { expect, test } from '@playwright/test';
import { SURFACES, type SurfaceId } from '../../src/app/navigation.ts';
import { DEMO_SURFACE_POLICIES } from '../../src/demo/surfaces.ts';

// Fixed public fixture addresses. Do not import Vite application modules into
// Playwright's Node process: those modules read import.meta.env at startup.
const DEMO_IDS = {
  org: 'org_11111111-1111-4111-8111-111111111111',
  project: 'prj_11111111-1111-4111-8111-111111111111',
  staging: 'env_22222222-2222-4222-8222-222222222222',
};
const DEMO_MATRIX_PATH = `/orgs/${DEMO_IDS.org}/projects/${DEMO_IDS.project}/matrix`;

// Real browser network requests must only load static files. The demo's API
// calls are handled before they reach the browser network stack.
test.beforeEach(async ({ page }) => {
  page.on('pageerror', (error) => { throw error; });
  page.on('console', (message) => {
    if (message.type() === 'error') throw new Error(`Browser console error: ${message.text()}`);
  });
  page.on('request', (request) => {
    const url = new URL(request.url());
    expect(url.origin).toBe('http://127.0.0.1:5196');
    expect(url.pathname.startsWith('/demo/')).toBe(true);
    expect(url.pathname).not.toContain('/api/');
  });
  page.on('response', (response) => { expect(response.status()).toBeLessThan(400); });
});

test('matrix key names preview on the first phone tap and open on the second', async ({ page, isMobile }) => {
  await page.goto('./');
  const link = page.getByRole('link', { name: 'Declaration of LOG_LEVEL', exact: true });
  const tip = page.locator('.matrix__key-tip').filter({ hasText: /^LOG_LEVEL$/ });
  await expect(tip).toBeHidden();

  if (isMobile) {
    // Use real touch input: a click does not reproduce Chromium's synthesized
    // mouse events after touchend, which used to dismiss this preview.
    await link.tap();
    await expect(tip).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`#${DEMO_MATRIX_PATH}$`));
    // Also exercise the compatibility event explicitly so this regression is
    // covered on browsers that do not synthesize it for this particular tap.
    await link.dispatchEvent('mouseout', { relatedTarget: null });
    await expect(tip).toBeVisible();
    await link.tap();
  } else {
    await link.hover();
    await expect(tip).toBeHidden();
    await link.click();
  }

  await expect(page).toHaveURL(new RegExp(`#${DEMO_MATRIX_PATH}/keys/key_11111111-1111-4111-8111-000000000001$`));
  await expect(page.getByRole('heading', { name: 'LOG_LEVEL', exact: true })).toBeVisible();
  await expect(tip).toBeHidden();
});

test('compact matrix key previews dismiss on outside press, Escape and focus loss', async ({ page, isMobile }) => {
  // A narrow window with a mouse still supports hover; a phone uses taps.
  if (!isMobile) await page.setViewportSize({ width: 393, height: 851 });
  await page.goto('./');
  const link = page.getByRole('link', { name: 'Declaration of LOG_LEVEL', exact: true });
  const tip = page.locator('.matrix__key-tip').filter({ hasText: /^LOG_LEVEL$/ });
  const showPreview = async () => {
    if (isMobile) await link.tap();
    else {
      await page.mouse.move(0, 0);
      await link.hover();
    }
    await expect(tip).toBeVisible();
  };

  if (!isMobile) {
    await showPreview();
    await page.mouse.move(0, 0);
    await expect(tip).toBeHidden();
    await test.step('a hovering pen opens and dismisses the preview', async () => {
      await link.dispatchEvent('pointerover', { pointerType: 'pen', relatedTarget: null });
      await link.dispatchEvent('mouseover', { relatedTarget: null });
      await expect(tip).toBeVisible();
      await link.dispatchEvent('pointerout', { pointerType: 'pen', relatedTarget: null });
      await link.dispatchEvent('mouseout', { relatedTarget: null });
      await expect(tip).toBeHidden();
    });
  }

  await showPreview();
  const heading = page.getByRole('heading', { name: 'Environment matrix', exact: true });
  if (isMobile) await heading.tap();
  else await heading.click();
  await expect(tip).toBeHidden();

  await showPreview();
  await page.keyboard.press('Escape');
  await expect(tip).toBeHidden();

  await showPreview();
  await link.focus();
  await page.keyboard.press('Tab');
  await expect(link).not.toBeFocused();
  await expect(tip).toBeHidden();
  await expect(page).toHaveURL(new RegExp(`#${DEMO_MATRIX_PATH}$`));
});

test('keyboard activation opens a matrix declaration without a preview tap', async ({ page }) => {
  await page.goto('./');
  const link = page.getByRole('link', { name: 'Declaration of LOG_LEVEL', exact: true });
  await link.focus();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(new RegExp(`#${DEMO_MATRIX_PATH}/keys/key_11111111-1111-4111-8111-000000000001$`));
  await expect(page.getByRole('heading', { name: 'LOG_LEVEL', exact: true })).toBeVisible();
});

test('developer stages drafts, cannot publish or edit production, and reset clears them', async ({ page }) => {
  await page.goto('./');
  await expect(page.getByRole('heading', { name: 'Environment matrix', exact: true })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Try as' })).toHaveValue('developer');
  await page.getByRole('button', { name: /^LOG_LEVEL in development:/ }).click();
  await page.getByRole('textbox', { name: 'development value' }).fill('trace');
  await page.getByRole('button', { name: 'Save 1 draft', exact: true }).click();
  await page.getByRole('button', { name: /Publish drafts/ }).click();
  await expect(page.getByRole('button', { name: /Publish selected/ })).toBeDisabled();
  await expect(page.getByText('Publishing is available to the Demo admin account.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: /^LOG_LEVEL in production:/ }).click();
  await expect(page.getByRole('textbox', { name: 'production value' })).toHaveAttribute('readonly', '');
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'Reset demo' }).click();
  await expect(page.getByRole('button', { name: /Publish drafts/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^LOG_LEVEL in development:/ })).toHaveText('debug');
});

test('admin publishes, browses new history, reveals fake secrets and switches to a fresh viewer', async ({ page }) => {
  await page.goto('./?account=admin');
  await page.getByRole('button', { name: /^LOG_LEVEL in development:/ }).click();
  await page.getByRole('textbox', { name: 'development value' }).fill('trace');
  await page.getByRole('button', { name: 'Save 1 draft', exact: true }).click();
  await page.getByRole('button', { name: /Publish drafts/ }).click();
  await page.getByRole('button', { name: /Publish selected/ }).click();
  await expect(page.getByRole('button', { name: /^LOG_LEVEL in development:/ })).toHaveText('trace');
  await page.getByRole('link', { name: 'rev 2 · history', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Revision history', exact: true })).toBeVisible();
  await page.goto(`./?account=admin#${DEMO_MATRIX_PATH}`);
  await page.getByRole('button', { name: /^AUTH_SECRET in development:/ }).click();
  await page.getByRole('button', { name: 'Reveal AUTH_SECRET', exact: true }).click();
  await expect(page.getByText('DEMO_ONLY_development_not_a_real_secret', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('combobox', { name: 'Try as' }).selectOption('viewer');
  await expect(page.getByRole('combobox', { name: 'Try as' })).toHaveValue('viewer');
  await expect(page.getByRole('button', { name: /^LOG_LEVEL in development:/ })).toHaveText('debug');
  await page.getByRole('button', { name: /^AUTH_SECRET in development:/ }).click();
  await expect(page.getByRole('button', { name: 'Reveal AUTH_SECRET', exact: true })).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'development value' })).toHaveAttribute('readonly', '');
});

test('settings and members are read-only, remote connections stay disabled, and deep links reload', async ({ page }) => {
  const project = `/orgs/${DEMO_IDS.org}/projects/${DEMO_IDS.project}`;
  await page.goto(`./?account=admin#${project}/settings`);
  await expect(page.getByRole('heading', { name: /^Project settings\s*·?\s*Payments$/ })).toBeVisible();
  // These surfaces use the `actions` read-only mode (not a disabled `fieldset`):
  // tabs and other navigation stay clickable; mutating controls disable
  // individually, the same way `ui/Button` and the raw controls beside it do.
  await expect(page.getByText(/^Read-only sample data\./)).toBeVisible();
  await page.getByRole('tab', { name: 'Metadata' }).click();
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toBeDisabled();
  await page.getByRole('tab', { name: 'Policy' }).click();
  await expect(page.getByRole('button', { name: 'Download definitions bundle', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Download portable bundle', exact: true })).toBeDisabled();
  for (const control of await page.locator('#project-policy input, #project-policy select, #project-policy textarea, #project-policy button').all()) {
    await expect(control).toBeDisabled();
  }
  await page.getByRole('tab', { name: 'Identity' }).click();
  await expect(page.getByRole('heading', { name: 'Identity', exact: true })).toBeVisible();
  // Downloads must not navigate around the fetch adapter to a nonexistent API
  // on the static host.
  await expect(page.locator('a[href*="/api/"]')).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('heading', { name: /^Project settings\s*·?\s*Payments$/ })).toBeVisible();

  await page.goto(`./?account=admin#/orgs/${DEMO_IDS.org}/members`);
  await expect(page.getByText(/^Read-only sample data\./)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add access', exact: true })).toBeDisabled();
  await expect(page.getByText('Sam Rivera').first()).toBeVisible();
  await page.getByRole('searchbox', { name: 'Find a member' }).fill('no-such-member');
  await expect(page.getByText(/No member matches/)).toBeVisible();

  await page.goto('./?account=admin#/remotes');
  await expect(page.getByRole('heading', { name: 'Remotes', exact: true })).toBeVisible();
  await expect(page.getByText(/^Read-only sample data\./)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add a remote', exact: true })).toBeDisabled();

  await page.goto(`./?account=admin#${DEMO_MATRIX_PATH}?remote=other-instance`);
  await expect(page.getByRole('heading', { name: 'Not available in this demo' })).toBeVisible();
  await page.getByRole('link', { name: 'Back to the environment matrix' }).click();
  await expect(page.getByRole('heading', { name: 'Environment matrix', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

const orgPath = `/orgs/${DEMO_IDS.org}`;
const projectPath = `${orgPath}/projects/${DEMO_IDS.project}`;
// Both records are closed over the real router's SurfaceId union. A new route
// needs a deliberate demo policy AND an independently asserted browser outcome.
type SurfaceCheck = {
  [Id in SurfaceId]: { mode: (typeof DEMO_SURFACE_POLICIES)[Id]['mode'] } & (
    (typeof DEMO_SURFACE_POLICIES)[Id]['mode'] extends 'excluded'
      ? { result: 'matrix' | 'unavailable' }
      : { heading: string | RegExp; sample: string; tab?: string; environment?: string; level?: number }
  );
};
const surfaceChecks = {
  login: { mode: 'excluded', result: 'matrix' },
  signup: { mode: 'excluded', result: 'matrix' },
  'signup-verify': { mode: 'excluded', result: 'unavailable' },
  'establish-credential': { mode: 'excluded', result: 'unavailable' },
  overview: { mode: 'excluded', result: 'matrix' },
  projects: { mode: 'interactive', heading: 'Projects', level: 1, sample: 'Payments' },
  remotes: { mode: 'read-only', heading: 'Remotes', sample: 'European production (sample)' },
  members: { mode: 'read-only', heading: /^Members\s*·?\s*Acme$/, sample: 'Sam Rivera' },
  'org-settings': { mode: 'read-only', heading: /^Organisation settings\s*·?\s*Acme$/, sample: 'Payments', tab: 'Policy' },
  scim: { mode: 'read-only', heading: 'SCIM provisioning', sample: 'acme-sso' },
  audit: { mode: 'read-only', heading: 'Audit', sample: 'revision.published' },
  'instance-admin': { mode: 'excluded', result: 'unavailable' },
  'instance-config': { mode: 'excluded', result: 'unavailable' },
  'instance-members': { mode: 'excluded', result: 'unavailable' },
  settings: { mode: 'read-only', heading: 'Account & security', sample: 'Sample security key', tab: 'Sign-in factors' },
  matrix: { mode: 'interactive', heading: 'Environment matrix', sample: 'LOG_LEVEL' },
  history: { mode: 'interactive', heading: 'Revision history', sample: 'current r1' },
  'key-detail': { mode: 'interactive', heading: 'LOG_LEVEL', sample: 'Application log verbosity.' },
  values: { mode: 'interactive', heading: 'Values', sample: 'LOG_LEVEL' },
  'machine-access': { mode: 'read-only', heading: 'Machine access', sample: 'payments-api' },
  'change-approvals': { mode: 'read-only', heading: 'Change approvals', sample: 'Sample review: rotate the payment provider credential.', tab: 'Review queue', environment: DEMO_IDS.staging },
  'temporary-access': { mode: 'read-only', heading: 'Temporary access', sample: 'Sample request: investigate a checkout error.', environment: DEMO_IDS.staging },
  adapters: { mode: 'read-only', heading: 'Deployment adapters', sample: 'acme/payments' },
  'project-audit': { mode: 'read-only', heading: 'Project audit', sample: 'revision.published' },
  'project-settings': { mode: 'read-only', heading: /^Project settings\s*·?\s*Payments$/, sample: 'Identity' },
  'cli-login': { mode: 'excluded', result: 'unavailable' },
  'cli-reauth': { mode: 'excluded', result: 'unavailable' },
  'workspace-approve': { mode: 'excluded', result: 'unavailable' },
  'workspace-callback': { mode: 'excluded', result: 'unavailable' },
  'oidc-done': { mode: 'excluded', result: 'unavailable' },
  'saml-done': { mode: 'excluded', result: 'unavailable' },
} satisfies SurfaceCheck;

const routeParameters: Readonly<Record<string, string>> = {
  org: DEMO_IDS.org,
  project: DEMO_IDS.project,
  environment: DEMO_IDS.staging,
  key: 'key_11111111-1111-4111-8111-000000000001',
};

// Derive paths from the production registry too: changing a route cannot leave
// this suite exercising an obsolete URL that happens to redirect successfully.
for (const surface of SURFACES) {
  const check = surfaceChecks[surface.id];
  const path = surface.path.replace(/:([a-z]+)/gi, (_, parameter: string) => {
    const value = routeParameters[parameter];
    if (value === undefined) throw new Error(`Missing demo route parameter: ${parameter}`);
    return encodeURIComponent(value);
  });
  test(`${surface.id}: ${check.mode} for every demo account`, async ({ page }) => {
    expect(DEMO_SURFACE_POLICIES[surface.id].mode).toBe(check.mode);
    for (const role of ['viewer', 'developer', 'admin']) {
      await test.step(role, async () => {
        await page.goto(`./?account=${role}#${path}`);
        if (check.mode === 'excluded') {
          const heading = check.result === 'matrix' ? 'Environment matrix' : 'Not available in this demo';
          await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
          if (check.result === 'matrix') await expect(page).toHaveURL(new RegExp(`#${DEMO_MATRIX_PATH}$`));
          else await expect(page.getByRole('link', { name: 'Back to the environment matrix' })).toBeVisible();
        } else {
          await expect(page.getByRole('heading', { name: check.heading, exact: true, level: 'level' in check ? check.level : undefined })).toBeVisible();
          if (check.mode === 'read-only') await expect(page.getByText(/^Read-only/)).toBeVisible();
          if ('tab' in check) await page.getByRole('tab', { name: check.tab, exact: true }).click();
          if ('environment' in check) {
            await page.getByRole('combobox', { name: 'Environment', exact: true }).selectOption(check.environment);
          }
          await expect(page.getByRole('main').getByText(check.sample, { exact: false }).first()).toBeVisible();
        }
        await expect(page.getByText(/^Loading/)).toHaveCount(0);
        await expect(page.locator('main .alert')).toHaveCount(0);
        await expect(page.locator('a[href*="/api/"]')).toHaveCount(0);
      });
    }
  });
}

test('read-only details and filters work while management actions stay disabled', async ({ page }) => {
  await page.goto(`./?account=admin#${projectPath}/machine-access`);
  await expect(page.getByRole('button', { name: 'Create service account', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: /payments-api/ }).click();
  await expect(page.getByText('hik_demo_only', { exact: false }).first()).toBeVisible();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  for (const name of ['Federation', 'Kubernetes targets', 'Providers', 'Leases', 'SSH certificates', 'Certificates']) {
    await page.getByRole('tab', { name: new RegExp(`^${name}`) }).click();
    await expect(page.getByRole('main').getByText(/^Loading/)).toHaveCount(0);
    await expect(page.getByRole('main').locator('.alert')).toHaveCount(0);
    await expect(page.getByRole('main').getByText(/does not support delivery-target/)).toHaveCount(0);
    await expect(page.locator('a[href*="/api/"]')).toHaveCount(0);
  }

  await page.goto(`./?account=admin#${projectPath}/adapters`);
  await page.getByRole('button', { name: /PAYMENTS_.*acme/ }).click();
  await expect(page.getByRole('heading', { name: 'Target', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Resync', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Test connection', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Target', exact: true })).toHaveCount(0);

  await page.goto(`./?account=admin#${orgPath}/scim`);
  await page.getByRole('button', { name: 'Administer', exact: true }).click();
  await expect(page.getByText('Payments engineering').first()).toBeVisible();
  // The mapping form opens in the side panel for reading; its submit stays off.
  await page.getByRole('button', { name: 'Map a group', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Add mapping', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('tab', { name: 'Directory', exact: true }).click();
  await page.getByRole('searchbox', { name: 'Filter users by name' }).fill('Sam');
  await expect(page.getByRole('main').getByText('Sam Rivera', { exact: true })).toBeVisible();
  await expect(page.getByRole('main').getByText('Dana Chen', { exact: true })).toHaveCount(0);

  await page.goto(`./?account=admin#${projectPath}/audit`);
  await page.getByRole('textbox', { name: 'Operation', exact: true }).fill('revision.*');
  await page.getByRole('button', { name: 'Apply filter', exact: true }).click();
  await expect(page.getByRole('button', { name: /grant.created/ })).toHaveCount(0);
  await page.getByRole('button', { name: /revision.published/ }).click();
  await expect(page.getByRole('heading', { name: 'revision.published', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Export JSONL', exact: true })).toBeDisabled();
});
