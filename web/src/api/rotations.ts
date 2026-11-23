import {
  createRotationOp,
  deleteRotationOp,
  listRotationRunsOp,
  listRotationsOp,
  pauseRotationOp,
  releaseRotationHoldOp,
  resumeRotationOp,
  runRotationNowOp,
} from '@hikyo/operations';
import { zCredentialRotation, zRotationRun } from '@hikyo/zod';
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type InfiniteData,
  type UseInfiniteQueryResult,
} from '@tanstack/react-query';
import type { z } from 'zod';

import { parsed } from './client.ts';
import { commonRefusalText, statusText, withDetail } from './statusText.ts';
import { useTransport } from './transport.tsx';

/** A scheduled credential rotation (#148), status only: no secret is ever on it. */
export type Rotation = z.infer<typeof zCredentialRotation>;

/** One run of a rotation: the fixed-word error code, never provider detail. */
export type RotationRun = z.infer<typeof zRotationRun>;

/** EnvironmentRef addresses one environment: rotations are environment-scoped. */
export type EnvironmentRef = {
  readonly org: string;
  readonly project: string;
  readonly environment: string;
};

type RotationPage = { readonly items: readonly Rotation[]; readonly next_after?: string };
type RunPage = { readonly items: readonly RotationRun[]; readonly next_after?: string };

/** The server's own page ceiling for the runs list; the CLI reads runs at it too. */
export const RUNS_PAGE_LIMIT = 500;

/** How many run pages the browser reads back, the same bound as `hikyo rotation show`. */
export const MAX_RUN_PAGES = 20;

const rotationsKey = (env: EnvironmentRef, includeDisabled: boolean) =>
  ['rotations', env.org, env.project, env.environment, includeDisabled] as const;

const runsKey = (env: EnvironmentRef, rotation: string) =>
  ['rotation-runs', env.org, env.project, env.environment, rotation] as const;

// The first page carries no cursor; the server answers `next_after` until the last page.
const FIRST_PAGE: string | undefined = undefined;

/**
 * useRotations pages one environment's rotations in id order. Deleted
 * (`disabled`) rotations are history and only listed on request.
 */
export function useRotations(
  env: EnvironmentRef,
  includeDisabled: boolean,
): UseInfiniteQueryResult<InfiniteData<RotationPage, string | undefined>, unknown> {
  const transport = useTransport();
  return useInfiniteQuery({
    queryKey: rotationsKey(env, includeDisabled),
    initialPageParam: FIRST_PAGE,
    queryFn: ({ pageParam, signal }) =>
      parsed(listRotationsOp, {
        path: { org: env.org, project: env.project, environment: env.environment },
        query: {
          ...(includeDisabled ? { include_disabled: true } : {}),
          ...(pageParam === undefined ? {} : { after: pageParam }),
        },
        signal,
        ...transport,
      }),
    getNextPageParam: (last) => last.next_after,
    enabled: env.org !== '' && env.project !== '' && env.environment !== '',
  });
}

/** useRotationRuns pages one rotation's runs, newest first. */
export function useRotationRuns(
  env: EnvironmentRef,
  rotation: string,
): UseInfiniteQueryResult<InfiniteData<RunPage, string | undefined>, unknown> {
  const transport = useTransport();
  return useInfiniteQuery({
    queryKey: runsKey(env, rotation),
    initialPageParam: FIRST_PAGE,
    queryFn: ({ pageParam, signal }) =>
      parsed(listRotationRunsOp, {
        path: {
          org: env.org,
          project: env.project,
          environment: env.environment,
          rotation,
        },
        query: {
          limit: RUNS_PAGE_LIMIT,
          ...(pageParam === undefined ? {} : { after: pageParam }),
        },
        signal,
        ...transport,
      }),
    getNextPageParam: (last) => last.next_after,
    enabled: rotation !== '',
  });
}

/** The six acts on a rotation (#1440); none of them carries or returns a credential. */
export type RotationVerb = 'create' | 'pause' | 'resume' | 'release-hold' | 'run-now' | 'delete';

