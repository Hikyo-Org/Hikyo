import { useEffect, useMemo, useState } from 'react';

import { useProjectKeys } from '../../api/adapters.ts';
import type { DynamicProvider } from '../../api/dynamic.ts';
import {
  MAX_RUN_PAGES,
  owedRunText,
  owesDrop,
  restoreHoldText,
  rotationRefusalText,
  useRotationRuns,
  useRotations,
  type EnvironmentRef,
  type Rotation,
  type RotationRun,
} from '../../api/rotations.ts';
import { duration } from '../../app/duration.ts';
import { formatWhen } from '../../lib/formatWhen.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Badge, type BadgeTone } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { NamedId } from '../../ui/NamedId.tsx';
import { Select } from '../../ui/Select.tsx';

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
  providers,
}: {
  org: string;
  project: string;
  environments: readonly EnvOption[];
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
        <p role="status">This project has no environments, so it has no rotations.</p>
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
        label="Include deleted rotations"
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
          the server stopped {halted.length === 1 ? 'it' : 'them'} after a failed check and starts
          no run until a person resumes {halted.length === 1 ? 'it' : 'them'} with{' '}
          <code>hikyo rotation resume</code>. A halted rotation does not extend the credential
          already delivered.
        </Alert>
      ) : null}

      {query.isSuccess ? (
        <div className="table-scroll">
          <table className="values__table machine__table">
            <caption className="visually-hidden">
              The environment&apos;s credential rotations. No credential is listed.
            </caption>
            <thead>
              <tr>
                <th scope="col">Keys</th>
                <th scope="col">Provider</th>
                <th scope="col">State</th>
                <th scope="col" className="col-secondary">Schedule</th>
                <th scope="col" className="col-secondary">Next run</th>
                <th scope="col" className="col-secondary">Delivered credential expires</th>
                <th scope="col">Runs</th>
              </tr>
            </thead>
            <tbody>
              {rotations.map((rotation) => {
                const minter = provider(rotation.provider_id);
                const isHeld = rotation.restore_hold && rotation.state !== 'disabled';
                return (
                  <tr key={rotation.id} data-rotation={rotation.id}>
                    <td>
                      <div>
                        Password key{' '}
                        <NamedId id={rotation.key_id} name={keyName(rotation.key_id)} />
                      </div>
                      <div>
                        Role name key{' '}
                        <NamedId id={rotation.username_key_id} name={keyName(rotation.username_key_id)} />
                      </div>
                      <div className="mono machine__name" title={rotation.id}>
                        {rotation.id}
                      </div>
                    </td>
                    <td>
                      {minter === undefined ? (
                        <NamedId id={rotation.provider_id} name={undefined} />
                      ) : (
                        <>
                          {minter.kind} <span className="mono machine__origin" title={minter.origin}>{minter.origin}</span>
                        </>
                      )}
                    </td>
                    <td>
                      <Badge tone={STATE_TONE[rotation.state]}>{rotation.state}</Badge>
                      {isHeld ? (
                        <>
                          {' '}
                          <Badge tone="danger">Restore hold</Badge>
                        </>
                      ) : null}
                    </td>
                    <td className="col-secondary">
                      Every {span(rotation.interval_seconds)}, {span(rotation.revocation_delay_seconds)} overlap
                    </td>
                    <td className="col-secondary">
                      {rotation.state === 'active'
                        ? at(rotation.next_run_at, 'Not scheduled')
                        : 'Not scheduled'}
                      {isHeld ? ' (held: no run starts)' : null}
                    </td>
                    <td className="col-secondary">
                      {at(rotation.credential_expires_at, 'No run has published yet')}
                    </td>
                    <td>
                      <Button
                        type="button"
                        variant="quiet"
                        aria-pressed={selected === rotation.id}
                        aria-label={`Runs of rotation ${keyName(rotation.key_id) ?? rotation.id}`}
                        onClick={() => setSelected(selected === rotation.id ? null : rotation.id)}
                      >
                        {selected === rotation.id ? 'Hide runs' : 'Show runs'}
                      </Button>
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
      {query.hasNextPage ? (
        <div className="actions">
          <Button type="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
            Load more rotations
          </Button>
        </div>
      ) : null}

      {chosen === undefined ? null : (
        <RunTimeline env={env} rotation={chosen} name={keyName(chosen.key_id) ?? chosen.id} />
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
  const { hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage } = query;
  const pages = query.data?.pages.length ?? 0;
  const runs = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data]);
  const reading = hasNextPage && pages < MAX_RUN_PAGES && !isFetchNextPageError;
  useEffect(() => {
    if (reading && !isFetchingNextPage) void fetchNextPage();
  }, [reading, isFetchingNextPage, fetchNextPage]);
  const owed = runs.filter(owesDrop);

  return (
    <section className="machine__sub" aria-labelledby={`runs-title-${rotation.id}`}>
      <h3 id={`runs-title-${rotation.id}`}>Runs of {name}</h3>
      {query.isPending ? <p role="status">Loading runs…</p> : null}
      {query.isError ? <Alert>{rotationRefusalText(query.error, 'runs')}</Alert> : null}
      {hasNextPage && !reading && !query.isError ? (
        <Alert tone="warn">
          Only the newest {String(runs.length)} runs are shown. An older run can still owe a
          provider role a drop: page through <code>hikyo rotation runs {rotation.id}</code> to
          check.
        </Alert>
      ) : null}
      {owed.map((run) => (
        <Alert key={run.id}>{owedRunText(run)}</Alert>
      ))}
      {query.isSuccess && runs.length === 0 ? (
        <p role="status">This rotation has not run yet.</p>
      ) : null}
      {runs.length > 0 ? (
        <div className="table-scroll" role="region" aria-label={`Run history of ${name}`} tabIndex={0}>
          <table className="values__table machine__table">
            <caption className="visually-hidden">
              Runs of this rotation, newest first. Error codes are fixed words, never provider detail.
            </caption>
            <thead>
              <tr>
                <th scope="col">Run</th>
                <th scope="col">Phase</th>
                <th scope="col">Trigger</th>
                <th scope="col" className="col-secondary">Started</th>
                <th scope="col" className="col-secondary">Last change</th>
                <th scope="col" className="col-secondary">Published revision</th>
                <th scope="col" className="col-secondary">Overlap</th>
                <th scope="col">Error</th>
                <th scope="col" className="col-secondary">Adapter targets not converged</th>
                <th scope="col" className="col-secondary">Owed role valid until</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id} data-run={run.id}>
                  <td>
                    <span className="mono machine__name" title={run.id}>{run.id}</span>
                  </td>
                  <td>
                    <Badge tone={PHASE_TONE[run.phase]}>{run.phase}</Badge>
                    {owesDrop(run) ? (
                      <>
                        {' '}
                        <Badge tone="danger">Owes a drop</Badge>
                      </>
                    ) : null}
                  </td>
                  <td>{run.trigger}</td>
                  <td className="col-secondary">{formatWhen(run.started_at)}</td>
                  <td className="col-secondary">{formatWhen(run.phase_at)}</td>
                  <td className="col-secondary">
                    {run.published_revision === undefined || run.published_revision === null
                      ? 'Not published'
                      : String(run.published_revision)}
                  </td>
                  <td className="col-secondary">{run.revocation_delay_seconds === 0n ? 'Skipped' : span(run.revocation_delay_seconds)}</td>
                  <td>
                    {run.error_code === undefined || run.error_code === null ? (
                      'None'
                    ) : (
                      <span className="mono">{run.error_code}</span>
                    )}
                  </td>
                  <td className="col-secondary">
                    {run.not_converged_adapter_targets === undefined || run.not_converged_adapter_targets === null
                      ? 'Not measured'
                      : String(run.not_converged_adapter_targets)}
                  </td>
                  <td className="col-secondary">
                    {run.credential_valid_until === undefined || run.credential_valid_until === null
                      ? 'Not applicable'
                      : `${run.credential_valid_until_lower_bound === true ? 'At least ' : ''}${formatWhen(run.credential_valid_until)}`}
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
