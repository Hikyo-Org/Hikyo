import { useEffect, useId, useState, type FormEvent } from 'react';
import { Link, useSearchParams } from 'react-router';

import { useAuthMethods } from '../api/account.ts';
import { useSensitiveMutation, useSensitiveState } from '../api/sensitiveMutation.ts';
import {
  beginRecovery,
  establishCredential,
  establishFailureText,
  recoveryFailureText,
  useClaimStart,
} from '../api/session.ts';
import { surfaceById } from '../app/navigation.ts';
import { Alert } from '../ui/Alert.tsx';
import { ProviderButton, type LoginProvider } from '../ui/auth/ProviderButton.tsx';
import { Button } from '../ui/Button.tsx';

/**
 * The public credential-establishment page (#568, registry surface
 * `establish-credential`).
 *
 * Where a display-once authority, from an invitation, a credential reset,
 * bootstrap or break-glass, becomes a first credential. Chromeless and
 * sessionless like login: the holder has no session yet. The authority comes
 * first; then either the password form (a 204 that establishes no session:
 * they sign in afterwards like anyone else) or one button per enabled OIDC or
 * OAuth2 provider, which claims the invitation by that identity (#610): the
 * provider round-trip spends the authority, binds the identity and signs
 * them in. The login page never offers a claim.
 *
 * Refusals are one sentence on purpose, whichever path refused. The server
 * answers an expired, spent, unknown, malformed or recovery-issued authority,
 * an account that already holds a credential and an identity bound elsewhere
 * uniformly, and this page keeps that oracle closed rather than reopening it
 * with helpful wording.
 *
 * `?mode=recover` (#571) is the lost-second-factor entry: username plus one
 * recovery code spend for an authority, which is handed straight into this
 * same form. The mode is a query parameter because it is navigation, not
 * state; the authority itself is component state only.
 */
