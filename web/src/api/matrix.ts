import {
  clearValueOp,
  copyValuesOp,
  createKeyOp,
  getEnvironmentSignalsOp,
  getKeyOp,
  importValuesOp,
  listKeyGroupsOp,
  listKeysOp,
  listPendingDraftsOp,
  listValueOccurrencesOp,
  listValuesOp,
  publishPendingChangesOp,
  reclassifyKeyOp,
  deleteKeyOp,
  renameKeyOp,
  setValueOp,
  updateKeyMetadataOp,
} from '@hikyo/operations';
import type { ImportPrecondition, KeyClassification, KeyPresence, KeyRule } from '@hikyo/client';
import {
  zEnvironmentList,
  zEnvironmentSettings,
  zEnvironmentSignals,
  zImportValuesResult,
  zKeyList,
  zPendingDraftList,
  zPublishResult,
  zScanFinding,
  zValueList,
  zValueOccurrenceList,
} from '@hikyo/zod';
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';
import { z } from 'zod';

import {
  signalsPollInterval,
  useAdvisoryStream,
  type AdvisoryEvent,
} from './advisory.ts';
import { ApiError, ok, parsed } from './client.ts';
import { callerSafeRefusal } from './history.ts';
import {
  invalidateAfterCopy,
  matrixGroupsKey,
  matrixKeyKey,
  matrixKeysKey,
  pendingDraftsKey,
  pendingMatrixKey,
  pinsKey,
  revisionsKey,
  signalsKey,
  signalsMatrixKey,
  valuesKey,
  valuesMatrixKey,
  windowKey,
  type MatrixRef,
} from './keys.ts';
import { environmentSettingsQueryOptions, useEnvironments } from './settings.ts';
import { useTransport } from './transport.tsx';

export { useProjects } from './settings.ts';
export type { MatrixRef } from './keys.ts';

/**
 * Whole-project matrix API boundary.
 *
 * The matrix reads one project catalogue and then fans out over the project's
 * environments. Every body crosses the generated Zod schema at this boundary;
 * the component receives only parsed domain records. Live updates ride the
 * advisory SSE stream (#510): one subscription per open project invalidates
 * the caches an event names, and the signals poll survives only as the
 * documented fallback while that stream is not healthy.
 */

export type MatrixKeyList = z.infer<typeof zKeyList>;
/**
 * A redacted secret-scanning finding (#74, secret-scanning ADR §4). It rides
 * a config value-write response and carries a rule id, an immutable locator,
 * and — for a keep-as-config dismissal — an opaque acknowledgement token. It
 * never carries the matched text, so the UI renders only what it holds.
 */
export type ScanFinding = z.infer<typeof zScanFinding>;
type MatrixEnvironmentSignalsWire = z.infer<typeof zEnvironmentSignals>;
type MatrixSignalCellWire = MatrixEnvironmentSignalsWire['cells'][number];
export type MatrixSignalCell = Omit<
  MatrixSignalCellWire,
  'pending_version_id' | 'pending_operation'
> & {
  readonly pending?: {
    readonly versionId: string;
    readonly operation: NonNullable<MatrixSignalCellWire['pending_operation']>;
  };
};
export type MatrixEnvironmentSignals = Omit<MatrixEnvironmentSignalsWire, 'cells'> & {
  readonly cells: readonly MatrixSignalCell[];
};

type MatrixEnvironment = z.infer<typeof zEnvironmentList>['items'][number];
type MatrixEnvironmentSettings = z.infer<typeof zEnvironmentSettings>;
type MatrixValueList = z.infer<typeof zValueList>;
type MatrixPendingDraftList = z.infer<typeof zPendingDraftList>;

export type MatrixQueryStatus = 'pending' | 'forbidden' | 'error' | 'stale' | 'ready';

export type MatrixQueryState<T> =
  | { readonly status: 'pending'; readonly data?: undefined }
  // A per-environment 403: the caller may not read this column. Distinct from
  // 'error' because a denial never heals on retry, so the surface must say so
  // rather than offering a reload. Fail-closed — it carries no data even when a
  // stale copy is cached, because a revoked column must blank, not linger.
  | { readonly status: 'forbidden'; readonly data?: undefined }
  | { readonly status: 'error'; readonly data?: undefined }
  | { readonly status: 'stale'; readonly data: T }
  | { readonly status: 'ready'; readonly data: T };

type MatrixQueryResult<T> = {
  readonly data: T | undefined;
  readonly isPending: boolean;
  readonly isError: boolean;
  readonly error: unknown;
};

export type MatrixEnvironmentQuery<T> = {
  readonly environmentId: string;
  readonly query: MatrixQueryState<T>;
};

type EnvironmentQueryData<T> = {
  readonly environmentId: string;
  readonly value: T;
};

/** One ordered display row with every per-environment query attached by identity. */
export type MatrixEnvironmentRow = {
  readonly environmentId: string;
  readonly environment: MatrixEnvironment;
  readonly readiness: MatrixQueryStatus;
  readonly values: MatrixQueryState<MatrixValueList>;
  readonly signals: MatrixQueryState<MatrixEnvironmentSignals>;
  readonly settings: MatrixQueryState<MatrixEnvironmentSettings>;
  readonly pendingDrafts: MatrixQueryState<MatrixPendingDraftList>;
};

type MatrixEnvironmentQueries = {
  readonly values: readonly MatrixEnvironmentQuery<MatrixValueList>[];
  readonly signals: readonly MatrixEnvironmentQuery<MatrixEnvironmentSignals>[];
  readonly settings: readonly MatrixEnvironmentQuery<MatrixEnvironmentSettings>[];
  readonly pendingDrafts: readonly MatrixEnvironmentQuery<MatrixPendingDraftList>[];
};

function matrixQueryIndex<T>(
  label: string,
  entries: readonly MatrixEnvironmentQuery<T>[],
): ReadonlyMap<string, MatrixQueryState<T>> {
  const queries = new Map<string, MatrixQueryState<T>>();
  for (const entry of entries) {
    if (queries.has(entry.environmentId)) {
      throw new Error(
        `matrix ${label} queries contain duplicate environment ${entry.environmentId}`,
      );
    }
    queries.set(entry.environmentId, entry.query);
  }
  return queries;
}

