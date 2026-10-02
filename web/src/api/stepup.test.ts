import { expect, test } from 'vitest';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { hasSecondFactor } from './stepup.ts';

test.each([
  { factors: ['password'], adequate: false },
  { factors: ['password', 'password'], adequate: false },
  { factors: ['totp'], adequate: false },
  { factors: ['password', 'totp'], adequate: true },
  { factors: ['webauthn'], adequate: true },
  { factors: ['oidc'], adequate: false },
  { factors: ['oidc', 'oidc-mfa'], adequate: true },
  { factors: ['saml'], adequate: false },
  { factors: ['saml', 'saml-mfa'], adequate: true },
])('assurance factors $factors match server adequacy $adequate', ({ factors, adequate }) => {
  expect(hasSecondFactor({
    ...authenticatedIdentity,
    session: {
      ...authenticatedIdentity.session,
      assurance: { ...authenticatedIdentity.session.assurance, factors },
    },
  })).toBe(adequate);
});
