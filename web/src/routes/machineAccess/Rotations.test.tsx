// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../../testkit/renderForm.tsx';
import { dynamicProvider } from '../../testkit/machineAccess.ts';
import { ORG, PRJ, PROD, STAGING } from '../../testkit/ids.ts';
import {
  deletedRotation,
  haltedRotation,
  HALTED_ROTATION,
  HELD_ROTATION,
  owedRuns,
  ROTATION,
  rotationKeys,
  rotationList,
  settledRuns,
} from '../../testkit/rotations.ts';
import { MAX_RUN_PAGES } from '../../api/rotations.ts';
import { ReadOnlyContext } from '../../ui/ReadOnlyContext.ts';
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
function stub(routes: Readonly<Record<string, (url: URL) => Response | Promise<Response>>>): Stub {
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

const nthRun = (n: number) => ({ ...owedRuns.items[3]!, id: `rrn_123e4567-e89b-12d3-a456-4266141${String(70000 + n)}` });

const mount = () => renderForm(<RotationsPanel org={ORG} project={PRJ} environments={environments} environmentsRead="ready" providers={providers} />);

it('lists each rotation with its keys, provider, state, schedule and expiry', async () => {
  stub({ ...keysRoute, [listPath(PROD)]: () => Response.json({ items: [...rotationList().items, haltedRotation()] }) });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const rows = [...container.querySelectorAll('tbody tr')];
    expect(rows).toHaveLength(3);
    const active = container.querySelector(`[data-rotation="${ROTATION}"]`)!;
    expect(active.textContent).toContain('Password keyDATABASE_PASSWORD');
    expect(active.textContent).toContain('Role name keyDATABASE_USER');
    expect(active.textContent).toContain('postgres');
    expect(active.textContent).toContain('db.internal.example.com:5432');
    expect(active.textContent).toContain(dynamicProvider.id);
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

it('names why a rotation halted, in the CLI sentence, on the notice and on the row', async () => {
  stub({ ...keysRoute, [listPath(PROD)]: () => Response.json({ items: [...rotationList().items, haltedRotation()] }) });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const notice = container.querySelector('.notice')?.textContent ?? '';
    expect(notice).toContain(`Rotation ${HALTED_ROTATION} is halted (retries_exhausted): the retry window ran out before a run could publish or extend the credential.`);
    const row = container.querySelector(`[data-rotation="${HALTED_ROTATION}"]`)!;
    expect(row.textContent).toContain('Halt reason');
    expect(row.textContent).toContain('retries_exhausted: the retry window ran out');
    expect(row.textContent).toContain('Last run');
    expect(row.textContent).toContain('failed (retries_exhausted)');
    // No owed drop: no attention line or badge on this row.
    expect(row.textContent).not.toContain('Owes a drop');
    expect(row.textContent).not.toContain('ATTENTION');
  } finally {
    await unmount();
  }
});

it('reads a halt it cannot name as unknown, and quotes a word it does not know', async () => {
  const unknown = { ...haltedRotation(), halt_reason: 'unknown' };
  const future = { ...haltedRotation(), id: 'rot_123e4567-e89b-12d3-a456-426614174299', halt_reason: 'solar_flare' };
  stub({ ...keysRoute, [listPath(PROD)]: () => Response.json({ items: [unknown, future] }) });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const notice = container.querySelector('.notice')?.textContent ?? '';
    expect(notice).toContain(`Rotation ${HALTED_ROTATION} is halted (unknown): the server halted the rotation without a reason it can name.`);
    expect(notice).toContain('is halted ("solar_flare"): the server recorded a reason this client does not know.');
  } finally {
    await unmount();
  }
});

it('flags a rotation that owes a drop in words, and never says drop when a restore refused one', async () => {
  const plain = { ...rotationList().items[0]!, id: 'rot_123e4567-e89b-12d3-a456-426614174298', owed_drop_runs: 1, owed_drop_runs_versions_changed: 0 };
  stub({ ...keysRoute, [listPath(PROD)]: () => Response.json({ items: [...rotationList().items, plain] }) });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const restore = container.querySelector(`[data-rotation="${ROTATION}"]`)!.textContent ?? '';
    expect(restore).toContain('Do not drop: restore');
    expect(restore).not.toContain('Owes a drop');
    expect(restore).toContain('3 run(s) that owe a provider role a drop, 1 of them refused after a restore (published_versions_changed)');
    expect(restore).toContain('do NOT drop it at the provider until the keys have moved off it');
    expect(restore).toContain(`hikyo rotation runs ${ROTATION}`);
    expect(restore).toContain('Last run');
    expect(restore).toContain('revoke_refused (published_versions_changed)');
    const owed = container.querySelector(`[data-rotation="${plain.id}"]`)!.textContent ?? '';
    expect(owed).toContain('Owes a drop');
    expect(owed).toContain('1 run(s) that owe a provider role a drop and the role is still live; before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back');
    expect(owed).not.toContain('do NOT drop');
    // A rotation with nothing owed has neither badge nor line, and the page offers no act.
    const held = container.querySelector(`[data-rotation="${HELD_ROTATION}"]`)!.textContent ?? '';
    expect(held).not.toContain('Owes a drop');
    expect(held).not.toContain('Do not drop');
    expect(held).not.toContain('ATTENTION');
    expect([...container.querySelectorAll('button')].map((b) => b.textContent).join(' ')).not.toMatch(/release|resume|pause|delete|run now|drop it/i);
  } finally {
    await unmount();
  }
});

