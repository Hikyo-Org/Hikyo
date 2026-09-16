import type { ComponentProps } from 'react';

import { Field, type FieldProps } from './Field.tsx';

/**
 * A labelled native select. Emits the `.field` wrapper + `<label>` + `<select>`;
 * the native control keeps keyboard and platform behaviour, so this stays a
 * migration swap rather than a rebuilt listbox. Options are the children.
 */
type SelectProps = Omit<ComponentProps<'select'>, 'className'> & FieldProps;

export function Select({ label, hint, error, id, className, children, ...rest }: SelectProps) {
  return (
    <Field label={label} hint={hint} error={error} id={id} className={className}>
      {(control) => (
        <select {...rest} {...control}>
          {children}
        </select>
      )}
    </Field>
  );
}
