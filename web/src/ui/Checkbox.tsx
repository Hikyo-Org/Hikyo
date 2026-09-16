import { useId, type ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * A labelled checkbox. Emits the `.field.chk` row (input then label) the screens
 * already hand-write. ui.css draws the box on pseudo-elements and keeps the
 * input itself as the hit target: 24px on a fine pointer, the 44px touch floor
 * the pinned set asserts on a coarse one. `type` is locked to `checkbox` after
 * the spread.
 */
type CheckboxProps = ComponentProps<'input'> & { label: string };

export function Checkbox({ label, id, className, ...rest }: CheckboxProps) {
  const generated = useId();
  const inputId = id ?? generated;
  return (
    <div className={cx('field', 'chk', className)}>
      <input id={inputId} {...rest} type="checkbox" />
      <label htmlFor={inputId}>{label}</label>
    </div>
  );
}
