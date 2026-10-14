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
  zSshCertificateList,
  zSshProfileList,
  zSshcaList,
} from '@hikyo/zod';
import { expect, userEvent, waitFor, within } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG, PRJ, PROD, STAGING } from '../testkit/ids.ts';
import { target } from '../testkit/deliveryTargets.ts';
import { dynamicProvider } from '../testkit/machineAccess.ts';
import { MachineAccessPage } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { openTab } from '../../.storybook/openTab.ts';

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

const app = (responses: readonly MockRoute[], path = PATH) => ({
  auth: true,
  path,
  routePath: ROUTE,
  responses,
});

const meta = {
  title: 'Pages/MachineAccess',
  id: 'routes-machineaccess',
  component: MachineAccessPage,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "The project machine-access screen: service accounts, credentials, bindings, leases and delivery information. Its catalogues use the real page and API contracts; private-key disclosure and browser-owned passkey prompts are deliberately excluded." } },  app: app(populatedResponses) },
} satisfies Meta<typeof MachineAccessPage>;

export default meta;
type Story = StoryObj<typeof meta>;

// Three accounts across the journey: one reading and revealing production
// (a bearer days from expiry plus a federated binding), one automation
// principal, one fresh workload with nothing yet. Opt-in on. The title and
// tabs are page chrome; the tab's module is the card under them.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: /api-gateway/ })).toBeVisible();
    const accountsTab = canvas.getByRole('tab', { name: 'Service accounts, 3' });
    await expect(accountsTab).toBeVisible();
    // The count is a pill after the label, not "(3)" spelled into it.
    await expect(accountsTab.querySelector('.tab__count')).toHaveTextContent('3');
    await expect(accountsTab).not.toHaveTextContent('(');
    await expect(canvas.getByText(/opt-in\): on/)).toBeVisible();
    await expect(canvas.getByRole('heading', { level: 1 }).closest('.card')).toBeNull();
    await expect(canvas.getByRole('tablist').closest('.card')).toBeNull();
    const card = canvas.getByRole('heading', { name: 'Service accounts', level: 2 }).closest('.card');
    await expect(card).not.toBeNull();
    await expect(card?.querySelector('table')).not.toBeNull();
    // Create stands clear of the table: the card's gap spaces them.
    const create = canvas.getByRole('button', { name: 'Create service account' }).getBoundingClientRect();
    const table = canvas.getByRole('table').getBoundingClientRect();
    await expect(table.top - create.bottom).toBeGreaterThanOrEqual(8);
    // An account's kind is a tag with the kind's icon; an account
    // with no setup journey shows "NA", named in full for assistive tech.
    const kinds = [...canvas.getByRole('table').querySelectorAll('td .badge')].filter((tag) => /^(workload|automation)$/.test(tag.textContent ?? ''));
    await expect(kinds.length).toBeGreaterThan(0);
    for (const kind of kinds) await expect(kind.querySelector('svg')?.getAttribute('data-glyph')).toBe(kind.textContent);
    const na = canvas.getAllByTitle('not applicable');
    for (const tag of na) {
      await expect(tag).toHaveTextContent('not applicable');
      await expect(tag.querySelector('[aria-hidden="true"]:not(svg)')).toHaveTextContent('NA');
    }
    // A long "kind: subject" origin chip ellipsises at `--chip-id-max` rather
    // than driving the table wider; the full id
    // still rides in `title` for assistive tech and a hover.
    const origins = [...canvas.getByRole('table').querySelectorAll<HTMLElement>('.machine__origin')];
    await expect(origins.length).toBeGreaterThan(0);
    for (const origin of origins) {
      const max = Number.parseFloat(getComputedStyle(origin).maxWidth);
      await expect(origin.getBoundingClientRect().width).toBeLessThanOrEqual(max + 0.5);
      await expect(origin.title).toBe(origin.textContent);
      await expect(origin.title.length).toBeGreaterThan(0);
    }
  },
};

