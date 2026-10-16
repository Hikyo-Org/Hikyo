import { startSecurityInstance, stopInstance } from '../fixtures/instance.ts';

export default async function setup(): Promise<void> {
  try {
    await startSecurityInstance();
  } catch (error) {
    try {
      await stopInstance();
    } catch (cleanupError) {
      throw new AggregateError([error, cleanupError], 'security instance setup failed and cleanup also failed');
    }
    throw error;
  }
}
