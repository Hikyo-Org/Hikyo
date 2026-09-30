// @vitest-environment happy-dom
import { act } from 'react';
import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { FolderMove, FolderMoveOutcome } from '../api/catalogue.ts';
import { FolderCleanupDialog } from './FolderCleanupDialog.tsx';
import type { WideningRefusal } from './accessRules/KeyMoveConfirmDialog.tsx';

const proposals = [
  { id: 'id_HIKYO_ARGON2_TIME', name: 'HIKYO_ARGON2_TIME', folder: 'Argon2' },
  { id: 'id_HIKYO_ARGON2_MEMORY_KIB', name: 'HIKYO_ARGON2_MEMORY_KIB', folder: 'Argon2' },
  { id: 'id_HIKYO_EXTERNAL_ORIGIN', name: 'HIKYO_EXTERNAL_ORIGIN', folder: '' },
];

afterEach(() => {
  document.body.innerHTML = '';
});



async function render(onApply: (moves: readonly FolderMove[]) => Promise<readonly FolderMoveOutcome[]>) {
  const onClose = vi.fn();
  const view = await renderForm(
    <FolderCleanupDialog
        proposals={proposals}
        existingFolders={['Legacy']}
        busy={false}
        envName={(id) => (id === 'env_01989abc-def0-7123-8123-00000000000b' ? 'production' : id)}
        onApply={onApply}
        onClose={onClose}
      />
  );
  const { container } = view;
  const button = (label: RegExp) => {
    const found = [...container.querySelectorAll('button')].find((node) => label.test(node.textContent ?? ''));
    if (found === undefined) throw new Error(`no button ${String(label)}`);
    return found;
  };
  const input = (label: string) => {
    const found = container.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`);
    if (found === null) throw new Error(`no input ${label}`);
    return found;
  };
  return { container, onClose, button, input, unmount: view.unmount };
}

describe('FolderCleanupDialog', () => {
  it('ticks proposed keys only, and moves exactly the ticked ones with their edited folders', async () => {
    const onApply = vi.fn<(moves: readonly FolderMove[]) => Promise<readonly FolderMoveOutcome[]>>(
      async (moves) => moves.map((move) => ({ id: move.id, error: null })),
    );
    const view = await render(onApply);
    expect(view.input('Move HIKYO_ARGON2_TIME').checked).toBe(true);
    expect(view.input('Move HIKYO_EXTERNAL_ORIGIN').checked).toBe(false);
    expect(view.button(/^Move/).textContent).toBe('Move 2 key(s)');
    // Datalist offers existing and proposed folders once each.
    const options = [...view.container.querySelectorAll('datalist option')].map((node) => node.getAttribute('value'));
    expect(options).toEqual(['Argon2', 'Legacy']);

    // Untick one, retype another, type a folder for the root key (which ticks it).
    await act(async () => {
      view.input('Move HIKYO_ARGON2_TIME').click();
    });
    await act(async () => {
      typeInto(view.input('Folder for HIKYO_ARGON2_MEMORY_KIB'), 'Hashing');
      typeInto(view.input('Folder for HIKYO_EXTERNAL_ORIGIN'), 'Web');
    });
    expect(view.input('Move HIKYO_EXTERNAL_ORIGIN').checked).toBe(true);
    expect(view.button(/^Move/).textContent).toBe('Move 2 key(s)');

    await act(async () => {
      view.button(/^Move/).click();
    });
    expect(onApply).toHaveBeenCalledWith([
      { id: 'id_HIKYO_ARGON2_MEMORY_KIB', name: 'HIKYO_ARGON2_MEMORY_KIB', folder: 'Hashing' },
      { id: 'id_HIKYO_EXTERNAL_ORIGIN', name: 'HIKYO_EXTERNAL_ORIGIN', folder: 'Web' },
    ]);
    // Every move succeeded: the dialog closes itself.
    expect(view.onClose).toHaveBeenCalledTimes(1);
    await view.unmount();
  });

  it('keeps refused keys with their refusal and drops moved ones', async () => {
    const onApply = vi.fn<(moves: readonly FolderMove[]) => Promise<readonly FolderMoveOutcome[]>>(
      async (moves) =>
        moves.map((move) => ({
          id: move.id,
          error: move.name === 'HIKYO_ARGON2_TIME' ? 'Not moved: budget used up.' : null,
        })),
    );
    const view = await render(onApply);
    await act(async () => {
      view.button(/^Move/).click();
    });
    expect(view.onClose).not.toHaveBeenCalled();
    const keys = [...view.container.querySelectorAll('.catalogue-manage__row .mono')].map((node) => node.textContent);
    expect(keys).toEqual(['HIKYO_ARGON2_TIME', 'HIKYO_EXTERNAL_ORIGIN']);
    expect(view.container.querySelector('[role="alert"]')?.textContent).toContain('Not moved: budget used up.');
    expect(view.container.textContent).toContain('Moved 1 so far.');
    // The refused key is still ticked, so a retry is one click.
    expect(view.button(/^Move/).textContent).toBe('Move 1 key(s)');
    await view.unmount();
  });

  const gainer = 'usr_01989abc-def0-7123-8123-00000000000a';
  /** Refuses HIKYO_ARGON2_TIME's unconfirmed move as widening; moves the rest. */
  const widenedApply = (widening: WideningRefusal) =>
    vi.fn<(moves: readonly FolderMove[]) => Promise<readonly FolderMoveOutcome[]>>(async (moves) =>
      moves.map((move) =>
        move.name === 'HIKYO_ARGON2_TIME' && move.confirmWidening === undefined
          ? { id: move.id, error: 'Not moved: this move gives people new access through their access rules.', widening }
          : { id: move.id, error: null },
      ),
    );
  const openDialog = (container: HTMLElement): HTMLElement => {
    const found = [...container.querySelectorAll('dialog[open]')].find(
      (node) => node.querySelector('.dialog__title')?.textContent === 'This move gives people new access',
    );
    if (!(found instanceof HTMLElement)) throw new Error('widening confirmation missing');
    return found;
  };

  it('keeps a widened key with a review that names the gainers and resends only it with confirm_widening', async () => {
    const onApply = widenedApply({
      count: 1,
      gainers: [
        { principal_id: gainer, principal_name: 'Dana Ruiz', capability: 'reveal', environments: ['env_01989abc-def0-7123-8123-00000000000b'] },
      ],
    });
    const view = await render(onApply);
    await act(async () => {
      view.button(/^Move/).click();
    });
    expect(view.onClose).not.toHaveBeenCalled();
    await act(async () => {
      view.button(/^Review access for HIKYO_ARGON2_TIME/).click();
    });
    const dialog = openDialog(view.container);
    expect(dialog.textContent).toContain('Move HIKYO_ARGON2_TIME from (no folder) to Argon2/');
    expect(dialog.textContent).toContain('Dana Ruiz: Reveal (production)');
    const confirm = [...dialog.querySelectorAll('button')].find((node) => node.textContent === 'Move and give access');
    if (confirm === undefined) throw new Error('confirm missing');
    await act(async () => confirm.click());
    expect(onApply).toHaveBeenLastCalledWith([
      { id: 'id_HIKYO_ARGON2_TIME', name: 'HIKYO_ARGON2_TIME', folder: 'Argon2', confirmWidening: [gainer] },
    ]);
    // Moved: the key leaves the list and the confirmation closes.
    const keys = [...view.container.querySelectorAll('.catalogue-manage__row .mono')].map((node) => node.textContent);
    expect(keys).toEqual(['HIKYO_EXTERNAL_ORIGIN']);
    expect(view.container.querySelector('dialog[open] .access-diff')).toBeNull();
    await view.unmount();
  });

  it('offers no confirm when the refusal carries only a count', async () => {
    const view = await render(widenedApply({ count: 2 }));
    await act(async () => {
      view.button(/^Move/).click();
    });
    await act(async () => {
      view.button(/^Review access for HIKYO_ARGON2_TIME/).click();
    });
    const dialog = openDialog(view.container);
    expect(dialog.textContent).toContain('Gains access: 2 people');
    expect([...dialog.querySelectorAll('button')].map((node) => node.textContent)).toEqual(['Cancel']);
    await view.unmount();
  });
});
