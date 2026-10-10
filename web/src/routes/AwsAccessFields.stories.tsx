import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';

import { AwsAccessFields, emptyAwsAccess, type AwsAuthMode } from './AwsAccessFields.tsx';

function Example({ mode = 'assume-role' }: { mode?: AwsAuthMode }) {
  const [value, setValue] = useState({ ...emptyAwsAccess, mode });
  return <AwsAccessFields value={value} onChange={setValue} />;
}
const meta = {
  title: 'Features/Adapters/AwsAccessFields',
  component: Example,
  render: (args) => <Example key={args.mode ?? 'assume-role'} {...args} />,
  tags: ['ai-generated'],
  argTypes: { mode: { description: 'Initial AWS authentication mode. Changing this control resets the local fixture fields; the Authentication select edits the current descriptor.', table: { type: { summary: 'AwsAuthMode' }, defaultValue: { summary: 'assume-role' } }, control: 'select', options: ['assume-role', 'web-identity', 'ambient', 'static'] } },
  parameters: { docs: { description: { component: 'Controlled AWS descriptor fields. Modes reveal different fields; changing away from static clears the typed key. Examples start with empty fixture credentials.' } } },
} satisfies Meta<typeof Example>;
export default meta;
type Story = StoryObj<typeof meta>;
export const AssumeRole: Story = {};
export const WebIdentity: Story = { args: { mode: 'web-identity' } };
export const Ambient: Story = { args: { mode: 'ambient' } };
export const Static: Story = { args: { mode: 'static' } };
