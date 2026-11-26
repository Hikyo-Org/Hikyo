import type { ComponentProps } from 'react';

import { cx } from './cx.ts';
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
  /** Classes for the textarea itself (a route's value face, a masked state); `className` reaches the wrapper. */
  controlClassName?: string;
};

export function Textarea({ label, hint, error, id, className, labelHidden, mono, controlClassName, 'aria-describedby': describedBy, 'aria-invalid': invalid, ...rest }: TextareaProps) {
  return (
    <Field label={label} hint={hint} error={error} id={id} className={className} labelHidden={labelHidden} aria-describedby={describedBy} aria-invalid={invalid}>
      {(control) => <textarea {...rest} {...control} className={cx(mono === true && 'mono', controlClassName) || undefined} />}
    </Field>
  );
}
