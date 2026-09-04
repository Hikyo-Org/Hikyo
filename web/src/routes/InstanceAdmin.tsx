import {
  getCredentialPolicyOp,
  getRetentionHealthOp,
  setCredentialPolicyOp,
} from '@hikyo/operations';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useEffect, useId, useState } from 'react';
import { generatePath, Link } from 'react-router';

import { ApiError, parsed } from '../api/client.ts';
import { retentionBanner } from '../api/retention.ts';
import {
  cryptoFailureText,
  settingsFailureText,
  settingsOperationFailure,
  useCreateOrg,
  useInstanceOrgs,
  useReencryptInstance,
  useRotateDek,
  useRotateMasterKey,
  useRotateRootKey,
  useRotateScanningKey,
  useRotateTokenKey,
} from '../api/settings.ts';
import { notifySuccess } from '../app/notifications.tsx';
import { surfaceById } from '../app/navigation.ts';
import { FederationIssuersPanel } from './FederationIssuersPanel.tsx';
import { OidcProvidersPanel } from './OidcProvidersPanel.tsx';
import { SamlProvidersPanel } from './SamlProvidersPanel.tsx';
import { SamlSpKeysPanel } from './SamlSpKeysPanel.tsx';
import { Alert, ConsequencesDialog, Done, JumpIndex, Panel } from './Sections.tsx';
import { useFeedback } from './useModalDialog.ts';
import { useReencryptDrain } from './useReencryptDrain.ts';

const credentialPolicyKey = ['instance-credential-policy'] as const;
const instanceRetentionKey = ['instance-retention-health'] as const;
function useCredentialPolicy() {
  return useQuery({
    queryKey: credentialPolicyKey,
    queryFn: () => parsed(getCredentialPolicyOp, {}),
    retry: false,
  });
}

function useSetCredentialPolicy() {
  return useMutation({
    mutationFn: (input: { maxFiniteLifetimeSeconds: number; allowIndefinite: boolean; maxLiveCredentials: number; confirm: boolean }) =>
      parsed(setCredentialPolicyOp, {
        body: {
          max_finite_lifetime_seconds: input.maxFiniteLifetimeSeconds,
          allow_indefinite: input.allowIndefinite,
          max_live_credentials: input.maxLiveCredentials,
          ...(input.confirm ? { confirm: true } : {}),
        },
      }),
  });
}

function useInstanceRetentionHealth() {
  return useQuery({
    queryKey: instanceRetentionKey,
    queryFn: () => parsed(getRetentionHealthOp, {}),
    retry: false,
  });
}

const secondFactor = (error: unknown) => error instanceof ApiError && error.status === 403;
const nondisclosed = (error: unknown) => error instanceof ApiError && error.status === 404;
class SurfaceMessage extends Error {}
const instanceFailureText = (error: unknown) =>
  error instanceof SurfaceMessage
    ? error.message
    : settingsFailureText(error, 'set-credential-policy');

/**
 * Instance settings (#60, #567): instance-wide policy, identity providers,
 * federation and key custody. Membership and instance grants are the sibling
 * surface `/instance/members`, so every scope is a {Members, Settings} pair.
 */
