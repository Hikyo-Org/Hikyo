import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { reauthPasskeyFinishOp, reauthPasskeyStartOp } from '@hikyo/operations';

import { ContinueInBrowserError, dismissContinueInBrowser } from '../app/shell.ts';
import { settleSessionEpoch } from './sessionEpoch.ts';
import { ceremonyRefusalText, runAdapterPasskeyCeremony, runAppReauth, runPasskeyCeremony } from './values.ts';

const request = vi.hoisted(() => vi.fn());
vi.mock('./client.ts', async (importActual) => ({
  ...(await importActual<typeof import('./client.ts')>()),
  parsed: request,
}));
const shell = vi.hoisted(() => ({ reauth: vi.fn() }));
vi.mock('../app/shell.ts', async (importActual) => ({
  ...(await importActual<typeof import('../app/shell.ts')>()),
  shellReauth: shell.reauth,
}));

function mode(protocol: string) {
  vi.stubGlobal('location', { protocol, origin: `${protocol}//x` });
}

beforeEach(() => {
  request.mockReset();
  shell.reauth.mockReset().mockResolvedValue(undefined);
  settleSessionEpoch();
});
afterEach(() => { vi.unstubAllGlobals(); dismissContinueInBrowser(); settleSessionEpoch(); });

describe('in the desktop app', () => {
  beforeEach(() => mode('wails:'));

  it.each([
    ['reveal', 'reveal', 'value.reveal'],
    ['copy', 'copy', 'value.copy-source'],
    ['publish', 'publish', 'value.copy-destination'],
    ['approve', 'approve', 'approval.vote'],
    ['reject', 'reject', 'approval.vote'],
    ['bypass', 'bypass', 'approval.bypass'],
  ] as const)('runs %s as a browser hop and never touches the passkey routes', async (decision, purpose, operation) => {
    await runPasskeyCeremony({ operation: decision, environmentId: 'env', keyIds: ['k1', 'k2'] });
    expect(shell.reauth).toHaveBeenCalledWith({ purpose, operation, environment_ids: ['env'], key_ids: ['k1', 'k2'] });
    expect(request).not.toHaveBeenCalled();
  });

  it.each(['mint', 'access'] as const)('offers Continue in browser for %s instead of calling the shell', async (decision) => {
    const error = await runPasskeyCeremony({ operation: decision, environmentId: 'env', keyIds: [] }).catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(ContinueInBrowserError);
    expect(shell.reauth).not.toHaveBeenCalled();
    expect(request).not.toHaveBeenCalled();
    expect(ceremonyRefusalText(error)).toContain('needs your browser');
  });

  it.each(['reveal', 'approve'] as const)('offers Continue in browser for %s with no keys, not a schema error', async (decision) => {
    const error = await runPasskeyCeremony({ operation: decision, environmentId: 'env', keyIds: [] }).catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(ContinueInBrowserError);
    expect(shell.reauth).not.toHaveBeenCalled();
    expect(ceremonyRefusalText(error)).toContain('Continue there');
  });

  it('serves the identity-provider button through the same hop', async () => {
    await runAppReauth({ operation: 'reveal', environmentId: 'env', keyIds: ['k'] });
    expect(shell.reauth).toHaveBeenCalledTimes(1);
  });

  it('sends the whole adapter environment set in one hop', async () => {
    await runAdapterPasskeyCeremony({ operation: 'adapter.sync', environmentId: 'a', environmentIds: ['a', 'b', 'c'] });
    expect(shell.reauth).toHaveBeenCalledOnce();
    expect(shell.reauth).toHaveBeenCalledWith({ purpose: 'adapter', operation: 'adapter.sync', environment_ids: ['a', 'b', 'c'] });
    expect(request).not.toHaveBeenCalled();
  });
});

describe('in a browser', () => {
  beforeEach(() => mode('https:'));

  it('never calls the shell and starts the passkey ceremony as before', async () => {
    request.mockRejectedValue(new Error('stop after start'));
    await expect(runPasskeyCeremony({ operation: 'reveal', environmentId: 'env', keyIds: ['k'] })).rejects.toThrow('stop after start');
    expect(request).toHaveBeenCalledWith(reauthPasskeyStartOp, { body: { operation: 'reveal', environment_id: 'env', key_ids: ['k'] } });
    expect(shell.reauth).not.toHaveBeenCalled();
    expect(reauthPasskeyFinishOp).toBeDefined();
  });

  it('runs the adapter ceremony per environment as before', async () => {
    request.mockRejectedValue(new Error('stop after start'));
    await expect(runAdapterPasskeyCeremony({ operation: 'adapter.sync', environmentId: 'a', environmentIds: ['a', 'b'] })).rejects.toThrow();
    expect(request).toHaveBeenCalledWith(reauthPasskeyStartOp, expect.objectContaining({ body: expect.objectContaining({ environment_id: 'a', environment_ids: ['a', 'b'] }) }));
    expect(shell.reauth).not.toHaveBeenCalled();
  });
});
