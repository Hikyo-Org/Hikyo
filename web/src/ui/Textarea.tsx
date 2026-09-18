import type { ComponentProps } from 'react';

import { Field, type FieldProps } from './Field.tsx';

/**
 * A labelled textarea. The `.field` wrapper + `<label>` + `<textarea>` the
 * screens hand-write, with the label association wired. Sized by the same
 * control token as {@link Input} (two and a half rows by default, resizable
 * vertically), so a form of mixed controls lines up.
 */
type TextareaProps = Omit<ComponentProps<'textarea'>, 'className'> & FieldProps & {
  /** Set the control in the value face (identifiers, keys, fingerprints). */
  mono?: boolean;
};

export function Textarea({ label, hint, error, id, className, mono, 'aria-describedby': describedBy, 'aria-invalid': invalid, ...rest }: TextareaProps) {
  return (
    <Field label={label} hint={hint} error={error} id={id} className={className} aria-describedby={describedBy} aria-invalid={invalid}>
      {(control) => <textarea {...rest} {...control} className={mono === true ? 'mono' : undefined} />}
    </Field>
  );
}
