import { useState, type FormEvent } from 'react';

import { useSensitiveState } from '../../api/sensitiveMutation.ts';
import { Alert } from '../Alert.tsx';
import { Button } from '../Button.tsx';
import { Checkbox } from '../Checkbox.tsx';
import { Input } from '../Input.tsx';
import { AuthenticatorCodeField } from './AuthenticatorCodeField.tsx';
import { QrCode } from './QrCode.tsx';

/** Where the enrolment stands; the parent drives it from the API's answers. */
export type SetupStep =
  | { kind: 'password' }
  | { kind: 'codes'; codes: readonly string[] }
  | { kind: 'choose' }
  | { kind: 'totp'; otpauthUrl: string; secret: string };

/**
 * The enrolment gate: a PASSWORD sign-in on an account with no second factor
 * lands here, and nothing else is reachable until a factor stands
 * (storybook-ui-consistency handoff, decision 2B). No skip control exists by
 * design; an instance that allows unenrolled accounts never shows this step
 * at all, that decision belongs to the flow, not to this surface.
 *
 * Four steps (#785): confirm the password; store the recovery codes, shown
 * once and acknowledged; pick a factor; for an authenticator, scan and confirm
 * one code (a passkey enrols in one gesture). The codes come FIRST because
 * they are proved by the password, which stops being an accepted proof the
 * moment an authenticator stands; issued after it, they would need a second
 * authenticator code from the next time step.
 *
 * Security note: this runs on a session that has JUST presented a password,
 * and the enrolment still asks for it again ("a new credential may never
 * authorize its own enrolment", the human-auth ADR): the sign-in form's copy
 * is gone by the time the gate renders, and a reload lands here too.
 */
export function SecondFactorSetup({
  username,
  step,
  passkeys,
  busy,
  error,
  onPassword,
  onCodesStored,
  onChooseTotp,
  onChoosePasskey,
  onConfirmCode,
}: {
  username: string;
  step: SetupStep;
  /** The platform can create a WebAuthn credential. */
  passkeys: boolean;
  busy: 'password' | 'totp' | 'passkey' | 'code' | null;
  error: string | null;
  onPassword: (password: string) => void;
  onCodesStored: () => void;
  onChooseTotp: () => void;
  onChoosePasskey: () => void;
  onConfirmCode: (code: string) => void;
}) {
  const [stored, setStored] = useState(false);
  // Component-owned sensitive state like the sign-in form's: session
  // retirement wipes it, and it is cleared once handed to the parent.
  const [password, setPassword] = useSensitiveState('');
  const anyBusy = busy !== null;

  const alert = error !== null ? <Alert>{error}</Alert> : null;

  if (step.kind === 'password') {
    const submit = (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      onPassword(password);
      setPassword('');
    };
    return (
      <form className="login__card" onSubmit={submit} noValidate>
        <p className="eyebrow">Step 2 of 4</p>
        <h1 className="login__title">Set up a second factor</h1>
        <p className="login__account">
          Password accepted for <strong>{username}</strong>. This instance requires a second factor
          on every account; nothing else is available until one is enrolled. Confirm your password
          to begin: your recovery codes first, then the factor.
        </p>
        {alert}
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
        <Button variant="primary" type="submit" disabled={anyBusy || password === ''}>
          {busy === 'password' ? 'Issuing recovery codes…' : 'Continue'}
        </Button>
      </form>
    );
  }

  if (step.kind === 'codes') {
    return (
      <div className="login__card">
        <p className="eyebrow">Step 3 of 4</p>
        <h1 className="login__title">Store your recovery codes</h1>
        <p className="login__account">
          Shown once. They are stored as hashes, so nobody, including this instance, can show them
          to you again.
        </p>
        <ul className="codes" aria-label="Recovery codes">
          {step.codes.map((value) => (
            <li className="mono" key={value}>
              {value}
            </li>
          ))}
        </ul>
        {alert}
        <Checkbox
          label="I have stored these somewhere safe."
          checked={stored}
          onChange={(event) => setStored(event.target.checked)}
        />
        <Button variant="primary" type="button" disabled={!stored} onClick={onCodesStored}>
          Continue
        </Button>
      </div>
    );
  }

  if (step.kind === 'choose') {
    return (
      <div className="login__card">
        <p className="eyebrow">Step 4 of 4</p>
        <h1 className="login__title">Choose your second factor</h1>
        <p className="login__account">
          Enrol one for <strong>{username}</strong>. Sign-in completes as soon as it stands.
        </p>
        {alert}
        <div className="login__actions">
          <Button variant="primary" type="button" onClick={onChooseTotp} disabled={anyBusy}>
            {busy === 'totp' ? 'Preparing…' : 'Use an authenticator app'}
          </Button>
          {passkeys ? (
            <Button type="button" onClick={onChoosePasskey} disabled={anyBusy}>
              {busy === 'passkey' ? 'Waiting for the passkey…' : 'Create a passkey'}
            </Button>
          ) : null}
        </div>
      </div>
    );
  }

  // step.kind === 'totp'
  return (
    <div className="login__card">
      <p className="eyebrow">Step 4 of 4</p>
      <h1 className="login__title">Scan, then confirm one code</h1>
      <p className="login__account">
        Add <strong>{username}</strong> to your authenticator app, then enter the code it shows.
      </p>
      <div className="login__qr">
        <QrCode value={step.otpauthUrl} title="Authenticator enrolment QR code" />
        <p className="login__secret">
          <span>Or enter the key by hand</span>
          <code>{step.secret}</code>
        </p>
      </div>
      <AuthenticatorCodeField
        submitLabel="Confirm and enrol"
        busy={busy === 'code' ? 'Checking…' : null}
        disabled={anyBusy}
        onSubmit={onConfirmCode}
      >
        {alert}
      </AuthenticatorCodeField>
    </div>
  );
}
