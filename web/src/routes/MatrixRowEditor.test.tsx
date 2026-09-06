// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { retireSensitiveOperations } from '../api/sensitiveMutation.ts';
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

async function renderEditor(record: MatrixRowEditorProps['keyRecord'] = keyRecord, entries: MatrixRowEditorProps['rows'] = rows) {
  const onApply = vi
    .fn<(changes: readonly MatrixEditorChange[]) => Promise<void>>()
    .mockResolvedValue(undefined);
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  const queries = new QueryClient({ defaultOptions: { queries: { enabled: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={queries}>
        <MemoryRouter>
          <MatrixRowEditor
            refData={{ org: 'org-a', project: 'project-a' }}
            keyRecord={record}
            environmentId={environmentId}
            rows={entries}
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
    queries,
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

    // Only the readable source column is editable, the degraded column is not.
    expect(view.container.querySelectorAll('textarea[id^="matrix-edit-"]')).toHaveLength(1);
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

it('clears secret fill-all input on transfer, refusal and session retirement', async () => {
  const first = rows[0];
  if (first === undefined) throw new Error('fixture row is missing');
  const secretRows: MatrixRowEditorProps['rows'] = [first, {
    ...first, environmentId: 'env-second', environment: { ...environment, id: 'env-second', name: 'production' },
  }].map((row) => ({ ...row, cell: undefined }));
  const view = await renderEditor({ ...keyRecord, classification: 'secret' }, secretRows);
  const button = (label: string) => {
    const result = [...view.container.querySelectorAll('button')].find((node) => node.textContent === label);
    if (result === undefined) throw new Error(`Missing button: ${label}`);
    return result;
  };
  try {
    await act(async () => button('Edit all environments').click());
    const fill = view.container.querySelector<HTMLTextAreaElement>('textarea#matrix-fill-all');
    if (fill === null) throw new Error('fill-all input missing');
    // A masked textarea, not a password input: newlines survive.
    expect(fill.classList.contains('matrix-editor__value--masked')).toBe(true);
    const rowValues = () =>
      [...view.container.querySelectorAll<HTMLTextAreaElement>('textarea[id^="matrix-edit-"]')].map((node) => node.value);
    await act(async () => typeInto(fill, 'SENTINEL-fill-all'));
    await act(async () => button('Fill all').click());
    expect(fill.value).toBe('');
    expect(rowValues()).toEqual(['SENTINEL-fill-all', 'SENTINEL-fill-all']);
    view.onApply.mockRejectedValueOnce(new Error('Refused'));
    await act(async () => button('Save 2 drafts').click());
    expect(view.onApply).toHaveBeenCalledOnce();
    expect(rowValues()).toEqual(['', '']);
    expect(view.container.textContent).toContain('Re-enter the value');
    await act(async () => typeInto(fill, 'SENTINEL-session-fill'));
    await act(async () => retireSensitiveOperations(view.queries));
    expect(fill.value).toBe('');
    expect(view.queries.getMutationCache().getAll()).toEqual([]);
  } finally { await view.unmount(); }
});

describe('MatrixRowEditor surface (a11y audit)', () => {
  const twoRows: MatrixRowEditorProps['rows'] = [rows[0]!, {
    ...rows[0]!,
    environmentId: 'env-second',
    environment: { ...environment, id: 'env-second', name: 'production' },
    cell: undefined,
  }];
  const button = (container: HTMLElement, label: string) => {
    const result = [...container.querySelectorAll('button')].find((node) => node.textContent === label);
    if (result === undefined) throw new Error(`Missing button: ${label}`);
    return result;
  };

  it('labels the dialog by its heading and closes only on a real backdrop click', async () => {
    const onClose = vi.fn();
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    const queries = new QueryClient({ defaultOptions: { queries: { enabled: false } } });
    await act(async () => {
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
              onClose={onClose}
              onApply={vi.fn()}
              onCopy={vi.fn()}
            />
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    const dialog = container.querySelector('dialog');
    if (dialog === null) throw new Error('dialog missing');
    const heading = dialog.querySelector('h2');
    expect(heading?.id).toBeTruthy();
    expect(dialog.getAttribute('aria-labelledby')).toBe(heading?.id);

    dialog.getBoundingClientRect = () =>
      ({ left: 100, top: 100, right: 300, bottom: 300, width: 200, height: 200, x: 100, y: 100, toJSON: () => ({}) });
    await act(async () => {
      dialog.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: 150, clientY: 150 }));
    });
    expect(onClose).not.toHaveBeenCalled();
    await act(async () => {
      dialog.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: 10, clientY: 10 }));
    });
    expect(onClose).toHaveBeenCalledOnce();
    await act(async () => root.unmount());
  });

  it('toggles edit-all with aria-expanded and offers the per-row clear only in the single view', async () => {
    const view = await renderEditor(keyRecord, twoRows);
    const clearButtons = () =>
      [...view.container.querySelectorAll('button')].filter((node) => node.textContent?.startsWith('Clear '));
    expect(clearButtons()).toHaveLength(1);
    const toggle = button(view.container, 'Edit all environments');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    await act(async () => toggle.click());
    expect(clearButtons()).toHaveLength(0);
    const back = button(view.container, 'Back to development only');
    expect(back.getAttribute('aria-expanded')).toBe('true');
    await act(async () => back.click());
    expect(clearButtons()).toHaveLength(1);
    await view.unmount();
  });

  it('summarises the declaration in words with the raw JSON nested, and links to the declaration', async () => {
    const view = await renderEditor({
      ...keyRecord,
      declaration: { rule: { type: 'string', pattern: '^[a-z]+$', max_length: 8 } },
      presence: {
        required_in: { mode: 'explicit', environment_ids: ['env-second'] },
        forbidden_in: { mode: 'none' },
      },
    }, twoRows);
    const schema = view.container.querySelector('.matrix-editor__schema');
    expect(schema?.querySelector('p')?.textContent).toBe('string · pattern · range · required in production');
    expect(schema?.querySelector(':scope > details > summary')?.textContent).toBe('Raw declaration');
    expect([...view.container.querySelectorAll('a')].some((a) => a.textContent === 'Edit declaration')).toBe(true);
    await view.unmount();
  });

  it('reads state as set/absent and pending set/clear, and marks errors with a glyph', async () => {
    const view = await renderEditor(
      { ...keyRecord, declaration: { any_of: [{ type: 'boolean' }, { type: 'integer', min: 5n }] } },
      [{ ...rows[0]!, cell: undefined, signal: { key_id: keyRecord.id, name: keyRecord.name, classification: 'config', pending_by_others: false, pending: { versionId: 'v1', operation: 'unset' } }, problems: [{ message: 'LOG_LEVEL is required in development but is absent.' }] }],
    );
    const head = view.container.querySelector('.matrix-row-editor__row-head')?.textContent ?? '';
    expect(head).toContain('· absent');
    expect(head).toContain('Δ pending clear');
    expect(view.container.querySelector('.matrix-row-editor__row .alert')?.getAttribute('role')).toBe('status');
    const textarea = view.container.querySelector<HTMLTextAreaElement>('textarea[id^="matrix-edit-"]');
    if (textarea === null) throw new Error('textarea missing');
    await act(async () => typeInto(textarea, 'abc'));
    const error = view.container.querySelector('.matrix-cell__error');
    expect(error?.textContent).toBe('✕ Enter a boolean (true or false), or an integer at least 5.');
    expect(error?.querySelector('[aria-hidden="true"]')?.textContent).toBe('✕ ');
    await view.unmount();
  });

  it('fails closed on reveal and copy for a secret until the guard grants it', async () => {
    const view = await renderEditor({ ...keyRecord, classification: 'secret' }, rows);
    const labels = [...view.container.querySelectorAll('button')].map((node) => node.textContent);
    expect(labels).not.toContain('Reveal LOG_LEVEL');
    expect(labels.some((label) => label?.startsWith('Copy LOG_LEVEL'))).toBe(false);
    // No guard answer yet, so no "no reveal for your role" claim either.
    expect(view.container.textContent).not.toContain('No reveal for your role here');
    await view.unmount();
  });
});
