import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router';

import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { historyHref } from '../api/history.ts';
import type { MatrixKeyList, MatrixSignalCell } from '../api/matrix.ts';
import type { MatrixRef } from '../api/keys.ts';
import { withRemote, useTransport, useWorkspaceContext } from '../api/transport.tsx';
import {
  disclosureRefusalText,
  fetchRevealWindow,
  useRevealOne,
  useRevealWindow,
  type EnvironmentList,
  type ValueCell,
} from '../api/values.ts';
import { writeExpiringClipboard } from '../app/clipboard.ts';
import { Ceremony, type CeremonyPurpose } from './Ceremony.tsx';
import {
  canClearMatrixCell,
  copyRequiresProtectedConfirmation,
  draftValueForMatrixCell,
  matrixDraftChanges,
  validateMatrixDraft,
  type MatrixDraftChange,
  type MatrixDraftEdit,
} from './matrix-state.ts';
import {
  useProtectedPublishCeremony,
  type ProtectedPublishTarget,
} from './useProtectedPublishCeremony.ts';
import { useCeremonyTask, type CeremonyTask } from './useCeremonyTask.ts';
import { useModalDialog } from './useModalDialog.ts';

type MatrixKey = MatrixKeyList['items'][number];
type Environment = EnvironmentList['items'][number];

type EditorRow = {
  readonly environmentId: string;
  readonly environment: Environment;
  readonly protected: boolean;
  // #451: this column's value/signal reads failed or were denied. It is excluded
  // from bulk edit and copy destinations — the caller cannot read it, so writing
  // to it blind would be a silent guess.
  readonly degraded: boolean;
  readonly cell: ValueCell | undefined;
  readonly signal: MatrixSignalCell | undefined;
  readonly draftPreview: string | undefined;
  readonly problems: readonly { readonly message: string }[];
};

export type MatrixEditorChange = MatrixDraftChange;

const REMASK_MS = 10_000;