// The inventory lays every row out the same: name buttons never wrap (their
// chevrons line up), and dates, badges and identifiers stay on one line, a long
// identifier ellipsising with its full text in the title.
export const AccountRowsAligned: Story = {
  play: async ({ canvas }) => {
    await canvas.findByRole('button', { name: /api-gateway/ });
    const table = canvas.getByRole('table');
    const chevrons = [...table.querySelectorAll('.values__keyname .glyph')].map((glyph) => glyph.getBoundingClientRect().left);
    await expect(new Set(chevrons.map(Math.round)).size).toBe(1);
    for (const node of table.querySelectorAll<HTMLElement>('.values__keyname, td.col-secondary, .badge')) {
      await expect(getComputedStyle(node).whiteSpace).toBe('nowrap');
    }
    for (const origin of table.querySelectorAll<HTMLElement>('.machine__origin')) {
      await expect(origin.title).toBe(origin.textContent);
      await expect(getComputedStyle(origin).textOverflow).toBe('ellipsis');
    }
  },
};

// At a phone width the table drops to the compact type size and tighter cell
// padding, the account name included. This table's four visible
// columns (Credentials and Last used already drop at phone, `.col-secondary`)
// still exceed a phone width on their own content, so it keeps its sideways
// scroll by design; the width==container case is checked where it actually
// holds, on the Who-can grants table (Members.stories.tsx).
export const PhoneCompactTable: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    const row = await canvas.findByRole('button', { name: /api-gateway/ });
    const table = canvas.getByRole('table');
    const compact = getComputedStyle(document.documentElement).getPropertyValue('--fs-xs');
    const name = row.querySelector('.machine__name');
    await expect(name).not.toBeNull();
    await expect(getComputedStyle(name!).fontSize).toBe(compact.trim());
    const cell = table.querySelector('td, th:not([scope="col"])');
    await expect(cell).not.toBeNull();
    await expect(getComputedStyle(cell!).paddingTop).toBe(getComputedStyle(document.documentElement).getPropertyValue('--space-1').trim());
  },
};

// An account's detail opens in the side panel over the inventory:
// credentials and bindings left, targets and actions right, the five-step
// setup journey full-width below.
export const AccountPanel: Story = {
  play: async ({ canvas }) => {
    const row = await canvas.findByRole('button', { name: /api-gateway/ });
    await userEvent.click(row);
    const panel = await canvas.findByRole('complementary', { name: 'Service account api-gateway' });
    // The row opens a panel, it does not expand in place: no disclosure state.
    await expect(row).not.toHaveAttribute('aria-expanded');
    await expect(within(panel).getByRole('heading', { name: 'Setup journey' })).toBeVisible();
    await expect(within(panel).getByRole('heading', { name: 'Federated bindings' })).toBeVisible();
    await expect(
      within(panel).getByRole('button', { name: 'Mint credential for api-gateway' }),
    ).toBeEnabled();
  },
};

// At a phone width the panel takes the full width, and its four account
// actions, full-sentence labels, stack in one full-width column.
export const AccountPanelPhone: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: /api-gateway/ }));
    const panel = await canvas.findByRole('complementary', { name: 'Service account api-gateway' });
    // Wait out the slide-in animation before measuring: mid-animation the
    // panel sits at its `translateX(100%)` starting point.
    await waitFor(() => expect(panel.getAnimations()).toHaveLength(0));
    const box = panel.getBoundingClientRect();
    await expect(Math.round(box.left)).toBe(0);
    await expect(Math.round(box.width)).toBe(document.documentElement.clientWidth);
    const mint = within(panel).getByRole('button', { name: 'Mint credential for api-gateway' });
    const row = mint.closest<HTMLElement>('.actions');
    await expect(row).not.toBeNull();
    const width = Math.round(row!.getBoundingClientRect().width);
    const buttons = [...row!.querySelectorAll<HTMLElement>(':scope > .btn')].map((button) => button.getBoundingClientRect());
    await expect(buttons).toHaveLength(4);
    for (const [index, button] of buttons.entries()) {
      await expect(Math.round(button.left)).toBe(Math.round(buttons[0]!.left));
      await expect(Math.round(button.width)).toBe(width);
      if (index > 0) await expect(button.top).toBeGreaterThanOrEqual(buttons[index - 1]!.bottom);
    }
    await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
  },
};

