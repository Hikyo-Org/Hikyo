import { useVirtualizer } from '@tanstack/react-virtual';
import { type MouseEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { generatePath, Link, useParams } from 'react-router';

import { GIT_DEFINITIONS_NOTICE, useDefinitionsSettings } from '../api/definitions.ts';
import { historyHref } from '../api/history.ts';
import { useWorkspaceContext, withRemote } from '../api/transport.tsx';
import { surfaceById } from '../app/navigation.ts';
import {
  assembleKeyImpact,
  matrixPublishValidation,
  matrixMutationError,
  pendingConfigPreview,
  restorePreviewWasAttached,
  useClearMatrixValue,
  useCopyMatrixConfig,
  useCreateKey,
  useMatrixProject,
  usePublishMatrix,
  useReclassifyKey,
  useStageMatrixValue,
  type MatrixKeyList,
  type MatrixEnvironmentRow,
  type MatrixPendingDraft,
  type MatrixRef,
  type MatrixSignalCell,
} from '../api/matrix.ts';
import {
  type EnvironmentList,
  type ValueCell,
} from '../api/values.ts';
import { HistoryDrawer } from './HistoryDrawer.tsx';
import { KeyDeclarationDetail } from './KeyDeclarationDetail.tsx';
import type { HistoryCurrentCell } from './history-state.ts';
import {
  MatrixPublishSheet,
  type MatrixPendingEntry,
} from './MatrixPublishSheet.tsx';
import { ApiError, type RefusalFinding } from '../api/client.ts';
import { ImportWizard } from './ImportWizard.tsx';
import { MatrixKeyCreate, type MatrixKeyCreatePayload } from './MatrixKeyCreate.tsx';
import { MatrixRowEditor } from './MatrixRowEditor.tsx';
import { ScanBlockDialog } from './ScanBlockDialog.tsx';
import { ScanWarnDialog, type ScanWarnItem } from './ScanWarnDialog.tsx';
import { useProjectSidebar } from './Shell.tsx';
import {
  computeMatrixProblems,
  groupProblemCounts,
  indexMatrixProblems,
  keysForMatrixFilter,
  normalizeMatrixDraftValue,
  requiredInEnvironment,
  toggleVisibleEnvironment,
  type MatrixFilter,
  type MatrixPresence,
  type MatrixStateKey,
  type MatrixValidationError,
} from './matrix-state.ts';

type MatrixKey = MatrixKeyList['items'][number];
type Environment = EnvironmentList['items'][number];
type Selection = { readonly keyId: string; readonly environmentId?: string };

type DisplayGroup = {
  readonly id: string;
  readonly name: string;
  readonly keys: readonly MatrixKey[];
};

type DisplayRow =
  | { readonly kind: 'group'; readonly group: DisplayGroup }
  /**
   * `alt` is the zebra stripe, decided here rather than in CSS.
   *
   * `nth-child` cannot express it: the striping restarts at every group so a
   * group always opens unshaded, and it counts only the rows the problems
   * filter left standing. The virtualiser's two spacer rows would shift the
   * parity of every `nth-child` calculation anyway.
   */
  | { readonly kind: 'key'; readonly key: MatrixKey; readonly alt: boolean };

/**
 * Whole-project environment matrix (#57, frozen prototype iteration 31).
 *
 * The prototype supplies the table geometry and density valves. The flat
 * model supplies the semantics: every cell is set or absent in exactly one
 * environment. No inheritance labels, masks, provenance chains, or ambient
 * cross-environment comparison survive here. Lineage is one gesture away in
 * the cell modal as the API's actor, timestamp, and revision facts.
 */
export function Matrix({
  historyOpen = false,
  keyDetailOpen = false,
}: { historyOpen?: boolean; keyDetailOpen?: boolean } = {}) {
  const params = useParams();
  // Inside a workspace, every link to another of the workspace's own surfaces
  // must carry the `?remote=` marker or it silently drops back to this
  // instance's data. `remote` is '' at home, and `historyLink` is then a no-op.
  const workspace = useWorkspaceContext();
  const remote = workspace?.remote ?? '';
  const historyLink = (input: Parameters<typeof historyHref>[0]) =>
    withRemote(historyHref(input), remote);
  const ref: MatrixRef = { org: params['org'] ?? '', project: params['project'] ?? '' };
  // The catalogue detail is addressed by the key's immutable id (#491): a
  // rename must not break a bookmarked link. `remote` rides along for the same
  // workspace reason `historyLink` carries it.
  const keyDetailLink = (keyId: string) =>
    withRemote(generatePath(surfaceById('key-detail').path, { ...ref, key: keyId }), remote);
  // The key the detail surface is open on, read from the route only when this
  // component is mounted as the key-detail element. `undefined` at every other
  // matrix mount, so the panel never appears there.
  const keyDetailId = keyDetailOpen ? params['key'] ?? '' : undefined;
  const matrix = useMatrixProject(ref);
  const stage = useStageMatrixValue(ref);
  const clear = useClearMatrixValue(ref);
  const publish = usePublishMatrix(ref);
  const copy = useCopyMatrixConfig(ref);
  const reclassify = useReclassifyKey(ref);
  const createKey = useCreateKey(ref);
  // Git-managed projects declare keys only through `definitions apply`; the SPA
  // keeps every value action but explains why declaration is unavailable (#492
  // AC4). A failed/absent settings read never fabricates 'git' — declaration
  // stays available and any real refusal surfaces at the write.
  const definitionsSettings = useDefinitionsSettings(ref.org, ref.project);
  const gitManaged = definitionsSettings.data?.definitions_source === 'git';

  const environmentRows = matrix.environmentRows;
  const environments = environmentRows.map((row) => row.environment);
  const keys = matrix.keys.data?.items ?? [];
  const keyGroups = matrix.groups.data?.items ?? [];
  const [visibleEnvironmentIds, setVisibleEnvironmentIds] = useState<readonly string[]>([]);
  const [collapsedGroups, setCollapsedGroups] = useState<ReadonlySet<string>>(() => new Set());
  const [filter, setFilter] = useState<MatrixFilter>('all');
  const [selection, setSelection] = useState<Selection | null>(null);
  // env-matrix 31 `+ key`: the create modal, opened with a folder prefilled from
  // a group header or `null` from the empty state / a new group.
  const [create, setCreate] = useState<{ readonly folder: string | null } | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  // #495: the browser dotenv import wizard, opened from the matrix toolbar. Value
  // import is not git-gated (only declaration is), so it stays available on a
  // git-managed project — the wizard itself skips new keys there.
  const [importOpen, setImportOpen] = useState(false);
  // Surface-2 scanner block (#183): the redacted findings a refused write
  // carried, plus the override that retries it with their acknowledgements.
  const [scanBlock, setScanBlock] = useState<{
    readonly title: string;
    readonly intro: string;
    readonly findings: readonly RefusalFinding[];
    readonly onOverride: ((tokens: readonly string[]) => Promise<void>) | null;
  } | null>(null);
  const [warn, setWarn] = useState<{
    readonly keyId: string;
    readonly keyName: string;
    readonly items: readonly ScanWarnItem[];
  } | null>(null);
  const [validationErrors, setValidationErrors] = useState<readonly MatrixValidationError[]>([]);
  const [publishOpen, setPublishOpen] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<{
    readonly owner: Selection;
    readonly message: string;
  } | null>(null);
  // If the settings read confirms git-managed while the declare modal is open,
  // withdraw it: declaration is unavailable, and the header/empty-state entry
  // points have already vanished. The declareKey guard is the write-time
  // backstop; this closes the surface the operator can see.
  useEffect(() => {
    if (gitManaged && create !== null) {
      setCreate(null);
      setCreateError(null);
    }
  }, [gitManaged, create]);

  const matrixScroll = useRef<HTMLDivElement>(null);
  const matrixTable = useRef<HTMLTableElement>(null);
  // A state ref, not a `useRef`: the table only exists in one of this
  // component's four bodies, so the effect below has to run when the node
  // ARRIVES, and a ref object mutating does not re-run anything.
  const [matrixHead, setMatrixHead] = useState<HTMLTableSectionElement | null>(null);
  const historyOpener = useRef<HTMLAnchorElement>(null);
  const keyDetailOpener = useRef<HTMLAnchorElement>(null);

  /**
   * The group rows stick UNDER the column header, so they need its height.
   *
   * Measured rather than assumed: the header is a 44px minimum, not a 44px
   * box, and it grows when the webfont lands or a long environment name wraps.
   * `getBoundingClientRect` and not `offsetHeight` because the fractional part
   * is the difference between the two rows meeting and a one-pixel seam of
   * scrolling content between them.
   */
  useEffect(() => {
    const table = matrixTable.current;
    if (matrixHead === null || table === null) return;
    const sync = () => {
      table.style.setProperty('--gh', `${String(matrixHead.getBoundingClientRect().height)}px`);
    };
    sync();
    const observer = new ResizeObserver(sync);
    observer.observe(matrixHead);
    return () => {
      observer.disconnect();
    };
  }, [matrixHead]);

  const [mobileLayout, setMobileLayout] = useState(
    () =>
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(max-width: 800px)').matches,
  );

  useEffect(() => {
    if (typeof window.matchMedia !== 'function') {
      return;
    }
    const query = window.matchMedia('(max-width: 800px)');
    const update = () => setMobileLayout(query.matches);
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  }, []);

  // The legend and environment-picker are native <details> popovers. Native
  // <details> only close by re-clicking their summary — so, like the prototype's
  // popovers, close them on an outside pointer-down or Escape. Opening one this
  // way also closes the other (its summary click lands outside the first).
  useEffect(() => {
    const selector =
      'details.matrix__legend[open], details.matrix__environment-picker[open]';
    const closeOutside = (event: Event) => {
      const target = event.target as Node | null;
      for (const open of document.querySelectorAll(selector)) {
        if (event.type === 'keydown' || target === null || !open.contains(target)) {
          open.removeAttribute('open');
        }
      }
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') closeOutside(event);
    };
    document.addEventListener('pointerdown', closeOutside);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', closeOutside);
      document.removeEventListener('keydown', onKey);
    };
  }, []);

  const environmentSignature = environments.map((environment) => environment.id).join('/');
  useEffect(() => {
    setVisibleEnvironmentIds(environments.map((environment) => environment.id));
    setCollapsedGroups(new Set());
    setFilter('all');
    setSelection(null);
    setMutationError(null);
  }, [ref.org, ref.project, environmentSignature]);

  const valuesByCell = useMemo(() => {
    const cells = new Map<string, ValueCell>();
    for (const row of environmentRows) {
      for (const cell of row.values.data?.items ?? []) {
        cells.set(cellID(cell.key_id, row.environmentId), cell);
      }
    }
    return cells;
  }, [environmentRows]);

  const signalsByCell = useMemo(() => {
    const cells = new Map<string, MatrixSignalCell>();
    for (const row of environmentRows) {
      for (const signal of row.signals.data?.cells ?? []) {
        cells.set(cellID(signal.key_id, row.environmentId), signal);
      }
    }
    return cells;
  }, [environmentRows]);

  // The caller's own drafts, keyed by immutable version id. Server truth: the
  // publish sheet and the editors preview from this map, never from anything
  // cached client-side, so a reload or a second browser shows the same review.
  const draftsByVersion = useMemo(() => {
    const drafts = new Map<string, MatrixPendingDraft>();
    for (const row of environmentRows) {
      for (const draft of row.pendingDrafts.data?.items ?? []) {
        drafts.set(draft.version_id, draft);
      }
    }
    return drafts;
  }, [environmentRows]);

  // The open key's cross-environment lifecycle impact (#494), assembled here
  // from cells the matrix already holds and handed to the detail surface as
  // value-free id lists — a config value cell can carry material the detail
  // must never receive, so only the booleans (set / pending) cross. It stays
  // "not ready" until the environment list and every row's values+signals have
  // loaded, so the delete/reclassify previews never understate what an action
  // touches (fail-closed, the surface's own idiom).
  const keyDetailImpact = useMemo(() => {
    if (keyDetailId === undefined || keyDetailId === '') {
      return { setEnvironmentIds: [], pendingEnvironmentIds: [] };
    }
    return assembleKeyImpact(
      environmentRows.map((row) => ({
        environmentId: row.environmentId,
        set: valuesByCell.get(cellID(keyDetailId, row.environmentId))?.set === true,
        pending: signalsByCell.get(cellID(keyDetailId, row.environmentId))?.pending !== undefined,
      })),
    );
  }, [environmentRows, keyDetailId, signalsByCell, valuesByCell]);
  const keyDetailImpactReady =
    matrix.environments.isSuccess &&
    environmentRows.every(
      (row) => row.values.status !== 'pending' && row.signals.status !== 'pending',
    );

  const stateKeys = useMemo<readonly MatrixStateKey[]>(
    () =>
      keys.map((key) => ({
        id: key.id,
        name: key.name,
        groupId: displayGroupID(key),
        requiredIn: matrixPresence(key),
      })),
    [keys],
  );
  const problems = useMemo(
    () =>
      computeMatrixProblems({
        keys: stateKeys,
        environmentIds: environments.map((environment) => environment.id),
        values: environments.flatMap((environment) =>
          keys.map((key) => ({
            keyId: key.id,
            environmentId: environment.id,
            set: valuesByCell.get(cellID(key.id, environment.id))?.set === true,
            pendingOperation: signalsByCell.get(cellID(key.id, environment.id))?.pending?.operation,
          })),
        ),
        validationErrors,
      }),
    [environments, keys, signalsByCell, stateKeys, validationErrors, valuesByCell],
  );
  const problemCounts = useMemo(() => groupProblemCounts(problems), [problems]);
  const problemsByCell = useMemo(() => indexMatrixProblems(problems), [problems]);
  const filteredKeyIDs = useMemo(
    () => new Set(keysForMatrixFilter(stateKeys, problems, filter).map((key) => key.id)),
    [filter, problems, stateKeys],
  );
  const displayGroupList = useMemo(() => displayGroups(keys, keyGroups), [keyGroups, keys]);
  const groups = useMemo(
    () => displayGroupList.map((group) => ({
      ...group,
      keys: group.keys.filter((key) => filteredKeyIDs.has(key.id)),
    })),
    [displayGroupList, filteredKeyIDs],
  );
  const displayRows = useMemo<readonly DisplayRow[]>(
    () =>
      groups.flatMap<DisplayRow>((group) =>
        group.keys.length === 0
          ? []
          : [
              { kind: 'group', group },
              ...(collapsedGroups.has(group.id)
                ? []
                : group.keys.map((key, index): DisplayRow => ({
                    kind: 'key',
                    key,
                    alt: index % 2 === 1,
                  }))),
            ],
      ),
    [collapsedGroups, groups],
  );
  const groupRowIndexes = useMemo(
    () =>
      new Map(
        displayRows.flatMap((row, index) =>
          row.kind === 'group' ? [[row.group.id, index] as const] : [],
        ),
      ),
    [displayRows],
  );
  const rowVirtualizer = useVirtualizer({
    count: displayRows.length,
    getScrollElement: () => matrixScroll.current,
    estimateSize: (index) => (displayRows[index]?.kind === 'group' ? 44 : 58),
    overscan: 8,
  });
  const selectProjectGroup = useCallback((groupId: string) => {
    const index = groupRowIndexes.get(groupId);
    if (index !== undefined) rowVirtualizer.scrollToIndex(index, { align: 'start' });
  }, [groupRowIndexes, rowVirtualizer]);
  const toggleProblems = useCallback(() => {
    setFilter((current) => current === 'all' ? 'problems' : 'all');
  }, []);
  const projectSidebarState = useMemo(() => ({
    groups: displayGroupList.map((group) => ({
      id: group.id,
      name: group.name,
      keyCount: group.keys.length,
      problemCount: problemCounts.get(group.id) ?? 0,
      hidden: filter === 'problems' && group.keys.every((key) => !filteredKeyIDs.has(key.id)),
    })),
    problemCount: problems.length,
    problemsActive: filter === 'problems',
    onSelectGroup: selectProjectGroup,
    onToggleProblems: toggleProblems,
  }), [
    displayGroupList,
    filter,
    filteredKeyIDs,
    problemCounts,
    problems.length,
    selectProjectGroup,
    toggleProblems,
  ]);
  useProjectSidebar(projectSidebarState);
  const visibleEnvironments = environments.filter((environment) =>
    visibleEnvironmentIds.includes(environment.id),
  );
  const pendingByEnvironment = useMemo(() => {
    const pending = new Map<string, readonly MatrixPendingEntry[]>();
    for (const row of environmentRows) {
      const rows: MatrixPendingEntry[] = [];
      for (const signal of row.signals.data?.cells ?? []) {
        if (signal.pending !== undefined) {
          rows.push({
            versionId: signal.pending.versionId,
            keyId: signal.key_id,
            name: signal.name,
            classification: signal.classification,
            operation: signal.pending.operation,
            configPreview: pendingConfigPreview(signal, draftsByVersion),
          });
        }
      }
      pending.set(row.environmentId, rows);
    }
    return pending;
  }, [draftsByVersion, environmentRows]);
  const pendingCount = [...pendingByEnvironment.values()].reduce(
    (total, entries) => total + entries.length,
    0,
  );
  const revisionsByEnvironment = useMemo<ReadonlyMap<string, bigint>>(() => {
    const revisions = new Map<string, bigint>();
    for (const row of environmentRows) {
      const revision = row.signals.data?.revision;
      if (revision !== undefined) {
        revisions.set(row.environmentId, revision);
      }
    }
    return revisions;
  }, [environmentRows]);
  const protectedEnvironmentIds = environmentRows.flatMap((row) =>
    row.settings.data?.protected === true ? [row.environmentId] : [],
  );
  const pendingCountByEnvironment = useMemo<ReadonlyMap<string, number>>(
    () =>
      new Map(
        [...pendingByEnvironment.entries()].map(([environmentId, entries]) => [
          environmentId,
          entries.length,
        ]),
      ),
    [pendingByEnvironment],
  );
  const pendingByOthersByEnvironment = useMemo<ReadonlyMap<string, number>>(() => {
    const counts = new Map<string, number>();
    for (const row of environmentRows) {
      counts.set(
        row.environmentId,
        (row.signals.data?.cells ?? []).filter((cell) => cell.pending_by_others).length,
      );
    }
    return counts;
  }, [environmentRows]);
  // The history drawer needs the CURRENT cell state to enumerate the ceremony
  // unit a restore will need: the comparison opens current secret plaintext only
  // where a set secret is being replaced.
  const cellsByEnvironment = useMemo<ReadonlyMap<string, readonly HistoryCurrentCell[]>>(() => {
    const cells = new Map<string, readonly HistoryCurrentCell[]>();
    for (const environment of environments) {
      cells.set(
        environment.id,
        keys.map((key) => ({
          keyId: key.id,
          classification: key.classification,
          set: valuesByCell.get(cellID(key.id, environment.id))?.set === true,
        })),
      );
    }
    return cells;
  }, [environments, keys, valuesByCell]);
  const currentValuesByEnvironment = useMemo<
    ReadonlyMap<string, readonly ValueCell[]>
  >(() => {
    const values = new Map<string, readonly ValueCell[]>();
    for (const row of environmentRows) {
      values.set(row.environmentId, row.values.data?.items ?? []);
    }
    return values;
  }, [environmentRows]);
  const loading =
    matrix.environments.isPending ||
    matrix.keys.isPending ||
    matrix.groups.isPending ||
    environmentRows.some((row) => row.readiness === 'pending');
  const loadError =
    (matrix.environments.isError && matrix.environments.data === undefined) ||
    (matrix.keys.isError && matrix.keys.data === undefined) ||
    (matrix.groups.isError && matrix.groups.data === undefined) ||
    environmentRows.some((row) => row.readiness === 'error');
  const backgroundRefreshError =
    (matrix.environments.isError && matrix.environments.data !== undefined) ||
    (matrix.keys.isError && matrix.keys.data !== undefined) ||
    (matrix.groups.isError && matrix.groups.data !== undefined) ||
    environmentRows.some((row) => row.readiness === 'stale');
  const virtualRows = rowVirtualizer.getVirtualItems();
  const virtualPaddingTop = virtualRows[0]?.start ?? 0;
  const virtualPaddingBottom =
    rowVirtualizer.getTotalSize() - (virtualRows[virtualRows.length - 1]?.end ?? 0);

  const clearValidation = (keyId: string, environmentId: string) =>
    setValidationErrors((current) =>
      current.filter(
        (error) => error.keyId !== keyId || error.environmentId !== environmentId,
      ),
    );
  const recordValidation = (keyId: string, environmentId: string, message: string) =>
    setValidationErrors((current) => [
      ...current.filter(
        (error) => error.keyId !== keyId || error.environmentId !== environmentId,
      ),
      { keyId, environmentId, message },
    ]);

  const publishSelected = (selectedEnvironmentIds: readonly string[]) => {
    const addressedEnvironment = selectedEnvironmentIds[0];
    if (addressedEnvironment === undefined) {
      throw new Error('publish action has no selected environment');
    }
    const selectedVersionIds = selectedEnvironmentIds.flatMap((environmentId) =>
      (pendingByEnvironment.get(environmentId) ?? []).map((entry) => entry.versionId),
    );
    if (selectedVersionIds.length === 0) {
      throw new Error('publish action has no selected draft versions');
    }
    publish.mutate(
      {
        addressedEnvironment,
        environmentIds: selectedEnvironmentIds,
        versionIds: selectedVersionIds,
      },
      {
        onSuccess: (result) => {
          setValidationErrors((current) =>
            current.filter(
              (error) => !selectedEnvironmentIds.includes(error.environmentId),
            ),
          );
          const revisions = result.environments.map((published) => {
            const environment = environments.find(
              (candidate) => candidate.id === published.environment_id,
            );
            return `${environment?.name ?? published.environment_id} r${String(published.revision)}`;
          });
          setPublishOpen(false);
          setNotice(`Published atomically: ${revisions.join(', ')}. Signals updated.`);
        },
        onError: (error) => {
          const validation = matrixPublishValidation(error, keys, selectedEnvironmentIds);
          if (validation !== null) {
            recordValidation(
              validation.keyId,
              validation.environmentId,
              validation.message,
            );
          }
        },
      },
    );
  };

  /**
   * declareKey runs the two-phase create journey (#492): the declaration, then
   * its optional opening values. It is reused verbatim by the Surface-2 override
   * (#183) — a blocked declaration retries through the SAME path with the
   * findings' acknowledgement tokens, so the override cannot drift from the
   * first attempt.
   */
  const declareKey = async (
    payload: MatrixKeyCreatePayload,
    acknowledgements: readonly string[] = [],
  ): Promise<void> => {
    // Fail closed: if the settings read resolved to git-managed after the modal
    // opened (the entry buttons vanish, but an open form could still submit),
    // refuse before any write rather than trust a stale gate.
    if (gitManaged) {
      setCreate(null);
      return;
    }
    setCreateError(null);
    // Phase 1 — declaration. A failure keeps the modal open with its fields
    // intact (rethrow), because nothing was created yet.
    let created: Awaited<ReturnType<typeof createKey.mutateAsync>>;
    try {
      created = await createKey.mutateAsync({
        name: payload.name,
        classification: payload.classification,
        rule: payload.rule,
        folderPath: payload.folderPath,
        description: payload.description,
        required: payload.required,
        forbidden: payload.forbidden,
        ...(acknowledgements.length === 0 ? {} : { acknowledgements }),
      });
    } catch (error) {
      // Surface-2 scanner block (#183): route to the block dialog, which shows
      // ONLY the redacted findings and offers an audited override. The value the
      // operator typed is never passed there. The modal closes so the two
      // dialogs never stack — a blocked declaration is a review moment.
      if (error instanceof ApiError && error.findings.length > 0) {
        const findings = error.findings;
        // A declaration block is a phase-1 failure: nothing was created, so the
        // create modal STAYS OPEN behind the block dialog. Closing the block
        // without overriding returns the operator to their intact form to edit
        // the flagged material — never a rebuild from scratch.
        setCreateError(null);
        setScanBlock({
          title: 'Declaration blocked by secret scanning',
          intro: `Declaring ${payload.name} was refused: it looks like it carries secret material.`,
          findings,
          onOverride: findings.every((finding) => finding.acknowledgement !== undefined)
            ? (tokens) => declareKey(payload, tokens)
            : null,
        });
        throw error;
      }
      setCreateError(
        matrixMutationError(
          error instanceof Error ? error : new Error('key declaration failed'),
          'create',
        ),
      );
      throw error;
    }
    // Phase 2 — opening values. The key now exists, so both dialogs close; a
    // value that fails to stage is reported against the declared key rather than
    // implying the declaration itself failed.
    setCreate(null);
    setScanBlock(null);
    let stagedCount = 0;
    const failedEnvironments: string[] = [];
    const warnItems: ScanWarnItem[] = [];
    const blocked: {
      readonly environmentId: string;
      readonly environmentName: string;
      readonly findings: readonly RefusalFinding[];
    }[] = [];
    const normalizedValue =
      payload.firstValue === null ? '' : normalizeMatrixDraftValue(payload.firstValue.value);
    if (payload.firstValue !== null) {
      for (const environmentId of payload.firstValue.environmentIds) {
        const environmentName =
          environments.find((candidate) => candidate.id === environmentId)?.name ?? environmentId;
        try {
          const result = await stage.mutateAsync({
            environment: environmentId,
            key: payload.name,
            value: normalizedValue,
          });
          stagedCount += 1;
          // Surface-1 warn (#74) is a CONFIG-only affordance: it holds the
          // value's canonical bytes for the keep-as-config re-stage. A secret
          // never enters that path, so if a secret write ever returns findings
          // we fail closed — the plaintext is not retained or shown, and the
          // finding is left to the server's own secret handling.
          if (payload.classification === 'config') {
            for (const finding of result.findings ?? []) {
              warnItems.push({ environmentId, environmentName, value: normalizedValue, finding });
            }
          }
        } catch (error) {
          // Surface-2 block (#183): findings ride the refusal. Route them to the
          // block dialog — which shows only the redacted findings, never the
          // value — rather than the vague "could not be staged" line.
          if (error instanceof ApiError && error.findings.length > 0) {
            blocked.push({ environmentId, environmentName, findings: error.findings });
          } else {
            failedEnvironments.push(environmentName);
          }
        }
      }
    }
    const staged =
      stagedCount === 0
        ? ''
        : ` with a draft value in ${String(stagedCount)} environment${stagedCount === 1 ? '' : 's'}`;
    const failedNote =
      failedEnvironments.length === 0
        ? ''
        : ` Its opening value could not be staged in ${failedEnvironments.join(', ')} — set it from the cell.`;
    const blockedNote =
      blocked.length === 0
        ? ''
        : ` Its opening value was blocked by secret scanning in ${blocked.map((entry) => entry.environmentName).join(', ')} — review below.`;
    setNotice(`Declared ${payload.name}${staged}.${failedNote}${blockedNote}`);
    // A Surface-2 block takes the dialog; a Surface-1 warn (which needs a
    // succeeded save) only shows when nothing was blocked, so the two modals
    // never stack. The warned value is still reachable from its cell.
    if (blocked.length > 0) {
      const allFindings = blocked.flatMap((entry) => entry.findings);
      const overridable = allFindings.every((finding) => finding.acknowledgement !== undefined);
      setScanBlock({
        title: 'Opening value blocked by secret scanning',
        intro: `${payload.name} was declared, but its opening value looks like it carries secret material.`,
        findings: allFindings,
        onOverride: overridable
          ? async () => {
              const stillFailed: string[] = [];
              for (const entry of blocked) {
                try {
                  await stage.mutateAsync({
                    environment: entry.environmentId,
                    key: payload.name,
                    value: normalizedValue,
                    acknowledgements: entry.findings
                      .map((finding) => finding.acknowledgement)
                      .filter((token): token is string => token !== undefined),
                  });
                } catch {
                  stillFailed.push(entry.environmentName);
                }
              }
              setNotice(
                stillFailed.length === 0
                  ? `Staged the opening value for ${payload.name} after review.`
                  : `Some opening values for ${payload.name} still could not be staged: ${stillFailed.join(', ')}.`,
              );
            }
          : null,
      });
    } else if (warnItems.length > 0) {
      setWarn({ keyId: created.id, keyName: payload.name, items: warnItems });
    }
  };

  if (loading) {
    return <p role="status">Loading environment matrix…</p>;
  }
  if (loadError) {
    return (
      <p className="alert" role="alert">
        <span className="alert__glyph" aria-hidden="true">!</span>
        <span>The environment matrix could not be loaded. Reload to try again.</span>
      </p>
    );
  }

  const selectedKey = selection === null ? undefined : keys.find((key) => key.id === selection.keyId);
  const selectedEnvironment =
    selection === null
      ? undefined
      : selection.environmentId === undefined
        ? environments[0]
        : environments.find((environment) => environment.id === selection.environmentId);
  return (
    <>
    <section
      className="matrix"
      aria-labelledby="matrix-title"
      inert={(historyOpen || keyDetailOpen) && mobileLayout}
    >
      {/* env-matrix 31 trims the head to the essentials: the legend is a `?`
          icon and drafts surface as a pill only once there is something to
          publish. The level-1 heading stays — every surface carries one (see
          shell.spec) — but its restated key/env count is gone; the matrix says
          that itself. */}
      <div className="matrix__head">
        <h1 id="matrix-title">Environment matrix</h1>
        <span className="matrix__head-spacer" />
        <MatrixLegend />
        {pendingCount === 0 ? null : (
          <button
            type="button"
            className="matrix__drafts"
            aria-expanded={publishOpen}
            aria-controls="matrix-publish"
            onClick={() => setPublishOpen((open) => !open)}
          >
            {`Δ ${String(pendingCount)} unpublished edit${pendingCount === 1 ? '' : 's'} · publish…`}
          </button>
        )}
        {/* #495: import a .env file. Value import is not git-gated, so the entry
            stays available on a git-managed project; the wizard skips new keys
            there. Needs at least one environment to target. */}
        {environments.length > 0 ? (
          <button
            type="button"
            className="btn matrix__import"
            onClick={() => setImportOpen(true)}
          >
            Import .env
          </button>
        ) : null}
        {/* env-matrix 31 / #492: the header's primary declare action. Git-managed
            projects disable it and say why — value actions still work. */}
        {environments.length > 0 && !gitManaged ? (
          <button
            type="button"
            className="btn btn--primary matrix__new-key"
            onClick={() => {
              setCreateError(null);
              setCreate({ folder: null });
            }}
          >
            + New key
          </button>
        ) : null}
      </div>

      {gitManaged ? (
        <p className="notice" role="status">
          <span aria-hidden="true">ℹ</span>
          <span>{GIT_DEFINITIONS_NOTICE}</span>
        </p>
      ) : null}

      {notice === null ? null : (
        <p className="notice" role="status">
          <span aria-hidden="true">✓</span>
          <span>{notice}</span>
        </p>
      )}

      {backgroundRefreshError ? (
        <p className="alert" role="status">
          <span className="alert__glyph" aria-hidden="true">!</span>
          <span>Live matrix refresh failed. Your loaded data and open edits are preserved; retrying automatically.</span>
        </p>
      ) : null}

      {publishOpen ? (
        <MatrixPublishSheet
          refData={ref}
          environments={environments}
          revisions={revisionsByEnvironment}
          pendingByEnvironment={pendingByEnvironment}
          problems={problems}
          protectedEnvironmentIds={protectedEnvironmentIds}
          busy={publish.isPending}
          mutationError={
            publish.isError
              ? matrixMutationError(
                  publish.error,
                  'publish',
                  restorePreviewWasAttached(publish.error),
                )
              : null
          }
          onPublish={publishSelected}
        />
      ) : null}

      <div className="matrix__layout">
        <div className="matrix__surface">
          {filter === 'problems' ? (
            <div className="matrix__filter" role="status">
              <span>{`⚠ filter active: problems — showing ${String(filteredKeyIDs.size)} of ${String(keys.length)} keys`}</span>
              <button type="button" className="btn" onClick={() => setFilter('all')}>
                ✕ show all keys
              </button>
            </div>
          ) : null}

          {environments.length === 0 ? (
            <div className="matrix__empty" role="status">
              <h2>No environments yet</h2>
              <p>
                A matrix needs at least one environment before any key can hold a value. Add one
                under{' '}
                <Link
                  to={generatePath(surfaceById('project-settings').path, {
                    org: ref.org,
                    project: ref.project,
                  })}
                >
                  Project settings › New environment
                </Link>
                .
              </p>
            </div>
          ) : keys.length === 0 ? (
            <div className="matrix__empty" role="status">
              <h2>No keys yet</h2>
              <p>
                Declare a key once, give each environment its own explicit value — the matrix shows
                the whole configuration surface at a glance.
              </p>
              {gitManaged ? (
                <p role="status">{GIT_DEFINITIONS_NOTICE}</p>
              ) : (
                <div className="matrix__empty-actions">
                  <button
                    type="button"
                    className="btn btn--primary"
                    onClick={() => {
                      setCreateError(null);
                      setCreate({ folder: null });
                    }}
                  >
                    Declare first key
                  </button>
                </div>
              )}
              <p>
                {gitManaged
                  ? 'Keys are declared in Git. Scaffold from an existing file, then apply:'
                  : 'Or import every key from an existing file through the CLI, then apply:'}
              </p>
              <pre className="matrix__cli">
                <code>{'hikyo definitions scaffold --from .env\nhikyo definitions apply'}</code>
              </pre>
            </div>
          ) : filter === 'problems' && filteredKeyIDs.size === 0 ? (
            <div className="matrix__empty" role="status">
              <h2>No problems</h2>
              <p>Every readable environment satisfies its required values.</p>
              <button type="button" className="btn btn--primary" onClick={() => setFilter('all')}>
                Show all keys
              </button>
            </div>
          ) : (
            <div className="matrix__scroll" ref={matrixScroll}>
              <table className="matrix__table" ref={matrixTable}>
                <thead ref={setMatrixHead}>
                  <tr>
                    <th scope="col">
                      <div className="matrix__key-heading">
                        <span>Key</span>
                        <details className="matrix__environment-picker">
                          <summary className="btn">
                            {`envs ${String(visibleEnvironments.length)}/${String(environments.length)} ▾`}
                          </summary>
                          <fieldset>
                            <legend>Visible environments</legend>
                            {environments.map((environment) => {
                              const checked = visibleEnvironmentIds.includes(environment.id);
                              return (
                                <label key={environment.id}>
                                  <input
                                    type="checkbox"
                                    checked={checked}
                                    disabled={checked && visibleEnvironmentIds.length === 1}
                                    onChange={() =>
                                      setVisibleEnvironmentIds((current) =>
                                        toggleVisibleEnvironment(
                                          current,
                                          environment.id,
                                          environments.map((candidate) => candidate.id),
                                        ),
                                      )
                                    }
                                  />
                                  <span>{environment.name}</span>
                                  {protectedEnvironmentIds.includes(environment.id) ? (
                                    <span className="matrix__protected">PROTECTED</span>
                                  ) : null}
                                </label>
                              );
                            })}
                          </fieldset>
                        </details>
                      </div>
                    </th>
                    {visibleEnvironments.map((environment) => {
                      const revision = revisionsByEnvironment.get(environment.id);
                      return (
                        <th scope="col" key={environment.id}>
                          <span>{environment.name}</span>
                          {/* DESIGN.md: the protected state is named in text, in
                              the header — a column you cannot reveal from should
                              say so before you try, not after the refusal. */}
                          {protectedEnvironmentIds.includes(environment.id) ? (
                            <span className="matrix__protected">PROTECTED</span>
                          ) : null}
                          {revision === undefined ? null : (
                            <Link
                              className="btn matrix__history-link"
                              data-history-environment={environment.id}
                              to={historyLink({ ...ref, env: environment.id })}
                              onClick={(event: MouseEvent<HTMLAnchorElement>) => {
                                historyOpener.current = event.currentTarget;
                              }}
                            >
                              {`rev ${String(revision)} · history`}
                            </Link>
                          )}
                        </th>
                      );
                    })}
                  </tr>
                </thead>
                <tbody>
                  {virtualPaddingTop > 0 ? (
                    <tr aria-hidden="true" className="matrix__virtual-spacer">
                      <td
                        colSpan={visibleEnvironments.length + 1}
                        style={{ height: virtualPaddingTop }}
                      />
                    </tr>
                  ) : null}
                  {virtualRows.map((virtualRow) => {
                    const row = displayRows[virtualRow.index];
                    if (row === undefined) return null;
                    if (row.kind === 'group') {
                      const { group } = row;
                      const collapsed = collapsedGroups.has(group.id);
                      const count = problemCounts.get(group.id) ?? 0;
                      return (
                        <tr
                          className="matrix__group-row"
                          key={`group-${group.id}`}
                          data-index={virtualRow.index}
                          ref={rowVirtualizer.measureElement}
                        >
                          <th colSpan={visibleEnvironments.length + 1}>
                            <div className="matrix__group-row-inner">
                              <button
                                type="button"
                                className="matrix__group-toggle"
                                id={groupDOMID(group.id)}
                                aria-expanded={!collapsed}
                                onClick={() =>
                                  setCollapsedGroups((current) => {
                                    const next = new Set(current);
                                    if (next.has(group.id)) next.delete(group.id);
                                    else next.add(group.id);
                                    return next;
                                  })
                                }
                              >
                                <span className="matrix__group-chevron" aria-hidden="true">
                                  ▾
                                </span>
                                <span>{group.name}</span>
                                <span>{String(group.keys.length)}</span>
                                {count === 0 ? null : (
                                  <span className="matrix__problem-count count">
                                    {`! ${String(count)} problem${count === 1 ? '' : 's'}`}
                                  </span>
                                )}
                                {collapsed ? (
                                  <span className="matrix__group-summary mono">
                                    {group.keys.map((key) => key.name).join(', ')}
                                  </span>
                                ) : null}
                              </button>
                              {/* env-matrix 31: declare a key straight into this
                                  group. Hidden while collapsed to match the
                                  prototype — you open a group, then add to it. */}
                              {collapsed || gitManaged ? null : (
                                <button
                                  type="button"
                                  className="matrix__add-key"
                                  onClick={() => {
                                    setCreateError(null);
                                    // Prefill the actual folder_path, not the
                                    // display name — an explicit key-group's name
                                    // can differ from its folder, and the
                                    // ungrouped pseudo-group has none (→ blank).
                                    setCreate({ folder: group.keys[0]?.folder_path || null });
                                  }}
                                >
                                  + key
                                </button>
                              )}
                            </div>
                          </th>
                        </tr>
                      );
                    }
                    const { key } = row;
                    return (
                      <tr
                        className={`matrix__key-row${row.alt ? ' matrix__key-row--alt' : ''}`}
                        key={key.id}
                        data-index={virtualRow.index}
                        ref={rowVirtualizer.measureElement}
                      >
                        <th scope="row" title={key.name}>
                          {/* The key NAME opens its declaration detail (#491):
                              the routable, reload-safe catalogue surface that
                              inspects every declaration field and hosts the
                              shared editor foundation. Per-key revision history
                              stays one gesture deeper, from inside the detail.
                              Any CELL still opens the row editor. */}
                          <Link
                            className="matrix__key mono"
                            aria-label={`Declaration of ${key.name}`}
                            to={keyDetailLink(key.id)}
                            onClick={(event: MouseEvent<HTMLAnchorElement>) => {
                              keyDetailOpener.current = event.currentTarget;
                            }}
                          >
                            {key.classification === 'secret' ? <span aria-hidden="true">🔒 </span> : null}
                            {key.name}
                          </Link>
                          <span className="matrix__required">{requiredLabel(key, environments)}</span>
                        </th>
                        {visibleEnvironments.map((environment) => {
                          const id = cellID(key.id, environment.id);
                          return (
                            <td key={environment.id}>
                              <MatrixCell
                                cell={valuesByCell.get(id)}
                                keyRecord={key}
                                environment={environment}
                                signal={signalsByCell.get(id)}
                                problems={problemsByCell.get(id) ?? []}
                                onOpen={() =>
                                  setSelection({ keyId: key.id, environmentId: environment.id })
                                }
                              />
                            </td>
                          );
                        })}
                      </tr>
                    );
                  })}
                  {virtualPaddingBottom > 0 ? (
                    <tr aria-hidden="true" className="matrix__virtual-spacer">
                      <td
                        colSpan={visibleEnvironments.length + 1}
                        style={{ height: virtualPaddingBottom }}
                      />
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      {selection === null || selectedKey === undefined || selectedEnvironment === undefined ? null : (
        <MatrixRowEditor
          refData={ref}
          keyRecord={selectedKey}
          environmentId={selectedEnvironment.id}
          rows={environmentRows.map((row: MatrixEnvironmentRow) => {
            const signal = signalsByCell.get(cellID(selectedKey.id, row.environmentId));
            return {
              environmentId: row.environmentId,
              environment: row.environment,
              protected: row.settings.data?.protected === true,
              cell: valuesByCell.get(cellID(selectedKey.id, row.environmentId)),
              signal,
              draftPreview: pendingConfigPreview(signal, draftsByVersion),
              problems:
                problemsByCell.get(cellID(selectedKey.id, row.environmentId)) ?? [],
            };
          })}
          busy={stage.isPending || clear.isPending || copy.isPending}
          mutationError={mutationError?.owner === selection ? mutationError.message : null}
          onClose={() => {
            setMutationError(null);
            setSelection(null);
          }}
          onApply={async (changes) => {
            setMutationError(null);
            let normalizedCount = 0;
            const warnItems: ScanWarnItem[] = [];
            for (const change of changes) {
              try {
                if (change.operation === 'set') {
                  const normalizedValue = normalizeMatrixDraftValue(change.value);
                  if (normalizedValue !== change.value) normalizedCount += 1;
                  const staged = await stage.mutateAsync({
                    environment: change.environmentId,
                    key: selectedKey.name,
                    value: normalizedValue,
                  });
                  // Surface-1 warn (#74): the save succeeded; any findings ride
                  // the response bound to the value that produced them (the
                  // canonical, post-normalization bytes the token dismisses).
                  const environmentName =
                    environments.find((candidate) => candidate.id === change.environmentId)?.name ??
                    change.environmentId;
                  for (const finding of staged.findings ?? []) {
                    warnItems.push({
                      environmentId: change.environmentId,
                      environmentName,
                      value: normalizedValue,
                      finding,
                    });
                  }
                } else {
                  await clear.mutateAsync({
                    environment: change.environmentId,
                    key: selectedKey.name,
                  });
                }
                clearValidation(selectedKey.id, change.environmentId);
              } catch (error) {
                setMutationError({
                  owner: selection,
                  message: matrixMutationError(
                    error instanceof Error ? error : new Error('matrix mutation failed'),
                    change.operation === 'set' ? 'stage' : 'clear',
                  ),
                });
                throw error;
              }
            }
            setNotice(
              `${String(changes.length)} draft${changes.length === 1 ? '' : 's'} updated for ${selectedKey.name}.${normalizedCount === 0 ? '' : ` Leading and trailing whitespace was removed from ${String(normalizedCount)} value${normalizedCount === 1 ? '' : 's'}.`}`,
            );
            setSelection(null);
            if (warnItems.length > 0) {
              setWarn({ keyId: selectedKey.id, keyName: selectedKey.name, items: warnItems });
            }
          }}
          onCopy={(destinations, confirmProtected) => {
            setMutationError(null);
            copy.mutate(
              {
                sourceEnvironment: selectedEnvironment.id,
                key: selectedKey.name,
                destinationEnvironments: destinations,
                confirmProtected,
              },
              {
                onSuccess: () => {
                  setMutationError(null);
                  setNotice(
                    `${selectedKey.name} copied to ${String(destinations.length)} environment${destinations.length === 1 ? '' : 's'}.`,
                  );
                  setSelection(null);
                },
                onError: (error) => setMutationError({
                  owner: selection,
                  message: matrixMutationError(error, 'copy'),
                }),
              },
            );
          }}
        />
      )}

      {create === null ? null : (
        <MatrixKeyCreate
          folders={[...new Set(keys.map((key) => key.folder_path).filter((path) => path !== ''))]}
          environments={environments}
          protectedEnvironmentIds={protectedEnvironmentIds}
          initialFolder={create.folder}
          existingKeyNames={keys.map((key) => key.name)}
          busy={createKey.isPending || stage.isPending}
          mutationError={createError}
          onClose={() => {
            setCreateError(null);
            setCreate(null);
          }}
          onCreate={declareKey}
        />
      )}

      {!importOpen ? null : (
        <ImportWizard
          matrixRef={ref}
          environments={environments.map((environment) => ({
            id: environment.id,
            name: environment.name,
          }))}
          gitManaged={gitManaged}
          onClose={() => setImportOpen(false)}
        />
      )}

      {scanBlock === null ? null : (
        <ScanBlockDialog
          title={scanBlock.title}
          intro={scanBlock.intro}
          findings={scanBlock.findings}
          onOverride={scanBlock.onOverride}
          onClose={() => setScanBlock(null)}
        />
      )}

      {warn === null ? null : (
        <ScanWarnDialog
          keyName={warn.keyName}
          items={warn.items}
          onClose={() => setWarn(null)}
          onReclassify={async () => {
            await reclassify.mutateAsync({ key: warn.keyId, classification: 'secret' });
          }}
          onDismiss={async (item) => {
            const staged = await stage.mutateAsync({
              environment: item.environmentId,
              key: warn.keyName,
              value: item.value,
              acknowledgements:
                item.finding.acknowledgement === undefined
                  ? undefined
                  : [item.finding.acknowledgement],
            });
            return (staged.findings ?? []).map((finding) => ({
              environmentId: item.environmentId,
              environmentName: item.environmentName,
              value: item.value,
              finding,
            }));
          }}
        />
      )}

    </section>

      {historyOpen ? (
        <HistoryDrawer
          refData={ref}
          environments={environments}
          keys={keys}
          currentRevisions={revisionsByEnvironment}
          protectedEnvironmentIds={protectedEnvironmentIds}
          cellsByEnvironment={cellsByEnvironment}
          pendingByEnvironment={pendingCountByEnvironment}
          pendingByOthersByEnvironment={pendingByOthersByEnvironment}
          currentValuesByEnvironment={currentValuesByEnvironment}
          openerRef={historyOpener}
        />
      ) : null}

      {keyDetailId === undefined ? null : (
        <KeyDeclarationDetail
          refData={ref}
          keyId={keyDetailId}
          environments={environments}
          impact={keyDetailImpact}
          impactReady={keyDetailImpactReady}
          openerRef={keyDetailOpener}
        />
      )}
    </>
  );
}

function MatrixCell({
  cell,
  keyRecord,
  environment,
  signal,
  problems,
  onOpen,
}: {
  cell: ValueCell | undefined;
  keyRecord: MatrixKey;
  environment: Environment;
  signal: MatrixSignalCell | undefined;
  problems: readonly { readonly kind: string; readonly message: string }[];
  onOpen: () => void;
}) {
  const requiredProblem = problems.find((problem) => problem.kind === 'required-absent');
  const validationProblem = problems.find((problem) => problem.kind === 'validation');
  let state = '· absent';
  let stateClass = 'matrix-cell--absent';
  if (requiredProblem !== undefined) {
    state = '! required · absent';
    stateClass = 'matrix-cell--problem';
  } else if (validationProblem !== undefined) {
    // Name the offending value, not just the fact of one. Reading a column of
    // "value problem" tells you where to click; reading `✕ ten` tells you what
    // happened. `offendingValue` is absent for anything the caller may not
    // read, so a secret stays a secret in its own failure.
    state = `✕ ${offendingValue(cell, keyRecord) ?? 'value problem'}`;
    stateClass = 'matrix-cell--problem';
  } else if (cell?.set === true && keyRecord.classification === 'secret') {
    state = '••••••••';
    stateClass = 'matrix-cell--secret';
  } else if (cell?.set === true) {
    state = cell.value ?? 'set';
    stateClass = 'matrix-cell--set';
  }
  // env-matrix 31 fixes the changed/draft vocabulary to bare marks, not
  // sentences: a set cell carries a `Δ` when it changed since publish and a
  // draft dot when it holds an unpublished edit. The revision and the set/clear
  // sense move to the mark's tooltip and the accessible label, off the row.
  const draftSense =
    signal?.pending === undefined
      ? null
      : signal.pending.operation === 'unset'
        ? 'clear'
        : 'set';
  const changedRevision = signal?.changed_in_revision;
  const otherDraft = signal?.pending_by_others === true;
  const signalWords = [
    draftSense === null ? null : `unpublished draft ${draftSense}`,
    changedRevision === undefined ? null : `changed in r${String(changedRevision)}`,
    otherDraft ? 'another editor has a draft here' : null,
  ].filter((word): word is string => word !== null);
  const label = `${keyRecord.name} in ${environment.name}: ${state}${signalWords.length === 0 ? '' : `, ${signalWords.join(', ')}`}`;

  return (
    <>
      <button
        type="button"
        className={`matrix-cell ${stateClass}`}
        aria-label={label}
        onClick={onOpen}
      >
        <span className="matrix-cell__value">{state}</span>
        {changedRevision === undefined ? null : (
          <span
            className="matrix-cell__delta"
            aria-hidden="true"
            title={`changed in r${String(changedRevision)}`}
          >
            Δ
          </span>
        )}
        {draftSense === null ? null : (
          <span
            className="matrix-cell__draft-dot"
            aria-hidden="true"
            title={`unpublished draft — ${draftSense}`}
          />
        )}
        {otherDraft ? (
          <span
            className="matrix-cell__other"
            aria-hidden="true"
            title="another editor has a draft here"
          >
            ◌
          </span>
        ) : null}
      </button>
      {validationProblem === undefined ? null : (
        <span className="matrix-cell__error">{validationProblem.message}</span>
      )}
    </>
  );
}

/**
 * MatrixLegend says what the cell vocabulary means.
 *
 * The matrix is dense on purpose, and density is bought with abbreviation: `·`,
 * `••••••••`, `Δ` and `✕` are all shorter than the sentences they replace. That
 * trade is only honest if the expansion is one gesture away on the surface
 * itself — a reader who has to leave to find out what a glyph means has been
 * handed a puzzle, not a table.
 *
 * A `<details>`, like the environment chooser beside it: the platform already
 * owns the disclosure, the escape key and the accessible name.
 */
function MatrixLegend() {
  return (
    <details className="matrix__legend">
      <summary
        className="btn matrix__legend-toggle"
        aria-label="what the cells mean"
        title="what the cells mean"
      >
        ?
      </summary>
      <div className="matrix__legend-body">
        <dl>
          <dt className="mono">value</dt>
          <dd>set in that environment — nothing inherits</dd>
          <dt className="mono">••••••••</dt>
          <dd>a secret is set; 🔒 marks the key. Open the cell to reveal it, if permitted</dd>
          <dt className="mono">· absent</dt>
          <dd>not set here, so nothing is delivered</dd>
          <dt className="mono">! required · absent</dt>
          <dd>required in this environment and absent — publish is blocked</dd>
          <dt className="mono">✕ value</dt>
          <dd>the value is set but fails its declaration</dd>
          <dt className="mono">Δ</dt>
          <dd>changed since the last publish</dd>
          <dt>
            <span className="matrix-cell__draft-dot" aria-hidden="true" />
            <span className="visually-hidden">draft dot</span>
          </dt>
          <dd>an unpublished draft of your own</dd>
          <dt className="mono">◌</dt>
          <dd>another editor has a draft here</dd>
        </dl>
        <p>Choose any cell to inspect or edit it.</p>
      </div>
    </details>
  );
}

/** How much of a rejected value a cell shows before the CSS ellipsis takes over. */
const OFFENDING_VALUE_CHARS = 42;

/**
 * offendingValue is the rejected value as a cell may print it, or null when the
 * cell must not print one.
 *
 * A secret never qualifies, whatever the wire happens to be carrying: the
 * matrix is not a disclosure surface, and a validation failure is not a reason
 * to become one. JSON is collapsed onto one line first, because a pretty-printed
 * object in a table row is a column of whitespace.
 */
function offendingValue(cell: ValueCell | undefined, keyRecord: MatrixKey): string | null {
  if (keyRecord.classification === 'secret') return null;
  const value = cell?.value;
  if (value === undefined || value === '') return null;
  const flat = value.replace(/\s+/g, ' ').trim();
  if (flat === '') return null;
  return flat.length > OFFENDING_VALUE_CHARS
    ? `${flat.slice(0, OFFENDING_VALUE_CHARS)}…`
    : flat;
}

function cellID(keyId: string, environmentId: string): string {
  return `${keyId}/${environmentId}`;
}

function matrixPresence(key: MatrixKey): MatrixPresence {
  const presence = key.presence.required_in;
  if (presence.mode === 'all') return { mode: 'all' };
  if (presence.mode === 'none') return { mode: 'none' };
  return { mode: 'explicit', environmentIds: presence.environment_ids ?? [] };
}

function displayGroupID(key: MatrixKey): string {
  if (key.group_id !== '') return `group:${key.group_id}`;
  return `folder:${key.folder_path === '' ? 'ungrouped' : key.folder_path}`;
}

function displayGroups(
  keys: readonly MatrixKey[],
  groups: readonly { readonly id: string; readonly name: string }[],
): readonly DisplayGroup[] {
  const names = new Map(groups.map((group) => [`group:${group.id}`, group.name]));
  const result = new Map<string, MatrixKey[]>();
  for (const key of keys) {
    const id = displayGroupID(key);
    const entries = result.get(id) ?? [];
    entries.push(key);
    result.set(id, entries);
  }
  return [...result].map(([id, members]) => ({
    id,
    name: names.get(id) ?? (members[0]?.folder_path || 'ungrouped'),
    keys: members,
  }));
}

function groupDOMID(groupId: string): string {
  return `matrix-group-${groupId.replace(/[^A-Za-z0-9_-]/g, '-')}`;
}

function requiredLabel(key: MatrixKey, environments: readonly Environment[]): string {
  const required = environments.filter((environment) =>
    requiredInEnvironment(matrixPresence(key), environment.id),
  );
  // env-matrix 31 keeps the required marker terse and inline beside the key —
  // `req` when it is required everywhere, `req · <envs>` when only in some.
  if (required.length === 0) return '';
  if (required.length === environments.length) return 'req';
  return `req · ${required.map((environment) => environment.name).join(', ')}`;
}
