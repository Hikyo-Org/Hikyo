import { watchProjectEventsOp } from '@hikyo/operations';
import { useEffect, useRef, useState } from 'react';
import { z } from 'zod';

import { useResetOnChange } from '../app/useResetOnChange.ts';
import type { MatrixRef } from './keys.ts';
import type { TransportOptions } from './transport.tsx';

/**
 * The advisory event stream boundary (#510, system-architecture ADR §
 * Real-time; revision-model ADR § Live updates).
 *
 * The server already ships the whole channel: an SSE endpoint with heartbeats
 * and dead-peer detection, a bounded fan-out service that authorizes and
 * projects every event, and a generated streaming operation bound to its
 * success status. The matrix opens ONE such stream per open project and
 * invalidates react-query caches from its events, instead of polling every
 * environment's signals every two seconds. That per-environment poll remains,
 * demoted to the documented fallback: it runs while the stream has never
 * connected or has failed, and stops once the stream is healthy again.
 *
 * The channel is ADVISORY ONLY and replays nothing (`Last-Event-ID` refetches
 * nothing): a client that misses events refetches current state. Two things
 * are that refetch. The fallback poll runs precisely while the stream is not
 * healthy and keeps signals and pending drafts fresh, so a dropped stream
 * costs two seconds of age, never correctness. And every RECOVERY, a stream
 * that went healthy again after it was lost, invalidates the whole matrix
 * working set once (system-architecture ADR § Real-time: "a reconnecting
 * client performs a normal authorized refetch of current state"), so what
 * the poll does not cover (keys, groups, settings) converges too.
 */

/** The event types the revision-model ADR enumerates. */
type AdvisoryEventType = 'revision.published' | 'cell.changed' | 'pending.staged';

/** One advisory event, camelCased for the SPA. */
export type AdvisoryEvent = {
  readonly type: AdvisoryEventType;
  readonly environmentId: string;
  readonly keyId?: string;
  readonly keyName?: string;
  readonly revision?: bigint;
  readonly actorId?: string;
};

/**
 * The wire envelope every event shares (`wireAdvisory`, internal/server/
 * revisions.go): the type, and the environment the event was authorized
 * against. Metadata only, no field here could hold a value or a change
 * token, and the schemas below must never grow one.
 */
const zAdvisoryEnvelope = z.object({
  type: z.string(),
  environment_id: z.string().min(1),
});

const zEventKey = {
  key_id: z.string().min(1),
  name: z.string().min(1),
} as const;

/** A JSON number on the wire; bigint at this boundary, like every revision. */
const zEventRevision = z.coerce
  .bigint()
  .min(BigInt(1), { error: 'Invalid value: expected a positive revision' })
  .max(BigInt('9223372036854775807'), { error: 'Invalid value: expected an int64 revision' });

const zRevisionPublishedWire = z.object({
  type: z.literal('revision.published'),
  environment_id: z.string().min(1),
  revision: zEventRevision,
});

const zCellChangedWire = z.object({
  type: z.literal('cell.changed'),
  environment_id: z.string().min(1),
  ...zEventKey,
  revision: zEventRevision,
});

const zPendingStagedWire = z.object({
  type: z.literal('pending.staged'),
  environment_id: z.string().min(1),
  ...zEventKey,
  // The actor survives only on the recipient's OWN events (advisory.go's
  // projection blanks everyone else), so `actor_id` present means "your own
  // draft", a fact the recipient already knows and may act on.
  actor_id: z.string().min(1).optional(),
});

/**
 * parseAdvisoryEvent narrows one stream payload, returning null for an event
 * type this build does not know. Unknown types are skipped, never fatal: the
 * channel is additive by design, and a future server naming a new fact must
 * not kill delivery of the types this build does understand, nothing was
 * missed that the fallback refetch would not fetch. A KNOWN type with a
 * malformed body is refused loudly instead: a silently-accepted wrong shape is
 * the bug this Zod boundary exists to stop.
 */
export function parseAdvisoryEvent(input: unknown): AdvisoryEvent | null {
  const envelope = zAdvisoryEnvelope.parse(input);
  switch (envelope.type) {
    case 'revision.published':
      return wireAdvisoryEvent(zRevisionPublishedWire.parse(input));
    case 'cell.changed':
      return wireAdvisoryEvent(zCellChangedWire.parse(input));
    case 'pending.staged':
      return wireAdvisoryEvent(zPendingStagedWire.parse(input));
    default:
      return null;
  }
}

