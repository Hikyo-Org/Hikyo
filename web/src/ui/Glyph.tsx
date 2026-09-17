import type { ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * The state-vocabulary glyphs as monochrome inline SVG, one em, current
 * colour. Replaces the colour emoji the routes used for `lock` (secret) and
 * `link` (linked key), which render per platform and ignore the palette, and
 * gives the text glyphs (check, cross, delta, draft, ellipsis) one weight.
 *
 * A glyph is decorative beside the word that names the state (DESIGN.md:
 * never colour-only, never glyph-only), so it is `aria-hidden` unless a
 * `label` is given, in which case it is an image with that name.
 */
export type GlyphName = 'lock' | 'link' | 'check' | 'cross' | 'warn' | 'delta' | 'draft' | 'ellipsis';

const PATHS: Record<GlyphName, string> = {
  lock: 'M5 7V5a3 3 0 0 1 6 0v2h1v7H4V7h1zm1.5 0h3V5a1.5 1.5 0 0 0-3 0v2z',
  link: 'M6.2 9.8a2.5 2.5 0 0 0 3.5 0l2-2a2.5 2.5 0 0 0-3.5-3.5l-.7.7 1 1 .7-.7a1.1 1.1 0 0 1 1.5 1.5l-2 2a1.1 1.1 0 0 1-1.5 0zM9.8 6.2a2.5 2.5 0 0 0-3.5 0l-2 2a2.5 2.5 0 0 0 3.5 3.5l.7-.7-1-1-.7.7a1.1 1.1 0 0 1-1.5-1.5l2-2a1.1 1.1 0 0 1 1.5 0z',
  check: 'M6.5 11.2 3 7.7l1.1-1.1 2.4 2.4 5.4-5.4L13 4.7z',
  cross: 'M4.1 3 3 4.1 6.9 8 3 11.9 4.1 13 8 9.1l3.9 3.9 1.1-1.1L9.1 8 13 4.1 11.9 3 8 6.9z',
  warn: 'M7.2 3h1.6v6H7.2zM8 10.6a1 1 0 1 1 0 2 1 1 0 0 1 0-2z',
  delta: 'M8 3l5 10H3zm0 3.2L5.6 11.5h4.8z',
  draft: 'M8 3.5a4.5 4.5 0 1 1 0 9 4.5 4.5 0 0 1 0-9zm0 1.5a3 3 0 1 0 0 6 3 3 0 0 0 0-6z',
  ellipsis: 'M3 8a1.2 1.2 0 1 1 2.4 0A1.2 1.2 0 0 1 3 8zm3.8 0a1.2 1.2 0 1 1 2.4 0 1.2 1.2 0 0 1-2.4 0zm3.8 0a1.2 1.2 0 1 1 2.4 0 1.2 1.2 0 0 1-2.4 0z',
};

type GlyphProps = Omit<ComponentProps<'svg'>, 'children'> & { name: GlyphName; label?: string };

export function Glyph({ name, label, className, ...rest }: GlyphProps) {
  return (
    <svg
      className={cx('glyph', className)}
      viewBox="0 0 16 16"
      width="1em"
      height="1em"
      role={label === undefined ? undefined : 'img'}
      aria-label={label}
      aria-hidden={label === undefined ? true : undefined}
      focusable="false"
      {...rest}
    >
      <path d={PATHS[name]} />
    </svg>
  );
}