export function EstablishCredential() {
  const [search, setSearch] = useSearchParams();
  const recovering = search.get('mode') === 'recover';
  const establish = useSensitiveMutation({
    mutationFn: (input: { authority: string; password: string }) => establishCredential(input.authority, input.password),
  });
  const [authority, setAuthority] = useSensitiveState('');
  const [password, setPassword] = useSensitiveState('');
  const [repeat, setRepeat] = useSensitiveState('');
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [recovered, setRecovered] = useState(false);
  const authorityId = useId();
  const passwordId = useId();
  const repeatId = useId();
  const claim = useClaimStart();
  const methods = useAuthMethods();
  // A claim binds an OIDC or OAuth2 identity; SAML has no claim purpose.
  const providers = (methods.data?.providers ?? []).filter(
    (provider): provider is LoginProvider & { kind: 'oidc' | 'oauth2' } =>
      provider.kind === 'oidc' || provider.kind === 'oauth2',
  );
  // The row being contacted, so only ITS button shows the busy label.
  const [contacting, setContacting] = useState<string | null>(null);
  const busy = pending || claim.isPending;
  const noAuthority = authority.trim() === '';

  // Leaving for a provider can park this page in the back-forward cache
  // without unmounting it, so the secrets are wiped on a persisted pagehide
  // too: Back from the provider must never restore a live authority.
  useEffect(() => {
    const wipe = (event: PageTransitionEvent) => {
      if (!event.persisted) return;
      setAuthority('');
      setPassword('');
      setRepeat('');
    };
    globalThis.addEventListener('pagehide', wipe);
    return () => globalThis.removeEventListener('pagehide', wipe);
  }, [setAuthority, setPassword, setRepeat]);

  const onClaim = (provider: LoginProvider & { kind: 'oidc' | 'oauth2' }) => {
    setFailure(null);
    if (noAuthority) {
      setFailure('Paste the authority you were handed first.');
      return;
    }
    setContacting(`${provider.kind}:${provider.slug}`);
    claim.mutate(
      {
        provider: provider.slug,
        kind: provider.kind,
        authority: authority.trim(),
        returnTo: surfaceById('settings').path,
        // Cleared before the browser leaves for the provider (#610 review).
        beforeLeave: () => {
          setAuthority('');
          setPassword('');
          setRepeat('');
        },
      },
      {
        // Success leaves the page for the provider; only a refusal returns.
        onError: (error) => {
          setContacting(null);
          setFailure(establishFailureText(error));
        },
      },
    );
  };

  const setMode = (recover: boolean) => {
    const next = new URLSearchParams(search);
    if (recover) next.set('mode', 'recover');
    else next.delete('mode');
    setSearch(next, { replace: true });
    setFailure(null);
    establish.reset();
    setAuthority('');
    setPassword('');
    setRepeat('');
    setRecovered(false);
  };

  const onSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFailure(null);
    if (authority.trim() === '' || password === '') {
      setFailure('Paste the authority you were handed and choose a password.');
      return;
    }
    if (password.length < 12) {
      setFailure('Choose a password of at least 12 characters.');
      return;
    }
    if (password !== repeat) {
      // Decided here, before any request: the server never sees a password
      // the holder did not type twice.
      setFailure('The two passwords differ. Type the same password twice.');
      return;
    }
    setPending(true);
    try {
      await establish.mutateAsync({ authority: authority.trim(), password });
      setAuthority('');
      setPassword('');
      setRepeat('');
      setRecovered(false);
      setDone(true);
    } catch (error) {
      setFailure(establishFailureText(error));
      setPassword('');
      setRepeat('');
    } finally {
      setPending(false);
    }
  };

  if (done) {
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Credential established</h1>
          <p className="login__lede" role="status">
            Sign in with your username and the password you just set. The authority is spent.
          </p>
          <Link className="btn btn--primary" to={surfaceById('login').path}>
            Sign in
          </Link>
        </div>
      </main>
    );
  }

  if (recovering && !recovered) {
    return (
      <RecoveryForm
        onRecovered={(issued) => {
          setAuthority(issued);
          setRecovered(true);
        }}
        onBack={() => setMode(false)}
      />
    );
  }

  return (
    <main className="login">
      <form className="login__card" onSubmit={onSubmit} noValidate>
        <h1 className="login__title">Establish your credential</h1>
        {recovered ? (
          <p className="login__lede" role="status">
            Your recovery code was accepted. Choose a new password; the establishment authority
            is held for you and works once.
          </p>
        ) : (
          <p className="login__lede">
            {providers.length === 0
              ? 'Paste the setup authority you were handed. It works once, and it only sets a password: you sign in afterwards like anyone else.'
              : 'Paste the setup authority you were handed. Then choose a password, or continue with a provider: either one becomes your first credential.'}
          </p>
        )}

        {failure === null ? null : (
          <Alert>{failure}</Alert>
        )}

        {recovered ? null : (
          // Masked like the password below: a setup authority is a bearer of
          // credential establishment until it is spent, and a text field would
          // hand it to screenshots, extensions and accessibility tooling. After
          // a recovery the authority stays in component state and is not
          // rendered at all.
          <div className="field">
            <label htmlFor={authorityId}>Setup authority</label>
            <input
              id={authorityId}
              name="authority"
              type="password"
              autoComplete="off"
              spellCheck={false}
              required
              disabled={busy}
              value={authority}
              onChange={(event) => setAuthority(event.target.value)}
            />
          </div>
        )}

        <div className="field">
          <label htmlFor={passwordId}>New password</label>
          <input
            id={passwordId}
            name="password"
            type="password"
            autoComplete="new-password"
            required
            disabled={busy}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </div>

        <div className="field">
          <label htmlFor={repeatId}>Repeat the password</label>
          <input
            id={repeatId}
            name="repeat"
            type="password"
            autoComplete="new-password"
            required
            disabled={busy}
            value={repeat}
            onChange={(event) => setRepeat(event.target.value)}
          />
        </div>

        <Button variant="primary" type="submit" disabled={busy}>
          {pending ? 'Establishing…' : 'Establish password'}
        </Button>
        {/* A recovery-issued authority never claims (#610): after a recovery
            the providers are not offered. */}
        {recovered || providers.length === 0 ? null : (
          <>
            <p className="login__or">or continue with</p>
            <div className="login__methods">
              {providers.map((provider) => {
                const id = `${provider.kind}:${provider.slug}`;
                return (
                  <ProviderButton
                    key={id}
                    provider={provider}
                    intent="sign-in"
                    busy={contacting === id}
                    disabled={busy || noAuthority}
                    onClick={() => onClaim(provider)}
                  />
                );
              })}
            </div>
            {providers.some((provider) => provider.brand === 'microsoft') ? (
              <p className="login__brand-hint">Microsoft: work or school account</p>
            ) : null}
          </>
        )}
        {/* Quiet links, as on the login card (#567): a full-width button
            would not wrap, and the long recovery label overflowed a 390px
            screen. The CSS keeps them on the touch floor. */}
        <p className="login__links">
          {recovered ? null : (
            <Link to={`${surfaceById('establish-credential').path}?mode=recover`}>
              Lost your second factor? Recover with a code
            </Link>
          )}
          <Link to={surfaceById('login').path}>Back to sign in</Link>
        </p>
      </form>
    </main>
  );
}

