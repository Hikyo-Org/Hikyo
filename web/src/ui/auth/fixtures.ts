import type { LoginProvider } from './ProviderButton.tsx';
import type { SetupStep } from './SecondFactorSetup.tsx';

/** Enrolment fixtures shared by the setup-step and login-flow stories. */
export const totpStep: SetupStep = {
  kind: 'totp',
  otpauthUrl: 'otpauth://totp/Hikyo:alex?secret=JBSWY3DPEHPK3PXP&issuer=Hikyo&digits=6&period=30',
  secret: 'JBSW Y3DP EHPK 3PXP',
};

export const codesStep: SetupStep = {
  kind: 'codes',
  codes: ['k7m2-p9qa-x4nd', 'b3rt-w8vc-h6ls', 'z2qe-n5my-t9kf', 'g4hd-c7xp-r1wb', 'v8ln-s3kq-m6ze', 'p5tc-y2rf-d9nh'],
};

/**
 * The instance-enabled providers the prototype (social-signin/2) draws:
 * Google, one Entra row per tenant (#588 d1), GitHub, and a generic OIDC row.
 * Google's and Entra's `brand` are what `GET /auth/methods` derives from the
 * pinned issuer (#607); GitHub's is story-side until the OAuth2 kind (#609).
 */
export const google: LoginProvider = { slug: 'google', display_name: 'Google', kind: 'oidc', brand: 'google' };
export const contoso: LoginProvider = { slug: 'contoso', display_name: 'Contoso', kind: 'oidc', brand: 'microsoft' };
export const fabrikam: LoginProvider = { slug: 'fabrikam', display_name: 'Fabrikam', kind: 'oidc', brand: 'microsoft' };
export const github: LoginProvider = { slug: 'github', display_name: 'GitHub', kind: 'oauth2', brand: 'github' };
export const corp: LoginProvider = { slug: 'corp', display_name: 'Corp SSO', kind: 'oidc' };
export const socialProviders: readonly LoginProvider[] = [google, contoso, fabrikam, github, corp];
