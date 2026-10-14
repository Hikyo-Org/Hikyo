import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { ProjectContext } from './Shell.tsx';

const base = '/orgs/acme/projects/payments';

const meta = {
  title: 'Shared/ProjectContext',
  id: 'routes-projectcontext',
  component: ProjectContext,
  tags: ['ai-generated'],
  // The block renders router links; the app harness supplies the MemoryRouter.
  parameters: {
    ...topLayerDocs,
    docs: { ...topLayerDocs.docs, description: { component: "The sidebar's project block: the environment matrix with the folder groups and the Problems filter the matrix publishes into it, then the project's other surfaces." } },
    app: { responses: [], path: `${base}/matrix` },
  },
  args: {
    org: 'acme',
    orgName: 'Acme',
    projectName: 'Payments',
    orgRole: 'admin',
    links: [
      { id: 'matrix', label: 'Environment matrix', to: `${base}/matrix`, disabledReason: null },
      { id: 'machine-access', label: 'Machine access', to: `${base}/machine-access`, disabledReason: null },
      { id: 'project-settings', label: 'Project settings', to: `${base}/settings`, disabledReason: null },
    ],
    state: {
      groups: [
        { id: 'app', name: 'app', keyCount: 3, problemCount: 0, hidden: false },
        { id: 'db', name: 'db', keyCount: 2, problemCount: 1, hidden: false },
      ],
      problemCount: 1,
      problemsActive: false,
      onSelectGroup: fn(),
      onToggleProblems: fn(),
    },
    onNavigate: fn(),
  },
} satisfies Meta<typeof ProjectContext>;

export default meta;
type Story = StoryObj<typeof meta>;

// The filter is a capitalised "Problems" toggle with its count.
export const WithGroups: Story = {
  play: async ({ canvas, canvasElement, args }) => {
    const problems = canvas.getByRole('button', { name: /^Problems/ });
    await expect(problems).toHaveAttribute('aria-pressed', 'false');
    await expect(problems).toHaveTextContent('Problems1');
    problems.click();
    await expect(args.state?.onToggleProblems).toHaveBeenCalled();
    // Rows run flush to the block's left edge with no guide line; folders keep their indent.
    const block = canvasElement.querySelector('.context-sidebar');
    const link = canvas.getByRole('link', { name: 'Machine access' });
    if (block === null) throw new Error('project block missing');
    await expect(link.getBoundingClientRect().left).toBe(block.getBoundingClientRect().left);
    await expect(getComputedStyle(link).borderLeftColor).toBe('rgba(0, 0, 0, 0)');
    await expect(parseFloat(getComputedStyle(problems).paddingLeft)).toBeGreaterThan(parseFloat(getComputedStyle(link).paddingLeft));
  },
};