export type RotationAct =
  | { readonly verb: 'pause' | 'resume' | 'release-hold'; readonly rotation: Rotation }
  | { readonly verb: 'run-now'; readonly rotation: Rotation; readonly skipOverlap: boolean }
  | { readonly verb: 'delete'; readonly rotation: Rotation; readonly abandon: boolean };

export type CreateRotationInput = {
  readonly providerId: string;
  readonly keyId: string;
  readonly usernameKeyId: string;
  readonly intervalSeconds: number;
  readonly revocationDelaySeconds: number;
};

/** Every list and run read of one environment, so an act refreshes them all. */
function useRefreshEnvironment(env: EnvironmentRef): () => void {
  const queries = useQueryClient();
  return () => {
    void queries.invalidateQueries({ queryKey: ['rotations', env.org, env.project, env.environment] });
    void queries.invalidateQueries({ queryKey: ['rotation-runs', env.org, env.project, env.environment] });
  };
}

/**
 * useCreateRotation configures a rotation. The caller runs the publish ceremony
 * over the environment and both keys first; the server consumes it.
 */
export function useCreateRotation(env: EnvironmentRef) {
  const transport = useTransport();
  const refresh = useRefreshEnvironment(env);
  return useMutation({
    mutationFn: (input: CreateRotationInput) =>
      parsed(createRotationOp, {
        path: { org: env.org, project: env.project, environment: env.environment },
        body: {
          provider_id: input.providerId,
          key_id: input.keyId,
          username_key_id: input.usernameKeyId,
          interval_seconds: input.intervalSeconds,
          revocation_delay_seconds: input.revocationDelaySeconds,
        },
        ...transport,
      }),
    // A lost response may still have committed, so the list is re-read either way.
    onSettled: refresh,
  });
}

/**
 * useRotationAct takes one act on an existing rotation. Each carries the
 * generation the browser read, so a change someone made since is refused
 * (409) instead of overwritten. Resume, release-hold and run-now consume the
 * publish ceremony the caller ran; pause and delete need none.
 */
/** GenerationRangeError is a fence the browser cannot send exactly; nothing was sent. */
class GenerationRangeError extends Error {
  constructor(generation: bigint) {
    super(`its generation ${String(generation)} is beyond what this page can send exactly, so nothing was sent`);
  }
}

export function useRotationAct(env: EnvironmentRef) {
  const transport = useTransport();
  const refresh = useRefreshEnvironment(env);
  return useMutation({
    mutationFn: async (act: RotationAct): Promise<void> => {
      const path = { org: env.org, project: env.project, environment: env.environment, rotation: act.rotation.id };
      // The generated client types the fence as number: refuse one it cannot carry exactly.
      if (act.rotation.generation > BigInt(Number.MAX_SAFE_INTEGER)) {
        throw new GenerationRangeError(act.rotation.generation);
      }
      const expected_generation = Number(act.rotation.generation);
      const options = { path, ...transport };
      switch (act.verb) {
        case 'pause':
          await parsed(pauseRotationOp, { ...options, body: { expected_generation } });
          return;
        case 'resume':
          await parsed(resumeRotationOp, { ...options, body: { expected_generation } });
          return;
        case 'release-hold':
          await parsed(releaseRotationHoldOp, { ...options, body: { expected_generation } });
          return;
        case 'run-now':
          await parsed(runRotationNowOp, {
            ...options,
            body: { confirm_destructive: true, expected_generation, skip_overlap: act.skipOverlap },
          });
          return;
        case 'delete':
          await parsed(deleteRotationOp, { ...options, query: { expected_generation, abandon: act.abandon } });
          return;
      }
    },
    onSettled: refresh,
  });
}

/**
 * rotationActRefusalText names a refused act. The server's named refusal (a
 * protected environment, a stale generation, a restore hold, a run in flight)
 * travels as the safe detail and is shown as given, never re-derived here.
 */
