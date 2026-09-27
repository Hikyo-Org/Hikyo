// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { useCertificates, useRenewCertificate } from './pki.ts';
afterEach(() => vi.unstubAllGlobals());
function RenewalProbe() {
  const project = { org: 'org_acme', project: 'prj_app' };
  useCertificates(project, [{ id: 'env_prod', name: 'prod' }, { id: 'env_dev', name: 'dev' }]);
  const renew = useRenewCertificate(project);
  return <button onClick={() => renew.mutate({ environment: 'env_prod', certificate: 'cert_00000000-0000-7000-8000-000000000001' })}>Renew</button>;
}
it('refreshes only the renewal input environment after a lost response', async () => {
  let prodReads = 0;
  let devReads = 0;
  let renewals = 0;
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    if (request.method === 'GET' && path.endsWith('/certificates')) {
      if (path.includes('/env_prod/')) prodReads++;
      if (path.includes('/env_dev/')) devReads++;
      return Promise.resolve(Response.json({ certificates: [] }));
    }
    if (request.method === 'POST' && path.endsWith('/renew')) {
      renewals++;
      return Promise.reject(new TypeError('response lost after server commit'));
    }
    throw new Error(`unexpected ${request.method} ${path}`);
  }));
  const { container, unmount } = await renderForm(<RenewalProbe />);
  try {
    await settleTask();
    expect([prodReads, devReads]).toEqual([1, 1]);
    await act(async () => container.querySelector('button')!.click());
    await settleTask();
    expect(renewals).toBe(1);
    expect([prodReads, devReads]).toEqual([2, 1]);
  } finally {
    await unmount();
  }
});
