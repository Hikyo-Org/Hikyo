import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import {
  Alert,
  ConsequencesDialog,
  DisplayOnceCopy,
  Done,
  Explain,
  JumpIndex,
  Panel,
  TypedNameConfirm,
} from './Sections.tsx';

// Panel is the primary component; meta.args covers its required props so
// render-only stories for the sibling primitives don't require an `args` key.
const meta = {
  component: Panel,
  tags: ['ai-generated'],
  args: {
    id: 'panel',
    title: 'Sessions',
    children: 'Every device with an active session and the authority to revoke it.',
  },
} satisfies Meta<typeof Panel>;

export default meta;
type Story = StoryObj<typeof meta>;

export const DefaultPanel: Story = {};

export const DangerPanel: Story = { args: { danger: true, title: 'Danger zone' } };

export const QuestionPanel: Story = { args: { question: true, title: 'Open question' } };

export const TightPanel: Story = { args: { tight: true, title: 'Profile' } };

export const Jump: Story = {
  render: () => (
    <JumpIndex
      sections={[
        { id: 'profile', label: 'Profile' },
        { id: 'sessions', label: 'Sessions' },
        { id: 'danger', label: 'Danger zone' },
      ]}
    />
  ),
};

export const RefusalAlert: Story = {
  render: () => <Alert>That session no longer exists, so nothing was revoked.</Alert>,
};

export const DoneNotice: Story = {
  render: () => <Done>The session was revoked and cannot be resumed.</Done>,
};

export const Disclosure: Story = {
  render: () => (
    <Explain
      label="revocation"
      text="Revoking a session ends it immediately; re-granting is a new audited grant, not an undo."
    />
  ),
};

export const CopyOnce: Story = {
  // Clipboard write refuses in headless without a user gesture — a plain
  // rendered variant, no clipboard play.
  render: () => (
    <DisplayOnceCopy value="ABCD-EFGH-IJKL" success="Recovery code copied." />
  ),
};

export const Consequences: Story = {
  render: () => (
    <ConsequencesDialog
      titleId="ceremony-title"
      title="Delete this workspace"
      confirmLabel="Delete workspace"
      busyLabel="Deleting…"
      busy={false}
      failure={null}
      onCancel={fn()}
      onConfirm={fn()}
    >
      This deletes every environment and its history. It cannot be undone.
    </ConsequencesDialog>
  ),
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /delete this workspace/i })).toBeVisible();
  },
};

// Mandatory interaction: the destructive button arms only when the exact name
// is typed, and the armed state is reflected in the status text, not colour.
export const TypedConfirm: Story = {
  render: () => (
    <TypedNameConfirm
      label="Type the workspace name"
      expect="delete-me"
      action="Delete workspace"
      hint="This confirms you mean this exact workspace."
      busy={false}
      onConfirm={fn()}
    />
  ),
  play: async ({ canvas, userEvent }) => {
    const input = canvas.getByLabelText('Type the workspace name');
    const button = canvas.getByRole('button', { name: 'Delete workspace' });
    const status = canvas.getByRole('status');

    await userEvent.type(input, 'wrong');
    await expect(button).toBeDisabled();
    await expect(status).toHaveTextContent('Type delete-me exactly to enable delete workspace.');

    await userEvent.clear(input);
    await userEvent.type(input, 'delete-me');
    await expect(button).toBeEnabled();
    await expect(status).toHaveTextContent('The name matches. Delete workspace is now possible.');
  },
};
