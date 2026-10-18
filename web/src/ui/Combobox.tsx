import { useId, useLayoutEffect, useRef, useState, type ComponentProps, type KeyboardEvent } from 'react';

import { cx } from './cx.ts';
import { Glyph } from './Glyph.tsx';
import './Combobox.css';

type ComboboxProps = Omit<ComponentProps<'input'>, 'value' | 'onChange' | 'type' | 'list'> & {
  value: string;
  onValueChange: (value: string) => void;
  options: readonly string[];
  /** Accessible name for the suggestion list and its open button. */
  optionsLabel: string;
  emptyMessage?: string;
};

/**
 * Editable suggestions, with manual selection: typing always keeps the exact
 * text until the caller submits it. Focus stays in the input; arrows explore,
 * Enter accepts, Escape dismisses, and Tab keeps the typed value. The native
 * popover stays above dialogs and scroll containers without a portal.
 */
export function Combobox({
  value, onValueChange, options, optionsLabel,
  emptyMessage = 'No matching options. You can use the text you entered.',
  disabled, readOnly, className, id, onKeyDown, onBlur, onClick, ...rest
}: ComboboxProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;
  const listId = `${inputId}-options`;
  const popupId = `${inputId}-popup`;
  const input = useRef<HTMLInputElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const control = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState(false);
  const [active, setActive] = useState<string | null>(null);
  const choices = [...new Set(options)].filter((option) => option !== '');
  const visible = filter
    ? choices.filter((option) => option.toLowerCase().includes(value.trim().toLowerCase()))
    : choices;
  const activeIndex = active === null ? -1 : visible.indexOf(active);
  const expanded = open && !disabled && !readOnly;

  const close = () => {
    panel.current?.hidePopover();
    setOpen(false);
    setActive(null);
  };
  const show = (filtered: boolean) => {
    if (disabled || readOnly) return;
    setFilter(filtered);
    setActive(null);
    panel.current?.showPopover();
    setOpen(true);
  };
  const select = (option: string) => {
    onValueChange(option);
    close();
    input.current?.focus();
  };

  // CSSOM positioning also works without CSS anchor positioning. Recalculate
  // on ancestor scroll, viewport resize, filtering and mobile keyboard resize.
  useLayoutEffect(() => {
    if (!expanded) {
      // The toggle event synchronizes state after an external disable/lock.
      if (open) panel.current?.hidePopover();
      return;
    }
    const place = () => {
      const box = control.current?.getBoundingClientRect();
      const list = panel.current;
      if (box === undefined || list === null) return;
      const viewport = window.visualViewport;
      const top = viewport?.offsetTop ?? 0;
      const bottom = top + (viewport?.height ?? window.innerHeight);
      const below = bottom - box.bottom;
      const above = box.top - top;
      const upward = below < box.height * 4 && above > below;
      const available = Math.max(0, (upward ? above : below) - 4);
      list.style.width = `${box.width}px`;
      list.style.maxHeight = `${Math.min(box.height * 6, available)}px`;
      list.style.left = `${box.left}px`;
      list.style.top = `${upward ? box.top - list.getBoundingClientRect().height : box.bottom}px`;
    };
    place();
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    window.visualViewport?.addEventListener('resize', place);
    window.visualViewport?.addEventListener('scroll', place);
    return () => {
      window.removeEventListener('resize', place);
      window.removeEventListener('scroll', place, true);
      window.visualViewport?.removeEventListener('resize', place);
      window.visualViewport?.removeEventListener('scroll', place);
    };
  }, [expanded, open, value, filter, options]);

  useLayoutEffect(() => {
    if (expanded && activeIndex >= 0) {
      panel.current?.querySelectorAll<HTMLElement>('[role="option"]')[activeIndex]?.scrollIntoView({ block: 'nearest' });
    }
  }, [expanded, activeIndex]);

  const handleKey = (event: KeyboardEvent<HTMLInputElement>) => {
    onKeyDown?.(event);
    if (event.defaultPrevented || event.nativeEvent.isComposing || disabled || readOnly) return;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const candidates = expanded ? visible : choices;
      if (!expanded) show(false);
      const index = expanded ? activeIndex : -1;
      const next = event.key === 'ArrowDown'
        ? Math.min(index + 1, candidates.length - 1)
        : index < 0 ? candidates.length - 1 : Math.max(0, index - 1);
      setActive(candidates[next] ?? null);
    } else if (event.key === 'Enter' && expanded) {
      // Accepting a suggestion must never also submit the enclosing form.
      event.preventDefault();
      if (activeIndex >= 0 && active !== null) select(active);
      else close();
    } else if (event.key === 'Escape' && expanded) {
      event.preventDefault();
      event.stopPropagation();
      close();
    } else if (event.key === 'Tab') {
      close();
    }
  };

  return (
    <div className="combobox" ref={control}>
      <input
        {...rest}
        id={inputId}
        ref={input}
        type="text"
        className={className}
        autoComplete="off"
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={expanded}
        aria-controls={listId}
        aria-activedescendant={expanded && activeIndex >= 0 ? `${listId}-${String(activeIndex)}` : undefined}
        value={value}
        disabled={disabled}
        readOnly={readOnly}
        onChange={(event) => {
          onValueChange(event.target.value);
          show(true);
        }}
        onKeyDown={handleKey}
        onClick={(event) => { onClick?.(event); if (!event.defaultPrevented) show(false); }}
        onBlur={(event) => { onBlur?.(event); close(); }}
      />
      <button
        type="button"
        className="combobox__toggle"
        tabIndex={-1}
        aria-label={`Show ${optionsLabel}`}
        aria-controls={listId}
        aria-expanded={expanded}
        disabled={disabled || readOnly}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => {
          input.current?.focus();
          if (expanded) close(); else show(false);
        }}
      >
        <Glyph name="chevron" />
      </button>
      <div
        ref={panel}
        id={popupId}
        popover="auto"
        className="combobox__list"
        onToggle={(event) => {
          setOpen(event.newState === 'open');
          if (event.newState === 'closed') setActive(null);
        }}
      >
        <div id={listId} role="listbox" aria-label={optionsLabel}>
          {visible.map((option, index) => (
            <div
              key={option}
              id={`${listId}-${String(index)}`}
              role="option"
              aria-selected={activeIndex === index}
              className={cx('combobox__option', className)}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => select(option)}
            >
              {option}
            </div>
          ))}
        </div>
        {visible.length === 0 ? <p className="combobox__empty" role="status">{emptyMessage}</p> : null}
      </div>
    </div>
  );
}
