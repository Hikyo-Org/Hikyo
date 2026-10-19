import type { Meta, StoryObj } from '@storybook/react-vite';
import { zProjectCapacity, zSetProjectCapacityRequest } from '@hikyo/zod';
import { expect, userEvent } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { expectStackedRow } from '../../.storybook/layout.ts';
import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG, PRJ } from '../testkit/ids.ts';
import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import { ProjectCapacityPolicy } from './ProjectCapacityPolicy.tsx';

const url = `/api/v1/orgs/${ORG}/projects/${PRJ}/capacity`;
const inherited = {
  key_limit_override: null, instance_default: 1000, instance_maximum: 10000,
  effective_limit: 1000, key_count: 240, environment_count: 4, revision: 1,
  inherited: true, capped: false,
} satisfies z.input<typeof zProjectCapacity>;
const custom = { ...inherited, key_limit_override: 5000, effective_limit: 5000, inherited: false };

function responses(body: z.input<typeof zProjectCapacity>): MockRoute[] {
  return [{ url, body }, { url, method: 'PUT', handler: async (request) => {
    const proposal = zSetProjectCapacityRequest.parse(await request.json());
    if (request.signal.aborted) return new Response(null, { status: 499 });
    const override = proposal.key_limit_override;
    const result = { ...body, key_limit_override: override,
      effective_limit: Math.min(override ?? body.instance_default, body.instance_maximum),
      inherited: override === null, capped: override !== null && override > body.instance_maximum, revision: 2 };
    zProjectCapacity.parse(result);
    return Response.json(result);
  } }];
}

