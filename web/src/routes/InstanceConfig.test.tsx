// @vitest-environment happy-dom
import { act, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { z } from 'zod';
import { createClient, createConfig } from '@hikyo/runtime-core';
import { WorkspaceContextProvider } from '../api/transport.tsx';
import { AuthProvider } from '../app/AuthProvider.tsx';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { InstanceConfig } from './InstanceConfig.tsx';

const active = { owner_instance_id: 'instance_local', managed: true, binding: { org_id: 'org_system', project_id: 'prj_system', environment_id: 'env_system', schema_version: 1 }, generation: 7, desired_revision: 2, latest_revision: 3, state: 'active', nodes: [{ node_id: 'node-a', active_generation: 7, active_revision: 2, state: 'active', updated_at: '2026-09-06T12:00:00Z' }], job: null };
const completedJob = { id: 'job-complete', state: 'completed', revision: 2, generation: 7, created_at: '2026-09-06T11:00:00Z', completed_at: '2026-09-06T11:01:00Z' };
const preparedJob = { ...completedJob, id: 'job-prepared', state: 'preparing', revision: 3, generation: 8, prepared: true };
const prepared = { ...active, state: 'pending', job: preparedJob };
const json = (value: object, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } });
const bodySchema = z.object({ revision: z.number(), expected_generation: z.number(), schema_version: z.number(), idempotency_key: z.string(), confirm_restored_credentials: z.boolean(), prepare_only: z.boolean().optional(), restore_deployment: z.boolean().optional(), plan_digest: z.string().optional() });
afterEach(() => vi.unstubAllGlobals());
function button(container: ParentNode, label: string) {
 const found = [...container.querySelectorAll('button')].find((entry) => entry.textContent === label);
 if (!(found instanceof HTMLButtonElement)) throw new Error(`Missing ${label}`);
 return found;
}
async function mount(handler: (request: Request) => Response | Promise<Response | null> | null, extra?: ReactNode) {
 vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
  const request = input instanceof Request ? input : new Request(input, init);
  const response = await handler(request);
  if (response !== null) return response;
  const path = new URL(request.url).pathname;
  if (path === '/api/v1/auth/whoami') return json(authenticatedIdentity);
  if (path === '/api/v1/instance/config') return json(active);
  if (path === '/api/v1/remotes') return json({ items: [] });
  return new Response(null, { status: 404 });
 }));
 return renderForm(<AuthProvider><MemoryRouter><InstanceConfig />{extra}</MemoryRouter></AuthProvider>);
}

function RefreshConfiguration() {
 const client = useQueryClient();
 return <button type="button" onClick={() => void client.invalidateQueries({ queryKey: ['self-config'] })}>Refresh test configuration</button>;
}

