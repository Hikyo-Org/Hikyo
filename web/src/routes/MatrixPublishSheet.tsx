import { useState } from 'react';

import type { EnvironmentList } from '../api/values.ts';
import { Ceremony } from './Ceremony.tsx';
import {
  blockedPublishEnvironmentIds,
  type MatrixProblem,
} from './matrix-state.ts';
import {
  useProtectedPublishCeremony,
  type ProtectedPublishTarget,
} from './useProtectedPublishCeremony.ts';

type Environment = EnvironmentList['items'][number];

export type MatrixPendingEntry = {
  readonly versionId: string;
  readonly keyId: string;
  readonly name: string;
  readonly classification: 'config' | 'secret';
  readonly operation: 'set' | 'unset';
  readonly configPreview?: string;
  /** The linked-key group this key belongs to, when the matrix knows it. */
  readonly group?: { readonly id: string; readonly name: string };
};

/**
 * Linked keys publish together, so the sheet shows them together: one bucket
 * per group (in first-seen order), then the ungrouped entries. Entries without
 * a group stay flat.
 */
export function groupPendingEntries(
  entries: readonly MatrixPendingEntry[],
): readonly { readonly group: MatrixPendingEntry['group'] | undefined; readonly entries: readonly MatrixPendingEntry[] }[] {
  const buckets = new Map<string, MatrixPendingEntry[]>();
  const ungrouped: MatrixPendingEntry[] = [];
  const groups = new Map<string, NonNullable<MatrixPendingEntry['group']>>();
  for (const entry of entries) {
    if (entry.group === undefined) {
      ungrouped.push(entry);
      continue;
    }
    groups.set(entry.group.id, entry.group);
    buckets.set(entry.group.id, [...(buckets.get(entry.group.id) ?? []), entry]);
  }
  return [
    ...[...buckets].map(([id, grouped]) => ({ group: groups.get(id), entries: grouped })),
    ...(ungrouped.length === 0 ? [] : [{ group: undefined, entries: ungrouped }]),
  ];
}

