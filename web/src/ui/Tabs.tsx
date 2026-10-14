import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react';

import { cx } from './cx.ts';

export type TabItem<Id extends string> = { id: Id } & (
  | {
      /** A counted tab's label is a string: its accessible name is "label, count". */
      label: string;
      /**
       * A count shown as a badge after the label. `unknown` is said as the
       * word, never as zero: a listing that failed does not know there are none.
       */
      count: number | 'unknown';
    }
  | { label: ReactNode; count?: undefined }
);

/**
 * APG tabs: one tab stop, arrows move and select, Home and End jump. Emits
 * the `.tabs` / `.tab` markup the machine-access page hand-writes (with its
 * ids, so the panel can name its tab), sized on the control token like every
 * other control instead of the 44px touch floor it carried.
 *
 * Wider than the phone breakpoint the row never wraps: where it overflows it
 * scrolls sideways, with a thin scrollbar as the cue that there is more, and
 * the row (only the row, never the page) is scrolled so the selected tab is in
 * view, so a tab restored from the URL is not hidden past the edge. At the
 * phone breakpoint the row wraps onto its own rows of pills instead, so there
 * is nothing to scroll.
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
  const list = useRef<HTMLDivElement>(null);
  const index = tabs.findIndex((tab) => tab.id === selected);
  useEffect(() => {
    const row = list.current;
    const tab = row?.querySelectorAll('[role="tab"]')[index];
    if (row === null || tab === undefined) return;
    const [r, t] = [row.getBoundingClientRect(), tab.getBoundingClientRect()];
    row.scrollLeft += Math.min(0, t.left - r.left) + Math.max(0, t.right - r.right);
  }, [index]);
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
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
    <div ref={list} className={cx('tabs', className)} role="tablist" aria-label={label} onKeyDown={onKeyDown}>
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
          // The name reads "Service accounts, 2", the pause a sighted reader
          // gets from the badge.
          aria-label={tab.count === undefined ? undefined : `${tab.label}, ${String(tab.count)}`}
          onClick={() => onSelect(tab.id)}
        >
          {tab.label}
          {tab.count === undefined ? null : (
            <span className="tab__count" data-zero={tab.count === 0 ? true : undefined}>
              {String(tab.count)}
            </span>
          )}
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
