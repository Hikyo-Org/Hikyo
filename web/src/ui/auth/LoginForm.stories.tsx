import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { LoginForm, type SignInProvider } from './LoginForm.tsx';

const providers: readonly SignInProvider[] = [
  { slug: 'corp', display_name: 'Corporate IdP', kind: 'oidc' },
  { slug: 'sso', display_name: 'SAML SSO', kind: 'saml' },
];

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

export const WithProviders: Story = {};

export const LocalOnly: Story = { args: { providers: [], passkeys: false } };

export const PasswordAndPasskey: Story = { args: { providers: [] } };

export const SigningIn: Story = { args: { busy: 'password' } };

export const ContactingProvider: Story = { args: { busy: { provider: 'corp' } } };

export const Refused: Story = {
  args: { error: 'That username and password did not match. Check both and try again.' },
};

export const SubmitsAndClearsPassword: Story = {
  play: async ({ canvas, args }) => {
    await userEvent.type(canvas.getByLabelText('Username'), 'alex');
    await userEvent.type(canvas.getByLabelText('Password'), 'hunter2');
    await userEvent.click(canvas.getByRole('button', { name: 'Sign in' }));
    await expect(args.onPassword).toHaveBeenCalledWith({ username: 'alex', password: 'hunter2' });
    await expect(canvas.getByLabelText('Password')).toHaveValue('');
    await expect(canvas.getByLabelText('Username')).toHaveValue('alex');
  },
};


