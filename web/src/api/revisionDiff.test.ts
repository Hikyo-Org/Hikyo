import { describe, expect, it } from 'vitest';
import { zMaskedRevisionDiff } from './revisionDiff.ts';

const masked = {
  left_revision: 1, right_revision: 2,
  items: [{ key_id: 'key_0198aaaa-bbbb-7ccc-8ddd-eeeeffff0002', name: 'TOKEN', classification: 'secret', status: 'edited', revealed: false }],
};
describe('revision diff disclosure boundary', () => {
  it('accepts secret write-presence and rejects plaintext or equality oracles', () => {
    expect(zMaskedRevisionDiff.safeParse(masked).success).toBe(true);
    for (const change of [{ before: 'secret' }, { after: 'secret' }, { revealed: true }, { status: 'changed' }, { status: 'unchanged' }]) {
      expect(zMaskedRevisionDiff.safeParse({ ...masked, items: [{ ...masked.items[0], ...change }] }).success).toBe(false);
    }
  });
  it('accepts config old and new values including empty strings', () => {
    expect(zMaskedRevisionDiff.safeParse({ ...masked, items: [{ ...masked.items[0], classification: 'config', status: 'changed', revealed: true, before: '', after: 'next' }] }).success).toBe(true);
  });
});
