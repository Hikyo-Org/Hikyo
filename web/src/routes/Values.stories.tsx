import type { Meta, StoryObj } from '@storybook/react-vite';
import { zEnvironmentList, zRevealWindow, zValueList } from '@hikyo/zod';
import { expect, spyOn, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG, PRJ, PROD as ENV, STAGING } from '../testkit/ids.ts';
import { Values } from './Values.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { dottedText } from '../testkit/dottedText.ts';

// The reveal / copy / write-only-edit surface for ONE environment. On mount it
// reads three things: the masked value list, the project's environments (the
// publish destinations and the ceremony's environment name) and the reveal
// window, whose `can_reveal` decides whether a secret gets Reveal/Copy controls
// or the write-only editor. Reveal/copy/publish run the ceremony and are not
// driven here; the stories cover what the page looks like before any of that.
const PATH = `/orgs/${ORG}/projects/${PRJ}/environments/${ENV}/values`;
const ROUTE = '/orgs/:org/projects/:project/environments/:environment/values';
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const VALUES_URL = `${PROJECT_URL}/environments/${ENV}/values`;
const WINDOW_URL = `${PROJECT_URL}/environments/${ENV}/reveal-window`;
const ENVIRONMENTS_URL = `${PROJECT_URL}/environments`;

type ValueList = z.input<typeof zValueList>;
type RevealWindow = z.input<typeof zRevealWindow>;

const environments = {
  count: 3,
  items: [
    { id: ENV, name: 'production', display_order: 0 },
    { id: STAGING, name: 'staging', display_order: 1 },
    { id: 'env_123e4567-e89b-12d3-a456-426614174012', name: 'development', display_order: 2 },
  ].map((item) => ({ ...item, org_id: ORG, project_id: PRJ, created_at: '2026-01-01T00:00:00Z' })),
} satisfies z.input<typeof zEnvironmentList>;

const keyId = (n: number) => `key_123e4567-e89b-12d3-a456-4266141740${String(n).padStart(2, '0')}`;
const cell = (
  n: number,
  name: string,
  classification: 'secret' | 'config',
  value?: string,
): ValueList['items'][number] => ({
  key_id: keyId(n),
  name,
  classification,
  set: value !== undefined,
  revealed: classification === 'config' && value !== undefined,
  ...(classification === 'config' && value !== undefined ? { value } : {}),
});

// Dense enough to design against: secrets set and absent, config short and
// long, and a key name near the 128-character ceiling to test the mono column.
const populated: ValueList = {
  count: 9,
  items: [
    cell(1, 'DATABASE_URL', 'secret', 'x'),
    cell(2, 'STRIPE_SECRET_KEY', 'secret', 'x'),
    cell(3, 'SMTP_PASSWORD', 'secret'),
    cell(4, 'LOG_LEVEL', 'config', 'info'),
    cell(5, 'FEATURE_FLAGS', 'config', 'checkout-v2,search-rerank,beta-dashboard,new-onboarding'),
    cell(6, 'PUBLIC_BASE_URL', 'config', 'https://app.example.com'),
    cell(7, 'CACHE_TTL_SECONDS', 'config'),
    cell(8, 'OIDC_CLIENT_SECRET', 'secret', 'x'),
    cell(
      9,
      'ANALYTICS_PIPELINE_WAREHOUSE_CONNECTION_STRING_FOR_THE_NIGHTLY_EXPORT_JOB_IN_EUROPE_WEST',
      'config',
      'warehouse://eu-west-4/nightly',
    ),
  ],
};

const locked: RevealWindow = {
  effective_window_seconds: 600,
  protected: false,
  totp_offered: false,
  live: false,
  single_decision: false,
  can_reveal: true,
};

const rows = (values: MockRoute, window: MockRoute = { url: WINDOW_URL, body: locked }) => ({
  app: { path: PATH, routePath: ROUTE, responses: [values, window, { url: ENVIRONMENTS_URL, body: environments }] },
});

const meta = {
  title: 'Pages/Values',
  id: 'routes-values',
  component: Values,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "The real environment-values screen. Data reads distinguish loading, empty and refused states; stored secret disclosure stays behind the existing authorization ceremony and is excluded from ordinary fixtures." } },  ...rows({ url: VALUES_URL, body: populated }) },
} satisfies Meta<typeof Values>;

