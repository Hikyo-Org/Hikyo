import { zCreateProjectRequest, zMeta, zMyOrgList, zProject, zProjectList } from '@hikyo/zod';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { ORG, PRJ } from '../testkit/ids.ts';

export const overviewIdentity = {
  ...authenticatedIdentity,
  principal: { ...authenticatedIdentity.principal, display_name: 'Alex Rivera' },
  capabilities: { instance_operator: false, delivery_report_grant: { instance: false, orgs: [] } },
};

export const overviewProject = {
  id: PRJ,
  org_id: ORG,
  name: 'Billing platform',
  created_at: '2026-01-01T00:00:00Z',
} satisfies z.input<typeof zProject>;

/** One story's in-memory API. Real routes and cache invalidation consume these replies. */
export function createOverviewFixture() {
  let projects: z.input<typeof zProject>[] = [];
  let submittedNames: string[] = [];
  const projectPath = `/api/v1/orgs/${ORG}/projects`;
  const responses: readonly MockRoute[] = [
    {
      url: '/api/v1/me/orgs',
      body: zMyOrgList.parse({ items: [{ id: ORG, name: 'Acme operations' }], count: 1 }),
    },
    {
      url: '/api/v1/meta',
      body: zMeta.parse({ server_version: 'storybook-local', api_revision: 5, protocol_capabilities: [] }),
    },
    {
      url: projectPath,
      handler: () => Response.json(zProjectList.parse({ items: projects, count: projects.length })),
    },
    {
      url: projectPath,
      method: 'POST',
      handler: async (request) => {
        const input = zCreateProjectRequest.safeParse(await request.json());
        request.signal.throwIfAborted();
        if (!input.success) {
          return Response.json({ error: { code: 'bad_request', message: 'A valid project name is required.' } }, { status: 400 });
        }
        if (projects.some((project) => project.name === input.data.name)) {
          return Response.json({ error: { code: 'conflict', message: 'That project name already exists.' } }, { status: 409 });
        }
        const project = zProject.parse({ ...overviewProject, name: input.data.name });
        projects = [...projects, project];
        submittedNames.push(input.data.name);
        return Response.json(project, { status: 201 });
      },
    },
  ];
  return {
    responses,
    reset(items: readonly z.input<typeof zProject>[] = []) {
      projects = zProjectList.parse({ items, count: items.length }).items;
      submittedNames = [];
    },
    submittedNames: () => [...submittedNames],
  };
}
