import type { zWideningRefusal } from '@hikyo/zod';
import type { z } from 'zod';

import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { label } from './model.ts';

export type WideningRefusal = z.infer<typeof zWideningRefusal>;

/** One line per person: "Dana Ruiz: Reveal (prod), Reveal history (prod)". */
export function gainerLines(widening: WideningRefusal, envName: (id: string) => string): { id: string; text: string }[] {
  const byPerson = new Map<string, { name: string; parts: string[] }>();
  for (const gain of widening.gainers ?? []) {
    const line = byPerson.get(gain.principal_id) ?? { name: gain.principal_name ?? gain.principal_id, parts: [] };
    const envs = gain.environments.map(envName).join(', ');
    line.parts.push(envs === '' ? label(gain.capability) : `${label(gain.capability)} (${envs})`);
    byPerson.set(gain.principal_id, line);
  }
  return [...byPerson.entries()].map(([id, line]) => ({ id, text: `${line.name}: ${line.parts.join(', ')}` }));
}

/** The principal ids a confirmation must name: exactly the people the refusal named. */
export const wideningConfirmation = (widening: WideningRefusal): string[] => [...new Set((widening.gainers ?? []).map((g) => g.principal_id))];

/**
 * The confirmation a key folder move needs when it widens anyone's access
 * through their access rules (ADR D9): a rule whose except named the old
 * folder no longer leaves the key out, or an only-pick of the new folder now
 * covers it. The server names who gains what to a member manager of the
 * project; anyone else learns only how many, and cannot confirm the move, so
 * the dialog then offers no confirm action and says why.
 *
 * A decision, not an editor: no scrim dismissal, only Cancel and Escape. The
 * actions stay pinned so a long list never hides them.
 */
export function KeyMoveConfirmDialog({
  widening,
  change,
  envName,
  busy = false,
  failure = null,
  onCancel,
  onConfirm,
}: {
  widening: WideningRefusal;
  /** The move in words, e.g. "Move DB_PASSWORD from db/ to app/". */
  change: string;
  envName: (id: string) => string;
  busy?: boolean;
  failure?: string | null;
  onCancel: () => void;
  onConfirm: (principals: string[]) => void;
}) {
  const lines = gainerLines(widening, envName);
  const named = lines.length > 0;
  const people = widening.count === 1 ? '1 person' : `${widening.count} people`;
  return (
    <Dialog
      title="This move gives people new access"
      className="access-dialog"
      lede={`${change}. Rules that left this key out through its old folder, or that pick its new folder, now reach it.`}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onCancel();
      }}
      pinActions
      actions={
        <>
          <Button type="button" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          {named ? (
            <Button type="button" variant="primary" disabled={busy} aria-busy={busy ? true : undefined} onClick={() => onConfirm(wideningConfirmation(widening))}>
              {busy ? 'Moving…' : 'Move and give access'}
            </Button>
          ) : null}
        </>
      }
    >
      {failure === null ? null : <Alert>{failure}</Alert>}
      <section className="access-diff access-diff--gain" aria-label="Gains access">
        <p>
          <Glyph name="warn" /> <strong>Gains access: {people}</strong>
        </p>
        {named ? (
          <ul>
            {lines.map((line) => (
              <li key={line.id}>{line.text}</li>
            ))}
          </ul>
        ) : (
          <p className="access-hint">Who gains is shown only to someone who manages access on this project; ask one of them to make this move.</p>
        )}
      </section>
      {named ? <p className="access-hint">To keep it hidden from them, cancel and add an except for this key to their rule first, or pick a different folder.</p> : null}
    </Dialog>
  );
}
