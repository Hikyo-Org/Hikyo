// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import type { AdapterTargetInput } from '../api/adapters.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { Adapters, TargetForm } from './Adapters.tsx';
import { awsAccessComplete, awsAccessDescriptor, emptyAwsAccess } from './AwsAccessFields.tsx';

function selectValue(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set;
  setter?.call(select, value);
  select.dispatchEvent(new Event('change', { bubbles: true }));
}

afterEach(() => vi.unstubAllGlobals());

it.each([
  ['forgejo', 'Forgejo'],
  ['github-actions', 'GitHub Actions'],
  ['gitlab', 'GitLab'],
  ['aws-secrets-manager', 'AWS Secrets Manager'],
  ['vault-kv', 'Vault / OpenBao KV'],
  ['cloudflare', 'Cloudflare Workers & Pages'],
  ['future-provider', 'future-provider'],
])('renders the %s provider through the response decoder', async (provider, label) => {
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    if (request.method !== 'GET') throw new Error(`unexpected ${request.method}`);
    const base = '/api/v1/orgs/acme/projects/app';
    if (path === `${base}/adapters`) {
      return Promise.resolve(Response.json({ items: [{
        id: 'adp_00000000-0000-0000-0000-000000000001', provider,
        origin: 'https://ci.example', credential_present: false,
        authority_principal_id: 'usr_00000000-0000-0000-0000-000000000001',
        state: 'active', created_at: '2026-09-01T00:00:00Z', targets: [],
      }] }));
    }
    if (path === `${base}/environments` || path === `${base}/keys`) {
      return Promise.resolve(Response.json({ items: [] }));
    }
    throw new Error(`unexpected ${path}`);
  }));
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/projects/app/adapters']}>
      <Routes><Route path="/orgs/:org/projects/:project/adapters" element={<Adapters />} /></Routes>
    </MemoryRouter>,
  );
  try {
    await settleTask();
    expect(container.querySelector('.adapters__adapter h2')?.textContent).toBe(label);
    expect(container.querySelector('[role="alert"]')).toBeNull();
  } finally {
    await unmount();
  }
});

it('TargetForm keeps the typed prefix, previews the stored form, and parses selected repository ids', async () => {
  const submitted: AdapterTargetInput[] = [];
  const onSubmit = (input: AdapterTargetInput) => {
    submitted.push(input);
    return Promise.resolve();
  };
  const { container, unmount } = await renderForm(
    <TargetForm
      title="Add target"
      environments={[{ id: 'env_1', name: 'prod' }]}
      keys={[]}
      busy={false}
      onCancel={() => undefined}
      onSubmit={onSubmit}
    />,
  );
  try {
    // Fields are a mix of the wrapping-label form and the ui/Input, ui/Select
    // atoms, which associate by `htmlFor`; resolve both.
    const select = (label: string) => {
      const found = [...container.querySelectorAll('label')].find((l) => l.textContent?.startsWith(label));
      if (found === undefined) return null;
      const inside = found.querySelector('select, input');
      if (inside !== null) return inside;
      const target = found.htmlFor === '' ? null : container.querySelector(`#${CSS.escape(found.htmlFor)}`);
      return target;
    };
    const kind = select('Destination kind');
    if (!(kind instanceof HTMLSelectElement)) throw new Error('kind select missing');
    expect([...kind.options].map((o) => o.textContent)).toContain('GitHub organization');
    await act(async () => selectValue(kind, 'organization'));
    const visibility = select('Visibility');
    if (!(visibility instanceof HTMLSelectElement)) throw new Error('visibility select missing');
    await act(async () => selectValue(visibility, 'selected'));
    const ids = select('Repository ids');
    if (!(ids instanceof HTMLInputElement)) throw new Error('repository ids input missing');
    await act(async () => typeInto(ids, '12, x'));
    const prefix = select('Name prefix');
    if (!(prefix instanceof HTMLInputElement)) throw new Error('prefix input missing');
    await act(async () => typeInto(prefix, 'prod_'));
    expect(prefix.value).toBe('prod_');
    expect(container.textContent).toContain('Will be stored as PROD_');

    const form = container.querySelector('form');
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted).toHaveLength(0);
    expect(container.textContent).toContain('Repository ids are whole numbers.');

    await act(async () => typeInto(ids, '12, 34'));
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[0]).toMatchObject({ visibility: 'selected', selected_repository_ids: [12, 34], name_prefix: 'PROD_' });
  } finally {
    await unmount();
  }
});

