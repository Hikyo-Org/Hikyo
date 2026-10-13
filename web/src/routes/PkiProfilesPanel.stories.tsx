import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor, within } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { profiles } from '../testkit/storybookPki.ts';
import { PkiProfilesPanel } from './PkiProfilesPanel.tsx';

const meta = {
  title: 'Features/PKI/PkiProfilesPanel',
  component: PkiProfilesPanel,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { responses: [{ url: '/api/v1/instance/pki/profiles', body: profiles }] } },
} satisfies Meta<typeof PkiProfilesPanel>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('web-services', { exact: true })).toBeVisible();
    // The ceiling is spelled out in words, not "72h".
    await expect(canvas.getByText(/max 3 days/)).toBeVisible();
  },
};
export const Empty: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', body: { profiles: [] } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByText(/No certificate profiles yet/)).toBeVisible(); },
};
export const Loading: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', pending: true }] } },
  play: async ({ canvas }) => { await waitFor(() => expect(canvas.getByText('Loading certificate profiles…')).toBeVisible()); },
};
export const SecondFactorRequired: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('needs instance-config and a second factor'); },
};
export const Failed: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', status: 500, body: { error: { code: 'internal', message: 'Internal' } } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent(/server error 500|could not|failed/i); },
};
export const Narrowing: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Narrow' }));
    const panel = within(canvas.getByRole('complementary', { name: 'Narrow web-services' }));
    await expect(panel.getByRole('textbox', { name: 'Policy of web-services (JSON)' })).toBeVisible();
    await expect(panel.getByText(/Only a narrowing is accepted/)).toBeVisible();
  },
};

// Binding opens its own panel; Bind stays disabled until an organization and
// a project are named.
export const Binding: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Bind' }));
    const panel = within(canvas.getByRole('complementary', { name: 'Bind web-services' }));
    const bind = panel.getByRole('button', { name: 'Bind' });
    await expect(bind).toBeDisabled();
    await userEvent.type(panel.getByRole('textbox', { name: 'Organization id' }), 'org_acme');
    await userEvent.type(panel.getByRole('textbox', { name: 'Project id' }), 'prj_app');
    await expect(bind).toBeEnabled();
  },
};

// At a phone width a profile's three actions sit on the two-column grid,
// equal-width cells, rather than wrapping raggedly beside the copy.
export const PhoneRowActions: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    const row = (await canvas.findByRole('button', { name: 'Narrow' })).closest<HTMLElement>('.actions');
    await expect(row).not.toBeNull();
    await expect(getComputedStyle(row!).display).toBe('grid');
    await expect(getComputedStyle(row!).gridTemplateColumns.split(' ')).toHaveLength(2);
    const [narrow, bind] = [...row!.querySelectorAll('.btn')].map((button) => button.getBoundingClientRect());
    await expect(Math.round(narrow!.top)).toBe(Math.round(bind!.top));
    await expect(Math.round(narrow!.width)).toBe(Math.round(bind!.width));
    await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
  },
};
// Binding and deleting open in the side panel too, one at a time.
export const BindAndDeleteInPanel: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Bind' }));
    const bind = within(canvas.getByRole('complementary', { name: 'Bind web-services' }));
    await expect(bind.getByRole('textbox', { name: 'Organization id' })).toBeVisible();
    await userEvent.keyboard('{Escape}');
    await expect(canvas.queryByRole('complementary')).toBeNull();
    await userEvent.click(canvas.getByRole('button', { name: 'Delete' }));
    await expect(canvas.getAllByRole('complementary')).toHaveLength(1);
    await expect(canvas.getByRole('complementary', { name: 'Delete web-services' })).toBeVisible();
  },
};
export const InvalidPolicy: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', body: { profiles: [] } }] } },
  play: async ({ canvas }) => {
    await canvas.findByText(/No certificate profiles yet/);
    await userEvent.click(canvas.getByRole('button', { name: 'Create a certificate profile' }));
    const panel = within(canvas.getByRole('complementary', { name: 'Create a certificate profile' }));
    await userEvent.type(panel.getByRole('textbox', { name: 'Name' }), 'web');
    await userEvent.clear(panel.getByRole('textbox', { name: 'Policy (JSON)' }));
    await userEvent.type(panel.getByRole('textbox', { name: 'Policy (JSON)' }), 'invalid JSON');
    await userEvent.click(panel.getByRole('button', { name: 'Create' }));
    await expect(await panel.findByRole('alert')).toHaveTextContent('not valid JSON');
  },
};