it('shows a deleted rotation that still owes a drop, marked deleted, in the default list', async () => {
  const gone = { ...deletedRotation(), owed_drop_runs: 1, owed_drop_runs_versions_changed: 0 };
  stub({ ...keysRoute, [listPath(PROD)]: () => Response.json({ items: [gone] }) });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const row = container.querySelector(`[data-rotation="${gone.id}"]`)!.textContent ?? '';
    expect(row).toContain('deleted');
    expect(row).not.toContain('disabled');
    expect(row).toContain('Owes a drop');
    expect(row).toContain(`rotation ${gone.id} (deleted) has 1 run(s) that owe a provider role a drop`);
    expect(row).toContain('check that neither key serves that role');
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
    expect(container.querySelector(`[data-rotation="${HELD_ROTATION}"]`)?.textContent).toContain('Held after a restore: no run starts');
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
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await settleTask();
    const attention = [...container.querySelectorAll('.alert')].map((a) => a.textContent ?? '').filter((t) => t.includes('ATTENTION'));
    expect(attention).toHaveLength(3);
    expect(attention[0]).toContain('do NOT drop it at the provider until the keys have moved off it');
    expect(attention[1]).toContain('stays valid at least until 2026-10-29T12:30:45Z, and possibly longer');
    expect(attention[2]).toContain('no recorded expiry');
    const rows = [...container.querySelectorAll('[data-run]')];
    expect(rows).toHaveLength(5);
    expect(rows[0]!.textContent).toContain('revoke_refused');
    // A restore may have put the role back: no badge says to drop it.
    expect(rows[0]!.textContent).toContain('published_versions_changed');
    expect(rows[0]!.textContent).toContain('Do not drop: restore');
    expect(rows[0]!.textContent).not.toContain('Owes a drop');
    expect(rows[1]!.textContent).toContain('Owes a drop');
    expect(rows[1]!.textContent).toContain('At least');
    expect(rows[2]!.textContent).toContain('manual');
    expect(rows[2]!.textContent).toContain('Skipped');
    expect(rows[3]!.textContent).not.toContain('Owes a drop');
    expect(container.textContent).toContain('Adapter targets not converged counts adapter (sync) targets only');
    // Hiding the runs removes the timeline.
    await act(async () => buttonNamed(container, 'Hide runs of DATABASE_PASSWORD').click());
    expect(container.querySelector('[data-run]')).toBeNull();
  } finally {
    await unmount();
  }
});

