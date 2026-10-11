import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor, within } from 'storybook/test';

import { isManualDocsFrame } from '../../.storybook/manualDocsFrame.ts';
import { appearancePass, interactionOnce } from '../../.storybook/interactionCoverage.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { AppRoutes } from '../app/App.tsx';
import { Overview } from './Placeholder.tsx';
import { createOverviewFixture, overviewIdentity, overviewProject } from './Overview.story-fixtures.ts';

const fixture = createOverviewFixture();

const meta = {
  title: 'Pages/Overview',
  component: Overview,
  tags: ['ai-generated'],
  render: () => <AppRoutes />,
  beforeEach: async () => {
    fixture.reset();
    // The journey explores navigation and query refresh, not a cold module
    // download. Await the actual lazy group's readiness so full-suite
    // transform contention cannot consume the interaction assertion window.
    // AppRoutes still owns and renders the real lazy route elements.
    await import('../app/route-groups/workspace.ts');
    return () => fixture.reset();
  },
  parameters: {
    ...topLayerDocs,
    docs: {
      ...topLayerDocs.docs,
      description: {
        component: 'The real application route and navigation shell. The journey creates a project through its actual form and verifies that the overview and project list share the updated query cache. API storage is simulated; authentication, route policy, components, and providers are real.',
      },
      story: { ...topLayerDocs.docs.story, autoplay: false },
    },
    app: { auth: true, identity: overviewIdentity, path: '/', routeTree: true, responses: fixture.responses },
  },
} satisfies Meta<typeof Overview>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Choose a project' })).toBeVisible();
    await expect(await canvas.findByText('Acme operations')).toBeVisible();
    await expect(canvas.queryByRole('link', { name: 'Settings for Billing platform' })).toBeNull();
  },
};

export const Populated: Story = {
  beforeEach: () => fixture.reset([overviewProject]),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Open matrix' })).toBeVisible();
  },
};

export const LongNames: Story = {
  beforeEach: () => fixture.reset([{ ...overviewProject, name: 'Customer billing and reconciliation platform with an intentionally long operational project name' }]),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('link', { name: /Settings for Customer billing and reconciliation/ })).toBeVisible();
  },
};

/** Real navigation, form submission and cache refresh, with an explorable Docs starting state. */
export const CreateProjectJourney: Story = {
  tags: ['interaction-once'],
  beforeEach: ({ globals }) => {
    // Seed before rendering so the appearance replacement reaches the same
    // populated Projects route, providers and navigation shell as the journey.
    if (appearancePass(globals)) fixture.reset([overviewProject]);
  },
  play: interactionOnce('pages-overview--create-project-journey', async ({ canvas, step }) => {
    if (isManualDocsFrame()) return;
    await step('Start in the authenticated overview', async () => {
      await expect(await canvas.findByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
      await expect(await canvas.findByText('Acme operations')).toBeVisible();
      await expect(canvas.queryByRole('link', { name: 'Settings for Billing platform' })).toBeNull();
    });
    await step('Follow the real Projects route', async () => {
      await userEvent.click(canvas.getByRole('link', { name: 'Choose a project' }));
      await expect(await canvas.findByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
      await expect(await canvas.findByText('No projects yet. Create the first one below.')).toBeVisible();
      await expect(canvas.getByRole('button', { name: 'Create project' })).toBeDisabled();
    });
    await step('Create a project and wait for its list refresh', async () => {
      await userEvent.type(canvas.getByLabelText('Project name'), 'Billing platform');
      await userEvent.click(canvas.getByRole('button', { name: 'Create project' }));
      await expect(await canvas.findByText('Project Billing platform created.')).toBeVisible();
      await expect(canvas.getByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
      await expect(canvas.getByLabelText('Project name')).toHaveValue('');
      await expect(fixture.submittedNames()).toEqual(['Billing platform']);
    });
    await step('Return through the sidebar and revisit the shared project list', async () => {
      const narrow = globalThis.matchMedia('(max-width: 700px)').matches;
      if (narrow) await userEvent.click(canvas.getByRole('button', { name: 'Menu' }));
      const navigation = canvas.getByRole('navigation', { name: 'Sections' });
      const overview = within(navigation).getByRole('link', { name: 'Overview' });
      await userEvent.click(overview);
      await expect(await canvas.findByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
      await waitFor(() => expect(canvas.getByRole('link', { name: 'Settings for Billing platform' })).toBeVisible());
      if (narrow) await expect(canvas.getByRole('button', { name: 'Menu' })).toHaveAttribute('aria-expanded', 'false');
      await userEvent.click(canvas.getByRole('link', { name: 'Choose a project' }));
      await expect(await canvas.findByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
      await expect(canvas.getByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
      await expect(canvas.queryByText('No projects yet. Create the first one below.')).toBeNull();
    });
  }, async ({ canvas }) => {
    // Render the same final real-route composition without repeating creation
    // and cache-refresh behavior. Async readiness still precedes the a11y scan.
    await expect(await canvas.findByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await expect(await canvas.findByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
    await userEvent.click(canvas.getByRole('link', { name: 'Choose a project' }));
    await expect(await canvas.findByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
    await expect(await canvas.findByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
    await expect(canvas.getByLabelText('Project name')).toHaveValue('');
    await expect(canvas.queryByText('No projects yet. Create the first one below.')).toBeNull();
  }),
};
