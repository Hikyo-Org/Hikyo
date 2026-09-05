import { whoamiOp } from '@hikyo/operations';
import { zWhoAmI } from '@hikyo/zod';
import { QueryClientProvider } from '@tanstack/react-query';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { flushSync } from 'react-dom';
import type { z } from 'zod';

import { retireSensitiveOperations, transferSensitiveState, type SensitiveStateTransfer } from '../api/sensitiveMutation.ts';
import {
  announceSessionChange,
  blockSessionEpoch,
  checkSessionCookie,
  installSessionFence,
  isSessionStorageEvent,
  isSessionEpochBlocked,
  isPeerSessionMessage,
  settleSessionEpoch,
  SessionChangedError,
  SESSION_CHANNEL,
  type ExpectedSessionRotation,
} from '../api/sessionEpoch.ts';

import { ApiError, parsed, readCsrfToken } from '../api/client.ts';
import { transitionWorkspaceOwner } from '../api/workspace.ts';
import { makeQueryClient } from './queryClient.ts';

export type WhoAmI = z.infer<typeof zWhoAmI>;

/**
 * The source identity of a session transition. `whoami` carries `capabilities`;
 * a login or step-up RESULT does not — that response predates knowing the
 * caller's grants. `acceptSession` binds the epoch from it immediately and then
 * hydrates the real capabilities from a `whoami`, so the operator chrome a fresh
 * operator session is entitled to appears within one round trip rather than
 * waiting for the next revalidation.
 */
export type AcceptedIdentity = Omit<WhoAmI, 'capabilities'> &
  Partial<Pick<WhoAmI, 'capabilities'>>;

export type AuthState =
  | { readonly status: 'checking' }
  | { readonly status: 'anonymous' }
  | {
      readonly status: 'authenticated';
      readonly sessionEpoch: string;
      readonly principal: WhoAmI['principal'];
    }
  | { readonly status: 'transitioning' };

type AuthContextValue = {
  readonly state: AuthState;
  readonly identity: WhoAmI | null;
  readonly failure: Error | null;
  // A transport/5xx/schema error from a BACKGROUND revalidation of a session
  // that is still held. Unlike `failure` this never latches the reload wall: the
  // last-known-good session keeps painting and a backed-off retry recovers it.
  readonly degraded: Error | null;
  readonly captureTransition: () => SessionTransitionGuard;
  readonly acceptSession: (identity: AcceptedIdentity, guard: SessionTransitionGuard) => void;
  readonly acceptAccountSession: (identity: AcceptedIdentity, guard: SessionTransitionGuard, transfer: SensitiveStateTransfer) => boolean;
  readonly endSession: (guard: SessionTransitionGuard) => void;
  readonly refreshSession: () => Promise<void>;
  readonly revalidate: () => Promise<void>;
};

type SessionTransitionGuard = {
  readonly revision: number;
};

type SessionCheckMode =
  | 'blocking'
  | 'blocking-and-publish'
  | 'refresh-and-publish'
  | 'rotation-and-publish';

type Snapshot = {
  readonly epoch: number;
  readonly checkingRotation: boolean;
  readonly state: AuthState;
  readonly identity: WhoAmI | null;
  readonly failure: Error | null;
  readonly degraded: Error | null;
  readonly queries: ReturnType<typeof makeQueryClient>;
};

const AuthContext = createContext<AuthContextValue | null>(null);
const MAX_TIMEOUT_MS = 2_147_483_647;
const FOCUS_CHECK_INTERVAL_MS = 1_000;
const DEGRADED_RETRY_BASE_MS = 1_000;
const DEGRADED_RETRY_MAX_MS = 30_000;

/** Read root identity without putting it in a session-owned query cache. */
async function readIdentity(): Promise<WhoAmI | null> {
  const cookie = readCsrfToken();
  try {
    const identity = await parsed(whoamiOp, {});
    if (cookie !== readCsrfToken()) throw new SessionChangedError();
    return identity;
  } catch (error) {
    if (cookie !== readCsrfToken()) throw new SessionChangedError();
    if (error instanceof ApiError && error.status === 401) {
      return null;
    }
    throw error;
  }
}

/**
 * Whether a held identity is provably dead regardless of the server. Only the
 * ABSOLUTE deadline qualifies: it can never be extended, so once it passes the
 * session is over even if we cannot reach the server to be told so. The idle
 * deadline is deliberately excluded — a successful revalidation refreshes it, so
 * a locally-passed idle window during an outage does not PROVE expiry and must
 * not wall a session the server might still honour.
 */
