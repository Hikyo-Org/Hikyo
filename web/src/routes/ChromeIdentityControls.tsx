import { useContext, useId, useState, type ReactNode } from 'react';

import { Button } from '../ui/Button.tsx';
import { Glyph } from '../ui/Glyph.tsx';
import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import { CHROME_IMAGE_ACCEPT, prepareChromeImage } from './chrome-image.ts';
import { graphemes, limitMark, MAX_MARK_GRAPHEMES } from './chrome-mark.ts';
import {
  chromeIdentityFromApi,
  chromeIdentityMark,
  chromeIdentityStyle,
  chromeMonogram,
  readChromeIdentity,
  writeChromeIdentity,
  type ApiChromeIdentity,
  type ChromeIdentity,
} from './chrome-identity.ts';
import { useResetOnChange } from '../app/useResetOnChange.ts';

const HUES: readonly number[] = [195, 280, 60, 140, 20, 320];
const GLYPHS: readonly string[] = ['🚀', '🔐', '🌿', '📦', '🛰', '🧪'];
const prototypeMode = import.meta.env.MODE === 'prototype';

/**
 * What the page wires in outside prototype mode. Each action resolves when the
 * server accepted the change and REJECTS when it did not; the page has already
 * surfaced the error through its feedback by then, and the controls only need
 * to revert to the server state.
 */
export type ChromeIdentityActions = {
  readonly setIdentity: (next: { hue: number; mark: string | null }) => Promise<void>;
  readonly setImage: (image: { blob: Blob; contentType: string }) => Promise<void>;
  readonly clearImage: () => Promise<void>;
};

