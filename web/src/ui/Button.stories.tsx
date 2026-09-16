import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { design } from '../../.storybook/design.ts';
import { Button } from './Button.tsx';

const meta = {
  component: Button,
  tags: ['ai-generated'],
  args: { children: 'Save changes', onClick: fn() },
  argTypes: { variant: { control: 'select', options: ['secondary', 'primary'] } },
} satisfies Meta<typeof Button>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Secondary: Story = { parameters: { design: design('Button/Secondary') } };
export const Primary: Story = { args: { variant: 'primary' }, parameters: { design: design('Button/Primary') } };
export const Disabled: Story = { args: { disabled: true }, parameters: { design: design('Button/Disabled') } };
export const Icon: Story = {
  args: { icon: true, 'aria-label': 'Toggle theme', children: '☾' },
  parameters: { design: design('Button/Icon') },
};

export const Clicks: Story = {
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole('button'));
    await expect(args.onClick).toHaveBeenCalled();
  },
};

// The one story the design tuning actually needs: every variant side by side,
// swept by the theme toolbar for light/dark.
export const AllVariants: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'center' }}>
      <Button>Secondary</Button>
      <Button variant="primary">Primary</Button>
      <Button disabled>Disabled</Button>
      <Button variant="primary" disabled>
        Primary disabled
      </Button>
      <Button icon aria-label="Icon only">
        ☾
      </Button>
    </div>
  ),
};
