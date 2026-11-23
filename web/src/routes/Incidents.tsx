import { useState, type FormEvent } from 'react';
import { useParams, useSearchParams } from 'react-router';

import {
  INCIDENT_SCOPES,
  incidentFailureText,
  isOpenItem,
  itemStateLabel,
  useIncident,
  useIncidentActions,
  useIncidentItems,
  useOpenIncident,
  type Incident,
  type IncidentItem,
  type IncidentScope,
} from '../api/incidents.ts';
import { formatWhen } from '../lib/formatWhen.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge, type BadgeTone } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';
import { Select } from '../ui/Select.tsx';
import { ConsequencesDialog, Panel } from './Sections.tsx';
import { useFeedback } from './useFeedback.ts';

/**
 * Incident containment (#1039): open an incident on an account, organisation,
 * project or adapter, then work its ledger. The page shows what the server
 * names: an item refused by its owner reads differently from one that has not
 * run yet, and a rotation item shows the rotation's own state as the server
 * read it when the page loaded.
 */

const SCOPE_LABEL: Readonly<Record<IncidentScope, string>> = {
  account: 'Account',
  org: 'Organisation',
  project: 'Project',
  adapter: 'Deployment adapter',
};

function narrowScope(value: string): IncidentScope {
  return INCIDENT_SCOPES.find((scope) => scope === value) ?? 'account';
}

function itemTone(item: IncidentItem): BadgeTone {
  if (item.refused || item.outcome === 'blocked') return 'danger';
  if (item.outcome === 'done' || item.outcome === 'attested') return 'ok';
  if (item.outcome === 'out_of_scope') return 'neutral';
  return 'changed';
}

const REASON_TEXT: Readonly<Record<string, string>> = {
  owner_failed: 'The owner refused this item.',
  owner_unconfirmed: 'The owner did not confirm it.',
  needs_another_operator: 'Another operator must act.',
  enumeration_truncated: 'The enumeration was cut short.',
  upstream_not_observable: 'The upstream cannot be observed.',
  no_revoke_owner: 'Nothing here can revoke it.',
};

function OpenForm({ org, onOpened }: { org: string; onOpened: (id: string) => void }) {
  const open = useOpenIncident();
  const [scope, setScope] = useState<IncidentScope>('account');
  const [principal, setPrincipal] = useState('');
  const [project, setProject] = useState('');
  const [adapter, setAdapter] = useState('');
  const submit = (event: FormEvent) => {
    event.preventDefault();
    open.mutate({ scope, principal: principal.trim(), org, project: project.trim(), adapter: adapter.trim() }, {
      onSuccess: (incident) => onOpened(incident.id),
    });
  };
  return (
    <Panel id="incident-open" title="Open an incident">
      <p className="settings-note">
        Opening lists what the scope leaves behind. A scope with an open incident is enumerated again and only the missing
        objects are added.
      </p>
      <form className="form" onSubmit={submit}>
        <Select label="Scope" value={scope} onChange={(event) => setScope(narrowScope(event.target.value))}>
          {INCIDENT_SCOPES.map((value) => <option key={value} value={value}>{SCOPE_LABEL[value]}</option>)}
        </Select>
        {scope === 'account' ? <Input label="Account principal" mono value={principal} onChange={(event) => setPrincipal(event.target.value)} required /> : null}
        {scope === 'project' || scope === 'adapter' ? <Input label="Project" mono value={project} onChange={(event) => setProject(event.target.value)} required /> : null}
        {scope === 'adapter' ? <Input label="Adapter" mono value={adapter} onChange={(event) => setAdapter(event.target.value)} required /> : null}
        {scope === 'account' ? null : <p className="field__hint">Organisation: <span className="mono">{org}</span></p>}
        {open.isError ? <Alert>{incidentFailureText(open.error)}</Alert> : null}
        <div className="actions">
          <Button type="submit" variant="primary" disabled={open.isPending}>{open.isPending ? 'Opening…' : 'Open incident'}</Button>
        </div>
      </form>
    </Panel>
  );
}

function Counts({ incident }: { incident: Incident }) {
  const counts = incident.counts;
  const entries: readonly (readonly [string, number])[] = [
    ['Not yet run', counts.pending],
    ['Unconfirmed', counts.unknown],
    ['Blocked', counts.blocked],
    ['Done', counts.done],
    ['Attested', counts.attested],
    ['Out of scope', counts.out_of_scope],
  ];
  return (
    <dl className="incident__counts">
      {entries.map(([label, value]) => (
        <div key={label}><dt>{label}</dt><dd>{value}</dd></div>
      ))}
    </dl>
  );
}

