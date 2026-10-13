import { useEffect, useEffectEvent, useId, useRef, useState, type ReactNode, type RefObject } from 'react';

import { Button } from './Button.tsx';
import { cx } from './cx.ts';
import { Glyph } from './Glyph.tsx';

/**
 * The side panel: a routed detail surface that slides in from the right and
 * covers the content area up to the app's side menu, which stays in view. The
 * header stays pinned and only the body scrolls, so moving focus deep into the
 * body never carries the title (and the Close action in it) off screen.
 *
 * Three ways out, all calling `onClose`: the caller's own Close action in the
 * header, Escape anywhere in the document, and a pointer-down anywhere outside
 * the panel, the side menu included, except on a toast (dismissing a
 * notification leaves the panel open). Escape and outside-press both stand
 * down while a native `<dialog>` is open: a sheet the panel opened owns the
 * top layer and its own dismissal, and must not also collapse the surface
 * behind it.
 *
 * Unsaved input: an `input` or `change` event in the body (outside a nested
 * `<dialog>` or a `role="search"` lookup) marks the panel edited, for as
 * long as the edited control is still in the body: a form the caller removes
 * after saving it takes its edits with it. While edited, an outside press does nothing, and Escape or the built-in Close
 * asks "Discard unsaved changes?" in the header: Discard calls `onClose`,
 * Keep editing (focused first, and Escape) puts focus back where it was. Only these user dismissals ask: a
 * caller that closes the panel itself (after a save, a Cancel in the body)
 * unmounts it without asking. A custom `header` that closes through its own
 * link or button bypasses the question, so use it for detail panels only.
 *
 * Focus: `initialFocus` (usually the header's heading, `tabIndex={-1}`) takes
 * focus on open. On close focus returns to `opener`, snapshotted at mount
 * because the shared ref may be re-pointed while the panel is open, or to
 * `fallbackFocus()` when that element has left the document. It is restored
 * only when focus was left nowhere: a surface that replaced the panel keeps
 * the focus it took.
 *
 * Modal over the content area: while open, everything in the page's `<main>`
 * (the whole document outside the app) except the panel is `inert`, and Tab
 * wraps inside the panel. The side menu and toasts sit outside `<main>`, so
 * they stay live for the pointer: a press on the menu navigates and closes the
 * panel. Keyboard focus never leaves the panel by Tab, so the menu is not
 * tabbable while it is open. Entry slides on `--dur`, which is zero under
 * reduced motion.
 *
 * `title` is the short form for a panel a button opened (a form, a detail):
 * the panel renders the heading and a Close button itself, focuses the
 * heading, and returns focus to whatever had it when the panel mounted.
 */
const TABBABLE =
  'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, [tabindex]:not([tabindex="-1"])';

type HeaderProps =
  | {
      /** Pinned above the scrolling body: the title and the Close action. */
      header: ReactNode;
      initialFocus: RefObject<HTMLElement | null>;
      opener: RefObject<HTMLElement | null>;
      title?: undefined;
    }
  | { title: ReactNode; header?: undefined; initialFocus?: undefined; opener?: undefined };

