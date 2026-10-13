import { describe, expect, it } from 'vitest';

import { duration } from './duration.ts';

describe('duration', () => {
  it('spells the largest exact unit with a correct plural', () => {
    expect(duration(7_776_000)).toBe('90 days');
    expect(duration(86_400)).toBe('1 day');
    expect(duration(28_800)).toBe('8 hours');
    expect(duration(3_600)).toBe('1 hour');
    expect(duration(1_800)).toBe('30 minutes');
    expect(duration(60)).toBe('1 minute');
    expect(duration(90)).toBe('90 seconds');
    expect(duration(1)).toBe('1 second');
    expect(duration(0)).toBe('0 seconds');
  });
});
