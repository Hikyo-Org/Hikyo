import { createRuleOp, listKeysOp, listOrgRulesOp, listProjectRulesOp, revokeRuleOp } from '@hikyo/operations';
import type { zRuleList } from '@hikyo/zod';
import { useMutation, useQueries, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { useAuth } from '../app/AuthProvider.tsx';
import { createBody, savePlan, type Key, type Rule } from '../routes/accessRules/model.ts';
import { ApiError, ok, parsed, transportRefusalText } from './client.ts';

/**
 * Member access rules (member-access-rules ADR) as the Members surface reads
 * and writes them. The server stores one capability per rule; the page edits
 * the set sharing one Where, so a save is a short sequence of creates and
 * revokes (see `savePlan`). There is no rule update on the server: an edit is
 * a create plus a revoke.
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

/** A save that created part of a rule and could not finish: which half stands. */
export class RuleSaveFailure extends Error {
  override readonly cause: unknown;

  constructor(
    readonly stage: 'create' | 'rollback' | 'revoke',
    cause: unknown,
  ) {
    super(`rule save failed at ${stage}`, { cause });
    this.name = 'RuleSaveFailure';
    this.cause = cause;
  }
}

/**
 * saveRule makes the server's rules match the draft. Creates come first: when
 * one is refused, the ones already created are revoked again, so the member's
 * access is what it was. Only then are the replaced rows revoked.
 */
export async function saveRule(org: string, before: Rule | null, draft: Rule): Promise<void> {
  const plan = savePlan(before, draft);
  const created: string[] = [];
  try {
    for (const capability of plan.create) {
      created.push((await parsed(createRuleOp, { path: { org }, body: createBody(draft, capability) })).id);
    }
  } catch (error) {
    try {
      for (const rule of created) await ok(revokeRuleOp, { path: { org, rule } });
    } catch {
      throw new RuleSaveFailure('rollback', error);
    }
    throw new RuleSaveFailure('create', error);
  }
  try {
    for (const rule of plan.revoke) await ok(revokeRuleOp, { path: { org, rule } });
  } catch (error) {
    throw new RuleSaveFailure('revoke', error);
  }
}

/** Revoke every server rule a rule is made of. */
export async function removeRule(org: string, rule: Rule): Promise<void> {
  if (rule.source.kind !== 'rule') return;
  for (const part of rule.source.parts) await ok(revokeRuleOp, { path: { org, rule: part.id } });
}

/** Save and remove, refreshing the listings and this session (a rule change can end the holder's sessions). */
export function useRuleMutations(org: string) {
  const auth = useAuth();
  const queries = useQueryClient();
  const settle = () => Promise.all([queries.invalidateQueries({ queryKey: ['rules', org] }), auth.refreshSession()]);
  return {
    save: useMutation({ mutationFn: (input: { before: Rule | null; draft: Rule }) => saveRule(org, input.before, input.draft), onSettled: settle }),
    remove: useMutation({ mutationFn: (rule: Rule) => removeRule(org, rule), onSettled: settle }),
  };
}

function refusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return error.detail ?? 'The server refused this rule: check where it applies and what it gives.';
      case 401:
        return 'Your session ended. Sign in again to continue.';
      case 403:
        return 'Managing access needs a second factor. Sign in again and present your passkey or a code, then retry.';
      case 404:
        return 'You do not manage access on every project this rule names, or something it names no longer exists. The two are deliberately the same answer.';
      case 409:
        return error.detail ?? 'The server refused this change as it stands.';
      case 429:
        return 'Too many attempts right now. Wait a moment and try again.';
      default:
        return `The server failed (${error.status}); whether the change applied is unknown: reload to check.`;
    }
  }
  return transportRefusalText(error) ?? 'The rules could not be reached, or the answer did not match the contract. Whether the change applied is unknown: reload to check.';
}

/** A save or remove failure in words, saying which half of an edit stands. */
export function ruleFailureText(error: unknown): string {
  if (error instanceof RuleSaveFailure) {
    switch (error.stage) {
      case 'create':
        return `Nothing changed: ${refusalText(error.cause)}`;
      case 'rollback':
        return `Part of the new rule was saved and could not be undone: reload and check this member's rules. ${refusalText(error.cause)}`;
      case 'revoke':
        return `The new rule is saved, but the rule it replaces could not be removed, so both apply: remove the old one. ${refusalText(error.cause)}`;
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
