import type { ReactNode } from 'react';

import { cx } from './cx.ts';

/** The region needs a name a screen reader can announce: the table's caption id, or a label where it has none. */
type Name = { 'aria-labelledby': string; 'aria-label'?: never } | { 'aria-label': string; 'aria-labelledby'?: never };

export type TableScrollProps = Name & { className?: string; children: ReactNode };

/**
 * The one wrapper for a scrollable data table. A table that overflows on a
 * phone and holds nothing focusable fails axe `scrollable-region-focusable`,
 * so the wrapper is itself a named, keyboard-focusable region (`tabIndex` 0).
 * Name it with `aria-labelledby` pointing at the table's `<caption id>`, or
 * `aria-label` where the table has no caption.
 */
export function TableScroll({ className, children, ...name }: TableScrollProps) {
  return (
    <div className={cx('table-scroll', className)} role="region" tabIndex={0} {...name}>
      {children}
    </div>
  );
}
