import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  inactiveText,
  policyView,
  preconditionProvider,
  preconditionText,
  registrationFailureText,
  requirementLine,
  resaveBody,
  signupLink,
  type RegistrationPolicy,
} from './registration.ts';

const wire: RegistrationPolicy = {
  id: 'rpol_1',
  authority_principal_id: 'prn_alex',
  external: [
    { provider: { kind: 'oidc', slug: 'corp' }, display_name: 'Corporate IdP', claim: 'hd', values: ['acme.example'] },
    { provider: { kind: 'oidc', slug: 'plain' } },
  ],
  local: { domains: ['acme.example'] },
  landing: { kind: 'fresh-org', cap: 3 },
  fresh_org_count: 1,
  state: 'inactive',
  inactive_cause: 'precondition',
  inactive_precondition: 'mailer-unconfigured',
  row_version: 2,
  created_at: '2026-09-20T08:00:00Z',
  updated_at: '2026-09-20T08:00:00Z',
};

describe('registration policy helpers', () => {
  it('parses the wire couplings into a view and re-saves exactly what stands', () => {
    const view = policyView(wire);
    expect(view.mintedOrgs).toBe(1);
    expect(view.external[0]?.allow).toEqual({ claim: 'hd', values: ['acme.example'] });
    expect(view.external[1]?.name).toBe('plain');
    expect(resaveBody(view, '123456')).toEqual({
      external: [
        { provider: { kind: 'oidc', slug: 'corp' }, claim: 'hd', values: ['acme.example'] },
        { provider: { kind: 'oidc', slug: 'plain' } },
      ],
      local: { domains: ['acme.example'] },
      landing: { kind: 'fresh-org', cap: 3 },
      proof: '123456',
    });
  });

  it('refuses a response that breaks a coupling instead of defaulting it', () => {
    const { fresh_org_count: _count, ...noCount } = wire;
    expect(() => policyView(noCount)).toThrow('live org count');
    const { inactive_cause: _cause, ...noCause } = wire;
    expect(() => policyView(noCause)).toThrow('names its cause');
    const { inactive_precondition: _precondition, ...noPrecondition } = wire;
    expect(() => policyView(noPrecondition)).toThrow('a precondition cause its precondition');
    expect(() => policyView({ ...wire, landing: { kind: 'org-template' } })).toThrow();
    expect(() => policyView({ ...wire, external: [{ provider: { kind: 'oidc', slug: 'x' }, claim: 'hd' }] })).toThrow(
      'arrive together',
    );
    expect(() => policyView({ ...wire, external: [{ provider: { kind: 'saml', slug: 'x' } }] })).toThrow();
  });

  it('names every precondition and the provider row', () => {
    expect(preconditionProvider('provider-disabled: oidc:corp')).toBe('oidc:corp');
    expect(preconditionProvider('mailer-unconfigured')).toBeNull();
    expect(preconditionText('no-public-origin')).toContain('HIKYO_EXTERNAL_ORIGIN');
    expect(preconditionText('mailer-unconfigured')).toContain('HIKYO_MAIL_');
    expect(preconditionText('cap-zero')).toBe('A cap of at least 1 is required for this landing.');
    expect(preconditionText('template-not-org-applicable')).toContain('organisation scope');
    expect(preconditionText('provider-missing-email-scope: oidc:corp')).toContain('corp: this provider row does not request the email scope');
    expect(preconditionText('provider-disabled: oidc:corp')).toContain('corp is disabled');
    expect(preconditionText('provider-missing: oidc:idp_gone')).toContain('(idp_gone) no longer exists');
    expect(preconditionText('provider-kind-unsupported: oauth2:github')).toContain('oauth2:github: this provider kind cannot admit');
    expect(registrationFailureText(new ApiError(400, 'bad_request', 'cap-zero'))).toBe(
      'A cap of at least 1 is required for this landing.',
    );
  });

  it('voices the inactive cause on the panel, never on the public page', () => {
    expect(inactiveText({ state: 'inactive', inactive_cause: 'precondition', inactive_precondition: 'mailer-unconfigured' }, 'Alex')).toContain(
      'The email entry needs a configured mailer',
    );
    expect(inactiveText({ state: 'inactive', inactive_cause: 'authority-lost' }, 'Alex')).toContain('Alex no longer holds');
    expect(inactiveText({ state: 'inactive', inactive_cause: 'authority-unassigned' }, 'Alex')).toContain('no authority yet');
  });

  it('states one requirement per method kind, none borrowed from another', () => {
    expect(requirementLine('oidc')).toContain('email_verified');
    expect(requirementLine('oauth2')).not.toContain('email_verified');
    expect(requirementLine('oauth2')).not.toContain('GitHub');
    expect(requirementLine('local')).toContain('mailed link');
  });

  it('builds the org sign-up link on the instance origin', () => {
    expect(signupLink('https://hikyo.example', 'org_acme')).toBe('https://hikyo.example/signup?org=org_acme');
  });
});
