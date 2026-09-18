// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { authenticatedIdentity } from '../testkit/identity.ts';
import { AuthProvider } from '../app/AuthProvider.tsx';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { InstanceAdmin } from './InstanceAdmin.tsx';

afterEach(() => {
  vi.unstubAllGlobals();
});

const POLICY = { max_finite_lifetime_seconds: 3600, allow_indefinite: false, max_live_credentials: 5 };

function mountInstanceAdmin(): ReturnType<typeof renderForm> {
  const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
    const input = args[0];
    const request = input instanceof Request ? input : new Request(input, args[1]);
    const path = new URL(request.url, 'http://localhost').pathname;
    if (path === '/api/v1/instance/credential-policy') {
      return Promise.resolve(new Response(JSON.stringify(POLICY), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      }));
    }
    if (path === '/api/v1/auth/whoami') {
      return Promise.resolve(new Response(JSON.stringify(authenticatedIdentity), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      }));
    }
    // Every other page GET lands honestly in a "not disclosed" panel.
    return Promise.resolve(new Response(null, { status: 404 }));
  });
  vi.stubGlobal('fetch', fetchMock);
  return renderForm(
    <AuthProvider>
      <MemoryRouter initialEntries={['/instance']}>
        <Routes>
          <Route path="/instance" element={<InstanceAdmin />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>,
  );
}

function labelledInput(root: ParentNode, text: string): HTMLInputElement {
  const label = [...root.querySelectorAll('label')].find((l) => l.textContent?.trim() === text);
  const id = label?.getAttribute('for');
  const input = id === null || id === undefined ? null : root.querySelector(`#${id}`);
  if (!(input instanceof HTMLInputElement)) {
    throw new Error(`no input for label ${text}`);
  }
  return input;
}

describe('CredentialPolicyPanel', () => {
  it('loads the edit form fields from the fetched policy', async () => {
    const { container, unmount } = await mountInstanceAdmin();
    await settleTask();

    const edit = [...container.querySelectorAll('button')].find(
      (b) => b.textContent?.trim() === 'edit',
    );
    if (!(edit instanceof HTMLButtonElement)) {
      throw new Error('the policy edit button is missing (the success row did not render)');
    }
    await act(async () => edit.click());

    expect(labelledInput(container, 'Maximum finite lifetime (seconds)').value).toBe('3600');
    expect(labelledInput(container, 'Maximum live credentials per service account').value).toBe('5');
    const allowIndefinite = labelledInput(container, 'Allow credentials with no expiry');
    expect(allowIndefinite.checked).toBe(false);

    await unmount();
  });
});