function requiredMatrixQuery<T>(
  label: string,
  environmentId: string,
  queries: ReadonlyMap<string, MatrixQueryState<T>>,
): MatrixQueryState<T> {
  const query = queries.get(environmentId);
  if (query === undefined) {
    throw new Error(`matrix ${label} query is missing environment ${environmentId}`);
  }
  return query;
}

/**
 * Join independently updating query families by environment ID while keeping
 * the server's environment order as the explicit display order.
 */
export function assembleMatrixEnvironmentRows(
  environments: readonly MatrixEnvironment[],
  input: MatrixEnvironmentQueries,
): readonly MatrixEnvironmentRow[] {
  const values = matrixQueryIndex('values', input.values);
  const signals = matrixQueryIndex('signals', input.signals);
  const settings = matrixQueryIndex('settings', input.settings);
  const pendingDrafts = matrixQueryIndex('pending drafts', input.pendingDrafts);
  return environments.map((environment) => {
    const rowValues = requiredMatrixQuery('values', environment.id, values);
    const rowSignals = requiredMatrixQuery('signals', environment.id, signals);
    const rowSettings = requiredMatrixQuery('settings', environment.id, settings);
    const rowPendingDrafts = requiredMatrixQuery(
      'pending drafts',
      environment.id,
      pendingDrafts,
    );
    return {
      environmentId: environment.id,
      environment,
      readiness: matrixRowReadiness([
        rowValues,
        rowSignals,
        rowSettings,
        rowPendingDrafts,
      ]),
      values: rowValues,
      signals: rowSignals,
      settings: rowSettings,
      pendingDrafts: rowPendingDrafts,
    };
  });
}

function matrixRowReadiness(
  states: readonly { readonly status: MatrixQueryStatus }[],
): MatrixQueryStatus {
  if (states.some((state) => state.status === 'pending')) {
    return 'pending';
  }
  // Forbidden outranks error: both degrade the row, but a denial is the more
  // specific fact and carries its own message, so a row that is part-forbidden,
  // part-error reads as forbidden.
  if (states.some((state) => state.status === 'forbidden')) {
    return 'forbidden';
  }
  if (states.some((state) => state.status === 'error')) {
    return 'error';
  }
  if (states.some((state) => state.status === 'stale')) {
    return 'stale';
  }
  return 'ready';
}

function matrixQueryState<T>(query: MatrixQueryResult<T>): MatrixQueryState<T> {
  if (query.isPending) {
    return { status: 'pending' };
  }
  if (query.isError) {
    // A 403 is a permission denial, not a transient failure: retrying never
    // resolves it, so it maps to 'forbidden' even when a stale copy is cached
    // (fail-closed — a revoked column blanks rather than lingering as 'stale').
    if (query.error instanceof ApiError && query.error.status === 403) {
      return { status: 'forbidden' };
    }
    return query.data === undefined
      ? { status: 'error' }
      : { status: 'stale', data: query.data };
  }
  return query.data === undefined
    ? { status: 'error' }
    : { status: 'ready', data: query.data };
}

export function bindMatrixEnvironmentQueries<T>(
  label: string,
  environments: readonly MatrixEnvironment[],
  queries: readonly MatrixQueryResult<EnvironmentQueryData<T>>[],
): readonly MatrixEnvironmentQuery<T>[] {
  if (environments.length !== queries.length) {
    throw new Error(
      `matrix ${label} query count ${String(queries.length)} does not match environment count ${String(environments.length)}`,
    );
  }
  return environments.map((environment, index) => {
    const query = queries[index];
    if (query === undefined) {
      throw new Error(`matrix ${label} query is missing position ${String(index)}`);
    }
    if (query.data !== undefined && query.data.environmentId !== environment.id) {
      throw new Error(
        `matrix ${label} query for ${environment.id} returned data for ${query.data.environmentId}`,
      );
    }
    return {
      environmentId: environment.id,
      query: matrixQueryState({
        data: query.data?.value,
        isPending: query.isPending,
        isError: query.isError,
        error: query.error,
      }),
    };
  });
}

type RestorePreview = { readonly versionIds: readonly string[]; readonly token: string };

/**
 * Restore previews intentionally live only for this page load, but outside any
 * route component so matrix/history SPA navigation cannot drop them. A browser
 * reload clears the store and requires the restore to be staged again.
 */
const restorePreviews = new Map<string, readonly RestorePreview[]>();
const previewAttachedErrors = new WeakSet<Error>();

class RestorePreviewSelectionError extends Error {}

const restorePreviewKey = (ref: MatrixRef): string => `${ref.org}/${ref.project}`;
const sortedVersionIds = (versionIds: readonly string[]): readonly string[] =>
  [...new Set(versionIds)].sort((left, right) => left.localeCompare(right));

function sameVersionSet(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((versionId, index) => versionId === right[index]);
}

export function rememberRestorePreview(
  ref: MatrixRef,
  versionIds: readonly string[],
  token: string,
): void {
  const normalized = sortedVersionIds(versionIds);
  if (normalized.length === 0) {
    throw new Error('Cannot remember a restore preview without version ids.');
  }
  const key = restorePreviewKey(ref);
  const existing = restorePreviews.get(key) ?? [];
  restorePreviews.set(key, [
    ...existing.filter((entry) => !sameVersionSet(entry.versionIds, normalized)),
    { versionIds: normalized, token },
  ]);
}

export function restorePreviewFor(
  ref: MatrixRef,
  selectedVersionIds: readonly string[],
): { readonly token: string } | { readonly conflict: readonly string[] } | null {
  const selected = sortedVersionIds(selectedVersionIds);
  const remembered = restorePreviews.get(restorePreviewKey(ref)) ?? [];
  const exact = remembered.find((entry) => sameVersionSet(entry.versionIds, selected));
  if (exact !== undefined) {
    return { token: exact.token };
  }
  const selectedSet = new Set(selected);
  const overlaps = sortedVersionIds(
    remembered.flatMap((entry) => entry.versionIds.filter((versionId) => selectedSet.has(versionId))),
  );
  return overlaps.length === 0 ? null : { conflict: overlaps };
}

