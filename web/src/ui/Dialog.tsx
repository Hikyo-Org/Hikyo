import {
  useId,
  useRef,
  type MouseEvent,
  type ReactNode,
  type RefObject,
  type SyntheticEvent,
} from 'react';

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
 *
 * `onBackdropClick` is the editors' third way out, opt-in per dialog.
 */
export function Dialog({
  title,
  mono,
  lede,
  size = 'narrow',
  actions,
  onCancel,
  onBackdropClick,
  initialFocus,
  className,
  children,
}: {
  title: string;
  /** Sets the title in the value face, for a dialog named after an identifier. */
  mono?: boolean;
  lede?: ReactNode;
  size?: 'narrow' | 'wide';
  /** The button row. Put the primary action last. */
  actions?: ReactNode;
  onCancel?: (event: SyntheticEvent<HTMLDialogElement>) => void;
  /**
   * Called when a click lands on the scrim. The dialog receives the click for
   * its own PADDING as well as for the backdrop (`event.target` is the dialog
   * either way), so a click inside the dialog's box never counts: aiming at a
   * field and missing it by a few pixels must not discard the edit.
   *
   * BOTH ends of the click must land on the scrim. A drag-select that starts
   * in a field and releases past the dialog's edge is a selection, not a walk
   * away, and the reverse drag (down on the scrim, up inside) is not one
   * either.
   */
  onBackdropClick?: () => void;
  initialFocus?: RefObject<HTMLElement | null>;
  className?: string;
  children?: ReactNode;
}) {
  const ref = useModalDialog(initialFocus);
  const titleId = useId();
  const pressedScrim = useRef(false);
  const onScrim = (event: MouseEvent<HTMLDialogElement>) => {
    if (event.target !== event.currentTarget) return false;
    const box = event.currentTarget.getBoundingClientRect();
    return (
      event.clientX < box.left ||
      event.clientX > box.right ||
      event.clientY < box.top ||
      event.clientY > box.bottom
    );
  };
  return (
    <dialog
      ref={ref}
      className={cx('dialog', size === 'wide' && 'dialog--wide', className)}
      aria-labelledby={titleId}
      onCancel={onCancel}
      onMouseDown={
        onBackdropClick === undefined
          ? undefined
          : (event) => {
              pressedScrim.current = onScrim(event);
            }
      }
      onClick={
        onBackdropClick === undefined
          ? undefined
          : (event) => {
              if (pressedScrim.current && onScrim(event)) onBackdropClick();
            }
      }
    >
      <h2 className={cx('dialog__title', mono === true && 'mono')} id={titleId}>
        {title}
      </h2>
      {lede !== undefined ? <p className="dialog__lede">{lede}</p> : null}
      {children}
      {actions !== undefined ? <div className="dialog__actions">{actions}</div> : null}
    </dialog>
  );
}
