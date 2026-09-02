import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  createProviderRefusalText,
  deleteProviderRefusalText,
  leaseActionRefusalText,
  leaseMintFailureText,
  leaseMintRefusalText,
  revokeCredentialRefusalText,
  setCredentialRefusalText,
} from './dynamic.ts';

describe('createProviderRefusalText', () => {
  it('reads a 400 as unreachable-or-refused and carries the safe detail', () => {
    const text = createProviderRefusalText(
      new ApiError(400, 'bad request', 'dynamic provider unreachable or credential refused'),
    );
    expect(text).toContain('could not be reached');
    expect(text).toContain('(dynamic provider unreachable or credential refused)');
    expect(text).toContain('Nothing was created.');
  });

  it('names manage-identities on a 403', () => {
    expect(createProviderRefusalText(new ApiError(403, 'forbidden'))).toContain('manage-identities');
  });
});

describe('setCredentialRefusalText', () => {
  it('says the stored credential is unchanged on a 400', () => {
    const text = setCredentialRefusalText(new ApiError(400, 'bad request'));
    expect(text).toContain('unchanged');
  });
});

describe('revokeCredentialRefusalText', () => {
  it('names the manage-identities requirement on a 403', () => {
    expect(revokeCredentialRefusalText(new ApiError(403, 'forbidden'))).toContain(
      'manage-identities',
    );
  });
});

describe('deleteProviderRefusalText', () => {
  it('reads a 409 as the live-leases cascade guard', () => {
    const text = deleteProviderRefusalText(new ApiError(409, 'conflict'));
    expect(text).toContain('live leases');
    expect(text).toContain('cascade');
  });
});

describe('leaseMintRefusalText', () => {
  it('explains both the human and machine gates on a 403', () => {
    const text = leaseMintRefusalText(new ApiError(403, 'forbidden'));
    expect(text).toContain('reauthentication');
    expect(text).toContain('machine-reveal');
  });

  it('reports a dismissed passkey without inventing a server cause', () => {
    const dismissed = new Error('dismissed');
    dismissed.name = 'NotAllowedError';
    expect(leaseMintRefusalText(dismissed)).toContain('dismissed');
  });
});

describe('leaseMintFailureText', () => {
  it('stays plain for a pre-commit refusal (nothing was disclosed)', () => {
    const text = leaseMintFailureText(new ApiError(409, 'conflict'));
    expect(text).not.toContain('may still have been minted');
  });

  it('adds the may-have-minted honesty for an ambiguous outcome', () => {
    const text = leaseMintFailureText(new ApiError(500, 'server error'));
    expect(text).toContain('may still have been minted');
    expect(text).toContain('not recoverable');
  });
});

describe('leaseActionRefusalText', () => {
  it('tells a renew 409 the lease is not active', () => {
    expect(leaseActionRefusalText('renew', new ApiError(409, 'conflict'))).toContain(
      'not active',
    );
  });

  it('tells a settle 409 there is nothing to reconcile', () => {
    expect(leaseActionRefusalText('settle', new ApiError(409, 'conflict'))).toContain(
      'not awaiting reconcile',
    );
  });

  it('names the read re-check on a renew 403', () => {
    expect(leaseActionRefusalText('renew', new ApiError(403, 'forbidden'))).toContain('read');
  });

  it('conjugates the fallthrough verb correctly', () => {
    expect(leaseActionRefusalText('settle', new ApiError(500, 'server error'))).toContain(
      'settled',
    );
    expect(leaseActionRefusalText('revoke', new ApiError(500, 'server error'))).toContain(
      'revoked',
    );
  });
});
