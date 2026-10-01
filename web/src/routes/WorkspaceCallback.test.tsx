// @vitest-environment happy-dom
import { renderForm } from '../testkit/renderForm.tsx';
import { act } from 'react';
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
  const { container, unmount } = await renderForm(<WorkspaceCallback />);
  await act(async () => new Promise((resolve) => setTimeout(resolve, 5)));

  expect(globalThis.close).toHaveBeenCalledOnce();
  expect(container.querySelector('[role="status"]')?.textContent).toBe(
    'This window could not close itself. Close it to continue.',
  );
  await unmount();
});
