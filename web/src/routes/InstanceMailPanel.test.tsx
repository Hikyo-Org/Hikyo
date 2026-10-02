// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { InstanceMailPanel } from './InstanceMailPanel.tsx';

const mocks = vi.hoisted(() => ({ configured: true, send: vi.fn() }));
vi.mock('../api/signup.ts', async (original) => ({ ...await original(),
  useInstanceMail: () => ({ isPending: false, isError: false, isSuccess: true, data: { configured: mocks.configured } }),
  testInstanceMail: mocks.send,
}));
afterEach(() => { mocks.configured = true; vi.clearAllMocks(); });

it('keeps static configuration separate from delivery and requires proof for an explicit test', async () => {
  mocks.send.mockResolvedValue(undefined);
  const view = await renderForm(<MemoryRouter><InstanceMailPanel /></MemoryRouter>);
  try {
    expect(view.container.textContent).toContain('Only a test send checks reachability');
    expect(mocks.send).not.toHaveBeenCalled();
    const recipient = view.container.querySelector('input');
    if (!(recipient instanceof HTMLInputElement)) throw new Error('missing recipient');
    await act(async () => typeInto(recipient, 'operator@example.com'));
    await act(async () => view.container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(mocks.send).not.toHaveBeenCalled();
    const dialog = document.querySelector('dialog');
    const proof = dialog?.querySelector('input');
    if (!(proof instanceof HTMLInputElement)) throw new Error('missing proof');
    await act(async () => typeInto(proof, '123456'));
    await act(async () => dialog?.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await settleTask();
    expect(mocks.send).toHaveBeenCalledWith({ to: 'operator@example.com', proof: '123456' });
    expect(view.container.textContent).toContain('Test mail sent');
  } finally { await view.unmount(); }
});

it('disables sending when the mailer is unconfigured', async () => {
  mocks.configured = false;
  const view = await renderForm(<MemoryRouter><InstanceMailPanel /></MemoryRouter>);
  try {
    expect(view.container.querySelector('button')?.disabled).toBe(true);
    expect(view.container.textContent).toContain('HIKYO_MAIL_*');
    expect(mocks.send).not.toHaveBeenCalled();
  } finally { await view.unmount(); }
});
