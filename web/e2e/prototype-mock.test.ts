import { describe, expect, it } from 'vitest';

import {
  createPrototypeSessionTimes,
  prototypeSessionTimes,
} from '../prototype/mock-api.ts';

describe('prototype mock session', () => {
  it('keeps a long-running mock server session unexpired', () => {
    const requestedAt = Date.parse('2026-08-25T10:57:00Z');
    const times = createPrototypeSessionTimes(requestedAt);
    const oneCenturyLater = Date.parse('2126-08-25T10:57:00Z');

    expect(Date.parse(times.created_at)).toBe(requestedAt);
    expect(Date.parse(times.idle_expires_at)).toBeGreaterThan(oneCenturyLater);
    expect(Date.parse(times.absolute_expires_at)).toBeGreaterThan(
      Date.parse(times.idle_expires_at),
    );
  });

  it('keeps the assurance identity stable across successive reads', () => {
    const firstRead = { ...prototypeSessionTimes };
    const secondRead = { ...prototypeSessionTimes };

    expect(secondRead).toEqual(firstRead);
  });
});
