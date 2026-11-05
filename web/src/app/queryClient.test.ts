import { QueryClient } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { makeQueryClient, retireQueryClient } from './queryClient.ts';

const sensitive = vi.hoisted(() => ({
  retireSensitiveOperations: vi.fn<(client: QueryClient) => void>(),
}));

vi.mock('../api/sensitiveMutation.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/sensitiveMutation.ts')>();
  return { ...actual, retireSensitiveOperations: sensitive.retireSensitiveOperations };
});

afterEach(() => {
  vi.restoreAllMocks();
  sensitive.retireSensitiveOperations.mockReset();
});

// The order is the contract: sensitive deliveries are revoked before any
// answer could settle, queries are cancelled before the cache they would write
// to disappears, and the clear comes last so nothing outlives its owner.
it('retires sensitive work, cancels queries, then clears, in that order', () => {
  const cancel = vi.spyOn(QueryClient.prototype, 'cancelQueries');
  const clear = vi.spyOn(QueryClient.prototype, 'clear');
  const queries = makeQueryClient();

  retireQueryClient(queries);

  expect(sensitive.retireSensitiveOperations).toHaveBeenCalledExactlyOnceWith(queries);
  expect(cancel).toHaveBeenCalledOnce();
  expect(clear).toHaveBeenCalledOnce();
  const retiredAt = sensitive.retireSensitiveOperations.mock.invocationCallOrder[0];
  const cancelledAt = cancel.mock.invocationCallOrder[0];
  const clearedAt = clear.mock.invocationCallOrder[0];
  expect(retiredAt).toBeLessThan(cancelledAt ?? Number.NaN);
  expect(cancelledAt).toBeLessThan(clearedAt ?? Number.NaN);
});

describe('query retries', () => {
  const { retry, retryDelay } = makeQueryClient().getDefaultOptions().queries ?? {};
  if (typeof retry !== 'function' || typeof retryDelay !== 'function') throw new Error('expected retry policies');

  it('retries only a throttled read, boundedly', () => {
    const throttled = new ApiError(429, 'throttled');
    expect(retry(0, throttled)).toBe(true);
    expect(retry(1, throttled)).toBe(true);
    expect(retry(2, throttled)).toBe(false);
    expect(retry(0, new ApiError(403, 'denied'))).toBe(false);
    expect(retry(0, new Error('offline'))).toBe(false);
  });

  it('waits as long as the refusal says', () => {
    expect(retryDelay(0, new ApiError(429, 'throttled', undefined, 12_000))).toBe(12_000);
    expect(retryDelay(0, new ApiError(429, 'throttled'))).toBe(5_000);
  });
});