it('lists each failed name on its own line and every warning separately', async () => {
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    const base = '/api/v1/orgs/acme/projects/app';
    const target = {
      id: 'tgt_00000000-0000-0000-0000-000000000001', adapter_id: 'adp_00000000-0000-0000-0000-000000000001',
      environment_id: 'env_00000000-0000-0000-0000-000000000001', destination_kind: 'repository',
      destination_owner: 'acme', destination_name: 'app', destination_environment: '', visibility: '',
      destination_id: 1, repository_id: 0,
      selected_repository_ids: [], name_prefix: '', generation: 1, state: 'active', sync_status: 'degraded',
      converged_revision: null, last_attempted_revision: null, last_attempted_at: null,
      last_error_class: '', retry_at: null, paused_at: null, drift_attention: false,
      findings: [{ surface: 'secret', effective_name: 'TOKEN', finding: 'possible_capture' }, { surface: 'variable', effective_name: 'MODE', finding: 'owned_missing' }],
      failure_names: ['DB_URL', 'API_KEY'], warnings: ['rate limited', 'name truncated'],
      keys: [], conflicts: [],
    };
    if (path === `${base}/adapters`) {
      return Promise.resolve(Response.json({ items: [{
        id: 'adp_00000000-0000-0000-0000-000000000001', provider: 'forgejo',
        origin: 'https://ci.example', credential_present: true,
        authority_principal_id: 'usr_00000000-0000-0000-0000-000000000001',
        state: 'active', created_at: '2026-09-01T00:00:00Z', targets: [target],
      }] }));
    }
    if (path === `${base}/adapter-targets/${target.id}`) {
      return Promise.resolve(Response.json({ target, conflicts: [], mapping: [] }));
    }
    if (path === `${base}/environments` || path === `${base}/keys`) {
      return Promise.resolve(Response.json({ items: [] }));
    }
    throw new Error(`unexpected ${path}`);
  }));
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/projects/app/adapters?target=tgt_00000000-0000-0000-0000-000000000001']}>
      <Routes><Route path="/orgs/:org/projects/:project/adapters" element={<Adapters />} /></Routes>
    </MemoryRouter>,
  );
  try {
    // Two settles: the adapter list lands first, the target detail second.
    await settleTask();
    await settleTask();
    const failures = [...container.querySelectorAll('[aria-label="Failed names"] li')].map((li) => li.textContent?.trim());
    expect(failures).toEqual(['! DB_URL: failed', '! API_KEY: failed']);
    const warnings = [...container.querySelectorAll('.adapters__warnings li')].map((li) => li.textContent);
    expect(warnings).toEqual(['rate limited', 'name truncated']);
    expect(container.querySelector('[aria-label="Sync findings"]')?.textContent).toContain('possible_capture');
    expect(container.querySelector('[aria-label="Sync findings"]')?.textContent).toContain('owned_missing');
  } finally {
    await unmount();
  }
});

it('requires an explicit environment auto-create checkbox before sending consent', async () => {
  const submitted: AdapterTargetInput[] = [];
  const { container, unmount } = await renderForm(<TargetForm title="Add target" environments={[{ id: 'env_1', name: 'prod' }]} keys={[]} busy={false} onCancel={() => undefined} onSubmit={(input) => { submitted.push(input); return Promise.resolve(); }} />);
  try {
    const kind = [...container.querySelectorAll('label')].find((label) => label.textContent?.startsWith('Destination kind'))?.querySelector('select');
    if (!(kind instanceof HTMLSelectElement)) throw new Error('kind missing');
    await act(async () => selectValue(kind, 'environment'));
    expect(container.textContent).toContain('Administration:write');
    const form = container.querySelector('form');
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[0]?.allow_environment_create).toBe(false);
    const consent = container.querySelector('input[type="checkbox"]');
    if (!(consent instanceof HTMLInputElement)) throw new Error('consent missing');
    await act(async () => consent.click());
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[1]?.allow_environment_create).toBe(true);
  } finally { await unmount(); }
});