export function InstanceAdmin() {
  const orgs = useInstanceOrgs();
  const health = useInstanceRetentionHealth();
  const policy = useCredentialPolicy();
  const nameId = useId();
  const { failure, done, report, ok } = useFeedback(instanceFailureText);
  const [name, setName] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const create = useCreateOrg((org) => {
    const message = `Created ${org.name} and granted you organisation admin access. Sign in again to use the new authority.`;
    setName('');
    setShowCreate(false);
    notifySuccess(message);
    ok(message);
  });
  const stale = retentionBanner(health.data, health.isError);

  return <div className="page page--chrome">
    <h1>Instance settings</h1>
    <p className="page__lede">
      Instance-wide policy, identity providers, federation and key custody. Every operation here is
      the same grant-evaluated network operation the CLI verb calls; membership and instance grants
      have their own surface.
    </p>
    <JumpIndex sections={[
      { id: 'instance-orgs', label: 'Organisations' },
      { id: 'instance-members', label: 'Members' },
      { id: 'instance-settings', label: 'Policy' },
      { id: 'instance-oidc', label: 'Identity providers' },
      { id: 'instance-federation', label: 'Federation' },
      { id: 'instance-retention', label: 'Retention health' },
      { id: 'instance-keys', label: 'Keys & crypto' },
      { id: 'instance-saml-providers', label: 'SAML providers' },
      { id: 'instance-saml-sp-keys', label: 'SP signing keys' },
    ]} />
    {failure !== null ? <Alert>{failure}</Alert> : null}
    {done !== null ? <Done>{done}</Done> : null}

    <Panel id="instance-orgs" title="Organisations">
      {orgs.isPending ? <p role="status">Loading organisations…</p> : null}
      {secondFactor(orgs.error) ? <Alert>Listing every organisation on this instance needs a second factor. This session does not have sufficient second-factor assurance; present your authenticator code or passkey in the banner above.</Alert> : null}
      {nondisclosed(orgs.error) ? <p role="status">The organisation directory is not disclosed to this session.</p> : null}
      {orgs.isError && !secondFactor(orgs.error) && !nondisclosed(orgs.error) ? <Alert>{settingsFailureText(orgs.error, 'list-instance-orgs')}</Alert> : null}
      {orgs.isSuccess ? orgs.data.items.map((org) => (
        <div className="settings-row settings-row--compact" key={org.id}>
          <div className="settings-row__copy">
            <Link className="settings-row__title settings-row__title--link" to={generatePath(surfaceById('org-settings').path, { org: org.id })}>{org.name}</Link>
            <span className="settings-row__detail">Organisation settings</span>
          </div>
          <span className="settings-row__spacer" />
          <span className="settings-tag mono">{org.active ? 'active' : 'inactive'}</span>
        </div>
      )) : null}
      {showCreate ? (
        <div className="settings-row">
          <div className="field settings-row__spacer"><label htmlFor={nameId}>New organisation name</label><input id={nameId} value={name} onChange={(event) => setName(event.target.value)} /></div>
          <button type="button" className="btn btn--primary" aria-label="Create organisation" disabled={create.isPending || name.trim() === ''} onClick={() => create.mutate({ name: name.trim() }, {
            onError: (error) => report(settingsOperationFailure('create-org', error)),
          })}>Create organisation</button>
        </div>
      ) : null}
      <div className="instance-create-row">
        <button type="button" className="btn btn--primary" aria-label="Open create organisation form" onClick={() => setShowCreate((visible) => !visible)}>+ create organisation</button>
        <code className="instance-cli">$ hikyo org create</code>
      </div>
    </Panel>

    {/* The same entry-point panel organisation and project settings carry:
        granting, revoking and inspection live on the members surface. */}
    <Panel id="instance-members" title="Members">
      <div className="settings-row">
        <div className="settings-row__copy">
          <span className="settings-row__title">Members</span>
          <span className="settings-row__detail">Instance-scope grants inherit downward into every organisation</span>
        </div>
        <span className="settings-row__spacer" />
        <Link className="btn" to={surfaceById('instance-members').path}>open members →</Link>
      </div>
      <p className="settings-note">Entry point only: granting, revoking and inspection live on the members surface.</p>
    </Panel>

    <CredentialPolicyPanel query={policy} onDone={ok} onFailure={report} />

    <OidcProvidersPanel />

    <FederationIssuersPanel />

    <Panel id="instance-retention" title="Retention health">
      {health.isPending ? <p role="status">Loading retention health…</p> : null}
      {secondFactor(health.error) ? <Alert>Retention health requires a second factor for this session.</Alert> : null}
      {nondisclosed(health.error) ? <p role="status">Retention health is not disclosed to this session.</p> : null}
      {health.isError && !secondFactor(health.error) && !nondisclosed(health.error) ? <Alert>{settingsFailureText(health.error, 'get-retention-health')}</Alert> : null}
      {health.isSuccess ? <><p role="status">{stale?.kind === 'stale'
        ? health.data.last_prune_success === null ? 'Payload pruning has never succeeded — retention bounds are not being enforced.' : `Payload pruning has not succeeded since ${new Date(health.data.last_prune_success).toLocaleString()} — retention bounds are not being enforced.`
        : `Payload pruning last succeeded ${health.data.last_prune_success === null ? 'never' : new Date(health.data.last_prune_success).toLocaleString()}.`}</p>
        <p className="field__hint">A run is stale after {health.data.stale_after_seconds / 3600} hours. The same fact is a Prometheus gauge and a <span className="mono">hikyo doctor</span> row.</p></> : null}
    </Panel>

    <Panel id="instance-keys" title="Keys &amp; crypto">
      <CryptoMaintenance onDone={ok} />
    </Panel>
    <SamlProvidersPanel />
    <SamlSpKeysPanel />
  </div>;
}

