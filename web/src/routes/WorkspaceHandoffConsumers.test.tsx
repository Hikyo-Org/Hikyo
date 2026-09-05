// @vitest-environment happy-dom
import { act, createRef, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type {
  PreparedWorkspace,
  StepUpParams,
  WorkspaceBearer,
} from '../api/workspace.ts';
import { WorkspaceStepUp } from './Ceremony.tsx';
import { Reconnect } from './WorkspaceScope.tsx';

const workspace = vi.hoisted(() => ({
  workspaceBearer: vi.fn<(origin: string) => WorkspaceBearer | undefined>(),
  prepareWorkspace:
    vi.fn<(origin: string, stepUp?: StepUpParams) => Promise<PreparedWorkspace>>(),
  openPrepared: vi.fn<(prepared: PreparedWorkspace) => Promise<void>>(),
}));

vi.mock('../api/workspace.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/workspace.ts')>();
  return {
    ...actual,
    workspaceBearer: workspace.workspaceBearer,
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
const bearer: WorkspaceBearer = {
  origin,
  value: 'bearer-1',
  session: 'session-1',
  idleExpiresAt: '2026-08-24T15:00:00Z',
  absoluteExpiresAt: '2026-08-24T16:00:00Z',
};

afterEach(() => {
  workspace.workspaceBearer.mockReset();
  workspace.prepareWorkspace.mockReset();
  workspace.openPrepared.mockReset();
  document.body.replaceChildren();
});

describe('workspace handoff consumers', () => {
  it('gives Ceremony a reachable retry after preparation fails', async () => {
    const retry = deferred<PreparedWorkspace>();
    workspace.workspaceBearer.mockReturnValue(bearer);
    workspace.prepareWorkspace
      .mockRejectedValueOnce(new Error('offline'))
      .mockReturnValueOnce(retry.promise);

    const mounted = await render(
      <WorkspaceStepUp
        origin={origin}
        operation="reveal"
        environmentId="environment-1"
        keyIds={['key-1']}
        firstRef={createRef<HTMLButtonElement>()}
        onAuthorised={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    await settle();

    expect(mounted.container.querySelector('[role="alert"]')?.textContent).toContain(
      'The remote could not be reached to authorise this disclosure.',
    );
    expect(buttonNamed(mounted.container, 'Try again')).toMatchObject({ disabled: false });
    expect(mounted.container.textContent).not.toContain('Contacting…');

    act(() => buttonNamed(mounted.container, 'Try again').click());
    expect(buttonNamed(mounted.container, 'Contacting…')).toMatchObject({ disabled: true });
    await act(async () => retry.resolve(prepared));
    expect(buttonNamed(mounted.container, `Continue to ${origin} to authorise`)).toMatchObject({
      disabled: false,
    });
    await unmount(mounted.root);
  });

  it('keeps WorkspaceScope disabled while sign-in is pending without preparing twice', async () => {
    const authorisation = deferred<void>();
    workspace.prepareWorkspace.mockResolvedValue(prepared);
    workspace.openPrepared.mockReturnValue(authorisation.promise);

    const mounted = await render(<Reconnect origin={origin} name="remote" />);
    await settle();
    expect(mounted.container.querySelector('h1')?.textContent).toBe(`Session expired on ${origin}`);
    const proceed = buttonNamed(mounted.container, 'Reconnect');

    act(() => proceed.click());
    const waiting = buttonNamed(mounted.container, 'Waiting for sign-in…');
    expect(waiting).toMatchObject({ disabled: true });
    waiting.click();
    expect(workspace.prepareWorkspace).toHaveBeenCalledOnce();
    expect(workspace.openPrepared).toHaveBeenCalledOnce();

    await act(async () => authorisation.resolve(undefined));
    await unmount(mounted.root);
  });
});

async function render(element: ReactNode) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => root.render(element));
  return { container, root };
}

function buttonNamed(container: HTMLElement, name: string): HTMLButtonElement {
  const button = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === name,
  );
  if (button === undefined) throw new Error(`button not found: ${name}`);
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