// An automation principal's panel: no setup journey, it never delivers to a
// workload.
export const AutomationAccountPanel: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: /nightly-report/ }));
    const panel = await canvas.findByRole('complementary', { name: 'Service account nightly-report' });
    await expect(within(panel).getByText(/has no setup journey/)).toBeVisible();
    await expect(within(panel).queryByRole('heading', { name: 'Federated bindings' })).toBeNull();
  },
};

// An act taken in the panel reports there, not on the page under its scrim.
export const AccountPanelOutcome: Story = {
  parameters: {
    app: app([
      { url: `${credentialsUrl(GATEWAY)}/mcr_123e4567-e89b-12d3-a456-426614174040`, method: 'DELETE', status: 204 },
      ...populatedResponses,
    ]),
  },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: /api-gateway/ }));
    const panel = await canvas.findByRole('complementary', { name: 'Service account api-gateway' });
    await userEvent.click(within(panel).getByRole('button', { name: 'Revoke hk_9f3a' }));
    await expect(await within(panel).findByText(/stops authenticating at the next request/)).toBeVisible();
    await expect(canvas.getAllByText(/stops authenticating at the next request/)).toHaveLength(1);
  },
};

// The Federation tab: every binding as a card with its byte-exact pins.
export const FederationTab: Story = {
  play: async ({ canvas }) => {
    await openTab(canvas, 'Federation, 1');
    await expect(
      await canvas.findByRole('button', { name: 'Replace binding on api-gateway' }),
    ).toBeVisible();
    await expect(canvas.getByText('repo:acme/gateway:ref:refs/heads/main')).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'Federated bindings', level: 2 }).closest('.card')).not.toBeNull();
  },
};

// The Providers tab: an active provider with its actions and a tombstoned one.
export const ProvidersTab: Story = {
  play: async ({ canvas }) => {
    await openTab(canvas, 'Providers, 2');
    await expect(await canvas.findByRole('button', { name: 'Replace credential' })).toBeVisible();
    await expect(canvas.getByText('tombstoned')).toBeVisible();
    const card = canvas.getByRole('heading', { name: 'Dynamic-secret providers', level: 2 }).closest('.card');
    await expect(card).not.toBeNull();
    // The table scrolls inside its own container, never the page.
    await expect(card?.querySelector('.table-scroll > table')).not.toBeNull();
  },
};

// The Kubernetes tab: the production report grouped by cluster and namespace;
// staging is unreadable, so it is absent rather than empty.
export const KubernetesTab: Story = {
  play: async ({ canvas }) => {
    await openTab(canvas, 'Kubernetes targets, 1');
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
    // ", unknown" also matches the loading page: wait for the settled one.
    await canvas.findByRole('tab', { name: 'Service accounts, 3' });
    await openTab(canvas, 'Kubernetes targets, unknown');
    await expect(
      await canvas.findByText(/this server does not support delivery-target reporting:/i),
    ).toBeVisible();
    await expect(canvas.queryByText('api-secrets-0')).not.toBeInTheDocument();
  },
};