describe('managed instance configuration', () => {
 it('links the bound project and keeps publish distinct from apply', async () => {
  const writes: string[] = [];
  const view = await mount((request) => { if (request.method !== 'GET') writes.push(request.url); return null; });
  try {
   await settleTask();
   expect(view.container.textContent).toContain('Generation 7');
   expect(view.container.textContent).toContain('Latest published r3');
   expect(view.container.querySelector('a[href="/orgs/org_system/projects/prj_system/matrix"]')).not.toBeNull();
   expect(view.container.textContent).toContain('Saving drafts and publishing keep the running settings unchanged');
   expect(writes).toEqual([]);
  } finally { await view.unmount(); }
 });
 it.each([false, true])('reauthenticates exact revision and restore confirmation %s', async (recovering) => {
  const seen: string[] = [];
  let applied: z.infer<typeof bodySchema> | undefined;
  let preparation: z.infer<typeof bodySchema> | undefined;
  const view = await mount(asyncHandler);
  async function asyncHandler(request: Request) {
   const path = new URL(request.url).pathname;
   if (path === '/api/v1/instance/config' && recovering) return json({ ...active, state: 'recovery_required' });
   if (request.method !== 'POST') return null;
   seen.push(path);
   if (path === '/api/v1/auth/reauth/totp') {
    const proof = z.object({ purpose: z.literal('self-config'), self_config: z.object({ action: z.literal('apply'), revision: z.literal(3), expected_generation: z.literal(7), owner_instance_id: z.literal('instance_local'), confirm_restored_credentials: z.boolean() }), code: z.string() }).parse(await request.json());
    expect(proof.code).toBe('123456');
    expect(proof.self_config.confirm_restored_credentials).toBe(recovering);
    return json({ session_id: authenticatedIdentity.session.id, environment_id: 'instance:instance_local', single_decision: true, window_expires: '2026-09-06T12:05:00Z' });
   }
   if (path === '/api/v1/instance/config/apply') { const body = bodySchema.parse(await request.json()); if (body.prepare_only) { preparation = body; return json({ ...prepared, state: recovering ? 'recovery_required' : 'pending' }, 202); } applied = body; return json({ ...active, state: 'pending', desired_revision: 3, generation: 8 }, 202); }
   return null;
  }
  try {
   await settleTask();
   if (recovering) {
    expect(button(view.container, 'Apply selected revision').disabled).toBe(true);
    const confirmation = view.container.querySelector('input[type=checkbox]');
    if (!(confirmation instanceof HTMLInputElement)) throw new Error('Missing restore confirmation');
    await act(async () => confirmation.click());
   }
   await act(async () => button(view.container, 'Apply selected revision').click());
   await settleTask();
   expect(seen).toEqual(['/api/v1/instance/config/apply']);
   expect(view.container.textContent).toContain('Reload live');
   const dialog = view.container.querySelector('dialog');
   if (!(dialog instanceof HTMLDialogElement)) throw new Error('Missing decision dialog');
   const input = dialog.querySelector('input');
   if (!(input instanceof HTMLInputElement)) throw new Error('Missing code input');
   await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
    setter?.call(input, '123456'); input.dispatchEvent(new Event('input', { bubbles: true }));
   });
   await act(async () => button(dialog, 'Authorize with code').click());
   await settleTask();
   expect(dialog.querySelector('[role="alert"]')?.textContent).toBeUndefined();
   expect(seen).toEqual(['/api/v1/instance/config/apply', '/api/v1/auth/reauth/totp', '/api/v1/instance/config/apply']);
   expect(applied?.idempotency_key).toBe(preparation?.idempotency_key);
   expect(applied?.prepare_only).toBeUndefined();
   expect(applied).toMatchObject({ revision: 3, expected_generation: 7, schema_version: 1, confirm_restored_credentials: recovering });
   expect(applied?.idempotency_key).toMatch(/^[a-f0-9-]{36}$/);
  } finally { await view.unmount(); }
 });
 it.each(z.enum(['failed', 'preparing']).options)('does not report success when final Apply returns %s', async (state) => {
  const view = await mount(async (request) => {
   const path = new URL(request.url).pathname;
   if (path === '/api/v1/instance/config/apply') {
    const body = bodySchema.parse(await request.json());
    return json(body.prepare_only ? prepared : { ...prepared, job: { ...preparedJob, state } }, 202);
   }
   if (path === '/api/v1/auth/reauth/totp') return json({ session_id: authenticatedIdentity.session.id, environment_id: 'instance:instance_local', single_decision: true, window_expires: '2026-09-06T12:05:00Z' });
   return null;
  });
  try {
   await settleTask(); await act(async () => button(view.container, 'Apply selected revision').click()); await settleTask();
   const dialog = view.container.querySelector('dialog');
   if (dialog === null) throw new Error('Missing decision');
   const code = dialog.querySelector('input'); if (!(code instanceof HTMLInputElement)) throw new Error('Missing code');
   await act(async () => typeInto(code, '123456'));
   await act(async () => button(dialog, 'Authorize with code').click()); await settleTask();
   expect(view.container.querySelector('dialog')).toBeNull();
   expect(view.container.textContent).not.toContain('Apply committed');
   expect(view.container.textContent).not.toContain('Revision r3 is active');
   expect(view.container.textContent).toContain(state === 'failed' ? 'Preparation expired or failed' : 'Preparation is still pending');
   if (state === 'preparing') expect(button(view.container, 'Check preparation')).toBeDefined();
  } finally { await view.unmount(); }
 });
 it('binds controlled rollout review, MFA and final apply to the same prepared plan and key', async () => {
  const digest = 'a'.repeat(64);
  const requests: z.infer<typeof bodySchema>[] = [];
  let authorized = false;
  const view = await mount(async (request) => {
   const path = new URL(request.url).pathname;
   if (path === '/api/v1/instance/config/apply') {
    const body = bodySchema.parse(await request.json()); requests.push(body);
    if (body.prepare_only) return json({ ...prepared, job: { ...preparedJob, plan_digest: digest } }, 202);
    expect(authorized).toBe(true);
    return json({ ...active, generation: 8, desired_revision: 3, job: { ...completedJob, revision: 3, generation: 8 } }, 202);
   }
   if (path === '/api/v1/auth/reauth/totp') {
    const proof = z.object({ self_config: z.object({ plan_digest: z.string(), revision: z.number(), expected_generation: z.number() }) }).parse(await request.json());
    expect(proof.self_config).toEqual({ plan_digest: digest, revision: 3, expected_generation: 7 }); authorized = true;
    return json({ session_id: authenticatedIdentity.session.id, environment_id: 'instance:instance_local', single_decision: true, window_expires: '2026-09-06T12:05:00Z' });
   }
   return null;
  });
  try {
   await settleTask(); await act(async () => button(view.container, 'Apply selected revision').click()); await settleTask();
   const dialog = view.container.querySelector('dialog');
   if (dialog === null) throw new Error('Missing prepared decision');
   expect(dialog.textContent).toContain('Controlled rollout'); expect(dialog.textContent).toContain(digest);
   expect(authorized).toBe(false);
   const code = dialog.querySelector('input'); if (!(code instanceof HTMLInputElement)) throw new Error('Missing code');
   await act(async () => typeInto(code, '123456'));
   await act(async () => button(dialog, 'Authorize with code').click()); await settleTask();
   expect(requests).toHaveLength(2);
   expect(requests[0]?.prepare_only).toBe(true); expect(requests[0]?.plan_digest).toBeUndefined();
   expect(requests[1]?.plan_digest).toBe(digest); expect(requests[1]?.idempotency_key).toBe(requests[0]?.idempotency_key);
   expect(view.container.textContent).toContain('Revision r3 is active.');
  } finally { await view.unmount(); }
 });
 it.each([
  { label: 'incomplete', response: { ...prepared, job: { ...preparedJob, prepared: false } }, waiting: true },
  { label: 'expired', response: { ...prepared, job: { ...preparedJob, state: 'failed' } }, waiting: false },
  { label: 'wrong revision', response: { ...prepared, job: { ...preparedJob, revision: 2 } }, waiting: false },
  { label: 'wrong generation', response: { ...prepared, generation: 8 }, waiting: false },
  { label: 'wrong owner', response: { ...prepared, owner_instance_id: 'instance_other' }, waiting: false },
 ])('never requests MFA for $label preparation', async ({ response, waiting }) => {
  const writes: string[] = [];
  const view = await mount((request) => { if (request.method === 'POST') { writes.push(new URL(request.url).pathname); return json(response, 202); } return null; });
  try {
   await settleTask(); await act(async () => button(view.container, 'Apply selected revision').click()); await settleTask();
   expect(view.container.querySelector('dialog')).toBeNull();
   expect(writes).toEqual(['/api/v1/instance/config/apply']);
   if (waiting) expect(view.container.textContent).toContain('Preparing alone does not authorize Apply');
   else expect(view.container.querySelector('[role=alert]')).not.toBeNull();
  } finally { await view.unmount(); }
 });
 it('keeps the exact idempotency key while waiting and refuses a replacement job', async () => {
  const keys: string[] = [];
  const view = await mount(async (request) => {
   if (new URL(request.url).pathname !== '/api/v1/instance/config/apply') return null;
   const body = bodySchema.parse(await request.json()); keys.push(body.idempotency_key);
   return json({ ...prepared, job: { ...preparedJob, id: keys.length === 1 ? 'job-original' : 'job-replaced', prepared: keys.length !== 1 } }, 202);
  });
  try {
   await settleTask(); await act(async () => button(view.container, 'Apply selected revision').click()); await settleTask();
   expect(view.container.querySelector('dialog')).toBeNull();
   await act(async () => button(view.container, 'Check preparation').click()); await settleTask();
   expect(keys).toHaveLength(2); expect(keys[0]).toBe(keys[1]);
   expect(view.container.querySelector('dialog')).toBeNull();
   expect(view.container.textContent).toContain('changed during preparation');
  } finally { await view.unmount(); }
 });
 it('resumes a cancelled review with the same preparation key', async () => {
  const keys: string[] = [];
  const view = await mount(async (request) => {
   if (new URL(request.url).pathname !== '/api/v1/instance/config/apply') return null;
   const body = bodySchema.parse(await request.json()); keys.push(body.idempotency_key); return json(prepared, 202);
  });
  try {
   await settleTask(); await act(async () => button(view.container, 'Apply selected revision').click()); await settleTask();
   const dialog = view.container.querySelector('dialog'); if (dialog === null) throw new Error('Missing review');
   await act(async () => button(dialog, 'Cancel').click());
   await act(async () => button(view.container, 'Check preparation').click()); await settleTask();
   expect(keys).toHaveLength(2); expect(keys[0]).toBe(keys[1]); expect(view.container.querySelector('dialog')).not.toBeNull();
  } finally { await view.unmount(); }
 });
 it('retains a preparation key after an ambiguous network failure', async () => {
  const keys: string[] = [];
  const view = await mount(async (request) => {
   if (new URL(request.url).pathname !== '/api/v1/instance/config/apply') return null;
   const body = bodySchema.parse(await request.json()); keys.push(body.idempotency_key);
   if (keys.length === 1) throw new TypeError('Connection lost');
   return json(prepared, 202);
  });
  try {
   await settleTask(); await act(async () => button(view.container, 'Apply selected revision').click()); await settleTask();
   expect(view.container.querySelector('dialog')).toBeNull();
   await act(async () => button(view.container, 'Check preparation').click()); await settleTask();
   expect(keys).toHaveLength(2); expect(keys[0]).toBe(keys[1]); expect(view.container.querySelector('dialog')).not.toBeNull();
  } finally { await view.unmount(); }
 });
 it('requires a distinct exact MFA decision before restoring a partial deployment', async () => {
  const digest = 'c'.repeat(64);
  const partial = { ...active, state: 'partial', job: { ...completedJob, state: 'partial', plan_digest: digest } };
  const writes: string[] = [];
  const view = await mount(async (request) => {
   const path = new URL(request.url).pathname;
   if (path === '/api/v1/instance/config') return json(partial);
   if (request.method !== 'POST') return null;
   writes.push(path);
   if (path === '/api/v1/auth/reauth/totp') {
    const proof = z.object({ self_config: z.object({ action: z.literal('rollout-restore'), revision: z.literal(2), expected_generation: z.literal(7), plan_digest: z.literal(digest), confirm_restored_credentials: z.literal(false) }) }).parse(await request.json());
    expect(proof.self_config.action).toBe('rollout-restore');
    return json({ session_id: authenticatedIdentity.session.id, environment_id: 'instance:instance_local', single_decision: true, window_expires: '2026-09-06T12:05:00Z' });
   }
   if (path === '/api/v1/instance/config/apply') {
    const body = bodySchema.parse(await request.json());
    expect(body).toMatchObject({ revision: 2, expected_generation: 7, plan_digest: digest, restore_deployment: true, confirm_restored_credentials: false });
    expect(body.prepare_only).toBeUndefined();
    return json({ ...partial, job: { ...partial.job, deployment_restore_pending: true } }, 202);
   }
   return null;
  });
  try {
   await settleTask(); expect(button(view.container, 'Apply selected revision').disabled).toBe(true);
   await act(async () => button(view.container, 'Restore deployment').click()); expect(writes).toEqual([]);
   let dialog = view.container.querySelector('dialog'); if (dialog === null) throw new Error('Missing restore review');
   expect(dialog.textContent).toContain('desired revision stays unchanged and fenced');
   await act(async () => { if (dialog !== null) button(dialog, 'Cancel').click(); }); expect(writes).toEqual([]);
   await act(async () => button(view.container, 'Restore deployment').click());
   dialog = view.container.querySelector('dialog'); if (dialog === null) throw new Error('Missing restore review');
   const code = dialog.querySelector('input'); if (!(code instanceof HTMLInputElement)) throw new Error('Missing code');
   await act(async () => typeInto(code, '123456'));
   await act(async () => { if (dialog !== null) button(dialog, 'Authorize with code').click(); }); await settleTask();
   expect(writes).toEqual(['/api/v1/auth/reauth/totp', '/api/v1/instance/config/apply']);
   expect(view.container.textContent).toContain('Deployment restoration requested');
   expect(view.container.textContent).not.toContain('Revision r2 is active.');
  } finally { await view.unmount(); }
 });
 it.each([false, true])('allows a new repair only after deployment restored=%s', async (restored) => {
  const view = await mount((request) => new URL(request.url).pathname === '/api/v1/instance/config' ? json({ ...active, state: 'partial', job: { ...completedJob, state: 'partial', plan_digest: 'd'.repeat(64), deployment_restore_pending: !restored, deployment_restored: restored } }) : null);
  try {
   await settleTask(); expect(button(view.container, 'Apply selected revision').disabled).toBe(!restored);
   expect([...view.container.querySelectorAll('button')].some((entry) => entry.textContent === 'Restore deployment')).toBe(false);
   expect(view.container.textContent).toContain(restored ? 'Deployment resources are restored' : 'restoration is pending controller confirmation');
  } finally { await view.unmount(); }
 });
 it('allows a separately authenticated repair after partial activation', async () => {
  const view = await mount((request) => new URL(request.url).pathname === '/api/v1/instance/config' ? json({ ...active, state: 'partial', job: { ...completedJob, state: 'partial' } }) : new URL(request.url).pathname === '/api/v1/instance/config/apply' ? json(prepared, 202) : null);
  try { await settleTask(); expect(view.container.textContent).toContain('publish a repair'); expect(button(view.container, 'Apply selected revision').disabled).toBe(false); await act(async () => button(view.container, 'Apply selected revision').click()); await settleTask(); expect(view.container.querySelector('dialog')).not.toBeNull(); } finally { await view.unmount(); }
 });
 it('allows a new membership decision when the prior job completed and a retired node is unknown', async () => {
  const view = await mount((request) => new URL(request.url).pathname === '/api/v1/instance/config' ? json({ ...active, state: 'pending', job: completedJob, nodes: [{ ...active.nodes[0], state: 'unknown' }] }) : null);
  try { await settleTask(); expect(view.container.textContent).toContain('unknown'); expect(button(view.container, 'Apply selected revision').disabled).toBe(false); } finally { await view.unmount(); }
 });
 it('follows a freshly published default until the administrator chooses an exact revision', async () => {
  let latest = 3;
  const view = await mount((request) => new URL(request.url).pathname === '/api/v1/instance/config' ? json({ ...active, latest_revision: latest }) : null, <RefreshConfiguration />);
  try {
   await settleTask();
   const revision = view.container.querySelector('input[inputmode=numeric]');
   if (!(revision instanceof HTMLInputElement)) throw new Error('Missing revision input');
   expect(revision.value).toBe('3');
   latest = 4;
   await act(async () => button(view.container, 'Refresh test configuration').click());
   await settleTask();
   expect(revision.value).toBe('4');
   await act(async () => typeInto(revision, '2'));
   latest = 5;
   await act(async () => button(view.container, 'Refresh test configuration').click());
   await settleTask();
   expect(revision.value).toBe('2');
  } finally { await view.unmount(); }
 });
 it('renders access refusal without guessing an empty project', async () => {
  const view = await mount((request) => new URL(request.url).pathname === '/api/v1/instance/config' ? new Response(null, { status: 404 }) : null);
  try { await settleTask(); expect(view.container.textContent).toContain('not disclosed'); expect(view.container.textContent).not.toContain('Preview adoption'); } finally { await view.unmount(); }
 });
 it('reads remote status directly from its owner and retains the workspace on project links', async () => {
  const owners: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
   const request = input instanceof Request ? input : new Request(input, init);
   const url = new URL(request.url);
   if (url.pathname === '/api/v1/auth/whoami') return json(authenticatedIdentity);
   if (url.pathname === '/api/v1/instance/config') { owners.push(url.origin); return json({ ...active, owner_instance_id: 'instance_remote' }); }
   return new Response(null, { status: 404 });
  }));
  const workspace = { origin: 'https://owner.example', remote: 'independent', client: createClient(createConfig({ baseUrl: 'https://owner.example', credentials: 'omit' })) };
  const view = await renderForm(<AuthProvider><MemoryRouter><WorkspaceContextProvider value={workspace}><InstanceConfig /></WorkspaceContextProvider></MemoryRouter></AuthProvider>);
  try {
   await settleTask();
   expect(owners.length).toBeGreaterThan(0);
   expect(new Set(owners)).toEqual(new Set(['https://owner.example']));
   expect(view.container.textContent).toContain('instance_remote');
   expect(view.container.textContent).not.toContain('Independent instances');
   expect(view.container.querySelector('a[href="/orgs/org_system/projects/prj_system/matrix?remote=independent"]')).not.toBeNull();
  } finally { await view.unmount(); }
 });
});