export function forgetRestorePreviews(ref: MatrixRef, versionIds: readonly string[]): void {
  const key = restorePreviewKey(ref);
  const forgotten = new Set(versionIds);
  const remaining = (restorePreviews.get(key) ?? []).filter(
    (entry) => !entry.versionIds.some((versionId) => forgotten.has(versionId)),
  );
  if (remaining.length === 0) {
    restorePreviews.delete(key);
  } else {
    restorePreviews.set(key, remaining);
  }
}

export function restorePreviewWasAttached(error: Error): boolean {
  return previewAttachedErrors.has(error);
}

const zMatrixEnvironmentSignals = zEnvironmentSignals
  .superRefine((signals, context) => {
    signals.cells.forEach((cell, index) => {
      if ((cell.pending_version_id === undefined) !== (cell.pending_operation === undefined)) {
        context.addIssue({
          code: 'custom',
          message: 'pending_version_id and pending_operation must be present together',
          path: ['cells', index],
        });
      }
    });
  })
  .transform((signals) => ({
    ...signals,
    cells: signals.cells.map(({ pending_version_id, pending_operation, ...cell }) => ({
      ...cell,
      ...(pending_version_id === undefined || pending_operation === undefined
        ? {}
        : { pending: { versionId: pending_version_id, operation: pending_operation } }),
    })),
  }));

export function parseMatrixEnvironmentSignals(input: unknown): MatrixEnvironmentSignals {
  return zMatrixEnvironmentSignals.parse(input);
}

export function revisionAdvanced(previous: bigint | undefined, next: bigint): boolean {
  return previous !== undefined && next > previous;
}

export function signalsRequireValuesRefresh(
  previous: bigint | undefined,
  next: bigint,
): boolean {
  // Values carry no revision. The first signal snapshot therefore establishes
  // ordering by refreshing once; later snapshots refresh only on advancement.
  return previous === undefined || revisionAdvanced(previous, next);
}

/**
 * advisoryInvalidations maps one advisory event (#510) onto exactly the cache
 * prefixes it names — the event payload is metadata-only, so the mapping is
 * this mechanical and the pure function is the whole of it:
 *
 *  - `revision.published`: the environment advanced. Its values moved, its
 *    published drafts stopped being pending, and its signals carry the new
 *    revision and cleared markers. The signals queryFn keeps the ordering
 *    cascade (`signalsRequireValuesRefresh`), so the values invalidation here
 *    is direct rather than deferred.
 *  - `cell.changed`: the named cell moved; the signals refetch decides from
 *    the revision whether values must follow.
 *  - `pending.staged`: a draft appeared — the recipient's own (the projection
 *    blanks other actors) or another principal's write-presence marker. Both
 *    the draft list and the pending cells render that fact.
 */
export function advisoryInvalidations(
  ref: MatrixRef,
  event: AdvisoryEvent,
): readonly (readonly string[])[] {
  const environmentId = event.environmentId;
  switch (event.type) {
    case 'revision.published':
      return [
        valuesKey({ ...ref, environment: environmentId }),
        signalsKey(ref, environmentId),
        pendingDraftsKey(ref, environmentId),
      ];
    case 'cell.changed':
      return [signalsKey(ref, environmentId)];
    case 'pending.staged':
      return [
        pendingDraftsKey(ref, environmentId),
        signalsKey(ref, environmentId),
      ];
  }
}

/**
 * The caller's own drafts, as the publish sheet previews them.
 *
 * Previews come from the server (`listPendingDrafts`), bound to the immutable
 * pending version id, so they survive a reload and a second browser alike and
 * are never cached in client storage. The refinement pins the contract the
 * endpoint promises: `value` iff `revealed`, and secret or unset drafts never
 * carry material on this surface.
 */
export const zMatrixPendingDraftList = zPendingDraftList.superRefine((drafts, context) => {
  drafts.items.forEach((draft, index) => {
    const hasValue = draft.value !== undefined;
    if (draft.revealed !== hasValue) {
      context.addIssue({
        code: 'custom',
        path: ['items', index, 'value'],
        message: 'pending draft value must appear if and only if revealed is true',
      });
    }
    if (draft.classification === 'secret' && draft.revealed) {
      context.addIssue({
        code: 'custom',
        path: ['items', index, 'revealed'],
        message: 'secret pending drafts must remain unrevealed',
      });
    }
    if (draft.operation === 'unset' && draft.revealed) {
      context.addIssue({
        code: 'custom',
        path: ['items', index, 'revealed'],
        message: 'unset pending drafts must remain unrevealed',
      });
    }
  });
});

export type MatrixPendingDraft = z.infer<typeof zMatrixPendingDraftList>['items'][number];

export function parseMatrixPendingDrafts(input: unknown): z.infer<typeof zMatrixPendingDraftList> {
  return zMatrixPendingDraftList.parse(input);
}

/** The config material a signal's own pending set previews, if the server revealed it. */
export function pendingConfigPreview(
  signal: MatrixSignalCell | undefined,
  draftsByVersion: ReadonlyMap<string, MatrixPendingDraft>,
): string | undefined {
  if (signal?.pending === undefined) {
    return undefined;
  }
  const draft = draftsByVersion.get(signal.pending.versionId);
  if (draft === undefined) {
    return undefined;
  }
  if (draft.key_id !== signal.key_id) {
    throw new Error(`pending draft ${draft.version_id} is bound to the wrong key`);
  }
  if (draft.classification !== 'config' || draft.operation !== 'set' || !draft.revealed) {
    return undefined;
  }
  return draft.value;
}

export function matrixPublishValidation(
  error: Error,
  keys: readonly { readonly id: string; readonly name: string }[],
  environmentIds: readonly string[],
): { readonly keyId: string; readonly environmentId: string; readonly message: string } | null {
  if (!(error instanceof ApiError) || error.status !== 400 || error.detail === undefined) {
    return null;
  }
  const match = /^key "([^"]+)" is `(?:required_in|forbidden_in)` environment ([^ ]+) /.exec(
    error.detail,
  );
  if (match === null) {
    return null;
  }
  const [, keyName, environmentId] = match;
  const key = keys.find((candidate) => candidate.name === keyName);
  if (key === undefined || environmentId === undefined || !environmentIds.includes(environmentId)) {
    return null;
  }
  return { keyId: key.id, environmentId, message: error.detail };
}

