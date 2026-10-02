// @vitest-environment happy-dom
import { renderForm } from '../testkit/renderForm.tsx';
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { MAX_FILE_BYTES } from './import-sources.ts';

const cleanups: Array<() => Promise<void>> = [];
const listOccurrences = vi.fn();
const createKey = vi.fn();
const importValues = vi.fn();
const publishPreflight = vi.hoisted(() => vi.fn());
vi.mock('./Ceremony.tsx', () => ({
  Ceremony: ({ request, onAuthorised, onCancel }: {
    request: import('./Ceremony.tsx').CeremonyRequest; onAuthorised: () => void; onCancel: () => void;
  }) => <section aria-label="Publish ceremony"><p>{`${request.purpose}:${request.environmentName}:${request.keys.map((key) => key.name).join(',')}`}</p>
    <button onClick={onAuthorised}>Authorise import</button><button onClick={onCancel}>Cancel ceremony</button></section>,
}));
vi.mock('../api/values.ts', async (original) => ({
  ...(await original<typeof import('../api/values.ts')>()),
  fetchRevealWindow: publishPreflight,
}));

vi.mock('../api/matrix.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/matrix.ts')>();
  return {
    ...actual,
    useListValueOccurrences: () => ({ mutateAsync: listOccurrences, isPending: false }),
    useCreateKey: () => ({ mutateAsync: createKey, isPending: false }),
    useImportValues: () => ({ mutateAsync: importValues, isPending: false }),
  };
});

const { ImportWizard } = await import('./ImportWizard.tsx');

const environments = [
  { id: 'env-dev', name: 'development' },
  { id: 'env-prod', name: 'production' },
];

// Phase 1a candidates carry the default `secret` intent; the final review read
// carries the selected intent. The mock uses that to distinguish discovery from
// the tokens the operator actually reviews.
function occurrenceList(input: {
  environment: string;
  candidates: readonly { name: string; classification: string }[];
}) {
  const isBindingRead = input.candidates[0]?.classification === 'config';
  return {
    environment_id: input.environment,
    definitions_revision: 3n,
    items: input.candidates.map((candidate) => ({
      name: candidate.name,
      declared: candidate.name === 'EXISTING' ? true : isBindingRead,
      set: candidate.name === 'EXISTING' && input.environment === 'env-prod',
      token: `tok-${candidate.name}-${input.environment}`,
    })),
  };
}

beforeEach(() => {
  listOccurrences.mockReset().mockImplementation(async (input) => occurrenceList(input));
  createKey.mockReset().mockResolvedValue({ id: 'key-new' });
  importValues.mockReset().mockResolvedValue({ imported: ['EXISTING', 'NEW'], skipped: [] });
  publishPreflight.mockReset().mockResolvedValue({ protected: false, live: false });
  vi.stubGlobal('fetch', vi.fn(() => {
    const names = [...new Set(listOccurrences.mock.calls.flatMap(([input]) => input.candidates.map((candidate: { name: string }) => candidate.name)))];
    return Promise.resolve(Response.json({ count: names.length, schema_revision: 3, items: names.map((name, index) => ({
      id: `key_123e4567-e89b-12d3-a456-4266141740${String(10 + index)}`,
      org_id: 'org_123e4567-e89b-12d3-a456-426614174001', project_id: 'prj_123e4567-e89b-12d3-a456-426614174002',
      name, folder_path: '', classification: 'secret', description: '', deprecated: false, deprecation_note: '',
      declaration: { rule: { type: 'string', allow_empty: true } },
      presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
      group_id: '', created_at: '2026-01-01T00:00:00Z',
    })) }));
  }));
});

afterEach(async () => {
  for (const unmount of cleanups.splice(0)) await unmount();
  document.body.innerHTML = '';
  vi.unstubAllGlobals();
});

async function render(gitManaged = false, canDeclareKeys = !gitManaged) {
  const onClose = vi.fn();
  const { container, unmount } = await renderForm(
    <ImportWizard
      matrixRef={{ org: 'acme', project: 'app' }}
      environments={environments}
      gitManaged={gitManaged}
      canDeclareKeys={canDeclareKeys}
      onClose={onClose}
    />,
  );
  cleanups.push(unmount);
  return { container, onClose };
}

async function settle(): Promise<void> {
  for (let round = 0; round < 12; round += 1) {
    await act(async () => {
      await Promise.resolve();
    });
  }
  await act(async () => {
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
  });
}

function button(container: HTMLElement, name: string): HTMLButtonElement {
  const found = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent?.trim() === name,
  );
  if (found === undefined) {
    throw new Error(`no button labelled ${name}`);
  }
  return found;
}

