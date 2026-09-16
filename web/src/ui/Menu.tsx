import { useId, type ComponentProps, type ReactNode } from 'react';

import { cx } from './cx.ts';

/**
 * An action / overflow menu built on the native Popover API. The trigger's
 * `popoverTarget` gives the panel an implicit anchor plus light-dismiss,
 * Escape-to-close, and top-layer stacking for free, no open/close state, no
 * outside-click listener (unlike the bespoke account menu in Shell.tsx). It
 * reuses the existing `.menu` / `.menu__item` classes; `.menu--pop` only swaps
 * the account menu's fixed corner offsets for anchor positioning.
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
      <div id={id} popover="auto" className={cx('menu', 'menu--pop', className)} role="menu" aria-label={label}>
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
