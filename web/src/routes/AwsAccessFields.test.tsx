// @vitest-environment happy-dom
import { act, useState } from 'react';
import { expect, it } from 'vitest';

import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { AwsAccessFields, emptyAwsAccess, type AwsAccess } from './AwsAccessFields.tsx';
import storyMeta from './AwsAccessFields.stories.tsx';

function ControlledFields() {
  const [value, setValue] = useState<AwsAccess>({ ...emptyAwsAccess, mode: 'static' });
  return <AwsAccessFields value={value} onChange={setValue} />;
}

function changeMode(select: HTMLSelectElement, value: string) {
  select.value = value;
  select.dispatchEvent(new Event('change', { bubbles: true }));
}

it('drops the typed key when leaving static mode and returning to it', async () => {
  const view = await renderForm(<ControlledFields />);
  try {
    const key = view.container.querySelector('input[type="password"]');
    const mode = view.container.querySelector('select');
    if (!(key instanceof HTMLInputElement) || mode === null) throw new Error('Missing static access controls');
    await act(async () => typeInto(key, 'storybook-inert-key'));
    expect(key.value).toBe('storybook-inert-key');
    await act(async () => changeMode(mode, 'ambient'));
    expect(view.container.querySelector('input[type="password"]')).toBeNull();
    await act(async () => changeMode(mode, 'static'));
    const returned = view.container.querySelector('input[type="password"]');
    expect(returned).toBeInstanceOf(HTMLInputElement);
    if (!(returned instanceof HTMLInputElement)) throw new Error('Missing returned static key field');
    expect(returned.value).toBe('');
  } finally { await view.unmount(); }
});

it('updates the story fields when its live initial-mode control changes', async () => {
  const view = await renderForm(storyMeta.render({ mode: 'static' }));
  try {
    expect(view.container.querySelector('input[type="password"]')).not.toBeNull();
    await view.rerender(storyMeta.render({ mode: 'ambient' }));
    expect(view.container.querySelector('input[type="password"]')).toBeNull();
    expect(view.container.querySelector('select')?.value).toBe('ambient');
    await view.rerender(storyMeta.render({ mode: 'web-identity' }));
    expect(view.container.textContent).toContain('Role ARN');
    expect(view.container.textContent).not.toContain('External id');
  } finally { await view.unmount(); }
});