export function rotationActRefusalText(error: unknown, verb: RotationVerb): string {
  const subject = verb === 'create' ? 'The rotation was not created' : `The ${verb} was refused`;
  if (error instanceof GenerationRangeError) return `${subject}: ${error.message}. Use hikyo rotation instead.`;
  return statusText(
    error,
    {
      400: (e) => withDetail(`${subject}: the server refused the request.`, e),
      401: commonRefusalText.sessionEnded,
      403: (e) =>
        withDetail(
          `${subject}: you may lack access. It needs ${verb === 'pause' || verb === 'delete' ? 'publish' : 'edit, publish and reveal'} on both keys, from a signed-in person${verb === 'pause' || verb === 'delete' ? '' : ', and a publish confirmation that is still live'}.`,
          e,
        ),
      404: `${subject}: the rotation or environment is not here, or you may not read it.`,
      409: (e) => withDetail(`${subject}: it conflicts with the rotation's current state.`, e),
      429: commonRefusalText.requests,
    },
    `${subject}. Try again.`,
    (e) => `${subject} (server error ${String(e.status)}).`,
  );
}

/**
 * rotationRefusalText names a failed read without inventing a cause. The API
 * answers a caller who may not read the environment with the same not-found it
 * gives an absent one, so 403 and 404 both read as "needs read here".
 */
export function rotationRefusalText(error: unknown, what: 'rotations' | 'runs'): string {
  const subject = what === 'rotations' ? 'The rotations' : 'The runs';
  return statusText(
    error,
    {
      401: commonRefusalText.sessionEnded,
      403: `${subject} could not be listed. Listing them needs read on this environment.`,
      404: `${subject} could not be listed. Listing them needs read on this environment.`,
      429: commonRefusalText.requests,
    },
    `${subject} could not be listed. Reload to try again.`,
  );
}

// ---- Safety wording ------------------------------------------------------
//
// These sentences are the CLI's, word for word (internal/cli/credential_rotation.go,
// noteOwedRuns, noteOwedSummary, noteRestoreHold and haltReasonSentence) and the user docs' (rotation.mdx). An operator
// reads the same instruction in the terminal and in the browser, so a change to
// one is a change to all three. Times are RFC 3339 UTC, as the CLI prints them.

/** rfc3339 prints an instant as the CLI does: UTC, whole seconds. */
export function rfc3339(value: string): string {
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toISOString().replace(/\.\d+Z$/, 'Z');
}

/** The run error code of a drop refused because a restore may have put the role back. */
export const VERSIONS_CHANGED_CODE = 'published_versions_changed';

/** Whether a run still owes a provider role a drop: a refused revoke, or a quarantine. */
export function owesDrop(run: RotationRun): boolean {
  return run.phase === 'revoke_refused' || run.phase === 'quarantine';
}

/** owedRunText is the ATTENTION sentence for a run that owes a provider role a drop. */
export function owedRunText(run: RotationRun): string {
  let until = 'no recorded expiry';
  if (run.credential_valid_until !== undefined && run.credential_valid_until !== null) {
    const at = rfc3339(run.credential_valid_until);
    until =
      run.credential_valid_until_lower_bound === true
        ? `stays valid at least until ${at}, and possibly longer: an extend of it is unsettled`
        : `stays valid until ${at}`;
  }
  if (run.error_code === VERSIONS_CHANGED_CODE) {
    return `ATTENTION: run ${run.id} is ${run.phase} (${VERSIONS_CHANGED_CODE}): a restore put an earlier value back, so the keys may be serving the role it owes a drop again (${until}); do NOT drop it at the provider until the keys have moved off it`;
  }
  return `ATTENTION: run ${run.id} is ${run.phase} and a provider role it owes a drop is still live (${until}); before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back`;
}

/** The command that lifts a restore hold, the CLI twin of the browser's release action. */
export function releaseHoldCommand(rotation: Rotation): string {
  return `hikyo rotation release-hold ${rotation.id}`;
}

/** restoreHoldText is the RESTORE HOLD sentence for a rotation a restore holds. */
export function restoreHoldText(rotation: Rotation): string {
  return `${restoreHoldState(rotation)}; reconcile its grants and provider credential, then run: ${releaseHoldCommand(rotation)}`;
}

/** restoreHoldState is the RESTORE HOLD sentence without the CLI instruction, for the browser's release dialog. */
export function restoreHoldState(rotation: Rotation): string {
  const until =
    rotation.credential_expires_at === undefined || rotation.credential_expires_at === null
      ? 'no recorded expiry'
      : `the delivered credential lapses at ${rfc3339(rotation.credential_expires_at)}`;
  return `RESTORE HOLD: rotation ${rotation.id} is held after a restore and starts no run (${until})`;
}

