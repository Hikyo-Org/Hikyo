import { samlStartOp } from '@hikyo/operations';
import { useState, type FormEvent } from 'react';
import { Link } from 'react-router';

import { parsed } from '../api/client.ts';
import { useSensitiveMutation, useSensitiveState } from '../api/sensitiveMutation.ts';
import { useAuthMethods } from '../api/account.ts';
import { loginFailureText, useLogin, useOIDCLogin } from '../api/session.ts';
import { passkeysAvailable, stepUpFailureText, usePasskeyLogin } from '../api/stepup.ts';
import { surfaceById } from '../app/navigation.ts';
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
  const [username, setUsername] = useState('');
  const [password, setPassword] = useSensitiveState('');
  // The provider being contacted, so only ITS button shows the busy label.
  const [contacting, setContacting] = useState<string | null>(null);
  const providerPending = oidc.isPending || saml.isPending;
  const busy = login.isPending || passkey.isPending || providerPending;

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    login.mutate({ username, password });
    setPassword('');
  };

  return (
    <main className="login">
      <form className="login__card" onSubmit={onSubmit} noValidate>
        <h1 className="login__title">Sign in to Hikyo</h1>
        <p className="login__lede">Use the credential you established with your setup authority.</p>

        {login.isError ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>{loginFailureText(login.error)}</span>
          </p>
        ) : null}

        <div className="field">
          <label htmlFor="username">Username</label>
          <input
            id="username"
            name="username"
            autoComplete="username"
            required
            disabled={busy}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>

        <div className="field">
          <label htmlFor="password">Password</label>
          <input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            required
            disabled={busy}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>

        <button className="btn btn--primary" type="submit" disabled={busy}>
          {login.isPending ? 'Signing in…' : 'Sign in'}
        </button>
        {passkeysAvailable() ? (
          <>
            <p className="login__or" aria-hidden="true">
              or
            </p>
            <button
              className="btn"
              type="button"
              onClick={() => passkey.mutate()}
              disabled={busy}
            >
              {passkey.isPending ? 'Waiting for the passkey…' : 'Use a passkey instead'}
            </button>
            {passkey.isError ? (
              <p className="alert" role="alert">
                <span className="alert__glyph" aria-hidden="true">
                  !
                </span>
                <span>{stepUpFailureText(passkey.error)}</span>
              </p>
            ) : null}
          </>
        ) : null}
        {methods.isPending ? <p role="status">Loading sign-in methods…</p> : null}
        {methods.data?.providers
          .filter((provider) => provider.kind === 'oidc' || provider.kind === 'saml')
          .map((provider) => (
            <button
              className="btn"
              type="button"
              key={provider.slug}
              onClick={() => {
                setContacting(provider.slug);
                if (provider.kind === 'saml') saml.mutate(provider.slug);
                else oidc.mutate(provider.slug);
              }}
              disabled={busy}
            >
              {providerPending && contacting === provider.slug
                ? 'Contacting identity provider…'
                : `Continue with ${provider.display_name}`}
            </button>
          ))}
        {methods.isError ? (
          <ProviderDiscoveryAlert onRetry={() => void methods.refetch()} />
        ) : null}
        {oidc.isError || saml.isError ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{loginFailureText(oidc.isError ? oidc.error : saml.error)}</span>
          </p>
        ) : null}
        {/* Quiet links, demoted from buttons: the CSS keeps them on the 44px
            touch floor (#567) without reading as a third way to sign in. */}
        <p className="login__links">
          <Link to={surfaceById('establish-credential').path}>
            Have a setup authority? Establish your credential
          </Link>
          <Link to={`${surfaceById('establish-credential').path}?mode=recover`}>
            Lost your second factor? Recover with a code
          </Link>
        </p>
      </form>
    </main>
  );
}
