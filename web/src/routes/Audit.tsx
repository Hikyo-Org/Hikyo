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
import { useScopeNames } from '../api/scopeNames.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Button } from '../ui/Button.tsx';
import { JumpIndex, Panel } from './Sections.tsx';

/** The glyph before an outcome word, so the state is never colour-only. */
function outcomeGlyph(outcome: AuditEvent['outcome']): string | null {
  switch (outcome) {
    case 'failure':
      return '✕ ';
    case 'denied':
      return '⊘ ';
    case 'disconnected':
      return '⇅ ';
    default:
      return null;
  }
}

function Outcome({ outcome }: { readonly outcome: AuditEvent['outcome'] }) {
  const glyph = outcomeGlyph(outcome);
  return (
    <span className={`chip audit__outcome audit__outcome--${outcome}`}>
      {glyph === null ? null : <span aria-hidden="true">{glyph}</span>}
      {outcome}
    </span>
  );
}

/** A stored UTC timestamp rendered in the operator's locale, or the raw value. */
function when(value: string): string {
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString();
}

function refusalText(error: unknown, scope: AuditScope): string {
  if (error instanceof ApiError) {
    if (error.status === 404 || error.status === 403) {
      // `audit-read` is its own grant (audit-model ADR): no template seeds
      // it, the organisation `admin` template included. Name the exact grant
      // and where it is made, so the holder of everything else is not left
      // guessing which capability this is.
      const where =
        scope.project === undefined
          ? 'this organisation'
          : scope.environment === ''
            ? 'this project'
            : 'this environment';
      return `This trail is not available, or you may not read it. Reading it needs the audit-read grant on ${where} with a second factor in this session. No role template includes audit-read; a member manager grants it under Members.`;
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
  // Keyed by scope so a client-side move between organisations or projects
  // remounts the page: a picked environment or an applied filter belongs to
  // the scope it was picked in and must not follow the route to another.
  return <AuditTrail key={`${org}/${project}`} org={org} project={project} />;
}

/** The text filter fields — every field but the `outcomes` set, which has its own toggle. */
type AuditTextField = Exclude<keyof AuditFilter, 'outcomes'>;

function AuditTrail({ org, project }: { readonly org: string; readonly project: string }) {
  const auth = useAuth();
  const selfId = auth.identity?.principal.id ?? '';
  const [draft, setDraft] = useState<AuditFilter>(emptyAuditFilter);
  const [applied, setApplied] = useState<AuditFilter>(emptyAuditFilter);
  const [environmentDraft, setEnvironmentDraft] = useState('');
  const [environment, setEnvironment] = useState('');
  const [selected, setSelected] = useState<AuditEvent | null>(null);
  const scope: AuditScope = project === '' ? { org } : { org, project, environment };
  const trail = useAuditTrail(scope, applied);
  const environments = useProjectEnvironments({ org, project }, project !== '');

  const events = trail.data?.pages.flatMap((page) => page.items) ?? [];
  const scannedEnd = trail.hasNextPage !== true;
  const emptyResult = trail.isSuccess && events.length === 0;
  const clearButton = (
    <Button type="button" variant="quiet" onClick={() => apply(emptyAuditFilter, '')}>
      Clear
    </Button>
  );

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

  function set(key: AuditTextField, value: string) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  /** Toggle one outcome in the set; the set filter matches any listed outcome. */
  function toggleOutcome(outcome: string, checked: boolean) {
    setDraft((current) => ({
      ...current,
      outcomes: checked
        ? [...current.outcomes, outcome]
        : current.outcomes.filter((o) => o !== outcome),
    }));
  }

  return (
    <div className="page page--chrome page--audit">
      <h1>{project === '' ? 'Audit' : 'Project audit'}</h1>
      <p className="page__lede">
        {project === ''
          ? 'Every recorded event in this organisation, oldest first. Reading the trail is itself audited.'
          : 'Every recorded event in this project, oldest first, or one environment of it. Reading the trail is itself audited.'}
      </p>

      <JumpIndex
        sections={[
          { id: 'audit-filter', label: 'Filter' },
          { id: 'audit-events', label: 'Events' },
          { id: 'audit-detail', label: 'Detail' },
        ]}
      />

      <Panel id="audit-filter" title="Filter">
      <form className="audit__filter" onSubmit={onSubmit} aria-label="Filter the audit trail">
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
            {/* An exact principal id. "Self" fills it with the current session's
                own id — no server lookup, just the id whoami already returned. */}
            <div className="field__inline">
              <input
                value={draft.actor}
                onChange={(event) => set('actor', event.target.value)}
                placeholder="usr_…"
              />
              {selfId === '' ? null : (
                <Button
                  type="button"
                  variant="quiet"
                  onClick={() => set('actor', selfId)}
                  disabled={draft.actor === selfId}
                >
                  Self
                </Button>
              )}
            </div>
          </label>
          <label className="field">
            <span className="field__label">Principal name</span>
            {/* A glob over the acting principal's display name; the server
                resolves ids to names on rows you may already read. */}
            <input
              value={draft.actorName}
              onChange={(event) => set('actorName', event.target.value)}
              placeholder="Ada* — name, * wildcards"
            />
          </label>
          <label className="field">
            <span className="field__label">Operation</span>
            <input
              value={draft.operation}
              onChange={(event) => set('operation', event.target.value)}
              placeholder="value.* — * wildcards"
            />
          </label>
          <fieldset className="field audit__outcomes">
            <legend className="field__label">Outcomes</legend>
            {/* A set: check several to match any of them; none checked means any
                outcome. Checkboxes, not a multi-select — three values read
                cleaner and stay keyboard-reachable. */}
            {AUDIT_OUTCOMES.map((outcome) => (
              <label key={outcome} className="audit__outcome-choice">
                <input
                  type="checkbox"
                  checked={draft.outcomes.includes(outcome)}
                  onChange={(event) => toggleOutcome(outcome, event.target.checked)}
                />
                {outcome}
              </label>
            ))}
          </fieldset>
          <label className="field">
            <span className="field__label">Resource type</span>
            <input
              value={draft.objectType}
              onChange={(event) => set('objectType', event.target.value)}
              placeholder="key — * wildcards"
            />
          </label>
          <label className="field">
            <span className="field__label">Resource id</span>
            <input
              value={draft.objectId}
              onChange={(event) => set('objectId', event.target.value)}
              placeholder="* wildcards"
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
          <Button type="submit" variant="primary">
            Apply filter
          </Button>
          {/* Clear lives in ONE place: here while there are events, inside the
              empty state when the filter matched nothing. */}
          {emptyResult ? null : clearButton}
          {/* A plain same-origin GET: the browser streams the JSONL to disk under
              the session cookie, so the SPA never holds the trail in memory and no
              token rides the URL. */}
          <a className="btn" href={auditExportUrl(scope, applied)} download>
            Export JSONL
          </a>
        </div>
      </form>
      </Panel>

      <div className="audit__panes">
        <Panel id="audit-events" title="Events">
          {trail.isPending ? <p role="status">Loading events…</p> : null}
          {trail.isError ? (
            <p className="audit__empty alert" role="alert">
              {refusalText(trail.error, scope)}
            </p>
          ) : emptyResult ? (
            <div className="audit__empty" role="status">
              <p>
                {scannedEnd
                  ? 'No events match this filter.'
                  : 'No matches in the pages scanned so far. Keep scanning for more.'}
              </p>
              {clearButton}
            </div>
          ) : (
            <ol className="audit__list" aria-label="Audit events, oldest first">
              {events.map((event) => (
                <li key={String(event.seq)}>
                  <button
                    type="button"
                    className="audit__row"
                    aria-pressed={selected?.seq === event.seq}
                    onClick={() => setSelected(event)}
                  >
                    <span className="audit__row-op mono">{event.type}</span>
                    <Outcome outcome={event.outcome} />
                    <span className="audit__row-actor">{event.actor_name ?? event.actor_id ?? event.actor_class}</span>
                    <span className="audit__row-when">{when(event.recorded_at)}</span>
                  </button>
                </li>
              ))}
            </ol>
          )}

          {!scannedEnd && !trail.isError ? (
            <Button
              type="button"
              className="audit__more"
              onClick={() => void trail.fetchNextPage()}
              disabled={trail.isFetchingNextPage}
            >
              {trail.isFetchingNextPage ? 'Scanning…' : 'Load more'}
            </Button>
          ) : events.length > 0 ? (
            <p className="audit__end" role="status">
              End of the trail.
            </p>
          ) : null}
        </Panel>

        {/* The detail stays an aside (a complementary landmark beside the
            list) rather than a Panel; the id and tabIndex give the jump index
            the same focus landing a Panel has. */}
        {selected === null ? (
          <aside className="audit__detail card panel" id="audit-detail" tabIndex={-1} aria-label="Event detail">
            <h2>Event detail</h2>
            <p role="status">Select an event to see its detail.</p>
          </aside>
        ) : (
          <aside className="audit__detail card panel" id="audit-detail" tabIndex={-1} aria-label="Event detail">
            <div className="audit__detail-head">
              <h2 className="mono">{selected.type}</h2>
              <Button
                type="button"
                variant="quiet"
                className="audit__detail-close"
                onClick={() => setSelected(null)}
              >
                Close
              </Button>
            </div>
            <dl className="audit__facts">
              <AuditFact label="Sequence" value={String(selected.seq)} />
              <AuditFact label="Event id" value={selected.id} />
              <div className="audit__fact">
                <dt>Outcome</dt>
                <dd>
                  <Outcome outcome={selected.outcome} />
                </dd>
              </div>
              <AuditFact label="Recorded" value={when(selected.recorded_at)} />
              <AuditFact label="Occurred" value={when(selected.occurred_at)} />
              <AuditFact label="Principal" value={selected.actor_name ?? selected.actor_id ?? 'absent'} />
              {selected.actor_name !== undefined ? <AuditFact label="Principal ID" value={selected.actor_id ?? 'absent'} /> : null}
              <AuditFact label="Actor class" value={selected.actor_class} />
              <AuditFact label="Scope" value={selected.scope_class} />
              <AuditScopeFacts event={selected} />
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
                    <Button
                      type="button"
                      variant="quiet"
                      className="audit__correlate"
                      onClick={() =>
                        apply({ ...emptyAuditFilter, correlationId: selected.correlation_id ?? '' })
                      }
                    >
                      Show correlated events
                    </Button>
                  </dd>
                </div>
              ) : null}
            </dl>
            <h3 className="audit__payload-title">Payload</h3>
            <pre className="audit__payload">{JSON.stringify(selected.payload, null, 2)}</pre>
          </aside>
        )}
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

function AuditScopeFacts({ event }: { readonly event: AuditEvent }) {
  const names = useScopeNames(event.org_id ?? '', event.project_id ?? '', event.env_id ?? '');
  return <>
    {event.org_id !== undefined ? <AuditFact label="Org" value={names.org} /> : null}
    {event.project_id !== undefined ? <AuditFact label="Project" value={names.project} /> : null}
    {event.env_id !== undefined ? <AuditFact label="Environment" value={names.environment} /> : null}
  </>;
}
