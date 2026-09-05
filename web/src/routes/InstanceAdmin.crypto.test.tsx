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

/**
 * The instance-scoped cryptographic maintenance surface (#503). Every GET the
 * page fires that we do not care about returns 404 so its panel lands in an
 * honest "not disclosed" state; we drive only the rotation and re-encryption
 * POSTs.
 */
function mountInstanceAdmin(
  handler: (request: Request, path: string) => Response | null,
): ReturnType<typeof renderForm> {
  const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
    const input = args[0];
    const request = input instanceof Request ? input : new Request(input, args[1]);
    const path = new URL(request.url, 'http://localhost').pathname;
    const specific = handler(request, path);
    if (specific !== null) return Promise.resolve(specific);
    if (path === '/api/v1/auth/whoami') {
      return Promise.resolve(new Response(JSON.stringify(authenticatedIdentity), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      }));
    }
    // Directory GETs may be refused independently of the authenticated owner.
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

function requests(mock: ReturnType<typeof vi.fn>): Request[] {
  return mock.mock.calls.map(([input, init]) =>
    input instanceof Request ? input : new Request(input as string, init as RequestInit),
  );
}

function button(root: ParentNode, text: string): HTMLButtonElement {
  const match = [...root.querySelectorAll('button')].find(
    (candidate) => candidate.textContent?.trim() === text,
  );
  if (!(match instanceof HTMLButtonElement)) {
    throw new Error(`${text} button is missing`);
  }
  return match;
}

function dialog(container: HTMLElement): HTMLElement {
  const found = container.ownerDocument.querySelector('dialog.ceremony');
  if (!(found instanceof HTMLElement)) {
    throw new Error('the ceremony dialog is missing');
  }
  return found;
}

