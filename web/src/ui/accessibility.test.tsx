// @vitest-environment happy-dom
import { act } from 'react';
import { expect, it } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { Alert } from './Alert.tsx';
import { Checkbox } from './Checkbox.tsx';
import { Glyph } from './Glyph.tsx';
import { Input } from './Input.tsx';
import { Radio } from './Radio.tsx';
import { Select } from './Select.tsx';
import { Textarea } from './Textarea.tsx';

it('associates every composed field with its own label and description', async () => {
  const view = await renderForm(<>
    <Input label="Username" hint="Use your username." />
    <Select label="Environment" hint="Choose the deployment." ><option>Production</option></Select>
    <Textarea label="Description" hint="Explain this project." />
  </>);
  try {
    const labels = [...view.container.querySelectorAll('label')];
    expect(labels.map((label) => label.textContent)).toEqual(['Username', 'Environment', 'Description']);
    expect(new Set(labels.map((label) => label.htmlFor)).size).toBe(3);
    for (const label of labels) {
      const control = document.getElementById(label.htmlFor);
      expect(control).not.toBeNull();
      expect(control?.matches('input,select,textarea')).toBe(true);
      const descriptionId = control?.getAttribute('aria-describedby');
      expect(descriptionId).toBeTruthy();
      expect(document.getElementById(descriptionId ?? '')?.textContent).toBeTruthy();
    }
  } finally { await view.unmount(); }
});

it('only announces refusal assertively while confirmations and facts stay polite', async () => {
  const view = await renderForm(<>
    <Alert>Refused.</Alert>
    <Alert tone="done">Saved.</Alert>
    <Alert tone="warn">Lifetime shortened.</Alert>
    <Alert tone="info">Git managed.</Alert>
  </>);
  try {
    expect([...view.container.querySelectorAll('[role="alert"]')].map((node) => node.textContent)).toEqual(['!Refused.']);
    expect([...view.container.querySelectorAll('[role="status"]')].map((node) => node.textContent)).toEqual(['✓Saved.', '!Lifetime shortened.', 'ℹGit managed.']);
    expect(view.container.querySelectorAll('.alert__glyph[aria-hidden="true"]')).toHaveLength(4);
  } finally { await view.unmount(); }
});

it('hides decorative glyphs and gives named glyphs an image role', async () => {
  const view = await renderForm(<><Glyph name="lock" /><Glyph name="link" label="Linked key" /></>);
  try {
    const [decorative, named] = view.container.querySelectorAll('svg');
    expect(decorative?.getAttribute('aria-hidden')).toBe('true');
    expect(decorative?.hasAttribute('role')).toBe(false);
    expect(named?.getAttribute('role')).toBe('img');
    expect(named?.getAttribute('aria-label')).toBe('Linked key');
    expect(named?.hasAttribute('aria-hidden')).toBe(false);
  } finally { await view.unmount(); }
});

it('activates the checkbox from either its control or its associated label', async () => {
  const view = await renderForm(<Checkbox label="Allow credentials with no expiry" />);
  try {
    const box = view.container.querySelector('input');
    const label = view.container.querySelector('label');
    if (box === null || label === null) throw new Error('Missing checkbox or label');
    expect(label.htmlFor).toBe(box.id);
    expect(box.checked).toBe(false);
    await act(async () => box.click());
    expect(box.checked).toBe(true);
    await act(async () => label.click());
    expect(box.checked).toBe(false);
  } finally { await view.unmount(); }
});

it('selects the radio without changing a separate Docs example group', async () => {
  const view = await renderForm(<>
    <Radio name="first-example" label="Every environment" />
    <Radio name="second-example" label="An independent checked example" defaultChecked />
  </>);
  try {
    const [radio, independent] = view.container.querySelectorAll('input');
    if (radio === undefined || independent === undefined) throw new Error('Missing radios');
    expect(radio.checked).toBe(false);
    await act(async () => radio.click());
    expect(radio.checked).toBe(true);
    expect(independent.checked).toBe(true);
  } finally { await view.unmount(); }
});
