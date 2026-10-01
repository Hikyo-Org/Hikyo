import { CeremonyNotice } from '../ui/CeremonyNotice.tsx';
import { useEffect, useRef, useState } from 'react';

import { isoDay } from '../api/identities.ts';
import {
  createSSHCA,
  issueSSHCertificate,
  rotateSSHCA,
  sshIssueFailureText,
  sshKRLPath,
  sshRefusalText,
  sshTrustedKeys,
  useDeleteSSHCA,
  useDeleteSSHProfile,
  useRefreshSSH,
  useRetireSSHCAKey,
  useRevokeSSHCertificate,
  useSaveSSHProfile,
  useSSHCAs,
  useSSHCertificates,
  useSSHProfiles,
  type EnvironmentRef,
  type KeyAlgorithm,
  type SSHCA,
  type SSHCertificate,
  type SSHIssued,
  type SSHProfile,
  type SSHProfileInput,
} from '../api/ssh.ts';
import { useTransport, useWorkspaceContext } from '../api/transport.tsx';
import { runPasskeyCeremony } from '../api/values.ts';
import { writeClipboard } from '../app/clipboard.ts';
import { useNavigationGuard } from '../app/useNavigationGuard.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { useMintLifecycle, type MintBoundaryFields } from './mintLifecycle.ts';

/**
 * The SSH tab of the machine-access page (#155): an environment's SSH user
 * CAs, the profiles that bound what a certificate may say and who may ask, and
 * the certificates issued through them.
 *
 * Two invariants carry over from the rest of the page. A CA private key typed
 * or pasted for import lives only in its dialog's state and is sent by a plain
 * call, never a cached mutation. A generated user private key is display-once:
 * it rides the same mint lifecycle as a lease password, so a route or session
 * change masks it and the dialog will not close until the operator confirms it
 * was stored.
 */

const ALGORITHMS: readonly KeyAlgorithm[] = ['ed25519', 'ecdsa-p256', 'rsa-3072'];
const EXTENSIONS: readonly SSHProfile['extensions'][number][] = [
  'permit-pty',
  'permit-port-forwarding',
  'permit-agent-forwarding',
  'permit-X11-forwarding',
  'permit-user-rc',
];

type EnvOption = { readonly id: string; readonly name: string };
type RequesterOption = { readonly id: string; readonly label: string };

type IssueRequest = MintBoundaryFields & {
  readonly environmentId: string;
  readonly profileId: string;
  readonly profileName: string;
};

type Dialogs =
  | { kind: 'create-ca' }
  | { kind: 'rotate'; ca: SSHCA }
  | { kind: 'trust'; ca: SSHCA }
  | { kind: 'delete-ca'; ca: SSHCA }
  | { kind: 'profile'; profile: SSHProfile | null }
  | { kind: 'delete-profile'; profile: SSHProfile }
  | { kind: 'issue' };

/** statusTone maps a derived certificate status onto a badge tone. */
function statusTone(status: SSHCertificate['status']): 'neutral' | 'danger' | 'changed' | 'ok' {
  switch (status) {
    case 'active':
      return 'ok';
    case 'revoked':
      return 'danger';
    case 'untrusted':
      return 'changed';
    default:
      return 'neutral';
  }
}

function lines(text: string): string[] {
  return text
    .split(/[\n,]/)
    .map((s) => s.trim())
    .filter((s) => s !== '');
}

function seconds(label: string, raw: string): number {
  const n = Number(raw);
  if (!Number.isInteger(n) || n <= 0) {
    throw new Error(`Enter ${label} as a whole number of seconds.`);
  }
  return n;
}

