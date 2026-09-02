import { useEffect, useState, type FormEvent } from 'react';
import { useParams, useSearchParams } from 'react-router';

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
import { useEnvironments } from '../api/settings.ts';

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
 * Audit renders one trail, org- or project-scoped (#572). The org page reads
 * the whole org trail; the project page reads one project's, and an environment
 * filter narrows it to one environment through the environment operations. The
 * two are the SAME component with a `scope` prop rather than a copy, so the
 * filter bar, paging, refusal handling and JSONL export are written once.
 */
export function Audit({ scope }: { readonly scope: 'org' | 'project' }) {
  const params = useParams();
  const org = params.org ?? '';
  const project = params.project ?? '';
  const [search, setSearch] = useSearchParams();
  const [draft, setDraft] = useState<AuditFilter>(emptyAuditFilter);
  const [applied, setApplied] = useState<AuditFilter>(emptyAuditFilter);
  const [selected, setSelected] = useState<AuditEvent | null>(null);

  // The environment picker is a filter on the project page, carried in the URL
  // (`?environment=<id>`) so a reload and a shared link resolve the same
  // environment, and so a sidebar hop to another project cannot leave a stale
  // environment id from the first behind. Its list load is INDEPENDENT of the
  // trail: a project-only holder of `audit-read` may not read the environment
  // list, and that must never blank the trail.
  const isProject = scope === 'project';
  const environments = useEnvironments(org, isProject ? project : '');
  const environment = isProject ? search.get('environment') ?? '' : '';
  const envItems = environments.data?.items ?? [];

  // A scope switch changes :org/:project or ?environment without remounting
  // this component: the element's key is fixed, so React reuses the instance.
  // Close the open detail on ANY scope change, tenant or environment, because
  // the selected event belongs to the trail that was showing — and the switch
  // can arrive by URL (a sidebar link clearing ?environment, the back button)
  // and so bypass the picker's own reset.
  useEffect(() => {
    setSelected(null);
  }, [org, project, environment]);

  // A tenant change also clears the filter, so one project's query does not
  // silently narrow another's. An environment switch within a project keeps
  // the filter: it is a refinement of the same project's view.
  useEffect(() => {
    setDraft(emptyAuditFilter);
    setApplied(emptyAuditFilter);
  }, [org, project]);

  const auditScope: AuditScope = !isProject
    ? { kind: 'org', org }
    : environment === ''
      ? { kind: 'project', org, project }
      : { kind: 'env', org, project, environment };

  const trail = useAuditTrail(auditScope, applied);

  const events = trail.data?.pages.flatMap((page) => page.items) ?? [];
  const scannedEnd = trail.hasNextPage !== true;

  function apply(next: AuditFilter) {
    setSelected(null);
    setDraft(next);
    setApplied(next);
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    apply(draft);
  }

  function set<K extends keyof AuditFilter>(key: K, value: string) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  function selectEnvironment(next: string) {
    // The detail is closed by the scope-change effect above, so it is not
    // cleared here: one source of truth for "scope changed, drop the detail".
    const params = new URLSearchParams(search);
    if (next === '') {
      params.delete('environment');
    } else {
      params.set('environment', next);
    }
    setSearch(params, { replace: true });
  }

  const lede = !isProject
    ? 'Every recorded event in this organisation, oldest first. Reading the trail is itself audited.'
    : environment === ''
      ? 'Every recorded event in this project, oldest first. Reading the trail is itself audited.'
      : 'Every recorded event in this environment, oldest first. Reading the trail is itself audited.';

  return (
    <div className="page page--chrome page--audit">
      <h1>Audit</h1>
      <p className="page__lede">{lede}</p>

      {isProject ? (
        <div className="audit__scope panel">
          <label className="field">
            <span className="field__label">Environment</span>
            {/* Never disabled: "All environments" is the only way back to the
                project trail, so it must stay reachable even when the list read
                fails while an environment is selected. When the list cannot be
                read, the active environment still gets an option (by id) so the
                current scope is honestly represented and can be switched away. */}
            <select value={environment} onChange={(event) => selectEnvironment(event.target.value)}>
              <option value="">All environments in this project</option>
              {environment !== '' && !envItems.some((env) => env.id === environment) ? (
                <option value={environment}>{environment}</option>
              ) : null}
              {envItems.map((env) => (
                <option key={env.id} value={env.id}>
                  {env.name}
                </option>
              ))}
            </select>
          </label>
          {environments.isError ? (
            <p className="audit__scope-note alert" role="alert">
              The environment list could not be read.
            </p>
          ) : null}
        </div>
      ) : null}

      <form className="audit__filter panel" onSubmit={onSubmit} aria-label="Filter the audit trail">
        <div className="audit__filter-grid">
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
            onClick={() => apply(emptyAuditFilter)}
          >
            Clear
          </button>
          {/* A plain same-origin GET: the browser streams the JSONL to disk under
              the session cookie, so the SPA never holds the trail in memory and no
              token rides the URL. */}
          <a className="btn" href={auditExportUrl(auditScope, applied)} download>
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
