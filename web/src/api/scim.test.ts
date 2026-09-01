import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import { scimMintFailureText, scimMutationFailureText, scimReadFailureText } from './scim.ts';

const err = (status: number, detail?: string) => new ApiError(status, `status ${status}`, detail);

describe('scimReadFailureText', () => {
  it('names the second-factor refusal on 403 and the uniform absence on 404', () => {
    expect(scimReadFailureText(err(403))).toMatch(/second factor/i);
    expect(scimReadFailureText(err(404))).toMatch(/the same answer/i);
  });

  it('reports an unknown server failure with its status, and a non-ApiError as unreachable', () => {
    expect(scimReadFailureText(err(500))).toMatch(/500/);
    expect(scimReadFailureText(new Error('boom'))).toMatch(/could not be reached/i);
  });
});

describe('scimMutationFailureText', () => {
  it('prefers a caller-safe detail on 400 and 409, falling back when absent', () => {
    expect(scimMutationFailureText(err(400, 'template not admitted at this scope'))).toBe(
      'template not admitted at this scope',
    );
    expect(scimMutationFailureText(err(409))).toMatch(/conflicts/i);
  });

  it('shares the second-factor and uniform-absence answers with reads', () => {
    expect(scimMutationFailureText(err(403))).toMatch(/second factor/i);
    expect(scimMutationFailureText(err(404))).toMatch(/the same answer/i);
  });

  it('is honest that an unknown server failure leaves the outcome unknown', () => {
    expect(scimMutationFailureText(err(500))).toMatch(/unknown/i);
  });
});

describe('scimMintFailureText', () => {
  it('says no credential was issued on a 400 proof/opt-in refusal', () => {
    expect(scimMintFailureText(err(400))).toMatch(/no credential was issued/i);
    expect(scimMintFailureText(err(400, 'that code was not accepted'))).toBe(
      'that code was not accepted',
    );
  });

  it('routes 403 to the second-factor answer and 500 to no-credential-issued', () => {
    expect(scimMintFailureText(err(403))).toMatch(/second factor/i);
    expect(scimMintFailureText(err(500))).toMatch(/no credential was issued/i);
  });
});
