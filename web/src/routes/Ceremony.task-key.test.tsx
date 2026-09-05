// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ceremonyRequest, deferred } from '../testkit/ceremony.ts';
import { settle } from '../testkit/renderForm.tsx';
import { Ceremony } from './Ceremony.tsx';
import { useCeremonyTask } from './useCeremonyTask.ts';

const mocks = vi.hoisted(() => ({
  runPasskeyCeremony: vi.fn(),
  runOIDCCeremony: vi.fn(),
  refreshSession: vi.fn(),
  identity: null as null | {
    session: { assurance: { method: string; provider?: string } };
  },
  providerAvailable: false,
  provider: { kind: 'oidc', slug: 'strict', display_name: 'Corporate IdP' },
  methods: { isError: false, refetch: vi.fn() },
}));

vi.mock('../api/values.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/values.ts')>();
  return {
    ...actual,
    runPasskeyCeremony: mocks.runPasskeyCeremony,
    runOIDCCeremony: mocks.runOIDCCeremony,
  };
});

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: mocks.identity, refreshSession: mocks.refreshSession }),
}));

vi.mock('../api/account.ts', () => ({
  useSessionOIDCProvider: () => (mocks.providerAvailable ? mocks.provider : null),
  useAuthMethods: () => mocks.methods,
}));

vi.mock('../api/transport.tsx', () => ({
  useWorkspaceContext: () => null,
}));

type Controller = ReturnType<typeof useCeremonyTask>;
let latestController: Controller | undefined;

function Harness() {
  const ceremony = useCeremonyTask(['values']);
  latestController = ceremony;
  return ceremony.request === null ? null : (
    <Ceremony
      key={ceremony.requestKey}
      request={ceremony.request}
      onAuthorised={ceremony.onAuthorised}
      onCancel={ceremony.onCancel}
    />
  );
}

function controller(): Controller {
  if (latestController === undefined) throw new Error('ceremony controller is not mounted');
  return latestController;
}

function button(container: HTMLElement, name: string): HTMLButtonElement {
  const match = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === name,
  );
  if (match === undefined) throw new Error(`button ${name} is missing`);
  return match;
}

async function stage(name: string, committed: string[]): Promise<void> {
  const active = controller();
  await act(async () => {
    // Deliberately identical: a double-click/newer retry of the same protected
    // operation must still get a fresh modal executor.
    const task = active.begin(['same-operation']);
    active.stage(task, ceremonyRequest(name), () => {
      committed.push(name);
      active.finish(task);
    });
  });
}

beforeEach(() => {
  latestController = undefined;
  mocks.runPasskeyCeremony.mockReset();
  mocks.runOIDCCeremony.mockReset();
  mocks.refreshSession.mockReset();
  mocks.methods.refetch.mockReset();
  mocks.identity = null;
  mocks.providerAvailable = false;
  mocks.methods.isError = false;
});