export function SSHCertificatesPanel({
  org,
  project,
  sessionId,
  environments,
  requesterOptions,
}: {
  org: string;
  project: string;
  sessionId: string | null;
  environments: readonly EnvOption[];
  requesterOptions: readonly RequesterOption[];
}) {
  const [environmentId, setEnvironmentId] = useState(environments[0]?.id ?? '');
  const current = environments.find((e) => e.id === environmentId) ?? environments[0];
  const env: EnvironmentRef = { org, project, environment: current?.id ?? '' };
  const cas = useSSHCAs(env);
  const profiles = useSSHProfiles(env);
  const certificates = useSSHCertificates(env);
  const retire = useRetireSSHCAKey(env);
  const revoke = useRevokeSSHCertificate(env);
  const workspace = useWorkspaceContext();
  const [dialog, setDialog] = useState<Dialogs | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const issue = useMintLifecycle<IssueRequest, SSHIssued>({ sessionId, org, project });

  if (current === undefined) {
    return <p role="status">This project has no environments, so it has no SSH CAs.</p>;
  }
  const caItems = cas.data?.items ?? [];
  const profileItems = profiles.data?.items ?? [];
  const certificateItems = certificates.data?.items ?? [];
  const caName = (id: string) => caItems.find((ca) => ca.id === id)?.name ?? id;
  const canAct = sessionId !== null;
  const done = (message: string) => {
    setRefusal(null);
    setNotice(message);
    setDialog(null);
  };

  return (
    <>
      <h2>SSH certificates</h2>
      <p className="machine__lede">
        Short-lived OpenSSH user certificates from an environment&apos;s own CA. Hosts trust the CA
        once through <code>TrustedUserCAKeys</code> and refuse revoked certificates through the KRL
        in <code>RevokedKeys</code>. A CA private key is never shown or returned; a generated user
        key is shown exactly once.
      </p>
      <div className="field">
        <label htmlFor="ssh-environment">Environment</label>
        <select
          id="ssh-environment"
          value={current.id}
          onChange={(event) => {
            setEnvironmentId(event.target.value);
            setRefusal(null);
            setNotice(null);
          }}
        >
          {environments.map((e) => (
            <option key={e.id} value={e.id}>
              {e.name}
            </option>
          ))}
        </select>
      </div>
      {refusal !== null ? <Alert>{refusal}</Alert> : null}
      {notice !== null ? <Alert tone="done">{notice}</Alert> : null}

      <h3>Certificate authorities</h3>
      {cas.isError ? <Alert>The SSH CAs could not be listed. Listing them needs read on this environment.</Alert> : null}
      <p className="machine__actions">
        <Button variant="primary" type="button" disabled={!canAct} onClick={() => setDialog({ kind: 'create-ca' })}>
          Create CA
        </Button>
      </p>
      <table className="values__table machine__table">
        <caption className="visually-hidden">The environment&apos;s SSH CAs and their signing keys.</caption>
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">Keys</th>
            <th scope="col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {caItems.map((ca) => (
            <tr key={ca.id}>
              <td>
                <code>{ca.name}</code>
              </td>
              <td>
                <ul className="machine__keys">
                  {ca.keys.map((key) => (
                    <li key={key.id}>
                      <Badge tone={key.state === 'active' ? 'ok' : key.trusted ? 'changed' : 'neutral'}>
                        {key.state}
                      </Badge>{' '}
                      {key.algorithm} <code>{key.fingerprint}</code>
                      {key.state === 'retiring' && key.retire_after !== undefined && key.retire_after !== null
                        ? ` trusted until ${isoDay(key.retire_after)}`
                        : null}
                      {key.state === 'retiring' ? (
                        <>
                          {' '}
                          <Button
                            type="button"
                            disabled={!canAct || retire.isPending}
                            onClick={() =>
                              retire.mutate(
                                { ca: ca.id, key: key.id },
                                {
                                  onSuccess: () => done('Retired. Hosts drop the key at their next trust-bundle refresh.'),
                                  onError: (error) => setRefusal(sshRefusalText('retire', error)),
                                },
                              )
                            }
                          >
                            Retire now
                          </Button>
                        </>
                      ) : null}
                    </li>
                  ))}
                </ul>
              </td>
              <td>
                <Button type="button" onClick={() => setDialog({ kind: 'trust', ca })}>
                  Trust bundle
                </Button>{' '}
                {workspace === null ? (
                  <>
                    <a className="btn" href={sshKRLPath(env, ca.id)} download={`${ca.name}.krl`}>
                      Download KRL
                    </a>{' '}
                  </>
                ) : null}
                <Button type="button" disabled={!canAct} onClick={() => setDialog({ kind: 'rotate', ca })}>
                  Rotate
                </Button>{' '}
                <Button variant="danger" type="button" disabled={!canAct} onClick={() => setDialog({ kind: 'delete-ca', ca })}>
                  Delete
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {cas.isSuccess && caItems.length === 0 ? (
        <p role="status">No SSH CA in this environment yet. Create one to issue certificates.</p>
      ) : null}

      <h3>Profiles</h3>
      {profiles.isError ? <Alert>The SSH profiles could not be listed.</Alert> : null}
      <p className="machine__actions">
        <Button
          variant="primary"
          type="button"
          disabled={!canAct || caItems.length === 0}
          onClick={() => setDialog({ kind: 'profile', profile: null })}
        >
          Create profile
        </Button>
      </p>
      <table className="values__table machine__table">
        <caption className="visually-hidden">The signing profiles of this environment.</caption>
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">CA</th>
            <th scope="col">Principals</th>
            <th scope="col" className="col-secondary">Max TTL</th>
            <th scope="col" className="col-secondary">Requesters</th>
            <th scope="col">State</th>
            <th scope="col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {profileItems.map((profile) => (
            <tr key={profile.id}>
              <td>
                <code>{profile.name}</code>
              </td>
              <td>{caName(profile.ca_id)}</td>
              <td>
                <code>{profile.principals.join(', ')}</code>
              </td>
              <td className="col-secondary">{`${String(profile.max_ttl_seconds)}s`}</td>
              <td className="col-secondary">{String(profile.requesters.length)}</td>
              <td>
                <Badge tone={profile.enabled ? 'ok' : 'neutral'}>{profile.enabled ? 'enabled' : 'disabled'}</Badge>
              </td>
              <td>
                <Button type="button" disabled={!canAct} onClick={() => setDialog({ kind: 'profile', profile })}>
                  Edit
                </Button>{' '}
                <Button
                  variant="danger"
                  type="button"
                  disabled={!canAct}
                  onClick={() => setDialog({ kind: 'delete-profile', profile })}
                >
                  Delete
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {profiles.isSuccess && profileItems.length === 0 ? (
        <p role="status">No SSH profile in this environment yet.</p>
      ) : null}

      <h3>Certificates</h3>
      {certificates.isError ? <Alert>The SSH certificates could not be listed.</Alert> : null}
      <p className="machine__actions">
        <Button
          variant="primary"
          type="button"
          disabled={!canAct || profileItems.length === 0}
          onClick={() => setDialog({ kind: 'issue' })}
        >
          Issue certificate
        </Button>
      </p>
      <table className="values__table machine__table">
        <caption className="visually-hidden">
          The most recent certificates. The record carries public data only; no private key is ever listed.
        </caption>
        <thead>
          <tr>
            <th scope="col">Serial</th>
            <th scope="col">Principals</th>
            <th scope="col" className="col-secondary">Requester</th>
            <th scope="col">Valid before</th>
            <th scope="col">Status</th>
            <th scope="col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {certificateItems.map((cert) => (
            <tr key={cert.id}>
              <td>
                <code>{cert.serial}</code>
              </td>
              <td>
                <code>{cert.principals.join(', ')}</code>
              </td>
              <td className="col-secondary">
                <code>{cert.requester_principal_id}</code>
              </td>
              <td>{isoDay(cert.valid_before)}</td>
              <td>
                <Badge tone={statusTone(cert.status)}>{cert.status}</Badge>
                {cert.in_krl ? ' in KRL' : null}
              </td>
              <td>
                {cert.status === 'active' || cert.status === 'untrusted' ? (
                  <Button
                    variant="danger"
                    type="button"
                    disabled={!canAct || revoke.isPending}
                    onClick={() =>
                      revoke.mutate(cert.id, {
                        onSuccess: () => done('Revoked. The serial is in the KRL; hosts refuse it at their next KRL refresh.'),
                        onError: (error) => setRefusal(sshRefusalText('revoke', error)),
                      })
                    }
                  >
                    Revoke
                  </Button>
                ) : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {certificates.isSuccess && certificateItems.length === 0 ? (
        <p role="status">No SSH certificate issued in this environment yet.</p>
      ) : null}

      {dialog?.kind === 'create-ca' ? (
        <CADialog env={env} onClose={() => setDialog(null)} onDone={done} />
      ) : null}
      {dialog?.kind === 'rotate' ? (
        <CADialog env={env} rotating={dialog.ca} onClose={() => setDialog(null)} onDone={done} />
      ) : null}
      {dialog?.kind === 'trust' ? (
        <TrustDialog env={env} ca={dialog.ca} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.kind === 'delete-ca' ? (
        <DeleteCADialog env={env} ca={dialog.ca} onClose={() => setDialog(null)} onDone={done} />
      ) : null}
      {dialog?.kind === 'profile' ? (
        <ProfileDialog
          env={env}
          cas={caItems}
          profile={dialog.profile}
          requesterOptions={requesterOptions}
          onClose={() => setDialog(null)}
          onDone={done}
        />
      ) : null}
      {dialog?.kind === 'delete-profile' ? (
        <DeleteProfileDialog env={env} profile={dialog.profile} onClose={() => setDialog(null)} onDone={done} />
      ) : null}
      {dialog?.kind === 'issue' && sessionId !== null ? (
        <IssueDialog
          env={env}
          environmentName={current.name}
          sessionId={sessionId}
          profiles={profileItems.filter((p) => p.enabled)}
          lifecycle={issue}
          onClose={() => setDialog(null)}
        />
      ) : null}
    </>
  );
}

function CADialog({
  env,
  rotating,
  onClose,
  onDone,
}: {
  env: EnvironmentRef;
  rotating?: SSHCA;
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const refresh = useRefreshSSH(env);
  const transport = useTransport();
  const [name, setName] = useState('');
  const [algorithm, setAlgorithm] = useState<KeyAlgorithm>('ed25519');
  const [privateKey, setPrivateKey] = useState('');
  const [overlap, setOverlap] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const importing = privateKey.trim() !== '';

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      if (rotating === undefined) {
        await createSSHCA(env, { name, ...(importing ? { privateKey } : { algorithm }) }, transport);
        onDone(`Created CA ${name}. Distribute its trust bundle to hosts before issuing certificates.`);
      } else {
        const overlapSeconds = overlap.trim() === '' ? null : Number(overlap);
        if (overlapSeconds !== null && (!Number.isInteger(overlapSeconds) || overlapSeconds < 0)) {
          setError('Enter the overlap as a whole number of seconds, or leave it empty for the default.');
          return;
        }
        await rotateSSHCA(env, { ca: rotating.id, overlapSeconds, ...(importing ? { privateKey } : { algorithm }) }, transport);
        onDone('Rotated. New certificates are signed by the new key; the old key stays trusted, never signing, until its overlap ends.');
      }
    } catch (err) {
      setError(sshRefusalText(rotating === undefined ? 'create-ca' : 'rotate', err));
    } finally {
      // The pasted key is dropped whatever happened; it is never retried.
      setPrivateKey('');
      setBusy(false);
      refresh();
    }
  };

  return (
    <Dialog
      title={rotating === undefined ? 'Create SSH CA' : `Rotate ${rotating.name}`}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onClose();
      }}
      actions={
        <>
          <Button type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="button" disabled={busy || (rotating === undefined && name === '')} onClick={() => void submit()}>
            {rotating === undefined ? 'Create' : 'Rotate'}
          </Button>
        </>
      }
    >
      <fieldset className="machine__lock" disabled={busy}>
        {rotating === undefined ? (
          <div className="field">
            <label htmlFor="ssh-ca-name">Name</label>
            <input id="ssh-ca-name" className="mono" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
        ) : (
          <div className="field">
            <label htmlFor="ssh-ca-overlap">Overlap (seconds, empty until the old key's last live certificate expires)</label>
            <input id="ssh-ca-overlap" className="mono" inputMode="numeric" value={overlap} onChange={(e) => setOverlap(e.target.value)} />
          </div>
        )}
        <div className="field">
          <label htmlFor="ssh-ca-algorithm">Key algorithm (generated key)</label>
          <select
            id="ssh-ca-algorithm"
            value={algorithm}
            disabled={importing}
            onChange={(e) => setAlgorithm(e.target.value as KeyAlgorithm)}
          >
            {ALGORITHMS.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="ssh-ca-import">Import a private key instead (optional, unencrypted PEM)</label>
          <textarea
            id="ssh-ca-import"
            className="mono"
            rows={4}
            autoComplete="off"
            spellCheck={false}
            value={privateKey}
            onChange={(e) => setPrivateKey(e.target.value)}
          />
        </div>
      </fieldset>
      <p className="ceremony__scope">
        The private key is sealed on the server and never shown or returned again, by any route.
      </p>
      {error !== null ? <Alert>{error}</Alert> : null}
    </Dialog>
  );
}

function TrustDialog({ env, ca, onClose }: { env: EnvironmentRef; ca: SSHCA; onClose: () => void }) {
  const [bundle, setBundle] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);
  const transport = useTransport();
  useEffect(() => {
    let live = true;
    sshTrustedKeys(env, ca.id, transport).then(
      (text) => {
        if (live) setBundle(text);
      },
      () => {
        if (live) setError('The trust bundle could not be read.');
      },
    );
    return () => {
      live = false;
    };
  }, [env.org, env.project, env.environment, ca.id]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <Dialog
      title={`Trust bundle: ${ca.name}`}
      size="wide"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      actions={
        <Button variant="primary" type="button" onClick={onClose}>
          Done
        </Button>
      }
    >
      <p className="ceremony__scope">
        Install as <code>TrustedUserCAKeys</code> on every host. It lists the active key and any retiring key
        still inside its overlap. Refresh it on the same timer as the KRL.
      </p>
      {error !== null ? <Alert>{error}</Alert> : null}
      {bundle !== null ? (
        <>
          <pre className="mono machine__token">{bundle}</pre>
          <Button
            type="button"
            onClick={async () => {
              const result = await writeClipboard(bundle);
              setCopied(result === 'ok' ? 'Copied.' : 'This browser refused clipboard access, so nothing was copied.');
            }}
          >
            Copy
          </Button>
          {copied !== null ? <p role="status">{copied}</p> : null}
        </>
      ) : null}
    </Dialog>
  );
}

