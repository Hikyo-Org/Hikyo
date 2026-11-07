import {
  deleteMyPersonalValueOp,
  deletePersonalValueMemberOp,
  getPersonalOffboardingReportOp,
  listMyPersonalValuesOp,
  listPersonalValueCountsOp,
  listKeysOp,
  listPersonalValueMembersOp,
  revealMyPersonalValueOp,
  setMyPersonalValueOp,
} from '@hikyo/operations';
import type {
  PersonalOffboardingReport,
  PersonalValueCounts,
  PersonalValueEntry,
  PersonalValueHolder,
  PersonalValueList,
  PersonalValueNoticeCause,
} from '@hikyo/client';
import { useMutation, useQueries, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { useMemo } from 'react';

import { ok, parsed } from './client.ts';
import { matrixKeysKey, type EnvRef, type MatrixRef } from './keys.ts';
import { commonRefusalText, statusText } from './statusText.ts';
import { useSensitiveMutation } from './sensitiveMutation.ts';
import { useTransport } from './transport.tsx';

/**
 * Personal values (ADR personal-values.md): one value per developer on a
 * shared environment. The owner lists, sets, reveals and deletes their own;
 * a `manage-members` holder sees counts and who holds one, and can delete it
 * (leaving a notice), never read or overwrite it. No route here returns
 * another person's value, and there is deliberately no `.env` download (O7).
 */

export type { PersonalValueEntry, PersonalValueHolder, PersonalOffboardingReport, PersonalValueCounts };

/** The ADR's fixed statement, shown wherever a personal value is entered. */
export const PERSONAL_STATEMENT =
  'Hikyo does not show this to administrators. Your instance operator could read it, and anyone who can change shared values here could capture it once your program runs. If someone gave you your password, authenticator or recovery codes, replace them, regenerate your recovery codes and check your linked identities first.';

/** Why a value disappeared, in words. One table, used by every surface. */
export const NOTICE_CAUSE_TEXT: Record<PersonalValueNoticeCause, string> = {
  administrator: 'an administrator deleted it',
  retention: 'it was deleted after 14 days without access',
  restore: 'a restore removed every personal value',
  key_renamed: 'the key was renamed',
  key_deleted: 'the key was deleted',
  forbidden: 'the key became forbidden in this environment',
  qualification_lost: 'your account lost its authenticator app or passkey',
};

const personalKey = (env: EnvRef): readonly string[] => ['personal', env.org, env.project, env.environment];
const countsKey = (env: EnvRef): readonly string[] => ['personal-counts', env.org, env.project, env.environment];
const membersKey = (env: EnvRef, key: string): readonly string[] => ['personal-members', env.org, env.project, env.environment, key];

function envPath(env: EnvRef) {
  return { org: env.org, project: env.project, environment: env.environment };
}

/** The caller's own values, notices and checklist, masked. */
export function usePersonalValues(env: EnvRef): UseQueryResult<PersonalValueList> {
  const transport = useTransport();
  return useQuery({
    queryKey: personalKey(env),
    queryFn: () => parsed(listMyPersonalValuesOp, { path: envPath(env), ...transport }),
    enabled: env.environment !== '',
  });
}

/** The project's declared keys, shared with the matrix's cache: the keys a personal value may be set for. */
export function useDeclaredKeys(env: EnvRef) {
  const transport = useTransport();
  const ref = { org: env.org, project: env.project };
  return useQuery({
    queryKey: matrixKeysKey(ref),
    queryFn: () => parsed(listKeysOp, { path: ref, ...transport }),
    enabled: env.org !== '' && env.project !== '',
  });
}

/** Set or replace the caller's own value. The plaintext never enters a cache. */
export function useSetPersonalValue(env: EnvRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useSensitiveMutation({
    mutationFn: (input: { key: string; value: string }) =>
      parsed(setMyPersonalValueOp, {
        path: { ...envPath(env), key: input.key },
        body: { value: input.value },
        ...transport,
      }),
    onSettled: () => invalidatePersonal(queries, env),
  });
}

/** Reveal the caller's own value. One-shot: the result is never retained by the cache. */
export function useRevealPersonalValue(env: EnvRef) {
  const transport = useTransport();
  return useSensitiveMutation({
    mutationFn: (key: string) =>
      parsed(revealMyPersonalValueOp, { path: { ...envPath(env), key }, ...transport }),
  });
}

/** Delete the caller's own value, or dismiss its notice. */
export function useDeletePersonalValue(env: EnvRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (key: string) => ok(deleteMyPersonalValueOp, { path: { ...envPath(env), key }, ...transport }),
    onSettled: () => invalidatePersonal(queries, env),
  });
}

