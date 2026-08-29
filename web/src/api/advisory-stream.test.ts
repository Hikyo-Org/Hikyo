import { afterEach, describe, expect, it, vi } from 'vitest';

import { watchProjectAdvisoryStream } from './advisory.ts';

/**
 * The stream loop is tested against a scripted fake of the generated stream
 * operation, because the real one is a fetch transport: what this module owns
 * is the STATE MACHINE around the iteration — parse, dispatch, reconnect after
 * a clean end, stop on abort — and a script can hold all of that while the
 * hey-api transport itself is proven by the flow suite against the real
 * server, which is the only honest way to prove an SSE handshake.
 */

const ref = { org: 'org_a', project: 'project_a' };

const hoisted = vi.hoisted(() => ({
  calls: [] as { readonly options: Record<string, unknown> }[],
  script: [] as ((options: Record<string, unknown>) => { stream: AsyncGenerator<unknown> })[],
}));

vi.mock('@hikyo/operations', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@hikyo/operations')>();
  return {
    ...actual,
    watchProjectEventsOp: {
      call: async (options: unknown) => {
        hoisted.calls.push({ options: options as Record<string, unknown> });
        const step = hoisted.script.shift();
        if (step === undefined) {
          return hang(options);
        }
        return step(options as Record<string, unknown>);
      },
    },
  };
});

afterEach(() => {
  hoisted.calls.length = 0;
  hoisted.script.length = 0;
});

/** hang mirrors a stream the server never ends: iteration blocks until abort. */
function hang(options: unknown): { stream: AsyncGenerator<unknown> } {
  const signal = (options as { signal?: AbortSignal }).signal;
  return {
    stream: (async function* () {
      await new Promise<void>((resolve) => {
        signal?.addEventListener('abort', () => resolve(), { once: true });
      });
    })(),
  };
}

/** script queues one stream that yields the events, then ends cleanly. */
function script(...events: readonly unknown[]): void {
  hoisted.script.push(() => ({
    stream: (async function* () {
      for (const event of events) {
        yield event;
      }
    })(),
  }));
}

/** failingScript queues one stream whose iteration throws immediately. */
function failingScript(): void {
  hoisted.script.push(() => ({
    stream: (async function* () {
      throw new Error('connection refused');
    })(),
  }));
}

function handlers(state: { events: unknown[]; states: string[] }) {
  return {
    onEvent: (event: unknown) => {
      state.events.push(event);
    },
    onState: (connection: string) => {
      state.states.push(connection);
    },
  };
}

describe('watchProjectAdvisoryStream', () => {
  it('parses and dispatches events, skips unknown ones, and reconnects after a clean end', async () => {
    vi.useFakeTimers();
    try {
      const state = { events: [] as unknown[], states: [] as string[] };
      script(
        { type: 'revision.published', environment_id: 'env_a', revision: 3 },
        { type: 'environment.created', environment_id: 'env_x' },
        { type: 'cell.changed', environment_id: 'env_a', key_id: 'key_1', name: 'LOG_LEVEL', revision: 3 },
      );
      // Nothing further is scripted: the reconnect hangs open, as a healthy
      // stream would, and no further calls are made in the window.
      const handle = watchProjectAdvisoryStream(ref, {}, handlers(state));
      await vi.advanceTimersByTimeAsync(10_000);
      expect(state.events).toEqual([
        { type: 'revision.published', environmentId: 'env_a', revision: 3n },
        { type: 'cell.changed', environmentId: 'env_a', keyId: 'key_1', keyName: 'LOG_LEVEL', revision: 3n },
      ]);
      expect(hoisted.calls.length).toBe(2);
      expect(hoisted.calls[0]?.options['path']).toEqual({ org: ref.org, project: ref.project });
      handle.stop();
    } finally {
      vi.useRealTimers();
    }
  });

  it('marks the fallback state when a stream fails, then reconnects after backoff', async () => {
    vi.useFakeTimers();
    try {
      const state = { events: [] as unknown[], states: [] as string[] };
      failingScript();
      // Nothing further is scripted: the reconnect hangs open.
      const handle = watchProjectAdvisoryStream(ref, {}, handlers(state));
      await vi.advanceTimersByTimeAsync(0);
      expect(state.states).toContain('failed');
      expect(hoisted.calls.length).toBe(1);
      await vi.advanceTimersByTimeAsync(5_000);
      expect(hoisted.calls.length).toBe(2);
      handle.stop();
    } finally {
      vi.useRealTimers();
    }
  });

  it('stops subscribing when disposed, even mid-backoff', async () => {
    vi.useFakeTimers();
    try {
      const state = { events: [] as unknown[], states: [] as string[] };
      script();
      const handle = watchProjectAdvisoryStream(ref, {}, handlers(state));
      await vi.advanceTimersByTimeAsync(0);
      expect(hoisted.calls.length).toBe(1);
      await handle.stop();
      await vi.advanceTimersByTimeAsync(60_000);
      expect(hoisted.calls.length).toBe(1);
    } finally {
      vi.useRealTimers();
    }
  });
});
