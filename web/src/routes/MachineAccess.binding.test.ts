import { describe, expect, it } from 'vitest';

import type { MachineCredential } from '../api/identities.ts';
import { presetForBinding, seedClaims } from './MachineAccess.tsx';

/**
 * The replace form's seeding. A replacement inherits the predecessor's platform
 * and pinned claims; a wrong answer here would render the wrong fields or,
 * worse, silently drop a numeric pin — an int64 repository id that rounded
 * through a float would bind the wrong repository — so both are pinned here.
 */

const binding = (claims: MachineCredential['required_claims']): MachineCredential => ({
  id: 'mcr_00000000-0000-0000-0000-000000000000',
  kind: 'oidc-federation',
  lifetime: 'finite',
  created_at: '2026-08-01T00:00:00Z',
  created_by: 'pr_00000000-0000-0000-0000-000000000000',
  expiring_soon: false,
  issuer: 'https://token.actions.githubusercontent.com',
  subject: 'repo:owner/repo:ref:refs/heads/main',
  audience: 'hikyo',
  required_claims: claims,
});

describe('presetForBinding', () => {
  it('detects GitHub Actions from its numeric repository pins', () => {
    const credential = binding([
      { claim: 'repository_id', number_value: 42n },
      { claim: 'repository_owner_id', number_value: 7n },
      { claim: 'event_name', string_value: 'push' },
    ]);
    expect(presetForBinding(credential).id).toBe('github-actions');
  });

  it('detects Kubernetes from its ServiceAccount UID pointer', () => {
    const credential = binding([
      { claim: '/kubernetes.io/serviceaccount/uid', string_value: 'abc' },
    ]);
    expect(presetForBinding(credential).id).toBe('kubernetes');
  });

  it('falls back to Kubernetes when nothing matches, never crashing', () => {
    expect(presetForBinding(binding(undefined)).id).toBe('kubernetes');
  });
});

describe('seedClaims', () => {
  it('stringifies a 64-bit repository id losslessly, past the float boundary', () => {
    const bigId = 9007199254740993n; // 2^53 + 1: unrepresentable as a JS number.
    const credential = binding([
      { claim: 'repository_id', number_value: bigId },
      { claim: 'repository_owner_id', number_value: 7n },
      { claim: 'event_name', string_value: 'push' },
    ]);
    const seeded = seedClaims(presetForBinding(credential), credential);
    expect(seeded['repository_id']).toBe('9007199254740993');
    expect(seeded['event_name']).toBe('push');
  });

  it('leaves a preset field blank when the predecessor did not pin it', () => {
    const credential = binding([{ claim: 'event_name', string_value: 'push' }]);
    const seeded = seedClaims(presetForBinding(credential), credential);
    expect(seeded['event_name']).toBe('push');
    expect(Object.values(seeded).some((value) => value === '')).toBe(true);
  });
});
