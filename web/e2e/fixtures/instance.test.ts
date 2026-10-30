import { describe, expect, it, vi } from 'vitest';

import {
  fixtureEnvironment,
  idpIssuerMatchesRequestedPort,
  persistSharedPasskey,
  refreshSharedSessionFromProbe,
  type VirtualCredential,
} from './instance.ts';

describe('security fixture child environment', () => {
  const ambient = {
    PATH: '/fixture/toolchain/bin', HOME: '/fixture/host-home', TMPDIR: '/fixture/host-temp',
    GH_TOKEN: 'hostile-github-fixture-token', AWS_SECRET_ACCESS_KEY: 'hostile-provider-fixture-token',
    HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE: '500', HIKYO_DEV_SERVICE_BUDGETS_DISABLED: 'true',
    HIKYO_DEV_ADAPTER_FAKE_PROVIDER: 'true', HIKYO_SECOND_FACTOR: 'optional',
    HIKYO_ROOT_KEY: 'hostile-root-key-fixture', HIKYO_DB: 'hostile-datastore-fixture',
  };

  it('drops ambient credentials and policy overrides, and owns HOME and temporary storage', () => {
    expect(fixtureEnvironment('security', '/fixture/owned', 'http://localhost:28789', ambient)).toEqual({
      PATH: '/fixture/toolchain/bin', HOME: '/fixture/owned', TMPDIR: '/fixture/owned',
      XDG_CACHE_HOME: '/fixture/owned/cache', HIKYO_EXTERNAL_ORIGIN: 'http://localhost:28789',
    });
  });

  it('preserves existing flow defaults and ambient inheritance only for the flow profile', () => {
    expect(fixtureEnvironment('flows', '/fixture/owned', 'http://localhost:28789', ambient)).toEqual({
      ...ambient, HIKYO_EXTERNAL_ORIGIN: 'http://localhost:28789',
      HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE: '2000',
    });
  });

  it('does not inherit the flow source allowance into the real-limit security profile', () => {
    const flows = fixtureEnvironment('flows', '/fixture/flow', 'http://localhost:28789', ambient);
    expect(fixtureEnvironment('security', '/fixture/security', 'http://localhost:28789', flows))
      .not.toHaveProperty('HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE');
  });
});

describe('fake IdP issuer binding', () => {
  it('accepts the actual loopback port when the OS assigns it', () => {
    expect(idpIssuerMatchesRequestedPort('http://127.0.0.1:54321', 0)).toBe(true);
  });

  it('requires an explicitly requested port to match', () => {
    expect(idpIssuerMatchesRequestedPort('http://127.0.0.1:45792', 45792)).toBe(true);
    expect(idpIssuerMatchesRequestedPort('http://127.0.0.1:45793', 45792)).toBe(false);
  });

  it('rejects malformed, non-loopback, and unbound issuers', () => {
    expect(idpIssuerMatchesRequestedPort('not a URL', 0)).toBe(false);
    expect(idpIssuerMatchesRequestedPort('http://localhost:54321', 0)).toBe(false);
    expect(idpIssuerMatchesRequestedPort('http://127.0.0.1:0', 0)).toBe(false);
  });
});

const credential: VirtualCredential = {
  credentialId: 'shared-passkey',
  isResidentCredential: true,
  privateKey: 'private-key',
  rpId: 'localhost',
  signCount: 1,
  userHandle: 'user',
};

describe('shared passkey persistence', () => {
  it('throws when the authenticator no longer holds the shared credential', () => {
    expect(() => persistSharedPasskey([], credential)).toThrow(
      'the shared virtual authenticator lost its passkey credential',
    );
  });
});

describe('shared session refresh decision', () => {
  it('does not re-mint a live file-backed session', async () => {
    const remint = vi.fn();

    await refreshSharedSessionFromProbe(async () => 200, remint);

    expect(remint).not.toHaveBeenCalled();
  });

  it('re-mints only when the file-backed session is unauthenticated', async () => {
    const remint = vi.fn();

    await refreshSharedSessionFromProbe(async () => 401, remint);

    expect(remint).toHaveBeenCalledOnce();
  });

  it('fails loud when the probe itself fails', async () => {
    const remint = vi.fn();

    await expect(refreshSharedSessionFromProbe(async () => 500, remint)).rejects.toThrow(
      'shared session probe answered 500',
    );
    expect(remint).not.toHaveBeenCalled();
  });
});
