import { useId, type ComponentProps, type ReactNode } from 'react';

import { cx } from './cx.ts';

export type FieldProps = {
  label: string;
  /** Guidance under the control; secondary ink, same size as the label. */
  hint?: string;
  /** A refusal for THIS control: announced, glyph-carried, and the control is marked invalid. */
  error?: string;
  className?: string;
};

/**
 * The caller's own accessibility wiring, merged rather than replaced: an
 * external description (a paragraph elsewhere on the screen) is kept in front
 * of the generated hint and error ids, and an external invalid state is kept
 * unless the field's own error already marks the control.
 */
export type FieldAriaProps = {
  'aria-describedby'?: string;
  'aria-invalid'?: ComponentProps<'input'>['aria-invalid'];
};

/** What the control inside a {@link Field} must spread to be described and marked. */
export type FieldControlProps = {
  id: string;
  'aria-describedby': string | undefined;
  'aria-invalid': ComponentProps<'input'>['aria-invalid'];
};

/**
 * The composed field: label, control, hint, error. Input, Select and Textarea
 * render through it so the three share one layout and one wiring. The
 * control is a render function because it must carry the ids: the label
 * targets it, the hint and error describe it, and an error marks it invalid,
 * which is what a screen reader announces and what `[aria-invalid]` styles.
 *
 * Errors here are per control. A form-level refusal (server said no) stays
 * an {@link Alert} above the actions, as the routes do today.
 *
 * Its own stories show the shared layout with a bare control; the Input,
 * Select and Textarea stories (WithHint, WithError, ErrorIsWired, the
 * External* pair) cover the wiring through each real control.
 */
export function Field({
  label,
  hint,
  error,
  className,
  id,
  'aria-describedby': describedByProp,
  'aria-invalid': invalidProp,
  children,
}: FieldProps & FieldAriaProps & { id?: string; children: (control: FieldControlProps) => ReactNode }) {
  const generated = useId();
  const controlId = id ?? generated;
  const hintId = `${controlId}-hint`;
  const errorId = `${controlId}-error`;
  const describedBy = [describedByProp, hint !== undefined ? hintId : undefined, error !== undefined ? errorId : undefined]
    .filter((part): part is string => part !== undefined && part !== '')
    .join(' ');
  return (
    <div className={cx('field', className)}>
      <label htmlFor={controlId}>{label}</label>
      {children({
        id: controlId,
        'aria-describedby': describedBy === '' ? undefined : describedBy,
        'aria-invalid': error !== undefined ? true : invalidProp,
      })}
      {hint !== undefined ? (
        <p className="field__hint" id={hintId}>
          {hint}
        </p>
      ) : null}
      {error !== undefined ? (
        <p className="field__error" id={errorId} role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{error}</span>
        </p>
      ) : null}
    </div>
  );
}