const meta = {
  title: 'Features/Projects/ProjectCapacityPolicy',
  component: ProjectCapacityPolicy,
  tags: ['ai-generated'],
  args: { org: ORG, project: PRJ },
  parameters: {
    ...topLayerDocs,
    docs: { ...topLayerDocs.docs, description: { component: 'Project settings > Policy > Key capacity. Inherit the instance default, or set a project override within the operator maximum. Existing keys survive lower limits; the independent 100,000-cell budget still applies. Uses the approved [project policy and retention layout](../prototypes/app-chrome/16/), with the shared SidePanel edit form.' } },
    app: { responses: responses(inherited) },
  },
} satisfies Meta<typeof ProjectCapacityPolicy>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Inherit: Story = { play: async ({ canvas }) => {
  await expect(await canvas.findByText('240 keys used / 1,000 key limit')).toBeVisible();
  await expect(canvas.getByText(/Inherited from the instance default/)).toBeVisible();
  const row = canvas.getByText('Key capacity').closest<HTMLElement>('.settings-row');
  if (row === null) throw new globalThis.Error('capacity settings row missing');
  await expectStackedRow(row, 2);
} };
export const Custom: Story = { parameters: { app: { responses: responses(custom) } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText('240 keys used / 5,000 key limit')).toBeVisible();
  await expect(canvas.getByText(/Project override: 5,000 keys/)).toBeVisible();
} };
export const Phone: Story = { ...Inherit, globals: { viewport: { value: 'phone', isRotated: false } } };
export const Loading: Story = { parameters: { app: { responses: [{ url, pending: true }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByRole('status')).toHaveTextContent('Loading key capacity');
} };
export const Error: Story = { parameters: { app: { responses: [{ url, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByRole('alert')).toHaveTextContent('Key capacity could not be read');
  await expect(canvas.queryByRole('button', { name: 'Edit key capacity' })).toBeNull();
} };
export const ReadOnly: Story = { decorators: [(Story) => <ReadOnlyContext value={true}><Story /></ReadOnlyContext>], play: async ({ canvas }) => {
  await expect(await canvas.findByRole('button', { name: 'Edit key capacity' })).toBeDisabled();
  await expect(canvas.getByRole('button', { name: 'Refresh key capacity' })).toBeEnabled();
} };
export const ClampedOverride: Story = { parameters: { app: { responses: responses({ ...custom, instance_maximum: 2000, effective_limit: 2000, capped: true }) } }, play: async ({ canvas }) => {
  await expect(await canvas.findByRole('alert')).toHaveTextContent('effective limit is capped at 2,000 keys');
  await expect(canvas.getByText(/Project override: 5,000 keys/)).toBeVisible();
} };
export const OverLimit: Story = { parameters: { app: { responses: responses({ ...inherited, key_count: 1200 }) } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText('1,200 keys used / 1,000 key limit')).toBeVisible();
  await expect(canvas.getByRole('alert')).toHaveTextContent('Existing keys are preserved');
} };
export const SaveCustom: Story = { play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Edit key capacity' }));
  await userEvent.click(canvas.getByRole('radio', { name: 'Custom project limit' }));
  const input = canvas.getByRole('spinbutton', { name: 'Project key limit' });
  await userEvent.clear(input);
  await userEvent.type(input, '5000');
  await userEvent.click(canvas.getByRole('button', { name: 'Save key capacity' }));
  await expect(await canvas.findByText('240 keys used / 5,000 key limit')).toBeVisible();
  await expect(canvas.getByRole('status')).toHaveTextContent('Key capacity saved');
} };
export const ResetToInheritance: Story = { parameters: { app: { responses: responses(custom) } }, play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Edit key capacity' }));
  await userEvent.click(canvas.getByRole('radio', { name: 'Inherit instance default' }));
  await userEvent.click(canvas.getByRole('button', { name: 'Save key capacity' }));
  await expect(await canvas.findByText('240 keys used / 1,000 key limit')).toBeVisible();
  await expect(canvas.getByText(/Inherited from the instance default/)).toBeVisible();
} };
export const LowerBelowUsage: Story = { play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Edit key capacity' }));
  await userEvent.click(canvas.getByRole('radio', { name: 'Custom project limit' }));
  const input = canvas.getByRole('spinbutton', { name: 'Project key limit' });
  await userEvent.clear(input);
  await userEvent.type(input, '100');
  await expect(canvas.getByText(/Existing keys are preserved when lowering the limit/)).toBeVisible();
  await userEvent.click(canvas.getByRole('button', { name: 'Save key capacity' }));
  await expect(await canvas.findByText('240 keys used / 100 key limit')).toBeVisible();
  await expect(canvas.getByText(/This project is over its effective key limit/)).toBeVisible();
} };
export const PhoneEditor: Story = { globals: { viewport: { value: 'phone', isRotated: false } }, play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Edit key capacity' }));
  await userEvent.click(canvas.getByRole('radio', { name: 'Custom project limit' }));
  await expect(canvas.getByRole('spinbutton', { name: 'Project key limit' })).toBeVisible();
  await expect(canvas.getByRole('button', { name: 'Save key capacity' })).toBeVisible();
  const save = canvas.getByRole('button', { name: 'Save key capacity' });
  const actions = save.closest<HTMLElement>('.actions');
  if (actions === null) throw new globalThis.Error('capacity editor actions missing');
  await expect(actions).toHaveClass('actions--stack');
  for (const control of actions.querySelectorAll<HTMLButtonElement>('button')) {
    await expect(getComputedStyle(control).alignItems).toBe('center');
    await expect(getComputedStyle(control).justifyContent).toBe('center');
    await expect(Math.abs(control.getBoundingClientRect().width - actions.getBoundingClientRect().width)).toBeLessThanOrEqual(1);
  }
} };
export const Saving: Story = { parameters: { app: { responses: [{ url, body: inherited }, { url, method: 'PUT', pending: true }] } }, play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Edit key capacity' }));
  await userEvent.click(canvas.getByRole('button', { name: 'Save key capacity' }));
  await expect(await canvas.findByRole('button', { name: 'Saving key capacity…' })).toBeDisabled();
  await expect(canvas.getByRole('radio', { name: 'Inherit instance default' })).toBeDisabled();
} };
export const SaveRefused: Story = { parameters: { app: { responses: [{ url, body: inherited }, { url, method: 'PUT', status: 409, body: { error: { code: 'conflict', message: 'Changed' } } }] } }, play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Edit key capacity' }));
  await userEvent.click(canvas.getByRole('radio', { name: 'Custom project limit' }));
  await userEvent.click(canvas.getByRole('button', { name: 'Save key capacity' }));
  await expect(await canvas.findByText(/The project or instance capacity policy changed/)).toBeVisible();
  await expect(canvas.getByRole('button', { name: 'Save key capacity' })).toBeDisabled();
  await expect(canvas.getByRole('spinbutton', { name: 'Project key limit' })).toHaveValue(1000);
} };