it('says a rotation with no owed drop and no runs has not run yet', async () => {
  const settledList = rotationList().items.map((r) => ({ ...r, owed_drop_runs: 0, owed_drop_runs_versions_changed: 0 }));
  stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json({ items: settledList }),
    [runsPath(PROD, ROTATION)]: () => Response.json(settledRuns),
    [runsPath(PROD, HELD_ROTATION)]: () => Response.json({ items: [] }),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await settleTask();
    expect(container.textContent).not.toContain('ATTENTION');
    await act(async () => buttonNamed(container, 'Show runs of REPORTING_PASSWORD').click());
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
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    // The second page is fetched by an effect after the first renders: wait for the state, not for ticks.
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.querySelectorAll('[data-run]')).toHaveLength(5);
    });
    expect(calls.calls.filter((c) => c.includes('/runs'))).toEqual([
      `GET ${runsPath(PROD, ROTATION)}?limit=500`,
      `GET ${runsPath(PROD, ROTATION)}?limit=500&after=${first!.id}`,
    ]);
    expect([...container.querySelectorAll('.alert')].filter((a) => a.textContent?.includes('ATTENTION'))).toHaveLength(3);
    expect(container.textContent).not.toContain('Loading more runs');
  } finally {
    await unmount();
  }
});

it('reads three pages that each answer at once, one after another', async () => {
  // Each page lands before the next render, so `isFetchingNextPage` is never seen as true:
  // the third page is only asked for because the page count changed.
  const ids = [1, 2, 3].map((n) => nthRun(n).id);
  const calls = stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: (url) => {
      const after = url.searchParams.get('after');
      const index = after === null ? 0 : ids.indexOf(after) + 1;
      const next = ids[index];
      return Response.json({ items: [nthRun(index + 1)], ...(index < ids.length - 1 && next !== undefined ? { next_after: next } : {}) });
    },
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.querySelectorAll('[data-run]')).toHaveLength(3);
    });
    expect(calls.calls.filter((c) => c.includes('/runs'))).toHaveLength(3);
    expect(container.textContent).not.toContain('Loading more runs');
    expect(container.textContent).not.toContain('are shown');
  } finally {
    await unmount();
  }
});

it('stops at the page bound and says older runs were not read', async () => {
  let served = 0;
  stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: () => {
      served += 1;
      return Response.json({ items: [nthRun(served)], next_after: nthRun(served).id });
    },
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    // Each page is fetched by an effect after the last renders: wait for the state it ends in.
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.textContent).toContain('Only the newest 20 runs are shown');
    });
    expect(served).toBe(MAX_RUN_PAGES);
    expect(container.querySelectorAll('[data-run]')).toHaveLength(MAX_RUN_PAGES);
    expect(container.textContent).toContain(`hikyo rotation runs ${ROTATION}`);
    expect(container.textContent).not.toContain('Loading more runs');
  } finally {
    await unmount();
  }
});

it('shows a run that appeared while the timeline was hidden when it is reopened', async () => {
  // Page 1 is cached and page 2 never landed, so the reopened timeline has a refetch of page 1
  // in flight and a next page to read: the next page must wait for the refetch, not cancel it.
  const [a1, a2, b] = [nthRun(1), nthRun(2), nthRun(3)];
  let reopened = false;
  stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: (url) => {
      if (url.searchParams.get('after') === null) {
        return Response.json({ items: reopened ? [b, a1] : [a1], next_after: a1.id });
      }
      return reopened ? Response.json({ items: [a2] }) : new Promise<Response>(() => {});
    },
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.querySelectorAll('[data-run]')).toHaveLength(1);
    });
    await act(async () => buttonNamed(container, 'Hide runs of DATABASE_PASSWORD').click());
    reopened = true;
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await vi.waitFor(async () => {
      await settleTask();
      expect([...container.querySelectorAll('[data-run]')].map((row) => row.getAttribute('data-run'))).toEqual([b.id, a1.id, a2.id]);
    });
  } finally {
    await unmount();
  }
});