function DeleteCADialog({
  env,
  ca,
  onClose,
  onDone,
}: {
  env: EnvironmentRef;
  ca: SSHCA;
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const remove = useDeleteSSHCA(env);
  const [error, setError] = useState<string | null>(null);
  return (
    <Dialog
      title={`Delete ${ca.name}`}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      actions={
        <>
          <Button type="button" onClick={onClose} disabled={remove.isPending}>
            Cancel
          </Button>
          <Button
            variant="danger"
            type="button"
            disabled={remove.isPending}
            onClick={() =>
              remove.mutate(ca.id, {
                onSuccess: () =>
                  onDone(`Deleted ${ca.name}. Remove its keys from host trust: certificates it signed stay valid until they expire.`),
                onError: (err) => setError(sshRefusalText('delete-ca', err)),
              })
            }
          >
            Delete CA
          </Button>
        </>
      }
    >
      <Alert tone="warn">
        Deleting destroys the signing key. It is record deletion, not revocation: hosts that still trust this CA
        keep accepting its certificates until they expire.
      </Alert>
      {error !== null ? <Alert>{error}</Alert> : null}
    </Dialog>
  );
}

function ProfileDialog({
  env,
  cas,
  profile,
  requesterOptions,
  onClose,
  onDone,
}: {
  env: EnvironmentRef;
  cas: readonly SSHCA[];
  profile: SSHProfile | null;
  requesterOptions: readonly RequesterOption[];
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const save = useSaveSSHProfile(env);
  const [caId, setCaId] = useState(profile?.ca_id ?? cas[0]?.id ?? '');
  const [name, setName] = useState(profile?.name ?? '');
  const [principals, setPrincipals] = useState((profile?.principals ?? []).join('\n'));
  const [sources, setSources] = useState((profile?.source_addresses ?? []).join('\n'));
  const [forceCommand, setForceCommand] = useState(profile?.force_command ?? '');
  const [extensions, setExtensions] = useState<readonly SSHProfile['extensions'][number][]>(
    profile?.extensions ?? ['permit-pty'],
  );
  const [algorithms, setAlgorithms] = useState<readonly KeyAlgorithm[]>(profile?.key_algorithms ?? ['ed25519']);
  const [defaultTtl, setDefaultTtl] = useState(String(profile?.default_ttl_seconds ?? 3600));
  const [maxTtl, setMaxTtl] = useState(String(profile?.max_ttl_seconds ?? 28800));
  const [enabled, setEnabled] = useState(profile?.enabled ?? true);
  const [requesters, setRequesters] = useState((profile?.requesters ?? []).join('\n'));
  const [error, setError] = useState<string | null>(null);

  const toggle = <T,>(list: readonly T[], item: T, on: boolean): readonly T[] =>
    on ? [...list.filter((x) => x !== item), item] : list.filter((x) => x !== item);

  const submit = () => {
    let input: SSHProfileInput;
    try {
      input = {
        caId,
        name,
        principals: lines(principals),
        forceCommand,
        sourceAddresses: lines(sources),
        extensions,
        keyAlgorithms: algorithms,
        defaultTtlSeconds: seconds('the default lifetime', defaultTtl),
        maxTtlSeconds: seconds('the maximum lifetime', maxTtl),
        enabled,
        requesters: lines(requesters),
      };
    } catch (err) {
      setError(err instanceof Error ? err.message : 'The profile is not valid.');
      return;
    }
    setError(null);
    const kept = input.requesters;
    const removedRequester = (profile?.requesters ?? []).some((id) => !kept.includes(id));
    save.mutate(
      { id: profile?.id ?? null, profile: input },
      {
        onSuccess: () =>
          onDone(
            profile === null
              ? `Created profile ${name}.`
              : removedRequester
                ? 'Saved. Live certificates issued through this profile to the removed requesters were revoked.'
                : 'Saved.',
          ),
        onError: (err) => setError(sshRefusalText('save-profile', err)),
      },
    );
  };

  return (
    <Dialog
      title={profile === null ? 'Create SSH profile' : `Edit ${profile.name}`}
      size="wide"
      onCancel={(event) => {
        event.preventDefault();
        if (!save.isPending) onClose();
      }}
      actions={
        <>
          <Button type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="button" disabled={save.isPending} onClick={submit}>
            Save
          </Button>
        </>
      }
    >
      <fieldset className="machine__lock" disabled={save.isPending}>
        <div className="field">
          <label htmlFor="ssh-profile-name">Name</label>
          <input id="ssh-profile-name" className="mono" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="ssh-profile-ca">CA</label>
          <select id="ssh-profile-ca" value={caId} disabled={profile !== null} onChange={(e) => setCaId(e.target.value)}>
            {cas.map((ca) => (
              <option key={ca.id} value={ca.id}>
                {ca.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="ssh-profile-principals">Allowed principals (remote user names, one per line)</label>
          <textarea id="ssh-profile-principals" className="mono" rows={3} value={principals} onChange={(e) => setPrincipals(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="ssh-profile-sources">Source addresses (CIDRs, one per line, optional)</label>
          <textarea id="ssh-profile-sources" className="mono" rows={2} value={sources} onChange={(e) => setSources(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="ssh-profile-force">Force command (optional)</label>
          <input id="ssh-profile-force" className="mono" value={forceCommand} onChange={(e) => setForceCommand(e.target.value)} />
        </div>
        <fieldset>
          <legend>Allowed extensions</legend>
          {EXTENSIONS.map((ext) => (
            <Checkbox
              key={ext}
              label={ext}
              checked={extensions.includes(ext)}
              onChange={(e) => setExtensions(toggle(extensions, ext, e.target.checked))}
            />
          ))}
        </fieldset>
        <fieldset>
          <legend>Allowed user key algorithms</legend>
          {ALGORITHMS.map((alg) => (
            <Checkbox
              key={alg}
              label={alg}
              checked={algorithms.includes(alg)}
              onChange={(e) => setAlgorithms(toggle(algorithms, alg, e.target.checked))}
            />
          ))}
        </fieldset>
        <div className="field">
          <label htmlFor="ssh-profile-default-ttl">Default lifetime (seconds)</label>
          <input id="ssh-profile-default-ttl" className="mono" inputMode="numeric" value={defaultTtl} onChange={(e) => setDefaultTtl(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="ssh-profile-max-ttl">Maximum lifetime (seconds)</label>
          <input id="ssh-profile-max-ttl" className="mono" inputMode="numeric" value={maxTtl} onChange={(e) => setMaxTtl(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="ssh-profile-requesters">Requesters (principal ids, one per line)</label>
          <textarea id="ssh-profile-requesters" className="mono" rows={3} value={requesters} onChange={(e) => setRequesters(e.target.value)} />
          {requesterOptions.length > 0 ? (
            <p className="machine__actions">
              {requesterOptions.map((option) => (
                <Button
                  key={option.id}
                  type="button"
                  onClick={() => setRequesters((text) => (lines(text).includes(option.id) ? text : `${text.trim()}\n${option.id}`.trim()))}
                >
                  {`Add ${option.label}`}
                </Button>
              ))}
            </p>
          ) : null}
        </div>
        <Checkbox label="Enabled (issuance allowed)" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
      </fieldset>
      <p className="ceremony__scope">
        Disabling stops issuance at once and revokes nothing. Removing a requester revokes its live certificates
        issued through this profile.
      </p>
      {error !== null ? <Alert>{error}</Alert> : null}
    </Dialog>
  );
}

function DeleteProfileDialog({
  env,
  profile,
  onClose,
  onDone,
}: {
  env: EnvironmentRef;
  profile: SSHProfile;
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const remove = useDeleteSSHProfile(env);
  const [revokeIssued, setRevokeIssued] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <Dialog
      title={`Delete ${profile.name}`}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      actions={
        <>
          <Button type="button" onClick={onClose} disabled={remove.isPending}>
            Cancel
          </Button>
          <Button
            variant="danger"
            type="button"
            disabled={remove.isPending}
            onClick={() =>
              remove.mutate(
                { id: profile.id, revokeIssued },
                {
                  onSuccess: (result) =>
                    onDone(
                      revokeIssued
                        ? `Deleted; ${String(result.revoked_certificate_count)} certificates revoked.`
                        : 'Deleted. Certificates it issued stay valid until they expire.',
                    ),
                  onError: (err) => setError(sshRefusalText('delete-profile', err)),
                },
              )
            }
          >
            Delete profile
          </Button>
        </>
      }
    >
      <Checkbox
        label="Also revoke every live certificate issued through this profile"
        checked={revokeIssued}
        onChange={(e) => setRevokeIssued(e.target.checked)}
      />
      <p className="ceremony__scope">Deleting without revoking is record deletion only.</p>
      {error !== null ? <Alert>{error}</Alert> : null}
    </Dialog>
  );
}

function IssueDialog({
  env,
  environmentName,
  sessionId,
  profiles,
  lifecycle,
  onClose,
}: {
  env: EnvironmentRef;
  environmentName: string;
  sessionId: string;
  profiles: readonly SSHProfile[];
  lifecycle: ReturnType<typeof useMintLifecycle<IssueRequest, SSHIssued>>;
  onClose: () => void;
}) {
  const refresh = useRefreshSSH(env);
  const transport = useTransport();
  const confirmation = useRef<HTMLInputElement>(null);
  const [profileId, setProfileId] = useState(profiles[0]?.id ?? '');
  const [publicKey, setPublicKey] = useState('');
  const [algorithm, setAlgorithm] = useState<KeyAlgorithm>('ed25519');
  const [principals, setPrincipals] = useState('');
  const [ttl, setTtl] = useState('');
  const [formError, setFormError] = useState<string | null>(null);
  const state = lifecycle.active;
  const move = lifecycle.moveMint;
  const disclosed = state.kind === 'disclosed' ? state : null;
  const busy = state.kind === 'submitting';
  const failure = state.kind === 'failed' ? state.error : null;
  const holdsSecret = disclosed !== null && disclosed.result.private_key !== undefined && disclosed.result.private_key !== null;

  useEffect(() => {
    if (disclosed !== null) confirmation.current?.focus();
  }, [disclosed]);

  const run = async () => {
    const profile = profiles.find((p) => p.id === profileId);
    if (profile === undefined) {
      setFormError('Choose a profile.');
      return;
    }
    let ttlSeconds: number | null = null;
    if (ttl.trim() !== '') {
      try {
        ttlSeconds = seconds('the lifetime', ttl);
      } catch (err) {
        setFormError(err instanceof Error ? err.message : 'Invalid lifetime.');
        return;
      }
    }
    setFormError(null);
    const reviewed = move({
      type: 'review',
      request: {
        id: lifecycle.nextRequestId(),
        sessionId,
        org: env.org,
        project: env.project,
        environmentId: env.environment,
        profileId: profile.id,
        profileName: profile.name,
      },
    });
    if (!reviewed.accepted || reviewed.state.kind !== 'reviewing') return;
    const started = move({ type: 'submit' });
    if (!started.accepted || started.state.kind !== 'submitting') return;
    const active = started.state.request;
    let issued = false;
    try {
      await runPasskeyCeremony({ operation: 'mint', environmentId: active.environmentId, keyIds: [] });
      if (!lifecycle.isSubmitting(active.id)) return;
      issued = true;
      const result = await issueSSHCertificate(env, {
        profileId: active.profileId,
        publicKey,
        keyAlgorithm: algorithm,
        principals: lines(principals),
        ttlSeconds,
      }, transport);
      move({ type: 'succeeded', requestId: active.id, result });
      // A supplied-key certificate carries no secret: nothing to confirm.
      if (result.private_key === undefined || result.private_key === null) {
        move({ type: 'confirm-stored', stored: true });
      }
    } catch (error) {
      move({ type: 'failed', requestId: active.id, error: sshIssueFailureText(error, issued) });
    } finally {
      refresh();
    }
  };

  const dismiss = () => {
    if (state.kind === 'idle') {
      onClose();
      return;
    }
    const result = move({ type: 'dismiss' });
    if (result.state.kind === 'idle') onClose();
  };

  useNavigationGuard(busy || (holdsSecret && disclosed !== null && !disclosed.stored), dismiss);

  return (
    <Dialog
      title={disclosed === null ? `Issue SSH certificate in ${environmentName}` : 'Certificate issued'}
      size="wide"
      onCancel={(event) => {
        event.preventDefault();
        dismiss();
      }}
      actions={
        disclosed === null ? (
          <>
            <Button type="button" onClick={dismiss} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="button" disabled={busy} onClick={() => void run()}>
              {busy ? 'Issuing…' : 'Use a passkey and issue'}
            </Button>
          </>
        ) : (
          <Button variant="primary" type="button" onClick={dismiss}>
            Done
          </Button>
        )
      }
    >
      {disclosed === null ? (
        <>
          <fieldset className="machine__lock" disabled={busy}>
            <div className="field">
              <label htmlFor="ssh-issue-profile">Profile</label>
              <select id="ssh-issue-profile" value={profileId} onChange={(e) => setProfileId(e.target.value)}>
                {profiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {`${p.name} (${p.principals.join(', ')})`}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor="ssh-issue-principals">Principals (one per line; empty when the profile allows one)</label>
              <textarea id="ssh-issue-principals" className="mono" rows={2} value={principals} onChange={(e) => setPrincipals(e.target.value)} />
            </div>
            <div className="field">
              <label htmlFor="ssh-issue-ttl">Lifetime (seconds; empty for the profile default)</label>
              <input id="ssh-issue-ttl" className="mono" inputMode="numeric" value={ttl} onChange={(e) => setTtl(e.target.value)} />
            </div>
            <div className="field">
              <label htmlFor="ssh-issue-public-key">Your public key (authorized_keys line; empty to generate a key pair)</label>
              <textarea
                id="ssh-issue-public-key"
                className="mono"
                rows={2}
                spellCheck={false}
                value={publicKey}
                onChange={(e) => setPublicKey(e.target.value)}
              />
            </div>
            {publicKey.trim() === '' ? (
              <div className="field">
                <label htmlFor="ssh-issue-algorithm">Generated key algorithm</label>
                <select id="ssh-issue-algorithm" value={algorithm} onChange={(e) => setAlgorithm(e.target.value as KeyAlgorithm)}>
                  {ALGORITHMS.map((a) => (
                    <option key={a} value={a}>
                      {a}
                    </option>
                  ))}
                </select>
              </div>
            ) : null}
          </fieldset>
          <p className="ceremony__scope">
            You must be on the profile&apos;s requester list. A generated private key is shown exactly once and
            never stored.
          </p>
          {formError !== null ? <Alert>{formError}</Alert> : null}
          {failure !== null ? <Alert>{failure}</Alert> : null}
        </>
      ) : (
        <>
          <div className="field">
            <label htmlFor="ssh-issued-cert">Certificate (save as your key file name plus -cert.pub)</label>
            <pre className="mono machine__token" id="ssh-issued-cert">
              {disclosed.result.certificate_text}
            </pre>
          </div>
          {holdsSecret ? (
            <>
              <div className="field">
                <label htmlFor="ssh-issued-key">Private key</label>
                <pre className="mono machine__token" id="ssh-issued-key">
                  {disclosed.result.private_key}
                </pre>
              </div>
              <CeremonyNotice>
                This private key is never retrievable again. Save it with mode 0600 now; if it is lost, revoke this
                certificate and issue a new one.
              </CeremonyNotice>
              <Button
                type="button"
                onClick={async () => {
                  const result = await writeClipboard(disclosed.result.private_key ?? '');
                  move({
                    type: 'copy-status',
                    requestId: disclosed.request.id,
                    message: result === 'ok' ? 'Copied.' : 'This browser refused clipboard access, so nothing was copied.',
                  });
                }}
              >
                Copy private key
              </Button>
              {disclosed.copyStatus === null ? null : <p role="status">{disclosed.copyStatus}</p>}
              <Checkbox
                label="I have stored this private key."
                ref={confirmation}
                checked={disclosed.stored}
                onChange={(event) => move({ type: 'confirm-stored', stored: event.target.checked })}
              />
              {disclosed.heldBack ? (
                <Alert>Confirm you have stored it: there is no second look at this private key.</Alert>
              ) : null}
            </>
          ) : null}
        </>
      )}
    </Dialog>
  );
}
