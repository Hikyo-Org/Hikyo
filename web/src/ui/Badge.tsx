import type { ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * A status badge. Colour only echoes a state that the word beside it already
 * names; the content is the children (the word), never the tone alone.
 *
 * Tones map to the state vocabulary in DESIGN.md: `danger` for a violation or
 * refusal, `changed` for pending or drafted (slate, never red), `ok` for a
 * measured-good condition (accent hairline), `neutral` for everything else.
 * `mono` sets the badge in the value face, for identifiers and revision
 * numbers. Routes that emit `badge--warn` migrate to `changed`.
 */
type BadgeProps = ComponentProps<'span'> & {
  tone?: 'neutral' | 'danger' | 'changed' | 'ok';
  mono?: boolean;
};

export function Badge({ tone = 'neutral', mono, className, ...rest }: BadgeProps) {
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
    />
  );
}
