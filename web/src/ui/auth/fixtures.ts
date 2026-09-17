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
