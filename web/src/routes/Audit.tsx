import { useState, type FormEvent } from 'react';
import { useParams } from 'react-router';

import { useProjectEnvironments } from '../api/adapters.ts';
import {
  AUDIT_OUTCOMES,
  auditExportUrl,
  emptyAuditFilter,
  useAuditTrail,
  type AuditEvent,
  type AuditFilter,
  type AuditScope,
} from '../api/audit.ts';
import { ApiError } from '../api/client.ts';

/** A stored UTC timestamp rendered in the operator's locale, or the raw value. */
function when(value: string): string {
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString();
}

function refusalText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 404 || error.status === 403) {
      return 'This trail is not available, or you may not read it.';
    }
    if (error.status === 400) {
      return error.detail ?? 'The filter is not valid.';
    }
    if (error.status === 429) {
      return 'The instance is busy. Try again shortly.';
    }
  }
  return 'The trail could not be read.';
}

/**
 * Audit serves two registry surfaces: the org trail (`audit`, #502) and the
 * project trail (`project-audit`, #572). The project route carries `:project`;
 * the environment is a filter on that page, because an environment trail is a
 * slice of its project's and a holder who can read one can pick which. The
 * scope is state alongside the filter so a picked environment survives the
 * same apply/clear discipline as every other control.
 */
