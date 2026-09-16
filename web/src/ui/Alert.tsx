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
export function Alert({ tone = 'danger', children }: { tone?: 'danger' | 'done'; children: ReactNode }) {
  const done = tone === 'done';
  return (
    <p className={done ? 'notice' : 'alert'} role={done ? 'status' : 'alert'}>
      <span className="alert__glyph" aria-hidden="true">
        {done ? '✓' : '!'}
      </span>
      <span>{children}</span>
    </p>
  );
}
