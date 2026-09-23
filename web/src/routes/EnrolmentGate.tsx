import { useState } from 'react';

import {
  accountFailureText,
  useConfirmTotp,
  useEnrolmentGateCodes,
  useEnrolPasskey,
  useEnrolTotpStart,
} from '../api/account.ts';
import { ApiError } from '../api/client.ts';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { useLogout } from '../api/session.ts';
import { passkeysAvailable, stepUpFailureText } from '../api/stepup.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Button } from '../ui/Button.tsx';
import { SecondFactorSetup, type SetupStep } from '../ui/auth/SecondFactorSetup.tsx';

/**
 * The sign-in enrolment gate (#785): what `/login` renders, and every other
 * path redirects to, while `whoami` reports `enrolment_required`. The server
 * already refuses everything but enrolment, recovery codes, whoami and logout
 * for such a session (#760); this is the SPA rendering of that confinement.
 *
 * The gate ends by itself: enrolling a factor reissues the session, the
 * reissued session is minted without the flag, and the root router stops
 * rendering this route. Nothing here decides the gate is over.
 *
 * Sign-out is offered beside the card. It is not a way past the gate (the next
 * sign-in lands here again), only a way out for someone without their device.
 */
export function EnrolmentGate() {
  const auth = useAuth();
  const codes = useEnrolmentGateCodes();
  const totpStart = useEnrolTotpStart();
  const totpConfirm = useConfirmTotp();
  const passkey = useEnrolPasskey();
  const logout = useLogout();
  const [stored, setStored] = useState(false);
  const [enrolment, setEnrolment] = useSensitiveState<{ uri: string; secret: string } | null>(null);

  const held = codes.held;
  const step: SetupStep =
    held === null
      ? { kind: 'password' }
      : !stored
        ? { kind: 'codes', codes: held.codes }
        : enrolment === null
          ? { kind: 'choose' }
          : { kind: 'totp', otpauthUrl: enrolment.uri, secret: enrolment.secret };

  const busy = codes.isPending
    ? 'password'
    : totpStart.isPending
      ? 'totp'
      : passkey.isPending
        ? 'passkey'
        : totpConfirm.isPending
          ? 'code'
          : null;

  const error =
    step.kind === 'password' && codes.error !== null
      ? passwordFailureText(codes.error)
      : step.kind === 'choose' && totpStart.isError
        ? accountFailureText(totpStart.error)
        : step.kind === 'choose' && passkey.isError
          ? accountFailureText(passkey.error)
          : step.kind === 'totp' && totpConfirm.isError
            ? stepUpFailureText(totpConfirm.error)
            : null;

  return (
    <main className="login">
      <SecondFactorSetup
        // whoami carries no username; the display name is what the chrome shows.
        username={auth.identity?.principal.display_name ?? 'your account'}
        step={step}
        passkeys={passkeysAvailable()}
        busy={busy}
        error={error}
        onPassword={(password) => {
          setStored(false);
          codes.issue(password);
        }}
        onCodesStored={() => setStored(true)}
        onChooseTotp={() => {
          if (held === null) return;
          passkey.reset();
          totpStart.mutate(
            { password: held.password },
            {
              onSuccess: (result) =>
                setEnrolment({ uri: result.otpauth_uri, secret: otpauthSecret(result.otpauth_uri) }),
            },
          );
        }}
        onChoosePasskey={() => {
          if (held === null) return;
          totpStart.reset();
          passkey.mutate({ proof: { kind: 'password', password: held.password } });
        }}
        onConfirmCode={(code) => totpConfirm.mutate({ code })}
      />
      <Button type="button" disabled={logout.isPending} onClick={() => logout.mutate()}>
        {logout.isPending ? 'Signing out…' : 'Sign out'}
      </Button>
    </main>
  );
}

/** The gate's password step is a proof, so a 401 names the password alone. */
function passwordFailureText(error: Error): string {
  if (error instanceof ApiError && error.status === 401) {
    return 'That password was not accepted. Check it and try again.';
  }
  return accountFailureText(error);
}

/**
 * The manual-entry key is the `secret` parameter of the provisioning URI the
 * server returned; a URI without one is a contract regression, said loudly.
 */
function otpauthSecret(uri: string): string {
  const secret = new URL(uri).searchParams.get('secret');
  if (secret === null || secret === '') {
    throw new Error('the authenticator provisioning URI carried no secret');
  }
  return secret;
}
