import type { KeyboardEvent, ReactNode } from 'react';

import { cx } from './cx.ts';

export type TabItem<Id extends string> = { id: Id; label: ReactNode };

/**
 * APG tabs: one tab stop, arrows move and select, Home and End jump. Emits
 * the `.tabs` / `.tab` markup the machine-access page hand-writes (with its
 * ids, so the panel can name its tab), sized on the control token like every
 * other control instead of the 44px touch floor it carried.
 *
 * The panel is the caller's: render it with `TabPanel` so the `aria-controls`
 * and `aria-labelledby` pair resolves.
 */
export function Tabs<Id extends string>({
  label,
  idPrefix,
  tabs,
  selected,
  onSelect,
  className,
}: {
  label: string;
  /** Prefix for the tab and panel ids, e.g. `machine`. */
  idPrefix: string;
  tabs: readonly TabItem<Id>[];
  selected: Id;
  onSelect: (id: Id) => void;
  className?: string;
}) {
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const index = tabs.findIndex((tab) => tab.id === selected);
    const last = tabs.length - 1;
    const next =
      event.key === 'ArrowRight'
        ? (index + 1) % tabs.length
        : event.key === 'ArrowLeft'
          ? (index - 1 + tabs.length) % tabs.length
          : event.key === 'Home'
            ? 0
            : event.key === 'End'
              ? last
              : null;
    if (next === null) {
      return;
    }
    event.preventDefault();
    const target = tabs[next];
    if (target === undefined) {
      return;
    }
    onSelect(target.id);
    // Focus within THIS tablist, by position: a document-wide id lookup would
    // land on the first tablist that shares the prefix (a Docs page renders
    // several examples inline), and a generated prefix is not selector-safe.
    event.currentTarget.querySelectorAll<HTMLElement>('[role="tab"]')[next]?.focus();
  };
  return (
    <div className={cx('tabs', className)} role="tablist" aria-label={label} onKeyDown={onKeyDown}>
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          role="tab"
          id={`${idPrefix}-tab-${tab.id}`}
          className="tab"
          aria-selected={tab.id === selected}
          aria-controls={`${idPrefix}-panel`}
          tabIndex={tab.id === selected ? 0 : -1}
          onClick={() => onSelect(tab.id)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}

export function TabPanel({
  idPrefix,
  selected,
  children,
}: {
  idPrefix: string;
  selected: string;
  children: ReactNode;
}) {
  return (
    <div className="tabpanel" role="tabpanel" id={`${idPrefix}-panel`} aria-labelledby={`${idPrefix}-tab-${selected}`}>
      {children}
    </div>
  );
}
