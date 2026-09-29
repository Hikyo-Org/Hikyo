// @vitest-environment happy-dom
import { act } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { GIT_DEFINITIONS_NOTICE } from '../api/definitions.ts';
import { renderForm, settle } from '../testkit/renderForm.tsx';
import { DefinitionsBundlePanel, LastApplyProvenance, monoCommands } from './DefinitionsBundlePanel.tsx';
import type { WideningRefusal } from './accessRules/KeyMoveConfirmDialog.tsx';

const mocks = vi.hoisted(() => ({ apply: vi.fn() }));

const diff = { creates: [], updates: [], renames: [], deletes: [] };
const plan = {
  id: 'pln_123e4567-e89b-12d3-a456-426614174000',
  digest: 'a'.repeat(64),
  current_revision: 2n,
  additive: true,
  expires_at: '2030-01-01T00:00:00Z',
  protected_environments: [],
  deletions_present: false,
  reveal_required: [],
  diff: { environments: diff, key_groups: diff, keys: diff, key_deletions: [], env_deletions: [], reveal_required: [] },
};

// The wire is covered in definitions-bundle.test.ts; here only the dialog's
// handling of an apply refusal is under test.
vi.mock('../api/definitions-bundle.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/definitions-bundle.ts')>();
  return {
    ...actual,
    readDefinitionsFile: async () => ({ format_version: 1, environments: [], key_groups: [], keys: [] }),
    checkBundle: async () => ({ state: 'file_ahead', current_revision: 2n, differences: { environments: diff, key_groups: diff, keys: diff } }),
    planBundle: async () => ({ plan }),
    applyBundle: mocks.apply,
  };
});

vi.mock('../api/settings.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/settings.ts')>();
  return {
    ...actual,
    useEnvironments: () => ({
      data: { items: [{ id: 'env_123e4567-e89b-12d3-a456-42661417000b', name: 'production' }], count: 1 },
    }),
  };
});

beforeEach(() => mocks.apply.mockReset());

describe('the Git-mode bundle notice', () => {
  it('keeps the normative sentence verbatim with its command names in mono', () => {
    const container = document.createElement('div');
    container.innerHTML = renderToStaticMarkup(<p>{monoCommands(GIT_DEFINITIONS_NOTICE)}</p>);
    expect(container.textContent).toBe(GIT_DEFINITIONS_NOTICE.replaceAll('`', ''));
    expect([...container.querySelectorAll('.mono')].map((m) => m.textContent)).toEqual([
      'definitions plan',
      'definitions apply',
    ]);
  });

  it('shows last-applied commit, ref and actor as display-only labels', () => {
    const container = document.createElement('div');
    container.innerHTML = renderToStaticMarkup(
      <LastApplyProvenance
        lastApply={{
          plan_id: 'pln_123e4567-e89b-12d3-a456-426614174000',
          applied_at: '2026-09-01T10:00:00Z',
          applied_by: 'prn_123e4567-e89b-12d3-a456-426614174000',
          commit: 'abc1234',
          ref: 'refs/heads/main',
          revision: 7n,
        }}
      />,
    );
    expect([...container.querySelectorAll('.mono')].map((m) => m.textContent)).toEqual([
      'abc1234',
      'refs/heads/main',
    ]);
    expect(container.textContent).not.toContain('Actor');
    expect(container.textContent).toContain('(display only, not verified)');
    expect(container.querySelector('time')?.getAttribute('datetime')).toBe('2026-09-01T10:00:00Z');
  });
});

describe('applying a plan whose folder moves widen access', () => {
  const gainer = 'prn_123e4567-e89b-12d3-a456-42661417000a';
  const refusal = (widening: WideningRefusal) =>
    new ApiError(409, 'widens access', undefined, undefined, [], widening);

  function buttonIn(root: Element, text: string): HTMLButtonElement {
    const found = [...root.querySelectorAll('button')].find((node) => node.textContent === text);
    if (found === undefined) throw new Error(`button "${text}" missing`);
    return found;
  }
  function wideningDialog(container: HTMLElement): HTMLElement {
    const found = [...container.querySelectorAll('dialog[open]')].find(
      (node) => node.querySelector('.dialog__title')?.textContent === 'This move gives people new access',
    );
    if (!(found instanceof HTMLElement)) throw new Error('widening confirmation missing');
    return found;
  }

  /** Select a file, check it, plan it and confirm the apply. */
  async function applyOnce() {
    const view = await renderForm(
      <DefinitionsBundlePanel org="org-1" project="project-1" settings={{ definitions_source: 'db' }} />,
    );
    await act(async () => buttonIn(view.container, 'Check a bundle').click());
    const input = view.container.querySelector<HTMLInputElement>('input[type="file"]');
    if (input === null) throw new Error('file input missing');
    Object.defineProperty(input, 'files', { value: [new File(['{}'], 'definitions.json')] });
    await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
    await settle();
    await act(async () => buttonIn(view.container, 'Check bundle').click());
    await settle();
    await act(async () => buttonIn(view.container, 'Create impact plan').click());
    await settle();
    await act(async () => buttonIn(view.container, 'Review and apply').click());
    await act(async () => buttonIn(view.container, 'Apply and publish').click());
    await settle();
    return view;
  }

  it('opens the confirmation naming the gainers and resends the plan with confirm_widening', async () => {
    mocks.apply
      .mockRejectedValueOnce(
        refusal({
          count: 1,
          gainers: [
            { principal_id: gainer, principal_name: 'Dana Ruiz', capability: 'reveal', environments: ['env_123e4567-e89b-12d3-a456-42661417000b'] },
          ],
        }),
      )
      .mockResolvedValueOnce({ revision: 3n, published: ['production'], plan_id: plan.id });
    const view = await applyOnce();
    const dialog = wideningDialog(view.container);
    expect(dialog.textContent).toContain('Apply this definitions plan');
    expect(dialog.textContent).toContain('Dana Ruiz: Reveal (production)');
    // The refusal is the confirmation, not an inline error.
    expect(view.container.querySelector('.definitions-bundle__body [role="alert"]')).toBeNull();

    await act(async () => buttonIn(dialog, 'Move and give access').click());
    await settle();
    expect(mocks.apply).toHaveBeenCalledTimes(2);
    // Same plan, same deletion consent, same (no) acknowledgements, plus the gainers.
    expect(mocks.apply.mock.calls[1]?.slice(1, 3)).toEqual([plan, false]);
    expect(mocks.apply.mock.calls[1]?.slice(5)).toEqual([[], [gainer]]);
    expect(view.container.textContent).toContain('Definitions applied at revision 3');
    expect(view.container.querySelector('dialog[open] .access-diff')).toBeNull();
    await view.unmount();
  });

  it('offers no confirm when the refusal carries only a count', async () => {
    mocks.apply.mockRejectedValueOnce(refusal({ count: 3 }));
    const view = await applyOnce();
    const dialog = wideningDialog(view.container);
    expect(dialog.textContent).toContain('Gains access: 3 people');
    expect([...dialog.querySelectorAll('button')].map((node) => node.textContent)).toEqual(['Cancel']);
    await act(async () => buttonIn(dialog, 'Cancel').click());
    expect(view.container.querySelector('dialog[open] .access-diff')).toBeNull();
    expect(mocks.apply).toHaveBeenCalledTimes(1);
    await view.unmount();
  });
});
