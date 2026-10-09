import { useEffect, useRef } from 'react';

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
