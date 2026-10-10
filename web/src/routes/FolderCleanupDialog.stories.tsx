import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import type { FolderMove, FolderMoveOutcome } from '../api/catalogue.ts';
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
  title: 'Features/Values/FolderCleanupDialog',
  id: 'routes-foldercleanupdialog',
  component: FolderCleanupDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    proposals,
    existingFolders: ['Legacy'],
    busy: false,
    envName: (id: string) => (id === 'env_01989abc-def0-7123-8123-00000000000b' ? 'production' : id),
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

/** A move that gives someone new access through their access rules stays
 *  with a review action; the confirmation names who gains what. */
export const WideningRefused: Story = {
  args: {
    onApply: fn(async (moves: readonly FolderMove[]): Promise<readonly FolderMoveOutcome[]> =>
      moves.map((move) =>
        move.name === 'HIKYO_ARGON2_TIME' && move.confirmWidening === undefined
          ? {
              id: move.id,
              error: 'Not moved: this move gives people new access through their access rules.',
              widening: {
                count: 1,
                gainers: [
                  {
                    principal_id: 'usr_01989abc-def0-7123-8123-00000000000a',
                    principal_name: 'Dana Ruiz',
                    capability: 'reveal',
                    environments: ['env_01989abc-def0-7123-8123-00000000000b'],
                  },
                ],
              },
            }
          : { id: move.id, error: null },
      ),
    ),
  },
  play: async ({ canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole('button', { name: /move 2 key\(s\)/i }));
    await userEvent.click(await canvas.findByRole('button', { name: /review access for HIKYO_ARGON2_TIME/i }));
    await expect(await canvas.findByText('Dana Ruiz: Reveal (production)')).toBeVisible();
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
