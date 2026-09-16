import { useId, type ReactNode, type RefObject, type SyntheticEvent } from 'react';

import { cx } from './cx.ts';
import { useModalDialog } from './useModalDialog.ts';

/**
 * The modal dialog: a native `<dialog>` opened with `showModal()`, one
 * anatomy for every decision surface. The audit found two families in the
 * app (`.ceremony` at 520px with a 20px title, 14px ink lede and 12px
 * action gap, no shadow; `.matrix-editor` at 760px with a 15px title, 13px
 * dim lede, 8px gap and a shadow). This is the one they become:
 *
 * - `narrow` (520px) for a decision with two or three buttons; `wide`
 *   (760px) for an editor with fields.
 * - Title is an h2 on the type scale (16/700), lede a caption (13, dim).
 * - Actions wrap in one row, 8px gap like page action rows, PRIMARY LAST
 *   in reading order so Escape and the leftmost button both mean "no".
 * - Shadow on: DESIGN.md reserves shadows for modal overlays, so both
 *   families get one.
 * - Buttons and fields are the page's: one control height, no dialog tier.
 * - No close X (decided 2026-09-16): Escape and the Cancel action are the
 *   two ways out; a corner X invites dismiss-by-reflex on a decision surface.
 *
 * `onCancel` receives the platform's cancel (Escape); a dialog that must be
 * acknowledged before it closes calls `preventDefault()` there.
 */
export function Dialog({
  title,
  lede,
  size = 'narrow',
  actions,
  onCancel,
  initialFocus,
  className,
  children,
}: {
  title: string;
  lede?: ReactNode;
  size?: 'narrow' | 'wide';
  /** The button row. Put the primary action last. */
  actions?: ReactNode;
  onCancel?: (event: SyntheticEvent<HTMLDialogElement>) => void;
  initialFocus?: RefObject<HTMLElement | null>;
  className?: string;
  children?: ReactNode;
}) {
  const ref = useModalDialog(initialFocus);
  const titleId = useId();
  return (
    <dialog
      ref={ref}
      className={cx('dialog', size === 'wide' && 'dialog--wide', className)}
      aria-labelledby={titleId}
      onCancel={onCancel}
    >
      <h2 className="dialog__title" id={titleId}>
        {title}
      </h2>
      {lede !== undefined ? <p className="dialog__lede">{lede}</p> : null}
      {children}
      {actions !== undefined ? <div className="dialog__actions">{actions}</div> : null}
    </dialog>
  );
}
