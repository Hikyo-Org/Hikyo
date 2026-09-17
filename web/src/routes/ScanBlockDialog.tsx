import { useState } from 'react';

import { ApiError, type RefusalFinding } from '../api/client.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Dialog } from '../ui/Dialog.tsx';

/**
 * ScanBlockDialog is the Surface-2 block (#74 / #183, secret-scanning ADR §4).
 *
 * Unlike the Surface-1 warn, the write was REFUSED: the scanner matched
 * credential-shaped material on a write that must not carry it. The dialog
 * renders only the redacted findings the refusal carried, a rule id and an
 * immutable locator (secret-scanning ADR §4), NEVER the matched text, and
 * never the value the operator supplied (which is not in the refusal and is not
 * passed here). That is the whole non-leak guarantee.
 *
 * Where every finding carries an acknowledgement token, the operator may
 * override, an explicit, audited "I have reviewed this and it is intentional".
 * A finding without a token is a hard block that cannot be overridden from the
 * browser; the dialog says so rather than offering a button that would refuse.
 */
export function ScanBlockDialog({
  title,
  intro,
  findings,
  onOverride,
  onClose,
}: {
  title: string;
  intro: string;
  findings: readonly RefusalFinding[];
  /**
   * Retries the blocked write with every finding's acknowledgement token.
   * Resolves when the retry succeeds (the dialog closes); rejects to keep the
   * dialog open with a message. Absent when no override is possible.
   */
  onOverride: ((tokens: readonly string[]) => Promise<void>) | null;
  onClose: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const tokens = findings
    .map((finding) => finding.acknowledgement)
    .filter((token): token is string => token !== undefined);
  const overridable = onOverride !== null && tokens.length === findings.length && tokens.length > 0;

  const override = (): void => {
    if (onOverride === null) return;
    setBusy(true);
    setError(null);
    void onOverride(tokens)
      .then(() => onClose())
      // A rejected override carries the server's OWN caller-safe reason, a
      // content-bound token the field's content outran is rejected by name
      // (stale / version-skew / surplus / expired), never as matched text. Show
      // that named refusal verbatim; only a refusal that carried no safe detail
      // falls back to the generic line.
      .catch((error: unknown) => {
        setError(
          error instanceof ApiError && error.detail !== undefined && error.detail !== ''
            ? error.detail
            : 'The override was refused. Reload and review the findings again.',
        );
      })
      .finally(() => setBusy(false));
  };

  return (
    <Dialog
      title={title}
      lede={intro}
      size="wide"
      // Escape closes the native dialog on its own; without this the caller
      // would still believe the block is open.
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <ul className="scan-block__findings">
        {findings.map((finding, index) => (
          <li className="scan-block__finding" key={`${finding.rule_id} ${finding.locator} ${String(index)}`}>
            <span className="mono scan-block__rule">{finding.rule_id}</span>
            <span className="mono scan-block__locator">{finding.locator}</span>
          </li>
        ))}
      </ul>

      {error === null ? null : (
        <Alert>{error}</Alert>
      )}

      {/* The row stays in the children because the hint below it explains what
          acknowledging records; `actions` would put it after that sentence. */}
      <div className="dialog__actions">
        <Button type="button" disabled={busy} onClick={onClose}>
          {overridable ? 'Cancel' : 'Close'}
        </Button>
        {overridable ? (
          <Button type="button" variant="primary" disabled={busy} onClick={override}>
            {busy ? 'Acknowledging…' : 'Acknowledge and continue'}
          </Button>
        ) : null}
      </div>

      {overridable ? (
        <p className="matrix-editor__hint">
          Acknowledging records that this material is intentional and lets the write proceed. A
          secret belongs in a secret key; reclassify instead if it is one.
        </p>
      ) : (
        <p className="matrix-editor__hint">
          This block cannot be overridden from the browser. Remove the flagged material, or declare
          the key as a secret so it is handled as one.
        </p>
      )}
    </Dialog>
  );
}
