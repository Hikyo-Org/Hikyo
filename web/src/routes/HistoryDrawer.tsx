import { useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react';
import type { RetentionConsequence } from '@hikyo/client';
import { exportValuesOp } from '@hikyo/operations';
import { zExportedValues } from '@hikyo/zod';
import { useMutation } from '@tanstack/react-query';
import { generatePath, Link, useNavigate, useSearchParams } from 'react-router';

import {
  callerSafeRefusal,
  historyRefusalText,
  revisionNumber,
  useReleaseRevisionPin,
  useRestoreRevision,
  useRevisionDetail,
  useRevisionHistory,
  useRevisionPins,
  useProjectRetention,
  useSetRevisionPin,
  type HistoryRevisionItem,
  type RestoreResult,
  type RevisionPinItem,
} from '../api/history.ts';
import { useServiceAccounts } from '../api/identities.ts';
import { ApiError, parsed } from '../api/client.ts';
import { useTransport, useWorkspaceContext, withRemote } from '../api/transport.tsx';
import {
  matrixMutationError,
  rememberRestorePreview,
  restorePreviewWasAttached,
  usePublishMatrix,
  type MatrixKeyList,
} from '../api/matrix.ts';
import type { EnvRef, MatrixRef } from '../api/keys.ts';
import type { EnvironmentList, ValueCell } from '../api/values.ts';
import { surfaceById } from '../app/navigation.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Glyph } from '../ui/Glyph.tsx';
import { Ceremony } from './Ceremony.tsx';
import {
  defaultPinExpiry,
  historyKeyDisplay,
  impactHeading,
  pinAction,
  pinCeremonyUnit,
  pinExpiry,
  pinExpiryDateBounds,
  pinExpiryInstant,
  pinComparedToLatest,
  pinSchemaOverrideOffered,
  relativeAge,
  restoreCeremonyUnit,
  restoreKeyName,
  restorePreviewSummary,
  restorePublishLabel,
  retentionLine,
  revisionActionGate,
  revisionsForKeyFilter,
  toHistoryRetention,
  workloadLabel,
  type CeremonyKey,
  type HistoryCurrentCell,
  type HistoryImpactChange,
  type HistoryImpactEnvironment,
  type HistoryPin,
  type HistoryRevision,
  type HistorySnapshotKey,
  type PinComparison,
  type RevisionActionGate,
} from './history-state.ts';
import { useProtectedPublishCeremony } from './useProtectedPublishCeremony.ts';
import { RevisionDiffDialog } from './RevisionDiff.tsx';

type Environment = EnvironmentList['items'][number];
type MatrixKey = MatrixKeyList['items'][number];

const zPinComparisonValues = zExportedValues.superRefine((values, context) => {
  values.items.forEach((value, index) => {
    if (value.classification === 'secret' && (value.revealed || value.value !== undefined)) {
      context.addIssue({
        code: 'custom',
        path: ['items', index],
        message: 'pin comparison secret values must remain write-presence only',
      });
    }
    if (value.classification === 'config' && (!value.revealed || value.value === undefined)) {
      context.addIssue({
        code: 'custom',
        path: ['items', index],
        message: 'pin comparison config values must carry plaintext',
      });
    }
  });
});

/**
 * The revision-history drawer (#59, locked prototype `revision-history/6`).
 *
 * The shape is iteration 2's verdict **b**: a slim revision list beside a
 * detail pane, rendered over the matrix rather than instead of it. Below the
 * chrome's 800px breakpoint the two panes become one, list, then detail, with
 * a back affordance, because a 440px drawer split in two is neither.
 *
 * Three properties are the surface's whole point and none of them is decoration:
 *
 *  - **Lineage outlives its payload.** A collected revision keeps its row, its
 *    actor and its changed keys, gains a `payload collected` tag, and loses
 *    restore and pin with the stamped policy named. Nothing is reconstructed.
 *  - **Secrets are write-presence only.** The changed-key list says added /
 *    edited / removed and marks the key as secret. No value, no length, no
 *    digest, no
 *    comparison status reaches this surface for a secret, ever.
 *  - **Restore is not a privileged path.** It stages ordinary drafts; the
 *    matrix's own draft dots appear and the ordinary publish sheet commits
 *    them, carrying the preview token that binds them.
 *
 */
export function HistoryDrawer({
  refData,
  environments,
  keys,
  currentRevisions,
  protectedEnvironmentIds,
  cellsByEnvironment,
  pendingByEnvironment,
  pendingByOthersByEnvironment,
  currentValuesByEnvironment,
  openerRef,
}: {
  refData: MatrixRef;
  environments: readonly Environment[];
  keys: readonly MatrixKey[];
  currentRevisions: ReadonlyMap<string, bigint>;
  protectedEnvironmentIds: readonly string[];
  cellsByEnvironment: ReadonlyMap<string, readonly HistoryCurrentCell[]>;
  pendingByEnvironment: ReadonlyMap<string, number>;
  pendingByOthersByEnvironment: ReadonlyMap<string, number>;
  currentValuesByEnvironment: ReadonlyMap<string, readonly ValueCell[]>;
  openerRef: RefObject<HTMLAnchorElement | null>;
}) {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const fallbackEnvironment = environments[0]?.id ?? '';
  const requested = params.get('env') ?? '';
  const environmentId = environments.some((candidate) => candidate.id === requested)
    ? requested
    : fallbackEnvironment;
  const environment = environments.find((candidate) => candidate.id === environmentId);
  const keyFilter = params.get('key');
  const env = { ...refData, environment: environmentId };

  const transport = useTransport();
  const workspace = useWorkspaceContext();
  const history = useRevisionHistory(env);
  const pins = useRevisionPins(env);
  const retention = useProjectRetention(refData);
  const accounts = useServiceAccounts(refData);
  const restore = useRestoreRevision(env);
  const setPin = useSetRevisionPin(env);
  const releasePin = useReleaseRevisionPin(env);
  const publish = usePublishMatrix(refData);
  const comparePin = useMutation({
    mutationFn: async (input: { readonly environmentId: string; readonly revision: bigint }) =>
      zPinComparisonValues.parse(
        await parsed(exportValuesOp, {
          path: { ...refData, environment: input.environmentId },
          body: { revision: revisionNumber(input.revision), reveal: false },
          ...transport,
        }),
      ),
  });
  const guard = useProtectedPublishCeremony(
    refData,
    [environmentId, params.toString()],
  );

  const [sheet, setSheet] = useState<Sheet | null>(null);
  const [outcome, setOutcome] = useState<ReactNode>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  // A deep link with `rev` lands on the detail pane on a phone, not on the list.
  const [mobileDetail, setMobileDetail] = useState(() => params.get('rev') !== null);
  const drawer = useRef<HTMLElement>(null);
  const drawerHeading = useRef<HTMLHeadingElement>(null);
  const detailHeading = useRef<HTMLHeadingElement>(null);
  const selectedRow = useRef<HTMLButtonElement>(null);
  const currentEnvironment = useRef(environmentId);
  currentEnvironment.current = environmentId;

  // The clock is read once per mount rather than per row: a timeline
  // whose rows disagree about "now" by a few milliseconds can render two
  // different expiry tiers for the same instant.
  const [now] = useState(() => new Date());
  const expiryBounds = pinExpiryDateBounds(now);

  const revisions = useMemo<readonly HistoryRevision[]>(
    () => (history.data?.items ?? []).map(toHistoryRevision),
    [history.data],
  );
  const filtered = useMemo(
    () => revisionsForKeyFilter(revisions, keyFilter),
    [keyFilter, revisions],
  );
  const currentRevision = currentRevisions.get(environmentId) ?? 0n;
  const requestedRevision = params.get('rev');
  const selected =
    filtered.find((entry) => String(entry.revision) === requestedRevision) ?? filtered[0];
  const keyDisplay =
    keyFilter === null || !history.isSuccess
      ? null
      : historyKeyDisplay(keyFilter, keys, selected);

  const pinRows = useMemo<readonly HistoryPin[]>(
    () => (pins.data?.items ?? []).map(toHistoryPin),
    [pins.data],
  );
  const workloads = (accounts.data?.items ?? []).filter((account) => account.kind === 'workload');
  // A pin binds a WORKLOAD, and a workload does resolve to a name, through the
  // project's service accounts. (A human publisher does not: nothing in this
  // API maps a human principal id to a display name, so those stay ids.)
  const workloadNames = new Map(
    (accounts.data?.items ?? []).map((account) => [account.principal_id, account.name]),
  );
  const selectedGate =
    selected === undefined ? null : revisionActionGate(selected, currentRevision);

  const setParam = (name: string, value: string | null) => {
    const next = new URLSearchParams(params);
    if (value === null) {
      next.delete(name);
    } else {
      next.set(name, value);
    }
    setParams(next, { replace: true });
  };

  // Back into the matrix, keeping the workspace: closing the drawer inside a
  // workspace must land on the remote's matrix, not this instance's (#71).
  const matrixPath = withRemote(
    generatePath(surfaceById('matrix').path, refData),
    workspace?.remote ?? '',
  );
  // The retention knob lives on the project-settings Policy panel (`#project-policy`).
  const policyPath = `${withRemote(
    generatePath(surfaceById('project-settings').path, refData),
    workspace?.remote ?? '',
  )}#project-policy`;
  const environmentNames = new Map(environments.map((candidate) => [candidate.id, candidate.name]));

  useEffect(() => {
    setSheet(null);
    setOutcome(null);
    setRefusal(null);
  }, [environmentId, keyFilter]);

  useEffect(() => {
    // Snapshot the opener at MOUNT: the matrix stays interactive behind a desktop
    // drawer, so a later click can re-point the shared ref, and close must return
    // focus to the element that actually opened this drawer, not the last one
    // touched. The current-environment link is the fallback when that element
    // is gone (an environment hidden, a re-render that replaced the header).
    const opener = openerRef.current;
    drawerHeading.current?.focus();
    return () => {
      requestAnimationFrame(() => {
        if (opener?.isConnected === true) {
          opener.focus();
          return;
        }
        Array.from(document.querySelectorAll<HTMLAnchorElement>('.matrix__history-link'))
          .find((link) => link.dataset['historyEnvironment'] === currentEnvironment.current)
          ?.focus();
      });
    };
  }, [openerRef]);

  useEffect(() => {
    if (mobileDetail) {
      detailHeading.current?.focus();
    }
  }, [mobileDetail, selected?.revision]);

  // A pointer-down outside the drawer closes it back to the matrix, the same way
  // the Close link and Escape do. Guarded on an open dialog: the restore/pin/
  // release sheets are native `<dialog>`s rendered as SIBLINGS of the drawer, so
  // a click inside one lands outside the aside — the guard keeps that from also
  // collapsing the drawer, leaving each sheet its own dismissal.
  useEffect(() => {
    const closeOutside = (event: PointerEvent) => {
      if (
        event.target instanceof Node &&
        drawer.current?.contains(event.target) !== true &&
        document.querySelector('dialog[open]') === null
      ) {
        void navigate(matrixPath);
      }
    };
    document.addEventListener('pointerdown', closeOutside);
    return () => document.removeEventListener('pointerdown', closeOutside);
  }, [navigate, matrixPath]);

  if (environment === undefined) {
    return null;
  }

  const secretByKeyId = new Map(keys.map((key) => [key.id, key.classification === 'secret']));

  /** Runs the disclosure ceremony for `unit` when the guard needs one, then acts. */
  const withCeremony = (
    unit: readonly CeremonyKey[],
    purpose: 'restore' | 'pin',
    act: () => void,
  ) => {
    setRefusal(null);
    if (unit.length === 0) {
      act();
      return;
    }
    void guard.run(
      [
        {
          environmentId: environment.id,
          environmentName: environment.name,
          keys: [...unit],
          purpose,
        },
      ],
      act,
      'The disclosure guard could not be read, so nothing was staged',
    );
  };

  const runRestore = (revision: bigint, keyId: string | null, keyName: string | null) => {
    const unit = restoreCeremonyUnit({
      revisionKeys: sheetRevisionKeys(sheet),
      currentCells: cellsByEnvironment.get(environment.id) ?? [],
      keyId,
    });
    withCeremony(unit, 'restore', () => {
      restore.mutate(
        keyName === null ? { revision } : { revision, key: keyName },
        {
          onSuccess: (result) => {
            rememberRestorePreview(
              refData,
              result.changes.map((change) => change.version_id),
              result.preview.token,
            );
            setSheet({
              kind: 'restore',
              revision,
              keyId,
              keyName,
              keys: sheetRevisionKeys(sheet),
              result: {
                changes: result.changes,
                preview: { environments: result.preview.environments },
              },
            });
            setOutcome(
              `Staged ${String(result.changes.length)} draft${result.changes.length === 1 ? '' : 's'} from r${String(revision)}. Nothing is published yet.`,
            );
          },
          onError: (error) =>
            setRefusal(historyMutationRefusal(error, 'restore', unit, environment.name)),
        },
      );
    });
  };

  const runPin = (input: {
    readonly revision: bigint;
    readonly workloadPrincipalID: string;
    readonly expiresAt: string;
    readonly overrideSchema: boolean;
    readonly revisionKeys: readonly HistorySnapshotKey[];
  }) => {
    let expiresAt: string;
    try {
      expiresAt = pinExpiryInstant(input.expiresAt);
    } catch (error) {
      if (!(error instanceof Error)) {
        throw error;
      }
      setRefusal(error.message);
      return;
    }
    const historical = input.revision !== currentRevision;
    const unit = historical
      ? pinCeremonyUnit(input.revisionKeys, cellsByEnvironment.get(environment.id) ?? [])
      : [];
    withCeremony(unit, 'pin', () => {
      setPin.mutate(
        {
          workloadPrincipalID: input.workloadPrincipalID,
          revision: input.revision,
          expiresAt,
          overrideSchema: input.overrideSchema,
        },
        {
          onSuccess: (result) => {
            setSheet(null);
            // The verb is the SERVER's (`RevisionPinResult.action`), not the
            // label the sheet guessed: created, reassigned and renewed are the
            // locked taxonomy, and the outcome reports what happened.
            setOutcome(
              `Pin ${result.action}: r${String(result.pin.revision)} for ${
                workloadLabel(result.pin.workload_principal_id, workloadNames)
              }, expiring ${result.pin.expires_at.slice(0, 10)}.`,
            );
          },
          onError: (error) => {
            setRefusal(historyMutationRefusal(error, 'pin', unit, environment.name));
            setSheet((current) =>
              current !== null && current.kind === 'pin'
                ? {
                    ...current,
                    offerOverride: pinSchemaOverrideOffered(
                      error instanceof ApiError ? error.detail : undefined,
                    ),
                  }
                : current,
            );
          },
        },
      );
    });
  };

  const publishRestore = (revision: bigint, result: RestoreSheetResult) => {
    const versionIds = result.changes.map((change) => change.version_id);
    const act = () => {
      setRefusal(null);
      publish.mutate(
        {
          addressedEnvironment: environment.id,
          environmentIds: [environment.id],
          versionIds,
        },
        {
          onSuccess: (published) => {
            setSheet(null);
            // Secret-change approvals (#151): a covered environment stages the
            // restore for review rather than publishing it.
            if (!('environments' in published)) {
              setOutcome(
                `Submitted the restore from r${String(revision)} for approval in ${environment.name}. ` +
                  'Track it under Change approvals.',
              );
              return;
            }
            const target = published.environments.find(
              (entry) => entry.environment_id === environment.id,
            );
            if (target === undefined) {
              throw new Error(`restore publish returned no revision for ${environment.id}`);
            }
            setOutcome(
              `Published the restore from r${String(revision)} as ${environment.name} r${String(target.revision)}.`,
            );
          },
          onError: (error) =>
            setRefusal(
              matrixMutationError(error, 'publish', restorePreviewWasAttached(error)),
            ),
        },
      );
    };
    if (!protectedEnvironmentIds.includes(environment.id)) {
      act();
      return;
    }
    void guard.run(
      [
        {
          environmentId: environment.id,
          environmentName: environment.name,
          keys: result.changes.map((change) => ({ id: change.key_id, name: change.name })),
          purpose: 'publish',
        },
      ],
      act,
      'The protected publish guard could not be read, so nothing was published',
    );
  };

  const runRelease = (pin: HistoryPin) => {
    setRefusal(null);
    releasePin.mutate(pin.workloadPrincipalId, {
      onSuccess: (result) => {
        setSheet(null);
        setOutcome(
          <PinReleaseOutcome
            consequence={result.retention_consequence}
            revision={result.revision}
          />,
        );
      },
      onError: (error) => setRefusal(historyRefusalText(error, 'release')),
    });
  };

  const stagedByMe = pendingByEnvironment.get(environment.id) ?? 0;
  const stagedByOthers = pendingByOthersByEnvironment.get(environment.id) ?? 0;
  const line = retention.data === undefined
    ? null
    : retentionLine({
        inherited: retention.data.inherited,
        ...toHistoryRetention({
          mode: retention.data.mode,
          ...(retention.data.max_age_seconds == null
            ? {}
            : { max_age_seconds: retention.data.max_age_seconds }),
          ...(retention.data.last_revisions == null
            ? {}
            : { last_revisions: retention.data.last_revisions }),
        }),
      });

  return (
    <>
      <aside
        ref={drawer}
        className={`history${mobileDetail ? ' history--detail' : ''}`}
        aria-label="Revision history"
        onKeyDown={(event) => {
          if (event.key === 'Escape' && sheet === null) {
            event.preventDefault();
            void navigate(matrixPath);
          }
        }}
      >
        <div className="history__head">
          <div className="history__title">
            <h2 id="history-title" ref={drawerHeading} tabIndex={-1}>
              <span aria-hidden="true">↺ </span>
              Revision history
            </h2>
            <Badge>{`current r${String(currentRevision)}`}</Badge>
            {protectedEnvironmentIds.includes(environment.id) ? (
              <span className="history__protected">PROTECTED</span>
            ) : null}
            <Link id="history-close" className="btn history__close" to={matrixPath} aria-label="Close revision history">
              <Glyph name="cross" /> Close
            </Link>
          </div>

          {/*
            Plain toggle buttons, deliberately NOT `role="tab"`. These do not
            switch a panel inside the page: they rewrite the URL the whole
            surface is addressed by, and a tab role would promise keyboard
            semantics (arrow-key roving, an owned tabpanel) that a set of links
            to different states does not have.
          */}
          <div className="history__tabs">
            {environments.map((candidate) => (
              <Button
                key={candidate.id}
                type="button"
                className="history__tab"
                aria-pressed={candidate.id === environment.id}
                onClick={() => {
                  const next = new URLSearchParams(params);
                  next.set('env', candidate.id);
                  next.delete('rev');
                  setParams(next, { replace: true });
                  setMobileDetail(false);
                }}
              >
                {candidate.name}
              </Button>
            ))}
          </div>

          {stagedByMe === 0 && stagedByOthers === 0 ? null : (
            <p className="history__pending" role="status">
              {stagedByMe === 0
                ? ''
                : `${String(stagedByMe)} staged by you (unpublished)`}
              {stagedByMe > 0 && stagedByOthers > 0 ? ' · ' : ''}
              {/* The pending signal is a boolean per cell (`pending_by_others`), so
                  no actor names reach this surface. */}
              {stagedByOthers === 0
                ? ''
                : `${String(stagedByOthers)} change${stagedByOthers === 1 ? '' : 's'} pending by others`}
            </p>
          )}

          {line === null ? null : (
            <p className="history__retention">
              <span>{line.window}</span>
              <span className="history__badge" title={line.badgeTitle}>
                {line.badge}
              </span>
              <Link className="history__settings-pointer" to={policyPath}>
                <span aria-hidden="true">→ </span>
                change it in project settings › Policy
              </Link>
            </p>
          )}

          {keyFilter === null ? null : (
            <p className="history__filter" role="status">
              <span>
                <span aria-hidden="true">⚠ </span>
                {`filter active: history of ${keyDisplay?.label ?? keyFilter}, showing ${String(filtered.length)} of ${String(revisions.length)} revisions`}
              </span>
              <Button type="button" onClick={() => setParam('key', null)}>
                <Glyph name="cross" /> show every revision
              </Button>
            </p>
          )}
        </div>

        {outcome === null ? null : (
          <Alert tone="done">{outcome}</Alert>
        )}
        {refusal === null && guard.error === null ? null : (
          <Alert>{refusal ?? guard.error}</Alert>
        )}
        {retention.isError ? (
          <Alert>Retention policy could not be read. Pin release consequences still come from the server.</Alert>
        ) : null}

        {history.isPending ? (
          <p role="status">Loading revision history…</p>
        ) : history.isError ? (
          <Alert>The revision history could not be read. Reload to try again.</Alert>
        ) : filtered.length === 0 ? (
          <p className="history__empty" role="status">
            {keyFilter === null
              ? 'No revisions published in this environment yet.'
              : `No revision has moved ${keyDisplay?.label ?? keyFilter} in this environment.`}
          </p>
        ) : (
          <div className="history__panes">
            <ol className="history__list" aria-label="Revisions, newest first">
              {filtered.map((entry) => {
                const pinnedHere = pinRows.filter((pin) => pin.revision === entry.revision);
                return (
                  <li key={String(entry.revision)}>
                    <Button
                      data-history-revision={String(entry.revision)}
                      ref={entry.revision === selected?.revision ? selectedRow : undefined}
                      type="button"
                      className="history__row"
                      aria-current={entry.revision === selected?.revision}
                      onClick={() => {
                        setParam('rev', String(entry.revision));
                        setMobileDetail(true);
                      }}
                    >
                      <span className="mono">{`r${String(entry.revision)}`}</span>
                      {entry.revision === currentRevision ? (
                        <span className="history__tag">current</span>
                      ) : null}
                      {pinnedHere.length === 0 ? null : (
                        <span className="history__tag" title={`pinned by ${String(pinnedHere.length)} workload(s)`}>
                          <span aria-hidden="true">⚲ </span>
                          pinned
                        </span>
                      )}
                      {entry.payloadPresent ? null : (
                        <span className="history__tag history__tag--collected">payload collected</span>
                      )}
                      <span className="history__age">{relativeAge(entry.publishedAt, now)}</span>
                    </Button>
                  </li>
                );
              })}
            </ol>

            <div className="history__detail">
              <Button
                id="history-detail-back"
                type="button"
                className="history__back"
                onClick={() => {
                  setMobileDetail(false);
                  requestAnimationFrame(() => selectedRow.current?.focus());
                }}
              >
                ← All revisions
              </Button>
              {selected === undefined || selectedGate === null ? null : (
                <RevisionDetail
                  environmentName={environment.name}
                  env={env}
                  revision={selected}
                  revisions={revisions}
                  headingRef={detailHeading}
                  gate={selectedGate}
                  currentRevision={currentRevision}
                  pins={pinRows}
                  workloadNames={workloadNames}
                  secretByKeyId={secretByKeyId}
                  now={now}
                  keyFilter={keyFilter}
                  onFilterKey={(keyId) => {
                    setParam('key', keyId);
                    // "Show me this key's history" answers with a LIST, so the
                    // phone's single pane goes back to it. On a desktop both
                    // panes are on screen and this changes nothing.
                    setMobileDetail(false);
                    requestAnimationFrame(() => selectedRow.current?.focus());
                  }}
                  onRestore={(revisionKeys, keyId) => {
                    setRefusal(null);
                    let keyName: string | null = null;
                    if (keyId !== null) {
                      try {
                        keyName = restoreKeyName(keyId, keys, selected);
                      } catch (error) {
                        if (!(error instanceof Error)) {
                          throw error;
                        }
                        setRefusal(error.message);
                        return;
                      }
                    }
                    setSheet({
                      kind: 'restore',
                      revision: selected.revision,
                      keyId,
                      keyName,
                      keys: revisionKeys,
                      result: null,
                    });
                  }}
                  onPin={(revisionKeys) => {
                    setRefusal(null);
                    setSheet({
                      kind: 'pin',
                      revision: selected.revision,
                      keys: revisionKeys,
                      workloadPrincipalID: workloads[0]?.principal_id ?? '',
                      expiresAt: defaultPinExpiry(now),
                      overrideSchema: false,
                      offerOverride: false,
                    })
                  }}
                  onRelease={(pin) => {
                    setRefusal(null);
                    if (pin.releaseRetentionConsequence === 'collection_eligible') {
                      setSheet({ kind: 'release', pin });
                      return;
                    }
                    runRelease(pin);
                  }}
                />
              )}
            </div>
          </div>
        )}
      </aside>

      {sheet?.kind === 'restore' ? (
        <RestoreSheet
          environmentName={environment.name}
          environmentNames={environmentNames}
          revision={sheet.revision}
          keyName={sheet.keyName}
          result={sheet.result}
          busy={restore.isPending}
          publishBusy={publish.isPending}
          refusal={refusal ?? guard.error}
          matrixPath={matrixPath}
          onStage={() => runRestore(sheet.revision, sheet.keyId, sheet.keyName)}
          onPublish={() => {
            if (sheet.result === null) {
              throw new Error('restore publish started without a staged result');
            }
            publishRestore(sheet.revision, sheet.result);
          }}
          onClose={() => setSheet(null)}
        />
      ) : null}

      {sheet?.kind === 'pin' ? (
        <PinSheet
          environmentName={environment.name}
          revision={sheet.revision}
          isCurrent={sheet.revision === currentRevision}
          currentRevision={currentRevision}
          workloads={workloads.map((account) => ({
            principalID: account.principal_id,
            name: account.name,
            existingPin: pinRows.find((pin) => pin.workloadPrincipalId === account.principal_id),
          }))}
          state={sheet}
          pinCount={pinRows.length}
          expiryMinimum={expiryBounds.minimum}
          expiryMaximum={expiryBounds.maximum}
          busy={setPin.isPending}
          refusal={refusal ?? guard.error}
          comparisonBusy={comparePin.isPending}
          comparisonError={
            comparePin.isError &&
            comparePin.variables.environmentId === environment.id &&
            comparePin.variables.revision === sheet.revision
              ? comparisonRefusal(comparePin.error, sheet.revision)
              : null
          }
          comparison={
            comparePin.isSuccess &&
            comparePin.variables.environmentId === environment.id &&
            comparePin.variables.revision === sheet.revision
              ? pinComparedToLatest({
                  revision: sheet.revision,
                  revisionKeys: sheet.keys,
                  historical: comparePin.data.items,
                  latest: (currentValuesByEnvironment.get(environment.id) ?? []).map((value) => ({
                    keyId: value.key_id,
                    name: value.name,
                    classification: value.classification,
                    set: value.set,
                    revealed: value.revealed,
                    ...(value.value === undefined ? {} : { value: value.value }),
                  })),
                  laterRevisions: revisions.filter(
                    (revision) => revision.revision > sheet.revision,
                  ),
                })
              : null
          }
          onCompare={() => comparePin.mutate({ environmentId: environment.id, revision: sheet.revision })}
          onChange={(next) => {
            setSheet({ ...sheet, ...next });
            if (next.expiresAt === undefined) {
              return;
            }
            if (next.expiresAt !== '') {
              setRefusal(null);
              return;
            }
            try {
              pinExpiryInstant(next.expiresAt);
            } catch (error) {
              if (!(error instanceof Error)) {
                throw error;
              }
              setRefusal(error.message);
            }
          }}
          onSubmit={() =>
            runPin({
              revision: sheet.revision,
              workloadPrincipalID: sheet.workloadPrincipalID,
              expiresAt: sheet.expiresAt,
              overrideSchema: sheet.overrideSchema,
              revisionKeys: sheet.keys,
            })
          }
          onClose={() => setSheet(null)}
        />
      ) : null}

      {sheet?.kind === 'release' ? (
        <ReleaseSheet
          pin={sheet.pin}
          currentRevision={currentRevision}
          workloadName={
            workloadLabel(sheet.pin.workloadPrincipalId, workloadNames)
          }
          busy={releasePin.isPending}
          onRelease={() => runRelease(sheet.pin)}
          onClose={() => setSheet(null)}
        />
      ) : null}

      {guard.request === null ? null : (
        <Ceremony
          key={guard.requestKey}
          request={guard.request}
          onAuthorised={guard.onAuthorised}
          onCancel={guard.onCancel}
        />
      )}
    </>
  );
}

type Sheet =
  | {
      readonly kind: 'restore';
      readonly revision: bigint;
      readonly keyId: string | null;
      readonly keyName: string | null;
      readonly keys: readonly HistorySnapshotKey[];
      readonly result: RestoreSheetResult | null;
    }
  | {
      readonly kind: 'pin';
      readonly revision: bigint;
      readonly keys: readonly HistorySnapshotKey[];
      readonly workloadPrincipalID: string;
      readonly expiresAt: string;
      readonly overrideSchema: boolean;
      readonly offerOverride: boolean;
    }
  | { readonly kind: 'release'; readonly pin: HistoryPin };

type RestoreSheetResult = {
  readonly changes: RestoreResult['changes'];
  readonly preview: {
    readonly environments: RestoreResult['preview']['environments'];
  };
};

function sheetRevisionKeys(sheet: Sheet | null): readonly HistorySnapshotKey[] {
  return sheet !== null && sheet.kind !== 'release' ? sheet.keys : [];
}

/**
 * The detail pane: one revision's lineage, its consumers, and its two actions.
 *
 * The delivered key set comes from `getRevision`, which is NOT fetched for a
 * collected revision, that endpoint derives a change token over the snapshot's
 * manifest and refuses a collected payload by name. The changed-key list below
 * is lineage and survives collection either way.
 */
function RevisionDetail({
  environmentName,
  env,
  revision,
  revisions,
  headingRef,
  gate,
  currentRevision,
  pins,
  workloadNames,
  secretByKeyId,
  now,
  keyFilter,
  onFilterKey,
  onRestore,
  onPin,
  onRelease,
}: {
  env: EnvRef;
  environmentName: string;
  revision: HistoryRevision;
  revisions: readonly HistoryRevision[];
  headingRef: RefObject<HTMLHeadingElement | null>;
  gate: RevisionActionGate;
  currentRevision: bigint;
  pins: readonly HistoryPin[];
  workloadNames: ReadonlyMap<string, string>;
  secretByKeyId: ReadonlyMap<string, boolean>;
  now: Date;
  keyFilter: string | null;
  onFilterKey: (keyId: string) => void;
  onRestore: (revisionKeys: readonly HistorySnapshotKey[], keyId: string | null) => void;
  onPin: (revisionKeys: readonly HistorySnapshotKey[]) => void;
  onRelease: (pin: HistoryPin) => void;
}) {
  const [diffTarget, setDiffTarget] = useState<bigint | null>(null);
  const previous = revisions.find((item) => item.revision < revision.revision);
  const detail = useRevisionDetail(env, revision.revision, revision.payloadPresent);
  const revisionKeys: readonly HistorySnapshotKey[] = (detail.data?.keys ?? []).map((key) => ({
    keyId: key.key_id,
    name: key.name,
    classification: key.classification,
  }));
  const pinnedHere = pins.filter((pin) => pin.revision === revision.revision);

  return (
    <section aria-labelledby="history-detail-title">
      <h3 id="history-detail-title" className="mono" ref={headingRef} tabIndex={-1}>
        {`r${String(revision.revision)}`}
        {revision.revision === currentRevision ? <span className="history__tag">current</span> : null}
        {revision.payloadPresent ? null : (
          <span className="history__tag history__tag--collected">payload collected</span>
        )}
      </h3>
      <dl className="history__meta">
        <div>
          <dt>Published by</dt>
          <dd>
            <span className="mono" title={revision.publishedBy}>
              {revision.publishedByName ?? shortPrincipal(revision.publishedBy)}
            </span>
          </dd>
        </div>
        <div>
          <dt>Published</dt>
          <dd>
            <time dateTime={revision.publishedAt}>{relativeAge(revision.publishedAt, now)}</time>
          </dd>
        </div>
        <div>
          <dt>Schema revision pinned</dt>
          <dd className="mono">{`s${String(revision.schemaRevision)}`}</dd>
        </div>
      </dl>

      {pinnedHere.length === 0 ? null : (
        <p className="history__consumers">
          <span aria-hidden="true">⚲ </span>
          {`${pinnedHere
            .map((pin) => workloadLabel(pin.workloadPrincipalId, workloadNames))
            .join(', ')} receive${pinnedHere.length === 1 ? 's' : ''} this revision's values`}
          {revision.revision === currentRevision ? '' : ` instead of latest (r${String(currentRevision)})`}
          {' until the pin is released or expires.'}
        </p>
      )}

      {gate.reason === null ? null : (
        <p className="history__gate" role="status">
          {gate.reason}
        </p>
      )}
      {detail.isError ? (
        <Alert>{revisionDetailRefusal(detail.error, revision.revision)}</Alert>
      ) : null}

      <h4>{`Changed keys (${String(revision.changedKeys.length)})`}</h4>
      <ul className="history__changes">
        {revision.changedKeys.map((changed) => {
          const secret = secretByKeyId.get(changed.keyId) === true;
          return (
            <li key={changed.keyId}>
              <Button
                type="button"
                className="history__change mono"
                aria-pressed={keyFilter === changed.keyId}
                onClick={() => onFilterKey(changed.keyId)}
              >
                {secret ? <><Glyph name="lock" /> </> : null}
                {changed.name}
              </Button>
              <span className="history__kind">{changed.change}</span>
              {secret ? <span className="history__presence">write-presence only</span> : null}
              <Button
                type="button"
                disabled={!gate.restore || !detail.isSuccess}
                onClick={() => onRestore(revisionKeys, changed.keyId)}
              >
                {`Restore ${changed.name}…`}
              </Button>
            </li>
          );
        })}
      </ul>

      <div className="history__actions">
        <Button
          type="button"
          variant="primary"
          disabled={!gate.restore || !detail.isSuccess}
          title={gate.restore ? undefined : gate.reason ?? undefined}
          onClick={() => onRestore(revisionKeys, null)}
        >
          {`Restore r${String(revision.revision)}…`}
        </Button>
        <Button
          type="button"
          disabled={!gate.pin || !detail.isSuccess}
          title={gate.pin ? undefined : gate.reason ?? undefined}
          onClick={() => onPin(revisionKeys)}
        >
          {`Pin r${String(revision.revision)}…`}
        </Button>
      </div>

      <div className="history__actions">
        <Button type="button" disabled={!revision.payloadPresent || previous?.payloadPresent !== true} onClick={() => setDiffTarget(previous?.revision ?? null)}>Diff vs previous</Button>
        <Button type="button" disabled={!revision.payloadPresent || revision.revision === currentRevision} onClick={() => setDiffTarget(currentRevision)}>Diff vs current</Button>
      </div>
      {diffTarget !== null ? <RevisionDiffDialog key={`${env.environment}:${String(revision.revision)}:${String(diffTarget)}`} env={env} environmentName={environmentName} left={diffTarget < revision.revision ? diffTarget : revision.revision} right={diffTarget < revision.revision ? revision.revision : diffTarget} onClose={() => setDiffTarget(null)} /> : null}

      <h4>{`Pins (${String(pins.length)})`}</h4>
      <p className="history__pin-note">
        A pinned workload stops following latest: it keeps receiving exactly the pinned
        revision&apos;s values, restarts included, until the pin is released or expires.
      </p>
      {pins.length === 0 ? (
        <p className="history__empty">{`No pins: every workload here follows latest (r${String(currentRevision)}).`}</p>
      ) : (
        <ul className="history__pins">
          {pins.map((pin) => {
            const expiry = pinExpiry(pin.expiresAt, now);
            const workload = workloadLabel(pin.workloadPrincipalId, workloadNames);
            const publishesBehind = revisions.filter(
              (entry) => entry.revision > pin.revision,
            ).length;
            const gap = pin.revision === currentRevision
              ? `${workload} is pinned to the current revision r${String(pin.revision)}: it will keep these values when the next publish lands.`
              : `${workload} still runs on r${String(pin.revision)}'s values, ${String(publishesBehind)} publishes behind latest (r${String(currentRevision)}). New publishes don't reach it.`;
            return (
              <li key={pin.id} className="history__pin">
                <span className="mono">
                  <span aria-hidden="true">⚲ </span>
                  {`r${String(pin.revision)}`}
                </span>
                <span className="mono" title={pin.workloadPrincipalId}>
                  {workload}
                </span>
                <span className={`history__expiry history__expiry--${expiry.tier}`}>
                  {expiry.tier === 'expired'
                    ? `expired: still delivering r${String(pin.revision)} while its payload survives`
                    : expiry.text}
                </span>
                <span className="history__pin-gap">{gap}</span>
                {pin.schemaOverride ? (
                  <span className="history__drift" title="Pinned despite a current-schema failure, recorded as an explicit override. Pinned delivery is verbatim.">
                    <Glyph name="delta" /> schema drift
                  </span>
                ) : null}
                {/* The server's preview: this pin is the only thing holding the
                    payload past its policy window (ADR revision-model § Retention). */}
                {pin.releaseRetentionConsequence === 'collection_eligible' ? (
                  <>
                    <span className="history__warn">
                      <span aria-hidden="true">⚠ </span>
                      sole keeper
                    </span>
                    <span className="history__pin-gap">
                      {`r${String(pin.revision)} is past normal retention: its values survive only because of this pin.`}
                    </span>
                  </>
                ) : null}
                <Button
                  type="button"
                  onClick={() => onRelease(pin)}
                >
                  Release
                </Button>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

/**
 * The restore sheet: what a restore does, then what it did.
 *
 * **Divergence from the prototype, named.** Iteration 1 previews first and
 * stages on confirmation. The shipped verb does both in one call,
 * `rollbackRevision` writes the drafts and returns the impact preview with the
 * token that binds them, so this sheet explains the act before it is taken and
 * reports the exact impact after. Nothing is published either way: the drafts
 * are ordinary, the matrix's draft dots appear, and the ordinary publish sheet
 * commits them.
 */
function RestoreSheet({
  environmentName,
  environmentNames,
  revision,
  keyName,
  result,
  busy,
  publishBusy,
  refusal,
  matrixPath,
  onStage,
  onPublish,
  onClose,
}: {
  environmentName: string;
  /** Names for every environment the preview may cover; an unknown id renders as itself. */
  environmentNames: ReadonlyMap<string, string>;
  revision: bigint;
  keyName: string | null;
  result: RestoreSheetResult | null;
  busy: boolean;
  publishBusy: boolean;
  refusal: string | null;
  matrixPath: string;
  onStage: () => void;
  onPublish: () => void;
  onClose: () => void;
}) {
  const groups: readonly HistoryImpactEnvironment[] =
    result === null
      ? []
      : result.preview.environments.map((environment) => ({
          environmentId: environment.environment_id,
          name: environmentNames.get(environment.environment_id) ?? environment.environment_id,
          baseRevision: environment.base_revision,
          protected: environment.protected,
          changes: environment.changes.map((change): HistoryImpactChange => ({
            keyId: change.key_id,
            name: change.name,
            classification: change.classification,
            operation: change.operation,
            status: change.status,
            ...(change.before === undefined ? {} : { before: change.before }),
            ...(change.after === undefined ? {} : { after: change.after }),
          })),
        }));
  const summary = restorePreviewSummary(groups.flatMap((group) => group.changes));

  return (
    <Dialog
      title={
        keyName === null
          ? `Restore r${String(revision)} · ${environmentName}`
          : `Restore ${keyName} from r${String(revision)} · ${environmentName}`
      }
      lede={
        <>
          Stages drafts reproducing r{String(revision)}. Publishing them runs the normal
          pipeline and re-validates against the CURRENT schema; history is never rewritten.
        </>
      }
      size="wide"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      {refusal === null ? null : (
        <Alert>{refusal}</Alert>
      )}

      {result === null ? (
        <div className="dialog__actions">
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="button" variant="primary" disabled={busy} onClick={onStage}>
            {busy ? 'Staging…' : `Stage the restore from r${String(revision)}`}
          </Button>
        </div>
      ) : (
        <>
          <p className="history__chips" role="status">
            {summary.chips.map((chip) => (
              <span className="count" key={chip}>
                {chip}
              </span>
            ))}
          </p>
          {groups.map((group) => (
            <section key={group.environmentId} className="history__impact-group">
              <h3 className="history__impact-heading">
                {impactHeading(group)}
                {group.protected ? <span className="history__protected">PROTECTED</span> : null}
              </h3>
              <ul className="history__impact">
                {group.changes.map((change) => (
                  <li key={`${group.environmentId}:${change.keyId}`}>
                    <span className="mono">
                      {change.classification === 'secret' ? <><Glyph name="lock" /> </> : null}
                      {change.name}
                    </span>
                    <span className="history__kind">{change.operation === 'set' ? 'set' : 'clear'}</span>
                    <span className="mono history__impact-values">
                      {change.classification === 'secret' ? (
                        `secret: ${change.status}, write-presence only`
                      ) : (
                        <>
                          <ImpactValue value={change.before} absent="absent" />
                          {' → '}
                          {change.operation === 'unset' ? (
                            <span className="values__absent">cleared</span>
                          ) : (
                            <ImpactValue value={change.after} absent="unreadable" />
                          )}
                        </>
                      )}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          ))}
          <Alert tone="done">Drafts are staged; they are also visible on the matrix.</Alert>
          {/* The row stays in the children: the gate sentence below explains a
              disabled publish, so `actions` would put it after its own reason. */}
          <div className="dialog__actions">
            <Link id="history-restore-back" className="btn" to={matrixPath}>
              Back to the matrix
            </Link>
            <Button
              id="history-restore-publish"
              type="button"
              variant="primary"
              disabled={publishBusy || result.changes.length === 0}
              aria-describedby={result.changes.length === 0 ? 'history-restore-no-drafts' : undefined}
              onClick={onPublish}
            >
              {publishBusy
                ? 'Publishing this restore…'
                : restorePublishLabel(revision, groups)}
            </Button>
          </div>
          {result.changes.length === 0 ? (
            <p id="history-restore-no-drafts" className="history__gate" role="status">
              Nothing changed, so this restore has no drafts to publish.
            </p>
          ) : null}
        </>
      )}
    </Dialog>
  );
}

/** The pin sheet: what pinning does, to which workload, until when. */
function PinSheet({
  environmentName,
  revision,
  isCurrent,
  currentRevision,
  workloads,
  state,
  pinCount,
  expiryMinimum,
  expiryMaximum,
  busy,
  refusal,
  comparisonBusy,
  comparisonError,
  comparison,
  onCompare,
  onChange,
  onSubmit,
  onClose,
}: {
  environmentName: string;
  revision: bigint;
  isCurrent: boolean;
  currentRevision: bigint;
  workloads: readonly {
    readonly principalID: string;
    readonly name: string;
    readonly existingPin: HistoryPin | undefined;
  }[];
  state: {
    readonly workloadPrincipalID: string;
    readonly expiresAt: string;
    readonly overrideSchema: boolean;
    readonly offerOverride: boolean;
  };
  /** Pins already held in this environment, against the per-project quota. */
  pinCount: number;
  expiryMinimum: string;
  expiryMaximum: string;
  busy: boolean;
  refusal: string | null;
  comparisonBusy: boolean;
  comparisonError: string | null;
  comparison: PinComparison | null;
  onCompare: () => void;
  onChange: (next: Partial<{ workloadPrincipalID: string; expiresAt: string; overrideSchema: boolean }>) => void;
  onSubmit: () => void;
  onClose: () => void;
}) {
  const chosen = workloads.find((workload) => workload.principalID === state.workloadPrincipalID);
  const plan = pinAction(chosen?.existingPin?.revision, revision);
  const moveMayCollect =
    plan.kind === 'move' && chosen?.existingPin?.releaseRetentionConsequence === 'collection_eligible';

  return (
    <Dialog
      title={`Pin r${String(revision)} · ${environmentName}`}
      lede={`One pin per workload and environment. ${String(pinCount)} pinned in this environment; the project quota is 100 and expiry is mandatory.`}
      size="wide"
      className="history-sheet"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      {refusal === null ? null : (
        <Alert>{refusal}</Alert>
      )}

      <div className="history__pin-what">
        <h3>What pinning does</h3>
        <ul>
          <li>
            The workload keeps receiving exactly r{String(revision)}&apos;s values, across
            restarts and redeploys, instead of following latest.
          </li>
          <li>
            New publishes to {environmentName} stop reaching this workload while the pin lives;
            everyone else still gets latest.
          </li>
          <li>r{String(revision)}&apos;s values are kept from retention clean-up while the pin exists.</li>
          <li>
            On release the workload resumes latest on its next fetch. On EXPIRY it keeps
            delivering the pinned revision until the payload is collected; expiry never silently
            changes delivery.
          </li>
        </ul>
      </div>

      {isCurrent ? null : (
        <p className="history__gate" role="status">
          Non-current revision: this routes HISTORICAL content to a workload, so it takes
          reveal-history and one disclosure ceremony over the revision&apos;s secret keys.
        </p>
      )}

      <label className="history__field" htmlFor="history-pin-workload">
        Workload
      </label>
      <select
        id="history-pin-workload"
        className="btn"
        value={state.workloadPrincipalID}
        onChange={(event) => onChange({ workloadPrincipalID: event.target.value })}
      >
        {workloads.length === 0 ? <option value="">No workloads in this project</option> : null}
        {workloads.map((workload) => (
          <option key={workload.principalID} value={workload.principalID}>
            {`${workload.name}: ${workload.existingPin === undefined ? 'follows latest' : `pinned to r${String(workload.existingPin.revision)}`}`}
          </option>
        ))}
      </select>

      {plan.kind === 'move' && chosen?.existingPin !== undefined ? (
        <>
          <p className="history__gate" role="status">
            {`${chosen.name} is currently pinned to r${String(chosen.existingPin.revision)}. One pin per workload, so this MOVES it to r${String(revision)} and replaces the old pin atomically.`}
          </p>
          {moveMayCollect ? (
            <p id="history-pin-move-collection-warning" className="history__gate" role="alert">
              Moving this pin may make r{String(chosen.existingPin.revision)}&apos;s values eligible for immediate collection. The server will re-evaluate atomically.
            </p>
          ) : null}
        </>
      ) : null}
      {plan.kind === 'renew' ? (
        <p className="history__gate" role="status">
          {`${chosen?.name ?? 'This workload'} is already pinned to r${String(revision)}. Pinning again RENEWS it: the expiry is extended and the pin is re-validated against the current schema, which surfaces any new drift.`}
        </p>
      ) : null}

      <label className="history__field" htmlFor="history-pin-expiry">
        Expires on (default 180 days, maximum 365)
      </label>
      <input
        id="history-pin-expiry"
        className="mono"
        type="date"
        min={expiryMinimum}
        max={expiryMaximum}
        value={state.expiresAt}
        onChange={(event) => onChange({ expiresAt: event.target.value })}
      />

      {state.offerOverride ? (
        <Checkbox
          label="Pin despite the current-schema failure above. Pinned delivery is verbatim, so this is recorded as an explicit override and the pin is surfaced as drift afterwards."
          checked={state.overrideSchema}
          onChange={(event) => onChange({ overrideSchema: event.target.checked })}
        />
      ) : null}

      <section className="history__comparison" aria-labelledby="history-pin-compare-heading">
        <h3 id="history-pin-compare-heading">
          {`Compare r${String(revision)} to latest (reads r${String(revision)}'s config values)`}
        </h3>
        <p>Secret lines are write-presence from the lineage, never a value comparison.</p>
        <Button
          id="history-pin-compare"
          type="button"
          disabled={comparisonBusy}
          aria-expanded={comparison !== null}
          aria-controls="history-pin-compare-results"
          onClick={onCompare}
        >
          {comparisonBusy ? 'Comparing…' : 'Run comparison'}
        </Button>
        {comparisonError === null ? null : (
          <Alert>{comparisonError}</Alert>
        )}
        {comparison === null ? null : (
          <div id="history-pin-compare-results" role="status" aria-live="polite">
            {comparison.lines.length === 0 ? (
              <p>No set keys differ from latest.</p>
            ) : (
              <ul className="history__comparison-lines">
                {comparison.lines.map((line) => <li key={line}>{line}</li>)}
              </ul>
            )}
            {comparison.unchangedConfigKeys === 0 ? null : (
              <p>{`${String(comparison.unchangedConfigKeys)} config key${comparison.unchangedConfigKeys === 1 ? '' : 's'} unchanged`}</p>
            )}
          </div>
        )}
      </section>

      {/* The row stays in the children because the latest-revision sentence
          below it is body copy, not part of the decision. */}
      <div className="dialog__actions">
        <Button type="button" onClick={onClose}>
          Cancel
        </Button>
        <Button
          id="history-pin-submit"
          type="button"
          variant="primary"
          disabled={busy || state.workloadPrincipalID === '' || state.expiresAt === ''}
          onClick={onSubmit}
        >
          {busy ? 'Pinning…' : moveMayCollect ? `${plan.label}, old values may be collected` : plan.label}
        </Button>
      </div>
      <p>{`Latest in ${environmentName} is r${String(currentRevision)}.`}</p>
    </Dialog>
  );
}

/** Neutral confirmation: retention truth exists only after the locked release. */
function ReleaseSheet({
  pin,
  currentRevision,
  workloadName,
  busy,
  onRelease,
  onClose,
}: {
  pin: HistoryPin;
  currentRevision: bigint;
  workloadName: string;
  busy: boolean;
  onRelease: () => void;
  onClose: () => void;
}) {
  const soleKeeper = pin.releaseRetentionConsequence === 'collection_eligible';
  const revision = String(pin.revision);
  return (
    <Dialog
      title="Release pin"
      size="wide"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      actions={
        <>
          <Button type="button" onClick={onClose}>
            Keep the pin
          </Button>
          <Button
            id="history-release-confirm"
            type="button"
            variant="danger"
            disabled={busy}
            onClick={onRelease}
          >
            {busy ? 'Releasing…' : soleKeeper ? `Release and allow collection of r${revision}` : 'Release pin'}
          </Button>
        </>
      }
    >
      {/* Two ledes, so they stay in the children: the atom's `lede` is one
          paragraph and a sole-keeper release has a consequence to state first. */}
      {soleKeeper ? (
        <p className="dialog__lede">
          {`This pin is the only thing keeping r${revision}'s values. Releasing it makes them collection-eligible: no diff by value, no restore, no reveal once collected.`}
        </p>
      ) : null}
      <p className="dialog__lede">{`The server will report r${revision}'s retention consequence after release.`}</p>
      <ul className="history__consequences">
        <li>{`${workloadName} resumes latest (r${String(currentRevision)}) on its next fetch.`}</li>
        <li>
          {soleKeeper
            ? `r${revision}'s values become collection-eligible at release; a sweep may collect them immediately.`
            : 'The values may remain retained, become collection-eligible, or already be collected.'}
        </li>
        <li>The lineage entry stays in every case.</li>
      </ul>
    </Dialog>
  );
}

export function PinReleaseOutcome({
  consequence,
  revision,
}: {
  consequence: RetentionConsequence;
  revision: bigint;
}) {
  let retention: string;
  switch (consequence) {
    case 'retained':
      retention = `r${String(revision)}'s values remain retained by current policy or another live pin.`;
      break;
    case 'collection_eligible':
      retention = `At release time, r${String(revision)}'s values became eligible for collection. A sweep may collect them immediately; lineage stays.`;
      break;
    case 'already_collected':
      retention = `r${String(revision)}'s values were already collected before release completed; lineage stays.`;
      break;
    default: {
      const exhaustive: never = consequence;
      return exhaustive;
    }
  }
  return <>{`Pin released: workload resumes latest on its next fetch. ${retention}`}</>;
}

function toHistoryRevision(item: HistoryRevisionItem): HistoryRevision {
  const shared = {
    revision: item.revision,
    schemaRevision: item.schema_revision,
    publishedBy: item.published_by,
    ...(item.published_by_name === undefined ? {} : { publishedByName: item.published_by_name }),
    publishedAt: item.published_at,
    changedKeys: item.changed_keys.map((changed) => ({
      keyId: changed.key_id,
      name: changed.name,
      change: changed.change,
    })),
  };
  if (item.payload_present) {
    return { ...shared, payloadPresent: true };
  }
  if (item.collected_policy === undefined) {
    throw new Error(`Collected revision r${String(item.revision)} does not name its retention policy.`);
  }
  return { ...shared, payloadPresent: false, collectedPolicy: item.collected_policy };
}

function historyMutationRefusal(
  error: unknown,
  action: 'restore' | 'pin',
  unit: readonly CeremonyKey[],
  environmentName: string,
): string {
  if (error instanceof ApiError && error.status === 403 && unit.length > 0) {
    return (
      `Refused: reading the earlier secret values of ${unit.map((key) => key.name).join(', ')} ` +
      `requires reveal-history on ${environmentName}.`
    );
  }
  return historyRefusalText(error, action);
}

function revisionDetailRefusal(error: unknown, revision: bigint): string {
  const detailed = callerSafeRefusal(error, `Revision r${String(revision)} detail refused`);
  if (detailed !== null) {
    return detailed;
  }
  if (error instanceof ApiError) {
    return `Revision r${String(revision)} detail could not be read (error ${String(error.status)}).`;
  }
  return `Revision r${String(revision)} detail could not be read.`;
}

function comparisonRefusal(error: unknown, revision: bigint): string {
  const detailed = callerSafeRefusal(error, `Comparison of r${String(revision)} refused`);
  if (detailed !== null) {
    return detailed;
  }
  if (error instanceof ApiError) {
    return `Comparison of r${String(revision)} could not be read (error ${String(error.status)}).`;
  }
  return `Comparison of r${String(revision)} could not be read.`;
}

/** A config value in the impact preview, with explicit absence rendered as such. */
function ImpactValue({ value, absent }: { value: string | undefined; absent: 'absent' | 'unreadable' }) {
  return value === undefined ? <span className="values__absent">{absent}</span> : <>{value}</>;
}

function toHistoryPin(pin: RevisionPinItem): HistoryPin {
  return {
    id: pin.id,
    workloadPrincipalId: pin.workload_principal_id,
    revision: pin.revision,
    expiresAt: pin.expires_at,
    expired: pin.expired,
    schemaOverride: pin.schema_override,
    releaseRetentionConsequence: pin.release_retention_consequence,
  };
}

/**
 * A prefixed UUIDv7 is unreadable at a glance; the whole one lives in `title`.
 * Used only when the authorized revision response has no current publisher name.
 */
export function shortPrincipal(id: string): string {
  return id.length <= 12 ? id : `${id.slice(0, 12)}…`;
}
