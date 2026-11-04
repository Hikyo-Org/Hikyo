// @vitest-environment happy-dom
import { act, useState } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { JsonSchemaBuilder } from './JsonSchemaBuilder.tsx';

function Harness({ initial, onChange }: { initial: string; onChange: (text: string, error: string | null) => void }) {
  const [value, setValue] = useState(initial);
  const [held, setHeld] = useState<string | null>(null);
  return (
    <JsonSchemaBuilder
      value={value}
      error={held}
      onChange={(text, error) => {
        setValue(text);
        setHeld(error);
        onChange(text, error);
      }}
    />
  );
}

function control(container: HTMLElement, label: string, index = 0): HTMLInputElement | HTMLSelectElement {
  const found = [...container.querySelectorAll('label')]
    .filter((candidate) => candidate.textContent === label)
    .map((candidate) => container.ownerDocument.getElementById(candidate.htmlFor))
    .filter((element) => element instanceof HTMLInputElement || element instanceof HTMLSelectElement)[index];
  if (found === undefined) throw new Error(`no control ${label}`);
  return found;
}

function write(element: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement, value: string): void {
  const prototype =
    element instanceof HTMLSelectElement
      ? HTMLSelectElement.prototype
      : element instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(prototype, 'value')?.set?.call(element, value);
  element.dispatchEvent(new Event(element instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
}

function addField(container: HTMLElement): void {
  const add = [...container.querySelectorAll('button')].find((button) => button.textContent === 'Add field');
  if (add === undefined) throw new Error('no Add field');
  add.click();
}

describe('JsonSchemaBuilder', () => {
  it('never reports a change on mount, for a schema it can show or not', async () => {
    for (const initial of ['', '{"type":"object"}', '{"type":"string","pattern":"^a"}', '{"type":"integer","minimum":1}']) {
      const onChange = vi.fn();
      const view = await renderForm(<Harness initial={initial} onChange={onChange} />);
      expect(onChange).not.toHaveBeenCalled();
      await view.unmount();
    }
  });

  it('keeps fields when the type flips away and back, and holds the last valid text while a name is blank', async () => {
    const onChange = vi.fn<(text: string, error: string | null) => void>();
    const view = await renderForm(<Harness initial="" onChange={onChange} />);
    await act(async () => write(control(view.container, 'Value is'), 'object'));
    await act(async () => addField(view.container));
    // A new field has no name yet: the refusal rides with the last valid text.
    expect(onChange).toHaveBeenLastCalledWith('{\n  "type": "object"\n}', 'Every field needs a name.');

    await act(async () => write(control(view.container, 'Field name'), 'host'));
    const named = onChange.mock.lastCall;
    expect(named?.[1]).toBeNull();
    expect(JSON.parse(named?.[0] ?? '')).toEqual({ type: 'object', properties: { host: { type: 'string' } }, required: ['host'] });

    await act(async () => write(control(view.container, 'Value is'), 'string'));
    expect(onChange.mock.lastCall?.[0]).toBe('{\n  "type": "string"\n}');
    await act(async () => write(control(view.container, 'Value is'), 'object'));
    expect(JSON.parse(onChange.mock.lastCall?.[0] ?? '')).toEqual({
      type: 'object',
      properties: { host: { type: 'string' } },
      required: ['host'],
    });
    await view.unmount();
  });

  it('keeps the same input while a name is typed, so focus survives each keystroke', async () => {
    const view = await renderForm(<Harness initial='{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}' onChange={vi.fn()} />);
    const before = control(view.container, 'Field name');
    await act(async () => write(before, 'ab'));
    expect(control(view.container, 'Field name')).toBe(before);
    expect(before.value).toBe('ab');
    await view.unmount();
  });

  it('drops a blank row when the parent stops holding its refusal, with the text unchanged', async () => {
    // The parent clears the refusal without changing the text (Replace with an
    // identical inference, a re-seed): the row must not linger to be silently lost.
    const onChange = vi.fn();
    const schema = '{"type":"object"}';
    function Clearing() {
      const [held, setHeld] = useState<string | null>(null);
      return (
        <>
          <JsonSchemaBuilder
            value={schema}
            error={held}
            onChange={(text, error) => {
              setHeld(error);
              onChange(text, error);
            }}
          />
          <button type="button" onClick={() => setHeld(null)}>
            clear
          </button>
        </>
      );
    }
    const view = await renderForm(<Clearing />);
    const button = (name: string) =>
      [...view.container.querySelectorAll('button')].find((candidate) => candidate.textContent === name);
    await act(async () => button('Add field')?.click());
    expect(view.container.querySelectorAll('.schema-builder__field')).toHaveLength(1);
    expect(onChange.mock.lastCall).toEqual([schema, 'Every field needs a name.']);
    await act(async () => button('clear')?.click());
    expect(view.container.querySelectorAll('.schema-builder__field')).toHaveLength(0);
    await view.unmount();
  });

  it('opens a schema with a blank property name as raw JSON, without looping', async () => {
    const view = await renderForm(<Harness initial='{"type":"object","properties":{"":{}}}' onChange={vi.fn()} />);
    expect(view.container.textContent).toContain('the builder cannot show');
    await view.unmount();
  });

  it('updates the builder live below the raw editor, so the text under the cursor never moves', async () => {
    const view = await renderForm(<Harness initial='{"type":"object"}' onChange={vi.fn()} />);
    const raw = view.container.querySelector('textarea');
    if (raw === null) throw new Error('no raw editor');
    await act(async () => raw.focus());
    await act(async () => write(raw, '{"type":"obj'));
    const hint = [...view.container.querySelectorAll('p')].find((p) => p.textContent?.includes('not valid JSON yet'));
    if (hint === undefined) throw new Error('no hint');
    expect(raw.compareDocumentPosition(hint) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(view.container.querySelector('textarea')).toBe(raw);
    await view.unmount();
  });

  it('opens the raw editor when a live update brings a schema the builder cannot show', async () => {
    function LiveUpdate() {
      const [value, setValue] = useState('{"type":"object"}');
      return (
        <>
          <JsonSchemaBuilder value={value} error={null} onChange={() => undefined} />
          <button type="button" onClick={() => setValue('{"type":"integer","minimum":1}')}>
            refetch
          </button>
        </>
      );
    }
    const view = await renderForm(<LiveUpdate />);
    const button = (name: string) =>
      [...view.container.querySelectorAll('button')].find((candidate) => candidate.textContent?.startsWith(name));
    expect(button('Advanced')?.getAttribute('aria-expanded')).toBe('false');
    await act(async () => button('refetch')?.click());
    expect(button('Advanced')?.getAttribute('aria-expanded')).toBe('true');
    await view.unmount();
  });

  it('changes nothing when the raw editor is left, so a click that caused the blur still lands', async () => {
    // A control replaced or moved between mousedown and mouseup loses the click,
    // and the blur that leaves the raw editor is exactly what the mousedown causes.
    const view = await renderForm(<Harness initial='{"type":"object","properties":{"a":{"type":"string"}}}' onChange={vi.fn()} />);
    const raw = view.container.querySelector('textarea');
    if (raw === null) throw new Error('no raw editor');
    await act(async () => raw.focus());
    // Renames a row and adds one above it: the edit that used to shift the layout on blur.
    await act(async () => write(raw, '{"type":"object","properties":{"new":{"type":"integer"},"renamed":{"type":"string"}}}'));
    expect(view.container.querySelectorAll('.schema-builder__field')).toHaveLength(2);
    const remove = [...view.container.querySelectorAll('button')].find((candidate) => candidate.textContent === 'Remove');
    const before = view.container.innerHTML;
    await act(async () => raw.blur());
    expect(view.container.innerHTML).toBe(before);
    expect(remove?.isConnected).toBe(true);
    await view.unmount();
  });

  it('writes string rules and a closed object as typed, and holds an in-progress length without coercing it', async () => {
    const onChange = vi.fn<(text: string, error: string | null) => void>();
    const view = await renderForm(
      <Harness initial='{"type":"object","properties":{"host":{"type":"string"}}}' onChange={onChange} />,
    );
    const text = () => view.container.textContent;
    expect(text()).toContain('Rules for host');
    expect(text()).not.toContain('Rules for host (');
    await act(async () => write(control(view.container, 'Min length'), '3'));
    await act(async () => write(control(view.container, 'Pattern'), '^h'));
    expect(text()).toContain('Rules for host (2)');
    await act(async () => write(control(view.container, 'Max length'), '2'));
    expect(onChange.mock.lastCall?.[1]).toBe('host: Min length cannot be more than max length.');
    expect(view.container.querySelector('.field__error')?.textContent).toBe('Min length cannot be more than max length.');
    await act(async () => write(control(view.container, 'Max length'), '2.'));
    expect(control(view.container, 'Max length').value).toBe('2.');
    expect(onChange.mock.lastCall?.[1]).toBe('host: Max length: Use a whole number of 0 or more.');
    await act(async () => write(control(view.container, 'Max length'), '9'));
    expect(onChange.mock.lastCall?.[1]).toBeNull();
    const tick = control(view.container, 'Reject fields not listed');
    await act(async () => tick.click());
    expect(JSON.parse(onChange.mock.lastCall?.[0] ?? '')).toEqual({
      type: 'object',
      properties: { host: { type: 'string', minLength: 3, maxLength: 9, pattern: '^h' } },
      additionalProperties: false,
    });
    await view.unmount();
  });

  it('opens the rules disclosure when a rule is set, and names the owner in nested controls', async () => {
    const view = await renderForm(
      <Harness
        initial='{"type":"object","properties":{"host":{"type":"string","pattern":"^h"},"zone":{"type":"string"}}}'
        onChange={vi.fn()}
      />,
    );
    const toggles = [...view.container.querySelectorAll('.disclosure__toggle')];
    const open = (label: string) => toggles.find((toggle) => toggle.textContent === label)?.getAttribute('aria-expanded');
    expect(open('Rules for host (1)')).toBe('true');
    expect(open('Rules for zone')).toBe('false');
    expect(view.container.querySelector('[aria-label="Pattern of host"]')).not.toBeNull();
    expect(view.container.querySelector('[aria-label="Min length of zone"]')).not.toBeNull();
    await view.unmount();
  });

  it('moves focus to Add field when a row is removed', async () => {
    const view = await renderForm(
      <Harness initial='{"type":"object","properties":{"a":{"type":"string"}}}' onChange={vi.fn()} />,
    );
    const button = (name: string) =>
      [...view.container.querySelectorAll('button')].find((candidate) => candidate.textContent === name);
    await act(async () => button('Remove')?.click());
    expect(view.container.ownerDocument.activeElement).toBe(button('Add field'));
    await view.unmount();
  });
});
