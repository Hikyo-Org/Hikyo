import type { KeyDeclaration, KeyPresenceRules, WideningRefusal } from '@hikyo/client';
import {
  createFolderOp,
  createKeyGroupOp,
  deleteFolderOp,
  deleteKeyGroupOp,
  listFoldersOp,
  listKeyGroupsOp,
  renameFolderOp,
  renameKeyGroupOp,
  setKeyGroupOp,
  updateKeyDeclarationOp,
  updateKeyMetadataOp,
} from '@hikyo/operations';
import { zFolderList, zKeyGroupList } from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import type { z } from 'zod';

import { ApiError, ok, parsed, type RefusalFinding } from './client.ts';
import { callerSafeRefusal } from './history.ts';
import {
  foldersKey,
  matrixGroupsKey,
  matrixKeyKey,
  matrixKeysKey,
  type MatrixRef,
} from './keys.ts';
import { keyMetadataRefusalText } from './matrix.ts';
import { useTransport, useWorkspaceContext } from './transport.tsx';
import { isCurrentWorkspaceSession, workspaceSession, WorkspaceError } from './workspace.ts';

/**
 * The declaration-catalogue writes #493 adds on top of #491's read/metadata
 * foundation (`useKey`, `useUpdateKeyMetadata` in matrix.ts): value-rule and
 * presence edits, key rename/delete and group membership, and folder and
 * key-group lifecycle.
 *
 * Every write here is a Surface-2 scanning chokepoint (#74/#183), a rule
 * change, a folder or group name, so its request carries `acknowledgements`
 * and its refusal can arrive with redacted findings on `ApiError.findings`. No
 * secret value is ever read to edit a declaration: the whole surface builds on
 * `key.get` and the list endpoints alone.
 */

export type { KeyDeclaration, KeyPresenceRules } from '@hikyo/client';
export type Folder = z.infer<typeof zFolderList>['items'][number];
export type KeyGroup = z.infer<typeof zKeyGroupList>['items'][number];

type Acknowledgeable = { readonly acknowledgements?: readonly string[] };

function ackBody(input: Acknowledgeable): { acknowledgements?: string[] } {
  return input.acknowledgements === undefined || input.acknowledgements.length === 0
    ? {}
    : { acknowledgements: [...input.acknowledgements] };
}

/**
 * scanFindings returns the Surface-2 findings a refusal carries, or null when
 * the error is anything else. `ApiError.findings` is always an array (empty for
 * a non-scanner refusal), so an empty list reads as null and callers branch
 * cleanly. A 404 NEVER opens a findings dialog even if one somehow rode it, 
 * that would leak existence through the uniform missing-resource mask, matching
 * the foundation's own `scanBlockFrom` guard.
 */
export function scanFindings(error: unknown): readonly RefusalFinding[] | null {
  if (error instanceof ApiError && error.status !== 404 && error.findings.length > 0) {
    return error.findings;
  }
  return null;
}

export type CatalogueAction =
  | 'update the rules'
  | 'change the linked keys'
  | 'create the folder'
  | 'rename the folder'
  | 'delete the folder'
  | 'create the linked-key set'
  | 'rename the linked-key set'
  | 'delete the linked-key set';

/**
 * catalogueRefusalText renders a catalogue-write refusal in the caller's own
 * words. A caller-safe server detail (a presence veto, an invalid rule, a
 * duplicate path) is shown verbatim; the uniform 403/404/409 refusals fall back
 * to fixed sentences. A scanner block is handled by the caller through
 * {@link scanFindings}, never here.
 */
export function catalogueRefusalText(error: unknown, action: CatalogueAction): string {
  if (error instanceof ApiError) {
    const detailed = callerSafeRefusal(error, 'Refused');
    if (detailed !== null) {
      return detailed;
    }
    if (error.status === 403) {
      return `You do not have permission to ${action} in this project.`;
    }
    if (error.status === 404) {
      return 'This no longer exists: reload the catalogue to see its current state.';
    }
    if (error.status === 409) {
      return `The server refused this change to avoid overwriting a concurrent edit; reload and ${action} again.`;
    }
    return `The server could not ${action} (error ${String(error.status)}).`;
  }
  return `The server could not ${action}.`;
}

