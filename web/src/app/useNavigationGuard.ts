import { useEffect, useRef } from 'react';

/**
 * useNavigationGuard keeps navigation from destroying what dismissal is not
 * allowed to.
 *
 * `dismissDecision` gates Escape and the buttons, but the browser has two more
 * ways to unmount this dialog: unload (reload, tab close, external navigation)
 * and the Back button, which pops the route out from under the component. The
 * first gets the platform's `beforeunload` confirmation; the second gets a
 * history sentinel, a duplicate entry pushed while the guard is active, so a
 * Back press consumes the sentinel instead of the route, the URL never changes,
 * and the press is surfaced as a dismissal ATTEMPT routed through the same
 * gate as Escape. Deactivating consumes the sentinel again so Back is not a
 * double-press afterwards.
 *
 * Guards can overlap (a page may open a second guarded dialog while a mint is
 * in flight) and deactivation's `history.back()` settles asynchronously, after
 * a guard activated in the same tick has pushed its own sentinel. Each sentinel
 * therefore names its owner, and the module keeps the activation order of the
 * live guards: only the newest live guard answers a popstate, and it reads the
 * entry it landed on to tell a real Back press (the route, or an older live
 * guard's sentinel) from a stale sentinel a finished guard left behind.
 */

type Sentinel = { readonly hikyoNavigationGuard: number };

/** Live guards in activation order; the last one owns the Back button. */
const live: number[] = [];
let nextGuardId = 0;

function sentinelOwner(state: unknown): number | null {
  if (typeof state !== 'object' || state === null || !('hikyoNavigationGuard' in state)) {
    return null;
  }
  const owner = state.hikyoNavigationGuard;
  return typeof owner === 'number' ? owner : null;
}

export function useNavigationGuard(active: boolean, onAttempt: () => void) {
  const attempt = useRef(onAttempt);
  useEffect(() => {
    attempt.current = onAttempt;
  }, [onAttempt]);
  useEffect(() => {
    if (!active) {
      return;
    }
    const id = nextGuardId++;
    const sentinel: Sentinel = { hikyoNavigationGuard: id };
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    const onPopState = (event: PopStateEvent) => {
      if (live[live.length - 1] !== id) {
        // An older guard under a newer one: the newest owns the press.
        return;
      }
      const owner = sentinelOwner(event.state);
      if (owner === id) {
        // A newer guard's deactivation settled onto our own entry. Nothing
        // was pressed; the stack is already where it should be.
        return;
      }
      if (owner !== null && !live.includes(owner)) {
        // A finished guard's sentinel, left under the cursor because its
        // `history.back()` consumed ours instead. Adopt it rather than
        // surface a dismissal nobody attempted.
        history.replaceState(sentinel, '', window.location.href);
        return;
      }
      // The route, or an older live guard's sentinel: a real Back press.
      history.pushState(sentinel, '', window.location.href);
      attempt.current();
    };
    live.push(id);
    history.pushState(sentinel, '', window.location.href);
    window.addEventListener('beforeunload', onBeforeUnload);
    window.addEventListener('popstate', onPopState);
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload);
      window.removeEventListener('popstate', onPopState);
      live.splice(live.indexOf(id), 1);
      history.back();
    };
  }, [active]);
}
