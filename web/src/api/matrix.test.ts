import { beforeEach, describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  assembleKeyImpact,
  assembleMatrixEnvironmentRows,
  bindMatrixEnvironmentQueries,
  type MatrixEnvironmentQuery,
  matrixImpactReady,
  matrixPublishValidation,
  matrixMutationError,
  forgetRestorePreviews,
  parseMatrixEnvironmentSignals,
  parseMatrixPendingDrafts,
  pendingConfigPreview,
  rememberRestorePreview,
  restorePreviewFor,
  revisionAdvanced,
  signalsRequireValuesRefresh,
} from './matrix.ts';

const envDev = 'env_01989abc-def0-7123-8123-123456789abc';
const keyLog = 'key_01989abc-def0-7123-8123-123456789abc';
const keyOther = 'key_01989abc-def0-7123-8123-123456789abd';
const version = 'ver_01989abc-def0-7123-8123-123456789abc';
const ref = { org: 'org', project: 'project' };

const environmentDev = {
  id: envDev,
  org_id: 'org_01989abc-def0-7123-8123-123456789abc',
  project_id: 'prj_01989abc-def0-7123-8123-123456789abc',
  name: 'development',
  display_order: 0,
  created_at: '2026-08-22T08:00:00Z',
};
const envProd = 'env_01989abc-def0-7123-8123-123456789abd';
const environmentProd = {
  ...environmentDev,
  id: envProd,
  name: 'production',
  display_order: 1,
};

function query<T>(data: T | undefined, isPending = false, isError = false, error: unknown = undefined) {
  return { data, isPending, isError, error };
}

function environmentQuery<T>(
  environmentId: string,
  data: T | undefined,
  isPending = false,
  isError = false,
): MatrixEnvironmentQuery<T> {
  if (isPending) {
    return { environmentId, query: { status: 'pending' } };
  }
  if (isError) {
    return data === undefined
      ? { environmentId, query: { status: 'error' } }
      : { environmentId, query: { status: 'stale', data } };
  }
  return data === undefined
    ? { environmentId, query: { status: 'error' } }
    : { environmentId, query: { status: 'ready', data } };
}

beforeEach(() => {
  forgetRestorePreviews(ref, [version, 'ver_second', 'ver_other']);
});

describe('restore preview lifecycle', () => {
  it('matches exact sorted version sets without overwriting another restore', () => {
    rememberRestorePreview(ref, [version, 'ver_second'], 'token-one');
    rememberRestorePreview(ref, ['ver_other'], 'token-two');
    expect(restorePreviewFor(ref, ['ver_second', version])).toEqual({ token: 'token-one' });
    expect(restorePreviewFor(ref, ['ver_other'])).toEqual({ token: 'token-two' });
  });

  it('returns overlapping version ids for a partial selection and null for no overlap', () => {
    rememberRestorePreview(ref, [version, 'ver_second'], 'token-one');
    expect(restorePreviewFor(ref, [version, 'ver_other'])).toEqual({ conflict: [version] });
    expect(restorePreviewFor(ref, ['ver_other'])).toBeNull();
  });

  it('keeps the client-side exact-selection refusal actionable', () => {
    rememberRestorePreview(ref, [version, 'ver_second'], 'token-one');
    const preview = restorePreviewFor(ref, [version, 'ver_other']);
    expect(preview).toEqual({ conflict: [version] });
  });

  it('forgets a preview after its versions publish', () => {
    rememberRestorePreview(ref, [version], 'token-one');
    forgetRestorePreviews(ref, [version]);
    expect(restorePreviewFor(ref, [version])).toBeNull();
  });

  it('names a detail-less 409 as stale only when a preview token was attached', () => {
    const conflict = new ApiError(409, 'request failed with 409');
    expect(matrixMutationError(conflict, 'publish', true)).toBe(
      'Publish refused: the restore preview is stale or missing — stage the restore again from the history drawer.',
    );
    expect(matrixMutationError(conflict, 'publish', false)).toBe(
      'Publish was refused. Fix the named matrix problems, then retry.',
    );
  });
});

