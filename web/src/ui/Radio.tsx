import { useId, type ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * A labelled radio. Same `.field.chk` row as {@link Checkbox}, so the two
 * share one size, one focus ring and one disabled treatment. `type` is locked
 * to `radio` after the spread; `name` groups the options.
 */
type RadioProps = ComponentProps<'input'> & { label: string; name: string };

export function Radio({ label, id, className, ...rest }: RadioProps) {
  const generated = useId();
  const inputId = id ?? generated;
  return (
    <div className={cx('field', 'chk', className)}>
      <input id={inputId} {...rest} type="radio" />
      <label htmlFor={inputId}>{label}</label>
    </div>
  );
}
