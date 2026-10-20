// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { retireSensitiveOperations } from '../api/sensitiveMutation.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { PairRemoteForm } from './RemotePairing.tsx';

const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) await cleanup();
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});
async function click(container: HTMLElement, label: string) {
  const button = [...container.querySelectorAll('button')].find((node) => node.textContent === label);
  if (button === undefined) throw new Error(`Missing button ${label}`);
  await act(async () => button.click()); await settleTask();
}
async function prepare() {
  vi.stubGlobal('fetch', vi.fn(async () => Response.json({ phrase: 'public sample phrase', requester_state: 'local-state', expires_at: new Date(Date.now() + 60_000).toISOString() }, { status: 201 })));
  const view = await renderForm(<PairRemoteForm />); cleanups.push(view.unmount);
  await click(view.container, 'Generate pairing phrase'); return view;
}
function mockCamera() {
  vi.stubGlobal('isSecureContext', true);
  let deliver: ((stream: MediaStream) => void) | undefined;
  const getUserMedia = vi.fn(() => new Promise<MediaStream>((resolve) => { deliver = resolve; }));
  Object.defineProperty(navigator, 'mediaDevices', { value: { getUserMedia }, configurable: true });
  const stop = vi.fn();
  const stream = new MediaStream();
  Object.defineProperty(stream, 'getTracks', { value: () => [{ stop }] });
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue();
  return { getUserMedia, stop, deliver: () => deliver?.(stream) };
}

it('offers paste and image fallback when camera access is unavailable or refused', async () => {
  const view = await prepare();
  vi.stubGlobal('isSecureContext', false);
  await click(view.container, 'Scan QR with camera');
  expect(view.container.textContent).toContain('needs HTTPS');
  const camera = mockCamera(); camera.getUserMedia.mockRejectedValue(new DOMException('Denied', 'NotAllowedError'));
  await click(view.container, 'Scan QR with camera');
  expect(view.container.textContent).toContain('Camera permission was refused');
  expect(view.container.querySelector('#pairing-qr-image')).not.toBeNull();
  expect(view.container.querySelector('#pair-remote-invitation')).not.toBeNull();
});

it.each(['stop', 'edit', 'discard', 'retire', 'unmount', 'hidden'])('stops a late camera permission result after %s', async (action) => {
  const view = await prepare(); const camera = mockCamera();
  await click(view.container, 'Scan QR with camera');
  if (action === 'stop') await click(view.container, 'Stop camera');
  if (action === 'discard') await click(view.container, 'Discard pairing');
  if (action === 'edit') {
    const input = view.container.querySelector('#pair-remote-invitation');
    if (!(input instanceof HTMLInputElement)) throw new Error('Missing invitation');
    await act(async () => typeInto(input, 'manual input'));
  }
  if (action === 'retire') await act(async () => retireSensitiveOperations(view.client));
  if (action === 'unmount') await view.unmount();
  if (action === 'hidden') {
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
  }
  await act(async () => camera.deliver()); await settleTask();
  expect(camera.stop).toHaveBeenCalledOnce();
  if (action === 'edit') expect(view.container.querySelector('input#pair-remote-invitation')).toHaveProperty('value', 'manual input');
});

it('releases an active camera when the panel closes', async () => {
  const view = await prepare(); const camera = mockCamera();
  await click(view.container, 'Scan QR with camera');
  await act(async () => camera.deliver()); await settleTask();
  expect(camera.stop).not.toHaveBeenCalled();
  await view.unmount();
  expect(camera.stop).toHaveBeenCalledOnce();
});
