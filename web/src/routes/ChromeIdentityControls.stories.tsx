import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { ChromeIdentityControls } from './ChromeIdentityControls.tsx';

// Outside prototype mode (import.meta.env.MODE !== 'prototype') the component
// is read-only until the page supplies `actions` (the editable stories below);
// without them it is a preview plus its children. Storybook always runs outside
// prototype mode.
const meta = {
  parameters: { docs: { description: { component: "Organisation and project identity previews used by navigation chrome. The production surface saves hue, content and image through the identity API when the page supplies actions, and is a read-only preview otherwise; prototype mode exposes browser-local hue, glyph and image choices. Every control has an accessible name." } } },
  title: 'Shared/ChromeIdentityControls',
  id: 'routes-chromeidentitycontrols',
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

const noop = () => Promise.resolve();

export const EditableOrg: Story = {
  args: {
    identity: { hue: 280, mark: 'AC' },
    actions: { setIdentity: noop, setImage: noop, clearImage: noop },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Hue 280')).toHaveAttribute('aria-pressed', 'true');
    await expect(canvas.getByLabelText(/^Content/)).toHaveValue('AC');
    await expect(canvas.getByText(/PNG, JPEG, WebP or SVG, replaces the monogram/)).toBeVisible();
  },
};

export const EditableProject: Story = {
  args: {
    identityId: 'prj_01989abc-def0-7123-8123-000000000000',
    name: 'Payments',
    kind: 'project',
    identity: { hue: 195, mark: null },
    actions: { setIdentity: noop, setImage: noop, clearImage: noop },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Use monogram')).toHaveAttribute('aria-pressed', 'true');
    await expect(canvas.getByText(/replaces the content/)).toBeVisible();
  },
};
