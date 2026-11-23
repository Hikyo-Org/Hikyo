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
  restoreHoldState,
  restoreHoldText,
  VERSIONS_CHANGED_CODE,
  rfc3339,
  rotationActRefusalText,
  rotationRefusalText,
  useCreateRotation,
  useRotationAct,
  useRotationRuns,
  useRotations,
  type EnvironmentRef,
  type Rotation,
  type RotationAct,
  type RotationRun,
} from '../../api/rotations.ts';
import { duration, plural } from '../../app/duration.ts';
import { formatWhen } from '../../lib/formatWhen.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Badge, type BadgeTone } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { Input } from '../../ui/Input.tsx';
import { Breakable, NamedId } from '../../ui/NamedId.tsx';
import { Select } from '../../ui/Select.tsx';
import { Ceremony } from '../Ceremony.tsx';
import { useProtectedPublishCeremony } from '../useProtectedPublishCeremony.ts';
import { NoEnvironments, type EnvironmentsRead } from './EnvironmentsRead.tsx';

type EnvOption = { readonly id: string; readonly name: string };
type CatalogueKey = { readonly id: string; readonly name: string; readonly classification: 'secret' | 'config'; readonly group_id: string };

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
 * of one of them, and the six acts on a rotation (#1440) with the CLI's
 * authorization: create, resume, run-now and release-hold run the publish
 * ceremony over the environment and both keys first (the server consumes it);
 * pause and delete need none. Every act carries the generation it read.
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
        dropped by the server on its own. This page never shows a credential. Creating, resuming and
        running a rotation, and lifting a restore hold, hand publishing of its keys to the server, so
        each asks you to confirm the publish first (passkey, code or your identity provider), unless a
        re-authentication window still live in this environment covers it; the same acts are{' '}
        <code>hikyo rotation</code> commands.
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
          <RotationList key={current.id} env={{ org, project, environment: current.id }} environmentName={current.name} providers={providers} />
        </>
      )}
    </section>
  );
}

function RotationList({
  env,
  environmentName,
  providers,
}: {
  env: EnvironmentRef;
  environmentName: string;
  providers: readonly DynamicProvider[];
}) {
  const [includeDisabled, setIncludeDisabled] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [acting, setActing] = useState<{ verb: ActVerb; rotation: Rotation } | 'create' | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const query = useRotations(env, includeDisabled);
  const keys = useProjectKeys({ org: env.org, project: env.project });
  const rotations = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data]);
  const catalogue: readonly CatalogueKey[] = keys.data?.items ?? [];
  const keyName = (id: string) => catalogue.find((key) => key.id === id)?.name;
  const provider = (id: string) => providers.find((p) => p.id === id);
  const held = rotations.filter((rotation) => rotation.restore_hold && rotation.state !== 'disabled');
  const halted = rotations.filter((rotation) => rotation.state === 'halted');
  const chosen = rotations.find((rotation) => rotation.id === selected);
  // The dialog acts on the generation the person reviewed; a newer read of that rotation makes it stale.
  const actingNow = acting === null || acting === 'create' ? undefined : rotations.find((rotation) => rotation.id === acting.rotation.id);
  const stale =
    acting !== null &&
    acting !== 'create' &&
    (actingNow === undefined || actingNow.generation !== acting.rotation.generation || !actVerbs(actingNow).includes(acting.verb));

  return (
    <>
      <div className="actions">
        <Button type="button" variant="primary" onClick={() => setActing('create')}>
          Create rotation
        </Button>
      </div>
      {done === null ? null : <p role="status" className="rotations__done">{done}</p>}
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
        <Alert key={rotation.id}>{restoreHoldText(rotation)} Or use Release hold on its row.</Alert>
      ))}
      {halted.length > 0 ? (
        <Alert tone="warn">
          {halted.length === 1 ? 'One rotation is' : `${String(halted.length)} rotations are`} halted:
          the server stopped {halted.length === 1 ? 'it' : 'them'} and starts no run until a person
          resumes {halted.length === 1 ? 'it' : 'them'} with Resume on {halted.length === 1 ? 'its row' : 'their rows'} or{' '}
          <code>hikyo rotation resume</code>. A
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
                Rotation {rotation.id} is also held after a restore: lift the hold with Release hold on its row
                (or <code>hikyo rotation release-hold {rotation.id}</code>) first, then resume it.
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
                        {actVerbs(rotation).map((verb) => (
                          <Button
                            key={verb}
                            type="button"
                            variant={verb === 'delete' ? 'danger' : 'secondary'}
                            aria-label={`${ACT_LABEL[verb]} ${label}`}
                            onClick={() => {
                              setDone(null);
                              setActing({ verb, rotation });
                            }}
                          >
                            {ACT_LABEL[verb]}
                          </Button>
                        ))}
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
            : 'No rotations in this environment yet. Create one here or with hikyo rotation create.'}
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

      {acting === 'create' ? (
        <CreateRotationDialog
          env={env}
          environmentName={environmentName}
          providers={providers}
          catalogue={catalogue}
          onClose={() => setActing(null)}
          onDone={(message) => {
            setActing(null);
            setDone(message);
          }}
        />
      ) : acting === null ? null : (
        <RotationActDialog
          env={env}
          environmentName={environmentName}
          verb={acting.verb}
          rotation={acting.rotation}
          stale={stale}
          catalogue={catalogue}
          onClose={() => setActing(null)}
          onDone={(message) => {
            setActing(null);
            setDone(message);
          }}
        />
      )}
    </>
  );
}

