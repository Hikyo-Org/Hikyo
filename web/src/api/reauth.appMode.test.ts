// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { dismissContinueInBrowser } from '../app/shell.ts';
import { reauthenticateSelfConfig, type SelfConfigIntent } from './selfConfig.ts';
import { installSessionFence, settleSessionEpoch } from './sessionEpoch.ts';
import { runAdapterPasskeyCeremony } from './values.ts';

// The shell is real here and only `fetch` is stubbed, so these pin the exact
// body that reaches POST /__shell/reauth (docs/spec/desktop-shell-protocol.md).
const fetcher = vi.fn<(url: string, init: RequestInit) => Promise<Response>>();
function sent(): { url: string; body: unknown } {
  const [url, init] = fetcher.mock.calls[0] ?? ['', {}];
  return { url, body: typeof init.body === 'string' ? JSON.parse(init.body) : undefined };
}

let uninstall: () => void;
beforeEach(() => {
  uninstall = installSessionFence(() => undefined, () => undefined, async () => { settleSessionEpoch(); });
  fetcher.mockReset().mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal('fetch', fetcher);
  vi.stubGlobal('location', { protocol: 'wails:', origin: 'wails://x' });
  settleSessionEpoch();
});
afterEach(() => { uninstall(); vi.unstubAllGlobals(); dismissContinueInBrowser(); settleSessionEpoch(); });

describe('adapter reauth in the app', () => {
  it('sends one hop over the whole environment set, with no key_ids', async () => {
    await runAdapterPasskeyCeremony({ operation: 'adapter.sync', environmentId: 'a', environmentIds: ['a', 'b'] });
    expect(fetcher).toHaveBeenCalledOnce();
    expect(sent()).toEqual({ url: '/__shell/reauth', body: { purpose: 'adapter', operation: 'adapter.sync', environment_ids: ['a', 'b'] } });
  });
});

describe('self-config reauth in the app', () => {
  const intent: SelfConfigIntent = {
    action: 'rollout-restore', owner_instance_id: 'inst', revision: 3n, schema_version: 1, expected_generation: 7n,
    preview_token: 'tok', to: '', confirm_restored_credentials: true,
  };

  it('sends the decision with an empty environment set and the matching operation', async () => {
    await reauthenticateSelfConfig(intent, { kind: 'passkey' });
    expect(fetcher).toHaveBeenCalledOnce();
    expect(sent()).toEqual({
      url: '/__shell/reauth',
      body: { purpose: 'self-config', operation: 'self-config.apply', environment_ids: [], self_config: { ...intent, revision: 3, expected_generation: 7 } },
    });
  });

  it.each([['adopt', 'self-config.adopt'], ['apply', 'self-config.apply'], ['mail-test', 'self-config.test']] as const)(
    'maps %s to %s', async (action, operation) => {
      await reauthenticateSelfConfig({ ...intent, action }, { kind: 'passkey' });
      expect(fetcher).toHaveBeenCalledOnce();
      expect(sent().body).toMatchObject({ operation });
    },
  );
});
