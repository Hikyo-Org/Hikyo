import { describe, expect, it } from 'vitest';

import { graphemes, limitMark } from './chrome-mark.ts';

describe('chrome mark', () => {
  it('counts grapheme clusters, not code units', () => {
    expect(graphemes('🚀')).toHaveLength(1);
    expect(graphemes('👨‍👩‍👧')).toHaveLength(1);
    expect(graphemes('é')).toHaveLength(1);
  });

  it('keeps at most two graphemes and trims', () => {
    expect(limitMark('  PO ')).toBe('PO');
    expect(limitMark('ABC')).toBe('AB');
    expect(limitMark('🚀🔐🌿')).toBe('🚀🔐');
    expect(limitMark('   ')).toBe('');
  });
});
