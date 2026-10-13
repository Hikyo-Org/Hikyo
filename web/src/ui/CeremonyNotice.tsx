import type { ReactNode } from 'react';

import { Glyph, type GlyphName } from './Glyph.tsx';

/** A standing ceremony fact, announced politely and kept above its controls. */
export function CeremonyNotice({ children, glyph = 'alert' }: { children: ReactNode; glyph?: GlyphName }) {
  return (
    <p className="ceremony__cap" role="status">
      <Glyph name={glyph} className="alert__glyph" />
      <span>{children}</span>
    </p>
  );
}
