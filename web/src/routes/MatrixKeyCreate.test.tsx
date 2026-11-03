// @vitest-environment happy-dom
import { renderForm } from '../testkit/renderForm.tsx';
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  editDistance,
  MatrixKeyCreate,
  nearMissKeyName,
  type MatrixKeyCreatePayload,
} from './MatrixKeyCreate.tsx';

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
  const { container, unmount } = await renderForm(
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
  return { container, onCreate, unmount };
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

/** The checkbox a label names, resolved through the label's `for`, so the test
    asserts the accessible wiring rather than a class or a wrapper shape. */
function checkboxNamed(container: HTMLElement, name: string): HTMLInputElement {
  const label = [...container.querySelectorAll('label')].find((candidate) =>
    candidate.textContent?.includes(name),
  );
  const control =
    label === undefined ? null : container.ownerDocument.getElementById(label.htmlFor);
  if (!(control instanceof HTMLInputElement)) throw new Error(`no checkbox named ${name}`);
  return control;
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
    const secret = checkboxNamed(view.container, 'secret');
    await act(async () => secret.click());
    // A textarea (a password input flattens newlines), masked by class until
    // the operator opts to see what they type.
    const value = byLabel(view.container, 'matrix-create-value');
    expect(value.tagName).toBe('TEXTAREA');
    expect(value.classList.contains('matrix-editor__value--masked')).toBe(true);
    const show = checkboxNamed(view.container, 'Show while typing');
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

/** A control resolved through its visible label text, as an assistive technology would. */
function byLabelText(container: HTMLElement, text: string): Field {
  const label = [...container.querySelectorAll('label')].find((candidate) => candidate.textContent === text);
  const control = label === undefined ? null : container.ownerDocument.getElementById(label.htmlFor);
  if (!(control instanceof HTMLInputElement || control instanceof HTMLTextAreaElement || control instanceof HTMLSelectElement)) {
    throw new Error(`no control labelled ${text}`);
  }
  return control;
}

function buttonText(container: HTMLElement, text: string): HTMLButtonElement | undefined {
  return [...container.querySelectorAll('button')].find((candidate) => candidate.textContent === text);
}

describe('MatrixKeyCreate JSON Schema', () => {
  async function chooseJson(container: HTMLElement): Promise<void> {
    await act(async () => set(byLabel(container, 'matrix-create-type'), 'json'));
  }

  it('infers a schema from the first value on click only, then submits it', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"host":"db","port":5432}'));

    const schema = byLabelText(view.container, 'Schema document');
    expect(schema.value).toBe('');
    const infer = buttonText(view.container, 'Infer schema from this value');
    if (infer === undefined) throw new Error('no infer button');
    expect(infer.disabled).toBe(false);
    await act(async () => infer.click());

    const expected = JSON.stringify(
      {
        type: 'object',
        properties: { host: { type: 'string' }, port: { type: 'integer' } },
        required: ['host', 'port'],
      },
      null,
      2,
    );
    expect(schema.value).toBe(expected);
    expect(view.container.querySelector('[role="status"]')?.textContent).toBe(
      'Schema filled in from this value. Field names are exported to Git; review it below.',
    );
    expect(buttonText(view.container, 'Replace schema from this value')).toBeDefined();
    // The sample's values never reach the schema.
    expect(schema.value).not.toContain('"db"');
    expect(schema.value).not.toContain('5432');

    await fillNameAndSubmit(view.container, 'DB');
    const payload = view.onCreate.mock.calls[0]![0];
    expect(payload.rule).toEqual({ type: 'json', jsonSchema: expected });
    expect(payload.firstValue?.value).toBe('{"host":"db","port":5432}');
    await view.unmount();
  });

  it('disables inference until the first value is valid JSON', async () => {
    const view = await render();
    await chooseJson(view.container);
    const infer = buttonText(view.container, 'Infer schema from this value');
    expect(infer?.disabled).toBe(true);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{oops'));
    expect(infer?.disabled).toBe(true);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '[1]'));
    expect(infer?.disabled).toBe(false);
    await view.unmount();
  });

  it('disables inference for a secret key and says why', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => checkboxNamed(view.container, 'secret').click());
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"AKIAEXAMPLE":"x"}'));

    expect(buttonText(view.container, 'Infer schema from this value')?.disabled).toBe(true);
    expect(view.container.textContent).toContain(
      'Schema inference is off for secret keys: field names would be exported to Git.',
    );
    expect(byLabelText(view.container, 'Schema document').value).toBe('');
    await view.unmount();
  });

  it('removes an inferred schema when the key is then marked secret, keeping a hand-written one', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"AKIAEXAMPLE":"x"}'));
    await act(async () => buttonText(view.container, 'Infer schema from this value')?.click());
    expect(byLabelText(view.container, 'Schema document').value).toContain('AKIAEXAMPLE');

    await act(async () => checkboxNamed(view.container, 'secret').click());
    expect(byLabelText(view.container, 'Schema document').value).toBe('');
    expect(view.container.textContent).toContain('The schema was removed');
    await view.unmount();
  });

  it('keeps a schema rewritten by hand after inferring, once no sampled name is left in it', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"AKIAEXAMPLE":"x"}'));
    await act(async () => buttonText(view.container, 'Infer schema from this value')?.click());
    await act(async () => set(byLabelText(view.container, 'Schema document'), '{"type":"string"}'));

    await act(async () => checkboxNamed(view.container, 'secret').click());
    expect(byLabelText(view.container, 'Schema document').value).toBe('{"type":"string"}');
    expect(view.container.textContent).not.toContain('The schema was removed');
    await view.unmount();
  });

  it('leaves no sampled name behind a type flip and an identical Replace, once the key is secret', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"AKIAEXAMPLE":"x"}'));
    await act(async () => buttonText(view.container, 'Infer schema from this value')?.click());
    await act(async () => set(byLabelText(view.container, 'Value is'), 'string'));
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '"x"'));
    await act(async () => buttonText(view.container, 'Replace schema from this value')?.click());
    await act(async () => checkboxNamed(view.container, 'secret').click());
    await act(async () => set(byLabelText(view.container, 'Value is'), 'object'));
    expect(byLabelText(view.container, 'Schema document').value).not.toContain('AKIAEXAMPLE');
    expect(view.container.querySelectorAll('.schema-builder__field')).toHaveLength(0);
    await view.unmount();
  });

  it('sees through an escaped spelling of a sampled name', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"AKIAEXAMPLE":"x"}'));
    await act(async () => buttonText(view.container, 'Infer schema from this value')?.click());
    await act(async () =>
      set(
        byLabelText(view.container, 'Schema document'),
        '{"type":"object","properties":{"\\u0041KIAEXAMPLE":{"type":"string"}}}',
      ),
    );
    await act(async () => checkboxNamed(view.container, 'secret').click());
    expect(byLabelText(view.container, 'Schema document').value).toBe('');
    await view.unmount();
  });

  it('holds no refusal for rows the secret tick removed', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"a":1}'));
    await act(async () => buttonText(view.container, 'Infer schema from this value')?.click());
    await act(async () => buttonText(view.container, 'Remove')?.click());
    await act(async () => buttonText(view.container, 'Add field')?.click());
    await act(async () => checkboxNamed(view.container, 'secret').click());
    expect(view.container.querySelectorAll('.schema-builder__field')).toHaveLength(0);
    await fillNameAndSubmit(view.container, 'DB');
    expect(view.onCreate).toHaveBeenCalledTimes(1);
    await view.unmount();
  });

  it('shows the secret reason, not an older note, once the key is secret', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), 'null'));
    await act(async () => buttonText(view.container, 'Infer schema from this value')?.click());
    expect(view.container.textContent).toContain('A null value has no shape');
    await act(async () => checkboxNamed(view.container, 'secret').click());
    expect(view.container.textContent).not.toContain('A null value has no shape');
    expect(view.container.textContent).toContain('Schema inference is off for secret keys');
    await view.unmount();
  });

  it('removes inferred names on the secret tick even while the builder has them hidden', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"AKIAEXAMPLE":"x"}'));
    await act(async () => buttonText(view.container, 'Infer schema from this value')?.click());
    // Any JSON empties the text while the tree keeps the fields for flipping back.
    await act(async () => set(byLabelText(view.container, 'Value is'), 'any'));
    expect(byLabelText(view.container, 'Schema document').value).toBe('');

    await act(async () => checkboxNamed(view.container, 'secret').click());
    await act(async () => set(byLabelText(view.container, 'Value is'), 'object'));
    expect(byLabelText(view.container, 'Schema document').value).not.toContain('AKIAEXAMPLE');
    expect(view.container.querySelectorAll('.schema-builder__field')).toHaveLength(0);
    await view.unmount();
  });

  it('does not lose a blank row silently when Replace infers identical text', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), '{"a":1}'));
    await act(async () => buttonText(view.container, 'Infer schema from this value')?.click());
    await act(async () => buttonText(view.container, 'Add field')?.click());
    expect(view.container.querySelectorAll('.schema-builder__field')).toHaveLength(2);

    await act(async () => buttonText(view.container, 'Replace schema from this value')?.click());
    expect(view.container.querySelectorAll('.schema-builder__field')).toHaveLength(1);
    await view.unmount();
  });

  it('keeps the current schema and explains when a null sample has no shape', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabelText(view.container, 'Schema document'), '{"type":"object"}'));
    await act(async () => set(byLabel(view.container, 'matrix-create-value'), 'null'));
    await act(async () => buttonText(view.container, 'Replace schema from this value')?.click());
    expect(byLabelText(view.container, 'Schema document').value).toBe('{"type":"object"}');
    expect(view.container.textContent).toContain('A null value has no shape');
    await view.unmount();
  });

  it('blocks submit while a field has no name', async () => {
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabelText(view.container, 'Value is'), 'object'));
    await act(async () => buttonText(view.container, 'Add field')?.click());
    // A new blank row is not a mistake yet; saving is what refuses it.
    expect(view.container.textContent).not.toContain('Every field needs a name.');
    await fillNameAndSubmit(view.container, 'DB');

    expect(view.onCreate).not.toHaveBeenCalled();
    expect(alertText(view.container)).toBe('Every field needs a name.');

    await act(async () => set(byLabelText(view.container, 'Field name'), 'host'));
    await act(async () => view.container.querySelector<HTMLButtonElement>('button[type="submit"]')?.click());
    expect(view.onCreate).toHaveBeenCalledTimes(1);
    expect(view.onCreate.mock.calls[0]![0].rule).toEqual({
      type: 'json',
      jsonSchema: JSON.stringify(
        { type: 'object', properties: { host: { type: 'string' } }, required: ['host'] },
        null,
        2,
      ),
    });
    await view.unmount();
  });

  it('submits a hand-written schema the builder cannot show byte-identical', async () => {
    const handWritten = '{"type":"string","pattern":"^a+$"}';
    const view = await render();
    await chooseJson(view.container);
    await act(async () => set(byLabelText(view.container, 'Schema document'), handWritten));
    expect(view.container.textContent).toContain('the builder cannot show');
    expect(buttonText(view.container, 'Add field')).toBeUndefined();

    await fillNameAndSubmit(view.container, 'TOKEN');
    expect(view.onCreate.mock.calls[0]![0].rule).toEqual({ type: 'json', jsonSchema: handWritten });
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
