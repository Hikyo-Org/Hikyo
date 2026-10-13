import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  zOrg,
  zProjectList,
  zScimBindingList,
  zScimCredentialList,
  zScimDirectoryGroupList,
  zScimDirectoryUserList,
  zScimMappingList,
} from '@hikyo/zod';
import { expect, userEvent, within } from 'storybook/test';
import type { z } from 'zod';

import { openTab } from '../../.storybook/openTab.ts';
import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG } from '../testkit/ids.ts';
import { ScimProvisioningPage } from './ScimProvisioning.tsx';

import { expectLeftAlignedPage } from '../../.storybook/layout.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The page behind the system-scope gate (the gate's own answers are storied in
// SystemScopeRefusal). It reads `useParams().org` and the `binding` search
// param; the bindings list is its only load-time read. Administering a binding
// mounts three more panels, each with its own reads, plus the org (mapping
// scope labels) and the org's projects (scope options): an EMPTY project list
// keeps the topology ready without a per-environment fan-out.
const BINDING = 'bnd_123e4567-e89b-12d3-a456-426614174201';
const BINDINGS_URL = `/api/v1/orgs/${ORG}/scim-bindings`;
const bindingUrl = (tail: string) => `${BINDINGS_URL}/${BINDING}/${tail}`;

const org = { id: ORG, name: 'Acme', active: true, created_at: '2026-01-01T00:00:00Z', origin: 'manual' } satisfies z.input<typeof zOrg>;
const noProjects = { count: 0, items: [] } satisfies z.input<typeof zProjectList>;

const GROUP_ENG = 'grp_123e4567-e89b-12d3-a456-426614174301';
const GROUP_OPS = 'grp_123e4567-e89b-12d3-a456-426614174302';
const GROUP_GONE = 'grp_123e4567-e89b-12d3-a456-426614174303';

const bindings = {
  count: 2,
  items: [
    {
      id: BINDING,
      org_id: ORG,
      provider_kind: 'oidc',
      provider_slug: 'okta',
      provider_issuer: 'https://acme.okta.com',
      subject_source: 'externalId',
      connection_principal_id: 'prn_123e4567-e89b-12d3-a456-426614174210',
      last_contact_at: '2026-09-22T08:15:00Z',
      created_at: '2026-06-01T00:00:00Z',
      attention: [],
    },
    {
      id: 'bnd_123e4567-e89b-12d3-a456-426614174202',
      org_id: ORG,
      provider_kind: 'saml',
      provider_slug: 'acme-adfs',
      provider_issuer: 'https://adfs.acme.example/adfs/services/trust',
      subject_source: 'urn:ietf:params:scim:schemas:extension:acme:2.0:User:employeeNumber',
      connection_principal_id: 'prn_123e4567-e89b-12d3-a456-426614174211',
      created_at: '2026-09-01T00:00:00Z',
      attention: [
        {
          state: 'provider_unavailable',
          subject_ref: 'acme-adfs',
          cause: 'The provider has not contacted this binding since it was created.',
          entered_at: '2026-09-01T00:00:00Z',
          remediation: 'Mint a credential below and configure it at the identity provider.',
        },
      ],
    },
  ],
} satisfies z.input<typeof zScimBindingList>;

const MAPPING_ENG = 'map_123e4567-e89b-12d3-a456-426614174401';
const mappings = {
  count: 2,
  items: [
    {
      id: MAPPING_ENG,
      binding_id: BINDING,
      group_id: GROUP_ENG,
      template: 'engineer',
      inert: false,
      created_at: '2026-06-02T00:00:00Z',
      capabilities: ['read', 'reveal'],
      capability_origins: [
        { capability: 'read', kind: 'scim', binding_id: BINDING, mapping_id: MAPPING_ENG, group_id: GROUP_ENG },
        { capability: 'reveal', kind: 'scim', binding_id: BINDING, mapping_id: MAPPING_ENG, group_id: GROUP_ENG },
      ],
    },
    {
      id: 'map_123e4567-e89b-12d3-a456-426614174402',
      binding_id: BINDING,
      group_id: GROUP_GONE,
      template: 'operator',
      inert: true,
      created_at: '2026-06-02T00:00:00Z',
      capabilities: ['read', 'manage-members'],
    },
  ],
} satisfies z.input<typeof zScimMappingList>;

const credentials = {
  count: 3,
  items: [
    {
      id: 'scr_123e4567-e89b-12d3-a456-426614174501',
      binding_id: BINDING,
      created_at: '2026-09-01T00:00:00Z',
      expires_at: '2026-12-01T00:00:00Z',
      last_used_at: '2026-09-22T08:15:00Z',
      live: true,
    },
    {
      id: 'scr_123e4567-e89b-12d3-a456-426614174502',
      binding_id: BINDING,
      created_at: '2026-03-01T00:00:00Z',
      expires_at: '2026-06-01T00:00:00Z',
      last_used_at: '2026-05-30T00:00:00Z',
      live: false,
    },
    {
      id: 'scr_123e4567-e89b-12d3-a456-426614174503',
      binding_id: BINDING,
      created_at: '2026-06-01T00:00:00Z',
      revoked_at: '2026-09-01T00:00:00Z',
      live: false,
    },
  ],
} satisfies z.input<typeof zScimCredentialList>;

