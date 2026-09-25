import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  zDeliveryTargetList,
  zDynamicLeaseList,
  zMeta,
  zDynamicProviderList,
  zEnvironmentList,
  zGrantList,
  zMachineCredentialList,
  zMachineRevealSettings,
  zServiceAccountList,
} from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG, PRJ, PROD, STAGING } from '../testkit/ids.ts';
import { target } from '../testkit/deliveryTargets.ts';
import { dynamicProvider } from '../testkit/machineAccess.ts';
import { MachineAccessPage } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The page behind `gateSystemScope` (the exported `MachineAccess` wraps it):
// storied directly so no instance-config read is needed. It calls `useAuth` for
// the live session id, so the harness runs it with `auth: true`. Every
// load-time GET below needs a row or the fetch stub 404s into the page's own
// alerts: service accounts, grants, machine-reveal, environments, providers,
// one credential listing per account and one lease listing per environment.
// Wire fixtures are `z.input` shapes: int64 fields travel as JSON numbers (a
// bigint cannot be stringified), and `parsed()` coerces them on the way in.
const PATH = `/orgs/${ORG}/projects/${PRJ}/machine-access`;
const ROUTE = '/orgs/:org/projects/:project/machine-access';
const BASE = `/api/v1/orgs/${ORG}/projects/${PRJ}`;

const environments = {
  count: 2,
  items: [
    { id: PROD, org_id: ORG, project_id: PRJ, name: 'production', display_order: 0, created_at: '2026-01-01T00:00:00Z' },
    { id: STAGING, org_id: ORG, project_id: PRJ, name: 'staging', display_order: 1, created_at: '2026-01-01T00:00:00Z' },
  ],
} satisfies z.input<typeof zEnvironmentList>;

const GATEWAY = 'msa_123e4567-e89b-12d3-a456-426614174020';
const REPORT = 'msa_123e4567-e89b-12d3-a456-426614174021';
const BUILDER = 'msa_123e4567-e89b-12d3-a456-426614174022';
const OPERATOR = 'prn_123e4567-e89b-12d3-a456-426614174010';
const account = (
  id: string,
  name: string,
  kind: 'workload' | 'automation',
  live: number,
): z.input<typeof zServiceAccountList>['items'][number] => ({
  id,
  principal_id: id.replace('msa_', 'prn_'),
  name,
  kind,
  created_at: '2026-08-01T00:00:00Z',
  created_by: OPERATOR,
  live_credentials: live,
});
const accounts = {
  count: 3,
  items: [
    account(GATEWAY, 'api-gateway', 'workload', 2),
    account(REPORT, 'nightly-report', 'automation', 1),
    account(BUILDER, 'image-builder', 'workload', 0),
  ],
} satisfies z.input<typeof zServiceAccountList>;

const grant = (
  n: number,
  principal: string,
  capability: 'read' | 'reveal',
  environment: string,
): z.input<typeof zGrantList>['items'][number] => ({
  id: `grt_123e4567-e89b-12d3-a456-4266141740${String(30 + n)}`,
  principal_id: principal,
  capability,
  scope: { org_id: ORG, project_id: PRJ, environment_id: environment },
  origins: [{ kind: 'direct', subject: 'dana@example.com' }],
  created_at: '2026-08-02T00:00:00Z',
});
const grants = {
  count: 4,
  items: [
    grant(0, GATEWAY.replace('msa_', 'prn_'), 'read', PROD),
    grant(1, GATEWAY.replace('msa_', 'prn_'), 'reveal', PROD),
    grant(2, GATEWAY.replace('msa_', 'prn_'), 'read', STAGING),
    grant(3, REPORT.replace('msa_', 'prn_'), 'read', PROD),
  ],
} satisfies z.input<typeof zGrantList>;

// One bearer near its expiry (danger tier) and one GitHub Actions binding.
const inDays = (days: number) => new Date(Date.now() + days * 86_400_000).toISOString();
const gatewayCredentials = {
  count: 2,
  items: [
    {
      id: 'mcr_123e4567-e89b-12d3-a456-426614174040',
      kind: 'hikyo-token',
      prefix_hint: 'hk_9f3a',
      lifetime: 'finite',
      expires_at: inDays(5),
      created_at: '2026-08-10T00:00:00Z',
      created_by: OPERATOR,
      last_used_at: '2026-09-20T08:00:00Z',
      expiring_soon: true,
    },
    {
      id: 'mcr_123e4567-e89b-12d3-a456-426614174041',
      kind: 'oidc-federation',
      lifetime: 'finite',
      expires_at: inDays(80),
      issuer: 'https://token.actions.githubusercontent.com',
      subject: 'repo:acme/gateway:ref:refs/heads/main',
      audience: 'hikyo',
      required_claims: [
        { claim: 'repository_id', number_value: 987654321 },
        { claim: 'repository_owner_id', number_value: 4242 },
        { claim: 'event_name', string_value: 'push' },
      ],
      created_at: '2026-08-12T00:00:00Z',
      created_by: OPERATOR,
      expiring_soon: false,
    },
  ],
} satisfies z.input<typeof zMachineCredentialList>;
const reportCredentials = {
  count: 1,
  items: [
    {
      id: 'mcr_123e4567-e89b-12d3-a456-426614174042',
      kind: 'hikyo-token',
      prefix_hint: 'hk_c01d',
      lifetime: 'indefinite',
      created_at: '2026-07-01T00:00:00Z',
      created_by: OPERATOR,
      expiring_soon: false,
    },
  ],
} satisfies z.input<typeof zMachineCredentialList>;
const noCredentials = { count: 0, items: [] } satisfies z.input<typeof zMachineCredentialList>;

