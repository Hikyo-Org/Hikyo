import { useMemo, useState } from 'react';
import { useParams, useSearchParams } from 'react-router';

import { metadataReadDenied } from '../api/metadataQuery.ts';
import { useEnvironments } from '../api/settings.ts';
import { transitRefusalText, useRefreshTransit, useTransitKey, useTransitKeys, type EnvRef, type TransitKey } from '../api/transit.ts';
import { useWorkspaceContext } from '../api/transport.tsx';
import { useAuth } from '../app/AuthProvider.tsx';
import { formatWhen } from '../lib/formatWhen.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Select } from '../ui/Select.tsx';
import { Textarea } from '../ui/Textarea.tsx';
import { TransitCreateKey, TransitKeyManagement } from './TransitKeyManagement.tsx';
import { TransitOperations } from './TransitOperations.tsx';

export function TransitKeyFacts({ transitKey }: { transitKey: TransitKey }) {
  return <>
    <dl className="adapters__facts">
      <dt>Algorithm</dt><dd>{transitKey.algorithm}</dd><dt>Custody</dt><dd>{transitKey.custody}</dd>
      <dt>State</dt><dd><Badge tone={transitKey.state === 'active' ? 'ok' : 'changed'}>{transitKey.state}</Badge></dd>
      <dt>Latest version</dt><dd>{String(transitKey.latest_version)}</dd>
      <dt>Minimum encrypt version</dt><dd>{String(transitKey.min_encrypt_version)}</dd><dt>Minimum decrypt version</dt><dd>{String(transitKey.min_decrypt_version)}</dd>
      <dt>Compromised through version</dt><dd>{String(transitKey.compromised_through_version)}</dd>
      <dt>Rotation period</dt><dd>{transitKey.rotation_period_seconds === 0n ? 'Automatic rotation off' : `${String(transitKey.rotation_period_seconds)} seconds`}</dd>
      <dt>Rotation due</dt><dd>{transitKey.rotation_due ? 'Yes' : 'No'}</dd>
      <dt>Deletion scheduled</dt><dd>{transitKey.deletion_after == null ? 'Not scheduled' : formatWhen(transitKey.deletion_after)}</dd>
      <dt>Allowed operations</dt><dd>{transitKey.allowed_operations.join(', ')}</dd>
    </dl>
    <h3>Key versions</h3><div className="table-scroll"><table className="values__table machine__table">
      <caption className="visually-hidden">Public version metadata for {transitKey.name}</caption><thead><tr><th scope="col">Version</th><th scope="col">Material status</th><th scope="col">Created</th><th scope="col">Public key</th></tr></thead>
      <tbody>{transitKey.versions.map((version) => <tr key={String(version.version)}><td>{String(version.version)}</td><td>{version.material}</td><td>{formatWhen(version.created_at)}</td><td>{version.public_key == null ? 'None' : <TextareaPublicKey value={version.public_key} version={String(version.version)} />}</td></tr>)}</tbody>
    </table></div>
    <h3>Caller entries</h3>{transitKey.callers.length === 0 ? <p>No caller restrictions. Any principal with crypto-use on this environment may use the allowed operations.</p> : <ul>{transitKey.callers.map((caller) => <li key={caller.principal_id}><code>{caller.principal_id}</code>: {caller.operations.join(', ')}</li>)}</ul>}
  </>;
}

function TextareaPublicKey({ value, version }: { value: string; version: string }) {
  return <details><summary>Public key for version {version}</summary><Textarea label={`Public key version ${version}`} value={value} readOnly mono /></details>;
}

/** Public metadata is cacheable; cryptographic input and output are owned only
 * by the keyed child surface, which unmounts at every authority boundary. */
