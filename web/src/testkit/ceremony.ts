import type { RevealWindow } from '../api/values.ts';
import type { CeremonyRequest } from '../routes/Ceremony.tsx';

export type Deferred<T> = {
  readonly promise: Promise<T>;
  readonly resolve: (value: T) => void;
  readonly reject: (reason: Error) => void;
};

/** A manually settled promise for latest-task and unmount regressions. */
export function deferred<T>(): Deferred<T> {
  let resolve: (value: T) => void = (_value: T) => { throw new Error('deferred promise not initialized'); };
  let reject: (reason: Error) => void = (_reason: Error) => { throw new Error('deferred promise not initialized'); };
  const promise = new Promise<T>((settle, fail) => {
    resolve = settle;
    reject = fail;
  });
  return { promise, resolve, reject };
}

/** The minimum guard state needed to choose live-window or modal behavior. */
export function revealWindow(live: boolean): RevealWindow {
  return {
    protected: true,
    effective_window_seconds: 0,
    live,
    single_decision: false,
    can_reveal: true,
    totp_offered: false,
  };
}

/** One protected reveal request for controller and modal ownership tests. */
export function ceremonyRequest(name: string): CeremonyRequest {
  return {
    purpose: 'reveal',
    environmentId: name,
    environmentName: name,
    keys: [{ id: `key-${name}`, name: `KEY_${name.toUpperCase()}` }],
    window: revealWindow(false),
  };
}