it('TargetForm offers only the AWS destination kinds and routes the secret name and KMS key', async () => {
  const submitted: AdapterTargetInput[] = [];
  const { container, unmount } = await renderForm(
    <TargetForm
      title="First target"
      provider="aws-secrets-manager"
      environments={[{ id: 'env_1', name: 'prod' }]}
      keys={[]}
      busy={false}
      onCancel={() => undefined}
      onSubmit={(input) => {
        submitted.push(input);
        return Promise.resolve();
      }}
    />,
  );
  try {
    const field = (label: string) =>
      [...container.querySelectorAll('label')].find((l) => l.textContent?.startsWith(label))?.querySelector('select, input') ?? null;
    const kind = field('Destination kind');
    if (!(kind instanceof HTMLSelectElement)) throw new Error('kind select missing');
    expect([...kind.options].map((o) => o.value)).toEqual(['json-object', 'per-key']);
    const account = field('AWS account id');
    const secret = field('Secret name');
    const kms = field('KMS key');
    if (!(account instanceof HTMLInputElement) || !(secret instanceof HTMLInputElement) || !(kms instanceof HTMLInputElement)) {
      throw new Error('AWS routing inputs missing');
    }
    await act(async () => typeInto(account, '123456789012'));
    await act(async () => typeInto(secret, 'prod/app'));
    await act(async () => typeInto(kms, 'alias/hikyo'));
    const form = container.querySelector('form');
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[0]).toMatchObject({
      destination_kind: 'json-object', destination_owner: '123456789012', destination_name: 'prod/app',
      destination_environment: 'alias/hikyo', visibility: '', selected_repository_ids: [],
    });
    await act(async () => selectValue(kind, 'per-key'));
    expect(field('Path prefix')).not.toBeNull();
  } finally { await unmount(); }
});

it('TargetForm addresses a vault-kv target by mount and path prefix on the repository destination', async () => {
  const submitted: AdapterTargetInput[] = [];
  const { container, unmount } = await renderForm(<TargetForm title="Add target" provider="vault-kv" environments={[{ id: 'env_1', name: 'prod' }]} keys={[]} busy={false} onCancel={() => undefined} onSubmit={(input) => { submitted.push(input); return Promise.resolve(); }} />);
  try {
    const labels = [...container.querySelectorAll('label')];
    expect(labels.some((label) => label.textContent?.startsWith('Destination kind'))).toBe(false);
    const field = (text: string) => {
      const input = labels.find((label) => label.textContent?.startsWith(text))?.querySelector('input');
      if (!(input instanceof HTMLInputElement)) throw new Error(`${text} missing`);
      return input;
    };
    await act(async () => typeInto(field('KV v2 mount'), 'kv/team-a'));
    await act(async () => typeInto(field('Path prefix'), 'apps/pay'));
    const form = container.querySelector('form');
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[0]).toMatchObject({
      destination_kind: 'repository',
      destination_owner: 'kv/team-a',
      destination_name: 'apps/pay',
      destination_environment: '',
      visibility: '',
      allow_environment_create: false,
    });
  } finally {
    await unmount();
  }
});