it('does not read on from an old cursor when the reopened timeline fails to refetch', async () => {
  // Page 1 is cached with a next page unread; the reopen refetches page 1 and it fails. The old
  // pages still say a next page exists, but asking for it would continue from a stale cursor and
  // clear the error while a newer run is missing: the failure must stay, and nothing more is read.
  const [a1] = [nthRun(1)];
  let reopened = false;
  const calls = stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: (url) => {
      if (url.searchParams.get('after') !== null) return new Promise<Response>(() => {});
      return reopened ? Response.json({ error: 'boom' }, { status: 500 }) : Response.json({ items: [a1], next_after: a1.id });
    },
  });
  const afterCalls = () => calls.calls.filter((c) => c.includes('after=')).length;
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await vi.waitFor(async () => {
      await settleTask();
      expect(afterCalls()).toBe(1);
    });
    await act(async () => buttonNamed(container, 'Hide runs of DATABASE_PASSWORD').click());
    reopened = true;
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.textContent).toContain('The runs could not be listed. Reload to try again.');
    });
    await settleTask();
    expect(afterCalls()).toBe(1);
    expect(container.textContent).toContain('Only the newest 1 run is shown');
    expect(container.textContent).toContain('The runs could not be listed. Reload to try again.');
    expect(container.textContent).not.toContain('Loading more runs');
  } finally {
    await unmount();
  }
});

it('keeps the runs it has and says older ones were not read when a later page fails', async () => {
  const [first] = owedRuns.items;
  stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: (url) =>
      url.searchParams.get('after') === null
        ? Response.json({ items: [first], next_after: first!.id })
        : Response.json({ error: 'boom' }, { status: 500 }),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.textContent).toContain('Only the newest 1 run is shown');
    });
    expect(container.textContent).toContain('The runs could not be listed. Reload to try again.');
    expect([...container.querySelectorAll('.alert')].filter((a) => a.textContent?.includes('ATTENTION'))).toHaveLength(1);
    expect(container.textContent).not.toContain('Loading more runs');
  } finally {
    await unmount();
  }
});

it('shows the run toggle as an expandable control named by its visible text, and focuses the timeline', async () => {
  stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json(rotationList()),
    [runsPath(PROD, ROTATION)]: () => Response.json(settledRuns),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const toggle = buttonNamed(container, 'Show runs of DATABASE_PASSWORD');
    expect(toggle.textContent).toBe('Show runs');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(toggle.hasAttribute('aria-pressed')).toBe(false);
    await act(async () => toggle.click());
    await settleTask();
    const open = buttonNamed(container, 'Hide runs of DATABASE_PASSWORD');
    expect(open.textContent).toBe('Hide runs');
    expect(open.getAttribute('aria-expanded')).toBe('true');
    const timeline = container.querySelector(`#${open.getAttribute('aria-controls')}`);
    expect(timeline?.querySelector('h3')?.textContent).toBe('Runs of DATABASE_PASSWORD');
    expect(document.activeElement).toBe(timeline?.querySelector('h3'));
  } finally {
    await unmount();
  }
});

