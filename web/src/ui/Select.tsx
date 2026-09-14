import { useId, type ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * A labelled native select. Emits the `.field` wrapper + `<label>` + `<select>`;
 * the native control keeps keyboard and platform behaviour, so this stays a
 * migration swap rather than a rebuilt listbox. Options are the children.
 */
type SelectProps = ComponentProps<'select'> & { label: string };

export function Select({ label, id, className, children, ...rest }: SelectProps) {
  const generated = useId();
  const selectId = id ?? generated;
  return (
    <div className={cx('field', className)}>
      <label htmlFor={selectId}>{label}</label>
      <select id={selectId} {...rest}>
        {children}
      </select>
    </div>
  );
}
