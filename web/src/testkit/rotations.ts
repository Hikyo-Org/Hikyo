import type { zCredentialRotationList, zKeyList, zRotationRunList } from '@hikyo/zod';
import type { z } from 'zod';

import { dynamicProvider } from './machineAccess.ts';
import { PROD } from './ids.ts';

// The rotation fixtures the stories, the unit tests and the end-to-end stubs
// share. Wire shapes are `z.input` types: int64 fields travel as JSON numbers
// and `parsed()` coerces them on the way in. Every id is well formed, because
// the response schema refuses anything else.

type WireRotation = z.input<typeof zCredentialRotationList>['items'][number];
type WireRun = z.input<typeof zRotationRunList>['items'][number];
type WireKey = z.input<typeof zKeyList>['items'][number];

export const PASSWORD_KEY = 'key_123e4567-e89b-12d3-a456-426614174101';
export const USERNAME_KEY = 'key_123e4567-e89b-12d3-a456-426614174102';
export const OTHER_PASSWORD_KEY = 'key_123e4567-e89b-12d3-a456-426614174103';
export const OTHER_USERNAME_KEY = 'key_123e4567-e89b-12d3-a456-426614174104';
export const HALTED_PASSWORD_KEY = 'key_123e4567-e89b-12d3-a456-426614174105';
export const HALTED_USERNAME_KEY = 'key_123e4567-e89b-12d3-a456-426614174106';

const OPERATOR = 'prn_123e4567-e89b-12d3-a456-426614174010';
export const ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174201';
export const HELD_ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174202';
export const HALTED_ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174203';
export const DELETED_ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174204';

const catalogueKey = (id: string, name: string): WireKey => ({
  id,
  org_id: 'org_123e4567-e89b-12d3-a456-426614174001',
  project_id: 'prj_123e4567-e89b-12d3-a456-426614174000',
  name,
  folder_path: '',
  classification: 'secret',
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: {},
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '',
  created_at: '2026-09-01T00:00:00Z',
});

export const rotationKeys = {
  count: 6,
  schema_revision: 1,
  items: [
    catalogueKey(PASSWORD_KEY, 'DATABASE_PASSWORD'),
    catalogueKey(USERNAME_KEY, 'DATABASE_USER'),
    catalogueKey(OTHER_PASSWORD_KEY, 'REPORTING_PASSWORD'),
    catalogueKey(OTHER_USERNAME_KEY, 'REPORTING_USER'),
    catalogueKey(HALTED_PASSWORD_KEY, 'ANALYTICS_PASSWORD'),
    catalogueKey(HALTED_USERNAME_KEY, 'ANALYTICS_USER'),
  ],
} satisfies z.input<typeof zKeyList>;

const rotation = (
  id: string,
  environment: string,
  keys: readonly [string, string],
  overrides: Partial<WireRotation>,
): WireRotation => ({
  id,
  environment_id: environment,
  provider_id: dynamicProvider.id,
  key_id: keys[0],
  username_key_id: keys[1],
  interval_seconds: 30 * 86_400,
  revocation_delay_seconds: 86_400,
  authority_principal_id: OPERATOR,
  created_by: OPERATOR,
  state: 'active',
  restore_hold: false,
  next_run_at: '2099-01-01T00:00:00Z',
  credential_expires_at: '2099-01-31T00:00:00Z',
  generation: 1,
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  ...overrides,
});

/** An active rotation and a restore-held one; `haltedRotation` and `deletedRotation` are separate. */
export const rotationList = (environment: string = PROD) =>
  ({
    items: [
      rotation(ROTATION, environment, [PASSWORD_KEY, USERNAME_KEY], {}),
      rotation(HELD_ROTATION, environment, [OTHER_PASSWORD_KEY, OTHER_USERNAME_KEY], {
        restore_hold: true,
        interval_seconds: 7 * 86_400,
        revocation_delay_seconds: 3_600,
      }),
    ],
  }) satisfies z.input<typeof zCredentialRotationList>;

export const haltedRotation = (environment: string = PROD) =>
  rotation(HALTED_ROTATION, environment, [HALTED_PASSWORD_KEY, HALTED_USERNAME_KEY], {
    state: 'halted',
    next_run_at: null,
    interval_seconds: 3_600,
    revocation_delay_seconds: 300,
  });

export const deletedRotation = (environment: string = PROD) =>
  rotation(DELETED_ROTATION, environment, [PASSWORD_KEY, USERNAME_KEY], {
    state: 'disabled',
    next_run_at: null,
  });

const run = (n: number, overrides: Partial<WireRun>): WireRun => ({
  id: `rrn_123e4567-e89b-12d3-a456-4266141743${String(10 + n)}`,
  trigger: 'schedule',
  phase: 'done',
  published_revision: 41 - n,
  revocation_delay_seconds: 86_400,
  started_at: `2026-10-${String(20 - n).padStart(2, '0')}T02:00:00Z`,
  phase_at: `2026-10-${String(21 - n).padStart(2, '0')}T02:05:00Z`,
  ...overrides,
});

export const VERSIONS_CHANGED_RUN = 'rrn_123e4567-e89b-12d3-a456-426614174311';
export const REFUSED_RUN = 'rrn_123e4567-e89b-12d3-a456-426614174312';
export const QUARANTINED_RUN = 'rrn_123e4567-e89b-12d3-a456-426614174313';

/**
 * A timeline that owes three drops, newest first: a drop the executor refused
 * because a restore put an earlier value back (`published_versions_changed`), a
 * refused drop whose owed role may outlive its recorded expiry (a lower bound),
 * and a quarantine with no recorded expiry, then ordinary settled runs.
 */
export const owedRuns = {
  items: [
    run(1, {
      id: VERSIONS_CHANGED_RUN,
      phase: 'revoke_refused',
      error_code: 'published_versions_changed',
      credential_valid_until: '2026-10-30T00:00:00Z',
      not_converged_adapter_targets: 2,
    }),
    run(2, {
      id: REFUSED_RUN,
      phase: 'revoke_refused',
      error_code: 'role_owns_objects',
      credential_valid_until: '2026-10-29T12:30:45Z',
      credential_valid_until_lower_bound: true,
      not_converged_adapter_targets: 0,
    }),
    run(3, { id: QUARANTINED_RUN, phase: 'quarantine', trigger: 'manual', revocation_delay_seconds: 0, error_code: 'provider_unavailable', published_revision: null }),
    run(4, { not_converged_adapter_targets: 0 }),
    run(5, { phase: 'failed', error_code: 'retries_exhausted', published_revision: null }),
  ],
} satisfies z.input<typeof zRotationRunList>;

export const settledRuns = {
  items: [run(1, { not_converged_adapter_targets: 0 }), run(2, {})],
} satisfies z.input<typeof zRotationRunList>;