/**
 * RecoveryForm spends one recovery code (#571). It never states which of the
 * server's refusals happened: an unknown user, a used batch, a stale epoch and
 * a wrong code are one sentence, so the page is not an oracle.
 */
function RecoveryForm({
  onRecovered,
  onBack,
}: {
  readonly onRecovered: (authority: string) => void;
  readonly onBack: () => void;
}) {
  const recover = useSensitiveMutation({
    mutationFn: (input: { username: string; code: string }) => beginRecovery(input.username, input.code),
  });
  const [username, setUsername] = useState('');
  const [code, setCode] = useSensitiveState('');
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const usernameId = useId();
  const codeId = useId();

  const onSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFailure(null);
    if (username.trim() === '' || code.trim() === '') {
      setFailure('Enter your username and one unused recovery code.');
      return;
    }
    setPending(true);
    try {
      const authority = await recover.mutateAsync({ username: username.trim(), code: code.trim() });
      setCode('');
      onRecovered(authority);
    } catch (error) {
      setFailure(recoveryFailureText(error));
    } finally {
      setCode('');
      setPending(false);
    }
  };

  return (
    <main className="login">
      <form className="login__card" onSubmit={onSubmit} noValidate>
        <h1 className="login__title">Recover your account</h1>
        <p className="login__lede">
          Lost your second factor? One unused recovery code sets a new password. The code is
          spent whether or not you finish; you sign in afterwards like anyone else.
        </p>

        {failure === null ? null : (
          <Alert>{failure}</Alert>
        )}

        <div className="field">
          <label htmlFor={usernameId}>Username</label>
          <input
            id={usernameId}
            name="username"
            autoComplete="username"
            required
            disabled={pending}
            value={username}
            onChange={(event) => setUsername(event.target.value)}
          />
        </div>

        <div className="field">
          <label htmlFor={codeId}>Recovery code</label>
          <input
            id={codeId}
            name="code"
            type="password"
            autoComplete="one-time-code"
            spellCheck={false}
            required
            disabled={pending}
            value={code}
            onChange={(event) => setCode(event.target.value)}
          />
        </div>

        <Button variant="primary" type="submit" disabled={pending}>
          {pending ? 'Checking…' : 'Continue'}
        </Button>
        <Button type="button" onClick={onBack} disabled={pending}>
          Have a setup authority instead?
        </Button>
        <Link className="btn" to={surfaceById('login').path}>
          Back to sign in
        </Link>
      </form>
    </main>
  );
}
