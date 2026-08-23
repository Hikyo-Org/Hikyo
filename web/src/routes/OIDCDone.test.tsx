// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { OIDCDone } from './OIDCDone.tsx';

const channels: Array<{ name: string; message?: unknown; closed: boolean }> = [];

class TestBroadcastChannel {
  private readonly record: (typeof channels)[number];

  constructor(name: string) {
    this.record = { name, closed: false };
    channels.push(this.record);
  }

  postMessage(message: unknown) {
    this.record.message = message;
  }

  close() {
    this.record.closed = true;
  }
}

beforeEach(() => {
  channels.length = 0;
  vi.stubGlobal('BroadcastChannel', TestBroadcastChannel);
  vi.stubGlobal('close', vi.fn());
});

describe('OIDC done page', () => {
  it('broadcasts a completed reauthentication on the transaction channel', async () => {
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=oidc-state&purpose=reauth');
    const container = document.createElement('div');
    const root = createRoot(container);

    await act(async () => root.render(<OIDCDone />));

    expect(channels).toEqual([
      {
        name: 'hikyo-oidc:oidc-state',
        message: { state: 'oidc-state', ok: true },
        closed: true,
      },
    ]);
    expect(globalThis.close).toHaveBeenCalledOnce();
    await act(async () => root.unmount());
  });

  it('does not broadcast when the callback has no state', async () => {
    globalThis.history.replaceState({}, '', '/auth/oidc/done?purpose=reauth');
    const container = document.createElement('div');
    const root = createRoot(container);

    await act(async () => root.render(<OIDCDone />));

    expect(channels).toEqual([]);
    expect(container.textContent).toContain('without an OIDC transaction');
    await act(async () => root.unmount());
  });

  it('shows an actionable sign-in refusal instead of silently navigating home', async () => {
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=login-state&purpose=login&error=unauthenticated');
    const container = document.createElement('div');
    const root = createRoot(container);

    await act(async () => root.render(<OIDCDone />));

    expect(channels).toEqual([]);
    expect(container.textContent).toContain('identity provider refused this sign-in');
    expect(container.textContent).toContain('Return to sign in');
    await act(async () => root.unmount());
  });

  it('keeps a refused identity link visible before returning to account security', async () => {
    globalThis.sessionStorage.setItem('hikyo-oidc-return:link-state', '/settings#account-security');
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=link-state&purpose=link&error=unauthenticated');
    const container = document.createElement('div');
    const root = createRoot(container);

    await act(async () => root.render(<OIDCDone />));

    expect(channels[0]?.message).toEqual({ state: 'link-state', ok: false, error: 'unauthenticated' });
    expect(container.textContent).toContain('identity provider refused this link');
    expect(container.textContent).toContain('Return to account security');
    await act(async () => root.unmount());
  });
});