/** Selective publish review mirrors the frozen per-environment sheet. */
export function MatrixPublishSheet({
  refData,
  environments,
  revisions,
  pendingByEnvironment,
  problems,
  protectedEnvironmentIds,
  busy,
  mutationError,
  onPublish,
  onClose,
}: {
  refData: { readonly org: string; readonly project: string };
  environments: readonly Environment[];
  revisions: ReadonlyMap<string, bigint>;
  pendingByEnvironment: ReadonlyMap<string, readonly MatrixPendingEntry[]>;
  problems: readonly MatrixProblem[];
  protectedEnvironmentIds: readonly string[];
  busy: boolean;
  mutationError: string | null;
  onPublish: (environmentIds: readonly string[]) => void;
  /** Closes the sheet; the caller returns focus to the drafts button. */
  onClose: () => void;
}) {
  const pendingEnvironmentIds = environments
    .filter((environment) => (pendingByEnvironment.get(environment.id)?.length ?? 0) > 0)
    .map((environment) => environment.id);
  const blockedEnvironmentIds = blockedPublishEnvironmentIds(problems, pendingEnvironmentIds);
  const selectableEnvironmentIds = pendingEnvironmentIds.filter(
    (environmentId) => !blockedEnvironmentIds.has(environmentId),
  );
  // `null` means "all currently selectable". Signal queries arrive independently,
  // so taking one mount-time snapshot could omit a later environment forever.
  const [selectedEnvironmentIdsState, setSelectedEnvironmentIds] =
    useState<readonly string[] | null>(null);
  const [protectedConfirmed, setProtectedConfirmed] = useState(false);
  const selectedEnvironmentIds = (
    selectedEnvironmentIdsState ?? selectableEnvironmentIds
  ).filter((environmentId) => selectableEnvironmentIds.includes(environmentId));
  const selectedEntries = selectedEnvironmentIds.flatMap(
    (environmentId) => pendingByEnvironment.get(environmentId) ?? [],
  );
  const selectedProtectedIds = selectedEnvironmentIds.filter((environmentId) =>
    protectedEnvironmentIds.includes(environmentId),
  );
  const protectedConfirmationRequired = selectedProtectedIds.length > 0;
  const protectedGuard = useProtectedPublishCeremony(
    refData,
    selectedEnvironmentIds.map((environmentId) => [
      environmentId,
      (pendingByEnvironment.get(environmentId) ?? []).map((entry) => entry.keyId),
    ]),
  );

  const protectedTargets = (): readonly ProtectedPublishTarget[] =>
    selectedProtectedIds.map((environmentId) => {
      const environment = environments.find((candidate) => candidate.id === environmentId);
      if (environment === undefined) {
        throw new Error(`protected publish environment ${environmentId} is not in the matrix`);
      }
      const entries = pendingByEnvironment.get(environmentId) ?? [];
      return {
        environmentId,
        environmentName: environment.name,
        keys: entries.map((entry) => ({ id: entry.keyId, name: entry.name, classification: entry.classification })),
      };
    });

  return (
    <>
      <section className="matrix__publish" id="matrix-publish" aria-label="Publish drafts">
        <h2>Publish drafts</h2>
        <p>
          Each environment publishes as its own atomic revision: untick any you want to hold
          back.
        </p>
        {environments.map((environment) => {
          const entries = pendingByEnvironment.get(environment.id) ?? [];
          if (entries.length === 0) {
            return null;
          }
          const environmentProblems = problems.filter(
            (problem) => problem.environmentId === environment.id,
          );
          const blocked = blockedEnvironmentIds.has(environment.id);
          const checked = !blocked && selectedEnvironmentIds.includes(environment.id);
          const revision = revisions.get(environment.id);
          if (revision === undefined) {
            throw new Error(`publish review has no revision for environment ${environment.id}`);
          }
          return (
            <div
              className={`matrix__publish-env${blocked ? ' matrix__publish-env--blocked' : ''}`}
              key={environment.id}
            >
              <label className="matrix__publish-heading">
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={blocked || busy}
                  onChange={() => {
                    setSelectedEnvironmentIds((current) => {
                      const selected = current ?? selectableEnvironmentIds;
                      return checked
                        ? selected.filter((id) => id !== environment.id)
                        : [...selected, environment.id];
                    });
                    setProtectedConfirmed(false);
                  }}
                />
                <strong>{environment.name}</strong>
                {protectedEnvironmentIds.includes(environment.id) ? (
                  <span className="matrix__publish-protected">
                    PROTECTED: confirms before publish
                  </span>
                ) : null}
                <span className="matrix__publish-revision">
                  {`r${String(revision)} → r${String(revision + 1n)}`}
                </span>
              </label>
              {groupPendingEntries(entries).map((bucket) => (
                <ul
                  key={bucket.group?.id ?? ''}
                  className={bucket.group === undefined ? undefined : 'matrix__publish-group'}
                  aria-label={bucket.group === undefined ? undefined : `Linked keys: ${bucket.group.name}, publish together`}
                >
                  {bucket.group === undefined ? null : (
                    <li className="matrix__publish-group-name">
                      <span aria-hidden="true">🔗 </span>
                      {`Linked keys: ${bucket.group.name}`}
                    </li>
                  )}
                  {bucket.entries.map((entry) => (
                    <li key={entry.versionId} className="mono">
                      <span>
                        {entry.classification === 'secret' ? '🔒 ' : ''}
                        {entry.name}
                      </span>
                      <span>{publishPreview(entry)}</span>
                    </li>
                  ))}
                </ul>
              ))}
              {blocked ? (
                <div className="matrix__publish-blocked" role="alert">
                  {`✕ Publish blocked: ${environmentProblems
                    .map((problem) => `${problem.keyName} in ${environment.name}`)
                    .join('; ')}. This environment has violations or missing required keys.`}
                </div>
              ) : (
                <span className="matrix__publish-ready">✓ ready</span>
              )}
            </div>
          );
        })}
        {protectedConfirmationRequired ? (
          <label className="matrix__publish-confirmation">
            <input
              type="checkbox"
              checked={protectedConfirmed}
              onChange={(event) => setProtectedConfirmed(event.target.checked)}
            />
            <span>
              I confirm publishing to protected{' '}
              {selectedProtectedIds
                .map((environmentId) =>
                  environmentName(environments, environmentId),
                )
                .join(', ')}
              .
            </span>
          </label>
        ) : null}
        {protectedGuard.error === null ? null : (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{protectedGuard.error}</span>
          </p>
        )}
        <button
          type="button"
          className="btn btn--primary"
          disabled={
            busy ||
            selectedEnvironmentIds.length === 0 ||
            (protectedConfirmationRequired && !protectedConfirmed)
          }
          onClick={() => {
            if (protectedConfirmationRequired && !protectedConfirmed) {
              throw new Error('protected publish started without explicit confirmation');
            }
            void protectedGuard.run(
              protectedTargets(),
              () => onPublish(selectedEnvironmentIds),
              'The protected publish guard could not be read, so nothing was published',
            );
          }}
        >
          {busy
            ? 'Publishing atomically…'
            : `Publish selected · ${String(selectedEntries.length)} draft${selectedEntries.length === 1 ? '' : 's'} · ${String(selectedEnvironmentIds.length)} environment${selectedEnvironmentIds.length === 1 ? '' : 's'}`}
        </button>
        <button type="button" className="btn" onClick={onClose} disabled={busy}>
          Close
        </button>
        {mutationError === null ? null : (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{mutationError}</span>
          </p>
        )}
        <p>Invalid environments cannot publish: delivery only sees valid revisions.</p>
      </section>
      {protectedGuard.request === null ? null : (
        <Ceremony
          key={protectedGuard.requestKey}
          request={protectedGuard.request}
          onAuthorised={protectedGuard.onAuthorised}
          onCancel={protectedGuard.onCancel}
        />
      )}
    </>
  );
}

function publishPreview(entry: MatrixPendingEntry): string {
  if (entry.operation === 'unset') {
    return '(cleared)';
  }
  if (entry.classification === 'secret') {
    return '••••••••';
  }
  return entry.configPreview ?? '(staged value unavailable after reload)';
}

function environmentName(environments: readonly Environment[], environmentId: string): string {
  const environment = environments.find((candidate) => candidate.id === environmentId);
  if (environment === undefined) {
    throw new Error(`selected publish environment ${environmentId} is not in the matrix`);
  }
  return environment.name;
}
