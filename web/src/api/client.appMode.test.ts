// @vitest-environment happy-dom
import { regenerateRecoveryCodesOp } from '@hikyo/operations';
import { client } from '@hikyo/runtime';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { authenticatedIdentity } from '../testkit/identity.ts';
import { installSessionFence, settleSessionEpoch } from './sessionEpoch.ts';
import { isOwnArtifact, parsed } from './client.ts';

// The recovery-codes regeneration reissues the acting session. The SPA verifies
// the reissue with whoami before showing the codes, and it only does that for an
// artifact it owns: `browser` in a browser, `cli` in the desktop app (ADR D3).

function regenerated(artifact: 'browser' | 'cli') {
  const { session, principal } = authenticatedIdentity;
  return { recovery_codes: ['aaaa-bbbb'], login: { session: { ...session, id: 'ses_123e4567-e89b-12d3-a456-426614174099', artifact }, principal } };
}

function mode(protocol: string) {
  vi.stubGlobal('location', { protocol, origin: `${protocol}//x` });
}

let uninstall: () => void;
const rotated = vi.fn<(expected?: { session: string; principal: string }) => Promise<void>>();

beforeEach(() => {
  // The same-origin singleton's empty baseUrl makes Node's Request reject the relative URL.
  client.setConfig({ baseUrl: 'http://hikyo.test' });
  rotated.mockReset().mockImplementation(async () => { settleSessionEpoch(); });
  uninstall = installSessionFence(() => undefined, () => undefined, rotated);
  settleSessionEpoch();
});
afterEach(() => {
  client.setConfig({ baseUrl: '' });
  uninstall();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function respondWith(artifact: 'browser' | 'cli') {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(regenerated(artifact)), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  }));
}

describe('isOwnArtifact', () => {
  it('is browser in a browser and browser or cli in the app', () => {
    mode('https:');
    expect(isOwnArtifact('browser')).toBe(true);
    expect(isOwnArtifact('cli')).toBe(false);
    expect(isOwnArtifact('workspace')).toBe(false);
    mode('wails:');
    expect(isOwnArtifact('browser')).toBe(true);
    expect(isOwnArtifact('cli')).toBe(true);
    expect(isOwnArtifact('workspace')).toBe(false);
  });
});

describe('recovery-code regeneration', () => {
  it('in the app, verifies the reissued cli session before the codes are released', async () => {
    mode('wails:');
    respondWith('cli');
    const result = await parsed(regenerateRecoveryCodesOp, { body: { proof: 'p' } });
    expect(result.recovery_codes).toEqual(['aaaa-bbbb']);
    expect(rotated).toHaveBeenCalledWith({ session: 'ses_123e4567-e89b-12d3-a456-426614174099', principal: authenticatedIdentity.principal.id });
  });

  it('holds the codes until the reconciliation settles, and withholds them if it fails', async () => {
    mode('wails:');
    respondWith('cli');
    let finish: () => void = () => undefined;
    rotated.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = () => { settleSessionEpoch(); resolve(); }; }));
    let settled = false;
    const pending = parsed(regenerateRecoveryCodesOp, { body: { proof: 'p' } }).finally(() => { settled = true; });
    await vi.waitFor(() => expect(rotated).toHaveBeenCalledOnce());
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(settled).toBe(false);
    finish();
    await expect(pending).resolves.toMatchObject({ recovery_codes: ['aaaa-bbbb'] });

    respondWith('cli');
    rotated.mockRejectedValueOnce(new Error('whoami failed'));
    await expect(parsed(regenerateRecoveryCodesOp, { body: { proof: 'p' } })).rejects.toThrow();
  });

  it('in a browser, still verifies a browser reissue and ignores a cli one, as before', async () => {
    mode('https:');
    respondWith('browser');
    await parsed(regenerateRecoveryCodesOp, { body: { proof: 'p' } });
    expect(rotated).toHaveBeenCalledTimes(1);
    rotated.mockClear();
    respondWith('cli');
    await parsed(regenerateRecoveryCodesOp, { body: { proof: 'p' } });
    expect(rotated).not.toHaveBeenCalled();
  });
});
