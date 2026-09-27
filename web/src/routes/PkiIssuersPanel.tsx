import { useId, useState } from 'react';

import { ApiError } from '../api/client.ts';
import {
  fetchIssuerCrl,
  pkiAdminRefusalText,
  useCreatePkiIssuer,
  useInstallPkiIssuerCertificate,
  usePkiIssuers,
  usePublishPkiIssuerCrl,
  useReleasePkiIssuerHold,
  useRetirePkiIssuer,
  useRevokePkiIssuer,
  useRotatePkiIssuer,
  type IssuerDraft,
  type PkiIssuer,
} from '../api/pki.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';
import { Textarea } from '../ui/Textarea.tsx';
import { Panel, TypedNameConfirm } from './Sections.tsx';
import { selectOption } from './selectOption.ts';

const secondFactor = (error: unknown) => error instanceof ApiError && error.status === 403;

const ALGORITHMS = ['ecdsa-p256', 'ecdsa-p384', 'rsa-3072', 'rsa-4096'] as const;
const OFFLINE = '';

type Feedback = { failure: string | null; done: string | null };

/**
 * PkiIssuersPanel is the private-PKI certificate-authority lifecycle (#154), a
 * panel on `instance-admin`. Every row is one key version of a named issuer.
 * Only certificates, CSRs and public-key fingerprints are ever shown: a CA
 * private key never leaves the server, and there is no route that returns one.
 * Importing an existing CA key is a CLI act (`hikyo pki issuer import`), so a
 * key is never pasted into this page.
 */
export function PkiIssuersPanel() {
  const issuers = usePkiIssuers();
  const [feedback, setFeedback] = useState<Feedback>({ failure: null, done: null });
  const [crl, setCrl] = useState<{ label: string; pem: string } | null>(null);
  const report = (error: unknown, action: string) => setFeedback({ failure: pkiAdminRefusalText(error, action), done: null });
  const ok = (message: string) => setFeedback({ failure: null, done: message });
  const clear = () => setFeedback({ failure: null, done: null });
  const rows = issuers.isSuccess ? issuers.data.issuers : [];
  const names = [...new Set(rows.map((issuer) => issuer.name))];

  return (
    <Panel id="instance-pki-issuers" title="Certificate authorities">
      <p>
        Private PKI issuers. Each row is one key version: rotation adds a version and keeps the
        previous one valid while its certificates run out. A root created here is for evaluation;
        in production, create an intermediate without a parent, sign its CSR with an offline root,
        and install the certificate.
      </p>
      {issuers.isPending ? <p role="status">Loading certificate authorities…</p> : null}
      {secondFactor(issuers.error) ? (
        <Alert>
          Reading certificate authorities needs instance-config and a second factor. If you hold
          it, present your authenticator code or passkey in the banner above.
        </Alert>
      ) : null}
      {issuers.isError && !secondFactor(issuers.error) ? (
        <Alert>{pkiAdminRefusalText(issuers.error, 'list the certificate authorities')}</Alert>
      ) : null}
      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Alert tone="done">{feedback.done}</Alert> : null}

      {issuers.isSuccess && rows.length === 0 ? (
        <p role="status">No certificate authorities yet. Create one below.</p>
      ) : null}
      {rows.map((issuer) => (
        <IssuerRow
          key={issuer.id}
          issuer={issuer}
          newest={rows.filter((row) => row.name === issuer.name).every((row) => row.version <= issuer.version)}
          onDone={ok}
          onFailure={report}
          onBusy={clear}
          onCrl={(label, pem) => setCrl({ label, pem })}
        />
      ))}

      {crl !== null ? (
        <Textarea
          label={`CRL of ${crl.label}`}
          mono
          rows={6}
          readOnly
          value={crl.pem}
          hint="Public material. Copy it to your CRL distribution point; Hikyo embeds that URL in the certificates it issues and never fetches it."
        />
      ) : null}

      <CreateIssuerForm names={names} onDone={ok} onFailure={report} onBusy={clear} />
      <code className="instance-cli">$ hikyo pki issuer import &lt;issuer&gt; --key-file &lt;path&gt; --cert-file &lt;path&gt;</code>
    </Panel>
  );
}

function stateTone(state: PkiIssuer['state']): 'neutral' | 'danger' {
  return state === 'revoked' || state === 'retired' ? 'danger' : 'neutral';
}

