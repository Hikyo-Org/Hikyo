import { useCallback, useEffect, useMemo, useState } from 'react';
import { generatePath, Link } from 'react-router';

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
import { surfaceById } from '../app/navigation.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { ChoiceGroup } from '../ui/ChoiceGroup.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Field } from '../ui/Field.tsx';
import { Glyph } from '../ui/Glyph.tsx';
import { Ceremony, type CeremonyPurpose } from './Ceremony.tsx';
import {
  canClearMatrixCell,
  copyRequiresProtectedConfirmation,
  draftValueForMatrixCell,
  matrixDraftChanges,
  validateMatrixDeclaration,
  type MatrixDraftChange,
  type MatrixDraftEdit,
  type MatrixDraftRule,
  type MatrixDraftValidation,
} from './matrix-state.ts';
import {
  useProtectedPublishCeremony,
  type ProtectedPublishTarget,
} from './useProtectedPublishCeremony.ts';
import { useCeremonyTask, type CeremonyTask } from './useCeremonyTask.ts';

type MatrixKey = MatrixKeyList['items'][number];
type Environment = EnvironmentList['items'][number];

type EditorRow = {
  readonly environmentId: string;
  readonly environment: Environment;
  readonly protected: boolean;
  // #451: this column's value/signal reads failed or were denied. It is excluded
  // from bulk edit and copy destinations, the caller cannot read it, so writing
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
  // Secret inputs are masked while typing (a shoulder-surfer reads a textarea as
  // easily as a screen); this is the operator's own opt-out.
  const [showTyping, setShowTyping] = useState(false);
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
  const copyDestinations = rows.filter(
    (row) => row.environmentId !== environmentId && !row.degraded,
  );
  const protectedDestinationNames = rows
    .filter(
      (row) =>
        destinations.includes(row.environmentId) &&
        protectedEnvironmentIds.includes(row.environmentId),
    )
    .map((row) => row.environment.name);

  const secret = keyRecord.classification === 'secret';
  const valueClass = `mono matrix-editor__value${secret && !showTyping ? ' matrix-editor__value--masked' : ''}`;
  const declarationHref = withRemote(
    generatePath(surfaceById('key-detail').path, { ...refData, key: keyRecord.id }),
    workspace?.remote ?? '',
  );
  const validationByEnvironment = new Map<string, MatrixDraftValidation>();
  for (const row of rows) {
    const edit = edits.get(row.environmentId);
    if (edit?.op !== 'set') {
      continue;
    }
    const error = validateMatrixDeclaration(declarationRules(keyRecord), edit.value);
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
          keys: [{ id: keyRecord.id, name: keyRecord.name, classification: keyRecord.classification }],
        };
      });

  return (
    <>
      <Dialog
        title={keyRecord.name}
        lede={keyRecord.description || 'Explicit value and provenance for this environment.'}
        size="wide"
        onCancel={(event) => {
          event.preventDefault();
          onClose();
        }}
        onBackdropClick={onClose}
      >
        <form
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
          {/* The eyebrow follows the title now: the atom's h2 is always first,
              and it already names the classification the lock glyph stood for. */}
          <p className="matrix-editor__eyebrow">
            {`${environment.name} · ${keyRecord.classification}`}
          </p>

          {secret ? (
            <Checkbox
              className="matrix-editor__show-typing"
              label="Show while typing"
              checked={showTyping}
              onChange={(event) => setShowTyping(event.target.checked)}
            />
          ) : null}
          {secret && disclosure.revealDenied ? (
            <p className="matrix-editor__hint">
              No reveal for your role here. Write-only replacement only; copying is a disclosure
              too, so it stays gated with reveal.
            </p>
          ) : null}

          {editAll ? (
            <Field
              className="matrix-row-editor__fill"
              label="Fill all environments"
              id="matrix-fill-all"
            >
              {(control) => (
                <div>
                  {/* markup-check: a raw textarea inside the atom's field, not an
                      Input: the secret face is `-webkit-text-security` on a
                      textarea so pasted newlines survive, which no Input type
                      expresses. Field still owns the label and the wiring. */}
                  <textarea
                    {...control}
                    className={valueClass}
                    rows={2}
                    autoComplete="off"
                    value={fillAll}
                    placeholder={secret ? 'Write-only replacement' : 'Shared draft value'}
                    onChange={(event) => setFillAll(event.target.value)}
                  />
                  <Button
                    type="button"
                    disabled={fillAll === '' || busy || applying}
                    onClick={() => {
                      setEdits(new Map<string, MatrixDraftEdit>(
                        editableRows.map((row) => [row.environmentId, { op: 'set', value: fillAll }]),
                      ));
                      setFillAll('');
                    }}
                  >
                    Fill all
                  </Button>
                </div>
              )}
            </Field>
          ) : null}

          <div className="matrix-row-editor__rows">
            {visibleRows.map((row) => {
              const rowEnvironmentId = row.environmentId;
              const publishedSet = row.cell?.set === true;
              const edit = edits.get(rowEnvironmentId);
              const clearing = edit?.op === 'unset';
              const liveValidation = validationByEnvironment.get(rowEnvironmentId) ?? null;
              // One refusal fits the control's error slot. A live validation
              // error owns it (it is about what is being typed); a lone standing
              // problem takes it when nothing is being refused; several problems
              // stay a list above the control, because a field error is one
              // sentence, not an enumeration.
              const messages = row.problems.map((problem) => problem.message);
              const liveError = liveValidation?.level === 'error' ? liveValidation.message : undefined;
              const loneProblem = liveError === undefined && messages.length === 1 ? messages[0] : undefined;
              const listedProblems = loneProblem === undefined ? messages : [];
              return (
                <section
                  className={`matrix-row-editor__row${row.protected ? ' matrix-row-editor__row--protected' : ''}`}
                  key={rowEnvironmentId}
                  aria-labelledby={`matrix-row-${rowEnvironmentId}`}
                >
                  <div className="matrix-row-editor__row-head">
                    <h3 id={`matrix-row-${rowEnvironmentId}`}>{row.environment.name}</h3>
                    {row.protected ? <span>PROTECTED</span> : null}
                    <span>{publishedSet ? 'set' : '· absent'}</span>
                    {row.signal?.pending === undefined ? null : (
                      <span>
                        <Glyph name="delta" />{' '}
                        {`pending ${row.signal.pending.operation === 'unset' ? 'clear' : 'set'}`}
                      </span>
                    )}
                  </div>
                  {listedProblems.length === 0 ? null : (
                    <Alert tone="warn">
                      <ul className="matrix-row-editor__problems">
                        {listedProblems.map((message) => (
                          <li key={message}>{message}</li>
                        ))}
                      </ul>
                    </Alert>
                  )}
                  <Field
                    label={`${row.environment.name} value`}
                    id={`matrix-edit-${rowEnvironmentId}`}
                    hint={
                      liveValidation !== null && liveValidation.level !== 'error'
                        ? liveValidation.message
                        : undefined
                    }
                    error={liveError ?? loneProblem}
                  >
                    {/* markup-check: a raw textarea inside the atom's field, not
                        an Input: the secret face is `-webkit-text-security` on a
                        textarea so pasted newlines survive, which no Input type
                        expresses. Field owns the label, the hint, the error and
                        the aria wiring the row used to hand-write. */}
                    {(control) => (
                      <textarea
                        {...control}
                        className={valueClass}
                        rows={2}
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
                        onChange={(event) => {
                          setEdits((current) => {
                            const next = new Map(current);
                            next.set(rowEnvironmentId, { op: 'set', value: event.target.value });
                            return next;
                          });
                        }}
                      />
                    )}
                  </Field>
                  <dl className="matrix-editor__provenance">
                    <div>
                      <dt>Updated</dt>
                      <dd>{row.cell?.updated_at === undefined ? 'No published value' : formatTimestamp(row.cell.updated_at)}</dd>
                    </div>
                    <div><dt>Updated by</dt><dd className="mono">{row.cell?.updated_by ?? 'unknown'}</dd></div>
                    <div>
                      <dt>Revision</dt>
                      <dd>{row.signal?.changed_in_revision === undefined ? 'No change signal' : `r${String(row.signal.changed_in_revision)}`}</dd>
                    </div>
                  </dl>
                  {rowEnvironmentId === environmentId && disclosure.revealed !== null ? (
                    // No aria-label here: it would REPLACE the plaintext as the
                    // accessible name, hiding the disclosed value from AT. The
                    // announcement lives in the status region below.
                    <p className="matrix-editor__revealed mono">
                      <span>{disclosure.revealed.value}</span>
                      <small aria-hidden="true">{`re-masks in ${String(disclosure.revealed.remaining)}s`}</small>
                    </p>
                  ) : null}
                  {editAll ? null : (
                  <Button
                    type="button"
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
                  </Button>
                  )}
                </section>
              );
            })}
          </div>

          {applyError === null ? null : <Alert>{applyError}</Alert>}
          {mutationError === null ? null : <Alert>{mutationError}</Alert>}
          {disclosure.error === null ? null : <Alert>{disclosure.error}</Alert>}
          {disclosure.notice === null ? null : <Alert tone="done">{disclosure.notice}</Alert>}
          <p className="visually-hidden" role="status">
            {disclosure.announcement === null ? null : (
              <span key={disclosure.announcement.id}>{disclosure.announcement.message}</span>
            )}
          </p>

          {copyOpen ? (
            <div className="matrix-editor__copy">
              <ChoiceGroup
                legend="Copy independent published value to"
                columns={copyDestinations.length}
                hint="Each copied value is independent; later source edits do not propagate."
              >
                {copyDestinations.map((row) => (
                  <Checkbox
                    key={row.environmentId}
                    label={`${row.environment.name}${row.protected ? ' · protected' : ''}`}
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
                ))}
              </ChoiceGroup>
              {protectedConfirmationRequired ? (
                <Checkbox
                  label={`I confirm copying into protected ${protectedDestinationNames.join(', ')}.`}
                  checked={protectedCopyConfirmed}
                  onChange={(event) => setProtectedCopyConfirmed(event.target.checked)}
                />
              ) : null}
              {protectedGuard.error === null ? null : (
                <Alert>{protectedGuard.error}</Alert>
              )}
              <Button
                type="button"
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
              </Button>
            </div>
          ) : null}

          <details className="matrix-editor__schema">
            <summary>Schema and presence rules</summary>
            <p>{declarationSummary(keyRecord, rows)}</p>
            <details>
              <summary>Raw declaration</summary>
              <pre className="mono">{JSON.stringify(
                { declaration: keyRecord.declaration, presence: keyRecord.presence },
                // Integer bounds arrive as bigint, which JSON.stringify refuses.
                (_key, value: unknown) => (typeof value === 'bigint' ? value.toString() : value),
                2,
              )}</pre>
            </details>
          </details>

          {/* One action row, and it is last: everything the dialog asks for sits
              above it, the way ui/Dialog's own anatomy reads. */}
          <div className="dialog__actions">
            {/* The close X is gone (ui/Dialog): Escape and this button are the
                two ways out, and it leads the row the way Cancel does. */}
            <Button type="button" onClick={onClose}>
              Close
            </Button>
            {rows.length > 1 ? (
              <Button
                type="button"
                aria-expanded={editAll}
                onClick={() => {
                  if (editAll) {
                    // Leaving the all-environments view drops the edits it
                    // alone could show; the save count must match what is on
                    // screen.
                    setEdits((current) => {
                      const kept = current.get(environmentId);
                      return kept === undefined ? new Map() : new Map([[environmentId, kept]]);
                    });
                    setFillAll('');
                  }
                  setEditAll(!editAll);
                }}
              >
                {editAll ? `Back to ${environment.name} only` : 'Edit all environments'}
              </Button>
            ) : null}
            {sourceSet && secret && disclosure.canReveal ? (
              <Button type="button" onClick={disclosure.reveal}>
                {`Reveal ${keyRecord.name}`}
              </Button>
            ) : null}
            {sourceSet && (!secret || disclosure.canReveal) ? (
              <Button type="button" onClick={disclosure.copy}>
                {secret ? `Copy ${keyRecord.name} (audited disclosure)` : `Copy ${keyRecord.name}`}
              </Button>
            ) : null}
            <Link className="btn" to={declarationHref} onClick={onClose}>
              Edit declaration
            </Link>
            {/*
              The per-key history entry point. Per-key history is a FILTER over
              the same lineage, so it is the history surface with `key` set, 
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
              <Button
                type="button"
                aria-expanded={copyOpen}
                onClick={() => setCopyOpen((open) => !open)}
              >
                {`Copy published ${environment.name} value to…`}
              </Button>
            ) : null}
            <Button
              type="submit"
              variant="primary"
              disabled={changes.length === 0 || busy || applying}
            >
              {busy || applying ? 'Saving drafts…' : `Save ${String(changes.length)} draft${changes.length === 1 ? '' : 's'}`}
            </Button>
          </div>

        </form>
      </Dialog>
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
  // Announced ONCE per reveal, with the window's full length: a status region
  // that ticked with the countdown would read the value aloud every 250ms.
  const [announcement, setAnnouncement] = useState<{ id: number; message: string } | null>(null);

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
    setAnnouncement(null);
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
      keys: [{ id: keyRecord.id, name: keyRecord.name, classification: keyRecord.classification }],
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
        setAnnouncement((current) => ({
          id: (current?.id ?? 0) + 1,
          message: `${keyRecord.name} revealed, re-masks in ${String(REMASK_MS / 1000)}s`,
        }));
      });
    }));
  }, [ceremony, discloseValue, keyRecord.name, withDisclosure]);

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
    // Fail closed: no guard answer means no reveal, and no copy of a secret.
    canReveal: window.data?.can_reveal === true,
    // Only once the guard has answered: a "no reveal for your role" line shown
    // while the read is in flight would lie for a second on every open.
    revealDenied: window.data !== undefined && !window.data.can_reveal,
    announcement,
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

function declarationRules(keyRecord: MatrixKey): readonly MatrixDraftRule[] {
  return keyRecord.declaration.rule === undefined
    ? keyRecord.declaration.any_of ?? []
    : [keyRecord.declaration.rule];
}

/** One human line for the declaration: `string · pattern · range · required in production`. */
function declarationSummary(keyRecord: MatrixKey, rows: readonly EditorRow[]): string {
  const rules = declarationRules(keyRecord);
  const parts: string[] = [rules.map((rule) => rule.type).join(' | ') || 'no rule'];
  const push = (part: string) => {
    if (!parts.includes(part)) parts.push(part);
  };
  for (const rule of rules) {
    if (rule.pattern !== undefined) push('pattern');
    if ([rule.min, rule.max, rule.min_length, rule.max_length].some((bound) => bound !== undefined)) {
      push('range');
    }
    if (rule.members !== undefined) push('members');
    if (rule.schemes !== undefined) push('schemes');
    if (rule.json_schema !== undefined) push('schema');
  }
  const names = (ids: readonly string[] | undefined) =>
    rows
      .filter((row) => ids?.includes(row.environmentId) === true)
      .map((row) => row.environment.name)
      .join(', ');
  const presences: readonly [string, MatrixKey['presence']['required_in']][] = [
    ['required', keyRecord.presence.required_in],
    ['forbidden', keyRecord.presence.forbidden_in],
  ];
  for (const [label, presence] of presences) {
    if (presence.mode === 'all') push(`${label} everywhere`);
    else if (presence.mode === 'explicit') push(`${label} in ${names(presence.environment_ids)}`);
  }
  return parts.join(' · ');
}

function formatTimestamp(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  );
}
