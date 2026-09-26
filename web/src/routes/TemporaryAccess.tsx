import { useState } from 'react';
import { useParams } from 'react-router';

import { ApiError } from '../api/client.ts';
import { useEnvironments } from '../api/settings.ts';
import {
  ACCESS_CAPABILITIES,
  useAccessAction,
  useAccessPolicies,
  useAccessQueue,
  useDeleteAccessPolicy,
  useSaveAccessPolicy,
  type AccessApprover,
  type AccessCapability,
  type AccessPolicy,
  type AccessPolicyDraft,
  type AccessRequest,
  type AccessRequestDraft,
} from '../api/temporaryAccess.ts';
import { fetchRevealWindow } from '../api/values.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Ceremony, type CeremonyRequest } from './Ceremony.tsx';
import { JumpIndex, Panel } from './Sections.tsx';

/**
 * Temporary access (#152): approval-mediated, time-bound access to one
 * environment. A requester asks for exact capabilities for a bounded duration
 * with a reason; eligible approvers decide; the grant carries an absolute
 * expiry the server enforces on every operation. Policy administration sits on
 * the same page for member managers. Every state is text-labelled.
 */

/** A stored UTC timestamp rendered in the operator's locale. */
function when(value: string | undefined): string {
  if (value === undefined) return '-';
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString();
}

/** A duration in seconds as the largest whole unit a human reads. */
export function duration(seconds: number): string {
  if (seconds % 3600 === 0) return `${seconds / 3600}h`;
  if (seconds % 60 === 0) return `${seconds / 60}m`;
  return `${seconds}s`;
}

function refusal(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 401:
      case 403:
        return error.detail ?? 'You may not perform this action, or re-authentication is required.';
      case 404:
        return 'Not found, or you may not see it.';
      case 409:
        return error.detail ?? 'This request can no longer be acted on (it was resolved, expired, or invalidated).';
      case 400:
        return error.detail ?? 'The request is not valid.';
    }
  }
  return 'The action could not be completed.';
}

const emptyPolicy: AccessPolicyDraft = {
  environmentId: '',
  capabilities: ['reveal'],
  maxDurationSeconds: 8 * 3600,
  minApprovals: 1,
  allowSelfApproval: false,
  requestTtlSeconds: 86400,
  enabled: true,
  approvers: [],
  bypassers: [],
};

function approversToText(approvers: readonly AccessApprover[]): string {
  return approvers
    .map((a) => (a.kind === 'scim_group' ? `group:${a.subject_id}:${a.binding_id ?? ''}` : a.subject_id))
    .join('\n');
}

function parseApprovers(text: string): AccessApprover[] {
  const out: AccessApprover[] = [];
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (line === '') continue;
    if (line.startsWith('group:')) {
      const [, group, binding] = line.split(':');
      out.push({ kind: 'scim_group', subject_id: group ?? '', binding_id: binding ?? '' });
    } else {
      out.push({ kind: 'principal', subject_id: line });
    }
  }
  return out;
}

function parseList(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '');
}

function toggle(list: readonly AccessCapability[], capability: AccessCapability, on: boolean): AccessCapability[] {
  const next = list.filter((c) => c !== capability);
  if (on) next.push(capability);
  return ACCESS_CAPABILITIES.filter((c) => next.includes(c));
}

