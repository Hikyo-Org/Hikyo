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

it('ignores the completion of a request whose offer was dismissed, so a newer offer stays', async () => {
  vi.stubGlobal('location', { protocol: 'wails:', pathname: '/x', search: '' });
  let answer: (response: Response) => void = () => undefined;
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { answer = resolve; })));
  const view = await mount();
  await act(async () => { offerContinueInBrowser('first'); });
  await act(async () => button(view.container, 'Continue in browser')?.click());
  await act(async () => button(view.container, 'Not now')?.click());
  await act(async () => { offerContinueInBrowser('second'); });
  await act(async () => answer(new Response(null, { status: 204 })));
  expect(view.container.textContent).toContain('second');
  await view.unmount();
});
