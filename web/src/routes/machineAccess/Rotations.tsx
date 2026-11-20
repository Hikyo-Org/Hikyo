import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';

import { useProjectKeys } from '../../api/adapters.ts';
import type { DynamicProvider } from '../../api/dynamic.ts';
import {
  haltCause,
  haltReasonField,
  MAX_RUN_PAGES,
  owedBadgeText,
  owedRunText,
  owedSummaryText,
  owesDrop,
  owesDropSummary,
  restoreHoldText,
  VERSIONS_CHANGED_CODE,
  rotationRefusalText,
  useRotationRuns,
  useRotations,
  type EnvironmentRef,
  type Rotation,
  type RotationRun,
} from '../../api/rotations.ts';
import { duration, plural } from '../../app/duration.ts';
import { formatWhen } from '../../lib/formatWhen.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Badge, type BadgeTone } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { Breakable, NamedId } from '../../ui/NamedId.tsx';
import { Select } from '../../ui/Select.tsx';
import { NoEnvironments, type EnvironmentsRead } from './EnvironmentsRead.tsx';

type EnvOption = { readonly id: string; readonly name: string };

const STATE_TONE: Record<Rotation['state'], BadgeTone> = {
  active: 'ok',
  paused: 'changed',
  halted: 'danger',
  disabled: 'neutral',
};

const PHASE_TONE: Record<RotationRun['phase'], BadgeTone> = {
  minting: 'neutral',
  minted: 'neutral',
  staged: 'neutral',
  published: 'neutral',
  overlap: 'neutral',
  revoking: 'neutral',
  revoke_refused: 'danger',
  done: 'ok',
  quarantine: 'danger',
  failed: 'danger',
};

/** span names a schedule length from the wire's int64 seconds (bounded to 90 days by the server). */
function span(seconds: bigint): string {
  return duration(Number(seconds));
}

/** at renders an optional instant, or the words that say why there is none. */
function at(value: string | null | undefined, none: string): string {
  return value === undefined || value === null ? none : formatWhen(value);
}

/** CodeText is a sentence whose snake_case words (error codes, halt reasons) are Breakable. */
function CodeText({ text }: { text: string }) {
  return (
    <>
      {text.split(/([a-z]+(?:_[a-z]+)+)/).map((part, index) =>
        index % 2 === 1 ? (
          <span key={`${String(index)}-${part}`} className="rotations__code">
            <Breakable text={part} />
          </span>
        ) : (
          part
        ),
      )}
    </>
  );
}

/**
 * Facts is a labelled list of one row's fields. Every field is its own labelled
 * line inside a wrapping cell, so no column is hidden or scrolled out of sight
 * at any width, and the label survives where a table header does not (a phone
 * lays the table out as one card per row, without its header).
 */
