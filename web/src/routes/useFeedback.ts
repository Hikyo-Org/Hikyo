import { useState } from 'react';

/**
 * useFeedback is the one (failure | done) status pair a surface shows after a
 * mutation: reporting a failure clears the last success and vice versa, so the
 * page never shows both "saved" and "refused" for the same act.
 */
export function useFeedback(failureText: (error: unknown) => string) {
  const [state, setState] = useState<{ failure: string | null; done: string | null }>({
    failure: null,
    done: null,
  });
  return {
    failure: state.failure,
    done: state.done,
    report: (error: unknown) => setState({ failure: failureText(error), done: null }),
    ok: (text: string) => setState({ failure: null, done: text }),
    clear: () => setState({ failure: null, done: null }),
  };
}