/** The halt reasons the server writes, as fixed words; `unknown` is a halt it did not name. */
const HALT_SENTENCES: Record<string, string> = {
  retries_exhausted: 'the retry window ran out before a run could publish or extend the credential',
  authority_lost:
    'the recorded person no longer holds the access the rotation needs, or one of its keys or its environment is gone',
  gate_failed: 'a pre-publish gate refused: the environment became protected, a key changed or a draft appeared',
  unknown: 'the server halted the rotation without a reason it can name',
};

/** haltReasonField is `word: sentence`, the halt reason as a field of the rotation's row. */
export function haltReasonField(rotation: Rotation): string {
  const given = rotation.halt_reason ?? 'unknown';
  // Own keys only: `toString` or `__proto__` is an unrecognised word, not a sentence.
  const known = Object.hasOwn(HALT_SENTENCES, given);
  // A word this client does not know is printed quoted and never interpreted.
  const word = known ? given : JSON.stringify(given);
  const why = known ? HALT_SENTENCES[given] : 'the server recorded a reason this client does not know';
  return `${word}: ${why}`;
}

/** haltCause is `halted (word): sentence`, the part of the CLI's HALTED line that names the cause. */
export function haltCause(rotation: Rotation): string {
  const [word, ...why] = haltReasonField(rotation).split(': ');
  return `halted (${word}): ${why.join(': ')}`;
}

/** haltReasonText is the CLI's whole HALTED line for a halted rotation. */
export function haltReasonText(rotation: Rotation): string {
  return `HALTED: rotation ${rotation.id} is ${haltCause(rotation)}; it starts no run and does not extend the delivered credential until a person resumes it: hikyo rotation resume ${rotation.id}`;
}

/** Whether a rotation has a run that still owes a provider role a drop (the server counts them). */
export function owesDropSummary(rotation: Rotation): boolean {
  return rotation.owed_drop_runs > 0n;
}

/** Whether a restore refused any of those drops: the keys may serve the role again. */
export function owedVersionsChanged(rotation: Rotation): boolean {
  return rotation.owed_drop_runs_versions_changed > 0n;
}

/** A deleted rotation is gone from the default list but its roles can still be live. */
function deletedMark(rotation: Rotation): string {
  return rotation.state === 'disabled' ? ' (deleted)' : '';
}

/**
 * owedSummaryText is the rotation-level ATTENTION line. It counts the runs
 * `owesDrop` marks and points at the run list instead of repeating each run's
 * sentence. It carries the same key check as a run's own line: a restore (one that
 * refused a run, or a hold that is still on) may have put the role back, so it never
 * plainly says to drop.
 */
export function owedSummaryText(rotation: Rotation): string {
  const count = String(rotation.owed_drop_runs);
  if (owedVersionsChanged(rotation)) {
    return `ATTENTION: rotation ${rotation.id}${deletedMark(rotation)} has ${count} run(s) that owe a provider role a drop, ${String(rotation.owed_drop_runs_versions_changed)} of them refused after a restore (${VERSIONS_CHANGED_CODE}): the keys may be serving that role again, so do NOT drop it at the provider until the keys have moved off it; read: hikyo rotation runs ${rotation.id}`;
  }
  if (rotation.restore_hold) {
    return `ATTENTION: rotation ${rotation.id}${deletedMark(rotation)} has ${count} run(s) that owe a provider role a drop and a restore holds the rotation: the keys may be serving that role again, so do NOT drop it at the provider until you have checked that neither key serves it; read: hikyo rotation runs ${rotation.id}`;
  }
  return `ATTENTION: rotation ${rotation.id}${deletedMark(rotation)} has ${count} run(s) that owe a provider role a drop and the role is still live; before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back; read: hikyo rotation runs ${rotation.id}`;
}

/** The badge on a rotation row that owes a drop: text, with the restore case never saying drop. */
export function owedBadgeText(rotation: Rotation): string {
  return owedVersionsChanged(rotation) || rotation.restore_hold ? 'Do not drop: restore' : 'Owes a drop';
}
