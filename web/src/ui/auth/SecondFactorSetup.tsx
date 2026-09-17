import { useState } from 'react';

import { Alert } from '../Alert.tsx';
import { Button } from '../Button.tsx';
import { Checkbox } from '../Checkbox.tsx';
import { AuthenticatorCodeField } from './AuthenticatorCodeField.tsx';
import { QrCode } from './QrCode.tsx';

/** Where the enrolment stands; the parent drives it from the API's answers. */
export type SetupStep =
  | { kind: 'choose' }
  | { kind: 'totp'; otpauthUrl: string; secret: string }
  | { kind: 'codes'; codes: readonly string[] };

/**
 * The enrolment gate: a PASSWORD sign-in on an account with no second factor
 * lands here, and nothing else is reachable until a factor stands
 * (storybook-ui-consistency handoff, decision 2B). No skip control exists by
 * design; an instance that allows unenrolled accounts never shows this step
 * at all, that decision belongs to the flow, not to this surface.
 *
 * Three steps: pick a factor; for an authenticator, scan and confirm one
 * code (a passkey enrols in one gesture); then the recovery codes, shown once
 * and acknowledged before the session may proceed.
 *
 * Security note for the migration: this runs on a session that has JUST
 * presented a password, which is what lets it enrol at all ("a new credential
 * may never authorize its own enrolment", the human-auth ADR). The backend
 * work it needs is listed in the storybook-ui-consistency handoff, §3.
 */
export function SecondFactorSetup({
  username,
  step,
  passkeys,
  busy,
  error,
  onChooseTotp,
  onChoosePasskey,
  onConfirmCode,
  onDone,
}: {
  username: string;
  step: SetupStep;
  /** The platform can create a WebAuthn credential. */
  passkeys: boolean;
  busy: 'totp' | 'passkey' | 'code' | null;
  error: string | null;
  onChooseTotp: () => void;
  onChoosePasskey: () => void;
  onConfirmCode: (code: string) => void;
  onDone: () => void;
}) {
  const [stored, setStored] = useState(false);
  const anyBusy = busy !== null;

  const alert = error !== null ? <Alert>{error}</Alert> : null;

  if (step.kind === 'choose') {
    return (
      <div className="login__card">
        <p className="eyebrow">Step 2 of 3</p>
        <h1 className="login__title">Set up a second factor</h1>
        <p className="login__account">
          Password accepted for <strong>{username}</strong>. This instance requires a second factor
          on every account; nothing else is available until one is enrolled.
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

  if (step.kind === 'totp') {
    return (
      <div className="login__card">
        <p className="eyebrow">Step 2 of 3</p>
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

  return (
    <div className="login__card">
      <p className="eyebrow">Step 3 of 3</p>
      <h1 className="login__title">Store your recovery codes</h1>
      <p className="login__account">
        Shown once. They are stored as hashes, so nobody, including this instance, can show them to
        you again.
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
      <Button variant="primary" type="button" disabled={!stored} onClick={onDone}>
        Continue to Hikyo
      </Button>
    </div>
  );
}
