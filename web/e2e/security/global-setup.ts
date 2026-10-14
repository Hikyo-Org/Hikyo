import { startSecurityInstance, stopInstance } from '../fixtures/instance.ts';

export default async function setup(): Promise<void> {
  try { await startSecurityInstance(); }
  catch (error) { stopInstance(); throw error; }
}
