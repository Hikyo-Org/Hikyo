import type { ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * The button primitive. It emits exactly the `.btn` markup screens already
 * hand-write (`btn`, `btn--primary`, `btn--icon`), so migrating a screen is a
 * tag swap and the Playwright token assertions keep passing unchanged.
 *
 * `type` is deliberately not defaulted: native pass-through keeps behaviour
 * identical to a raw <button> (submit inside a form, plain button elsewhere).
 * An icon-only button carries no text, so the a11y gate needs a name — the
 * `icon` variant makes `aria-label` required in the type rather than at runtime.
 */
type ButtonProps = ComponentProps<'button'> & { variant?: 'primary' | 'secondary' } & (
    | { icon?: false }
    | { icon: true; 'aria-label': string }
  );

export function Button({ variant = 'secondary', icon, className, ...rest }: ButtonProps) {
  return (
    <button
      className={cx('btn', variant === 'primary' && 'btn--primary', icon === true && 'btn--icon', className)}
      {...rest}
    />
  );
}