/** Locked cell modal: one environment first, with explicit multi-environment editing. */
export function MatrixRowEditor({
  refData,
  keyRecord,
  environmentId,
  rows,
  busy,
  mutationError,
  onClose,
  onApply,
  onCopy,
}: {
  refData: MatrixRef;
  keyRecord: MatrixKey;
  environmentId: string;
  rows: readonly EditorRow[];
  busy: boolean;
  mutationError: string | null;
  onClose: () => void;
  onApply: (changes: readonly MatrixEditorChange[]) => Promise<void>;
  onCopy: (destinations: readonly string[], confirmProtected: boolean) => void;
}) {
  const dialog = useModalDialog();
  const initialDrafts = useMemo(
    () =>
      new Map(
        rows.map((row) => [
          row.environmentId,
          draftValueForMatrixCell(
            keyRecord.classification,
            row.cell?.set === true ? row.cell.value : undefined,
            row.signal?.pending?.operation,
            row.draftPreview,
          ),
        ]),
      ),
    [keyRecord.classification, rows],
  );
  const [edits, setEdits] = useSensitiveState<ReadonlyMap<string, MatrixDraftEdit>>(() => new Map());
  const [fillAll, setFillAll] = useSensitiveState('');
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState<string | null>(null);
  const [copyOpen, setCopyOpen] = useState(false);
  const [editAll, setEditAll] = useState(false);
  const [destinations, setDestinations] = useState<readonly string[]>([]);
  const [protectedCopyConfirmed, setProtectedCopyConfirmed] = useState(false);
  const protectedEnvironmentIds = rows.flatMap((row) =>
    row.protected ? [row.environmentId] : [],
  );
  const protectedGuard = useProtectedPublishCeremony(refData, [
    environmentId,
    keyRecord.id,
    destinations,
    protectedEnvironmentIds,
  ]);
  const sourceRow = rows.find((row) => row.environmentId === environmentId);
  if (sourceRow === undefined) {
    throw new Error(`matrix editor environment ${environmentId} is not in its keyed rows`);
  }
  const environment = sourceRow.environment;
  const workspace = useWorkspaceContext();
  const sourceSet = sourceRow.cell?.set === true;
  const disclosure = useCellDisclosure(refData, keyRecord, sourceRow);
  // Degraded columns (#451) are never editable here: bulk edit and fill-all
  // target only the columns whose reads succeeded.
  const editableRows = rows.filter((row) => !row.degraded);
  const visibleRows = editAll ? editableRows : [sourceRow];
  const degradedEnvironmentIds = rows.flatMap((row) => (row.degraded ? [row.environmentId] : []));
  const degradedSignature = degradedEnvironmentIds.join('/');
  // A copy destination or a queued bulk edit chosen before its column degraded
  // would otherwise linger in `destinations`/`edits` and reach the copy or apply
  // call for a column we can no longer read. Prune both when a column degrades so
  // the selectable set and every pending change stay in sync (#451).
  useEffect(() => {
    const degraded = new Set(degradedSignature === '' ? [] : degradedSignature.split('/'));
    setDestinations((current) => {
      const pruned = current.filter((id) => !degraded.has(id));
      return pruned.length === current.length ? current : pruned;
    });
    setEdits((current) => {
      if (![...current.keys()].some((id) => degraded.has(id))) {
        return current;
      }
      return new Map([...current].filter(([id]) => !degraded.has(id)));
    });
  }, [degradedSignature]);
  const protectedConfirmationRequired = copyRequiresProtectedConfirmation(
    destinations,
    protectedEnvironmentIds,
  );
  const protectedDestinationNames = rows
    .filter(
      (row) =>
        destinations.includes(row.environmentId) &&
        protectedEnvironmentIds.includes(row.environmentId),
    )
    .map((row) => row.environment.name);

  const validationByEnvironment = new Map<string, ReturnType<typeof validateMatrixDraft>>();
  for (const row of rows) {
    const edit = edits.get(row.environmentId);
    if (edit?.op !== 'set') {
      continue;
    }
    const error = validateDeclaration(keyRecord, edit.value);
    if (error !== null) {
      validationByEnvironment.set(row.environmentId, error);
    }
  }

  // Derived from editableRows, not rows: a change is only ever applied to a
  // column whose reads succeed, even if a stale edit for a since-degraded column
  // has not yet been pruned from `edits` (#451).
  const changes = matrixDraftChanges(
    editableRows.map((row) => row.environmentId),
    edits,
  );

  const protectedTargets = (): readonly ProtectedPublishTarget[] =>
    destinations
      .filter((environmentId) => protectedEnvironmentIds.includes(environmentId))
      .map((environmentId) => {
        const destination = rows.find((row) => row.environmentId === environmentId);
        if (destination === undefined) {
          throw new Error(`protected copy destination ${environmentId} is not in the matrix`);
        }
        return {
          environmentId,
          environmentName: destination.environment.name,
          keys: [{ id: keyRecord.id, name: keyRecord.name }],
        };
      });

  return (
    <>
      <dialog
        className="matrix-editor matrix-row-editor"
        ref={dialog}
        onClose={onClose}
        onClick={(event) => {
          if (event.target === event.currentTarget) onClose();
        }}
      >
        <form
          method="dialog"
          onSubmit={(event) => {
            event.preventDefault();
            if (changes.length === 0) return;
            setApplying(true);
            setApplyError(null);
            void onApply(changes)
              .catch(() => setApplyError('The draft update failed. Re-enter the value in the named row and retry.'))
              .finally(() => {
                setEdits(new Map());
                setFillAll('');
                setApplying(false);
              });
          }}
        >
          <div className="matrix-editor__head">
            <div>
              <p className="matrix-editor__eyebrow">
                {`${environment.name} · ${keyRecord.classification}`}
              </p>
              <h2 className="mono">
                {keyRecord.classification === 'secret' ? (
                  <span aria-hidden="true">🔒 </span>
                ) : null}
                {keyRecord.name}
              </h2>
              <p>{keyRecord.description || 'Explicit value and provenance for this environment.'}</p>
            </div>
            <button
              type="button"
              className="btn matrix-editor__close"
              aria-label="Close row editor"
              onClick={onClose}
            >
              ✕
            </button>
          </div>

          <details className="matrix-editor__schema">
            <summary>Schema and presence rules</summary>
            <pre className="mono">{JSON.stringify({
              declaration: keyRecord.declaration,
              presence: keyRecord.presence,
            }, null, 2)}</pre>
          </details>

          {editAll ? <div className="matrix-row-editor__fill">
            <label htmlFor="matrix-fill-all">Fill all environments</label>
            <div>
              <input
                id="matrix-fill-all"
                className="mono"
                type={keyRecord.classification === 'secret' ? 'password' : 'text'}
                autoComplete="off"
                value={fillAll}
                placeholder={keyRecord.classification === 'secret' ? 'Write-only replacement' : 'Shared draft value'}
                onChange={(event) => setFillAll(event.target.value)}
              />
              <button
                type="button"
                className="btn"
                disabled={fillAll === '' || busy || applying}
                onClick={() => {
                  setEdits(new Map<string, MatrixDraftEdit>(
                    editableRows.map((row) => [row.environmentId, { op: 'set', value: fillAll }]),
                  ));
                  setFillAll('');
                }}
              >
                Fill all
              </button>
            </div>
          </div> : null}

          <div className="matrix-row-editor__rows">
            {visibleRows.map((row) => {
              const rowEnvironmentId = row.environmentId;
              const publishedSet = row.cell?.set === true;
              const edit = edits.get(rowEnvironmentId);
              const clearing = edit?.op === 'unset';
              const liveValidation = validationByEnvironment.get(rowEnvironmentId) ?? null;
              return (
                <section
                  className={`matrix-row-editor__row${row.protected ? ' matrix-row-editor__row--protected' : ''}`}
                  key={rowEnvironmentId}
                  aria-labelledby={`matrix-row-${rowEnvironmentId}`}
                >
                  <div className="matrix-row-editor__row-head">
                    <h3 id={`matrix-row-${rowEnvironmentId}`}>{row.environment.name}</h3>
                    {row.protected ? <span>PROTECTED</span> : null}
                    <span>{publishedSet ? 'explicit set' : 'explicit absent'}</span>
                    {row.signal?.pending === undefined ? null : (
                      <span>{`Δ ${row.signal.pending.operation} pending`}</span>
                    )}
                  </div>
                  {row.problems.map((problem) => (
                    <p className="alert" role="alert" key={problem.message}>
                      <span className="alert__glyph" aria-hidden="true">!</span>
                      <span>{problem.message}</span>
                    </p>
                  ))}
                  <label htmlFor={`matrix-edit-${rowEnvironmentId}`}>
                    {`${row.environment.name} value`}
                  </label>
                  <textarea
                    id={`matrix-edit-${rowEnvironmentId}`}
                    className="mono matrix-editor__value"
                    rows={keyRecord.declaration.rule?.type === 'json' ? 6 : 2}
                    autoComplete="off"
                    value={
                      edit?.op === 'set'
                        ? edit.value
                        : clearing
                          ? ''
                          : initialDrafts.get(rowEnvironmentId) ?? ''
                    }
                    placeholder={
                      keyRecord.classification === 'secret'
                        ? publishedSet
                          ? 'Write-only · replace current secret'
                          : 'Write-only · set a new secret'
                        : publishedSet
                          ? 'Edit the explicit value'
                          : 'Touch to stage an explicit value'
                    }
                    aria-invalid={liveValidation?.level === 'error' ? true : undefined}
                    aria-describedby={liveValidation === null ? undefined : `matrix-error-${rowEnvironmentId}`}
                    onChange={(event) => {
                      setEdits((current) => {
                        const next = new Map(current);
                        next.set(rowEnvironmentId, { op: 'set', value: event.target.value });
                        return next;
                      });
                    }}
                  />
                  {liveValidation === null ? null : (
                    <p
                      className={liveValidation.level === 'error' ? 'matrix-cell__error' : 'matrix-editor__hint'}
                      id={`matrix-error-${rowEnvironmentId}`}
                    >
                      {liveValidation.message}
                    </p>
                  )}
                  <dl className="matrix-editor__provenance">
                    <div>
                      <dt>Updated</dt>
                      <dd>{row.cell?.updated_at === undefined ? 'No published value' : formatTimestamp(row.cell.updated_at)}</dd>
                    </div>
                    <div><dt>Updated by</dt><dd className="mono">{row.cell?.updated_by ?? '—'}</dd></div>
                    <div>
                      <dt>Revision</dt>
                      <dd>{row.signal?.changed_in_revision === undefined ? 'No change signal' : `r${String(row.signal.changed_in_revision)}`}</dd>
                    </div>
                  </dl>
                  {rowEnvironmentId === environmentId && disclosure.revealed !== null ? (
                    <p className="matrix-editor__revealed mono" aria-label={`${keyRecord.name} revealed`}>
                      <span>{disclosure.revealed.value}</span>
                      <small>{`re-masks in ${String(disclosure.revealed.remaining)}s`}</small>
                    </p>
                  ) : null}
                  <button
                    type="button"
                    className="btn"
                    disabled={busy || applying || (!clearing && !canClearMatrixCell(publishedSet, row.signal?.pending?.operation))}
                    onClick={() => {
                      setEdits((current) => {
                        if (clearing) {
                          const kept = new Map(current);
                          kept.delete(rowEnvironmentId);
                          return kept;
                        }
                        return new Map(current).set(rowEnvironmentId, { op: 'unset' });
                      });
                    }}
                  >
                    {clearing ? 'Keep current state' : `Clear ${row.environment.name} to absent`}
                  </button>
                </section>
              );
            })}
          </div>

          {applyError === null ? null : <p className="alert" role="alert">{applyError}</p>}
          {mutationError === null ? null : <p className="alert" role="alert">{mutationError}</p>}
          {disclosure.error === null ? null : <p className="alert" role="alert">{disclosure.error}</p>}
          {disclosure.notice === null ? null : <p className="notice" role="status">{disclosure.notice}</p>}

          <div className="matrix-editor__actions">
            <button
              type="submit"
              className="btn btn--primary"
              disabled={changes.length === 0 || busy || applying}
            >
              {busy || applying ? 'Saving drafts…' : `Save ${String(changes.length)} draft${changes.length === 1 ? '' : 's'}`}
            </button>
            {!editAll && rows.length > 1 ? (
              <button type="button" className="btn" onClick={() => setEditAll(true)}>
                Edit all environments
              </button>
            ) : null}
            {sourceSet && keyRecord.classification === 'secret' && disclosure.canReveal ? (
              <button type="button" className="btn" onClick={disclosure.reveal}>
                {`Reveal ${keyRecord.name}`}
              </button>
            ) : null}
            {sourceSet ? (
              <button type="button" className="btn" onClick={disclosure.copy}>
                {`Copy ${keyRecord.name}`}
              </button>
            ) : null}
            {/*
              The per-key history entry point. Per-key history is a FILTER over
              the same lineage, so it is the history surface with `key` set —
              never a second surface, and never a second fetch.
            */}
            <Link
              className="btn"
              to={withRemote(
                historyHref({ ...refData, env: environment.id, keyId: keyRecord.id }),
                workspace?.remote ?? '',
              )}
            >
              {`History for ${keyRecord.name}`}
            </Link>
            {keyRecord.classification === 'config' && sourceSet ? (
              <button
                type="button"
                className="btn"
                aria-expanded={copyOpen}
                onClick={() => setCopyOpen((open) => !open)}
              >
                {`Copy published ${environment.name} value to…`}
              </button>
            ) : null}
          </div>

          {copyOpen ? (
            <fieldset className="matrix-editor__copy">
              <legend>Copy independent published value to</legend>
              {rows
                .filter((row) => row.environmentId !== environmentId && !row.degraded)
                .map((row) => (
                  <label key={row.environmentId}>
                    <input
                      type="checkbox"
                      checked={destinations.includes(row.environmentId)}
                      onChange={() => {
                        setDestinations((current) =>
                          current.includes(row.environmentId)
                            ? current.filter((id) => id !== row.environmentId)
                            : [...current, row.environmentId],
                        );
                        setProtectedCopyConfirmed(false);
                      }}
                    />
                    <span>{row.environment.name}{row.protected ? ' · protected' : ''}</span>
                  </label>
                ))}
              {protectedConfirmationRequired ? (
                <label className="matrix-editor__protected-confirmation">
                  <input
                    type="checkbox"
                    checked={protectedCopyConfirmed}
                    onChange={(event) => setProtectedCopyConfirmed(event.target.checked)}
                  />
                  <span>I confirm copying into protected {protectedDestinationNames.join(', ')}.</span>
                </label>
              ) : null}
              {protectedGuard.error === null ? null : (
                <p className="alert" role="alert">
                  <span className="alert__glyph" aria-hidden="true">!</span>
                  <span>{protectedGuard.error}</span>
                </p>
              )}
              <p>Each copied value is independent; later source edits do not propagate.</p>
              <button
                type="button"
                className="btn"
                disabled={destinations.length === 0 || busy || applying || (protectedConfirmationRequired && !protectedCopyConfirmed)}
                onClick={() => {
                  void protectedGuard.run(
                    protectedTargets(),
                    () => onCopy(destinations, protectedConfirmationRequired),
                    'The protected destination guard could not be read, so nothing was copied',
                  );
                }}
              >
                {`Copy to ${String(destinations.length)} environment${destinations.length === 1 ? '' : 's'}`}
              </button>
            </fieldset>
          ) : null}
        </form>
      </dialog>
      {protectedGuard.request === null ? null : (
        <Ceremony
          key={protectedGuard.requestKey}
          request={protectedGuard.request}
          onAuthorised={protectedGuard.onAuthorised}
          onCancel={protectedGuard.onCancel}
        />
      )}
      {disclosure.ceremony.request === null ? null : (
        <Ceremony
          key={disclosure.ceremony.requestKey}
          request={disclosure.ceremony.request}
          onAuthorised={disclosure.ceremony.onAuthorised}
          onCancel={disclosure.ceremony.onCancel}
        />
      )}
    </>
  );
}

