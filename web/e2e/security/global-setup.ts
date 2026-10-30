import { setTimeout as delay } from 'node:timers/promises';

import { startSecurityInstance, stopInstance } from '../fixtures/instance.ts';
import { seedStoredContent } from './seed.ts';
import { startStoredContent } from './stored-content.ts';

export default async function setup(): Promise<() => Promise<void>> {
  const canary = await startStoredContent();
  try {
    await startSecurityInstance((page, seed) => seedStoredContent(page, seed, canary.payloads));
    // Native bootstrap, MFA and tenant seeding spend the same real one-minute
    // source allowance as the worker. Let that completed setup window expire
    // before the campaign; neither server limits nor test deadlines change.
    await delay(60_100, undefined, { signal: AbortSignal.timeout(61_000) });
    return canary.close;
  } catch (error) {
    const stopped = await Promise.allSettled([stopInstance(), canary.close()]);
    const cleanupErrors = stopped.flatMap((result) => result.status === 'rejected' ? [result.reason] : []);
    if (cleanupErrors.length > 0) {
      throw new AggregateError([error, ...cleanupErrors], 'security instance setup failed and cleanup also failed');
    }
    throw error;
  }
}
