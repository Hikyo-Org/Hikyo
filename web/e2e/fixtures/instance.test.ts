import { describe, expect, it, vi } from 'vitest';

import {
  idpIssuerMatchesRequestedPort,
  persistSharedPasskey,
  refreshSharedSessionFromProbe,
  type VirtualCredential,
} from './instance.ts';

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
