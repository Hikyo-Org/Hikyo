// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { EstablishCredential } from './EstablishCredential.tsx';

afterEach(() => {
  vi.unstubAllGlobals();
});

const PASSWORD = 'a first password long enough';

function mount(status: number) {
  const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) =>
    Promise.resolve(new Response(null, { status })),
  );
  vi.stubGlobal('fetch', fetchMock);
  return {
    fetchMock,
    rendered: renderForm(
      <MemoryRouter>
        <EstablishCredential />
      </MemoryRouter>,
    ),
  };
}

function field(root: ParentNode, name: string): HTMLInputElement {
  const control = root.querySelector(`input[name="${name}"]`);
  if (!(control instanceof HTMLInputElement)) {
    throw new Error(`${name} input is missing`);
  }
  return control;
}

async function fill(root: ParentNode, authority: string, password: string, repeat: string) {
  typeInto(field(root, 'authority'), authority);
  typeInto(field(root, 'password'), password);
  typeInto(field(root, 'repeat'), repeat);
  const form = root.querySelector('form');
  if (!(form instanceof HTMLFormElement)) throw new Error('the establish form is missing');
  await act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  await settle();
}

describe('EstablishCredential', () => {
  it('refuses mismatched passwords locally, before any request', async () => {
    const { fetchMock, rendered } = mount(204);
    const { container, unmount } = await rendered;
    await fill(container, 'hik_cea_authority_value_1234', PASSWORD, `${PASSWORD} but different`);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('differ');
    expect(fetchMock).not.toHaveBeenCalled();
    await unmount();
  });

  it('refuses a short password locally, before any request', async () => {
    const { fetchMock, rendered } = mount(204);
    const { container, unmount } = await rendered;
    await fill(container, 'hik_cea_authority_value_1234', 'short', 'short');
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('at least 12');
    expect(fetchMock).not.toHaveBeenCalled();
    await unmount();
  });

  it('establishes the credential and offers sign-in, with the authority cleared', async () => {
    const { fetchMock, rendered } = mount(204);
    const { container, unmount } = await rendered;
    await fill(container, ' hik_cea_authority_value_1234 ', PASSWORD, PASSWORD);
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    const request = input instanceof Request ? input : new Request(String(input), init);
    expect(new URL(request.url, 'http://localhost').pathname).toBe('/api/v1/auth/credential/establish');
    expect(await request.json()).toEqual({ authority: 'hik_cea_authority_value_1234', password: PASSWORD });
    expect(container.querySelector('h1')?.textContent).toBe('Credential established');
    expect(container.querySelector('a[href="/login"]')?.textContent).toBe('Sign in');
    expect(container.textContent).not.toContain('hik_cea_authority_value_1234');
    await unmount();
  });

  it('voices a refused authority uniformly', async () => {
    const { rendered } = mount(401);
    const { container, unmount } = await rendered;
    await fill(container, 'hik_cea_spent_authority_value', PASSWORD, PASSWORD);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      'The authority was not accepted. It may have expired or already been used.',
    );
    expect(container.querySelector('form')).not.toBeNull();
    await unmount();
  });
});
