import {
  useId,
  useRef,
  useState,
  type ComponentProps,
  type KeyboardEvent,
  type ReactNode,
  type ToggleEvent,
} from 'react';

import { cx } from './cx.ts';

/**
 * An action / overflow menu built on the native Popover API. The trigger's
 * `popoverTarget` gives the panel an implicit anchor plus light-dismiss,
 * Escape-to-close, and top-layer stacking for free, no open/close state, no
 * outside-click listener (unlike the bespoke account menu in Shell.tsx). It
 * reuses the existing `.menu` / `.menu__item` classes; `.menu--pop` only swaps
 * the account menu's fixed corner offsets for anchor positioning.
 *
 * On top of the popover sits the APG menu button keyboard contract the
 * `menu` / `menuitem` roles promise assistive technology: opening moves focus
 * to the first item, Arrow keys move with wrap, Home/End jump, Tab closes and
 * lets focus travel on, Escape and a selection close and return focus to the
 * trigger, whose `aria-expanded` mirrors the popover. Items are
 * `tabIndex={-1}` with programmatic focus, one consistent rule instead of a
 * roving index.
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
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  const onToggle = (event: ToggleEvent<HTMLDivElement>) => {
    const opened = event.newState === 'open';
    setOpen(opened);
    if (opened) {
      menuItems(event.currentTarget)[0]?.focus();
      return;
    }
    // Closed: hand focus back to the trigger, unless a light-dismiss click or
    // a Tab already moved it somewhere real. A hidden panel drops focus to the
    // body, which is the case this catches.
    const active = document.activeElement;
    if (active === null || active === document.body || event.currentTarget.contains(active)) {
      trigger.current?.focus();
    }
  };

  const onTriggerKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') {
      return;
    }
    event.preventDefault();
    // The DOM, not `open`: React state lags the toggle event by a task, and
    // `showPopover()` on a showing popover throws.
    const target = panel.current;
    if (target !== null && !target.matches(':popover-open')) {
      target.showPopover();
    }
  };

  const onPanelKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const items = menuItems(event.currentTarget);
    const index = items.findIndex((item) => item === document.activeElement);
    const target = menuFocusTarget(event.key, index, items.length);
    if (target !== null) {
      event.preventDefault();
      items[target]?.focus();
      return;
    }
    if (event.key === 'Tab') {
      // Focus the trigger first and leave the default alone: the browser then
      // tabs onward FROM the trigger, forward or backward, as the APG asks.
      trigger.current?.focus();
      event.currentTarget.hidePopover();
      return;
    }
    if (event.key === 'Escape') {
      // The popover's own close watcher would do this for a real key press,
      // but only for one: cancelling here keeps an enclosing dialog's close
      // watcher from consuming the same Escape once the menu is gone.
      event.preventDefault();
      event.currentTarget.hidePopover();
    }
  };

  return (
    <>
      <button
        ref={trigger}
        type="button"
        className="btn btn--icon"
        popoverTarget={id}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        onKeyDown={onTriggerKeyDown}
      >
        {glyph}
      </button>
      <div
        ref={panel}
        id={id}
        popover="auto"
        className={cx('menu', 'menu--pop', className)}
        role="menu"
        aria-label={label}
        onToggle={onToggle}
        onKeyDown={onPanelKeyDown}
      >
        {children}
      </div>
    </>
  );
}

function menuItems(panel: HTMLElement): readonly HTMLElement[] {
  return [...panel.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)')];
}

/**
 * menuFocusTarget maps one navigation key onto the index to focus, or null
 * for a key the menu does not own. `index` is -1 when no item has focus, so
 * ArrowDown enters at the first item and ArrowUp at the last. Pure, so the
 * wrap arithmetic is testable without a popover.
 */
export function menuFocusTarget(key: string, index: number, count: number): number | null {
  if (count === 0) {
    return null;
  }
  switch (key) {
    case 'ArrowDown':
      return index < 0 ? 0 : (index + 1) % count;
    case 'ArrowUp':
      return index < 0 ? count - 1 : (index - 1 + count) % count;
    case 'Home':
      return 0;
    case 'End':
      return count - 1;
    default:
      return null;
  }
}

/**
 * One row in a {@link Menu}. Runs its `onClick`, then closes the enclosing
 * popover, so a selection dismisses the menu the way a real action menu does.
 * Focus never Tabs into a row (the menu owns entry and movement), hence -1.
 */
export function MenuItem({ className, onClick, ...rest }: ComponentProps<'button'>) {
  return (
    <button
      type="button"
      role="menuitem"
      tabIndex={-1}
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
