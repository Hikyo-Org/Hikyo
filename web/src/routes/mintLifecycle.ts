import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * The fields every mint request carries so the lifecycle can address a response
 * to the request that started it and mask it at a session/route boundary. A
 * concrete request type (machine credential, dynamic lease) extends this.
 */
export type MintBoundaryFields = {
  readonly id: number;
  readonly sessionId: string;
  readonly org: string;
  readonly project: string;
};

/**
 * Non-secret inputs frozen when the operator opens mint review.
 *
 * Keep this narrower than ServiceAccount and MachineEnvScope: retry needs the
 * addressed account, the review labels, and the disclosure environments. It
 * must not inherit unrelated query data or any credential response field.
 */
export type MintRequest = MintBoundaryFields & {
  readonly accountId: string;
  readonly accountName: string;
  readonly rotating: boolean;
  readonly reach: readonly { readonly id: string; readonly name: string }[];
};

export type MintResult = {
  readonly value: string;
  readonly clamped: boolean;
};

/**
 * Closed display-once mint state machine, generic over the request it addresses
 * and the disclosed result it carries. The machine-credential mint (`MintRequest`
 * / `MintResult`) is the default instantiation; the dynamic-lease mint supplies
 * its own request and result. The security-critical parts, request-addressed
 * completion, the stored-confirmation gate, boundary masking, are identical for
 * both, so they live here once.
 */
export type MintLifecycle<Req extends MintBoundaryFields = MintRequest, Res = MintResult> =
  | { readonly kind: 'idle' }
  | { readonly kind: 'reviewing'; readonly request: Req }
  | { readonly kind: 'submitting'; readonly request: Req }
  | { readonly kind: 'failed'; readonly request: Req; readonly error: string }
  | {
      readonly kind: 'disclosed';
      readonly request: Req;
      readonly result: Res;
      readonly stored: boolean;
      readonly heldBack: boolean;
      readonly copyStatus: string | null;
    };

export type MintLifecycleEvent<Req extends MintBoundaryFields = MintRequest, Res = MintResult> =
  | { readonly type: 'review'; readonly request: Req }
  | { readonly type: 'submit' }
  | { readonly type: 'succeeded'; readonly requestId: number; readonly result: Res }
  | { readonly type: 'failed'; readonly requestId: number; readonly error: string }
  | { readonly type: 'confirm-stored'; readonly stored: boolean }
  | { readonly type: 'copy-status'; readonly requestId: number; readonly message: string }
  | { readonly type: 'dismiss' }
  | {
      readonly type: 'clear';
      readonly reason: 'close' | 'navigation' | 'session-transition';
    };

/**
 * The idle state, typed as the bare `idle` variant so it is assignable to every
 * `MintLifecycle` instantiation and never drives type-parameter inference when
 * passed as the starting state.
 */
export const idleMintLifecycle: { readonly kind: 'idle' } = { kind: 'idle' };

export type MintBoundary = {
  readonly sessionId: string | null;
  readonly org: string;
  readonly project: string;
};

/** Mask lifecycle state synchronously when its route or session no longer owns it. */
export function mintLifecycleAtBoundary<Req extends MintBoundaryFields, Res>(
  state: MintLifecycle<Req, Res>,
  boundary: MintBoundary,
): MintLifecycle<Req, Res> {
  if (state.kind === 'idle') {
    return state;
  }
  return state.request.sessionId === boundary.sessionId &&
    state.request.org === boundary.org &&
    state.request.project === boundary.project
    ? state
    : { kind: 'idle' };
}

/**
 * Closed mint state machine.
 *
 * Invalid events return the same object. Callers use that identity to avoid
 * starting a second transport while React is still scheduling the first
 * submitting render. Async completion is request-addressed, so an old response
 * cannot put plaintext back after navigation, a new mint, or a session exit.
 */