export function useMatrixProject(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  const environments = useEnvironments(ref.org, ref.project);
  // The live channel (#510): one advisory stream for the whole project. An
  // event invalidates exactly the caches its payload names; the connection
  // state gates the signals fallback poll below. Subscribing here — inside
  // the same hook that owns the matrix's queries — is what makes the
  // subscription die with the route and never outlive its ref.
  const signalsStream = useAdvisoryStream(
    ref,
    transport,
    (event) => {
      for (const queryKey of advisoryInvalidations(ref, event)) {
        void queries.invalidateQueries({ queryKey });
      }
    },
    ref.org !== '' && ref.project !== '',
  );
  const keys = useQuery({
    queryKey: matrixKeysKey(ref),
    queryFn: () => parsed(listKeysOp, { path: ref, ...transport }),
    enabled: ref.org !== '' && ref.project !== '',
    retry: false,
  });
  const groups = useQuery({
    queryKey: matrixGroupsKey(ref),
    queryFn: () => parsed(listKeyGroupsOp, { path: ref, ...transport }),
    enabled: ref.org !== '' && ref.project !== '',
    retry: false,
  });
  const environmentItems = environments.data === undefined ? [] : environments.data.items;
  const values = useQueries({
    queries: environmentItems.map((environment) => ({
      queryKey: valuesKey({ ...ref, environment: environment.id }),
      queryFn: () =>
        parsed(listValuesOp, {
          path: { ...ref, environment: environment.id },
          ...transport,
        }),
      select: (value: MatrixValueList) => ({ environmentId: environment.id, value }),
      retry: false,
    })),
  });
  const settings = useQueries({
    queries: environmentItems.map((environment) => {
      const options = environmentSettingsQueryOptions(
        ref.org,
        ref.project,
        environment.id,
        transport.client,
      );
      return {
        ...options,
        select: (value: MatrixEnvironmentSettings) => ({
          environmentId: environment.id,
          value,
        }),
      };
    }),
  });
  const signals = useQueries({
    queries: environmentItems.map((environment) => ({
      queryKey: signalsKey(ref, environment.id),
      queryFn: async () => {
        const key = signalsKey(ref, environment.id);
        const previous = queries.getQueryData<MatrixEnvironmentSignals>(key);
        const next = zMatrixEnvironmentSignals.parse(
          await parsed(getEnvironmentSignalsOp, {
            path: { ...ref, environment: environment.id },
            ...transport,
          }),
        );
        if (signalsRequireValuesRefresh(previous?.revision, next.revision)) {
          await queries.invalidateQueries({
            queryKey: valuesKey({ ...ref, environment: environment.id }),
          });
        }
        if (next.environment_id !== environment.id) {
          throw new Error(
            `matrix signals query for ${environment.id} returned data for ${next.environment_id}`,
          );
        }
        return next;
      },
      select: (value: MatrixEnvironmentSignals) => ({ environmentId: environment.id, value }),
      // The fallback, not the heartbeat (#510): the advisory stream owns
      // liveness while it is healthy, and this cadence exists only for a
      // stream that has not connected or has failed. A healthy stream shows
      // `false` and the tab holds exactly one events connection, zero
      // periodic signals requests.
      refetchInterval: signalsPollInterval(signalsStream),
      retry: false,
    })),
  });
  const pendingDrafts = useQueries({
    queries: environmentItems.map((environment) => ({
      queryKey: pendingDraftsKey(ref, environment.id),
      queryFn: async () =>
        zMatrixPendingDraftList.parse(
          await parsed(listPendingDraftsOp, {
            path: { ...ref, environment: environment.id },
            ...transport,
          }),
        ),
      select: (value: MatrixPendingDraftList) => ({ environmentId: environment.id, value }),
      retry: false,
    })),
  });

  const environmentRows = useMemo(
    () =>
      assembleMatrixEnvironmentRows(environmentItems, {
        values: bindMatrixEnvironmentQueries('values', environmentItems, values),
        signals: bindMatrixEnvironmentQueries('signals', environmentItems, signals),
        settings: bindMatrixEnvironmentQueries('settings', environmentItems, settings),
        pendingDrafts: bindMatrixEnvironmentQueries(
          'pending drafts',
          environmentItems,
          pendingDrafts,
        ),
      }),
    [environmentItems, pendingDrafts, settings, signals, values],
  );

  return { environments, keys, groups, environmentRows };
}

export function useStageMatrixValue(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    // `acknowledgements` carries a keep-as-config token to dismiss a Surface-1
    // warning (#74): re-staging the SAME value with its token records the
    // dismissal so the identical value no longer re-warns. The save succeeds
    // either way — the token only settles whether the finding rides back.
    mutationFn: (input: {
      readonly environment: string;
      readonly key: string;
      readonly value: string;
      readonly acknowledgements?: readonly string[];
    }) =>
      parsed(setValueOp, {
          path: { ...ref, environment: input.environment, key: input.key },
          body: {
            value: input.value,
            ...(input.acknowledgements === undefined
              ? {}
              : { acknowledgements: [...input.acknowledgements] }),
          },
          ...transport,
        }),
    onSuccess: (_result, input) =>
      Promise.all([
        queries.invalidateQueries({ queryKey: valuesKey({ ...ref, environment: input.environment }) }),
        queries.invalidateQueries({ queryKey: signalsKey(ref, input.environment) }),
        queries.invalidateQueries({ queryKey: pendingDraftsKey(ref, input.environment) }),
      ]),
  });
}

/**
 * useReclassifyKey drives the reclassification ceremony (#12). The scanner's
 * warn dialog reaches it as the primary "reclassify as secret" resolution
 * (#74, ADR §4): moving the key to `secret` routes every value through secret
 * handling and drops the key's config-dismissals server-side. The keys query
 * is invalidated so the matrix reflects the new classification (the 🔒 lock).
 */
