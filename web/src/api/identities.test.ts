import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  createServiceAccountFailureText,
  createServiceAccountRefusalText,
  deleteServiceAccountFailureText,
  deleteServiceAccountRefusalText,
  expiryLabel,
  grantWideningReach,
  lastUsedLabel,
  parseClaimNumber,
  postStateReach,
  pullRequestRefusal,
  scopeOf,
  serviceAccountNameRefusal,
  setupJourney,
  type Grant,
  type MachineCredential,
} from './identities.ts';

/**
 * The machine-access surface's derivations (#67).
 *
 * These are the pieces where a wrong answer is a SECURITY statement rather than
 * a layout bug: which environments a service account reaches, whether the mint
 * needs a disclosure ceremony, and whether a pull-request identity is being
 * bound. Each is a pure function precisely so it can be pinned here rather than
 * inferred from a screenshot.
 */

const ENVS = [
  { id: 'env_dev', name: 'development' },
  { id: 'env_prod', name: 'production' },
];

const grant = (
  principal: string,
  capability: string,
  scope: { project_id?: string; environment_id?: string },
): Grant => ({
  id: 'gr_00000000-0000-0000-0000-000000000000',
  principal_id: principal,
  capability,
  scope: { org_id: 'org_x', project_id: 'prj_x', ...scope },
  origins: [],
  created_at: '2026-08-13T00:00:00Z',
});

describe('scopeOf', () => {
  it('reads one environment-scoped grant as reach on that environment only', () => {
    const scope = scopeOf([grant('mp_a', 'read', { environment_id: 'env_dev' })], 'mp_a', ENVS);
    expect(scope).toEqual([
      { id: 'env_dev', name: 'development', read: true, reveal: false },
      { id: 'env_prod', name: 'production', read: false, reveal: false },
    ]);
  });

  it('lets a project-scoped grant reach every environment beneath it', () => {
    // The ordinary downward inheritance. A listing confined to one project has
    // no wider row, so an absent environment_id can only mean project scope.
    const scope = scopeOf([grant('mp_a', 'read', {})], 'mp_a', ENVS);
    expect(scope.every((s) => s.read)).toBe(true);
  });

  it('never reads another principal\'s grant as this one\'s', () => {
    const scope = scopeOf([grant('mp_other', 'read', { environment_id: 'env_dev' })], 'mp_a', ENVS);
    expect(scope.some((s) => s.read)).toBe(false);
  });
});

describe('postStateReach', () => {
  it('is empty without reveal, however read is granted', () => {
    // The state every workload principal is in today: the permission model's
    // machine allowlist admits `read` and nothing else, so nothing this
    // account holds reaches plaintext and the mint's disclosure conjunct is
    // vacuous.
    const scope = scopeOf([grant('mp_a', 'read', { environment_id: 'env_dev' })], 'mp_a', ENVS);
    expect(postStateReach(scope)).toEqual([]);
  });

  it('is empty with reveal but no read: no delivery means no plaintext', () => {
    const scope = scopeOf([grant('mp_a', 'reveal', { environment_id: 'env_dev' })], 'mp_a', ENVS);
    expect(postStateReach(scope)).toEqual([]);
  });

  it('names the environment when both are held', () => {
    const scope = scopeOf(
      [
        grant('mp_a', 'read', { environment_id: 'env_dev' }),
        grant('mp_a', 'reveal', { environment_id: 'env_dev' }),
      ],
      'mp_a',
      ENVS,
    );
    expect(postStateReach(scope).map((s) => s.name)).toEqual(['development']);
  });
});

describe('grantWideningReach', () => {
  // The mint's conjunct for a GRANT is the DELTA, not the whole post-state:
  // `checkMachineWidening` computes exactly that server-side, so a client
  // asking for a ceremony over everything the account already reaches would
  // prompt for authority the server never consumes.
  it('is empty for a read grant on an account that cannot decrypt', () => {
    // The state of every workload principal today.
    expect(grantWideningReach(scopeOf([], 'mp_a', ENVS), 'env_dev', 'read')).toEqual([]);
  });

  it('names the environment when the read grant completes an existing reveal', () => {
    // `reveal` without `read` reaches nothing; adding `read` is what turns it
    // into a working path to plaintext, and that IS a widening.
    const scope = scopeOf([grant('mp_a', 'reveal', { environment_id: 'env_dev' })], 'mp_a', ENVS);
    expect(grantWideningReach(scope, 'env_dev', 'read').map((s) => s.name)).toEqual(['development']);
  });

  it('is empty when the environment was already reachable', () => {
    const scope = scopeOf(
      [
        grant('mp_a', 'read', { environment_id: 'env_dev' }),
        grant('mp_a', 'reveal', { environment_id: 'env_dev' }),
      ],
      'mp_a',
      ENVS,
    );
    expect(grantWideningReach(scope, 'env_dev', 'read')).toEqual([]);
  });

  it('never widens an environment the grant does not name', () => {
    const scope = scopeOf([grant('mp_a', 'reveal', {})], 'mp_a', ENVS);
    expect(grantWideningReach(scope, 'env_dev', 'read').map((s) => s.id)).toEqual(['env_dev']);
  });
});

