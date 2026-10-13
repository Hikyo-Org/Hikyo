import { useEffect, useId, useState, type ReactNode } from 'react';
import { useLocation, useSearchParams } from 'react-router';

import { writeClipboard } from '../app/clipboard.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Field } from '../ui/Field.tsx';
import { Glyph } from '../ui/Glyph.tsx';
import { SidePanel } from '../ui/SidePanel.tsx';
import { TabPanel, Tabs } from '../ui/Tabs.tsx';

/**
 * The sectioned-surface parts every chrome settings surface is built from
 * (#60, locked prototype app-chrome iteration 15).
 *
 * A sectioned surface is tabs: one `ui/Tabs` row, the selected section in `?tab=` like
 * machine access, and an old `#section` link opening the tab that holds it.
 */

type TabSection = {
  /** The panel's element id, e.g. `project-identity`; the tab is the id without the page prefix. */
  readonly id: string;
  readonly label: string;
  /** Element ids inside this section that older `#fragment` links point at. */
  readonly anchors?: readonly string[];
  readonly content: ReactNode;
};

/** A URL fragment decoded, or undefined when it is empty or malformed (`#%`): a bad link opens the first tab rather than throwing. */
function safeDecode(fragment: string): string | undefined {
  if (fragment === '') return undefined;
  try {
    return decodeURIComponent(fragment);
  } catch {
    return undefined;
  }
}

/**
 * useTabParam keeps the selected tab in `?tab=` so a reload or a shared link
 * keeps it. The first tab is the default and leaves no param; an unknown value
 * is the first tab; `fromHash` maps an older `#fragment` link to its tab.
 */
export function useTabParam<Id extends string>(
  ids: readonly Id[],
  fromHash?: (hash: string) => Id | undefined,
): [Id, (next: Id) => void] {
  const [search, setSearch] = useSearchParams();
  const { hash } = useLocation();
  const first = ids[0] as Id;
  const requested = search.get('tab');
  const fragment = safeDecode(hash.slice(1));
  const tab =
    ids.find((id) => id === requested) ??
    (fragment === undefined ? undefined : fromHash?.(fragment)) ??
    first;
  const setTab = (next: Id) =>
    setSearch(
      (current) => {
        const params = new URLSearchParams(current);
        if (next === first) params.delete('tab');
        else params.set('tab', next);
        return params;
      },
      { replace: true },
    );
  return [tab, setTab];
}

/**
 * SectionTabs renders a sectioned page body: a tab per section, or the lone
 * section bare (one section has nothing to switch to). Every section stays
 * mounted and the others are `hidden`, so a half-filled form survives a tab
 * switch and an anchored element still exists to scroll to.
 */
export function SectionTabs({
  label,
  idPrefix,
  sections,
}: {
  label: string;
  /** The page prefix shared by the section ids (`project` for `project-identity`). */
  idPrefix: string;
  sections: readonly (TabSection | false | null | undefined)[];
}) {
  const shown = sections.filter((section): section is TabSection => Boolean(section));
  const key = (id: string) => (id.startsWith(`${idPrefix}-`) ? id.slice(idPrefix.length + 1) : id);
  const [tab, setTab] = useTabParam(
    shown.map((section) => key(section.id)),
    (hash) => {
      const owner = shown.find((section) => section.id === hash || section.anchors?.includes(hash));
      return owner === undefined ? undefined : key(owner.id);
    },
  );
  const { hash } = useLocation();
  // The browser's own fragment scroll ran before the tab was shown; redo it.
  useEffect(() => {
    const fragment = safeDecode(hash.slice(1));
    if (fragment !== undefined) document.getElementById(fragment)?.scrollIntoView({ block: 'start' });
  }, [hash]);

  if (shown.length < 2) return <>{shown[0]?.content}</>;
  return (
    <>
      <Tabs
        label={label}
        idPrefix={idPrefix}
        tabs={shown.map((section) => ({ id: key(section.id), label: section.label }))}
        selected={tab}
        onSelect={setTab}
      />
      <TabPanel idPrefix={idPrefix} selected={tab}>
        {shown.map((section) => (
          <div key={section.id} className="tabpanel__section" hidden={key(section.id) !== tab}>
            {section.content}
          </div>
        ))}
      </TabPanel>
    </>
  );
}

/**
 * Panel is one section: a card with an h2, the body of one tab.
 *
 * `tabIndex={-1}` so a fragment link or an error can move FOCUS to the section
 * and not only the viewport.
 */
export function Panel({
  id,
  title,
  danger = false,
  question = false,
  tight = false,
  children,
}: {
  id: string;
  title: string;
  danger?: boolean;
  /** Tighter row gap for dense read-mostly panels (profile, metadata). */
  tight?: boolean;
  /**
   * A panel that poses an open question rather than presenting a decision.
   * It is drawn with a dashed boundary and no fill so it never reads as one of
   * the settled cards beside it, the distinction is the point of the card.
   */
  question?: boolean;
  children: ReactNode;
}) {
  const variant = danger ? ' panel--danger' : question ? ' panel--question' : '';
  const density = tight ? ' panel--tight' : '';
  return (
    <section className={`card panel${variant}${density}`} id={id} tabIndex={-1}>
      <h2>{title}</h2>
      {children}
    </section>
  );
}

/**
 * PanelForm is a button that opens its form in the side panel. By default the
 * opener stays usable in the read-only demo, so a visitor can read the form;
 * its submit is still disabled there.
 */
