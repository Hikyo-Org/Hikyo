import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { expect, userEvent } from 'storybook/test';

import { Disclosure } from './Disclosure.tsx';

const meta = {
  title: 'Design system/Disclosure',
  id: 'ui-disclosure',
  component: Disclosure,
  tags: ['ai-generated'],
  args: {
    label: 'Pick single keys (3)',
    children: <p className="mono">DB_HOST, DB_USER, DB_PASSWORD</p>,
  },
} satisfies Meta<typeof Disclosure>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Closed: Story = {};
export const Open: Story = { args: { defaultOpen: true } };

// The chevron turns and the content shows; the state is aria-expanded, not only the glyph.
export const Toggles: Story = {
  play: async ({ canvas }) => {
    const toggle = canvas.getByRole('button', { name: 'Pick single keys (3)' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(canvas.getByText('DB_HOST, DB_USER, DB_PASSWORD')).not.toBeVisible();
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(canvas.getByText('DB_HOST, DB_USER, DB_PASSWORD')).toBeVisible();
    const content = document.getElementById(toggle.getAttribute('aria-controls') ?? '');
    await expect(content).toHaveTextContent('DB_PASSWORD');
    // A control like any other: the one control height.
    const control = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--control'));
    await expect(Math.round(toggle.getBoundingClientRect().height)).toBe(Math.round(control));
  },
};

export const Controlled: Story = {
  args: { label: 'Advanced', children: 'Content' },
  render: function Render(args) {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button type="button" onClick={() => setOpen(true)}>
          Open from outside
        </button>
        <Disclosure {...args} open={open} onOpenChange={setOpen} />
      </>
    );
  },
  play: async ({ canvas, userEvent }) => {
    const toggle = canvas.getByRole('button', { name: 'Advanced' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(canvas.getByRole('button', { name: 'Open from outside' }));
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  },
};
