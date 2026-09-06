import { describe, expect, it } from 'vitest';

import type { MachineCredential } from '../api/identities.ts';
import { carriedClaims, nextTab, presetForBinding, seedClaims, tabLabel } from './MachineAccess.tsx';

/**
 * The replace form's seeding. A replacement inherits the predecessor's platform
 * and pinned claims; a wrong answer here would render the wrong fields or,
 * worse, silently drop a numeric pin, an int64 repository id that rounded
 * through a float would bind the wrong repository, so both are pinned here.
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

describe('carriedClaims', () => {
  it('preserves a custom pin no preset field renders, so replacement never weakens it', () => {
    const credential = binding([
      { claim: 'repository_id', number_value: 42n },
      { claim: 'repository_owner_id', number_value: 7n },
      { claim: 'event_name', string_value: 'push' },
      // A claim the operator added via the CLI, beyond the GitHub preset.
      { claim: 'environment', string_value: 'production' },
      { claim: 'ref_protected', bool_value: true },
    ]);
    const preset = presetForBinding(credential);
    const carried = carriedClaims(preset, credential);
    // The three preset claims are rendered as fields; the two extras are carried.
    expect(carried).toEqual([
      { claim: 'environment', string_value: 'production' },
      { claim: 'ref_protected', bool_value: true },
    ]);
    // And the preset fields are NOT double-counted in the carried set.
    for (const field of preset.claims) {
      expect(carried.some((pin) => pin.claim === field.claim)).toBe(false);
    }
  });

  it('carries nothing when every pin is a preset field', () => {
    const credential = binding([
      { claim: '/kubernetes.io/serviceaccount/uid', string_value: 'abc' },
    ]);
    expect(carriedClaims(presetForBinding(credential), credential)).toEqual([]);
  });
});

describe('tabLabel', () => {
  it('spells a known count, says unknown for a listing it lacks, and omits a count it cannot report', () => {
    expect(tabLabel('Service accounts', 3)).toBe('Service accounts (3)');
    expect(tabLabel('Leases', 'unknown')).toBe('Leases (unknown)');
    expect(tabLabel('Kubernetes targets', null)).toBe('Kubernetes targets');
  });
});

describe('nextTab', () => {
  const tabs = ['a', 'b', 'c'];
  it('roves with the arrows, wrapping, and jumps with Home and End', () => {
    expect(nextTab(tabs, 'a', 'ArrowRight')).toBe('b');
    expect(nextTab(tabs, 'c', 'ArrowRight')).toBe('a');
    expect(nextTab(tabs, 'a', 'ArrowLeft')).toBe('c');
    expect(nextTab(tabs, 'b', 'Home')).toBe('a');
    expect(nextTab(tabs, 'b', 'End')).toBe('c');
  });
  it('ignores every other key', () => {
    expect(nextTab(tabs, 'a', 'Enter')).toBeNull();
    expect(nextTab(tabs, 'a', 'ArrowDown')).toBeNull();
  });
});
