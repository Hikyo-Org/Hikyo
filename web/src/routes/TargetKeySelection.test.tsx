// @vitest-environment happy-dom
import { act } from 'react';
import { expect, it, vi } from 'vitest';

import type { AdapterTargetInput, ProjectKey } from '../api/adapters.ts';
import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { TargetForm } from './Adapters.tsx';
import { TargetKeySelection, useTargetKeySelection, type TargetKeySelectionValue } from './TargetKeySelection.tsx';

type SelectionRequest = Pick<AdapterTargetInput, 'key_ids' | 'key_selection'>;
const keys = [{ id: 'key_db', name: 'DB_URL' }, { id: 'key_mode', name: 'APP_MODE' }];

function SelectionForm({
  initialIds,
  catalogue = keys,
  onSubmit,
}: {
  readonly initialIds?: readonly string[];
  readonly catalogue?: readonly { readonly id: string; readonly name: string }[];
  readonly onSubmit: (request: SelectionRequest) => void;
}) {
  const selection = useTargetKeySelection(initialIds);
  return (
    <form onSubmit={(event) => {
      event.preventDefault();
      onSubmit({
        key_ids: selection.keyIds,
        ...(selection.keySelection === undefined ? {} : { key_selection: selection.keySelection }),
      });
    }}>
      <TargetKeySelection keys={catalogue} value={selection.value} onChange={selection.onChange} />
      <button type="submit">Save</button>
    </form>
  );
}

function input(container: HTMLElement, label: string): HTMLInputElement {
  const found = [...container.querySelectorAll('label')].find((candidate) => candidate.textContent === label);
  const control = found?.querySelector('input') ?? (found?.htmlFor ? container.querySelector(`#${CSS.escape(found.htmlFor)}`) : null);
  if (!(control instanceof HTMLInputElement)) throw new Error(`${label} input missing`);
  return control;
}

async function submit(container: HTMLElement): Promise<void> {
  const form = container.querySelector('form');
  if (form === null) throw new Error('selection form missing');
  await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
}

async function classify(container: HTMLElement, value: string): Promise<void> {
  const control = container.querySelector('select[name="key_classification"]');
  if (!(control instanceof HTMLSelectElement)) throw new Error('classification select missing');
  await act(async () => {
    control.value = value;
    control.dispatchEvent(new Event('change', { bubbles: true }));
  });
}

it('starts empty and never adds catalogue keys automatically', async () => {
  const requests: SelectionRequest[] = [];
  const onSubmit = (request: SelectionRequest) => { requests.push(request); };
  const { container, rerender, unmount } = await renderForm(<SelectionForm catalogue={[]} onSubmit={onSubmit} />);
  try {
    expect(container.textContent).toContain('This project has no keys yet.');
    await submit(container);
    expect(requests[0]).toEqual({ key_ids: [] });
    await rerender(<SelectionForm catalogue={keys} onSubmit={onSubmit} />);
    expect([...container.querySelectorAll('input[type="checkbox"]')].every((control) => control instanceof HTMLInputElement && !control.checked)).toBe(true);
    await submit(container);
    expect(requests[1]).toEqual({ key_ids: [] });
  } finally {
    await unmount();
  }
});

it('keeps initial ids missing from the catalogue and toggles only the chosen id', async () => {
  const requests: SelectionRequest[] = [];
  const { container, unmount } = await renderForm(
    <SelectionForm initialIds={['key_unlisted', 'key_db', 'key_db']} onSubmit={(request) => { requests.push(request); }} />,
  );
  try {
    expect(input(container, 'DB_URL').checked).toBe(true);
    await act(async () => input(container, 'APP_MODE').click());
    await act(async () => input(container, 'DB_URL').click());
    await submit(container);
    expect(requests[0]).toEqual({ key_ids: ['key_unlisted', 'key_mode'] });
  } finally {
    await unmount();
  }
});

it('preserves explicit ids and exact names while forwarding automatic selection filters', async () => {
  const requests: SelectionRequest[] = [];
  const { container, unmount } = await renderForm(
    <SelectionForm initialIds={['key_db']} onSubmit={(request) => { requests.push(request); }} />,
  );
  try {
    await act(async () => typeInto(input(container, 'Explicit names'), ' DB_URL, APP_MODE, , MISSING_KEY '));
    await act(async () => typeInto(input(container, 'Include patterns'), ' DB_*, API_*, '));
    await act(async () => typeInto(input(container, 'Exclude patterns'), ' DB_*, *_TEST '));
    await classify(container, 'config');
    expect(input(container, 'DB_URL').checked).toBe(true);
    await submit(container);
    expect(requests[0]).toEqual({
      key_ids: ['key_db'],
      key_selection: {
        names: ['DB_URL', 'APP_MODE', 'MISSING_KEY'],
        include: ['DB_*', 'API_*'],
        exclude: ['DB_*', '*_TEST'],
        classification: 'config',
      },
    });
    expect(container.textContent).toContain('never checked keys or explicit names');
    expect(container.textContent).toContain('when you save and are not stored');
  } finally {
    await unmount();
  }
});

