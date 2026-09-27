import { useId, useState } from 'react';

import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { useTransport } from '../api/transport.tsx';
import {
  certificateRefusalText,
  fetchCertificateCrl,
  issueFailureText,
  issueGeneratedCertificate,
  useCertificateProfiles,
  useIssueCsrCertificate,
  useRefreshCertificates,
  useRenewCertificate,
  useRevokeCertificate,
  type CertificateRow,
  type IssueDraft,
  type RevocationReason,
} from '../api/pki.ts';
import { runPasskeyCeremony } from '../api/values.ts';
import { useNavigationGuard } from '../app/useNavigationGuard.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Input } from '../ui/Input.tsx';
import { Radio } from '../ui/Radio.tsx';
import { Textarea } from '../ui/Textarea.tsx';
import { selectOption } from './selectOption.ts';

type ProjectRef = { readonly org: string; readonly project: string };
type EnvOption = { readonly id: string; readonly name: string };

const KEY_ALGORITHMS = ['ecdsa-p256', 'ecdsa-p384', 'ed25519', 'rsa-2048', 'rsa-3072', 'rsa-4096'] as const;
const REASONS: readonly RevocationReason[] = [
  'unspecified',
  'key-compromise',
  'superseded',
  'cessation-of-operation',
  'affiliation-changed',
  'privilege-withdrawn',
];

/** lines splits a one-per-line textarea into trimmed, non-empty entries. */
function lines(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '');
}

/** certificateCount spells the tab count: unknown until every listing settled. */
export function certificateCount(view: { rows: readonly CertificateRow[]; isPending: boolean; isError: boolean }): number | 'unknown' {
  return view.isPending || view.isError ? 'unknown' : view.rows.length;
}

/**
 * CertificatesTab is the private-PKI certificate lifecycle on `machine-access`
 * (#154): the project's certificates across every environment, issuance
 * through a bound profile, renewal and revocation. Metadata and public
 * certificates only: a private key is never stored, and a generated one is
 * shown exactly once, in the issue dialog, after a passkey reauthentication.
 */