function IssuerRow({
  issuer,
  newest,
  onDone,
  onFailure,
  onBusy,
  onCrl,
}: {
  issuer: PkiIssuer;
  newest: boolean;
  onDone: (message: string) => void;
  onFailure: (error: unknown, action: string) => void;
  onBusy: () => void;
  onCrl: (label: string, pem: string) => void;
}) {
  const [mode, setMode] = useState<'idle' | 'retire' | 'revoke' | 'install'>('idle');
  const [certificatePem, setCertificatePem] = useState('');
  const [chainPem, setChainPem] = useState('');
  const rotate = useRotatePkiIssuer();
  const retire = useRetirePkiIssuer();
  const revoke = useRevokePkiIssuer();
  const releaseHold = useReleasePkiIssuerHold();
  const install = useInstallPkiIssuerCertificate();
  const publish = usePublishPkiIssuerCrl();
  const version = Number(issuer.version);
  const label = `${issuer.name} v${String(version)}`;
  const signing = issuer.state === 'active' || issuer.state === 'retiring';
  const toggle = (next: typeof mode) => {
    onBusy();
    setMode((current) => (current === next ? 'idle' : next));
  };

  return (
    <div className="settings-row settings-row--stacked" data-pki-issuer={label}>
      <div className="settings-row__copy">
        <span className="settings-row__title mono">{label}</span>
        <span className="settings-row__detail">
          {issuer.kind} · {issuer.origin} · {issuer.key_algorithm} · {issuer.subject_cn}
        </span>
        <span className="settings-row__detail mono">{issuer.key_fingerprint}</span>
        {issuer.not_after == null ? null : (
          <span className="settings-row__detail mono">valid until {new Date(issuer.not_after).toLocaleString()}</span>
        )}
        {issuer.restore_hold ? (
          <span className="settings-row__detail">
            Held after a restore: it issues nothing until you re-apply revocations made since the
            backup, then release the hold.
          </span>
        ) : null}
      </div>
      <span className="settings-row__spacer" />
      <Badge tone={stateTone(issuer.state)}>{issuer.state}</Badge>
      {issuer.restore_hold ? <Badge tone="danger">held</Badge> : null}
      <div className="panel__actions">
        {newest && issuer.state !== 'pending' && issuer.state !== 'revoked' ? (
          <Button
            type="button"
            disabled={rotate.isPending}
            onClick={() => {
              onBusy();
              rotate.mutate(issuer.name, {
                onSuccess: (next) =>
                  onDone(
                    next.state === 'pending'
                      ? `Created ${next.name} v${String(Number(next.version))} pending: sign its CSR with the offline root and install it.`
                      : `Rotated ${next.name}: v${String(Number(next.version))} is active and the previous version is retiring.`,
                  ),
                onError: (error) => onFailure(error, `rotate ${issuer.name}`),
              });
            }}
          >
            Rotate
          </Button>
        ) : null}
        {issuer.state === 'pending' ? (
          <Button type="button" variant="primary" onClick={() => toggle('install')}>
            Install certificate
          </Button>
        ) : null}
        {signing ? (
          <>
            <Button
              type="button"
              disabled={publish.isPending}
              onClick={() => {
                onBusy();
                publish.mutate(
                  { issuer: issuer.name, version },
                  {
                    onSuccess: () => onDone(`Published a fresh CRL for ${label}.`),
                    onError: (error) => onFailure(error, `publish the CRL of ${label}`),
                  },
                );
              }}
            >
              Publish CRL
            </Button>
            <Button
              type="button"
              onClick={() => {
                onBusy();
                fetchIssuerCrl(issuer.name, version).then(
                  (pem) => onCrl(label, pem),
                  (error: unknown) => onFailure(error, `read the CRL of ${label}`),
                );
              }}
            >
              View CRL
            </Button>
            <Button type="button" onClick={() => toggle('retire')}>
              Retire
            </Button>
          </>
        ) : null}
        {issuer.restore_hold ? (
          <Button
            type="button"
            disabled={releaseHold.isPending}
            onClick={() => {
              onBusy();
              releaseHold.mutate(issuer.name, {
                onSuccess: () => onDone(`Released the hold on ${issuer.name}: it issues again.`),
                onError: (error) => onFailure(error, `release the hold on ${issuer.name}`),
              });
            }}
          >
            Release hold
          </Button>
        ) : null}
        {issuer.state !== 'retired' && issuer.state !== 'revoked' ? (
          <Button type="button" variant="danger" onClick={() => toggle('revoke')}>
            Revoke (compromise)
          </Button>
        ) : null}
      </div>
      {issuer.csr_pem == null ? null : (
        <Textarea
          label={`CSR of ${label}`}
          mono
          rows={5}
          readOnly
          value={issuer.csr_pem}
          hint="Sign this with the offline root as a CA that may sign only leaves (pathlen 0, keyCertSign and cRLSign), then install the certificate."
        />
      )}
      {mode === 'install' ? (
        <fieldset className="machine__lock" disabled={install.isPending}>
          <Textarea
            label="Signed CA certificate (PEM)"
            mono
            rows={5}
            value={certificatePem}
            onChange={(event) => setCertificatePem(event.target.value)}
          />
          <Textarea
            label="Signing chain (PEM), ending with the offline root"
            mono
            rows={5}
            value={chainPem}
            onChange={(event) => setChainPem(event.target.value)}
          />
          <div className="panel__actions">
            <Button
              type="button"
              variant="primary"
              disabled={certificatePem.trim() === '' || chainPem.trim() === ''}
              onClick={() =>
                install.mutate(
                  { issuer: issuer.name, certificatePem, chainPem },
                  {
                    onSuccess: () => {
                      setMode('idle');
                      onDone(`Installed ${label}: it is active now.`);
                    },
                    onError: (error) => onFailure(error, `install the certificate of ${label}`),
                  },
                )
              }
            >
              Verify and install
            </Button>
          </div>
        </fieldset>
      ) : null}
      {mode === 'retire' ? (
        <TypedNameConfirm
          label="Retire this version"
          expect={issuer.name}
          action="Retire"
          busy={retire.isPending}
          hint={
            <>
              Retiring destroys the key of {label}. It is refused while the version still has live
              certificates. Type <span className="mono">{issuer.name}</span> to confirm.
            </>
          }
          onConfirm={() =>
            retire.mutate(
              { issuer: issuer.name, version },
              {
                onSuccess: () => {
                  setMode('idle');
                  onDone(`Retired ${label}; its key is destroyed.`);
                },
                onError: (error) => onFailure(error, `retire ${label}`),
              },
            )
          }
        />
      ) : null}
      {mode === 'revoke' ? (
        <TypedNameConfirm
          label="Revoke this version as compromised"
          expect={issuer.name}
          action="Revoke"
          busy={revoke.isPending}
          hint={
            <>
              Revoking destroys the key of {label} and revokes every live certificate it signed
              (reason cACompromise). This cannot be undone. Type{' '}
              <span className="mono">{issuer.name}</span> to confirm.
            </>
          }
          onConfirm={() =>
            revoke.mutate(
              { issuer: issuer.name, version },
              {
                onSuccess: () => {
                  setMode('idle');
                  onDone(`Revoked ${label}; its certificates are revoked and its key is destroyed.`);
                },
                onError: (error) => onFailure(error, `revoke ${label}`),
              },
            )
          }
        />
      ) : null}
    </div>
  );
}

