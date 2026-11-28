import { renameEnvironmentOp } from '@hikyo/operations';
import { beforeEach, expect, it, vi } from 'vitest';

import { ApiError } from './client.ts';
import { RenameReauthFailure, renameEnvironmentRefusalText, renameWithReauth } from './settings.ts';

const ceremony = vi.hoisted(() => vi.fn());
vi.mock('./values.ts', () => ({ runPasskeyCeremony: ceremony }));

const refusal = new ApiError(409, 'request failed with 409',
  'reauthenticate over the environments this operation makes reachable, then retry (env_a)', undefined, [], undefined, renameEnvironmentOp);

beforeEach(() => {
  ceremony.mockReset();
});

it('says a rename whose passkey prompt was dismissed was not renamed (#1447)', async () => {
  const cancelled = new DOMException('cancelled', 'NotAllowedError');
  ceremony.mockRejectedValue(cancelled);
  const send = vi.fn().mockRejectedValue(refusal);
  const error = await renameWithReauth(send).catch((cause: unknown) => cause);
  expect(error).toBeInstanceOf(RenameReauthFailure);
  expect(error).toMatchObject({ environment: 'env_a', cause: cancelled });
  expect(send).toHaveBeenCalledOnce();
  expect(renameEnvironmentRefusalText(error)).toBe('The passkey prompt was dismissed or timed out. The environment was not renamed.');
});

it('does not read a passkey endpoint refusal as the rename\'s own', async () => {
  ceremony.mockRejectedValue(new ApiError(409, 'x', 'name taken'));
  const error = await renameWithReauth(vi.fn().mockRejectedValue(refusal)).catch((cause: unknown) => cause);
  expect(renameEnvironmentRefusalText(error)).toBe('The passkey reauthentication could not be completed. The environment was not renamed.');
});

it('keeps an unknown outcome uncertain', () => {
  expect(renameEnvironmentRefusalText(new Error('network'))).toContain('whether the environment was renamed is unknown');
});
