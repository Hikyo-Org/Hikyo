// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import type { RegistrationPolicy } from '../api/registration.ts';
import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { OpenRegistrationPanel } from './OpenRegistration.tsx';

type PolicyQuery = { data: RegistrationPolicy | null | undefined; isPending: boolean; isError: boolean; isSuccess: boolean; error: unknown };

type Mocks = {
  policy: PolicyQuery;
  put: ReturnType<typeof vi.fn>;
  del: ReturnType<typeof vi.fn>;
  writeClipboard: ReturnType<typeof vi.fn>;
};

const mocks = vi.hoisted((): Mocks => ({
  policy: { data: null, isPending: false, isError: false, isSuccess: true, error: null },
  put: vi.fn(),
  del: vi.fn(),
  writeClipboard: vi.fn(),
}));

vi.mock('../api/registration.ts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/registration.ts')>()),
  useRegistrationPolicy: () => mocks.policy,
  putRegistrationPolicy: mocks.put,
  deleteRegistrationPolicy: mocks.del,
}));
vi.mock('../api/account.ts', () => ({
  useAuthMethods: () => ({
    isPending: false,
    data: {
      local_login_enabled: true,
      providers: [
        { kind: 'oidc', slug: 'corp', display_name: 'Corporate IdP' },
        { kind: 'saml', slug: 'sso', display_name: 'SAML SSO' },
      ],
      signup_open: false,
      signup_paused: false,
      signup_methods: [],
    },
  }),
}));
vi.mock('../app/clipboard.ts', () => ({ writeClipboard: mocks.writeClipboard }));
vi.mock('../ui/useModalDialog.ts', async (importActual) => {
  const actual = await importActual<typeof import('../ui/useModalDialog.ts')>();
  return { ...actual, useModalDialog: () => ({ current: null }) };
});

const base = {
  id: 'rpol_1',
  authority_principal_id: 'prn_alex',
  row_version: 1,
  created_at: '2026-09-20T08:00:00Z',
  updated_at: '2026-09-20T08:00:00Z',
};

function policyOf(overrides: Partial<RegistrationPolicy>): RegistrationPolicy {
  return {
    ...base,
    external: [{ provider: { kind: 'oidc', slug: 'corp' }, display_name: 'Corporate IdP' }],
    landing: { kind: 'org-template', template: 'viewer' },
    state: 'active',
    ...overrides,
  };
}

function buttonNamed(root: ParentNode, name: string): HTMLButtonElement {
  const button = [...root.querySelectorAll('button')].find((candidate) => candidate.textContent === name);
  if (!(button instanceof HTMLButtonElement)) throw new Error(`no ${name} button`);
  return button;
}

function inputLabelled(root: ParentNode, label: string): HTMLInputElement {
  const match = [...root.querySelectorAll('label')].find((candidate) => candidate.textContent === label);
  const control = match === undefined ? null : root.querySelector(`#${CSS.escape(match.htmlFor)}`);
  if (!(control instanceof HTMLInputElement)) throw new Error(`${label} input is missing`);
  return control;
}

async function click(button: HTMLElement) {
  await act(async () => {
    button.click();
    await Promise.resolve();
  });
  await settle();
}

function mount(scope: { kind: 'org'; org: string } | { kind: 'instance' }) {
  const onChanged = vi.fn();
  const rendered = renderForm(
    <OpenRegistrationPanel
      scope={scope}
      scopeName={scope.kind === 'org' ? 'Acme' : 'Instance'}
      origin="https://hikyo.example"
      authorityName={(principal) => (principal === 'prn_alex' ? 'Alex' : principal)}
      onChanged={onChanged}
    />,
  );
  return { rendered, onChanged };
}

beforeEach(() => {
  mocks.policy = { data: null, isPending: false, isError: false, isSuccess: true, error: null };
});

afterEach(() => {
  mocks.put.mockReset();
  mocks.del.mockReset();
});