/** Every cache a declaration change on one key can move. */
function invalidateKey(
  queries: ReturnType<typeof useQueryClient>,
  ref: MatrixRef,
  key: string,
): Promise<unknown> {
  return Promise.all([
    queries.invalidateQueries({ queryKey: matrixKeyKey(ref, key) }),
    queries.invalidateQueries({ queryKey: matrixKeysKey(ref) }),
    queries.invalidateQueries({ queryKey: matrixGroupsKey(ref) }),
    queries.invalidateQueries({ queryKey: foldersKey(ref) }),
  ]);
}

export function useUpdateKeyDeclaration(ref: MatrixRef, key: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (
      input: Acknowledgeable & {
        readonly declaration: KeyDeclaration;
        readonly presence: KeyPresenceRules;
      },
    ) =>
      parsed(updateKeyDeclarationOp, {
        path: { ...ref, key },
        body: { declaration: input.declaration, presence: input.presence, ...ackBody(input) },
        ...transport,
      }),
    onSuccess: () => invalidateKey(queries, ref, key),
  });
}

/**
 * `id` is the key's immutable id (the PATCH target); `name` is for messages.
 * `confirmWidening` resends a move the server refused as widening access
 * (ADR D9), naming exactly the people that refusal named.
 */
export type FolderMove = {
  readonly id: string;
  readonly name: string;
  readonly folder: string;
  readonly confirmWidening?: readonly string[];
};
/** `widening` rides a move refused because it gives people new access
 *  through their access rules, so the caller can confirm that key alone. */
export type FolderMoveOutcome = {
  readonly id: string;
  readonly error: string | null;
  readonly widening?: WideningRefusal;
};

/**
 * useMoveKeysToFolders is the write behind the matrix "Cleanup" dry run: it
 * moves each key to its chosen folder with one metadata PATCH per key,
 * creating any folder row the project does not have yet so the folder list in
 * the lifecycle dialog matches what the matrix shows.
 *
 * The moves run SEQUENTIALLY, deliberately. Each move is a schema revision
 * charged against the project's per-hour revision budget (§151), so the loop
 * stops at the first 429 and reports the rest as not attempted rather than
 * burning the budget on requests that will fail the same way. Any other
 * refusal is recorded for that key and the loop goes on: a folder move is
 * metadata, reversible, and one bad key must not block the others. A move
 * that widens access is recorded with its widening so the caller can confirm
 * that key and resend it with `confirmWidening`.
 *
 * ponytail: one revision per key caps a cleanup at the hourly budget (60);
 * a server-side bulk move (one revision for N keys) is the upgrade path.
 */
