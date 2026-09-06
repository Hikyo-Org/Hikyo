import { useProjectEnvironments } from './adapters.ts';
import { useOrg, useProject } from './settings.ts';

/** Resolve only the addressed scope through ordinary authorized metadata reads.
 * Never enumerate an instance or member directory to label a reference.
 */
export function useScopeNames(org: string, project = '', environment = '') {
  const organisation = useOrg(org);
  const projectQuery = useProject(org, project);
  const environments = useProjectEnvironments({ org, project }, environment !== '' && project !== '');
  return {
    org: organisation.isError ? org : organisation.data?.name ?? org,
    project: projectQuery.isError ? project : projectQuery.data?.name ?? project,
    environment: environments.isError ? environment : environments.data?.items.find((item) => item.id === environment)?.name ?? environment,
  };
}
