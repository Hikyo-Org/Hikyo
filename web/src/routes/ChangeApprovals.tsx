import { useState } from 'react';
import { useParams } from 'react-router';

import {
  useApprovalPolicies,
  useApprovalRequests,
  useDeletePolicy,
  useResolveRequest,
  useSavePolicy,
  useVote,
  type ApprovalPolicy,
  type ApprovalRequest,
  type ApproverSpec,
  type PolicyDraft,
} from '../api/approvals.ts';
import { ApiError } from '../api/client.ts';
import { useEnvironments } from '../api/settings.ts';

/** A stored UTC timestamp rendered in the operator's locale. */
function when(value: string): string {
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString();
}

function refusal(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 401:
      case 403:
        return 'Re-authentication is required, or you may not perform this action. Sign in again and retry.';
      case 404:
        return 'Not found, or you may not see it.';
      case 409:
        return error.detail ?? 'This request can no longer be acted on (it was resolved, invalidated, or its quorum is not met).';
      case 400:
        return error.detail ?? 'The request is not valid.';
    }
  }
  return 'The action could not be completed.';
}

const emptyDraft: PolicyDraft = {
  environmentId: '',
  minApprovals: 1,
  allowSelfApproval: false,
  requestTtlSeconds: 86400,
  enabled: true,
  approvers: [],
  bypassers: [],
};

/** Renders the approver set as one id-or-group per line for the textarea. */
function approversToText(approvers: readonly ApproverSpec[]): string {
  return approvers
    .map((a) => (a.kind === 'scim_group' ? `group:${a.subject_id}:${a.binding_id ?? ''}` : a.subject_id))
    .join('\n');
}