export function ChromeIdentityControls({
  identityId,
  name,
  kind,
  identity: serverIdentity,
  imageUrl = null,
  actions,
  children,
}: {
  readonly identityId: string;
  readonly name: string;
  readonly kind: 'org' | 'project';
  /** The identity as the API delivers it; absent until the entity has loaded. */
  readonly identity?: ApiChromeIdentity | undefined;
  /** Builds the same-origin image URL from the image version; null in a remote workspace. */
  readonly imageUrl?: ((version: string) => string) | null;
  readonly actions?: ChromeIdentityActions | undefined;
  readonly children?: ReactNode;
}) {
  const rangeId = useId();
  const uploadId = useId();
  const markId = useId();
  const readOnly = useContext(ReadOnlyContext);
  const [local, setLocal] = useState<ChromeIdentity>(() =>
    readChromeIdentity(kind, identityId, prototypeMode),
  );
  // Uncommitted edits: the slider preview and the content text. They revert to
  // the server state on failure and clear when the server value changes.
  const [draftHue, setDraftHue] = useState<number | null>(null);
  const [draftMark, setDraftMark] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const serverHue = serverIdentity?.hue ?? 195;
  const serverMark = serverIdentity?.mark ?? '';
  const serverVersion = serverIdentity?.image_version ?? '';
  const editable = !prototypeMode && !readOnly && actions !== undefined && serverIdentity !== undefined;

  const identity: ChromeIdentity = prototypeMode
    ? local
    : chromeIdentityFromApi(
        {
          hue: draftHue ?? serverHue,
          mark: draftMark === null ? serverMark : limitMark(draftMark),
          image_version: serverVersion === '' ? null : serverVersion,
        },
        imageUrl,
      );
  const mark = chromeIdentityMark(identity, name);

  // The consumer keeps this instance mounted across an id/kind change (no
  // `key`, and its own file is out of reach), so re-seed the local identity
  // during render when the watched pair changes: the documented alternative
  // to a reset effect.
  useResetOnChange(`${kind}:${identityId}`, () => {
    setLocal(readChromeIdentity(kind, identityId, prototypeMode));
    setDraftHue(null);
    setDraftMark(null);
    setRefusal(null);
  });
  useResetOnChange(`${String(serverHue)}:${serverMark}:${serverVersion}`, () => {
    setDraftHue(null);
    setDraftMark(null);
  });

  const updateIdentity = (next: ChromeIdentity) => {
    setLocal(next);
    if (prototypeMode) writeChromeIdentity(kind, identityId, next);
  };

  const run = async (change: (apply: ChromeIdentityActions) => Promise<void>) => {
    if (actions === undefined) return;
    setSaving(true);
    setRefusal(null);
    try {
      await change(actions);
    } catch {
      setDraftHue(null);
      setDraftMark(null);
    } finally {
      setSaving(false);
    }
  };
  const commitHue = (hue: number) => {
    if (hue === serverHue) {
      setDraftHue(null);
      return;
    }
    void run((apply) => apply.setIdentity({ hue, mark: serverMark === '' ? null : serverMark }));
  };
  const commitMark = (next: string | null) => {
    if ((next ?? '') === serverMark) {
      setDraftMark(null);
      return;
    }
    void run((apply) => apply.setIdentity({ hue: serverHue, mark: next }));
  };
  const pickImage = async (file: File) => {
    const prepared = await prepareChromeImage(file);
    if (!prepared.ok) {
      setRefusal(prepared.message);
      return;
    }
    void run((apply) => apply.setImage({ blob: prepared.blob, contentType: prepared.contentType }));
  };

  const preview = (
    <span
      className={`identity-controls__preview${kind === 'org' ? ' identity-controls__preview--org avatar' : ''}`}
      style={chromeIdentityStyle(identity)}
      aria-label={`${kind === 'org' ? 'Organisation' : 'Project'} identity preview: ${mark}`}
    >
      {mark}
    </span>
  );

  // Without actions (or under a read-only preview) outside prototype mode the
  // identity is the preview and nothing that pretends to change it. Prototype
  // mode keeps its browser-local version.
  if (!prototypeMode && !editable) {
    return (
      <>
        <div className="identity-controls identity-controls--readonly">
          {preview}
          <p className="settings-row__detail">
            Monogram <span className="mono">{mark}</span>, hue {String(identity.hue)}°
          </p>
        </div>
        {children}
      </>
    );
  }

  const chooseHue = (hue: number) => {
    if (prototypeMode) updateIdentity({ ...local, hue });
    else commitHue(hue);
  };
  const chooseGlyph = (glyph: string | null) => {
    if (prototypeMode) updateIdentity({ ...local, glyph });
    else commitMark(glyph);
  };
  const imageCopy = prototypeMode
    ? `PNG / SVG / JPG, replaces ${kind === 'project' ? 'monogram and glyph' : 'the monogram'}`
    : `PNG, JPEG, WebP or SVG, replaces ${kind === 'project' ? 'the content' : 'the monogram'}`;

  return (
    <>
      <div className="identity-controls">
        {preview}
        <div className="identity-controls__choices">
          <div className="identity-hues" aria-label="Preset hues">
            {HUES.map((choice) => (
              <button
                key={choice}
                type="button"
                className="identity-hue"
                style={{ background: `oklch(0.62 0.11 ${String(choice)})` }}
                aria-label={`Hue ${String(choice)}`}
                aria-pressed={choice === identity.hue}
                disabled={saving}
                onClick={() => chooseHue(choice)}
              />
            ))}
          </div>
          <div className="field identity-controls__range">
            <label htmlFor={rangeId}>Custom hue · {String(identity.hue)}°</label>
            <input
              id={rangeId}
              className="identity-hue-range"
              type="range"
              min="0"
              max="359"
              value={identity.hue}
              onChange={(event) => {
                const hue = Number(event.currentTarget.value);
                if (prototypeMode) updateIdentity({ ...local, hue });
                else setDraftHue(hue);
              }}
              // The slider previews live and saves once, when the drag or key press ends.
              onPointerUp={prototypeMode ? undefined : (event) => commitHue(Number(event.currentTarget.value))}
              onKeyUp={prototypeMode ? undefined : (event) => commitHue(Number(event.currentTarget.value))}
            />
          </div>
          {kind === 'project' ? (
            <div className="identity-glyphs" aria-label="Project glyph">
              <button
                type="button"
                className="identity-glyph"
                aria-label="Use monogram"
                aria-pressed={identity.glyph === null}
                disabled={saving}
                onClick={() => chooseGlyph(null)}
              >
                {chromeMonogram(name)}
              </button>
              {GLYPHS.map((choice) => (
                <button
                  key={choice}
                  type="button"
                  className="identity-glyph"
                  aria-label={`Use ${choice} glyph`}
                  aria-pressed={identity.glyph === choice}
                  disabled={saving}
                  onClick={() => chooseGlyph(choice)}
                >
                  {choice}
                </button>
              ))}
            </div>
          ) : null}
          {prototypeMode ? null : (
            <div className="field identity-controls__mark">
              <label htmlFor={markId}>
                Content (up to {String(MAX_MARK_GRAPHEMES)} characters, empty uses the monogram)
              </label>
              <input
                id={markId}
                type="text"
                autoComplete="off"
                value={draftMark ?? serverMark}
                onChange={(event) =>
                  setDraftMark(graphemes(event.currentTarget.value).slice(0, MAX_MARK_GRAPHEMES).join(''))
                }
                onBlur={() => {
                  if (draftMark === null) return;
                  const next = limitMark(draftMark);
                  commitMark(next === '' ? null : next);
                }}
              />
            </div>
          )}
        </div>
      </div>
      {children}
      <div className={identity.image === null ? 'settings-row identity-upload' : 'settings-row settings-row--stacked identity-upload'}>
        <div className="settings-row__copy">
          <span className="settings-row__title">Image</span>
          <span className="settings-row__detail">{imageCopy}</span>
          {prototypeMode ? null : (
            <span className="settings-row__detail">SVG is converted to PNG in your browser before upload.</span>
          )}
        </div>
        {identity.image === null ? <span className="settings-row__spacer" /> : null}
        <div className="actions">
          <input
            id={uploadId}
            className="visually-hidden"
            type="file"
            accept={prototypeMode ? 'image/*' : CHROME_IMAGE_ACCEPT}
            disabled={saving}
            onChange={(event) => {
              const input = event.currentTarget;
              const file = input.files?.[0];
              if (file === undefined) return;
              if (prototypeMode) {
                const reader = new FileReader();
                reader.addEventListener('load', () => {
                  if (typeof reader.result === 'string') {
                    updateIdentity({ ...local, image: reader.result });
                  }
                });
                reader.readAsDataURL(file);
              } else {
                void pickImage(file);
              }
              // Picking the same file again must still fire a change.
              input.value = '';
            }}
          />
          <label className="btn" htmlFor={uploadId}>
            {identity.image === null ? 'upload' : 'replace'}
          </label>
          {identity.image === null ? null : (
            <Button
              icon
              type="button"
              aria-label="remove image"
              disabled={saving}
              onClick={() => {
                if (prototypeMode) updateIdentity({ ...local, image: null });
                else void run((apply) => apply.clearImage());
              }}
            >
              <Glyph name="cross" />
            </Button>
          )}
        </div>
      </div>
      {refusal === null ? null : <p role="alert" className="settings-note">{refusal}</p>}
      {prototypeMode ? (
        <p className="settings-note">
          {kind === 'project'
            ? 'Monogram + hue is the default; glyph and image are opt-in (image wins). The custom-hue slider keeps every choice on the brand formula: same lightness and chroma, hue free.'
            : 'The org avatar stays a circle: identity circles are one of the two shapes the 999px pill is reserved for.'}
        </p>
      ) : null}
    </>
  );
}
