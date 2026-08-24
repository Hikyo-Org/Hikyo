import { describe, expect, it } from 'vitest';

import {
  pendingDraftsKey,
  pendingMatrixKey,
  signalsKey,
  signalsMatrixKey,
  valuesKey,
  valuesMatrixKey,
  type EnvRef,
} from './keys.ts';

const env: EnvRef = {
  org: 'org_a',
  project: 'project_a',
  environment: 'environment_a',
};

describe('matrix-wide query keys', () => {
  it.each([
    [valuesMatrixKey(env), valuesKey(env)],
    [signalsMatrixKey(env), signalsKey(env, env.environment)],
    [pendingMatrixKey(env), pendingDraftsKey(env, env.environment)],
  ])('matches the full query key prefix', (matrixKey, fullKey) => {
    expect(matrixKey).toEqual(fullKey.slice(0, 3));
  });
});
