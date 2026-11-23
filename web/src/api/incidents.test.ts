import { describe, expect, it } from 'vitest';

import { isOpenItem, itemStateLabel, type IncidentItem } from './incidents.ts';

function item(over: Partial<IncidentItem>): IncidentItem {
  return {
    id: 'itm_00000000-0000-0000-0000-000000000001',
    owner_function: 'account.contain',
    object_kind: 'account',
    object_id: 'usr_00000000-0000-0000-0000-00000000000b',
    outcome: 'pending',
    refused: false,
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