export default meta;
type Story = StoryObj<typeof meta>;

// No window is live: every secret is masked with Reveal and Copy on offer, the
// chip says each disclosure will ask first, config values read in the clear.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: 'Reveal DATABASE_URL' })).toBeVisible();
    await expect(canvas.getByText(dottedText(/^Locked\s*·\s*a passkey per disclosure$/))).toBeVisible();
    await expect(canvas.getByText('info')).toBeVisible();
    await expect(canvas.getAllByText('absent')).toHaveLength(2);
  },
};

// A phone: an unbreakable value (a URL) and a key name past the 18 character
// wrap point must not push the Actions column off the scrolling wrapper. The
// table fits the phone, so every row's action is in reach without a sideways scroll.
export const PhoneKeepsActionsInReach: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  parameters: rows({
    url: VALUES_URL,
    body: { ...populated, count: populated.count + 1, items: [...populated.items, cell(10, 'PUBLIC_APP_URL', 'config', 'https://staging.example.test/callbacks/oauth')] },
  }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: 'Reveal DATABASE_URL' })).toBeVisible();
    const wrapper = canvas.getByRole('table').parentElement!;
    await expect(wrapper.scrollWidth).toBeLessThanOrEqual(wrapper.clientWidth);
    const edge = wrapper.getBoundingClientRect().right;
    for (const button of canvas.getAllByRole('button', { name: /^(Reveal|Copy) / })) {
      await expect(button.getBoundingClientRect().right).toBeLessThanOrEqual(edge);
    }
    await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
  },
};

// A live reveal on the narrowest phone: the value is the text with its
// countdown, and the value column is what the key and Actions columns leave.
// The text breaks anywhere and keeps the room, and the countdown wraps under
// it instead of holding its own width: in a row it kept its width and the text
// shrank to a glyph a line, then the table outgrew the wrapper and pushed
// Copy out of reach. One secret revealed, then every secret.
const longSecret = 'postgresql://demo-only:fixture@database.example.test:5432/hikyo_staging?sslmode=require';
const liveWindow: RevealWindow = { ...locked, live: true, expires_at: '2099-01-01T00:00:00Z' };
const revealed = (n: number, name: string, value: string): ValueList['items'][number] => ({ ...cell(n, name, 'secret', value), revealed: true, value });

export const PhoneRevealedValueStaysReadable: Story = {
  globals: { viewport: { value: 'phoneNarrow', isRotated: false } },
  beforeEach: () => {
    // The reveal acts only on a focused window, which a headless run may lack.
    const focus = spyOn(document, 'hasFocus').mockReturnValue(true);
    return () => focus.mockRestore();
  },
  parameters: {
    app: {
      ...rows({ url: VALUES_URL, body: populated }, { url: WINDOW_URL, body: liveWindow }).app,
      responses: [
        { url: VALUES_URL, body: populated },
        { url: WINDOW_URL, body: liveWindow },
        { url: ENVIRONMENTS_URL, body: environments },
        { url: `${VALUES_URL}/DATABASE_URL/reveal`, method: 'POST', body: revealed(1, 'DATABASE_URL', longSecret) },
        {
          url: `${VALUES_URL}/reveal`,
          method: 'POST',
          body: {
            count: 3,
            items: [revealed(1, 'DATABASE_URL', longSecret), revealed(2, 'STRIPE_SECRET_KEY', `sk_live_${'0123456789'.repeat(4)}`), revealed(8, 'OIDC_CLIENT_SECRET', 'oidc-client-secret-fixture-value')],
          },
        },
      ],
    },
  },
  play: async ({ canvas }) => {
    const wrapper = (await canvas.findByRole('table')).parentElement!;
    const edge = wrapper.getBoundingClientRect().right;
    const expectReadable = async (count: number) => {
      const values = await waitFor(() => {
        const found = [...wrapper.querySelectorAll<HTMLElement>('.values__plain')].filter((value) => value.querySelector('.values__countdown') !== null);
        if (found.length !== count) throw new Error(`${String(found.length)} live values, want ${String(count)}`);
        return found;
      });
      for (const value of values) {
        const box = value.getBoundingClientRect();
        // Text, not a column of glyphs: at least six characters a line (a mono
        // glyph is 0.6em).
        await expect(box.width).toBeGreaterThanOrEqual(4 * Number.parseFloat(getComputedStyle(value).fontSize));
        const countdown = value.querySelector<HTMLElement>('.values__countdown')!;
        // The countdown sits under the text inside the value's own width.
        await expect(countdown.getBoundingClientRect().right).toBeLessThanOrEqual(box.right + 0.5);
        await expect(countdown.scrollWidth).toBeLessThanOrEqual(countdown.clientWidth);
      }
      await expect(wrapper.scrollWidth).toBeLessThanOrEqual(wrapper.clientWidth);
      for (const button of canvas.getAllByRole('button', { name: /^(Reveal|Copy) / })) {
        await expect(button.getBoundingClientRect().right).toBeLessThanOrEqual(edge);
      }
      await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
    };
    await userEvent.click(await canvas.findByRole('button', { name: 'Reveal DATABASE_URL' }));
    await expectReadable(1);
    await userEvent.click(canvas.getByRole('button', { name: 'Reveal every secret' }));
    await expectReadable(3);
  },
};

