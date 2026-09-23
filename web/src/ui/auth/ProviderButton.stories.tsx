import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { contoso, corp, github, google } from './fixtures.ts';
import { ProviderButton } from './ProviderButton.tsx';

const meta = {
  component: ProviderButton,
  tags: ['ai-generated'],
  args: { provider: google, intent: 'sign-in', busy: false, disabled: false, onClick: fn() },
  decorators: [
    (Story) => (
      <div className="login" style={{ minHeight: 'auto' }}>
        <div className="login__card">
          <div className="login__methods">
            <Story />
          </div>
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof ProviderButton>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Google's standard-colour G; "Continue with Google" on the sign-in door. */
export const Google: Story = {};

/** The same row under the sign-up heading: Google permits "Sign up with". */
export const GoogleSignUp: Story = { args: { intent: 'sign-up' } };

/** Microsoft blesses only "Sign in with Microsoft"; the tenant follows the fixed text. */
export const Microsoft: Story = { args: { provider: contoso } };

/** The Microsoft wording does not change on the sign-up door (#587 Q3). */
export const MicrosoftSignUp: Story = {
  args: { provider: contoso, intent: 'sign-up' },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Sign in with Microsoft · Contoso' })).toBeVisible();
  },
};

/** The Invertocat in the text colour, white on the dark fill. */
export const GitHub: Story = { args: { provider: github } };

export const GitHubSignUp: Story = { args: { provider: github, intent: 'sign-up' } };

/** A generic OIDC or SAML row in the house style, no mark. */
export const Plain: Story = { args: { provider: corp } };

/** This row's ceremony is in flight: the mark stays, the text says so. */
export const Contacting: Story = { args: { provider: github, busy: true, disabled: true } };

/** Another leg is in flight: barred, still recognisable. */
export const Disabled: Story = { args: { disabled: true } };

export const FiresOnClick: Story = {
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Continue with Google' }));
    await expect(args.onClick).toHaveBeenCalledOnce();
  },
};
