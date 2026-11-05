// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { consentIds, consentLookup, type ConsentLookupFailures } from '../testkit/consentNames.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { CLIReauth } from './CLIReauth.tsx';

const mocks = vi.hoisted(() => ({ load: vi.fn() }));

vi.mock('../api/cliReauth.ts', () => ({
  loadCLIReauthTransaction: mocks.load,
  approveCLIReauth: vi.fn(),
  cliReauthCallbackURL: vi.fn(),
}));
vi.mock('../api/account.ts', () => ({
  useTotpStatus: () => ({ isSuccess: true, data: { confirmed: false, has_passkey: true, can_reauth_oidc: false } }),
  useSessionOIDCProvider: () => null,
  useAuthMethods: () => ({ isError: false, refetch: vi.fn() }),
}));
vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ state: { status: 'authenticated' }, identity: { session: { assurance: { method: 'local-password' } } }, refreshSession: vi.fn() }),
}));
vi.mock('../api/values.ts', () => ({
  runDeveloperCredentialPasskeyCeremony: vi.fn(),
  runDeveloperCredentialTOTPCeremony: vi.fn(),
  runAdapterPasskeyCeremony: vi.fn(),
  runAdapterTOTPCeremony: vi.fn(),
  runPasskeyCeremony: vi.fn(),
  runOIDCCeremony: vi.fn(),
  runTOTPCeremony: vi.fn(),
}));

const fetcher = vi.fn<typeof fetch>();
const policy = (environmentId: string, org = consentIds.org, project = consentIds.project) => ({
  org_id: org, project_id: project, environment_id: environmentId, effective_window_seconds: 0, requires_webauthn: true,
});
const keyIds = [consentIds.secretKey, consentIds.configKey, consentIds.missingKey];

function route(transaction: object, failures: ConsentLookupFailures = {}, keys?: () => Promise<void>) {
  mocks.load.mockResolvedValue({
    state: 'txn-names', operation: 'value.reveal', purpose: 'reveal', environments: [policy(consentIds.environment)],
    key_ids: keyIds, redirect_uri: 'http://127.0.0.1:40126/callback', expires_at: '2099-08-23T12:00:00Z', ...transaction,
  });
  fetcher.mockImplementation(async (input) => {
    if (!(input instanceof Request)) throw new Error('expected SDK Request');
    const lookup = consentLookup(new URL(input.url).pathname, failures, keys);
    if (lookup === undefined) throw new Error(`unexpected ${input.url}`);
    return lookup;
  });
}
async function render() {
  globalThis.history.replaceState({}, '', '/auth/cli/reauth?transaction=txn-names');
  const view = await renderForm(<CLIReauth />);
  await settleTask();
  return view;
}
const authorize = (container: HTMLElement) => {
  const found = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Authorize CLI');
  if (found === undefined) throw new Error('missing Authorize CLI');
  return found;
};
const requested = () => fetcher.mock.calls.map(([input]) => (input instanceof Request ? new URL(input.url).pathname : ''));

beforeEach(() => {
  fetcher.mockReset();
  mocks.load.mockReset();
  vi.stubGlobal('fetch', fetcher);
});
afterEach(() => vi.unstubAllGlobals());

describe('CLI authorization names', () => {
  it('names the scope above the keys and keeps every bound key listed with its ID', async () => {
    route({});
    const { container, unmount } = await render();
    try {
      const text = container.textContent ?? '';
      expect(text).toContain('Acme');
      expect(text).toContain('Billing');
      expect(text).toContain('production …000001 (passkey required)');
      expect(text).not.toContain(consentIds.environment);
      expect(text).toContain('3 keys');
      const rows = [...container.querySelectorAll('.ceremony__keys li')];
      expect(rows.map((row) => row.getAttribute('title'))).toEqual(keyIds);
      expect(rows[0]?.textContent).toContain('secret DATABASE_URL');
      expect(rows[1]?.textContent).toBe('LOG_LEVEL …000002');
      // Absent from the catalogue, so listed by its ID.
      expect(rows[2]?.textContent).toBe(consentIds.missingKey);
      expect(container.querySelector('.ceremony__keys')?.getAttribute('tabindex')).toBe('0');
      expect(text.indexOf('production')).toBeLessThan(text.indexOf('DATABASE_URL'));
    } finally { await unmount(); }
  });

  it('falls back to IDs when every lookup fails and still allows approval', async () => {
    route({}, { keys: true, environments: true, org: true, project: true });
    const { container, unmount } = await render();
    try {
      const text = container.textContent ?? '';
      expect(text).toContain(consentIds.org);
      expect(text).toContain(consentIds.project);
      expect(text).toContain(`${consentIds.environment} (passkey required)`);
      expect(text).toContain(consentIds.secretKey);
      expect(authorize(container).disabled).toBe(false);
    } finally { await unmount(); }
  });

  it('keeps Authorize disabled until the name lookups have settled', async () => {
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    route({}, {}, () => gate);
    const { container, unmount } = await render();
    try {
      expect(authorize(container).disabled).toBe(true);
      await act(async () => release());
      await settleTask();
      expect(authorize(container).disabled).toBe(false);
    } finally { await unmount(); }
  });

  it('lists several adapter environments without a key list', async () => {
    const other = consentIds.environment;
    route({ purpose: 'adapter', operation: 'adapter.sync', environments: [policy(other), policy('env_00000000-0000-4000-8000-0000000000ff')], key_ids: [] });
    const { container, unmount } = await render();
    try {
      expect(container.querySelectorAll('ul[aria-label="Environments this decision covers"] li')).toHaveLength(2);
      expect(container.querySelector('.ceremony__keys')).toBeNull();
      expect(requested().some((path) => path.endsWith('/keys'))).toBe(false);
      expect(container.textContent).toContain('production …000001 (passkey required)');
      expect(authorize(container).disabled).toBe(false);
    } finally { await unmount(); }
  });

  it('keeps IDs and looks nothing up when the environments span projects', async () => {
    route({ environments: [policy(consentIds.environment), policy('env_00000000-0000-4000-8000-0000000000ff', consentIds.org, 'prj_00000000-0000-4000-8000-0000000000ee')] });
    const { container, unmount } = await render();
    try {
      expect(requested()).toEqual([]);
      expect(container.textContent).toContain(consentIds.environment);
      expect(authorize(container).disabled).toBe(false);
    } finally { await unmount(); }
  });

  it('makes no lookup for a self-config decision with no environments', async () => {
    route({
      purpose: 'self-config', operation: 'self-config.apply', environments: [], key_ids: [],
      self_config: { action: 'apply', owner_instance_id: 'instance_local', revision: 3, expected_generation: 7, schema_version: 1, to: '', preview_token: '', confirm_restored_credentials: false },
    });
    const { container, unmount } = await render();
    try {
      expect(requested()).toEqual([]);
      expect(container.querySelector('.ceremony__keys')).toBeNull();
      expect(authorize(container).disabled).toBe(false);
    } finally { await unmount(); }
  });
});
