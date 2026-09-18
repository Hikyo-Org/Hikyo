// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { WorkspaceError, type PreparedWorkspace, type StepUpParams } from '../api/workspace.ts';
import {
  useWorkspaceHandoff,
  workspaceHandoffAction,
  type WorkspaceHandoffPreparation,
} from './useWorkspaceHandoff.ts';

const workspace = vi.hoisted(() => ({
  prepareWorkspace:
    vi.fn<
      (
        origin: string,
        request: { signal: AbortSignal; stepUp?: StepUpParams },
      ) => Promise<PreparedWorkspace>
    >(),
  openPrepared:
    vi.fn<(prepared: PreparedWorkspace, request: { signal: AbortSignal }) => Promise<void>>(),
}));

vi.mock('../api/workspace.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/workspace.ts')>();
  return {
    ...actual,
    prepareWorkspace: workspace.prepareWorkspace,
    openPrepared: workspace.openPrepared,
  };
});

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const origin = 'https://remote.example';
const prepared: PreparedWorkspace = {
  origin,
  state: 'state-1',
  verifier: 'verifier-1',
  approveURL: `${origin}/workspace/approve?state=state-1`,
};

afterEach(() => {
  workspace.prepareWorkspace.mockReset();
  workspace.openPrepared.mockReset();
  document.body.replaceChildren();
});