describe('matrix signal boundary', () => {
  it('refuses a pending version without its operation', () => {
    expect(() =>
      parseMatrixEnvironmentSignals({
        environment_id: envDev,
        revision: 2,
        cells: [
          {
            key_id: keyLog,
            name: 'LOG_LEVEL',
            classification: 'config',
            pending_version_id: version,
            pending_by_others: false,
          },
        ],
      }),
    ).toThrow(/pending_version_id and pending_operation/);
  });

  it('accepts complete pending and absent-pending cells', () => {
    expect(
      parseMatrixEnvironmentSignals({
        environment_id: envDev,
        revision: 2,
        cells: [
          {
            key_id: keyLog,
            name: 'LOG_LEVEL',
            classification: 'config',
            pending_version_id: version,
            pending_operation: 'set',
            pending_by_others: false,
          },
          {
            key_id: keyOther,
            name: 'OTHER',
            classification: 'config',
            pending_by_others: false,
          },
        ],
      }).cells,
    ).toEqual([
      {
        key_id: keyLog,
        name: 'LOG_LEVEL',
        classification: 'config',
        pending: { versionId: version, operation: 'set' },
        pending_by_others: false,
      },
      {
        key_id: keyOther,
        name: 'OTHER',
        classification: 'config',
        pending_by_others: false,
      },
    ]);
  });
});

describe('matrix cache coherence', () => {
  it('establishes initial ordering, then refreshes only when the signal advances', () => {
    expect(revisionAdvanced(undefined, 2n)).toBe(false);
    expect(revisionAdvanced(2n, 2n)).toBe(false);
    expect(revisionAdvanced(2n, 3n)).toBe(true);
    expect(signalsRequireValuesRefresh(undefined, 2n)).toBe(true);
    expect(signalsRequireValuesRefresh(2n, 2n)).toBe(false);
    expect(signalsRequireValuesRefresh(2n, 3n)).toBe(true);
  });
});

