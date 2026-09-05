// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { runOIDCCeremony } from './values.ts';

class TestBroadcastChannel {
  static latest: TestBroadcastChannel | undefined;
  readonly name: string;
  onmessage: ((event: MessageEvent<unknown>) => void) | null = null;
  closed = false;

  constructor(name: string) {
    this.name = name;
    TestBroadcastChannel.latest = this;
  }

  close() {
    this.closed = true;
  }

  emit(data: unknown) {
    this.onmessage?.({ data } as MessageEvent<unknown>);
  }
}

class TestPopup {
  opener: unknown = {};
  readonly close = vi.fn();
  readonly location = { replace: vi.fn() };
}

let popup = new TestPopup();

function oidcStartResponse(): Response {
  return new Response(
    JSON.stringify({ authorization_url: 'https://idp.example/authorize?state=state-195' }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

async function startedCeremony(): Promise<{ pending: Promise<void> }> {
  const pending = runOIDCCeremony('strict', 'env-production');
  await vi.waitFor(() => {
    expect(TestBroadcastChannel.latest?.name).toBe('hikyo-oidc:state-195');
  });
  return { pending };
}

beforeEach(() => {
  TestBroadcastChannel.latest = undefined;
  popup = new TestPopup();
  vi.stubGlobal('BroadcastChannel', TestBroadcastChannel);
  vi.stubGlobal('open', vi.fn(() => popup));
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(oidcStartResponse());
  globalThis.history.replaceState({}, '', '/values');
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('runOIDCCeremony', () => {
  it('resolves only for the matching successful callback message', async () => {
    const { pending } = await startedCeremony();
    TestBroadcastChannel.latest?.emit({ state: 'some-other-state', ok: true });
    expect(TestBroadcastChannel.latest?.closed).toBe(false);
    TestBroadcastChannel.latest?.emit({ state: 'state-195', ok: true });

    await expect(pending).resolves.toBeUndefined();
    expect(TestBroadcastChannel.latest?.closed).toBe(true);
    expect(popup.opener).toBeNull();
    expect(popup.location.replace).toHaveBeenCalledWith(
      'https://idp.example/authorize?state=state-195',
    );
  });

  it('surfaces a callback refusal', async () => {
    const { pending } = await startedCeremony();
    TestBroadcastChannel.latest?.emit({ state: 'state-195', ok: false, error: 'unauthenticated' });

    await expect(pending).rejects.toThrow('unauthenticated');
    expect(TestBroadcastChannel.latest?.closed).toBe(true);
  });

  it('times out and closes the channel after the OIDC transaction lifetime', async () => {
    vi.useFakeTimers();
    const { pending } = await startedCeremony();
    const refusal = expect(pending).rejects.toThrow(
      'identity provider reauthentication timed out',
    );
    await vi.advanceTimersByTimeAsync(10 * 60 * 1_000);

    await refusal;
    expect(TestBroadcastChannel.latest?.closed).toBe(true);
  });

  it('describes a start refusal as an identity-provider policy refusal', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ code: 'unauthenticated', message: 'unauthenticated' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(runOIDCCeremony('strict', 'env-production')).rejects.toThrow(
      'identity provider refused this reauthentication',
    );
    expect(popup.close).toHaveBeenCalledOnce();
  });
});