it('assembles the AWS access descriptor with only the fields the mode takes', () => {
  expect(awsAccessComplete(emptyAwsAccess)).toBe(false);
  const role = { ...emptyAwsAccess, roleArn: 'arn:aws:iam::123456789012:role/hikyo', externalId: 'tenant', sessionSeconds: '1800', accessKeyId: 'stale', secretAccessKey: 'stale' };
  expect(awsAccessComplete(role)).toBe(true);
  expect(awsAccessComplete({ ...role, sessionSeconds: '' })).toBe(true);
  expect(awsAccessComplete({ ...role, sessionSeconds: 'abc' })).toBe(false);
  expect(awsAccessComplete({ ...role, sessionSeconds: '1800.5' })).toBe(false);
  expect(awsAccessComplete({ ...role, sessionSeconds: '899' })).toBe(false);
  expect(awsAccessComplete({ ...role, sessionSeconds: '3601' })).toBe(false);
  expect(awsAccessComplete({ ...role, sessionSeconds: '3600' })).toBe(true);
  expect(JSON.parse(awsAccessDescriptor(role))).toEqual({ mode: 'assume-role', role_arn: 'arn:aws:iam::123456789012:role/hikyo', external_id: 'tenant', session_seconds: 1800 });
  expect(JSON.parse(awsAccessDescriptor({ ...role, mode: 'web-identity' }))).toEqual({ mode: 'web-identity', role_arn: 'arn:aws:iam::123456789012:role/hikyo', session_seconds: 1800 });
  expect(JSON.parse(awsAccessDescriptor({ ...emptyAwsAccess, mode: 'ambient', region: 'eu-west-1' }))).toEqual({ mode: 'ambient', region: 'eu-west-1' });
  const key = { ...emptyAwsAccess, mode: 'static' as const, accessKeyId: 'AKIAHIKYOTEST0000001', secretAccessKey: 'sealed-on-save' };
  expect(JSON.parse(awsAccessDescriptor(key))).toEqual({ mode: 'static', access_key_id: 'AKIAHIKYOTEST0000001', secret_access_key: 'sealed-on-save' });
});

it('offers Cloudflare destinations and sends one Pages environment', async () => {
  const submitted: AdapterTargetInput[] = [];
  const { container, unmount } = await renderForm(<TargetForm title="Add target" provider="cloudflare" environments={[{ id: 'env_1', name: 'prod' }]} keys={[]} busy={false} onCancel={() => undefined} onSubmit={(input) => { submitted.push(input); return Promise.resolve(); }} />);
  try {
    const field = (label: string) => [...container.querySelectorAll('label')].find((node) => node.textContent?.startsWith(label));
    const kind = field('Destination kind')?.querySelector('select');
    if (!(kind instanceof HTMLSelectElement)) throw new Error('kind missing');
    expect([...kind.options].map((option) => option.value)).toEqual(['workers-script', 'pages-project']);
    const form = container.querySelector('form');
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[0]?.destination_kind).toBe('workers-script');
    expect(submitted[0]?.destination_environment).toBe('');
    await act(async () => selectValue(kind, 'pages-project'));
    const environment = field('Pages environment')?.querySelector('select');
    if (!(environment instanceof HTMLSelectElement)) throw new Error('pages environment missing');
    await act(async () => selectValue(environment, 'preview'));
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[1]?.destination_kind).toBe('pages-project');
    expect(submitted[1]?.destination_environment).toBe('preview');
    expect(submitted[1]?.allow_environment_create).toBe(false);
    expect(field('Account id')).toBeDefined();
  } finally { await unmount(); }
});

