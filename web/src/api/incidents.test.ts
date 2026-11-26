import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import { commonRefusalText } from './statusText.ts';
import { IncidentListTooLong, incidentFailureText, isOpenItem, itemStateLabel, notYetRun, resumeBody, type IncidentItem } from './incidents.ts';

function item(over: Partial<IncidentItem>): IncidentItem {
  return {
    id: 'itm_00000000-0000-0000-0000-000000000001',
    owner_function: 'account.contain',
    object_kind: 'account',
    object_id: 'usr_00000000-0000-0000-0000-00000000000b',
    outcome: 'pending',
    refused: false,
    attestable: false,
    updated_at: '2026-11-23T09:00:00Z',
    ...over,
  };
}

describe('incident item state', () => {
  it('tells a refused item from one that has not run yet', () => {
    expect(itemStateLabel(item({ refused: true, reason: 'owner_failed' }))).toBe('Refused by its owner');
    expect(itemStateLabel(item({}))).toBe('Not yet run');
  });

  it('treats pending, unknown and blocked as open, and the rest as settled', () => {
    const open: IncidentItem['outcome'][] = ['pending', 'unknown', 'blocked'];
    const settled: IncidentItem['outcome'][] = ['done', 'attested', 'out_of_scope'];
    expect(open.every((outcome) => isOpenItem(item({ outcome })))).toBe(true);
    expect(settled.some((outcome) => isOpenItem(item({ outcome })))).toBe(false);
  });
});

describe('incident failure text', () => {
  it('names the CLI when the item list outgrows the page cap, never showing a half list', () => {
    expect(incidentFailureText(new IncidentListTooLong())).toContain('hikyo incident items --open');
  });

  it('reads a wrong proof as a refused proof, not as a lost session', () => {
    const wrongProof = incidentFailureText(new ApiError(401, 'failed'));
    expect(wrongProof).toContain('password or code was not accepted');
    expect(wrongProof).not.toBe(commonRefusalText.sessionEnded);
  });
});

describe('resume request', () => {
  it('sends the digest of exactly the listed items, in any order', async () => {
    // The same vector pins the server's api.IncidentItemsDigest.
    const digest = '3dbcd4b098200b219ec0411f546330e8a2430e5baed3762ecb0a6280a4f5d8a4';
    expect(await resumeBody('123456', [item({ id: 'itm_b' }), item({ id: 'itm_a' })])).toEqual({ proof: '123456', items_digest: digest });
    expect((await resumeBody('123456', [item({ id: 'itm_a' })])).items_digest).not.toBe(digest);
    expect((await resumeBody('123456', [])).items_digest).not.toBe(digest);
  });
});

describe('not yet run', () => {
  it('excludes the refused items the server counts under pending', () => {
    expect(notYetRun({ pending: 5, refused: 2 })).toBe(3);
    expect(notYetRun({ pending: 1, refused: 3 })).toBe(0);
  });
});
