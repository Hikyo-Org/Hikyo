import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { reauthPasskeyFinishOp, reauthPasskeyStartOp } from '@hikyo/operations';

import { blockSessionEpoch, settleSessionEpoch } from './sessionEpoch.ts';
import { runAdapterPasskeyCeremony, runPasskeyCeremony } from './values.ts';

const request = vi.hoisted(() => vi.fn());
vi.mock('./client.ts', async (importActual) => ({
  ...(await importActual<typeof import('./client.ts')>()),
  parsed: request,
}));

class AssertionResponse {
  readonly clientDataJSON = new Uint8Array([1]).buffer;
  readonly authenticatorData = new Uint8Array([2]).buffer;
  readonly signature = new Uint8Array([3]).buffer;
  readonly userHandle = null;
}
class Credential {
  readonly id = 'credential';
  readonly rawId = new Uint8Array([4]).buffer;
  readonly type = 'public-key';
  readonly response = new AssertionResponse();
}

const ceremonies = [
  {
    name: 'disclosure',
    run: () => runPasskeyCeremony({ operation: 'reveal', environmentId: 'env', keyIds: ['key'] }),
    body: { operation: 'reveal', environment_id: 'env', key_ids: ['key'] },
  },
  {
    name: 'adapter',
    run: () => runAdapterPasskeyCeremony({
      operation: 'adapter.sync', environmentId: 'env', environmentIds: ['env', 'other'],
    }),
    body: {
      operation: 'adapter', adapter_operation: 'adapter.sync', environment_id: 'env',
      environment_ids: ['env', 'other'], key_ids: [],
    },
  },
];

beforeEach(() => {
  request.mockReset();
  request.mockResolvedValueOnce({ challenge: 'AQ' }).mockResolvedValue(undefined);
  settleSessionEpoch();
  vi.stubGlobal('PublicKeyCredential', Credential);
  vi.stubGlobal('AuthenticatorAssertionResponse', AssertionResponse);
});
afterEach(() => { vi.unstubAllGlobals(); settleSessionEpoch(); });

for (const ceremony of ceremonies) {
  describe(`${ceremony.name} passkey ceremony`, () => {
    it('keeps its purpose binding and completes with the binary assertion', async () => {
      vi.stubGlobal('navigator', { credentials: { get: vi.fn().mockResolvedValue(new Credential()) } });
      await ceremony.run();
      expect(request).toHaveBeenNthCalledWith(1, reauthPasskeyStartOp, { body: ceremony.body });
      expect(request).toHaveBeenNthCalledWith(2, reauthPasskeyFinishOp, {
        body: {
          id: 'credential', rawId: 'BA', type: 'public-key',
          response: { clientDataJSON: 'AQ', authenticatorData: 'Ag', signature: 'Aw', userHandle: null },
        },
      });
    });

    it('refuses to submit an assertion after the browser session changes', async () => {
      vi.stubGlobal('navigator', { credentials: { get: vi.fn().mockImplementation(() => {
        blockSessionEpoch();
        return Promise.resolve(new Credential());
      }) } });
      await expect(ceremony.run()).rejects.toThrow('The browser session changed.');
      expect(request).toHaveBeenCalledTimes(1);
    });
  });
}
