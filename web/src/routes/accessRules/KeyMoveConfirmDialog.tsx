import { Button } from '../../ui/Button.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { accessDiff, accessLineText, keyById, type World } from './model.ts';
import { Lock } from './parts.tsx';

/**
 * The confirmation a key move needs when it widens anyone's access: a rule
 * whose except named the old folder no longer leaves the key out. Names who
 * gains and who loses before anything is saved. The Definitions move flow
 * shows it when `accessDiff(before, after, keyId).gained` is not empty;
 * moves that only narrow, renames and adds save without it.
 */
export function KeyMoveConfirmDialog({
  before,
  after,
  keyId,
  change,
  onCancel,
  onConfirm,
}: {
  before: World;
  after: World;
  keyId: string;
  /** The move in words, e.g. "Move DB_PASSWORD from db/ to app/". */
  change: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { gained, lost } = accessDiff(before, after, keyId);
  const key = keyById(after, keyId);
  return (
    <Dialog
      title="This move gives people new access"
      className="access-dialog"
      lede={`${change}. Rules that named the old folder in an except no longer leave this key out.`}
      onCancel={onCancel}
      actions={
        <>
          <Button type="button" onClick={onCancel}>
            Cancel
          </Button>
          <Button type="button" variant="primary" onClick={onConfirm}>
            Move and give access
          </Button>
        </>
      }
    >
      <section className="access-diff access-diff--gain" aria-label="Gains access">
        <p>
          <Glyph name="warn" /> <strong>Gains access to {key?.name ?? keyId}</strong>
          {key?.secret === true ? <Lock word="secret" /> : null}
        </p>
        <ul>
          {gained.map((line) => (
            <li key={line.member}>{accessLineText(after, line)}</li>
          ))}
        </ul>
      </section>
      {lost.length > 0 ? (
        <section className="access-diff" aria-label="Loses access">
          <p>
            <strong>Loses access</strong>
          </p>
          <ul>
            {lost.map((line) => (
              <li key={line.member}>{accessLineText(before, line)}</li>
            ))}
          </ul>
        </section>
      ) : null}
      <p className="access-hint">To keep it hidden from them, cancel and add an except for this key to their rule first, or pick a different folder.</p>
    </Dialog>
  );
}