describe('instance crypto maintenance', () => {
  it('is titled Instance settings and carries no grants panel or prototype fiction', async () => {
    const view = await mountInstanceAdmin(() => null);
    await settleTask();
    try {
      expect(view.container.querySelector('h1')?.textContent).toBe('Instance settings');
      const headings = [...view.container.querySelectorAll('h2')].map((h) => h.textContent);
      expect(headings).not.toContain('Instance grants');
      expect(headings.some((h) => h?.startsWith('Connected instances'))).toBe(false);
      expect(view.container.textContent).not.toContain('decided round 3');
      expect(view.container.textContent).not.toContain('rotated 2026-06-20');
      const members = [...view.container.querySelectorAll('#instance-members a')];
      expect(members.map((a) => a.getAttribute('href'))).toEqual(['/instance/members']);
    } finally {
      await view.unmount();
    }
  });

  it('rotates the instance DEK only after the consequences dialog is confirmed', async () => {
    const fetchMock = vi.fn();
    const view = mountInstanceAdmin((request, path) => {
      fetchMock(request, path);
      if (request.method === 'POST' && path === '/api/v1/instance/rotate-dek') {
        return new Response(JSON.stringify({ scope: 'instance', key_version: 4 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return null;
    });
    const { container, unmount } = await view;
    try {
      await settleTask();

      // Nothing is posted just by rendering the panel.
      expect(fetchMock.mock.calls.some(([, p]) => p === '/api/v1/instance/rotate-dek')).toBe(false);

      await act(async () => button(container, 'Rotate the instance DEK').click());
      expect(dialog(container).textContent).toContain('incomplete until');

      await act(async () => button(container, 'Rotate the DEK').click());
      await settleTask();

      const rotate = requests(fetchMock).find(
        (r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/instance/rotate-dek',
      );
      if (rotate === undefined) throw new Error('the rotate-dek request is missing');
      expect(await rotate.json()).toEqual({ scope: 'instance' });
      expect(container.textContent).toContain('version 4');
      expect(container.textContent).toContain('run the instance re-encryption');
    } finally {
      await unmount();
    }
  });

  it('drains instance re-encryption across runs until a run moves no rows', async () => {
    let calls = 0;
    const fetchMock = vi.fn();
    const view = mountInstanceAdmin((request, path) => {
      fetchMock(request, path);
      if (request.method === 'POST' && path === '/api/v1/instance/reencrypt') {
        calls += 1;
        const rowsMoved = calls === 1 ? 5 : 0;
        return new Response(JSON.stringify({ scope: 'instance', rows_moved: rowsMoved }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return null;
    });
    const { container, unmount } = await view;
    try {
      await settleTask();
      await act(async () => button(container, 'Re-encrypt the instance').click());
      await settleTask();

      const reencrypts = requests(fetchMock).filter(
        (r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/instance/reencrypt',
      );
      // First run moves rows, so the loop runs again; the second moves none and stops.
      expect(reencrypts).toHaveLength(2);
      expect(container.textContent).toContain('moved 5 ciphertext rows');
      expect(container.textContent).toContain('2 runs');
    } finally {
      await unmount();
    }
  });

  it('keeps the committed row count when a later re-encryption run fails', async () => {
    let calls = 0;
    const view = mountInstanceAdmin((request, path) => {
      if (request.method === 'POST' && path === '/api/v1/instance/reencrypt') {
        calls += 1;
        if (calls === 1) {
          return new Response(JSON.stringify({ scope: 'instance', rows_moved: 5 }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          });
        }
        return new Response(null, { status: 500 });
      }
      return null;
    });
    const { container, unmount } = await view;
    try {
      await settleTask();
      await act(async () => button(container, 'Re-encrypt the instance').click());
      await settleTask();

      // The committed 5 rows are not lost when the second run fails; the failure
      // text names them and says the idempotent walk resumes on re-run.
      expect(container.textContent).toContain('5 ciphertext rows moved across 1 run before this failure');
      expect(container.textContent).toContain('re-running resumes');
    } finally {
      await unmount();
    }
  });

  it('reads a 403 on a crypto job as a session step-up requirement, not a permanent refusal', async () => {
    const view = mountInstanceAdmin((request, path) => {
      if (request.method === 'POST' && path === '/api/v1/instance/rotate-dek') {
        return new Response(null, { status: 403 });
      }
      return null;
    });
    const { container, unmount } = await view;
    try {
      await settleTask();
      await act(async () => button(container, 'Rotate the instance DEK').click());
      await act(async () => button(container, 'Rotate the DEK').click());
      await settleTask();

      const text = dialog(container).textContent ?? '';
      expect(text).toContain('needs a second factor');
      expect(text).toContain('present your authenticator code or passkey in the banner above');
      expect(text).not.toContain('You are not permitted');
    } finally {
      await unmount();
    }
  });

  it('maps a master-key 409 to the finalize-root-rotation-first refusal', async () => {
    const view = mountInstanceAdmin((request, path) => {
      if (request.method === 'POST' && path === '/api/v1/instance/rotate-master-key') {
        return new Response(JSON.stringify({}), {
          status: 409,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return null;
    });
    const { container, unmount } = await view;
    try {
      await settleTask();
      await act(async () => button(container, 'Rotate the master key').click());
      await act(async () => button(container, 'Rotate the key').click());
      await settleTask();

      expect(dialog(container).textContent).toContain('Finalize the root-key rotation before rotating the master key.');
    } finally {
      await unmount();
    }
  });

  it('sends the prepare phase for the root-key rotation with its crash-safety copy', async () => {
    const fetchMock = vi.fn();
    const view = mountInstanceAdmin((request, path) => {
      fetchMock(request, path);
      if (request.method === 'POST' && path === '/api/v1/instance/rotate-root-key') {
        return new Response(JSON.stringify({ phase: 'prepare', root_key_epoch: 2 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return null;
    });
    const { container, unmount } = await view;
    try {
      await settleTask();
      await act(async () => button(container, 'Prepare').click());
      const ceremony = dialog(container);
      expect(ceremony.textContent).toContain('No key material crosses the wire');
      expect(ceremony.textContent).toContain('install the new root at the primary source');

      await act(async () => button(ceremony, 'Prepare').click());
      await settleTask();

      const prepare = requests(fetchMock).find(
        (r) =>
          r.method === 'POST' && new URL(r.url).pathname === '/api/v1/instance/rotate-root-key',
      );
      if (prepare === undefined) throw new Error('the rotate-root-key request is missing');
      expect(await prepare.json()).toEqual({ phase: 'prepare' });
      expect(container.textContent).toContain('epoch 2');
    } finally {
      await unmount();
    }
  });
});
