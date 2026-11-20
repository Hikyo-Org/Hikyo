// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  assertSessionEpoch, blockSessionEpoch, captureSessionEpoch, installSessionFence, reconcileAfterShellHop,
  SessionChangedError, settleSessionEpoch,
} from './sessionEpoch.ts';
import { shellReauth, type ShellReauthRequest } from '../app/shell.ts';

// In the app there is no companion cookie, so the fence proves the owner with
// whoami after a shell hop instead (reconcileAfterShellHop).

let uninstall: () => void;
const identityCheck = vi.fn<(expected?: unknown) => Promise<void>>();

beforeEach(() => {
  vi.stubGlobal('location', { protocol: 'wails:', origin: 'wails://x' });
  identityCheck.mockReset();
  uninstall = installSessionFence(() => undefined, () => undefined, identityCheck);
  settleSessionEpoch();
});
afterEach(() => {
  uninstall();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('reconcileAfterShellHop', () => {
  it('runs the identity check and keeps the epoch when the owner is unchanged', async () => {
    identityCheck.mockImplementation(async () => { settleSessionEpoch(); });
    const captured = captureSessionEpoch();
    await reconcileAfterShellHop(captured);
    expect(identityCheck).toHaveBeenCalledOnce();
    expect(() => assertSessionEpoch(captured)).not.toThrow();
  });

  it('fails closed with no identity check installed', async () => {
    uninstall();
    const captured = captureSessionEpoch();
    await expect(reconcileAfterShellHop(captured)).rejects.toBeInstanceOf(SessionChangedError);
    uninstall = installSessionFence(() => undefined, () => undefined, identityCheck);
  });

  it('fails closed when another check settled a different owner during the hop', async () => {
    identityCheck.mockImplementation(async () => { settleSessionEpoch(); });
    const captured = captureSessionEpoch();
    blockSessionEpoch();
    settleSessionEpoch();
    await expect(reconcileAfterShellHop(captured)).rejects.toBeInstanceOf(SessionChangedError);
    expect(identityCheck).not.toHaveBeenCalled();
  });

  it('fails closed when the owner changed', async () => {
    identityCheck.mockImplementation(async () => { blockSessionEpoch(); settleSessionEpoch(); });
    const captured = captureSessionEpoch();
    await expect(reconcileAfterShellHop(captured)).rejects.toBeInstanceOf(SessionChangedError);
    expect(() => assertSessionEpoch(captured)).toThrow(SessionChangedError);
  });

  it('fails closed when the identity check itself fails', async () => {
    identityCheck.mockImplementation(async () => { blockSessionEpoch(); throw new Error('whoami down'); });
    await expect(reconcileAfterShellHop(captureSessionEpoch())).rejects.toThrow('whoami down');
    expect(() => captureSessionEpoch()).toThrow(SessionChangedError);
  });
});

describe('shellReauth', () => {
  const request: ShellReauthRequest = { purpose: 'adapter', operation: 'adapter.sync', environment_ids: ['e'] };

  it('proves the owner after the hop, and never after a refused one', async () => {
    identityCheck.mockImplementation(async () => { settleSessionEpoch(); });
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetcher);
    await shellReauth(request);
    expect(identityCheck).toHaveBeenCalledOnce();
    fetcher.mockResolvedValue(new Response(JSON.stringify({ error: 'cancelled', message: 'x' }), { status: 409 }));
    await expect(shellReauth(request)).rejects.toMatchObject({ code: 'cancelled' });
    expect(identityCheck).toHaveBeenCalledOnce();
  });

  it('rejects the hop when another check settled a different owner while it waited', async () => {
    identityCheck.mockImplementation(async () => { settleSessionEpoch(); });
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockImplementation(async () => {
      blockSessionEpoch();
      settleSessionEpoch();
      return new Response(null, { status: 204 });
    }));
    await expect(shellReauth(request)).rejects.toBeInstanceOf(SessionChangedError);
  });

  it('rejects the hop when the session changed behind it', async () => {
    identityCheck.mockImplementation(async () => { blockSessionEpoch(); settleSessionEpoch(); });
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 204 })));
    const captured = captureSessionEpoch();
    await expect(shellReauth(request)).rejects.toBeInstanceOf(SessionChangedError);
    expect(() => assertSessionEpoch(captured)).toThrow(SessionChangedError);
  });
});
