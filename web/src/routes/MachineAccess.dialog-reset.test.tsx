// @vitest-environment happy-dom
import { useState } from 'react';
import { renderForm } from '../testkit/renderForm.tsx';
import { afterEach, describe, expect, it } from 'vitest';

import { useResetOnChange } from '../app/useResetOnChange.ts';

/**
 * MachineAccess closes its create/binding/grant and lease dialogs whenever the
 * project or live session changes, so a submit after the boundary can never
 * target the new project or act under a replaced session. That reset is a
 * one-line `useResetOnChange` call keyed on `${org} ${project} ${session}`.
 *
 * This test exercises that exact contract in a minimal host rather than
 * rendering MachineAccess itself (its query surface is large): the host mirrors
 * MachineAccess's three dialog states and the same signature and reset closure.
 */
function DialogHost({ org, project, session }: { org: string; project: string; session: string }) {
  const [dialog, setDialog] = useState<string | null>('create');
  const [leaseMintOpen, setLeaseMintOpen] = useState(true);
  const [leaseAction, setLeaseAction] = useState<string | null>('extend');
  useResetOnChange(`${org} ${project} ${session}`, () => {
    setDialog(null);
    setLeaseMintOpen(false);
    setLeaseAction(null);
  });
  const open = dialog !== null || leaseMintOpen || leaseAction !== null;
  return <div data-open={open ? 'yes' : 'no'} />;
}

let view: Awaited<ReturnType<typeof renderForm>> | null = null;

afterEach(async () => {
  await view?.unmount();
  view = null;
});

async function render(props: { org: string; project: string; session: string }): Promise<void> {
  if (view === null) view = await renderForm(<DialogHost {...props} />);
  else await view.rerender(<DialogHost {...props} />);
}

function isOpen(): boolean {
  return view?.container.querySelector('div')?.getAttribute('data-open') === 'yes';
}

describe('MachineAccess dialog reset', () => {
  it('closes open dialogs when the project changes', async () => {
    await render({ org: 'o1', project: 'p1', session: 's1' });
    expect(isOpen()).toBe(true);

    // Same boundary: an unrelated re-render leaves an open dialog alone.
    await render({ org: 'o1', project: 'p1', session: 's1' });
    expect(isOpen()).toBe(true);

    // Project change: every dialog closes.
    await render({ org: 'o1', project: 'p2', session: 's1' });
    expect(isOpen()).toBe(false);
  });

  it('closes open dialogs when the live session is replaced', async () => {
    await render({ org: 'o1', project: 'p1', session: 's1' });
    expect(isOpen()).toBe(true);
    await render({ org: 'o1', project: 'p1', session: 's2' });
    expect(isOpen()).toBe(false);
  });
});
