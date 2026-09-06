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
    <p className="page__lede">Edit settings in the configuration project, publish a revision, then review and apply it. Ordinary settings reload live. Bootstrap source changes use an enrolled controlled rollout.</p>
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
type Preparation = { decision: Decision; jobID?: string };

function matchesDecision(status: SelfConfigStatus, intent: SelfConfigIntent): boolean {
  return status.owner_instance_id === intent.owner_instance_id && status.generation === intent.expected_generation && status.binding?.schema_version === intent.schema_version && (status.state === 'recovery_required') === intent.confirm_restored_credentials;
}
function ConfigurationOwner({ status, stale }: { status: SelfConfigStatus; stale: boolean }) {
  const actions = useSelfConfigActions();
  const workspace = useWorkspaceContext();
  const remote = workspace?.remote ?? '';
  const [revision, setRevision] = useState('');
  const [revisionChosen, setRevisionChosen] = useState(false);
  const [recipient, setRecipient] = useState('');
  const [confirmRestored, setConfirmRestored] = useState(false);
  const [decision, setDecision] = useState<Decision | null>(null);
  const [preparation, setPreparation] = useState<Preparation | null>(null);
  const current = useRef(true);
  const latestStatus = useRef(status);
  latestStatus.current = status;
  useEffect(() => { current.current = true; return () => { current.current = false; }; }, []);
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const revisionId = useId();
  const recipientId = useId();
  const binding = status.binding;
  const candidate = revisionNumber(revision);
  const busy = actions.adopt.isPending || actions.apply.isPending || actions.test.isPending;
  const rolloutUnresolved = status.job?.state === 'partial' && status.job.plan_digest !== undefined && status.job.deployment_restored !== true;
  const unsettled = rolloutUnresolved || status.job !== null && (status.job.state === 'preparing' || status.job.state === 'pending');
  const recovering = status.state === 'recovery_required';
  useEffect(() => { if (!revisionChosen && status.latest_revision !== null) setRevision(String(status.latest_revision)); }, [status.latest_revision, revisionChosen]);
  const prepare = async (selected: Decision, expectedJobID?: string) => {
    setDone(null); setFailure(null);
    const intent = selected.intent;
    setPreparation({ decision: selected, jobID: expectedJobID });
    try {
      const result = await actions.apply.mutateAsync({ revision: intent.revision, expected_generation: intent.expected_generation, schema_version: intent.schema_version, idempotency_key: selected.idempotencyKey, confirm_restored_credentials: intent.confirm_restored_credentials, prepare_only: true });
      if (!current.current) return;
      const job = result.job;
      if (!matchesDecision(result, intent) || !matchesDecision(latestStatus.current, intent) || job === null || job.revision !== intent.revision || job.generation !== intent.expected_generation + 1n || (expectedJobID !== undefined && job.id !== expectedJobID)) {
        setPreparation(null);
        setFailure('The owner, revision or generation changed during preparation. Refresh status and review again.');
        return;
      }
      if (job.state !== 'preparing' || job.error !== undefined) {
        setPreparation(null);
        setFailure('The owner refused or expired this preparation. Review node status, correct the configuration and prepare again.');
        return;
      }
      if (job.prepared !== true) {
        setPreparation({ decision: selected, jobID: job.id });
        return;
      }
      setPreparation({ decision: selected, jobID: job.id });
      setDecision({ ...selected, intent: { ...intent, plan_digest: job.plan_digest } });
    } catch (error) {
      if (current.current) setFailure(error instanceof Error ? selfConfigFailure(error) : 'Preparation did not complete.');
    }
  };
  const choose = (action: 'apply' | 'mail-test') => {
    if (binding === null || candidate === null || stale) return;
    setDone(null); setFailure(null); setRevisionChosen(true);
    const selected: Decision = { idempotencyKey: crypto.randomUUID(), label: action === 'apply' ? `Apply revision r${candidate}` : `Test email with revision r${candidate}`, intent: { action, owner_instance_id: status.owner_instance_id, revision: candidate, schema_version: binding.schema_version, expected_generation: status.generation, preview_token: '', to: action === 'mail-test' ? recipient.trim() : '', confirm_restored_credentials: action === 'apply' && recovering && confirmRestored } };
    if (action === 'apply') void prepare(selected);
    else setDecision(selected);
  };
  const chooseRestore = () => {
    const job = status.job;
    if (binding === null || job?.state !== 'partial' || job.plan_digest === undefined || job.deployment_restore_pending || job.deployment_restored || stale) return;
    setDone(null); setFailure(null);
    setDecision({ idempotencyKey: crypto.randomUUID(), label: 'Restore deployment', intent: { action: 'rollout-restore', owner_instance_id: status.owner_instance_id, revision: job.revision, schema_version: binding.schema_version, expected_generation: status.generation, plan_digest: job.plan_digest, preview_token: '', to: '', confirm_restored_credentials: false } });
  };
  const execute = async (selected: Decision) => {
    const intent = selected.intent;
    if (intent.action === 'adopt') {
      await actions.adopt.mutateAsync({ preview_token: intent.preview_token, idempotency_key: selected.idempotencyKey });
      setDone('Configuration adopted. Open the project to edit settings.');
    } else if (intent.action === 'rollout-restore') {
      if (!matchesDecision(latestStatus.current, intent)) throw new Error('The restore decision changed. Review again.');
      await actions.apply.mutateAsync({ revision: intent.revision, expected_generation: intent.expected_generation, schema_version: intent.schema_version, idempotency_key: selected.idempotencyKey, confirm_restored_credentials: false, plan_digest: intent.plan_digest, restore_deployment: true });
      setDone('Deployment restoration requested. The desired revision remains fenced until you apply a separate repair.');
    } else if (intent.action === 'apply') {
      if (!matchesDecision(latestStatus.current, intent)) throw new Error('The selected decision changed. Prepare again.');
      const result = await actions.apply.mutateAsync({ revision: intent.revision, expected_generation: intent.expected_generation, schema_version: intent.schema_version, idempotency_key: selected.idempotencyKey, confirm_restored_credentials: intent.confirm_restored_credentials, plan_digest: intent.plan_digest });
      if (result.job?.state === 'failed') {
        setPreparation(null);
        setDecision(null);
        setFailure('Preparation expired or failed. Review the current state and prepare again.');
        return;
      }
      if (result.job?.state === 'preparing') {
        setPreparation({ decision: selected, jobID: result.job.id });
        setDecision(null);
        setFailure('Preparation is still pending. Check preparation before authorizing again.');
        return;
      }
      const committed = result.owner_instance_id === intent.owner_instance_id && result.desired_revision === intent.revision && result.generation === intent.expected_generation + 1n;
      setDone(committed && result.state === 'active' && result.job?.state === 'completed' ? `Revision r${intent.revision} is active.` : committed ? `Apply committed for r${intent.revision}. Watch node status for convergence.` : `The owner has not confirmed applying r${intent.revision}. Refresh status before trying again.`);
    } else {
      const result = await actions.test.mutateAsync({ revision: intent.revision, expected_generation: intent.expected_generation, schema_version: intent.schema_version, to: intent.to });
      setDone(result.sent ? `Test email sent using r${result.revision}. Active configuration is unchanged by a test.` : `The owner did not confirm delivery using r${result.revision}.`);
    }
    setPreparation(null);
    setDecision(null);
  };
  return <>
    {failure === null ? null : <Alert>{failure}</Alert>}
    {done === null ? null : <Done>{done}</Done>}
    <Panel id="configuration-owner" title={workspace === null ? 'This instance' : workspace.remote}>
      <p className="settings-note">Owner <code>{status.owner_instance_id}</code></p>
      <div className="settings-row"><div className="settings-row__copy"><span className="settings-row__title">{stateLabel(status.state)}</span><span className="settings-row__detail">Generation {String(status.generation)} · Desired {status.desired_revision === null ? 'none' : `r${status.desired_revision}`} · Latest published {status.latest_revision === null ? 'none' : `r${status.latest_revision}`}</span></div></div>
      {status.state === 'partial' ? <Alert>Some nodes have not applied the committed revision. Review and publish a repair, then apply that revision with fresh authentication. The current target stays in place until the repair passes preparation.</Alert> : null}
      {rolloutUnresolved ? <div className="self-config-preview"><p>The controlled deployment must be restored before a new configuration repair can apply. Restoration keeps the desired revision fenced.</p>{status.job?.deployment_restore_pending ? <p role="status">Deployment restoration is pending controller confirmation.</p> : <button className="btn" type="button" disabled={busy || stale || decision !== null || preparation !== null} onClick={chooseRestore}>Restore deployment</button>}</div> : null}
      {status.job?.deployment_restored ? <p role="status">Deployment resources are restored. Publish and apply a repair revision to resume Hikyo.</p> : null}
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
        <p className="field__hint">Shared settings apply to this instance’s HA nodes. <code>HIKYO_NODE_OVERRIDES</code> holds each node’s listeners, resource limits, backup directory and managed TLS certificate/key contents. Keep an entry for every selected node; independent remote instances use their own projects.</p>
        <p className="field__hint">Changing the public hostname requires a fresh TOTP code and an existing password login on the same administrator account. Preparation does not prove the new hostname is reachable.</p>
        <p className="field__hint">Saving drafts and publishing keep the running settings unchanged. To roll back, restore history into drafts, publish, then apply the new revision.</p>
        {recovering ? <label className="field"><span><input type="checkbox" checked={confirmRestored} disabled={busy || preparation !== null || decision !== null} onChange={(event) => setConfirmRestored(event.target.checked)} /> I reviewed the restored credentials and reconciled access grants on this owner.</span></label> : null}
        <div className="self-config-controls">
          <div className="field"><label htmlFor={revisionId}>Published revision to apply or test</label><input id={revisionId} inputMode="numeric" pattern="[1-9][0-9]*" value={revision} disabled={busy || preparation !== null || decision !== null} onChange={(event) => { setRevisionChosen(true); setRevision(event.target.value); }} /></div>
          <button className="btn btn--primary" type="button" disabled={candidate === null || busy || stale || preparation !== null || decision !== null || unsettled || (recovering && !confirmRestored)} onClick={() => choose('apply')}>Apply selected revision</button>
        </div>
        {actions.apply.isPending && decision === null ? <p role="status">Preparing the exact revision on the selected nodes…</p> : null}
        {preparation === null || decision !== null ? null : <div className="self-config-preview"><p role="status">Preparation for r{String(preparation.decision.intent.revision)} has not been applied. Check node preparation before authorizing. Preparing alone does not authorize Apply.</p><button type="button" className="btn" disabled={busy || stale} onClick={() => void prepare(preparation.decision, preparation.jobID)}>Check preparation</button><button type="button" className="btn" disabled={busy} onClick={() => setPreparation(null)}>Dismiss preparation</button><p className="field__hint">Dismissing leaves the server preparation to expire. It does not apply settings.</p></div>}
        <div className="self-config-controls">
          <div className="field"><label htmlFor={recipientId}>Test email recipient</label><input id={recipientId} type="email" value={recipient} onChange={(event) => setRecipient(event.target.value)} /></div>
          <button className="btn" type="button" disabled={candidate === null || recipient.trim() === '' || busy || stale || preparation !== null || decision !== null} onClick={() => choose('mail-test')}>Send test email</button>
          {status.desired_revision === null ? null : <button className="btn" type="button" disabled={busy || preparation !== null || decision !== null} onClick={() => { setRevisionChosen(true); setRevision(String(status.desired_revision)); }}>Select committed target r{String(status.desired_revision)}</button>}
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
    {decision.intent.action === 'apply' ? <p><strong>{decision.intent.plan_digest === undefined ? 'Reload live' : 'Controlled rollout'}</strong>{decision.intent.plan_digest === undefined ? ': prepared settings take effect after the current operations drain.' : ': bootstrap source changes replace the enrolled deployment. Nodes remain pending until the installed sources are verified.'}</p> : null}
    {decision.intent.plan_digest === undefined ? null : <p>Prepared plan <code className="self-config-plan">{decision.intent.plan_digest}</code>. This exact plan is bound to your one-use authorization.</p>}
    {decision.intent.confirm_restored_credentials ? <p>This decision confirms that the restored credentials were reviewed and access grants reconciled, and clears the outbound configuration fence.</p> : null}
    {decision.intent.action === 'rollout-restore' ? <p>Restore the deployment resources retained for this exact plan. Hikyo’s desired revision stays unchanged and fenced. This does not apply a configuration repair.</p> : null}
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
