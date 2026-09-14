import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, waitFor } from 'storybook/test';

import { MatrixPublishSheet } from './MatrixPublishSheet.tsx';

// Every story reuses one publish target (env `env_a`, revision r1) and varies
// only problems/protectedEnvironmentIds.
const meta = {
  component: MatrixPublishSheet,
  tags: ['ai-generated'],
  args: {
    refData: { org: 'org_a', project: 'prj_a' },
    environments: [
      {
        id: 'env_a',
        org_id: 'org_a',
        project_id: 'prj_a',
        name: 'preview',
        display_order: 0,
        created_at: '2026-09-12T00:00:00Z',
      },
    ],
    revisions: new Map([['env_a', 1n]]),
    pendingByEnvironment: new Map([
      [
        'env_a',
        [
          {
            versionId: 'pcv_a',
            keyId: 'key_a',
            name: 'PORT',
            classification: 'config',
            operation: 'set',
            configPreview: '8080',
          },
        ],
      ],
    ]),
    problems: [],
    protectedEnvironmentIds: [],
    busy: false,
    mutationError: null,
    onPublish: fn(),
    onClose: fn(),
  },
} satisfies Meta<typeof MatrixPublishSheet>;

export default meta;
type Story = StoryObj<typeof meta>;

// Unprotected, no problems: the environment is ready and publishing fires the
// callback with its id. No protected targets means `run` resolves without a
// reveal fetch, so clicking Publish here is safe.
export const Default: Story = {
  play: async ({ args, canvas }) => {
    args.onPublish.mockClear();
    await expect(canvas.getByText('✓ ready')).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: /Publish selected/ }));
    await waitFor(() => expect(args.onPublish).toHaveBeenCalledWith(['env_a']));
  },
};

// A validation problem on the environment blocks its checkbox and disables
// Publish; the sheet names the offending key.
export const Blocked: Story = {
  args: {
    problems: [
      {
        keyId: 'key_a',
        keyName: 'PORT',
        groupId: '',
        environmentId: 'env_a',
        kind: 'validation',
        message: 'PORT must be a valid port number',
      },
    ],
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent('✕ Publish blocked: PORT in preview');
    await expect(canvas.getByRole('button', { name: /Publish selected/ })).toBeDisabled();
  },
};

// A protected environment gates Publish behind an explicit confirmation; ticking
// it enables the button. We stop at enabling — clicking would open the reveal
// ceremony and fetch, which this pure-props story does not stub.
export const Protected: Story = {
  args: {
    protectedEnvironmentIds: ['env_a'],
  },
  play: async ({ canvas }) => {
    const publish = canvas.getByRole('button', { name: /Publish selected/ });
    await expect(publish).toBeDisabled();
    await userEvent.click(
      canvas.getByRole('checkbox', { name: /I confirm publishing to protected/ }),
    );
    await expect(publish).toBeEnabled();
  },
};