describe('useWorkspaceHandoff', () => {
  it('moves from contacting through authorising to success without re-arming', async () => {
    const preparation = deferred<PreparedWorkspace>();
    const authorisation = deferred<void>();
    const authorised = vi.fn();
    workspace.prepareWorkspace.mockReturnValue(preparation.promise);
    workspace.openPrepared.mockReturnValue(authorisation.promise);

    const mounted = await renderHandoff(authorised);
    expect(action(mounted.container)).toMatchObject({ disabled: true, textContent: 'Contacting…' });

    await act(async () => preparation.resolve(prepared));
    expect(action(mounted.container)).toMatchObject({
      disabled: false,
      textContent: `Continue to ${origin} to sign in`,
    });

    act(() => action(mounted.container).click());
    expect(workspace.openPrepared).toHaveBeenCalledOnce();
    expect(action(mounted.container)).toMatchObject({
      disabled: true,
      textContent: 'Waiting for sign-in…',
    });
    action(mounted.container).click();
    expect(workspace.openPrepared).toHaveBeenCalledOnce();

    await act(async () => authorisation.resolve(undefined));
    expect(authorised).toHaveBeenCalledOnce();
    await unmount(mounted.root);
  });

  it('exposes retry after preparation fails and leaves no false contacting state', async () => {
    const retry = deferred<PreparedWorkspace>();
    workspace.prepareWorkspace
      .mockRejectedValueOnce(new Error('offline'))
      .mockReturnValueOnce(retry.promise);

    const mounted = await renderHandoff(vi.fn());
    await settle();

    expect(mounted.container.querySelector('[role="alert"]')?.textContent).toBe(
      'Could not contact remote.',
    );
    expect(action(mounted.container)).toMatchObject({ disabled: false, textContent: 'Try again' });

    act(() => action(mounted.container).click());
    expect(action(mounted.container)).toMatchObject({ disabled: true, textContent: 'Contacting…' });
    expect(workspace.prepareWorkspace).toHaveBeenCalledTimes(2);

    await act(async () => retry.resolve(prepared));
    expect(action(mounted.container)).toMatchObject({
      disabled: false,
      textContent: `Continue to ${origin} to sign in`,
    });
    await unmount(mounted.root);
  });

  it('does not report a completed handoff after its consumer unmounts', async () => {
    const authorisation = deferred<void>();
    const authorised = vi.fn();
    workspace.prepareWorkspace.mockResolvedValue(prepared);
    workspace.openPrepared.mockReturnValue(authorisation.promise);

    const mounted = await renderHandoff(authorised);
    await settle();
    act(() => action(mounted.container).click());
    await unmount(mounted.root);
    await act(async () => authorisation.resolve(undefined));

    expect(authorised).not.toHaveBeenCalled();
  });

  it('fails with a retry affordance when a step-up bearer is unavailable', async () => {
    const mounted = await renderHandoff(vi.fn(), {
      preparation: {
        kind: 'refused',
        message: 'This workspace is no longer connected.',
      },
    });

    expect(workspace.prepareWorkspace).not.toHaveBeenCalled();
    expect(mounted.container.querySelector('[role="alert"]')?.textContent).toBe(
      'This workspace is no longer connected.',
    );
    expect(action(mounted.container)).toMatchObject({ disabled: false, textContent: 'Try again' });
    await unmount(mounted.root);
  });

  it('replaces an in-flight contacting state immediately when preparation becomes unavailable', async () => {
    workspace.prepareWorkspace.mockReturnValue(deferred<PreparedWorkspace>().promise);
    const mounted = await renderHandoff(vi.fn());
    expect(action(mounted.container).textContent).toBe('Contacting…');

    await act(async () =>
      mounted.root.render(
        <HandoffHarness
          onAuthorised={vi.fn()}
          preparation={{ kind: 'refused', message: 'Workspace disconnected.' }}
        />,
      ),
    );

    expect(mounted.container.textContent).not.toContain('Contacting…');
    expect(mounted.container.querySelector('[role="alert"]')?.textContent).toBe(
      'Workspace disconnected.',
    );
    expect(action(mounted.container).textContent).toBe('Try again');
    await unmount(mounted.root);
  });

  it('prepares once for stable step-up content and again when the target changes', async () => {
    workspace.prepareWorkspace.mockResolvedValue(prepared);
    const prepareArgs: StepUpParams = {
      session: 'session-1',
      operation: 'reveal',
      environment: 'environment-1',
      keySet: ['key-1'],
    };
    const mounted = await renderHandoff(vi.fn(), {
      preparation: { kind: 'step-up', params: prepareArgs },
    });
    await settle();

    await act(async () =>
      mounted.root.render(
        <HandoffHarness
          onAuthorised={vi.fn()}
          preparation={{
            kind: 'step-up',
            params: { ...prepareArgs, keySet: ['key-1'] },
          }}
        />,
      ),
    );
    expect(workspace.prepareWorkspace).toHaveBeenCalledOnce();

    await act(async () =>
      mounted.root.render(
        <HandoffHarness
          onAuthorised={vi.fn()}
          preparation={{
            kind: 'step-up',
            params: { ...prepareArgs, keySet: ['key-2'] },
          }}
        />,
      ),
    );
    expect(workspace.prepareWorkspace).toHaveBeenCalledTimes(2);
    await unmount(mounted.root);
  });

  // Each attempt owns an AbortController. Disposal and supersession abort the
  // requests themselves, and a disposed attempt's outcome is dropped without a
  // state write; only a live attempt's deadline becomes a retryable failure.
  describe('attempt ownership', () => {
    it('aborts the in-flight prepare on unmount and writes no state', async () => {
      workspace.prepareWorkspace.mockImplementation((_origin, { signal }) => abortsWith(signal));
      const onFailMessage = vi.fn(() => 'unexpected');

      const mounted = await renderHandoff(vi.fn(), { onFailMessage });
      const request = workspace.prepareWorkspace.mock.calls[0]?.[1];
      expect(request?.signal.aborted).toBe(false);

      await unmount(mounted.root);
      await settle();

      expect(request?.signal.aborted).toBe(true);
      expect(onFailMessage).not.toHaveBeenCalled();
    });

    it('aborts the previous attempt when the target changes and a retry supersedes it', async () => {
      workspace.prepareWorkspace.mockImplementation((_origin, { signal }) => abortsWith(signal));
      const onFailMessage = vi.fn(() => 'unexpected');
      const params: StepUpParams = {
        session: 'session-1',
        operation: 'reveal',
        environment: 'environment-1',
        keySet: ['key-1'],
      };
      const mounted = await renderHandoff(vi.fn(), {
        onFailMessage,
        preparation: { kind: 'step-up', params },
      });

      await act(async () =>
        mounted.root.render(
          <HandoffHarness
            onAuthorised={vi.fn()}
            onFailMessage={onFailMessage}
            preparation={{ kind: 'step-up', params: { ...params, keySet: ['key-2'] } }}
          />,
        ),
      );

      expect(workspace.prepareWorkspace).toHaveBeenCalledTimes(2);
      expect(workspace.prepareWorkspace.mock.calls[0]?.[1].signal.aborted).toBe(true);
      expect(workspace.prepareWorkspace.mock.calls[1]?.[1].signal.aborted).toBe(false);
      expect(onFailMessage).not.toHaveBeenCalled();
      expect(action(mounted.container).textContent).toBe('Contacting…');
      await unmount(mounted.root);
    });

    it('lands a deadline failure in the failed phase with retry available', async () => {
      const deadline = new WorkspaceError(`${origin} did not answer within 15 seconds. Try again.`);
      workspace.prepareWorkspace
        .mockRejectedValueOnce(deadline)
        .mockReturnValueOnce(deferred<PreparedWorkspace>().promise);
      const onFailMessage = vi.fn((error: unknown) =>
        error instanceof WorkspaceError ? error.message : 'unexpected',
      );

      const mounted = await renderHandoff(vi.fn(), { onFailMessage });
      await settle();

      expect(onFailMessage).toHaveBeenCalledExactlyOnceWith(deadline, 'prepare');
      expect(mounted.container.querySelector('[role="alert"]')?.textContent).toBe(deadline.message);
      expect(action(mounted.container)).toMatchObject({ disabled: false, textContent: 'Try again' });

      act(() => action(mounted.container).click());
      expect(action(mounted.container)).toMatchObject({ disabled: true, textContent: 'Contacting…' });
      expect(workspace.prepareWorkspace).toHaveBeenCalledTimes(2);
      await unmount(mounted.root);
    });

    it('aborts the ceremony when its consumer unmounts while authorising', async () => {
      workspace.prepareWorkspace.mockResolvedValue(prepared);
      workspace.openPrepared.mockImplementation((_prepared, { signal }) => abortsWith(signal));
      const onFailMessage = vi.fn(() => 'unexpected');
      const authorised = vi.fn();

      const mounted = await renderHandoff(authorised, { onFailMessage });
      await settle();
      act(() => action(mounted.container).click());
      const request = workspace.openPrepared.mock.calls[0]?.[1];
      expect(request?.signal.aborted).toBe(false);

      await unmount(mounted.root);
      await settle();

      expect(request?.signal.aborted).toBe(true);
      expect(authorised).not.toHaveBeenCalled();
      expect(onFailMessage).not.toHaveBeenCalled();
    });
  });
});

