import { useEffect, useRef, useState } from 'react';

import { cryptoFailureText, type SettingsOperation } from '../api/settings.ts';

/**
 * useReencryptDrain drives a chunked, idempotent re-encryption to completion
 * (#503). The server walks the ciphertext in bounded chunks, returning how many
 * rows a run moved; the drain re-invokes until a run moves nothing, which is the
 * only honest "complete" signal (there is no status endpoint). It is safe to
 * re-run after a refresh, disconnect or crash, the server resumes from its own
 * cursor.
 *
 * Lifecycle correctness the loop must hold:
 *  - abort on unmount is re-checked AFTER every await, so a request that resolves
 *    for an unmounted page neither updates state nor emits a completion notice;
 *  - committed progress survives a mid-drain failure: `runs` and `total` count
 *    only successful runs (a throwing run increments neither), and the failure
 *    text carries that partial total so the operator sees what already moved and
 *    that re-running resumes rather than restarts.
 */
export function useReencryptDrain(
  reencrypt: { mutateAsync: () => Promise<{ rows_moved: bigint }> },
  options: { operation: SettingsOperation; noun: 'Instance' | 'Project'; onDone: (message: string) => void },
): { running: boolean; runs: number; total: bigint; failure: string | null; run: () => void } {
  const [state, setState] = useState<{ running: boolean; runs: number; total: bigint }>({
    running: false,
    runs: 0,
    total: 0n,
  });
  const [failure, setFailure] = useState<string | null>(null);
  const abort = useRef(false);
  useEffect(() => () => { abort.current = true; }, []);

  const run = () => {
    void (async () => {
      setFailure(null);
      setState({ running: true, runs: 0, total: 0n });
      let runs = 0;
      let total = 0n;
      try {
        for (;;) {
          const result = await reencrypt.mutateAsync();
          if (abort.current) return;
          runs += 1;
          total += result.rows_moved;
          setState({ running: true, runs, total });
          if (result.rows_moved === 0n) break;
        }
        setState({ running: false, runs, total });
        options.onDone(
          total === 0n
            ? `${options.noun} re-encryption complete: nothing to move; all ciphertext is already on the active DEK version.`
            : `${options.noun} re-encryption complete: moved ${total} ciphertext row${total === 1n ? '' : 's'} onto the active DEK version across ${runs} run${runs === 1 ? '' : 's'}.`,
        );
      } catch (error) {
        if (abort.current) return;
        setState((current) => ({ ...current, running: false }));
        const base = cryptoFailureText(error, options.operation);
        setFailure(
          total > 0n
            ? `${base} ${total} ciphertext row${total === 1n ? '' : 's'} moved across ${runs} run${runs === 1 ? '' : 's'} before this failure; the walk is idempotent, so re-running resumes rather than restarts.`
            : base,
        );
      }
    })();
  };

  return { running: state.running, runs: state.runs, total: state.total, failure, run };
}
