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
 */
/** The sentinel entry's state, so a popstate can tell a sentinel from the route. */
const SENTINEL = { hikyoNavigationGuard: true } as const;

function isSentinel(state: unknown): boolean {
  return typeof state === 'object' && state !== null && 'hikyoNavigationGuard' in state && state.hikyoNavigationGuard === true;
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
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    const onPopState = (event: PopStateEvent) => {
      // A Back press from the sentinel lands on the route. Landing on a
      // sentinel instead means the pop is another guard's deactivation: its
      // `history.back()` settles asynchronously, after a guard mounted in the
      // same tick (a sibling dialog opening as one closes, StrictMode's effect
      // replay) has pushed its own sentinel. That pop consumed OUR sentinel and
      // left the stale one under the cursor, so adopt it rather than surface a
      // dismissal nobody attempted. Guards are mutually exclusive today (one
      // dialog owns the screen); two active at once would both adopt the pop
      // and swallow the press, so a second concurrent guard needs a per-guard
      // marker, not a shared one.
      if (isSentinel(event.state)) {
        history.replaceState(SENTINEL, '', window.location.href);
        return;
      }
      history.pushState(SENTINEL, '', window.location.href);
      attempt.current();
    };
    history.pushState(SENTINEL, '', window.location.href);
    window.addEventListener('beforeunload', onBeforeUnload);
    window.addEventListener('popstate', onPopState);
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload);
      window.removeEventListener('popstate', onPopState);
      history.back();
    };
  }, [active]);
}
