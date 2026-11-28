import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { MatrixKeyCreate } from './MatrixKeyCreate.tsx';

const development = {
  id: 'env_01989abc-def0-7123-8123-000000000001',
  org_id: 'org_01989abc-def0-7123-8123-000000000000',
  project_id: 'prj_01989abc-def0-7123-8123-000000000000',
  name: 'development',
  display_order: 0,
  created_at: '2026-08-23T08:00:00Z',
};
const production = { ...development, id: 'env_01989abc-def0-7123-8123-000000000002', name: 'production', display_order: 1 };

const meta = {
  title: 'Features/Values/MatrixKeyCreate',
  id: 'routes-matrixkeycreate',
  component: MatrixKeyCreate,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    folders: ['app'],
    environments: [development, production],
    protectedEnvironmentIds: [production.id],
    initialFolder: 'app',
    existingKeyNames: ['EXISTING_KEY'],
    busy: false,
    mutationError: null,
    onClose: fn(),
    onCreate: fn(async () => undefined),
  },
} satisfies Meta<typeof MatrixKeyCreate>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /new key/i })).toBeVisible();
    await expect(canvas.getByLabelText('Group')).toHaveValue('app');
    await expect(canvas.getByLabelText('Key name')).toBeVisible();
    await expect(canvas.getByRole('button', { name: /^declare$/i })).toBeEnabled();
  },
};

// A name one edit away from an existing key: a caveat, not a refusal.
export const NearMiss: Story = {
  play: async ({ canvas, userEvent }) => {
    await userEvent.type(canvas.getByLabelText('Key name'), 'EXISTNG_KEY');
    await expect(canvas.getByText('Similar to existing key EXISTING_KEY. Continue if this is intentional.')).toBeVisible();
    await expect(canvas.getByRole('button', { name: /^declare$/i })).toBeEnabled();
  },
};

// A whitespace-only first value is empty after trimming, and the dialog says so.
export const WhitespaceOnlyFirstValue: Story = {
  play: async ({ canvas, userEvent }) => {
    await userEvent.type(canvas.getByLabelText('First value (optional)'), '   ');
    await expect(canvas.getByText(/This value is only whitespace, so trimming leaves it empty/)).toBeVisible();
  },
};

export const FolderPicker: Story = {
  args: { folders: ['app', 'database', 'empty-folder'], initialFolder: null },
  play: async ({ canvas, userEvent, args }) => {
    const folder = canvas.getByRole('combobox', { name: 'Group' });
    await userEvent.click(canvas.getByRole('button', { name: 'Show folders' }));
    await expect(canvas.getByRole('option', { name: 'empty-folder' })).toBeVisible();
    await userEvent.keyboard('{ArrowDown}{ArrowDown}{Enter}');
    await expect(folder).toHaveValue('database');
    await expect(args.onCreate).not.toHaveBeenCalled();
    await userEvent.click(canvas.getByRole('button', { name: 'Show folders' }));
    await userEvent.keyboard('{Escape}');
    await expect(canvas.getByRole('heading', { name: /new key/i })).toBeVisible();
    await expect(args.onClose).not.toHaveBeenCalled();
    await userEvent.clear(folder);
    await userEvent.type(folder, 'New services/日本語');
    await userEvent.tab();
    await userEvent.type(canvas.getByLabelText('Key name'), 'NEW_KEY');
    await userEvent.click(canvas.getByRole('button', { name: /^declare$/i }));
    await expect(args.onCreate).toHaveBeenCalledWith(expect.objectContaining({ folderPath: 'New services/日本語', name: 'NEW_KEY' }));
  },
};

export const PhoneFolderPicker: Story = {
  args: { folders: ['app', 'database', 'long/nested-folder-name-for-production'], initialFolder: null },
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Show folders' }));
    await expect(canvas.getByRole('option', { name: /long\/nested/ })).toBeVisible();
  },
};

