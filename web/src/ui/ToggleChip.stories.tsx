import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { expect, userEvent } from 'storybook/test';

import { ToggleChip } from './ToggleChip.tsx';

const meta = {
  component: ToggleChip,
  tags: ['ai-generated'],
  args: { pressed: false, mono: true, children: 'staging' },
  argTypes: { mode: { control: 'select', options: ['include', 'exclude'] } },
} satisfies Meta<typeof ToggleChip>;

export default meta;
type Story = StoryObj<typeof meta>;

export const NotIncluded: Story = {};
export const Included: Story = { args: { pressed: true } };
// Exclude mode, untouched: in because nothing left it out.
export const Implied: Story = { args: { mode: 'exclude' } };
export const LeftOut: Story = { args: { mode: 'exclude', pressed: true, children: 'prod' } };
export const Disabled: Story = { args: { mode: 'exclude', disabled: true, children: 'web' } };

function ExcludeSet() {
  const [out, setOut] = useState<string[]>(['prod']);
  return (
    <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
      {['dev', 'staging', 'prod'].map((env) => (
        <ToggleChip key={env} mono mode="exclude" pressed={out.includes(env)} onClick={() => setOut(out.includes(env) ? out.filter((e) => e !== env) : [...out, env])}>
          {env}
        </ToggleChip>
      ))}
    </div>
  );
}

// "All, except...": every chip is in until tapped; the tap is named in the accessible name.
export const ExcludeToggles: Story = {
  render: () => <ExcludeSet />,
  play: async ({ canvas }) => {
    const prod = canvas.getByRole('button', { name: 'prod (left out)' });
    await expect(prod).toHaveAttribute('aria-pressed', 'true');
    await userEvent.click(canvas.getByRole('button', { name: 'dev (included)' }));
    await expect(canvas.getByRole('button', { name: 'dev (left out)' })).toHaveAttribute('aria-pressed', 'true');
    await userEvent.click(prod);
    await expect(canvas.getByRole('button', { name: 'prod (included)' })).toHaveAttribute('aria-pressed', 'false');
  },
};
