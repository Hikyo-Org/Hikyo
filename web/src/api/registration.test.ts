import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  inactiveText,
  preconditionProvider,
  preconditionText,
  registrationFailureText,
  resaveBody,
  signupLink,
  type RegistrationPolicy,
} from './registration.ts';

const policy: RegistrationPolicy = {
  id: 'rpol_1',
  authority_principal_id: 'prn_alex',
  external: [
    { provider: { kind: 'oidc', slug: 'corp' }, display_name: 'Corporate IdP', claim: 'hd', values: ['acme.example'] },
    { provider: { kind: 'oidc', slug: 'plain' } },
  ],
  local: { domains: ['acme.example'] },
  landing: { kind: 'fresh-org', cap: 3 },
  state: 'inactive',
  inactive_cause: 'precondition',
  inactive_precondition: 'mailer-unconfigured',
  row_version: 2,
  created_at: '2026-09-20T08:00:00Z',
  updated_at: '2026-09-20T08:00:00Z',
};

describe('registration policy helpers', () => {
  it('re-saves exactly what stands, dropping the response-only members', () => {
    expect(resaveBody(policy)).toEqual({
      external: [
        { provider: { kind: 'oidc', slug: 'corp' }, claim: 'hd', values: ['acme.example'] },
        { provider: { kind: 'oidc', slug: 'plain' } },
      ],
      local: { domains: ['acme.example'] },
      landing: { kind: 'fresh-org', cap: 3 },
    });
  });

  it('names every write-time precondition and the provider row', () => {
    expect(preconditionProvider('provider-disabled: oidc:corp')).toBe('oidc:corp');
    expect(preconditionProvider('mailer-unconfigured')).toBeNull();
    expect(preconditionText('no-public-origin')).toContain('HIKYO_EXTERNAL_ORIGIN');
    expect(preconditionText('mailer-unconfigured')).toContain('HIKYO_MAIL_');
    expect(preconditionText('cap-zero')).toBe('A cap of at least 1 is required for this landing.');
    expect(preconditionText('template-not-org-applicable')).toContain('organisation scope');
    expect(preconditionText('provider-missing-email-scope: oidc:corp')).toContain('corp: this provider row does not request the email scope');
    expect(preconditionText('provider-disabled: oidc:corp')).toContain('corp is disabled');
    expect(registrationFailureText(new ApiError(400, 'bad_request', 'cap-zero'))).toBe(
      'A cap of at least 1 is required for this landing.',
    );
  });

  it('voices the inactive cause on the panel, never on the public page', () => {
    expect(inactiveText(policy, 'Alex')).toContain('The email entry needs a configured mailer');
    expect(inactiveText({ ...policy, inactive_cause: 'authority-lost' }, 'Alex')).toContain('Alex no longer holds');
    expect(inactiveText({ ...policy, inactive_cause: 'authority-unassigned' }, 'Alex')).toContain('no authority yet');
  });

  it('builds the org sign-up link on the instance origin', () => {
    expect(signupLink('https://hikyo.example', 'org_acme')).toBe('https://hikyo.example/signup?org=org_acme');
  });
});