export const ExistingFolderKeepsItsName: Story = {
  args: { folders: ['Database/Primary', 'Team keys/日本語'], initialFolder: null },
  play: async ({ canvas, userEvent, args }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Show folders' }));
    await userEvent.click(canvas.getByRole('option', { name: 'Team keys/日本語' }));
    await userEvent.type(canvas.getByLabelText('Key name'), 'NEW_KEY');
    await userEvent.click(canvas.getByRole('button', { name: /^declare$/i }));
    await expect(args.onCreate).toHaveBeenCalledWith(expect.objectContaining({ folderPath: 'Team keys/日本語' }));
  },
};

export const JsonInferredFromValue: Story = {
  play: async ({ canvas, userEvent }) => {
    await userEvent.selectOptions(canvas.getByLabelText('Type'), 'json');
    const infer = canvas.getByRole('button', { name: 'Infer schema from this value' });
    await expect(infer).toBeDisabled();
    await expect(canvas.getByText('Enter a valid JSON value to infer a schema from it.')).toBeVisible();

    await userEvent.click(canvas.getByLabelText('First value (optional)'));
    await userEvent.paste('{"host":"db.internal","port":5432}');
    await userEvent.click(infer);
    await expect(canvas.getAllByLabelText('Field name')).toHaveLength(2);
    await expect(canvas.getByLabelText('Schema document')).not.toHaveDisplayValue(/db\.internal|5432/);
    await expect(canvas.getByRole('button', { name: 'Replace schema from this value' })).toBeEnabled();
  },
};

export const JsonInferenceOffForSecret: Story = {
  play: async ({ canvas, userEvent }) => {
    await userEvent.selectOptions(canvas.getByLabelText('Type'), 'json');
    await userEvent.click(canvas.getByLabelText('First value (optional)'));
    await userEvent.paste('{"host":"db.internal"}');
    await userEvent.click(canvas.getByRole('button', { name: 'Infer schema from this value' }));
    await expect(canvas.getAllByLabelText('Field name')).toHaveLength(1);

    // Marking the key secret after inferring still removes the sampled names.
    await userEvent.click(canvas.getByRole('checkbox', { name: /secret/i }));
    await expect(canvas.queryAllByLabelText('Field name')).toHaveLength(0);
    await expect(canvas.getByText(/schema was removed/)).toBeVisible();
    await expect(canvas.getByLabelText('Schema document')).toHaveValue('');
    await expect(canvas.getByRole('button', { name: 'Infer schema from this value' })).toBeDisabled();
  },
};

// No control may push the sheet wider than a phone: the dialog scrolls down, never across.
const fitsPhone = async (canvasElement: HTMLElement) => {
  const dialog = canvasElement.ownerDocument.querySelector('dialog[open]');
  if (!(dialog instanceof HTMLElement)) throw new Error('dialog missing');
  await expect(dialog.scrollWidth).toBeLessThanOrEqual(dialog.clientWidth);
};

// The default string declaration at phone width: the two lengths still share a
// line, and the type select keeps the secret checkbox beside it.
export const PhoneStringConstraints: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, canvasElement }) => {
    const top = (label: string) => canvas.getByLabelText(label).getBoundingClientRect().top;
    await expect(top('Max length')).toBe(top('Min length'));
    const secret = canvas.getByRole('checkbox', { name: 'secret' }).getBoundingClientRect();
    const type = canvas.getByLabelText('Type').getBoundingClientRect();
    await expect(secret.top).toBeLessThan(type.bottom);
    await expect(canvas.getByLabelText('Pattern (RE2, whole value)')).toBeVisible();
    await fitsPhone(canvasElement);
  },
};

// A json key at phone width: the schema builder sits inside the sheet.
export const PhoneSchemaBuilder: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, canvasElement, userEvent }) => {
    await userEvent.selectOptions(canvas.getByLabelText('Type'), 'json');
    await userEvent.click(canvas.getByLabelText('First value (optional)'));
    await userEvent.paste('{"host":"db.internal","port":5432,"tls":{"mode":"verify-full"}}');
    await userEvent.click(canvas.getByRole('button', { name: 'Infer schema from this value' }));
    await expect(canvas.getAllByLabelText('Field name').length).toBeGreaterThan(2);
    await fitsPhone(canvasElement);
  },
};
