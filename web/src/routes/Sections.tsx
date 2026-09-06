import { useId, useState, type ReactNode } from 'react';

import { writeClipboard } from '../app/clipboard.ts';
import { useModalDialog } from './useModalDialog.ts';

/**
 * The sectioned-surface parts every chrome settings surface is built from
 * (#60, locked prototype app-chrome iteration 15).
 *
 * Iteration 9 made the sticky jump index the pattern for EVERY sectioned
 * surface rather than only for account & security, so it lives here once. The
 * index is plain anchors: the browser already scrolls to a fragment, moves
 * focus there and puts the section in the history, and every line of script
 * that reimplements that is a line that can get focus wrong.
 */

type SectionRef = { readonly id: string; readonly label: string };

export function JumpIndex({ sections }: { sections: readonly SectionRef[] }) {
  return (
    <nav className="jump" aria-label="Sections on this page">
      <ul>
        {sections.map((section) => (
          <li key={section.id}>
            <a className="jump__link" href={`#${section.id}`}>
              {section.label}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}

/**
 * Panel is one section: a card with an h2 that the jump index points at.
 *
 * `tabIndex={-1}` so the fragment jump moves FOCUS to the section and not only
 * the viewport, without it a keyboard user's next Tab continues from wherever
 * they were, which on a long settings page is the wrong end of the document.
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

/** Alert is a refusal: text, a glyph, and `role=alert` so it is announced. */
export function Alert({ children }: { children: ReactNode }) {
  return (
    <p className="alert" role="alert">
      <span className="alert__glyph" aria-hidden="true">
        !
      </span>
      <span>{children}</span>
    </p>
  );
}

/**
 * Done is post-action feedback that STAYS.
 *
 * The prototype used an eight-second toast with an undo. A toast that removes
 * itself is a message a screen-reader user can miss and a keyboard user cannot
 * return to, and there is no undo behind it here: a revoke is a real
 * revocation, and re-granting is an ordinary audited grant, not an undo.
 */
export function Done({ children }: { children: ReactNode }) {
  return (
    <p className="notice" role="status">
      <span className="alert__glyph" aria-hidden="true">
        ✓
      </span>
      <span>{children}</span>
    </p>
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
      <div className="field">
        <label htmlFor={id}>{label}</label>
        <p className="danger-zone__hint">{hint}</p>
        <input
          id={id}
          value={typed}
          disabled={expect === null}
          autoComplete="off"
          spellCheck={false}
          placeholder={expect === null ? undefined : expect}
          aria-describedby={`${id}-state`}
          onChange={(event) => setTyped(event.target.value)}
        />
      </div>
      {/* The armed state is TEXT, not only a disabled attribute and a colour:
          "why is this button dead" must be answerable without seeing it. */}
      <p className="danger-zone__state mono" id={`${id}-state`} role="status">
        {expect === null
          ? `The expected name is not available. ${action} stays disabled.`
          : armed
          ? `The name matches. ${action} is now possible.`
          : `Type ${expect} exactly to enable ${action.toLowerCase()}.`}
      </p>
      <button
        type="button"
        className="btn btn--danger"
        disabled={!armed || busy}
        onClick={onConfirm}
      >
        {action}
      </button>
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
  titleId,
  title,
  confirmLabel,
  busyLabel,
  busy,
  failure,
  onCancel,
  onConfirm,
  children,
}: {
  titleId: string;
  title: string;
  confirmLabel: string;
  busyLabel: string;
  busy: boolean;
  failure: string | null;
  onCancel: () => void;
  onConfirm: () => void;
  children: ReactNode;
}) {
  const dialog = useModalDialog();
  return (
    <dialog
      className="ceremony"
      ref={dialog}
      aria-labelledby={titleId}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onCancel();
      }}
    >
      <h2 id={titleId}>{title}</h2>
      <div className="ceremony__lede">{children}</div>
      {busy ? <p role="status">{busyLabel}</p> : null}
      {failure === null ? null : <Alert>{failure}</Alert>}
      <div className="ceremony__actions">
        <button type="button" className="btn" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button type="button" className="btn btn--danger" onClick={onConfirm} disabled={busy}>
          {confirmLabel}
        </button>
      </div>
    </dialog>
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
        ?
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
      <div className="panel__actions">
        <button
          type="button"
          className="btn"
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
        </button>
      </div>
      {status === null ? null : (
        <p className="notice" role="status">
          <span className="alert__glyph" aria-hidden="true">
            ⧉
          </span>
          <span>{status}</span>
        </p>
      )}
    </>
  );
}
