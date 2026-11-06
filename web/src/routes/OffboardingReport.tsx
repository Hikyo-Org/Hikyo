import { useId, useState } from 'react';
import { generatePath, Link, useParams, useSearchParams } from 'react-router';

import { surfaceById } from '../app/navigation.ts';
import { useOffboardingReport, type PersonalOffboardingReport } from '../api/personal.ts';
import { commonRefusalText, statusText } from '../api/statusText.ts';
import { useWorkspaceContext, withRemote } from '../api/transport.tsx';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';

/**
 * What to rotate after someone leaves a project (personal-values ADR): the
 * shared keys the person is recorded as receiving, whether each changed since,
 * and the service accounts they created. Names only, never values, bounded by
 * the audit evidence window the report states. Readable on screen and printable.
 */
export function OffboardingReport() {
  const params = useParams();
  const org = params['org'] ?? '';
  const project = params['project'] ?? '';
  const [search, setSearch] = useSearchParams();
  const member = search.get('member') ?? '';
  const remote = useWorkspaceContext()?.remote ?? '';
  const [draft, setDraft] = useState(member);
  const report = useOffboardingReport({ org, project }, member);
  const membersPath = withRemote(`${generatePath(surfaceById('members').path, { org })}?project=${encodeURIComponent(project)}`, remote);

  return (
    <section className="page page--chrome offboarding-report" aria-labelledby="offboarding-title">
      <h1 id="offboarding-title">Offboarding report</h1>
      <p className="page__lede">
        What to rotate after someone leaves <strong>{project}</strong>: the shared keys they are recorded as receiving and
        the service accounts they created. The report names keys only, never values.
      </p>
      <form
        className="form offboarding-report__member no-print"
        onSubmit={(event) => {
          event.preventDefault();
          const next = draft.trim();
          setSearch(next === '' ? {} : { member: next });
        }}
      >
        <Input
          label="Member"
          hint="A username or account id. A person who already left can still be named."
          mono
          autoComplete="off"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
        />
        <div className="actions">
          <Button type="submit" variant="primary" disabled={draft.trim() === ''}>Show report</Button>
          {report.isSuccess ? <Button type="button" onClick={() => window.print()}>Print</Button> : null}
          <Link className="btn btn--quiet" to={membersPath}>Back to members</Link>
        </div>
      </form>
      {member === '' ? null : report.isPending ? <p role="status">Reading the audit evidence…</p> : null}
      {report.isError ? <Alert>{offboardingRefusalText(report.error)}</Alert> : null}
      {report.isSuccess ? <ReportBody report={report.data} /> : null}
    </section>
  );
}

function offboardingRefusalText(error: unknown): string {
  return statusText(error, {
    401: commonRefusalText.sessionEnded,
    403: 'The report needs manage-members and audit-read on this project.',
    404: 'No such member of this project.',
    429: commonRefusalText.requests,
  }, 'The report could not be read. Reload and try again.');
}

function when(at: string): string {
  return new Date(at).toLocaleString();
}

/** The report itself: plain tables and lists so it reads and prints the same. */
export function ReportBody({ report }: { report: PersonalOffboardingReport }) {
  const captionId = useId();
  return (
    <article className="offboarding-report__body" aria-labelledby="offboarding-report-member">
      <h2 id="offboarding-report-member">
        <span className="mono">{report.username}</span>
      </h2>
      <p>
        Evidence from {when(report.evidence_from)}. Generated {when(report.generated_at)}. Anything received before the
        evidence window, or never audited, does not appear.
      </p>
      <h3>Shared keys received</h3>
      {report.received.length === 0 ? (
        <p>No shared key is recorded as received in the evidence window.</p>
      ) : (
        // Focusable: on a phone the table scrolls and holds nothing else a keyboard can reach.
        <div className="table-scroll" role="region" aria-labelledby={captionId} tabIndex={0}>
          <table>
            <caption id={captionId} className="visually-hidden">Shared keys {report.username} is recorded as receiving, by environment.</caption>
            <thead>
              <tr>
                <th scope="col">Environment</th>
                <th scope="col">Key</th>
                <th scope="col">Last received</th>
                <th scope="col">Since then</th>
              </tr>
            </thead>
            <tbody>
              {report.received.map((row) => (
                <tr key={`${row.environment_id}/${row.key_name}`}>
                  <td>{row.environment_name}</td>
                  <th scope="row" className="mono">{row.key_name}</th>
                  <td>{when(row.last_received_at)}</td>
                  <td>
                    {row.changed_since
                      ? <Badge tone="ok">changed since</Badge>
                      : <Badge tone="danger">unchanged, rotate</Badge>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <h3>Service accounts they created</h3>
      {report.service_accounts.length === 0 ? (
        <p>None.</p>
      ) : (
        <ul>
          {report.service_accounts.map((account) => (
            <li key={account.id}>
              <span className="mono">{account.name}</span>, created {when(account.created_at)}
            </li>
          ))}
        </ul>
      )}
      <h3>What this report cannot show</h3>
      <ul>
        {report.limits.map((limit) => <li key={limit}>{limit}</li>)}
      </ul>
    </article>
  );
}
