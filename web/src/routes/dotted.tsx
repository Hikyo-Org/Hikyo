import { Fragment, type ReactNode } from 'react';

import { Sep } from '../ui/Sep.tsx';

/** Renders a " · "-joined label with the styled separator; the string form stays for options and names. */
export function dotted(text: string): ReactNode {
  return text.split(' · ').map((part, index) => (
    <Fragment key={index}>
      {index === 0 ? null : <Sep />}
      {part}
    </Fragment>
  ));
}
