// @vitest-environment happy-dom
import { act } from 'react';
import { beforeEach, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { GrantModal } from './Members.tsx';

const mocks = vi.hoisted(() => ({ create: vi.fn(), passkey: vi.fn() }));
vi.mock('../api/access.ts', async (original) => ({
  ...(await original<typeof import('../api/access.ts')>()),
  useCreateGrants: () => ({ isPending: false, mutateAsync: mocks.create }),
  useApplyTemplate: () => ({ isPending: false }),
}));
vi.mock('../api/values.ts', async (original) => ({
  ...(await original<typeof import('../api/values.ts')>()), runPasskeyCeremony: mocks.passkey,
}));

const scope = { kind: 'environment' as const, org: 'org_a', project: 'prj_a', environment: 'env_a' };
const option = { value: 'environment', label: 'production', scope, level: 'environment' as const, group: 'project', isProtected: true };
const draft = { principal: 'mch_a', capabilities: ['read'], template: '', mode: 'capabilities' as const, scope: option.value };

beforeEach(() => {
  mocks.create.mockReset();
  mocks.passkey.mockReset();
  mocks.create.mockRejectedValueOnce(new ApiError(409, 'reauth_required',
    'reauthenticate over the environments this operation makes reachable, then retry (env_a)',
  ));
});

it('keeps Cancel and draft controls disabled throughout reauthentication and never retries after unmount', async () => {
  let finish: () => void = () => {};
  mocks.passkey.mockImplementation(() => new Promise<void>((resolve) => { finish = resolve; }));
  const done = vi.fn();
  const { container, unmount } = await renderForm(<GrantModal orgName="Acme" options={[option]} draft={draft}
    effectiveScope={option.value} stage="grant" projects={[]} topologyReady topologyPending={false} topologyError={false}
    known={['mch_a']} principalName={(id) => id} onDraft={vi.fn()} onStage={vi.fn()} onDone={done} projectContext={false} />);
  const confirm = [...container.querySelectorAll('button')].find((button) => button.textContent?.startsWith('Grant'));
  if (confirm === undefined) throw new Error('No grant confirmation.');
  await act(async () => confirm.click());
  await settleTask();
  expect(mocks.passkey).toHaveBeenCalledOnce();
  const cancel = [...container.querySelectorAll('button')].find((button) => button.textContent === 'Cancel');
  expect(cancel?.disabled).toBe(true);
  expect([...container.querySelectorAll('select')].every((select) => select.disabled)).toBe(true);
  await unmount();
  await act(async () => finish());
  await settleTask();
  expect(mocks.create).toHaveBeenCalledOnce();
  expect(done).not.toHaveBeenCalled();
});
