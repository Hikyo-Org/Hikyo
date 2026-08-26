import { useEffect, useId, useState, type ReactNode } from 'react';

import {
  chromeIdentityMark,
  chromeIdentityStyle,
  chromeMonogram,
  readChromeIdentity,
  writeChromeIdentity,
  type ChromeIdentity,
} from './chrome-identity.ts';

const HUES: readonly number[] = [195, 280, 60, 140, 20, 320];
const GLYPHS: readonly string[] = ['🚀', '🔐', '🌿', '📦', '🛰', '🧪'];
const prototypeMode = import.meta.env.MODE === 'prototype';

export function ChromeIdentityControls({
  identityId,
  name,
  kind,
  children,
}: {
  readonly identityId: string;
  readonly name: string;
  readonly kind: 'org' | 'project';
  readonly children?: ReactNode;
}) {
  const rangeId = useId();
  const uploadId = useId();
  const [identity, setIdentity] = useState<ChromeIdentity>(() =>
    readChromeIdentity(kind, identityId, prototypeMode),
  );
  const mark = chromeIdentityMark(identity, name);

  useEffect(() => {
    setIdentity(readChromeIdentity(kind, identityId, prototypeMode));
  }, [identityId, kind]);

  const updateIdentity = (next: ChromeIdentity) => {
    setIdentity(next);
    if (prototypeMode) writeChromeIdentity(kind, identityId, next);
  };

  return (
    <>
      <div className="identity-controls">
        <span
          className={`identity-controls__preview${kind === 'org' ? ' identity-controls__preview--org avatar' : ''}`}
          style={chromeIdentityStyle(identity)}
          aria-label={`${kind === 'org' ? 'Organisation' : 'Project'} identity preview: ${mark}`}
        >
          {mark}
        </span>
        <div className="identity-controls__choices">
          <div className="identity-hues" aria-label="Preset hues">
            {HUES.map((choice) => (
              <button
                key={choice}
                type="button"
                disabled={!prototypeMode}
                className="identity-hue"
                style={{ background: `oklch(0.62 0.11 ${String(choice)})` }}
                aria-label={`Hue ${String(choice)}`}
                aria-pressed={choice === identity.hue}
                onClick={() => updateIdentity({ ...identity, hue: choice })}
              />
            ))}
          </div>
          <div className="field identity-controls__range">
            <label htmlFor={rangeId}>Custom hue · {String(identity.hue)}°</label>
            <input
              id={rangeId}
              className="identity-hue-range"
              type="range"
              disabled={!prototypeMode}
              min="0"
              max="359"
              value={identity.hue}
              onChange={(event) =>
                updateIdentity({ ...identity, hue: Number(event.currentTarget.value) })
              }
            />
          </div>
          {kind === 'project' ? (
            <div className="identity-glyphs" aria-label="Project glyph">
              <button
                type="button"
                disabled={!prototypeMode}
                className="identity-glyph"
                aria-label="Use monogram"
                aria-pressed={identity.glyph === null}
                onClick={() => updateIdentity({ ...identity, glyph: null })}
              >
                {chromeMonogram(name)}
              </button>
              {GLYPHS.map((choice) => (
                <button
                  key={choice}
                  type="button"
                  disabled={!prototypeMode}
                  className="identity-glyph"
                  aria-label={`Use ${choice} glyph`}
                  aria-pressed={identity.glyph === choice}
                  onClick={() => updateIdentity({ ...identity, glyph: choice })}
                >
                  {choice}
                </button>
              ))}
            </div>
          ) : null}
        </div>
      </div>
      {children}
      <div className="settings-row identity-upload">
        <div className="settings-row__copy">
          <span className="settings-row__title">Image</span>
          <span className="settings-row__detail">
            PNG / SVG / JPG, replaces {kind === 'project' ? 'monogram and glyph' : 'the monogram'}
          </span>
        </div>
        <span className="settings-row__spacer" />
        <input
          id={uploadId}
          className="visually-hidden"
          type="file"
          disabled={!prototypeMode}
          accept="image/*"
          onChange={(event) => {
            const file = event.currentTarget.files?.[0];
            if (file === undefined) return;
            const reader = new FileReader();
            reader.addEventListener('load', () => {
              if (typeof reader.result === 'string') {
                updateIdentity({ ...identity, image: reader.result });
              }
            });
            reader.readAsDataURL(file);
          }}
        />
        {/* aria-disabled alone leaves a control that looks and focuses like a
            live button; the class is what tells a sighted reader it is not. */}
        <label
          className={prototypeMode ? 'btn' : 'btn btn--disabled'}
          aria-disabled={!prototypeMode}
          htmlFor={uploadId}
        >
          {identity.image === null ? 'upload…' : 'replace…'}
        </label>
        {identity.image === null ? null : (
          <button
            type="button"
            className="btn"
            onClick={() => updateIdentity({ ...identity, image: null })}
          >
            <span aria-hidden="true">✕</span><span className="visually-hidden">remove image</span>
          </button>
        )}
      </div>
      <p className="settings-note">
        {kind === 'project'
          ? 'Monogram + hue is the default; glyph and image are opt-in (image wins). The custom-hue slider keeps every choice on the brand formula: same lightness and chroma, hue free.'
          : 'The org avatar stays a circle: identity circles are one of the two shapes the 999px pill is reserved for.'}
      </p>
      {/* Every control above is inert outside prototype mode, because there is
          no API to store the choice against yet. A disabled control that never
          says why reads as a bug; saying so is the honest state. */}
      {prototypeMode ? null : (
        <p className="settings-note" role="status">
          Choosing an icon is not available yet — the appearance above is the
          default, and there is nowhere to save a change to it.
        </p>
      )}
    </>
  );
}
