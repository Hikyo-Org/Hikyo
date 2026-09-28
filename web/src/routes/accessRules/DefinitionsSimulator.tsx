import { useState } from 'react';

import { Button } from '../../ui/Button.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { Panel } from '../Sections.tsx';
import { CHANGES, type DefinitionsChange } from './fixture.ts';
import { accessDiff, accessLineText, keyById, type World } from './model.ts';
import { Lock } from './parts.tsx';

type Entry = { id: string; label: string; isAdd: boolean; gained: string[]; lost: string[] };

/**
 * "Try a definitions change": add, move or rename a key and see who gains and
 * who loses access. A move that widens anyone's access (their except named
 * the old folder) is held for confirmation, naming who; adds, renames and
 * moves that only narrow save directly.
 */
export function DefinitionsSimulator({ world, onChange }: { world: World; onChange: (world: World) => void }) {
  const [initial] = useState(world);
  const [log, setLog] = useState<Entry[]>([]);
  const [pending, setPending] = useState<{ next: World; entry: Entry; change: DefinitionsChange } | null>(null);

  const commit = (next: World, entry: Entry) => {
    onChange(next);
    setLog([entry, ...log]);
    setPending(null);
  };
  const apply = (change: DefinitionsChange) => {
    const next = change.apply(world);
    const { gained, lost } = accessDiff(world, next, change.keyId);
    const entry: Entry = {
      id: change.id,
      label: change.label,
      isAdd: change.isAdd,
      gained: gained.map((line) => accessLineText(next, line)),
      lost: lost.map((line) => accessLineText(world, line)),
    };
    if (change.isAdd || entry.gained.length === 0) commit(next, entry);
    else setPending({ next, entry, change });
  };
  const key = pending === null ? undefined : keyById(world, pending.change.keyId);

  return (
    <Panel id="access-simulator" title="Try a definitions change">
      <p className="access-hint">
        Folder picks (like <code>db/</code>) follow the folder: a key moved out leaves the rule, a key added in joins it. Single-key picks follow the key
        itself, through renames and moves. A move that gives anyone new access asks for confirmation first, naming who.
      </p>
      <div className="access-examples">
        {CHANGES.map((change) => {
          const done = log.some((entry) => entry.id === change.id);
          return (
            <Button key={change.id} type="button" disabled={done} onClick={() => apply(change)}>
              {done ? <Glyph name="check" label="done" /> : null}
              {change.label}
            </Button>
          );
        })}
        <Button
          type="button"
          variant="quiet"
          onClick={() => {
            onChange(initial);
            setLog([]);
          }}
        >
          Reset
        </Button>
      </div>
      {log.length > 0 ? (
        <ul className="access-log" aria-label="Changes made">
          {log.map((entry) => (
            <li key={entry.id} className="access-log__entry">
              <strong>{entry.label}</strong>
              {entry.gained.length > 0 ? (
                <p>
                  {entry.isAdd ? 'Can use the new key' : 'Gains'}: {entry.gained.join('; ')}
                </p>
              ) : null}
              {entry.lost.length > 0 ? <p>Loses: {entry.lost.join('; ')}</p> : null}
              {entry.gained.length === 0 && entry.lost.length === 0 ? <p>Nobody gains or loses access.</p> : null}
              {entry.gained.length > 0 && !entry.isAdd ? <p>Confirmed before saving: this move widened access.</p> : null}
            </li>
          ))}
        </ul>
      ) : null}
      {pending === null ? null : (
        <Dialog
          title="This move gives people new access"
          className="access-dialog"
          lede={`${pending.entry.label}. Rules that named the old folder in an except no longer leave this key out.`}
          onCancel={() => setPending(null)}
          actions={
            <>
              <Button type="button" onClick={() => setPending(null)}>
                Cancel
              </Button>
              <Button type="button" variant="primary" onClick={() => commit(pending.next, pending.entry)}>
                Move and give access
              </Button>
            </>
          }
        >
          <section className="access-diff access-diff--gain" aria-label="Gains access">
            <p>
              <Glyph name="warn" /> <strong>Gains access to {key?.name ?? pending.change.keyId}</strong>
              {key?.secret === true ? <Lock word="secret" /> : null}
            </p>
            <ul>
              {pending.entry.gained.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </section>
          {pending.entry.lost.length > 0 ? (
            <section className="access-diff" aria-label="Loses access">
              <p>
                <strong>Loses access</strong>
              </p>
              <ul>
                {pending.entry.lost.map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ul>
            </section>
          ) : null}
          <p className="access-hint">To keep it hidden from them, cancel and add an except for this key to their rule first, or pick a different folder.</p>
        </Dialog>
      )}
    </Panel>
  );
}
