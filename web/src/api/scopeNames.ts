import { useProjectEnvironments } from './adapters.ts';
import { useOrg, useProject } from './settings.ts';

/** A query is settled once it succeeded or failed; a disabled (idle) query never will, so it counts as done. */
export function querySettled(q: { isPending: boolean; fetchStatus: string }): boolean {
  return !q.isPending || q.fetchStatus === 'idle';
}

/** Resolve only the addressed scope through ordinary authorized metadata reads.
 * Never enumerate an instance or member directory to label a reference.
 */
export function useScopeNames(org: string, project = '', environment = '') {
  const organisation = useOrg(org);
  const projectQuery = useProject(org, project);
  const environments = useProjectEnvironments({ org, project }, environment !== '' && project !== '');
  return {
    settled: querySettled(organisation) && querySettled(projectQuery) && querySettled(environments),
    org: organisation.isError ? org : organisation.data?.name ?? org,
    project: projectQuery.isError ? project : projectQuery.data?.name ?? project,
    environment: environments.isError ? environment : environments.data?.items.find((item) => item.id === environment)?.name ?? environment,
  };
}
