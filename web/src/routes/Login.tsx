import { samlStartOp } from '@hikyo/operations';
import { useState } from 'react';
import { Link } from 'react-router';

import { useAuthMethods } from '../api/account.ts';
import { ApiError, parsed } from '../api/client.ts';
import { useSensitiveMutation } from '../api/sensitiveMutation.ts';
import { loginFailureText, useLogin, useLoginChallengeTotp, useOIDCLogin } from '../api/session.ts';
import {
  passkeyFailureText,
  passkeysAvailable,
  stepUpFailureText,
  useLoginChallengePasskey,
  usePasskeyLogin,
} from '../api/stepup.ts';
import { surfaceById } from '../app/navigation.ts';
import { LoginForm, type SignInBusy } from '../ui/auth/LoginForm.tsx';
import { SecondFactorChallenge } from '../ui/auth/SecondFactorChallenge.tsx';
import { ProviderDiscoveryAlert } from './ProviderDiscoveryAlert.tsx';

/**
 * The local password login page.
 *
 * Local credentials and every configured OIDC provider establish the same
 * browser-session artifact. The provider callback returns through OIDCDone.
 *
 * Refusal presentation follows the locked rule that no state is carried by
 * colour alone: the message is text, it is announced through `role="alert"`,
 * and it carries a glyph. The wording never distinguishes an unknown account
 * from a wrong password, because the server deliberately does not either.
 *
 * The card itself is ui/auth/LoginForm: this route owns the transport (the
 * four mutations and the discovery query) and hands the atom the three ways
 * in as callbacks. Provider discovery, which is about the page rather than
 * the credential, stays outside the card.
 */
/** A SAML provider's login leg: the same artifact as OIDC, over the SP-initiated redirect. */
function useSAMLLogin() {
  return useSensitiveMutation({
    mutationFn: (provider: string) =>
      parsed(samlStartOp, { path: { provider }, body: { purpose: 'login' } }),
    onSuccess: (result) => globalThis.location.assign(result.redirect_url),
  });
}

/**
 * A challenge refusal reads like the factor's own refusal (`otherwise`),
 * except the 404: the challenge is single-use and expiring, so a missing one
 * means this sign-in is over.
 */
function challengeFailureText(error: unknown, otherwise: (error: unknown) => string): string {
  if (error instanceof ApiError && error.status === 404) {
    return 'This sign-in expired or was already completed. Reload the page and sign in again.';
  }
  return otherwise(error);
}

export function Login() {
  const login = useLogin();
  const passkey = usePasskeyLogin();
  const oidc = useOIDCLogin();
  const saml = useSAMLLogin();
  const methods = useAuthMethods();
  // A password login on an account with an enrolled factor answers a challenge,
  // not a session (#760): the route then presents the second factor. The
  // sensitive-mutation surface retains no result, so the challenge is captured
  // from the mutate callback into route state. Either enrolled factor finishes
  // it (#785); the enrolment gate for an account with none is EnrolmentGate.
  const [challenge, setChallenge] = useState<{
    id: string;
    factors: string[];
    username: string;
  } | null>(null);
  const challengeTotp = useLoginChallengeTotp(challenge?.id ?? '');
  const challengePasskey = useLoginChallengePasskey(challenge?.id ?? '');
  // The provider being contacted, so only ITS button shows the busy label.
  const [contacting, setContacting] = useState<string | null>(null);
  // The kind discriminator is open (zIdentityProviderKind is a string), so the
  // card is only offered the two protocols this route can actually start.
  const providers = (methods.data?.providers ?? []).filter(
    (provider) => provider.kind === 'oidc' || provider.kind === 'saml',
  );
  const providerPending = oidc.isPending || saml.isPending;
  // A provider ceremony ends in a redirect or a session change, so every
  // control is barred while one is in flight, even before a slug is known.
  // The empty slug matches no provider: nothing wears a label it did not earn.
  const busy: SignInBusy = login.isPending
    ? 'password'
    : passkey.isPending
      ? 'passkey'
      : providerPending
        ? { provider: contacting ?? '' }
        : null;
  // The card has one refusal slot, and the latest attempt is what the person
  // is waiting on: starting ANY leg retires every leg's refusal, so a stale
  // one cannot outlive the attempt it described or mask a fresh failure. That
  // includes the sibling protocol (a SAML refusal must not sit in the slot
  // while an OIDC attempt runs) and the leg about to start, where the reset is
  // a no-op: mutateAsync bumps the operation counter and sets pending anyway.
  const retireEveryLeg = () => {
    login.reset();
    passkey.reset();
    oidc.reset();
    saml.reset();
  };
  const error = login.isError
    ? loginFailureText(login.error)
    : passkey.isError
      ? passkeyFailureText(passkey.error)
      : oidc.isError || saml.isError
        ? loginFailureText(oidc.isError ? oidc.error : saml.error)
        : null;

  if (challenge !== null) {
    // One refusal slot again: presenting either factor retires the other's.
    const retireChallengeLegs = () => {
      challengeTotp.reset();
      challengePasskey.reset();
    };
    return (
      <main className="login">
        <SecondFactorChallenge
          username={challenge.username}
          totp={challenge.factors.includes('totp')}
          passkey={challenge.factors.includes('webauthn') && passkeysAvailable()}
          busy={challengeTotp.isPending ? 'code' : challengePasskey.isPending ? 'passkey' : null}
          error={
            challengeTotp.isError
              ? challengeFailureText(challengeTotp.error, stepUpFailureText)
              : challengePasskey.isError
                ? challengeFailureText(challengePasskey.error, passkeyFailureText)
                : null
          }
          onCode={(code) => {
            retireChallengeLegs();
            challengeTotp.mutate(code);
          }}
          onPasskey={() => {
            retireChallengeLegs();
            challengePasskey.mutate();
          }}
        />
      </main>
    );
  }

  return (
    <main className="login">
      <LoginForm
        providers={providers}
        passkeys={passkeysAvailable()}
        busy={busy}
        error={error}
        onPassword={(credentials) => {
          retireEveryLeg();
          login.mutate(credentials, {
            onSuccess: (outcome) => {
              if (outcome.kind === 'challenge') {
                setChallenge({
                  id: outcome.challenge.challenge_id,
                  factors: outcome.challenge.factors,
                  username: outcome.username,
                });
              }
            },
          });
        }}
        onPasskey={() => {
          retireEveryLeg();
          passkey.mutate();
        }}
        onProvider={(slug) => {
          retireEveryLeg();
          setContacting(slug);
          const provider = providers.find((candidate) => candidate.slug === slug);
          if (provider?.kind === 'saml') saml.mutate(slug);
          else oidc.mutate(slug);
        }}
        /* Quiet links, demoted from buttons: the CSS keeps them on the 44px
           touch floor (#567) without reading as a third way to sign in. */
        links={
          <>
            <Link to={surfaceById('establish-credential').path}>
              Have a setup authority? Establish your credential
            </Link>
            <Link to={`${surfaceById('establish-credential').path}?mode=recover`}>
              Lost your second factor? Recover with a code
            </Link>
          </>
        }
      />
      {/* An inactive registration policy (#606): the public page says only
          this, never the cause, which renders on the Members panel (#587 d3).
          The sign-up door itself lands with #607. */}
      {methods.data?.signup_paused === true ? (
        <p className="login__paused" role="status">
          Sign-up is paused.
        </p>
      ) : null}
      {methods.isPending ? <p role="status">Loading sign-in methods…</p> : null}
      {methods.isError ? <ProviderDiscoveryAlert onRetry={() => void methods.refetch()} /> : null}
    </main>
  );
}
