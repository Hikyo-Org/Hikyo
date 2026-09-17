import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Alert } from './Alert.tsx';
import { Button } from './Button.tsx';

const meta = {
  component: Alert,
  tags: ['ai-generated'],
  args: { children: 'That username and password did not match. Check both and try again.' },
  argTypes: { tone: { control: 'select', options: ['danger', 'done', 'warn', 'info'] } },
} satisfies Meta<typeof Alert>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Danger: Story = {};
export const Done: Story = { args: { tone: 'done', children: 'Grant revoked. The member keeps their other grants.' } };
export const Warn: Story = {
  args: {
    tone: 'warn',
    children:
      'The instance lifetime ceiling shortened this credential. It expires earlier than the default asked for.',
  },
};
export const Info: Story = {
  args: {
    tone: 'info',
    children: 'Key definitions come from Git for this project. Declare keys with definitions apply.',
  },
};

export const WithAction: Story = {
  args: {
    children: 'Identity provider options could not be loaded.',
    action: (
      <Button type="button">Retry identity providers</Button>
    ),
  },
};

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, maxWidth: 480 }}>
      <Alert>Refused: the remote answered with a certificate that does not match the pin.</Alert>
      <Alert tone="done">Remote added. Its projects appear in the rail after the next refresh.</Alert>
      <Alert tone="warn">
        The instance lifetime ceiling shortened this credential. It expires earlier than the default
        asked for.
      </Alert>
      <Alert tone="info">
        Key definitions come from Git for this project. Declare keys with definitions apply.
      </Alert>
      <Alert action={<Button type="button">Replace with whole days</Button>}>
        Current maximum age is exact (90000 seconds), not whole days. The day editor is disabled so that exact value cannot look absent.
      </Alert>
    </div>
  ),
};

export const RolesMatchTone: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, maxWidth: 480 }}>
      <Alert>Refused: the remote answered with a certificate that does not match the pin.</Alert>
      <Alert tone="done">Remote added. Its projects appear in the rail after the next refresh.</Alert>
      <Alert tone="warn">The provider shortened the lifetime this credential asked for.</Alert>
      <Alert tone="info">Key definitions come from Git for this project.</Alert>
    </div>
  ),
  play: async ({ canvas }) => {
    // Only the refusal interrupts; the other three are polite.
    await expect(canvas.getByRole('alert')).toHaveTextContent(/refused/i);
    const polite = canvas.getAllByRole('status');
    await expect(polite).toHaveLength(3);
    await expect(polite[0]).toHaveTextContent(/remote added/i);
    await expect(polite[1]).toHaveTextContent(/shortened the lifetime/i);
    await expect(polite[2]).toHaveTextContent(/come from git/i);
  },
};
