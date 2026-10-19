// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { zSetProjectCapacityRequest } from '@hikyo/zod';

import { projectCapacityKey } from '../api/projectCapacity.ts';
import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { ProjectCapacityPolicy } from './ProjectCapacityPolicy.tsx';

const url = '/api/v1/orgs/org_a/projects/prj_a/capacity';
const initial = {
  key_limit_override: null, instance_default: 1000, instance_maximum: 10000,
  effective_limit: 1000, key_count: 1200, environment_count: 4, revision: 1,
  inherited: true, capped: false,
};
afterEach(() => vi.unstubAllGlobals());

type FixtureCapacity = Omit<typeof initial, 'key_limit_override'> & { key_limit_override: number | null };
function fixture(overrides: Partial<FixtureCapacity> = {}) {
  let current: FixtureCapacity = { ...initial, ...overrides };
  const proposals: ReturnType<typeof zSetProjectCapacityRequest.parse>[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const request = input instanceof Request ? input : new Request(input);
    if (new URL(request.url).pathname !== url) return Response.json({}, { status: 404 });
    if (request.method === 'PUT') {
      const proposal = zSetProjectCapacityRequest.parse(await request.json());
      proposals.push(proposal);
      if (proposal.expected_revision !== BigInt(current.revision) ||
        proposal.expected_instance_default !== current.instance_default ||
        proposal.expected_instance_maximum !== current.instance_maximum) {
        return Response.json({ error: { code: 'conflict', message: 'Policy changed' } }, { status: 409 });
      }
      const override = proposal.key_limit_override;
      current = { ...current, key_limit_override: override,
        effective_limit: Math.min(override ?? current.instance_default, current.instance_maximum),
        inherited: override === null, capped: override !== null && override > current.instance_maximum,
        revision: current.revision + 1 };
      return Response.json(current);
    }
    return Response.json(current);
  });
  vi.stubGlobal('fetch', fetchMock);
  return { proposals, fetchMock, change: (next: Partial<FixtureCapacity>) => { current = { ...current, ...next }; } };
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = [...container.querySelectorAll('button')].find((node) => node.textContent === text);
  if (found === undefined) throw new Error(`Missing button: ${text}`);
  return found;
}
function radio(container: HTMLElement, text: string): HTMLInputElement {
  const label = [...container.querySelectorAll('label')].find((node) => node.textContent === text);
  const input = label === undefined ? null : container.querySelector<HTMLInputElement>(`input[id="${label.htmlFor}"]`);
  if (input === null) throw new Error(`Missing radio: ${text}`);
  return input;
}
function limitInput(container: HTMLElement) {
  const input = container.querySelector<HTMLInputElement>('input[type="number"]');
  if (input === null) throw new Error('Missing key limit input');
  return input;
}
async function click(container: HTMLElement, text: string) {
  await act(async () => button(container, text).click());
  await settleTask();
}
async function submit(container: HTMLElement) {
  const form = container.querySelector('form');
  if (form === null) throw new Error('Missing capacity form');
  await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
  await settleTask();
}

it('shows inherited usage and safely lowers below existing usage with a revision-bound proposal', async () => {
  const api = fixture();
  const view = await renderForm(<ProjectCapacityPolicy org="org_a" project="prj_a" />);
  try {
    await settleTask();
    expect(view.container.textContent).toContain('1,200 keys used / 1,000 key limit');
    expect(view.container.textContent).toContain('Inherited from the instance default');
    expect(view.container.textContent).toContain('Existing keys are preserved');
    await click(view.container, 'Edit key capacity');
    await act(async () => radio(view.container, 'Custom project limit').click());
    await act(async () => typeInto(limitInput(view.container), '800'));
    await submit(view.container);
    expect(api.proposals).toEqual([{ key_limit_override: 800, expected_revision: 1n,
      expected_instance_default: 1000, expected_instance_maximum: 10000 }]);
    expect(view.container.textContent).toContain('Key capacity saved. Existing keys are preserved.');
    expect(view.container.querySelector('form')).toBeNull();
  } finally { await view.unmount(); }
});

it('resets a custom override by sending explicit null', async () => {
  const api = fixture();
  const view = await renderForm(<ProjectCapacityPolicy org="org_a" project="prj_a" />);
  try {
    await settleTask();
    await click(view.container, 'Edit key capacity');
    await act(async () => radio(view.container, 'Custom project limit').click());
    await act(async () => typeInto(limitInput(view.container), '5000'));
    await submit(view.container);
    await click(view.container, 'Edit key capacity');
    await act(async () => radio(view.container, 'Inherit instance default').click());
    await submit(view.container);
    expect(api.proposals.map((proposal) => proposal.key_limit_override)).toEqual([5000, null]);
  } finally { await view.unmount(); }
});