describe('parseClaimNumber', () => {
  // An immutable repository id is what stops a renamed-and-reused path
  // inheriting a production binding. Every case below is a way `Number()`
  // silently binds the wrong repository.
  it('takes a plain integer', () => {
    expect(parseClaimNumber('4242')).toBe(4242);
    expect(parseClaimNumber(' 4242 ')).toBe(4242);
    expect(parseClaimNumber('-7')).toBe(-7);
  });

  it('refuses an empty field rather than binding repository 0', () => {
    expect(parseClaimNumber('')).toBeNull();
    expect(parseClaimNumber('   ')).toBeNull();
  });

  it('refuses anything that is not digits', () => {
    expect(parseClaimNumber('1e3')).toBeNull();
    expect(parseClaimNumber('4242.7')).toBeNull();
    expect(parseClaimNumber('0x10')).toBeNull();
    expect(parseClaimNumber('42abc')).toBeNull();
  });

  it('refuses a value past the range JSON carries exactly', () => {
    // 2^53 + 1 rounds to 2^53 — a DIFFERENT, existing repository id.
    expect(parseClaimNumber('9007199254740993')).toBeNull();
    expect(parseClaimNumber('9007199254740991')).toBe(9_007_199_254_740_991);
  });
});

describe('setupJourney', () => {
  it('has no journey for an automation principal', () => {
    expect(setupJourney('automation', [], false)).toBeNull();
  });

  it('waits on the read grant before anything else', () => {
    const steps = setupJourney('workload', scopeOf([], 'mp_a', ENVS), false) ?? [];
    expect(steps.map((s) => s.state)).toEqual(['done', 'next', 'next', 'next', 'unavailable']);
  });

  it('marks delivery done once read is granted, and gates reveal on the opt-in', () => {
    const scope = scopeOf([grant('mp_a', 'read', { environment_id: 'env_dev' })], 'mp_a', ENVS);
    const steps = setupJourney('workload', scope, false) ?? [];
    expect(steps[1]?.title).toBe('read granted — development');
    expect(steps[2]?.state).toBe('done');
    // With the opt-in off the grant API refuses reveal, so the step says so
    // rather than offering a control the server refuses every time.
    expect(steps[3]?.state).toBe('next');
    expect(steps[4]?.state).toBe('unavailable');
  });

  it('offers the reveal grant once the project has opted in, and closes it once held', () => {
    const reading = scopeOf([grant('mp_a', 'read', { environment_id: 'env_dev' })], 'mp_a', ENVS);
    const steps = setupJourney('workload', reading, true) ?? [];
    expect(steps[3]?.state).toBe('done');
    expect(steps[4]?.state).toBe('next');
    const revealing = scopeOf(
      [
        grant('mp_a', 'read', { environment_id: 'env_dev' }),
        grant('mp_a', 'reveal', { environment_id: 'env_dev' }),
      ],
      'mp_a',
      ENVS,
    );
    const held = setupJourney('workload', revealing, true) ?? [];
    expect(held[4]?.state).toBe('done');
    expect(held[4]?.title).toBe('reveal granted — development');
  });
});

const credential = (over: Partial<MachineCredential>): MachineCredential => ({
  id: 'mcr_00000000-0000-0000-0000-000000000000',
  kind: 'hikyo-token',
  lifetime: 'finite',
  created_at: '2026-08-01T00:00:00Z',
  created_by: 'pr_00000000-0000-0000-0000-000000000000',
  expiring_soon: false,
  ...over,
});

describe('expiryLabel', () => {
  const now = new Date('2026-08-13T00:00:00Z');

  it('counts the days left', () => {
    expect(expiryLabel(credential({ expires_at: '2026-08-27T00:00:00Z' }), now)).toBe(
      'expires in 14 days',
    );
  });

  it('says expired rather than a negative count', () => {
    expect(expiryLabel(credential({ expires_at: '2026-08-01T00:00:00Z' }), now)).toBe('expired');
  });

  it('says revoked first, whatever the expiry says', () => {
    const dead = credential({ expires_at: '2026-12-01T00:00:00Z', revoked_at: '2026-08-02T00:00:00Z' });
    expect(expiryLabel(dead, now)).toBe('revoked');
  });

  it('states an indefinite lifetime as a fact, not as a large number', () => {
    expect(expiryLabel(credential({ lifetime: 'indefinite' }), now)).toBe('no expiry');
  });
});