export function transitionMintLifecycle<Req extends MintBoundaryFields, Res>(
  state: MintLifecycle<Req, Res>,
  event: MintLifecycleEvent<Req, Res>,
): MintLifecycle<Req, Res> {
  switch (event.type) {
    case 'review':
      return { kind: 'reviewing', request: event.request };
    case 'submit':
      return state.kind === 'reviewing' || state.kind === 'failed'
        ? { kind: 'submitting', request: state.request }
        : state;
    case 'succeeded':
      return state.kind === 'submitting' && state.request.id === event.requestId
        ? {
            kind: 'disclosed',
            request: state.request,
            result: event.result,
            stored: false,
            heldBack: false,
            copyStatus: null,
          }
        : state;
    case 'failed':
      return state.kind === 'submitting' && state.request.id === event.requestId
        ? { kind: 'failed', request: state.request, error: event.error }
        : state;
    case 'confirm-stored':
      return state.kind === 'disclosed'
        ? { ...state, stored: event.stored, heldBack: false }
        : state;
    case 'copy-status':
      return state.kind === 'disclosed' && state.request.id === event.requestId
        ? { ...state, copyStatus: event.message }
        : state;
    case 'dismiss':
      if (state.kind === 'submitting' || state.kind === 'idle') {
        return state;
      }
      if (state.kind === 'disclosed' && !state.stored) {
        return { ...state, heldBack: true };
      }
      return { kind: 'idle' };
    case 'clear':
      return state.kind === 'idle' ? state : { kind: 'idle' };
  }
}

/** The result of applying an event: the next state and whether it changed. */
type MintTransitionResult<Req extends MintBoundaryFields, Res> = {
  readonly state: MintLifecycle<Req, Res>;
  readonly accepted: boolean;
};

export type MoveMint<Req extends MintBoundaryFields = MintRequest, Res = MintResult> = (
  event: MintLifecycleEvent<Req, Res>,
) => MintTransitionResult<Req, Res>;

export type IsMintSubmitting = (requestId: number) => boolean;

/**
 * useMintLifecycle wires one display-once mint: the render state, a synchronous
 * ref that async completions read (so a late response sees the current state,
 * never a stale closure), and the three boundary clears, navigation, session
 * replacement, unmount, that keep a disclosed value from surviving into a
 * context that no longer owns it. Both the machine-credential mint and the
 * dynamic-lease mint run on one of these.
 */
export function useMintLifecycle<Req extends MintBoundaryFields = MintRequest, Res = MintResult>(
  boundary: MintBoundary,
): {
  readonly lifecycle: MintLifecycle<Req, Res>;
  readonly active: MintLifecycle<Req, Res>;
  readonly moveMint: MoveMint<Req, Res>;
  readonly isSubmitting: IsMintSubmitting;
  readonly nextRequestId: () => number;
} {
  const [lifecycle, setLifecycle] = useState<MintLifecycle<Req, Res>>({ kind: 'idle' });
  const lifecycleRef = useRef<MintLifecycle<Req, Res>>(lifecycle);
  const boundaryRef = useRef<MintBoundary>(boundary);
  boundaryRef.current = boundary;
  const requestId = useRef(0);

  const moveMint = useCallback<MoveMint<Req, Res>>((event) => {
    const current = lifecycleRef.current;
    const next = transitionMintLifecycle(current, event);
    lifecycleRef.current = next;
    if (next !== current) {
      setLifecycle(next);
    }
    return { state: next, accepted: next !== current };
  }, []);

  const isSubmitting = useCallback<IsMintSubmitting>((id) => {
    const current = mintLifecycleAtBoundary(lifecycleRef.current, boundaryRef.current);
    return current.kind === 'submitting' && current.request.id === id;
  }, []);

  // A project route is a new mint boundary. Clear before any completion from
  // the old route can publish its display-once response into this one.
  useEffect(() => {
    moveMint({ type: 'clear', reason: 'navigation' });
  }, [moveMint, boundary.org, boundary.project]);

  // Session replacement is a harder boundary than navigation: even if the route
  // remains mounted, no disclosure from the old browser session may survive.
  useEffect(() => {
    moveMint({ type: 'clear', reason: 'session-transition' });
  }, [moveMint, boundary.sessionId]);

  // Wipe the synchronous ref on unmount too, so an already-resolving promise
  // sees idle and drops its late result.
  useEffect(
    () => () => {
      lifecycleRef.current = { kind: 'idle' };
    },
    [],
  );

  return {
    lifecycle,
    active: mintLifecycleAtBoundary(lifecycle, boundary),
    moveMint,
    isSubmitting,
    nextRequestId: () => {
      requestId.current += 1;
      return requestId.current;
    },
  };
}
