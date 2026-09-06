import { useEffect, useRef, useState } from 'react';
import type { EnvRef } from '../api/keys.ts';
import { useRevisionDiff, type RevisionDiff, type RevisionDiffRow } from '../api/revisionDiff.ts';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { useTransport } from '../api/transport.tsx';
import { fetchRevealWindow } from '../api/values.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Ceremony } from './Ceremony.tsx';
import { useCeremonyTask } from './useCeremonyTask.ts';
import { useModalDialog } from './useModalDialog.ts';

export function RevisionDiffDialog({ env, environmentName, left, right, onClose }: {
  env: EnvRef; environmentName: string; left: bigint; right: bigint; onClose: () => void;
}) {
  const dialog = useModalDialog();
  const transport = useTransport();
  const auth = useAuth();
  const { compare, reveal } = useRevisionDiff(env, left, right);
  const ceremony = useCeremonyTask([env.org, env.project, env.environment, String(left), String(right), auth.identity?.session.id ?? '']);
  const [data, setData] = useSensitiveState<RevisionDiff | null>(null);
  const [disclosure, setDisclosure] = useSensitiveState<{ row: RevisionDiffRow; until: number } | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [now, setNow] = useState(Date.now);
  const preflightGeneration = useRef(0);
  const actions = useRef({ compare, reveal, setData, setDisclosure });
  actions.current = { compare, reveal, setData, setDisclosure };
  useEffect(() => {
    actions.current.compare.mutate(undefined, { onSuccess: (result) => actions.current.setData(result) });
    const ticker = window.setInterval(() => setNow(Date.now()), 1000);
    const clear = () => {
      preflightGeneration.current += 1;
      actions.current.reveal.reset();
      actions.current.setDisclosure(null);
    };
    window.addEventListener('blur', clear);
    document.addEventListener('visibilitychange', clear);
    return () => {
      window.clearInterval(ticker);
      window.removeEventListener('blur', clear);
      document.removeEventListener('visibilitychange', clear);
    };
  }, []);
  useEffect(() => {
    if (disclosure !== null && now >= disclosure.until) setDisclosure(null);
  }, [now, disclosure, setDisclosure]);

  const disclose = async (row: RevisionDiffRow) => {
    setDisclosure(null);
    setFailure(null);
    const task = ceremony.begin([row.key_id]);
    const generation = preflightGeneration.current;
    const run = async () => {
      if (document.hidden) { ceremony.finish(task); return; }
      try {
        const value = await reveal.mutateAsync(row.key_id);
        ceremony.commit(task, () => setDisclosure({ row: value, until: Date.now() + 30_000 }));
      } catch (error) {
        ceremony.commit(task, () => setFailure(error instanceof Error ? error.message : 'Revision disclosure refused.'));
      } finally { ceremony.finish(task); }
    };
    try {
      const window = await fetchRevealWindow(env, transport.client, task.signal);
      if (!ceremony.isCurrent(task)) return;
      if (generation !== preflightGeneration.current || document.hidden) { ceremony.finish(task); return; }
      if (window.live) await run();
      else ceremony.stage(task, { purpose: 'reveal', environmentId: env.environment, environmentName,
        keys: [{ id: row.key_id, name: row.name, classification: 'secret' }], window }, () => void run());
    } catch (error) {
      ceremony.commit(task, () => setFailure(error instanceof Error ? error.message : 'Reveal window could not be read.'));
      ceremony.finish(task);
    }
  };
  const visible = disclosure !== null && now < disclosure.until ? disclosure.row : null;
  return <>
    <dialog className="matrix-editor history-sheet" ref={dialog} onClose={onClose} aria-labelledby="revision-diff-title">
      <div className="matrix-editor__head">
        <h2 id="revision-diff-title">{`Diff r${String(left)} → r${String(right)} · ${environmentName}`}</h2>
        <button className="btn" type="button" onClick={onClose}>Close diff</button>
      </div>
      <p>Secret rows show write-presence. Revealing one key discloses both retained values, requires current or historical reveal permission for each side, and is audited.</p>
      {compare.isPending ? <p role="status">Reading revision diff…</p> : null}
      {compare.error !== null ? <p role="alert">{compare.error.message}</p> : null}
      {failure !== null ? <p role="alert">{failure}</p> : null}
      {data?.items.length === 0 ? <p>No set keys in these revisions.</p> : null}
      <ul className="history__impact">
        {data?.items.map((row) => {
          const shown = visible?.key_id === row.key_id ? visible : row;
          return <li key={row.key_id}>
            <span className="mono">{row.name}</span>
            <span>{shown.status.replace('_', ' ')}</span>
            {shown.revealed ? <span className="mono">{shown.before ?? 'absent'}{' → '}{shown.after ?? 'absent'}</span> : <span>masked · write-presence only</span>}
            {row.classification === 'secret' ? visible?.key_id === row.key_id
              ? <button type="button" className="btn" onClick={() => setDisclosure(null)}>Mask {row.name}</button>
              : <button type="button" className="btn" disabled={reveal.isPending} onClick={() => void disclose(row)}>Reveal {row.name} in diff</button>
              : null}
          </li>;
        })}
      </ul>
      {visible !== null ? <p role="status">Re-masks in {Math.ceil(((disclosure?.until ?? now) - now) / 1000)}s. Switching away masks immediately.</p> : null}
    </dialog>
    {ceremony.request !== null ? <Ceremony key={ceremony.requestKey} request={ceremony.request} onAuthorised={ceremony.onAuthorised} onCancel={ceremony.onCancel} /> : null}
  </>;
}
