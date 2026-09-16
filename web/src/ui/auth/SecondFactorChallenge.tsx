import { useState, type FormEvent } from 'react';

import { Button } from '../Button.tsx';
import { Input } from '../Input.tsx';

/**
 * The second step of a PASSWORD sign-in when the account has a second factor
 * enrolled: an authenticator code, or a passkey assertion as the alternative.
 *
 * Never skippable (Marc, 2026-09-16): an enrolled factor is presented or the
 * sign-in does not complete. Today `localLoginOp` mints the session at
 * password assurance before this step, so the challenge is sequencing until
 * the backend work in docs/handoff/storybook-ui-consistency.md lands; the
 * surface is built for the enforced version and has no way past it.
 *
 * Passkey sign-in and identity-provider sign-in never reach this step: the
 * first already carries multi-factor assurance, the second takes its
 * assurance from the provider.
 */
export function SecondFactorChallenge({
  username,
  totp,
  passkey,
  busy,
  error,
  onCode,
  onPasskey,
}: {
  /** The account that just presented a password, so the step names who it is for. */
  username: string;
  /** An authenticator app is enrolled: show the code field. */
  totp: boolean;
  /** A passkey is enrolled and the platform can assert: show the passkey button. */
  passkey: boolean;
  busy: 'code' | 'passkey' | null;
  error: string | null;
  onCode: (code: string) => void;
  onPasskey: () => void;
}) {
  const [code, setCode] = useState('');
  const anyBusy = busy !== null;

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onCode(code.trim());
    setCode('');
  };

  return (
    <form className="login__card" onSubmit={onSubmit} noValidate>
      <p className="login__step">Step 2 of 2</p>
      <h1 className="login__title">Present your second factor</h1>
      <p className="login__account">
        Password accepted for <strong>{username}</strong>. Instance settings, grants and secret
        disclosure need a second factor presented in this session.
      </p>

      {error !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{error}</span>
        </p>
      ) : null}

      {totp ? (
        <>
          <Input
            className="login__code"
            label="Authenticator code"
            name="code"
            inputMode="numeric"
            autoComplete="one-time-code"
            pattern="[0-9]{6,10}"
            required
            autoFocus
            disabled={anyBusy}
            value={code}
            onChange={(event) => setCode(event.target.value)}
          />
          <Button variant="primary" type="submit" disabled={anyBusy || code.trim() === ''}>
            {busy === 'code' ? 'Checking…' : 'Present code'}
          </Button>
        </>
      ) : null}

      {totp && passkey ? (
        <p className="login__or" aria-hidden="true">
          or
        </p>
      ) : null}

      {passkey ? (
        <Button type="button" onClick={onPasskey} disabled={anyBusy}>
          {busy === 'passkey' ? 'Waiting for the passkey…' : 'Use a passkey'}
        </Button>
      ) : null}
    </form>
  );
}
