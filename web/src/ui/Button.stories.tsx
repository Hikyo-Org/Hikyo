import type { Meta, StoryObj } from '@storybook/react-vite';
import { fn } from 'storybook/test';

import { design } from '../../.storybook/design.ts';
import { Button } from './Button.tsx';
import { ThemeIcon } from './ThemeIcon.tsx';

const meta = {
  title: 'Design system/Button',
  id: 'ui-button',
  component: Button,
  parameters: { docs: { description: { component: 'Native button with shared action styling and an accessible-name requirement for icon-only controls. Secondary is the default; type preserves native form behavior.' } } },
  tags: ['ai-generated'],
  args: { children: 'Save changes', onClick: fn() },
  argTypes: {
    variant: {
      description: 'Action priority: primary leads, danger deletes, quiet is tertiary.',
      control: 'select', options: ['secondary', 'primary', 'danger', 'quiet'],
      table: { type: { summary: "'primary' | 'secondary' | 'danger' | 'quiet'" }, defaultValue: { summary: 'secondary' } },
    },
    children: { description: 'Visible action label, or graphic for an icon-only control.', control: 'text', table: { type: { summary: 'ReactNode' } } },
    type: { description: 'Native button type. Omitted preserves submit behavior inside a form.', control: 'select', options: ['button', 'submit', 'reset'], table: { type: { summary: "'button' | 'submit' | 'reset'" }, defaultValue: { summary: 'native submit in forms' } } },
    disabled: { description: 'Native disabled control; cannot receive focus or activate.', control: 'boolean', table: { type: { summary: 'boolean' }, defaultValue: { summary: 'false' } } },
    icon: { description: 'Icon-only control. Requires an accessible aria-label.', control: 'boolean', table: { type: { summary: 'boolean' }, defaultValue: { summary: 'false' } } },
    'aria-label': { description: 'Required accessible name when icon is true.', control: 'text', table: { type: { summary: 'string' } } },
    onClick: { description: 'Native click handler.', control: false, table: { type: { summary: 'MouseEventHandler<HTMLButtonElement>' } } },
  },
} satisfies Meta<typeof Button>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Secondary: Story = { parameters: { design: design('Button/Secondary') } };
export const Primary: Story = { args: { variant: 'primary' }, parameters: { design: design('Button/Primary') } };
export const Disabled: Story = { args: { disabled: true }, parameters: { design: design('Button/Disabled') } };
export const Icon: Story = {
  args: { icon: true, 'aria-label': 'Toggle theme', children: <ThemeIcon dark /> },
  parameters: { design: design('Button/Icon') },
};

// The one story the design tuning actually needs: every variant side by side,
// swept by the theme toolbar for light/dark.

export const AllVariants: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', alignItems: 'start' }}>
      {[
        { caption: 'Secondary (default)', control: <Button>Save changes</Button> },
        { caption: 'Primary action', control: <Button variant="primary">Save changes</Button> },
        { caption: 'Destructive action', control: <Button variant="danger">Delete adapter</Button> },
        { caption: 'Tertiary action', control: <Button variant="quiet">History</Button> },
        { caption: 'Disabled', control: <Button disabled>Save changes</Button> },
        { caption: 'Primary disabled', control: <Button variant="primary" disabled>Save changes</Button> },
        { caption: 'Named icon', control: <Button icon aria-label="Toggle theme"><ThemeIcon dark /></Button> },
      ].map(({ caption, control }) => (
        <figure key={caption} style={{ margin: 0, display: 'grid', gap: 8 }}>
          <figcaption>{caption}</figcaption>
          {control}
        </figure>
      ))}
    </div>
  ),
};
