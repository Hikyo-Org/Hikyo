import { useLayoutEffect, useRef, type RefObject } from 'react';

/**
 * useModalDialog opens a native `<dialog>` with `showModal()` and closes it on
 * unmount, optionally putting focus on the dialog's first decision.
 *
 * The platform gives a real focus trap, an inert document behind it, Escape
 * and the top layer; a hand-rolled `role="dialog"` has to reimplement every
 * part, and the focus trap is the part everyone gets wrong. The close on
 * unmount is what makes focus RESTORATION real: the platform returns focus to
 * the element focused before `showModal()` only when the dialog is closed.
 *
 * This is the app's only copy: {@link Dialog} builds on it and the routes that
 * still own a raw `<dialog>` import it from here.
 */
export function useModalDialog(
  initialFocus?: RefObject<HTMLElement | null>,
): RefObject<HTMLDialogElement | null> {
  const dialog = useRef<HTMLDialogElement>(null);

  useLayoutEffect(() => {
    const element = dialog.current;
    if (element !== null && !element.open) {
      element.showModal();
    }
    initialFocus?.current?.focus();
    return () => {
      if (element !== null && element.open) {
        element.close();
      }
    };
  }, [initialFocus]);

  return dialog;
}
