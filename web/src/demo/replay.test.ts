import { describe, expect, it } from 'vitest';
import { containsTypedValue, typedValuePattern } from './replay.ts';

const contains = (text: string, value: string) => containsTypedValue(text, [typedValuePattern(value)]);

describe('containsTypedValue', () => {
  it('finds a typed value in rendered text, however short', () => {
    expect(contains('sk-real', 'sk-real')).toBe(true);
    expect(contains('LOG_LEVEL in development: abc', 'abc')).toBe(true);
    expect(contains('LOG_LEVEL in development: abc, unpublished draft set', 'abc')).toBe(true);
  });

  it('does not treat a half-typed value as part of a longer word', () => {
    expect(contains('Environment: development', 'deve')).toBe(false);
  });

  it('matches values holding regular expression characters literally', () => {
    expect(contains('token a.b*c(1)', 'a.b*c(1)')).toBe(true);
    expect(contains('token aXbbbc1', 'a.b*c(1)')).toBe(false);
  });

  it('leaves other text alone', () => {
    expect(contains('LOG_LEVEL in development: debug', 'sk-real')).toBe(false);
    expect(containsTypedValue('LOG_LEVEL', [])).toBe(false);
  });
});
