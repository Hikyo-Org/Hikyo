import type { ValueOccurrence } from '@hikyo/client';
import { describe, expect, it } from 'vitest';

import {
  indexOccurrences,
  needsTrim,
  normalizeValue,
  parseDotenv,
  planEnvironment,
  suggestType,
} from './import-state.ts';

describe('parseDotenv', () => {
  it('parses assignments in file order with 1-based lines', () => {
    const { entries, errors } = parseDotenv('A=1\nB=two\n');
    expect(errors).toEqual([]);
    expect(entries).toEqual([
      { key: 'A', value: '1', line: 1 },
      { key: 'B', value: 'two', line: 2 },
    ]);
  });

  it('skips blank lines and leading-# comments only', () => {
    const { entries } = parseDotenv('\n  # a comment\nA=has#hash\n');
    // `#` after an unquoted value is part of the value, not an inline comment.
    expect(entries).toEqual([{ key: 'A', value: 'has#hash', line: 3 }]);
  });

  it('strips the export keyword but not exportKEY', () => {
    const { entries } = parseDotenv('export A=1\nEXPORTB=2\n');
    expect(entries).toEqual([
      { key: 'A', value: '1', line: 1 },
      { key: 'EXPORTB', value: '2', line: 2 },
    ]);
  });

  it('trims trailing \\r for CRLF files', () => {
    const { entries } = parseDotenv('A=1\r\nB=2\r\n');
    expect(entries.map((entry) => entry.value)).toEqual(['1', '2']);
  });

  it('decodes double-quoted escapes and keeps inner whitespace', () => {
    const { entries, errors } = parseDotenv('A="a\\nb"\nB="  spaced  "\n');
    expect(errors).toEqual([]);
    expect(entries[0]?.value).toBe('a\nb');
    expect(entries[1]?.value).toBe('  spaced  ');
  });

  it('treats single quotes as literal', () => {
    const { entries } = parseDotenv("A='a\\nb'\n");
    expect(entries[0]?.value).toBe('a\\nb');
  });

  it('refuses malformed lines by line, collecting all', () => {
    const { entries, errors } = parseDotenv('NOEQUALS\nlower=1\nA="unterminated\nB=ok\n');
    expect(entries).toEqual([{ key: 'B', value: 'ok', line: 4 }]);
    expect(errors.map((error) => error.line)).toEqual([1, 2, 3]);
    expect(errors[1]?.reason).toContain('upper-snake');
  });

  it('refuses an unknown escape and content after the closing quote', () => {
    expect(parseDotenv('A="x\\q"\n').errors[0]?.reason).toContain('unknown escape');
    expect(parseDotenv('A="x"trailing\n').errors[0]?.reason).toContain('after the closing quote');
  });

  it('refuses a duplicate key by name, keeping the first', () => {
    const { entries, errors } = parseDotenv('A=first\nA=second\n');
    expect(entries).toEqual([{ key: 'A', value: 'first', line: 1 }]);
    expect(errors[0]?.reason).toContain('already assigned on line 1');
  });
});

describe('normalizeValue / needsTrim', () => {
  it('flags surrounding whitespace', () => {
    expect(needsTrim(' x')).toBe(true);
    expect(needsTrim('x ')).toBe(true);
    expect(needsTrim('x')).toBe(false);
    expect(normalizeValue('  x  ')).toBe('x');
  });
});

describe('suggestType', () => {
  it('is string for an empty set', () => {
    expect(suggestType([])).toBe('string');
  });

  it('suggests boolean only for canonical true/false', () => {
    expect(suggestType(['true', 'false'])).toBe('boolean');
    expect(suggestType(['TRUE'])).toBe('string');
    expect(suggestType(['1'])).toBe('integer');
  });

  it('suggests integer within int64 and rejects wider magnitudes', () => {
    expect(suggestType(['0', '-42', '9223372036854775807'])).toBe('integer');
    expect(suggestType(['9223372036854775808'])).toBe('string');
    expect(suggestType(['+1'])).toBe('string');
    expect(suggestType(['0x1f'])).toBe('string');
  });

  it('suggests json only for objects/arrays, not scalars', () => {
    expect(suggestType(['{"a":1}'])).toBe('json');
    expect(suggestType(['[1,2]'])).toBe('json');
    expect(suggestType(['"scalar"'])).toBe('string');
  });

  it('suggests a type only when every value satisfies it', () => {
    expect(suggestType(['1', 'two'])).toBe('string');
  });
});

function occurrence(over: Partial<ValueOccurrence> & { name: string }): ValueOccurrence {
  return { declared: true, set: false, token: `tok-${over.name}`, ...over };
}

describe('planEnvironment', () => {
  const entries = [
    { key: 'NEW', value: 'n', line: 1 },
    { key: 'ABSENT', value: 'a', line: 2 },
    { key: 'SET_KEEP', value: 'k', line: 3 },
    { key: 'SET_OVER', value: 'o', line: 4 },
  ];
  const occurrences = indexOccurrences([
    occurrence({ name: 'NEW', declared: false }),
    occurrence({ name: 'ABSENT', set: false }),
    occurrence({ name: 'SET_KEEP', set: true }),
    occurrence({ name: 'SET_OVER', set: true }),
  ]);

  it('buckets new, absent, skipped, and overwritten keys', () => {
    const plan = planEnvironment(entries, occurrences, new Set(['SET_OVER']));
    expect(plan.newKeys).toEqual(['NEW']);
    expect(plan.imported).toEqual(['NEW', 'ABSENT', 'SET_OVER']);
    expect(plan.skipped).toEqual(['SET_KEEP']);
    expect(plan.collisions).toEqual(['SET_KEEP', 'SET_OVER']);
  });

  it('skips every set key when no overwrite is chosen', () => {
    const plan = planEnvironment(entries, occurrences, new Set());
    expect(plan.imported).toEqual(['NEW', 'ABSENT']);
    expect(plan.skipped).toEqual(['SET_KEEP', 'SET_OVER']);
  });
});
