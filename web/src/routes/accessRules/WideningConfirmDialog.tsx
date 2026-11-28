import type { zWideningRefusal } from '@hikyo/zod';
import { Fragment, type ReactNode } from 'react';
import type { z } from 'zod';

import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { NamedId } from '../../ui/NamedId.tsx';
import { keyScopeText, label } from './model.ts';

export type WideningRefusal = z.infer<typeof zWideningRefusal>;

/** One line per person: "Dana Ruiz …a1b2c3: Reveal (prod), Edit (prod, only db/)". */
export function gainerLines(widening: WideningRefusal, envName: (id: string) => string): { id: string; name: string | undefined; parts: string[] }[] {
  const byPerson = new Map<string, { name: string | undefined; parts: string[] }>();
  for (const gain of widening.gainers ?? []) {
    const line = byPerson.get(gain.principal_id) ?? { name: gain.principal_name, parts: [] };
    const scope = [...gain.environments.map(envName), ...(gain.keys === undefined ? [] : [keyScopeText(gain.keys)])].join(', ');
    line.parts.push(scope === '' ? label(gain.capability) : `${label(gain.capability)} (${scope})`);
    byPerson.set(gain.principal_id, line);
  }
  return [...byPerson.entries()].map(([id, line]) => ({ id, name: line.name, parts: line.parts }));
}

/** The principal ids a confirmation must name: exactly the principals the refusal named. */
export const wideningConfirmation = (widening: WideningRefusal): string[] => [...new Set((widening.gainers ?? []).map((g) => g.principal_id))];

/**
 * The words of a widening confirmation. The defaults are a key folder move's;
 * an environment create, rename or clone (#1447) passes its own.
 */
export type WideningCopy = {
  readonly title: string;
  readonly lede: string;
  readonly confirm: string;
  readonly busy: string;
  /** Who gains, counted: "1 person", "3 people". */
  readonly count: (n: number) => string;
  readonly undisclosed: string;
  readonly keepHidden: string;
};

const KEY_MOVE_COPY: WideningCopy = {
  title: 'This move gives people new access',
  lede: 'Rules that left this key out through its old folder, or that pick its new folder, now reach it.',
  confirm: 'Move and give access',
  busy: 'Moving…',
  count: (n) => (n === 1 ? '1 person' : `${n} people`),
  undisclosed: 'Who gains is shown only to someone who manages access on this project; ask one of them to make this move.',
  keepHidden: 'To keep it hidden from them, cancel and add an except for this key to their rule first, or pick a different folder.',
};

/** An environment create, rename or clone that widens rules selecting environments by name (#1447). */
export const ENV_WIDENING_COPY: WideningCopy = {
  title: 'This gives people and machines new access',
  lede: 'Access rules select environments by name: rules that pick this name, or that no longer leave the old one out, now reach the environment.',
  confirm: 'Continue and give access',
  busy: 'Saving…',
  count: (n) => (n === 1 ? '1 person or machine' : `${n} people and machines`),
  undisclosed: 'Who gains is shown only to someone who manages access on this project; ask one of them to make this change.',
  keepHidden: 'To keep it from them, cancel and change their rules first, or pick a different name.',
};

/** The same confirmation for deleting an environment an `all` rule leaves out (#1447). */
export const ENV_DELETE_WIDENING_COPY: WideningCopy = {
  ...ENV_WIDENING_COPY,
  lede: 'Access rules select environments by name: rules that leave this environment out reach the rest of the project once it is deleted. They keep the name, so an environment created later with it is left out again.',
  confirm: 'Delete and give access',
  busy: 'Deleting…',
  keepHidden: 'To keep it from them, cancel and change their rules first.',
};

/**
 * The confirmation a key folder move needs when it widens anyone's access
 * through their access rules (ADR D9): a rule whose except named the old
 * folder no longer leaves the key out, or an only-pick of the new folder now
 * covers it. An environment create, rename or clone that widens rules by name
 * (D3, #1447) asks the same with its own `copy`. The server names who gains
 * what to a member manager of the project; anyone else learns only how many,
 * and cannot confirm, so the dialog then offers no confirm action and says why.
 *
 * A decision, not an editor: no scrim dismissal, only Cancel and Escape. The
 * actions stay pinned so a long list never hides them.
 */
export function WideningConfirmDialog({
  widening,
  change,
  envName,
  copy = KEY_MOVE_COPY,
  busy = false,
  failure = null,
  onCancel,
  onConfirm,
}: {
  widening: WideningRefusal;
  /** The move in words, e.g. "Move DB_PASSWORD from db/ to app/". */
  change: ReactNode;
  envName: (id: string) => string;
  copy?: WideningCopy;
  busy?: boolean;
  failure?: string | null;
  onCancel: () => void;
  onConfirm: (principals: string[]) => void;
}) {
  const lines = gainerLines(widening, envName);
  const named = lines.length > 0;
  const people = copy.count(widening.count);
  return (
    <Dialog
      title={copy.title}
      className="access-dialog"
      lede={<>{change}. {copy.lede}</>}
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
              {busy ? copy.busy : copy.confirm}
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
              <li key={line.id}>
                <NamedId id={line.id} name={line.name} tail />:{' '}
                {line.parts.map((part, index) => (
                  <Fragment key={part}>
                    {index === 0 ? null : ', '}
                    <bdi>{part}</bdi>
                  </Fragment>
                ))}
              </li>
            ))}
          </ul>
        ) : (
          <p className="access-hint">{copy.undisclosed}</p>
        )}
      </section>
      {named ? <p className="access-hint">{copy.keepHidden}</p> : null}
    </Dialog>
  );
}
