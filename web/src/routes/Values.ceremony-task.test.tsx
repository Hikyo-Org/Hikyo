// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import type { RevealWindow } from '../api/values.ts';
import { deferred, revealWindow } from '../testkit/ceremony.ts';
import { settle, typeInto } from '../testkit/renderForm.tsx';
import type { CeremonyRequest } from './Ceremony.tsx';
import { Values } from './Values.tsx';

const mocks = vi.hoisted(() => ({
  copy: vi.fn(),
  fetchRevealWindow: vi.fn(),
  revealAll: vi.fn(),
  revealGuard: { canReveal: true },
  revealOne: vi.fn(),
  setValue: vi.fn(),
}));

vi.mock('../api/values.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/values.ts')>();
  const { useState } = await import('react');

  function useSetValue() {
    const [isPending, setIsPending] = useState(false);
    return {
      isPending,
      mutateAsync: async (input: { key: string; value: string }) => {
        setIsPending(true);
        try {
          return await mocks.setValue(input);
        } finally {
          setIsPending(false);
        }
      },
    };
  }

  return {
    ...actual,
    fetchRevealWindow: mocks.fetchRevealWindow,
    useCopyValues: () => ({ mutateAsync: mocks.copy }),
    useEnvironments: () => ({
      data: {
        items: [
          { id: 'env-a', name: 'Alpha' },
          { id: 'env-b', name: 'Beta' },
          { id: 'env-c', name: 'Gamma' },
        ],
      },
    }),
    useRevealAll: () => ({ mutateAsync: mocks.revealAll }),
    useRevealOne: () => ({ mutateAsync: mocks.revealOne }),
    useRevealWindow: () => ({
      data: { ...revealWindow(true), can_reveal: mocks.revealGuard.canReveal },
    }),
    useSetValue,
    useValues: () => ({
      data: {
        items: [{
          key_id: 'key-a',
          name: 'KEY_A',
          classification: 'secret',
          set: true,
        }],
      },
      isError: false,
    }),
  };
});

vi.mock('../api/transport.tsx', () => ({
  useTransport: () => ({ client: undefined }),
}));

vi.mock('./Ceremony.tsx', async (importActual) => {
  const actual = await importActual<typeof import('./Ceremony.tsx')>();
  return {
    ...actual,
    Ceremony: ({ request }: { request: CeremonyRequest }) => (
      <output data-testid="ceremony">{request.environmentId}</output>
    ),
  };
});

function App() {
  const navigate = useNavigate();
  return (
    <>
      <button
        type="button"
        onClick={() => navigate('/orgs/org-a/projects/project-a/environments/env-c/values')}
      >
        Navigate
      </button>
      <Routes>
        <Route
          path="/orgs/:org/projects/:project/environments/:environment/values"
          element={<Values />}
        />
      </Routes>
    </>
  );
}

async function renderValues(): Promise<{ container: HTMLElement; root: Root }> {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/orgs/org-a/projects/project-a/environments/env-a/values']}>
        <App />
      </MemoryRouter>,
    );
  });
  return { container, root };
}

function button(container: HTMLElement, name: string): HTMLButtonElement {
  const match = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === name || candidate.getAttribute('aria-label') === name,
  );
  if (match === undefined) throw new Error(`button ${name} is missing`);
  return match;
}

