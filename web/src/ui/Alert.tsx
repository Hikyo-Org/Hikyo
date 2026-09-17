import type { AriaRole, ReactNode } from 'react';

type Tone = 'danger' | 'done' | 'warn' | 'info';

/**
 * Only `danger` interrupts. The other three are `role="status"`, so a screen
 * reader finishes its sentence first: a confirmation, a caveat on something
 * that already succeeded, and a standing fact are none of them urgent, and a
 * surface that assertively announced all four would train people to ignore the
 * one that matters.
 */
const TONES: Record<Tone, { className: string; role: AriaRole; glyph: string }> = {
  danger: { className: 'alert', role: 'alert', glyph: '!' },
  done: { className: 'notice', role: 'status', glyph: '✓' },
  warn: { className: 'notice', role: 'status', glyph: '!' },
  info: { className: 'notice', role: 'status', glyph: 'ℹ' },
};

/**
 * Inline feedback that STAYS. Four tones, each carrying a glyph so no state is
 * colour-only:
 * - `danger`: a refusal or failure; `.alert`, `role="alert"`, announced now.
 * - `done`: post-action confirmation; `.notice`, `role="status"`, polite.
 * - `warn`: a caveat that wants attention without being a refusal, whether it
 *   is about something that did happen (a lifetime the server shortened, a
 *   credential that joined a live one) or a state that has gone wrong on its
 *   own (a mapping whose provider group no longer exists). `.notice` with the
 *   `!` glyph: it reads as a warning without claiming an act failed.
 * - `info`: a standing fact about the surface (this project is git-managed).
 *   `.notice` with `ℹ`; nothing went wrong and nothing is asked.
 *
 * None auto-dismisses: a toast that removes itself is a message a
 * screen-reader user can miss and a keyboard user cannot return to.
 * Supersedes `Alert` and `Done` in routes/Sections.tsx.
 */
export function Alert({
  tone = 'danger',
  action,
  children,
}: {
  tone?: Tone;
  /** One action that answers the message (retry, replace); sits at the end of the row, drops under it when narrow. */
  action?: ReactNode;
  children: ReactNode;
}) {
  const { className, role, glyph } = TONES[tone];
  return (
    <div className={className} role={role}>
      <span className="alert__glyph" aria-hidden="true">
        {glyph}
      </span>
      <span className="alert__text">{children}</span>
      {action !== undefined ? <span className="alert__action">{action}</span> : null}
    </div>
  );
}