describe('environment-keyed matrix rows', () => {
  const configClassification: 'config' = 'config';
  const valueCell = {
    key_id: keyLog,
    name: 'LOG_LEVEL',
    classification: configClassification,
    set: true,
    revealed: true,
  };
  const devValues = {
    items: [{ ...valueCell, value: 'debug' }],
    count: 1,
  };
  const prodValues = {
    items: [{ ...valueCell, value: 'warn' }],
    count: 1,
  };
  const devSignals = { environment_id: envDev, revision: 2n, cells: [] };
  const prodSignals = { environment_id: envProd, revision: 7n, cells: [] };
  const devSettings = { protected: false, reauth_window_seconds: null };
  const prodSettings = { protected: true, reauth_window_seconds: 300 };
  const drafts = { items: [], count: 0 };

  const inputs = {
    values: [environmentQuery(envDev, devValues), environmentQuery(envProd, prodValues)],
    signals: [environmentQuery(envProd, prodSignals), environmentQuery(envDev, devSignals)],
    settings: [environmentQuery(envDev, devSettings), environmentQuery(envProd, prodSettings)],
    pendingDrafts: [environmentQuery(envProd, drafts), environmentQuery(envDev, drafts)],
  };

  it('keeps query state attached by environment id while display order changes', () => {
    const rows = assembleMatrixEnvironmentRows(
      [environmentProd, environmentDev],
      inputs,
    );

    expect(rows.map((row) => row.environmentId)).toEqual([envProd, envDev]);
    expect(rows.map((row) => row.values.data?.items[0]?.value)).toEqual(['warn', 'debug']);
    expect(rows.map((row) => row.signals.data?.revision)).toEqual([7n, 2n]);
    expect(rows.map((row) => row.settings.data?.protected)).toEqual([true, false]);
  });

  it('removes a row without shifting another environment query into its place', () => {
    const rows = assembleMatrixEnvironmentRows([environmentProd], inputs);

    expect(rows).toHaveLength(1);
    expect(rows[0]?.environmentId).toBe(envProd);
    expect(rows[0]?.values.data?.items[0]?.value).toBe('warn');
  });

  it('keeps one pending query on its own row while other rows render ready data', () => {
    const rows = assembleMatrixEnvironmentRows([environmentDev, environmentProd], {
      ...inputs,
      signals: [
        environmentQuery<typeof prodSignals>(envProd, undefined, true),
        environmentQuery(envDev, devSignals),
      ],
    });

    expect(rows[0]?.signals.data?.revision).toBe(2n);
    expect(rows[0]?.signals.status).toBe('ready');
    expect(rows[0]?.readiness).toBe('ready');
    expect(rows[1]?.environmentId).toBe(envProd);
    expect(rows[1]?.signals.data).toBeUndefined();
    expect(rows[1]?.signals.status).toBe('pending');
    expect(rows[1]?.readiness).toBe('pending');
  });

  it('maps query flags once into pending, error, stale, and ready states', () => {
    const [pending, error, stale, ready] = bindMatrixEnvironmentQueries(
      'values',
      [
        environmentDev,
        environmentProd,
        { ...environmentDev, id: 'env_stale' },
        { ...environmentDev, id: 'env_ready' },
      ],
      [
        query(undefined, true),
        query(undefined, false, true),
        query({ environmentId: 'env_stale', value: devValues }, false, true),
        query({ environmentId: 'env_ready', value: prodValues }),
      ],
    ).map((entry) => entry.query);

    expect(pending).toEqual({ status: 'pending' });
    expect(error).toEqual({ status: 'error' });
    expect(stale).toEqual({ status: 'stale', data: devValues });
    expect(ready).toEqual({ status: 'ready', data: prodValues });
  });

  it('maps a 403 to forbidden, a non-403 to error, and forbids even a cached column', () => {
    const [forbidden, forbiddenCached, otherError] = bindMatrixEnvironmentQueries(
      'values',
      [environmentDev, { ...environmentDev, id: 'env_cached' }, { ...environmentDev, id: 'env_500' }],
      [
        query(undefined, false, true, new ApiError(403, 'forbidden')),
        // A revoked column blanks (fail-closed): a cached copy does not soften a
        // 403 into 'stale'.
        query({ environmentId: 'env_cached', value: devValues }, false, true, new ApiError(403, 'forbidden')),
        query(undefined, false, true, new ApiError(500, 'boom')),
      ],
    ).map((entry) => entry.query);

    expect(forbidden).toEqual({ status: 'forbidden' });
    expect(forbiddenCached).toEqual({ status: 'forbidden' });
    expect(otherError).toEqual({ status: 'error' });
  });

  it('ranks a mixed row as forbidden when one column is denied and another errors', () => {
    const [row] = assembleMatrixEnvironmentRows([environmentDev], {
      values: [{ environmentId: envDev, query: { status: 'forbidden' } }],
      signals: [{ environmentId: envDev, query: { status: 'error' } }],
      settings: [environmentQuery(envDev, devSettings)],
      pendingDrafts: [environmentQuery(envDev, drafts)],
    });
    expect(row?.readiness).toBe('forbidden');

    const [loadingRow] = assembleMatrixEnvironmentRows([environmentDev], {
      values: [{ environmentId: envDev, query: { status: 'forbidden' } }],
      signals: [environmentQuery<typeof devSignals>(envDev, undefined, true)],
      settings: [environmentQuery(envDev, devSettings)],
      pendingDrafts: [environmentQuery(envDev, drafts)],
    });
    // Pending still outranks forbidden — the column may yet resolve.
    expect(loadingRow?.readiness).toBe('pending');
  });

  it('derives one row readiness with loading precedence and includes stale pending drafts', () => {
    const [row] = assembleMatrixEnvironmentRows([environmentDev], {
      values: [environmentQuery<typeof devValues>(envDev, undefined, true)],
      signals: [{ environmentId: envDev, query: { status: 'error' } }],
      settings: [{ environmentId: envDev, query: { status: 'stale', data: devSettings } }],
      pendingDrafts: [{ environmentId: envDev, query: { status: 'stale', data: drafts } }],
    });

    expect(row?.readiness).toBe('pending');

    const [errorRow] = assembleMatrixEnvironmentRows([environmentDev], {
      values: [environmentQuery(envDev, devValues)],
      signals: [{ environmentId: envDev, query: { status: 'error' } }],
      settings: [environmentQuery(envDev, devSettings)],
      pendingDrafts: [environmentQuery(envDev, drafts)],
    });
    expect(errorRow?.readiness).toBe('error');

    const [staleRow] = assembleMatrixEnvironmentRows([environmentDev], {
      values: [environmentQuery(envDev, devValues)],
      signals: [environmentQuery(envDev, devSignals)],
      settings: [environmentQuery(envDev, devSettings)],
      pendingDrafts: [{ environmentId: envDev, query: { status: 'stale', data: drafts } }],
    });
    expect(staleRow?.readiness).toBe('stale');
  });

  it('rejects duplicate or missing query identities instead of guessing by position', () => {
    expect(() =>
      assembleMatrixEnvironmentRows([environmentDev, environmentProd], {
        ...inputs,
        values: [environmentQuery(envDev, devValues), environmentQuery(envDev, prodValues)],
      }),
    ).toThrow(`matrix values queries contain duplicate environment ${envDev}`);

    expect(() =>
      assembleMatrixEnvironmentRows([environmentDev, environmentProd], {
        ...inputs,
        values: [environmentQuery(envDev, devValues)],
      }),
    ).toThrow(`matrix values query is missing environment ${envProd}`);
  });

  it('refuses to assign returned data to a different positional environment', () => {
    expect(() =>
      bindMatrixEnvironmentQueries('values', [environmentProd, environmentDev], [
        query({ environmentId: envDev, value: devValues }),
        query({ environmentId: envProd, value: prodValues }),
      ]),
    ).toThrow(`matrix values query for ${envProd} returned data for ${envDev}`);
  });
});