/** Who holds a value or notice for one key here: metadata only, `manage-members`. */
export function usePersonalMembers(env: EnvRef, key: string): UseQueryResult<PersonalValueHolder[]> {
  const transport = useTransport();
  return useQuery({
    queryKey: membersKey(env, key),
    queryFn: async () =>
      (await parsed(listPersonalValueMembersOp, { path: { ...envPath(env), key }, ...transport })).items,
    enabled: env.environment !== '' && key !== '',
  });
}

/** Delete someone's value. They keep a notice, so `hikyo run` refuses the key until they act. */
export function useDeletePersonalMember(env: EnvRef, key: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (member: string) =>
      ok(deletePersonalValueMemberOp, { path: { ...envPath(env), key, member }, ...transport }),
    onSettled: async () => {
      await queries.invalidateQueries({ queryKey: membersKey(env, key) });
      await invalidatePersonal(queries, env);
    },
  });
}

/** The offboarding report for one member of the project: names only. */
export function useOffboardingReport(ref: MatrixRef, member: string): UseQueryResult<PersonalOffboardingReport> {
  const transport = useTransport();
  return useQuery({
    queryKey: ['personal-offboarding', ref.org, ref.project, member],
    queryFn: () => parsed(getPersonalOffboardingReportOp, { path: { ...ref, member }, ...transport }),
    enabled: ref.org !== '' && ref.project !== '' && member !== '',
  });
}

async function invalidatePersonal(queries: ReturnType<typeof useQueryClient>, env: EnvRef) {
  await Promise.all([
    queries.invalidateQueries({ queryKey: personalKey(env) }),
    queries.invalidateQueries({ queryKey: countsKey(env) }),
  ]);
}

/** One matrix mark: short visible text plus the words the accessible name carries. */
export type PersonalMark = { readonly text: string; readonly words: string };

/**
 * The mark a `manage-members` holder sees: counts, never values. A required
 * key reads `personal · 11/14` (holders of eligible), any other key with
 * holders `3 personal`. Notices add `1 notice`.
 */
export function countsMark(item: PersonalValueCounts['items'][number], eligible: number): PersonalMark | null {
  const parts: string[] = [];
  const words: string[] = [];
  if (item.required) {
    parts.push(`personal · ${String(item.holders)}/${String(eligible)}`);
    words.push(`personal value required, ${String(item.holders)} of ${String(eligible)} people hold one`);
  } else if (item.holders > 0) {
    parts.push(`${String(item.holders)} personal`);
    words.push(`${String(item.holders)} ${item.holders === 1 ? 'person holds' : 'people hold'} a personal value`);
  }
  if (item.notices > 0) {
    parts.push(`${String(item.notices)} ${item.notices === 1 ? 'notice' : 'notices'}`);
    words.push(`${String(item.notices)} ${item.notices === 1 ? 'removal notice' : 'removal notices'}`);
  }
  return parts.length === 0 ? null : { text: parts.join(' · '), words: words.join(', ') };
}

/** The mark a developer sees: only their own state for the key. */
export function ownMark(entry: PersonalValueEntry): PersonalMark | null {
  if (entry.suspended === true) {
    return { text: 'personal · suspended', words: 'your personal value is suspended and not delivered' };
  }
  if (entry.notice !== undefined) {
    return {
      text: 'personal · removed',
      words: `your personal value was removed: ${NOTICE_CAUSE_TEXT[entry.notice]}; hikyo run refuses this key until you set a value or dismiss the notice`,
    };
  }
  if (entry.set) {
    return { text: 'personal', words: 'your personal value is used for you' };
  }
  if (entry.required === true) {
    return { text: 'personal · missing', words: 'a personal value is required here and you have not set one' };
  }
  return null;
}

