// @vitest-environment happy-dom
import { createClient, createConfig } from '@hikyo/runtime-core';
import { act, createElement, Fragment, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { ApiError } from './client.ts';
import {
  runTransit, transitRefusalText, useChangeTransitKeyState, useConfigureTransitKey,
  useCreateTransitKey, useRotateTransitKey, useTransitKey, useTransitKeys, useTrimTransitKey,
  type TransitInput,
} from './transit.ts';
import { WorkspaceContextProvider } from './transport.tsx';

const ref = {
  org: 'org_00000000-0000-0000-0000-000000000001',
  project: 'prj_00000000-0000-0000-0000-000000000001',
  environment: 'env_00000000-0000-0000-0000-000000000001',
};
const key = {
  id: 'trk_00000000-0000-0000-0000-000000000001', environment_id: ref.environment,
  name: 'application', algorithm: 'xchacha20-poly1305', custody: 'software',
  allowed_operations: ['encrypt', 'decrypt'], exportable: false, state: 'active',
  latest_version: 1, min_encrypt_version: 1, min_decrypt_version: 1,
  compromised_through_version: 0, rotation_period_seconds: 0, rotation_due: false,
  created_by: 'owner', created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
  versions: [{ version: 1, material: 'sealed', created_at: '2026-10-01T00:00:00Z' }], callers: [],
};
const listKey = ['transit-keys', ref.org, ref.project, ref.environment, 'https://remote.test'];
const detailKey = ['transit-key', ref.org, ref.project, ref.environment, 'https://remote.test', key.name];

afterEach(() => vi.unstubAllGlobals());

function remote(node: ReactNode, fetch: typeof globalThis.fetch, origin = 'https://remote.test') {
  const props = {
    value: { origin, remote: 'remote', client: createClient(createConfig({ baseUrl: origin, fetch })) },
    children: node,
  };
  return createElement(WorkspaceContextProvider, props);
}

describe('Transit data-plane ownership', () => {
  const cases: { input: TransitInput; suffix: string; output: {
    key_version: number; ciphertext?: string; plaintext?: string; signature?: string; valid?: boolean; mac?: string;
  } }[] = [
    { input: { operation: 'encrypt', body: { plaintext: 'c2VjcmV0', context: 'Y29udGV4dA==' } }, suffix: 'encrypt', output: { ciphertext: 'hikyo:v1:wrapped', key_version: 1 } },
    { input: { operation: 'decrypt', body: { ciphertext: 'hikyo:v1:wrapped' } }, suffix: 'decrypt', output: { plaintext: 'c2VjcmV0', key_version: 1 } },
    { input: { operation: 'rewrap', body: { ciphertext: 'hikyo:v1:wrapped' } }, suffix: 'rewrap', output: { ciphertext: 'hikyo:v2:wrapped', key_version: 2 } },
    { input: { operation: 'datakey', body: { bits: 256, plaintext: true } }, suffix: 'datakey', output: { plaintext: 'c2VjcmV0', ciphertext: 'hikyo:v1:wrapped', key_version: 1 } },
    { input: { operation: 'sign', body: { message: 'bWVzc2FnZQ==' } }, suffix: 'sign', output: { signature: 'hikyo:v1:signature', key_version: 1 } },
    { input: { operation: 'verify', body: { message: 'bWVzc2FnZQ==', signature: 'hikyo:v1:signature' } }, suffix: 'verify', output: { valid: false, key_version: 1 } },
    { input: { operation: 'hmac', body: { message: 'bWVzc2FnZQ==' } }, suffix: 'hmac', output: { mac: 'hikyo:v1:mac', key_version: 1 } },
    { input: { operation: 'hmac-verify', body: { message: 'bWVzc2FnZQ==', mac: 'hikyo:v1:mac' } }, suffix: 'hmac-verify', output: { valid: true, key_version: 1 } },
  ];

  it.each(cases)('dispatches $suffix through the selected transport without a cache', async ({ input, suffix, output }) => {
    let request: Request | undefined;
    const fetch = vi.fn<typeof globalThis.fetch>(async (value) => {
      if (!(value instanceof Request)) throw new Error('Expected generated request');
      request = value;
      return Response.json({ ...output, unrelated: 'discard me' });
    });
    const client = createClient(createConfig({ baseUrl: 'https://remote.test', fetch }));
    const signal = new AbortController().signal;
    const result = await runTransit(ref, key.name, input, { client }, signal);
    expect(request?.url).toBe(`https://remote.test/api/v1/orgs/${ref.org}/projects/${ref.project}/environments/${ref.environment}/transit-keys/${key.name}/${suffix}`);
    expect(request?.method).toBe('POST');
    expect(await request?.text()).toBe(JSON.stringify(input.body));
    expect(result.operation).toBe(input.operation);
    expect(result.output).toMatchObject({ ...output, key_version: BigInt(output.key_version) });
    expect(result.output).not.toHaveProperty('unrelated');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('refuses malformed success bodies and missing requested plaintext', async () => {
    let body: object = { plaintext: 'c2VjcmV0', key_version: 'invalid' };
    const fetch = vi.fn<typeof globalThis.fetch>(async () => Response.json(body));
    const client = createClient(createConfig({ baseUrl: 'https://remote.test', fetch }));
    await expect(runTransit(ref, key.name, { operation: 'decrypt', body: { ciphertext: 'wrapped' } }, { client })).rejects.toThrow();
    body = { ciphertext: 'wrapped', key_version: 1 };
    await expect(runTransit(ref, key.name, { operation: 'datakey', body: { plaintext: true } }, { client })).rejects.toThrow('no plaintext');
  });

  it('discards plaintext that was not requested from data-key responses', async () => {
    const fetch = vi.fn<typeof globalThis.fetch>(async () => Response.json({ plaintext: 'SECRET', ciphertext: 'wrapped', key_version: 1 }));
    const client = createClient(createConfig({ baseUrl: 'https://remote.test', fetch }));
    const result = await runTransit(ref, key.name, { operation: 'datakey', body: {} }, { client });
    expect(result.output).toEqual({ ciphertext: 'wrapped', key_version: 1n });
  });

  it('passes cancellation through the generated transport', async () => {
    let started: (() => void) | undefined;
    const ready = new Promise<void>((resolve) => { started = resolve; });
    const fetch = vi.fn<typeof globalThis.fetch>((value) => {
      if (!(value instanceof Request)) throw new Error('Expected generated request');
      started?.();
      return new Promise<Response>((_resolve, reject) => value.signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true }));
    });
    const client = createClient(createConfig({ baseUrl: 'https://remote.test', fetch }));
    const abort = new AbortController();
    const pending = runTransit(ref, key.name, { operation: 'decrypt', body: { ciphertext: 'wrapped' } }, { client }, abort.signal);
    await ready;
    abort.abort();
    await expect(pending).rejects.toThrow();
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});

function MetadataProbe() {
  useTransitKeys(ref);
  useTransitKey(ref, key.name);
  return null;
}

it('does not read metadata until an environment and key are selected', async () => {
  function Unselected() {
    useTransitKeys(null);
    useTransitKey(null, null);
    useTransitKey(ref, null);
    return null;
  }
  const fetch = vi.fn<typeof globalThis.fetch>();
  const view = await renderForm(remote(createElement(Unselected), fetch));
  try {
    await settleTask();
    expect(fetch).not.toHaveBeenCalled();
  } finally { await view.unmount(); }
});

it('keeps identical environment and key identifiers isolated by remote origin', async () => {
  const fetch = vi.fn<typeof globalThis.fetch>(async (value) => {
    if (!(value instanceof Request)) throw new Error('Expected generated request');
    const originKey = { ...key, created_by: new URL(value.url).origin };
    return Response.json(value.url.endsWith('/transit-keys') ? { items: [originKey] } : originKey);
  });
  const view = await renderForm(createElement(Fragment, null,
    remote(createElement(MetadataProbe), fetch),
    remote(createElement(MetadataProbe), fetch, 'https://second.test')));
  try {
    await vi.waitFor(() => {
      expect(view.client.getQueryData(detailKey)).toMatchObject({ created_by: 'https://remote.test' });
      expect(view.client.getQueryData(['transit-key', ref.org, ref.project, ref.environment, 'https://second.test', key.name])).toMatchObject({ created_by: 'https://second.test' });
    });
    expect(fetch).toHaveBeenCalledTimes(4);
  } finally { await view.unmount(); }
});

it.each([401, 403, 404])('retires Transit metadata after %i, preserves null through network failure, and recovers', async (status) => {
  let phase: 'loaded' | 'denied' | 'offline' | 'recovered' = 'loaded';
  const fetch = vi.fn<typeof globalThis.fetch>(async (value) => {
    if (!(value instanceof Request)) throw new Error('Expected generated request');
    if (phase === 'denied') return Response.json({}, { status });
    if (phase === 'offline') throw new TypeError('Network unavailable');
    return Response.json(value.url.endsWith('/transit-keys') ? { items: [key] } : key);
  });
  const view = await renderForm(remote(createElement(MetadataProbe), fetch));
  try {
    await vi.waitFor(() => expect(view.client.getQueryData(detailKey)).toMatchObject({ name: key.name, latest_version: 1n }));
    expect(view.client.getQueryData(listKey)).toMatchObject({ items: [{ name: key.name }] });
    phase = 'denied';
    await act(async () => { await view.client.invalidateQueries(); });
    expect(view.client.getQueryData(listKey)).toBeNull();
    expect(view.client.getQueryData(detailKey)).toBeNull();
    phase = 'offline';
    await act(async () => { await view.client.invalidateQueries(); });
    expect(view.client.getQueryData(listKey)).toBeNull();
    expect(view.client.getQueryData(detailKey)).toBeNull();
    phase = 'recovered';
    await act(async () => { await view.client.invalidateQueries(); });
    expect(view.client.getQueryData(detailKey)).toMatchObject({ name: key.name });
  } finally { await view.unmount(); }
});

type ManagementActions = {
  create: ReturnType<typeof useCreateTransitKey>;
  configure: ReturnType<typeof useConfigureTransitKey>;
  rotate: ReturnType<typeof useRotateTransitKey>;
  state: ReturnType<typeof useChangeTransitKeyState>;
  trim: ReturnType<typeof useTrimTransitKey>;
};
function MutationProbe({ ready }: { ready: (actions: ManagementActions) => void }) {
  ready({ create: useCreateTransitKey(ref), configure: useConfigureTransitKey(ref), rotate: useRotateTransitKey(ref), state: useChangeTransitKeyState(ref), trim: useTrimTransitKey(ref) });
  return null;
}

it('routes all management operations remotely and refreshes metadata after success and lost responses', async () => {
  const requests: Request[] = [];
  let fail = false;
  const fetch = vi.fn<typeof globalThis.fetch>(async (value) => {
    if (!(value instanceof Request)) throw new Error('Expected generated request');
    requests.push(value);
    if (fail) throw new TypeError('Response lost');
    return Response.json(value.url.endsWith('/trim') ? { key, versions_deleted: 0 } : key, { status: value.url.endsWith('/transit-keys') ? 201 : 200 });
  });
  let actions: ManagementActions | undefined;
  const view = await renderForm(remote(createElement(MutationProbe, { ready: (value) => { actions = value; } }), fetch));
  try {
    const execute = actions;
    if (execute === undefined) throw new Error('Mutation hooks were not mounted');
    const runs = [
      () => execute.create.mutateAsync({ name: key.name, algorithm: 'xchacha20-poly1305' }),
      () => execute.configure.mutateAsync({ key: key.name, body: { min_decrypt_version: 1 } }),
      () => execute.rotate.mutateAsync(key.name),
      () => execute.state.mutateAsync({ key: key.name, body: { action: 'disable' } }),
      () => execute.trim.mutateAsync(key.name),
    ];
    for (const request of runs) {
      view.client.setQueryData(listKey, { items: [key] });
      view.client.setQueryData(detailKey, key);
      await act(async () => { await request(); });
      expect(view.client.getQueryState(listKey)?.isInvalidated).toBe(true);
      expect(view.client.getQueryState(detailKey)?.isInvalidated).toBe(true);
    }
    expect(requests.map((request) => new URL(request.url).pathname.split('/').at(-1))).toEqual(['transit-keys', key.name, 'rotate', 'lifecycle', 'trim']);
    expect(requests.map((request) => request.method)).toEqual(['POST', 'PATCH', 'POST', 'POST', 'POST']);
    expect(requests.every((request) => request.url.startsWith('https://remote.test/'))).toBe(true);
    fail = true;
    view.client.setQueryData(listKey, { items: [key] });
    view.client.setQueryData(detailKey, key);
    await act(async () => { await expect(execute.rotate.mutateAsync(key.name)).rejects.toThrow(); });
    await settleTask();
    expect(view.client.getQueryState(listKey)?.isInvalidated).toBe(true);
    expect(view.client.getQueryState(detailKey)?.isInvalidated).toBe(true);
  } finally { await view.unmount(); }
});

it('keeps server details and sensitive validation errors out of refusal text', () => {
  for (const status of [400, 401, 403, 404, 409, 429, 500]) {
    expect(transitRefusalText(new ApiError(status, 'SECRET', 'SECRET'))).not.toContain('SECRET');
  }
  expect(transitRefusalText(new Error('SECRET'))).not.toContain('SECRET');
});