function Ledger({ id }: { id: string }) {
  const incident = useIncident(id);
  const items = useIncidentItems(id);
  const actions = useIncidentActions(id);
  const feedback = useFeedback(incidentFailureText);
  const [attesting, setAttesting] = useState<IncidentItem | null>(null);
  const [closing, setClosing] = useState(false);

  if (incident.isPending || items.isPending) return <p role="status">Loading the incident…</p>;
  if (incident.isError) return <Alert>{incidentFailureText(incident.error)}</Alert>;
  if (items.isError) return <Alert>{incidentFailureText(items.error)}</Alert>;
  const live = incident.data.status === 'open';
  const open = items.data.filter(isOpenItem);

  return (
    <Panel id="incident-ledger" title="Incident ledger">
      <p>
        <Badge tone={live ? 'changed' : 'ok'}>{incident.data.status.replaceAll('_', ' ')}</Badge>{' '}
        {SCOPE_LABEL[narrowScope(incident.data.scope_kind)]} <span className="mono">{incident.data.scope_id}</span>
        {' '}opened <time dateTime={incident.data.opened_at}>{formatWhen(incident.data.opened_at)}</time>
      </p>
      <Counts incident={incident.data} />
      {feedback.done === null ? null : <Alert tone="done">{feedback.done}</Alert>}
      {feedback.failure === null ? null : <Alert>{feedback.failure}</Alert>}
      {live ? (
        <div className="actions">
          <Button
            type="button"
            variant="primary"
            disabled={actions.resume.isPending}
            onClick={() => {
              feedback.clear();
              actions.resume.mutate(undefined, {
                onSuccess: (r) => feedback.ok(`Resume ran ${r.examined} items: ${r.confirmed} confirmed, ${r.failed} refused, ${r.blocked} blocked, ${r.unconfirmed} unconfirmed.`),
                onError: feedback.report,
              });
            }}
          >
            {actions.resume.isPending ? 'Resuming…' : 'Resume open items'}
          </Button>
          <Button type="button" variant="danger" disabled={actions.close.isPending} onClick={() => { feedback.clear(); setClosing(true); }}>
            {open.length === 0 ? 'Close incident' : 'Close with residuals'}
          </Button>
        </div>
      ) : null}
      {items.data.length === 0 ? <p role="status">This incident has no items.</p> : (
        <div className="table-scroll" role="region" aria-label="Incident items" tabIndex={0}>
          <table className="incident__items">
            <caption className="visually-hidden">Ledger items of the incident</caption>
            <thead><tr>
              <th scope="col">Object</th><th scope="col">State</th><th scope="col">Rotation</th><th scope="col">Detail</th><th scope="col"><span className="visually-hidden">Actions</span></th>
            </tr></thead>
            <tbody>
              {items.data.map((item) => (
                <tr key={item.id}>
                  <td>{item.object_kind}<p className="field__hint mono" title={item.id}>{item.object_id}</p></td>
                  <td><Badge tone={itemTone(item)}>{itemStateLabel(item)}</Badge></td>
                  <td>{item.object_state === undefined ? '' : <Badge tone={item.object_state === 'active' ? 'ok' : item.object_state === 'gone' ? 'neutral' : 'danger'} mono>{item.object_state}</Badge>}</td>
                  <td>{item.reason === undefined ? '' : (REASON_TEXT[item.reason] ?? item.reason)}</td>
                  <td>
                    {live && isOpenItem(item) ? (
                      <Button type="button" disabled={actions.attest.isPending} onClick={() => { feedback.clear(); setAttesting(item); }}>
                        Attest <span className="visually-hidden">{item.object_id}</span>
                      </Button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {attesting === null ? null : (
        <ConsequencesDialog
          title="Attest this item?"
          confirmLabel="Attest item"
          busyLabel="Attesting…"
          busy={actions.attest.isPending}
          failure={feedback.failure}
          onCancel={() => { setAttesting(null); feedback.clear(); }}
          onConfirm={() => actions.attest.mutate(attesting.id, {
            onSuccess: () => { setAttesting(null); feedback.ok('Item attested. It is recorded as stated by you, not confirmed by its owner.'); },
            onError: feedback.report,
          })}
        >
          <p>Record that <span className="mono">{attesting.object_id}</span> ({attesting.object_kind}) was dealt with outside this system.</p>
          <p>An attestation is your statement. The owner function did not confirm it, and the ledger names you as the one who stated it.</p>
        </ConsequencesDialog>
      )}
      {!closing ? null : (
        <ConsequencesDialog
          title={open.length === 0 ? 'Close this incident?' : 'Close with residuals?'}
          confirmLabel={open.length === 0 ? 'Close incident' : 'Close with residuals'}
          busyLabel="Closing…"
          busy={actions.close.isPending}
          failure={feedback.failure}
          onCancel={() => { setClosing(false); feedback.clear(); }}
          onConfirm={() => actions.close.mutate(open.length > 0, {
            onSuccess: () => { setClosing(false); feedback.ok('Incident closed.'); },
            onError: feedback.report,
          })}
        >
          {open.length === 0
            ? <p>Every item is resolved. Closing ends the ledger.</p>
            : <p>{open.length} items are still open. They stay listed as residuals and the incident is recorded as closed with residuals.</p>}
        </ConsequencesDialog>
      )}
    </Panel>
  );
}

export function Incidents() {
  const { org = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const id = params.get('incident') ?? '';
  return (
    <div className="page page--chrome incident">
      <h1>Incident containment</h1>
      <p className="page__lede">
        Contain an affected scope and follow what remains. Requires credential-reset at the incident&apos;s organisation.
        Session reset and local secret removal are not full containment: unresolved items stay listed until an owner confirms
        them or you attest them.
      </p>
      <OpenForm org={org} onOpened={(opened) => setParams({ incident: opened })} />
      {id === '' ? null : <Ledger key={id} id={id} />}
    </div>
  );
}