export function TransitKeys() {
  const { org = '', project = '' } = useParams();
  const [search, setSearch] = useSearchParams();
  const environments = useEnvironments(org, project);
  const topologyAvailable = environments.data !== null && !metadataReadDenied(environments.error);
  const requestedEnvironment = search.get('env');
  const current = topologyAvailable ? (requestedEnvironment === null ? environments.data?.items[0] : environments.data?.items.find((environment) => environment.id === requestedEnvironment)) : undefined;
  const environment = current?.id ?? '';
  const env = useMemo<EnvRef | null>(() => environment === '' ? null : { org, project, environment }, [org, project, environment]);
  const selected = search.get('key');
  const listing = useTransitKeys(env);
  const detail = useTransitKey(env, selected);
  const refresh = useRefreshTransit(env);
  const auth = useAuth();
  const workspace = useWorkspaceContext();
  const session = auth.identity?.session.id ?? '';
  const canAct = auth.state.status === 'authenticated';
  const [creating, setCreating] = useState(false);
  const listAvailable = env !== null && listing.data !== null && !metadataReadDenied(listing.error);
  const key = listAvailable && detail.data !== null && !metadataReadDenied(detail.error) && listing.data?.items.some((entry) => entry.id === detail.data?.id) ? detail.data : undefined;
  const scope = [org, project, environment, workspace?.origin ?? '', session].join('\u0000');
  const revision = key === undefined ? '' : [key.id, key.updated_at, key.state, key.latest_version, key.min_encrypt_version, key.min_decrypt_version, key.compromised_through_version, key.allowed_operations.join(','), key.callers.map((caller) => `${caller.principal_id}:${caller.operations.join(',')}`).join(';')].join('\u0000');
  const select = (field: 'env' | 'key', value: string) => {
    const next = new URLSearchParams(search);
    next.set(field, value);
    if (field === 'env') { next.delete('key'); setCreating(false); }
    setSearch(next, { replace: true });
  };
  return <div className="page page--chrome">
    <header className="page__head"><h1>Transit keys</h1><p className="page__lede">Environment-scoped encryption, signing and message authentication. Managed private key material is never returned.</p></header>
    {environments.isPending ? <p role="status">Loading environments…</p> : null}
    {environments.isError || !topologyAvailable ? <Alert>Environments could not be loaded. Check your project access and refresh.</Alert> : null}
    {topologyAvailable && environments.data?.items.length === 0 ? <p>This project has no environments. Create an environment before adding Transit keys.</p> : null}
    {topologyAvailable && (environments.data?.items.length ?? 0) > 0 ? <Select label="Environment" value={environment} onChange={(event) => select('env', event.target.value)}>
      {current === undefined ? <option value="">Choose an available environment</option> : null}
      {environments.data?.items.map((entry) => <option key={entry.id} value={entry.id}>{entry.name}</option>)}
    </Select> : null}
    {requestedEnvironment !== null && current === undefined && topologyAvailable && environments.isSuccess ? <Alert>The selected environment is no longer available. Choose an environment to continue.</Alert> : null}
    {env === null ? null : <>
      <section className="card machine__card" aria-label="Transit keys list">
        <h2>{current?.name} keys</h2><div className="actions"><Button type="button" variant="primary" disabled={!canAct} onClick={() => setCreating(true)}>Create key</Button><Button type="button" onClick={refresh} readOnlySafe>Refresh keys</Button></div>
        {listing.isPending ? <p role="status">Loading Transit keys…</p> : null}
        {listing.isError ? <Alert>{transitRefusalText(listing.error)}</Alert> : null}
        {listAvailable && listing.data?.items.length === 0 ? <p role="status">No Transit keys in this environment.</p> : null}
        {listAvailable && (listing.data?.items.length ?? 0) > 0 ? <div className="table-scroll"><table className="values__table machine__table"><caption className="visually-hidden">Transit keys in the selected environment</caption><thead><tr><th scope="col">Name</th><th scope="col">Algorithm</th><th scope="col">State</th><th scope="col">Latest version</th><th scope="col">Rotation</th><th scope="col">Deletion</th><th scope="col">Actions</th></tr></thead><tbody>{listing.data?.items.map((entry) => <tr key={entry.id}><th scope="row" className="mono">{entry.name}</th><td>{entry.algorithm}</td><td><Badge tone={entry.state === 'active' ? 'ok' : 'changed'}>{entry.state}</Badge>{entry.compromised_through_version > 0n ? <Badge tone="danger">Compromised versions</Badge> : null}</td><td>{String(entry.latest_version)}</td><td>{entry.rotation_due ? <Badge tone="changed">Rotation due</Badge> : 'Not due'}</td><td>{entry.deletion_after == null ? 'Not scheduled' : formatWhen(entry.deletion_after)}</td><td><Button type="button" readOnlySafe onClick={() => select('key', entry.name)}>Inspect {entry.name}</Button></td></tr>)}</tbody></table></div> : null}
      </section>
      {selected === null ? <p>Select a key to inspect its public metadata and operations.</p> : <section className="card machine__card" aria-label="Selected Transit key">
        <h2>{selected}</h2>{detail.isPending ? <p role="status">Loading Transit key…</p> : null}{detail.isError ? <Alert>{transitRefusalText(detail.error)}</Alert> : null}
        {key === undefined ? null : <><TransitKeyFacts transitKey={key} /><TransitKeyManagement key={`management:${scope}:${revision}`} env={env} transitKey={key} canAct={canAct} />{canAct ? <TransitOperations key={`operations:${scope}:${revision}`} env={env} transitKey={key} canAct={canAct} /> : null}</>}
      </section>}
      {creating && canAct ? <TransitCreateKey key={scope} env={env} onClose={() => setCreating(false)} /> : null}
    </>}
  </div>;
}
