// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { editDistance, MatrixKeyCreate, nearMissKeyName, type MatrixKeyCreatePayload } from './MatrixKeyCreate.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

type Props = Parameters<typeof MatrixKeyCreate>[0];
type Environment = Props['environments'][number];

const development: Environment = {
  id: 'env_01989abc-def0-7123-8123-000000000001',
  org_id: 'org_01989abc-def0-7123-8123-000000000000',
  project_id: 'prj_01989abc-def0-7123-8123-000000000000',
  name: 'development',
  display_order: 0,
  created_at: '2026-08-23T08:00:00Z',
};
const production: Environment = { ...development, id: 'env_01989abc-def0-7123-8123-000000000002', name: 'production', display_order: 1 };
const environments: readonly Environment[] = [development, production];

afterEach(() => {
  document.body.innerHTML = '';
});

async function render(overrides: Partial<Props> = {}) {
  const onCreate = vi.fn<(payload: MatrixKeyCreatePayload) => Promise<void>>().mockResolvedValue(undefined);
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      <MatrixKeyCreate
        folders={['app']}
        environments={environments}
        protectedEnvironmentIds={[production.id]}
        initialFolder="app"
        existingKeyNames={['EXISTING']}
        busy={false}
        mutationError={null}
        onClose={vi.fn()}
        onCreate={onCreate}
        {...overrides}
      />,
    );
  });
  return { container, onCreate, unmount: () => act(async () => root.unmount()) };
}

function set(element: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement, value: string): void {
  const prototype =
    element instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : element instanceof HTMLSelectElement
        ? HTMLSelectElement.prototype
        : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(prototype, 'value')?.set;
  if (setter === undefined) throw new Error('no value setter');
  setter.call(element, value);
  element.dispatchEvent(new Event('input', { bubbles: true }));
  element.dispatchEvent(new Event('change', { bubbles: true }));
}

type Field = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement;

function byLabel(container: HTMLElement, id: string): Field {
  const element = container.querySelector<Field>(`#${id}`);
  if (element === null) throw new Error(`no field ${id}`);
  return element;
}

async function fillNameAndSubmit(container: HTMLElement, name: string): Promise<void> {
  await act(async () => set(byLabel(container, 'matrix-create-name'), name));
  const submit = container.querySelector<HTMLButtonElement>('button[type="submit"]');
  if (submit === null) throw new Error('no submit');
  await act(async () => submit.click());
}

function radio(container: HTMLElement, name: string, value: string): HTMLInputElement {
  const element = container.querySelector<HTMLInputElement>(
    `input[name="${name}"][value="${value}"]`,
  );
  if (element === null) throw new Error(`no radio ${name}=${value}`);
  return element;
}

function alertText(container: HTMLElement): string | null {
  return container.querySelector('.alert')?.textContent ?? null;
}