export function Audit() {
  const params = useParams();
  const org = params.org ?? '';
  const project = params.project ?? '';
  const [draft, setDraft] = useState<AuditFilter>(emptyAuditFilter);
  const [applied, setApplied] = useState<AuditFilter>(emptyAuditFilter);
  const [environmentDraft, setEnvironmentDraft] = useState('');
  const [environment, setEnvironment] = useState('');
  const [selected, setSelected] = useState<AuditEvent | null>(null);
  const scope: AuditScope = project === '' ? { org } : { org, project, environment };
  const trail = useAuditTrail(scope, applied);
  const environments = useProjectEnvironments({ org, project });

  const events = trail.data?.pages.flatMap((page) => page.items) ?? [];
  const scannedEnd = trail.hasNextPage !== true;

  function apply(next: AuditFilter, nextEnvironment = environmentDraft) {
    setSelected(null);
    setDraft(next);
    setApplied(next);
    setEnvironmentDraft(nextEnvironment);
    setEnvironment(nextEnvironment);
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    apply(draft);
  }

  function set<K extends keyof AuditFilter>(key: K, value: string) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  return (
    <div className="page page--chrome page--audit">
      <h1>{project === '' ? 'Audit' : 'Project audit'}</h1>
      <p className="page__lede">
        {project === ''
          ? 'Every recorded event in this organisation, oldest first. Reading the trail is itself audited.'
          : 'Every recorded event in this project, oldest first, or one environment of it. Reading the trail is itself audited.'}
      </p>

      <form className="audit__filter panel" onSubmit={onSubmit} aria-label="Filter the audit trail">
        <div className="audit__filter-grid">
          {project === '' ? null : (
            <label className="field">
              <span className="field__label">Environment</span>
              <select
                value={environmentDraft}
                onChange={(event) => setEnvironmentDraft(event.target.value)}
              >
                <option value="">Whole project</option>
                {(environments.data?.items ?? []).map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="field">
            <span className="field__label">From</span>
            <input
              type="datetime-local"
              value={draft.from}
              onChange={(event) => set('from', event.target.value)}
            />
          </label>
          <label className="field">
            <span className="field__label">To</span>
            <input
              type="datetime-local"
              value={draft.to}
              onChange={(event) => set('to', event.target.value)}
            />
          </label>
          <label className="field">
            <span className="field__label">Principal</span>
            <input
              value={draft.actor}
              onChange={(event) => set('actor', event.target.value)}
              placeholder="usr_…"
            />
          </label>
          <label className="field">
            <span className="field__label">Operation</span>
            <input
              value={draft.operation}
              onChange={(event) => set('operation', event.target.value)}
              placeholder="value.set"
            />
          </label>
          <label className="field">
            <span className="field__label">Outcome</span>
            <select value={draft.outcome} onChange={(event) => set('outcome', event.target.value)}>
              <option value="">Any</option>
              {AUDIT_OUTCOMES.map((outcome) => (
                <option key={outcome} value={outcome}>
                  {outcome}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="field__label">Resource type</span>
            <input
              value={draft.objectType}
              onChange={(event) => set('objectType', event.target.value)}
              placeholder="key"
            />
          </label>
          <label className="field">
            <span className="field__label">Resource id</span>
            <input
              value={draft.objectId}
              onChange={(event) => set('objectId', event.target.value)}
            />
          </label>
          <label className="field">
            <span className="field__label">Correlation id</span>
            <input
              value={draft.correlationId}
              onChange={(event) => set('correlationId', event.target.value)}
            />
          </label>
        </div>
        <div className="audit__filter-actions">
          <button type="submit" className="btn btn--primary">
            Apply filter
          </button>
          <button
            type="button"
            className="btn btn--quiet"
            onClick={() => apply(emptyAuditFilter, '')}
          >
            Clear
          </button>
          {/* A plain same-origin GET: the browser streams the JSONL to disk under
              the session cookie, so the SPA never holds the trail in memory and no
              token rides the URL. */}
          <a className="btn" href={auditExportUrl(scope, applied)} download>
            Export JSONL
          </a>
        </div>
      </form>

      <div className="audit__panes">
        <section className="audit__list-pane" aria-label="Audit events">
          {trail.isError ? (
            <p className="audit__empty alert" role="alert">
              {refusalText(trail.error)}
            </p>
          ) : events.length === 0 && trail.isSuccess ? (
            <p className="audit__empty" role="status">
              {scannedEnd
                ? 'No events match this filter.'
                : 'No matches in the pages scanned so far — keep scanning for more.'}
            </p>
          ) : (
            <ol className="audit__list" aria-label="Audit events, oldest first">
              {events.map((event) => (
                <li key={String(event.seq)}>
                  <button
                    type="button"
                    className="btn audit__row"
                    aria-pressed={selected?.seq === event.seq}
                    onClick={() => setSelected(event)}
                  >
                    <span className="audit__row-op">{event.type}</span>
                    <span className={`chip audit__outcome audit__outcome--${event.outcome}`}>
                      {event.outcome}
                    </span>
                    <span className="audit__row-actor">{event.actor_id ?? event.actor_class}</span>
                    <span className="audit__row-when">{when(event.recorded_at)}</span>
                  </button>
                </li>
              ))}
            </ol>
          )}

          {!scannedEnd && !trail.isError ? (
            <button
              type="button"
              className="btn audit__more"
              onClick={() => void trail.fetchNextPage()}
              disabled={trail.isFetchingNextPage}
            >
              {trail.isFetchingNextPage ? 'Scanning…' : 'Load more'}
            </button>
          ) : events.length > 0 ? (
            <p className="audit__end" role="status">
              End of the trail.
            </p>
          ) : null}
        </section>

        {selected !== null ? (
          <aside className="audit__detail panel" aria-label="Event detail">
            <div className="audit__detail-head">
              <h2>{selected.type}</h2>
              <button
                type="button"
                className="btn btn--quiet audit__detail-close"
                onClick={() => setSelected(null)}
              >
                Close
              </button>
            </div>
            <dl className="audit__facts">
              <AuditFact label="Sequence" value={String(selected.seq)} />
              <AuditFact label="Event id" value={selected.id} />
              <AuditFact label="Outcome" value={selected.outcome} />
              <AuditFact label="Recorded" value={when(selected.recorded_at)} />
              <AuditFact label="Occurred" value={when(selected.occurred_at)} />
              <AuditFact label="Principal" value={selected.actor_id ?? '—'} />
              <AuditFact label="Actor class" value={selected.actor_class} />
              <AuditFact label="Scope" value={selected.scope_class} />
              {selected.org_id !== undefined ? <AuditFact label="Org" value={selected.org_id} /> : null}
              {selected.project_id !== undefined ? (
                <AuditFact label="Project" value={selected.project_id} />
              ) : null}
              {selected.env_id !== undefined ? <AuditFact label="Environment" value={selected.env_id} /> : null}
              {selected.object_type !== undefined ? (
                <AuditFact label="Resource type" value={selected.object_type} />
              ) : null}
              {selected.object_id !== undefined ? (
                <AuditFact label="Resource id" value={selected.object_id} />
              ) : null}
              {selected.correlation_id !== undefined ? (
                <div className="audit__fact">
                  <dt>Correlation</dt>
                  <dd>
                    <span className="audit__fact-value">{selected.correlation_id}</span>{' '}
                    {/* Following the correlation id is how INTENT and its OUTCOME are
                        read together: it filters to exactly the events of one act. */}
                    <button
                      type="button"
                      className="btn btn--quiet audit__correlate"
                      onClick={() =>
                        apply({ ...emptyAuditFilter, correlationId: selected.correlation_id ?? '' })
                      }
                    >
                      Show correlated events
                    </button>
                  </dd>
                </div>
              ) : null}
            </dl>
            <h3 className="audit__payload-title">Payload</h3>
            <pre className="audit__payload">{JSON.stringify(selected.payload, null, 2)}</pre>
          </aside>
        ) : null}
      </div>
    </div>
  );
}

function AuditFact({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="audit__fact">
      <dt>{label}</dt>
      <dd>
        <span className="audit__fact-value">{value}</span>
      </dd>
    </div>
  );
}
