// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { ApiError, type RefusalFinding } from '../api/client.ts';
import { KeyDeclarationDetail } from './KeyDeclarationDetail.tsx';
import { KEY_GONE_REFUSAL, type MatrixKey } from '../api/matrix.ts';

const mocks = vi.hoisted(() => ({
  key: vi.fn(),
  definitions: vi.fn(),
  mutate: vi.fn(),
  rename: vi.fn(),
  reclassify: vi.fn(),
  deleteKey: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock('../api/matrix.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/matrix.ts')>();
  return {
    ...actual,
    useKey: () => mocks.key(),
    useUpdateKeyMetadata: () => ({ mutate: mocks.mutate, isPending: false }),
    useRenameKey: () => ({ mutate: mocks.rename, isPending: false }),
    useReclassifyKey: () => ({ mutate: mocks.reclassify, isPending: false }),
    useDeleteKey: () => ({ mutate: mocks.deleteKey, isPending: false }),
  };
});

vi.mock('../api/definitions.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/definitions.ts')>();
  return { ...actual, useDefinitionsSettings: () => mocks.definitions() };
});

// The #493 editors (rules/presence, group) call catalogue hooks; stub them so
// this suite exercises the foundation without live fetches (the pure helpers —
// presenceImpact, catalogueRefusalText — stay real).
vi.mock('../api/catalogue.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/catalogue.ts')>();
  return {
    ...actual,
    useUpdateKeyDeclaration: () => ({ mutate: vi.fn(), isPending: false }),
    useSetKeyGroup: () => ({ mutate: vi.fn(), isPending: false }),
    useKeyGroups: () => ({
      data: { items: [], count: 0 },
      isSuccess: true,
      isError: false,
      isPending: false,
    }),
  };
});

vi.mock('../api/transport.tsx', async (importActual) => {
  const actual = await importActual<typeof import('../api/transport.tsx')>();
  return { ...actual, useWorkspaceContext: () => null };
});