const users = {
  count: 3,
  items: [
    {
      id: 'usr_123e4567-e89b-12d3-a456-426614174601',
      user_name: 'ada.lovelace',
      external_id: '00u1a2b3c4',
      account_id: 'acc_123e4567-e89b-12d3-a456-426614174701',
      active: true,
      groups: [GROUP_ENG, GROUP_OPS],
      created_at: '2026-06-02T00:00:00Z',
      updated_at: '2026-09-20T00:00:00Z',
      attention: [],
    },
    {
      id: 'usr_123e4567-e89b-12d3-a456-426614174602',
      user_name: 'grace.hopper',
      account_id: 'acc_123e4567-e89b-12d3-a456-426614174702',
      active: false,
      groups: [GROUP_ENG],
      created_at: '2026-06-02T00:00:00Z',
      updated_at: '2026-09-15T00:00:00Z',
      attention: [
        {
          state: 'manual_grants_remain',
          subject_ref: 'grace.hopper',
          cause: 'deprovisioned',
          entered_at: '2026-09-15T00:00:00Z',
          remediation: 'Review the manual grants on Members and revoke the ones nobody vouches for.',
        },
      ],
    },
    {
      id: 'usr_123e4567-e89b-12d3-a456-426614174603',
      user_name: 'contractor.temp',
      account_id: 'acc_123e4567-e89b-12d3-a456-426614174703',
      active: false,
      groups: [],
      created_at: '2026-07-01T00:00:00Z',
      updated_at: '2026-08-01T00:00:00Z',
      attention: [],
    },
  ],
} satisfies z.input<typeof zScimDirectoryUserList>;

const groups = {
  count: 2,
  items: [
    { id: GROUP_ENG, display_name: 'Engineering', external_id: '00g1', member_count: 2, created_at: '2026-06-02T00:00:00Z', updated_at: '2026-09-20T00:00:00Z' },
    { id: GROUP_OPS, display_name: 'Operations', member_count: 1, created_at: '2026-06-02T00:00:00Z', updated_at: '2026-09-20T00:00:00Z' },
  ],
} satisfies z.input<typeof zScimDirectoryGroupList>;

const administering: readonly MockRoute[] = [
  { url: BINDINGS_URL, body: bindings },
  { url: bindingUrl('mappings'), body: mappings },
  { url: bindingUrl('credentials'), body: credentials },
  { url: bindingUrl('directory/users'), body: users },
  { url: bindingUrl('directory/groups'), body: groups },
  { url: `/api/v1/orgs/${ORG}`, body: org },
  { url: `/api/v1/orgs/${ORG}/projects`, body: noProjects },
];

const at = (search: string, responses: readonly MockRoute[]) => ({
  path: `/orgs/${ORG}/scim${search}`,
  routePath: '/orgs/:org/scim',
  responses,
});

const meta = {
  title: 'Pages/ScimProvisioning',
  id: 'routes-scimprovisioning',
  component: ScimProvisioningPage,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "SCIM provisioning bindings and their lifecycle on the organisation administration screen. The real page keeps pending, empty, refusal and configured states distinct; privileged changes retain their proof requirements." } },

    app: at('', [{ url: BINDINGS_URL, body: bindings }]),
  },
} satisfies Meta<typeof ScimProvisioningPage>;

export default meta;
type Story = StoryObj<typeof meta>;

// One binding selected: its sections are tabs. Mappings (a live row
// with origins and an inert one), credentials in every lifecycle state, and a
// directory with active, deprovisioned, and deprovisioned-with-manual-grants
// users.
export const Administering: Story = {
  parameters: { app: at(`?binding=${BINDING}&tab=mappings`, administering) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('tab', { name: 'Mappings' })).toHaveAttribute('aria-selected', 'true');
    await expect(await canvas.findByRole('heading', { name: 'Engineering' })).toBeVisible();
    await expect(canvas.getByText('inert')).toBeVisible();
    await openTab(canvas, 'Provisioning credentials');
    await expect(await canvas.findByText('revoked')).toBeVisible();
    await expect(canvas.getByText('expired')).toBeVisible();
    await openTab(canvas, 'Directory');
    await expect(await canvas.findByText('deprovisioned, manual grants remain')).toBeVisible();
    await expect(canvas.getByText('in no groups')).toBeVisible();
    await openTab(canvas, 'Bindings');
    await expect(await canvas.findByRole('button', { name: 'Administering' })).toBeVisible();
    await expectLeftAlignedPage(canvas.getByRole('heading', { level: 1 }));
  },
};

