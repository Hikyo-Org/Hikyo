import type { ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * A status badge. Colour only echoes a state that the word beside it already
 * names — the tier maps to `.badge--danger` / `.badge--warn`, and neutral is the
 * bare `.badge`. Content is the children (the word), never the tier alone.
 */
type BadgeProps = ComponentProps<'span'> & { tone?: 'neutral' | 'danger' | 'warn' };

export function Badge({ tone = 'neutral', className, ...rest }: BadgeProps) {
  return (
    <span
      className={cx('badge', tone === 'danger' && 'badge--danger', tone === 'warn' && 'badge--warn', className)}
      {...rest}
    />
  );
}
