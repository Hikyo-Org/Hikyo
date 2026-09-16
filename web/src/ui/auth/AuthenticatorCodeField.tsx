import { useState, type FormEvent, type ReactNode } from 'react';

import { Button } from '../Button.tsx';
import { Input } from '../Input.tsx';

/**
 * The one-time-code field and its submit, shared by the sign-in challenge
 * and the enrolment confirmation: numeric, `one-time-code` autocomplete, the
 * value trimmed on submit and cleared after it, the button disabled until a
 * code is typed. Renders as the form so Enter submits.
 */
export function AuthenticatorCodeField({
  submitLabel,
  busy,
  disabled,
  onSubmit,
  children,
}: {
  submitLabel: string;
  /** Label while the code is being checked. */
  busy: string | null;
  disabled: boolean;
  onSubmit: (code: string) => void;
  /** Rendered between the field and the submit (an error, say). */
  children?: ReactNode;
}) {
  const [code, setCode] = useState('');
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onSubmit(code.trim());
    setCode('');
  };
  return (
    <form className="login__actions" onSubmit={submit} noValidate>
      <Input
        className="login__code"
        label="Authenticator code"
        name="code"
        inputMode="numeric"
        autoComplete="one-time-code"
        pattern="[0-9]{6,10}"
        required
        autoFocus
        disabled={disabled}
        value={code}
        onChange={(event) => setCode(event.target.value)}
      />
      {children}
      <Button variant="primary" type="submit" disabled={disabled || code.trim() === ''}>
        {busy ?? submitLabel}
      </Button>
    </form>
  );
}