function CredentialPolicyPanel({ query, onDone, onFailure }: { query: ReturnType<typeof useCredentialPolicy>; onDone: (message: string) => void; onFailure: (error: unknown) => void }) {
  const update = useSetCredentialPolicy();
  const [editing, setEditing] = useState(false);
  const finiteId = useId(); const liveId = useId(); const indefiniteId = useId();
  const [finite, setFinite] = useState(''); const [live, setLive] = useState(''); const [indefinite, setIndefinite] = useState(false);
  type PolicyProposal = { readonly maxFiniteLifetimeSeconds: number; readonly allowIndefinite: boolean; readonly maxLiveCredentials: number };
  type PolicyPreview = { readonly result: Awaited<ReturnType<typeof update.mutateAsync>>; readonly proposal: PolicyProposal };
  const [preview, setPreview] = useState<PolicyPreview | null>(null);
  useEffect(() => { if (query.data !== undefined) { setFinite(String(query.data.max_finite_lifetime_seconds)); setLive(String(query.data.max_live_credentials)); setIndefinite(query.data.allow_indefinite); } }, [query.data]);
  const submit = (confirm: boolean, confirmedProposal?: PolicyProposal) => {
    const proposal = confirmedProposal ?? { maxFiniteLifetimeSeconds: Number(finite), allowIndefinite: indefinite, maxLiveCredentials: Number(live) };
    if (!Number.isInteger(proposal.maxFiniteLifetimeSeconds) || proposal.maxFiniteLifetimeSeconds < 1 || !Number.isInteger(proposal.maxLiveCredentials) || proposal.maxLiveCredentials < 1) return onFailure(new SurfaceMessage('Credential policy values must be whole numbers of at least one.'));
    update.mutate({ ...proposal, confirm }, { onSuccess: (result) => {
      if (!result.applied) return setPreview({ result, proposal });
      setPreview(null);
      void query.refetch();
      onDone(`Credential policy updated. ${String(result.clamped_count)} live credential${result.clamped_count === 1 ? ' was' : 's were'} clamped.`);
    }, onError: onFailure });
  };
  return <Panel id="instance-settings" title="Policy">
    {query.isPending ? <p role="status">Loading credential policy…</p> : null}
    {secondFactor(query.error) ? <Alert>Credential policy requires a second factor.</Alert> : null}
    {nondisclosed(query.error) ? <p role="status">Credential policy is not disclosed to this session.</p> : null}
    {query.isError && !secondFactor(query.error) && !nondisclosed(query.error) ? <Alert>{settingsFailureText(query.error, 'get-credential-policy')}</Alert> : null}
    {query.isSuccess ? <>
      <div className="settings-row">
        <div className="settings-row__copy"><span className="settings-row__title">Machine-credential ceiling</span><span className="settings-row__detail">authoritative instance policy; clamps every org value</span></div>
        <span className="settings-row__spacer" /><code className="mono">{String(query.data.max_finite_lifetime_seconds)}s · {String(query.data.max_live_credentials)} live max · {query.data.allow_indefinite ? 'indefinite allowed' : 'finite only'}</code><button type="button" className="btn" onClick={() => setEditing(true)}>edit</button>
      </div>
      <p className="settings-note">Server URL and audit-retention settings are not exposed by this API.</p>
    </> : null}
    {editing ? <>
      {query.isSuccess ? <>
        <div className="field"><label htmlFor={finiteId}>Maximum finite lifetime (seconds)</label><input id={finiteId} inputMode="numeric" value={finite} onChange={(event) => { setPreview(null); setFinite(event.target.value); }} /></div>
        <div className="field"><label htmlFor={liveId}>Maximum live credentials per service account</label><input id={liveId} inputMode="numeric" value={live} onChange={(event) => { setPreview(null); setLive(event.target.value); }} /></div>
        <div className="field chk"><input id={indefiniteId} type="checkbox" checked={indefinite} onChange={(event) => { setPreview(null); setIndefinite(event.target.checked); }} /><label htmlFor={indefiniteId}>Allow credentials with no expiry</label></div>
        {preview === null ? null : <div className="policy-impact" role="alert"><p>This tightening affects {preview.result.affected.length} live credential{preview.result.affected.length === 1 ? '' : 's'}. Nothing has changed yet.</p><ul>{preview.result.affected.map((credential) => <li key={credential.id} className="mono">{credential.id} — {credential.reason}</li>)}</ul><button type="button" className="btn btn--danger" disabled={update.isPending} onClick={() => submit(true, preview.proposal)}>Apply and affect these credentials</button></div>}
        <div className="panel__actions"><button type="button" className="btn" onClick={() => setEditing(false)}>Cancel</button><button type="button" className="btn btn--primary" disabled={update.isPending} onClick={() => submit(false)}>Save credential policy</button></div>
      </> : null}
    </> : null}
  </Panel>;
}

