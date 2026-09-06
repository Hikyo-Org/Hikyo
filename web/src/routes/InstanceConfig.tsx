import { useEffect, useId, useRef, useState } from 'react';
import { generatePath, Link } from 'react-router';

import { reauthenticateSelfConfig, revisionNumber, selfConfigFailure, useSelfConfig, useSelfConfigActions, type SelfConfigIntent, type SelfConfigStatus } from '../api/selfConfig.ts';
import { useRemotes } from '../api/remotes.ts';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { useTransport, useWorkspaceContext, withRemote } from '../api/transport.tsx';
import { rememberWorkspace, workspaceSession } from '../api/workspace.ts';
import { surfaceById } from '../app/navigation.ts';
import { Alert, Done, Panel } from './Sections.tsx';
import { useModalDialog } from './useModalDialog.ts';

export function InstanceConfig() {
  const query = useSelfConfig();
  const workspace = useWorkspaceContext();
  return <div className="page page--chrome">
    <h1>Hikyo configuration</h1>
    <p className="page__lede">Edit settings in the configuration project, publish a revision, then apply it. New operations use the applied settings without restarting Hikyo.</p>
    {query.isPending ? <p role="status">Loading configuration…</p> : null}
    {query.isError ? <Alert>{selfConfigFailure(query.error)} Last confirmed state is unavailable. <button className="btn" type="button" onClick={() => void query.refetch()}>Refresh status</button></Alert> : null}
    {query.isSuccess ? <ConfigurationOwner key={query.data.owner_instance_id} status={query.data} stale={!query.isFetchedAfterMount} /> : null}
    {workspace === null ? <ConfigurationRoot /> : null}
  </div>;
}

function ConfigurationRoot() {
  const remotes = useRemotes();
  return <Panel id="configuration-instances" title="Independent instances">
    <p className="settings-note">Each instance owns its project, settings and revision history. HA replicas of one instance share that instance’s project.</p>
    {remotes.isPending ? <p role="status">Loading connected instance references…</p> : null}
    {remotes.isError ? <Alert>Instance references could not be loaded.</Alert> : null}
    {remotes.data?.items.length === 0 ? <p>No remote instances connected. <Link to={surfaceById('remotes').path}>Manage connections</Link></p> : null}
    {remotes.data?.items.map((remote) => <div className="settings-row" key={remote.name}>
      <div className="settings-row__copy"><span className="settings-row__title">{remote.name}</span><span className="settings-row__detail">{remote.url}</span></div>
      <Link className="btn" to={withRemote(surfaceById('instance-config').path, remote.name)}>Open owner configuration</Link>
    </div>)}
    <p className="field__hint">Configuration status is checked directly with the owner after connecting as yourself. Removing a connection leaves the owner’s project and running configuration intact.</p>
  </Panel>;
}