describe('Ceremony task identity', () => {
  it('remounts executor state and ignores completion retained by the obsolete modal', async () => {
    const obsoleteAttempt = deferred<void>();
    mocks.runPasskeyCeremony
      .mockImplementationOnce(() => obsoleteAttempt.promise)
      .mockResolvedValueOnce(undefined);
    const committed: string[] = [];
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () => root.render(<Harness />));

    await stage('first', committed);
    await act(async () => button(container, 'Use a passkey').click());
    expect(container.textContent).toContain('Waiting for your passkey…');

    await stage('second', committed);
    expect(container.textContent).toContain('reveal · second');
    expect(container.textContent).toContain('Use a passkey');
    expect(container.textContent).not.toContain('Waiting for your passkey…');

    await act(async () => obsoleteAttempt.resolve(undefined));
    await settle();
    expect(container.textContent).toContain('reveal · second');
    expect(committed).toEqual([]);

    await act(async () => button(container, 'Use a passkey').click());
    await settle();
    expect(committed).toEqual(['second']);
    await act(async () => root.unmount());
  });

  it('offers the current OIDC provider only when a reusable window is allowed', async () => {
    mocks.identity = { session: { assurance: { method: 'oidc:strict', provider: 'strict' } } };
    mocks.providerAvailable = true;
    mocks.runOIDCCeremony.mockResolvedValue(undefined);
    mocks.refreshSession.mockResolvedValue(undefined);
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () =>
      root.render(
        <Ceremony
          request={{
            ...ceremonyRequest('production'),
            window: {
              protected: false,
              effective_window_seconds: 300,
              live: false,
              single_decision: false,
              can_reveal: false,
              totp_offered: true,
            },
          }}
          onAuthorised={vi.fn()}
          onCancel={vi.fn()}
        />,
      ),
    );

    await act(async () => button(container, 'Re-authenticate with Corporate IdP').click());
    await settle();
    expect(mocks.runOIDCCeremony).toHaveBeenCalledWith('strict', 'production');
    expect(mocks.refreshSession).toHaveBeenCalledOnce();
    await act(async () => root.unmount());
  });

  it('keeps a removed OIDC provider passkey-only', async () => {
    mocks.identity = { session: { assurance: { method: 'oidc:removed', provider: 'removed' } } };
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () =>
      root.render(
        <Ceremony
          request={{
            ...ceremonyRequest('production'),
            window: {
              protected: false,
              effective_window_seconds: 300,
              live: false,
              single_decision: false,
              can_reveal: false,
              totp_offered: true,
            },
          }}
          onAuthorised={vi.fn()}
          onCancel={vi.fn()}
        />,
      ),
    );

    expect(container.textContent).not.toContain('Re-authenticate with');
    expect(container.textContent).toContain('Use a passkey');
    await act(async () => root.unmount());
  });

  it('shows and retries provider discovery failure during a reusable-window ceremony', async () => {
    mocks.identity = { session: { assurance: { method: 'oidc:strict', provider: 'strict' } } };
    mocks.methods.isError = true;
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () =>
      root.render(
        <Ceremony
          request={{
            ...ceremonyRequest('production'),
            window: {
              protected: false,
              effective_window_seconds: 300,
              live: false,
              single_decision: false,
              can_reveal: false,
              totp_offered: true,
            },
          }}
          onAuthorised={vi.fn()}
          onCancel={vi.fn()}
        />,
      ),
    );

    expect(container.textContent).toContain('Identity provider options could not be loaded.');
    await act(async () => button(container, 'Retry identity providers').click());
    expect(mocks.methods.refetch).toHaveBeenCalledOnce();
    await act(async () => root.unmount());
  });

  it('keeps a zero-window OIDC session passkey-only and explains why', async () => {
    mocks.identity = { session: { assurance: { method: 'oidc:strict', provider: 'strict' } } };
    mocks.providerAvailable = true;
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () =>
      root.render(
        <Ceremony
          request={ceremonyRequest('production')}
          onAuthorised={vi.fn()}
          onCancel={vi.fn()}
        />,
      ),
    );

    expect(container.textContent).not.toContain('Re-authenticate with Corporate IdP');
    expect(container.textContent).toContain(
      'Your identity provider cannot satisfy a per-disclosure gate; use a passkey.',
    );
    await act(async () => root.unmount());
  });
});

describe('Ceremony copy and factor state (a11y audit)', () => {
  const slidingWindow = {
    protected: false,
    effective_window_seconds: 300,
    live: false,
    single_decision: false,
    can_reveal: false,
    totp_offered: true,
  };

  async function mount(request: Parameters<typeof Ceremony>[0]['request']) {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () =>
      root.render(<Ceremony request={request} onAuthorised={vi.fn()} onCancel={vi.fn()} />),
    );
    return { container, unmount: () => act(async () => root.unmount()) };
  }

  it('names the act in the lede, marks secret keys, and explains the sliding window', async () => {
    const view = await mount({
      ...ceremonyRequest('production'),
      purpose: 'publish',
      keys: [
        { id: 'k1', name: 'API_KEY', classification: 'secret' },
        { id: 'k2', name: 'LOG_LEVEL', classification: 'config' },
      ],
      window: slidingWindow,
    });
    expect(view.container.querySelector('.ceremony__lede strong')?.textContent).toBe(
      'publish into a protected environment',
    );
    const items = [...view.container.querySelectorAll('.ceremony__keys li')];
    expect(items[0]?.querySelector('.visually-hidden')?.textContent).toBe('secret ');
    expect(items[1]?.querySelector('.visually-hidden')).toBeNull();
    expect(view.container.textContent).toContain('Success opens a sliding reveal window.');
    await view.unmount();
  });

  it('keeps disclosure wording for a reveal and no window sentence for a single decision', async () => {
    const view = await mount(ceremonyRequest('production'));
    expect(view.container.querySelector('.ceremony__lede strong')?.textContent).toBe('disclosure');
    expect(view.container.textContent).not.toContain('Success opens a sliding reveal window.');
    await view.unmount();
  });

  it('relabels only the pending factor and puts Cancel last', async () => {
    mocks.identity = { session: { assurance: { method: 'oidc:strict', provider: 'strict' } } };
    mocks.providerAvailable = true;
    const passkey = deferred<void>();
    mocks.runPasskeyCeremony.mockReturnValue(passkey.promise);
    const view = await mount({ ...ceremonyRequest('production'), window: slidingWindow });
    const actions = [...view.container.querySelectorAll('.ceremony__actions button')];
    expect(actions.at(-1)?.textContent).toBe('Cancel');

    await act(async () => button(view.container, 'Use a passkey').click());
    const labels = [...view.container.querySelectorAll<HTMLButtonElement>('.ceremony__actions button')].map(
      (node) => [node.textContent, node.disabled],
    );
    expect(labels).toEqual([
      ['Waiting for your passkey…', true],
      ['Re-authenticate with Corporate IdP', true],
      ['Cancel', true],
    ]);
    await act(async () => passkey.resolve());
    await view.unmount();
  });
});