type CryptoCeremony =
  | 'token'
  | 'scanning'
  | 'master'
  | 'dek-instance'
  | 'root-prepare'
  | 'root-verify'
  | 'root-finalize';

/**
 * CryptoMaintenance exposes every remotely operable rotation and re-encryption
 * job (#503). Each is the same grant-evaluated network operation the CLI verb
 * calls; the host-only set (`init`, `migrate`, restore reconciliation,
 * break-glass, host-file custody, startup-only key material) stays absent.
 */
function CryptoMaintenance({ onDone }: { onDone: (message: string) => void }) {
  const token = useRotateTokenKey();
  const scanning = useRotateScanningKey();
  const master = useRotateMasterKey();
  const dek = useRotateDek();
  const reencrypt = useReencryptInstance();
  const root = useRotateRootKey();
  const titleId = useId();
  const [ceremony, setCeremony] = useState<CryptoCeremony | null>(null);
  const [dialogFailure, setDialogFailure] = useState<string | null>(null);
  const open = (which: CryptoCeremony) => { setDialogFailure(null); setCeremony(which); };
  const close = () => { setDialogFailure(null); setCeremony(null); };

  const drain = useReencryptDrain(reencrypt, { operation: 'reencrypt-instance', noun: 'Instance', onDone });

  const busy = token.isPending || scanning.isPending || master.isPending || dek.isPending || root.isPending;

  return <div className="crypto-maintenance">
    <div className="settings-row">
      <div className="settings-row__copy"><span className="settings-row__title">Change-token key</span><span className="settings-row__detail">Rotating it invalidates every client cursor: the next fetch from every workload is a full one. No restart wave, no downtime.</span></div>
      <span className="settings-row__spacer" /><code className="instance-cli">$ hikyo rotate-token-key</code>
      <button type="button" className="btn" onClick={() => open('token')}>Rotate the change-token key</button>
    </div>

    <div className="settings-row">
      <div className="settings-row__copy"><span className="settings-row__title">Secret-scanning key</span><span className="settings-row__detail">Rotating it drops every scan dismissal in the same transaction; suppressed warns re-fire, because their fingerprints are no longer recomputable.</span></div>
      <span className="settings-row__spacer" /><code className="instance-cli">$ hikyo rotate-scanning-key</code>
      <button type="button" className="btn" onClick={() => open('scanning')}>Rotate the scanning key</button>
    </div>

    <div className="settings-row">
      <div className="settings-row__copy"><span className="settings-row__title">Master key</span><span className="settings-row__detail">Re-wraps every tier-3 key (all DEKs and the root token key) under a new master, then retires the old one. Refused while the root key is dual-wrapped — finalize the root rotation first.</span></div>
      <span className="settings-row__spacer" /><code className="instance-cli">$ hikyo rotate-master-key</code>
      <button type="button" className="btn" onClick={() => open('master')}>Rotate the master key</button>
    </div>

    <div className="settings-row">
      <div className="settings-row__copy"><span className="settings-row__title">Data-encryption key (instance)</span><span className="settings-row__detail">Appends a new instance DEK version. New writes seal under it immediately; existing ciphertext stays readable until you re-encrypt. A rotation is incomplete without the re-encryption below.</span></div>
      <span className="settings-row__spacer" /><code className="instance-cli">$ hikyo rotate-dek --scope instance</code>
      <button type="button" className="btn" onClick={() => open('dek-instance')}>Rotate the instance DEK</button>
    </div>

    <div className="settings-row">
      <div className="settings-row__copy"><span className="settings-row__title">Instance re-encryption</span><span className="settings-row__detail">Walks every instance credential ciphertext onto the active DEK version and retires the superseded ones — the completion of an instance DEK rotation. Chunked and resumable: safe to re-run, and complete once it moves no rows.</span></div>
      <span className="settings-row__spacer" /><code className="instance-cli">$ hikyo reencrypt</code>
      <button type="button" className="btn" disabled={drain.running} onClick={drain.run}>{drain.running ? 'Re-encrypting…' : 'Re-encrypt the instance'}</button>
    </div>
    {drain.running ? <p role="status" className="field__hint">Re-encrypting… run {drain.runs}, {String(drain.total)} row{drain.total === 1n ? '' : 's'} moved so far. Safe to leave and resume later.</p> : null}
    {drain.failure === null ? null : <Alert>{drain.failure}</Alert>}

    <div className="settings-row">
      <div className="settings-row__copy">
        <span className="settings-row__title">Root-key rotation</span>
        <span className="settings-row__detail">Three crash-safe phases over a dual-wrapped master. No key material crosses the wire. Between <strong>prepare</strong> and <strong>verify</strong> you install the new root at the primary source on the host. The instance stays bootable under either root and warns on every start until <strong>finalize</strong>. Run the phases in order — the server refuses one run out of turn.</span>
      </div>
      <span className="settings-row__spacer" /><code className="instance-cli">$ hikyo rotate-root-key</code>
      <div className="crypto-phases">
        <button type="button" className="btn" onClick={() => open('root-prepare')}>Prepare</button>
        <button type="button" className="btn" onClick={() => open('root-verify')}>Verify</button>
        <button type="button" className="btn" onClick={() => open('root-finalize')}>Finalize</button>
      </div>
    </div>

    <p className="field__hint"><span className="mono">init</span>, <span className="mono">migrate</span>, restore reconciliation, break-glass, host-file custody and startup-only key material are local host authority. They are deliberately absent from every network surface — CLI-at-the-box, not CLI-over-network.</p>

    {ceremony === 'token' ? <ConsequencesDialog titleId={titleId} title="Rotate the change-token key?" confirmLabel="Rotate the key" busyLabel="Rotating the change-token key…" busy={busy} failure={dialogFailure} onCancel={close} onConfirm={() => {
      setDialogFailure(null);
      token.mutate(undefined, { onSuccess: (result) => { onDone(`The change-token key was rotated (version ${String(result.token_key_version)}). Every client cursor is invalid; the next fetch from each workload is a full one.`); close(); }, onError: (error) => setDialogFailure(cryptoFailureText(error, 'rotate-token-key')) });
    }}>
      <p>Every conditional-fetch cursor in circulation stops matching. The next fetch from every workload is a full one, and nothing restarts. This cannot be undone by rotating back.</p>
    </ConsequencesDialog> : null}

    {ceremony === 'scanning' ? <ConsequencesDialog titleId={titleId} title="Rotate the secret-scanning key?" confirmLabel="Rotate the key" busyLabel="Rotating the scanning key…" busy={busy} failure={dialogFailure} onCancel={close} onConfirm={() => {
      setDialogFailure(null);
      scanning.mutate(undefined, { onSuccess: (result) => { onDone(`The secret-scanning key was rotated (version ${String(result.scanning_key_version)}). ${String(result.dismissals_dropped)} dismissal${result.dismissals_dropped === 1n ? ' was' : 's were'} dropped; their warns will re-fire.`); close(); }, onError: (error) => setDialogFailure(cryptoFailureText(error, 'rotate-scanning-key')) });
    }}>
      <p>Every stored scan fingerprint becomes unrecomputable under the new key, so every dismissal is dropped in the same transaction and the warns they suppressed will fire again. This cannot be undone.</p>
    </ConsequencesDialog> : null}

    {ceremony === 'master' ? <ConsequencesDialog titleId={titleId} title="Rotate the master key?" confirmLabel="Rotate the key" busyLabel="Rotating the master key…" busy={busy} failure={dialogFailure} onCancel={close} onConfirm={() => {
      setDialogFailure(null);
      master.mutate(undefined, { onSuccess: (result) => { onDone(`The master key was rotated (version ${String(result.key_version)}). Every tier-3 key is now wrapped under it.`); close(); }, onError: (error) => setDialogFailure(cryptoFailureText(error, 'rotate-master-key')) });
    }}>
      <p>A new master key is generated, every tier-3 key is re-wrapped under it, and the old master is retired after a zero-reference check. This is refused while the root key is dual-wrapped — finalize the root rotation first.</p>
    </ConsequencesDialog> : null}

    {ceremony === 'dek-instance' ? <ConsequencesDialog titleId={titleId} title="Rotate the instance DEK?" confirmLabel="Rotate the DEK" busyLabel="Rotating the instance DEK…" busy={busy} failure={dialogFailure} onCancel={close} onConfirm={() => {
      setDialogFailure(null);
      dek.mutate({ scope: 'instance' }, { onSuccess: (result) => { onDone(`The instance DEK was rotated (version ${String(result.key_version)}). New writes seal under it; existing ciphertext stays readable until you run the instance re-encryption to complete the rotation.`); close(); }, onError: (error) => setDialogFailure(cryptoFailureText(error, 'rotate-dek')) });
    }}>
      <p>A new instance DEK version is appended. New writes seal under it immediately; existing ciphertext stays readable under the previous version until the instance re-encryption walks it forward. The rotation is incomplete until you run that re-encryption.</p>
    </ConsequencesDialog> : null}

    {ceremony === 'root-prepare' ? <ConsequencesDialog titleId={titleId} title="Prepare the root-key rotation?" confirmLabel="Prepare" busyLabel="Preparing the root-key rotation…" busy={busy} failure={dialogFailure} onCancel={close} onConfirm={() => {
      setDialogFailure(null);
      root.mutate('prepare', { onSuccess: (result) => { onDone(`Root-key rotation prepared (epoch ${String(result.root_key_epoch)}). Install the new root at the primary source on the host, then run verify. The instance stays bootable under either root and warns on every start until finalize.`); close(); }, onError: (error) => setDialogFailure(cryptoFailureText(error, 'rotate-root-key')) });
    }}>
      <p>Prepare reads the new root from the server-side source and seals a second master wrapper. No key material crosses the wire. After this you must install the new root at the primary source on the host, then run verify. The instance stays bootable under either root until you finalize.</p>
    </ConsequencesDialog> : null}

    {ceremony === 'root-verify' ? <ConsequencesDialog titleId={titleId} title="Verify the root-key rotation?" confirmLabel="Verify" busyLabel="Verifying the root-key rotation…" busy={busy} failure={dialogFailure} onCancel={close} onConfirm={() => {
      setDialogFailure(null);
      root.mutate('verify', { onSuccess: (result) => { onDone(`Root-key rotation verified (epoch ${String(result.root_key_epoch)}). The primary source now unwraps the new root. Run finalize to retire the old wrapper.`); close(); }, onError: (error) => setDialogFailure(cryptoFailureText(error, 'rotate-root-key')) });
    }}>
      <p>Verify re-reads the primary source and confirms it now unwraps the new wrapper you sealed in prepare. Run this only after installing the new root at the primary source on the host. If it has not been installed yet, this phase is refused.</p>
    </ConsequencesDialog> : null}

    {ceremony === 'root-finalize' ? <ConsequencesDialog titleId={titleId} title="Finalize the root-key rotation?" confirmLabel="Finalize" busyLabel="Finalizing the root-key rotation…" busy={busy} failure={dialogFailure} onCancel={close} onConfirm={() => {
      setDialogFailure(null);
      root.mutate('finalize', { onSuccess: (result) => { onDone(`Root-key rotation finalized (epoch ${String(result.root_key_epoch)}). The old wrapper is retired and the startup warning clears.`); close(); }, onError: (error) => setDialogFailure(cryptoFailureText(error, 'rotate-root-key')) });
    }}>
      <p>Finalize retires the old master wrapper, leaving the instance wrapped under the new root only. The startup warning clears. Run this only after verify has succeeded.</p>
    </ConsequencesDialog> : null}
  </div>;
}
