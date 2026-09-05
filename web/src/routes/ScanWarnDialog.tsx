import { useId, useState } from 'react';

import type { ScanFinding } from '../api/matrix.ts';
import { useModalDialog } from './useModalDialog.ts';

/**
 * One config value that a save flagged as credential-shaped (#74, Surface 1).
 *
 * `value` is the CANONICAL stored bytes (post-normalization), exactly what
 * the value write persisted, because the keep-as-config token is content-bound
 * to those bytes. Re-saving anything else re-warns instead of dismissing.
 */
export type ScanWarnItem = {
  readonly environmentId: string;
  readonly environmentName: string;
  readonly value: string;
  readonly finding: ScanFinding;
};

const rowKey = (item: ScanWarnItem): string =>
  `${item.environmentId}\u0000${item.finding.rule_id}`;

/**
 * ScanWarnDialog is the Surface-1 warn (#74, secret-scanning ADR §4).
 *
 * The save already SUCCEEDED, this is a non-blocking warning that a value
 * classified as `config` looks like a credential, and `config` skips every
 * secret-handling rule (masking, the reveal ceremony, write-presence diffing).
 * It renders only what the redacted finding carries, the rule id and the key
 * locator, never the matched text, which the response does not contain.
 *
 * Two first-class resolutions per ADR §4, and deliberately no blanket
 * ignore-all control:
 *
 *   - Reclassify as secret (primary), routes the key through secret handling
 *     via the reclassification ceremony;
 *   - Keep as config, an explicit, sticky dismissal presenting the finding's
 *     acknowledgement token, so the identical value never re-warns while a
 *     distinct offending value still does. The value stays a plain config
 *     value, outside secret handling, which is what the dismissal affirms.
 *
 * Closing the dialog without choosing leaves the warning undismissed: the same
 * value simply re-warns on the next save. That is the safe direction, and it
 * is not a third action.
 */
export function ScanWarnDialog({
  keyName,
  items,
  onDismiss,
  onReclassify,
  onClose,
}: {
  keyName: string;
  items: readonly ScanWarnItem[];
  /**
   * Records the keep-as-config dismissal by re-saving the value with its
   * token, and returns the server's fresh findings for that value, empty when
   * the dismissal took, or a re-fired finding (with a new token) when the
   * token was stale. The dialog trusts that answer rather than assuming.
   */
  onDismiss: (item: ScanWarnItem) => Promise<readonly ScanWarnItem[]>;
  onReclassify: () => Promise<void>;
  onClose: () => void;
}) {
  const dialog = useModalDialog();
  const titleId = useId();
  const [rows, setRows] = useState<readonly ScanWarnItem[]>(items);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const dismiss = (item: ScanWarnItem): void => {
    setBusy(rowKey(item));
    setError(null);
    void onDismiss(item)
      .then((refreshed) => {
        // Replace this value's rows with whatever the server returns after the
        // re-scan: gone when dismissed, re-fired when the token was rejected.
        setRows((current) => {
          const others = current.filter(
            (row) => !(row.environmentId === item.environmentId && row.value === item.value),
          );
          const next = [...others, ...refreshed];
          if (next.length === 0) onClose();
          return next;
        });
      })
      .catch(() => setError('Keeping this value as config failed. Try again, or reclassify instead.'))
      .finally(() => setBusy(null));
  };

  const reclassify = (): void => {
    setBusy('reclassify');
    setError(null);
    void onReclassify()
      .then(() => onClose())
      .catch(() => setError('Reclassifying this key as secret failed. Try again.'))
      .finally(() => setBusy(null));
  };

  return (
    <dialog className="matrix-editor scan-warn" ref={dialog} aria-labelledby={titleId} onClose={onClose}>
      <div className="matrix-editor__head">
        <div>
          <h2 id={titleId}>Possible secret in a config value</h2>
          <p>
            {`${keyName} is classified as config, so its value is stored without secret handling: no masking, no reveal ceremony. These saved values look like credentials.`}
          </p>
        </div>
        <button
          type="button"
          className="btn matrix-editor__close"
          aria-label="Close scanning warning"
          onClick={onClose}
        >
          ✕
        </button>
      </div>

      <ul className="scan-warn__findings">
        {rows.map((item) => (
          <li className="scan-warn__finding" key={rowKey(item)}>
            <div className="scan-warn__finding-head">
              <span className="mono scan-warn__rule">{item.finding.rule_id}</span>
              <span className="mono scan-warn__locator">{item.finding.locator}</span>
              <span className="scan-warn__env">{item.environmentName}</span>
            </div>
            {item.finding.acknowledgement === undefined ? null : (
              <button
                type="button"
                className="btn"
                disabled={busy !== null}
                onClick={() => dismiss(item)}
              >
                {busy === rowKey(item) ? 'Keeping as config…' : 'Keep as config'}
              </button>
            )}
          </li>
        ))}
      </ul>

      <p className="scan-warn__note">
        Keep as config affirms the value belongs here and stays outside secret handling; the
        identical value will not warn again. A different credential-shaped value still will.
      </p>

      {error === null ? null : (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">!</span>
          <span>{error}</span>
        </p>
      )}

      <div className="matrix-editor__actions">
        <button
          type="button"
          className="btn btn--primary"
          disabled={busy !== null}
          onClick={reclassify}
        >
          {busy === 'reclassify' ? 'Reclassifying…' : `Reclassify ${keyName} as secret`}
        </button>
      </div>
    </dialog>
  );
}
