import { existsSync, mkdtempSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, expect, it } from 'vitest';

import { readWebuiOIDCIssuer, startWebuiIdP, stopInstance } from './instance.ts';

const directories: string[] = [];
afterEach(() => {
  stopInstance();
  for (const dir of directories.splice(0)) rmSync(dir, { recursive: true, force: true });
});

it('starts the second IdP when the previous fixed default port is occupied', async () => {
  const blocker = createServer();
  await new Promise<void>((resolve, reject) => {
    blocker.once('error', (error: NodeJS.ErrnoException) => {
      if (error.code === 'EADDRINUSE') resolve();
      else reject(error);
    });
    blocker.listen(45795, '127.0.0.1', resolve);
  });
  const dir = mkdtempSync(join(tmpdir(), 'hikyo-idp-port-test-'));
  directories.push(dir);
  try {
    const issuerFile = join(dir, 'issuer.json');
    writeFileSync(issuerFile, JSON.stringify({ issuer: 'http://127.0.0.1:45795' }), { mode: 0o644 });
    await startWebuiIdP({ dir }, issuerFile);
    const issuer = readWebuiOIDCIssuer(issuerFile);
    expect(new URL(issuer).port).not.toBe('45795');
    expect(statSync(issuerFile).mode & 0o777).toBe(0o600);
    const response = await fetch(`${issuer}/.well-known/openid-configuration`);
    expect(response.ok).toBe(true);
    stopInstance();
    expect(existsSync(issuerFile)).toBe(false);
  } finally {
    if (blocker.listening) {
      await new Promise<void>((resolve, reject) => {
        blocker.close((error) => {
          if (error) reject(error);
          else resolve();
        });
      });
    }
  }
}, 30_000);

it('refuses an occupied explicitly requested second-provider port', async () => {
  const blocker = createServer();
  await new Promise<void>((resolve, reject) => {
    blocker.once('error', reject);
    blocker.listen(0, '127.0.0.1', resolve);
  });
  const address = blocker.address();
  if (address === null || typeof address === 'string') throw new Error('expected TCP address');
  const dir = mkdtempSync(join(tmpdir(), 'hikyo-idp-explicit-test-'));
  directories.push(dir);
  const issuerFile = join(dir, 'issuer.json');
  try {
    await expect(startWebuiIdP({ dir }, issuerFile, address.port)).rejects.toThrow(
      `something is already listening on 127.0.0.1:${String(address.port)}`,
    );
    expect(existsSync(issuerFile)).toBe(false);
  } finally {
    await new Promise<void>((resolve, reject) => {
      blocker.close((error) => {
        if (error) reject(error);
        else resolve();
      });
    });
  }
});

it('reads persisted issuer state rather than process-local setup values', () => {
  const dir = mkdtempSync(join(tmpdir(), 'hikyo-idp-state-test-'));
  directories.push(dir);
  const issuerFile = join(dir, 'issuer.json');
  writeFileSync(issuerFile, JSON.stringify({ issuer: 'http://127.0.0.1:51001' }));
  expect(readWebuiOIDCIssuer(issuerFile)).toBe('http://127.0.0.1:51001');
  writeFileSync(issuerFile, JSON.stringify({ issuer: 'http://127.0.0.1:51002' }));
  expect(readWebuiOIDCIssuer(issuerFile)).toBe('http://127.0.0.1:51002');
  writeFileSync(issuerFile, JSON.stringify({ issuer: 'http://127.0.0.1:0' }));
  expect(() => readWebuiOIDCIssuer(issuerFile)).toThrow('expected a bound loopback OIDC issuer');
});
