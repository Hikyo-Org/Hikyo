import { type ReactNode } from 'react';

import { cx } from './cx.ts';

/**
 * A labelled set of {@link Checkbox} or {@link Radio} rows. A fieldset, so
 * the legend names the whole set for assistive tech; the browser's fieldset
 * chrome is reset to the plain `.field` box. Rows stack by default and wrap
 * in a row with `inline`.
 */
export function ChoiceGroup({
  legend,
  hint,
  inline,
  className,
  children,
}: {
  legend: string;
  hint?: string;
  inline?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <fieldset className={cx('field', 'choice-group', inline === true && 'choice-group--inline', className)}>
      <legend>{legend}</legend>
      {hint !== undefined ? <p className="field__hint">{hint}</p> : null}
      <div className="choice-group__options">{children}</div>
    </fieldset>
  );
}
