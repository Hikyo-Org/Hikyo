// @vitest-environment happy-dom
import { renderForm } from '../testkit/renderForm.tsx';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AUTHORITY_REFUSAL } from '../api/session.ts';
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
  vi.spyOn(globalThis.location, 'replace').mockImplementation(() => undefined);
  vi.stubGlobal('BroadcastChannel', TestBroadcastChannel);
  vi.stubGlobal('close', vi.fn());
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('OIDC done page', () => {
  it('broadcasts a completed reauthentication on the transaction channel', async () => {
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=oidc-state&purpose=reauth');
    const { unmount } = await renderForm(<OIDCDone />);

    expect(channels).toEqual([
      {
        name: 'hikyo-oidc:oidc-state',
        message: { state: 'oidc-state', ok: true },
        closed: true,
      },
    ]);
    expect(globalThis.close).toHaveBeenCalledOnce();
    await unmount();
  });

  it('does not broadcast when the callback has no state', async () => {
    globalThis.history.replaceState({}, '', '/auth/oidc/done?purpose=reauth');
    const { container, unmount } = await renderForm(<OIDCDone />);

    expect(channels).toEqual([]);
    expect(container.textContent).toContain('without an OIDC transaction');
    await unmount();
  });

  it('shows an actionable sign-in refusal instead of silently navigating home', async () => {
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=login-state&purpose=login&error=unauthenticated');
    const { container, unmount } = await renderForm(<OIDCDone />);

    expect(channels).toEqual([]);
    expect(container.textContent).toContain('identity provider refused this sign-in');
    expect(container.textContent).toContain('Return to sign in');
    await unmount();
  });

  it('keeps a refused reauthentication on screen with a way back, and never closes the window', async () => {
    globalThis.sessionStorage.setItem('hikyo-oidc-return:reauth-state', '/settings#account-security');
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=reauth-state&purpose=reauth&error=access_denied');
    const { container, unmount } = await renderForm(<OIDCDone />);

    expect(channels[0]?.message).toEqual({ state: 'reauth-state', ok: false, error: 'access_denied' });
    expect(globalThis.close).not.toHaveBeenCalled();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('refused this reauthentication');
    const back = container.querySelector('a.btn');
    expect(back?.textContent).toBe('Back');
    expect(back?.getAttribute('href')).toBe('/settings#account-security');
    await unmount();
  });

  it('keeps a refused identity link visible before returning to account security', async () => {
    globalThis.sessionStorage.setItem('hikyo-oidc-return:link-state', '/settings#account-security');
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=link-state&purpose=link&error=unauthenticated');
    const { container, unmount } = await renderForm(<OIDCDone />);

    expect(channels[0]?.message).toEqual({ state: 'link-state', ok: false, error: 'unauthenticated' });
    expect(container.textContent).toContain('identity provider refused this link');
    expect(container.textContent).toContain('Return to account security');
    await unmount();
  });
});


it('announces a successful OIDC login to other tabs before returning home', async () => {
  globalThis.history.replaceState({}, '', '/auth/oidc/done?state=login-state&purpose=login');
  const { container, unmount } = await renderForm(<OIDCDone />);
  expect(container.querySelector('[role="status"]')?.textContent).toBe('Signed in.');
  expect(channels).toContainEqual({
    name: 'hikyo-root-auth',
    message: { type: 'session-changed', sender: expect.any(String) },
    closed: true,
  });
  expect(globalThis.location.replace).toHaveBeenCalledWith('/');
  await unmount();
});

it('restores the transaction-bound workspace approval after OIDC login', async () => {
  globalThis.sessionStorage.setItem(
    'hikyo-oidc-return:workspace-login',
    '/workspace/approve?state=workspace-state',
  );
  globalThis.history.replaceState(
    {},
    '',
    '/auth/oidc/done?state=workspace-login&purpose=login',
  );
  const { unmount } = await renderForm(<OIDCDone />);
  expect(globalThis.location.replace).toHaveBeenCalledWith(
    '/workspace/approve?state=workspace-state',
  );
  await unmount();
});

// An invitation claim (#610) returns here like a login: no broadcast, a
// successful claim lands where the establish page asked, a refusal is the
// establish page's one sentence with the way back to it.
describe('OIDC done page: claim', () => {
  it('lands a successful claim signed in where the establish page asked', async () => {
    globalThis.sessionStorage.setItem('hikyo-oidc-return:claim-state', '/settings');
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=claim-state&purpose=claim');
    const { container, unmount } = await renderForm(<OIDCDone />);

    // Like a login: the new session is announced, the transaction channel
    // (an opener's link/reauth listener) hears nothing.
    expect(channels.map((channel) => channel.name)).toEqual(['hikyo-root-auth']);
    expect(container.textContent).toContain('Invitation claimed');
    expect(globalThis.location.replace).toHaveBeenCalledWith('/settings');
    await unmount();
  });

  it('voices a refused claim in the establish page sentence, with the way back to it', async () => {
    globalThis.sessionStorage.setItem('hikyo-oidc-return:claim-refused', '/settings');
    globalThis.history.replaceState({}, '', '/auth/oidc/done?state=claim-refused&purpose=claim&error=unauthenticated');
    const { container, unmount } = await renderForm(<OIDCDone />);

    expect(channels).toEqual([]);
    expect(globalThis.location.replace).not.toHaveBeenCalled();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(AUTHORITY_REFUSAL);
    const back = container.querySelector('a.btn');
    expect(back?.getAttribute('href')).toBe('/establish');
    expect(back?.textContent).toBe('Back to your setup authority');
    await unmount();
  });
});
