import { useState } from 'react';

import {
  developerCredentialFailureText,
  useProjectDeveloperCredentials,
  useRevokeProjectDeveloperCredential,
} from '../api/developerCredentials.ts';
import type { ProjectRef } from '../api/identities.ts';
import { useEnvironments } from '../api/settings.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { NamedId } from '../ui/NamedId.tsx';
import { ConsequencesDialog, Panel } from './Sections.tsx';
import { useFeedback } from './useFeedback.ts';

/** Keyed by the owning project so an incident action cannot follow navigation. */
export function ProjectDeveloperCredentials(ref: ProjectRef) {
  const credentials = useProjectDeveloperCredentials(ref);
  const revoke = useRevokeProjectDeveloperCredential(ref);
  const [selected, setSelected] = useState<string | null>(null);
  const environments = useEnvironments(ref.org, ref.project);
  const environmentName = (id: string) => environments.data?.items.find((item) => item.id === id)?.name;
  const feedback = useFeedback(developerCredentialFailureText);
  const target = credentials.isSuccess ? credentials.data.items.find((item) => item.id === selected) : undefined;

  return <Panel id="project-developer-credentials" title="Developer credentials">
    <p className="settings-note">
      Inspect and revoke this project&apos;s bounded developer credentials with manage-identities permission.
      Credentials deliver current published values from one unprotected environment using the human delegator&apos;s current access.
      Minting and your personal credential lifecycle remain in the CLI: <code>hikyo dev session</code>.
      Bearers cannot be retrieved here.
    </p>
    {credentials.isPending ? <p role="status">Loading developer credentials…</p> : null}
    {credentials.isError ? <Alert>{developerCredentialFailureText(credentials.error)}</Alert> : null}
    {feedback.done === null ? null : <Alert tone="done">{feedback.done}</Alert>}
    {target === undefined && feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
    <div className="actions">
      <Button type="button" readOnlySafe disabled={credentials.isFetching || revoke.isPending} onClick={() => { void credentials.refetch(); }}>
        {credentials.isFetching ? 'Refreshing…' : 'Refresh developer credentials'}
      </Button>
    </div>
    {credentials.isSuccess && credentials.data.items.length === 0 ? <p role="status">No developer credentials in this project.</p> : null}
    {credentials.isSuccess && credentials.data.items.length > 0 ? <div className="table-scroll">
      <table>
        <caption>Developer credential metadata, including expired and revoked credentials</caption>
        <thead><tr><th scope="col">Credential</th><th scope="col">Human delegator</th><th scope="col">Environment</th><th scope="col">Lifetime</th><th scope="col">Revocation</th></tr></thead>
        <tbody>{credentials.data.items.map((credential) => <tr key={credential.id}>
          <td><code title={credential.id}>{credential.prefix_hint}…</code><p className="field__hint">Developer: <span className="mono">{credential.principal_id}</span></p></td>
          <td><NamedId id={credential.authority_principal_id} name={credential.authority_principal_name} /></td>
          <td><NamedId id={credential.environment_id} name={environmentName(credential.environment_id)} /></td>
          <td>Created <time dateTime={credential.created_at}>{credential.created_at}</time><br />Expires <time dateTime={credential.expires_at}>{credential.expires_at}</time></td>
          <td>{credential.revoked_at === undefined ? <Button type="button" disabled={revoke.isPending} onClick={() => { feedback.clear(); setSelected(credential.id); }}>Revoke <span className="visually-hidden">{credential.id}</span></Button> : <>Revoked <time dateTime={credential.revoked_at}>{credential.revoked_at}</time></>}</td>
        </tr>)}</tbody>
      </table>
    </div> : null}
    {target === undefined || target.revoked_at !== undefined ? null : <ConsequencesDialog
      title="Revoke developer credential?"
      confirmLabel="Revoke credential"
      busyLabel="Revoking developer credential…"
      busy={revoke.isPending}
      failure={feedback.failure}
      onCancel={() => { setSelected(null); feedback.clear(); }}
      onConfirm={() => revoke.mutate(target.id, {
        onSuccess: () => { setSelected(null); feedback.ok('Developer credential revoked. Its next fetch will be refused.'); },
        onError: feedback.report,
      })}
    >
      <p>Revoke <code title={target.id}>{target.prefix_hint}…</code>, delegated by <NamedId id={target.authority_principal_id} name={target.authority_principal_name} /> for environment <NamedId id={target.environment_id} name={environmentName(target.environment_id)} />.</p>
      <p>This is permanent. The next fetch will be refused; values already delivered to running processes cannot be erased. Another credential requires a fresh interactive CLI ceremony.</p>
    </ConsequencesDialog>}
  </Panel>;
}