type ActVerb = RotationAct['verb'];

const ACT_LABEL: Record<ActVerb, string> = {
  pause: 'Pause',
  resume: 'Resume',
  'release-hold': 'Release hold',
  'run-now': 'Run now',
  delete: 'Delete',
};

/**
 * actVerbs offers only the acts the server could accept in this state. A held
 * rotation refuses resume, run-now and delete until its hold is released, so
 * release is offered instead; a deleted rotation is history and takes no act.
 */
function actVerbs(rotation: Rotation): ActVerb[] {
  if (rotation.state === 'disabled') return [];
  const held = rotation.restore_hold;
  const verbs: ActVerb[] = [];
  if (held) verbs.push('release-hold');
  if (rotation.state === 'active') {
    verbs.push('pause');
    if (!held) verbs.push('run-now');
  } else if (!held) {
    verbs.push('resume');
  }
  if (!held) verbs.push('delete');
  return verbs;
}

/** The acts that hand publishing to the rotation, so the server consumes the publish ceremony. */
const CEREMONY_VERBS: ReadonlySet<ActVerb | 'create'> = new Set(['create', 'resume', 'release-hold', 'run-now']);

/** The enumerated unit of the ceremony: both keys, by name where the catalogue knows them. */
function ceremonyKeys(catalogue: readonly CatalogueKey[], ids: readonly string[]) {
  return ids.map((id) => {
    const key = catalogue.find((k) => k.id === id);
    return key === undefined ? { id, name: id } : { id, name: key.name, classification: key.classification };
  });
}

/** expiryNote says what pausing or deleting leaves valid, as the CLI does after the act. */
function expiryNote(rotation: Rotation): string {
  return rotation.credential_expires_at === undefined || rotation.credential_expires_at === null
    ? 'No run has published a credential yet.'
    : `The credential already delivered stays valid until ${rfc3339(rotation.credential_expires_at)}; this revokes nothing.`;
}

/**
 * useActGuard runs the publish ceremony before an act that needs it, then the
 * act; an act that needs none runs at once. A refused ceremony lookup is the
 * guard's error, shown in the dialog.
 */
