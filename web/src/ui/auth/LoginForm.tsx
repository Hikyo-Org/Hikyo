import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';

import { isLastSignIn, type LastSignIn } from '../../api/lastSignIn.ts';
import { useSensitiveState } from '../../api/sensitiveMutation.ts';
import { Alert } from '../Alert.tsx';
import { Badge } from '../Badge.tsx';
import { Button } from '../Button.tsx';
import { Glyph } from '../Glyph.tsx';
import { Input } from '../Input.tsx';
import { ProviderButton, type LoginProvider, type SignInIntent } from './ProviderButton.tsx';

export type { LoginProvider, ProviderBrand, SignInIntent, SignInProvider } from './ProviderButton.tsx';

/**
 * One configured provider as the card names it back: a slug is unique per kind
 * only (an OIDC and a SAML provider can both be `corp`), so the pair is the
 * identity.
 */
export type ProviderIdentity = { readonly kind: LoginProvider['kind']; readonly slug: string };

/**
 * Which sign-in leg is in flight, so only ITS control shows the busy label. A
 * provider leg whose row is not known yet names `null`: every control is still
 * barred, and no row wears a label it did not earn.
 */
export type SignInBusy = 'password' | 'passkey' | { provider: ProviderIdentity | null } | null;

/**
 * The open sign-up door: the providers the scope's registration policy admits
 * (`signup_methods` of `GET /auth/methods`, matched by `{kind, slug}` into
 * `providers`, since a slug is unique per kind only) and where a new account
 * lands, already worded. `null` while registration is closed: the door is then
 * absent, not disabled (#579 d6).
 */
export type SignupDoor = {
  readonly providers: readonly LoginProvider[];
  /** "You'll join this organisation." and the like; null when the wire names no landing. */
  readonly landing: string | null;
};

type Stage =
  | { at: 'choose' }
  | { at: 'password' }
  | { at: 'sign-up' }
  | { at: 'confirm'; provider: ProviderIdentity };

/**
 * The step the card shows, from the one chosen and the door as it stands
 * now. The door and its confirmation exist only while `signup` stands: the
 * link that opens them renders only then, and a door that closes underneath
 * (discovery refetched, policy gone) falls back to the first step. A
 * confirmation whose provider the refreshed door no longer admits falls back
 * to the door, where the rows it still admits are.
 */
function shownStage(chosen: Stage, signup: SignupDoor | null): Stage {
  if (chosen.at !== 'sign-up' && chosen.at !== 'confirm') return chosen;
  if (signup === null) return { at: 'choose' };
  if (
    chosen.at === 'confirm' &&
    !signup.providers.some(
      (provider) => provider.kind === chosen.provider.kind && provider.slug === chosen.provider.slug,
    )
  ) {
    return { at: 'sign-up' };
  }
  return chosen;
}

