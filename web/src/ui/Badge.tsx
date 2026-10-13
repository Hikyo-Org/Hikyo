import type { ComponentProps } from 'react';

import { cx } from './cx.ts';
import { Glyph, type GlyphName } from './Glyph.tsx';

export type BadgeTone = 'neutral' | 'danger' | 'changed' | 'ok';

/** The icon a tone carries when the call site names none (DESIGN.md, Badge). */
const TONE_ICON: Record<BadgeTone, GlyphName | undefined> = {
  neutral: undefined,
  ok: 'check',
  changed: 'history',
  danger: 'alert',
};

/**
 * A status tag. Colour and icon only echo a state that the word already
 * names; the content is the children (the word), never the tone alone.
 *
 * Tones map to the state vocabulary in DESIGN.md: `danger` for a violation or
 * refusal, `changed` for pending or drafted (slate, never red), `ok` for a
 * measured-good condition (accent), `neutral` for everything else. Each tone
 * is a soft fill with its own ink and border; the word is set in capitals by
 * CSS, so the source text (and what tests and screen readers read) keeps its
 * case.
 *
 * `icon` names the glyph before the word; a tone supplies one by default
 * (ok: check, changed: history, danger: alert) and `icon={false}` drops it.
 * `mono` is an identifier (key name, revision, id, URL), not a tag: value
 * face, exact case, no default icon.
 */
type BadgeProps = ComponentProps<'span'> & {
  tone?: BadgeTone;
  mono?: boolean;
  icon?: GlyphName | false;
};

export function Badge({ tone = 'neutral', mono, icon, className, children, ...rest }: BadgeProps) {
  const glyph = icon === false ? undefined : (icon ?? (mono === true ? undefined : TONE_ICON[tone]));
  return (
    <span
      className={cx(
        'badge',
        tone === 'danger' && 'badge--danger',
        tone === 'changed' && 'badge--changed',
        tone === 'ok' && 'badge--ok',
        mono === true && 'mono',
        className,
      )}
      {...rest}
    >
      {glyph === undefined ? null : <Glyph name={glyph} />}
      {children}
    </span>
  );
}
