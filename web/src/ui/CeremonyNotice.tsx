import type { ReactNode } from 'react';

/** A standing ceremony fact, announced politely and kept above its controls. */
export function CeremonyNotice({ children, glyph = '!' }: { children: ReactNode; glyph?: string }) {
  return (
    <p className="ceremony__cap" role="status">
      <span className="alert__glyph" aria-hidden="true">
        {glyph}
      </span>
      <span>{children}</span>
    </p>
  );
}
