import { useContext, type ComponentProps } from 'react';

import { ReadOnlyContext } from './ReadOnlyContext.ts';

import { cx } from './cx.ts';

/**
 * The button primitive. It emits exactly the `.btn` markup screens already
 * hand-write (`btn`, `btn--primary`, `btn--danger`, `btn--quiet`, `btn--icon`),
 * so migrating a screen is a tag swap and the Playwright token assertions keep
 * passing unchanged.
 *
 * Variants carry a role, not a colour: `primary` is the one action a surface
 * leads with, `secondary` (default) every other action, `danger` a destructive
 * one (named in its text, colour only echoes), `quiet` a tertiary action that
 * sits inside a row or a panel header. A variant never changes the size: quiet
 * recedes by its unfilled background only.
 *
 * One size, `--control` tall; an icon-only button is square. The smaller
 * `btn--compact` class belongs to the matrix column header's anchor and
 * summary (DESIGN.md, Button).
 *
 * `type` is deliberately not defaulted: native pass-through keeps behaviour
 * identical to a raw <button> (submit inside a form, plain button elsewhere).
 * An icon-only button carries no text, so the a11y gate needs a name, so the
 * `icon` variant makes `aria-label` required in the type rather than at runtime.
 */
type ButtonProps = ComponentProps<'button'> & {
  variant?: 'primary' | 'secondary' | 'danger' | 'quiet';
  /** Navigation or inspection that remains available inside a read-only preview. */
  readOnlySafe?: boolean;
} & ({ icon?: false } | { icon: true; 'aria-label': string });

export function Button({ variant = 'secondary', icon, className, readOnlySafe = false, ...rest }: ButtonProps) {
  const readOnly = useContext(ReadOnlyContext);
  return (
    <button
      className={cx(
        'btn',
        variant === 'primary' && 'btn--primary',
        variant === 'danger' && 'btn--danger',
        variant === 'quiet' && 'btn--quiet',
        icon === true && 'btn--icon',
        className,
      )}
      {...rest}
      disabled={rest.disabled || (readOnly && !readOnlySafe)}
    />
  );
}
