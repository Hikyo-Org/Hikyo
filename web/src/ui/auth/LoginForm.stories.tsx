import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { LoginForm, type LoginProvider, type SignupDoor } from './LoginForm.tsx';
import { corp, github, google, socialProviders } from './fixtures.ts';

const providers: readonly LoginProvider[] = [
  { slug: 'corp', display_name: 'Corporate IdP', kind: 'oidc' },
  { slug: 'sso', display_name: 'SAML SSO', kind: 'saml' },
];

/** An org-scope policy (#579): Google, GitHub and one Entra tenant may sign up. */
const orgDoor: SignupDoor = {
  providers: ['google', 'github', 'contoso'],
  landing: "You'll join Acme Corp as Developer.",
};

const links = (
  <>
    <a href="#establish">Have a setup authority? Establish your credential</a>
    <a href="#recover">Lost your second factor? Recover with a code</a>
  </>
);

const meta = {
  component: LoginForm,
  tags: ['ai-generated'],
  args: {
    providers,
    passkeys: true,
    signup: null,
    paused: false,
    lastUsed: null,
    busy: null,
    error: null,
    onPassword: fn(),
    onPasskey: fn(),
    onProvider: fn(),
    links,
  },
  decorators: [
    (Story) => (
      <div className="login" style={{ minHeight: 'auto' }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof LoginForm>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Step one: one row per way in. Generic providers read "Continue with". */
export const WithProviders: Story = {};

/** The social providers of the locked prototype, brand rules per row, one Microsoft hint. */
export const SocialProviders: Story = {
  args: { providers: socialProviders },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Continue with Google' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Sign in with Microsoft · Contoso' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Sign in with Microsoft · Fabrikam' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Continue with GitHub' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Continue with Corp SSO' })).toBeVisible();
    await expect(canvas.getAllByText('Microsoft: work or school account')).toHaveLength(1);
  },
};

/** The row this browser used last time wears the badge; a provider is matched by slug. */
export const LastUsedProvider: Story = {
  args: { providers: socialProviders, lastUsed: { kind: 'provider', slug: 'github' } },
  play: async ({ canvas }) => {
    const row = canvas.getByRole('button', { name: /continue with github/i });
    await expect(row).toHaveTextContent('Last used');
    await expect(canvas.getAllByText('Last used')).toHaveLength(1);
  },
};

export const LastUsedPassword: Story = {
  args: { lastUsed: { kind: 'password' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: /^password/i })).toHaveTextContent('Last used');
  },
};

export const LastUsedPasskey: Story = { args: { lastUsed: { kind: 'passkey' } } };

/** Nothing but a password: the picker still stands, one row, so the shape never shifts. */
export const LocalOnly: Story = { args: { providers: [], passkeys: false } };

export const PasswordAndPasskey: Story = { args: { providers: [] } };

/** Step two, reached by the Password row: the credential form, with a way back. */
export const PasswordStep: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Password' }));
    const heading = canvas.getByRole('heading', { name: 'Sign in with a password' });
    await expect(heading).toBeVisible();
    // The pressed row is gone: the new step's heading takes focus, not the body.
    await expect(heading).toHaveFocus();
    await expect(canvas.getByLabelText('Username')).toBeVisible();
    await expect(canvas.getByLabelText('Password')).toBeVisible();
    await expect(canvas.getByRole('button', { name: /other ways to sign in/i })).toBeVisible();
  },
};

/** The passkey row itself carries the waiting label; every other row is barred. */
export const WaitingForPasskey: Story = {
  args: { busy: 'passkey' },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Waiting for the passkey…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Password' })).toBeDisabled();
  },
};

/** Only the provider being contacted wears the busy label; every control, the door included, is barred. */
export const ContactingProvider: Story = {
  args: { busy: { provider: 'corp' }, signup: { providers: ['corp'], landing: orgDoor.landing } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Contacting identity provider…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Continue with SAML SSO' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Create an account' })).toBeDisabled();
  },
};

/** A refusal sits above the rows, in text and ARIA, never colour alone. */
export const Refused: Story = {
  args: { error: 'That username and password did not match. Check both and try again.' },
};

/** An inactive policy (#606): "Sign-up is paused." and nothing about why. */
export const Paused: Story = {
  args: { providers: socialProviders, paused: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('status')).toHaveTextContent('Sign-up is paused.');
    await expect(canvas.queryByRole('button', { name: 'Create an account' })).toBeNull();
  },
};

/** Registration open: the door link renders under the rows. */
export const DoorOpen: Story = {
  args: { providers: socialProviders, signup: orgDoor },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Create an account' })).toBeVisible();
  },
};

/**
 * Through the door: only the policy-admitted providers (not Fabrikam, not
 * Corp SSO), Google and GitHub in their sign-up wording, Microsoft unchanged.
 */
export const SignUpDoor: Story = {
  args: { providers: socialProviders, signup: orgDoor },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Create an account' }));
    await expect(canvas.getByRole('heading', { name: 'Create an account' })).toBeVisible();
    await expect(canvas.getByText(orgDoor.landing)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Sign up with Google' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Sign up with GitHub' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Sign in with Microsoft · Contoso' })).toBeVisible();
    await expect(canvas.queryByRole('button', { name: /fabrikam|corp sso/i })).toBeNull();
    await expect(canvas.queryByRole('button', { name: 'Password' })).toBeNull();
  },
};

/** A provider chosen on the door passes the confirmation, then starts with intent sign-up. */
export const SignUpConfirmation: Story = {
  args: { providers: socialProviders, signup: orgDoor },
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Create an account' }));
    await userEvent.click(canvas.getByRole('button', { name: 'Sign up with GitHub' }));
    await expect(canvas.getByRole('heading', { name: 'Create an account with GitHub' })).toBeVisible();
    await expect(canvas.getByText(/this creates a new account/i)).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Continue to GitHub' }));
    await expect(args.onProvider).toHaveBeenCalledWith(github.slug, 'sign-up');
  },
};

/** Back from the confirmation lands on the door, back from the door on the rows. */
export const SignUpBackLinks: Story = {
  args: { providers: socialProviders, signup: orgDoor },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Create an account' }));
    await userEvent.click(canvas.getByRole('button', { name: 'Sign up with Google' }));
    await userEvent.click(canvas.getByRole('button', { name: /other ways to create an account/i }));
    await expect(canvas.getByRole('heading', { name: 'Create an account' })).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: /sign in instead/i }));
    await expect(canvas.getByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
  },
};

/** A provider row on the sign-in door starts at once, with intent sign-in. */
export const ProviderStartsFromStepOne: Story = {
  args: { providers: [google, corp] },
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Continue with Google' }));
    await expect(args.onProvider).toHaveBeenCalledWith(google.slug, 'sign-in');
  },
};

export const SubmitsAndClearsPassword: Story = {
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Password' }));
    await userEvent.type(canvas.getByLabelText('Username'), 'alex');
    await userEvent.type(canvas.getByLabelText('Password'), 'hunter2');
    await userEvent.click(canvas.getByRole('button', { name: 'Sign in' }));
    await expect(args.onPassword).toHaveBeenCalledWith({ username: 'alex', password: 'hunter2' });
    await expect(canvas.getByLabelText('Password')).toHaveValue('');
    await expect(canvas.getByLabelText('Username')).toHaveValue('alex');
  },
};