// The Leases tab: an active, an unknown (awaiting reconcile) and a revoked lease.
export const LeasesTab: Story = {
  play: async ({ canvas }) => {
    await openTab(canvas, 'Leases, 3');
    await expect(await canvas.findByText('unknown, awaiting reconcile')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Settle' })).toBeVisible();
    await expect(canvas.getByText('1 lease awaiting reconcile')).toBeVisible();
    const card = canvas.getByRole('heading', { name: 'Dynamic-secret leases', level: 2 }).closest('.card');
    await expect(card).not.toBeNull();
    await expect(card?.querySelector('.table-scroll > table')).not.toBeNull();
  },
};

// The SSH tab, opened from the URL (`?tab=ssh`): the intro and environment
// choice, then one card per subsection.
const sshBase = `${BASE}/environments/${PROD}`;
export const SshTab: Story = {
  parameters: {
    app: app(
      [
        { url: `${sshBase}/ssh-cas`, body: zSshcaList.parse({ items: [] }) },
        { url: `${sshBase}/ssh-profiles`, body: zSshProfileList.parse({ items: [] }) },
        { url: `${sshBase}/ssh-certificates`, body: zSshCertificateList.parse({ items: [] }) },
        ...populatedResponses,
      ],
      `${PATH}?tab=ssh`,
    ),
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('tab', { name: 'SSH certificates' })).toHaveAttribute('aria-selected', 'true');
    await expect(await canvas.findByRole('heading', { name: 'SSH certificates', level: 2 })).toBeVisible();
    for (const name of ['Certificate authorities', 'Profiles', 'Certificates']) {
      const card = canvas.getByRole('heading', { name, level: 3 }).closest('.card');
      await expect(card).not.toBeNull();
      await expect(card?.querySelector('table')).not.toBeNull();
    }
    await expect(await canvas.findByText(/no ssh ca in this environment yet/i)).toBeVisible();
  },
};

// The Certificates tab, opened from the URL: one card.
export const CertificatesTab: Story = {
  parameters: {
    app: app(
      [
        { url: `${BASE}/environments/${PROD}/certificates`, body: { items: [] } },
        { url: `${BASE}/environments/${STAGING}/certificates`, body: { items: [] } },
        ...populatedResponses,
      ],
      `${PATH}?tab=certificates`,
    ),
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('tab', { name: 'Certificates, 0' })).toHaveAttribute('aria-selected', 'true');
    const card = canvas.getByRole('heading', { name: 'Certificates', level: 2 }).closest('.card');
    await expect(card).not.toBeNull();
    await expect(await canvas.findByText('No certificates in this project yet.')).toBeVisible();
  },
};

// An unknown `?tab=` falls back to the first tab rather than an empty page.
export const UnknownTabParam: Story = {
  parameters: { app: app(populatedResponses, `${PATH}?tab=nonsense`) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('tab', { name: 'Service accounts, 3' })).toHaveAttribute('aria-selected', 'true');
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
    await expect(canvas.getByRole('tab', { name: 'Service accounts, unknown' })).toBeVisible();
    await expect(canvas.getByRole('tab', { name: 'Kubernetes targets, unknown' })).toBeVisible();
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

// On a phone the tab bar wraps into rows of pills: nothing scrolls sideways,
// neither the bar nor the page.
export const PhoneTabPills: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, canvasElement }) => {
    await canvas.findByRole('tab', { name: 'Service accounts, 3' });
    const bar = canvas.getByRole('tablist');
    await expect(getComputedStyle(bar).flexWrap).toBe('wrap');
    const tops = canvas.getAllByRole('tab').map((tab) => Math.round(tab.getBoundingClientRect().top));
    await expect(new Set(tops).size).toBeGreaterThan(1);
    await expect(bar.scrollWidth).toBeLessThanOrEqual(bar.clientWidth);
    // The story's own mounted root, not `document.documentElement`: the Vitest
    // browser-mode runner's surrounding page is wider than the story, and
    // checking the whole document would fail on that, not on this component.
    await expect(canvasElement.scrollWidth).toBeLessThanOrEqual(canvasElement.clientWidth);
  },
};

// At a phone width a provider row's actions sit on the two-column grid,
// equal-width cells, instead of wrapping raggedly inside the table cell.
export const PhoneProviderActions: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  parameters: { app: app(populatedResponses, `${PATH}?tab=providers`) },
  play: async ({ canvas }) => {
    const row = (await canvas.findByRole('button', { name: 'Replace credential' })).closest<HTMLElement>('.actions');
    await expect(row).not.toBeNull();
    await expect(getComputedStyle(row!).display).toBe('grid');
    await expect(getComputedStyle(row!).gridTemplateColumns.split(' ')).toHaveLength(2);
    const [a, b] = [...row!.querySelectorAll(':scope > .btn')].map((button) => button.getBoundingClientRect());
    await expect(Math.round(a!.top)).toBe(Math.round(b!.top));
    await expect(Math.round(a!.width)).toBe(Math.round(b!.width));
  },
};
