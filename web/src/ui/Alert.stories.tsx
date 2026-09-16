import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Alert } from './Alert.tsx';

const meta = {
  component: Alert,
  tags: ['ai-generated'],
  args: { children: 'That username and password did not match. Check both and try again.' },
  argTypes: { tone: { control: 'select', options: ['danger', 'done'] } },
} satisfies Meta<typeof Alert>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Danger: Story = {};
export const Done: Story = { args: { tone: 'done', children: 'Grant revoked. The member keeps their other grants.' } };

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, maxWidth: 480 }}>
      <Alert>Refused: the remote answered with a certificate that does not match the pin.</Alert>
      <Alert tone="done">Remote added. Its projects appear in the rail after the next refresh.</Alert>
    </div>
  ),
};

export const RolesMatchTone: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, maxWidth: 480 }}>
      <Alert>Refused: the remote answered with a certificate that does not match the pin.</Alert>
      <Alert tone="done">Remote added. Its projects appear in the rail after the next refresh.</Alert>
    </div>
  ),
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent(/refused/i);
    await expect(canvas.getByRole('status')).toHaveTextContent(/remote added/i);
  },
};
