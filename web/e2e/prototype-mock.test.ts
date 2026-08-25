import { describe, expect, it } from 'vitest';

import {
  createPrototypeSessionTimes,
  prototypeReadFixture,
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

  it('serves every read needed by the finalized non-matrix app chrome', () => {
    const paths = [
      '/api/v1/auth/methods',
      '/api/v1/auth/identities',
      '/api/v1/me/sessions',
      '/api/v1/orgs',
      '/api/v1/instance/grants',
      '/api/v1/instance/credential-policy',
      '/api/v1/orgs/org_11111111-1111-4111-8111-111111111111/projects/prj_11111111-1111-4111-8111-111111111111',
      '/api/v1/orgs/org_11111111-1111-4111-8111-111111111111/retention',
      '/api/v1/orgs/org_11111111-1111-4111-8111-111111111111/projects/prj_11111111-1111-4111-8111-111111111111/retention',
      '/api/v1/orgs/org_11111111-1111-4111-8111-111111111111/projects/prj_11111111-1111-4111-8111-111111111111/definitions/settings',
    ];

    for (const path of paths) {
      expect(prototypeReadFixture(path), path).toMatchObject({ status: 200 });
    }
  });
});
