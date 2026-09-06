// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { WorkspaceCallback } from './WorkspaceCallback.tsx';

class TestBroadcastChannel {
  postMessage() {}
  close() {}
}

beforeEach(() => {
  vi.stubGlobal('BroadcastChannel', TestBroadcastChannel);
  vi.stubGlobal('close', vi.fn());
  vi.stubGlobal('closed', false);
});

afterEach(() => vi.unstubAllGlobals());

it('says so when the browser refused to close the window', async () => {
  globalThis.history.replaceState({}, '', '/workspace/callback?code=c&state=s');
  const container = document.createElement('div');
  const root = createRoot(container);
  await act(async () => root.render(<WorkspaceCallback />));
  await act(async () => new Promise((resolve) => setTimeout(resolve, 5)));

  expect(globalThis.close).toHaveBeenCalledOnce();
  expect(container.querySelector('[role="status"]')?.textContent).toBe(
    'This window could not close itself. Close it to continue.',
  );
  await act(async () => root.unmount());
});
