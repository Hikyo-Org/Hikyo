import type { ComponentProps } from 'react';

import { Field, type FieldProps } from './Field.tsx';

/**
 * A labelled text input. Emits the `.field` wrapper + `<label>` + `<input>` the
 * screens already use, with the label association wired for free, and an
 * optional hint and per-control error through {@link Field}. `className`
 * reaches the wrapper so callers can add `field--inline` / `field--readonly`.
 */
type InputProps = Omit<ComponentProps<'input'>, 'className'> & FieldProps;

export function Input({ label, hint, error, id, className, ...rest }: InputProps) {
  return (
    <Field label={label} hint={hint} error={error} id={id} className={className}>
      {(control) => <input {...rest} {...control} />}
    </Field>
  );
}