describe('MatrixKeyCreate', () => {
  it('sends symbolic all presence as a mode, never derived from the environment set', async () => {
    const view = await render();
    await act(async () => radio(view.container, 'matrix-create-required', 'all').click());
    await fillNameAndSubmit(view.container, 'LOG_LEVEL');

    expect(view.onCreate).toHaveBeenCalledTimes(1);
    const payload = view.onCreate.mock.calls[0]![0];
    expect(payload.required).toEqual({ mode: 'all', environmentIds: [] });
    expect(payload.forbidden).toEqual({ mode: 'none', environmentIds: [] });
    expect(payload.classification).toBe('config');
    expect(payload.rule).toEqual({ type: 'string' });
    await view.unmount();
  });

  it('carries an explicit required set as the chosen environments', async () => {
    const view = await render();
    await act(async () => radio(view.container, 'matrix-create-required', 'explicit').click());
    const checkbox = view.container.querySelector<HTMLInputElement>(
      `.matrix-key-create__presence input[type="checkbox"]`,
    );
    if (checkbox === null) throw new Error('no explicit env checkbox');
    await act(async () => checkbox.click());
    await fillNameAndSubmit(view.container, 'DATABASE_URL');

    const payload = view.onCreate.mock.calls[0]![0];
    expect(payload.required.mode).toBe('explicit');
    expect(payload.required.environmentIds).toEqual([development.id]);
    await view.unmount();
  });

  it('builds an enum rule and rejects a first value outside its members', async () => {
    const view = await render();
    await act(async () => set(byLabel(view.container, 'matrix-create-type'), 'enum'));
    const members = view.container.querySelector<HTMLTextAreaElement>(
      '.matrix-key-create__constraints textarea',
    );
    if (members === null) throw new Error('no members field');
    await act(async () => set(members, 'debug\ninfo\nwarn'));
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), 'verbose'));
    await fillNameAndSubmit(view.container, 'LEVEL');

    expect(view.onCreate).not.toHaveBeenCalled();
    // The error must NOT echo the members back (a member could be sensitive).
    expect(alertText(view.container)).toContain('declared enum members');
    expect(alertText(view.container)).not.toContain('debug, info, warn');

    // A member value passes and rides through as the rule's members.
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), 'info'));
    await fillNameAndSubmit(view.container, 'LEVEL');
    const payload = view.onCreate.mock.calls[0]![0];
    expect(payload.rule).toEqual({ type: 'enum', members: ['debug', 'info', 'warn'] });
    expect(payload.firstValue).toEqual({ value: 'info', environmentIds: [development.id] });
    await view.unmount();
  });

  it('rejects an integer minimum above its maximum without calling onCreate', async () => {
    const view = await render();
    await act(async () => set(byLabel(view.container, 'matrix-create-type'), 'integer'));
    const [min, max] = [...view.container.querySelectorAll<HTMLInputElement>('.matrix-key-create__constraints input[type="number"]')];
    await act(async () => set(min!, '10'));
    await act(async () => set(max!, '3'));
    await fillNameAndSubmit(view.container, 'PORT');

    expect(view.onCreate).not.toHaveBeenCalled();
    expect(alertText(view.container)).toContain('Minimum cannot exceed maximum');
    await view.unmount();
  });

  it('refuses a required-all key that also forbids somewhere', async () => {
    const view = await render();
    await act(async () => radio(view.container, 'matrix-create-required', 'all').click());
    await act(async () => radio(view.container, 'matrix-create-forbidden', 'all').click());
    await fillNameAndSubmit(view.container, 'FLAG');

    expect(view.onCreate).not.toHaveBeenCalled();
    expect(alertText(view.container)).toContain('leaves none to forbid');
    await view.unmount();
  });

  it('refuses an integer bound that a JS number cannot hold exactly', async () => {
    const view = await render();
    await act(async () => set(byLabel(view.container, 'matrix-create-type'), 'integer'));
    const [min] = [...view.container.querySelectorAll<HTMLInputElement>('.matrix-key-create__constraints input[type="number"]')];
    // Beyond Number.MAX_SAFE_INTEGER, parseInt would round it silently.
    await act(async () => set(min!, '9223372036854775807'));
    await fillNameAndSubmit(view.container, 'BIG');

    expect(view.onCreate).not.toHaveBeenCalled();
    expect(alertText(view.container)).toContain('exact precision');
    await view.unmount();
  });

  it('marks a secret declaration and hides the first value field', async () => {
    const view = await render();
    const secret = view.container.querySelector<HTMLInputElement>('.matrix-key-create__secret input');
    if (secret === null) throw new Error('no secret checkbox');
    await act(async () => secret.click());
    // A textarea (a password input flattens newlines), masked by class until
    // the operator opts to see what they type.
    const value = byLabel(view.container, 'matrix-create-value');
    expect(value.tagName).toBe('TEXTAREA');
    expect(value.classList.contains('matrix-editor__value--masked')).toBe(true);
    const show = [...view.container.querySelectorAll('label')].find((label) =>
      label.textContent?.includes('Show while typing'),
    )?.querySelector('input');
    if (show === null || show === undefined) throw new Error('no show-while-typing toggle');
    await act(async () => show.click());
    expect(value.classList.contains('matrix-editor__value--masked')).toBe(false);
    await fillNameAndSubmit(view.container, 'API_KEY');
    expect(view.onCreate.mock.calls[0]![0].classification).toBe('secret');
    await view.unmount();
  });

  it('keeps the typed name and previews the normalised declaration', async () => {
    const view = await render();
    const nameField = byLabel(view.container, 'matrix-create-name');
    await act(async () => set(nameField, 'log level'));
    expect(nameField.value).toBe('log level');
    const preview = view.container.querySelector('#matrix-create-name-preview');
    expect(preview?.textContent).toBe('Will be declared as LOG_LEVEL in folder app/');
    expect(preview?.hasAttribute('role')).toBe(false);
    expect(nameField.getAttribute('aria-describedby')).toBe('matrix-create-name-preview');
    await view.unmount();
  });

  it('warns about a near-miss of an existing key without blocking', async () => {
    const view = await render({ existingKeyNames: ['DATABASE_URL'] });
    await act(async () => set(byLabel(view.container, 'matrix-create-name'), 'DATABSE_URL'));
    expect(view.container.textContent).toContain(
      'Similar to existing key DATABASE_URL. Continue if this is intentional.',
    );
    await fillNameAndSubmit(view.container, 'DATABSE_URL');
    expect(view.onCreate).toHaveBeenCalledTimes(1);
    await view.unmount();
  });

  it('normalises surrounding whitespace off the first value and says so', async () => {
    const view = await render();
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '  info\u00a0'));
    expect(view.container.textContent).toContain('Leading and trailing whitespace was removed.');
    await fillNameAndSubmit(view.container, 'LEVEL');
    expect(view.onCreate.mock.calls[0]![0].firstValue?.value).toBe('info');
    await view.unmount();
  });

  it('renders the Git notice and refuses to declare when git-managed', async () => {
    const view = await render({ gitManaged: true });
    expect(view.container.textContent).toContain('managed in Git');
    await fillNameAndSubmit(view.container, 'LEVEL');
    expect(view.onCreate).not.toHaveBeenCalled();
    await view.unmount();
  });
});

describe('editDistance / nearMissKeyName', () => {
  it('counts a transposition as one edit and ignores exact or distant names', () => {
    expect(editDistance('DATABASE_URL', 'DATABSE_URL')).toBe(1);
    expect(editDistance('ab', 'ba')).toBe(1);
    expect(editDistance('kitten', 'sitting')).toBe(3);
    expect(nearMissKeyName('DATABASE_URL', ['DATABASE_URL'])).toBeNull();
    expect(nearMissKeyName('DATABASE_URI', ['DATABASE_URL', 'LOG_LEVEL'])).toBe('DATABASE_URL');
    expect(nearMissKeyName('LOG', ['LOG_LEVEL'])).toBeNull();
    expect(nearMissKeyName('', ['A'])).toBeNull();
  });
});
