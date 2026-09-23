import { useId, useRef, useState, type FormEvent } from 'react';

import { useSensitiveState } from '../../api/sensitiveMutation.ts';
import { Alert } from '../Alert.tsx';
import { Button } from '../Button.tsx';
import { Dialog } from '../Dialog.tsx';

/** Which credential the proof field asks for. */
export type ProofField = 'code' | 'password' | 'code-or-password';

/**
 * The "Confirm it's you" proof prompt every account-security and
 * reauthentication-gated mutation shares: one field, Cancel and Confirm.
 *
 * The field matches what the server will accept. `code` is an authenticator
 * code (numeric, `one-time-code`); `password` is the account password
 * (`current-password`); `code-or-password` is either, the shape of the
 * reauth-gated administrative writes, which a password manager must never
 * autofill with a one-time code, so it is a password field
 * (`current-password`), as the profile's proof field is.
 *
 * The proof is component-owned plaintext (useSensitiveState) and dies with
 * the dialog. `onSubmit` receives a `clear` callback: a refused proof is
 * cleared and the field refocused, so the next attempt is a fresh one.
 * `reauth` renders Confirm in the CHANGED slate (ADR #16: "confirm it's you"
 * is visually distinct from "disclose a secret").
 */
export function ProofDialog({
  title = "Confirm it's you",
  lede,
  field,
  label,
  reauth = false,
  pending = false,
  failure = null,
  onCancel,
  onSubmit,
}: {
  title?: string;
  lede: string;
  field: ProofField;
  /** The field label; defaults to the credential the field asks for. */
  label?: string;
  reauth?: boolean;
  pending?: boolean;
  failure?: string | null;
  onCancel: () => void;
  onSubmit: (value: string, clear: () => void) => void;
}) {
  const formId = useId();
  const inputId = useId();
  const input = useRef<HTMLInputElement>(null);
  const [value, setValue] = useSensitiveState('');
  const [touched, setTouched] = useState(false);
  const clear = () => {
    setValue('');
    input.current?.focus();
  };
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setTouched(true);
    if (value === '' || pending) return;
    onSubmit(value, clear);
  };
  const fieldLabel =
    label ?? (field === 'code' ? 'Authenticator code' : field === 'password' ? 'Password' : 'Authenticator code or password');
  return (
    <Dialog
      title={title}
      lede={lede}
      className={reauth ? 'dialog--reauth' : undefined}
      initialFocus={input}
      onCancel={(event) => {
        event.preventDefault();
        if (!pending) onCancel();
      }}
      actions={
        <>
          <Button type="button" disabled={pending} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            type="submit"
            form={formId}
            variant={reauth ? 'secondary' : 'primary'}
            className={reauth ? 'btn--reauth' : undefined}
            disabled={pending || value === ''}
            aria-busy={pending ? true : undefined}
          >
            {pending ? 'Confirming…' : 'Confirm'}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} noValidate>
        {failure === null ? null : <Alert>{failure}</Alert>}
        <div className="field">
          <label htmlFor={inputId}>{fieldLabel}</label>
          {field === 'code' ? (
            <input
              id={inputId}
              ref={input}
              type="text"
              inputMode="numeric"
              autoComplete="one-time-code"
              value={value}
              disabled={pending}
              aria-invalid={touched && failure !== null ? true : undefined}
              onChange={(event) => setValue(event.target.value)}
            />
          ) : (
            <input
              id={inputId}
              ref={input}
              type="password"
              autoComplete="current-password"
              value={value}
              disabled={pending}
              aria-invalid={touched && failure !== null ? true : undefined}
              onChange={(event) => setValue(event.target.value)}
            />
          )}
        </div>
      </form>
    </Dialog>
  );
}