export function PanelForm({
  opener,
  title = opener,
  variant = 'secondary',
  readOnlySafe = true,
  children,
}: {
  opener: string;
  title?: string;
  variant?: 'primary' | 'secondary' | 'danger';
  readOnlySafe?: boolean;
  /** The form, or a render function handed the panel's close. */
  children: ReactNode | ((close: () => void) => ReactNode);
}) {
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  return (
    <>
      <div className="actions">
        <Button type="button" variant={variant} readOnlySafe={readOnlySafe} onClick={() => setOpen(true)}>
          {opener}
        </Button>
      </div>
      {open ? (
        <SidePanel title={title} onClose={close}>
          {typeof children === 'function' ? children(close) : children}
        </SidePanel>
      ) : null}
    </>
  );
}

/**
 * TypedNameConfirm is the danger-zone gate: the destructive button stays
 * disabled until the exact name is typed, inline, never behind a browser
 * `confirm()`.
 *
 * Inline for the reason the prototype gives: a native dialog cannot say WHAT
 * is about to happen in the product's own words, cannot be styled to the
 * forced-colors contract, and is dismissed by the same reflex that dismisses
 * every other one.
 */
export function TypedNameConfirm({
  label,
  expect,
  action,
  hint,
  busy,
  onConfirm,
}: {
  label: string;
  expect: string | null;
  action: string;
  hint: ReactNode;
  busy: boolean;
  onConfirm: () => void;
}) {
  const id = useId();
  const [typed, setTyped] = useState('');
  const armed = expect !== null && typed === expect;

  return (
    <div className="danger-zone">
      <Field id={id} label={label} hint={hint} aria-describedby={`${id}-state`}>
        {(control) => (
          <input
            {...control}
            value={typed}
            disabled={expect === null}
            autoComplete="off"
            spellCheck={false}
            placeholder={expect === null ? undefined : expect}
            onChange={(event) => setTyped(event.target.value)}
          />
        )}
      </Field>
      {/* The armed state is TEXT, not only a disabled attribute and a colour:
          "why is this button dead" must be answerable without seeing it. */}
      <p className="danger-zone__state mono" id={`${id}-state`} role="status">
        {expect === null
          ? `The expected name is not available. ${action} stays disabled.`
          : armed
          ? `The name matches. ${action} is now possible.`
          : `Type ${expect} exactly to enable ${action.toLowerCase()}.`}
      </p>
      <Button
        type="button"
        variant="danger"
        disabled={!armed || busy}
        onClick={onConfirm}
      >
        {action}
      </Button>
    </div>
  );
}

/**
 * ConsequencesDialog is the impact ceremony a destructive, remotely operable
 * operation passes through before it commits (#503): it names the exact
 * operation and its irreversible consequences, keeps its danger button INSIDE
 * the dialog (never on an always-rendered surface, which trips the forced-colors
 * contrast contract), and shows the busy state and any refusal inline.
 *
 * Reauthentication is bound at the session level, a 403 surfaces the step-up
 * banner, so the dialog itself carries only consequence and confirmation.
 */
export function ConsequencesDialog({
  title,
  confirmLabel,
  busyLabel,
  busy,
  failure,
  onCancel,
  onConfirm,
  children,
}: {
  title: string;
  confirmLabel: string;
  busyLabel: string;
  busy: boolean;
  failure: string | null;
  onCancel: () => void;
  onConfirm: () => void;
  children: ReactNode;
}) {
  return (
    <Dialog
      title={title}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onCancel();
      }}
      actions={
        <>
          <Button type="button" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button type="button" variant="danger" onClick={onConfirm} disabled={busy}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {/* The consequence copy is block content (the callers pass paragraphs),
          so it stays in the body wearing the lede's type; Dialog's `lede` prop
          renders inside a <p> and cannot carry a <p>. */}
      <div className="dialog__lede">{children}</div>
      {busy ? <p role="status">{busyLabel}</p> : null}
      {failure === null ? null : <Alert>{failure}</Alert>}
    </Dialog>
  );
}

/**
 * Explain is the `(?)` disclosure from iteration 12. Native details owns its
 * open state and keyboard semantics; React has no second copy to synchronize.
 *
 * A title-attribute tooltip was rejected there and stays rejected: it is dead
 * weight on touch, and this is a mobile-first product. The `title` is kept as
 * well because a desktop hover costs nothing, but nothing depends on it.
 */
export function Explain({ label, text }: { label: string; text: string }) {
  return (
    <details className="explain">
      <summary
        className="explain__toggle"
        aria-label={`Explain ${label}`}
        title={text}
      >
        <Glyph name="question" />
      </summary>
      <p className="explain__text">{text}</p>
    </details>
  );
}

/**
 * DisplayOnceCopy is the one copy control for a value the server will never
 * show again (recovery codes, an authenticator seed, an invitation or reset
 * authority). The outcome is text with a glyph, never colour alone, and a
 * refused clipboard says so instead of pretending.
 */
export function DisplayOnceCopy({ value, success }: { value: string; success: string }) {
  const [status, setStatus] = useState<string | null>(null);

  return (
    <>
      <div className="actions">
        <Button
          type="button"
          onClick={async () => {
            const result = await writeClipboard(value);
            setStatus(
              result === 'ok'
                ? success
                : 'This browser refused clipboard access, so nothing was copied.',
            );
          }}
        >
          Copy
        </Button>
      </div>
      {status === null ? null : (
        <p className="notice" role="status">{/* markup-check: copy receipt, not feedback */}
          <Glyph name="copy" className="alert__glyph" />
          <span>{status}</span>
        </p>
      )}
    </>
  );
}
