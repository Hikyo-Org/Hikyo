import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { expect, userEvent, waitFor } from 'storybook/test';

import { LoginForm, type ProviderIdentity, type SignInBusy, type SignInIntent } from './LoginForm.tsx';
import { SecondFactorChallenge } from './SecondFactorChallenge.tsx';
import { SecondFactorSetup, type SetupStep } from './SecondFactorSetup.tsx';
import { codesStep, github, google, socialProviders, totpStep } from './fixtures.ts';


const links = (
  <>
    <a href="#establish">Have a setup authority? Establish your credential</a>
    <a href="#recover">Lost your second factor? Recover with a code</a>
  </>
);

// A stateful walk through the ways in, with the transport simulated so the
// sequencing is the story. `scenario` fixes what the account has enrolled;
// `policy` is the one instance decision left: whether an account may exist
// with no second factor at all. Password `correct`, code `123456`.
//
// Rules (storybook-ui-consistency handoff, decision 2B): a factor that stands
// is presented, never skipped; an account with none is gated into enrolment
// when the policy requires it. Passkey and provider legs never see either step.
// `sign-up` opens the door (an org-scope registration policy): a provider
// chosen there passes the confirmation and starts with intent sign-up (#604).

type Scenario = 'password-enrolled' | 'password-unenrolled' | 'passkey' | 'provider' | 'sign-up';
type Policy = 'require-second-factor' | 'allow-unenrolled';
type Stage =
  | { at: 'sign-in'; busy: SignInBusy; error: string | null }
  | { at: 'challenge'; username: string; busy: 'code' | 'passkey' | null; error: string | null }
  | { at: 'setup'; username: string; step: SetupStep; busy: 'password' | 'totp' | 'passkey' | 'code' | null; error: string | null }
  | { at: 'done'; how: string; assurance: string };

const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 400));
const wrongPassword = 'That username and password did not match. Check both and try again.';
const wrongCode =
  'That code was not accepted. A code is valid for one time step and is used once: wait for the next code and try again.';

