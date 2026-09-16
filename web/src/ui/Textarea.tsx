import { useId, type ComponentProps } from 'react';

import { cx } from './cx.ts';

/**
 * A labelled textarea. The `.field` wrapper + `<label>` + `<textarea>` the
 * screens hand-write, with the label association wired. Sized by the same
 * control token as {@link Input} (two and a half rows by default, resizable
 * vertically), so a form of mixed controls lines up.
 */
type TextareaProps = ComponentProps<'textarea'> & { label: string };

export function Textarea({ label, id, className, ...rest }: TextareaProps) {
  const generated = useId();
  const textareaId = id ?? generated;
  return (
    <div className={cx('field', className)}>
      <label htmlFor={textareaId}>{label}</label>
      <textarea id={textareaId} {...rest} />
    </div>
  );
}