function CreateIssuerForm({
  names,
  onDone,
  onFailure,
  onBusy,
}: {
  names: readonly string[];
  onDone: (message: string) => void;
  onFailure: (error: unknown, action: string) => void;
  onBusy: () => void;
}) {
  const create = useCreatePkiIssuer();
  const modeId = useId();
  const algorithmId = useId();
  const parentId = useId();
  const [draft, setDraft] = useState<IssuerDraft>({
    mode: 'intermediate',
    name: '',
    commonName: '',
    organization: '',
    keyAlgorithm: 'ecdsa-p256',
    ttlDays: 365,
    parent: OFFLINE,
    crlUrl: '',
  });
  const set = <K extends keyof IssuerDraft>(key: K, value: IssuerDraft[K]) => setDraft((current) => ({ ...current, [key]: value }));
  const ready = draft.name.trim() !== '' && draft.commonName.trim() !== '' && draft.ttlDays >= 1;

  return (
    <fieldset className="machine__lock" disabled={create.isPending}>
      <legend className="machine__subhead">Create a certificate authority</legend>
      <div className="field">
        <label htmlFor={modeId}>Kind</label>
        <select id={modeId} value={draft.mode} onChange={(event) => set('mode', selectOption(['root', 'intermediate'] as const, event.target.value))}>
          <option value="intermediate">Intermediate</option>
          <option value="root">Evaluation root</option>
        </select>
      </div>
      <Input label="Name" mono value={draft.name} placeholder="issuing" onChange={(event) => set('name', event.target.value)} hint="Lowercase letters, digits and hyphens. Profiles name issuers by this." />
      <Input label="Common name" value={draft.commonName} onChange={(event) => set('commonName', event.target.value)} />
      <Input label="Organization (optional)" value={draft.organization} onChange={(event) => set('organization', event.target.value)} />
      <div className="field">
        <label htmlFor={algorithmId}>Key algorithm</label>
        <select id={algorithmId} value={draft.keyAlgorithm} onChange={(event) => set('keyAlgorithm', selectOption(ALGORITHMS, event.target.value))}>
          {ALGORITHMS.map((algorithm) => (
            <option key={algorithm} value={algorithm}>
              {algorithm}
            </option>
          ))}
        </select>
      </div>
      <Input label="Lifetime (days)" inputMode="numeric" value={String(draft.ttlDays)} onChange={(event) => set('ttlDays', Number(event.target.value) || 0)} />
      {draft.mode === 'intermediate' ? (
        <div className="field">
          <label htmlFor={parentId}>Signed by</label>
          <select id={parentId} value={draft.parent} onChange={(event) => set('parent', event.target.value)}>
            <option value={OFFLINE}>An offline root (create a CSR)</option>
            {names.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </div>
      ) : null}
      <Input label="CRL distribution URL (optional)" mono value={draft.crlUrl} placeholder="https://pki.example.com/issuing.crl" onChange={(event) => set('crlUrl', event.target.value)} />
      <div className="panel__actions">
        <Button
          type="button"
          variant="primary"
          disabled={!ready}
          onClick={() => {
            onBusy();
            create.mutate(draft, {
              onSuccess: (issuer) => {
                setDraft((current) => ({ ...current, name: '', commonName: '' }));
                onDone(
                  issuer.state === 'pending'
                    ? `Created ${issuer.name} pending: sign its CSR with the offline root, then install the certificate.`
                    : `Created ${issuer.name}; it is active.`,
                );
              },
              onError: (error) => onFailure(error, 'create the certificate authority'),
            });
          }}
        >
          Create
        </Button>
      </div>
    </fieldset>
  );
}
