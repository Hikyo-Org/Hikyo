import { useId, useState, type ReactNode } from 'react';

import { cx } from './cx.ts';
import { Glyph } from './Glyph.tsx';

/**
 * A disclosure: a text-style toggle that shows or hides the content under
 * it. Deliberately NOT a button or a chip to look at (no border, no fill):
 * a chevron that turns from right to down is what says "this expands", as on
 * the matrix group headers. The turn is instant, so there is nothing for
 * reduced motion to switch off.
 *
 * The toggle is a control, so it stands at `--control` and takes the one
 * focus ring; `aria-expanded` and `aria-controls` name the state and the
 * content for assistive tech. The label should say what is behind it, with
 * a count where there is one ("Pick single keys (8)").
 */
export function Disclosure({
  label,
  defaultOpen = false,
  className,
  children,
}: {
  label: ReactNode;
  defaultOpen?: boolean;
  /** Classes for the content box, e.g. a layout for what it holds. */
  className?: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const contentId = useId();
  return (
    <div className="disclosure">
      <button type="button" className="disclosure__toggle" aria-expanded={open} aria-controls={contentId} onClick={() => setOpen(!open)}>
        <Glyph name="chevron" className="disclosure__chevron" />
        <span>{label}</span>
      </button>
      <div id={contentId} className={cx('disclosure__content', className)} hidden={!open}>
        {children}
      </div>
    </div>
  );
}