export function useMoveKeysToFolders(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  const workspace = useWorkspaceContext();
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => { live.current = false; };
  }, []);
  return useMutation({
    mutationFn: async (input: {
      readonly moves: readonly FolderMove[];
      readonly existingFolders: readonly string[];
    }): Promise<readonly FolderMoveOutcome[]> => {
      const initiatingSession = workspace === null ? undefined : workspaceSession(workspace.origin);
      const assertCurrent = (): void => {
        if (!live.current || (workspace !== null && (initiatingSession === undefined || !isCurrentWorkspaceSession(initiatingSession)))) {
          throw new WorkspaceError('This cleanup stopped because its workspace session ended or was replaced. Run Cleanup again after reconnecting.');
        }
      };
      assertCurrent();
      const known = new Set(input.existingFolders);
      const outcomes: FolderMoveOutcome[] = [];
      let exhausted: string | null = null;
      for (const move of input.moves) {
        assertCurrent();
        if (exhausted !== null) {
          outcomes.push({ id: move.id, error: exhausted });
          continue;
        }
        try {
          if (move.folder !== '' && !known.has(move.folder)) {
            await createFolderTolerant(transport, ref, move.folder);
            assertCurrent();
            known.add(move.folder);
          }
        } catch (error) {
          assertCurrent();
          outcomes.push({ id: move.id, error: catalogueRefusalText(error, 'create the folder') });
          continue;
        }
        try {
          await parsed(updateKeyMetadataOp, {
            path: { ...ref, key: move.id },
            body: {
              folder_path: move.folder,
              ...(move.confirmWidening === undefined || move.confirmWidening.length === 0
                ? {}
                : { confirm_widening: [...move.confirmWidening] }),
            },
            ...transport,
          });
          assertCurrent();
          outcomes.push({ id: move.id, error: null });
        } catch (error) {
          assertCurrent();
          // Only the PATCH is a schema revision, so only its 429 is the
          // budget; a throttled folder create is reported as that.
          if (error instanceof ApiError && error.status === 429) {
            exhausted = REVISION_BUDGET_REFUSAL;
            outcomes.push({ id: move.id, error: exhausted });
            continue;
          }
          if (error instanceof ApiError && error.status === 409 && error.widening !== undefined) {
            outcomes.push({ id: move.id, error: WIDENING_REFUSAL, widening: error.widening });
            continue;
          }
          outcomes.push({
            id: move.id,
            error: error instanceof Error ? keyMetadataRefusalText(error) : 'The server could not save this key.',
          });
        }
      }
      return outcomes;
    },
    onSettled: () =>
      Promise.all([
        queries.invalidateQueries({ queryKey: matrixKeysKey(ref) }),
        queries.invalidateQueries({ queryKey: matrixGroupsKey(ref) }),
        invalidateFolders(queries, ref),
      ]),
  });
}

const WIDENING_REFUSAL = 'Not moved: this move gives people new access through their access rules.';

const REVISION_BUDGET_REFUSAL =
  'Not moved: the project\'s hourly schema-revision budget is used up. Run Cleanup again later for the remaining keys.';

/** Create a folder row, treating "already exists" (409) as done. */
async function createFolderTolerant(
  transport: ReturnType<typeof useTransport>,
  ref: MatrixRef,
  path: string,
): Promise<void> {
  try {
    await parsed(createFolderOp, { path: ref, body: { path }, ...transport });
  } catch (error) {
    if (error instanceof ApiError && error.status === 409) return;
    throw error;
  }
}

export function useSetKeyGroup(ref: MatrixRef, key: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { readonly groupId: string }) =>
      parsed(setKeyGroupOp, {
        path: { ...ref, key },
        body: { group_id: input.groupId },
        ...transport,
      }),
    onSuccess: () => invalidateKey(queries, ref, key),
  });
}

export function useFolders(ref: MatrixRef): UseQueryResult<z.infer<typeof zFolderList>> {
  const transport = useTransport();
  return useQuery({
    queryKey: foldersKey(ref),
    queryFn: () => parsed(listFoldersOp, { path: ref, ...transport }),
    enabled: ref.org !== '' && ref.project !== '',
  });
}

export function useKeyGroups(ref: MatrixRef): UseQueryResult<z.infer<typeof zKeyGroupList>> {
  const transport = useTransport();
  return useQuery({
    queryKey: matrixGroupsKey(ref),
    queryFn: () => parsed(listKeyGroupsOp, { path: ref, ...transport }),
    enabled: ref.org !== '' && ref.project !== '',
  });
}

function invalidateFolders(
  queries: ReturnType<typeof useQueryClient>,
  ref: MatrixRef,
): Promise<unknown> {
  return Promise.all([
    queries.invalidateQueries({ queryKey: foldersKey(ref) }),
    queries.invalidateQueries({ queryKey: matrixKeysKey(ref) }),
  ]);
}

export function useCreateFolder(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: Acknowledgeable & { readonly path: string }) =>
      parsed(createFolderOp, {
        path: ref,
        body: { path: input.path, ...ackBody(input) },
        ...transport,
      }),
    onSuccess: () => invalidateFolders(queries, ref),
  });
}

export function useRenameFolder(ref: MatrixRef, folder: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: Acknowledgeable & { readonly path: string }) =>
      parsed(renameFolderOp, {
        path: { ...ref, folder },
        body: { path: input.path, ...ackBody(input) },
        ...transport,
      }),
    onSuccess: () => invalidateFolders(queries, ref),
  });
}

