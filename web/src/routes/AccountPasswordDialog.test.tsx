// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { AccountPasswordDialog } from './AccountPasswordDialog.tsx';

let unmount: (() => Promise<void>) | undefined;
afterEach(async () => { await unmount?.(); });

it('associates a submitted mismatch with both password fields and clears it on correction', async () => {
  const submit = vi.fn();
  const view = await renderForm(<AccountPasswordDialog first proof="" pending={false} failure={null} onCancel={vi.fn()} onSubmit={submit} />);
  unmount = view.unmount;
  const password = view.container.querySelector<HTMLInputElement>('input[name="password"]');
  const repeat = view.container.querySelector<HTMLInputElement>('input[name="repeat-password"]');
  const form = view.container.querySelector('form');
  if (password === null || repeat === null || form === null) throw new Error('Missing password form');
  expect(password.hasAttribute('aria-invalid')).toBe(false);
  expect(repeat.hasAttribute('aria-invalid')).toBe(false);
  await act(async () => {
    typeInto(password, 'a-new-long-password');
    typeInto(repeat, 'a-different-long-password');
  });
  await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
  expect(submit).not.toHaveBeenCalled();
  for (const input of [password, repeat]) {
    expect(input.getAttribute('aria-invalid')).toBe('true');
    const descriptions = (input.getAttribute('aria-describedby') ?? '').split(' ').map((id) => document.getElementById(id)?.textContent).join(' ');
    expect(descriptions).toContain('The passwords do not match. Enter them again.');
  }
  await act(async () => typeInto(repeat, 'a-new-long-password'));
  expect(password.hasAttribute('aria-invalid')).toBe(false);
  expect(repeat.hasAttribute('aria-invalid')).toBe(false);
  expect(view.container.querySelectorAll('.field__error')).toHaveLength(0);
  await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
  expect(submit).toHaveBeenCalledExactlyOnceWith('a-new-long-password', '');
});

it('keeps a server failure at form level without marking either password invalid', async () => {
  const view = await renderForm(<AccountPasswordDialog first={false} proof="proof" pending={false} failure="The change was refused." onCancel={vi.fn()} onSubmit={vi.fn()} />);
  unmount = view.unmount;
  expect(view.container.querySelector('.alert')?.textContent).toContain('The change was refused.');
  expect(view.container.querySelector('[aria-invalid="true"]')).toBeNull();
});