/**
 * useCreateKey declares a new key into the project catalogue (env-matrix 31's
 * web `+ key` / declare surface, #492). The declaration carries one value rule
 * with its type-specific constraints, and presence carries `required_in` and
 * `forbidden_in` as `all`/`none`/`explicit`. First values are staged separately
 * by the caller after the key exists, so a declaration and its opening values
 * ride the normal draft → publish pipeline.
 *
 * `all` is SYMBOLIC and covers environments created later; it is a choice the
 * operator makes, never one derived from "the explicit set happens to be every
 * environment today" — that derivation would silently exempt a new environment
 * from a rule written as "always" (zKeyPresence's own contract note).
 */
export type CreateKeyType = 'string' | 'integer' | 'boolean' | 'enum' | 'url' | 'json';

/** One value rule with only the constraints its type owns; the caller supplies
 * a field only when the operator set it, so the request never carries a
 * constraint on the wrong type (which the service refuses rather than ignores). */
export type CreateKeyRule = {
  readonly type: CreateKeyType;
  readonly minLength?: number;
  readonly maxLength?: number;
  readonly pattern?: string;
  readonly allowEmpty?: boolean;
  readonly min?: number;
  readonly max?: number;
  readonly members?: readonly string[];
  readonly schemes?: readonly string[];
  readonly jsonSchema?: string;
};

/** Presence for one axis: a mode plus, for `explicit`, the environment set. */
export type CreateKeyPresence = {
  readonly mode: 'all' | 'none' | 'explicit';
  readonly environmentIds: readonly string[];
};

function keyRuleBody(rule: CreateKeyRule): KeyRule {
  return {
    type: rule.type,
    ...(rule.minLength === undefined ? {} : { min_length: rule.minLength }),
    ...(rule.maxLength === undefined ? {} : { max_length: rule.maxLength }),
    ...(rule.pattern === undefined || rule.pattern === '' ? {} : { pattern: rule.pattern }),
    ...(rule.allowEmpty === undefined ? {} : { allow_empty: rule.allowEmpty }),
    ...(rule.min === undefined ? {} : { min: rule.min }),
    ...(rule.max === undefined ? {} : { max: rule.max }),
    ...(rule.members === undefined ? {} : { members: [...rule.members] }),
    ...(rule.schemes === undefined ? {} : { schemes: [...rule.schemes] }),
    ...(rule.jsonSchema === undefined || rule.jsonSchema === ''
      ? {}
      : { json_schema: rule.jsonSchema }),
  };
}

function presenceBody(presence: CreateKeyPresence): KeyPresence {
  return presence.mode === 'explicit'
    ? { mode: 'explicit', environment_ids: [...presence.environmentIds] }
    : { mode: presence.mode };
}

export function useCreateKey(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: {
      readonly name: string;
      readonly classification: 'config' | 'secret';
      readonly rule: CreateKeyRule;
      readonly folderPath?: string;
      readonly description?: string;
      readonly required: CreateKeyPresence;
      readonly forbidden: CreateKeyPresence;
      // Surface-2 acknowledgement tokens (#183): present only when the operator
      // overrode a scanner block; each dismisses the finding that produced it.
      readonly acknowledgements?: readonly string[];
    }) =>
      parsed(createKeyOp, {
          path: { ...ref },
          body: {
            name: input.name,
            classification: input.classification,
            declaration: { rule: keyRuleBody(input.rule) },
            ...(input.folderPath === undefined || input.folderPath === ''
              ? {}
              : { folder_path: input.folderPath }),
            ...(input.description === undefined || input.description === ''
              ? {}
              : { description: input.description }),
            presence: {
              required_in: presenceBody(input.required),
              forbidden_in: presenceBody(input.forbidden),
            },
            ...(input.acknowledgements === undefined || input.acknowledgements.length === 0
              ? {}
              : { acknowledgements: [...input.acknowledgements] }),
          },
          ...transport,
        }),
    onSuccess: () =>
      Promise.all([
        queries.invalidateQueries({ queryKey: matrixKeysKey(ref) }),
        queries.invalidateQueries({ queryKey: matrixGroupsKey(ref) }),
      ]),
  });
}

export type ValueOccurrenceList = z.infer<typeof zValueOccurrenceList>;
export type ImportValuesResult = z.infer<typeof zImportValuesResult>;

/**
 * useListValueOccurrences is import phase 1 (`import.presence`, #495): a
 * read-only POST that, for one environment, returns the project's definitions
 * revision and — per candidate — whether it is declared, whether it is `set`,
 * and a server-minted OPAQUE occurrence token. Nothing is written and no value
 * is sent; the token is the only thing that binds this review to the phase-2
 * write, so a candidate's intended classification/type must be its FINAL one
 * (an undeclared candidate's token binds that intent, and phase 2 matches it
 * only once the declaration lands).
 */
export function useListValueOccurrences(ref: MatrixRef) {
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: {
      readonly environment: string;
      readonly candidates: readonly {
        readonly name: string;
        readonly classification: KeyClassification;
        readonly type: CreateKeyType;
      }[];
    }) =>
      parsed(listValueOccurrencesOp, {
          path: { ...ref, environment: input.environment },
          body: {
            candidates: input.candidates.map((candidate) => ({
              name: candidate.name,
              intended_classification: candidate.classification,
              intended_type: candidate.type,
            })),
          },
          ...transport,
        }),
  });
}

/**
 * useImportValues is import phase 2 (`value.import`, #495): the strict,
 * human-only batch write. It is one transaction — an undeclared key rejects the
 * whole run by name (declare first), a `set` key is skipped unless it is in the
 * enumerated `overwrite` list, and the `precondition` (revision + the phase-1
 * tokens) is re-checked inside the write so a state that moved since review
 * rejects with 409 and writes nothing. On success it republishes, so the same
 * caches a stage+publish would invalidate are refreshed here.
 */