function LoginFlow({ scenario, policy }: { scenario: Scenario; policy: Policy }) {
  const [stage, setStage] = useState<Stage>({ at: 'sign-in', busy: null, error: null });

  const password = async ({ username, password }: { username: string; password: string }) => {
    setStage({ at: 'sign-in', busy: 'password', error: null });
    await tick();
    if (password !== 'correct') {
      setStage({ at: 'sign-in', busy: null, error: wrongPassword });
      return;
    }
    if (scenario === 'password-enrolled') {
      setStage({ at: 'challenge', username, busy: null, error: null });
    } else if (policy === 'require-second-factor') {
      setStage({ at: 'setup', username, step: { kind: 'password' }, busy: null, error: null });
    } else {
      setStage({ at: 'done', how: 'Password', assurance: 'password (single factor)' });
    }
  };

  const code = async (username: string, code: string) => {
    setStage({ at: 'challenge', username, busy: 'code', error: null });
    await tick();
    if (code !== '123456') {
      setStage({ at: 'challenge', username, busy: null, error: wrongCode });
      return;
    }
    setStage({ at: 'done', how: 'Password, then authenticator code', assurance: 'password + totp' });
  };

  const stepUpPasskey = async (username: string) => {
    setStage({ at: 'challenge', username, busy: 'passkey', error: null });
    await tick();
    setStage({ at: 'done', how: 'Password, then passkey', assurance: 'password + webauthn' });
  };

  const passkey = async () => {
    setStage({ at: 'sign-in', busy: 'passkey', error: null });
    await tick();
    setStage({ at: 'done', how: 'Passkey (passwordless)', assurance: 'webauthn, user-verified' });
  };

  const provider = async (chosen: ProviderIdentity, intent: SignInIntent) => {
    setStage({ at: 'sign-in', busy: { provider: chosen }, error: null });
    await tick();
    const name =
      socialProviders.find((candidate) => candidate.kind === chosen.kind && candidate.slug === chosen.slug)
        ?.display_name ?? chosen.slug;
    setStage({
      at: 'done',
      how: `Redirected to ${name} with intent ${intent}`,
      assurance: 'as asserted by the provider (acr/amr policy); no local factor asked',
    });
  };

  const done = () =>
    setStage({ at: 'done', how: 'Password, then enrolled a factor', assurance: 'password + newly enrolled factor' });

  const setup = {
    password: async (username: string, value: string) => {
      setStage({ at: 'setup', username, step: { kind: 'password' }, busy: 'password', error: null });
      await tick();
      if (value !== 'correct') {
        setStage({ at: 'setup', username, step: { kind: 'password' }, busy: null, error: wrongPassword });
        return;
      }
      setStage({ at: 'setup', username, step: codesStep, busy: null, error: null });
    },
    codesStored: (username: string) =>
      setStage({ at: 'setup', username, step: { kind: 'choose' }, busy: null, error: null }),
    chooseTotp: async (username: string) => {
      setStage({ at: 'setup', username, step: { kind: 'choose' }, busy: 'totp', error: null });
      await tick();
      setStage({ at: 'setup', username, step: totpStep, busy: null, error: null });
    },
    choosePasskey: async (username: string) => {
      setStage({ at: 'setup', username, step: { kind: 'choose' }, busy: 'passkey', error: null });
      await tick();
      done();
    },
    confirm: async (username: string, value: string) => {
      setStage({ at: 'setup', username, step: totpStep, busy: 'code', error: null });
      await tick();
      if (value !== '123456') {
        setStage({ at: 'setup', username, step: totpStep, busy: null, error: wrongCode });
        return;
      }
      done();
    },
  };

  switch (stage.at) {
    case 'sign-in':
      return (
        <LoginForm
          providers={scenario === 'provider' || scenario === 'sign-up' ? socialProviders : []}
          passkeys={scenario !== 'provider'}
          signup={
            scenario === 'sign-up'
              ? { providers: [google, github], landing: "You'll join Acme Corp as Developer." }
              : null
          }
          paused={false}
          lastUsed={null}
          busy={stage.busy}
          error={stage.error}
          onPassword={(credentials) => void password(credentials)}
          onPasskey={() => void passkey()}
          onProvider={(chosen, intent) => void provider(chosen, intent)}
          links={links}
        />
      );
    case 'challenge':
      return (
        <SecondFactorChallenge
          username={stage.username}
          totp
          passkey
          busy={stage.busy}
          error={stage.error}
          onCode={(value) => void code(stage.username, value)}
          onPasskey={() => void stepUpPasskey(stage.username)}
        />
      );
    case 'setup':
      return (
        <SecondFactorSetup
          username={stage.username}
          step={stage.step}
          passkeys
          busy={stage.busy}
          error={stage.error}
          onPassword={(value) => void setup.password(stage.username, value)}
          onCodesStored={() => setup.codesStored(stage.username)}
          onChooseTotp={() => void setup.chooseTotp(stage.username)}
          onChoosePasskey={() => void setup.choosePasskey(stage.username)}
          onConfirmCode={(value) => void setup.confirm(stage.username, value)}
        />
      );
    case 'done':
      return (
        <div className="login__card">
          <h1 className="login__title">Signed in</h1>
          <p className="login__account">
            How: <strong>{stage.how}</strong>
          </p>
          <p className="login__account">
            Session assurance: <strong>{stage.assurance}</strong>
          </p>
        </div>
      );
  }
}

