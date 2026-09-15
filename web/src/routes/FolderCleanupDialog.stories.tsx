import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import type { FolderMoveOutcome } from '../api/catalogue.ts';
import { FolderCleanupDialog } from './FolderCleanupDialog.tsx';
import type { FolderProposal } from './folder-cleanup.ts';

const proposals: readonly FolderProposal[] = [
  { id: 'id_HIKYO_ARGON2_TIME', name: 'HIKYO_ARGON2_TIME', folder: 'Argon2' },
  { id: 'id_HIKYO_ARGON2_MEMORY_KIB', name: 'HIKYO_ARGON2_MEMORY_KIB', folder: 'Argon2' },
  { id: 'id_HIKYO_EXTERNAL_ORIGIN', name: 'HIKYO_EXTERNAL_ORIGIN', folder: '' },
];

const outcomes: readonly FolderMoveOutcome[] = proposals
  .filter((proposal) => proposal.folder !== '')
  .map((proposal) => ({ id: proposal.id, error: null }));

const meta = {
  component: FolderCleanupDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    proposals,
    existingFolders: ['Legacy'],
    busy: false,
    onApply: fn(async () => outcomes),
    onClose: fn(),
  },
} satisfies Meta<typeof FolderCleanupDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('heading', { name: /cleanup: group keys into folders/i }),
    ).toBeVisible();
    await expect(canvas.getByLabelText('Move HIKYO_ARGON2_TIME')).toBeChecked();
    await expect(canvas.getByLabelText('Move HIKYO_EXTERNAL_ORIGIN')).not.toBeChecked();
    await expect(canvas.getByRole('button', { name: /move 2 key\(s\)/i })).toBeEnabled();
  },
};

export const Busy: Story = {
  args: { busy: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: /moving…/i })).toBeDisabled();
    await expect(canvas.getByLabelText('Move HIKYO_ARGON2_TIME')).toBeDisabled();
    await expect(canvas.getByRole('button', { name: /^cancel$/i })).toBeDisabled();
  },
};
