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
const json = (value: object, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } });
const bodySchema = z.object({ revision: z.number(), expected_generation: z.number(), schema_version: z.number(), idempotency_key: z.string(), confirm_restored_credentials: z.boolean() });
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
   if (path === '/api/v1/instance/config/apply') { applied = bodySchema.parse(await request.json()); return json({ ...active, state: 'pending', desired_revision: 3, generation: 8 }, 202); }
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
   expect(seen).toEqual([]);
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
   expect(seen).toEqual(['/api/v1/auth/reauth/totp', '/api/v1/instance/config/apply']);
   expect(applied).toMatchObject({ revision: 3, expected_generation: 7, schema_version: 1, confirm_restored_credentials: recovering });
   expect(applied?.idempotency_key).toMatch(/^[a-f0-9-]{36}$/);
  } finally { await view.unmount(); }
 });
 it('shows partial convergence and prevents another apply', async () => {
  const view = await mount((request) => new URL(request.url).pathname === '/api/v1/instance/config' ? json({ ...active, state: 'partial', job: { ...completedJob, state: 'partial' } }) : null);
  try { await settleTask(); expect(view.container.textContent).toContain('not a completed apply'); expect(button(view.container, 'Apply selected revision').disabled).toBe(true); } finally { await view.unmount(); }
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