const PROVIDER = dynamicProvider.id;
const providers = {
  items: [
    dynamicProvider,
    {
      id: 'dpv_123e4567-e89b-12d3-a456-426614174051',
      kind: 'postgres',
      origin: 'legacy-db.example.com:5432',
      tls_mode: 'verify-full',
      grant_role: 'hikyo_leases',
      credential_present: false,
      authority_principal_id: OPERATOR,
      state: 'tombstoned',
      created_at: '2026-06-20T00:00:00Z',
    },
  ],
} satisfies z.input<typeof zDynamicProviderList>;

const lease = (
  n: number,
  environment: string,
  state: 'active' | 'unknown' | 'revoked',
): z.input<typeof zDynamicLeaseList>['items'][number] => ({
  id: `dls_123e4567-e89b-12d3-a456-4266141740${String(60 + n)}`,
  provider_id: PROVIDER,
  environment_id: environment,
  principal_id: GATEWAY.replace('msa_', 'prn_'),
  principal_class: 'workload',
  provider_handle: `hk_lease_${String(n)}`,
  state,
  issued_at: '2026-09-22T08:00:00Z',
  expires_at: state === 'revoked' ? null : inDays(1),
  max_ttl_seconds: 3600,
  last_transition_at: '2026-09-22T08:00:00Z',
  created_at: '2026-09-22T08:00:00Z',
});
const prodLeases = { items: [lease(0, PROD, 'active'), lease(1, PROD, 'unknown')] } satisfies z.input<typeof zDynamicLeaseList>;
const stagingLeases = { items: [lease(2, STAGING, 'revoked')] } satisfies z.input<typeof zDynamicLeaseList>;

const prodTargets = {
  principals: [{ principal_id: GATEWAY.replace('msa_', 'prn_'), last_contact_at: '2026-09-25T11:58:00Z' }],
  // The testkit target reports as api-gateway's principal.
  targets: [target(0, 'reported')],
} satisfies z.input<typeof zDeliveryTargetList>;

// The server advertises the report vocabulary, or the tab reads no list.
const META_URL = '/api/v1/meta';
const metaBody = (protocol_capabilities: string[]) =>
  ({ server_version: '1.4.0', api_revision: 5, protocol_capabilities }) satisfies z.input<typeof zMeta>;

const optIn = (enabled: boolean) => ({ enabled }) satisfies z.input<typeof zMachineRevealSettings>;

const ACCOUNTS_URL = `${BASE}/service-accounts`;
const REVEAL_URL = `${BASE}/machine-reveal`;
const credentialsUrl = (sa: string) => `${ACCOUNTS_URL}/${sa}/credentials`;
const leasesUrl = (env: string) => `${BASE}/environments/${env}/leases`;
const targetsUrl = (env: string) => `${BASE}/environments/${env}/delivery-targets`;

// Everything the populated page reads, opt-in on. Stories replace rows by
// prepending: a string row matches the exact path, and the first match wins.
const populatedResponses: readonly MockRoute[] = [
  { url: ACCOUNTS_URL, body: accounts },
  { url: `${BASE}/grants`, body: grants },
  { url: REVEAL_URL, body: optIn(true) },
  { url: `${BASE}/environments`, body: environments },
  { url: `${BASE}/dynamic-providers`, body: providers },
  { url: credentialsUrl(GATEWAY), body: gatewayCredentials },
  { url: credentialsUrl(REPORT), body: reportCredentials },
  { url: credentialsUrl(BUILDER), body: noCredentials },
  { url: leasesUrl(PROD), body: prodLeases },
  { url: leasesUrl(STAGING), body: stagingLeases },
  // Production carries one fresh report; staging is unreadable, so its 404 is
  // the uniform nonexistent answer and it contributes no rows.
  { url: targetsUrl(PROD), body: prodTargets },
  { url: META_URL, body: metaBody(['local-password', 'delivery-target-report/1']) },
];

const app = (responses: readonly MockRoute[]) => ({
  auth: true,
  path: PATH,
  routePath: ROUTE,
  responses,
});

const meta = {
  component: MachineAccessPage,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: app(populatedResponses) },
} satisfies Meta<typeof MachineAccessPage>;

export default meta;
type Story = StoryObj<typeof meta>;

