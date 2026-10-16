// @vitest-environment happy-dom
import type { DeveloperCredential } from '@hikyo/client';
import { act, type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { z } from 'zod';

import { blockSessionEpoch, settleSessionEpoch } from '../api/sessionEpoch.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { DeveloperCredentialPolicy } from './DeveloperCredentialPolicy.tsx';
import { ProjectDeveloperCredentials } from './ProjectDeveloperCredentials.tsx';

const project = { org: 'org_019a0000-0000-7000-8000-000000000001', project: 'prj_019a0000-0000-7000-8000-000000000002' };
const listPath = `/api/v1/orgs/${project.org}/projects/${project.project}/developer-credentials`;
const policyPath = '/api/v1/instance/developer-credential-policy';
const credential: DeveloperCredential = {
  id: 'crd_019a0000-0000-7000-8000-000000000003',
  principal_id: 'prn_019a0000-0000-7000-8000-000000000004',
  authority_principal_id: 'prn_019a0000-0000-7000-8000-000000000005',
  org_id: project.org,
  project_id: project.project,
  environment_id: 'env_019a0000-0000-7000-8000-000000000006',
  created_at: '2026-10-16T08:00:00Z',
  expires_at: '2026-10-16T16:00:00Z',
  prefix_hint: 'hik_1_dev_hint',
};
const revokedAt = '2026-10-16T09:00:00Z';
const privateError = 'PRIVATE_SERVER_DETAIL_hik_1_dev_never_render';
const fetcher = vi.fn<(request: Request) => Promise<Response>>();
let view: Awaited<ReturnType<typeof renderForm>> | undefined;

function json(body: object, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function refusal(status: number): Response {
  return json({ error: { code: 'internal', message: privateError, detail: privateError } }, status);
}

function list(items: DeveloperCredential[] = [credential]): Response {
  return json({ items, count: items.length });
}

function button(root: ParentNode, label: string): HTMLButtonElement {
  const result = [...root.querySelectorAll('button')].find((candidate) => candidate.textContent?.trim() === label);
  if (!(result instanceof HTMLButtonElement)) throw new Error(`Missing button: ${label}`);
  return result;
}

function dialog(root: ParentNode): HTMLDialogElement {
  const result = root.querySelector('dialog');
  if (!(result instanceof HTMLDialogElement)) throw new Error('Missing confirmation dialog');
  return result;
}

function lifetimeInput(root: ParentNode): HTMLInputElement {
  const result = root.querySelector('input[name="max_lifetime_seconds"]');
  if (!(result instanceof HTMLInputElement)) throw new Error('Missing lifetime input');
  return result;
}

function requests(): { method: string; path: string }[] {
  return fetcher.mock.calls.map(([request]) => ({ method: request.method, path: new URL(request.url).pathname }));
}

async function mount(node: ReactNode) {
  view = await renderForm(node);
  await settleTask();
  return view;
}

async function click(root: ParentNode, label: string): Promise<void> {
  await act(async () => { button(root, label).click(); });
  await settleTask();
}

async function editPolicy(root: ParentNode, value: string): Promise<void> {
  await click(root, 'Edit developer credential policy');
  await act(async () => { typeInto(lifetimeInput(root), value); });
}

async function reviewPolicy(root: ParentNode): Promise<void> {
  const form = root.querySelector('form');
  if (!(form instanceof HTMLFormElement)) throw new Error('Missing policy form');
  // Exercise the application's validator even when native constraint validation
  // would prevent a click from submitting the same invalid value.
  await act(async () => { form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
  await settleTask();
}

function deferredResponse() {
  let release: ((response: Response) => void) | undefined;
  const promise = new Promise<Response>((resolve) => { release = resolve; });
  return {
    promise,
    resolve(response: Response) {
      if (release === undefined) throw new Error('Deferred response not initialized');
      release(response);
    },
  };
}

beforeEach(() => {
  settleSessionEpoch();
  fetcher.mockReset();
  vi.stubGlobal('fetch', fetcher);
});

afterEach(async () => {
  await view?.unmount();
  view?.client.clear();
  view = undefined;
  settleSessionEpoch();
  vi.unstubAllGlobals();
});

describe('project developer credentials', () => {
  it('lists active, expired and revoked metadata through the project-scoped GET', async () => {
    const expired = { ...credential, id: 'crd_019a0000-0000-7000-8000-000000000007', expires_at: '2026-10-16T08:30:00Z' };
    const revoked = { ...credential, id: 'crd_019a0000-0000-7000-8000-000000000008', revoked_at: revokedAt };
    fetcher.mockResolvedValueOnce(list([credential, expired, revoked]));
    const { container } = await mount(<ProjectDeveloperCredentials {...project} />);

    expect(requests()).toEqual([{ method: 'GET', path: listPath }]);
    expect(container.querySelector('caption')?.textContent).toContain('including expired and revoked credentials');
    expect(container.querySelectorAll('tbody tr')).toHaveLength(3);
    for (const value of [credential.id, expired.id, revoked.id, credential.principal_id, credential.authority_principal_id, credential.environment_id]) {
      expect(container.querySelector('tbody')?.textContent).toContain(value);
    }
    expect(container.querySelector(`time[datetime="${credential.created_at}"]`)).not.toBeNull();
    expect(container.querySelector(`time[datetime="${credential.expires_at}"]`)).not.toBeNull();
    expect(container.querySelector(`time[datetime="${revokedAt}"]`)).not.toBeNull();
    expect([...container.querySelectorAll('button')].some((candidate) => candidate.textContent === 'Revoke crd_019a0000-0000-7000-8000-000000000008')).toBe(false);
    expect(container.textContent).toContain('hikyo dev session');
    expect(container.textContent).toContain('Bearers cannot be retrieved here.');
    expect(container.querySelector('input')).toBeNull();
  });

  it('requires confirmation before one exact scoped DELETE and refreshes persisted revocation', async () => {
    fetcher.mockResolvedValueOnce(list())
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(list([{ ...credential, revoked_at: revokedAt }]));
    const { container } = await mount(<ProjectDeveloperCredentials {...project} />);
    await click(container, 'Revoke crd_019a0000-0000-7000-8000-000000000003');

    const confirmation = dialog(container);
    expect(confirmation.textContent).toContain('Revoke developer credential?');
    for (const value of [credential.id, credential.authority_principal_id, credential.environment_id]) {
      expect(confirmation.textContent).toContain(value);
    }
    expect(confirmation.textContent).toContain('This is permanent.');
    expect(confirmation.textContent).toContain('values already delivered to running processes cannot be erased');
    expect(requests()).toEqual([{ method: 'GET', path: listPath }]);

    await click(confirmation, 'Revoke credential');
    expect(requests()).toEqual([
      { method: 'GET', path: listPath },
      { method: 'DELETE', path: `${listPath}/crd_019a0000-0000-7000-8000-000000000003` },
      { method: 'GET', path: listPath },
    ]);
    expect(await fetcher.mock.calls[1]?.[0].text()).toBe('');
    expect(container.querySelector('dialog')).toBeNull();
    expect(container.querySelector('tbody')?.textContent).toContain(`Revoked ${revokedAt}`);
    expect(container.textContent).toContain('Developer credential revoked. Its next fetch will be refused.');
  });

  it('cancels revocation without writing', async () => {
    fetcher.mockResolvedValueOnce(list());
    const { container } = await mount(<ProjectDeveloperCredentials {...project} />);
    await click(container, 'Revoke crd_019a0000-0000-7000-8000-000000000003');
    await click(dialog(container), 'Cancel');
    expect(container.querySelector('dialog')).toBeNull();
    expect(button(container, 'Revoke crd_019a0000-0000-7000-8000-000000000003').disabled).toBe(false);
    expect(requests()).toEqual([{ method: 'GET', path: listPath }]);
    expect(container.textContent).not.toContain('Developer credential revoked.');
  });

  it.each([403, 404, 409, 429, 500])('redacts a %i revoke failure, rechecks permission refusals and permits retry', async (status) => {
    fetcher.mockResolvedValueOnce(list()).mockResolvedValueOnce(refusal(status));
    const rechecksPermission = status === 403 || status === 404;
    if (rechecksPermission) fetcher.mockResolvedValueOnce(list());
    fetcher.mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(list([{ ...credential, revoked_at: revokedAt }]));
    const { container } = await mount(<ProjectDeveloperCredentials {...project} />);
    await click(container, 'Revoke crd_019a0000-0000-7000-8000-000000000003');
    await click(dialog(container), 'Revoke credential');
    expect(dialog(container).querySelector('[role="alert"]')).not.toBeNull();
    expect(container.textContent).not.toContain(privateError);
    expect(container.textContent).not.toContain('Developer credential revoked.');
    expect(container.querySelector(`time[datetime="${revokedAt}"]`)).toBeNull();
    const expected = [
      { method: 'GET', path: listPath },
      { method: 'DELETE', path: `${listPath}/crd_019a0000-0000-7000-8000-000000000003` },
    ];
    if (rechecksPermission) expected.push({ method: 'GET', path: listPath });
    expect(requests()).toEqual(expected);
    expect(button(dialog(container), 'Revoke credential').disabled).toBe(false);

    await click(dialog(container), 'Revoke credential');
    expect(requests()).toEqual([
      ...expected,
      { method: 'DELETE', path: `${listPath}/crd_019a0000-0000-7000-8000-000000000003` },
      { method: 'GET', path: listPath },
    ]);
    expect(container.querySelector('dialog')).toBeNull();
    expect(container.querySelector('tbody')?.textContent).toContain(`Revoked ${revokedAt}`);
    expect(container.textContent).toContain('Developer credential revoked. Its next fetch will be refused.');
    expect(container.querySelector('[role="alert"]')).toBeNull();
  });

  it.each([403, 404])('hides cached project metadata after a %i mutation refusal followed by a 404 refresh', async (status) => {
    fetcher.mockResolvedValueOnce(list()).mockResolvedValueOnce(refusal(status)).mockResolvedValueOnce(refusal(404));
    const { container } = await mount(<ProjectDeveloperCredentials {...project} />);
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1);
    await click(container, `Revoke ${credential.id}`);
    await click(dialog(container), 'Revoke credential');

    expect(requests()).toEqual([
      { method: 'GET', path: listPath },
      { method: 'DELETE', path: `${listPath}/${credential.id}` },
      { method: 'GET', path: listPath },
    ]);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('not disclosed');
    expect(container.querySelector('table')).toBeNull();
    expect(container.querySelector('dialog')).toBeNull();
    for (const value of [credential.id, credential.principal_id, credential.authority_principal_id, credential.environment_id, credential.created_at, credential.expires_at]) {
      expect(container.textContent).not.toContain(value);
    }
    expect(container.textContent).not.toContain(privateError);
    expect(container.textContent).not.toContain('Developer credential revoked.');
    expect([...container.querySelectorAll('button')].some((candidate) => candidate.textContent?.startsWith('Revoke'))).toBe(false);
  });

  it('hides cached rows and an open revoke confirmation after permission narrows on refresh', async () => {
    fetcher.mockResolvedValueOnce(list()).mockResolvedValueOnce(refusal(404));
    const { container, client } = await mount(<ProjectDeveloperCredentials {...project} />);
    await click(container, 'Revoke crd_019a0000-0000-7000-8000-000000000003');
    // Permission changes arrive independently of the open modal. A background
    // refresh must stop disclosing its previously cached incident metadata.
    await act(async () => { await client.refetchQueries(); });
    await settleTask();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('not disclosed');
    expect(container.querySelector('tbody')).toBeNull();
    expect(container.querySelector('dialog')).toBeNull();
    expect(container.textContent).not.toContain(credential.id);
    expect(container.textContent).not.toContain(credential.authority_principal_id);
    expect(container.textContent).not.toContain(privateError);
    expect(requests()).toEqual([{ method: 'GET', path: listPath }, { method: 'GET', path: listPath }]);
  });

  it('distinguishes an empty list from a redacted initial load failure', async () => {
    fetcher.mockResolvedValueOnce(list([])).mockResolvedValueOnce(refusal(500));
    const { container } = await mount(<ProjectDeveloperCredentials {...project} />);
    expect(container.textContent).toContain('No developer credentials in this project.');
    await click(container, 'Refresh developer credentials');
    expect(container.querySelector('[role="alert"]')).not.toBeNull();
    expect(container.textContent).not.toContain('No developer credentials in this project.');
    expect(container.textContent).not.toContain(privateError);
  });

  it('retires a late list response after the session epoch changes', async () => {
    const pending = deferredResponse();
    fetcher.mockReturnValueOnce(pending.promise);
    const { container } = await mount(<ProjectDeveloperCredentials {...project} />);
    expect(fetcher).toHaveBeenCalledOnce();
    await act(async () => {
      blockSessionEpoch();
      settleSessionEpoch();
      pending.resolve(list());
    });
    await settleTask();
    expect(container.querySelector('tbody')).toBeNull();
    expect(container.textContent).not.toContain(credential.id);
    expect(container.textContent).not.toContain('Developer credential revoked.');
    expect(fetcher.mock.calls[0]?.[0].signal.aborted).toBe(true);
  });

  it('retires a late revoke response without success feedback or refresh under a new session epoch', async () => {
    const pending = deferredResponse();
    fetcher.mockResolvedValueOnce(list()).mockReturnValueOnce(pending.promise);
    const { container } = await mount(<ProjectDeveloperCredentials {...project} />);
    await click(container, `Revoke ${credential.id}`);
    await click(dialog(container), 'Revoke credential');
    expect(button(dialog(container), 'Revoke credential').disabled).toBe(true);
    await act(async () => {
      blockSessionEpoch();
      settleSessionEpoch();
      pending.resolve(new Response(null, { status: 204 }));
    });
    await settleTask();
    expect(container.textContent).not.toContain('Developer credential revoked.');
    expect(container.querySelector(`time[datetime="${revokedAt}"]`)).toBeNull();
    expect(dialog(container).querySelector('[role="alert"] .alert__text')?.textContent)
      .toBe('The browser session changed. Refresh after sign-in completes before retrying.');
    expect(requests()).toEqual([
      { method: 'GET', path: listPath },
      { method: 'DELETE', path: `${listPath}/${credential.id}` },
    ]);
    expect(fetcher.mock.calls[1]?.[0].signal.aborted).toBe(true);
  });
});

describe('developer credential policy', () => {
  it('loads the fetched ceiling into the row and edit field instead of the eight-hour default', async () => {
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 }));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    expect(container.textContent).toContain('(3600 seconds)');
    await click(container, 'Edit developer credential policy');
    const input = lifetimeInput(container);
    expect(input.value).toBe('3600');
    expect(input.min).toBe('1');
    expect(input.max).toBe('28800');
    expect(input.step).toBe('1');
    expect(input.labels?.[0]?.textContent).toBe('Maximum developer credential lifetime (seconds)');
    expect(requests()).toEqual([{ method: 'GET', path: policyPath }]);
  });

  it.each(['', '0', '-1', '28801', '1.5'])('rejects invalid lifetime %j before confirmation or PUT', async (value) => {
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 }));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    await editPolicy(container, value);
    await reviewPolicy(container);
    expect(container.querySelector('[role="alert"]')?.textContent).toBe('Enter a whole number from 1 to 28,800 seconds.');
    expect(lifetimeInput(container).getAttribute('aria-invalid')).toBe('true');
    expect(container.querySelector('dialog')).toBeNull();
    expect(requests()).toEqual([{ method: 'GET', path: policyPath }]);
  });

  it.each([1, 28800])('confirms and persists inclusive boundary %i with an exact PUT and permanent-expiry warning', async (seconds) => {
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 }))
      .mockResolvedValueOnce(json({ max_lifetime_seconds: seconds }))
      .mockResolvedValueOnce(json({ max_lifetime_seconds: seconds }));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    await editPolicy(container, String(seconds));
    expect(container.textContent).toContain('Lowering the ceiling permanently clamps existing expiry');
    expect(container.textContent).toContain('Raising it never extends or revives a credential.');
    await reviewPolicy(container);
    const confirmation = dialog(container);
    expect(confirmation.textContent).toContain(`Set the instance ceiling to ${String(seconds)} seconds`);
    expect(confirmation.textContent).toContain('last loaded ceiling is 3600 seconds');
    expect(confirmation.textContent).toContain('may expire immediately');
    expect(confirmation.textContent).toContain('Raising the ceiling later will not restore their old expiry.');
    expect(requests()).toEqual([{ method: 'GET', path: policyPath }]);

    await click(confirmation, 'Apply developer credential policy');
    expect(requests()).toEqual([
      { method: 'GET', path: policyPath },
      { method: 'PUT', path: policyPath },
      { method: 'GET', path: policyPath },
    ]);
    const request = fetcher.mock.calls[1]?.[0];
    if (request === undefined) throw new Error('Missing policy PUT');
    const body = z.object({ max_lifetime_seconds: z.number().int().min(1).max(28800) }).strict().parse(JSON.parse(await request.text()));
    expect(body).toEqual({ max_lifetime_seconds: seconds });
    expect(request.headers.get('Content-Type')).toContain('application/json');
    expect(container.querySelector('dialog')).toBeNull();
    expect(container.querySelector('form')).toBeNull();
    expect(container.textContent).toContain(`(${String(seconds)} seconds)`);
    expect(container.textContent).toContain(`Developer credential ceiling saved: ${String(seconds)} seconds. Existing expiry reductions are permanent.`);
  });

  it('cancels both confirmation and editing without a policy write', async () => {
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 }));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    await editPolicy(container, '1800');
    await reviewPolicy(container);
    await click(dialog(container), 'Cancel');
    expect(container.querySelector('dialog')).toBeNull();
    expect(lifetimeInput(container).value).toBe('1800');
    await click(container, 'Cancel');
    expect(container.querySelector('form')).toBeNull();
    expect(container.textContent).toContain('(3600 seconds)');
    expect(container.textContent).not.toContain('Developer credential ceiling saved:');
    expect(requests()).toEqual([{ method: 'GET', path: policyPath }]);
  });

  it.each([400, 403, 404, 409, 429, 500])('redacts a %i policy failure, rechecks permission refusals and permits retry', async (status) => {
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 })).mockResolvedValueOnce(refusal(status));
    const rechecksPermission = status === 403 || status === 404;
    if (rechecksPermission) fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 }));
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 1800 }))
      .mockResolvedValueOnce(json({ max_lifetime_seconds: 1800 }));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    await editPolicy(container, '1800');
    await reviewPolicy(container);
    await click(dialog(container), 'Apply developer credential policy');
    expect(dialog(container).querySelector('[role="alert"]')).not.toBeNull();
    expect(lifetimeInput(container).value).toBe('1800');
    expect(container.textContent).toContain('(3600 seconds)');
    expect(container.textContent).not.toContain(privateError);
    expect(container.textContent).not.toContain('Developer credential ceiling saved:');
    const expected = [{ method: 'GET', path: policyPath }, { method: 'PUT', path: policyPath }];
    if (rechecksPermission) expected.push({ method: 'GET', path: policyPath });
    expect(requests()).toEqual(expected);
    expect(button(dialog(container), 'Apply developer credential policy').disabled).toBe(false);

    await click(dialog(container), 'Apply developer credential policy');
    expect(requests()).toEqual([
      ...expected,
      { method: 'PUT', path: policyPath },
      { method: 'GET', path: policyPath },
    ]);
    expect(container.querySelector('dialog')).toBeNull();
    expect(container.querySelector('form')).toBeNull();
    expect(container.textContent).toContain('(1800 seconds)');
    expect(container.textContent).toContain('Developer credential ceiling saved: 1800 seconds. Existing expiry reductions are permanent.');
    expect(container.querySelector('[role="alert"]')).toBeNull();
  });

  it.each([
    {
      name: 'malformed 200',
      respond: () => Promise.resolve(json({ max_lifetime_seconds: privateError, token: privateError })),
      message: 'The server answered something this client cannot understand. This is likely a bug worth reporting. Refresh the current metadata before retrying a change; its outcome may be unconfirmed.',
    },
    {
      name: 'unexpected 204',
      respond: () => Promise.resolve(new Response(null, { status: 204, headers: { 'X-Diagnostic': privateError } })),
      message: 'The server answered something this client cannot understand. This is likely a bug worth reporting. Refresh the current metadata before retrying a change; its outcome may be unconfirmed.',
    },
    {
      name: 'fetch rejection',
      respond: () => Promise.reject(new TypeError(privateError)),
      message: 'The server could not be reached. Try again shortly. Refresh the current metadata before retrying a change; its outcome may be unconfirmed.',
    },
    {
      name: 'server 500',
      respond: () => Promise.resolve(refusal(500)),
      message: 'The request could not be completed (server error 500). Try again shortly. Refresh the current metadata before retrying a change; its outcome may be unconfirmed.',
    },
  ])('differentiates $name policy PUT failure safely and retains the loaded ceiling until explicit refresh', async ({ respond, message }) => {
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 }))
      .mockImplementationOnce(respond)
      .mockResolvedValueOnce(json({ max_lifetime_seconds: 1800 }));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    await editPolicy(container, '1800');
    await reviewPolicy(container);
    await click(dialog(container), 'Apply developer credential policy');

    expect(dialog(container).querySelector('[role="alert"] .alert__text')?.textContent).toBe(message);
    expect(container.querySelector('.settings-row__detail')?.textContent).toContain('(3600 seconds)');
    expect(lifetimeInput(container).value).toBe('1800');
    expect(container.textContent).not.toContain('Developer credential ceiling saved:');
    expect(container.textContent).not.toContain('Nothing was sent.');
    expect(container.innerHTML).not.toContain(privateError);
    expect(requests()).toEqual([{ method: 'GET', path: policyPath }, { method: 'PUT', path: policyPath }]);

    await click(dialog(container), 'Cancel');
    await click(container, 'Cancel');
    expect(container.querySelector('.settings-row__detail')?.textContent).toContain('(3600 seconds)');
    // A failed response cannot prove whether the server applied the PUT.
    // Only this subsequent GET establishes the current ceiling.
    await click(container, 'Refresh developer credential policy');
    expect(requests()).toEqual([
      { method: 'GET', path: policyPath },
      { method: 'PUT', path: policyPath },
      { method: 'GET', path: policyPath },
    ]);
    expect(container.querySelector('.settings-row__detail')?.textContent).toContain('(1800 seconds)');
    expect(container.textContent).not.toContain('Developer credential ceiling saved:');
    expect(container.innerHTML).not.toContain(privateError);
    await click(container, 'Edit developer credential policy');
    expect(lifetimeInput(container).value).toBe('1800');
  });

  it('reports a malformed policy GET as a safe contract failure without inventing a policy', async () => {
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: privateError, token: privateError }));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    expect(container.querySelector('[role="alert"] .alert__text')?.textContent)
      .toBe('The server answered something this client cannot understand. This is likely a bug worth reporting. Refresh the current metadata before retrying a change; its outcome may be unconfirmed.');
    expect(container.querySelector('.settings-row')).toBeNull();
    expect(container.querySelector('input')).toBeNull();
    expect(container.textContent).not.toContain('Edit developer credential policy');
    expect(container.textContent).not.toContain('Developer credential ceiling saved:');
    expect(container.innerHTML).not.toContain(privateError);
    expect(requests()).toEqual([{ method: 'GET', path: policyPath }]);
  });

  it.each([403, 404])('hides cached policy and editing after a %i mutation refusal followed by a 404 refresh', async (status) => {
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 }))
      .mockResolvedValueOnce(refusal(status))
      .mockResolvedValueOnce(refusal(404));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    expect(container.textContent).toContain('(3600 seconds)');
    await editPolicy(container, '1800');
    await reviewPolicy(container);
    await click(dialog(container), 'Apply developer credential policy');

    expect(requests()).toEqual([
      { method: 'GET', path: policyPath },
      { method: 'PUT', path: policyPath },
      { method: 'GET', path: policyPath },
    ]);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('not disclosed');
    expect(container.textContent).not.toContain('(3600 seconds)');
    expect(container.textContent).not.toContain('1800 seconds');
    expect(container.textContent).not.toContain('Edit developer credential policy');
    expect(container.querySelector('dialog')).toBeNull();
    expect(container.querySelector('form')).toBeNull();
    expect(container.querySelector('input')).toBeNull();
    expect(container.textContent).not.toContain(privateError);
    expect(container.textContent).not.toContain('Developer credential ceiling saved:');
  });

  it('does not invent a default policy or offer editing when policy loading fails', async () => {
    fetcher.mockResolvedValueOnce(refusal(404));
    const { container } = await mount(<DeveloperCredentialPolicy />);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('not disclosed');
    expect(container.textContent).not.toContain(privateError);
    expect(container.textContent).not.toContain('Edit developer credential policy');
    expect(container.querySelector('form')).toBeNull();
    expect(container.textContent).not.toContain('Developer credential ceiling saved:');
  });

  it('retires a late policy mutation response without success feedback or refresh under a new session epoch', async () => {
    const pending = deferredResponse();
    fetcher.mockResolvedValueOnce(json({ max_lifetime_seconds: 3600 })).mockReturnValueOnce(pending.promise);
    const { container } = await mount(<DeveloperCredentialPolicy />);
    await editPolicy(container, '1800');
    await reviewPolicy(container);
    await click(dialog(container), 'Apply developer credential policy');
    expect(button(dialog(container), 'Apply developer credential policy').disabled).toBe(true);
    await act(async () => {
      blockSessionEpoch();
      settleSessionEpoch();
      pending.resolve(json({ max_lifetime_seconds: 1800 }));
    });
    await settleTask();
    expect(dialog(container).querySelector('[role="alert"] .alert__text')?.textContent)
      .toBe('The browser session changed. Refresh after sign-in completes before retrying.');
    expect(container.textContent).not.toContain('Developer credential ceiling saved:');
    expect(container.textContent).toContain('(3600 seconds)');
    expect(requests()).toEqual([{ method: 'GET', path: policyPath }, { method: 'PUT', path: policyPath }]);
    expect(fetcher.mock.calls[1]?.[0].signal.aborted).toBe(true);
  });
});
