import { Badge, type BadgeTone } from '../ui/Badge.tsx';
import type { GlyphName } from '../ui/Glyph.tsx';

/**
 * The capability tag vocabulary: one tone and icon per capability id in the
 * closed registry (api/access.ts CAPABILITY_REGISTRY), so every checkbox
 * list and every plain-word capability listing wears the same tag Audit's
 * outcome filter does (DESIGN.md, Badge).
 *
 * `reveal` and `reveal-history` carry the danger tone because they disclose
 * secret plaintext; `read` (See) stays neutral with the same eye glyph
 * (`reveal`) accessRules/parts.tsx's PermBadge already uses for See, which
 * this map matches rather than duplicates a second opinion on. The
 * Administration-shaped atoms carry the changed tone and the settings icon
 * DESIGN.md reserves for Administration.
 */
export const CAPABILITY_TAG: Record<string, { tone: BadgeTone; icon: GlyphName }> = {
  read: { tone: 'neutral', icon: 'reveal' },
  edit: { tone: 'neutral', icon: 'edit' },
  publish: { tone: 'neutral', icon: 'arrow-up' },
  pin: { tone: 'neutral', icon: 'pin' },
  reveal: { tone: 'danger', icon: 'lock' },
  'reveal-history': { tone: 'danger', icon: 'lock' },
  'crypto-use': { tone: 'neutral', icon: 'passkey' },
  'issue-certificate': { tone: 'neutral', icon: 'passkey' },
  'audit-read': { tone: 'neutral', icon: 'history' },
  'definitions-edit': { tone: 'changed', icon: 'settings' },
  'project-settings': { tone: 'changed', icon: 'settings' },
  'manage-identities': { tone: 'changed', icon: 'settings' },
  'manage-adapters': { tone: 'changed', icon: 'settings' },
  'crypto-manage': { tone: 'changed', icon: 'settings' },
  'manage-members': { tone: 'changed', icon: 'settings' },
  'manage-projects': { tone: 'changed', icon: 'settings' },
  'credential-reset': { tone: 'changed', icon: 'passkey' },
  'backup-export': { tone: 'changed', icon: 'copy' },
  restore: { tone: 'changed', icon: 'restore' },
  'rotate-root-key': { tone: 'changed', icon: 'sync' },
  'rotate-master-key': { tone: 'changed', icon: 'sync' },
  'rotate-dek': { tone: 'changed', icon: 'sync' },
  reencrypt: { tone: 'changed', icon: 'sync' },
  'instance-config': { tone: 'changed', icon: 'settings' },
  'instance-directory': { tone: 'neutral', icon: 'link' },
  'scim-provision': { tone: 'neutral', icon: 'link' },
  'report-delivery-status': { tone: 'neutral', icon: 'workload' },
};

/**
 * A capability id as a tag: the word as the child (so it keeps its case and
 * stays the accessible name wherever this sits inside a Checkbox's `label`),
 * tone and icon from {@link CAPABILITY_TAG}. An id outside the map (there
 * should be none) falls back to a plain neutral tag rather than throwing.
 */
export function CapabilityTag({ id }: { id: string }) {
  const tag = CAPABILITY_TAG[id];
  return (
    <Badge tone={tag?.tone} icon={tag?.icon}>
      {id}
    </Badge>
  );
}
