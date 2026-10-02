// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { renderForm, settle } from '../testkit/renderForm.tsx';
import { useAdvisoryStream } from './advisory.ts';
import { installSessionFence, settleSessionEpoch } from './sessionEpoch.ts';
import type { TransportOptions } from './transport.tsx';
import { forgetWorkspace, rememberWorkspace } from './workspace.ts';
import { createWorkspaceClient } from './workspaceClient.ts';

const calls = vi.hoisted((): { signal: AbortSignal }[] => []);
vi.mock('@hikyo/operations', async (importOriginal) => ({
  ...await importOriginal<typeof import('@hikyo/operations')>(),
  watchProjectEventsOp: {
    call: async (options: { signal?: AbortSignal; onSseEvent?: () => void }) => {
      if (options.signal === undefined) throw new Error('expected owned stream cancellation');
      const signal = options.signal;
      calls.push({ signal });
      options.onSseEvent?.();
      return {
        stream: (async function* () {
          yield { type: 'revision.published', environment_id: 'env_a', revision: 1 };
          await new Promise<void>((resolve) => {
            if (signal.aborted) resolve();
            else signal.addEventListener('abort', () => resolve(), { once: true });
          });
        })(),
      };
    },
  },
}));

afterEach(() => {
  calls.length = 0;
  vi.restoreAllMocks();
  forgetWorkspace('https://remote.example');
});

function Harness({ transport }: { transport: TransportOptions }) {
  const state = useAdvisoryStream({ org: 'org_a', project: 'project_a' }, transport, () => undefined, true);
  return <output>{state.connection}:{state.recoveries}</output>;
}

test('same-owner browser credential rotation reconnects and catches up once', async () => {
  let cookie = '__Host-hikyo-csrf=first';
  vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
  const dispose = installSessionFence(() => undefined, () => undefined);
  const { container, unmount } = await renderForm(<Harness transport={{}} />);
  await settle();
  expect(container.textContent).toBe('healthy:0');
  const first = calls[0];
  await act(async () => {
    cookie = '__Host-hikyo-csrf=rotated';
    settleSessionEpoch();
  });
  await settle();
  expect(first?.signal.aborted).toBe(true);
  expect(calls).toHaveLength(2);
  expect(container.textContent).toBe('healthy:1');
  await act(async () => settleSessionEpoch());
  expect(calls).toHaveLength(2);
  await unmount();
  dispose();
});

test('same-session workspace bearer remint reconnects and catches up once', async () => {
  const origin = 'https://remote.example';
  const seed = (value: string) => rememberWorkspace({
    origin, value, session: 'ses_same',
    idleExpiresAt: '2099-01-01T00:00:00Z', absoluteExpiresAt: '2099-01-01T00:00:00Z',
  });
  seed('first');
  const { container, unmount } = await renderForm(<Harness transport={{ client: createWorkspaceClient(origin) }} />);
  await settle();
  const first = calls[0];
  await act(async () => seed('rotated'));
  await settle();
  expect(first?.signal.aborted).toBe(true);
  expect(calls).toHaveLength(2);
  expect(container.textContent).toBe('healthy:1');
  await unmount();
});
