// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';

import { ContinueInBrowser } from './ContinueInBrowser.tsx';
import { dismissContinueInBrowser, offerContinueInBrowser } from './shell.ts';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) { this.setAttribute('open', ''); };

afterEach(() => { dismissContinueInBrowser(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

async function mount() {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => root.render(<ContinueInBrowser />));
  return { container, unmount: async () => { await act(async () => root.unmount()); container.remove(); } };
}
const button = (container: HTMLElement, name: string) => [...container.querySelectorAll('button')].find((node) => node.textContent === name);

it('renders nothing until something raises the offer', async () => {
  const view = await mount();
  expect(view.container.querySelector('dialog')).toBeNull();
  await view.unmount();
});

it('names the reason and opens the current page in the browser through the shell', async () => {
  vi.stubGlobal('location', { protocol: 'wails:', pathname: '/orgs/o/members', search: '?x=1' });
  const fetcher = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal('fetch', fetcher);
  const view = await mount();
  await act(async () => { offerContinueInBrowser('Adding a passkey needs your browser.'); });
  expect(view.container.textContent).toContain('Adding a passkey needs your browser.');
  await act(async () => button(view.container, 'Continue in browser')?.click());
  expect(fetcher).toHaveBeenCalledWith('/__shell/open-in-browser', expect.objectContaining({ body: JSON.stringify({ path: '/orgs/o/members?x=1' }) }));
  expect(view.container.querySelector('dialog')).toBeNull();
  await view.unmount();
});

it('closes on Not now without calling the shell', async () => {
  const fetcher = vi.fn();
  vi.stubGlobal('fetch', fetcher);
  const view = await mount();
  await act(async () => { offerContinueInBrowser('x'); });
  await act(async () => button(view.container, 'Not now')?.click());
  expect(fetcher).not.toHaveBeenCalled();
  expect(view.container.querySelector('dialog')).toBeNull();
  await view.unmount();
});
