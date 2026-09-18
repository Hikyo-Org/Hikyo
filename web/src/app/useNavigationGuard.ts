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
    const onPopState = () => {
      history.pushState(null, '', window.location.href);
      attempt.current();
    };
    history.pushState(null, '', window.location.href);
    window.addEventListener('beforeunload', onBeforeUnload);
    window.addEventListener('popstate', onPopState);
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload);
      window.removeEventListener('popstate', onPopState);
      history.back();
    };
  }, [active]);
}
