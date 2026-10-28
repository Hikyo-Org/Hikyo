import { afterEach, expect, it, vi } from 'vitest';
import { existsSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import type { Server } from 'node:http';

import * as canaryModule from './canary.ts';
import { readStoredContent, startStoredContent } from './stored-content.ts';

const ownedDirectories: string[] = [];
afterEach(() => {
  vi.restoreAllMocks();
  for (const directory of ownedDirectories.splice(0)) rmSync(directory, { recursive: true, force: true });
});

function record(): string {
  const directory = mkdtempSync(join(tmpdir(), 'hikyo-stored-content-'));
  ownedDirectories.push(directory);
  return join(directory, 'observation.json');
}

it('publishes real hits atomically and refuses egress after joining the owned listener', async () => {
  const path = record();
  const owned = await startStoredContent(path);
  let closed = false;
  try {
    const before = readStoredContent(path);
    expect(before.hits).toBe(0);
    expect(before.payloads).toHaveLength(4);
    expect(before.payloads[0]).toContain(before.origin);
    await fetch(`${before.origin}/forbidden-egress`);
    expect(readStoredContent(path).hits).toBe(1);
    expect(existsSync(`${path}.tmp`)).toBe(false);
    closed = true;
    await expect(owned.close()).rejects.toThrow('stored-content canary failed');
    expect(existsSync(path)).toBe(false);
    await expect(fetch(before.origin)).rejects.toThrow();
  } finally {
    if (!closed) await owned.close();
  }
});

it('refuses a real hit that arrives after the last observation while joining the listener', async () => {
  const original = canaryModule.startCanary;
  vi.spyOn(canaryModule, 'startCanary').mockImplementation(async () => {
    const canary = await original();
    return {
      ...canary,
      close: async () => {
        await fetch(`${canary.origin}/late-forbidden-egress`);
        await canary.close();
      },
    };
  });
  const path = record();
  const owned = await startStoredContent(path);
  expect(readStoredContent(path).hits).toBe(0);
  await expect(owned.close()).rejects.toThrow('stored-content canary failed');
  expect(existsSync(path)).toBe(false);
});

it('invalidates observations and fails cleanup after a listener error', async () => {
  const original = canaryModule.startCanary;
  const listeners: Server[] = [];
  vi.spyOn(canaryModule, 'startCanary').mockImplementation(async () => {
    const canary = await original();
    listeners.push(canary.server);
    return canary;
  });
  const path = record();
  const owned = await startStoredContent(path);
  const server = listeners[0];
  if (server === undefined) throw new Error('owned canary listener missing');
  server.emit('error', new Error('fixture listener failure'));
  expect(() => readStoredContent(path)).toThrow();
  await expect(owned.close()).rejects.toThrow('stored-content canary failed');
  expect(server.listening).toBe(false);
  expect(existsSync(path)).toBe(false);
});

it('refuses oversized or malformed observations instead of treating them as zero hits', () => {
  const path = record();
  writeFileSync(path, 'x'.repeat(16 * 1024 + 1));
  expect(() => readStoredContent(path)).toThrow('exceeds its bound');
  writeFileSync(path, JSON.stringify({ origin: 'not a URL', payloads: [], hits: -1 }));
  expect(() => readStoredContent(path)).toThrow();
});

it('invalidates observations when the listener stops before teardown', async () => {
  const original = canaryModule.startCanary;
  const listeners: Server[] = [];
  vi.spyOn(canaryModule, 'startCanary').mockImplementation(async () => {
    const canary = await original();
    listeners.push(canary.server);
    return canary;
  });
  const path = record();
  const owned = await startStoredContent(path);
  const server = listeners[0];
  if (server === undefined) throw new Error('owned canary listener missing');
  await new Promise<void>((resolve, reject) => {
    server.close((error) => error === undefined ? resolve() : reject(error));
  });
  expect(() => readStoredContent(path)).toThrow();
  await expect(owned.close()).rejects.toThrow('stored-content canary failed');
  expect(existsSync(path)).toBe(false);
});

it('retains an observation failure that arrives while joining the listener', async () => {
  const original = canaryModule.startCanary;
  vi.spyOn(canaryModule, 'startCanary').mockImplementation(async () => {
    const canary = await original();
    return {
      ...canary,
      close: async () => {
        canary.server.emit('error', new Error('late listener failure'));
        await canary.close();
      },
    };
  });
  const path = record();
  const owned = await startStoredContent(path);
  await expect(owned.close()).rejects.toThrow('stored-content canary failed');
  expect(existsSync(path)).toBe(false);
});

it('refuses an existing observation without replacing its custody', async () => {
  const path = record();
  writeFileSync(path, 'prior-run-observation');
  await expect(startStoredContent(path)).rejects.toThrow('existing stored-content canary record');
});