describe('OpenRegistrationPanel', () => {
  it('shows a closed scope and refuses an empty policy before any proof is asked', async () => {
    const { rendered } = mount({ kind: 'org', org: 'org_acme' });
    const { container, unmount } = await rendered;
    expect(container.textContent).toContain('closed');
    expect(container.textContent).toContain('No one can sign up into Acme without an invitation.');
    await click(buttonNamed(container, 'Open registration…'));
    // Only federated kinds are sign-up providers: SAML is not listed.
    expect(container.textContent).toContain('Corporate IdP');
    expect(container.textContent).not.toContain('SAML SSO');
    await click(buttonNamed(container, 'Save'));
    expect(container.textContent).toContain('Admit at least one way to sign up, or close registration instead.');
    expect(container.textContent).not.toContain("Confirm it's you");
    expect(mocks.put).not.toHaveBeenCalled();
    await unmount();
  });

  it('refuses email as an allowlist claim on its row', async () => {
    const { rendered } = mount({ kind: 'org', org: 'org_acme' });
    const { container, unmount } = await rendered;
    await click(buttonNamed(container, 'Open registration…'));
    await click(inputLabelled(container, 'Corporate IdP'));
    // The requirement line appears with the entry.
    expect(container.textContent).toContain('The ID token must carry email plus email_verified or xms_edov as boolean true.');
    await act(async () => typeInto(inputLabelled(container, 'Allowlist claim'), 'email'));
    await act(async () => typeInto(inputLabelled(container, 'Accepted values'), 'a@b.example'));
    await click(buttonNamed(container, 'Save'));
    expect(container.textContent).toContain('email is not accepted as an allowlist claim');
    expect(mocks.put).not.toHaveBeenCalled();
    await unmount();
  });

  it('saves behind a blue proof step and shows a 400 on the row it names', async () => {
    mocks.put.mockRejectedValueOnce(new ApiError(400, 'bad_request', 'provider-missing-email-scope: oidc:corp'));
    const { rendered } = mount({ kind: 'org', org: 'org_acme' });
    const { container, unmount } = await rendered;
    await click(buttonNamed(container, 'Open registration…'));
    await click(inputLabelled(container, 'Corporate IdP'));
    const save = buttonNamed(container, 'Save');
    expect(save.classList.contains('btn--reauth')).toBe(true);
    await click(save);
    expect(container.textContent).toContain("Confirm it's you");
    const confirm = buttonNamed(container, 'Confirm');
    expect(confirm.classList.contains('btn--reauth')).toBe(true);
    await act(async () => typeInto(inputLabelled(container, 'Authenticator code or password'), '123456'));
    await click(confirm);
    expect(mocks.put).toHaveBeenCalledWith(
      { kind: 'org', org: 'org_acme' },
      {
        external: [{ provider: { kind: 'oidc', slug: 'corp' } }],
        landing: { kind: 'org-template', template: 'viewer' },
        proof: '123456',
      },
    );
    // Back on the editor, the refusal sits on the provider row it names.
    expect(container.textContent).toContain('corp: this provider row does not request the email scope');
    expect(container.textContent).not.toContain("Confirm it's you");
    await unmount();
  });

  it('renders n / cap, the requirement lines and the authority of an instance fresh-org policy', async () => {
    mocks.policy = {
      data: policyOf({
        landing: { kind: 'fresh-org', cap: 5 },
        fresh_org_count: 3,
        local: { domains: ['acme.example'] },
      }),
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
    };
    const { rendered } = mount({ kind: 'instance' });
    const { container, unmount } = await rendered;
    expect(container.textContent).toContain('3 / 5 minted');
    expect(container.textContent).toContain('@acme.example only');
    expect(container.textContent).toContain('The address is proven by a mailed link');
    expect(container.textContent).toContain('Alex · re-checked against their current grants');
    // No sign-up link at instance scope: the public login page is that door.
    expect(container.textContent).not.toContain('/signup?org=');
    await unmount();
  });

  it('shows the inactive cause and re-saves as authority behind fresh proof', async () => {
    mocks.policy = {
      data: policyOf({ state: 'inactive', inactive_cause: 'authority-lost' }),
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
    };
    mocks.put.mockResolvedValueOnce(policyOf({}));
    const { rendered, onChanged } = mount({ kind: 'org', org: 'org_acme' });
    const { container, unmount } = await rendered;
    expect(container.textContent).toContain('inactive · authority-lost');
    expect(container.textContent).toContain('Alex no longer holds the grant this policy hands out.');
    expect(container.textContent).toContain('https://hikyo.example/signup?org=org_acme');
    const resave = buttonNamed(container, 'Re-save as authority');
    expect(resave.classList.contains('btn--reauth')).toBe(true);
    await click(resave);
    await act(async () => typeInto(inputLabelled(container, 'Authenticator code or password'), '654321'));
    await click(buttonNamed(container, 'Confirm'));
    expect(mocks.put).toHaveBeenCalledWith(
      { kind: 'org', org: 'org_acme' },
      {
        external: [{ provider: { kind: 'oidc', slug: 'corp' } }],
        landing: { kind: 'org-template', template: 'viewer' },
        proof: '654321',
      },
    );
    expect(onChanged).toHaveBeenCalledWith('Policy saved. You are now its authority.');
    await unmount();
  });

  it('offers no re-save for a precondition cause, which re-saving cannot cure', async () => {
    mocks.policy = {
      data: policyOf({ state: 'inactive', inactive_cause: 'precondition', inactive_precondition: 'mailer-unconfigured' }),
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
    };
    const { rendered } = mount({ kind: 'org', org: 'org_acme' });
    const { container, unmount } = await rendered;
    expect(container.textContent).toContain('inactive · precondition');
    expect(container.textContent).toContain('The email entry needs a configured mailer');
    expect(container.textContent).not.toContain('Re-save as authority');
    await unmount();
  });

  it('keeps a refused proof on the proof step and clears it', async () => {
    mocks.policy = { data: policyOf({}), isPending: false, isError: false, isSuccess: true, error: null };
    mocks.del.mockRejectedValueOnce(new ApiError(401, 'unauthenticated'));
    const { rendered } = mount({ kind: 'org', org: 'org_acme' });
    const { container, unmount } = await rendered;
    await click(buttonNamed(container, 'Close registration'));
    const proof = inputLabelled(container, 'Authenticator code or password');
    await act(async () => typeInto(proof, 'wrong'));
    await click(buttonNamed(container, 'Confirm'));
    expect(mocks.del).toHaveBeenCalledWith({ kind: 'org', org: 'org_acme' }, 'wrong');
    expect(container.textContent).toContain('That proof was not accepted.');
    expect(inputLabelled(container, 'Authenticator code or password').value).toBe('');
    await unmount();
  });
});