export function useImportValues(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: {
      readonly environment: string;
      readonly entries: readonly { readonly key: string; readonly value: string }[];
      readonly overwrite: readonly string[];
      readonly precondition: ImportPrecondition;
    }) =>
      parsed(importValuesOp, {
          path: { ...ref, environment: input.environment },
          body: {
            entries: input.entries.map((entry) => ({ key: entry.key, value: entry.value })),
            ...(input.overwrite.length === 0 ? {} : { overwrite: [...input.overwrite] }),
            precondition: input.precondition,
          },
          ...transport,
        }),
    onSuccess: (_result, input) => {
      const environment = { ...ref, environment: input.environment };
      return Promise.all([
        queries.invalidateQueries({ queryKey: valuesKey(environment) }),
        queries.invalidateQueries({ queryKey: signalsKey(ref, input.environment) }),
        queries.invalidateQueries({ queryKey: pendingDraftsKey(ref, input.environment) }),
        queries.invalidateQueries({ queryKey: revisionsKey(environment) }),
        queries.invalidateQueries({ queryKey: pinsKey(environment) }),
      ]);
    },
  });
}

export function useReclassifyKey(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { readonly key: string; readonly classification: 'secret' | 'config' }) =>
      parsed(reclassifyKeyOp, {
          path: { ...ref, key: input.key },
          body: { classification: input.classification },
          ...transport,
        }),
    // Reclassification moves how every occurrence of the key is HANDLED, not
    // just the catalogue lock: declassifying (`secret` → `config`)
    // re-materialises the value under ordinary config read, and tightening
    // (`config` → `secret`) drops the key's config dismissals and re-secures the
    // cells. Both change what the value/signals/pending views must show, so the
    // whole matrix is invalidated alongside the single-key detail and the list —
    // the metadata-only edit's narrow key+list invalidation is not enough here.
    onSuccess: (_result, input) =>
      Promise.all([
        queries.invalidateQueries({ queryKey: matrixKeyKey(ref, input.key) }),
        queries.invalidateQueries({ queryKey: matrixKeysKey(ref) }),
        queries.invalidateQueries({ queryKey: valuesMatrixKey(ref) }),
        queries.invalidateQueries({ queryKey: signalsMatrixKey(ref) }),
        queries.invalidateQueries({ queryKey: pendingMatrixKey(ref) }),
      ]),
  });
}

/**
 * useRenameKey renames a key by its immutable id (#494). Identity is that id, so
 * no stored reference breaks — but the DELIVERED payload's key set changes,
 * which is a content-affecting schema change and advances the schema revision.
 * `acknowledgements` carries Surface-2 override tokens because the new name is
 * exported to Git and treated as public, so the scanner can refuse it exactly as
 * it refuses a free-text declaration field. The single-key detail, the project
 * key list and the groups (whose membership is recorded by NAME) are all
 * invalidated so the new name shows everywhere it appears.
 */
export function useRenameKey(ref: MatrixRef, key: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { readonly name: string; readonly acknowledgements?: readonly string[] }) =>
      parsed(renameKeyOp, {
          path: { ...ref, key },
          body: {
            name: input.name,
            ...(input.acknowledgements === undefined || input.acknowledgements.length === 0
              ? {}
              : { acknowledgements: [...input.acknowledgements] }),
          },
          ...transport,
        }),
    onSuccess: () =>
      Promise.all([
        queries.invalidateQueries({ queryKey: matrixKeyKey(ref, key) }),
        queries.invalidateQueries({ queryKey: matrixKeysKey(ref) }),
        queries.invalidateQueries({ queryKey: matrixGroupsKey(ref) }),
      ]),
  });
}

/**
 * useDeleteKey removes a key declaration, its explicit presence rows and its
 * group membership (#494).
 *
 * It deliberately does NOT invalidate the single-key detail cache, and the list
 * invalidation is `exact` for a load-bearing reason: `matrixKeyKey` is
 * `matrixKeysKey` plus a suffix, so a non-exact list invalidation would ALSO
 * match the still-mounted single-key query and re-fetch the now-deleted key.
 * That 404 would reject this `onSuccess` promise and, with it, the caller's
 * navigate-to-matrix — leaving the surface stranded on the deleted key. `exact`
 * refreshes only the list; the surface navigates away and the stale single-key
 * cache is dropped on unmount (a later visit re-fetches to the recoverable
 * deleted-key state). The key vanishes from the whole matrix, so its per-
 * environment views are invalidated too — none of those keys is a prefix of the
 * single-key query, so they do not cascade.
 */
export function useDeleteKey(ref: MatrixRef, key: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: () => ok(deleteKeyOp, { path: { ...ref, key }, ...transport }),
    onSuccess: () =>
      Promise.all([
        queries.invalidateQueries({ queryKey: matrixKeysKey(ref), exact: true }),
        queries.invalidateQueries({ queryKey: matrixGroupsKey(ref) }),
        queries.invalidateQueries({ queryKey: valuesMatrixKey(ref) }),
        queries.invalidateQueries({ queryKey: signalsMatrixKey(ref) }),
        queries.invalidateQueries({ queryKey: pendingMatrixKey(ref) }),
      ]),
  });
}

/** One key's cross-environment lifecycle impact: the environments whose value
 *  the action moves (a set occurrence) or whose pending draft it disturbs. Only
 *  environment IDs cross this boundary — never a value cell, which for a config
 *  key can carry material the detail surface must never hold (#491). */
export type KeyImpact = {
  readonly setEnvironmentIds: readonly string[];
  readonly pendingEnvironmentIds: readonly string[];
};

/** assembleKeyImpact reduces the per-environment occupancy of ONE key into the
 *  two id lists a delete/reclassify preview shows. Pure over booleans the caller
 *  extracts from the matrix cells, so no cell (hence no value) reaches it. */
export function assembleKeyImpact(
  cells: readonly {
    readonly environmentId: string;
    readonly set: boolean;
    readonly pending: boolean;
  }[],
): KeyImpact {
  return {
    setEnvironmentIds: cells.filter((cell) => cell.set).map((cell) => cell.environmentId),
    pendingEnvironmentIds: cells.filter((cell) => cell.pending).map((cell) => cell.environmentId),
  };
}

/**
 * matrixImpactReady reports whether a key's impact preview can be trusted enough
 * to arm a destructive action. It fails CLOSED: every environment row's values
 * AND signals must be fully `ready` — an `error` row has no data and a `stale`
 * row may be outdated, either of which would silently drop an affected
 * environment from the preview and understate the blast radius. The empty case
 * (a project with no environments) is legitimately ready once the environment
 * list itself has loaded.
 */
