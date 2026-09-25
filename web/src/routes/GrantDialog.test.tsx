// @vitest-environment happy-dom
import { act } from 'react';
import { describe, expect, it, vi } from 'vitest';

import type { ServiceAccount } from '../api/identities.ts';
import { renderForm } from '../testkit/renderForm.tsx';
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