export function TemporaryAccess() {
  const params = useParams();
  const org = params.org ?? '';
  const project = params.project ?? '';
  const ref = { org, project };
  const me = useAuth().identity?.principal.id ?? '';

  const environments = useEnvironments(org, project);
  const envItems = environments.data?.items ?? [];
  const policies = useAccessPolicies(ref);
  const [selectedEnv, setSelectedEnv] = useState('');
  const queue = useAccessQueue(ref, selectedEnv);
  const action = useAccessAction(ref, selectedEnv);
  const savePolicy = useSaveAccessPolicy(ref);
  const deletePolicy = useDeleteAccessPolicy(ref);

  const [actionError, setActionError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [requested, setRequested] = useState<readonly AccessCapability[]>([]);
  const [requestHours, setRequestHours] = useState(1);
  const [reason, setReason] = useState('');
  const [emergencyReason, setEmergencyReason] = useState('');
  const [ceremony, setCeremony] = useState<{ request: CeremonyRequest; draft: AccessRequestDraft } | null>(null);

  const [formOpen, setFormOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState<AccessPolicyDraft>(emptyPolicy);
  const [approversText, setApproversText] = useState('');
  const [bypassersText, setBypassersText] = useState('');

  const offer = queue.data?.offer;
  const envName = envItems.find((e) => e.id === selectedEnv)?.name ?? selectedEnv;
  const isManager = policies.isSuccess;

  const done = (message: string) => ({
    onSuccess: () => setNotice(message),
    onError: (error: unknown) => setActionError(refusal(error)),
  });
  const start = () => {
    setActionError(null);
    setNotice(null);
  };

  const submitRequest = () => {
    start();
    action.mutate(
      {
        kind: 'request',
        draft: { capabilities: requested, durationSeconds: Math.round(requestHours * 3600), reason },
      },
      {
        onSuccess: () => {
          setNotice('Request submitted. It takes effect once the required approvers approve it.');
          setReason('');
        },
        onError: (error) => setActionError(refusal(error)),
      },
    );
  };

  // Emergency access always takes its own purpose-bound decision unless a
  // live sliding window already stands over this environment.
  const takeEmergency = async () => {
    start();
    const draftRequest: AccessRequestDraft = { capabilities: requested, reason: emergencyReason };
    try {
      const window = await fetchRevealWindow({ org, project, environment: selectedEnv });
      if (window.live && !window.single_decision) {
        action.mutate({ kind: 'emergency', draft: draftRequest }, done('Emergency access granted. It is recorded and time-bound.'));
        return;
      }
      setCeremony({
        draft: draftRequest,
        request: { purpose: 'access', environmentId: selectedEnv, environmentName: envName, keys: [], window },
      });
    } catch (error) {
      setActionError(refusal(error));
    }
  };

  const openEditor = (policy: AccessPolicy | null) => {
    setActionError(null);
    if (policy === null) {
      setEditingId(null);
      setDraft(emptyPolicy);
      setApproversText('');
      setBypassersText('');
    } else {
      setEditingId(policy.id);
      setDraft({
        environmentId: policy.environment_id,
        capabilities: policy.capabilities,
        maxDurationSeconds: policy.max_duration_seconds,
        minApprovals: policy.min_approvals,
        allowSelfApproval: policy.allow_self_approval,
        requestTtlSeconds: policy.request_ttl_seconds,
        enabled: policy.enabled,
        approvers: policy.approvers,
        bypassers: policy.bypassers,
      });
      setApproversText(approversToText(policy.approvers));
      setBypassersText(policy.bypassers.join('\n'));
    }
    setFormOpen(true);
  };

  const submitPolicy = () => {
    setActionError(null);
    savePolicy.mutate(
      {
        id: editingId,
        draft: { ...draft, approvers: parseApprovers(approversText), bypassers: parseList(bypassersText) },
      },
      {
        onSuccess: () => setFormOpen(false),
        onError: (error) => setActionError(refusal(error)),
      },
    );
  };

  return (
    <div className="page page--chrome temporary-access">
      <h1>Temporary access</h1>
      <p className="page__lede">
        Ask for time-bound access to one environment. Approved access ends at a fixed time on
        every session and device, and can be revoked earlier.
      </p>

      <JumpIndex
        sections={[
          { id: 'ta-requests', label: 'Requests' },
          { id: 'ta-policies', label: 'Policies' },
        ]}
      />

      <Panel id="ta-requests" title="Requests">
        <label htmlFor="ta-env">Environment</label>
        <select id="ta-env" value={selectedEnv} onChange={(event) => setSelectedEnv(event.target.value)}>
          <option value="">Choose an environment</option>
          {envItems.map((env) => (
            <option key={env.id} value={env.id}>
              {env.name}
            </option>
          ))}
        </select>

        {actionError === null ? null : <Alert>{actionError}</Alert>}
        {notice === null ? null : <p role="status">{notice}</p>}

        {selectedEnv === '' ? <p role="status">Choose an environment to request access or review requests.</p> : null}
        {selectedEnv !== '' && queue.isPending ? <p role="status">Loading requests…</p> : null}
        {selectedEnv !== '' && queue.isError ? <Alert>{refusal(queue.error)}</Alert> : null}

        {selectedEnv !== '' && queue.isSuccess && offer === undefined ? (
          <p role="status">No access policy covers this environment, so nothing can be requested here.</p>
        ) : null}
        {offer !== undefined && !offer.enabled ? (
          <p role="status">The access policy for this environment is disabled. New requests are refused.</p>
        ) : null}

        {offer !== undefined && offer.enabled ? (
          <form
            className="temporary-access__form panel"
            onSubmit={(event) => {
              event.preventDefault();
              submitRequest();
            }}
          >
            <h3>Request access to {envName}</h3>
            <fieldset>
              <legend>Capabilities</legend>
              {offer.capabilities.map((capability) => (
                <Checkbox
                  key={capability}
                  label={capability}
                  checked={requested.includes(capability)}
                  onChange={(event) => setRequested(toggle(requested, capability, event.target.checked))}
                />
              ))}
            </fieldset>
            <label htmlFor="ta-hours">Duration (hours, at most {duration(offer.max_duration_seconds)})</label>
            <input
              id="ta-hours"
              type="number"
              min={0.25}
              step={0.25}
              max={offer.max_duration_seconds / 3600}
              value={requestHours}
              onChange={(event) => setRequestHours(Number(event.target.value))}
            />
            <label htmlFor="ta-reason">Reason</label>
            <textarea id="ta-reason" rows={2} maxLength={512} value={reason} onChange={(event) => setReason(event.target.value)} />
            <p className="field__hint">
              {offer.min_approvals === 1 ? 'One approval is' : `${offer.min_approvals} approvals are`} required.
              The request cannot be changed once submitted.
            </p>
            <div className="temporary-access__form-actions">
              <Button
                type="submit"
                variant="primary"
                disabled={action.isPending || requested.length === 0 || reason.trim() === '' || !(requestHours > 0)}
              >
                Request access
              </Button>
            </div>
            {offer.caller_may_bypass ? (
              <div className="temporary-access__emergency">
                <label htmlFor="ta-emergency-reason">Emergency reason</label>
                <input
                  id="ta-emergency-reason"
                  type="text"
                  maxLength={512}
                  value={emergencyReason}
                  onChange={(event) => setEmergencyReason(event.target.value)}
                />
                <Button
                  type="button"
                  variant="danger"
                  disabled={action.isPending || requested.length === 0 || emergencyReason.trim() === ''}
                  onClick={() => void takeEmergency()}
                >
                  Take emergency access
                </Button>
                <p className="field__hint">
                  Emergency access skips the approvals, requires you to re-authenticate, lasts at most one
                  hour by default, and is recorded as a high-signal audit event.
                </p>
              </div>
            ) : null}
          </form>
        ) : null}

        {selectedEnv !== '' && queue.isSuccess && queue.data.items.length === 0 ? (
          <p role="status">No access requests in this environment.</p>
        ) : null}
        {selectedEnv !== '' && queue.isSuccess && queue.data.items.length > 0 ? (
          <ul className="temporary-access__requests" aria-label="Access requests">
            {queue.data.items.map((request) => (
              <AccessRequestRow
                key={request.id}
                request={request}
                mine={request.requester === me}
                busy={action.isPending}
                onAct={(kind) => {
                  start();
                  const messages = {
                    approve: 'Vote recorded.',
                    reject: 'Request rejected.',
                    cancel: 'Request withdrawn.',
                    revoke: 'Access revoked. It ended immediately.',
                  };
                  action.mutate({ kind, request: request.id }, done(messages[kind]));
                }}
              />
            ))}
          </ul>
        ) : null}
      </Panel>

      <Panel id="ta-policies" title="Policies">
        {policies.isPending ? <p role="status">Loading policies…</p> : null}
        {policies.isError ? (
          <p role="status">Access policies are administered by project member managers.</p>
        ) : null}
        {isManager ? (
          <div className="temporary-access__section-head">
            <Button type="button" onClick={() => openEditor(null)}>
              New access policy
            </Button>
          </div>
        ) : null}
        {isManager && policies.data.items.length === 0 ? (
          <p>No access policies. Nothing is requestable in this project yet.</p>
        ) : null}
        {isManager && policies.data.items.length > 0 ? (
          <table className="temporary-access__policies">
            <thead>
              <tr>
                <th>Environment</th>
                <th>Capabilities</th>
                <th>Longest</th>
                <th>Approvals</th>
                <th>State</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {policies.data.items.map((policy) => (
                <tr key={policy.id}>
                  <td>
                    {policy.environment_id === ''
                      ? 'All environments'
                      : envItems.find((e) => e.id === policy.environment_id)?.name ?? policy.environment_id}
                  </td>
                  <td>{policy.capabilities.join(', ')}</td>
                  <td>{duration(policy.max_duration_seconds)}</td>
                  <td>{policy.min_approvals}</td>
                  <td>{policy.enabled ? 'enabled' : 'disabled'}</td>
                  <td>
                    <Button type="button" onClick={() => openEditor(policy)}>
                      Edit
                    </Button>
                    <Button
                      type="button"
                      variant="danger"
                      onClick={() => {
                        setActionError(null);
                        deletePolicy.mutate(policy.id, { onError: (error) => setActionError(refusal(error)) });
                      }}
                    >
                      Delete
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}

        {formOpen ? (
          <form
            className="temporary-access__form panel"
            onSubmit={(event) => {
              event.preventDefault();
              submitPolicy();
            }}
          >
            <h3>{editingId === null ? 'New access policy' : 'Edit access policy'}</h3>
            <label htmlFor="ta-policy-env">Environment</label>
            <select
              id="ta-policy-env"
              value={draft.environmentId}
              disabled={editingId !== null}
              onChange={(event) => setDraft({ ...draft, environmentId: event.target.value })}
            >
              <option value="">All environments in the project</option>
              {envItems.map((env) => (
                <option key={env.id} value={env.id}>
                  {env.name}
                </option>
              ))}
            </select>
            <fieldset>
              <legend>Requestable capabilities</legend>
              {ACCESS_CAPABILITIES.map((capability) => (
                <Checkbox
                  key={capability}
                  label={capability}
                  checked={draft.capabilities.includes(capability)}
                  onChange={(event) =>
                    setDraft({ ...draft, capabilities: toggle(draft.capabilities, capability, event.target.checked) })
                  }
                />
              ))}
            </fieldset>
            <label htmlFor="ta-max">Longest access (hours)</label>
            <input
              id="ta-max"
              type="number"
              min={1}
              value={Math.round(draft.maxDurationSeconds / 3600)}
              onChange={(event) =>
                setDraft({ ...draft, maxDurationSeconds: Math.max(1, Number(event.target.value)) * 3600 })
              }
            />
            <label htmlFor="ta-min">Approvals required</label>
            <input
              id="ta-min"
              type="number"
              min={1}
              value={draft.minApprovals}
              onChange={(event) => setDraft({ ...draft, minApprovals: Number(event.target.value) })}
            />
            <label htmlFor="ta-ttl">Review window (hours)</label>
            <input
              id="ta-ttl"
              type="number"
              min={1}
              value={Math.round(draft.requestTtlSeconds / 3600)}
              onChange={(event) =>
                setDraft({ ...draft, requestTtlSeconds: Math.max(1, Number(event.target.value)) * 3600 })
              }
            />
            <Checkbox
              label="Allow the requester to approve their own request"
              checked={draft.allowSelfApproval}
              onChange={(event) => setDraft({ ...draft, allowSelfApproval: event.target.checked })}
            />
            <Checkbox
              label="Enabled"
              checked={draft.enabled}
              onChange={(event) => setDraft({ ...draft, enabled: event.target.checked })}
            />
            <label htmlFor="ta-approvers">Approver IDs (one per line, or group:groupId:bindingId)</label>
            <textarea id="ta-approvers" rows={3} value={approversText} onChange={(event) => setApproversText(event.target.value)} />
            <label htmlFor="ta-bypassers">Emergency-access principal IDs (one per line)</label>
            <textarea id="ta-bypassers" rows={2} value={bypassersText} onChange={(event) => setBypassersText(event.target.value)} />
            <p className="field__hint">
              An approver can approve only capabilities they could grant themselves. This page cannot search
              the user directory; enter exact principal IDs.
            </p>
            <div className="temporary-access__form-actions">
              <Button type="submit" variant="primary" disabled={savePolicy.isPending}>
                {savePolicy.isPending ? 'Saving…' : 'Save policy'}
              </Button>
              <Button type="button" onClick={() => setFormOpen(false)}>
                Cancel
              </Button>
            </div>
          </form>
        ) : null}
      </Panel>

      {ceremony === null ? null : (
        <Ceremony
          request={ceremony.request}
          onAuthorised={() => {
            const pending = ceremony.draft;
            setCeremony(null);
            action.mutate({ kind: 'emergency', draft: pending }, done('Emergency access granted. It is recorded and time-bound.'));
          }}
          onCancel={() => setCeremony(null)}
        />
      )}
    </div>
  );
}

function AccessRequestRow({
  request,
  mine,
  busy,
  onAct,
}: {
  readonly request: AccessRequest;
  readonly mine: boolean;
  readonly busy: boolean;
  readonly onAct: (kind: 'approve' | 'reject' | 'cancel' | 'revoke') => void;
}) {
  const state = request.bypassed ? `${request.state} (emergency)` : request.state;
  return (
    <li className="temporary-access__request">
      <div className="temporary-access__request-head">
        <span className="mono">{request.id}</span>
        <span className="temporary-access__request-state">
          {state}
          {request.state === 'open' ? ` · ${request.approvals}/${request.min_approvals} approvals` : ''}
        </span>
      </div>
      <dl className="temporary-access__request-meta">
        <div>
          <dt>Requester</dt>
          <dd title={request.requester}>{request.requester_name ?? request.requester}</dd>
        </div>
        <div>
          <dt>Capabilities</dt>
          <dd>{request.capabilities.join(', ')}</dd>
        </div>
        <div>
          <dt>Duration</dt>
          <dd>{duration(request.duration_seconds)}</dd>
        </div>
        <div>
          <dt>{request.state === 'granted' ? 'Ends' : request.state === 'open' ? 'Review closes' : 'Ended'}</dt>
          <dd>
            {request.state === 'open'
              ? when(request.review_expires_at)
              : when(request.expires_at ?? request.resolved_at)}
          </dd>
        </div>
        <div>
          <dt>Reason</dt>
          <dd>{request.reason}</dd>
        </div>
      </dl>
      {request.votes.length > 0 ? (
        <ul aria-label="Recorded votes">
          {request.votes.map((entry) => (
            <li key={entry.principal_id}>
              <span title={entry.principal_id}>{entry.principal_name ?? entry.principal_id}</span>: {entry.decision}
            </li>
          ))}
        </ul>
      ) : null}
      {request.state === 'open' || request.state === 'granted' ? (
        <div className="temporary-access__request-actions">
          {request.state === 'open' && !mine ? (
            <>
              <Button type="button" disabled={busy} onClick={() => onAct('approve')}>
                Approve
              </Button>
              <Button type="button" disabled={busy} onClick={() => onAct('reject')}>
                Reject
              </Button>
            </>
          ) : null}
          {request.state === 'open' && mine ? (
            <Button type="button" disabled={busy} onClick={() => onAct('cancel')}>
              Withdraw
            </Button>
          ) : null}
          {request.state === 'granted' ? (
            <Button type="button" variant="danger" disabled={busy} onClick={() => onAct('revoke')}>
              {mine ? 'End my access' : 'Revoke'}
            </Button>
          ) : null}
        </div>
      ) : null}
    </li>
  );
}
