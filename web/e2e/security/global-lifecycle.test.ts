import { beforeEach, expect, it, vi } from 'vitest';

const fixture = vi.hoisted(() => ({
  startSecurityInstance: vi.fn<() => Promise<void>>(),
  stopInstance: vi.fn<() => Promise<void>>(),
  delay: vi.fn<(milliseconds: number, value: undefined, options: { signal: AbortSignal }) => Promise<void>>(),
  startStoredContent: vi.fn<() => Promise<{ payloads: string[]; close: () => Promise<void> }>>(),
  closeCanary: vi.fn<() => Promise<void>>(),
}));
vi.mock('../fixtures/instance.ts', () => fixture);
vi.mock('node:timers/promises', () => ({ setTimeout: fixture.delay }));
vi.mock('./stored-content.ts', () => ({ startStoredContent: fixture.startStoredContent }));
vi.mock('./seed.ts', () => ({ seedStoredContent: vi.fn() }));

import setup from './global-setup.ts';
import teardown from './global-teardown.ts';

beforeEach(() => {
  vi.resetAllMocks();
  fixture.startStoredContent.mockResolvedValue({ payloads: [], close: fixture.closeCanary });
});

it('holds the worker until the setup source window expires', async () => {
  let expire: () => void = () => { throw new Error('window barrier was not prepared'); };
  const expired = new Promise<void>((resolve) => { expire = resolve; });
  fixture.delay.mockReturnValue(expired);
  let completed = false;
  const started = setup().then((close) => { completed = true; return close; });
  await expect.poll(() => fixture.delay.mock.calls.length).toBe(1);
  expect(fixture.startSecurityInstance).toHaveBeenCalledOnce();
  expect(fixture.delay).toHaveBeenCalledOnce();
  expect(completed).toBe(false);
  expire();
  const close = await started;
  expect(completed).toBe(true);
  expect(fixture.closeCanary).not.toHaveBeenCalled();
  await close();
  expect(fixture.closeCanary).toHaveBeenCalledOnce();
});

it('cleans the owned instance when the source-window barrier aborts', async () => {
  const stopped = new Error('source window wait aborted');
  fixture.delay.mockRejectedValue(stopped);
  await expect(setup()).rejects.toBe(stopped);
  expect(fixture.stopInstance).toHaveBeenCalledOnce();
  expect(fixture.closeCanary).toHaveBeenCalledOnce();
});

it('does not start the instance after the canary listener fails to start', async () => {
  const error = new Error('canary could not bind');
  fixture.startStoredContent.mockRejectedValue(error);
  await expect(setup()).rejects.toBe(error);
  expect(fixture.startSecurityInstance).not.toHaveBeenCalled();
});

it('joins both cleanup owners when setup and canary cleanup fail', async () => {
  const setupError = new Error('instance setup failed');
  const canaryError = new Error('canary listener cleanup failed');
  fixture.startSecurityInstance.mockRejectedValue(setupError);
  fixture.closeCanary.mockRejectedValue(canaryError);
  await expect(setup()).rejects.toMatchObject({ errors: [setupError, canaryError] });
  expect(fixture.stopInstance).toHaveBeenCalledOnce();
  expect(fixture.closeCanary).toHaveBeenCalledOnce();
});

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