function useCellDisclosure(
  refData: MatrixRef,
  keyRecord: MatrixKey,
  sourceRow: EditorRow,
) {
  const env = { ...refData, environment: sourceRow.environmentId };
  const transport = useTransport();
  const window = useRevealWindow(env, keyRecord.classification === 'secret');
  const revealOne = useRevealOne(env);
  const ceremony = useCeremonyTask([
    refData.org,
    refData.project,
    sourceRow.environmentId,
    keyRecord.id,
  ]);
  const [plaintext, setPlaintext] = useSensitiveState<{ value: string; until: number } | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (plaintext === null) return;
    const timer = globalThis.setInterval(() => setNow(Date.now()), 250);
    return () => globalThis.clearInterval(timer);
  }, [plaintext]);

  useEffect(() => {
    if (plaintext !== null && plaintext.until <= now) setPlaintext(null);
  }, [now, plaintext]);

  useEffect(() => {
    setPlaintext(null);
    setError(null);
    setNotice(null);
  }, [keyRecord.id, sourceRow.environmentId]);

  const clipboardMessage = useCallback(
    (value: string, audited: boolean) => writeExpiringClipboard(value, audited),
    [],
  );

  const withDisclosure = useCallback(async (
    purpose: CeremonyPurpose,
    act: (task: CeremonyTask) => Promise<void>,
  ) => {
    const task = ceremony.begin([purpose, sourceRow.environmentId, keyRecord.id]);
    setError(null);
    setNotice(null);
    let state;
    try {
      state = await fetchRevealWindow(env, transport.client, task.signal);
    } catch {
      if (ceremony.commit(task, () => setError('The reveal window could not be read, so nothing was disclosed.'))) {
        ceremony.finish(task);
      }
      return;
    }
    if (!ceremony.isCurrent(task)) return;
    if (state.live && !state.single_decision) {
      await act(task);
      return;
    }
    ceremony.stage(task, {
      purpose,
      environmentId: sourceRow.environmentId,
      environmentName: sourceRow.environment.name,
      keys: [{ id: keyRecord.id, name: keyRecord.name }],
      window: state,
    }, () => void act(task));
  }, [ceremony, env, keyRecord.id, keyRecord.name, sourceRow.environment.name, sourceRow.environmentId, transport.client]);

  const discloseValue = useCallback(async (
    task: CeremonyTask,
    onValue: (value: string) => Promise<void> | void,
  ) => {
    try {
      const fresh = await revealOne.mutateAsync(keyRecord.name);
      if (fresh.value === undefined) {
        ceremony.commit(task, () => setError('The server disclosed no value for that key.'));
        return;
      }
      if (ceremony.isCurrent(task)) await onValue(fresh.value);
    } catch (caught) {
      ceremony.commit(task, () => {
        setPlaintext(null);
        setError(disclosureRefusalText(caught));
      });
    } finally {
      ceremony.finish(task);
    }
  }, [ceremony, keyRecord.name, revealOne]);

  const reveal = useCallback(() => {
    void withDisclosure('reveal', (task) => discloseValue(task, (value) => {
      ceremony.commit(task, () => {
        setPlaintext({ value, until: Date.now() + REMASK_MS });
        setNotice('Disclosure recorded.');
      });
    }));
  }, [ceremony, discloseValue, withDisclosure]);

  const copy = useCallback(() => {
    if (keyRecord.classification === 'config') {
      void clipboardMessage(sourceRow.cell?.value ?? '', false).then(setNotice);
      return;
    }
    void withDisclosure('clipboard', (task) => discloseValue(task, async (value) => {
      const message = await clipboardMessage(value, true);
      ceremony.commit(task, () => setNotice(message));
    }));
  }, [ceremony, clipboardMessage, discloseValue, keyRecord.classification, sourceRow.cell?.value, withDisclosure]);

  return {
    canReveal: window.data?.can_reveal !== false,
    ceremony,
    copy,
    error,
    notice,
    reveal,
    revealed: plaintext === null ? null : {
      value: plaintext.value,
      remaining: Math.max(0, Math.ceil((plaintext.until - now) / 1000)),
    },
  };
}

function validateDeclaration(
  keyRecord: MatrixKey,
  value: string,
): ReturnType<typeof validateMatrixDraft> {
  const rules = keyRecord.declaration.rule === undefined
    ? keyRecord.declaration.any_of ?? []
    : [keyRecord.declaration.rule];
  const errors = rules.map((rule) => validateMatrixDraft(rule, value));
  if (errors.some((error) => error === null)) return null;
  return errors.find((error) => error?.level === 'notice') ?? errors[0] ?? {
    level: 'notice',
    message: 'Full declaration validation runs when publishing.',
  };
}

function formatTimestamp(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  );
}