// Three accounts across the journey: one reading and revealing production
// (a bearer days from expiry plus a federated binding), one automation
// principal, one fresh workload with nothing yet. Opt-in on.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: /api-gateway/ })).toBeVisible();
    await expect(canvas.getByRole('tab', { name: 'Service accounts (3)' })).toBeVisible();
    await expect(canvas.getByText(/opt-in\): on/)).toBeVisible();
  },
};

// A row opened: credentials and bindings left, targets and actions right,
// the five-step setup journey full-width below.
export const ExpandedRow: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: /api-gateway/ }));
    await expect(await canvas.findByRole('heading', { name: 'Setup journey' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Mint credential for api-gateway' })).toBeEnabled();
  },
};

// The Federation tab: every binding as a card with its byte-exact pins.
export const FederationTab: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('tab', { name: 'Federation (1)' }));
    await expect(
      await canvas.findByRole('button', { name: 'Replace binding on api-gateway' }),
    ).toBeVisible();
    await expect(canvas.getByText('repo:acme/gateway:ref:refs/heads/main')).toBeVisible();
  },
};

// The Providers tab: an active provider with its actions and a tombstoned one.
export const ProvidersTab: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('tab', { name: 'Providers (2)' }));
    await expect(await canvas.findByRole('button', { name: 'Replace credential' })).toBeVisible();
    await expect(canvas.getByText('tombstoned')).toBeVisible();
  },
};

// The Kubernetes tab: the production report grouped by cluster and namespace;
// staging is unreadable, so it is absent rather than empty.
export const KubernetesTab: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('tab', { name: 'Kubernetes targets (1)' }));
    await expect(await canvas.findByText('api-secrets-0')).toBeVisible();
    await expect(canvas.getByText(/^reported by the controller, /)).toBeVisible();
    await expect(canvas.queryByText('staging')).not.toBeInTheDocument();
  },
};

// A server without delivery-target reporting: the tab says so, reads no list
// (production's report is on offer and never shown), and counts unknown.
export const KubernetesTabUnsupported: Story = {
  parameters: {
    app: app([{ url: META_URL, body: metaBody(['local-password']) }, ...populatedResponses]),
  },
  play: async ({ canvas }) => {
    // "(unknown)" also matches the loading page: wait for the settled one.
    await canvas.findByRole('tab', { name: 'Service accounts (3)' });
    await userEvent.click(canvas.getByRole('tab', { name: 'Kubernetes targets (unknown)' }));
    await expect(
      await canvas.findByText(/this server does not support delivery-target reporting:/i),
    ).toBeVisible();
    await expect(canvas.queryByText('api-secrets-0')).not.toBeInTheDocument();
  },
};

// The Leases tab: an active, an unknown (awaiting reconcile) and a revoked lease.
export const LeasesTab: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('tab', { name: 'Leases (3)' }));
    await expect(await canvas.findByText('unknown, awaiting reconcile')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Settle' })).toBeVisible();
    await expect(canvas.getByText('1 lease awaiting reconcile')).toBeVisible();
  },
};

// A fresh project: no accounts, environments or providers; opt-in off.
export const Empty: Story = {
  parameters: {
    app: app([
      { url: ACCOUNTS_URL, body: { count: 0, items: [] } },
      { url: `${BASE}/grants`, body: { count: 0, items: [] } },
      { url: REVEAL_URL, body: optIn(false) },
      { url: `${BASE}/environments`, body: { count: 0, items: [] } },
      { url: `${BASE}/dynamic-providers`, body: { items: [] } },
    ]),
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no service accounts on this project yet/i)).toBeVisible();
    await expect(canvas.getByText(/opt-in\): off/)).toBeVisible();
  },
};

// Nothing has settled: counts read "unknown" and the policy strip is reading.
export const Loading: Story = {
  parameters: {
    app: app([
      { url: ACCOUNTS_URL, pending: true },
      { url: `${BASE}/grants`, pending: true },
      { url: REVEAL_URL, pending: true },
      { url: `${BASE}/environments`, pending: true },
      { url: `${BASE}/dynamic-providers`, pending: true },
    ]),
  },
  play: async ({ canvas }) => {
    await waitFor(() =>
      expect(canvas.getByText(/reading the project.s machine-reveal opt-in/i)).toBeVisible(),
    );
    await expect(canvas.getByRole('tab', { name: 'Service accounts (unknown)' })).toBeVisible();
    await expect(canvas.getByRole('tab', { name: 'Kubernetes targets (unknown)' })).toBeVisible();
  },
};

// The listing is refused: the alert names the capability it needs.
export const Refused: Story = {
  parameters: {
    app: app([{ url: ACCOUNTS_URL, status: 403, body: { error: 'forbidden' } }, ...populatedResponses]),
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/needs manage-identities on this project/i)).toBeVisible();
  },
};

// The listing failed for a non-permission reason: reload, no capability named.
export const Failed: Story = {
  parameters: {
    app: app([{ url: ACCOUNTS_URL, status: 500, body: { error: 'boom' } }, ...populatedResponses]),
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/reload to try again/i)).toBeVisible();
  },
};
