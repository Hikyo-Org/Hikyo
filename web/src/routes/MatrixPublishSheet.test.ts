import { describe, expect, it } from 'vitest';

import { groupPendingEntries, type MatrixPendingEntry } from './MatrixPublishSheet.tsx';

const entry = (name: string, group?: MatrixPendingEntry['group']): MatrixPendingEntry => ({
  versionId: `ver_${name}`,
  keyId: `key_${name}`,
  name,
  classification: 'config',
  operation: 'set',
  ...(group === undefined ? {} : { group }),
});

describe('groupPendingEntries', () => {
  it('buckets linked keys together in first-seen order and leaves the rest flat', () => {
    const db = { id: 'grp_db', name: 'Database credentials' };
    const grouped = groupPendingEntries([
      entry('LOG_LEVEL'),
      entry('DB_HOST', db),
      entry('PORT'),
      entry('DB_PASS', db),
    ]);
    expect(grouped.map((bucket) => [bucket.group?.name, bucket.entries.map((e) => e.name)])).toEqual([
      ['Database credentials', ['DB_HOST', 'DB_PASS']],
      [undefined, ['LOG_LEVEL', 'PORT']],
    ]);
  });

  it('is a single flat bucket when nothing is linked', () => {
    expect(groupPendingEntries([entry('A'), entry('B')])).toEqual([
      { group: undefined, entries: [entry('A'), entry('B')] },
    ]);
    expect(groupPendingEntries([])).toEqual([]);
  });
});
