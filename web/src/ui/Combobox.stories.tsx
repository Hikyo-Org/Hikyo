import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { expect, fn, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { Combobox } from './Combobox.tsx';
import { Field } from './Field.tsx';

function Example(args: Parameters<typeof Combobox>[0]) {
  const [value, setValue] = useState(args.value);
  return (
    <Field label="Folder" hint="Choose an existing folder or type a new folder name.">
      {(control) => <Combobox {...args} {...control} value={value} onValueChange={setValue} />}
    </Field>
  );
}

const meta = {
  title: 'Design system/Combobox',
  component: Combobox,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    docs: {
      ...topLayerDocs.docs,
      description: { component: 'Editable suggestions with a visible picker, filtering and manual selection. Arrow keys explore without changing the input; Enter selects, Escape dismisses and Tab preserves custom text. The popup sits above dialogs and scrolling surfaces.' },
    },
  },
  args: {
    value: '',
    onValueChange: fn(),
    options: ['app', 'database', 'database/replica', 'logging'],
    optionsLabel: 'folders',
    className: 'mono',
    emptyMessage: 'No matching folders. You can use the new folder name.',
  },
  render: (args) => <Example {...args} />,
} satisfies Meta<typeof Combobox>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const ExistingAndNew: Story = {
  play: async ({ canvas, userEvent }) => {
    const input = canvas.getByRole('combobox', { name: 'Folder' });
    await userEvent.click(canvas.getByRole('button', { name: 'Show folders' }));
    await expect(canvas.getAllByRole('option')).toHaveLength(4);
    await userEvent.click(canvas.getByRole('option', { name: /^database$/ }));
    await expect(input).toHaveValue('database');
    await expect(input).toHaveFocus();
    await userEvent.clear(input);
    await userEvent.type(input, 'rep');
    await expect(canvas.getAllByRole('option')).toHaveLength(1);
    await expect(canvas.getByRole('option')).toHaveTextContent('database/replica');
    await userEvent.keyboard('{ArrowDown}{Enter}');
    await expect(input).toHaveValue('database/replica');
    await userEvent.clear(input);
    await userEvent.type(input, 'new/folder');
    await expect(canvas.getByRole('status')).toHaveTextContent('No matching folders');
    await userEvent.tab();
    await expect(input).toHaveValue('new/folder');
    await expect(input).toHaveAttribute('aria-expanded', 'false');
  },
};

export const Keyboard: Story = {
  args: { value: 'custom' },
  play: async ({ canvas, userEvent }) => {
    const input = canvas.getByRole('combobox', { name: 'Folder' });
    await userEvent.tab();
    await expect(input).toHaveFocus();
    await userEvent.keyboard('{ArrowDown}');
    await expect(input).toHaveValue('custom');
    await expect(canvas.getByRole('option', { name: /^app$/ })).toHaveAttribute('aria-selected', 'true');
    await userEvent.keyboard('{Escape}');
    await expect(input).toHaveValue('custom');
    await expect(input).toHaveAttribute('aria-expanded', 'false');
    await userEvent.keyboard('{ArrowUp}{Enter}');
    await expect(input).toHaveValue('logging');
    await expect(input).toHaveFocus();
    await waitFor(() => expect(input).toHaveAttribute('aria-expanded', 'false'));
  },
};

export const Empty: Story = {
  args: { options: [] },
  play: async ({ canvas, userEvent }) => {
    await userEvent.type(canvas.getByRole('combobox'), 'first-folder');
    await expect(canvas.getByRole('status')).toBeVisible();
    await userEvent.keyboard('{Escape}');
    await expect(canvas.getByRole('combobox')).toHaveValue('first-folder');
  },
};

export const Disabled: Story = {
  args: { disabled: true, value: 'app' },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('combobox')).toBeDisabled();
    await expect(canvas.getByRole('button')).toBeDisabled();
  },
};

export const Phone: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  args: { options: ['app', 'database/long-folder-name-for-production-replicas', 'logging'] },
  play: async ({ canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Show folders' }));
    await expect(canvas.getByRole('option', { name: /database/ })).toBeVisible();
  },
};