const meta = {
  title: 'ui/auth/LoginFlow',
  component: LoginFlow,
  tags: ['ai-generated'],
  args: { scenario: 'password-enrolled', policy: 'require-second-factor' },
  argTypes: {
    scenario: {
      control: 'select',
      options: ['password-enrolled', 'password-unenrolled', 'passkey', 'provider', 'sign-up'],
    },
    policy: { control: 'select', options: ['require-second-factor', 'allow-unenrolled'] },
  },
  render: (args) => <LoginFlow key={`${args.scenario}-${args.policy}`} {...args} />,
  decorators: [
    (Story) => (
      <div className="login" style={{ minHeight: 'auto' }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof LoginFlow>;

export default meta;
type FlowStory = StoryObj<typeof meta>;

async function signInWithPassword(canvas: Parameters<NonNullable<FlowStory['play']>>[0]['canvas']) {
  await userEvent.click(canvas.getByRole('button', { name: 'Password' }));
  await userEvent.type(canvas.getByLabelText('Username'), 'alex');
  await userEvent.type(canvas.getByLabelText('Password'), 'correct');
  await userEvent.click(canvas.getByRole('button', { name: 'Sign in' }));
}

/** Enrolled account: password, then the authenticator code. No skip exists. */
export const PasswordThenAuthenticator: FlowStory = {
  play: async ({ canvas }) => {
    await signInWithPassword(canvas);
    await expect(await canvas.findByRole('heading', { name: 'Present your second factor' })).toBeVisible();
    await expect(canvas.queryByRole('button', { name: /continue|skip|without/i })).toBeNull();
    await userEvent.type(canvas.getByLabelText('Authenticator code'), '123456');
    await userEvent.click(canvas.getByRole('button', { name: 'Present code' }));
    await expect(await canvas.findByRole('heading', { name: 'Signed in' })).toBeVisible();
    await expect(canvas.getByText('password + totp')).toBeVisible();
  },
};

/** Enrolled account, wrong code: the step holds and names the refusal. */
export const AuthenticatorCodeRefused: FlowStory = {
  play: async ({ canvas }) => {
    await signInWithPassword(canvas);
    await userEvent.type(await canvas.findByLabelText('Authenticator code'), '000000');
    await userEvent.click(canvas.getByRole('button', { name: 'Present code' }));
    await expect(await canvas.findByRole('alert')).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'Present your second factor' })).toBeVisible();
  },
};

/**
 * Nothing enrolled, instance requires a factor: gated into enrolment. The
 * password is re-proved, the recovery codes come first (the password proves
 * them only until an authenticator stands), then the factor completes it.
 */
export const UnenrolledIsGatedIntoSetup: FlowStory = {
  args: { scenario: 'password-unenrolled', policy: 'require-second-factor' },
  play: async ({ canvas }) => {
    await signInWithPassword(canvas);
    await expect(await canvas.findByRole('heading', { name: 'Set up a second factor' })).toBeVisible();
    await expect(canvas.queryByRole('button', { name: /skip|later|not now|without/i })).toBeNull();
    await userEvent.type(canvas.getByLabelText('Password'), 'correct');
    await userEvent.click(canvas.getByRole('button', { name: 'Continue' }));
    await expect(await canvas.findByRole('heading', { name: 'Store your recovery codes' })).toBeVisible();
    const proceed = canvas.getByRole('button', { name: 'Continue' });
    await expect(proceed).toBeDisabled();
    await userEvent.click(canvas.getByRole('checkbox'));
    await userEvent.click(proceed);
    await userEvent.click(await canvas.findByRole('button', { name: 'Use an authenticator app' }));
    await expect(await canvas.findByRole('img', { name: /enrolment qr/i })).toBeVisible();
    await userEvent.type(canvas.getByLabelText('Authenticator code'), '123456');
    await userEvent.click(canvas.getByRole('button', { name: 'Confirm and enrol' }));
    await expect(await canvas.findByRole('heading', { name: 'Signed in' })).toBeVisible();
  },
};

/** Nothing enrolled, instance allows it: the password is the whole ceremony. */
export const UnenrolledAllowedByPolicy: FlowStory = {
  args: { scenario: 'password-unenrolled', policy: 'allow-unenrolled' },
  play: async ({ canvas }) => {
    await signInWithPassword(canvas);
    await expect(await canvas.findByRole('heading', { name: 'Signed in' })).toBeVisible();
    await expect(canvas.getByText('password (single factor)')).toBeVisible();
  },
};

/** A discoverable passkey is primary authentication: one gesture, multi-factor, no second step. */
export const PasskeyPasswordless: FlowStory = {
  args: { scenario: 'passkey', policy: 'require-second-factor' },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Passkey' }));
    await expect(await canvas.findByRole('heading', { name: 'Signed in' })).toBeVisible();
    await expect(canvas.getByText('webauthn, user-verified')).toBeVisible();
  },
};

/** An identity provider leg: assurance is the provider's; no local factor is asked for. */
export const IdentityProvider: FlowStory = {
  args: { scenario: 'provider', policy: 'require-second-factor' },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Continue with Google' }));
    await expect(await canvas.findByRole('heading', { name: 'Signed in' })).toBeVisible();
    await expect(canvas.getByText('Redirected to Google with intent sign-in')).toBeVisible();
    await waitFor(() => expect(canvas.getByText(/no local factor asked/)).toBeVisible());
  },
};

/** Through the sign-up door: the confirmation names the new account, the start carries intent sign-up. */
export const SignUpThroughProvider: FlowStory = {
  args: { scenario: 'sign-up', policy: 'require-second-factor' },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Create an account' }));
    await userEvent.click(canvas.getByRole('button', { name: 'Sign up with GitHub' }));
    await expect(canvas.getByRole('heading', { name: 'Create an account with GitHub' })).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Continue to GitHub' }));
    await expect(await canvas.findByRole('heading', { name: 'Signed in' })).toBeVisible();
    await expect(canvas.getByText('Redirected to GitHub with intent sign-up')).toBeVisible();
  },
};
