import type { Meta, StoryObj } from '@storybook/react-vite';
import { MemoryRouter, useLocation } from 'react-router';
import { expect, fn } from 'storybook/test';

import { openTab } from '../../.storybook/openTab.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import {
  ConsequencesDialog,
  DisplayOnceCopy,
  Explain,
  Panel,
  SectionTabs,
  TypedNameConfirm,
} from './Sections.tsx';

// Panel is the primary component; meta.args covers its required props so
// render-only stories for the sibling primitives don't require an `args` key.
const meta = {
  title: 'Shared/Sections',
  id: 'routes-sections',
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

const tabSections = [
  { id: 'demo-profile', label: 'Profile', content: <Panel id="demo-profile" title="Profile">Name and avatar.</Panel> },
  {
    id: 'demo-sessions',
    label: 'Sessions',
    anchors: ['demo-revoke'],
    content: (
      <Panel id="demo-sessions" title="Sessions">
        <p id="demo-revoke">Every device with an active session.</p>
      </Panel>
    ),
  },
  { id: 'demo-danger', label: 'Danger zone', content: <Panel id="demo-danger" title="Danger zone" danger>Delete.</Panel> },
];

function Search() {
  return <output data-testid="search">{useLocation().search}</output>;
}

/** A sectioned page body: tabs over its panels, the selection kept in `?tab=`. */
export const Tabbed: Story = {
  render: () => (
    <MemoryRouter>
      <SectionTabs label="Demo sections" idPrefix="demo" sections={tabSections} />
      <Search />
    </MemoryRouter>
  ),
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('tab', { name: 'Profile' })).toHaveAttribute('aria-selected', 'true');
    await expect(canvas.getByRole('heading', { name: 'Profile' })).toBeVisible();
    // The other sections stay mounted but hidden, so a half-filled form survives.
    await expect(canvas.queryByRole('heading', { name: 'Sessions' })).toBeNull();
    await openTab(canvas, 'Sessions');
    await expect(canvas.getByRole('heading', { name: 'Sessions' })).toBeVisible();
    await expect(canvas.getByTestId('search')).toHaveTextContent('?tab=sessions');
    // The first tab is the default and leaves no param.
    await openTab(canvas, 'Profile');
    await expect(canvas.getByTestId('search')).toBeEmptyDOMElement();
  },
};

/** An old `#fragment` link, here to an element inside Sessions, opens that tab. */
export const TabbedFromAnchor: Story = {
  render: () => (
    <MemoryRouter initialEntries={['/#demo-revoke']}>
      <SectionTabs label="Demo sections" idPrefix="demo" sections={tabSections} />
    </MemoryRouter>
  ),
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('tab', { name: 'Sessions' })).toHaveAttribute('aria-selected', 'true');
    await expect(canvas.getByRole('tabpanel', { name: 'Sessions' })).toBeVisible();
  },
};

/** One section has nothing to switch to: no tab bar, the section bare. */
export const TabbedSingleSection: Story = {
  render: () => (
    <MemoryRouter>
      <SectionTabs label="Demo sections" idPrefix="demo" sections={[tabSections[0], false]} />
    </MemoryRouter>
  ),
  play: async ({ canvas }) => {
    await expect(canvas.queryByRole('tablist')).toBeNull();
    await expect(canvas.getByRole('heading', { name: 'Profile' })).toBeVisible();
  },
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
  parameters: topLayerDocs,
  render: () => (
    <ConsequencesDialog
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
