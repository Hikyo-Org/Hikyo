// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../../testkit/renderForm.tsx';
import { dynamicProvider } from '../../testkit/machineAccess.ts';
import { ORG, PRJ, PROD, STAGING } from '../../testkit/ids.ts';
import {
  deletedRotation,
  haltedRotation,
  HELD_ROTATION,
  owedRuns,
  ROTATION,
  rotationKeys,
  rotationList,
  settledRuns,
} from '../../testkit/rotations.ts';
import { RotationsPanel } from './Rotations.tsx';

afterEach(() => {
  vi.unstubAllGlobals();
});

const base = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const environments = [
  { id: PROD, name: 'production' },
  { id: STAGING, name: 'staging' },
];
const providers = [dynamicProvider];

type Stub = { readonly calls: string[] };

/** Answers each rotation read from `routes`, keyed by path; anything else is a failure to see. */
function stub(routes: Readonly<Record<string, (url: URL) => Response>>): Stub {
  const calls: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const url = new URL(request.url);
    calls.push(`${request.method} ${url.pathname}${url.search}`);
    const answer = request.method === 'GET' ? routes[url.pathname] : undefined;
    if (answer === undefined) throw new Error(`unexpected ${request.method} ${url.pathname}`);
    return answer(url);
  }));
  return { calls };
}

const keysRoute = { [`${base}/keys`]: () => Response.json(rotationKeys) };
const listPath = (environment: string) => `${base}/environments/${environment}/rotations`;
const runsPath = (environment: string, rotation: string) => `${listPath(environment)}/${rotation}/runs`;

function buttonNamed(container: ParentNode, name: string): HTMLButtonElement {
  const found = [...container.querySelectorAll('button')].find((b) => b.getAttribute('aria-label') === name || b.textContent === name);
  if (found === undefined) throw new Error(`no button ${name}`);
  return found;
}

const mount = () => renderForm(<RotationsPanel org={ORG} project={PRJ} environments={environments} providers={providers} />);

it('lists each rotation with its keys, provider, state, schedule and expiry', async () => {
  stub({ ...keysRoute, [listPath(PROD)]: () => Response.json({ items: [...rotationList().items, haltedRotation()] }) });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const rows = [...container.querySelectorAll('tbody tr')];
    expect(rows).toHaveLength(3);
    const active = container.querySelector(`[data-rotation="${ROTATION}"]`)!;
    expect(active.textContent).toContain('Password key DATABASE_PASSWORD');
    expect(active.textContent).toContain('Role name key DATABASE_USER');
    expect(active.textContent).toContain('postgres');
    expect(active.textContent).toContain('db.internal.example.com:5432');
    expect(active.textContent).toContain('active');
    expect(active.textContent).toContain('Every 30 days, 1 day overlap');
    // A halted rotation says so in a word and is not scheduled.
    const halted = container.querySelector('[data-rotation="rot_123e4567-e89b-12d3-a456-426614174203"]')!;
    expect(halted.textContent).toContain('halted');
    expect(halted.textContent).toContain('Not scheduled');
    expect(container.querySelector('.notice')?.textContent).toContain('hikyo rotation resume');
  } finally {
    await unmount();
  }
});

it('is loud about a restore hold and names the command that releases it', async () => {
  stub({ ...keysRoute, [listPath(PROD)]: () => Response.json(rotationList()) });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const alert = container.querySelector('.alert');
    expect(alert?.getAttribute('role')).toBe('alert');
    expect(alert?.textContent).toContain(`RESTORE HOLD: rotation ${HELD_ROTATION} is held after a restore and starts no run`);
    expect(alert?.textContent).toContain(`hikyo rotation release-hold ${HELD_ROTATION}`);
    // The row says it in words too, and offers no release action.
    expect(container.querySelector(`[data-rotation="${HELD_ROTATION}"]`)?.textContent).toContain('Restore hold');
    expect(container.querySelector(`[data-rotation="${HELD_ROTATION}"]`)?.textContent).toContain('held: no run starts');
    expect([...container.querySelectorAll('button')].map((b) => b.textContent).join(' ')).not.toMatch(/release|resume|pause|delete|run now/i);
  } finally {
    await unmount();
  }
});