it('keeps a dirty draft across refetch and blocks stale proposals until explicitly reconciled', async () => {
  const api = fixture();
  const view = await renderForm(<ProjectCapacityPolicy org="org_a" project="prj_a" />);
  try {
    await settleTask();
    await click(view.container, 'Edit key capacity');
    await act(async () => radio(view.container, 'Custom project limit').click());
    await act(async () => typeInto(limitInput(view.container), '5000'));
    api.change({ instance_maximum: 4000, revision: 2 });
    await act(async () => view.client.invalidateQueries({ queryKey: projectCapacityKey('org_a', 'prj_a') }));
    await settleTask();
    expect(limitInput(view.container).value).toBe('5000');
    expect(button(view.container, 'Save key capacity').disabled).toBe(true);
    await submit(view.container);
    expect(api.proposals).toHaveLength(0);
    await click(view.container, 'Load current policy');
    expect(limitInput(view.container).value).toBe('5000');
    expect(limitInput(view.container).max).toBe('4000');
    await submit(view.container);
    expect(view.container.textContent).toContain('Enter a whole number from 1 to 4,000.');
    expect(api.proposals).toHaveLength(0);
    await act(async () => typeInto(limitInput(view.container), '3000'));
    await submit(view.container);
    expect(api.proposals[0]).toEqual({ key_limit_override: 3000, expected_revision: 2n,
      expected_instance_default: 1000, expected_instance_maximum: 4000 });
  } finally { await view.unmount(); }
});

it('retires the editor and cached metadata after an authoritative permission refusal', async () => {
  fixture();
  const view = await renderForm(<ProjectCapacityPolicy org="org_a" project="prj_a" />);
  try {
    await settleTask();
    await click(view.container, 'Edit key capacity');
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(Response.json({ error: { code: 'forbidden', message: 'Forbidden' } }, { status: 403 }))));
    await act(async () => view.client.invalidateQueries({ queryKey: projectCapacityKey('org_a', 'prj_a') }));
    await settleTask();
    expect(view.container.querySelector('form')).toBeNull();
    expect(view.container.textContent).not.toContain('1,200 keys used');
    expect(view.client.getQueryData(projectCapacityKey('org_a', 'prj_a'))).toBeNull();
  } finally { await view.unmount(); }
});

it('repairs a server conflict without losing the draft or silently rebasing the proposal', async () => {
  const api = fixture();
  const view = await renderForm(<ProjectCapacityPolicy org="org_a" project="prj_a" />);
  try {
    await settleTask();
    await click(view.container, 'Edit key capacity');
    await act(async () => radio(view.container, 'Custom project limit').click());
    await act(async () => typeInto(limitInput(view.container), '5000'));
    api.change({ revision: 2 });
    await submit(view.container);
    expect(view.container.textContent).toContain('Load the current policy, review your draft');
    expect(limitInput(view.container).value).toBe('5000');
    expect(button(view.container, 'Save key capacity').disabled).toBe(true);
    await click(view.container, 'Load current policy');
    expect(limitInput(view.container).value).toBe('5000');
    await submit(view.container);
    expect(api.proposals.map((proposal) => proposal.expected_revision)).toEqual([1n, 2n]);
    expect(view.container.textContent).toContain('Key capacity saved');
  } finally { await view.unmount(); }
});

it('displays a retained capped override and lets the project return to inheritance', async () => {
  const api = fixture({ key_limit_override: 5000, instance_maximum: 2000,
    effective_limit: 2000, inherited: false, capped: true });
  const view = await renderForm(<ProjectCapacityPolicy org="org_a" project="prj_a" />);
  try {
    await settleTask();
    expect(view.container.textContent).toContain('Project override: 5,000 keys');
    expect(view.container.textContent).toContain('effective limit is capped at 2,000 keys');
    await click(view.container, 'Edit key capacity');
    expect(limitInput(view.container).value).toBe('5000');
    expect(view.container.textContent).not.toContain('Proposed effective limit: 5,000 keys');
    await submit(view.container);
    expect(api.proposals).toHaveLength(0);
    await act(async () => radio(view.container, 'Inherit instance default').click());
    await submit(view.container);
    expect(api.proposals[0]?.key_limit_override).toBeNull();
    expect(view.container.textContent).toContain('Inherited from the instance default');
  } finally { await view.unmount(); }
});

it('disables editing in a read-only preview while retaining refresh', async () => {
  fixture();
  const view = await renderForm(<ReadOnlyContext value={true}><ProjectCapacityPolicy org="org_a" project="prj_a" /></ReadOnlyContext>);
  try {
    await settleTask();
    expect(button(view.container, 'Edit key capacity').disabled).toBe(true);
    expect(button(view.container, 'Refresh key capacity').disabled).toBe(false);
  } finally { await view.unmount(); }
});