export function matrixImpactReady(
  environmentsLoaded: boolean,
  rows: readonly { readonly values: { readonly status: MatrixQueryStatus }; readonly signals: { readonly status: MatrixQueryStatus } }[],
): boolean {
  return (
    environmentsLoaded &&
    rows.every((row) => row.values.status === 'ready' && row.signals.status === 'ready')
  );
}

/**
 * keyLifecycleRefusalText renders a rename/reclassify/delete refusal in the
 * caller's words, and — this is the security-sensitive part — WITHOUT ever
 * turning the reveal gate into an oracle.
 *
 * A caller-safe server detail is quoted verbatim (a rename collision names the
 * clashing key; a Surface-2 block names its rule id and locator). Otherwise:
 *  - 403 on a declassification is the one place a step-up may be named, because
 *    the server discloses the assurance requirement ONLY to a caller who already
 *    holds the reveal grant; every other 403 is a plain permission refusal.
 *  - 404 is the uniform missing-key sentence. For a declassification it ALSO
 *    masks "you do not hold reveal on this key" — the gate is a one-bit oracle
 *    the instant the UI says anything else, so this stays identical to every
 *    other 404.
 */
export function keyLifecycleRefusalText(
  error: Error,
  action: 'rename' | 'reclassify' | 'declassify' | 'delete',
): string {
  if (error instanceof ApiError) {
    // 404 is handled FIRST and its wording is the one canonical constant: for a
    // declassification a 404 ALSO masks "you do not hold reveal on this key",
    // and ANY variance — a caller-safe detail smuggled onto the 404, or copy
    // that differs from the ordinary missing-key line — is exactly the
    // existence/permission oracle the reveal gate exists to close. So a 404
    // never consults `callerSafeRefusal`.
    if (error.status === 404) {
      return KEY_GONE_REFUSAL;
    }
    if (error.status === 403) {
      return action === 'declassify'
        ? 'Declassifying a secret needs a recent second-factor sign-in. Reauthenticate, then try again.'
        : `You do not have permission to ${action} this key in this project.`;
    }
    const detailed = callerSafeRefusal(error, 'Refused');
    if (detailed !== null) {
      return detailed;
    }
    if (error.status === 409) {
      return `The server refused this ${action}; reload the key and retry.`;
    }
    return `The server could not ${action} this key (error ${String(error.status)}).`;
  }
  return `The server could not ${action} this key.`;
}

/** The one missing-key sentence every key refusal shares. A single constant so
 *  no surface can render a distinguishable 404 that would turn the reveal gate
 *  into an existence oracle. */
export const KEY_GONE_REFUSAL = 'This key no longer exists. Return to the matrix and reopen it.';

/** One key declaration, as the catalogue detail surface (#491) reads it. */
export type MatrixKey = MatrixKeyList['items'][number];

/**
 * useKey loads ONE key's full declaration by its immutable id — the catalogue
 * detail surface's own fetch (#491), not a slice of the matrix list. A first-
 * class query gives the surface its own loading, 404 (deleted key) and 403
 * (authorization) states rather than inferring "missing" from a list that has
 * finished loading, which a deep link cannot tell apart from "not yet loaded".
 */
export function useKey(ref: MatrixRef, key: string) {
  const transport = useTransport();
  return useQuery({
    queryKey: matrixKeyKey(ref, key),
    queryFn: () => parsed(getKeyOp, { path: { ...ref, key }, ...transport }),
    enabled: ref.org !== '' && ref.project !== '' && key !== '',
    retry: false,
  });
}

/**
 * useUpdateKeyMetadata edits a key's organisational and documentation fields
 * (folder, description, deprecation) — the smallest complete write of the
 * shared declaration editor foundation (#491). It carries `acknowledgements`
 * so a Surface-2 scanning block on a free-text field can be deliberately
 * overridden once the caller owns the finding. Both the single-key detail and
 * the project key list are invalidated so the matrix reflects the edit.
 */
export function useUpdateKeyMetadata(ref: MatrixRef, key: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: {
      readonly folderPath?: string;
      readonly description?: string;
      readonly deprecated?: boolean;
      readonly deprecationNote?: string;
      readonly acknowledgements?: readonly string[];
    }) =>
      parsed(updateKeyMetadataOp, {
          path: { ...ref, key },
          body: {
            ...(input.folderPath === undefined ? {} : { folder_path: input.folderPath }),
            ...(input.description === undefined ? {} : { description: input.description }),
            ...(input.deprecated === undefined ? {} : { deprecated: input.deprecated }),
            ...(input.deprecationNote === undefined
              ? {}
              : { deprecation_note: input.deprecationNote }),
            ...(input.acknowledgements === undefined || input.acknowledgements.length === 0
              ? {}
              : { acknowledgements: [...input.acknowledgements] }),
          },
          ...transport,
        }),
    onSuccess: () =>
      Promise.all([
        queries.invalidateQueries({ queryKey: matrixKeyKey(ref, key) }),
        queries.invalidateQueries({ queryKey: matrixKeysKey(ref) }),
        queries.invalidateQueries({ queryKey: matrixGroupsKey(ref) }),
      ]),
  });
}

/**
 * keyMetadataRefusalText renders a metadata-edit refusal in the caller's own
 * words. A caller-safe server detail (a Surface-2 declaration block names its
 * rule id and locator, never the matched text) is shown verbatim; the uniform
 * 403/404/409 refusals fall back to fixed sentences so a bare status is never
 * the whole message.
 */
export function keyMetadataRefusalText(error: Error): string {
  if (error instanceof ApiError) {
    // 404 first and canonical, for the same anti-oracle reason as
    // keyLifecycleRefusalText: a 404 never consults the caller-safe detail.
    if (error.status === 404) {
      return KEY_GONE_REFUSAL;
    }
    const detailed = callerSafeRefusal(error, 'Refused');
    if (detailed !== null) {
      return detailed;
    }
    if (error.status === 403) {
      return 'You do not have permission to edit this key in this project.';
    }
    if (error.status === 409) {
      return 'The server refused this edit; reload the key and retry.';
    }
    return `The server could not save this key (error ${String(error.status)}).`;
  }
  return 'The server could not save this key.';
}

