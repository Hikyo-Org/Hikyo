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

// The laid-out text of an element's own text node: its width and how many
// lines it takes. The box of the element says nothing about this (a column of
// one glyph a line sits in a box as wide as its widest line).
const ownText = (element: HTMLElement) => {
  const node = [...element.childNodes].find((child) => child.nodeType === Node.TEXT_NODE && (child.textContent ?? '') !== '');
  if (node === undefined) throw new Error('no text node');
  const range = document.createRange();
  range.selectNodeContents(node);
  const rects = [...range.getClientRects()];
  return {
    chars: (node.textContent ?? '').length,
    lines: new Set(rects.map((rect) => Math.round(rect.top))).size,
    left: Math.min(...rects.map((rect) => rect.left)),
    right: Math.max(...rects.map((rect) => rect.right)),
  };
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
    const fontSize = (element: HTMLElement) => Number.parseFloat(getComputedStyle(element).fontSize);
    const expectReadable = async (count: number) => {
      const values = await waitFor(() => {
        const found = [...wrapper.querySelectorAll<HTMLElement>('.values__plain')].filter((value) => value.querySelector('.values__countdown') !== null);
        if (found.length !== count) throw new Error(`${String(found.length)} live values, want ${String(count)}`);
        return found;
      });
      for (const value of values) {
        const text = ownText(value);
        // The text itself, not its box: at least 20 characters a line on
        // average (a mono glyph is 0.6em), and inside the wrapper.
        await expect(text.right - text.left).toBeGreaterThanOrEqual(20 * 0.6 * fontSize(value));
        await expect(text.chars / text.lines).toBeGreaterThanOrEqual(20);
        await expect(text.right).toBeLessThanOrEqual(edge);
        const countdown = value.querySelector<HTMLElement>('.values__countdown')!;
        // The countdown sits under the text inside the value's own width.
        await expect(countdown.getBoundingClientRect().right).toBeLessThanOrEqual(value.getBoundingClientRect().right + 0.5);
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
    // The editor is the row's own full-width line: a fourth cell under a
    // three-column header grew the table past the phone and left the value a
    // column of glyphs. The value stays readable, and Save draft in reach.
    await userEvent.click(canvas.getByRole('button', { name: 'DATABASE_URL' }));
    const editor = wrapper.querySelector<HTMLElement>('.values__editor')!;
    await expect(editor).toBeVisible();
    await expectReadable(1);
    const room = editor.getBoundingClientRect();
    await expect(room.width).toBeGreaterThanOrEqual(wrapper.clientWidth - 2 * fontSize(editor));
    await expect(room.right).toBeLessThanOrEqual(edge);
    // The input itself takes the row (it sat at 170px in a 336px row), so a placeholder reads whole.
    await expect(canvas.getByLabelText(/^New value for DATABASE_URL/).getBoundingClientRect().width).toBeGreaterThanOrEqual(wrapper.clientWidth - 4 * fontSize(editor));
    const save = canvas.getByRole('button', { name: 'Save draft' }).getBoundingClientRect();
    await expect(save.left).toBeGreaterThanOrEqual(wrapper.getBoundingClientRect().left);
    await expect(save.right).toBeLessThanOrEqual(edge);
    await userEvent.click(canvas.getByRole('button', { name: 'DATABASE_URL' }));
    await userEvent.click(canvas.getByRole('button', { name: 'Reveal every secret' }));
    await expectReadable(3);
  },
};

// The longest key the fixtures hold (88 characters) and a value beside it: the
// key wraps in its own column and the value keeps the full width under it, on
// the narrowest phone, before and after a reveal of nothing at all.
export const PhoneLongKeyKeepsValueWide: Story = {
  globals: { viewport: { value: 'phoneNarrow', isRotated: false } },
  play: async ({ canvas }) => {
    const wrapper = (await canvas.findByRole('table')).parentElement!;
    const long = await canvas.findByRole('button', { name: /^ANALYTICS_PIPELINE/ });
    const value = canvas.getByText('warehouse://eu-west-4/nightly');
    const text = ownText(value);
    await expect(wrapper.scrollWidth).toBeLessThanOrEqual(wrapper.clientWidth);
    // The value reads on one line, at the width of the table, not squeezed beside the key.
    await expect(text.lines).toBe(1);
    await expect(long.getBoundingClientRect().bottom).toBeLessThanOrEqual(value.getBoundingClientRect().top + 0.5);
    for (const button of canvas.getAllByRole('button', { name: /^(Reveal|Copy) / })) {
      await expect(button.getBoundingClientRect().right).toBeLessThanOrEqual(wrapper.getBoundingClientRect().right);
    }
    // The wrapped key reads from the left, not centred like a button's label.
    await expect(getComputedStyle(long).textAlign).toBe('left');
    // ... and its text lines up with the value's, not 9px in from it (the button's padding and border).
    await expect(ownText(long).left).toBeCloseTo(text.left, 0);
    // The column headings stay for assistive technology and are not drawn: they
    // name three columns that the card layout no longer has.
    // Found without `hidden: true`, so a removed (display: none) header fails.
    for (const name of ['Key', 'Value', 'Actions']) {
      const heading = canvas.getByRole('columnheader', { name }).getBoundingClientRect();
      await expect(heading.width).toBeLessThanOrEqual(1);
    }
    // The key and its Actions share the first line, Reveal over Copy so the key
    // keeps the room: the Actions sit beside the key, not in a line of their own.
    const key = canvas.getByRole('button', { name: 'DATABASE_URL' }).getBoundingClientRect();
    const reveal = canvas.getByRole('button', { name: 'Reveal DATABASE_URL' }).getBoundingClientRect();
    const copy = canvas.getByRole('button', { name: /^Copy DATABASE_URL/ }).getBoundingClientRect();
    await expect(reveal.top).toBeLessThan(key.bottom);
    await expect(copy.top).toBeGreaterThanOrEqual(reveal.bottom);
    // A lone Copy fills the Actions track like the stacked buttons do.
    const lone = canvas.getAllByRole('button', { name: /^Copy / }).find((button) => button.closest('.actions')!.children.length === 1)!.getBoundingClientRect();
    await expect(lone.width).toBeCloseTo(reveal.width, 0);
    // The open editor holds the unbroken long key in its label without scrolling
    // the table sideways, and its field takes the row.
    await userEvent.click(long);
    const field = await canvas.findByLabelText(/^New value for ANALYTICS_PIPELINE/);
    await expect(wrapper.scrollWidth).toBeLessThanOrEqual(wrapper.clientWidth);
    await expect(field.getBoundingClientRect().width).toBeGreaterThanOrEqual(wrapper.clientWidth - 4 * Number.parseFloat(getComputedStyle(field).fontSize));
    await expect(canvas.getByRole('button', { name: 'Save draft' }).getBoundingClientRect().right).toBeLessThanOrEqual(wrapper.getBoundingClientRect().right);
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
