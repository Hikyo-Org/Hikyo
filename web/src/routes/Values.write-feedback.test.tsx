// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { deferred, revealWindow } from '../testkit/ceremony.ts';
import { settle, typeInto } from '../testkit/renderForm.tsx';
import { Values } from './Values.tsx';

const mocks = vi.hoisted(() => ({
  canReveal: { value: true },
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
    useCopyValues: () => ({ mutateAsync: vi.fn() }),
    useEnvironments: () => ({ data: { items: [] } }),
    useRevealAll: () => ({ mutateAsync: vi.fn() }),
    useRevealOne: () => ({ mutateAsync: vi.fn() }),
    useRevealWindow: () => ({
      data: { ...revealWindow(true), can_reveal: mocks.canReveal.value },
    }),
    useSetValue,
    useValues: () => ({
      data: {
        items: [
          {
            key_id: 'key-a',
            name: 'KEY_A',
            classification: 'secret',
            set: true,
          },
        ],
      },
      isError: false,
    }),
  };
});

vi.mock('../api/transport.tsx', () => ({
  useTransport: () => ({ client: undefined }),
}));

function App() {
  const navigate = useNavigate();
  return (
    <>
      <button
        type="button"
        onClick={() => navigate('/orgs/org-a/projects/project-a/environments/env-b/values')}
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

beforeEach(() => {
  mocks.canReveal.value = true;
  mocks.setValue.mockReset();
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

  it('shows a refusal, clears the submitted plaintext, and retries only with fresh input', async () => {
    mocks.canReveal.value = false;
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
    expect(button(container, 'KEY_A').disabled).toBe(true);

    await act(async () => rejectWrite(new ApiError(403, 'forbidden')));
    await settle();

    const retained = container.querySelector<HTMLInputElement>('#edit-key-a');
    expect(retained?.value).toBe('');
    expect(container.textContent).toContain('You are not permitted to stage this value.');
    expect(mocks.setValue).toHaveBeenCalledOnce();
    if (retained === null) throw new Error('value editor is missing after refusal');
    mocks.setValue.mockResolvedValueOnce({ id: 'pending-retry' });
    typeInto(retained, 'freshly entered value');
    await act(async () => button(container, 'Save draft').click());
    await settle();
    expect(mocks.setValue).toHaveBeenCalledTimes(2);
    expect(mocks.setValue).toHaveBeenLastCalledWith({ key: 'KEY_A', value: 'freshly entered value' });
    await act(async () => root.unmount());
  });

  it('leaves an empty submission unchanged without issuing a write', async () => {
    const { container, root } = await renderValues();

    await act(async () => button(container, 'KEY_A').click());
    await act(async () => button(container, 'Save draft').click());
    await settle();

    expect(mocks.setValue).not.toHaveBeenCalled();
    expect(container.querySelector('#edit-key-a')).not.toBeNull();
    await act(async () => root.unmount());
  });

  it('does not report a write that settles after navigation', async () => {
    const pending = deferred<{ id: string }>();
    mocks.setValue.mockImplementationOnce(() => pending.promise);
    const { container, root } = await renderValues();

    await act(async () => button(container, 'KEY_A').click());
    const input = container.querySelector<HTMLInputElement>('#edit-key-a');
    if (input === null) throw new Error('value editor is missing');
    typeInto(input, 'old-environment draft');
    await act(async () => button(container, 'Save draft').click());
    await act(async () => button(container, 'Navigate').click());
    await act(async () => pending.resolve({ id: 'pending-a' }));
    await settle();

    expect(container.textContent).not.toContain('KEY_A staged.');
    expect(container.querySelector('#edit-key-a')).toBeNull();
    await act(async () => root.unmount());
  });
});
