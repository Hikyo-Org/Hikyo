import type { Meta, StoryObj } from '@storybook/react-vite';
import type { SyntheticEvent } from 'react';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { Alert } from './Alert.tsx';
import { Button } from './Button.tsx';
import { Checkbox } from './Checkbox.tsx';
import { Dialog } from './Dialog.tsx';
import { Input } from './Input.tsx';
import { Select } from './Select.tsx';

const meta = {
  component: Dialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    title: 'Revoke this connection?',
    lede: 'The workspace loses access now. Its next request is refused and audited; nothing else changes.',
    onCancel: fn(),
    actions: (
      <>
        <Button type="button">Keep it</Button>
        <Button variant="danger" type="button">
          Revoke connection
        </Button>
      </>
    ),
  },
} satisfies Meta<typeof Dialog>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Decision: Story = {};

export const WithRefusal: Story = {
  args: {
    children: <Alert>The remote refused the revocation: its certificate no longer matches the pin.</Alert>,
  },
};

export const Wide: Story = {
  args: {
    size: 'wide',
    title: 'New key',
    lede: 'Declared once; every environment carries a cell for it.',
    children: (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Input label="Name" mono defaultValue="DATABASE_URL" hint="Uppercase, digits and underscores." />
        <Select label="Type" defaultValue="secret">
          <option value="plain">Plain</option>
          <option value="secret">Secret</option>
        </Select>
        <Checkbox label="Required in every environment" />
      </div>
    ),
    actions: (
      <>
        <Button type="button">Cancel</Button>
        <Button variant="primary" type="button">
          Create key
        </Button>
      </>
    ),
  },
};

export const MustAcknowledge: Story = {
  args: {
    title: 'Your new recovery codes',
    lede: 'Shown once. Store them, then acknowledge below; Escape does not dismiss this.',
    onCancel: fn((event: SyntheticEvent<HTMLDialogElement>) => event.preventDefault()),
    children: <Checkbox label="I have stored these somewhere safe." />,
    actions: (
      <Button variant="primary" type="button">
        Done
      </Button>
    ),
  },
};

/** Both sizes on one page. Each is its own modal, so the second one inerts the first; use the docs frame. */
export const AllStates: Story = {
  render: (args) => (
    <>
      <Dialog {...args} />
      <Dialog {...args} title="Wide" size="wide" />
    </>
  ),
};

/** The scrim is a way out; the dialog's own padding is not. */
export const BackdropClick: Story = {
  args: { onBackdropClick: fn() },
  play: async ({ canvas, args }) => {
    const dialog = canvas.getByRole('dialog', { name: 'Revoke this connection?' });
    const box = dialog.getBoundingClientRect();
    const click = (clientX: number, clientY: number) => {
      dialog.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX, clientY }));
    };
    // Inside the box: the padding around the title is still the dialog.
    click(box.left + 2, box.top + 2);
    await expect(args.onBackdropClick).not.toHaveBeenCalled();
    // Outside it: the scrim.
    click(box.left - 20, box.top - 20);
    await expect(args.onBackdropClick).toHaveBeenCalledTimes(1);
  },
};

export const IsModalAndLabelled: Story = {
  play: async ({ canvas, args }) => {
    const dialog = canvas.getByRole('dialog', { name: 'Revoke this connection?' });
    await expect(dialog).toBeVisible();
    // Opened through showModal(): the platform's focus trap, inert page and top layer.
    await expect(dialog.matches(':modal')).toBe(true);
    // One control height everywhere: a dialog button is the page button.
    const revoke = canvas.getByRole('button', { name: 'Revoke connection' });
    const control = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--control'));
    await expect(Math.round(revoke.getBoundingClientRect().height)).toBe(Math.round(control));
    // A synthetic Escape does not reach the platform's cancel path; dispatch
    // the event the platform would, and assert the handler is wired to it.
    dialog.dispatchEvent(new Event('cancel', { cancelable: true }));
    await expect(args.onCancel).toHaveBeenCalled();
  },
};
