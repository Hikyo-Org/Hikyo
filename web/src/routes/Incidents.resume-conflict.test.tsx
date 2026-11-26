// @vitest-environment happy-dom
import { getIncidentOp, listIncidentItemsOp, resumeIncidentOp } from '@hikyo/operations';
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { resumeBody, type Incident, type IncidentItem } from '../api/incidents.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { Incidents } from './Incidents.tsx';

const mocks = vi.hoisted(() => ({ parsed: vi.fn() }));
vi.mock('../api/client.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/client.ts')>()), parsed: mocks.parsed,
}));
afterEach(() => vi.clearAllMocks());

function item(id: string): IncidentItem {
  return { id, owner_function: 'session.revoke', object_kind: 'session', object_id: `obj_${id}`, outcome: 'pending', refused: false, attestable: false, updated_at: '2026-11-23T09:00:00Z' };
}

const incident: Incident = {
  id: 'inc_1', scope_kind: 'account', scope_id: 'usr_1', status: 'open', opened_by: 'usr_1', opened_at: '2026-11-23T09:00:00Z',
  counts: { pending: 2, refused: 0, unknown: 0, blocked: 0, done: 0, attested: 0, out_of_scope: 0 },
};

function button(root: HTMLElement, label: string): HTMLButtonElement {
  const found = [...root.querySelectorAll('button')].find((candidate) => candidate.textContent?.trim() === label);
  if (found === undefined) throw new Error(`no button ${label}`);
  return found;
}

// A 409 means another operator changed the open set. The dialog must show the
// current items and wait for a new confirmation: nothing is retried on its own.
it('refreshes the listed items after a conflict and asks for a new confirmation', async () => {
  let open = [item('itm_a'), item('itm_b')];
  const resumeCalls: unknown[] = [];
  mocks.parsed.mockImplementation(async (operation: object, args: { body?: unknown }) => {
    if (operation === getIncidentOp) return incident;
    if (operation === listIncidentItemsOp) return { items: open };
    if (operation === resumeIncidentOp) {
      resumeCalls.push(args.body);
      if (resumeCalls.length === 1) throw new ApiError(409, 'conflict', 'The incident has changed; list the items again.');
      return { examined: 2, confirmed: 2, unconfirmed: 0, failed: 0, blocked: 0, stale: 0, skipped: 0 };
    }
    throw new Error('unexpected operation');
  });
  const view = await renderForm(<MemoryRouter initialEntries={['/?incident=inc_1']}><Incidents /></MemoryRouter>);
  try {
    await settleTask();
    await act(async () => button(view.container, 'Resume open items').click());
    const dialogText = () => document.body.textContent ?? '';
    expect(dialogText()).toContain('obj_itm_a');

    // Another operator settles one item and reopens another before this one confirms.
    open = [item('itm_b'), item('itm_c')];
    const proof = document.querySelector<HTMLInputElement>('input[name="proof"]');
    if (proof === null) throw new Error('proof input missing');
    await act(async () => typeInto(proof, 'first-proof'));
    await act(async () => button(document.body, 'Run open items').click());
    await settleTask();

    // One call was sent, with the digest of the items first shown; nothing retried.
    expect(resumeCalls).toEqual([await resumeBody('first-proof', [item('itm_a'), item('itm_b')])]);
    // The dialog now lists the current items, the stale one is gone, and the proof must be re-entered.
    expect(dialogText()).toContain('obj_itm_c');
    expect(document.querySelector('.incident__resume-items')?.textContent).not.toContain('obj_itm_a');
    expect(document.querySelector<HTMLInputElement>('input[name="proof"]')?.value).toBe('');
    expect(dialogText()).toContain('The incident has changed');

    // Only a new confirmation sends the new set.
    const fresh = document.querySelector<HTMLInputElement>('input[name="proof"]');
    if (fresh === null) throw new Error('proof input missing after the conflict');
    await act(async () => typeInto(fresh, 'second-proof'));
    await act(async () => button(document.body, 'Run open items').click());
    await settleTask();
    expect(resumeCalls).toHaveLength(2);
    expect(resumeCalls[1]).toEqual(await resumeBody('second-proof', [item('itm_b'), item('itm_c')]));
  } finally { await view.unmount(); }
});

// A responder with neither a password nor an authenticator cannot resume: the server
// names that refusal by its detail, and the dialog says what to do, not that the
// session ended. A different 400 keeps the generic proof wording.
it.each([
  ['no_proof_credential', 'Your account has no password or authenticator to confirm this. Ask another responder with one to resume the incident.'],
  ['something_else', 'The incident scope or the proof is not valid.'],
])('words a 400 with detail %s', async (detail, expected) => {
  mocks.parsed.mockImplementation(async (operation: object) => {
    if (operation === getIncidentOp) return incident;
    if (operation === listIncidentItemsOp) return { items: [item('itm_a')] };
    if (operation === resumeIncidentOp) throw new ApiError(400, 'bad request', detail, undefined, [], undefined, undefined, 'bad_request');
    throw new Error('unexpected operation');
  });
  const view = await renderForm(<MemoryRouter initialEntries={['/?incident=inc_1']}><Incidents /></MemoryRouter>);
  try {
    await settleTask();
    await act(async () => button(view.container, 'Resume open items').click());
    const proof = document.querySelector<HTMLInputElement>('input[name="proof"]');
    if (proof === null) throw new Error('proof input missing');
    await act(async () => typeInto(proof, 'any-proof'));
    await act(async () => button(document.body, 'Run open items').click());
    await settleTask();
    expect(document.body.textContent).toContain(expected);
    expect(document.body.textContent).not.toContain('session ended');
  } finally { await view.unmount(); }
});
