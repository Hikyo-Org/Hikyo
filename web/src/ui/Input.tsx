import { useId, type ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * A labelled text input. Emits the `.field` wrapper + `<label>` + `<input>` the
 * screens already use, with the label association wired for free. The label is
 * required because every existing field carries one; `className` reaches the
 * wrapper so callers can add `field--inline` / `field--readonly` modifiers.
 */
type InputProps = ComponentProps<'input'> & { label: string };

export function Input({ label, id, className, ...rest }: InputProps) {
  const generated = useId();
  const inputId = id ?? generated;
  return (
    <div className={cx('field', className)}>
      <label htmlFor={inputId}>{label}</label>
      <input id={inputId} {...rest} />
    </div>
  );
}
