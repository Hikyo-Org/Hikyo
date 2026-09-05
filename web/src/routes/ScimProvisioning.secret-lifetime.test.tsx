// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { ScimProvisioning } from './ScimProvisioning.tsx';

const mocks = vi.hoisted(() => ({ mint: vi.fn() }));
vi.mock('../api/client.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/client.ts')>()), parsedPick: mocks.mint,
}));
vi.mock('../api/scim.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/scim.ts')>()),
  useScimBindings: () => ({ data: { items: [{ id: 'binding', provider_kind: 'oidc', provider_slug: 'fixture', provider_issuer: 'https://fixture.example', subject_source: 'externalId', created_at: '2026-09-05T00:00:00Z', attention: [] }] }, isSuccess: true }),
  useScimCredentials: () => ({ data: { items: [] }, isSuccess: true }),
  useScimMappings: () => ({ data: { items: [] }, isSuccess: true }),
  useScimDirectoryGroups: () => ({ data: { items: [] }, isSuccess: true }),
  useScimDirectoryUsers: () => ({ data: { items: [] }, isSuccess: true }),
}));
vi.mock('../api/settings.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/settings.ts')>()),
  useOrg: () => ({ data: { id: 'org', name: 'fixture' }, isSuccess: true }),
  useOrgTopology: () => ({ projects: [], isSuccess: true }),
}));
afterEach(() => vi.clearAllMocks());

it('clears refused SCIM proof and retries once with freshly entered input', async () => {
  mocks.mint.mockRejectedValue(new ApiError(400, 'bad_request'));
  const view = await renderForm(<MemoryRouter initialEntries={['/?binding=binding']}><ScimProvisioning /></MemoryRouter>);
  try {
    const proof = view.container.querySelector<HTMLInputElement>('#scim-proof');
    if (proof === null) throw new Error('SCIM proof input missing');
    const form = proof.closest('form');
    if (form === null) throw new Error('SCIM mint form missing');
    await act(async () => typeInto(proof, 'SENTINEL-refused-scim-proof'));
    await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(mocks.mint).toHaveBeenCalledOnce();
    expect(proof.value).toBe('');
    expect(view.container.textContent).toContain('No credential was issued.');
    expect(view.client.getMutationCache().getAll()).toEqual([]);
    await act(async () => typeInto(proof, 'SENTINEL-fresh-scim-proof'));
    await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(mocks.mint).toHaveBeenCalledTimes(2);
    expect(mocks.mint.mock.calls[1]?.[1]).toMatchObject({ body: { proof: 'SENTINEL-fresh-scim-proof' } });
    expect(proof.value).toBe('');
  } finally { await view.unmount(); }
});
