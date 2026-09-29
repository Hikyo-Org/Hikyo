// @vitest-environment happy-dom
import { act, useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { CatalogueManageDialog } from './CatalogueManageDialog.tsx';

const mocks = vi.hoisted(() => ({
  canDeclare: true,
  canEdit: true,
  deleteFolder: vi.fn(),
  deleteGroup: vi.fn(),
}));

vi.mock('../api/definitions.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/definitions.ts')>();
  return {
    ...actual,
    useDefinitionsSettings: () => ({
      data: {
        definitions_source: 'db',
        can_declare_keys: mocks.canDeclare,
        can_edit_definitions: mocks.canEdit,
      },
    }),
  };
});

vi.mock('../api/catalogue.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/catalogue.ts')>();
  const base = { id: '', org_id: 'org_a', project_id: 'project_a', created_at: '2026-01-01T00:00:00Z' };
  const idle = { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false };
  return {
    ...actual,
    useFolders: () => ({ data: { items: [{ ...base, id: 'fld_a', path: 'app' }] } }),
    useKeyGroups: () => ({
      data: { items: [{ ...base, id: 'kgr_a', name: 'pair', members: [], inert: false }] },
    }),
    useCreateFolder: () => idle,
    useCreateKeyGroup: () => idle,
    useRenameFolder: () => idle,
    useRenameKeyGroup: () => idle,
    useDeleteFolder: () => ({ ...idle, mutate: mocks.deleteFolder }),
    useDeleteKeyGroup: () => ({ ...idle, mutate: mocks.deleteGroup }),
  };
});

afterEach(() => {
  mocks.canDeclare = true;
  mocks.canEdit = true;
  mocks.deleteFolder.mockReset();
  mocks.deleteGroup.mockReset();
});

/** Re-renders the dialog on demand, the way a settings refetch would. */
function Harness() {
  const [, setTick] = useState(0);
  return (
    <>
      <button type="button" data-testid="rerender" onClick={() => setTick((tick) => tick + 1)} />
      <CatalogueManageDialog refData={{ org: 'org_a', project: 'project_a' }} onClose={vi.fn()} />
    </>
  );
}

function buttons(container: HTMLElement, text: string): HTMLButtonElement[] {
  return [...container.querySelectorAll('button')].filter((button) => button.textContent === text);
}

async function click(button: HTMLButtonElement | null | undefined): Promise<void> {
  if (button == null) throw new Error('button missing');
  await act(async () => {
    button.click();
  });
}

async function rerender(container: HTMLElement): Promise<void> {
  await click(container.querySelector<HTMLButtonElement>('[data-testid="rerender"]'));
}

describe('CatalogueManageDialog permission gates', () => {
  it('withdraws a pending linked-key delete confirmation when the caller may no longer republish', async () => {
    const view = await renderForm(<Harness />);
    // Folder row first, linked-key set row second.
    await click(buttons(view.container, 'Delete')[1]);
    expect(buttons(view.container, 'Confirm delete')).toHaveLength(1);

    mocks.canDeclare = false;
    await rerender(view.container);

    expect(buttons(view.container, 'Confirm delete')).toHaveLength(0);
    expect(buttons(view.container, 'Delete')[1]?.disabled).toBe(true);
    // Folders republish nothing, so the folder delete stays offered.
    expect(buttons(view.container, 'Delete')[0]?.disabled).toBe(false);
    expect(mocks.deleteGroup).not.toHaveBeenCalled();
    await view.unmount();
  });

  it('withdraws a pending folder delete confirmation when the caller may no longer edit', async () => {
    const view = await renderForm(<Harness />);
    await click(buttons(view.container, 'Delete')[0]);
    expect(buttons(view.container, 'Confirm delete')).toHaveLength(1);

    mocks.canEdit = false;
    await rerender(view.container);

    expect(buttons(view.container, 'Confirm delete')).toHaveLength(0);
    expect(mocks.deleteFolder).not.toHaveBeenCalled();
    await view.unmount();
  });
});
