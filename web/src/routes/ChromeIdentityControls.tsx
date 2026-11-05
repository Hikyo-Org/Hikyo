import { useContext, useEffect, useId, useRef, useState, type ReactNode } from 'react';

import { Button } from '../ui/Button.tsx';
import { Glyph } from '../ui/Glyph.tsx';
import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import { CHROME_IMAGE_ACCEPT, prepareChromeImage, type PreparedImage } from './chrome-image.ts';
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
/** A partial identity write: an absent field stays as stored; an empty mark selects the monogram. */
export type ChromeIdentityChange = { readonly hue?: number; readonly mark?: string };

export type ChromeIdentityActions = {
  readonly setIdentity: (next: ChromeIdentityChange) => Promise<void>;
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
  // Unsaved edits: the slider preview and the content text. They revert to the
  // server state on failure and clear once the server has caught up with them.
  const [draftHue, setDraftHue] = useState<number | null>(null);
  const [draftMark, setDraftMark] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const rangeRef = useRef<HTMLInputElement>(null);
  const identitySaving = useRef(false);
  const identityQueued = useRef<{
    readonly actions: ChromeIdentityActions;
    readonly next: ChromeIdentityChange;
  } | null>(null);
  // Image selections upload in the order they were picked, whichever prepares first.
  const imageTurn = useRef<Promise<void>>(Promise.resolve());

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

  // A refetch can arrive before a save settles. Only a draft the server now holds
  // clears: another field saving must not discard a drag or half-typed text.
  useResetOnChange(`${String(serverHue)}\u0000${serverMark}`, () => {
    if (draftHue === serverHue) setDraftHue(null);
    if (draftMark !== null && limitMark(draftMark) === serverMark) setDraftMark(null);
  });

  const updateIdentity = (next: ChromeIdentity) => {
    setLocal(next);
    if (prototypeMode) writeChromeIdentity(kind, identityId, next);
  };

  // One save at a time, so responses cannot arrive out of order. Each carries
  // only the fields the user changed, so it cannot overwrite another admin's
  // newer value of the other field; changes made during a save merge, the latest
  // value per field winning.
  const saveIdentity = async (change: ChromeIdentityChange) => {
    if (actions === undefined) return;
    setRefusal(null);
    identityQueued.current = { actions, next: { ...identityQueued.current?.next, ...change } };
    if (identitySaving.current) return;
    identitySaving.current = true;
    while (identityQueued.current !== null) {
      const queued = identityQueued.current;
      identityQueued.current = null;
      try {
        await queued.actions.setIdentity(queued.next);
      } catch {
        // The page's feedback already reported it.
      }
      // Settled, and the reads have refreshed: the server's value now shows. A
      // draft from an edit made since then is different, so it stays.
      const { hue, mark: sent } = queued.next;
      if (hue !== undefined) setDraftHue((draft) => (draft === hue ? null : draft));
      if (sent !== undefined) setDraftMark((draft) => (draft !== null && limitMark(draft) === sent ? null : draft));
    }
    identitySaving.current = false;
  };
  const commitHue = (hue: number) => {
    if (!identitySaving.current && hue === serverHue) {
      setDraftHue(null);
      return;
    }
    setDraftHue(hue);
    void saveIdentity({ hue });
  };
  const commitMark = (next: string) => {
    const text = limitMark(next);
    if (!identitySaving.current && text === serverMark) {
      setDraftMark(null);
      return;
    }
    setDraftMark(next);
    void saveIdentity({ mark: text });
  };
  const changeImage = async (change: (apply: ChromeIdentityActions) => Promise<void>) => {
    if (actions === undefined) return;
    setRefusal(null);
    try {
      await change(actions);
    } catch {
      // The page's feedback already reported it, and the image never changed.
    }
  };
  // Preparation starts at once, but each upload waits for every earlier
  // selection to settle: a slow SVG picked first must not commit after a PNG
  // picked later. A failed preparation settles its turn.
  const pickImage = (file: File): Promise<void> => {
    const prepared = prepareChromeImage(file).catch((): PreparedImage => ({ ok: false, message: 'That file could not be read.' }));
    const turn = imageTurn.current.then(async () => {
      const result = await prepared;
      if (!result.ok) {
        setRefusal(result.message);
        return;
      }
      const { blob, contentType } = result;
      await changeImage((apply) => apply.setImage({ blob, contentType }));
    });
    imageTurn.current = turn.catch(() => undefined);
    return turn;
  };

  // A range's native `change` fires once when a drag or key press ends, on every
  // input method. React's onChange is really `input`, which is the live preview.
  useEffect(() => {
    const range = rangeRef.current;
    if (prototypeMode || range === null) return undefined;
    const commit = () => commitHue(Number(range.value));
    range.addEventListener('change', commit);
    return () => range.removeEventListener('change', commit);
  });

  const preview = (
    <span
      className={`identity-controls__preview${kind === 'org' ? ' identity-controls__preview--org avatar' : ''}`}
      style={chromeIdentityStyle(identity)}
      role="img"
      aria-label={`${kind === 'org' ? 'Organisation' : 'Project'} identity preview: ${mark === '' ? 'uploaded image' : mark}`}
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
            {mark === '' ? 'Uploaded image' : <>Monogram <span className="mono">{mark}</span></>}, hue {String(identity.hue)}°
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
    else commitMark(glyph ?? '');
  };
  const imageCopy = prototypeMode
    ? `PNG / SVG / JPG, replaces ${kind === 'project' ? 'monogram and glyph' : 'the monogram'}`
    : `PNG, JPEG, WebP or SVG, replaces ${kind === 'project' ? 'the content' : 'the monogram'}`;

  return (
    <>
      <div className="identity-controls">
        {preview}
        <div className="identity-controls__choices">
          <div className="identity-hues" role="group" aria-label="Preset hues">
            {HUES.map((choice) => (
              <button
                key={choice}
                type="button"
                className="identity-hue"
                style={{ background: `oklch(0.62 0.11 ${String(choice)})` }}
                aria-label={`Hue ${String(choice)}`}
                aria-pressed={choice === identity.hue}
                onClick={() => chooseHue(choice)}
              />
            ))}
          </div>
          <div className="field identity-controls__range">
            <label htmlFor={rangeId}>Custom hue · {String(identity.hue)}°</label>
            <input
              id={rangeId}
              ref={rangeRef}
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
            />
          </div>
          {kind === 'project' ? (
            <div className="identity-glyphs" role="group" aria-label="Project glyph">
              <button
                type="button"
                className="identity-glyph"
                aria-label="Use monogram"
                aria-pressed={identity.glyph === null}
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
                  commitMark(draftMark);
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
          <span className="identity-upload__picker">
            <input
              id={uploadId}
              className="identity-upload__input"
              type="file"
              accept={prototypeMode ? 'image/*' : CHROME_IMAGE_ACCEPT}
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
          </span>
          {identity.image === null ? null : (
            <Button
              icon
              type="button"
              aria-label="remove image"
                onClick={() => {
                if (prototypeMode) updateIdentity({ ...local, image: null });
                else void changeImage((apply) => apply.clearImage());
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
