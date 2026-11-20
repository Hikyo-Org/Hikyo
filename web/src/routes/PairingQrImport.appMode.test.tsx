// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { continueInBrowserReason, dismissContinueInBrowser } from '../app/shell.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { PairingQrImport } from './PairingQrImport.tsx';

function mode(protocol: string) {
  vi.stubGlobal('location', { protocol, origin: `${protocol}//x` });
}
async function startCamera() {
  const view = await renderForm(<PairingQrImport onDecoded={() => undefined} revision="" disabled={false} />);
  const button = [...view.container.querySelectorAll('button')].find((node) => node.textContent === 'Scan QR with camera');
  await act(async () => button?.click());
  await settleTask();
  await view.unmount();
}
function cameraRefused() {
  vi.stubGlobal('isSecureContext', true);
  Object.defineProperty(navigator, 'mediaDevices', {
    value: { getUserMedia: vi.fn().mockRejectedValue(new DOMException('Denied', 'NotAllowedError')) }, configurable: true,
  });
}

beforeEach(() => dismissContinueInBrowser());
afterEach(() => { vi.unstubAllGlobals(); dismissContinueInBrowser(); });

describe('camera scanning in the app', () => {
  it('offers Continue in browser when the window has no camera support', async () => {
    mode('wails:');
    vi.stubGlobal('isSecureContext', false);
    await startCamera();
    expect(continueInBrowserReason()).toContain('cannot scan with a camera');
  });

  it('offers Continue in browser when the shell refuses the camera', async () => {
    mode('wails:');
    cameraRefused();
    await startCamera();
    expect(continueInBrowserReason()).toContain('refused the camera');
  });

  it('offers nothing in a browser', async () => {
    mode('https:');
    vi.stubGlobal('isSecureContext', false);
    await startCamera();
    cameraRefused();
    await startCamera();
    expect(continueInBrowserReason()).toBeNull();
  });
});