export function CertificatesTab({
  project,
  environments,
  view,
}: {
  project: ProjectRef;
  environments: readonly EnvOption[];
  view: { rows: readonly CertificateRow[]; isPending: boolean; isError: boolean };
}) {
  const [issuing, setIssuing] = useState(false);
  const [feedback, setFeedback] = useState<{ failure: string | null; done: string | null }>({ failure: null, done: null });
  const [crl, setCrl] = useState<string | null>(null);
  const renew = useRenewCertificate(project);
  const transport = useTransport();
  const revoke = useRevokeCertificate(project);
  const [revoking, setRevoking] = useState<{ row: CertificateRow; reason: RevocationReason } | null>(null);
  const clear = () => setFeedback({ failure: null, done: null });

  return (
    <>
      <h2>Certificates</h2>
      <p className="machine__lede">
        Short-lived X.509 certificates issued through the instance&apos;s certificate profiles. Only
        metadata and public certificates are listed: a private key is never stored. Renewal keeps
        the key and re-checks the names against the current profile; revocation reaches the CRL on
        its next publication.
      </p>
      <p className="machine__actions">
        <Button
          variant="primary"
          type="button"
          disabled={environments.length === 0}
          onClick={() => {
            clear();
            setIssuing(true);
          }}
        >
          Issue certificate
        </Button>
      </p>
      {view.isError ? <Alert>At least one environment&apos;s certificates could not be listed; the table is incomplete.</Alert> : null}
      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Alert tone="done">{feedback.done}</Alert> : null}
      <table className="values__table machine__table">
        <caption className="visually-hidden">
          The project&apos;s certificates across every environment. No private key is listed.
        </caption>
        <thead>
          <tr>
            <th scope="col">Environment</th>
            <th scope="col">Names</th>
            <th scope="col">State</th>
            <th scope="col" className="col-secondary">
              Issuer
            </th>
            <th scope="col" className="col-secondary">
              Expires
            </th>
            <th scope="col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {view.rows.map((row) => {
            const c = row.certificate;
            const live = c.state === 'issued';
            return (
              <tr key={c.id} data-certificate={c.id}>
                <td>{row.environmentName}</td>
                <td className="mono">{[...c.dns_names, ...c.ip_addresses, ...c.uris].join(', ')}</td>
                <td>
                  <Badge tone={c.state === 'revoked' || c.state === 'unknown' ? 'danger' : 'neutral'}>{c.state}</Badge>
                </td>
                <td className="col-secondary mono">
                  {c.issuer_name} v{String(Number(c.issuer_version))}
                </td>
                <td className="col-secondary mono">{new Date(c.not_after).toLocaleString()}</td>
                <td>
                  {live ? (
                    <Button
                      type="button"
                      variant="quiet"
                      disabled={renew.isPending}
                      onClick={() => {
                        clear();
                        renew.mutate(
                          { environment: row.environmentId, certificate: c.id },
                          {
                            onSuccess: (next) => setFeedback({ failure: null, done: `Renewed: the successor ${next.serial} carries the same key.` }),
                            onError: (error) => setFeedback({ failure: certificateRefusalText(error, 'renew the certificate'), done: null }),
                          },
                        );
                      }}
                    >
                      Renew
                    </Button>
                  ) : null}
                  {c.state === 'issued' || c.state === 'renewed' || c.state === 'unknown' ? (
                    <Button type="button" variant="quiet" onClick={() => setRevoking({ row, reason: 'unspecified' })}>
                      Revoke
                    </Button>
                  ) : null}
                  <Button
                    type="button"
                    variant="quiet"
                    onClick={() => {
                      clear();
                      fetchCertificateCrl({ ...project, environment: row.environmentId }, c.id, transport).then(
                        (pem) => setCrl(pem),
                        (error: unknown) => setFeedback({ failure: certificateRefusalText(error, 'read the CRL'), done: null }),
                      );
                    }}
                  >
                    CRL
                  </Button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {view.rows.length === 0 && !view.isPending && !view.isError ? (
        <p role="status">No certificates in this project yet.</p>
      ) : null}
      {crl !== null ? <Textarea label="Issuer CRL" mono rows={6} readOnly value={crl} hint="Public material." /> : null}

      {revoking !== null ? (
        <Dialog
          title="Revoke certificate"
          onCancel={(event) => {
            event.preventDefault();
            setRevoking(null);
          }}
          actions={
            <>
              <Button type="button" onClick={() => setRevoking(null)} disabled={revoke.isPending}>
                Cancel
              </Button>
              <Button
                type="button"
                variant="danger"
                disabled={revoke.isPending}
                onClick={() =>
                  revoke.mutate(
                    { environment: revoking.row.environmentId, certificate: revoking.row.certificate.id, reason: revoking.reason },
                    {
                      onSuccess: (c) => {
                        setRevoking(null);
                        setFeedback({ failure: null, done: `Revoked ${c.serial}. The next CRL lists it.` });
                      },
                      onError: (error) => {
                        setRevoking(null);
                        setFeedback({ failure: certificateRefusalText(error, 'revoke the certificate'), done: null });
                      },
                    },
                  )
                }
              >
                Revoke
              </Button>
            </>
          }
        >
          <p>
            Revoking <span className="mono">{revoking.row.certificate.serial}</span> is immediate and
            cannot be undone.
          </p>
          <div className="field">
            <label htmlFor="certificate-revoke-reason">Reason</label>
            <select
              id="certificate-revoke-reason"
              value={revoking.reason}
              onChange={(event) => setRevoking({ ...revoking, reason: selectOption(REASONS, event.target.value) })}
            >
              {REASONS.map((reason) => (
                <option key={reason} value={reason}>
                  {reason}
                </option>
              ))}
            </select>
          </div>
        </Dialog>
      ) : null}

      {issuing ? (
        <IssueDialog
          project={project}
          environments={environments}
          onClose={() => setIssuing(false)}
          onIssued={(message) => setFeedback({ failure: null, done: message })}
        />
      ) : null}
    </>
  );
}

