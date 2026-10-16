import type { MatrixEnvironmentRow } from '../api/matrix.ts';

/** Supply the production hook's projections for isolated route fixtures. */
export function matrixProjectFixture<T extends { readonly environmentRows: readonly MatrixEnvironmentRow[] }>(project: T) {
  return {
    ...project,
    environmentItems: project.environmentRows.map((row) => row.environment),
    environmentQueries: {
      values: project.environmentRows.map((row) => ({ environmentId: row.environmentId, query: row.values })),
      signals: project.environmentRows.map((row) => ({ environmentId: row.environmentId, query: row.signals })),
      settings: project.environmentRows.map((row) => ({ environmentId: row.environmentId, query: row.settings })),
      pendingDrafts: project.environmentRows.map((row) => ({ environmentId: row.environmentId, query: row.pendingDrafts })),
    },
  };
}
