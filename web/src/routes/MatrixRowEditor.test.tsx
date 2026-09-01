// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { MatrixRowEditor, type MatrixEditorChange } from './MatrixRowEditor.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

type MatrixRowEditorProps = Parameters<typeof MatrixRowEditor>[0];

const environmentId = 'env_01989abc-def0-7123-8123-123456789abc';
const environment: MatrixRowEditorProps['rows'][number]['environment'] = {
  id: environmentId,
  org_id: 'org_01989abc-def0-7123-8123-123456789abc',
  project_id: 'prj_01989abc-def0-7123-8123-123456789abc',
  name: 'development',
  display_order: 0,
  created_at: '2026-08-23T08:00:00Z',
};
const keyRecord: MatrixRowEditorProps['keyRecord'] = {
  id: 'key_01989abc-def0-7123-8123-123456789abc',
  org_id: environment.org_id,
  project_id: environment.project_id,
  name: 'LOG_LEVEL',
  folder_path: '',
  classification: 'config',
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string', allow_empty: true } },
  presence: {
    required_in: { mode: 'none' },
    forbidden_in: { mode: 'none' },
  },
  group_id: '',
  created_at: '2026-08-23T08:00:00Z',
};
const rows: MatrixRowEditorProps['rows'] = [{
  environmentId,
  environment,
  protected: false,
  degraded: false,
  cell: {
    key_id: keyRecord.id,
    name: keyRecord.name,
    classification: 'config',
    set: true,
    revealed: true,
    value: 'published',
  },
  signal: undefined,
  draftPreview: undefined,
  problems: [],
}];

afterEach(() => document.body.replaceChildren());

describe('MatrixRowEditor draft ownership', () => {
  it('restores the initial value after type, clear, then keep', async () => {
    const view = await renderEditor();

    const textarea = view.container.querySelector<HTMLTextAreaElement>('textarea');
    const clearButton = [...view.container.querySelectorAll('button')].find((candidate) =>
      candidate.textContent?.startsWith('Clear '),
    );
    if (textarea === null || clearButton === undefined) {
      throw new Error('row controls are missing');
    }

    await act(async () => typeInto(textarea, 'edited'));
    await act(async () => clearButton.click());
    await act(async () => clearButton.click());

    const saveButton = view.container.querySelector<HTMLButtonElement>('button[type="submit"]');
    expect(textarea.value).toBe('published');
    expect(saveButton?.disabled).toBe(true);

    await view.unmount();
  });

  it('submits a touched empty field as an explicit set', async () => {
    const view = await renderEditor();

    const textarea = view.container.querySelector<HTMLTextAreaElement>('textarea');
    const saveButton = view.container.querySelector<HTMLButtonElement>('button[type="submit"]');
    if (textarea === null || saveButton === null) {
      throw new Error('row form controls are missing');
    }

    await act(async () => typeInto(textarea, ''));
    await act(async () => saveButton.click());

    expect(view.onApply).toHaveBeenCalledWith([
      { environmentId, operation: 'set', value: '' },
    ]);

    await view.unmount();
  });
});

async function renderEditor() {
  const onApply = vi
    .fn<(changes: readonly MatrixEditorChange[]) => Promise<void>>()
    .mockResolvedValue(undefined);
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  await act(async () => {
    const queries = new QueryClient({ defaultOptions: { queries: { enabled: false } } });
    root.render(
      <QueryClientProvider client={queries}>
        <MemoryRouter>
          <MatrixRowEditor
            refData={{ org: 'org-a', project: 'project-a' }}
            keyRecord={keyRecord}
            environmentId={environmentId}
            rows={rows}
            busy={false}
            mutationError={null}
            onClose={vi.fn()}
            onApply={onApply}
            onCopy={vi.fn()}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });

  return {
    container,
    onApply,
    unmount: async () => act(async () => root.unmount()),
  };
}

describe('MatrixRowEditor degraded columns (#451)', () => {
  const degradedRows: MatrixRowEditorProps['rows'] = [
    rows[0]!,
    {
      environmentId: 'env_01989abc-def0-7123-8123-1234567890ff',
      environment: { ...environment, id: 'env_01989abc-def0-7123-8123-1234567890ff', name: 'production' },
      protected: false,
      degraded: true,
      cell: undefined,
      signal: undefined,
      draftPreview: undefined,
      problems: [],
    },
  ];

  async function renderWith(rowsInput: MatrixRowEditorProps['rows']) {
    const onApply = vi
      .fn<(changes: readonly MatrixEditorChange[]) => Promise<void>>()
      .mockResolvedValue(undefined);
    const onCopy = vi.fn();
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () => {
      const queries = new QueryClient({ defaultOptions: { queries: { enabled: false } } });
      root.render(
        <QueryClientProvider client={queries}>
          <MemoryRouter>
            <MatrixRowEditor
              refData={{ org: 'org-a', project: 'project-a' }}
              keyRecord={keyRecord}
              environmentId={environmentId}
              rows={rowsInput}
              busy={false}
              mutationError={null}
              onClose={vi.fn()}
              onApply={onApply}
              onCopy={onCopy}
            />
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    return { container, onApply, onCopy, unmount: async () => act(async () => root.unmount()) };
  }

  it('excludes a degraded column from bulk edit and copy destinations', async () => {
    const view = await renderWith(degradedRows);

    const editAll = [...view.container.querySelectorAll('button')].find(
      (button) => button.textContent === 'Edit all environments',
    );
    if (editAll === undefined) throw new Error('edit-all toggle missing');
    await act(async () => editAll.click());

    // Only the readable source column is editable — the degraded column is not.
    expect(view.container.querySelectorAll('textarea')).toHaveLength(1);
    expect(view.container.textContent).not.toContain('production');

    const copyToggle = [...view.container.querySelectorAll('button')].find((button) =>
      button.textContent?.startsWith('Copy'),
    );
    if (copyToggle !== undefined) {
      await act(async () => copyToggle.click());
      // The degraded column is not offered as a copy destination.
      expect(view.container.textContent).not.toContain('production');
    }

    await view.unmount();
  });
});

function typeInto(textarea: HTMLTextAreaElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
  if (setter === undefined) throw new Error('HTMLTextAreaElement exposes no value setter');
  setter.call(textarea, value);
  textarea.dispatchEvent(new Event('input', { bubbles: true }));
}