export function SidePanel({
  label,
  header,
  title,
  onClose,
  initialFocus,
  opener,
  fallbackFocus,
  className,
  children,
}: HeaderProps & {
  /** The panel's accessible name (it is a labelled `complementary` landmark); defaults to a string `title`. */
  label?: string;
  onClose: () => void;
  fallbackFocus?: () => HTMLElement | null | undefined;
  className?: string;
  children: ReactNode;
}) {
  const panel = useRef<HTMLElement>(null);
  const ownHeading = useRef<HTMLHeadingElement>(null);
  // Read once, at the first render: the element that opened the panel.
  const ownOpener = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null);
  initialFocus ??= ownHeading;
  opener ??= ownOpener;
  // Read at close time, not captured at mount: the callbacks may close over
  // state that changed while the panel was open.
  const latest = useRef({ onClose, fallbackFocus });
  useEffect(() => {
    latest.current = { onClose, fallbackFocus };
  });
  // The controls edited in the body. Only those still in the document count:
  // a form the caller unmounted after a save is no longer unsaved input.
  const editedControls = useRef(new Set<Element>());
  const edited = () => [...editedControls.current].some((element) => element.isConnected);
  // While asking "Discard unsaved changes?": where focus was, for Keep editing.
  const [asking, setAsking] = useState<{ from: Element | null } | null>(null);
  const keep = useRef<HTMLButtonElement>(null);
  const askId = useId();
  const requestClose = () => {
    if (!edited()) onClose();
    else if (asking === null) setAsking({ from: document.activeElement });
  };
  const keepEditing = () => {
    const from = asking?.from;
    setAsking(null);
    if (from instanceof HTMLElement && from.isConnected) from.focus();
  };
  const markEdited = (event: { target: EventTarget }) => {
    // Neither a sheet the panel opened (a reauthentication, a typed
    // confirmation) nor a lookup (`role="search"`, whose question the caller
    // keeps) is the panel's own unsaved input.
    if (event.target instanceof Element && event.target.closest('dialog, [role="search"]') === null) editedControls.current.add(event.target);
  };
  // Escape answers the question with Keep editing, or asks it.
  const onEscape = useEffectEvent(() => (asking === null ? requestClose() : keepEditing()));
  useEffect(() => {
    if (asking !== null) keep.current?.focus();
  }, [asking]);

  useEffect(() => {
    const from = opener.current;
    initialFocus.current?.focus();
    return () => {
      requestAnimationFrame(() => {
        const active = document.activeElement;
        if (active !== null && active !== document.body) return;
        if (from?.isConnected === true) {
          from.focus();
          return;
        }
        latest.current.fallbackFocus?.()?.focus();
      });
    };
  }, [opener, initialFocus]);

  useEffect(() => {
    const closeOutside = (event: PointerEvent) => {
      if (
        event.target instanceof Element &&
        panel.current?.contains(event.target) !== true &&
        event.target.closest('.toast') === null &&
        document.querySelector('dialog[open]') === null &&
        !edited()
      ) {
        latest.current.onClose();
      }
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented && document.querySelector('dialog[open]') === null) {
        event.preventDefault();
        onEscape();
      }
    };
    document.addEventListener('pointerdown', closeOutside);
    document.addEventListener('keydown', closeOnEscape);
    return () => {
      document.removeEventListener('pointerdown', closeOutside);
      document.removeEventListener('keydown', closeOnEscape);
    };
  }, []);

  // Inert every sibling on the way up from the panel to the content root, so
  // nothing behind the scrim takes focus or a click. Only what this panel
  // made inert is released: a caller's own `inert` stays.
  useEffect(() => {
    const own = panel.current;
    const root = own?.closest('main') ?? document.body;
    const made: HTMLElement[] = [];
    for (let node = own; node !== null && node !== root; node = node.parentElement) {
      for (const sibling of node.parentElement?.children ?? []) {
        if (sibling !== node && sibling instanceof HTMLElement && !sibling.inert && !sibling.classList.contains('side-panel__scrim')) {
          sibling.setAttribute('inert', '');
          made.push(sibling);
        }
      }
    }
    return () => {
      for (const element of made) element.removeAttribute('inert');
    };
  }, []);

  return (
    <>
      <div className="side-panel__scrim" aria-hidden="true" />
      <aside
        ref={panel}
        className={cx('side-panel', className)}
        aria-label={label ?? (typeof title === 'string' ? title : undefined)}
        onKeyDown={(event) => {
          // Tab wraps at either end, so focus never leaves for the page.
          if (event.key !== 'Tab' || panel.current === null) return;
          const stops = [...panel.current.querySelectorAll<HTMLElement>(TABBABLE)].filter(
            (element) => element.checkVisibility?.() ?? true,
          );
          const at = stops.indexOf(document.activeElement as HTMLElement);
          const to = event.shiftKey ? (at <= 0 ? stops.at(-1) : undefined) : at === stops.length - 1 ? stops[0] : undefined;
          if (to !== undefined) {
            event.preventDefault();
            to.focus();
          }
        }}
      >
        <div className="side-panel__head">
          {title === undefined ? header : (
            <div className="side-panel__title">
              <h2 ref={ownHeading} tabIndex={-1}>{title}</h2>
              <Button type="button" readOnlySafe className="btn--icon-phone" onClick={requestClose}>
                <Glyph name="cross" /><span className="btn__label"> Close</span>
              </Button>
            </div>
          )}
          {asking === null ? null : (
            <div className="side-panel__ask" role="alertdialog" aria-labelledby={askId}>
              <p id={askId}>Discard unsaved changes?</p>
              <div className="actions">
                <Button ref={keep} type="button" readOnlySafe onClick={keepEditing}>Keep editing</Button>
                <Button type="button" variant="danger" readOnlySafe onClick={onClose}>Discard</Button>
              </div>
            </div>
          )}
        </div>
        <div className="side-panel__body" onInput={markEdited} onChange={markEdited}>
          {children}
        </div>
      </aside>
    </>
  );
}
