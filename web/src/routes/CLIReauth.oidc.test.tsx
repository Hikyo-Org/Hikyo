// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { settleTask } from '../testkit/renderForm.tsx';
import { CLIReauth } from './CLIReauth.tsx';

const mocks = vi.hoisted(() => ({
  authStatus: 'authenticated',
  load: vi.fn(),
  providerAvailable: true,
  provider: { kind: 'oidc', slug: 'strict', display_name: 'Corporate IdP' },
  methods: { isError: false, refetch: vi.fn() },
  selfConfigPlan: '',
}));

vi.mock('../api/cliReauth.ts', () => ({
  loadCLIReauthTransaction: mocks.load,
  approveCLIReauth: vi.fn(),
  cliReauthCallbackURL: vi.fn(),
}));

vi.mock('../api/account.ts', () => ({
  useTotpStatus: () => ({ isSuccess: false }),
  useSessionOIDCProvider: () => (mocks.providerAvailable ? mocks.provider : null),
  useAuthMethods: () => mocks.methods,
}));

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({
    state: { status: mocks.authStatus },
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
    purpose: mocks.selfConfigPlan === '' ? 'reveal' : 'self-config',
    self_config: mocks.selfConfigPlan === '' ? undefined : { action: 'apply', owner_instance_id: 'instance_local', revision: 3, expected_generation: 7, schema_version: 1, to: '', preview_token: '', confirm_restored_credentials: false, plan_digest: mocks.selfConfigPlan },
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
  mocks.methods.refetch.mockReset();
  mocks.providerAvailable = true;
  mocks.methods.isError = false;
  mocks.selfConfigPlan = '';
});

describe('CLI OIDC disclosure handoff', () => {
  it('displays the exact controlled rollout digest in the CLI authorization handoff', async () => {
    mocks.selfConfigPlan = 'b'.repeat(64);
    const view = await renderTransaction([]);
    try {
      expect(view.container.textContent).toContain('Controlled rollout');
      expect(view.container.textContent).toContain(mocks.selfConfigPlan);
      expect(view.container.textContent).toContain('generation 7');
      expect(view.container.textContent).toContain('revision r3');
    } finally { await view.unmount(); }
  });
  it('renders the loading state inside the same card shell as the loaded page', async () => {
    mocks.authStatus = 'checking';
    try {
      const { container, unmount } = await renderTransaction([]);
      const status = container.querySelector('.login__card [role="status"]');
      expect(status?.textContent).toBe('Loading…');
      expect(container.querySelector('.login__card h1')?.textContent).toBe('Authorize CLI');
      await unmount();
    } finally {
      mocks.authStatus = 'authenticated';
    }
  });

  it('offers the current provider when any environment has a reusable window', async () => {
    const view = await renderTransaction([
      { environment_id: 'production', effective_window_seconds: 300, requires_webauthn: false },
      { environment_id: 'locked', effective_window_seconds: 0, requires_webauthn: true },
    ]);

    expect(view.container.textContent).toContain('Re-authenticate with Corporate IdP');
    expect(view.container.textContent).toContain(
      'Re-authenticate once per sliding-window environment with your identity provider.',
    );
    expect(view.container.textContent).toContain('locked (passkey required)');
    await view.unmount();
  });

  it('keeps an all-zero-window handoff passkey-only', async () => {
    const view = await renderTransaction([
      { environment_id: 'locked', effective_window_seconds: 0, requires_webauthn: true },
    ]);

    expect(view.container.textContent).not.toContain('Re-authenticate with Corporate IdP');
    expect(view.container.textContent).toContain('locked (passkey required)');
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

  it('shows and retries provider discovery failure for a sliding-window handoff', async () => {
    mocks.providerAvailable = false;
    mocks.methods.isError = true;
    const view = await renderTransaction([
      { environment_id: 'production', effective_window_seconds: 300, requires_webauthn: false },
    ]);

    expect(view.container.textContent).toContain('Identity provider options could not be loaded.');
    const retry = [...view.container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Retry identity providers',
    );
    expect(retry).toBeDefined();
    await act(async () => retry?.click());
    expect(mocks.methods.refetch).toHaveBeenCalledOnce();
    await view.unmount();
  });
});