function useActGuard(env: EnvironmentRef, environmentName: string, scope: string) {
  const guard = useProtectedPublishCeremony({ org: env.org, project: env.project }, ['rotation', env.environment, scope]);
  const run = (verb: ActVerb | 'create', keys: ReturnType<typeof ceremonyKeys>, act: () => void) => {
    if (!CEREMONY_VERBS.has(verb)) {
      act();
      return;
    }
    void guard.run(
      [{ environmentId: env.environment, environmentName, keys, purpose: verb === 'run-now' ? 'rotation-run' : verb === 'release-hold' ? 'rotation-release' : 'rotation' }],
      act,
      'The publish confirmation could not be started, so nothing was changed',
    );
  };
  const ceremony =
    guard.request === null ? null : (
      <Ceremony key={guard.requestKey} request={guard.request} onAuthorised={guard.onAuthorised} onCancel={guard.onCancel} />
    );
  return { run, ceremony, error: guard.error };
}

function RotationActDialog({
  env,
  environmentName,
  verb,
  rotation,
  stale,
  catalogue,
  onClose,
  onDone,
}: {
  env: EnvironmentRef;
  environmentName: string;
  verb: ActVerb;
  rotation: Rotation;
  stale: boolean;
  catalogue: readonly CatalogueKey[];
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const mutation = useRotationAct(env);
  const guard = useActGuard(env, environmentName, rotation.id);
  const [failure, setFailure] = useState<string | null>(null);
  const [skipOverlap, setSkipOverlap] = useState(false);
  const [abandon, setAbandon] = useState(false);
  const [reconciled, setReconciled] = useState(false);
  const busy = mutation.isPending;
  const name = catalogue.find((key) => key.id === rotation.key_id)?.name ?? rotation.id;

  const act: RotationAct =
    verb === 'run-now'
      ? { verb, rotation, skipOverlap }
      : verb === 'delete'
        ? { verb, rotation, abandon }
        : { verb, rotation };
  const DONE: Record<ActVerb, string> = {
    pause: `Rotation ${rotation.id} paused. ${expiryNote(rotation)}`,
    resume: `Rotation ${rotation.id} resumed; its next run is scheduled again.`,
    'release-hold':
      rotation.state === 'active'
        ? `Rotation ${rotation.id} released: the executor may now settle the runs the hold left alone, and its schedule publishes again.`
        : `Rotation ${rotation.id} released: the executor may now settle the runs the hold left alone. It was not resumed.`,
    'run-now': `A run of rotation ${rotation.id} was requested; the executor starts it shortly. Show its runs to follow it.`,
    delete: `Rotation ${rotation.id} deleted. ${expiryNote(rotation)}`,
  };

  const submit = () => {
    setFailure(null);
    guard.run(verb, ceremonyKeys(catalogue, [rotation.key_id, rotation.username_key_id]), () => {
      mutation.mutate(act, {
        onSuccess: () => onDone(DONE[verb]),
        onError: (error) => setFailure(rotationActRefusalText(error, verb)),
      });
    });
  };

  return (
    <>
      <Dialog
        title={`${ACT_LABEL[verb]} rotation · ${name}`}
        onCancel={(event) => {
          event.preventDefault();
          if (!busy) onClose();
        }}
        actions={
          <>
            <Button type="button" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button
              type="button"
              variant={verb === 'delete' || verb === 'run-now' ? 'danger' : 'primary'}
              disabled={busy || stale || (verb === 'release-hold' && !reconciled)}
              onClick={submit}
            >
              {busy ? 'Working…' : ACT_LABEL[verb]}
            </Button>
          </>
        }
      >
        <p className="mono rotations__id">{rotation.id}</p>
        {stale && !busy ? (
          <Alert tone="warn">
            Rotation {rotation.id} changed since you opened this, so nothing will be sent. Close this and review its row
            again.
          </Alert>
        ) : null}
        {verb === 'pause' ? (
          <p>Pausing stops scheduling this rotation until a person resumes it. {expiryNote(rotation)}</p>
        ) : null}
        {verb === 'resume' ? (
          <>
            {rotation.state === 'halted' ? (
              <Alert tone="warn">
                Rotation {rotation.id} is <CodeText text={haltCause(rotation)} />. Resume only once the cause is dealt with.
              </Alert>
            ) : null}
            <p>
              Resuming re-runs the whole authorization: you must hold edit, publish and reveal on both keys, the recorded
              authority principal must still hold the same access, the environment must still publish unattended and the
              provider must still be able to mint. You confirm the publish over both keys next.
            </p>
          </>
        ) : null}
        {verb === 'release-hold' ? (
          <>
            <Alert>{restoreHoldState(rotation)}.</Alert>
            <p>
              Before you release it: reconcile the rotation&apos;s grants and its provider credential, and read its runs.
              Releasing lets the executor roll back or reconcile the runs held before their publish and drop their
              unpublished candidate roles.{' '}
              {rotation.state === 'active'
                ? 'This rotation is active, so its next scheduled run mints and publishes unattended again.'
                : 'It never resumes a paused or halted rotation: this one stays stopped until a person resumes it.'}
            </p>
            <Checkbox
              label="I have reconciled the grants and the provider credential and read the runs"
              checked={reconciled}
              onChange={(event) => setReconciled(event.target.checked)}
            />
          </>
        ) : null}
        {verb === 'run-now' ? (
          <>
            <p>
              This publishes a new credential now, outside the schedule, through the same pre-publish gate and overlap as a
              scheduled run. You confirm the publish over both keys next.
            </p>
            <Checkbox
              label="Skip the overlap: revoke the superseded credential as soon as the new one is published"
              checked={skipOverlap}
              onChange={(event) => setSkipOverlap(event.target.checked)}
            />
            {skipOverlap ? (
              <Alert tone="warn">
                A consumer that has not picked up the new credential loses access at once. Adapter targets are counted
                afterwards; a machine that reads the keys directly is not.
              </Alert>
            ) : null}
          </>
        ) : null}
        {verb === 'delete' ? (
          <>
            <p>
              Deleting moves the rotation to deleted and frees its keys; its record and runs stay as history.{' '}
              {expiryNote(rotation)}
            </p>
            <Checkbox
              label="Close its unsettled runs instead of refusing while a run is in flight"
              checked={abandon}
              onChange={(event) => setAbandon(event.target.checked)}
            />
            {abandon ? (
              <Alert tone="warn">
                Closing revokes nothing: a role a run may still hold is kept for the executor to settle, and each closed run
                is recorded.
              </Alert>
            ) : null}
          </>
        ) : null}
        {guard.error === null ? null : <Alert>{guard.error}</Alert>}
        {failure === null ? null : <Alert><CodeText text={failure} /></Alert>}
      </Dialog>
      {guard.ceremony}
    </>
  );
}

const HOUR = 3600;
const MINUTE = 60;

/**
 * CreateRotationDialog configures a rotation. The bounds mirror the API's
 * (interval 1 hour to 90 days, overlap 5 minutes to 7 days and shorter than
 * the interval) so an obvious slip is named before the ceremony; every other
 * refusal (a protected environment, a key in use or grouped, a provider that
 * cannot mint) is the server's, named in its words.
 */
function CreateRotationDialog({
  env,
  environmentName,
  providers,
  catalogue,
  onClose,
  onDone,
}: {
  env: EnvironmentRef;
  environmentName: string;
  providers: readonly DynamicProvider[];
  catalogue: readonly CatalogueKey[];
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const mutation = useCreateRotation(env);
  const guard = useActGuard(env, environmentName, 'create');
  const minting = providers.filter((p) => p.state === 'active' && p.credential_present);
  // A key group publishes together, and a rotation publishes exactly its two keys.
  const ungrouped = catalogue.filter((key) => key.group_id === '');
  const secrets = ungrouped.filter((key) => key.classification === 'secret');
  const [providerId, setProviderId] = useState(minting[0]?.id ?? '');
  const [keyId, setKeyId] = useState('');
  const [usernameKeyId, setUsernameKeyId] = useState('');
  const [hours, setHours] = useState('24');
  const [minutes, setMinutes] = useState('60');
  const [failure, setFailure] = useState<string | null>(null);
  const busy = mutation.isPending;

  const submit = () => {
    // A fractional hour or minute would still be whole seconds: check the field itself.
    const interval = Number.isInteger(Number(hours)) ? Number(hours) * HOUR : Number.NaN;
    const delay = Number.isInteger(Number(minutes)) ? Number(minutes) * MINUTE : Number.NaN;
    let invalid: string | null = null;
    if (providerId === '' || keyId === '' || usernameKeyId === '') invalid = 'Choose a provider, a password key and a role name key.';
    else if (keyId === usernameKeyId) invalid = 'The password key and the role name key must be two different keys.';
    else if (!Number.isInteger(interval) || interval < HOUR || interval > 90 * 24 * HOUR) invalid = 'The interval is a whole number of hours, from 1 to 2160 (90 days).';
    else if (!Number.isInteger(delay) || delay < 5 * MINUTE || delay > 7 * 24 * HOUR) invalid = 'The overlap is a whole number of minutes, from 5 to 10080 (7 days).';
    else if (delay >= interval) invalid = 'The overlap must be shorter than the interval.';
    setFailure(invalid === null ? null : `${invalid} Nothing was created.`);
    if (invalid !== null) return;
    guard.run('create', ceremonyKeys(catalogue, [keyId, usernameKeyId]), () => {
      mutation.mutate(
        { providerId, keyId, usernameKeyId, intervalSeconds: interval, revocationDelaySeconds: delay },
        {
          onSuccess: (rotation) => onDone(`Rotation ${rotation.id} created; you are its recorded authority principal.`),
          onError: (error) => setFailure(rotationActRefusalText(error, 'create')),
        },
      );
    });
  };

  return (
    <>
      <Dialog
        title={`Create rotation · ${environmentName}`}
        lede={
          <>
            The server mints a new PostgreSQL role and password on the schedule, publishes them into the two keys
            unattended and drops the superseded role after the overlap. You are recorded as the authority it acts for, so
            you need edit, publish and reveal on both keys; a protected environment, or one under a secret-change approval
            policy, is refused.
          </>
        }
        onCancel={(event) => {
          event.preventDefault();
          if (!busy) onClose();
        }}
        actions={
          <>
            <Button type="button" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button type="button" variant="primary" disabled={busy || minting.length === 0} onClick={submit}>
              {busy ? 'Creating…' : 'Create rotation'}
            </Button>
          </>
        }
      >
        {minting.length === 0 ? (
          <Alert tone="warn">
            No dynamic provider here can mint: configure one with its admin credential under Dynamic secrets first.
          </Alert>
        ) : null}
        <fieldset className="machine__lock" disabled={busy}>
          <Select label="Provider" value={providerId} onChange={(event) => setProviderId(event.target.value)}>
            {minting.map((p) => (
              <option key={p.id} value={p.id}>
                {p.kind} {p.origin}
              </option>
            ))}
          </Select>
          <Select label="Password key" hint="A secret key outside any key group." value={keyId} onChange={(event) => setKeyId(event.target.value)}>
            <option value="">Choose a key</option>
            {secrets.map((key) => (
              <option key={key.id} value={key.id}>
                {key.name}
              </option>
            ))}
          </Select>
          <Select label="Role name key" hint="Receives the minted role name. Outside any key group." value={usernameKeyId} onChange={(event) => setUsernameKeyId(event.target.value)}>
            <option value="">Choose a key</option>
            {ungrouped.map((key) => (
              <option key={key.id} value={key.id}>
                {key.name}
              </option>
            ))}
          </Select>
          <Input label="Interval (hours)" type="number" min={1} max={2160} step={1} value={hours} onChange={(event) => setHours(event.target.value)} />
          <Input
            label="Overlap (minutes)"
            hint="How long the superseded credential stays valid after the new one is published."
            type="number"
            min={5}
            max={10080}
            step={1}
            value={minutes}
            onChange={(event) => setMinutes(event.target.value)}
          />
        </fieldset>
        {guard.error === null ? null : <Alert>{guard.error}</Alert>}
        {failure === null ? null : <Alert><CodeText text={failure} /></Alert>}
      </Dialog>
      {guard.ceremony}
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
