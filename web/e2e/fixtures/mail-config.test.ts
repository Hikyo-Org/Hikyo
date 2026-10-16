import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const effects = vi.hoisted(() => ({
  directory: vi.fn(() => '/fixture/private-mail'),
  write: vi.fn(),
  remove: vi.fn(),
  build: vi.fn(() => ({ status: 73, stderr: 'compiler fixture failure' })),
  spawn: vi.fn(() => { throw new Error('configured helper reached'); }),
}));

vi.mock('node:fs', async (original) => ({
  ...await original<typeof import('node:fs')>(),
  mkdtempSync: effects.directory,
  writeFileSync: effects.write,
  rmSync: effects.remove,
}));
vi.mock('node:child_process', () => ({ spawnSync: effects.build, spawn: effects.spawn }));

import { startMailSink } from './mail.ts';

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubEnv('CI', undefined);
  vi.stubEnv('HIKYO_E2E_MAIL_BINARY', undefined);
});
afterEach(() => { vi.unstubAllEnvs(); });

describe('browser mail binary contract', () => {
  it.each([
    { ci: 'true', binary: undefined },
    { ci: 'true', binary: '' },
    { ci: '', binary: undefined },
    { ci: 'false', binary: '' },
  ])('rejects CI=$ci with binary=$binary before filesystem or process effects', async ({ ci, binary }) => {
    vi.stubEnv('CI', ci);
    vi.stubEnv('HIKYO_E2E_MAIL_BINARY', binary);

    await expect(startMailSink()).rejects.toThrow('CI browser mail requires HIKYO_E2E_MAIL_BINARY');

    expect(effects.directory).not.toHaveBeenCalled();
    expect(effects.write).not.toHaveBeenCalled();
    expect(effects.remove).not.toHaveBeenCalled();
    expect(effects.build).not.toHaveBeenCalled();
    expect(effects.spawn).not.toHaveBeenCalled();
  });

  it('retains the local build fallback and its build-failure cleanup', async () => {
    await expect(startMailSink()).rejects.toThrow('mail sink build failed: compiler fixture failure');

    expect(effects.build).toHaveBeenCalledWith('go', [
      'build', '-o', '/fixture/private-mail/mail-sink', './internal/mailtest/cmd',
    ], expect.objectContaining({ encoding: 'utf8' }));
    expect(effects.remove).toHaveBeenCalledWith('/fixture/private-mail', { recursive: true, force: true });
    expect(effects.spawn).not.toHaveBeenCalled();
  });

  it.each([undefined, 'true'])('uses the explicit binary without building with CI=$0', async (ci) => {
    vi.stubEnv('CI', ci);
    vi.stubEnv('HIKYO_E2E_MAIL_BINARY', '/fixture/prebuilt/mail-sink');

    await expect(startMailSink()).rejects.toThrow('configured helper reached');

    expect(effects.build).not.toHaveBeenCalled();
    expect(effects.spawn).toHaveBeenCalledWith('/fixture/prebuilt/mail-sink', [], expect.objectContaining({
      stdio: ['pipe', 'pipe', 'pipe'],
    }));
  });
});
