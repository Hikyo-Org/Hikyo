import type { ComponentProps } from 'react';

import { cx } from './cx.ts';
import { Field, type FieldProps } from './Field.tsx';

/**
 * A labelled text input. Emits the `.field` wrapper + `<label>` + `<input>` the
 * screens already use, with the label association wired for free, and an
 * optional hint and per-control error through {@link Field}. `className`
 * reaches the wrapper so callers can add `field--inline` / `field--readonly`.
 */
type InputProps = Omit<ComponentProps<'input'>, 'className'> & FieldProps & {
  /** Set the control in the value face (identifiers, keys, fingerprints). */
  mono?: boolean;
};

export function Input({ label, hint, error, id, className, mono, ...rest }: InputProps) {
  return (
    <Field label={label} hint={hint} error={error} id={id} className={className}>
      {(control) => <input {...rest} {...control} className={cx(mono === true && 'mono')} />}
    </Field>
  );
}
