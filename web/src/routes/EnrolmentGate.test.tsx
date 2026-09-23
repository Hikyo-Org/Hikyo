// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, expect, it, vi, type Mock } from 'vitest';

import { authenticatedIdentity } from '../testkit/identity.ts';
import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { EnrolmentGate } from './EnrolmentGate.tsx';

type Leg = { mutate: Mock; reset: Mock; isPending: boolean; isError: boolean; error: Error | null };
type Mocks = {
  leg: () => Leg;
  codes: {
    held: { codes: readonly string[]; password: string } | null;
    isPending: boolean;
    error: Error | null;
    issue: Mock;
    releasePassword: Mock;
  };
  totpStart: Leg;
  totpConfirm: Leg;
  passkey: Leg;
  logout: Leg;
};

const mocks = vi.hoisted((): Mocks => {
  const leg = (): Leg => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null });
  return {
    leg,
    codes: { held: null, isPending: false, error: null, issue: vi.fn(), releasePassword: vi.fn() },
    totpStart: leg(),
    totpConfirm: leg(),
    passkey: leg(),
    logout: leg(),
  };
});
vi.mock('../api/account.ts', () => ({
  accountFailureText: () => 'Account change failed.',
  useEnrolmentGateCodes: () => mocks.codes,
  useEnrolTotpStart: () => mocks.totpStart,
  useConfirmTotp: () => mocks.totpConfirm,
  useEnrolPasskey: () => mocks.passkey,
}));
vi.mock('../api/session.ts', () => ({ useLogout: () => mocks.logout }));
vi.mock('../api/stepup.ts', () => ({
  passkeysAvailable: () => true,
  stepUpFailureText: () => 'Code refused.',
}));
vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { ...authenticatedIdentity, enrolment_required: true } }),
}));

beforeEach(() => {
  mocks.codes.held = null;
  mocks.codes.isPending = false;
  mocks.codes.error = null;
  mocks.codes.issue = vi.fn();
  mocks.codes.releasePassword = vi.fn();
  mocks.totpStart = mocks.leg();
  mocks.totpConfirm = mocks.leg();
  mocks.passkey = mocks.leg();
  mocks.logout = mocks.leg();
});

afterEach(() => vi.clearAllMocks());

async function mount() {
  const { container, unmount } = await renderForm(<EnrolmentGate />);
  const button = (name: string) =>
    [...container.querySelectorAll('button')].find((candidate) => candidate.textContent === name);
  return { container, button, unmount };
}

it('opens on the password proof and issues the recovery codes with it', async () => {
  const gate = await mount();
  expect(gate.container.textContent).toContain('Set up a second factor');
  expect(gate.container.textContent).toContain('Test operator');

  const password = gate.container.querySelector<HTMLInputElement>('input[type="password"]');
  if (password === null) throw new Error('no password field');
  await act(async () => typeInto(password, 'correct'));
  await act(async () => gate.button('Continue')?.click());
  expect(mocks.codes.issue).toHaveBeenCalledWith('correct');
  await gate.unmount();
});

it('names a refused password on the proof step', async () => {
  const { ApiError } = await import('../api/client.ts');
  mocks.codes.error = new ApiError(401, 'unauthenticated');
  const gate = await mount();
  expect(gate.container.querySelector('[role="alert"]')?.textContent).toContain(
    'That password was not accepted.',
  );
  await gate.unmount();
});

it('shows the codes, then the factor choice, and enrols an authenticator with the held password', async () => {
  mocks.codes.held = { codes: ['aaaa-bbbb', 'cccc-dddd'], password: 'correct' };
  mocks.totpStart.mutate.mockImplementation(
    (_input: unknown, callbacks: { onSuccess: (result: { otpauth_uri: string }) => void }) =>
      callbacks.onSuccess({ otpauth_uri: 'otpauth://totp/Hikyo:alex?secret=JBSWY3DP&issuer=Hikyo' }),
  );
  const gate = await mount();
  expect(gate.container.textContent).toContain('aaaa-bbbb');
  expect(gate.button('Continue')?.disabled).toBe(true);

  await act(async () => gate.container.querySelector<HTMLInputElement>('input[type="checkbox"]')?.click());
  await act(async () => gate.button('Continue')?.click());
  expect(gate.container.textContent).toContain('Choose your second factor');

  await act(async () => gate.button('Use an authenticator app')?.click());
  expect(mocks.totpStart.mutate).toHaveBeenCalledWith({ password: 'correct' }, expect.anything());
  expect(gate.container.textContent).toContain('JBSWY3DP');
  expect(gate.container.textContent).toContain('Scan, then confirm one code');
  expect(mocks.codes.releasePassword).toHaveBeenCalled();
  await gate.unmount();
});

it('enrols a passkey with the held password as the proof', async () => {
  mocks.codes.held = { codes: ['aaaa-bbbb'], password: 'correct' };
  const gate = await mount();
  await act(async () => gate.container.querySelector<HTMLInputElement>('input[type="checkbox"]')?.click());
  await act(async () => gate.button('Continue')?.click());
  await act(async () => gate.button('Create a passkey')?.click());
  expect(mocks.passkey.mutate).toHaveBeenCalledWith({ proof: { kind: 'password', password: 'correct' } });
  await gate.unmount();
});

it('offers sign-out, and no way past the gate', async () => {
  const gate = await mount();
  expect(
    [...gate.container.querySelectorAll('button')].some((button) =>
      /skip|later|not now|without/i.test(button.textContent ?? ''),
    ),
  ).toBe(false);
  await act(async () => gate.button('Sign out')?.click());
  expect(mocks.logout.mutate).toHaveBeenCalled();
  await gate.unmount();
});
