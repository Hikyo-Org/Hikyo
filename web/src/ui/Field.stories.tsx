import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { tokenColor } from '../../.storybook/tokens.ts';
import { Field } from './Field.tsx';

// The composed field is normally reached through Input, Select and Textarea;
// this page shows the layout those share with a bare control, so the label,
// hint and error rows can be designed once.
const meta = {
  title: 'Design system/Field',
  id: 'ui-field',
  component: Field,
  tags: ['ai-generated'],
  args: {
    label: 'Display name',
    children: (control) => <input {...control} defaultValue="Dana Jacobs" />,
  },
} satisfies Meta<typeof Field>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Display name')).toBeVisible();
  },
};

// A hint describes the control and sits under it in secondary ink.
export const WithHint: Story = {
  args: { hint: 'Shown to other members of the organisation.' },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Display name')).toHaveAccessibleDescription(
      'Shown to other members of the organisation.',
    );
  },
};

// An error marks the control invalid and is announced as an alert.
export const WithError: Story = {
  args: { hint: 'Shown to other members of the organisation.', error: 'A display name is required.' },
  play: async ({ canvas }) => {
    const control = canvas.getByLabelText('Display name');
    await expect(control).toHaveAttribute('aria-invalid', 'true');
    await expect(canvas.getByRole('alert')).toHaveTextContent('A display name is required.');
    // Glyph-carried: the alert mark, decorative beside the words.
    const mark = canvas.getByRole('alert').querySelector('svg.alert__glyph');
    await expect(mark).toHaveAttribute('data-glyph', 'alert');
    await expect(mark).toHaveAttribute('aria-hidden', 'true');
    await expect(control).toHaveAccessibleDescription(/Shown to other members.*A display name is required/);
  },
};

// One gap governs label-to-control and control-to-hint/error, token-based:
// every route's field uses this value, so a drift back to a literal pixel
// size shows up here.
export const Gaps: Story = {
  args: { hint: 'Shown to other members of the organisation.' },
  play: async ({ canvasElement }) => {
    const field = canvasElement.querySelector('.field');
    if (field === null) throw new Error('no .field in canvas');
    const space1 = getComputedStyle(document.documentElement).getPropertyValue('--space-1').trim();
    await expect(getComputedStyle(field).gap).toBe(space1);
  },
};

// A trailing "(...)" is a note, not the name: it renders in tertiary ink,
// distinct from the label text beside it, while the accessible name stays
// the whole string.
export const LabelWithNote: Story = {
  args: { label: 'Kind (immutable)' },
  play: async ({ canvas, canvasElement }) => {
    const control = canvas.getByLabelText('Kind (immutable)');
    await expect(control).toBeVisible();
    const label = canvasElement.querySelector('label');
    const note = canvasElement.querySelector('.field__label-note');
    if (label === null || note === null) throw new Error('no label/.field__label-note rendered');
    await expect(note).toHaveTextContent('(immutable)');
    await expect(getComputedStyle(note).color).not.toBe(getComputedStyle(label).color);
    // The note is the tertiary ink, and the label's text is unchanged.
    await expect(getComputedStyle(note).color).toBe(tokenColor('--tx-faint'));
    await expect(label.textContent).toBe('Kind (immutable)');
  },
};

// Only a trailing parenthetical is a note: one mid-label stays the name.
export const LabelWithInnerParenthesis: Story = {
  args: { label: 'Lifetime (days) ceiling' },
  play: async ({ canvas, canvasElement }) => {
    await expect(canvas.getByLabelText('Lifetime (days) ceiling')).toBeVisible();
    await expect(canvasElement.querySelector('.field__label-note')).toBeNull();
  },
};
