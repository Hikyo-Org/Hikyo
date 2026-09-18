import type { ComponentProps } from 'react';

import { Field, type FieldProps } from './Field.tsx';

/**
 * A labelled native select. Emits the `.field` wrapper + `<label>` + `<select>`;
 * the native control keeps keyboard and platform behaviour, so this stays a
 * migration swap rather than a rebuilt listbox. Options are the children.
 */
type SelectProps = Omit<ComponentProps<'select'>, 'className'> & FieldProps & {
  /** Set the control in the value face (identifiers, keys, fingerprints). */
  mono?: boolean;
};

export function Select({
  label,
  hint,
  error,
  id,
  className,
  mono,
  children,
  'aria-describedby': describedBy,
  'aria-invalid': invalid,
  ...rest
}: SelectProps) {
  return (
    <Field label={label} hint={hint} error={error} id={id} className={className} aria-describedby={describedBy} aria-invalid={invalid}>
      {(control) => (
        <select {...rest} {...control} className={mono === true ? 'mono' : undefined}>
          {children}
        </select>
      )}
    </Field>
  );
}