it('says both commands a halted rotation that is also held needs', async () => {
  stub({
    ...keysRoute,
    [listPath(PROD)]: () => Response.json({ items: [{ ...haltedRotation(), restore_hold: true }] }),
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const text = [...container.querySelectorAll('.alert, .notice')].map((a) => a.textContent ?? '').join('\n');
    expect(text).toContain('hikyo rotation resume');
    expect(text).toContain(`hikyo rotation release-hold ${HALTED_ROTATION}`);
    expect(text).toContain('first, then resume it');
    // No cause is named: the server stopped it, and the wire does not say why.
    expect(text).not.toContain('failed check');
  } finally {
    await unmount();
  }
});

it('does not call a project environment-less while its environments are still loading or refused', async () => {
  stub({});
  for (const [read, text] of [
    ['loading', 'Loading environments'],
    ['unavailable', 'environments could not be read'],
    ['ready', 'This project has no environments, so it has no rotations.'],
  ] as const) {
    const { container, unmount } = await renderForm(
      <RotationsPanel org={ORG} project={PRJ} environments={[]} environmentsRead={read} providers={providers} />,
    );
    try {
      await settleTask();
      expect(container.textContent).toContain(text);
      if (read !== 'ready') expect(container.textContent).not.toContain('no environments');
    } finally {
      await unmount();
    }
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
    expect(container.querySelector('tbody tr')?.textContent).toContain('deleted');
    expect(calls.calls.filter((c) => c.includes('/rotations')).map((c) => c.split('/rotations')[1])).toEqual([
      '',
      `?after=${ROTATION}`,
      '?include_disabled=true',
    ]);
  } finally {
    await unmount();
  }
});

/** A window refocus refetches the stale list; the test decides what that refetch and a next page answer. */
async function listWithMore(refetchFails: () => boolean, nextFails: () => boolean) {
  stub({
    ...keysRoute,
    [listPath(PROD)]: (url) => {
      if (url.searchParams.get('after') !== null) {
        return nextFails() ? Response.json({ error: 'boom' }, { status: 500 }) : Response.json({ items: [rotationList().items[1]] });
      }
      return refetchFails() ? Response.json({ error: 'boom' }, { status: 500 }) : Response.json({ items: [rotationList().items[0]], next_after: ROTATION });
    },
  });
  const mounted = await mount();
  await settleTask();
  return mounted;
}

it('hides "Load more rotations" while the list refetch has failed, so a stale list is not shown as current', async () => {
  let failing = false;
  const { container, unmount } = await listWithMore(() => failing, () => false);
  try {
    expect(buttonNamed(container, 'Load more rotations')).toBeDefined();
    failing = true;
    await act(async () => { window.dispatchEvent(new Event('visibilitychange')); });
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.textContent).toContain('The rotations could not be listed');
    });
    expect(container.textContent).not.toContain('Load more rotations');
    expect(container.querySelector('table')).toBeNull();
  } finally {
    await unmount();
  }
});

it('keeps "Load more rotations" to retry when only the next page failed', async () => {
  let failing = true;
  const { container, unmount } = await listWithMore(() => false, () => failing);
  try {
    await act(async () => buttonNamed(container, 'Load more rotations').click());
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.textContent).toContain('The rotations could not be listed');
    });
    expect(buttonNamed(container, 'Load more rotations')).toBeDefined();
    failing = false;
    await act(async () => buttonNamed(container, 'Load more rotations').click());
    await vi.waitFor(async () => {
      await settleTask();
      expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    });
    expect(container.textContent).not.toContain('could not be listed');
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
    expect(container.querySelector('table')).toBeNull();
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

it('keeps browsing controls usable in the read-only demo', async () => {
  stub({
    ...keysRoute,
    [listPath(PROD)]: (url) => (url.searchParams.get('after') === null
      ? Response.json({ items: [rotationList().items[0]], next_after: ROTATION })
      : Response.json({ items: [rotationList().items[1]] })),
    [runsPath(PROD, ROTATION)]: () => Response.json(owedRuns),
  });
  const { container, unmount } = await renderForm(
    <ReadOnlyContext value={true}>
      <RotationsPanel org={ORG} project={PRJ} environments={environments} environmentsRead="ready" providers={providers} />
    </ReadOnlyContext>,
  );
  try {
    await settleTask();
    expect(buttonNamed(container, 'Show runs of DATABASE_PASSWORD').disabled).toBe(false);
    expect(buttonNamed(container, 'Load more rotations').disabled).toBe(false);
    await act(async () => buttonNamed(container, 'Load more rotations').click());
    await settleTask();
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    await act(async () => buttonNamed(container, 'Show runs of DATABASE_PASSWORD').click());
    await settleTask();
    expect(container.querySelectorAll('[data-run]').length).toBeGreaterThan(0);
  } finally {
    await unmount();
  }
});
