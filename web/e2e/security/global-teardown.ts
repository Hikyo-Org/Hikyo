import { stopInstance } from '../fixtures/instance.ts';

export default async function teardown(): Promise<void> { await stopInstance(); }
