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

// The keyboard model role="menu" promises: opening lands on the first enabled
// item, arrows move and wrap, Home/End jump, a disabled row is skipped, and
// Escape closes and hands focus back to the trigger.
export const KeyboardModel: Story = {
  args: {
    children: (
      <>
        <MenuItem>Rename</MenuItem>
        <MenuItem disabled>Duplicate</MenuItem>
        <MenuItem>Delete</MenuItem>
      </>
    ),
  },
  play: async ({ canvas, canvasElement }) => {
    const trigger = canvas.getByRole('button', { name: 'Row actions' });
    const panel = canvasElement.querySelector('[popover]');
    trigger.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(panel).toBeVisible());
    const rename = canvas.getByRole('menuitem', { name: 'Rename' });
    const remove = canvas.getByRole('menuitem', { name: 'Delete' });
    await waitFor(() => expect(rename).toHaveFocus());
    await userEvent.keyboard('{ArrowDown}');
    await expect(remove).toHaveFocus();
    await userEvent.keyboard('{ArrowDown}');
    await expect(rename).toHaveFocus();
    await userEvent.keyboard('{ArrowUp}');
    await expect(remove).toHaveFocus();
    await userEvent.keyboard('{Home}');
    await expect(rename).toHaveFocus();
    await userEvent.keyboard('{End}');
    await expect(remove).toHaveFocus();
    await userEvent.keyboard('{Escape}');
    await waitFor(() => expect(panel).not.toBeVisible());
    await expect(trigger).toHaveFocus();
  },
};
