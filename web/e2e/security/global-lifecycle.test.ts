import { beforeEach, expect, it, vi } from 'vitest';

const fixture = vi.hoisted(() => ({
  startSecurityInstance: vi.fn<() => Promise<void>>(),
  stopInstance: vi.fn<() => Promise<void>>(),
}));
vi.mock('../fixtures/instance.ts', () => fixture);

import setup from './global-setup.ts';
import teardown from './global-teardown.ts';

beforeEach(() => { vi.resetAllMocks(); });

it('waits for asynchronous fixture cleanup before completing global teardown', async () => {
  let finish: () => void = () => { throw new Error('cleanup completion was not prepared'); };
  const finished = new Promise<void>((resolve) => { finish = resolve; });
  fixture.stopInstance.mockReturnValue(finished);
  let completed = false;
  const cleanup = teardown().then(() => { completed = true; });
  await Promise.resolve();
  expect(fixture.stopInstance).toHaveBeenCalledOnce();
  expect(completed).toBe(false);
  finish();
  await cleanup;
  expect(completed).toBe(true);
});

it('preserves both the setup failure and cleanup failure', async () => {
  const original = new Error('instance startup failed');
  const cleanup = new Error('mail child cleanup failed');
  fixture.startSecurityInstance.mockRejectedValue(original);
  fixture.stopInstance.mockRejectedValue(cleanup);
  await expect(setup()).rejects.toMatchObject({
    message: 'security instance setup failed and cleanup also failed',
    errors: [original, cleanup],
  });
});

it('preserves the original setup failure after successful cleanup', async () => {
  const original = new Error('instance startup failed');
  fixture.startSecurityInstance.mockRejectedValue(original);
  fixture.stopInstance.mockResolvedValue();
  await expect(setup()).rejects.toBe(original);
  expect(fixture.stopInstance).toHaveBeenCalledOnce();
});