async function click(element: HTMLElement): Promise<void> {
  await act(async () => {
    element.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await settle();
}

async function selectFile(container: HTMLElement, content: string): Promise<void> {
  const input = container.querySelector<HTMLInputElement>('input[type="file"]');
  if (input === null) {
    throw new Error('no file input');
  }
  const file = new File([content], 'app.env', { type: 'text/plain' });
  Object.defineProperty(input, 'files', { value: [file], configurable: true });
  await act(async () => {
    input.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await settle();
}

const FILE = 'EXISTING=1\nNEW=hello\n';

// The wizard opens on the source picker (#496); the `.env` journey is one row.
async function pickDotenv(container: HTMLElement): Promise<void> {
  const source = [...container.querySelectorAll('button')].find((candidate) =>
    candidate.textContent?.includes('.env file'),
  );
  if (source === undefined) {
    throw new Error('no .env source in the picker');
  }
  await click(source);
}

// Walk pick → source → classify → review, stopping before the final Import.
async function reachReview(container: HTMLElement): Promise<void> {
  await pickDotenv(container);
  await selectFile(container, FILE);
  await click(button(container, 'Review'));
  await click(button(container, 'Review changes'));
}

describe('ImportWizard success', () => {
  it('authorises exactly the written keys before publishing into each protected destination', async () => {
    publishPreflight.mockResolvedValue({ protected: true, live: false });
    const { container } = await render();
    await reachReview(container);
    await click(button(container, 'Import'));
    expect(importValues).not.toHaveBeenCalled();
    expect(container.textContent).toContain('publish:development:EXISTING,NEW');
    await click(button(container, 'Authorise import'));
    expect(importValues).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain('publish:production:NEW');
    await click(button(container, 'Authorise import'));
    expect(importValues).toHaveBeenCalledTimes(2);
  });

  it('stops the entire remaining batch when the protected publish ceremony is canceled', async () => {
    publishPreflight.mockResolvedValue({ protected: true, live: false });
    const { container } = await render();
    await reachReview(container);
    await click(button(container, 'Import'));
    expect(button(container, 'Cancel').disabled).toBe(true);
    await click(button(container, 'Cancel ceremony'));
    expect(importValues).not.toHaveBeenCalled();
    expect(publishPreflight).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain('Declared 1 new key');
    expect(container.textContent).toContain('No further values were sent');
  });

  it('does not create another declaration after the workflow is unmounted', async () => {
    let finish: (result: { id: string }) => void = () => {};
    createKey.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    const { container } = await render();
    await pickDotenv(container);
    await selectFile(container, 'FIRST=value\nSECOND=value\n');
    await click(button(container, 'Review'));
    await click(button(container, 'Review changes'));
    await click(button(container, 'Import'));
    expect(createKey).toHaveBeenCalledTimes(1);
    expect(button(container, 'Cancel').disabled).toBe(true);
    await cleanups.pop()?.();
    await act(async () => finish({ id: 'key-first' }));
    await settle();
    expect(createKey).toHaveBeenCalledTimes(1);
    expect(importValues).not.toHaveBeenCalled();
  });
  it('declares the new key, imports every environment, and reports what landed', async () => {
    const { container } = await render();
    await reachReview(container);
    await click(button(container, 'Import'));

    expect(createKey).toHaveBeenCalledTimes(1);
    expect(createKey.mock.calls[0]?.[0]).toMatchObject({
      name: 'NEW',
      classification: 'secret',
      rule: { type: 'string' },
      required: { mode: 'none' },
      forbidden: { mode: 'none' },
    });
    expect(importValues).toHaveBeenCalledTimes(2);
    const first = importValues.mock.calls[0]?.[0];
    expect(first.precondition.environment_ids).toEqual(['env-dev']);
    expect(first.precondition.definitions_revision).toBe(3);
    expect(first.precondition.occurrences).toHaveLength(2);
    expect(container.textContent).toContain('Declared 1 new key: NEW');
    expect(container.textContent).toContain('imported 2');
  });

  it('sends the token from the binding (config-intent) phase-1 read', async () => {
    const { container } = await render();
    await reachReview(container);
    await click(button(container, 'Import'));
    const call = importValues.mock.calls[0]?.[0];
    // env-dev tokens come from the final review read and are preserved while
    // declarations land.
    expect(call.precondition.occurrences).toContainEqual({
      key: 'NEW',
      environment_id: 'env-dev',
      token: 'tok-NEW-env-dev',
    });
    expect(listOccurrences).toHaveBeenCalledTimes(4);
  });

  it('does not rebind occurrence tokens after final review', async () => {
    const { container } = await render();
    await reachReview(container);
    const readsAtReview = listOccurrences.mock.calls.length;
    await click(button(container, 'Import'));
    expect(listOccurrences).toHaveBeenCalledTimes(readsAtReview);
  });
});

describe('ImportWizard invalid file', () => {
  it('blocks Review while any line is invalid (all-or-nothing, matching the server)', async () => {
    const { container } = await render();
    await pickDotenv(container);
    // `lower=1` fails the strict upper-snake grammar the Go parser refuses on.
    await selectFile(container, 'GOOD=1\nlower=2\n');
    expect(container.textContent).toContain('1 invalid line');
    expect(container.querySelector('[aria-label="Invalid lines"]')?.textContent).toContain(
      'Line 2',
    );
    expect(button(container, 'Review').disabled).toBe(true);
    expect(listOccurrences).not.toHaveBeenCalled();
  });

  it('clears a prior valid parse when an oversized file is then selected', async () => {
    const { container } = await render();
    await pickDotenv(container);
    await selectFile(container, 'GOOD=1\n');
    expect(button(container, 'Review').disabled).toBe(false);

    // Selecting an oversized file must invalidate the earlier importable parse,
    // not merely show an error while Review stays enabled.
    const input = container.querySelector<HTMLInputElement>('input[type="file"]');
    if (input === null) throw new Error('no file input');
    const big = new File(['x'], 'big.env', { type: 'text/plain' });
    Object.defineProperty(big, 'size', { value: MAX_FILE_BYTES + 1, configurable: true });
    Object.defineProperty(input, 'files', { value: [big], configurable: true });
    await act(async () => {
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await settle();

    expect(container.textContent).toContain('exceeds');
    expect(button(container, 'Review').disabled).toBe(true);
  });
});

describe('ImportWizard refusals', () => {
  it('surfaces an authorization refusal per environment', async () => {
    importValues.mockRejectedValue(new ApiError(403, 'forbidden'));
    const { container } = await render();
    await reachReview(container);
    await click(button(container, 'Import'));
    expect(container.textContent).toContain('do not have permission to import');
  });

  it('quotes a validation refusal detail verbatim', async () => {
    importValues.mockRejectedValue(new ApiError(400, 'bad request', 'INVALID is not declared'));
    const { container } = await render();
    await reachReview(container);
    await click(button(container, 'Import'));
    expect(container.textContent).toContain('INVALID is not declared');
  });

  it('explains a stale-state 409 with a re-review recovery', async () => {
    importValues.mockRejectedValue(new ApiError(409, 'conflict'));
    const { container } = await render();
    await reachReview(container);
    await click(button(container, 'Import'));
    expect(container.textContent).toContain('moved before this import ran');
  });
});

describe('ImportWizard without permission to declare', () => {
  it('skips new-key declaration, says why, and never blames Git', async () => {
    importValues.mockResolvedValue({ imported: ['EXISTING'], skipped: [] });
    const { container } = await render(false, false);
    await pickDotenv(container);
    await selectFile(container, FILE);
    await click(button(container, 'Review'));
    expect(container.textContent).toContain('cannot be declared here');
    expect(container.textContent).toContain(
      'You do not have permission to declare keys in this project.',
    );
    expect(container.textContent).not.toContain('managed in Git');
    await click(button(container, 'Review changes'));
    await click(button(container, 'Import'));
    expect(createKey).not.toHaveBeenCalled();
    const sent = importValues.mock.calls[0]?.[0];
    expect(sent.entries.map((entry: { key: string }) => entry.key)).toEqual(['EXISTING']);
  });
});

describe('ImportWizard git-managed', () => {
  it('skips new-key declaration and imports only declared keys', async () => {
    importValues.mockResolvedValue({ imported: ['EXISTING'], skipped: [] });
    const { container } = await render(true);
    await pickDotenv(container);
    await selectFile(container, FILE);
    await click(button(container, 'Review'));
    // The git-managed notice is its own alert; the dropped-keys line names the
    // skipped keys and the recovery.
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('managed in Git');
    expect(container.textContent).toContain('cannot be declared here');
    expect(container.textContent).toContain(
      'Declare the missing keys with definitions plan / definitions apply, then import again.',
    );
    expect(container.querySelector('.matrix-editor__eyebrow')?.textContent).toBe('Import · Step 3 of 5');
    await click(button(container, 'Review changes'));
    await click(button(container, 'Import'));
    expect(createKey).not.toHaveBeenCalled();
    const sent = importValues.mock.calls[0]?.[0];
    expect(sent.entries.map((entry: { key: string }) => entry.key)).toEqual(['EXISTING']);
  });
});