export function useClearMatrixValue(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { readonly environment: string; readonly key: string }) =>
      parsed(clearValueOp, { path: { ...ref, environment: input.environment, key: input.key }, ...transport }),
    onSuccess: (_result, input) =>
      Promise.all([
        queries.invalidateQueries({ queryKey: valuesKey({ ...ref, environment: input.environment }) }),
        queries.invalidateQueries({ queryKey: signalsKey(ref, input.environment) }),
        queries.invalidateQueries({ queryKey: pendingDraftsKey(ref, input.environment) }),
      ]),
  });
}

export function usePublishMatrix(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: async (input: {
      readonly addressedEnvironment: string;
      readonly environmentIds: readonly string[];
      readonly versionIds: readonly string[];
    }) => {
      const preview = restorePreviewFor(ref, input.versionIds);
      if (preview !== null && 'conflict' in preview) {
        throw new RestorePreviewSelectionError(
          'Restore drafts must be published exactly as previewed — deselect the other drafts or ' +
          `stage the restore again. Overlapping version ids: ${preview.conflict.join(', ')}.`,
        );
      }
      const previewToken = preview?.token;
      let result: z.infer<typeof zPublishResult>;
      try {
        result = await parsed(publishPendingChangesOp, {
            path: { ...ref, environment: input.addressedEnvironment },
            body: {
              version_ids: [...input.versionIds],
              ...(previewToken === undefined ? {} : { preview_token: previewToken }),
            },
            ...transport,
          });
      } catch (error) {
        if (previewToken !== undefined && error instanceof Error) {
          previewAttachedErrors.add(error);
        }
        throw error;
      }
      const publishedEnvironmentIds = new Set(
        result.environments.map((environment) => environment.environment_id),
      );
      const missingEnvironment = input.environmentIds.find(
        (environmentId) => !publishedEnvironmentIds.has(environmentId),
      );
      if (missingEnvironment !== undefined) {
        throw new Error(
          `publish succeeded without a revision for environment ${missingEnvironment}`,
        );
      }
      return result;
    },
    onSuccess: (result, input) => {
      forgetRestorePreviews(ref, input.versionIds);
      return Promise.all([
        queries.invalidateQueries({ queryKey: valuesMatrixKey(ref) }),
        queries.invalidateQueries({ queryKey: signalsMatrixKey(ref) }),
        queries.invalidateQueries({ queryKey: pendingMatrixKey(ref) }),
        ...result.environments.flatMap((published) => {
          const env = { ...ref, environment: published.environment_id };
          return [
            queries.invalidateQueries({ queryKey: revisionsKey(env) }),
            queries.invalidateQueries({ queryKey: pinsKey(env) }),
          ];
        }),
      ]);
    },
  });
}

/** Config-only copy: secret copy stays on Values with its disclosure ceremony. */
export function useCopyMatrixConfig(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: {
      readonly sourceEnvironment: string;
      readonly key: string;
      readonly destinationEnvironments: readonly string[];
      readonly confirmProtected: boolean;
    }) =>
      parsed(copyValuesOp, {
          path: ref,
          body: {
            source_environment_id: input.sourceEnvironment,
            keys: [input.key],
            destination_environment_ids: [...input.destinationEnvironments],
            confirm_protected: input.confirmProtected,
          },
          ...transport,
        }),
    onSuccess: (result, input) =>
      invalidateAfterCopy(queries, ref, [
        ...new Set([
          ...input.destinationEnvironments,
          ...result.copied.map((copied) => copied.destination_environment_id),
        ]),
      ]),
    onSettled: (_result, _error, input) =>
      queries.invalidateQueries({
        queryKey: windowKey({ ...ref, environment: input.sourceEnvironment }),
      }),
  });
}

/**
 * matrixMutationError turns a refusal into something the human can act on.
 *
 * The server's caller-safe detail is quoted VERBATIM whenever there is one.
 * Every refusal that names a key — a presence veto, a value that fails the
 * current schema, a stale or missing restore preview token — carries it, and
 * mvp-boundary C5 requires a schema-failing restore to block loud naming the
 * keys. Paraphrasing would put a second vocabulary in front of the one the CLI
 * and the audit trail use, and dropping it leaves a 400 with nothing to fix.
 */
export function matrixMutationError(
  error: Error,
  action: 'stage' | 'clear' | 'copy' | 'publish' | 'create' | 'import',
  restorePreviewAttached = false,
): string {
  if (error instanceof RestorePreviewSelectionError) {
    return error.message;
  }
  if (action === 'publish' && restorePreviewAttached && error instanceof ApiError && error.status === 409) {
    return 'Publish refused: the restore preview is stale or missing — stage the restore again from the history drawer.';
  }
  // `create` declares a key and `import` writes a batch; neither reads as a
  // single value, so both get their own object phrasing in the fallbacks.
  const object =
    action === 'create'
      ? 'declare this key'
      : action === 'import'
        ? 'import these values'
        : `${action} this value`;
  if (error instanceof ApiError) {
    const detailed = callerSafeRefusal(error, action === 'publish' ? 'Publish refused' : 'Refused');
    if (detailed !== null) {
      return action === 'publish'
        ? `${detailed} Fix the named key in the matrix row editor, then publish again.`
        : detailed;
    }
    if (error.status === 403) {
      return action === 'publish'
        ? 'You do not have permission to publish the selected drafts.'
        : action === 'create'
          ? 'You do not have permission to declare keys in this project.'
          : action === 'import'
            ? 'You do not have permission to import values into this environment.'
            : `You do not have permission to ${action} this value.`;
    }
    if (error.status === 409) {
      return action === 'publish'
        ? 'Publish was refused. Fix the named matrix problems, then retry.'
        : action === 'create'
          ? 'The server refused the declaration; reload the matrix and retry.'
          : action === 'import'
            ? 'The reviewed state moved before this import ran — re-review this environment and try again.'
            : `The server refused this ${action}; reload the matrix and retry.`;
    }
    return action === 'publish'
      ? `The server could not publish the selected drafts (error ${String(error.status)}).`
      : `The server could not ${object} (error ${String(error.status)}).`;
  }
  return action === 'publish'
    ? 'The server could not publish the selected drafts.'
    : `The server could not ${object}.`;
}