async function selectDestination(container: HTMLElement, environmentId: string): Promise<void> {
  const select = container.querySelector<HTMLSelectElement>('#publish-destination');
  if (select === null) throw new Error('publish destination is missing');
  await act(async () => {
    select.value = environmentId;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
}

beforeEach(() => {
  mocks.copy.mockReset();
  mocks.fetchRevealWindow.mockReset();
  mocks.revealAll.mockReset();
  mocks.revealGuard.canReveal = true;
  mocks.revealOne.mockReset();
  mocks.setValue.mockReset();
});

describe('Values ceremony task ownership', () => {
  it('ignores a guard completion from the environment visited before navigation', async () => {
    const pending = deferred<RevealWindow>();
    mocks.fetchRevealWindow.mockImplementationOnce(() => pending.promise);
    const { container, root } = await renderValues();

    await act(async () => button(container, 'Reveal KEY_A').click());
    await act(async () => button(container, 'Navigate').click());
    await act(async () => pending.resolve(revealWindow(false)));
    await settle();

    expect(container.querySelector('[data-testid="ceremony"]')).toBeNull();
    await act(async () => root.unmount());
  });

  it('ignores a guard completion for a publish destination that changed', async () => {
    const pending = deferred<RevealWindow>();
    mocks.fetchRevealWindow.mockImplementationOnce(() => pending.promise);
    const { container, root } = await renderValues();
    await selectDestination(container, 'env-b');

    await act(async () => button(container, 'Publish into environment').click());
    await selectDestination(container, 'env-c');
    await act(async () => pending.resolve(revealWindow(false)));
    await settle();

    expect(container.querySelector('[data-testid="ceremony"]')).toBeNull();
    await act(async () => root.unmount());
  });

  it('does not disclose a value whose request completes after navigation', async () => {
    const disclosure = deferred<{
      key_id: string;
      name: string;
      value: string;
    }>();
    mocks.fetchRevealWindow.mockResolvedValueOnce(revealWindow(true));
    mocks.revealOne.mockImplementationOnce(() => disclosure.promise);
    const { container, root } = await renderValues();

    await act(async () => button(container, 'Reveal KEY_A').click());
    await settle();
    await act(async () => button(container, 'Navigate').click());
    await act(async () => disclosure.resolve({
      key_id: 'key-a',
      name: 'KEY_A',
      value: 'must-not-cross-environments',
    }));
    await settle();

    expect(container.textContent).not.toContain('must-not-cross-environments');
    expect(container.textContent).toContain('No disclosures yet.');
    await act(async () => root.unmount());
  });
});

describe('Values write feedback', () => {
  it('confirms a staged value and closes the editor after the server accepts it', async () => {
    mocks.setValue.mockResolvedValueOnce({ id: 'pending-a' });
    const { container, root } = await renderValues();

    await act(async () => button(container, 'KEY_A').click());
    const input = container.querySelector<HTMLInputElement>('#edit-key-a');
    if (input === null) throw new Error('value editor is missing');
    typeInto(input, 'replacement');
    await act(async () => button(container, 'Save draft').click());
    await settle();

    expect(mocks.setValue).toHaveBeenCalledWith({ key: 'KEY_A', value: 'replacement' });
    expect(container.querySelector('#edit-key-a')).toBeNull();
    expect(container.textContent).toContain('KEY_A staged.');
    await act(async () => root.unmount());
  });

  it('shows saving, then surfaces a refusal and keeps the rejected draft editable', async () => {
    mocks.revealGuard.canReveal = false;
    let rejectWrite: (reason?: unknown) => void = () => undefined;
    mocks.setValue.mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectWrite = reject;
        }),
    );
    const { container, root } = await renderValues();

    await act(async () => button(container, 'KEY_A').click());
    const input = container.querySelector<HTMLInputElement>('#edit-key-a');
    if (input === null) throw new Error('value editor is missing');
    expect(input.dataset['writeOnly']).toBe('true');
    typeInto(input, 'correctable draft');
    await act(async () => button(container, 'Save draft').click());

    expect(button(container, 'Saving…').disabled).toBe(true);

    await act(async () => rejectWrite(new ApiError(403, 'forbidden')));
    await settle();

    const retained = container.querySelector<HTMLInputElement>('#edit-key-a');
    expect(retained?.value).toBe('correctable draft');
    expect(container.textContent).toContain('You are not permitted to stage this value.');
    await act(async () => root.unmount());
  });

  it('treats an empty submission as unchanged without issuing a write', async () => {
    const { container, root } = await renderValues();

    await act(async () => button(container, 'KEY_A').click());
    await act(async () => button(container, 'Save draft').click());
    await settle();

    expect(mocks.setValue).not.toHaveBeenCalled();
    expect(container.querySelector('#edit-key-a')).toBeNull();
    await act(async () => root.unmount());
  });
});
