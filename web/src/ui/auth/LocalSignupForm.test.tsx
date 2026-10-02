// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { renderForm, settleTask, typeInto } from '../../testkit/renderForm.tsx';
import { LocalSignupForm } from './LocalSignupForm.tsx';

const request = vi.hoisted(() => vi.fn());
vi.mock('../../api/signup.ts', async (original) => ({ ...await original(), requestSignup: request }));
afterEach(() => vi.clearAllMocks());
it('requests and resends at the addressed org without claiming delivery or account existence', async () => {
  request.mockResolvedValue(undefined);
  const view = await renderForm(<LocalSignupForm landing="You’ll join this organisation." org="org_acme" onBack={vi.fn()} />);
  try {
    const email = view.container.querySelector('input');
    if (!(email instanceof HTMLInputElement)) throw new Error('missing email');
    await act(async () => typeInto(email, 'alex@example.com'));
    await act(async () => view.container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await settleTask();
    expect(request).toHaveBeenCalledWith({ email: 'alex@example.com', org: 'org_acme' });
    expect(view.container.textContent).toContain('Check your mail');
    expect(view.container.textContent).toContain('If this address can sign up');
    const resend = [...view.container.querySelectorAll('button')].find((button) => button.textContent === 'Send it again');
    await act(async () => resend?.click());
    await settleTask();
    expect(request).toHaveBeenCalledTimes(2);
    expect(view.container.textContent).toContain('Use the newest mail');
  } finally { await view.unmount(); }
});

it('preserves focus on entry and announces the check-mail step through its heading', async () => {
  request.mockResolvedValue(undefined);
  const existing = document.createElement('input');
  document.body.append(existing);
  existing.focus();
  const view = await renderForm(<LocalSignupForm landing={null} onBack={vi.fn()} />);
  try {
    expect(document.activeElement).toBe(existing);
    const email = view.container.querySelector('input');
    if (!(email instanceof HTMLInputElement)) throw new Error('missing email');
    email.focus();
    await act(async () => typeInto(email, 'alex@example.com'));
    await act(async () => view.container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await settleTask();
    expect(document.activeElement).toBe(view.container.querySelector('h1'));
    expect(document.activeElement?.textContent).toBe('Check your mail');
  } finally { await view.unmount(); existing.remove(); }
});