type Decision = { intent: SelfConfigIntent; idempotencyKey: string; label: string };
function ConfigurationOwner({ status, stale }: { status: SelfConfigStatus; stale: boolean }) {
  const actions = useSelfConfigActions();
  const workspace = useWorkspaceContext();
  const remote = workspace?.remote ?? '';
  const [revision, setRevision] = useState('');
  const [revisionChosen, setRevisionChosen] = useState(false);
  const [recipient, setRecipient] = useState('');
  const [confirmRestored, setConfirmRestored] = useState(false);
  const [decision, setDecision] = useState<Decision | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const revisionId = useId();
  const recipientId = useId();
  const binding = status.binding;
  const candidate = revisionNumber(revision);
  const busy = actions.adopt.isPending || actions.apply.isPending || actions.test.isPending;
  const unsettled = status.job !== null && (status.job.state === 'preparing' || status.job.state === 'pending' || status.job.state === 'partial');
  const recovering = status.state === 'recovery_required';
  useEffect(() => { if (!revisionChosen && status.latest_revision !== null) setRevision(String(status.latest_revision)); }, [status.latest_revision, revisionChosen]);
  const choose = (action: 'apply' | 'mail-test') => {
    if (binding === null || candidate === null || stale) return;
    setDone(null); setFailure(null);
    setDecision({ idempotencyKey: crypto.randomUUID(), label: action === 'apply' ? `Apply revision r${candidate}` : `Test email with revision r${candidate}`, intent: { action, owner_instance_id: status.owner_instance_id, revision: candidate, schema_version: binding.schema_version, expected_generation: status.generation, preview_token: '', to: action === 'mail-test' ? recipient.trim() : '', confirm_restored_credentials: action === 'apply' && recovering && confirmRestored } });
  };
  const execute = async (selected: Decision) => {
    const intent = selected.intent;
    if (intent.action === 'adopt') {
      await actions.adopt.mutateAsync({ preview_token: intent.preview_token, idempotency_key: selected.idempotencyKey });
      setDone('Configuration adopted. Open the project to edit settings.');
    } else if (intent.action === 'apply') {
      const result = await actions.apply.mutateAsync({ revision: intent.revision, expected_generation: intent.expected_generation, schema_version: intent.schema_version, idempotency_key: selected.idempotencyKey, confirm_restored_credentials: intent.confirm_restored_credentials });
      setDone(result.state === 'active' ? `Revision r${intent.revision} is active.` : `Apply requested for r${intent.revision}. Watch node status for convergence.`);
    } else {
      const result = await actions.test.mutateAsync({ revision: intent.revision, expected_generation: intent.expected_generation, schema_version: intent.schema_version, to: intent.to });
      setDone(result.sent ? `Test email sent using r${result.revision}. Active configuration is unchanged by a test.` : `The owner did not confirm delivery using r${result.revision}.`);
    }
    setDecision(null);
  };
  return <>
    {failure === null ? null : <Alert>{failure}</Alert>}
    {done === null ? null : <Done>{done}</Done>}
    <Panel id="configuration-owner" title={workspace === null ? 'This instance' : workspace.remote}>
      <p className="settings-note">Owner <code>{status.owner_instance_id}</code></p>
      <div className="settings-row"><div className="settings-row__copy"><span className="settings-row__title">{stateLabel(status.state)}</span><span className="settings-row__detail">Generation {String(status.generation)} · Desired {status.desired_revision === null ? 'none' : `r${status.desired_revision}`} · Latest published {status.latest_revision === null ? 'none' : `r${status.latest_revision}`}</span></div></div>
      {status.state === 'partial' ? <Alert>The committed revision is still converging. Some nodes have not acknowledged it. This is not a completed apply.</Alert> : null}
      {status.state === 'recovery_required' ? <Alert>Outbound configuration use is fenced after restore. Review credentials and reconcile access grants, then confirm the selected revision to resume.</Alert> : null}
      {binding === null ? <>
        <p>Adopt the server’s effective settings into a protected Hikyo project once. Preview lists key names only.</p>
        <button type="button" className="btn btn--primary" disabled={actions.preview.isPending || stale} onClick={() => actions.preview.mutate(undefined, { onError: (error) => setFailure(selfConfigFailure(error)) })}>Preview adoption</button>
        {actions.preview.data === undefined ? null : <div className="self-config-preview">
          <h3>Adoption preview</h3>
          <p>Schema version {actions.preview.data.schema_version}. Import these configured keys:</p>
          <ul>{actions.preview.data.configured_keys.map((key) => <li key={key}><code>{key}</code></li>)}</ul>
          {actions.preview.data.warnings.map((warning) => <p key={warning}>{warning}</p>)}
          <button type="button" className="btn btn--primary" disabled={busy || stale} onClick={() => {
            const preview = actions.preview.data;
            if (preview === undefined) return;
            setDecision({ label: 'Adopt this configuration', idempotencyKey: crypto.randomUUID(), intent: { action: 'adopt', owner_instance_id: preview.owner_instance_id, schema_version: preview.schema_version, expected_generation: 0n, revision: 0n, preview_token: preview.preview_token, to: '', confirm_restored_credentials: false } });
          }}>Adopt previewed configuration</button>
        </div>}
      </> : <>
        <div className="panel__actions">
          <Link className="btn btn--primary" to={withRemote(generatePath(surfaceById('matrix').path, { org: binding.org_id, project: binding.project_id }), remote)}>Edit configuration project</Link>
          <Link className="btn" to={withRemote(`${generatePath(surfaceById('history').path, { org: binding.org_id, project: binding.project_id })}?environment=${encodeURIComponent(binding.environment_id)}`, remote)}>History and rollback</Link>
        </div>
        <p className="field__hint">Saving drafts and publishing keep the running settings unchanged. To roll back, restore history into drafts, publish, then apply the new revision.</p>
        {recovering ? <label className="field"><span><input type="checkbox" checked={confirmRestored} onChange={(event) => setConfirmRestored(event.target.checked)} /> I reviewed the restored credentials and reconciled access grants on this owner.</span></label> : null}
        <div className="self-config-controls">
          <div className="field"><label htmlFor={revisionId}>Published revision to apply or test</label><input id={revisionId} inputMode="numeric" pattern="[1-9][0-9]*" value={revision} onChange={(event) => { setRevisionChosen(true); setRevision(event.target.value); }} /></div>
          <button className="btn btn--primary" type="button" disabled={candidate === null || busy || stale || unsettled || (recovering && !confirmRestored)} onClick={() => choose('apply')}>Apply selected revision</button>
        </div>
        <div className="self-config-controls">
          <div className="field"><label htmlFor={recipientId}>Test email recipient</label><input id={recipientId} type="email" value={recipient} onChange={(event) => setRecipient(event.target.value)} /></div>
          <button className="btn" type="button" disabled={candidate === null || recipient.trim() === '' || busy || stale} onClick={() => choose('mail-test')}>Send test email</button>
          {status.desired_revision === null ? null : <button className="btn" type="button" onClick={() => { setRevisionChosen(true); setRevision(String(status.desired_revision)); }}>Select committed target r{String(status.desired_revision)}</button>}
        </div>
      </>}
    </Panel>
    {status.nodes.length === 0 ? null : <Panel id="configuration-nodes" title="Nodes on this instance">
      {status.nodes.map((node) => <div className="settings-row" key={node.node_id}>
        <div className="settings-row__copy"><span className="settings-row__title mono">{node.node_id}</span><span className="settings-row__detail">{node.state} · {node.active_revision === null ? 'Active revision unknown' : `r${node.active_revision}`} · generation {String(node.active_generation)} · last checked {new Date(node.updated_at).toLocaleString()}</span>{node.error === undefined ? null : <span className="settings-row__detail">{node.error}</span>}</div>
      </div>)}
      {status.job === null ? null : <p className="settings-note">Last apply: {status.job.id} · {status.job.state} · r{String(status.job.revision)}{status.job.error === undefined ? '' : ` · ${status.job.error}`}</p>}
    </Panel>}
    {decision === null ? null : <SelfConfigCeremony key={decision.idempotencyKey} decision={decision} onComplete={() => execute(decision)} onCancel={() => setDecision(null)} />}
  </>;
}