function parseApprovers(text: string): ApproverSpec[] {
  const out: ApproverSpec[] = [];
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (line === '') {
      continue;
    }
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

export function ChangeApprovals() {
  const params = useParams();
  const org = params.org ?? '';
  const project = params.project ?? '';
  const ref = { org, project };

  const environments = useEnvironments(org, project);
  const policies = useApprovalPolicies(ref);
  const [selectedEnv, setSelectedEnv] = useState('');
  const requests = useApprovalRequests(ref, selectedEnv);

  const savePolicy = useSavePolicy(ref);
  const deletePolicy = useDeletePolicy(ref);
  const vote = useVote(ref, selectedEnv);
  const resolve = useResolveRequest(ref, selectedEnv);

  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState<PolicyDraft>(emptyDraft);
  const [approversText, setApproversText] = useState('');
  const [bypassersText, setBypassersText] = useState('');
  const [formOpen, setFormOpen] = useState(false);
  const [bypassReasons, setBypassReasons] = useState<Record<string, string>>({});
  const [actionError, setActionError] = useState<string | null>(null);

  const envItems = environments.data?.items ?? [];

  const openEditor = (policy: ApprovalPolicy | null) => {
    setActionError(null);
    if (policy === null) {
      setEditingId(null);
      setDraft(emptyDraft);
      setApproversText('');
      setBypassersText('');
    } else {
      setEditingId(policy.id);
      setDraft({
        environmentId: policy.environment_id,
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
        draft: {
          ...draft,
          approvers: parseApprovers(approversText),
          bypassers: parseList(bypassersText),
        },
      },
      {
        onSuccess: () => setFormOpen(false),
        onError: (error) => setActionError(refusal(error)),
      },
    );
  };

  const act = (fn: () => void) => {
    setActionError(null);
    fn();
  };

  return (
    <main className="change-approvals">
      <header>
        <h1>Change approvals</h1>
        <p>
          Policy-bound review and merge for changes in sensitive environments. Staging stays open;
          publishing a covered environment submits a request for review instead.
        </p>
      </header>

      <section aria-labelledby="ca-policies">
        <div className="change-approvals__section-head">
          <h2 id="ca-policies">Policies</h2>
          <button type="button" className="btn" onClick={() => openEditor(null)}>
            New policy
          </button>
        </div>
        {policies.isError ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{refusal(policies.error)}</span>
          </p>
        ) : null}
        {policies.data !== undefined && policies.data.items.length === 0 ? (
          <p>No approval policies. Changes in every environment publish directly.</p>
        ) : null}
        {policies.data !== undefined && policies.data.items.length > 0 ? (
          <table className="change-approvals__policies">
            <thead>
              <tr>
                <th>Environment</th>
                <th>Approvals</th>
                <th>Self-approval</th>
                <th>Expiry</th>
                <th>State</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {policies.data.items.map((policy) => {
                const env = envItems.find((e) => e.id === policy.environment_id);
                return (
                  <tr key={policy.id}>
                    <td>{policy.environment_id === '' ? 'All environments' : env?.name ?? policy.environment_id}</td>
                    <td>{policy.min_approvals}</td>
                    <td>{policy.allow_self_approval ? 'allowed' : 'not allowed'}</td>
                    <td>{Math.round(policy.request_ttl_seconds / 3600)}h</td>
                    <td>{policy.enabled ? 'enabled' : 'disabled'}</td>
                    <td>
                      <button type="button" className="btn" onClick={() => openEditor(policy)}>
                        Edit
                      </button>
                      <button
                        type="button"
                        className="btn btn--danger"
                        onClick={() =>
                          act(() =>
                            deletePolicy.mutate(policy.id, {
                              onError: (error) => setActionError(refusal(error)),
                            }),
                          )
                        }
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        ) : null}

        {formOpen ? (
          <form
            className="change-approvals__form panel"
            onSubmit={(event) => {
              event.preventDefault();
              submitPolicy();
            }}
          >
            <h3>{editingId === null ? 'New policy' : 'Edit policy'}</h3>
            <label htmlFor="ca-env">Environment</label>
            <select
              id="ca-env"
              value={draft.environmentId}
              onChange={(event) => setDraft({ ...draft, environmentId: event.target.value })}
            >
              <option value="">All environments in the project</option>
              {envItems.map((env) => (
                <option key={env.id} value={env.id}>
                  {env.name}
                </option>
              ))}
            </select>

            <label htmlFor="ca-min">Approvals required</label>
            <input
              id="ca-min"
              type="number"
              min={1}
              value={draft.minApprovals}
              onChange={(event) => setDraft({ ...draft, minApprovals: Number(event.target.value) })}
            />

            <label htmlFor="ca-ttl">Request expiry (hours)</label>
            <input
              id="ca-ttl"
              type="number"
              min={1}
              value={Math.round(draft.requestTtlSeconds / 3600)}
              onChange={(event) =>
                setDraft({ ...draft, requestTtlSeconds: Math.max(1, Number(event.target.value)) * 3600 })
              }
            />

            <label>
              <input
                type="checkbox"
                checked={draft.allowSelfApproval}
                onChange={(event) => setDraft({ ...draft, allowSelfApproval: event.target.checked })}
              />
              Allow the requester to approve their own change
            </label>
            <label>
              <input
                type="checkbox"
                checked={draft.enabled}
                onChange={(event) => setDraft({ ...draft, enabled: event.target.checked })}
              />
              Enabled
            </label>

            <label htmlFor="ca-approvers">Approvers (one per line: a principal id, or group:groupId:bindingId)</label>
            <textarea
              id="ca-approvers"
              value={approversText}
              onChange={(event) => setApproversText(event.target.value)}
              rows={3}
            />

            <label htmlFor="ca-bypassers">Emergency bypassers (principal ids, one per line)</label>
            <textarea
              id="ca-bypassers"
              value={bypassersText}
              onChange={(event) => setBypassersText(event.target.value)}
              rows={2}
            />

            <div className="change-approvals__form-actions">
              <button type="submit" className="btn btn--primary" disabled={savePolicy.isPending}>
                {savePolicy.isPending ? 'Saving…' : 'Save policy'}
              </button>
              <button type="button" className="btn" onClick={() => setFormOpen(false)}>
                Cancel
              </button>
            </div>
          </form>
        ) : null}
      </section>

      <section aria-labelledby="ca-requests">
        <h2 id="ca-requests">Review queue</h2>
        <label htmlFor="ca-review-env">Environment</label>
        <select
          id="ca-review-env"
          value={selectedEnv}
          onChange={(event) => setSelectedEnv(event.target.value)}
        >
          <option value="">Choose an environment</option>
          {envItems.map((env) => (
            <option key={env.id} value={env.id}>
              {env.name}
            </option>
          ))}
        </select>

        {actionError !== null ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{actionError}</span>
          </p>
        ) : null}

        {selectedEnv !== '' && requests.data !== undefined && requests.data.items.length === 0 ? (
          <p>No requests in this environment.</p>
        ) : null}

        {selectedEnv !== '' && requests.data !== undefined && requests.data.items.length > 0 ? (
          <ul className="change-approvals__requests">
            {requests.data.items.map((request) => (
              <ApprovalRequestRow
                key={request.id}
                request={request}
                bypassReason={bypassReasons[request.id] ?? ''}
                onBypassReasonChange={(value) =>
                  setBypassReasons((current) => ({ ...current, [request.id]: value }))
                }
                onVote={(decision) =>
                  act(() =>
                    vote.mutate(
                      { request: request.id, decision },
                      { onError: (error) => setActionError(refusal(error)) },
                    ),
                  )
                }
                onMerge={() =>
                  act(() =>
                    resolve.mutate(
                      { request: request.id },
                      { onError: (error) => setActionError(refusal(error)) },
                    ),
                  )
                }
                onBypass={() =>
                  act(() =>
                    resolve.mutate(
                      { request: request.id, bypassReason: bypassReasons[request.id] ?? '' },
                      { onError: (error) => setActionError(refusal(error)) },
                    ),
                  )
                }
                busy={vote.isPending || resolve.isPending}
              />
            ))}
          </ul>
        ) : null}
      </section>
    </main>
  );
}

function ApprovalRequestRow({
  request,
  bypassReason,
  onBypassReasonChange,
  onVote,
  onMerge,
  onBypass,
  busy,
}: {
  readonly request: ApprovalRequest;
  readonly bypassReason: string;
  readonly onBypassReasonChange: (value: string) => void;
  readonly onVote: (decision: 'approve' | 'reject') => void;
  readonly onMerge: () => void;
  readonly onBypass: () => void;
  readonly busy: boolean;
}) {
  const active = request.state === 'open' || request.state === 'approved';
  const quorumMet = request.approvals >= request.min_approvals;
  return (
    <li className="change-approvals__request">
      <div className="change-approvals__request-head">
        <span className="mono">{request.id}</span>
        <span className="change-approvals__request-state">
          {request.state} · {request.approvals}/{request.min_approvals} approvals
        </span>
      </div>
      <dl className="change-approvals__request-meta">
        <div>
          <dt>Requester</dt>
          <dd className="mono">{request.requester}</dd>
        </div>
        <div>
          <dt>Changes</dt>
          <dd>{request.change_count}</dd>
        </div>
        <div>
          <dt>Expires</dt>
          <dd>{when(request.expires_at)}</dd>
        </div>
        {request.purpose === '' ? null : (
          <div>
            <dt>Purpose</dt>
            <dd>{request.purpose}</dd>
          </div>
        )}
      </dl>
      {active ? (
        <div className="change-approvals__request-actions">
          <button type="button" className="btn" disabled={busy} onClick={() => onVote('approve')}>
            Approve
          </button>
          <button type="button" className="btn" disabled={busy} onClick={() => onVote('reject')}>
            Reject
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={busy || !quorumMet}
            onClick={onMerge}
            title={quorumMet ? '' : 'Waiting for the required approvals'}
          >
            Merge
          </button>
          <div className="change-approvals__bypass">
            <label htmlFor={`bypass-${request.id}`}>Bypass reason</label>
            <input
              id={`bypass-${request.id}`}
              type="text"
              value={bypassReason}
              onChange={(event) => onBypassReasonChange(event.target.value)}
            />
            <button
              type="button"
              className="btn btn--danger"
              disabled={busy || bypassReason.trim() === ''}
              onClick={onBypass}
            >
              Emergency bypass
            </button>
          </div>
        </div>
      ) : null}
    </li>
  );
}
