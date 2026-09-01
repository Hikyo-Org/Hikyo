// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { ApiError } from '../api/client.ts';
import { KeyDeclarationDetail } from './KeyDeclarationDetail.tsx';
import type { MatrixKey } from '../api/matrix.ts';

const mocks = vi.hoisted(() => ({
  key: vi.fn(),
  definitions: vi.fn(),
  mutate: vi.fn(),
}));

vi.mock('../api/matrix.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/matrix.ts')>();
  return {
    ...actual,
    useKey: () => mocks.key(),
    useUpdateKeyMetadata: () => ({ mutate: mocks.mutate, isPending: false }),
  };
});

vi.mock('../api/definitions.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/definitions.ts')>();
  return { ...actual, useDefinitionsSettings: () => mocks.definitions() };
});

vi.mock('../api/transport.tsx', async (importActual) => {
  const actual = await importActual<typeof import('../api/transport.tsx')>();
  return { ...actual, useWorkspaceContext: () => null };
});

const environments = [
  {
    id: 'env_a',
    org_id: 'org_a',
    project_id: 'project_a',
    name: 'development',
    display_order: 0,
    created_at: '2026-08-24T08:00:00Z',
  },
  {
    id: 'env_b',
    org_id: 'org_a',
    project_id: 'project_a',
    name: 'production',
    display_order: 1,
    created_at: '2026-08-24T08:00:00Z',
  },
];

const record: MatrixKey = {
  id: 'key_a',
  org_id: 'org_a',
  project_id: 'project_a',
  name: 'DATABASE_URL',
  folder_path: 'db',
  classification: 'secret',
  description: 'Primary datastore connection string',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'url', schemes: ['postgres'] } },
  presence: {
    required_in: { mode: 'explicit', environment_ids: ['env_b'] },
    forbidden_in: { mode: 'none' },
  },
  group_id: 'db',
  created_at: '2026-08-24T08:00:00Z',
};

function render() {
  return renderForm(
    <MemoryRouter initialEntries={['/orgs/org_a/projects/project_a/matrix/keys/key_a']}>
      <KeyDeclarationDetail
        refData={{ org: 'org_a', project: 'project_a' }}
        keyId="key_a"
        environments={environments}
        openerRef={{ current: null }}
      />
    </MemoryRouter>,
  );
}

function textOf(container: HTMLElement): string {
  return container.textContent ?? '';
}

beforeEach(() => {
  mocks.key.mockReset();
  mocks.definitions.mockReset();
  mocks.mutate.mockReset();
});

describe('KeyDeclarationDetail', () => {
  it('renders every declaration field and, in db mode, the editor', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' } });

    const view = await render();
    const text = textOf(view.container);

    expect(text).toContain('DATABASE_URL');
    expect(text).toContain('secret');
    expect(text).toContain('db'); // folder / group
    expect(text).toContain('Primary datastore connection string');
    // Value rule type and its constraint are named.
    expect(text).toContain('url');
    expect(text).toContain('postgres');
    // Explicit presence resolves the env id to its name.
    expect(text).toContain('production');
    // The db-mode editor is present.
    expect(
      [...view.container.querySelectorAll('button')].some((b) =>
        b.textContent?.includes('Save declaration'),
      ),
    ).toBe(true);
    // No secret VALUE is ever rendered (the record carries none).
    expect(text).not.toContain('••••');

    await view.unmount();
  });

  it('is read-only with the Git notice and provenance in git mode', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({
      data: {
        definitions_source: 'git',
        last_apply: { applied_by: 'ci-bot', commit: 'abc123', ref: 'main', actor: 'octo' },
      },
    });

    const view = await render();
    const text = textOf(view.container);

    expect(text).toContain('managed in Git');
    expect(text).toContain('ci-bot');
    expect(text).toContain('abc123');
    // The editor's save control must not exist in git mode.
    expect(
      [...view.container.querySelectorAll('button')].some((b) =>
        b.textContent?.includes('Save declaration'),
      ),
    ).toBe(false);

    await view.unmount();
  });

  it('keeps a deleted key recoverable', async () => {
    mocks.key.mockReturnValue({
      isPending: false,
      isError: true,
      error: new ApiError(404, 'not found'),
    });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' } });

    const view = await render();
    const text = textOf(view.container);

    expect(text).toContain('no longer exists');
    expect(
      [...view.container.querySelectorAll('a')].some((a) => a.textContent?.includes('Back to the matrix')),
    ).toBe(true);

    await view.unmount();
  });

  it('surfaces a refusal from a rejected metadata edit', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' } });
    mocks.mutate.mockImplementation(
      (
        _input: unknown,
        callbacks: { onError: (error: Error) => void },
      ) => callbacks.onError(new ApiError(403, 'forbidden')),
    );

    const view = await render();
    const description = view.container.querySelector<HTMLTextAreaElement>('textarea');
    if (description === null) throw new Error('editor textarea missing');
    await act(async () => {
      description.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const save = [...view.container.querySelectorAll('button')].find((b) =>
      b.textContent?.includes('Save declaration'),
    );
    if (save === undefined) throw new Error('save button missing');
    await act(async () => save.click());

    expect(textOf(view.container)).toContain('not have permission');

    await view.unmount();
  });

  it('sends only touched metadata fields on save', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' } });
    mocks.mutate.mockImplementation(
      (_input: unknown, callbacks: { onSuccess: () => void }) => callbacks.onSuccess(),
    );

    const view = await render();
    const folder = view.container.querySelector<HTMLInputElement>('input.mono');
    if (folder === null) throw new Error('folder input missing');
    await act(async () => typeInto(folder, 'database'));
    const save = [...view.container.querySelectorAll('button')].find((b) =>
      b.textContent?.includes('Save declaration'),
    );
    if (save === undefined) throw new Error('save button missing');
    await act(async () => save.click());

    expect(mocks.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ folderPath: 'database', description: record.description }),
      expect.anything(),
    );
    expect(textOf(view.container)).toContain('Saved.');

    await view.unmount();
  });
});
