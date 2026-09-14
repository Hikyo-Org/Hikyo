import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { ChromeIdentityControls } from './ChromeIdentityControls.tsx';

// Outside prototype mode (import.meta.env.MODE !== 'prototype') the component
// is read-only: a preview plus its children, with no way to change the
// identity from the browser. That is the mode Storybook always runs in.
const meta = {
  component: ChromeIdentityControls,
  tags: ['ai-generated'],
  args: {
    identityId: 'org_01989abc-def0-7123-8123-000000000000',
    name: 'Acme Corp',
    kind: 'org',
    children: <p>Used across the workspace switcher and project rail.</p>,
  },
} satisfies Meta<typeof ChromeIdentityControls>;

export default meta;
type Story = StoryObj<typeof meta>;

export const OrgIdentity: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByLabelText('Organisation identity preview: AC'),
    ).toBeVisible();
    await expect(canvas.getByText('Used across the workspace switcher and project rail.')).toBeVisible();
  },
};

export const ProjectIdentity: Story = {
  args: {
    identityId: 'prj_01989abc-def0-7123-8123-000000000000',
    name: 'Payments',
    kind: 'project',
    children: <p>Shown on the project's rail tile.</p>,
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Project identity preview: PA')).toBeVisible();
    await expect(canvas.getByText("Shown on the project's rail tile.")).toBeVisible();
  },
};