function isDefinitivelyExpired(identity: WhoAmI): boolean {
  const absolute = Date.parse(identity.session.absolute_expires_at);
  return Number.isFinite(absolute) && Date.now() >= absolute;
}

function stateFor(identity: WhoAmI | null): AuthState {
  return identity === null
    ? { status: 'anonymous' }
    : {
        status: 'authenticated',
        sessionEpoch: identity.session.id,
        principal: identity.principal,
      };
}

function identityVersion(identity: WhoAmI | null): string | null {
  if (identity === null) {
    return null;
  }
  return [
    identity.session.id,
    identity.principal.id,
    identity.session.assurance.method,
    identity.session.assurance.authenticated_at,
    // A capability change is a change: without it the quiet focus check would
    // treat a mid-session grant/revoke as no-op and leave the operator chrome
    // (all gated on this) stale until a blocking revalidate.
    String(identity.capabilities.instance_operator),
    ...identity.session.assurance.factors,
  ].join('\u001f');
}

/**
 * AuthProvider is the one owner of root identity and local-instance cache
 * lifetime.
 *
 * There is deliberately no public QueryClient. Root `whoami` checks are
 * uncached, and every TanStack entry below this provider belongs to exactly one
 * browser session epoch (or to the anonymous pre-session epoch). A new session
 * id, logout, or expiry cancels old work and destroys every cached query and
 * mutation before the new epoch renders. A same-id assurance refresh or a
 * verified account-security remint preserves entries and invalidates their
 * answers for re-evaluation.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [snapshot, setSnapshot] = useState<Snapshot>(() => ({
    epoch: 0,
    checkingRotation: false,
    state: { status: 'checking' },
    identity: null,
    failure: null,
    degraded: null,
    queries: makeQueryClient(),
  }));
  const snapshotRef = useRef(snapshot);
  const mountedRef = useRef(true);
  const requestRef = useRef(0);
  const transitionRevisionRef = useRef(0);
  const verifiedRemintRef = useRef<{ revision: number; session: string; principal: string } | null>(null);
  const lastFocusCheckRef = useRef(0);
  const degradedRetriesRef = useRef(0);

  const commit = useCallback((next: Snapshot) => {
    snapshotRef.current = next;
    setSnapshot(next);
  }, []);

  const destroySessionCache = useCallback((current: Snapshot): Snapshot => {
    retireSensitiveOperations(current.queries);
    void current.queries.cancelQueries();
    current.queries.clear();
    return { ...current, epoch: current.epoch + 1, queries: makeQueryClient() };
  }, []);

  const publishChange = useCallback(() => {
    announceSessionChange();
  }, []);

  const settleIdentity = useCallback(
    (identity: WhoAmI | null, publish: boolean, expectedRotation?: ExpectedSessionRotation) => {
      const current = snapshotRef.current;
      const sameSession =
        identity !== null &&
        current.identity !== null &&
        identity.session.id === current.identity.session.id &&
        identity.principal.id === current.identity.principal.id;
      const expectedRemint =
        expectedRotation !== undefined &&
        identity !== null &&
        current.identity !== null &&
        identity.session.id === expectedRotation.session &&
        identity.principal.id === expectedRotation.principal &&
        current.identity.principal.id === expectedRotation.principal;
      // A verified account-security result continues the initiating logical
      // epoch. An unrelated login, including the same principal, still resets.
      const sameOwner = expectedRotation === undefined ? sameSession : expectedRemint;
      verifiedRemintRef.current = expectedRemint && !sameSession && identity !== null
        ? { revision: transitionRevisionRef.current, session: identity.session.id, principal: identity.principal.id }
        : null;
      if (identityVersion(identity) !== identityVersion(current.identity)) {
        transitionRevisionRef.current += 1;
      }
      if (!sameOwner) blockSessionEpoch();
      transitionWorkspaceOwner(identity?.session.id);
      settleSessionEpoch();
      // A successful settle is the single point that clears a degraded session
      // and resets its retry backoff.
      degradedRetriesRef.current = 0;

      if (sameOwner) {
        commit({
          ...current,
          state: stateFor(identity),
          identity,
          failure: null,
          degraded: null,
          checkingRotation: false,
        });
        void current.queries.invalidateQueries();
      } else {
        const fresh = destroySessionCache(current);
        flushSync(() => commit({
          ...fresh,
          state: stateFor(identity),
          identity,
          failure: null,
          degraded: null,
          checkingRotation: false,
        }));
      }
      if (publish) {
        publishChange();
      }
    },
    [commit, destroySessionCache, publishChange],
  );

  const invalidateIdentity = useCallback(() => {
    transitionRevisionRef.current += 1;
    blockSessionEpoch();
    transitionWorkspaceOwner(undefined);
    const current = destroySessionCache(snapshotRef.current);
    commit({ ...current, identity: null, state: { status: 'checking' }, failure: null, degraded: null, checkingRotation: false });
  }, [commit, destroySessionCache]);

  const checkSession = useCallback(
    async function checkSession(
      mode: SessionCheckMode, expectedRotation?: ExpectedSessionRotation,
    ): Promise<void> {
      const blocking = mode === 'blocking' || mode === 'blocking-and-publish';
      const publish = mode === 'blocking-and-publish' || mode === 'refresh-and-publish' || mode === 'rotation-and-publish';
      const request = requestRef.current + 1;
      requestRef.current = request;
      const current = snapshotRef.current;
      if (mode === 'rotation-and-publish') {
        // Hide disclosures while preserving component state until whoami proves
        // whether this was an in-place rotation or a replacement owner.
        flushSync(() => commit({ ...current, checkingRotation: true }));
      }
      if (blocking) {
        commit({
          ...current,
          state:
            current.identity === null ? { status: 'checking' } : { status: 'transitioning' },
          failure: null,
          degraded: null,
        });
      }
      try {
        const identity = await readIdentity();
        if (mountedRef.current && requestRef.current === request) {
          settleIdentity(identity, publish, expectedRotation);
        }
      } catch (error) {
        if (requestRef.current !== request) {
          return;
        }
        if (error instanceof SessionChangedError) {
          invalidateIdentity();
          return checkSession(mode);
        }
        const latest = snapshotRef.current;
        const problem =
          error instanceof Error ? error : new Error('root authentication check failed');
        if (!isSessionEpochBlocked() && latest.identity !== null && !isDefinitivelyExpired(latest.identity)) {
          // A still-valid session must not be buried under the global reload
          // wall by a transient background revalidation blip. Keep painting the
          // last-known-good session and surface the transport error only as the
          // non-latching `degraded` signal; the backoff effect retries it.
          degradedRetriesRef.current += 1;
          commit({
            ...latest,
            state: stateFor(latest.identity),
            failure: null,
            degraded: problem,
          });
        } else {
          // No usable session in hand — either genuinely session-less (initial
          // check, or a failing refresh while anonymous) or holding an identity
          // whose ABSOLUTE deadline has passed, which is dead regardless of the
          // server. The blocking reload wall is the correct answer; a definitively
          // expired session must never keep painting authenticated chrome.
          blockSessionEpoch();
          transitionWorkspaceOwner(undefined);
          commit({
            ...destroySessionCache(latest),
            identity: null,
            state: { status: 'anonymous' },
            failure: problem,
            degraded: null,
            checkingRotation: false,
          });
        }
      }
    },
    [commit, destroySessionCache, invalidateIdentity, settleIdentity],
  );

  const revalidate = useCallback(() => checkSession('blocking'), [checkSession]);

  const replaceFromPeer = useCallback(() => {
    // Remove component disclosures before the replacement cookie is usable.
    flushSync(invalidateIdentity);
    void checkSession('blocking');
  }, [checkSession, invalidateIdentity]);

  useEffect(() => installSessionFence(
    replaceFromPeer,
    () => {
      if (snapshotRef.current.identity !== null) void checkSession('refresh-and-publish');
    },
    (expected) => checkSession('rotation-and-publish', expected),
  ), [checkSession, replaceFromPeer]);

  const refreshSession = useCallback(
    () => {
      // Operation completion must stale the affected session answers now, not
      // one identity round-trip later. The root still owns that broad
      // invalidation and the subsequent whoami check remains the only place an
      // operation-adjacent 401 can end the browser session.
      void snapshotRef.current.queries.invalidateQueries();
      return checkSession('refresh-and-publish');
    },
    [checkSession],
  );
  const reconcileTransition = useCallback(
    () => checkSession('blocking-and-publish'),
    [checkSession],
  );

  // A window refocus is not, on its own, a session change. Password managers
  // blur and refocus the window every time their overlay opens or closes, so a
  // blocking revalidate here would tear the whole tree down to "Loading…" (and,
  // on the login page, keep re-firing an anonymous whoami 401 and resetting the
  // form the manager is trying to fill). Instead re-read identity quietly: keep
  // painting the current session, and only settle when the answer actually
  // differs.
  //
  // Unlike the other checks this one deliberately does NOT claim `requestRef`:
  // that guard cancels in-flight work, and a refocus arriving during a
  // post-mutation `refreshSession` must not cancel its settle/publish. It guards
  // its own late settle instead — against the identity it read, and only if no
  // authoritative check has replaced that identity meanwhile.
  const revalidateOnFocus = useCallback(async () => {
    const baseline = snapshotRef.current;
    if (baseline.state.status !== 'authenticated') {
      return;
    }
    const now = Date.now();
    if (now - lastFocusCheckRef.current < FOCUS_CHECK_INTERVAL_MS) {
      return;
    }
    // Reserve the window up front so a burst of refocus events reads once; a
    // failed read frees it again so a network blip does not burn the interval.
    lastFocusCheckRef.current = now;
    let identity: WhoAmI | null;
    try {
      identity = await readIdentity();
    } catch (error) {
      if (error instanceof SessionChangedError) {
        replaceFromPeer();
        return;
      }
      // A focus-time network blip must not blank the app or drop the session;
      // the expiry timer and the next user action remain the authoritative
      // checks. (A whoami 401 is not a blip — it is an authoritative end of
      // session, so readIdentity returns null and we settle to anonymous below.)
      lastFocusCheckRef.current = 0;
      return;
    }
    const latest = snapshotRef.current;
    if (!mountedRef.current || latest.identity !== baseline.identity) {
      // An authoritative check (mount, refresh, expiry, or a peer tab) settled
      // while we were reading. Defer to it rather than overwrite with a
      // possibly-staler read.
      return;
    }
    if (identityVersion(identity) === identityVersion(latest.identity)) {
      return;
    }
    settleIdentity(identity, true);
  }, [replaceFromPeer, settleIdentity]);

  const captureTransition = useCallback(
    (): SessionTransitionGuard => ({ revision: transitionRevisionRef.current }),
    [],
  );

  const acceptAccountSession = useCallback(
    (identity: AcceptedIdentity, guard: SessionTransitionGuard, transfer: SensitiveStateTransfer): boolean => {
      const current = snapshotRef.current;
      const verified = verifiedRemintRef.current;
      if (verified === null || verified.revision !== guard.revision ||
          verified.session !== identity.session.id || verified.principal !== identity.principal.id ||
          current.state.status !== 'authenticated' || current.identity === null ||
          identity.principal.id !== current.identity.principal.id ||
          identity.session.id !== current.identity.session.id || identity.session.artifact !== 'browser') return false;
      verifiedRemintRef.current = null;
      const hydrated: WhoAmI = {
        session: identity.session,
        principal: identity.principal,
        capabilities: current.identity.capabilities,
      };
      const accepted = transferSensitiveState(transfer, current.queries, {
        sessionId: identity.session.id, principalId: identity.principal.id,
      }, () => {
        requestRef.current += 1;
        // whoami already proved this exact remint. Retire old secrets while
        // preserving the initiating surface for its one-shot transfer.
        retireSensitiveOperations(current.queries);
        void current.queries.cancelQueries();
        current.queries.clear();
        settleIdentity(hydrated, false);
      });
      return accepted;
    },
    [settleIdentity],
  );

  const acceptSession = useCallback(
    (identity: AcceptedIdentity, guard: SessionTransitionGuard) => {
      if (guard.revision !== transitionRevisionRef.current) {
        void reconcileTransition();
        return;
      }
      // A login/step-up result carries no capabilities. Bind the epoch now with
      // a fail-closed default, then hydrate the authoritative value from whoami
      // so an operator's instance chrome appears without waiting for the next
      // revalidation. A whoami always carries capabilities, so re-accepting one
      // (the guard-mismatch reconcile path) skips the extra round trip.
      const hydrated: WhoAmI = {
        session: identity.session,
        principal: identity.principal,
        capabilities: identity.capabilities ?? { instance_operator: false },
      };
      const hydrateCapabilities = identity.capabilities === undefined;
      transitionRevisionRef.current += 1;
      const request = requestRef.current + 1;
      requestRef.current = request;
      const current = snapshotRef.current;
      commit({ ...current, state: { status: 'transitioning' }, failure: null });
      queueMicrotask(() => {
        if (mountedRef.current && requestRef.current === request) {
          settleIdentity(hydrated, true);
          if (hydrateCapabilities) {
            void refreshSession();
          }
        }
      });
    },
    [commit, reconcileTransition, refreshSession, settleIdentity],
  );

  const endSession = useCallback(
    (guard: SessionTransitionGuard) => {
      if (guard.revision !== transitionRevisionRef.current) {
        void reconcileTransition();
        return;
      }
      transitionRevisionRef.current += 1;
      const request = requestRef.current + 1;
      requestRef.current = request;
      const current = snapshotRef.current;
      commit({ ...current, state: { status: 'transitioning' }, failure: null });
      queueMicrotask(() => {
        if (mountedRef.current && requestRef.current === request) {
          settleIdentity(null, true);
        }
      });
    },
    [commit, reconcileTransition, settleIdentity],
  );

  useEffect(() => {
    mountedRef.current = true;
    void revalidate();
    return () => {
      mountedRef.current = false;
      requestRef.current += 1;
    };
  }, [revalidate]);

  useEffect(
    () => () => {
      const current = snapshotRef.current;
      retireSensitiveOperations(current.queries);
      void current.queries.cancelQueries();
      current.queries.clear();
    },
    [],
  );

  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (isSessionStorageEvent(event)) replaceFromPeer();
    };
    globalThis.addEventListener('storage', onStorage);
    return () => globalThis.removeEventListener('storage', onStorage);
  }, [replaceFromPeer]);

  useEffect(() => {
    if (typeof BroadcastChannel !== 'function') {
      return;
    }
    const channel = new BroadcastChannel(SESSION_CHANNEL);
    channel.addEventListener('message', (event) => {
      if (isPeerSessionMessage(event)) {
        replaceFromPeer();
      }
    });
    return () => {
      channel.close();
    };
  }, [replaceFromPeer]);

  useEffect(() => {
    const onFocus = () => {
      checkSessionCookie();
      void revalidateOnFocus();
    };
    const onVisible = () => { if (document.visibilityState === 'visible') onFocus(); };
    document.addEventListener('visibilitychange', onVisible);
    globalThis.addEventListener?.('focus', onFocus);
    return () => {
      globalThis.removeEventListener?.('focus', onFocus);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [revalidateOnFocus]);

  // A degraded (still-authenticated) session recovers on its own: a background
  // retry that backs off while the outage persists. The retry is non-blocking
  // (never tears the tree down to a spinner) and PUBLISHES on settle, so a
  // recovery that supersedes a racing post-mutation refresh still notifies peer
  // tabs rather than swallowing the session change. Each fresh `degraded` error
  // is a new reference, so this re-arms after every failed retry; a successful
  // settle clears `degraded` and the cleanup cancels the pending timer.
  useEffect(() => {
    if (snapshot.degraded === null) {
      return;
    }
    const attempt = Math.max(0, degradedRetriesRef.current - 1);
    const delay = Math.min(DEGRADED_RETRY_MAX_MS, DEGRADED_RETRY_BASE_MS * 2 ** Math.min(attempt, 5));
    const timer = setTimeout(() => void checkSession('refresh-and-publish'), delay);
    return () => clearTimeout(timer);
  }, [checkSession, snapshot.degraded]);

  useEffect(() => {
    if (snapshot.state.status !== 'authenticated' || snapshot.identity === null) {
      return;
    }
    // Once a check has already failed, the recovery cadence is owned elsewhere:
    // the degraded backoff retries a still-valid session, and the failure wall
    // waits for a manual reload. Without this guard the expiry timer, seeing an
    // already-past deadline, would reschedule a blocking revalidate every second
    // through an outage — a request storm that also races the degraded backoff.
    if (snapshot.failure !== null || snapshot.degraded !== null) {
      return;
    }
    const idle = Date.parse(snapshot.identity.session.idle_expires_at);
    const absolute = Date.parse(snapshot.identity.session.absolute_expires_at);
    const expiresAt = Math.min(idle, absolute);
    const delay = Number.isFinite(expiresAt)
      ? Math.min(MAX_TIMEOUT_MS, Math.max(1_000, expiresAt - Date.now()))
      : 1_000;
    const timer = setTimeout(() => void revalidate(), delay);
    return () => clearTimeout(timer);
  }, [revalidate, snapshot.degraded, snapshot.failure, snapshot.identity, snapshot.state.status]);

  const value = {
    state: snapshot.state,
    identity: snapshot.identity,
    failure: snapshot.failure,
    degraded: snapshot.degraded,
    captureTransition,
    acceptSession,
    acceptAccountSession,
    endSession,
    refreshSession,
    revalidate,
  };

  return (
    <div className="session-owner" hidden={snapshot.checkingRotation}>
      <QueryClientProvider key={snapshot.epoch} client={snapshot.queries}>
        <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
      </QueryClientProvider>
    </div>
  );
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (value === null) {
    throw new Error('useAuth must be rendered under AuthProvider');
  }
  return value;
}
