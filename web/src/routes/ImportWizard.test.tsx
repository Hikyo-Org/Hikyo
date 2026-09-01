// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';

const listOccurrences = vi.fn();
const createKey = vi.fn();
const importValues = vi.fn();

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

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const environments = [
  { id: 'env-dev', name: 'development' },
  { id: 'env-prod', name: 'production' },
];

// Phase 1a candidates carry the default `secret` intent; 1b carries `config`.
// The mock uses that to answer 1a with the new key still undeclared and 1b with
// every declaration landed — the exact transition the wizard depends on.
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
});

afterEach(() => {
  document.body.innerHTML = '';
});

async function render(gitManaged = false) {
  const onClose = vi.fn();
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      <ImportWizard
        matrixRef={{ org: 'acme', project: 'app' }}
        environments={environments}
        gitManaged={gitManaged}
        onClose={onClose}
      />,
    );
  });
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

// Walk source → classify → review → result, stopping before the final Import.
async function reachReview(container: HTMLElement): Promise<void> {
  await selectFile(container, FILE);
  await click(button(container, 'Review'));
  await click(button(container, 'Review changes'));
}

describe('ImportWizard success', () => {
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
    // env-dev tokens come from the 1b read, so they name the post-declaration state.
    expect(call.precondition.occurrences).toContainEqual({
      key: 'NEW',
      environment_id: 'env-dev',
      token: 'tok-NEW-env-dev',
    });
  });
});

describe('ImportWizard invalid file', () => {
  it('blocks Review while any line is invalid (all-or-nothing, matching the server)', async () => {
    const { container } = await render();
    // `lower=1` fails the strict upper-snake grammar the Go parser refuses on.
    await selectFile(container, 'GOOD=1\nlower=2\n');
    expect(container.textContent).toContain('1 invalid line');
    expect(container.querySelector('[aria-label="Invalid lines"]')?.textContent).toContain(
      'Line 2',
    );
    expect(button(container, 'Review').disabled).toBe(true);
    expect(listOccurrences).not.toHaveBeenCalled();
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

describe('ImportWizard git-managed', () => {
  it('skips new-key declaration and imports only declared keys', async () => {
    importValues.mockResolvedValue({ imported: ['EXISTING'], skipped: [] });
    const { container } = await render(true);
    await selectFile(container, FILE);
    await click(button(container, 'Review'));
    // The git-managed block names the skipped new keys on the classify step.
    expect(container.textContent).toContain('cannot be declared here');
    await click(button(container, 'Review changes'));
    await click(button(container, 'Import'));
    expect(createKey).not.toHaveBeenCalled();
    const sent = importValues.mock.calls[0]?.[0];
    expect(sent.entries.map((entry: { key: string }) => entry.key)).toEqual(['EXISTING']);
  });
});