it('forwards exclude-only input for server refusal and omits selectors after clearing them', async () => {
  const requests: SelectionRequest[] = [];
  const { container, unmount } = await renderForm(
    <SelectionForm initialIds={['key_db']} onSubmit={(request) => { requests.push(request); }} />,
  );
  try {
    await act(async () => typeInto(input(container, 'Exclude patterns'), 'DB_*'));
    await submit(container);
    expect(requests[0]).toEqual({ key_ids: ['key_db'], key_selection: { exclude: ['DB_*'] } });
    await act(async () => typeInto(input(container, 'Exclude patterns'), ' , '));
    await act(async () => typeInto(input(container, 'Explicit names'), ' , '));
    await act(async () => typeInto(input(container, 'Include patterns'), ' , '));
    await submit(container);
    expect(requests[1]).toEqual({ key_ids: ['key_db'] });
  } finally {
    await unmount();
  }
});

it('sends names-only and classification-only selections without inventing explicit ids', async () => {
  const requests: SelectionRequest[] = [];
  const { container, unmount } = await renderForm(
    <SelectionForm onSubmit={(request) => { requests.push(request); }} />,
  );
  try {
    await act(async () => typeInto(input(container, 'Explicit names'), ' APP_MODE '));
    await submit(container);
    expect(requests[0]).toEqual({ key_ids: [], key_selection: { names: ['APP_MODE'] } });
    await act(async () => typeInto(input(container, 'Explicit names'), ''));
    await classify(container, 'config');
    await submit(container);
    expect(requests[1]).toEqual({ key_ids: [], key_selection: { classification: 'config' } });
    await classify(container, '');
    await submit(container);
    expect(requests[2]).toEqual({ key_ids: [] });
  } finally {
    await unmount();
  }
});

it('uses the supplied controlled value and disables every control when requested', async () => {
  const value: TargetKeySelectionValue = { keyIds: [], names: '', include: '', exclude: '', classification: '' };
  const onChange = vi.fn<(next: TargetKeySelectionValue) => void>();
  const { container, rerender, unmount } = await renderForm(<TargetKeySelection keys={keys} value={value} onChange={onChange} />);
  try {
    await act(async () => input(container, 'DB_URL').click());
    expect(onChange).toHaveBeenCalledWith({ ...value, keyIds: ['key_db'] });
    expect(input(container, 'DB_URL').checked).toBe(false);
    await rerender(<TargetKeySelection keys={keys} value={{ ...value, keyIds: ['key_mode'], names: 'DB_URL' }} onChange={onChange} disabled />);
    expect(input(container, 'APP_MODE').checked).toBe(true);
    expect(input(container, 'Explicit names').value).toBe('DB_URL');
    expect([...container.querySelectorAll('input, select')].every((control) =>
      (control instanceof HTMLInputElement || control instanceof HTMLSelectElement) && control.disabled,
    )).toBe(true);
  } finally {
    await unmount();
  }
});

it('TargetForm submits the shared selection without filtering ids or changing routing', async () => {
  const key: ProjectKey = {
    id: 'key_db', name: 'DB_URL', classification: 'secret', org_id: 'org_1', project_id: 'prj_1',
    folder_path: '', description: '', deprecated: false, deprecation_note: '', group_id: '',
    declaration: { rule: { type: 'string', allow_empty: true } },
    presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
    created_at: '2026-10-16T00:00:00Z',
  };
  const requests: AdapterTargetInput[] = [];
  const { container, unmount } = await renderForm(
    <TargetForm title="Add target" environments={[{ id: 'env_1', name: 'prod' }]} keys={[key]} busy={false} onCancel={() => undefined} onSubmit={(request) => { requests.push(request); return Promise.resolve(); }} />,
  );
  try {
    await act(async () => input(container, 'DB_URL').click());
    await act(async () => typeInto(input(container, 'Explicit names'), ' APP_MODE, '));
    await act(async () => typeInto(input(container, 'Include patterns'), 'DB_*'));
    await act(async () => typeInto(input(container, 'Exclude patterns'), 'DB_*'));
    await classify(container, 'config');
    await submit(container);
    expect(requests[0]).toMatchObject({
      environment_id: 'env_1', destination_kind: 'repository',
      key_ids: ['key_db'],
      key_selection: { names: ['APP_MODE'], include: ['DB_*'], exclude: ['DB_*'], classification: 'config' },
    });
  } finally {
    await unmount();
  }
});
