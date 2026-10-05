import { listKeysOp, listOrgRulesOp, listProjectRulesOp, replaceRulesOp } from '@hikyo/operations';
import type { zRuleList } from '@hikyo/zod';
import { useMutation, useQueries, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { commonRefusalText, statusText } from './statusText.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { createBody, savePlan, type Key, type Rule } from '../routes/accessRules/model.ts';
import { ApiError, parsed, transportRefusalText } from './client.ts';

/**
 * Member access rules (member-access-rules ADR) as the Members surface reads
 * and writes them. The server stores one capability per rule; the page edits
 * the set sharing one Where. Each card change is committed atomically.
 */

type RuleList = z.infer<typeof zRuleList>;

const rulesKey = (org: string, project: string) => ['rules', org, project] as const;

/**
 * The org's rules, or one project's part of them. The project listing needs
 * only manage-members on that project, and marks a rule that also names other
 * projects with `other_projects`.
 */
export function useRules(org: string, project: string, enabled = true): UseQueryResult<RuleList> {
  return useQuery({
    queryKey: rulesKey(org, project),
    queryFn: () => (project === '' ? parsed(listOrgRulesOp, { path: { org } }) : parsed(listProjectRulesOp, { path: { org, project } })),
    enabled: enabled && org !== '',
  });
}

/**
 * Each project's key catalogue: names, folders and classification, and
 * nothing with a value member (`listKeys`, never `listValues`, like the grant
 * warning). It needs See on the project; a project the caller may not read
 * answers null, and the page says so rather than guessing.
 */
export function useKeyCatalogues(org: string, projects: readonly string[]): { readonly keys: ReadonlyMap<string, readonly Key[] | null>; readonly isPending: boolean } {
  const results = useQueries({
    queries: projects.map((project) => ({
      queryKey: ['key-catalogue', org, project] as const,
      queryFn: () => parsed(listKeysOp, { path: { org, project } }),
      enabled: org !== '',
    })),
  });
  const keys = new Map<string, readonly Key[] | null>();
  projects.forEach((project, index) => {
    const result = results[index];
    keys.set(
      project,
      result?.data === undefined
        ? null
        : result.data.items.map((k) => ({ id: k.id, name: k.name, folder: k.folder_path, secret: k.classification === 'secret' })),
    );
  });
  return { keys, isPending: results.some((r) => r.isPending) };
}

/** A dispatched rule mutation whose complete effect cannot be confirmed. */
export class RuleSaveFailure extends Error {
  override readonly cause: unknown;

  constructor(
    readonly stage: 'atomic' | 'create' | 'rollback' | 'revoke' | 'remove' | 'refresh',
    cause: unknown,
    readonly confirmedRevoked: readonly string[] = [],
  ) {
    super(`rule save failed at ${stage}`, { cause });
    this.name = 'RuleSaveFailure';
    this.cause = cause;
  }
}

/** Save one card with no intermediate permission union. */
export async function saveRule(org: string, before: Rule | null, draft: Rule): Promise<void> {
  const plan = savePlan(before, draft);
  if (plan.create.length === 0 && plan.revoke.length === 0) return;
  try {
    await parsed(replaceRulesOp, { path: { org }, body: {
      principal: draft.member,
      revoke: [...plan.revoke],
      create: plan.create.map((capability) => createBody(draft, capability)),
    } });
  } catch (error) {
    throw new RuleSaveFailure('atomic', error);
  }
}

/** Remove the complete card in one transaction. */
export async function removeRule(org: string, rule: Rule): Promise<void> {
  if (rule.source.kind !== 'rule') return;
  try {
    await parsed(replaceRulesOp, { path: { org }, body: {
      principal: rule.member, revoke: rule.source.parts.map((part) => part.id), create: [],
    } });
  } catch (error) {
    throw new RuleSaveFailure('atomic', error);
  }
}

/** Save and remove, refreshing the listings and this session (a rule change can end the holder's sessions). */
export function useRuleMutations(org: string) {
  const auth = useAuth();
  const queries = useQueryClient();
  const settle = async (_result: void, failure: Error | null) => {
    try {
      // The session owner invalidates all queries both before and after
      // whoami. A concurrent third refetch can inherit a canceled retryer and
      // report uncertainty even when the authoritative listing succeeded.
      // Settle the owner first, then confirm this listing before enabling edits.
      try {
        await auth.refreshSession();
      } finally {
        // An owner refusal is not evidence that the write did not commit.
        // Still retire the old listing; the outer catch retains the refusal.
        await queries.invalidateQueries({ queryKey: ['rules', org] }, { throwOnError: true });
      }
    } catch (error) {
      // The mutation already failed and its caller must abandon the draft.
      // Do not rethrow inside TanStack's error-settlement callback, which
      // reports callback failures as unhandled rejections instead of replacing
      // the original mutation error. The query/session owners retain refusal.
      if (failure !== null) return;
      throw new RuleSaveFailure('refresh', error);
    }
  };
  return {
    save: useMutation({ mutationFn: (input: { before: Rule | null; draft: Rule }) => saveRule(org, input.before, input.draft), onSettled: settle }),
    remove: useMutation({ mutationFn: (rule: Rule) => removeRule(org, rule), onSettled: settle }),
  };
}

function refusalText(error: unknown): string {
  return statusText(
    error,
    {
      400: (error) =>
        error.detail ?? 'The server refused this rule: check where it applies and what it gives.',
      401: commonRefusalText.sessionEnded,
      403: 'Managing access needs a second factor. Sign in again and present your passkey or a code, then retry.',
      404: 'You do not manage access on every project this rule names, or something it names no longer exists. The two are deliberately the same answer.',
      409: (error) => error.detail ?? 'The server refused this change as it stands.',
      429: commonRefusalText.attempts,
    },
    transportRefusalText(error) ?? 'The rules could not be reached, or the answer did not match the contract. Whether the change applied is unknown: reload to check.',
    (error) =>
      `The server failed (${error.status}); whether the change applied is unknown: reload to check.`,
  );
}

/** A save or remove failure in words, saying which half of an edit stands. */
export function ruleFailureText(error: unknown): string {
  if (error instanceof RuleSaveFailure) {
    const progress = error.confirmedRevoked.length === 1
      ? '1 rule part was confirmed removed.'
      : `${error.confirmedRevoked.length} rule parts were confirmed removed.`;
    switch (error.stage) {
      case 'atomic':
        return `The atomic change could not be confirmed. Check the refreshed rules before retrying; a lost response may follow a committed change. ${refusalText(error.cause)}`;
      case 'create':
        return `The save could not be confirmed. A new rule may still apply even if confirmed additions were undone. Check the refreshed rules and reopen the editor before retrying. ${refusalText(error.cause)}`;
      case 'rollback':
        return `Some new rules may still apply because rollback could not finish. ${progress} Check the refreshed rules and reopen the editor before retrying. ${refusalText(error.cause)}`;
      case 'revoke':
      case 'remove':
        return `Removal stopped. ${progress} Other removals could not be confirmed. Check the refreshed rules and reopen the editor before retrying. ${refusalText(error.cause)}`;
      case 'refresh':
        return `The requests completed, but the current rules or session could not be confirmed. Reload and check the rules before making another change. ${refusalText(error.cause)}`;
    }
  }
  return refusalText(error);
}

/** Why the rule listing is not shown, in the membership listing's words. */
export function rulesListingText(error: unknown): string {
  if (error instanceof ApiError && error.status === 404) return 'You hold no manage-members here, so access rules are not shown.';
  if (error instanceof ApiError && error.status === 403) return 'Access rules need a second factor. Present your authenticator code or passkey, then reload.';
  return refusalText(error);
}
