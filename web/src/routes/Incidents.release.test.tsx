// @vitest-environment happy-dom
import { getIncidentOp, listIncidentItemsOp, releaseIncidentPrincipalOp } from '@hikyo/operations';
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import type { Incident, IncidentItem } from '../api/incidents.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { Incidents } from './Incidents.tsx';

const mocks = vi.hoisted(() => ({ parsed: vi.fn(), ok: vi.fn() }));
vi.mock('../api/client.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/client.ts')>()), parsed: mocks.parsed, ok: mocks.ok,
}));
afterEach(() => vi.clearAllMocks());

const held: IncidentItem = {
  id: 'itm_contain', owner_function: 'account.contain', object_kind: 'account', object_id: 'usr_held', outcome: 'done',
  refused: false, attestable: false, updated_at: '2026-11-23T09:00:00Z',
};
const session: IncidentItem = { ...held, id: 'itm_session', owner_function: 'session.revoke', object_kind: 'session', object_id: 'ses_1' };

const incident: Incident = {
  id: 'inc_1', scope_kind: 'account', scope_id: 'usr_held', status: 'open', opened_by: 'usr_op', opened_at: '2026-11-23T09:00:00Z',
  counts: { pending: 0, refused: 0, unknown: 0, blocked: 0, done: 2, attested: 0, out_of_scope: 0 },
};

function button(root: HTMLElement, label: string): HTMLButtonElement {
  const found = [...root.querySelectorAll('button')].find((candidate) => candidate.textContent?.trim() === label);
  if (found === undefined) throw new Error(`no button ${label}`);
  return found;
}

function serve(release: () => Promise<void>) {
  mocks.parsed.mockImplementation(async (operation: object) => {
    if (operation === getIncidentOp) return incident;
    if (operation === listIncidentItemsOp) return { items: [held, session] };
    throw new Error('unexpected operation');
  });
  mocks.ok.mockImplementation(async (operation: object) => {
    if (operation === releaseIncidentPrincipalOp) return release();
    throw new Error('unexpected bodyless operation');
  });
}

async function openDialog(view: { container: HTMLElement }, proof: string) {
  await settleTask();
  await act(async () => button(view.container, 'Release usr_held').click());
  const input = document.querySelector<HTMLInputElement>('input[name="proof"]');
  if (input === null) throw new Error('proof input missing');
  await act(async () => typeInto(input, proof));
  await act(async () => button(document.body, 'Release account').click());
  await settleTask();
}

it('offers a release only on a held account and sends the principal with the proof once', async () => {
  const sent: unknown[] = [];
  serve(async () => { sent.push(mocks.ok.mock.calls.at(-1)?.[1]); });
  const view = await renderForm(<MemoryRouter initialEntries={['/?incident=inc_1']}><Incidents /></MemoryRouter>);
  try {
    await settleTask();
    expect([...view.container.querySelectorAll('button')].filter((b) => b.textContent?.includes('Release'))).toHaveLength(1);
    await openDialog(view, 'one-time-code');
    expect(sent).toEqual([{ path: { incident: 'inc_1' }, body: { principal: 'usr_held', proof: 'one-time-code' } }]);
    expect(document.body.textContent).toContain('Account released.');
  } finally { await view.unmount(); }
});

// A responder with no proof credential is told to hand the release over, in its own
// words; a wrong proof and an ended session are not the same message.
it.each([
  [400, 'bad_request' as const, 'no_proof_credential', 'Ask another responder with one to release the account.'],
  [401, 'unauthenticated' as const, undefined, 'the password or code was not accepted'],
  [404, 'not_found' as const, undefined, 'No such incident, or you may not work it.'],
  [409, 'conflict' as const, undefined, 'This account cannot be released yet.'],
])('words a %i refusal of a release', async (status, code, detail, expected) => {
  serve(async () => { throw new ApiError(status, 'refused', detail, undefined, [], undefined, undefined, code); });
  const view = await renderForm(<MemoryRouter initialEntries={['/?incident=inc_1']}><Incidents /></MemoryRouter>);
  try {
    await openDialog(view, 'any-proof');
    expect(document.body.textContent).toContain(expected);
    expect(document.body.textContent).not.toContain('Your session ended');
  } finally { await view.unmount(); }
});

// Closing is refused while the incident still holds a contained account; the dialog says
// to release it first instead of the generic "changed or is closed".
it('words a refused close that still holds a contained account', async () => {
  mocks.parsed.mockImplementation(async (operation: object) => {
    if (operation === getIncidentOp) return incident;
    if (operation === listIncidentItemsOp) return { items: [held, session] };
    throw new ApiError(409, 'conflict', undefined, undefined, [], undefined, undefined, 'conflict');
  });
  const view = await renderForm(<MemoryRouter initialEntries={['/?incident=inc_1']}><Incidents /></MemoryRouter>);
  try {
    await settleTask();
    await act(async () => button(view.container, 'Close incident').click());
    const dialog = document.querySelector<HTMLElement>('dialog');
    if (dialog === null) throw new Error('close dialog missing');
    await act(async () => button(dialog, 'Close incident').click());
    await settleTask();
    expect(document.body.textContent).toContain('still holds a contained account');
  } finally { await view.unmount(); }
});