it('shows the run timeline with an ATTENTION line for each run that owes a drop', async () => {
  stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: () => Response.json(owedRuns),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Runs of rotation DATABASE_PASSWORD').click());
    await settleTask();
    const attention = [...container.querySelectorAll('.alert')].map((a) => a.textContent ?? '').filter((t) => t.includes('ATTENTION'));
    expect(attention).toHaveLength(3);
    expect(attention[0]).toContain('do NOT drop it at the provider until the keys have moved off it');
    expect(attention[1]).toContain('stays valid at least until 2026-10-29T12:30:45Z, and possibly longer');
    expect(attention[2]).toContain('no recorded expiry');
    const rows = [...container.querySelectorAll('[data-run]')];
    expect(rows).toHaveLength(5);
    expect(rows[0]!.textContent).toContain('revoke_refused');
    expect(rows[0]!.textContent).toContain('Owes a drop');
    expect(rows[0]!.textContent).toContain('published_versions_changed');
    expect(rows[2]!.textContent).toContain('manual');
    expect(rows[2]!.textContent).toContain('Skipped');
    expect(rows[3]!.textContent).not.toContain('Owes a drop');
    expect(container.textContent).toContain('Adapter targets not converged counts adapter (sync) targets only');
    // Hiding the runs removes the timeline.
    await act(async () => buttonNamed(container, 'Runs of rotation DATABASE_PASSWORD').click());
    expect(container.querySelector('[data-run]')).toBeNull();
  } finally {
    await unmount();
  }
});

it('says a rotation with no owed drop and no runs has not run yet', async () => {
  stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: () => Response.json(settledRuns),
    [runsPath(PROD, HELD_ROTATION)]: () => Response.json({ items: [] }),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Runs of rotation DATABASE_PASSWORD').click());
    await settleTask();
    expect(container.textContent).not.toContain('ATTENTION');
    await act(async () => buttonNamed(container, 'Runs of rotation REPORTING_PASSWORD').click());
    await settleTask();
    expect(container.textContent).toContain('This rotation has not run yet.');
  } finally {
    await unmount();
  }
});

it('reads back through every run page, because an old run can still owe a drop', async () => {
  const [first, ...rest] = owedRuns.items;
  const calls = stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: (url) =>
      url.searchParams.get('after') === null
        ? Response.json({ items: rest.slice(2), next_after: first!.id })
        : Response.json({ items: [first, ...rest.slice(0, 2)] }),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Runs of rotation DATABASE_PASSWORD').click());
    await settleTask();
    expect(container.querySelectorAll('[data-run]')).toHaveLength(5);
    expect(calls.calls.filter((c) => c.includes('/runs'))).toEqual([
      `GET ${runsPath(PROD, ROTATION)}?limit=500`,
      `GET ${runsPath(PROD, ROTATION)}?limit=500&after=${first!.id}`,
    ]);
    expect([...container.querySelectorAll('.alert')].filter((a) => a.textContent?.includes('ATTENTION'))).toHaveLength(3);
  } finally {
    await unmount();
  }
});

it('pages the rotation list and asks for deleted ones only on request', async () => {
  const calls = stub({
    ...keysRoute,
    [listPath(PROD)]: (url) => {
      if (url.searchParams.get('include_disabled') === 'true') return Response.json({ items: [deletedRotation()] });
      return url.searchParams.get('after') === null
        ? Response.json({ items: [rotationList().items[0]], next_after: ROTATION })
        : Response.json({ items: [rotationList().items[1]] });
    },
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1);
    await act(async () => buttonNamed(container, 'Load more rotations').click());
    await settleTask();
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    const include = container.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    await act(async () => include.click());
    await settleTask();
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1);
    expect(container.querySelector('tbody tr')?.textContent).toContain('disabled');
    expect(calls.calls.filter((c) => c.includes('/rotations')).map((c) => c.split('/rotations')[1])).toEqual([
      '',
      `?after=${ROTATION}`,
      '?include_disabled=true',
    ]);
  } finally {
    await unmount();
  }
});

it('reads each environment on its own and answers a refusal as the permission answer', async () => {
  const calls = stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [listPath(STAGING)]: () => Response.json({ error: 'not found' }, { status: 404 }),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const select = container.querySelector('select')!;
    const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set;
    await act(async () => {
      setter?.call(select, STAGING);
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await settleTask();
    expect(container.querySelector('.alert')?.textContent).toBe('The rotations could not be listed. Listing them needs read on this environment.');
    expect(container.querySelector('table')).toBeNull();
    expect(calls.calls.filter((c) => c.includes('/rotations'))).toHaveLength(2);
  } finally {
    await unmount();
  }
});

it('says so when an environment has no rotations', async () => {
  stub({ ...keysRoute, [listPath(PROD)]: () => Response.json({ items: [] }) });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    expect(container.textContent).toContain('No rotations in this environment yet.');
    expect(container.querySelector('table')?.querySelectorAll('tbody tr')).toHaveLength(0);
  } finally {
    await unmount();
  }
});

it('falls back to the ids when the key catalogue cannot be read', async () => {
  stub({
    [`${base}/keys`]: () => Response.json({ error: 'not found' }, { status: 404 }),
    [listPath(PROD)]: () => Response.json(rotationList()),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    expect(container.querySelector(`[data-rotation="${ROTATION}"]`)?.textContent).toContain('key_123e4567-e89b-12d3-a456-426614174101');
  } finally {
    await unmount();
  }
});