function SelfConfigCeremony({ decision, onComplete, onCancel }: { decision: Decision; onComplete: () => Promise<void>; onCancel: () => void }) {
  const first = useRef<HTMLInputElement>(null);
  const dialog = useModalDialog(first);
  const titleId = useId();
  const codeId = useId();
  const [code, setCode] = useSensitiveState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const transport = useTransport();
  const workspace = useWorkspaceContext();
  const current = useRef(true);
  useEffect(() => { current.current = true; return () => { current.current = false; }; }, []);
  const confirm = async (factor: 'totp' | 'passkey') => {
    setBusy(true); setFailure(null);
    const acting = workspace === null ? null : workspaceSession(workspace.origin);
    try {
      const result = await reauthenticateSelfConfig(decision.intent, factor === 'totp' ? { kind: 'totp', code } : { kind: 'passkey' }, transport);
      setCode('');
      if (!current.current) return;
      if (workspace !== null) {
        if (acting === undefined || acting === null || workspaceSession(workspace.origin) !== acting || result.session_token === undefined || result.session_id !== acting.bearer.session) throw new Error('The owner session changed. Reconnect and retry.');
        rememberWorkspace({ ...acting.bearer, value: result.session_token });
      }
      await onComplete();
    } catch (error) {
      if (current.current) setFailure(error instanceof Error ? selfConfigFailure(error) : 'Authorization did not complete.');
    } finally { if (current.current) setBusy(false); }
  };
  return <dialog ref={dialog} className="ceremony" aria-labelledby={titleId} onCancel={(event) => { event.preventDefault(); if (!busy) onCancel(); }}>
    <h2 id={titleId}>{decision.label}</h2>
    <p>Owner <code>{decision.intent.owner_instance_id}</code>. This authorization covers only this decision at generation {String(decision.intent.expected_generation)} and schema version {decision.intent.schema_version}.</p>
    {decision.intent.confirm_restored_credentials ? <p>This decision confirms that the restored credentials were reviewed and access grants reconciled, and clears the outbound configuration fence.</p> : null}
    {decision.intent.action === 'mail-test' ? <p>Send one message to <strong>{decision.intent.to}</strong>.</p> : null}
    <div className="field"><label htmlFor={codeId}>Fresh authenticator code</label><input ref={first} id={codeId} inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value)} disabled={busy} /></div>
    {failure === null ? null : <Alert>{failure}</Alert>}
    <div className="ceremony__actions"><button type="button" className="btn btn--primary" disabled={busy || code.trim() === ''} onClick={() => void confirm('totp')}>{busy ? 'Authorizing…' : 'Authorize with code'}</button>
      {workspace === null ? <button type="button" className="btn" disabled={busy} onClick={() => void confirm('passkey')}>Authorize with passkey</button> : <a className="btn" href={`${workspace.origin}${surfaceById('instance-config').path}`} target="_blank" rel="noreferrer">Use a passkey on owner</a>}
      <button type="button" className="btn" disabled={busy} onClick={onCancel}>Cancel</button></div>
  </dialog>;
}

function stateLabel(state: SelfConfigStatus['state']): string {
  switch (state) {
    case 'unmanaged': return 'Using startup configuration';
    case 'active': return 'Applied configuration active';
    case 'pending': return 'Apply pending';
    case 'partial': return 'Partially applied';
    case 'recovery_required': return 'Recovery required';
  }
}

export function SystemProjectNotice({ org, project }: { org: string; project: string }) {
  const query = useSelfConfig();
  const workspace = useWorkspaceContext();
  if (query.data?.binding?.org_id !== org || query.data.binding.project_id !== project) return null;
  return <p className="self-config-notice"><strong>Hikyo system configuration</strong> · {stateLabel(query.data.state)} · Publishing does not apply settings. <Link to={withRemote(surfaceById('instance-config').path, workspace?.remote ?? '')}>Review and apply</Link></p>;
}
