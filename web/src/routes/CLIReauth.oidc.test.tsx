// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { settleTask } from '../testkit/renderForm.tsx';
import { CLIReauth } from './CLIReauth.tsx';

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  providerAvailable: true,
  provider: { kind: 'oidc', slug: 'strict', display_name: 'Corporate IdP' },
}));

vi.mock('../api/cliReauth.ts', () => ({
  loadCLIReauthTransaction: mocks.load,
  approveCLIReauth: vi.fn(),
  cliReauthCallbackURL: vi.fn(),
}));

vi.mock('../api/account.ts', () => ({
  useTotpStatus: () => ({ isSuccess: false }),
  useSessionOIDCProvider: () => (mocks.providerAvailable ? mocks.provider : null),
}));

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({
    state: { status: 'authenticated' },
    identity: { session: { assurance: { method: 'oidc:strict', provider: 'strict' } } },
    refreshSession: vi.fn(),
  }),
}));

vi.mock('../api/values.ts', () => ({
  runAdapterPasskeyCeremony: vi.fn(),
  runAdapterTOTPCeremony: vi.fn(),
  runPasskeyCeremony: vi.fn(),
  runOIDCCeremony: vi.fn(),
  runTOTPCeremony: vi.fn(),
}));

async function renderTransaction(environments: Array<{
  environment_id: string;
  effective_window_seconds: number;
  requires_webauthn: boolean;
}>): Promise<{ container: HTMLElement; unmount: () => Promise<void> }> {
  mocks.load.mockResolvedValue({
    state: 'txn-195',
    purpose: 'reveal',
    operation: 'value.reveal',
    environments,
    key_ids: ['key-production'],
    redirect_uri: 'http://127.0.0.1:40126/callback',
    expires_at: '2099-08-23T12:00:00Z',
  });
  globalThis.history.replaceState({}, '', '/auth/cli/reauth?transaction=txn-195');
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () =>
    root.render(
      <QueryClientProvider client={queryClient}>
        <CLIReauth />
      </QueryClientProvider>,
    ),
  );
  await settleTask();
  return {
    container,
    unmount: async () => {
      await act(async () => root.unmount());
      queryClient.clear();
    },
  };
}

beforeEach(() => {
  mocks.load.mockReset();
  mocks.providerAvailable = true;
});

describe('CLI OIDC disclosure handoff', () => {
  it('offers the current provider when any environment has a reusable window', async () => {
    const view = await renderTransaction([
      { environment_id: 'production', effective_window_seconds: 300, requires_webauthn: false },
      { environment_id: 'locked', effective_window_seconds: 0, requires_webauthn: true },
    ]);

    expect(view.container.textContent).toContain('Re-authenticate with Corporate IdP');
    expect(view.container.textContent).toContain(
      'Re-authenticate once per sliding-window environment with your identity provider.',
    );
    expect(view.container.textContent).toContain('locked — passkey required');
    await view.unmount();
  });

  it('keeps an all-zero-window handoff passkey-only', async () => {
    const view = await renderTransaction([
      { environment_id: 'locked', effective_window_seconds: 0, requires_webauthn: true },
    ]);

    expect(view.container.textContent).not.toContain('Re-authenticate with Corporate IdP');
    expect(view.container.textContent).toContain('locked — passkey required');
    await view.unmount();
  });

  it('keeps a removed OIDC provider passkey-only', async () => {
    mocks.providerAvailable = false;
    const view = await renderTransaction([
      { environment_id: 'production', effective_window_seconds: 300, requires_webauthn: false },
    ]);

    expect(view.container.textContent).not.toContain('Re-authenticate with');
    expect(view.container.textContent).toContain('Authorize CLI');
    await view.unmount();
  });
});
