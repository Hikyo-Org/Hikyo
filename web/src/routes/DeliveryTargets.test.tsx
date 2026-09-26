// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it } from 'vitest';

import { ApiError } from '../api/client.ts';
import {
  combineDeliveryTargets,
  reportingSupport,
  type DeliveryTargetsView,
} from '../api/deliveryTargets.ts';
import { ORG, PRJ, PROD } from '../testkit/ids.ts';
import { listing, NOW, refusedTarget, staging, target, viewOf } from '../testkit/deliveryTargets.ts';
import { serviceAccount } from '../testkit/machineAccess.ts';
import { DeliveryTargetsPanel } from './DeliveryTargets.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

let root: Root | null = null;
let container: HTMLElement | null = null;

afterEach(async () => {
  if (root !== null) {
    await act(async () => root?.unmount());
    root = null;
  }
  container = null;
});

async function render(view: DeliveryTargetsView, known = true): Promise<HTMLElement> {
  container = document.createElement('div');
  root = createRoot(container);
  const mounted = root;
  await act(async () =>
    mounted.render(
      <MemoryRouter>
        <DeliveryTargetsPanel
          project={{ org: ORG, project: PRJ }}
          view={view}
          known={known}
          accounts={[serviceAccount]}
          now={NOW}
        />
      </MemoryRouter>,
    ),
  );
  return container;
}

/** The healthy treatment: the `reported` word, its controller line, or the ok tone. */
function healthyTreatment(page: HTMLElement): readonly string[] {
  const found: string[] = [];
  if (page.querySelector('.badge--ok, [data-state="ok"], .badge[data-state="reported"]') !== null) {
    found.push('reported badge');
  }
  if (page.textContent?.includes('reported by the controller') === true) {
    found.push('controller line');
  }
  if (/healthy/i.test(page.textContent ?? '')) found.push('the word healthy');
  return found;
}

describe('DeliveryTargetsPanel', () => {
  it('renders a fresh report with the controller line, never the ok tone', async () => {
    const page = await render(viewOf(listing({ targets: [target(0, 'reported')] })));
    expect(page.textContent).toContain('reported by the controller, 5 minutes ago');
    expect(page.querySelector('.badge--ok')).toBeNull();
  });

  it.each([
    ['stale', viewOf(listing({ targets: [target(0, 'stale')] })), 'stale'],
    ['refused', viewOf(listing({ targets: [refusedTarget(0)] })), 'refused'],
    ['reporter-revoked', viewOf(listing({ targets: [target(0, 'reporter-revoked')] })), 'reporter-revoked'],
    ['missing', viewOf(listing({})), 'unknown'],
  ])('never renders a %s row with the healthy treatment', async (_state, view, badge) => {
    const page = await render(view);
    expect(page.querySelector(`.badge[data-state="${badge}"]`)).not.toBeNull();
    expect(healthyTreatment(page)).toEqual([]);
  });

  it('says "no reports" for an empty listing, never healthy', async () => {
    const page = await render(viewOf(listing({ observed: false })));
    expect(page.textContent).toContain('No reports.');
    expect(healthyTreatment(page)).toEqual([]);
  });

  it('leaves an unreadable environment absent and a failed one unknown', async () => {
    const production = { id: PROD, name: 'production' };
    const failed = { id: 'env_123e4567-e89b-12d3-a456-426614174012', name: 'development' };
    const view = combineDeliveryTargets(
      [production, staging, failed],
      [
        { data: listing({ targets: [target(0, 'reported')] }), error: null, isPending: false },
        { data: undefined, error: new ApiError(404, 'not found'), isPending: false },
        { data: undefined, error: new ApiError(500, 'boom'), isPending: false },
      ],
    );
    expect(view.reports.map((r) => r.environment.name)).toEqual(['production']);
    expect(view.failures.map((f) => f.name)).toEqual(['development']);

    const page = await render({ ...view, support: 'supported' });
    expect(page.textContent).not.toContain('staging');
    expect(page.textContent).toContain('The delivery-target reports for development could not be read');
  });

  it('gives an empty panel "unknown" rather than "no reports" while a listing failed', async () => {
    const page = await render(
      {
        support: 'supported',
        reports: [],
        failures: [{ id: PROD, name: 'production' }],
        isPending: false,
      },
      false,
    );
    expect(page.textContent).not.toContain('No reports.');
    expect(page.textContent).toContain('unknown');
  });
});

describe('DeliveryTargetsPanel before the environments are read', () => {
  it('says unknown, never "no reports", for an empty view that is not yet known', async () => {
    const page = await render(
      { support: 'supported', reports: [], failures: [], isPending: false },
      false,
    );
    expect(page.textContent).not.toContain('No reports.');
    expect(page.textContent).not.toContain('No reporting service account');
    expect(page.textContent).toContain('unknown');
  });
});

describe('delivery-target reporting support', () => {
  it('reads support from any advertised report vocabulary', () => {
    expect(reportingSupport(['local-password', 'delivery-target-report/1'])).toBe('supported');
    expect(reportingSupport(['local-password', 'oidc'])).toBe('unsupported');
  });

  it('says the server does not support reporting, never "no reports"', async () => {
    const page = await render(
      { support: 'unsupported', reports: [], failures: [], isPending: false },
      false,
    );
    expect(page.textContent).toContain('This server does not support delivery-target reporting');
    expect(page.textContent).not.toContain('No reports.');
    expect(healthyTreatment(page)).toEqual([]);
  });
});
