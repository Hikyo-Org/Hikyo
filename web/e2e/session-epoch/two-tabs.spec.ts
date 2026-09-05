import { expect, test, type Route } from '@playwright/test';

function identity(owner: string) {
  const suffix = owner === 'A' ? '01' : '02';
  return {
    session: {
      id: `ses_123e4567-e89b-12d3-a456-4266141740${suffix}`,
      artifact: 'browser',
      created_at: '2026-08-22T10:00:00Z',
      idle_expires_at: '2099-08-22T10:30:00Z',
      absolute_expires_at: '2099-08-22T18:00:00Z',
      assurance: {
        method: 'local-password',
        factors: ['password'],
        authenticated_at: '2026-08-22T10:00:00Z',
      },
    },
    principal: {
      id: `prn_123e4567-e89b-12d3-a456-4266141740${suffix}`,
      kind: 'human',
      display_name: `Person ${owner}`,
    },
    capabilities: { instance_operator: false },
  };
}

function orgs(owner: string) {
  return { items: [{ id: 'org_123e4567-e89b-12d3-a456-426614174001', name: `${owner} private org` }], count: 1 };
}

for (const channel of ['BroadcastChannel', 'storage fallback']) {
  test(`two tabs discard A before delayed B identity settles via ${channel}`, async ({ context }) => {
    if (channel === 'storage fallback') {
      await context.addInitScript(() => {
        Object.defineProperty(globalThis, 'BroadcastChannel', { value: undefined, configurable: true });
      });
    }
    await context.addCookies([{
      name: '__Host-hikyo-csrf', value: 'A', url: 'https://localhost:4319/',
      secure: true, sameSite: 'Lax',
    }]);
    const heldIdentity: Route[] = [];
    const heldOrgs: Route[] = [];
    let delayOrgs = false;
    let releaseIdentity = false;
    let prematureBOrgs = 0;
    await context.route('**/api/v1/**', async (route) => {
      const url = new URL(route.request().url());
      const owner = route.request().headers()['cookie']?.includes('__Host-hikyo-csrf=B') ? 'B' : 'A';
      if (url.pathname === '/api/v1/auth/whoami') {
        if (owner === 'B' && !releaseIdentity) {
          heldIdentity.push(route);
          return;
        }
        await route.fulfill({ json: identity(owner) });
        return;
      }
      if (url.pathname === '/api/v1/me/orgs') {
        if (owner === 'B' && !releaseIdentity) prematureBOrgs += 1;
        if (owner === 'A' && delayOrgs) {
          heldOrgs.push(route);
          return;
        }
        await route.fulfill({ json: orgs(owner) });
        return;
      }
      await route.fulfill({ status: 404, json: {} });
    });
    const first = await context.newPage();
    const second = await context.newPage();
    const pages = [first, second];
    const browserErrors: string[] = [];
    for (const page of pages) page.on('pageerror', (error) => browserErrors.push(error.message));
    for (const page of pages) {
      await page.goto('/e2e/session-epoch/harness.html');
      await expect(page.getByTestId('owner')).toHaveText('Person A');
      await expect(page.getByTestId('query')).toHaveText('A private org');
      await page.getByRole('button', { name: 'Reveal A', exact: true }).click();
      await expect(page.getByTestId('disclosure')).toHaveText('A display-once secret');
      await expect(page.getByTestId('workspace')).toHaveText('A workspace bearer');
    }
    delayOrgs = true;
    for (const page of pages) {
      await page.getByRole('button', { name: 'Start delayed A request' }).click();
    }
    await expect.poll(() => heldOrgs.length).toBe(2);

    await context.addCookies([{
      name: '__Host-hikyo-csrf', value: 'B', url: 'https://localhost:4319/',
      secure: true, sameSite: 'Lax',
    }]);
    await first.getByRole('button', { name: 'Announce replacement' }).click();
    for (const page of pages) {
      await expect(page.getByTestId('owner')).toHaveText('checking');
      await expect(page.getByTestId('query')).toBeEmpty();
      await expect(page.getByTestId('disclosure')).toBeEmpty();
      await expect(page.getByTestId('workspace')).toBeEmpty();
      await expect(page.getByTestId('retired-queries')).toHaveText('0');
      await expect(page.getByTestId('retired-mutations')).toHaveText('0');
    }
    await expect.poll(() => heldIdentity.length).toBeGreaterThanOrEqual(2);
    expect(prematureBOrgs).toBe(0);

    releaseIdentity = true;
    await Promise.all(heldIdentity.map((route) => route.fulfill({ json: identity('B') })));
    for (const page of pages) {
      await expect(page.getByTestId('owner')).toHaveText('Person B');
      await expect(page.getByTestId('query')).toHaveText('B private org');
    }
    await Promise.all(heldOrgs.map((route) => route.fulfill({ json: orgs('A') })));
    for (const page of pages) {
      await expect(page.locator('body')).toHaveAttribute('data-late-result', 'rejected');
      await expect(page.getByTestId('owner')).toHaveText('Person B');
      await expect(page.getByTestId('query')).toHaveText('B private org');
      await expect(page.getByTestId('disclosure')).toBeEmpty();
      await expect(page.getByTestId('workspace')).toBeEmpty();
      await page.screenshot({ path: test.info().outputPath(`${channel}-${pages.indexOf(page)}.png`) });
    }
    expect(browserErrors).toEqual([]);
  });
}
