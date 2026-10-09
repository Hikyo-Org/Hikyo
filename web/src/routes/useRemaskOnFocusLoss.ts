import { useEffect, useRef } from 'react';

export const FOCUS_REMASK_NOTICE = 'The window lost focus. Reveal again to view the value.';

/** Clear displayed disclosures and invalidate reveals begun before focus loss. */
export function useRemaskOnFocusLoss(remask: () => void) {
  const generation = useRef(0);
  useEffect(() => {
    const clear = () => {
      generation.current += 1;
      remask();
    };
    window.addEventListener('blur', clear);
    document.addEventListener('visibilitychange', clear);
    return () => {
      window.removeEventListener('blur', clear);
      document.removeEventListener('visibilitychange', clear);
    };
  }, [remask]);
  return generation;
}

/** Popup authorization can arrive before the opener regains focus. */
export function waitForDisclosureFocus(signal: AbortSignal): Promise<boolean> {
  if (signal.aborted) return Promise.resolve(false);
  if (!document.hidden && document.hasFocus()) return Promise.resolve(true);
  return new Promise((resolve) => {
    const cleanup = () => {
      window.removeEventListener('focus', onFocus);
      document.removeEventListener('visibilitychange', onFocus);
      signal.removeEventListener('abort', onAbort);
    };
    const onFocus = () => {
      if (document.hidden || !document.hasFocus()) return;
      cleanup();
      resolve(true);
    };
    const onAbort = () => {
      cleanup();
      resolve(false);
    };
    window.addEventListener('focus', onFocus);
    document.addEventListener('visibilitychange', onFocus);
    signal.addEventListener('abort', onAbort, { once: true });
  });
}
