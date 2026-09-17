import { useId, type CSSProperties, type ReactNode } from 'react';

import { cx } from './cx.ts';

export type ChoiceLayout = 'stack' | 'wrap' | 'nowrap';

/**
 * A labelled set of {@link Checkbox} or {@link Radio} rows. A fieldset, so
 * the legend names the whole set for assistive tech; the browser's fieldset
 * chrome is reset to the plain `.field` box.
 *
 * Layout is a prop, not a data shape (decided 2026-09-16, 1B): a set is
 * never an array of rows, because how it breaks is presentation and the
 * semantic grouping is a second ChoiceGroup with its own legend.
 * - `layout`: `stack` (one per line, default), `wrap` (a wrapping row), or
 *   `nowrap` (one line that scrolls sideways; the options stay reachable by
 *   keyboard because each is focusable).
 * - `columns`: a grid of N columns, row-major, so "one row of N" and "N
 *   columns" are the same thing. Collapses to one column when the group is
 *   narrower than 480px (container query), so a phone never gets a
 *   four-column checkbox grid. Takes precedence over `layout`.
 * - `variant="chips"`: dense sets of short items (key names) as selectable
 *   chips on the compact tier with the badge radius; the checked state is
 *   named by the box AND the chip border.
 */
export function ChoiceGroup({
  legend,
  hint,
  layout = 'stack',
  columns,
  variant = 'rows',
  className,
  children,
}: {
  legend: string;
  hint?: string;
  layout?: ChoiceLayout;
  /** Grid columns (2 or more); overrides `layout`. */
  columns?: number;
  variant?: 'rows' | 'chips';
  className?: string;
  children: ReactNode;
}) {
  const hintId = useId();
  const grid = columns !== undefined && columns > 1;
  // Typed, not cast: CSSProperties has no index signature for custom
  // properties, so the shape is declared rather than asserted.
  const style: (CSSProperties & Record<'--choice-columns', string>) | undefined = grid
    ? { '--choice-columns': String(columns) }
    : undefined;
  return (
    <fieldset
      className={cx(
        'field',
        'choice-group',
        grid ? 'choice-group--grid' : `choice-group--${layout}`,
        variant === 'chips' && 'choice-group--chips',
        className,
      )}
      style={style}
      aria-describedby={hint !== undefined ? hintId : undefined}
    >
      <legend>{legend}</legend>
      {hint !== undefined ? (
        <p className="field__hint" id={hintId}>
          {hint}
        </p>
      ) : null}
      <div className="choice-group__options">{children}</div>
    </fieldset>
  );
}