it('TargetForm for GitLab sends a project with scope and variable flags and no GitHub routing', async () => {
  const submitted: AdapterTargetInput[] = [];
  const { container, unmount } = await renderForm(
    <TargetForm
      title="Add target"
      provider="gitlab"
      environments={[{ id: 'env_1', name: 'prod' }]}
      keys={[{ id: 'key_1', name: 'API_TOKEN' } as never]}
      busy={false}
      onCancel={() => undefined}
      onSubmit={(input) => {
        submitted.push(input);
        return Promise.resolve();
      }}
    />,
  );
  try {
    const field = (label: string) => {
      const found = [...container.querySelectorAll('label')].find((l) => l.textContent?.startsWith(label));
      if (found === undefined) return null;
      const inside = found.querySelector('select, input');
      if (inside !== null) return inside;
      return found.htmlFor === '' ? null : container.querySelector(`#${CSS.escape(found.htmlFor)}`);
    };
    const kind = field('Destination kind');
    if (!(kind instanceof HTMLSelectElement)) throw new Error('kind select missing');
    expect([...kind.options].map((o) => o.textContent)).toEqual(['GitLab project', 'GitLab group']);
    expect(container.textContent).not.toContain('GitHub environment');
    const namespace = field('Namespace');
    const project = field('Project');
    const scope = field('Environment scope');
    if (!(namespace instanceof HTMLInputElement) || !(project instanceof HTMLInputElement) || !(scope instanceof HTMLInputElement)) {
      throw new Error('GitLab fields missing');
    }
    expect(scope.value).toBe('*');
    await act(async () => typeInto(namespace, 'platform/backend'));
    await act(async () => typeInto(project, 'api'));
    await act(async () => typeInto(scope, 'production'));
    const protectedBox = [...container.querySelectorAll('input[type="checkbox"]')].find((input) =>
      (input.closest('label')?.textContent ?? input.parentElement?.textContent ?? '').includes('Protected'),
    );
    if (!(protectedBox instanceof HTMLInputElement)) throw new Error('protected checkbox missing');
    await act(async () => protectedBox.click());

    const form = container.querySelector('form');
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[0]).toMatchObject({
      destination_kind: 'repository', destination_owner: 'platform/backend', destination_name: 'api',
      destination_environment: '', visibility: '', selected_repository_ids: [],
      destination_scope: 'production', variable_protected: true, variable_hidden: false, variable_expand: false,
    });

    await act(async () => selectValue(kind, 'organization'));
    expect(field('Visibility')).toBeNull();
    expect(field('Group path')).not.toBeNull();
  } finally {
    await unmount();
  }
});

it('resuming an AWS origin move retains the AWS access descriptor form', async () => {
  const id = (prefix: string) => `${prefix}_00000000-0000-0000-0000-000000000001`;
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    const base = '/api/v1/orgs/acme/projects/app';
    if (path === `${base}/adapters`) return Promise.resolve(Response.json({ items: [{
      id: id('adp'), provider: 'aws-secrets-manager', origin: 'https://secretsmanager.eu-west-1.amazonaws.com',
      credential_present: true, authority_principal_id: id('usr'), state: 'moving',
      created_at: '2026-09-01T00:00:00Z', targets: [],
    }] }));
    if (path === `${base}/adapter-moves/${id('arm')}`) return Promise.resolve(Response.json({
      id: id('arm'), adapter_id: id('adp'), kind: 'origin', state: 'attention_required', keep_remote: false,
      pending_origin: 'https://secretsmanager.eu-west-2.amazonaws.com', created_at: '2026-09-01T00:00:00Z',
      targets: [{ target_id: id('adt'), environment_id: id('env'), destination_kind: 'json-object',
        destination_owner: '123456789012', destination_name: 'app', destination_environment: '',
        destination_id: 0, repository_id: 0, visibility: '', selected_repository_ids: [], name_prefix: '',
        orphaned_names: [], jobs: [] }],
    }));
    if (path === `${base}/environments` || path === `${base}/keys`) return Promise.resolve(Response.json({ items: [] }));
    throw new Error(`unexpected ${request.method} ${path}`);
  }));
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={[`/orgs/acme/projects/app/adapters?move=${id('arm')}`]}>
      <Routes><Route path="/orgs/:org/projects/:project/adapters" element={<Adapters />} /></Routes>
    </MemoryRouter>,
  );
  try {
    await settleTask();
    const resume = [...container.querySelectorAll('button')].find((button) => button.textContent === 'Resume with a new credential');
    if (resume === undefined) throw new Error('resume action missing');
    await act(async () => resume.click());
    const form = container.querySelector('form[aria-label="Resume move"]');
    expect(form?.textContent).toContain('AWS access');
    expect(form?.textContent).toContain('Role ARN');
    expect(form?.textContent).not.toContain('New credential');
  } finally { await unmount(); }
});
