import type { ReactNode } from 'react';

/**
 * Inline feedback that STAYS. Two tones, both carrying a glyph so no state is
 * colour-only:
 * - `danger`: a refusal or failure; `.alert`, `role="alert"`, announced now.
 * - `done`: post-action confirmation; `.notice`, `role="status"`, polite.
 *
 * Neither auto-dismisses: a toast that removes itself is a message a
 * screen-reader user can miss and a keyboard user cannot return to.
 * Supersedes `Alert` and `Done` in routes/Sections.tsx.
 */
export function Alert({
  tone = 'danger',
  action,
  children,
}: {
  tone?: 'danger' | 'done';
  /** One action that answers the message (retry, replace); sits at the end of the row, drops under it when narrow. */
  action?: ReactNode;
  children: ReactNode;
}) {
  const done = tone === 'done';
  return (
    <div className={done ? 'notice' : 'alert'} role={done ? 'status' : 'alert'}>
      <span className="alert__glyph" aria-hidden="true">
        {done ? '✓' : '!'}
      </span>
      <span className="alert__text">{children}</span>
      {action !== undefined ? <span className="alert__action">{action}</span> : null}
    </div>
  );
}