export function useDeleteFolder(ref: MatrixRef, folder: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: () => ok(deleteFolderOp, { path: { ...ref, folder }, ...transport }),
    onSuccess: () => invalidateFolders(queries, ref),
  });
}

function invalidateGroups(
  queries: ReturnType<typeof useQueryClient>,
  ref: MatrixRef,
): Promise<unknown> {
  return Promise.all([
    queries.invalidateQueries({ queryKey: matrixGroupsKey(ref) }),
    queries.invalidateQueries({ queryKey: matrixKeysKey(ref) }),
  ]);
}

export function useCreateKeyGroup(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: Acknowledgeable & { readonly name: string }) =>
      parsed(createKeyGroupOp, {
        path: ref,
        body: { name: input.name, ...ackBody(input) },
        ...transport,
      }),
    onSuccess: () => invalidateGroups(queries, ref),
  });
}

export function useRenameKeyGroup(ref: MatrixRef, group: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: Acknowledgeable & { readonly name: string }) =>
      parsed(renameKeyGroupOp, {
        path: { ...ref, group },
        body: { name: input.name, ...ackBody(input) },
        ...transport,
      }),
    onSuccess: () => invalidateGroups(queries, ref),
  });
}

export function useDeleteKeyGroup(ref: MatrixRef, group: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: () => ok(deleteKeyGroupOp, { path: { ...ref, group }, ...transport }),
    onSuccess: () => invalidateGroups(queries, ref),
  });
}

// --- presence impact (criterion 3: affected environments before commit) -----

export type PresenceMode = KeyPresenceRules['required_in']['mode'];

export type PresenceRuleLike = {
  readonly mode: PresenceMode;
  readonly environment_ids?: readonly string[] | undefined;
};
export type PresenceRulesLike = {
  readonly required_in: PresenceRuleLike;
  readonly forbidden_in: PresenceRuleLike;
};

/**
 * environmentsUnder resolves a presence rule to the concrete environment id set
 * it covers: `all` is symbolic and covers every environment, `none` covers
 * nothing, `explicit` covers its listed ids intersected with those that exist.
 */
export function environmentsUnder(
  rule: PresenceRuleLike,
  environmentIds: readonly string[],
): readonly string[] {
  switch (rule.mode) {
    case 'all':
      return environmentIds;
    case 'none':
      return [];
    case 'explicit': {
      const listed = new Set(rule.environment_ids ?? []);
      return environmentIds.filter((id) => listed.has(id));
    }
  }
}

export type PresenceImpact = {
  readonly requiredAdded: readonly string[];
  readonly requiredRemoved: readonly string[];
  readonly forbiddenAdded: readonly string[];
  readonly forbiddenRemoved: readonly string[];
};

/** presenceImpact is the before/after of a presence edit as concrete env sets. */
export function presenceImpact(
  before: PresenceRulesLike,
  after: PresenceRulesLike,
  environmentIds: readonly string[],
): PresenceImpact {
  const requiredBefore = new Set(environmentsUnder(before.required_in, environmentIds));
  const requiredAfter = new Set(environmentsUnder(after.required_in, environmentIds));
  const forbiddenBefore = new Set(environmentsUnder(before.forbidden_in, environmentIds));
  const forbiddenAfter = new Set(environmentsUnder(after.forbidden_in, environmentIds));
  return {
    requiredAdded: environmentIds.filter((id) => requiredAfter.has(id) && !requiredBefore.has(id)),
    requiredRemoved: environmentIds.filter((id) => requiredBefore.has(id) && !requiredAfter.has(id)),
    forbiddenAdded: environmentIds.filter((id) => forbiddenAfter.has(id) && !forbiddenBefore.has(id)),
    forbiddenRemoved: environmentIds.filter(
      (id) => forbiddenBefore.has(id) && !forbiddenAfter.has(id),
    ),
  };
}

export function presenceImpactIsEmpty(impact: PresenceImpact): boolean {
  return (
    impact.requiredAdded.length === 0 &&
    impact.requiredRemoved.length === 0 &&
    impact.forbiddenAdded.length === 0 &&
    impact.forbiddenRemoved.length === 0
  );
}
