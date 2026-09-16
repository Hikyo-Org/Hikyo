import { useState, type ComponentProps } from 'react';

import { Field, type FieldProps } from './Field.tsx';

/**
 * A labelled text input. Emits the `.field` wrapper + `<label>` + `<input>` the
 * screens already use, with the label association wired for free, and an
 * optional hint and per-control error through {@link Field}. `className`
 * reaches the wrapper so callers can add `field--inline` / `field--readonly`.
 *
 * `revealable` on a password field adds a show/hide toggle inside the control
 * (a pressed button, named for assistive tech). It is for a value the person
 * is typing, not a stored secret: revealing a secret stays the ceremony
 * DESIGN.md describes, and never a toggle beside the field.
 */
type InputProps = Omit<ComponentProps<'input'>, 'className'> & FieldProps & {
  /** Set the control in the value face (identifiers, keys, fingerprints). */
  mono?: boolean;
  /** Password fields only: a show/hide toggle for the value being typed. */
  revealable?: boolean;
};

export function Input({ label, hint, error, id, className, mono, revealable, type, ...rest }: InputProps) {
  const [shown, setShown] = useState(false);
  const reveal = revealable === true && type === 'password';
  return (
    <Field label={label} hint={hint} error={error} id={id} className={className}>
      {(control) =>
        reveal ? (
          <span className="field__control">
            <input {...rest} {...control} type={shown ? 'text' : 'password'} className={mono === true ? 'mono' : undefined} />
            <button
              type="button"
              className="btn btn--quiet field__reveal"
              aria-pressed={shown}
              aria-label={shown ? 'Hide password' : 'Show password'}
              onClick={() => setShown((value) => !value)}
            >
              {shown ? 'Hide' : 'Show'}
            </button>
          </span>
        ) : (
          <input {...rest} {...control} type={type} className={mono === true ? 'mono' : undefined} />
        )
      }
    </Field>
  );
}