function wireAdvisoryEvent(wire: {
  type: AdvisoryEventType;
  environment_id: string;
  key_id?: string;
  name?: string;
  revision?: bigint;
  actor_id?: string;
}): AdvisoryEvent {
  return {
    type: wire.type,
    environmentId: wire.environment_id,
    ...(wire.key_id === undefined ? {} : { keyId: wire.key_id }),
    ...(wire.name === undefined ? {} : { keyName: wire.name }),
    ...(wire.revision === undefined ? {} : { revision: wire.revision }),
    ...(wire.actor_id === undefined ? {} : { actorId: wire.actor_id }),
  };
}

/** How live the stream believes it is. Anything but `healthy` polls. */
export type AdvisoryConnectionState = 'connecting' | 'healthy' | 'failed';

export type AdvisoryHandlers = {
  /** One parsed event; the consumer invalidates what the event names. */
  readonly onEvent: (event: AdvisoryEvent) => void;
  /** Connection-state transitions, driving the fallback poll. */
  readonly onState: (state: AdvisoryConnectionState) => void;
};

/**
 * The client-side reconnect cadence for attempts the server never answered.
 * After a CONNECTED stream the server's own jittered `retry:` hint governs,
 * so this only bounds the dark: one second before the first retry, ten at the
 * ceiling, a compromise between a reconnect storm and a tab that sits stale
 * behind its fallback poll for half a minute.
 */
const ADVISORY_RECONNECT_BASE_MS = 1_000;
const ADVISORY_RECONNECT_MAX_MS = 10_000;

/** The matrix's fallback cadence: the poll #510 demotes from primary to fallback. */
export const SIGNALS_FALLBACK_POLL_MS = 2_000;

/**
 * signalsPollInterval is the fallback-poll selector the signals and pending
 * drafts queries read: poll while the stream is not healthy, and never while
 * it is. Pure, so the connection logic is testable without a stream.
 */
export function signalsPollInterval(state: AdvisoryConnectionState): number | false {
  return state === 'healthy' ? false : SIGNALS_FALLBACK_POLL_MS;
}

/**
 * What the matrix reads off the stream: the connection state that gates the
 * fallback poll, and a recovery counter that ticks once per stream that went
 * healthy again after it was LOST. `lost` is the memory that makes the
 * difference between a first connect (no catch-up: the queries were just
 * mounted) and a reconnect (catch-up: events were missed in the dark).
 */
export type AdvisoryLiveness = {
  readonly connection: AdvisoryConnectionState;
  readonly recoveries: number;
  readonly lost: boolean;
};

export const INITIAL_ADVISORY_LIVENESS: AdvisoryLiveness = {
  connection: 'connecting',
  recoveries: 0,
  lost: false,
};

/**
 * advanceAdvisoryLiveness folds one connection-state report into the
 * liveness. Returns the SAME object when nothing changed, so the healthy
 * report every frame repeats never re-renders the matrix, and a recovery is
 * counted exactly once no matter how many failures preceded it (hey-api's
 * internal retries can report `failed` several times before a `connecting`,
 * or without one).
 */
export function advanceAdvisoryLiveness(
  previous: AdvisoryLiveness,
  connection: AdvisoryConnectionState,
): AdvisoryLiveness {
  switch (connection) {
    case 'failed':
      return previous.connection === 'failed' && previous.lost
        ? previous
        : { connection, recoveries: previous.recoveries, lost: true };
    case 'connecting':
      return previous.connection === 'connecting' ? previous : { ...previous, connection };
    case 'healthy':
      if (previous.connection === 'healthy') {
        return previous;
      }
      return {
        connection,
        recoveries: previous.lost ? previous.recoveries + 1 : previous.recoveries,
        lost: false,
      };
  }
}

/**
 * watchProjectAdvisoryStream opens the generated stream and drives it until
 * the returned disposer is called.
 *
 * Reconnection is layered, and both layers are load-bearing:
 *
 *  - hey-api's fetch-SSE client retries a FAILED attempt (a refused connection,
 *    a non-2xx handshake) internally, surfacing each failure through
 *    `onSseError`; the iteration only ends when the server ends the response
 *    cleanly, the slow-client drop, an instance shutting down.
 *  - That clean end ends the iteration, and this loop re-subscribes after its
 *    own jittered backoff.
 *
 * The abort signal is the whole teardown: aborting cancels the in-flight
 * read and stops the retry loop, so disposal closes the connection even
 * mid-backoff. The route-leave cleanup is an abort, not a leak.
 */