/**
 * The sign-in card, prop-driven. It is the presentational half of the Login
 * route: the `.login__card` markup, with the ways in (local password,
 * discoverable passkey, an identity provider) named as callbacks so Storybook
 * can walk the flow without a transport.
 *
 * The entry is staged (prototype social-signin/2, #587 locked): step one
 * picks a method, one row each, then only that method's form shows. A
 * provider row and the passkey row have no form, so they start their ceremony
 * from step one. The sign-up door lists only the methods the policy admits,
 * and a provider chosen there passes a confirmation step whose copy says a new
 * account is being created (#604: the intent rides the start request).
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
  signup,
  paused,
  lastUsed,
  busy,
  error,
  onPassword,
  onPasskey,
  onProvider,
  links,
  initialIntent = 'sign-in',
}: {
  providers: readonly LoginProvider[];
  /** Whether the platform can perform a WebAuthn assertion at all. */
  passkeys: boolean;
  signup: SignupDoor | null;
  /** The scope has a registration policy that is inactive: say so, never why (#587 Q2). */
  paused: boolean;
  /** The way in this browser used last time; its row wears the "Last used" badge. */
  lastUsed: LastSignIn | null;
  busy: SignInBusy;
  /** A refusal to show above the form, already worded (see loginFailureText). */
  error: string | null;
  onPassword: (credentials: { username: string; password: string }) => void;
  onPasskey: () => void;
  onProvider: (provider: ProviderIdentity, intent: SignInIntent) => void;
  /** The quiet links under the card (establish a credential, recover). */
  links?: ReactNode;
  /** The door the card opens on: `/signup` opens on "Create an account" while it is open. */
  initialIntent?: SignInIntent;
}) {
  const [chosen, setStage] = useState<Stage>({ at: initialIntent === 'sign-up' ? 'sign-up' : 'choose' });
  const stage = shownStage(chosen, signup);
  // A step change unmounts the control that was pressed, which would drop
  // focus to the document: the new step's heading takes it instead, so a
  // keyboard or screen-reader user lands on what changed. Not on mount, where
  // the page's own focus order is the right one: the effect keys on the
  // previous step, so StrictMode's double invocation cannot focus at load.
  const heading = useRef<HTMLHeadingElement>(null);
  const previous = useRef(stage.at);
  useEffect(() => {
    if (previous.current !== stage.at) heading.current?.focus();
    previous.current = stage.at;
  }, [stage.at]);
  const title = (text: string) => (
    <h1 className="login__title" ref={heading} tabIndex={-1}>
      {text}
    </h1>
  );
  const [username, setUsername] = useState('');
  // The plaintext password is component-owned sensitive state, not plain
  // useState: session retirement wipes it, it starts empty on mount, and a
  // setter captured before a session change cannot repopulate the field.
  const [password, setPassword] = useSensitiveState('');
  const anyBusy = busy !== null;
  const providerBusy = ({ kind, slug }: ProviderIdentity) =>
    typeof busy === 'object' && busy?.provider?.kind === kind && busy.provider.slug === slug;
  const alert = error !== null ? <Alert>{error}</Alert> : null;
  // The hint says "work or school account" once the door has an Entra row
  // (research section 3.7); one line for every tenant row, not one each.
  const microsoftHint = (rows: readonly LoginProvider[]) =>
    rows.some((provider) => provider.brand === 'microsoft') ? (
      <p className="login__brand-hint">Microsoft: work or school account</p>
    ) : null;
  const back = (label: string, to: Stage) => (
    <Button
      type="button"
      variant="quiet"
      className="login__back"
      disabled={anyBusy}
      onClick={() => setStage(to)}
    >
      <span aria-hidden="true">‹ </span>
      {label}
    </Button>
  );

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onPassword({ username, password });
    setPassword('');
  };

  switch (stage.at) {
    case 'password':
      return (
        <form className="login__card" onSubmit={onSubmit} noValidate>
          {back('Other ways to sign in', { at: 'choose' })}
          {title('Sign in with a password')}
          <p className="login__lede">Use the credential you established with your setup authority.</p>
          {alert}
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
          {links !== undefined ? <p className="login__links">{links}</p> : null}
        </form>
      );

    case 'sign-up': {
      if (signup === null) return null;
      return (
        <div className="login__card">
          {back('Sign in instead', { at: 'choose' })}
          {title('Create an account')}
          {signup.landing !== null ? <p className="login__landing">{signup.landing}</p> : null}
          <div className="login__methods">
            {signup.providers.map((provider) => (
              <ProviderButton
                key={`${provider.kind}:${provider.slug}`}
                provider={provider}
                intent="sign-up"
                busy={false}
                disabled={anyBusy}
                onClick={() => setStage({ at: 'confirm', provider: { kind: provider.kind, slug: provider.slug } })}
              />
            ))}
          </div>
          {microsoftHint(signup.providers)}
        </div>
      );
    }

    case 'confirm': {
      const provider = signup?.providers.find(
        (candidate) => candidate.kind === stage.provider.kind && candidate.slug === stage.provider.slug,
      );
      if (signup === null || provider === undefined) return null;
      return (
        <div className="login__card">
          {back('Other ways to create an account', { at: 'sign-up' })}
          {title(`Create an account with ${provider.display_name}`)}
          <p className="login__lede">
            This creates a new account. Already have one? Sign in with it first, then add{' '}
            {provider.display_name} under Settings › Security.
          </p>
          {signup.landing !== null ? <p className="login__landing">{signup.landing}</p> : null}
          {alert}
          <Button
            variant="primary"
            type="button"
            disabled={anyBusy}
            onClick={() => onProvider({ kind: provider.kind, slug: provider.slug }, 'sign-up')}
          >
            {providerBusy(provider) ? 'Contacting identity provider…' : `Continue to ${provider.display_name}`}
          </Button>
        </div>
      );
    }

    case 'choose':
      return (
        <div className="login__card">
          {title('Sign in to Hikyo')}
          <p className="login__lede">Choose how you sign in.</p>
          {alert}
          <div className="login__methods">
            <Button
              type="button"
              className="login__method"
              disabled={anyBusy}
              onClick={() => setStage({ at: 'password' })}
            >
              Password
              {isLastSignIn(lastUsed, { kind: 'password' }) ? (
                <Badge className="login__last-used">Last used</Badge>
              ) : null}
              <span className="login__method-hint" aria-hidden="true">
                username
              </span>
            </Button>
            {passkeys ? (
              <Button type="button" className="login__method" disabled={anyBusy} onClick={onPasskey}>
                {busy === 'passkey' ? 'Waiting for the passkey…' : 'Passkey'}
                {isLastSignIn(lastUsed, { kind: 'passkey' }) ? (
                  <Badge className="login__last-used">Last used</Badge>
                ) : null}
                {busy === 'passkey' ? null : (
                  <span className="login__method-hint" aria-hidden="true">
                    this device
                  </span>
                )}
              </Button>
            ) : null}
            {providers.map((provider) => (
              <ProviderButton
                key={`${provider.kind}:${provider.slug}`}
                provider={provider}
                intent="sign-in"
                busy={providerBusy(provider)}
                disabled={anyBusy}
                lastUsed={isLastSignIn(lastUsed, { kind: 'provider', providerKind: provider.kind, slug: provider.slug })}
                onClick={() => onProvider({ kind: provider.kind, slug: provider.slug }, 'sign-in')}
              />
            ))}
          </div>
          {microsoftHint(providers)}
          {paused ? (
            <p className="login__paused" role="status">
              <Glyph name="warn" />
              Sign-up is paused.
            </p>
          ) : null}
          {signup !== null ? (
            <p className="login__signup">
              New here?{' '}
              <Button
                type="button"
                variant="quiet"
                disabled={anyBusy}
                onClick={() => setStage({ at: 'sign-up' })}
              >
                Create an account
              </Button>
            </p>
          ) : null}
          {links !== undefined ? <p className="login__links">{links}</p> : null}
        </div>
      );
  }
}
