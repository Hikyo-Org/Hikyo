import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { SidebarLinkItem } from './Shell.tsx';

type Props = Parameters<typeof SidebarLinkItem>[0];
type Link = Props['link'];

const settingsLink: Link = {
  id: 'settings',
  label: 'Settings',
  to: '/orgs/acme/settings',
  disabledReason: null,
};

const meta = {
  component: SidebarLinkItem,
  tags: ['ai-generated'],
  // The row renders a router Link/NavLink; the app harness supplies the
  // MemoryRouter. No fetch, so an empty response table is enough.
  parameters: { app: { responses: [] } },
  args: {
    link: settingsLink,
    onNavigate: fn(),
  },
} satisfies Meta<typeof SidebarLinkItem>;

export default meta;
type Story = StoryObj<typeof meta>;

// A plain nav link on an unrelated route: rendered, not current.
export const Default: Story = {
  play: async ({ canvas }) => {
    const link = await canvas.findByRole('link', { name: 'Settings' });
    await expect(link).toHaveAttribute('href', '/orgs/acme/settings');
    await expect(link).not.toHaveAttribute('aria-current');
  },
};

// On its own route, NavLink marks itself the current page.
export const Active: Story = {
  parameters: { app: { responses: [], path: '/orgs/acme/settings' } },
  play: async ({ canvas }) => {
    const link = await canvas.findByRole('link', { name: 'Settings' });
    await expect(link).toHaveAttribute('aria-current', 'page');
  },
};

// A local-only surface renders as a disabled span, not a link.
export const Disabled: Story = {
  args: {
    link: {
      id: 'members',
      label: 'Members',
      to: '/orgs/acme/members',
      disabledReason: 'Members is local-instance only',
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/members · local only/i)).toBeVisible();
    await expect(canvas.queryByRole('link')).toBeNull();
  },
};

// The `members` row does its own active-matching: on the bare members path (no
// `?project=`), it is the current page.
export const MembersActive: Story = {
  args: {
    link: {
      id: 'members',
      label: 'Members',
      to: '/orgs/acme/members',
      disabledReason: null,
    },
  },
  parameters: { app: { responses: [], path: '/orgs/acme/members' } },
  play: async ({ canvas }) => {
    const link = await canvas.findByRole('link', { name: 'Members' });
    await expect(link).toHaveAttribute('aria-current', 'page');
  },
};