describe('publish validation mapping', () => {
  it('maps a safe named cell refusal to exactly one matrix cell', () => {
    const error = new ApiError(
      400,
      'request failed with 400',
      'key "LOG_LEVEL" is `required_in` environment env_prod and resolves to absent: publish is vetoed',
    );

    expect(
      matrixPublishValidation(error, [{ id: 'key_log', name: 'LOG_LEVEL' }], ['env_dev', 'env_prod']),
    ).toEqual({
      keyId: 'key_log',
      environmentId: 'env_prod',
      message: error.detail,
    });
  });

  it('keeps authorization, conflict, network, and unparsed refusals mutation-level', () => {
    const keys = [{ id: 'key_log', name: 'LOG_LEVEL' }];
    expect(matrixPublishValidation(new ApiError(403, 'forbidden'), keys, ['env_prod'])).toBeNull();
    expect(matrixPublishValidation(new ApiError(409, 'stale'), keys, ['env_prod'])).toBeNull();
    expect(matrixPublishValidation(new Error('offline'), keys, ['env_prod'])).toBeNull();
    expect(matrixPublishValidation(new ApiError(400, 'bad request'), keys, ['env_prod'])).toBeNull();
  });
});

describe('pending draft preview boundary', () => {
  const draft = {
    version_id: version,
    key_id: keyLog,
    name: 'LOG_LEVEL',
    classification: 'config',
    operation: 'set',
    staged_from_revision: 1,
    created_at: '2026-08-15T10:00:00Z',
  };

  it('accepts a revealed config preview and binds it to the signal by version id', () => {
    const drafts = parseMatrixPendingDrafts({
      items: [{ ...draft, revealed: true, value: 'debug' }],
      count: 1,
    });
    const byVersion = new Map(drafts.items.map((item) => [item.version_id, item]));
    const signal: Parameters<typeof pendingConfigPreview>[0] = {
      key_id: keyLog,
      name: 'LOG_LEVEL',
      classification: 'config',
      pending_by_others: false,
      pending: { versionId: version, operation: 'set' },
    };
    expect(pendingConfigPreview(signal, byVersion)).toBe('debug');
    expect(
      pendingConfigPreview(
        { ...signal, pending: { versionId: 'ver_other', operation: 'set' } },
        byVersion,
      ),
    ).toBeUndefined();
    expect(() => pendingConfigPreview({ ...signal, key_id: keyOther }, byVersion)).toThrow(
      'bound to the wrong key',
    );
  });

  it('accepts a hidden config set whose material originated as secret', () => {
    expect(
      parseMatrixPendingDrafts({ items: [{ ...draft, revealed: false }], count: 1 }).items[0],
    ).toMatchObject({ classification: 'config', operation: 'set', revealed: false });
  });

  it('rejects a revealed draft without a value', () => {
    expect(() =>
      parseMatrixPendingDrafts({ items: [{ ...draft, revealed: true }], count: 1 }),
    ).toThrow('pending draft value must appear if and only if revealed is true');
  });

  it('rejects secret material on the preview seam', () => {
    expect(() =>
      parseMatrixPendingDrafts({
        items: [{ ...draft, classification: 'secret', revealed: true, value: 'secret' }],
        count: 1,
      }),
    ).toThrow('secret pending drafts must remain unrevealed');
  });
});

describe('key lifecycle impact boundary', () => {
  it('reduces per-environment occupancy to value-free id lists', () => {
    expect(
      assembleKeyImpact([
        { environmentId: 'env_a', set: true, pending: false },
        { environmentId: 'env_b', set: false, pending: true },
        { environmentId: 'env_c', set: false, pending: false },
      ]),
    ).toEqual({ setEnvironmentIds: ['env_a'], pendingEnvironmentIds: ['env_b'] });
  });

  it('is ready only when every row is fully ready, and fails closed otherwise', () => {
    const ready = { values: { status: 'ready' as const }, signals: { status: 'ready' as const } };
    // The empty project (env list loaded, no rows) is legitimately ready.
    expect(matrixImpactReady(true, [])).toBe(true);
    expect(matrixImpactReady(false, [])).toBe(false);
    expect(matrixImpactReady(true, [ready, ready])).toBe(true);
    // A row whose values errored or went stale must NOT count as ready — that is
    // the fail-closed property that keeps a preview from understating its reach.
    expect(
      matrixImpactReady(true, [ready, { values: { status: 'error' }, signals: { status: 'ready' } }]),
    ).toBe(false);
    expect(
      matrixImpactReady(true, [{ values: { status: 'ready' }, signals: { status: 'stale' } }]),
    ).toBe(false);
    expect(
      matrixImpactReady(true, [{ values: { status: 'pending' }, signals: { status: 'ready' } }]),
    ).toBe(false);
  });
});
