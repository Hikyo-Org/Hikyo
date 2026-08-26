import type { CSSProperties } from 'react';

export type ChromeIdentityKind = 'org' | 'project';

export type ChromeIdentity = {
  readonly hue: number;
  readonly glyph: string | null;
  readonly image: string | null;
};

export const CHROME_IDENTITY_EVENT = 'hikyo:chrome-identity-change';

const DEFAULT_IDENTITY: ChromeIdentity = { hue: 195, glyph: null, image: null };

function storageKey(kind: ChromeIdentityKind, id: string, field: string): string {
  return `hikyo.chrome-identity.${kind}.${id}.${field}`;
}

export function chromeMonogram(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return '?';
  if (words.length === 1) return words[0]?.slice(0, 2).toUpperCase() ?? '?';
  return `${words[0]?.[0] ?? ''}${words[1]?.[0] ?? ''}`.toUpperCase();
}

export function readChromeIdentity(
  kind: ChromeIdentityKind,
  id: string,
  enabled = true,
): ChromeIdentity {
  if (!enabled || typeof window === 'undefined' || id === '') return DEFAULT_IDENTITY;
  try {
    const storedHueValue = window.localStorage.getItem(storageKey(kind, id, 'hue'));
    const storedHue = storedHueValue === null ? Number.NaN : Number(storedHueValue);
    const glyph = window.localStorage.getItem(storageKey(kind, id, 'glyph'));
    const image = window.localStorage.getItem(storageKey(kind, id, 'image'));
    return {
      hue: Number.isInteger(storedHue) && storedHue >= 0 && storedHue <= 359
        ? storedHue
        : DEFAULT_IDENTITY.hue,
      glyph: glyph === null || glyph === '' ? null : glyph,
      image: image === null || image === '' ? null : image,
    };
  } catch {
    return DEFAULT_IDENTITY;
  }
}

export function writeChromeIdentity(
  kind: ChromeIdentityKind,
  id: string,
  identity: ChromeIdentity,
): void {
  if (typeof window === 'undefined' || id === '') return;
  try {
    window.localStorage.setItem(storageKey(kind, id, 'hue'), String(identity.hue));
    if (identity.glyph === null) {
      window.localStorage.removeItem(storageKey(kind, id, 'glyph'));
    } else {
      window.localStorage.setItem(storageKey(kind, id, 'glyph'), identity.glyph);
    }
    if (identity.image === null) {
      window.localStorage.removeItem(storageKey(kind, id, 'image'));
    } else {
      window.localStorage.setItem(storageKey(kind, id, 'image'), identity.image);
    }
    window.dispatchEvent(new Event(CHROME_IDENTITY_EVENT));
  } catch {
    // File previews can exceed a browser's storage quota. The current render
    // still shows the choice; the next navigation returns to the safe default.
  }
}

export function chromeIdentityStyle(identity: ChromeIdentity): CSSProperties {
  return {
    background: `oklch(0.62 0.11 ${String(identity.hue)})`,
    color: 'oklch(0.12 0.02 225)',
    ...(identity.image === null
      ? {}
      : {
          backgroundImage: `url(${identity.image})`,
          backgroundPosition: 'center',
          backgroundSize: 'cover',
        }),
  };
}

/**
 * chromeRailIdentityStyle is the rail's tile treatment, which is NOT the avatar
 * treatment above.
 *
 * The rail's job is to say which project you are in. That is a selection state,
 * and selection is the stylesheet's to draw — `.project-avatar[aria-current]`
 * fills with the accent, `.rail .avatar[aria-current]` outlines. An inline
 * style would win against both, so the ACTIVE tile is given no inline colour at
 * all; returning `{}` is the point, not an omission. An inactive tile carries
 * its identity as a quiet 18% wash, the same fraction the prototype uses, so a
 * rail of six projects reads as one selected tile among five tinted ones rather
 * than six equally loud ones.
 *
 * An uploaded image is identity regardless of selection, so it survives both.
 */
export function chromeRailIdentityStyle(
  identity: ChromeIdentity,
  active: boolean,
): CSSProperties {
  const image = identity.image === null
    ? {}
    : {
        backgroundImage: `url(${identity.image})`,
        backgroundPosition: 'center',
        backgroundSize: 'cover',
      };
  if (active) return image;
  return { background: `oklch(0.62 0.11 ${String(identity.hue)} / 0.18)`, ...image };
}

export function chromeIdentityMark(identity: ChromeIdentity, name: string): string {
  if (identity.image !== null) return '';
  return identity.glyph ?? chromeMonogram(name);
}
