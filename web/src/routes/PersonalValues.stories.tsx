import type { Meta, StoryObj } from '@storybook/react-vite';
import { zEnvironmentList, zKeyList, zPersonalValueList, zRevealWindow, zTotpStatus } from '@hikyo/zod';
import { expect, userEvent, within } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { PERSONAL_STATEMENT } from '../api/personal.ts';
import { ORG, PRJ, STAGING as ENV } from '../testkit/ids.ts';
import { PersonalValues } from './PersonalValues.tsx';

// My personal values for one environment: the owner's own values, masked, with
// the checklist of required keys, removal notices, suspension and warnings.
// Set and reveal run the reveal ceremony, which these stories do not drive.
const PATH = `/orgs/${ORG}/projects/${PRJ}/environments/${ENV}/personal-values`;
const ROUTE = '/orgs/:org/projects/:project/environments/:environment/personal-values';
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const LIST_URL = `${PROJECT_URL}/environments/${ENV}/personal-values`;
const WINDOW_URL = `${PROJECT_URL}/environments/${ENV}/reveal-window`;

const stamp = { org_id: ORG, project_id: PRJ, created_at: '2026-01-01T00:00:00Z' };
const environments = {
  count: 1,
  items: [{ id: ENV, name: 'staging', display_order: 0, ...stamp }],
} satisfies z.input<typeof zEnvironmentList>;

const keyId = (n: number) => `key_123e4567-e89b-12d3-a456-4266141740${String(n).padStart(2, '0')}`;
const declared = (n: number, name: string): z.input<typeof zKeyList>['items'][number] => ({
  id: keyId(n),
  name,
  folder_path: '',
  classification: 'secret',
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: {},
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '',
  ...stamp,
});
const keys = {
  count: 5,
  schema_revision: 1,
  items: [
    declared(1, 'DATABASE_URL'),
    declared(2, 'STRIPE_SECRET_KEY'),
    declared(3, 'SENTRY_DSN'),
    declared(4, 'DEBUG_TOKEN'),
    declared(5, 'FEATURE_PREVIEW_KEY'),
  ],
} satisfies z.input<typeof zKeyList>;

const populated = {
  items: [
    { key_id: keyId(1), name: 'DATABASE_URL', set: true, updated_at: '2026-10-01T08:00:00Z' },
    { key_id: keyId(2), name: 'STRIPE_SECRET_KEY', set: false, required: true },
    { key_id: keyId(3), name: 'SENTRY_DSN', set: false, notice: 'administrator' },
    { key_id: keyId(4), name: 'DEBUG_TOKEN', set: true, suspended: true, suspended_at: '2026-10-20T12:00:00Z' },
    { key_id: keyId(5), name: 'FEATURE_PREVIEW_KEY', set: true, warnings: ['longer than 32 characters'] },
  ],
} satisfies z.input<typeof zPersonalValueList>;

const window = {
  effective_window_seconds: 600,
  protected: false,
  totp_offered: true,
  live: false,
  single_decision: false,
  can_reveal: true,
} satisfies z.input<typeof zRevealWindow>;

const factors = (over: Partial<z.input<typeof zTotpStatus>> = {}) => ({
  url: '/api/v1/auth/totp',
  body: { has_password: true, has_passkey: true, can_reauth_oidc: false, confirmed: true, pending: false, ...over } satisfies z.input<typeof zTotpStatus>,
});

const app = (rest: { list?: Omit<MockRoute, 'url'>; window?: z.input<typeof zRevealWindow>; totp?: MockRoute } = {}) => ({
  app: {
    // The page reads the session (an identity provider approval cannot set a personal value).
    auth: true,
    path: PATH,
    routePath: ROUTE,
    responses: [
      { url: LIST_URL, body: populated, ...rest.list },
      { url: WINDOW_URL, body: rest.window ?? window },
      { url: `${PROJECT_URL}/environments`, body: environments },
      { url: `${PROJECT_URL}/keys`, body: keys },
      rest.totp ?? factors(),
    ],
  },
});

const meta = {
  title: 'Pages/PersonalValues',
  id: 'routes-personalvalues',
  component: PersonalValues,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    docs: { ...topLayerDocs.docs, description: { component: 'My personal values: the owner sets, reveals and deletes their own value per key and environment. There is no download; the CLI runs programs with them.' } },
    ...app(),
  },
} satisfies Meta<typeof PersonalValues>;

export default meta;
type Story = StoryObj<typeof meta>;

// Every state at once: set, required and missing, removed with its cause,
// suspended, and set with a schema warning. The fixed statement leads.
export const Populated: Story = {
  play: async ({ canvas }) => {
    // AuthProvider remounts the page when the session binds: wait for the loaded list first.
    await expect(await canvas.findByRole('button', { name: 'Reveal DATABASE_URL' })).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'My personal values', level: 1 })).toBeVisible();
    await expect(canvas.getByText(PERSONAL_STATEMENT)).toBeVisible();
    await expect(canvas.getByText('required · missing')).toBeVisible();
    await expect(canvas.getByText(/an administrator deleted it/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Dismiss the notice for SENTRY_DSN' })).toBeVisible();
    await expect(canvas.getByText(/Not delivered while you lack access here/)).toBeVisible();
    await expect(canvas.getByText(/longer than 32 characters/)).toBeVisible();
    // No .env download anywhere: running a program is the CLI's job.
    await expect(canvas.queryByRole('link', { name: /download|\.env/i })).toBeNull();
    await expect(canvas.queryByRole('button', { name: /download|\.env/i })).toBeNull();
  },
};

// The set form, in the side panel, repeats the fixed statement where the value is entered.
export const SetForm: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Set STRIPE_SECRET_KEY' }));
    const panel = within(await canvas.findByRole('complementary', { name: 'Personal value for STRIPE_SECRET_KEY' }));
    await expect(panel.getByText(PERSONAL_STATEMENT)).toBeVisible();
    await expect(panel.getByLabelText('Your value for STRIPE_SECRET_KEY in staging')).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Save my value' })).toBeDisabled();
  },
};

// Nothing held and nothing required.
export const Empty: Story = {
  parameters: app({ list: { body: { items: [] } } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('You hold no personal values here, and none are required.')).toBeVisible();
  },
};

// An account without a passkey or authenticator app: the remedy, and Set is off.
export const NeedsAFactor: Story = {
  parameters: app({ totp: factors({ has_passkey: false, confirmed: false }) }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('link', { name: 'Settings › Security' })).toBeVisible();
    await expect(await canvas.findByRole('button', { name: 'Set a value' })).toBeDisabled();
  },
};

// A protected environment takes no personal values.
export const ProtectedEnvironment: Story = {
  parameters: app({ window: { ...window, protected: true } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/Personal values are for unprotected environments only/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Set a value' })).toBeDisabled();
  },
};

// The same page on a phone: a card per key, as on Values, so every action is in
// reach without scrolling sideways, and the page never scrolls sideways either.
export const Phone: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    const reveal = await canvas.findByRole('button', { name: 'Reveal DATABASE_URL' });
    await expect(reveal).toBeVisible();
    await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
    const scroller = reveal.closest('.table-scroll');
    if (!(scroller instanceof HTMLElement)) throw new Error('table wrapper missing');
    await expect(scroller.scrollWidth).toBeLessThanOrEqual(scroller.clientWidth);
    await expect(reveal.getBoundingClientRect().right).toBeLessThanOrEqual(scroller.getBoundingClientRect().right);
  },
};