export function watchProjectAdvisoryStream(
  ref: MatrixRef,
  transport: TransportOptions,
  handlers: AdvisoryHandlers,
): { stop: () => Promise<void> } {
  const controller = new AbortController();
  let resolveDone: (() => void) | undefined;
  const done = new Promise<void>((resolve) => {
    resolveDone = resolve;
  });
  const run = async (): Promise<void> => {
    let backoffMs = ADVISORY_RECONNECT_BASE_MS;
    try {
      while (!controller.signal.aborted) {
        handlers.onState('connecting');
        try {
          const result = await watchProjectEventsOp.call({
            path: { org: ref.org, project: ref.project },
            signal: controller.signal,
            onSseEvent: () => handlers.onState('healthy'),
            onSseError: () => handlers.onState('failed'),
            sseDefaultRetryDelay: ADVISORY_RECONNECT_BASE_MS,
            sseMaxRetryDelay: ADVISORY_RECONNECT_MAX_MS,
            ...transport,
          });
          for await (const data of result.stream) {
            handlers.onState('healthy');
            const event = parseAdvisoryEvent(data);
            if (event !== null) {
              handlers.onEvent(event);
            }
          }
        } catch {
          // The stream ended: a failed attempt the client already reported,
          // a refusal (401/404/429), or the abort. The state below resumes
          // the fallback poll for exactly the window the stream is not live.
        }
        handlers.onState('failed');
        if (controller.signal.aborted) {
          break;
        }
        await sleep(jitter(backoffMs), controller.signal);
        backoffMs = Math.min(backoffMs * 2, ADVISORY_RECONNECT_MAX_MS);
      }
    } finally {
      resolveDone?.();
    }
  };
  void run();
  return {
    stop: () => {
      controller.abort();
      return done;
    },
  };
}

/** `retry:` hints aside, reconnects are jittered so a server restart wave does not synchronise clients. */
function jitter(ms: number): number {
  return ms + Math.floor(Math.random() * ms);
}

/** Abort-aware sleep: disposal interrupts a backoff instead of outliving it. */
function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(finish, ms);
    signal.addEventListener('abort', finish, { once: true });
    function finish(): void {
      clearTimeout(timer);
      signal.removeEventListener('abort', finish);
      resolve();
    }
  });
}

/**
 * useAdvisoryStream owns one project's advisory subscription for the lifetime
 * of the calling component: opened on mount, aborted on unmount, re-opened if
 * the project ref changes. The event handler runs through a latest-ref so a
 * re-render never re-subscribes, and the liveness is React state so the
 * fallback poll and the recovery catch-up re-render with it. A new
 * subscription starts from the initial liveness: its queries were just
 * mounted, so its first connect is never a recovery.
 */
export function useAdvisoryStream(
  ref: MatrixRef,
  transport: TransportOptions,
  onEvent: (event: AdvisoryEvent) => void,
  enabled: boolean,
): AdvisoryLiveness {
  const [state, setState] = useState<AdvisoryLiveness>(INITIAL_ADVISORY_LIVENESS);
  const live = useRef({ onEvent, transport });
  useEffect(() => {
    live.current = { onEvent, transport };
  });

  const { org, project } = ref;
  // A new subscription always starts from the initial liveness: its queries were
  // just (re)mounted, so its first connect is never a recovery. Reset during
  // render as the subscription identity changes, ahead of the effect that
  // re-subscribes, rather than with a setState inside the effect body.
  useResetOnChange(`${enabled} ${org} ${project}`, () =>
    setState(INITIAL_ADVISORY_LIVENESS),
  );
  useEffect(() => {
    if (!enabled) {
      return;
    }
    let stopped = false;
    const handle = watchProjectAdvisoryStream(
      { org, project },
      live.current.transport,
      {
        onEvent: (event) => live.current.onEvent(event),
        onState: (connection) => {
          if (!stopped) {
            setState((previous) => advanceAdvisoryLiveness(previous, connection));
          }
        },
      },
    );
    return () => {
      stopped = true;
      void handle.stop();
    };
  }, [enabled, org, project]);

  return enabled ? state : INITIAL_ADVISORY_LIVENESS;
}