// A window is live: the chip counts it down and "Reveal every secret" is enabled.
export const WindowLive: Story = {
  parameters: rows(
    { url: VALUES_URL, body: populated },
    {
      url: WINDOW_URL,
      body: { ...locked, live: true, expires_at: new Date(Date.now() + 9 * 60_000).toISOString() },
    },
  ),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(dottedText(/^Reveal window\s*·\s*\d+ seconds?$/))).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Reveal every secret' })).toBeEnabled();
  },
};

// A protected environment with no live window: the chip names the passkey rule.
export const ProtectedLocked: Story = {
  parameters: rows(
    { url: VALUES_URL, body: populated },
    { url: WINDOW_URL, body: { ...locked, protected: true, effective_window_seconds: 0 } },
  ),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(dottedText(/^Protected\s*·\s*a passkey per disclosure$/))).toBeVisible();
  },
};

// `edit` without `reveal`: secrets carry no disclosure control, and the editor
// says it replaces without seeing. The play opens the editor (no request).
export const WriteOnly: Story = {
  parameters: rows(
    { url: VALUES_URL, body: populated },
    { url: WINDOW_URL, body: { ...locked, can_reveal: false } },
  ),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('DATABASE_URL')).toBeVisible();
    await expect(canvas.queryByRole('button', { name: 'Reveal DATABASE_URL' })).not.toBeInTheDocument();
    await expect(canvas.getByRole('button', { name: 'Reveal every secret' })).toBeDisabled();
    await userEvent.click(canvas.getByRole('button', { name: 'DATABASE_URL' }));
    await expect(
      canvas.getByPlaceholderText('Replace without seeing the current value'),
    ).toBeVisible();
    await expect(canvas.getByText(/You may replace this value but not read it/)).toBeVisible();
  },
};

// An environment with no keys yet: the bar's actions are disabled and the
// table has only its header.
export const Empty: Story = {
  parameters: rows({ url: VALUES_URL, body: { count: 0, items: [] } satisfies ValueList }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(dottedText(/^Locked\s*·\s*a passkey per disclosure$/))).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Reveal every secret' })).toBeDisabled();
    await expect(canvas.queryByRole('row', { name: /DATABASE_URL/ })).not.toBeInTheDocument();
  },
};

// Nothing has resolved: the heading stands with no chip, no rows, no controls
// beyond the disabled bar.
export const Loading: Story = {
  parameters: rows({ url: VALUES_URL, pending: true }, { url: WINDOW_URL, pending: true }),
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Values' })).toBeVisible());
    await expect(canvas.getByRole('button', { name: 'Reveal every secret' })).toBeDisabled();
  },
};

// The value read failed: the page keeps its chip and bar and says so.
export const Failed: Story = {
  parameters: rows({ url: VALUES_URL, status: 500, body: { error: 'values read failed' } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/The values could not be loaded/)).toBeVisible();
  },
};
