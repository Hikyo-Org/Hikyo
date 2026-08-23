// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { settleTask } from '../testkit/renderForm.tsx';
import { useSessionOIDCProvider } from './account.ts';

const mocks = vi.hoisted(() => ({
  assurance: { method: 'oidc:strict', provider: 'removed' },
  methods: {
    local_login_enabled: true,
    providers: [{ kind: 'oidc', slug: 'strict', display_name: 'Corporate IdP' }],
  },
}));

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { session: { assurance: mocks.assurance } } }),
}));

vi.mock('./client.ts', async (importActual) => {
  const actual = await importActual<typeof import('./client.ts')>();
  return {
    ...actual,
    parsed: vi.fn(() => Promise.resolve(mocks.methods)),
  };
});

beforeEach(() => {
  mocks.assurance = { method: 'oidc:strict', provider: 'removed' };
});

describe('useSessionOIDCProvider', () => {
  it('returns null when the session provider slug is no longer configured', async () => {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <ProviderName />
        </QueryClientProvider>,
      );
    });
    await settleTask();

    expect(container.textContent).toBe('passkey-only');

    await act(async () => root.unmount());
    client.clear();
    container.remove();
  });
});

function ProviderName() {
  const provider = useSessionOIDCProvider();
  return <span>{provider?.display_name ?? 'passkey-only'}</span>;
}
