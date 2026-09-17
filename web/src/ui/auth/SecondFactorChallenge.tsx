import { Alert } from '../Alert.tsx';
import { Button } from '../Button.tsx';
import { AuthenticatorCodeField } from './AuthenticatorCodeField.tsx';

/**
 * The second step of a PASSWORD sign-in when the account has a second factor
 * enrolled: an authenticator code, or a passkey assertion as the alternative.
 *
 * Never skippable (storybook-ui-consistency handoff, decision 2B): an
 * enrolled factor is presented or the sign-in does not complete. The
 * surface is built for the enforced version and has no way past it; the
 * server-side enforcement it needs is specified in the same handoff, §3.
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
  const anyBusy = busy !== null;
  const alert = error !== null ? <Alert>{error}</Alert> : null;

  return (
    <div className="login__card">
      <p className="eyebrow">Step 2 of 2</p>
      <h1 className="login__title">Present your second factor</h1>
      <p className="login__account">
        Password accepted for <strong>{username}</strong>. Sign-in completes once the second factor
        enrolled on this account is presented.
      </p>

      {totp ? (
        <AuthenticatorCodeField
          submitLabel="Present code"
          busy={busy === 'code' ? 'Checking…' : null}
          disabled={anyBusy}
          onSubmit={onCode}
        >
          {alert}
        </AuthenticatorCodeField>
      ) : (
        alert
      )}

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

      {!totp && !passkey ? (
        <Alert>
          The factor enrolled on this account cannot be presented from this device. Sign in from the
          device that holds it, or recover with a code from the sign-in page.
        </Alert>
      ) : null}
    </div>
  );
}