/**
 * The personal marks for every matrix cell, keyed `keyId/environmentId`.
 *
 * Counts answer only a `manage-members` holder. The caller's own list is read
 * only for an environment whose counts read failed or was refused, so a manager
 * costs one request per environment, not two. Both families are separate from
 * the matrix's readiness: a failed read means no mark, never a degraded column.
 */
export function usePersonalMarks(
  ref: MatrixRef,
  environmentIds: readonly string[],
): ReadonlyMap<string, PersonalMark> {
  const transport = useTransport();
  const counts = useQueries({
    queries: environmentIds.map((environment) => ({
      queryKey: countsKey({ ...ref, environment }),
      queryFn: () => parsed(listPersonalValueCountsOp, { path: { ...ref, environment }, ...transport }),
      retry: false,
    })),
    combine: dataOrFailure,
  });
  const ownData = useQueries({
    queries: environmentIds.map((environment, index) => ({
      queryKey: personalKey({ ...ref, environment }),
      queryFn: () => parsed(listMyPersonalValuesOp, { path: { ...ref, environment }, ...transport }),
      retry: false,
      // Mirrors the marks below: counts with data win, so only a failed counts read needs the own list.
      enabled: counts[index]?.failed === true,
    })),
    combine: dataOf,
  });
  return useMemo(() => {
    const marks = new Map<string, PersonalMark>();
    environmentIds.forEach((environment, index) => {
      const managed = counts[index]?.data;
      if (managed !== undefined) {
        for (const item of managed.items) {
          const mark = countsMark(item, managed.eligible);
          if (mark !== null) marks.set(`${item.key_id}/${environment}`, mark);
        }
        return;
      }
      for (const entry of ownData[index]?.items ?? []) {
        const mark = ownMark(entry);
        if (mark !== null && entry.key_id !== undefined) marks.set(`${entry.key_id}/${environment}`, mark);
      }
    });
    return marks;
  }, [counts, environmentIds, ownData]);
}

/** Stable `combine`: only the data, so an unchanged family keeps its identity across renders. */
function dataOf<T>(results: readonly { readonly data: T | undefined }[]): readonly (T | undefined)[] {
  return results.map((result) => result.data);
}

/** Stable `combine`: the data, and whether the read failed with none to show. */
function dataOrFailure<T>(
  results: readonly { readonly data: T | undefined; readonly isError: boolean }[],
): readonly { readonly data: T | undefined; readonly failed: boolean }[] {
  return results.map((result) => ({ data: result.data, failed: result.isError && result.data === undefined }));
}

/**
 * The schema warning for the owner's value, from a set or reveal response:
 * the rules it does not satisfy, by name, never the value. A warning only; the
 * value is kept and delivered (personal-values ADR). Null when there is none.
 */
export function personalWarningText(key: string, warnings: readonly string[] | undefined): string | null {
  if (warnings === undefined || warnings.length === 0) return null;
  const rules = warnings.length === 1 ? 'this rule' : 'these rules';
  return `Your value for ${key} does not satisfy ${rules} of the key: ${warnings.join(', ')}. This is a warning only: the value is kept and still delivered to you.`;
}

/**
 * Why a set failed, with the remedy. The 403 body is uniform on the wire, so
 * the cause is read from what this client knows about the account and the
 * approval it just used.
 */
export function personalSetRefusalText(
  error: unknown,
  facts: { readonly hasFactor: boolean; readonly federatedSession: boolean; readonly protectedEnvironment: boolean },
): string {
  return statusText(error, {
    400: (e) => e.detail ?? 'The server refused this value as invalid.',
    401: commonRefusalText.sessionEnded,
    403: () => {
      if (facts.protectedEnvironment) return 'Personal values are for unprotected environments only.';
      if (!facts.hasFactor) {
        return 'Personal values need an authenticator app or passkey on your account. Add one under Settings › Security, then set the value again.';
      }
      return facts.federatedSession
        ? 'Personal values take a passkey or authenticator app approval. An identity provider approval cannot set one. Approve again with your passkey or a code from your authenticator app.'
        : 'The server refused this value. Approve again with your passkey or a code from your authenticator app; if it is refused again, your access here may have changed.';
    },
    404: 'This key is not declared here. Reload and choose a declared key.',
    409: (e) => e.detail ?? 'This project holds its maximum number of personal values.',
    429: commonRefusalText.requests,
  }, 'The value could not be saved. Nothing was stored.');
}
