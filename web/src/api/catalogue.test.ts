import { describe, expect, it } from 'vitest';

import { ApiError, type RefusalFinding } from './client.ts';
import {
  catalogueRefusalText,
  environmentsUnder,
  presenceImpact,
  presenceImpactIsEmpty,
  scanFindings,
  type PresenceRulesLike,
} from './catalogue.ts';

const envs = ['env_a', 'env_b', 'env_c'];

const presence = (
  required: PresenceRulesLike['required_in'],
  forbidden: PresenceRulesLike['forbidden_in'] = { mode: 'none' },
): PresenceRulesLike => ({ required_in: required, forbidden_in: forbidden });

const finding: RefusalFinding = {
  rule_id: 'aws-access-token',
  surface: 'edit',
  locator: 'key.declaration.pattern',
  acknowledgement: 'tok_1',
};

/** Build a scanner-block refusal the way client.ts's `refusal` would. */
function scanRefusal(findings: readonly RefusalFinding[]): ApiError {
  return new ApiError(400, 'x', undefined, undefined, findings);
}

describe('environmentsUnder', () => {
  it('expands `all` to every environment and `none` to nothing', () => {
    expect(environmentsUnder({ mode: 'all' }, envs)).toEqual(envs);
    expect(environmentsUnder({ mode: 'none' }, envs)).toEqual([]);
  });

  it('intersects `explicit` with the environments that still exist', () => {
    expect(
      environmentsUnder({ mode: 'explicit', environment_ids: ['env_b', 'env_gone'] }, envs),
    ).toEqual(['env_b']);
  });
});

describe('presenceImpact', () => {
  it('names the environments a required rule newly covers', () => {
    const impact = presenceImpact(presence({ mode: 'none' }), presence({ mode: 'all' }), envs);
    expect(impact.requiredAdded).toEqual(envs);
    expect(impact.requiredRemoved).toEqual([]);
    expect(presenceImpactIsEmpty(impact)).toBe(false);
  });

  it('reports both directions across required and forbidden', () => {
    const before = presence(
      { mode: 'explicit', environment_ids: ['env_a'] },
      { mode: 'explicit', environment_ids: ['env_c'] },
    );
    const after = presence({ mode: 'explicit', environment_ids: ['env_b'] }, { mode: 'none' });
    const impact = presenceImpact(before, after, envs);
    expect(impact.requiredAdded).toEqual(['env_b']);
    expect(impact.requiredRemoved).toEqual(['env_a']);
    expect(impact.forbiddenRemoved).toEqual(['env_c']);
    expect(impact.forbiddenAdded).toEqual([]);
  });

  it('is empty when nothing changes', () => {
    const same = presence({ mode: 'all' });
    expect(presenceImpactIsEmpty(presenceImpact(same, same, envs))).toBe(true);
  });
});

describe('scanFindings', () => {
  it('returns the findings on a scanner refusal', () => {
    expect(scanFindings(scanRefusal([finding]))).toEqual([finding]);
  });

  it('is null for a plain refusal, an empty findings array, and a non-ApiError', () => {
    expect(scanFindings(new ApiError(400, 'x'))).toBeNull();
    expect(scanFindings(scanRefusal([]))).toBeNull();
    expect(scanFindings(new Error('boom'))).toBeNull();
  });
});

describe('catalogueRefusalText', () => {
  it('quotes the server caller-safe detail verbatim', () => {
    expect(
      catalogueRefusalText(new ApiError(400, 'x', 'key "PORT" fails its integer rule'), 'update the rules'),
    ).toBe('Refused: key "PORT" fails its integer rule');
  });

  it('names the action for permission, missing, and concurrency refusals', () => {
    expect(catalogueRefusalText(new ApiError(403, 'x'), 'delete the folder')).toContain(
      'permission to delete the folder',
    );
    expect(catalogueRefusalText(new ApiError(404, 'x'), 'rename the group')).toContain(
      'no longer exists',
    );
    expect(catalogueRefusalText(new ApiError(409, 'x'), 'update the rules')).toContain(
      'concurrent edit',
    );
  });
});
