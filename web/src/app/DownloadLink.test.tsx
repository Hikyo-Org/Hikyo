// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle } from '../testkit/renderForm.tsx';
import { clearNotification, ToastViewport } from './notifications.tsx';
import { DownloadLink } from './DownloadLink.tsx';

// A plain function rather than a spy for the failing case: vitest 5 reports a spy's own rejected result as unhandled.
const shell = vi.hoisted(() => {
  const calls: unknown[][] = [];
  const state: { fail: boolean } = { fail: false };
  return {
    calls, state,
    download: (...args: unknown[]) => { calls.push(args); return state.fail ? Promise.reject(new Error('no')) : Promise.resolve(true); },
  };
});
vi.mock('./shell.ts', async (importActual) => ({
  ...(await importActual<typeof import('./shell.ts')>()),
  shellDownloadPath: shell.download,
}));

function mode(protocol: string) {
  vi.stubGlobal('location', { protocol, origin: `${protocol}//x` });
}
beforeEach(() => { shell.calls.length = 0; shell.state.fail = false; });
afterEach(() => { vi.unstubAllGlobals(); clearNotification(); });

describe('DownloadLink', () => {
  it('is a plain download anchor in a browser', async () => {
    mode('https:');
    const view = await renderForm(<DownloadLink href="/api/v1/d" filename="d.json">Get</DownloadLink>);
    const anchor = view.container.querySelector('a');
    expect(anchor?.getAttribute('href')).toBe('/api/v1/d');
    expect(anchor?.getAttribute('download')).toBe('d.json');
    expect(view.container.querySelector('button')).toBeNull();
    await view.unmount();
  });

  it('is a button that asks the shell to save the path in the app', async () => {
    mode('wails:');
    const view = await renderForm(<DownloadLink href="/api/v1/d?portable=true" filename="d.json">Get</DownloadLink>);
    expect(view.container.querySelector('a')).toBeNull();
    await act(async () => view.container.querySelector('button')?.click());
    expect(shell.calls).toEqual([['/api/v1/d?portable=true', 'd.json']]);
    await view.unmount();
  });

  it('tells the person when the shell cannot save', async () => {
    mode('wails:');
    shell.state.fail = true;
    const view = await renderForm(<><ToastViewport /><DownloadLink href="/api/v1/d" filename="d.json">Get</DownloadLink></>);
    await act(async () => view.container.querySelector('button')?.click());
    await settle();
    expect(view.container.textContent).toContain('The file could not be saved');
    await view.unmount();
  });
});
