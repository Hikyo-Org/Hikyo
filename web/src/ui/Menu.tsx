import { useId, type ComponentProps, type ReactNode } from 'react';

import { cx } from './cx.ts';

/** The enabled rows of a menu panel, in DOM order. */
function enabledItems(panel: HTMLElement): HTMLElement[] {
  return [...panel.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)')];
}

/**
 * An action / overflow menu built on the native Popover API. The trigger's
 * `popoverTarget` gives the panel an implicit anchor plus light-dismiss,
 * Escape-to-close (which also returns focus to the trigger), and top-layer
 * stacking for free, no open/close state, no outside-click listener (unlike
 * the bespoke account menu in Shell.tsx). It reuses the existing `.menu` /
 * `.menu__item` classes; `.menu--pop` only swaps the account menu's fixed
 * corner offsets for anchor positioning.
 *
 * The popover covers layering and dismissal, not the menu keyboard model
 * that `role="menu"` promises, so that part is here: opening moves focus to
 * the first enabled item, Up/Down move through the items and wrap, Home/End
 * jump, disabled rows are skipped, Escape closes and returns focus.
 */
export function Menu({
  label,
  glyph = '⋯',
  children,
  className,
}: {
  label: string;
  glyph?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  const id = useId();
  return (
    <>
      <button
        type="button"
        className="btn btn--icon"
        popoverTarget={id}
        aria-haspopup="menu"
        aria-label={label}
      >
        {glyph}
      </button>
      <div
        id={id}
        popover="auto"
        className={cx('menu', 'menu--pop', className)}
        role="menu"
        aria-label={label}
        onToggle={(event) => {
          if (event.newState === 'open') {
            enabledItems(event.currentTarget)[0]?.focus();
          }
        }}
        onKeyDown={(event) => {
          // A real Escape is the popover's own close watcher; handling it here
          // too covers a synthetic key event (the play tests) and keeps the
          // focus return to the trigger, which hidePopover() performs, in one
          // place either way.
          if (event.key === 'Escape') {
            event.preventDefault();
            event.currentTarget.hidePopover();
            return;
          }
          const items = enabledItems(event.currentTarget);
          const count = items.length;
          if (count === 0) {
            return;
          }
          const index = items.findIndex((item) => item === document.activeElement);
          const next =
            event.key === 'ArrowDown'
              ? (index + 1) % count
              : event.key === 'ArrowUp'
                ? (index - 1 + count) % count
                : event.key === 'Home'
                  ? 0
                  : event.key === 'End'
                    ? count - 1
                    : null;
          if (next === null) {
            return;
          }
          event.preventDefault();
          items[next]?.focus();
        }}
      >
        {children}
      </div>
    </>
  );
}

/**
 * One row in a {@link Menu}. Runs its `onClick`, then closes the enclosing
 * popover, so a selection dismisses the menu the way a real action menu does.
 */
export function MenuItem({ className, onClick, ...rest }: ComponentProps<'button'>) {
  return (
    <button
      type="button"
      role="menuitem"
      className={cx('menu__item', className)}
      onClick={(event) => {
        onClick?.(event);
        const popover = event.currentTarget.closest('[popover]');
        if (popover instanceof HTMLElement) {
          popover.hidePopover();
        }
      }}
      {...rest}
    />
  );
}