function Facts({ children }: { children: ReactNode }) {
  return <dl className="kv">{children}</dl>;
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="kv__pair">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/**
 * RotationsPanel is the status surface for scheduled credential rotation
 * (#148) on `machine-access`: an environment's rotations and the run timeline
 * of one of them. Status only. Creating, pausing, resuming, running and
 * deleting a rotation, and lifting a restore hold, are `hikyo rotation` verbs:
 * the browser names the command and never takes the act.
 *
 * Nothing here holds a secret: a rotation names its keys and provider by id and
 * a run carries a fixed-word error code, never a handle or a provider detail.
 */
export function RotationsPanel({
  org,
  project,
  environments,
  environmentsRead,
  providers,
}: {
  org: string;
  project: string;
  environments: readonly EnvOption[];
  environmentsRead: EnvironmentsRead;
  providers: readonly DynamicProvider[];
}) {
  const [environmentId, setEnvironmentId] = useState(environments[0]?.id ?? '');
  const current = environments.find((e) => e.id === environmentId) ?? environments[0];

  return (
    <section className="card machine__card" aria-labelledby="rotations-title">
      <h2 id="rotations-title">Rotations</h2>
      <p className="machine__lede">
        Scheduled rotation of one PostgreSQL password and role name, minted, published and later
        dropped by the server on its own. This page shows status only: it never shows a credential,
        and every change to a rotation, including lifting a restore hold, is a{' '}
        <code>hikyo rotation</code> command.
      </p>
      {current === undefined ? (
        <NoEnvironments read={environmentsRead} what="rotations" />
      ) : (
        <>
          <Select
            label="Environment"
            value={current.id}
            onChange={(event) => setEnvironmentId(event.target.value)}
          >
            {environments.map((e) => (
              <option key={e.id} value={e.id}>
                {e.name}
              </option>
            ))}
          </Select>
          <RotationList key={current.id} env={{ org, project, environment: current.id }} providers={providers} />
        </>
      )}
    </section>
  );
}

function RotationList({
  env,
  providers,
}: {
  env: EnvironmentRef;
  providers: readonly DynamicProvider[];
}) {
  const [includeDisabled, setIncludeDisabled] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const query = useRotations(env, includeDisabled);
  const keys = useProjectKeys({ org: env.org, project: env.project });
  const rotations = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data]);
  const keyName = (id: string) => keys.data?.items.find((key) => key.id === id)?.name;
  const provider = (id: string) => providers.find((p) => p.id === id);
  const held = rotations.filter((rotation) => rotation.restore_hold && rotation.state !== 'disabled');
  const halted = rotations.filter((rotation) => rotation.state === 'halted');
  const chosen = rotations.find((rotation) => rotation.id === selected);

  return (
    <>
      <Checkbox
        label="Include all deleted rotations"
        checked={includeDisabled}
        onChange={(event) => {
          setIncludeDisabled(event.target.checked);
          setSelected(null);
        }}
      />
      {query.isPending ? <p role="status">Loading rotations…</p> : null}
      {query.isError ? <Alert>{rotationRefusalText(query.error, 'rotations')}</Alert> : null}

      {held.map((rotation) => (
        <Alert key={rotation.id}>{restoreHoldText(rotation)}</Alert>
      ))}
      {halted.length > 0 ? (
        <Alert tone="warn">
          {halted.length === 1 ? 'One rotation is' : `${String(halted.length)} rotations are`} halted:
          the server stopped {halted.length === 1 ? 'it' : 'them'} and starts no run until a person
          resumes {halted.length === 1 ? 'it' : 'them'} with <code>hikyo rotation resume</code>. A
          halted rotation does not extend the credential already delivered.
          {halted.map((rotation) => (
            <span key={`cause-${rotation.id}`}>
              {' '}
              Rotation {rotation.id} is <CodeText text={haltCause(rotation)} />.
            </span>
          ))}
          {halted
            .filter((rotation) => rotation.restore_hold)
            .map((rotation) => (
              <span key={rotation.id}>
                {' '}
                Rotation {rotation.id} is also held after a restore: lift the hold with{' '}
                <code>hikyo rotation release-hold {rotation.id}</code> first, then resume it.
              </span>
            ))}
        </Alert>
      ) : null}

      {query.isSuccess && rotations.length > 0 ? (
        <div className="table-scroll">
          <table className="values__table machine__table rotations__table table--cards">
            <caption className="visually-hidden">
              The environment&apos;s credential rotations. No credential is listed.
            </caption>
            <thead>
              <tr>
                <th scope="col">Rotation</th>
                <th scope="col">State and runs</th>
                <th scope="col">Schedule and record</th>
              </tr>
            </thead>
            <tbody>
              {rotations.map((rotation) => {
                const minter = provider(rotation.provider_id);
                const isHeld = rotation.restore_hold && rotation.state !== 'disabled';
                const label = keyName(rotation.key_id) ?? rotation.id;
                const open = selected === rotation.id;
                // A deleted rotation can still owe a drop: the role stays live, so the CLI warns for it too.
                const owed = owesDropSummary(rotation);
                return (
                  <tr key={rotation.id} data-rotation={rotation.id}>
                    <th scope="row">
                      <Facts>
                        <Fact label="Rotation">
                          <span className="mono rotations__id" title={rotation.id}>{rotation.id}</span>
                        </Fact>
                        <Fact label="Password key">
                          <NamedId breakable id={rotation.key_id} name={keyName(rotation.key_id)} />
                        </Fact>
                        <Fact label="Role name key">
                          <NamedId breakable id={rotation.username_key_id} name={keyName(rotation.username_key_id)} />
                        </Fact>
                        <Fact label="Provider">
                          {minter === undefined ? (
                            <NamedId id={rotation.provider_id} name={undefined} />
                          ) : (
                            <>
                              {minter.kind} <span className="mono machine__origin" title={minter.origin}>{minter.origin}</span>
                              <br />
                              <span className="mono rotations__id">{rotation.provider_id}</span>
                            </>
                          )}
                        </Fact>
                      </Facts>
                    </th>
                    <td>
                      <Badge tone={STATE_TONE[rotation.state]}>{rotation.state === 'disabled' ? 'deleted' : rotation.state}</Badge>
                      {isHeld ? (
                        <>
                          {' '}
                          <Badge tone="danger">Restore hold</Badge>
                        </>
                      ) : null}
                      {owed ? (
                        <>
                          {' '}
                          {/* A restore may have put the role back: the badge must not say to drop it. */}
                          <Badge tone="danger">{owedBadgeText(rotation)}</Badge>
                        </>
                      ) : null}
                      {rotation.state === 'halted' || owed ? (
                        <Facts>
                          {rotation.state === 'halted' ? (
                            <Fact label="Halt reason">
                              <span className="rotations__note"><CodeText text={haltReasonField(rotation)} /></span>
                            </Fact>
                          ) : null}
                          {owed ? (
                            <Fact label="Attention">
                              <span className="rotations__note"><CodeText text={owedSummaryText(rotation)} /></span>
                            </Fact>
                          ) : null}
                        </Facts>
                      ) : null}
                      <div className="rotations__runs">
                        <Button
                          type="button"
                          variant="quiet"
                          readOnlySafe
                          aria-expanded={open}
                          aria-controls={open ? `runs-${rotation.id}` : undefined}
                          aria-label={`${open ? 'Hide' : 'Show'} runs of ${label}`}
                          onClick={() => setSelected(open ? null : rotation.id)}
                        >
                          {open ? 'Hide runs' : 'Show runs'}
                        </Button>
                      </div>
                    </td>
                    <td>
                      <Facts>
                        <Fact label="Schedule">
                          Every {span(rotation.interval_seconds)}, {span(rotation.revocation_delay_seconds)} overlap
                        </Fact>
                        <Fact label="Next run">
                          {isHeld
                            ? 'Held after a restore: no run starts'
                            : rotation.state === 'active'
                              ? at(rotation.next_run_at, 'Not scheduled')
                              : 'Not scheduled'}
                        </Fact>
                        <Fact label="Delivered credential expires">
                          {at(rotation.credential_expires_at, 'No run has published yet')}
                        </Fact>
                        <Fact label="Authority principal">
                          <span className="mono rotations__id">{rotation.authority_principal_id}</span>
                        </Fact>
                        <Fact label="Created by">
                          <span className="mono rotations__id">{rotation.created_by}</span>
                        </Fact>
                        <Fact label="Last run">
                          {rotation.last_run === undefined ? (
                            'No run yet'
                          ) : (
                            <>
                              <span className="mono rotations__id">{rotation.last_run.id}</span>{' '}
                              <span className="rotations__code"><Breakable text={rotation.last_run.phase} /></span>
                              {rotation.last_run.error_code === undefined || rotation.last_run.error_code === null ? (
                                ''
                              ) : (
                                <>
                                  {' '}
                                  (<span className="mono rotations__code"><Breakable text={rotation.last_run.error_code} /></span>)
                                </>
                              )}
                              , {formatWhen(rotation.last_run.phase_at)}
                            </>
                          )}
                        </Fact>
                        <Fact label="Generation">{String(rotation.generation)}</Fact>
                        <Fact label="Created">{formatWhen(rotation.created_at)}</Fact>
                        <Fact label="Updated">{formatWhen(rotation.updated_at)}</Fact>
                      </Facts>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}
      {query.isSuccess && rotations.length === 0 ? (
        <p role="status">
          {includeDisabled
            ? 'No rotations in this environment, deleted ones included.'
            : 'No rotations in this environment yet. Create one with hikyo rotation create.'}
        </p>
      ) : null}
      {/* A refetch that failed leaves the old pages and their cursor: a page added after them
          would clear the error and show a stale list as current. A failed next page can be retried. */}
      {query.hasNextPage && !(query.isError && !query.isFetchNextPageError) ? (
        <div className="actions">
          <Button type="button" readOnlySafe disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
            Load more rotations
          </Button>
        </div>
      ) : null}

      {chosen === undefined ? null : (
        <RunTimeline key={chosen.id} env={env} rotation={chosen} name={keyName(chosen.key_id) ?? chosen.id} />
      )}
    </>
  );
}

/**
 * RunTimeline is one rotation's runs, newest first. It reads back through the
 * history like `hikyo rotation show` (up to the same bound), because the runs
 * that matter most, those still owing a provider role a drop, can be old.
 */
function RunTimeline({ env, rotation, name }: { env: EnvironmentRef; rotation: Rotation; name: string }) {
  const query = useRotationRuns(env, rotation.id);
  const { hasNextPage, isFetching, isError, fetchNextPage } = query;
  const pages = query.data?.pages.length ?? 0;
  const runs = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data]);
  // Any error stops the reading: a failed later page leaves older runs unread, and a failed
  // refetch of a reopened timeline leaves `hasNextPage` true from the old pages, so reading
  // on would continue from a stale cursor and clear the error with a newer run still missing.
  const reading = hasNextPage && pages < MAX_RUN_PAGES && !isError;
  // Ask for the next page only while nothing is fetching, of any kind. A reopened
  // timeline refetches its cached first pages, and `fetchNextPage` would cancel that
  // refetch and read on from a stale cursor, hiding a newer run for good. `pages` is a
  // dependency because a page that lands between two renders never shows `isFetching`
  // as true, so without it the next page is not asked for. Not `cancelRefetch: false`:
  // a refetch keeps the page count, so nothing would wake this effect again.
  useEffect(() => {
    if (reading && !isFetching) void fetchNextPage();
  }, [reading, isFetching, pages, fetchNextPage]);
  const owed = runs.filter(owesDrop);
  // Opening the timeline moves focus to its heading, so the keyboard and a
  // screen reader land on what the button revealed instead of staying above it.
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  // Older runs remain unread (the page bound, or a later page that failed): an
  // older run can still owe a drop, so the gap is said, as the CLI says it.
  const truncated = hasNextPage && !reading;

  return (
    <section className="machine__sub" id={`runs-${rotation.id}`} aria-labelledby={`runs-title-${rotation.id}`}>
      <h3 id={`runs-title-${rotation.id}`} ref={heading} tabIndex={-1}>Runs of {name}</h3>
      {query.isPending ? <p role="status">Loading runs…</p> : null}
      {reading && !query.isPending ? <p role="status">Loading more runs…</p> : null}
      {query.isError ? <Alert>{rotationRefusalText(query.error, 'runs')}</Alert> : null}
      {truncated ? (
        <Alert tone="warn">
          Only the newest {plural(runs.length, 'run')} {runs.length === 1 ? 'is' : 'are'} shown. An older run can still owe a
          provider role a drop: page through <code>hikyo rotation runs {rotation.id}</code> to
          check.
        </Alert>
      ) : null}
      {owed.map((run) => (
        <Alert key={run.id}><CodeText text={owedRunText(run)} /></Alert>
      ))}
      {query.isSuccess && runs.length === 0 ? (
        <p role="status">This rotation has not run yet.</p>
      ) : null}
      {runs.length > 0 ? (
        <div className="table-scroll">
          <table className="values__table machine__table rotations__table table--cards">
            <caption className="visually-hidden">
              Runs of this rotation, newest first. Error codes are fixed words, never provider detail.
            </caption>
            <thead>
              <tr>
                <th scope="col">Run</th>
                <th scope="col">Phase and error</th>
                <th scope="col">Detail</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id} data-run={run.id}>
                  <th scope="row">
                    <span className="mono rotations__id" title={run.id}>{run.id}</span>
                    <Facts>
                      <Fact label="Trigger">{run.trigger}</Fact>
                      <Fact label="Started">{formatWhen(run.started_at)}</Fact>
                      <Fact label="Last change">{formatWhen(run.phase_at)}</Fact>
                    </Facts>
                  </th>
                  <td>
                    <Badge tone={PHASE_TONE[run.phase]}>{run.phase}</Badge>
                    {owesDrop(run) ? (
                      <>
                        {' '}
                        {/* A restore may have put the role back: the badge must not say to drop it. */}
                        <Badge tone="danger">
                          {run.error_code === VERSIONS_CHANGED_CODE ? 'Do not drop: restore' : 'Owes a drop'}
                        </Badge>
                      </>
                    ) : null}
                    <Facts>
                      <Fact label="Error">
                        {run.error_code === undefined || run.error_code === null ? (
                          'None'
                        ) : (
                          <span className="mono rotations__code"><Breakable text={run.error_code} /></span>
                        )}
                      </Fact>
                      <Fact label="Owed role valid until">
                        {run.credential_valid_until === undefined || run.credential_valid_until === null
                          ? 'Not applicable'
                          : `${run.credential_valid_until_lower_bound === true ? 'At least ' : ''}${formatWhen(run.credential_valid_until)}`}
                      </Fact>
                    </Facts>
                  </td>
                  <td>
                    <Facts>
                      <Fact label="Published revision">
                        {run.published_revision === undefined || run.published_revision === null
                          ? 'Not published'
                          : String(run.published_revision)}
                      </Fact>
                      <Fact label="Overlap">
                        {run.revocation_delay_seconds === 0n ? 'Skipped' : span(run.revocation_delay_seconds)}
                      </Fact>
                      <Fact label="Adapter targets not converged">
                        {run.not_converged_adapter_targets === undefined || run.not_converged_adapter_targets === null
                          ? 'Not measured'
                          : String(run.not_converged_adapter_targets)}
                      </Fact>
                    </Facts>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      <p className="machine__footnote">
        Adapter targets not converged counts adapter (sync) targets only: a machine that reads the
        keys directly is not counted, so a zero does not prove every consumer has the new value.
      </p>
    </section>
  );
}