// Administer opens the binding's first section, the Mappings tab that appears.
export const AdministerOpensMappings: Story = {
  parameters: { app: at('', administering) },
  play: async ({ canvas }) => {
    await expect(canvas.queryByRole('tablist')).toBeNull();
    await userEvent.click((await canvas.findAllByRole('button', { name: 'Administer' }))[0] as HTMLElement);
    await expect(await canvas.findByRole('tab', { name: 'Mappings' })).toHaveAttribute('aria-selected', 'true');
  },
};

// Two bindings, none administered: cards with facts and attention, plus the
// create form; the per-binding panels stay unmounted.
export const Unselected: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('okta')).toBeVisible();
    await expect(canvas.getByText('acme-adfs')).toBeVisible();
    await expect(canvas.getByText('provider unavailable')).toBeVisible();
    await expect(canvas.getAllByRole('button', { name: 'Administer' })).toHaveLength(2);
    await expect(canvas.queryByRole('heading', { name: 'Mappings' })).toBeNull();
  },
};

// No bindings yet: the status line and the create form alone.
export const Empty: Story = {
  parameters: { app: at('', [{ url: BINDINGS_URL, body: { count: 0, items: [] } }]) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no bindings yet/i)).toBeVisible();
    await expect(canvas.queryByRole('button', { name: 'Create binding' })).toBeNull();
    // The create form opens in the side panel; its provider kinds sit side by side.
    await userEvent.click(canvas.getByRole('button', { name: 'Create a binding' }));
    const panel = within(canvas.getByRole('complementary', { name: 'Create a binding' }));
    await expect(panel.getByRole('button', { name: 'Create binding' })).toBeDisabled();
    const [oidc, saml] = [panel.getByRole('radio', { name: 'OIDC' }), panel.getByRole('radio', { name: 'SAML' })];
    await expect(Math.abs(oidc.getBoundingClientRect().top - saml.getBoundingClientRect().top)).toBeLessThan(2);
  },
};

// Every input of an administered binding opens from a button into the side
// panel: mapping a group, retargeting a row, minting, deleting the binding.
export const AdministeringPanels: Story = {
  parameters: Administering.parameters,
  play: async ({ canvas }) => {
    for (const [tab, opener, panel, field] of [
      ['Mappings', 'Map a group', 'Map a group', 'Provisioned group'],
      ['Provisioning credentials', 'Mint a credential', 'Mint a credential', 'Reauthentication proof'],
      ['Bindings', 'Delete this binding', 'Delete okta', 'Confirm by typing the provider slug'],
      ['Mappings', 'Retarget Engineering', 'Retarget Engineering', 'Retarget template'],
    ] as const) {
      await openTab(canvas, tab);
      await userEvent.click(await canvas.findByRole('button', { name: opener }));
      const sheet = within(canvas.getByRole('complementary', { name: panel }));
      await expect(sheet.getByLabelText(field)).toBeVisible();
      await userEvent.keyboard('{Escape}');
      await expect(canvas.queryByRole('complementary', { name: panel })).toBeNull();
    }
  },
};

// The binding cards are a plain stack: no list marker before each.
export const CardsWithoutMarkers: Story = {
  play: async ({ canvas }) => {
    const card = (await canvas.findByText('okta')).closest('li');
    if (card === null) throw new Error('binding card missing');
    await expect(getComputedStyle(card.parentElement ?? card).listStyleType).toBe('none');
  },
};

// Stacked binding cards are spaced by the list's gap, never flush,
// and each card header keeps the name and kind at the start and its action at
// the end of the same row.
export const CardSpacingAndHeader: Story = {
  play: async ({ canvas }) => {
    const [first, second] = (await canvas.findAllByRole('button', { name: 'Administer' })).map(
      (button) => button.closest('li') as HTMLElement,
    );
    if (first === undefined || second === undefined) throw new Error('two binding cards expected');
    await expect(second.getBoundingClientRect().top - first.getBoundingClientRect().bottom).toBeGreaterThanOrEqual(8);
    const head = first.querySelector<HTMLElement>('.scim-binding__head');
    const action = head?.querySelector<HTMLElement>('.btn');
    if (head === null || head === undefined || action === null || action === undefined) throw new Error('card head missing');
    const headRight = head.getBoundingClientRect().right;
    await expect(Math.abs(action.getBoundingClientRect().right - headRight)).toBeLessThan(2);
  },
};

// The bindings read failed: the panel names the failure and lists nothing.
export const Failed: Story = {
  parameters: { app: at('', [{ url: BINDINGS_URL, status: 500, body: { code: 'internal' } }]) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('alert')).toHaveTextContent(/server failed while reading/i);
  },
};
