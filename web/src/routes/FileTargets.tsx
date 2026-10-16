import { useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router';

import { useProjectEnvironments, useProjectKeys, type ProjectRef } from '../api/adapters.ts';
import {
  fileTargetConflict, fileTargetReadDenied, fileTargetRefusalText, useCreateFileTarget, useDeleteFileTarget,
  useFileTarget, useFileTargetAccounts, useFileTargets, useRefreshFileTargets,
  useUpdateFileTarget, type FileTarget,
} from '../api/fileTargets.ts';
import { formatWhen } from '../lib/formatWhen.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Input } from '../ui/Input.tsx';
import { Select } from '../ui/Select.tsx';
import { SidePanel } from '../ui/SidePanel.tsx';
import { TargetKeySelection, useTargetKeySelection } from './TargetKeySelection.tsx';

/** No freshness deadline is part of the file-target contract. Show age and
 * generation mismatch, never promote a historical client assertion to health. */
export function fileReportAge(reportedAt: string, now: number): string {
  const seconds = Math.floor((now - Date.parse(reportedAt)) / 1_000);
  if (!Number.isFinite(seconds)) return 'Unknown age';
  if (seconds < 0) return 'Client clock is ahead';
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3_600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3_600)}h ago`;
  return `${Math.floor(seconds / 86_400)}d ago`;
}

// A successful read remounts this clock, sampling even between periodic ticks.
// Keep clock reads in initialization and timer events, outside render.
function ReportAge({ reportedAt }: { readonly reportedAt: string }) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, []);
  return <>{fileReportAge(reportedAt, now)}</>;
}

function ReportFacts({ target, readAt }: { readonly target: FileTarget; readonly readAt: number }) {
  const report = target.report;
  if (report === null) return <p role="status">No client report. Host health is unknown.</p>;
  return <>
    <Badge tone={report.state === 'failed' || report.state === 'refused' ? 'danger' : 'neutral'}>
      Client reported {report.state}
    </Badge>
    <dl className="adapters__facts">
      <dt>Reported revision</dt><dd>{String(report.revision)}</dd>
      <dt>Reported generation</dt><dd>{String(report.generation)}</dd>
      <dt>Report age</dt><dd><ReportAge key={`${readAt}:${report.reported_at}`} reportedAt={report.reported_at} /></dd>
      <dt>Reported at</dt><dd>{formatWhen(report.reported_at)}</dd>
      <dt>Received at</dt><dd>{formatWhen(report.received_at)}</dd>
    </dl>
    {report.generation !== target.generation ? <Alert>
      This report is for a different target generation. The current selection has not been confirmed by this report.
    </Alert> : null}
    <p className="field__hint">Historical client assertion. Current host health is not verified; the host may have changed or gone offline since this report.</p>
  </>;
}

function TargetFacts({ target, readAt, environmentName, accountName }: {
  readonly target: FileTarget; readonly readAt: number;
  readonly environmentName: (id: string) => string;
  readonly accountName: (id: string) => string;
}) {
  return <>
    <dl className="adapters__facts">
      <dt>Environment</dt><dd>{environmentName(target.environment_id)}</dd>
      <dt>Workload account</dt><dd>{accountName(target.service_account_id)}</dd>
      <dt>Target generation</dt><dd>{String(target.generation)}</dd>
      <dt>Target ID</dt><dd className="mono">{target.id}</dd>
    </dl>
    <ul className="adapters__keys" aria-label="Selected keys">
      {target.keys.map((key) => <li key={key.key_id}><Badge mono>{key.name} ({key.classification})</Badge></li>)}
    </ul>
    {target.keys.length === 0 ? <p>No keys remain in this selection.</p> : null}
    <ReportFacts target={target} readAt={readAt} />
  </>;
}

export function FileTargets({ refData, selected, onSelect, panelAvailable }: {
  readonly refData: ProjectRef;
  readonly selected: string;
  readonly onSelect: (target: string) => void;
  readonly panelAvailable: boolean;
}) {
  const targets = useFileTargets(refData);
  const environments = useProjectEnvironments(refData);
  const accounts = useFileTargetAccounts(refData);
  const refresh = useRefreshFileTargets(refData);
  const denied = targets.data === null || fileTargetReadDenied(targets.error);
  const visibleTargets = denied ? undefined : targets.data;
  const visibleEnvironments = fileTargetReadDenied(environments.error) ? undefined : environments.data;
  const visibleAccounts = fileTargetReadDenied(accounts.error) ? undefined : accounts.data;
  const [done, setDone] = useState('');
  const environmentName = (id: string) => visibleEnvironments?.items.find((item) => item.id === id)?.name ?? id;
  const accountName = (id: string) => {
    const account = visibleAccounts?.items.find((item) => item.id === id);
    return account === undefined ? id : `${account.name} (${id})`;
  };
  const close = () => onSelect('');
  return <section className="adapters__list" aria-label="File targets">
    <h2>File targets</h2>
    <p>Workload clients pull the selected keys. Destination paths, file formats and refresh schedules stay in the client’s local configuration.</p>
    {done !== '' ? <p role="status">{done}</p> : null}
    {targets.isPending ? <p role="status">Loading file targets…</p> : null}
    {targets.isError ? <Alert>{fileTargetRefusalText(targets.error)}</Alert> : null}
    {targets.isError && visibleTargets !== undefined ? <p role="status">Showing the last loaded file targets. Refresh failed.</p> : null}
    {environments.isError ? <Alert>Environment names are unavailable. Showing environment IDs.</Alert> : null}
    {accounts.isError ? <Alert>Workload account names are unavailable. Showing account IDs. Creating a target requires manage-identities.</Alert> : null}
    {targets.isSuccess && visibleTargets?.items.length === 0 ? <p>No file targets yet.</p> : null}
    {visibleTargets?.items.map((target) => <article className="adapters__adapter" key={target.id} aria-label={`File target ${target.name}`}>
      <h3>{target.name}</h3>
      <TargetFacts target={target} readAt={targets.dataUpdatedAt} environmentName={environmentName} accountName={accountName} />
      <Button type="button" onClick={() => { setDone(''); onSelect(target.id); }}>Inspect {target.name}</Button>
    </article>)}
    <div className="actions">
      <Button type="button" variant="primary" disabled={denied} onClick={() => { setDone(''); onSelect('new'); }}>Add file target</Button>
      <Button type="button" disabled={targets.isFetching} onClick={() => refresh(selected === '' || selected === 'new' ? undefined : selected)}>Refresh file targets</Button>
    </div>
    {!denied && panelAvailable && selected === 'new' ? <CreateFileTargetPanel refData={refData} onClose={close}
      onSaved={(target) => { setDone('File target created. Configure the client to pull this target.'); onSelect(target.id); }}
    /> : null}
    {!denied && panelAvailable && selected !== '' && selected !== 'new' ? <FileTargetDetail
      key={selected} refData={refData} targetId={selected}
      environmentName={environmentName} accountName={accountName} onClose={close}
      onDeleted={() => { setDone('File target deleted. Local files were not changed.'); close(); }}
    /> : null}
  </section>;
}

function CreateFileTargetPanel({ refData, onClose, onSaved }: {
  readonly refData: ProjectRef; readonly onClose: () => void; readonly onSaved: (target: FileTarget) => void;
}) {
  // Busy state belongs to this mounted panel. A retired request cannot lock
  // an idle replacement or release a replacement panel's in-flight guard.
  const [saving, setSaving] = useState(false);
  const close = () => { if (!saving) onClose(); };
  return <SidePanel title="Add file target" onClose={close}>
    <FileTargetForm refData={refData} onBusyChange={setSaving} onCancel={close} onSaved={onSaved} />
  </SidePanel>;
}

function FileTargetDetail({ refData, targetId, environmentName, accountName, onClose, onDeleted }: {
  readonly refData: ProjectRef; readonly targetId: string;
  readonly environmentName: (id: string) => string; readonly accountName: (id: string) => string;
  readonly onClose: () => void; readonly onDeleted: () => void;
}) {
  const detail = useFileTarget(refData, targetId);
  const target = fileTargetReadDenied(detail.error) ? undefined : detail.data;
  if (target == null) return <SidePanel title="File target" onClose={onClose}>
    {detail.isPending ? <p role="status">Loading file target…</p> : null}
    {detail.isError ? <Alert>{fileTargetRefusalText(detail.error)}</Alert> : null}
    <Button type="button" disabled={detail.isFetching} onClick={() => void detail.refetch()}>Refresh report</Button>
  </SidePanel>;
  return <FileTargetLoaded key={target.id} refData={refData} target={target} readAt={detail.dataUpdatedAt}
    environmentName={environmentName} accountName={accountName} onClose={onClose} onDeleted={onDeleted}
    error={detail.error} refreshing={detail.isFetching} onRefresh={() => void detail.refetch()} />;
}

function FileTargetLoaded({ refData, target, readAt, environmentName, accountName, onClose, onDeleted, error, refreshing, onRefresh }: {
  readonly refData: ProjectRef; readonly target: FileTarget; readonly readAt: number;
  readonly environmentName: (id: string) => string; readonly accountName: (id: string) => string;
  readonly onClose: () => void; readonly onDeleted: () => void;
  readonly error: Error | null; readonly refreshing: boolean; readonly onRefresh: () => void;
}) {
  const remove = useDeleteFileTarget(refData);
  const [editing, setEditing] = useState<FileTarget | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [failure, setFailure] = useState('');
  const [done, setDone] = useState('');
  const [saving, setSaving] = useState(false);
  const close = () => { if (!remove.isPending && !saving) onClose(); };
  return <SidePanel title={target.name} onClose={close}>
    {error !== null ? <Alert>{fileTargetRefusalText(error)}</Alert> : null}
    {error !== null ? <p role="status">Showing the last loaded file target. Refresh failed.</p> : null}
    {done !== '' ? <p role="status">{done}</p> : null}
    <>
      <TargetFacts target={target} readAt={readAt} environmentName={environmentName} accountName={accountName} />
      {editing !== null ? <FileTargetForm refData={refData} initial={editing} onBusyChange={setSaving}
        onCancel={() => setEditing(null)} onSaved={() => { setEditing(null); setDone('Key selection saved. Waiting for a client report for this generation.'); }}
      /> : <div className="actions">
        <Button type="button" disabled={error !== null || refreshing} onClick={() => { setDone(''); setEditing(target); }}>Edit key selection</Button>
        <Button type="button" variant="danger" disabled={error !== null || refreshing} onClick={() => { setFailure(''); setDeleting(true); }}>Delete file target</Button>
        <Button type="button" disabled={refreshing} onClick={onRefresh}>Refresh report</Button>
      </div>}
      <p><Link to={`/orgs/${encodeURIComponent(refData.org)}/projects/${encodeURIComponent(refData.project)}/machine-access`}>Manage workload accounts and credentials</Link></p>
    </>
    {deleting ? <Dialog title={`Delete file target ${target.name}`}
      onCancel={(event) => { if (remove.isPending) event.preventDefault(); else setDeleting(false); }}
      actions={<>
        <Button type="button" disabled={remove.isPending} onClick={() => setDeleting(false)}>Cancel</Button>
        <Button type="button" variant="danger" disabled={remove.isPending} onClick={() => {
          setFailure('');
          void remove.mutateAsync(target.id).then(onDeleted).catch((error: Error) => {
            setFailure(fileTargetConflict(error)
              ? 'The workload account still holds live credentials. Revoke them in Machine access, or delete the account (which also deletes this target). Removing the binding now would widen delivery to the whole environment.'
              : fileTargetRefusalText(error));
          });
        }}>{remove.isPending ? 'Deleting…' : 'Confirm delete'}</Button>
      </>}>
      <p>Deletion is refused (409) while the bound workload account has live credentials: removing the binding would widen its delivery to the whole environment. Revoke its credentials first, or delete the account, which also removes this target.</p>
      <p>Deleting the server target does not delete files on the client host.</p>
      {failure !== '' ? <Alert>{failure}</Alert> : null}
    </Dialog> : null}
  </SidePanel>;
}

function FileTargetForm({ refData, initial, onCancel, onSaved, onBusyChange }: {
  readonly refData: ProjectRef; readonly initial?: FileTarget;
  readonly onBusyChange: (busy: boolean) => void;
  readonly onCancel: () => void; readonly onSaved: (target: FileTarget) => void;
}) {
  const environments = useProjectEnvironments(refData);
  const accounts = useFileTargetAccounts(refData);
  const keys = useProjectKeys(refData);
  const create = useCreateFileTarget(refData);
  const update = useUpdateFileTarget(refData);
  const [name, setName] = useState('');
  const [environment, setEnvironment] = useState('');
  const [account, setAccount] = useState('');
  const selection = useTargetKeySelection(initial?.keys.map((key) => key.key_id));
  const [failure, setFailure] = useState('');
  const [conflict, setConflict] = useState(false);
  const busy = create.isPending || update.isPending;
  const workloads = (fileTargetReadDenied(accounts.error) ? undefined : accounts.data)?.items.filter((item) => item.kind === 'workload') ?? [];
  const unavailable = !keys.isSuccess || keys.data === null || (initial === undefined &&
    (!environments.isSuccess || environments.data === null || !accounts.isSuccess || accounts.data === null));
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || unavailable || conflict) return;
    setFailure('');
    onBusyChange(true);
    const chosen = { key_ids: selection.keyIds, ...(selection.keySelection === undefined ? {} : { key_selection: selection.keySelection }) };
    try {
      const target = initial === undefined
        ? await create.mutateAsync({ name, environment_id: environment, service_account_id: account, ...chosen })
        : await update.mutateAsync({ target: initial.id, generation: initial.generation, selection: chosen });
      onSaved(target);
    } catch (error) {
      if (initial !== undefined && fileTargetConflict(error)) {
        setConflict(true);
        setFailure('This target changed while you were editing. Your selection has not been saved. Cancel, refresh the target, then review its current selection before editing again.');
      } else setFailure(fileTargetRefusalText(error));
    } finally {
      onBusyChange(false);
    }
  };
  return <form className="adapters__form" aria-label={initial === undefined ? 'Create file target' : 'Edit file target'} onSubmit={(event) => void submit(event)}>
    {initial === undefined ? <>
      <Input label="Target name" required maxLength={63} pattern="[a-z][a-z0-9-]{0,62}" value={name} disabled={busy} onChange={(event) => setName(event.target.value)} hint="Lowercase letters, digits and hyphens; start with a letter." />
      <Select label="Environment" required value={environment} disabled={busy || !environments.isSuccess} onChange={(event) => setEnvironment(event.target.value)}>
        <option value="">Choose an environment</option>
        {(fileTargetReadDenied(environments.error) ? undefined : environments.data)?.items.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </Select>
      <Select label="Workload service account" required value={account} disabled={busy || !accounts.isSuccess} onChange={(event) => setAccount(event.target.value)}>
        <option value="">Choose a workload account</option>
        {workloads.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </Select>
      {accounts.isSuccess && workloads.length === 0 ? <p>Create a workload service account in Machine access first. Automation accounts cannot be bound.</p> : null}
      <p className="field__hint">An account can bind to one file target. Existing grants and credentials are unchanged; delivery is narrowed to this target’s selected keys.</p>
    </> : <p>Replacing selection at generation {String(initial.generation)}. Environment and workload account stay bound.</p>}
    {keys.isError ? <Alert>Key catalogue could not be loaded. {fileTargetRefusalText(keys.error)}</Alert> : null}
    {initial === undefined && environments.isError ? <Alert>{fileTargetRefusalText(environments.error)}</Alert> : null}
    {initial === undefined && accounts.isError ? <Alert>{fileTargetRefusalText(accounts.error)}</Alert> : null}
    {unavailable && !keys.isError && !environments.isError && !accounts.isError ? <p role="status">Loading selection options…</p> : null}
    <TargetKeySelection keys={(fileTargetReadDenied(keys.error) ? undefined : keys.data)?.items ?? []} value={selection.value} onChange={selection.onChange} disabled={busy || unavailable || conflict} />
    {failure !== '' ? <Alert>{failure}</Alert> : null}
    <div className="actions">
      <Button type="submit" variant="primary" disabled={busy || unavailable || conflict}>{busy ? 'Saving…' : initial === undefined ? 'Create file target' : 'Save selection'}</Button>
      <Button type="button" disabled={busy} onClick={onCancel}>Cancel</Button>
    </div>
  </form>;
}
