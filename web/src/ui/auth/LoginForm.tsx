import { zAuthMethodProvider } from '@hikyo/zod';
import { useState, type FormEvent, type ReactNode } from 'react';
import type { z } from 'zod';

import { Alert } from '../Alert.tsx';
import { Button } from '../Button.tsx';
import { Input } from '../Input.tsx';

/** One configured identity provider, as `GET /auth/methods` describes it. */
export type SignInProvider = z.infer<typeof zAuthMethodProvider>;

/** Which sign-in leg is in flight, so only ITS control shows the busy label. */
export type SignInBusy = 'password' | 'passkey' | { provider: string } | null;

/**
 * The sign-in card, prop-driven. It is the presentational half of the Login
 * route: the same `.login__card` markup, with the three ways in (local
 * password, discoverable passkey, an identity provider) named as callbacks so
 * Storybook can walk the flow without a transport.
 *
 * Passkey sign-in is PRIMARY authentication, not a second factor: a
 * user-verifying discoverable credential mints a multi-factor session in one
 * gesture (human-auth ADR, "Passkey login"), so it is presented as an
 * alternative to the password, never after it. Providers are separate legs
 * again: their assurance comes from the identity provider's `acr`/`amr`, and
 * no local factor is asked for here.
 */
export function LoginForm({
  providers,
  passkeys,
  busy,
  error,
  onPassword,
  onPasskey,
  onProvider,
  links,
}: {
  providers: readonly SignInProvider[];
  /** Whether the platform can perform a WebAuthn assertion at all. */
  passkeys: boolean;
  busy: SignInBusy;
  /** A refusal to show above the form, already worded (see loginFailureText). */
  error: string | null;
  onPassword: (credentials: { username: string; password: string }) => void;
  onPasskey: () => void;
  onProvider: (slug: string) => void;
  /** The quiet links under the card (establish a credential, recover). */
  links?: ReactNode;
}) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const anyBusy = busy !== null;

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onPassword({ username, password });
    setPassword('');
  };

  return (
    <form className="login__card" onSubmit={onSubmit} noValidate>
      <h1 className="login__title">Sign in to Hikyo</h1>
      <p className="login__lede">Use the credential you established with your setup authority.</p>

      {error !== null ? <Alert>{error}</Alert> : null}

      <Input
        label="Username"
        name="username"
        autoComplete="username"
        required
        disabled={anyBusy}
        value={username}
        onChange={(event) => setUsername(event.target.value)}
      />
      <Input
        label="Password"
        name="password"
        type="password"
        autoComplete="current-password"
        required
        disabled={anyBusy}
        value={password}
        onChange={(event) => setPassword(event.target.value)}
      />

      <Button variant="primary" type="submit" disabled={anyBusy}>
        {busy === 'password' ? 'Signing in…' : 'Sign in'}
      </Button>

      {passkeys || providers.length > 0 ? (
        <p className="login__or" aria-hidden="true">
          or
        </p>
      ) : null}

      <div className="login__actions">
        {passkeys ? (
          <Button type="button" onClick={onPasskey} disabled={anyBusy}>
            {busy === 'passkey' ? 'Waiting for the passkey…' : 'Sign in with a passkey'}
          </Button>
        ) : null}
        {providers.map((provider) => (
          <Button
            type="button"
            key={provider.slug}
            onClick={() => onProvider(provider.slug)}
            disabled={anyBusy}
          >
            {typeof busy === 'object' && busy !== null && busy.provider === provider.slug
              ? 'Contacting identity provider…'
              : `Continue with ${provider.display_name}`}
          </Button>
        ))}
      </div>

      {links !== undefined ? <p className="login__links">{links}</p> : null}
    </form>
  );
}
