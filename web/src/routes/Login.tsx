import { samlStartOp } from '@hikyo/operations';
import { useState } from 'react';
import { Link } from 'react-router';

import { useAuthMethods } from '../api/account.ts';
import { parsed } from '../api/client.ts';
import { useSensitiveMutation } from '../api/sensitiveMutation.ts';
import { loginFailureText, useLogin, useOIDCLogin } from '../api/session.ts';
import { passkeysAvailable, stepUpFailureText, usePasskeyLogin } from '../api/stepup.ts';
import { surfaceById } from '../app/navigation.ts';
import { LoginForm, type SignInBusy } from '../ui/auth/LoginForm.tsx';
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

export function Login() {
  const login = useLogin();
  const passkey = usePasskeyLogin();
  const oidc = useOIDCLogin();
  const saml = useSAMLLogin();
  const methods = useAuthMethods();
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
  const error = login.isError
    ? loginFailureText(login.error)
    : passkey.isError
      ? stepUpFailureText(passkey.error)
      : oidc.isError || saml.isError
        ? loginFailureText(oidc.isError ? oidc.error : saml.error)
        : null;

  return (
    <main className="login">
      <LoginForm
        providers={providers}
        passkeys={passkeysAvailable()}
        busy={busy}
        error={error}
        onPassword={(credentials) => login.mutate(credentials)}
        onPasskey={() => passkey.mutate()}
        onProvider={(slug) => {
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
      {methods.isPending ? <p role="status">Loading sign-in methods…</p> : null}
      {methods.isError ? <ProviderDiscoveryAlert onRetry={() => void methods.refetch()} /> : null}
    </main>
  );
}
