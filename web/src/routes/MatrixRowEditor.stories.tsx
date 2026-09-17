import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { MatrixRowEditor } from './MatrixRowEditor.tsx';

type Props = Parameters<typeof MatrixRowEditor>[0];

const environmentId = 'env_01989abc-def0-7123-8123-123456789abc';
const environment: Props['rows'][number]['environment'] = {
  id: environmentId,
  org_id: 'org_01989abc-def0-7123-8123-123456789abc',
  project_id: 'prj_01989abc-def0-7123-8123-123456789abc',
  name: 'development',
  display_order: 0,
  created_at: '2026-08-23T08:00:00Z',
};
const keyRecord: Props['keyRecord'] = {
  id: 'key_01989abc-def0-7123-8123-123456789abc',
  org_id: environment.org_id,
  project_id: environment.project_id,
  name: 'LOG_LEVEL',
  folder_path: '',
  classification: 'config',
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string', allow_empty: true } },
  presence: {
    required_in: { mode: 'none' },
    forbidden_in: { mode: 'none' },
  },
  group_id: '',
  created_at: '2026-08-23T08:00:00Z',
};
const rows: Props['rows'] = [
  {
    environmentId,
    environment,
    protected: false,
    degraded: false,
    cell: {
      key_id: keyRecord.id,
      name: keyRecord.name,
      classification: 'config',
      set: true,
      revealed: true,
      value: 'published',
    },
    signal: undefined,
    draftPreview: undefined,
    problems: [],
  },
];

const meta = {
  component: MatrixRowEditor,
  tags: ['ai-generated'],
  // A config key needs no reveal window, so an empty response table is enough:
  // the harness supplies the QueryClientProvider and Router the editor mounts
  // under, and any stray fetch 404s loud.
  parameters: { app: { responses: [] }, ...topLayerDocs },
  args: {
    refData: { org: 'org-a', project: 'project-a' },
    keyRecord,
    environmentId,
    rows,
    busy: false,
    mutationError: null,
    onClose: fn(),
    onApply: fn(async () => undefined),
    onCopy: fn(),
  },
} satisfies Meta<typeof MatrixRowEditor>;

export default meta;
type Story = StoryObj<typeof meta>;

// The editor opens on the published value with Save disabled — nothing has
// changed yet.
export const Default: Story = {
  play: async ({ canvas }) => {
    // Named, not just present: the value control is labelled through ui/Field.
    await expect(canvas.getByRole('textbox', { name: 'development value' })).toHaveValue(
      'published',
    );
    await expect(canvas.getByRole('button', { name: /^Save/ })).toBeDisabled();
  },
};

// Editing the value enables Save and submits the change through onApply.
export const Edits: Story = {
  play: async ({ args, canvas }) => {
    const textarea = canvas.getByRole('textbox');
    await userEvent.clear(textarea);
    await userEvent.type(textarea, 'debug');
    const save = canvas.getByRole('button', { name: /^Save/ });
    await expect(save).toBeEnabled();
    await userEvent.click(save);
    await waitFor(() =>
      expect(args.onApply).toHaveBeenCalledWith([
        { environmentId, operation: 'set', value: 'debug' },
      ]),
    );
  },
};