/** Settles the way a real request does: only when its signal aborts. */
function abortsWith<T>(signal: AbortSignal): Promise<T> {
  return new Promise<T>((_resolve, reject) => {
    signal.addEventListener('abort', () => reject(signal.reason), { once: true });
  });
}

function HandoffHarness({
  onAuthorised,
  onFailMessage = (_error, stage) =>
    stage === 'prepare' ? 'Could not contact remote.' : 'Sign-in did not complete.',
  preparation = { kind: 'establishment' },
}: {
  onAuthorised: () => void;
  onFailMessage?: (error: unknown, stage: 'prepare' | 'authorise') => string;
  preparation?: WorkspaceHandoffPreparation;
}) {
  const handoff = useWorkspaceHandoff(origin, {
    preparation,
    onFailMessage,
    onAuthorised,
  });
  const button = workspaceHandoffAction(handoff, {
    ready: `Continue to ${origin} to sign in`,
    authorising: 'Waiting for sign-in…',
  });

  return (
    <>
      {handoff.phase.kind === 'failed' ? <p role="alert">{handoff.phase.message}</p> : null}
      <button
        type="button"
        disabled={button.disabled}
        onClick={button.onClick}
      >
        {button.label}
      </button>
    </>
  );
}

async function renderHandoff(
  onAuthorised: () => void,
  options?: {
    readonly preparation?: WorkspaceHandoffPreparation;
    readonly onFailMessage?: (error: unknown, stage: 'prepare' | 'authorise') => string;
  },
) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <HandoffHarness
        onAuthorised={onAuthorised}
        onFailMessage={options?.onFailMessage}
        preparation={options?.preparation}
      />,
    ),
  );
  return { container, root };
}

function action(container: HTMLElement): HTMLButtonElement {
  const button = container.querySelector('button');
  if (button === null) throw new Error('handoff action was not rendered');
  return button;
}

async function settle(): Promise<void> {
  await act(async () => Promise.resolve());
}

async function unmount(root: Root): Promise<void> {
  await act(async () => root.unmount());
}

function deferred<T>(): {
  readonly promise: Promise<T>;
  readonly resolve: (value: T) => void;
} {
  let resolvePromise: ((value: T) => void) | undefined;
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve;
  });
  return {
    promise,
    resolve: (value) => {
      if (resolvePromise === undefined) throw new Error('deferred promise was not initialised');
      resolvePromise(value);
    },
  };
}
