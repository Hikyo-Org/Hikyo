// @vitest-environment happy-dom
import { act, useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { MachineEnvScope, ServiceAccount } from '../api/identities.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { GrantDialog } from './MachineAccess.tsx';

const ACCOUNT: ServiceAccount = {
  id: 'svc_a',
  principal_id: 'mch_workload',
  name: 'worker',
  kind: 'workload',
  created_at: '2026-08-01T00:00:00Z',
  created_by: 'usr_admin',
  live_credentials: 1,
};

describe('GrantDialog with nothing to widen', () => {
  // An empty scope makes `grantableFor` empty for both capabilities, so
  // `GrantBody`, which owns the dialog's action row, is never mounted. Without
  // an action of its own that branch would leave Escape as the only way out,
  // which is not a way out a mouse has.
  it('offers a Close button that dismisses the dialog', async () => {
    const onClose = vi.fn();
    const { container } = await renderForm(
      <GrantDialog
        project={{ org: 'org_acme', project: 'prj_payments' }}
        account={ACCOUNT}
        scope={[]}
        machineReveal={false}
        mayGrantReporting={false}
        liveCredentials={1}
        onClose={onClose}
        onGranted={vi.fn()}
      />,
    );

    expect(container.querySelector('dialog[open] .dialog__title')?.textContent).toBe(
      'Add environment grant · worker',
    );
    expect(container.textContent).toContain('There is nothing to widen');

    const buttons = Array.from(container.querySelectorAll('dialog[open] button'));
    expect(buttons.map((button) => button.textContent)).toEqual(['Close']);
    const close = buttons[0];
    if (!(close instanceof HTMLButtonElement)) {
      throw new Error('the nothing-to-widen branch has no Close button');
    }
    await act(async () => close.click());
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

describe('GrantDialog while a grant is in flight', () => {
  // A report-only grant can take the last grantable option. The grant
  // listing refetch lands before the submission settles, and the dialog must
  // keep GrantBody (its Granting state and navigation guard) mounted rather
  // than swap in the bare Close branch mid-request.
  it('keeps the body mounted when the refreshed scope leaves nothing to widen', async () => {
    const env: MachineEnvScope = {
      id: 'env_123e4567-e89b-12d3-a456-426614174010',
      name: 'production',
      read: true,
      reveal: false,
      report: false,
      origins: [],
    };
    let resolveGrant: (response: Response) => void = () => {};
    vi.stubGlobal(
      'fetch',
      vi.fn((input: Parameters<typeof fetch>[0]) => {
        const request = input instanceof Request ? input : new Request(input);
        const path = new URL(request.url, 'http://localhost').pathname;
        if (path === '/api/v1/meta') {
          return Promise.resolve(
            jsonResponse({ server_version: '1.4.0', api_revision: 5, protocol_capabilities: ['delivery-target-report/1'] }),
          );
        }
        if (request.method === 'POST' && path.endsWith('/grants')) {
          return new Promise<Response>((resolve) => {
            resolveGrant = resolve;
          });
        }
        return Promise.resolve(jsonResponse({ error: 'not_found' }, 404));
      }),
    );
    document.cookie = '__Host-hikyo-csrf=token';

    const onGranted = vi.fn();
    // The refetched grant listing, as a click: the scope prop moves under the
    // open dialog exactly as it does when the page's grants query settles.
    function Host() {
      const [scope, setScope] = useState<readonly MachineEnvScope[]>([env]);
      return (
        <>
          <button type="button" id="refetch" onClick={() => setScope([{ ...env, report: true }])}>
            refetch
          </button>
          <GrantDialog
          project={{ org: 'org_acme', project: 'prj_payments' }}
          account={ACCOUNT}
          scope={scope}
          machineReveal={false}
          mayGrantReporting
          liveCredentials={1}
          onClose={vi.fn()}
          onGranted={onGranted}
          />
        </>
      );
    }
    const { container, unmount } = await renderForm(<Host />);
    await settleTask();

    const buttonNamed = (name: string) =>
      Array.from(container.querySelectorAll('dialog[open] button')).find((b) => b.textContent === name);
    const grant = buttonNamed('Grant report-delivery-status');
    if (!(grant instanceof HTMLButtonElement)) {
      throw new Error('the report-only grant is not offered');
    }
    await act(async () => grant.click());
    await settleTask();
    expect(buttonNamed('Granting…')).toBeDefined();

    // The refetched grants land: report now held everywhere, nothing to widen.
    const refetch = container.querySelector('#refetch');
    if (!(refetch instanceof HTMLButtonElement)) {
      throw new Error('no refetch control');
    }
    await act(async () => refetch.click());
    expect(buttonNamed('Close')).toBeUndefined();
    expect(buttonNamed('Granting…')).toBeDefined();

    resolveGrant(
      jsonResponse(
        { grant_id: 'grt_123e4567-e89b-12d3-a456-426614174030', capability: 'report-delivery-status', outcome: 'created' },
      ),
    );
    await settleTask();
    expect(onGranted).toHaveBeenCalledTimes(1);
    await unmount();
  });
});
