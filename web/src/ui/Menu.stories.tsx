import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, waitFor } from 'storybook/test';

import { Menu, MenuItem } from './Menu.tsx';

const meta = {
  component: Menu,
  tags: ['ai-generated'],
  args: {
    label: 'Row actions',
    children: (
      <>
        <MenuItem>Rename</MenuItem>
        <MenuItem>Duplicate</MenuItem>
        <MenuItem>Delete</MenuItem>
      </>
    ),
  },
} satisfies Meta<typeof Menu>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Opens: Story = {
  play: async ({ canvas, canvasElement }) => {
    // Top layer is render-only; the panel node stays in the story subtree.
    // Assert visibility (not just `:popover-open`): the panel inherits
    // `.menu { display: flex }`, so a closed popover must be re-hidden by
    // `.menu--pop:not(:popover-open)`, `:popover-open` alone can't catch that.
    const panel = canvasElement.querySelector('[popover]');
    await expect(panel).not.toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Row actions' }));
    await waitFor(() => expect(panel).toBeVisible());
  },
};

// Selecting a row runs its handler and dismisses the menu, the whole point of
// the popover over the bespoke account-menu state machine.
const onDelete = fn();
export const Selects: Story = {
  args: {
    children: (
      <>
        <MenuItem>Rename</MenuItem>
        <MenuItem onClick={onDelete}>Delete</MenuItem>
      </>
    ),
  },
  play: async ({ canvas, canvasElement }) => {
    onDelete.mockClear();
    await userEvent.click(canvas.getByRole('button', { name: 'Row actions' }));
    const panel = canvasElement.querySelector('[popover]');
    await waitFor(() => expect(panel).toBeVisible());
    await userEvent.click(canvas.getByRole('menuitem', { name: 'Delete' }));
    await expect(onDelete).toHaveBeenCalled();
    await waitFor(() => expect(panel).not.toBeVisible());
  },
};

// The APG menu button contract: Enter/Space on the trigger opens and puts
// focus on the first item, arrows move with wrap, Home/End jump, Escape closes
// and returns focus to the trigger, and `aria-expanded` tracks all of it.
export const Keyboard: Story = {
  play: async ({ canvas, canvasElement }) => {
    const trigger = canvas.getByRole('button', { name: 'Row actions' });
    const panel = canvasElement.querySelector('[popover]');
    await expect(trigger).toHaveAttribute('aria-expanded', 'false');

    trigger.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(panel).toBeVisible());
    await waitFor(() => expect(canvas.getByRole('menuitem', { name: 'Rename' })).toHaveFocus());
    await expect(trigger).toHaveAttribute('aria-expanded', 'true');

    await userEvent.keyboard('{ArrowDown}');
    await expect(canvas.getByRole('menuitem', { name: 'Duplicate' })).toHaveFocus();
    await userEvent.keyboard('{ArrowDown}{ArrowDown}');
    await expect(canvas.getByRole('menuitem', { name: 'Rename' })).toHaveFocus();
    await userEvent.keyboard('{ArrowUp}');
    await expect(canvas.getByRole('menuitem', { name: 'Delete' })).toHaveFocus();
    await userEvent.keyboard('{Home}');
    await expect(canvas.getByRole('menuitem', { name: 'Rename' })).toHaveFocus();
    await userEvent.keyboard('{End}');
    await expect(canvas.getByRole('menuitem', { name: 'Delete' })).toHaveFocus();

    await userEvent.keyboard('{Escape}');
    await waitFor(() => expect(panel).not.toBeVisible());
    await waitFor(() => expect(trigger).toHaveFocus());
    await expect(trigger).toHaveAttribute('aria-expanded', 'false');

    // Space opens too, and ArrowDown on the closed trigger is the APG's
    // third way in.
    await userEvent.keyboard(' ');
    await waitFor(() => expect(canvas.getByRole('menuitem', { name: 'Rename' })).toHaveFocus());
    await userEvent.keyboard('{Escape}');
    await waitFor(() => expect(trigger).toHaveFocus());
    await userEvent.keyboard('{ArrowDown}');
    await waitFor(() => expect(canvas.getByRole('menuitem', { name: 'Rename' })).toHaveFocus());
    await userEvent.keyboard('{Escape}');
    await waitFor(() => expect(trigger).toHaveFocus());
  },
};

// Tab leaves: the menu closes and focus moves on past the trigger instead of
// being trapped in, or snapped back to, the menu.
export const TabLeaves: Story = {
  render: (args) => (
    <>
      <Menu {...args} />
      <button type="button" className="btn">After</button>
    </>
  ),
  play: async ({ canvas, canvasElement }) => {
    const trigger = canvas.getByRole('button', { name: 'Row actions' });
    const panel = canvasElement.querySelector('[popover]');
    trigger.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(canvas.getByRole('menuitem', { name: 'Rename' })).toHaveFocus());

    await userEvent.tab();
    await waitFor(() => expect(panel).not.toBeVisible());
    await expect(canvas.getByRole('button', { name: 'After' })).toHaveFocus();
    await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  },
};

// A keyboard selection returns focus to the trigger, like Escape does.
export const SelectsByKeyboard: Story = {
  args: {
    children: (
      <>
        <MenuItem>Rename</MenuItem>
        <MenuItem onClick={onDelete}>Delete</MenuItem>
      </>
    ),
  },
  play: async ({ canvas, canvasElement }) => {
    onDelete.mockClear();
    const trigger = canvas.getByRole('button', { name: 'Row actions' });
    const panel = canvasElement.querySelector('[popover]');
    trigger.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(canvas.getByRole('menuitem', { name: 'Rename' })).toHaveFocus());
    await userEvent.keyboard('{End}{Enter}');
    await expect(onDelete).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(panel).not.toBeVisible());
    await waitFor(() => expect(trigger).toHaveFocus());
  },
};