vi.mock('react-router', async (importActual) => {
  const actual = await importActual<typeof import('react-router')>();
  return { ...actual, useNavigate: () => mocks.navigate };
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

const noImpact = { setEnvironmentIds: [], pendingEnvironmentIds: [] } as const;

function render(
  impact: { setEnvironmentIds: readonly string[]; pendingEnvironmentIds: readonly string[] } = noImpact,
  impactReady = true,
) {
  return renderForm(
    <MemoryRouter initialEntries={['/orgs/org_a/projects/project_a/matrix/keys/key_a']}>
      <KeyDeclarationDetail
        refData={{ org: 'org_a', project: 'project_a' }}
        keyId="key_a"
        environments={environments}
        impact={impact}
        impactReady={impactReady}
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
  mocks.rename.mockReset();
  mocks.reclassify.mockReset();
  mocks.deleteKey.mockReset();
  mocks.navigate.mockReset();
});

/** The db-managed, editable source mode the lifecycle actions require. */
function dbMode() {
  mocks.definitions.mockReturnValue({
    data: { definitions_source: 'db' },
    isSuccess: true,
    isError: false,
    isRefetchError: false,
  });
}

function buttonBy(container: HTMLElement, text: string): HTMLButtonElement {
  const button = [...container.querySelectorAll('button')].find((candidate) =>
    candidate.textContent?.includes(text),
  );
  if (button === undefined) throw new Error(`button "${text}" missing`);
  return button;
}

/** The confirm dialog's action button, matched EXACTLY so it is never confused
 *  with the section trigger whose label is a superset ("…config…"). */
function dialogButton(container: HTMLElement, text: string): HTMLButtonElement {
  const dialog = container.querySelector('dialog.matrix-editor');
  const button = [...(dialog?.querySelectorAll('button') ?? [])].find(
    (candidate) => candidate.textContent === text,
  );
  if (button === undefined) throw new Error(`dialog button "${text}" missing`);
  return button;
}

describe('KeyDeclarationDetail', () => {
  it('renders every declaration field and, in db mode, the editor', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });

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
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });

    const view = await render();
    const text = textOf(view.container);

    expect(text).toContain(KEY_GONE_REFUSAL);
    expect(
      [...view.container.querySelectorAll('a')].some((a) => a.textContent?.includes('Back to the matrix')),
    ).toBe(true);

    await view.unmount();
  });

  it('surfaces a refusal from a rejected metadata edit', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });
    mocks.mutate.mockImplementation(
      (
        _input: unknown,
        callbacks: { onError: (error: Error) => void },
      ) => callbacks.onError(new ApiError(403, 'forbidden')),
    );

    const view = await render();
    const description = view.container.querySelector<HTMLTextAreaElement>('textarea');
    if (description === null) throw new Error('editor textarea missing');
    // A real change, so the form is dirty and the save is enabled.
    await act(async () => setTextarea(description, 'A new description'));
    const save = [...view.container.querySelectorAll('button')].find((b) =>
      b.textContent?.includes('Save declaration'),
    );
    if (save === undefined) throw new Error('save button missing');
    await act(async () => save.click());

    expect(textOf(view.container)).toContain('not have permission');

    await view.unmount();
  });

  it('sends only the changed field on save', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });
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

    // Only the touched field travels — the untouched description is not written
    // back, so a concurrent edit to it is not clobbered.
    expect(mocks.mutate).toHaveBeenCalledWith({ folderPath: 'database' }, expect.anything());
    expect(textOf(view.container)).toContain('Saved.');

    await view.unmount();
  });

  it('keeps the save disabled until a field changes', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });

    const view = await render();
    const save = [...view.container.querySelectorAll('button')].find((b) =>
      b.textContent?.includes('Save declaration'),
    );
    expect(save?.disabled).toBe(true);

    await view.unmount();
  });

  it('fails closed: no editor while the source is unresolved', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    // Settings query still pending: definitions_source is unknown.
    mocks.definitions.mockReturnValue({ data: undefined, isSuccess: false, isError: false });

    const view = await render();
    // No live edit action may appear before the project is confirmed db-managed.
    expect(
      [...view.container.querySelectorAll('button')].some((b) =>
        b.textContent?.includes('Save declaration'),
      ),
    ).toBe(false);
    expect(textOf(view.container)).toContain('Editing unavailable');

    await view.unmount();
  });

  it('fails closed when a refetch errored over stale db data', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    // react-query keeps the prior success's data through a refetch error, so
    // `isError` stays false while `isRefetchError` is true — the editor must
    // still not appear on a source we can no longer trust as current.
    mocks.definitions.mockReturnValue({
      data: { definitions_source: 'db' },
      isSuccess: true,
      isError: false,
      isRefetchError: true,
    });

    const view = await render();
    expect(
      [...view.container.querySelectorAll('button')].some((b) =>
        b.textContent?.includes('Save declaration'),
      ),
    ).toBe(false);
    expect(textOf(view.container)).toContain('could not be read');

    await view.unmount();
  });

  it('routes a scanner refusal to the block dialog and overrides with the tokens', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });
    const finding: RefusalFinding = {
      rule_id: 'aws-access-key',
      surface: 'edit',
      locator: 'key.description',
      acknowledgement: 'ack-1',
    };
    let calls = 0;
    mocks.mutate.mockImplementation(
      (
        _input: unknown,
        callbacks: { onSuccess: () => void; onError: (error: Error) => void },
      ) => {
        calls += 1;
        // The first write is refused with the redacted finding; the acknowledged
        // resubmit succeeds.
        if (calls === 1) callbacks.onError(new ApiError(400, 'blocked', undefined, undefined, [finding]));
        else callbacks.onSuccess();
      },
    );

    const view = await render();
    const description = view.container.querySelector<HTMLTextAreaElement>('textarea');
    if (description === null) throw new Error('editor textarea missing');
    await act(async () => setTextarea(description, 'ghp_exampletoken'));
    const save = [...view.container.querySelectorAll('button')].find((b) =>
      b.textContent?.includes('Save declaration'),
    );
    if (save === undefined) throw new Error('save button missing');
    await act(async () => save.click());

    // The block dialog opened, stating the exported-as-public consequence and
    // rendering only the redacted finding.
    const dialog = view.container.querySelector('dialog.scan-block');
    expect(dialog).not.toBeNull();
    const dialogText = dialog?.textContent ?? '';
    expect(dialogText).toContain('exported to Git and treated as public');
    expect(dialogText).toContain('aws-access-key');
    expect(dialogText).toContain('key.description');
    // Never the value the operator typed.
    expect(dialogText).not.toContain('ghp_exampletoken');
    // No blanket ignore-all input exists (ADR §4).
    expect(
      [...(dialog?.querySelectorAll('button') ?? [])].some((b) => /ignore all/i.test(b.textContent ?? '')),
    ).toBe(false);

    // Overriding resubmits the SAME field with the finding's token.
    const acknowledge = [...(dialog?.querySelectorAll('button') ?? [])].find((b) =>
      b.textContent?.includes('Acknowledge and continue'),
    );
    if (acknowledge === undefined) throw new Error('override button missing');
    await act(async () => acknowledge.click());

    expect(mocks.mutate).toHaveBeenLastCalledWith(
      { description: 'ghp_exampletoken', acknowledgements: ['ack-1'] },
      expect.anything(),
    );
    expect(textOf(view.container)).toContain('Saved.');

    await view.unmount();
  });

  it('offers no override in the block dialog when a finding carries no token', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });
    const hardBlock: RefusalFinding = { rule_id: 'high-entropy', surface: 'edit', locator: 'key.description' };
    mocks.mutate.mockImplementation(
      (
        _input: unknown,
        callbacks: { onError: (error: Error) => void },
      ) => callbacks.onError(new ApiError(400, 'blocked', undefined, undefined, [hardBlock])),
    );

    const view = await render();
    const description = view.container.querySelector<HTMLTextAreaElement>('textarea');
    if (description === null) throw new Error('editor textarea missing');
    await act(async () => setTextarea(description, 'some entropy'));
    const save = [...view.container.querySelectorAll('button')].find((b) =>
      b.textContent?.includes('Save declaration'),
    );
    if (save === undefined) throw new Error('save button missing');
    await act(async () => save.click());

    const dialog = view.container.querySelector('dialog.scan-block');
    expect(dialog).not.toBeNull();
    expect(dialog?.textContent ?? '').toContain('cannot be overridden');
    expect(
      [...(dialog?.querySelectorAll('button') ?? [])].some((b) =>
        b.textContent?.includes('Acknowledge and continue'),
      ),
    ).toBe(false);

    await view.unmount();
  });

  it('never opens the scan block on a 404, even one carrying findings (no oracle)', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    dbMode();
    const finding: RefusalFinding = {
      rule_id: 'aws-access-key',
      surface: 'edit',
      locator: 'key.description',
      acknowledgement: 'ack-x',
    };
    // A 404 must be canonicalized BEFORE findings are considered: a 404 carrying
    // findings must render the uniform sentence, never the block dialog.
    mocks.mutate.mockImplementation(
      (_input: unknown, cb: { onError: (error: Error) => void }) =>
        cb.onError(new ApiError(404, 'not found', undefined, undefined, [finding])),
    );

    const view = await render();
    const description = view.container.querySelector<HTMLTextAreaElement>('textarea');
    if (description === null) throw new Error('editor textarea missing');
    await act(async () => setTextarea(description, 'changed'));
    await act(async () => buttonBy(view.container, 'Save declaration').click());

    expect(view.container.querySelector('dialog.scan-block')).toBeNull();
    const text = textOf(view.container);
    expect(text).toContain(KEY_GONE_REFUSAL);
    expect(text).not.toContain('aws-access-key');

    await view.unmount();
  });

  it('renames a key and reports it', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    dbMode();
    mocks.rename.mockImplementation((_input: unknown, cb: { onSuccess: () => void }) =>
      cb.onSuccess(),
    );

    const view = await render();
    const nameInput = [...view.container.querySelectorAll('input.mono')].find(
      (input) => (input as HTMLInputElement).value === 'DATABASE_URL',
    ) as HTMLInputElement | undefined;
    if (nameInput === undefined) throw new Error('name input missing');
    await act(async () => typeInto(nameInput, 'DATABASE_DSN'));
    await act(async () => buttonBy(view.container, 'Rename key').click());

    expect(mocks.rename).toHaveBeenCalledWith({ name: 'DATABASE_DSN' }, expect.anything());
    expect(textOf(view.container)).toContain('Renamed.');

    await view.unmount();
  });

  it('routes a scanner refusal on rename to the block dialog and overrides', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    dbMode();
    const finding: RefusalFinding = {
      rule_id: 'aws-access-key',
      surface: 'edit',
      locator: 'key.name',
      acknowledgement: 'ack-name',
    };
    let calls = 0;
    mocks.rename.mockImplementation(
      (_input: unknown, cb: { onSuccess: () => void; onError: (error: Error) => void }) => {
        calls += 1;
        if (calls === 1) cb.onError(new ApiError(400, 'blocked', undefined, undefined, [finding]));
        else cb.onSuccess();
      },
    );

    const view = await render();
    const nameInput = [...view.container.querySelectorAll('input.mono')].find(
      (input) => (input as HTMLInputElement).value === 'DATABASE_URL',
    ) as HTMLInputElement | undefined;
    if (nameInput === undefined) throw new Error('name input missing');
    await act(async () => typeInto(nameInput, 'AKIAEXAMPLE'));
    await act(async () => buttonBy(view.container, 'Rename key').click());

    const dialog = view.container.querySelector('dialog.scan-block');
    expect(dialog).not.toBeNull();
    expect(dialog?.textContent ?? '').toContain('aws-access-key');
    expect(dialog?.textContent ?? '').not.toContain('AKIAEXAMPLE');

    const acknowledge = [...(dialog?.querySelectorAll('button') ?? [])].find((b) =>
      b.textContent?.includes('Acknowledge and continue'),
    );
    if (acknowledge === undefined) throw new Error('override button missing');
    await act(async () => acknowledge.click());

    expect(mocks.rename).toHaveBeenLastCalledWith(
      { name: 'AKIAEXAMPLE', acknowledgements: ['ack-name'] },
      expect.anything(),
    );
    expect(textOf(view.container)).toContain('Renamed.');

    await view.unmount();
  });

  it('declassifies through a disclosure confirm and renders the Surface-1 warnings', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    dbMode();
    mocks.reclassify.mockImplementation(
      (input: { classification: string }, cb: { onSuccess: (key: unknown) => void }) => {
        expect(input.classification).toBe('config');
        cb.onSuccess({
          ...record,
          classification: 'config',
          findings: [{ rule_id: 'high-entropy', surface: 'value', locator: 'production' }],
        });
      },
    );

    const view = await render({ setEnvironmentIds: ['env_b'], pendingEnvironmentIds: [] });
    await act(async () => buttonBy(view.container, 'Reclassify as config…').click());

    const dialog = view.container.querySelector('dialog.matrix-editor');
    expect(dialog).not.toBeNull();
    const dialogText = dialog?.textContent ?? '';
    // The disclosure consequence and the second-factor requirement are stated.
    expect(dialogText).toContain('readable under ordinary config read');
    expect(dialogText).toContain('second-factor');
    // The impact preview names the affected environment, never a value.
    expect(dialogText).toContain('production');

    await act(async () => dialogButton(view.container, 'Reclassify as config').click());

    expect(mocks.reclassify).toHaveBeenCalledWith(
      { key: 'key_a', classification: 'config' },
      expect.anything(),
    );
    const text = textOf(view.container);
    expect(text).toContain('Reclassified as config.');
    // The re-materialised occurrence's warning is shown redacted.
    expect(text).toContain('high-entropy');

    await view.unmount();
  });

  it('surfaces the reauth requirement when a declassification is refused for assurance', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    dbMode();
    mocks.reclassify.mockImplementation(
      (_input: unknown, cb: { onError: (error: Error) => void }) =>
        cb.onError(new ApiError(403, 'forbidden')),
    );

    const view = await render();
    await act(async () => buttonBy(view.container, 'Reclassify as config…').click());
    await act(async () => dialogButton(view.container, 'Reclassify as config').click());

    expect(textOf(view.container)).toContain('second-factor sign-in');

    await view.unmount();
  });

  it('masks a missing reveal grant as a uniform not-found on declassify', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    dbMode();
    // A 404 that even CARRIES a caller-safe detail must still render the uniform
    // missing-key sentence — a detailed or otherwise distinguishable 404 would be
    // the existence/reveal oracle the gate exists to close.
    mocks.reclassify.mockImplementation(
      (_input: unknown, cb: { onError: (error: Error) => void }) =>
        cb.onError(new ApiError(404, 'not found', 'key requires reveal to declassify')),
    );

    const view = await render();
    await act(async () => buttonBy(view.container, 'Reclassify as config…').click());
    await act(async () => dialogButton(view.container, 'Reclassify as config').click());

    const text = textOf(view.container);
    expect(text).toContain(KEY_GONE_REFUSAL);
    // The gate must not become an oracle: neither the server detail nor any
    // reveal/permission wording may surface on a 404.
    expect(text).not.toContain('requires reveal');
    expect(text.toLowerCase()).not.toContain('reveal');
    expect(text.toLowerCase()).not.toContain('permission');

    await view.unmount();
  });

  it('tightens config to secret with its own consequence, no reveal', async () => {
    const configKey: MatrixKey = { ...record, classification: 'config' };
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: configKey });
    dbMode();
    mocks.reclassify.mockImplementation(
      (input: { classification: string }, cb: { onSuccess: (key: unknown) => void }) => {
        expect(input.classification).toBe('secret');
        cb.onSuccess({ ...configKey, classification: 'secret' });
      },
    );

    const view = await render();
    await act(async () => buttonBy(view.container, 'Reclassify as secret…').click());
    const dialog = view.container.querySelector('dialog.matrix-editor');
    expect(dialog?.textContent ?? '').toContain('dismissals are dropped');
    // Tightening does not disclose, so it names no second-factor requirement.
    expect((dialog?.textContent ?? '').toLowerCase()).not.toContain('second-factor');

    await act(async () => dialogButton(view.container, 'Reclassify as secret').click());
    expect(textOf(view.container)).toContain('Reclassified as secret.');

    await view.unmount();
  });

  it('previews impact and deletes behind a typed confirm, then returns to the matrix', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    dbMode();
    mocks.deleteKey.mockImplementation((_input: unknown, cb: { onSuccess: () => void }) =>
      cb.onSuccess(),
    );

    const view = await render({ setEnvironmentIds: ['env_b'], pendingEnvironmentIds: ['env_a'] });
    const deleteSection = view.container.querySelector('.danger-zone');
    expect(deleteSection).not.toBeNull();
    const text = textOf(view.container);
    // The impact names both the delivering and the drafting environment.
    expect(text).toContain('production');
    expect(text).toContain('development');

    // The delete stays disabled until the exact name is typed.
    const del = buttonBy(view.container, 'Delete key');
    expect(del.disabled).toBe(true);
    const confirmInput = deleteSection?.querySelector('input') as HTMLInputElement;
    await act(async () => typeInto(confirmInput, 'DATABASE_URL'));
    expect(buttonBy(view.container, 'Delete key').disabled).toBe(false);
    await act(async () => buttonBy(view.container, 'Delete key').click());

    expect(mocks.deleteKey).toHaveBeenCalledWith(undefined, expect.anything());
    expect(mocks.navigate).toHaveBeenCalledWith(expect.stringContaining('/matrix'));

    await view.unmount();
  });

  it('fails closed: delete cannot arm while the impact is still loading', async () => {
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: record });
    dbMode();

    const view = await render(noImpact, false);
    expect(textOf(view.container)).toContain('Checking which environments this affects');
    // No expected name while impact is unknown, so the confirm input is disabled.
    const deleteSection = view.container.querySelector('.danger-zone');
    const confirmInput = deleteSection?.querySelector('input') as HTMLInputElement;
    expect(confirmInput.disabled).toBe(true);
    // The reclassify-as-config trigger is also disabled without a ready preview.
    expect(buttonBy(view.container, 'Reclassify as config…').disabled).toBe(true);

    await view.unmount();
  });

  it('renders explicit allow_empty=false and the full JSON schema', async () => {
    const jsonKey: MatrixKey = {
      ...record,
      declaration: {
        rule: { type: 'json', allow_empty: false, json_schema: '{"type":"object"}' },
      },
    };
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: jsonKey });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });

    const view = await render();
    const text = textOf(view.container);
    expect(text).toContain('empty not allowed');
    expect(text).toContain('{"type":"object"}');

    await view.unmount();
  });

  it('renders the #493 editor for an int64-bounded rule without crashing', async () => {
    // The read model infers integer bounds as bigint; the DeclarationEditor's
    // reset signature must tolerate them (a plain JSON.stringify would throw).
    const boundedKey: MatrixKey = {
      ...record,
      declaration: { rule: { type: 'integer', min: 1n, max: 9_007_199_254_740_993n } },
      presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
    };
    mocks.key.mockReturnValue({ isPending: false, isError: false, data: boundedKey });
    mocks.definitions.mockReturnValue({ data: { definitions_source: 'db' }, isSuccess: true, isError: false, isRefetchError: false });

    const view = await render();
    expect(textOf(view.container)).toContain('Edit value rules & presence');

    await view.unmount();
  });
});

/** Write a controlled textarea's value the way React's onChange observes. */
function setTextarea(textarea: HTMLTextAreaElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
  if (setter === undefined) throw new Error('HTMLTextAreaElement exposes no value setter');
  setter.call(textarea, value);
  textarea.dispatchEvent(new Event('input', { bubbles: true }));
}
