import type { QueryClient } from '@tanstack/react-query';

export type EnvRef = { org: string; project: string; environment: string };
export type MatrixRef = { readonly org: string; readonly project: string };

export const valuesMatrixKey = (ref: MatrixRef) =>
  ['values', ref.org, ref.project] as const;
export const valuesKey = (env: EnvRef) =>
  [...valuesMatrixKey(env), env.environment] as const;
export const windowKey = (env: EnvRef) =>
  ['reveal-window', env.org, env.project, env.environment] as const;
export const revisionsKey = (env: EnvRef) =>
  ['revisions', env.org, env.project, env.environment] as const;
export const revisionDetailsKey = (env: EnvRef) =>
  ['revision-detail', env.org, env.project, env.environment] as const;
export const revisionDetailKey = (env: EnvRef, revision: string) =>
  [...revisionDetailsKey(env), revision] as const;
export const pinsKey = (env: EnvRef) =>
  ['revision-pins', env.org, env.project, env.environment] as const;
export const projectRetentionKey = (ref: MatrixRef) =>
  ['project-retention', ref.org, ref.project] as const;
export const matrixKeysKey = (ref: MatrixRef) =>
  ['matrix-keys', ref.org, ref.project] as const;
export const matrixGroupsKey = (ref: MatrixRef) =>
  ['matrix-groups', ref.org, ref.project] as const;
export const signalsMatrixKey = (ref: MatrixRef) =>
  ['matrix-signals', ref.org, ref.project] as const;
export const signalsKey = (ref: MatrixRef, environment: string) =>
  [...signalsMatrixKey(ref), environment] as const;
export const pendingMatrixKey = (ref: MatrixRef) =>
  ['matrix-pending', ref.org, ref.project] as const;
export const pendingDraftsKey = (ref: MatrixRef, environment: string) =>
  [...pendingMatrixKey(ref), environment] as const;

/** Project-wide cache prefixes affected when its environment topology changes. */
export function environmentTopologyQueryPrefixes(ref: MatrixRef) {
  return [
    ['values', ref.org, ref.project],
    ['reveal-window', ref.org, ref.project],
    ['revisions', ref.org, ref.project],
    ['revision-detail', ref.org, ref.project],
    ['revision-pins', ref.org, ref.project],
    ['matrix-keys', ref.org, ref.project],
    ['matrix-signals', ref.org, ref.project],
    ['matrix-pending', ref.org, ref.project],
  ];
}

/** Every cache whose destination-owned state changes after a successful copy. */
export function copyInvalidationKeys(
  ref: MatrixRef,
  destinationEnvironmentIds: readonly string[],
) {
  return destinationEnvironmentIds.flatMap((environment) => {
    const env = { ...ref, environment };
    return [
      valuesKey(env),
      windowKey(env),
      revisionsKey(env),
      pinsKey(env),
      revisionDetailsKey(env),
      signalsKey(ref, environment),
      pendingDraftsKey(ref, environment),
    ];
  });
}

export async function invalidateAfterCopy(
  queries: QueryClient,
  ref: MatrixRef,
  destinationEnvironmentIds: readonly string[],
): Promise<void> {
  await Promise.all(
    copyInvalidationKeys(ref, destinationEnvironmentIds).map((queryKey) =>
      queries.invalidateQueries({ queryKey }),
    ),
  );
}
