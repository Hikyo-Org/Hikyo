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
export const GONE_PASSWORD_KEY = 'key_123e4567-e89b-12d3-a456-426614174107';
export const GONE_USERNAME_KEY = 'key_123e4567-e89b-12d3-a456-426614174108';

export const LONG_PASSWORD_KEY = 'key_123e4567-e89b-12d3-a456-426614174109';
export const LONG_USERNAME_KEY = 'key_123e4567-e89b-12d3-a456-426614174110';
export const LONG_PASSWORD_NAME = 'ANALYTICS_REPORTING_WAREHOUSE_DB_PASSWORD';

const OPERATOR = 'prn_123e4567-e89b-12d3-a456-426614174010';
export const ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174201';
export const HELD_ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174202';
export const HALTED_ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174203';
export const DELETED_ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174204';
export const GONE_ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174205';
export const LONG_ROTATION = 'rot_123e4567-e89b-12d3-a456-426614174206';

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
  count: 10,
  schema_revision: 1,
  items: [
    catalogueKey(PASSWORD_KEY, 'DATABASE_PASSWORD'),
    catalogueKey(USERNAME_KEY, 'DATABASE_USER'),
    catalogueKey(OTHER_PASSWORD_KEY, 'REPORTING_PASSWORD'),
    catalogueKey(OTHER_USERNAME_KEY, 'REPORTING_USER'),
    catalogueKey(HALTED_PASSWORD_KEY, 'ANALYTICS_PASSWORD'),
    catalogueKey(HALTED_USERNAME_KEY, 'ANALYTICS_USER'),
    catalogueKey(GONE_PASSWORD_KEY, 'BILLING_PASSWORD'),
    catalogueKey(GONE_USERNAME_KEY, 'BILLING_USER'),
    catalogueKey(LONG_PASSWORD_KEY, LONG_PASSWORD_NAME),
    catalogueKey(LONG_USERNAME_KEY, 'ANALYTICS_REPORTING_WAREHOUSE_DB_USER'),
  ],
} satisfies z.input<typeof zKeyList>;

export const VERSIONS_CHANGED_RUN = 'rrn_123e4567-e89b-12d3-a456-426614174311';
export const REFUSED_RUN = 'rrn_123e4567-e89b-12d3-a456-426614174312';
export const QUARANTINED_RUN = 'rrn_123e4567-e89b-12d3-a456-426614174313';
const SETTLED_RUN = 'rrn_123e4567-e89b-12d3-a456-426614174314';
const FAILED_RUN = 'rrn_123e4567-e89b-12d3-a456-426614174315';

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
  owed_drop_runs: 0,
  owed_drop_runs_versions_changed: 0,
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
      // The same three owed drops `owedRuns` lists, one of them refused after a restore.
      rotation(ROTATION, environment, [PASSWORD_KEY, USERNAME_KEY], {
        owed_drop_runs: 3,
        owed_drop_runs_versions_changed: 1,
        last_run: {
          id: VERSIONS_CHANGED_RUN,
          phase: 'revoke_refused',
          error_code: 'published_versions_changed',
          phase_at: '2026-10-20T02:05:00Z',
        },
      }),
      rotation(HELD_ROTATION, environment, [OTHER_PASSWORD_KEY, OTHER_USERNAME_KEY], {
        last_run: { id: SETTLED_RUN, phase: 'done', phase_at: '2026-10-20T02:05:00Z' },
        restore_hold: true,
        interval_seconds: 7 * 86_400,
        revocation_delay_seconds: 3_600,
      }),
    ],
  }) satisfies z.input<typeof zCredentialRotationList>;

export const haltedRotation = (environment: string = PROD) =>
  rotation(HALTED_ROTATION, environment, [HALTED_PASSWORD_KEY, HALTED_USERNAME_KEY], {
    state: 'halted',
    halt_reason: 'retries_exhausted',
    last_run: { id: FAILED_RUN, phase: 'failed', error_code: 'retries_exhausted', phase_at: '2026-10-20T02:05:00Z' },
    next_run_at: null,
    interval_seconds: 3_600,
    revocation_delay_seconds: 300,
  });

export const deletedRotation = (environment: string = PROD) =>
  rotation(DELETED_ROTATION, environment, [PASSWORD_KEY, USERNAME_KEY], {
    state: 'disabled',
    next_run_at: null,
  });

/** A deleted rotation whose delete left a quarantined run: the default list still shows it. */
export const deletedOwedRotation = (environment: string = PROD) =>
  rotation(GONE_ROTATION, environment, [GONE_PASSWORD_KEY, GONE_USERNAME_KEY], {
    state: 'disabled',
    next_run_at: null,
    owed_drop_runs: 1,
    last_run: { id: QUARANTINED_RUN, phase: 'quarantine', error_code: 'abandoned', phase_at: '2026-10-20T02:05:00Z' },
  });

/** A rotation whose keys have names about 45 characters long: they must wrap, not overflow. */
export const longKeyNameRotation = (environment: string = PROD) =>
  rotation(LONG_ROTATION, environment, [LONG_PASSWORD_KEY, LONG_USERNAME_KEY], {
    last_run: { id: SETTLED_RUN, phase: 'done', phase_at: '2026-10-20T02:05:00Z' },
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
