import { QueryClientContext, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useContext, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';

// This registry contains only lifetime counters and cleanup callbacks, never
// request variables, results, credentials, or retryable work.
type Lifetime = { generation: number; listeners: Set<() => void> };
const lifetimes = new WeakMap<QueryClient, Lifetime>();
const transferState = Symbol('sensitive-state-transfer');
export type SensitiveSessionOwner = { readonly sessionId: string; readonly principalId: string };
export type SensitiveStateTransfer = {
  readonly [transferState]: (client: QueryClient, owner: SensitiveSessionOwner, accept: () => void) => boolean;
};

/** Consume a single component's handoff during an authoritative synchronous
 * session replacement. The capability's closure owns the only state setter. */
export function transferSensitiveState(
  transfer: SensitiveStateTransfer,
  client: QueryClient,
  owner: SensitiveSessionOwner,
  accept: () => void,
): boolean {
  return transfer[transferState](client, owner, accept);
}
function lifetimeFor(client: QueryClient): Lifetime {
  const existing = lifetimes.get(client);
  if (existing !== undefined) return existing;
  const lifetime: Lifetime = { generation: 0, listeners: new Set() };
  lifetimes.set(client, lifetime);
  return lifetime;
}

export function retireSensitiveOperations(client: QueryClient): void {
  const lifetime = lifetimeFor(client);
  lifetime.generation += 1;
  for (const clear of lifetime.listeners) clear();
}

type Callbacks<Result, Input, Context> = {
  onSuccess?: (result: Result, input: Input, context: Context | undefined) => void | Promise<void>;
  onError?: (error: Error, input: Input, context: Context | undefined) => void | Promise<void>;
  onSettled?: (result: Result | undefined, error: Error | null, input: Input, context: Context | undefined) => void | Promise<void>;
};
type Options<Result, Input, Context> = Callbacks<Result, Input, Context> & {
  mutationFn: (input: Input) => Promise<Result>;
  onMutate?: (input: Input) => Context;
};
type State = { status: 'idle' | 'pending' | 'success' | 'error'; error: Error | null };

/** A one-shot component operation. Plaintext is never registered with TanStack,
 * retained as hook data/variables, queued offline, or automatically retried.
 * reset, unmount and session retirement revoke delivery of pending results.
 */
export function useSensitiveMutation<Result, Input = void, Context = undefined>(
  options: Options<Result, Input, Context>,
) {
  const queries = useQueryClient();
  const lifetime = lifetimeFor(queries);
  const active = useRef(0);
  const mounted = useRef(false);
  const [state, setState] = useState<State>({ status: 'idle', error: null });
  function reset() {
    active.current += 1;
    if (mounted.current) setState({ status: 'idle', error: null });
  }
  useEffect(() => {
    mounted.current = true;
    const clear = () => {
      active.current += 1;
      setState({ status: 'idle', error: null });
    };
    lifetime.listeners.add(clear);
    return () => {
      mounted.current = false;
      active.current += 1;
      lifetime.listeners.delete(clear);
    };
  }, [lifetime]);

  async function mutateAsync(input: Input, callbacks?: Callbacks<Result, Input, Context>): Promise<Result> {
    const operation = ++active.current;
    const generation = lifetime.generation;
    const current = () => mounted.current && operation === active.current && generation === lifetime.generation;
    const requireCurrent = () => {
      if (!current()) throw new DOMException('The sensitive operation surface has closed.', 'AbortError');
    };
    requireCurrent();
    setState({ status: 'pending', error: null });
    let context: Context | undefined;
    let result: Result | undefined;
    let failure: Error | null = null;
    try {
      context = options.onMutate?.(input);
      result = await options.mutationFn(input);
      requireCurrent();
      await options.onSuccess?.(result, input, context);
      requireCurrent();
      await callbacks?.onSuccess?.(result, input, context);
      requireCurrent();
      return result;
    } catch (cause) {
      requireCurrent();
      failure = cause instanceof Error ? cause : new Error('The operation failed.');
      await options.onError?.(failure, input, context);
      if (current()) await callbacks?.onError?.(failure, input, context);
      throw failure;
    } finally {
      try {
        requireCurrent();
        await options.onSettled?.(result, failure, input, context);
        requireCurrent();
        await callbacks?.onSettled?.(result, failure, input, context);
        // A pending return survives an awaited finally block. Revoke it too,
        // not just the callbacks, if the surface retired during finalization.
        requireCurrent();
        setState({ status: failure === null ? 'success' : 'error', error: failure });
      } catch (cause) {
        requireCurrent();
        const finalizerError = cause instanceof Error ? cause : new Error('The operation could not finish.');
        setState({ status: 'error', error: finalizerError });
        throw finalizerError;
      }
    }
  }
  function mutate(input: Input, callbacks?: Callbacks<Result, Input, Context>): void {
    void mutateAsync(input, callbacks).catch(() => { /* Refusal is component state, never a replay. */ });
  }
  return { mutate, mutateAsync, reset, error: state.error, status: state.status,
    isPending: state.status === 'pending', isError: state.status === 'error',
    isSuccess: state.status === 'success', isIdle: state.status === 'idle' };
}

/** Component-owned plaintext with a session retirement boundary. A setter from
 * an earlier surface/session cannot repopulate state after an async response.
 * The initial value must be empty, never a previously disclosed credential.
 */
export function useSensitiveState<Value>(empty: Value | (() => Value)): [
  Value,
  Dispatch<SetStateAction<Value>>,
  (value: Value, owner: SensitiveSessionOwner) => SensitiveStateTransfer,
] {
  const queries = useContext<QueryClient | undefined>(QueryClientContext);
  const lifetime = queries === undefined ? undefined : lifetimeFor(queries);
  const generation = lifetime?.generation;
  const initial = useRef(empty);
  const mounted = useRef(false);
  const revision = useRef(0);
  const [value, setValue] = useState(empty);
  useEffect(() => {
    mounted.current = true;
    const clear = () => setValue(initial.current);
    clear();
    lifetime?.listeners.add(clear);
    return () => {
      mounted.current = false;
      revision.current += 1;
      lifetime?.listeners.delete(clear);
    };
  }, [lifetime]);
  const set: Dispatch<SetStateAction<Value>> = (next) => {
    if (mounted.current && lifetime?.generation === generation) {
      revision.current += 1;
      setValue(next);
    }
  };
  const prepareTransfer = (next: Value, target: SensitiveSessionOwner): SensitiveStateTransfer => {
    const sourceRevision = revision.current;
    const sessionId = target.sessionId;
    const principalId = target.principalId;
    let pending: { value: Value } | undefined = { value: next };
    return {
      [transferState]: (client, owner, accept) => {
        const result = pending;
        pending = undefined;
        if (result === undefined || client !== queries || lifetime === undefined ||
            !mounted.current || revision.current !== sourceRevision ||
            lifetime.generation !== generation || owner.sessionId !== sessionId ||
            owner.principalId !== principalId) return false;
        // Retirement clears every old value and operation. Only the exact next
        // generation may receive this result; no asynchronous gap is allowed.
        accept();
        if (!mounted.current || revision.current !== sourceRevision ||
            lifetime.generation !== generation + 1) return false;
        revision.current += 1;
        setValue(() => result.value);
        return true;
      },
    };
  };
  return [value, set, prepareTransfer];
}
