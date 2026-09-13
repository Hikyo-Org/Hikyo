import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import type { QueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import { retireSensitiveOperations } from '../api/sensitiveMutation.ts';
import { useModalDialog } from '../routes/useModalDialog.ts';

const runtimeStatus = z.discriminatedUnion('state', [
  z.object({ state: z.literal('ready'), phase: z.null() }),
  z.object({ state: z.literal('maintenance'), phase: z.enum(['preparing', 'backup', 'restore-check', 'migration', 'health-check']) }),
  z.object({ state: z.literal('recovery-required'), phase: z.null() }),
]);
type RuntimeStatus = z.infer<typeof runtimeStatus>;
type DisplayStatus = RuntimeStatus | { state: 'reconnecting' };
const visible = () => document.visibilityState !== 'hidden';

const phaseText = {
  preparing: 'Preparing the upgrade.',
  backup: 'Creating a recovery backup.',
  'restore-check': 'Checking that the backup can be restored.',
  migration: 'Updating the database.',
  'health-check': 'Checking the upgraded instance.',
};

/** Public HOME-instance status only. Never uses workspace bearer/origin state. */
export function RuntimeMaintenanceBoundary({ children, failure, refreshSession, queries }: {
  children: ReactNode;
  failure: Error | null;
  refreshSession: (signal?: AbortSignal) => Promise<void>;
  queries: QueryClient;
}) {
  const [status, setStatus] = useState<DisplayStatus>({ state: 'ready', phase: null });
  const latest = useRef({ failure, refreshSession });
  latest.current = { failure, refreshSession };

  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let timeout: ReturnType<typeof setTimeout> | undefined;
    let request: AbortController | undefined;
    let failures = 0;
    let interrupted = false;
    let running = false;

    const poll = async () => {
      if (disposed || running || !visible()) return;
      running = true;
      request = new AbortController();
      timeout = setTimeout(() => request?.abort(), 8_000);
      let delay = 15_000;
      try {
        const response = await fetch('/api/v1/runtime/status', {
          signal: request.signal, credentials: 'omit', cache: 'no-store', redirect: 'error',
        });
        // Only a validated status body confirms maintenance. A proxy's HTML
        // 503 or an offline socket says nothing about the cause of the outage.
        if (!response.ok && response.status !== 503 && response.status !== 404) throw new Error('Runtime status unavailable');
        const result = runtimeStatus.parse(response.status === 404 ? { state: 'ready', phase: null } : await response.json());
        if (disposed) return;
        failures = 0;
        if (result.state === 'ready' && (interrupted || latest.current.failure !== null)) {
          // Keep editing blocked until the root session has been revalidated.
          await latest.current.refreshSession(request.signal);
          request.signal.throwIfAborted();
          if (disposed) return;
        }
        interrupted = result.state !== 'ready';
        setStatus(result);
        if (result.state !== 'ready' || latest.current.failure !== null) delay = 2_000;
      } catch {
        if (disposed) return;
        interrupted = true;
        failures += 1;
        delay = Math.min(30_000, 1_000 * 2 ** Math.min(failures - 1, 5));
        setStatus({ state: 'reconnecting' });
      } finally {
        clearTimeout(timeout);
        running = false;
        request = undefined;
        if (!disposed && visible()) timer = setTimeout(() => void poll(), delay);
      }
    };
    const wake = () => {
      clearTimeout(timer);
      if (document.visibilityState !== 'hidden') void poll();
    };
    void poll();
    document.addEventListener('visibilitychange', wake);
    globalThis.addEventListener('online', wake);
    return () => {
      disposed = true;
      clearTimeout(timer);
      clearTimeout(timeout);
      request?.abort();
      document.removeEventListener('visibilitychange', wake);
      globalThis.removeEventListener('online', wake);
    };
  }, []);

  const blocked = status.state !== 'ready' || failure !== null;
  useLayoutEffect(() => {
    if (blocked) {
      retireSensitiveOperations(queries);
      void queries.cancelQueries();
    }
  }, [blocked, queries]);

  return <>
    <div inert={blocked} aria-hidden={blocked || undefined}>{children}</div>
    {blocked && <RuntimeInterruption status={status.state === 'ready' ? { state: 'reconnecting' } : status} />}
  </>;
}

function RuntimeInterruption({ status }: { status: DisplayStatus }) {
  const heading = useRef<HTMLHeadingElement>(null);
  const dialog = useModalDialog(heading);
  const title = status.state === 'maintenance' ? 'Hikyo is upgrading'
    : status.state === 'recovery-required' ? 'Recovery needs an operator'
    : 'Reconnecting to Hikyo';
  const detail = status.state === 'maintenance'
    ? phaseText[status.phase]
    : status.state === 'recovery-required'
      ? 'This instance could not complete its upgrade safely. An operator must recover it before editing can resume.'
      : 'This browser cannot reach its home instance. The cause is not yet confirmed.';
  return <dialog ref={dialog} className="ceremony runtime-maintenance" aria-labelledby="runtime-maintenance-title"
    aria-describedby="runtime-maintenance-detail" onCancel={(event) => event.preventDefault()}>
    <h1 ref={heading} tabIndex={-1} id="runtime-maintenance-title" className="ceremony__title">{title}</h1>
    <p id="runtime-maintenance-detail" className="ceremony__lede" role="status" aria-live="polite" aria-atomic="true">{detail}</p>
    <p className="ceremony__lede">Editing is paused. This page will reconnect automatically. Unsaved changes are never submitted automatically.</p>
  </dialog>;
}
