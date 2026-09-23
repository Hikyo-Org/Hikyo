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
import { expect } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ScimProvisioningPage } from './ScimProvisioning.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The page behind the system-scope gate (the gate's own answers are storied in
// SystemScopeRefusal). It reads `useParams().org` and the `binding` search
// param; the bindings list is its only load-time read. Administering a binding
// mounts three more panels, each with its own reads, plus the org (mapping
// scope labels) and the org's projects (scope options): an EMPTY project list
// keeps the topology ready without a per-environment fan-out.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const BINDING = 'bnd_123e4567-e89b-12d3-a456-426614174201';
const BINDINGS_URL = `/api/v1/orgs/${ORG}/scim-bindings`;
const bindingUrl = (tail: string) => `${BINDINGS_URL}/${BINDING}/${tail}`;

const org = { id: ORG, name: 'Acme', active: true, created_at: '2026-01-01T00:00:00Z' } satisfies z.infer<typeof zOrg>;
const noProjects = { count: 0, items: [] } satisfies z.infer<typeof zProjectList>;

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
} satisfies z.infer<typeof zScimBindingList>;

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
} satisfies z.infer<typeof zScimMappingList>;

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
} satisfies z.infer<typeof zScimCredentialList>;

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
} satisfies z.infer<typeof zScimDirectoryUserList>;

const groups = {
  count: 2,
  items: [
    { id: GROUP_ENG, display_name: 'Engineering', external_id: '00g1', member_count: 2, created_at: '2026-06-02T00:00:00Z', updated_at: '2026-09-20T00:00:00Z' },
    { id: GROUP_OPS, display_name: 'Operations', member_count: 1, created_at: '2026-06-02T00:00:00Z', updated_at: '2026-09-20T00:00:00Z' },
  ],
} satisfies z.infer<typeof zScimDirectoryGroupList>;

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
  component: ScimProvisioningPage,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: at('', [{ url: BINDINGS_URL, body: bindings }]),
  },
} satisfies Meta<typeof ScimProvisioningPage>;

export default meta;
type Story = StoryObj<typeof meta>;

// One binding selected: mappings (a live row with origins and an inert one),
// credentials in every lifecycle state, and a directory with active,
// deprovisioned, and deprovisioned-with-manual-grants users.
export const Administering: Story = {
  parameters: { app: at(`?binding=${BINDING}`, administering) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: 'Administering' })).toBeVisible();
    await expect(await canvas.findByRole('heading', { name: 'Engineering' })).toBeVisible();
    await expect(canvas.getByText('inert')).toBeVisible();
    await expect(await canvas.findByText('revoked')).toBeVisible();
    await expect(canvas.getByText('expired')).toBeVisible();
    await expect(await canvas.findByText('deprovisioned, manual grants remain')).toBeVisible();
    await expect(canvas.getByText('in no groups')).toBeVisible();
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
    await expect(canvas.getByRole('button', { name: 'Create binding' })).toBeDisabled();
  },
};

// The bindings read failed: the panel names the failure and lists nothing.
export const Failed: Story = {
  parameters: { app: at('', [{ url: BINDINGS_URL, status: 500, body: { code: 'internal' } }]) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('alert')).toHaveTextContent(/server failed while reading/i);
  },
};