describe('lastUsedLabel', () => {
  it('keeps never-used and used-at-the-epoch different facts', () => {
    expect(lastUsedLabel(credential({}))).toBe('never used');
    expect(lastUsedLabel(credential({ last_used_at: '1970-01-01T00:00:00Z' }))).toBe(
      'last used 1970-01-01',
    );
  });
});

describe('pullRequestRefusal', () => {
  it('refuses both pull-request events by name', () => {
    expect(pullRequestRefusal('pull_request')).toContain('pull_request');
    expect(pullRequestRefusal('pull_request_target')).toContain('pull_request_target');
  });

  it('passes the ordinary events', () => {
    expect(pullRequestRefusal('push')).toBeNull();
    expect(pullRequestRefusal('workflow_dispatch')).toBeNull();
  });

  it('is not fooled by an event that merely contains the word', () => {
    // The rule is about the pinned event being one of exactly two values, not
    // about the string looking pull-request-ish.
    expect(pullRequestRefusal('pull_request_review')).toBeNull();
  });
});

describe('serviceAccountNameRefusal', () => {
  it('refuses an empty name, and only an empty one', () => {
    // The server checks `name == "" || len(name) > 64` — byte-for-byte, no trim.
    // A whitespace-only name is server-legal, so the client does not invent a
    // stricter rule that would refuse what the server accepts.
    expect(serviceAccountNameRefusal('')).not.toBeNull();
    expect(serviceAccountNameRefusal('   ')).toBeNull();
  });

  it('refuses a name past the 64-BYTE ceiling, counting bytes not code units', () => {
    expect(serviceAccountNameRefusal('a'.repeat(65))).not.toBeNull();
    expect(serviceAccountNameRefusal('a'.repeat(64))).toBeNull();
    // 22 three-byte characters is 66 bytes but only 22 UTF-16 units: a
    // code-unit check would wave it through and the server would 400.
    expect(serviceAccountNameRefusal('€'.repeat(22))).not.toBeNull();
    expect(serviceAccountNameRefusal('€'.repeat(21))).toBeNull();
  });

  it('accepts an ordinary name', () => {
    expect(serviceAccountNameRefusal('ci-deploy')).toBeNull();
  });
});

describe('createServiceAccountRefusalText', () => {
  it('names the duplicate-name / limit conflict on 409, not the credential ceiling', () => {
    const text = createServiceAccountRefusalText(new ApiError(409, 'conflict'));
    // The account-create 409 is a duplicate live name or a structural limit —
    // never the "live-credential ceiling or identical binding" the mint's 409
    // is. A wrong sentence here sends an operator to look at credentials.
    expect(text).toContain('name');
    expect(text).not.toContain('binding');
  });

  it('names the name constraint on 400', () => {
    expect(createServiceAccountRefusalText(new ApiError(400, 'bad'))).toContain('64');
  });

  it('never demands disclosure or reauth on 403 — create needs neither', () => {
    const text = createServiceAccountRefusalText(new ApiError(403, 'no'));
    expect(text).toContain('manage-identities');
    expect(text).not.toContain('reauth');
    expect(text).not.toContain('disclosure');
  });

  it('names the PROJECT, not a service account, on 404', () => {
    // A create 404 is the project (or the authorization mask), never a
    // service account that "is no longer here".
    const text = createServiceAccountRefusalText(new ApiError(404, 'gone'));
    expect(text).toContain('project');
  });
});

describe('deleteServiceAccountRefusalText', () => {
  it('reads 404 as an already-gone account (the concurrent-deletion case)', () => {
    expect(deleteServiceAccountRefusalText(new ApiError(404, 'gone'))).toContain('no longer here');
  });

  it('never demands disclosure or reauth — delete is plain-capability', () => {
    const text = deleteServiceAccountRefusalText(new ApiError(403, 'no'));
    expect(text).not.toContain('reauth');
    expect(text).not.toContain('disclosure');
  });
});

describe('createServiceAccountFailureText', () => {
  it('stays plain on a pre-commit refusal', () => {
    // 409 is decided before any commit: nothing was created, so no "may still
    // have been created" caveat.
    expect(createServiceAccountFailureText(new ApiError(409, 'dup'))).not.toContain('may still');
  });

  it('warns of a possible commit on an ambiguous failure', () => {
    // A 500 or a lost/unparseable response may have committed a create.
    expect(createServiceAccountFailureText(new ApiError(500, 'boom'))).toContain('may still');
    expect(createServiceAccountFailureText(new Error('network'))).toContain('may still');
  });
});

describe('deleteServiceAccountFailureText', () => {
  it('stays plain when the account was already gone', () => {
    expect(deleteServiceAccountFailureText(new ApiError(404, 'gone'))).not.toContain('may still');
  });

  it('warns of a possible commit on an ambiguous failure', () => {
    expect(deleteServiceAccountFailureText(new ApiError(500, 'boom'))).toContain('may still');
  });
});
