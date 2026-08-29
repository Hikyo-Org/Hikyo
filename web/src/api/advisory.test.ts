import { describe, expect, it } from 'vitest';

import {
  parseAdvisoryEvent,
  signalsPollInterval,
  SIGNALS_FALLBACK_POLL_MS,
} from './advisory.ts';
import { advisoryInvalidations } from './matrix.ts';

const ref = { org: 'org_a', project: 'project_a' };
const envDev = 'env_01989abc-def0-7123-8123-123456789abc';
const envProd = 'env_01989abc-def0-7123-8123-123456789abd';
const keyId = 'key_01989abc-def0-7123-8123-123456789abc';

describe('advisory event boundary', () => {
  it('parses the three event types the advisory channel emits', () => {
    expect(
      parseAdvisoryEvent({ type: 'revision.published', environment_id: envDev, revision: 3 }),
    ).toEqual({ type: 'revision.published', environmentId: envDev, revision: 3n });
    expect(
      parseAdvisoryEvent({
        type: 'cell.changed',
        environment_id: envProd,
        key_id: keyId,
        name: 'LOG_LEVEL',
        revision: 4,
      }),
    ).toEqual({
      type: 'cell.changed',
      environmentId: envProd,
      keyId,
      keyName: 'LOG_LEVEL',
      revision: 4n,
    });
    expect(
      parseAdvisoryEvent({
        type: 'pending.staged',
        environment_id: envDev,
        key_id: keyId,
        name: 'LOG_LEVEL',
        actor_id: 'usr_self',
      }),
    ).toEqual({
      type: 'pending.staged',
      environmentId: envDev,
      keyId: keyId,
      keyName: 'LOG_LEVEL',
      actorId: 'usr_self',
    });
  });

  it('keeps an unknown event type from killing the stream', () => {
    expect(parseAdvisoryEvent({ type: 'environment.created', environment_id: envDev })).toBeNull();
  });

  it('refuses a known type whose payload the wire contract does not describe', () => {
    expect(() =>
      parseAdvisoryEvent({ type: 'revision.published', environment_id: envDev }),
    ).toThrow();
    expect(() => parseAdvisoryEvent({ type: 'cell.changed', environment_id: envDev })).toThrow();
    expect(() =>
      parseAdvisoryEvent({ type: 'pending.staged', environment_id: envDev, key_id: keyId }),
    ).toThrow();
    expect(() => parseAdvisoryEvent('retry: 100')).toThrow();
    expect(() => parseAdvisoryEvent({ type: 'cell.changed' })).toThrow();
    expect(() =>
      parseAdvisoryEvent({
        type: 'cell.changed',
        environment_id: envDev,
        key_id: keyId,
        name: 'LOG_LEVEL',
        revision: 0,
      }),
    ).toThrow(/positive revision/);
  });
});

describe('advisory invalidation mapping', () => {
  it('publishes refresh the moved environment, its signals, and its pending drafts', () => {
    expect(
      advisoryInvalidations(ref, {
        type: 'revision.published',
        environmentId: envDev,
        revision: 3n,
      }),
    ).toEqual([
      ['values', ref.org, ref.project, envDev],
      ['matrix-signals', ref.org, ref.project, envDev],
      ['matrix-pending', ref.org, ref.project, envDev],
    ]);
  });

  it('cell changes touch only the signals query, which cascades to values on advancement', () => {
    expect(
      advisoryInvalidations(ref, {
        type: 'cell.changed',
        environmentId: envProd,
        keyId: keyId,
        keyName: 'LOG_LEVEL',
        revision: 4n,
      }),
    ).toEqual([['matrix-signals', ref.org, ref.project, envProd]]);
  });

  it('staged drafts refresh the pending list and the write-presence cells', () => {
    expect(
      advisoryInvalidations(ref, {
        type: 'pending.staged',
        environmentId: envDev,
        keyId: keyId,
        keyName: 'LOG_LEVEL',
      }),
    ).toEqual([
      ['matrix-pending', ref.org, ref.project, envDev],
      ['matrix-signals', ref.org, ref.project, envDev],
    ]);
  });
});

describe('fallback poll selector', () => {
  it('polls exactly while the stream is not healthy', () => {
    expect(signalsPollInterval('healthy')).toBe(false);
    expect(signalsPollInterval('connecting')).toBe(SIGNALS_FALLBACK_POLL_MS);
    expect(signalsPollInterval('failed')).toBe(SIGNALS_FALLBACK_POLL_MS);
  });
});
