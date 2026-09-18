// @vitest-environment happy-dom
import { QueryClient } from '@tanstack/react-query';
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { forgetWorkspace, rememberWorkspace, type WorkspaceBearer } from '../api/workspace.ts';
import { WorkspaceScope } from './WorkspaceScope.tsx';

const origin = 'https://remote.example';

const remotes = vi.hoisted(() => ({
  useRemotes: vi.fn(() => ({
    isPending: false,
    data: { items: [{ name: 'remote', url: 'https://remote.example' }] },
  })),
}));
const workspace = vi.hoisted(() => ({
  assertCompatible:
    vi.fn<(origin: string, request: { signal: AbortSignal }) => Promise<void>>(),
}));

vi.mock('../api/remotes.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/remotes.ts')>();
  return { ...actual, useRemotes: remotes.useRemotes };
});
vi.mock('../api/workspace.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/workspace.ts')>();
  return { ...actual, assertCompatible: workspace.assertCompatible };
});

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const bearer: WorkspaceBearer = {
  origin,
  value: 'bearer-1',
  session: 'session-1',
  idleExpiresAt: '2026-08-24T15:00:00Z',
  absoluteExpiresAt: '2026-08-24T16:00:00Z',
};

afterEach(() => {
  vi.restoreAllMocks();
  workspace.assertCompatible.mockReset();
  forgetWorkspace(origin);
  document.body.replaceChildren();
});

describe('ConnectedWorkspace disposal', () => {
  it('cancels and clears its QueryClient when the connected workspace unmounts', async () => {
    const cancel = vi.spyOn(QueryClient.prototype, 'cancelQueries');
    const clear = vi.spyOn(QueryClient.prototype, 'clear');
    workspace.assertCompatible.mockResolvedValue(undefined);
    rememberWorkspace(bearer);

    const mounted = await render(<p>product</p>);
    await settle();
    expect(mounted.container.textContent).toContain('product');
    expect(cancel).not.toHaveBeenCalled();
    expect(clear).not.toHaveBeenCalled();

    await unmount(mounted.root);

    expect(cancel).toHaveBeenCalledOnce();
    expect(clear).toHaveBeenCalledOnce();
    expect(cancel.mock.invocationCallOrder[0]).toBeLessThan(
      clear.mock.invocationCallOrder[0] ?? Number.NaN,
    );
  });

  it('aborts a compatibility check still in flight when it unmounts', async () => {
    workspace.assertCompatible.mockReturnValue(new Promise<void>(() => {}));
    rememberWorkspace(bearer);

    const mounted = await render(<p>product</p>);
    expect(mounted.container.textContent).toContain(`Checking ${origin}`);
    const request = workspace.assertCompatible.mock.calls[0]?.[1];
    expect(request?.signal.aborted).toBe(false);

    await unmount(mounted.root);

    expect(request?.signal.aborted).toBe(true);
  });
});

async function render(children: ReactNode) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <MemoryRouter>
        <WorkspaceScope remote="remote">{children}</WorkspaceScope>
      </MemoryRouter>,
    ),
  );
  return { container, root };
}

async function settle(): Promise<void> {
  await act(async () => Promise.resolve());
}

async function unmount(root: Root): Promise<void> {
  await act(async () => root.unmount());
}
