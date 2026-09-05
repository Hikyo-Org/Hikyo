// @vitest-environment happy-dom
import { act, useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { AccountSecurity } from './AccountSecurity.tsx';

const mocks = vi.hoisted(() => ({
  regenerate: vi.fn(),
  startTotp: vi.fn(),
  totpFetching: false,
  writeClipboard: vi.fn(),
}));

vi.mock('../app/clipboard.ts', () => ({ writeClipboard: mocks.writeClipboard }));

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({
    identity: {
      principal: { id: 'acc_test', display_name: 'Test account' },
      session: { assurance: { factors: ['password'] } },
    },
  }),
}));

vi.mock('../app/theme.ts', () => ({
  themeLabel: (choice: string) => choice,
  useThemeChoice: () => ['system', vi.fn()],
}));

vi.mock('../app/notifications.tsx', () => ({
  clearNotification: vi.fn(),
  notifyFailure: vi.fn(),
}));

vi.mock('../api/oidcChannel.ts', () => ({ rememberOIDCReturn: vi.fn() }));

vi.mock('../api/remotes.ts', () => ({
  useRevokeSession: () => ({ isPending: false, mutate: vi.fn() }),
  useSessions: () => ({
    isPending: false,
    isError: false,
    isSuccess: true,
    data: { items: [] },
  }),
}));

vi.mock('../api/account.ts', () => {
  const mutation = () => ({ isPending: false, mutate: vi.fn() });
  return {
    accountFailureText: () => 'Account change refused.',
    useAuthMethods: () => ({
      isPending: false,
      isError: false,
      isSuccess: true,
      data: { providers: [] },
    }),
    useConfirmTotp: mutation,
    useEnrolPasskey: mutation,
    useEnrolTotpStart: () => ({ isPending: false, mutate: mocks.startTotp }),
    useIdentities: () => ({
      isPending: false,
      isError: false,
      isSuccess: true,
      data: { identities: [] },
    }),
    useLinkIdentity: mutation,
    usePasskeys: () => ({
      isPending: false,
      isError: false,
      isSuccess: true,
      data: { passkeys: [] },
    }),
    useRegenerateRecoveryCodes: () => {
      const [codes, setCodes] = useState<readonly string[] | null>(null);
      return { codes, isPending: false, dismiss: () => setCodes(null),
        mutate: (input: { proof: string }) => mocks.regenerate(input, {
          onSuccess: (result: { recovery_codes: readonly string[] }) => setCodes(result.recovery_codes),
        }) };
    },
    useRemovePasskey: mutation,
    useRemoveTotp: mutation,
    useTotpStatus: () => ({
      isPending: false,
      isError: false,
      isSuccess: true,
      isFetching: mocks.totpFetching,
      data: { confirmed: false, pending: false },
    }),
    useUnlinkIdentity: mutation,
  };
});

afterEach(() => {
  mocks.totpFetching = false;
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

function buttonNamed(container: HTMLElement, name: string): HTMLButtonElement {
  const button = Array.from(container.querySelectorAll('button')).find(
    (candidate) => candidate.textContent === name,
  );
  if (button === undefined) {
    throw new Error(`button ${name} is missing`);
  }
  return button;
}

async function submitProof(container: HTMLElement): Promise<void> {
  const input = container.querySelector('dialog input[type="password"]');
  if (!(input instanceof HTMLInputElement)) {
    throw new Error('proof input is missing');
  }
  const form = input.closest('form');
  if (!(form instanceof HTMLFormElement)) {
    throw new Error('proof form is missing');
  }
  await act(async () => typeInto(input, 'existing-password'));
  await act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  await settle();
}

describe('display-once account secrets', () => {
  it('copies recovery codes as newline-separated text and announces success', async () => {
    mocks.regenerate.mockImplementation(
      (
        _input: { proof: string },
        options: { onSuccess: (result: { recovery_codes: readonly string[] }) => void },
      ) => options.onSuccess({ recovery_codes: ['recover-one', 'recover-two'] }),
    );
    mocks.writeClipboard.mockResolvedValue('ok');

    const { container } = await renderForm(<AccountSecurity />);
    await act(async () => buttonNamed(container, 'Replace recovery codes').click());
    await submitProof(container);
    await act(async () => buttonNamed(container, 'Copy').click());
    await settle();

    expect(mocks.writeClipboard).toHaveBeenCalledWith('recover-one\nrecover-two');
    expect(container.textContent).toContain('Recovery codes copied.');
  });

  it('keeps recovery-code storage unconfirmed when clipboard access is refused', async () => {
    mocks.regenerate.mockImplementation(
      (
        _input: { proof: string },
        options: { onSuccess: (result: { recovery_codes: readonly string[] }) => void },
      ) => options.onSuccess({ recovery_codes: ['recover-only'] }),
    );
    mocks.writeClipboard.mockResolvedValue('refused');

    const { container } = await renderForm(<AccountSecurity />);
    await act(async () => buttonNamed(container, 'Replace recovery codes').click());
    await submitProof(container);
    await act(async () => buttonNamed(container, 'Copy').click());
    await settle();

    expect(container.textContent).toContain(
      'This browser refused clipboard access, so nothing was copied.',
    );
    expect(buttonNamed(container, 'Done').disabled).toBe(true);
  });

  it('copies the exact display-once authenticator URI', async () => {
    const uri = 'otpauth://totp/Hikyo:test?secret=SEED&issuer=Hikyo';
    mocks.startTotp.mockImplementation(
      (
        _input: { password: string },
        options: { onSuccess: (result: { otpauth_uri: string }) => void },
      ) => {
        mocks.totpFetching = true;
        options.onSuccess({ otpauth_uri: uri });
      },
    );
    mocks.writeClipboard.mockResolvedValue('ok');

    const { container } = await renderForm(<AccountSecurity />);
    await act(async () => buttonNamed(container, 'enrol').click());
    await submitProof(container);
    await act(async () => buttonNamed(container, 'Copy').click());
    await settle();

    expect(mocks.writeClipboard).toHaveBeenCalledWith(uri);
    expect(container.textContent).toContain('Authenticator setup copied.');
  });
});
