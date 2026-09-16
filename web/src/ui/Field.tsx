import { useId, type ReactNode } from 'react';

import { cx } from './cx.ts';

export type FieldProps = {
  label: string;
  /** Guidance under the control; secondary ink, same size as the label. */
  hint?: string;
  /** A refusal for THIS control: announced, glyph-carried, and the control is marked invalid. */
  error?: string;
  className?: string;
};

/** What the control inside a {@link Field} must spread to be described and marked. */
export type FieldControlProps = {
  id: string;
  'aria-describedby': string | undefined;
  'aria-invalid': true | undefined;
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
 */
export function Field({
  label,
  hint,
  error,
  className,
  id,
  children,
}: FieldProps & { id?: string; children: (control: FieldControlProps) => ReactNode }) {
  const generated = useId();
  const controlId = id ?? generated;
  const hintId = `${controlId}-hint`;
  const errorId = `${controlId}-error`;
  const describedBy = [hint !== undefined ? hintId : null, error !== undefined ? errorId : null]
    .filter((part): part is string => part !== null)
    .join(' ');
  return (
    <div className={cx('field', error !== undefined && 'field--invalid', className)}>
      <label htmlFor={controlId}>{label}</label>
      {children({
        id: controlId,
        'aria-describedby': describedBy === '' ? undefined : describedBy,
        'aria-invalid': error !== undefined ? true : undefined,
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
