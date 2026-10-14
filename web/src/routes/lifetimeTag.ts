import type { BadgeTone } from '../ui/Badge.tsx';
import type { GlyphName } from '../ui/Glyph.tsx';

/** A credential's lifetime as a tag (instance connections, SCIM credentials). */
export const LIFETIME_TAG = {
  live: { tone: 'ok', icon: 'check' },
  expired: { tone: 'neutral', icon: 'wait' },
  revoked: { tone: 'danger', icon: 'cross' },
} as const satisfies Record<string, { tone: BadgeTone; icon: GlyphName }>;
