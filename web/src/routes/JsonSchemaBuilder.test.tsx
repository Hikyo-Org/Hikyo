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
    for (const initial of ['', '{"type":"object"}', '{"type":"string","pattern":"^a"}']) {
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

  it('holds the view while typing in the raw editor and rebuilds on leaving it', async () => {
    const view = await renderForm(<Harness initial='{"type":"object"}' onChange={vi.fn()} />);
    const raw = view.container.querySelector('textarea');
    if (raw === null) throw new Error('no raw editor');
    await act(async () => raw.focus());
    await act(async () => write(raw, '{"type":"obj'));
    expect(view.container.textContent).not.toContain('not valid JSON yet');
    await act(async () => raw.blur());
    expect(view.container.textContent).toContain('not valid JSON yet');
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