function IssueDialog({
  project,
  environments,
  onClose,
  onIssued,
}: {
  project: ProjectRef;
  environments: readonly EnvOption[];
  onClose: () => void;
  onIssued: (message: string) => void;
}) {
  const envId = useId();
  const profileId = useId();
  const algorithmId = useId();
  const [environment, setEnvironment] = useState(environments[0]?.id ?? '');
  const profiles = useCertificateProfiles(project, environment);
  const [profile, setProfile] = useState('');
  const [method, setMethod] = useState<'csr' | 'generate'>('csr');
  const [csrPem, setCsrPem] = useState('');
  const [algorithm, setAlgorithm] = useState<(typeof KEY_ALGORITHMS)[number]>('ecdsa-p256');
  const [dns, setDns] = useState('');
  const [ips, setIps] = useState('');
  const [uris, setUris] = useState('');
  const [commonName, setCommonName] = useState('');
  const [ttl, setTtl] = useState('');
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [certificatePem, setCertificatePem] = useState<string | null>(null);
  // The display-once private key lives only here, in component-owned state
  // with a session retirement boundary, never in a query or mutation cache.
  const [privateKey, setPrivateKey] = useSensitiveState<string | null>(null);
  const [stored, setStored] = useState(false);
  const csr = useIssueCsrCertificate(project);
  const refresh = useRefreshCertificates(project);
  const transport = useTransport();
  const bound = profiles.data?.profiles ?? [];
  const chosenProfile = profile !== '' ? profile : (bound[0]?.name ?? '');
  const disclosed = privateKey !== null;

  const dismiss = () => {
    if (busy || (disclosed && !stored)) {
      return;
    }
    setPrivateKey(null);
    onClose();
  };
  useNavigationGuard(busy || (disclosed && !stored), dismiss);

  const draft = (): IssueDraft | null => {
    const hours = ttl.trim() === '' ? null : Number(ttl);
    if (hours !== null && (!Number.isFinite(hours) || hours <= 0)) {
      setFailure('Enter the lifetime as a positive number of hours, or leave it empty for the profile default.');
      return null;
    }
    if (chosenProfile === '') {
      setFailure('No certificate profile is bound to this environment.');
      return null;
    }
    return { profile: chosenProfile, commonName, dnsNames: lines(dns), ipAddresses: lines(ips), uris: lines(uris), ttlHours: hours };
  };

  const run = async () => {
    setFailure(null);
    const request = draft();
    if (request === null) {
      return;
    }
    if (method === 'csr') {
      csr.mutate(
        { environment, csrPem, draft: request },
        {
          onSuccess: (result) => {
            setCertificatePem(`${result.certificate.certificate_pem ?? ''}${result.certificate.chain_pem ?? ''}`);
            onIssued(`Issued ${result.certificate.serial}.`);
          },
          onError: (error) => setFailure(certificateRefusalText(error, 'issue the certificate')),
        },
      );
      return;
    }
    setBusy(true);
    // Once the issue request leaves, a failure no longer proves nothing was
    // issued: the key may be gone while the certificate exists.
    let sent = false;
    try {
      await runPasskeyCeremony({ operation: 'mint', environmentId: environment, keyIds: [] });
      sent = true;
      const result = await issueGeneratedCertificate({ ...project, environment }, request, algorithm, transport);
      setPrivateKey(result.privateKeyPem);
      setCertificatePem(`${result.certificate.certificate_pem ?? ''}${result.certificate.chain_pem ?? ''}`);
      onIssued(`Issued ${result.certificate.serial} with a generated key.`);
    } catch (error) {
      setFailure(sent ? issueFailureText(error) : certificateRefusalText(error, 'issue the certificate'));
    } finally {
      refresh(environment);
      setBusy(false);
    }
  };

  const done = certificatePem !== null;
  return (
    <Dialog
      title={disclosed ? 'Certificate issued, private key shown exactly once' : 'Issue certificate'}
      size="wide"
      onCancel={(event) => {
        event.preventDefault();
        dismiss();
      }}
      actions={
        done ? (
          <Button variant="primary" type="button" disabled={disclosed && !stored} onClick={dismiss}>
            Done
          </Button>
        ) : (
          <>
            <Button type="button" onClick={dismiss} disabled={busy || csr.isPending}>
              Cancel
            </Button>
            <Button variant="primary" type="button" disabled={busy || csr.isPending} onClick={() => void run()}>
              {method === 'generate' ? 'Use a passkey and issue' : 'Issue'}
            </Button>
          </>
        )
      }
    >
      {failure !== null ? <Alert>{failure}</Alert> : null}
      {done ? (
        <>
          {disclosed ? (
            <>
              <Textarea
                label="Private key (PKCS#8 PEM), shown once"
                mono
                rows={6}
                readOnly
                value={privateKey ?? ''}
                hint="Store it now. Hikyo never stored it and cannot show it again: a lost key means revoking this certificate and issuing a new one."
              />
              <Checkbox label="I have stored the private key" checked={stored} onChange={(event) => setStored(event.target.checked)} />
            </>
          ) : null}
          <Textarea label="Certificate and chain (PEM)" mono rows={8} readOnly value={certificatePem ?? ''} hint="Public material." />
        </>
      ) : (
        <fieldset className="machine__lock" disabled={busy || csr.isPending}>
          <div className="field">
            <label htmlFor={envId}>Environment</label>
            <select
              id={envId}
              value={environment}
              onChange={(event) => {
                setEnvironment(event.target.value);
                setProfile('');
              }}
            >
              {environments.map((env) => (
                <option key={env.id} value={env.id}>
                  {env.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor={profileId}>Profile</label>
            <select id={profileId} value={chosenProfile} onChange={(event) => setProfile(event.target.value)}>
              {bound.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.name}: {[...p.policy.dns_patterns, ...p.policy.ip_ranges, ...p.policy.uri_patterns].join(', ')}
                </option>
              ))}
            </select>
            {profiles.isSuccess && bound.length === 0 ? (
              <p className="field__hint">No profile is bound to this environment. An operator binds one under Instance settings.</p>
            ) : null}
          </div>
          <Radio name="certificate-method" label="My own key: paste a CSR (the key never leaves your machine)" checked={method === 'csr'} onChange={() => setMethod('csr')} />
          <Radio name="certificate-method" label="Generate the key on the server and show it to me once" checked={method === 'generate'} onChange={() => setMethod('generate')} />
          {method === 'csr' ? (
            <Textarea
              label="Certificate signing request (PEM)"
              mono
              rows={6}
              value={csrPem}
              onChange={(event) => setCsrPem(event.target.value)}
              hint="Only its public key is used; the names come from the fields below."
            />
          ) : (
            <div className="field">
              <label htmlFor={algorithmId}>Key algorithm</label>
              <select id={algorithmId} value={algorithm} onChange={(event) => setAlgorithm(selectOption(KEY_ALGORITHMS, event.target.value))}>
                {KEY_ALGORITHMS.map((a) => (
                  <option key={a} value={a}>
                    {a}
                  </option>
                ))}
              </select>
            </div>
          )}
          <Textarea label="DNS names (one per line)" mono rows={3} value={dns} onChange={(event) => setDns(event.target.value)} />
          <Textarea label="IP addresses (one per line)" mono rows={2} value={ips} onChange={(event) => setIps(event.target.value)} />
          <Textarea label="URIs, e.g. SPIFFE IDs (one per line)" mono rows={2} value={uris} onChange={(event) => setUris(event.target.value)} />
          <Input label="Common name (optional, one of the DNS names)" mono value={commonName} onChange={(event) => setCommonName(event.target.value)} />
          <Input label="Lifetime in hours (optional)" inputMode="numeric" value={ttl} onChange={(event) => setTtl(event.target.value)} />
        </fieldset>
      )}
    </Dialog>
  );
}
