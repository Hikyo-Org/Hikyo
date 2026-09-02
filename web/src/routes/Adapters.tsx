import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useParams, useSearchParams } from 'react-router';

import {
  adapterRefusalText,
  errorClassText,
  healthLabel,
  useAdapters,
  useAddAdapterTarget,
  useAdoptAdapterNames,
  useAdapterTarget,
  useCreateAdapter,
  usePauseAdapterTarget,
  useProjectEnvironments,
  useProjectKeys,
  useRemoveAdapterTarget,
  useResumeAdapterTarget,
  useResyncAdapterTarget,
  useUpdateAdapterTarget,
  type Adapter,
  type AdapterConflictArtifact,
  type AdapterTarget,
  type AdapterTargetInput,
  type ProjectEnvironment,
  type ProjectKey,
  type ProjectRef,
} from '../api/adapters.ts';
import {
  fetchRevealWindow,
  runAdapterPasskeyCeremony,
  runAdapterTOTPCeremony,
} from '../api/values.ts';
import { useFeedback, useModalDialog } from './useModalDialog.ts';

/**
 * Deployment adapters (#157): the multi-target synchronization surface.
 *
 * Hikyo stays the source: one published revision fans out to every target
 * an operator configured, each with its own health, its own ownership ledger
 * and its own pause. The surface lists adapters and targets, opens one
 * target's detail with the attempt facts and the control verbs, and never
 * shows a value: the mapping is names only, the credential is write-only.
 *
 * Every push-shaped act (create, add or edit a target, resume, adopt) runs
 * the adapter reauthentication ceremony over the adapter's whole environment
 * set before the request, exactly as the CLI handoff does. Pause and remove
 * narrow what Hikyo does at the destination and need no ceremony.
 */

type AdapterOperation = 'adapter.configure' | 'adapter.adopt' | 'adapter.sync';

/** The two facts the target form needs about an environment. */
type EnvironmentOption = { readonly id: string; readonly name: string };

type CeremonyAsk = {
  readonly operation: AdapterOperation;
  readonly environmentIds: readonly string[];
  readonly resolve: () => void;
  readonly reject: (error: unknown) => void;
};

function when(value: string | null | undefined): string {
  if (value === null || value === undefined) return '—';
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString();
}

function revision(value: bigint | null | undefined): string {
  return value === null || value === undefined ? '—' : `rev ${String(value)}`;
}

function destinationText(target: AdapterTarget): string {
  const base =
    target.destination_name === ''
      ? target.destination_owner
      : `${target.destination_owner}/${target.destination_name}`;
  return target.destination_environment === '' ? base : `${base} (${target.destination_environment})`;
}

function environmentSet(adapter: Adapter, extra?: string): string[] {
  const ids = new Set(adapter.targets.map((target) => target.environment_id));
  if (extra !== undefined && extra !== '') ids.add(extra);
  return [...ids].sort();
}

export function Adapters() {
  const params = useParams();
  const ref: ProjectRef = useMemo(
    () => ({ org: params.org ?? '', project: params.project ?? '' }),
    [params.org, params.project],
  );
  const [search, setSearch] = useSearchParams();
  const selected = search.get('target') ?? '';
  const adapters = useAdapters(ref);
  const environments = useProjectEnvironments(ref);
  const keys = useProjectKeys(ref);
  const [creating, setCreating] = useState(false);
  const [ceremony, setCeremony] = useState<CeremonyAsk | null>(null);
  const feedback = useFeedback(adapterRefusalText);

  const environmentName = (id: string): string =>
    environments.data?.items.find((environment) => environment.id === id)?.name ?? id;

  const select = (target: string) => {
    const next = new URLSearchParams(search);
    if (target === '') next.delete('target');
    else next.set('target', target);
    setSearch(next, { replace: true });
  };

  /** ceremonyFor resolves once the human has authorised the operation. */
  const ceremonyFor = (operation: AdapterOperation, environmentIds: readonly string[]) =>
    new Promise<void>((resolve, reject) => {
      setCeremony({ operation, environmentIds, resolve, reject });
    });

  const selectedAdapter = adapters.data?.items.find((adapter) =>
    adapter.targets.some((target) => target.id === selected),
  );

  return (
    <div className="page page--chrome page--adapters">
      <h1>Deployment adapters</h1>
      <p className="page__lede">
        Push selected published values to CI providers. Hikyo stays the source: every target
        receives the same pinned revision through its own durable job, and one target failing or
        pausing never blocks another.
      </p>

      {feedback.failure !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{feedback.failure}</span>
        </p>
      ) : null}
      {feedback.done !== null ? (
        <p className="adapters__done" role="status">
          {feedback.done}
        </p>
      ) : null}

      <div className={`adapters__panes${selected !== '' ? ' adapters__panes--split' : ''}`}>
        <section className="adapters__list" aria-label="Adapters">
          {adapters.isError ? (
            <p className="alert" role="alert">
              {adapterRefusalText(adapters.error)}
            </p>
          ) : null}
          {adapters.isSuccess && adapters.data.items.length === 0 ? (
            <p className="adapters__empty" role="status">
              No adapters yet. Add one to fan a published environment out to a CI provider.
            </p>
          ) : null}
          {adapters.data?.items.map((adapter) => (
            <AdapterPanel
              key={adapter.id}
              adapter={adapter}
              refData={ref}
              selected={selected}
              onSelect={select}
              environmentName={environmentName}
              environments={environments.data?.items ?? []}
              keys={keys.data?.items ?? []}
              ceremonyFor={ceremonyFor}
              feedback={feedback}
            />
          ))}
          {creating ? (
            <CreateAdapterPanel
              refData={ref}
              environments={environments.data?.items ?? []}
              keys={keys.data?.items ?? []}
              ceremonyFor={ceremonyFor}
              feedback={feedback}
              onClose={() => setCreating(false)}
            />
          ) : (
            <div className="panel__actions">
              <button type="button" className="btn btn--primary" onClick={() => setCreating(true)}>
                Add adapter
              </button>
            </div>
          )}
        </section>

        {selected !== '' && selectedAdapter !== undefined ? (
          <TargetDetail
            key={selected}
            refData={ref}
            adapter={selectedAdapter}
            targetId={selected}
            environmentName={environmentName}
            keys={keys.data?.items ?? []}
            ceremonyFor={ceremonyFor}
            feedback={feedback}
            onClose={() => select('')}
          />
        ) : null}
      </div>

      {ceremony !== null ? (
        <AdapterCeremony
          refData={ref}
          ask={ceremony}
          environmentName={environmentName}
          onDone={() => setCeremony(null)}
        />
      ) : null}
    </div>
  );
}

type Feedback = ReturnType<typeof useFeedback>;

function HealthChip({ target }: { readonly target: AdapterTarget }) {
  return (
    <span className={`chip adapters__health adapters__health--${target.sync_status}`}>
      <span className="adapters__health-glyph" aria-hidden="true" />
      {healthLabel(target.sync_status)}
      {target.drift_attention ? ' · needs attention' : ''}
    </span>
  );
}

function AdapterPanel({
  adapter,
  refData,
  selected,
  onSelect,
  environmentName,
  environments,
  keys,
  ceremonyFor,
  feedback,
}: {
  readonly adapter: Adapter;
  readonly refData: ProjectRef;
  readonly selected: string;
  readonly onSelect: (target: string) => void;
  readonly environmentName: (id: string) => string;
  readonly environments: readonly ProjectEnvironment[];
  readonly keys: readonly ProjectKey[];
  readonly ceremonyFor: (op: AdapterOperation, envs: readonly string[]) => Promise<void>;
  readonly feedback: Feedback;
}) {
  const [adding, setAdding] = useState(false);
  const add = useAddAdapterTarget(refData);
  return (
    <section className="panel adapters__adapter" aria-label={`Adapter ${adapter.origin}`}>
      <div className="adapters__adapter-head">
        <h2>{adapter.provider === 'forgejo' ? 'Forgejo' : 'GitHub Actions'}</h2>
        <span className="adapters__origin mono">{adapter.origin}</span>
        <span className="chip">
          {adapter.credential_present ? 'credential set' : 'credential absent'}
        </span>
      </div>
      {adapter.targets.length === 0 ? (
        <p className="adapters__empty">This adapter has no targets.</p>
      ) : (
        <ul className="adapters__targets" aria-label={`Targets of ${adapter.origin}`}>
          {adapter.targets.map((target) => (
            <li key={target.id}>
              <button
                type="button"
                className="adapters__target"
                aria-pressed={selected === target.id}
                onClick={() => onSelect(target.id)}
              >
                <span className="adapters__target-route">
                  <span className="adapters__target-env">{environmentName(target.environment_id)}</span>
                  <span className="adapters__target-dest mono">
                    {target.name_prefix !== '' ? `${target.name_prefix}* → ` : ''}
                    {destinationText(target)}
                  </span>
                </span>
                <HealthChip target={target} />
              </button>
            </li>
          ))}
        </ul>
      )}
      {adding ? (
        <TargetForm
          title="Add target"
          environments={environments}
          keys={keys}
          busy={add.isPending}
          onCancel={() => setAdding(false)}
          onSubmit={async (input) => {
            try {
              await ceremonyFor('adapter.configure', environmentSet(adapter, input.environment_id));
              await add.mutateAsync({ adapter: adapter.id, target: input });
              feedback.ok('Target added. Its first converge is queued.');
              setAdding(false);
            } catch (error) {
              feedback.report(error);
            }
          }}
        />
      ) : (
        <div className="panel__actions">
          <button type="button" className="btn" onClick={() => setAdding(true)}>
            Add target
          </button>
        </div>
      )}
    </section>
  );
}

function CreateAdapterPanel({
  refData,
  environments,
  keys,
  ceremonyFor,
  feedback,
  onClose,
}: {
  readonly refData: ProjectRef;
  readonly environments: readonly ProjectEnvironment[];
  readonly keys: readonly ProjectKey[];
  readonly ceremonyFor: (op: AdapterOperation, envs: readonly string[]) => Promise<void>;
  readonly feedback: Feedback;
  readonly onClose: () => void;
}) {
  const create = useCreateAdapter(refData);
  const [provider, setProvider] = useState<'forgejo' | 'github-actions'>('forgejo');
  const [origin, setOrigin] = useState('');
  const [credential, setCredential] = useState('');
  return (
    <section className="panel adapters__adapter" aria-label="New adapter">
      <div className="adapters__adapter-head">
        <h2>New adapter</h2>
      </div>
      <div className="adapters__form adapters__form--two">
        <label className="field">
          <span className="field__label">Provider</span>
          <select
            value={provider}
            onChange={(event) =>
              setProvider(event.target.value === 'github-actions' ? 'github-actions' : 'forgejo')
            }
          >
            <option value="forgejo">Forgejo</option>
            <option value="github-actions">GitHub Actions</option>
          </select>
        </label>
        <label className="field">
          <span className="field__label">Origin</span>
          <input
            value={origin}
            onChange={(event) => setOrigin(event.target.value)}
            placeholder="https://git.example.com"
            autoComplete="off"
          />
        </label>
        <label className="field">
          <span className="field__label">Credential</span>
          <input
            type="password"
            value={credential}
            onChange={(event) => setCredential(event.target.value)}
            autoComplete="new-password"
          />
          <span className="field__hint">Write-only. It is sealed on save and never shown again.</span>
        </label>
      </div>
      <TargetForm
        title="First target"
        environments={environments}
        keys={keys}
        busy={create.isPending}
        onCancel={onClose}
        onSubmit={async (input) => {
          try {
            await ceremonyFor('adapter.configure', [input.environment_id]);
            await create.mutateAsync({ provider, origin, credential, target: input });
            setCredential('');
            feedback.ok('Adapter created. Its first converge is queued.');
            onClose();
          } catch (error) {
            feedback.report(error);
          }
        }}
      />
    </section>
  );
}

/**
 * TargetForm is the one target editor: destination, prefix and the explicit
 * key subset. Ticked keys are ids; the include/exclude patterns and the
 * classification are conveniences resolved on save and never stored, and
 * the copy says so beside them.
 */
function TargetForm({
  title,
  environments,
  keys,
  busy,
  initial,
  lockRouting,
  onCancel,
  onSubmit,
}: {
  readonly title: string;
  readonly environments: readonly EnvironmentOption[];
  readonly keys: readonly ProjectKey[];
  readonly busy: boolean;
  readonly initial?: AdapterTarget;
  /** Editing keeps the route: a destination change is the CLI's move ceremony. */
  readonly lockRouting?: boolean;
  readonly onCancel: () => void;
  readonly onSubmit: (input: AdapterTargetInput) => Promise<void>;
}) {
  const [environmentId, setEnvironmentId] = useState(initial?.environment_id ?? environments[0]?.id ?? '');
  const [kind, setKind] = useState<AdapterTargetInput['destination_kind']>(
    initial?.destination_kind ?? 'repository',
  );
  const [owner, setOwner] = useState(initial?.destination_owner ?? '');
  const [name, setName] = useState(initial?.destination_name ?? '');
  const [destinationEnvironment, setDestinationEnvironment] = useState(
    initial?.destination_environment ?? '',
  );
  const [visibility, setVisibility] = useState<AdapterTargetInput['visibility']>(initial?.visibility ?? '');
  const [prefix, setPrefix] = useState(initial?.name_prefix ?? '');
  const [keyIds, setKeyIds] = useState<ReadonlySet<string>>(
    () => new Set(initial?.keys.map((key) => key.key_id) ?? []),
  );
  const [include, setInclude] = useState('');
  const [exclude, setExclude] = useState('');
  const [classification, setClassification] = useState<'' | 'secret' | 'config'>('');

  useEffect(() => {
    if (environmentId === '' && environments[0] !== undefined) setEnvironmentId(environments[0].id);
  }, [environmentId, environments]);

  const patterns = (raw: string): string[] =>
    raw
      .split(',')
      .map((pattern) => pattern.trim())
      .filter((pattern) => pattern !== '');

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const includePatterns = patterns(include);
    const excludePatterns = patterns(exclude);
    const selection =
      includePatterns.length === 0 && excludePatterns.length === 0 && classification === ''
        ? undefined
        : {
            ...(includePatterns.length === 0 ? {} : { include: includePatterns }),
            ...(excludePatterns.length === 0 ? {} : { exclude: excludePatterns }),
            ...(classification === '' ? {} : { classification }),
          };
    void onSubmit({
      environment_id: environmentId,
      destination_kind: kind,
      destination_owner: owner,
      destination_name: kind === 'organization' ? '' : name,
      destination_environment: kind === 'environment' ? destinationEnvironment : '',
      visibility: kind === 'organization' ? visibility : '',
      selected_repository_ids: [],
      name_prefix: prefix,
      key_ids: [...keyIds],
      ...(selection === undefined ? {} : { key_selection: selection }),
    });
  };

  const toggle = (id: string) =>
    setKeyIds((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  return (
    <form className="adapters__form" onSubmit={submit} aria-label={title}>
      <h3>{title}</h3>
      <div className="adapters__form adapters__form--two">
        <label className="field">
          <span className="field__label">Environment</span>
          <select
            value={environmentId}
            disabled={lockRouting === true}
            onChange={(event) => setEnvironmentId(event.target.value)}
          >
            {environments.map((environment) => (
              <option key={environment.id} value={environment.id}>
                {environment.name}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span className="field__label">Destination kind</span>
          <select
            value={kind}
            disabled={lockRouting === true}
            onChange={(event) => {
              const value = event.target.value;
              setKind(value === 'organization' || value === 'environment' ? value : 'repository');
            }}
          >
            <option value="repository">Repository</option>
            <option value="organization">Organization</option>
            <option value="environment">GitHub environment</option>
          </select>
        </label>
        <label className="field">
          <span className="field__label">Owner</span>
          <input value={owner} disabled={lockRouting === true} onChange={(event) => setOwner(event.target.value)} />
        </label>
        {kind !== 'organization' ? (
          <label className="field">
            <span className="field__label">Repository</span>
            <input value={name} disabled={lockRouting === true} onChange={(event) => setName(event.target.value)} />
          </label>
        ) : (
          <label className="field">
            <span className="field__label">Visibility</span>
            <select
              value={visibility}
              disabled={lockRouting === true}
              onChange={(event) => {
                const value = event.target.value;
                setVisibility(value === 'all' || value === 'private' || value === 'selected' ? value : '');
              }}
            >
              <option value="">Forgejo (none)</option>
              <option value="all">all</option>
              <option value="private">private</option>
            </select>
          </label>
        )}
        {kind === 'environment' ? (
          <label className="field">
            <span className="field__label">GitHub environment</span>
            <input
              value={destinationEnvironment}
              disabled={lockRouting === true}
              onChange={(event) => setDestinationEnvironment(event.target.value)}
            />
          </label>
        ) : null}
        <label className="field">
          <span className="field__label">Name prefix</span>
          <input
            value={prefix}
            onChange={(event) => setPrefix(event.target.value.toUpperCase())}
            placeholder="PROD_"
          />
          <span className="field__hint">Applied to every name at the provider; applications keep canonical names.</span>
        </label>
      </div>
      <fieldset className="field">
        <legend className="field__label">Keys</legend>
        {keys.length === 0 ? <p className="adapters__empty">This project has no keys yet.</p> : null}
        <ul className="adapters__keys" aria-label="Keys to include">
          {keys.map((key) => (
            <li key={key.id}>
              <label className="chip">
                <input type="checkbox" checked={keyIds.has(key.id)} onChange={() => toggle(key.id)} />{' '}
                <span className="mono">{key.name}</span>
              </label>
            </li>
          ))}
        </ul>
      </fieldset>
      <div className="adapters__form adapters__form--two">
        <label className="field">
          <span className="field__label">Include patterns</span>
          <input value={include} onChange={(event) => setInclude(event.target.value)} placeholder="DB_*, API_*" />
        </label>
        <label className="field">
          <span className="field__label">Exclude patterns</span>
          <input value={exclude} onChange={(event) => setExclude(event.target.value)} placeholder="*_TEST" />
        </label>
        <label className="field">
          <span className="field__label">Classification</span>
          <select
            value={classification}
            onChange={(event) => {
              const value = event.target.value;
              setClassification(value === 'secret' || value === 'config' ? value : '');
            }}
          >
            <option value="">Any</option>
            <option value="secret">Secrets only</option>
            <option value="config">Config only</option>
          </select>
        </label>
      </div>
      <p className="field__hint">
        Patterns and classification are resolved to explicit keys when you save and are not kept:
        a key created later is never added on its own.
      </p>
      <div className="panel__actions">
        <button type="submit" className="btn btn--primary" disabled={busy}>
          {busy ? 'Saving…' : 'Save'}
        </button>
        <button type="button" className="btn btn--quiet" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
      </div>
    </form>
  );
}

function TargetDetail({
  refData,
  adapter,
  targetId,
  environmentName,
  keys,
  ceremonyFor,
  feedback,
  onClose,
}: {
  readonly refData: ProjectRef;
  readonly adapter: Adapter;
  readonly targetId: string;
  readonly environmentName: (id: string) => string;
  readonly keys: readonly ProjectKey[];
  readonly ceremonyFor: (op: AdapterOperation, envs: readonly string[]) => Promise<void>;
  readonly feedback: Feedback;
  readonly onClose: () => void;
}) {
  const detail = useAdapterTarget(refData, targetId);
  const pause = usePauseAdapterTarget(refData);
  const resume = useResumeAdapterTarget(refData);
  const resync = useResyncAdapterTarget(refData);
  const remove = useRemoveAdapterTarget(refData);
  const update = useUpdateAdapterTarget(refData);
  const adopt = useAdoptAdapterNames(refData);
  const [editing, setEditing] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [ticked, setTicked] = useState<ReadonlySet<string>>(() => new Set());

  const target = detail.data?.target;
  const environmentsOfAdapter = environmentSet(adapter);
  const busy = pause.isPending || resume.isPending || resync.isPending || remove.isPending || update.isPending || adopt.isPending;

  const act = async (label: string, run: () => Promise<unknown>) => {
    try {
      await run();
      feedback.ok(label);
    } catch (error) {
      feedback.report(error);
    }
  };

  return (
    <aside className="panel adapters__detail" aria-label="Target detail">
      <div className="adapters__adapter-head">
        <h2>Target</h2>
        <button type="button" className="btn btn--quiet" onClick={onClose}>
          Close
        </button>
      </div>
      {detail.isError ? (
        <p className="alert" role="alert">
          {adapterRefusalText(detail.error)}
        </p>
      ) : null}
      {target === undefined ? (
        <p role="status">Loading…</p>
      ) : (
        <>
          <HealthChip target={target} />
          <dl className="adapters__facts">
            <dt>Environment</dt>
            <dd>{environmentName(target.environment_id)}</dd>
            <dt>Destination</dt>
            <dd className="mono">{destinationText(target)}</dd>
            <dt>Prefix</dt>
            <dd className="mono">{target.name_prefix === '' ? '—' : target.name_prefix}</dd>
            <dt>Last successful</dt>
            <dd>{revision(target.converged_revision)}</dd>
            <dt>Last attempted</dt>
            <dd>
              {revision(target.last_attempted_revision)}
              {target.last_attempted_at === null ? '' : ` at ${when(target.last_attempted_at)}`}
            </dd>
            <dt>Last error</dt>
            <dd className="adapters__error">
              {target.last_error_class === '' ? '—' : errorClassText(target.last_error_class)}
            </dd>
            <dt>Retry</dt>
            <dd>{target.retry_at === null ? '—' : when(target.retry_at)}</dd>
            <dt>Paused</dt>
            <dd>{target.paused_at === null ? 'no' : `since ${when(target.paused_at)}`}</dd>
            <dt>Attention</dt>
            <dd>
              {target.drift_attention
                ? 'The destination disagrees with the ownership ledger. Adopt the names below, or remove the target.'
                : 'none'}
            </dd>
            <dt>Generation</dt>
            <dd>{String(target.generation)}</dd>
          </dl>
          {target.failure_names.length > 0 ? (
            <p className="alert" role="alert">
              <span className="alert__glyph" aria-hidden="true">
                !
              </span>
              <span>Failed names: {target.failure_names.join(', ')}</span>
            </p>
          ) : null}
          {target.warnings.length > 0 ? (
            <ul className="adapters__warnings" aria-label="Provider warnings">
              {target.warnings.map((warning) => (
                <li key={warning}>{warning}</li>
              ))}
            </ul>
          ) : null}

          <h3>Keys</h3>
          <ul className="adapters__keys" aria-label="Member keys">
            {target.keys.map((key) => (
              <li key={key.key_id} className="chip mono">
                {key.name}
              </li>
            ))}
          </ul>

          <div className="panel__actions">
            {target.sync_status === 'paused' ? (
              <button
                type="button"
                className="btn btn--primary"
                disabled={busy}
                onClick={() =>
                  void (async () => {
                    try {
                      await ceremonyFor('adapter.sync', environmentsOfAdapter);
                      const result = await resume.mutateAsync(target.id);
                      feedback.ok(`Resumed. Catching up to revision ${String(result.revision)}.`);
                    } catch (error) {
                      feedback.report(error);
                    }
                  })()
                }
              >
                Resume
              </button>
            ) : (
              <button
                type="button"
                className="btn"
                disabled={busy}
                onClick={() => void act('Paused. Owned names stay at the destination.', () => pause.mutateAsync(target.id))}
              >
                Pause
              </button>
            )}
            <button
              type="button"
              className="btn"
              disabled={busy || target.sync_status === 'paused'}
              onClick={() =>
                void act('Resync queued.', async () => {
                  await ceremonyFor('adapter.sync', environmentsOfAdapter);
                  await resync.mutateAsync(target.id);
                })
              }
            >
              Resync
            </button>
            <button type="button" className="btn" disabled={busy} onClick={() => setEditing((open) => !open)}>
              {editing ? 'Stop editing' : 'Edit keys'}
            </button>
            <button type="button" className="btn btn--danger" disabled={busy} onClick={() => setRemoving(true)}>
              Remove
            </button>
          </div>

          {editing ? (
            <TargetForm
              title="Edit keys and prefix"
              environments={[{ id: target.environment_id, name: environmentName(target.environment_id) }]}
              keys={keys}
              initial={target}
              lockRouting
              busy={update.isPending}
              onCancel={() => setEditing(false)}
              onSubmit={async (input) => {
                try {
                  await ceremonyFor('adapter.configure', environmentsOfAdapter);
                  await update.mutateAsync({ target: target.id, expectedGeneration: target.generation, input });
                  feedback.ok('Target updated. A converge is queued.');
                  setEditing(false);
                } catch (error) {
                  feedback.report(error);
                }
              }}
            />
          ) : null}

          {detail.data !== undefined && detail.data.conflicts.length > 0 ? (
            <section aria-label="Conflicts">
              <h3>Names in the way</h3>
              <p className="field__hint">
                These exist at the destination but Hikyo does not own them. Tick exactly the ones to
                bring under management; nothing is adopted that you did not tick.
              </p>
              {detail.data.conflicts.map((artifact) => (
                <ConflictArtifact
                  key={artifact.id}
                  artifact={artifact}
                  ticked={ticked}
                  onToggle={(name) =>
                    setTicked((current) => {
                      const next = new Set(current);
                      if (next.has(name)) next.delete(name);
                      else next.add(name);
                      return next;
                    })
                  }
                  busy={adopt.isPending}
                  onAdopt={(entries) =>
                    void act('Adopted. A converge is queued.', async () => {
                      // Adoption reauthenticates over every environment the
                      // adapter serves, not just this target's: the server
                      // demands the whole set (TargetEnvironments spans every
                      // non-tombstoned sibling target), so establish it here.
                      await ceremonyFor('adapter.adopt', environmentsOfAdapter);
                      await adopt.mutateAsync({ target: target.id, artifact, entries, targetGeneration: target.generation });
                      setTicked(new Set());
                    })
                  }
                />
              ))}
            </section>
          ) : null}

          <h3>Workflow mapping</h3>
          <p className="field__hint">Names only. Applications keep canonical names.</p>
          <pre className="adapters__workflow mono">
            {detail.data?.mapping.map((entry) => `${entry.canonical_name}: \${{ ${entry.surface === 'secret' ? 'secrets' : 'vars'}.${entry.effective_name} }}`).join('\n')}
          </pre>
        </>
      )}

      {removing && target !== undefined ? (
        <RemoveDialog
          target={target}
          busy={remove.isPending}
          onCancel={() => setRemoving(false)}
          onDecide={(decision) =>
            void (async () => {
              try {
                const result = await remove.mutateAsync({ target: target.id, decision });
                setRemoving(false);
                onClose();
                if (result.orphaned.length > 0) {
                  feedback.ok(`Target removed. Orphaned names left at the destination: ${result.orphaned.join(', ')}`);
                } else if (decision === 'prune') {
                  feedback.ok('Target removed. Owned names are being scrubbed.');
                } else {
                  feedback.ok('Target removed. Custody released; the names stay at the destination.');
                }
              } catch (error) {
                feedback.report(error);
              }
            })()
          }
        />
      ) : null}
    </aside>
  );
}

function ConflictArtifact({
  artifact,
  ticked,
  onToggle,
  busy,
  onAdopt,
}: {
  readonly artifact: AdapterConflictArtifact;
  readonly ticked: ReadonlySet<string>;
  readonly onToggle: (name: string) => void;
  readonly busy: boolean;
  readonly onAdopt: (entries: AdapterConflictArtifact['entries']) => void;
}) {
  const chosen = artifact.entries.filter((entry) => ticked.has(`${entry.surface}:${entry.effective_name}`));
  return (
    <div className="adapters__artifact">
      <ul className="adapters__conflicts" aria-label={`Conflict artifact ${artifact.id}`}>
        {artifact.entries.map((entry) => {
          const name = `${entry.surface}:${entry.effective_name}`;
          return (
            <li key={name} className="adapters__conflict">
              <label>
                <input type="checkbox" checked={ticked.has(name)} onChange={() => onToggle(name)} />{' '}
                <span className="mono">{name}</span>
              </label>
            </li>
          );
        })}
      </ul>
      <div className="panel__actions">
        <button type="button" className="btn" disabled={busy || chosen.length === 0} onClick={() => onAdopt(chosen)}>
          Adopt {chosen.length} selected
        </button>
      </div>
    </div>
  );
}

/**
 * RemoveDialog is the explicit retain-or-prune decision. Neither option is
 * preselected: the human chooses what happens to the names Hikyo owns.
 */
function RemoveDialog({
  target,
  busy,
  onCancel,
  onDecide,
}: {
  readonly target: AdapterTarget;
  readonly busy: boolean;
  readonly onCancel: () => void;
  readonly onDecide: (decision: 'prune' | 'retain') => void;
}) {
  const first = useRef<HTMLInputElement>(null);
  const dialog = useModalDialog(first);
  const [decision, setDecision] = useState<'prune' | 'retain' | null>(null);
  return (
    <dialog ref={dialog} className="ceremony adapters__remove" aria-labelledby="adapters-remove-title" onCancel={onCancel}>
      <h2 className="ceremony__title" id="adapters-remove-title">
        Remove target {destinationText(target)}
      </h2>
      <p className="ceremony__lede">Decide what happens to the names Hikyo owns at the destination.</p>
      <div className="adapters__decision" role="radiogroup" aria-label="Remote names">
        <label>
          <input
            ref={first}
            type="radio"
            name="adapters-remove"
            checked={decision === 'prune'}
            onChange={() => setDecision('prune')}
          />
          <span>
            <strong>Prune</strong>: delete every ledger-owned name from the destination, sentinels last.
            Names Hikyo does not own are never touched.
          </span>
        </label>
        <label>
          <input
            type="radio"
            name="adapters-remove"
            checked={decision === 'retain'}
            onChange={() => setDecision('retain')}
          />
          <span>
            <strong>Retain</strong>: release custody and leave the names in place. They are listed as
            orphaned so you can clean them up by hand.
          </span>
        </label>
      </div>
      <div className="ceremony__actions">
        <button
          type="button"
          className="btn btn--danger"
          disabled={busy || decision === null}
          onClick={() => {
            if (decision !== null) onDecide(decision);
          }}
        >
          {busy ? 'Removing…' : 'Remove target'}
        </button>
        <button type="button" className="btn btn--quiet" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}

/**
 * AdapterCeremony runs the adapter-purpose reauthentication over an exact
 * environment set before a push-shaped act. Each environment's own policy
 * decides the factor: a sliding window accepts one authenticator code for
 * all of them, a zero-window environment takes a passkey decision of its
 * own, the same split the CLI handoff page applies.
 */
function AdapterCeremony({
  refData,
  ask,
  environmentName,
  onDone,
}: {
  readonly refData: ProjectRef;
  readonly ask: CeremonyAsk;
  readonly environmentName: (id: string) => string;
  readonly onDone: () => void;
}) {
  const first = useRef<HTMLInputElement>(null);
  const dialog = useModalDialog(first);
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [policy, setPolicy] = useState<{ sliding: string[]; passkey: string[] } | null>(null);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const sliding: string[] = [];
      const passkey: string[] = [];
      try {
        for (const environment of ask.environmentIds) {
          const window = await fetchRevealWindow({ ...refData, environment });
          if (window.totp_offered && !window.single_decision) sliding.push(environment);
          else passkey.push(environment);
        }
        if (!cancelled) setPolicy({ sliding, passkey });
      } catch (error) {
        if (!cancelled) setFailure(adapterRefusalText(error));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [ask, refData]);

  const cancel = () => {
    ask.reject(new Error('the reauthentication was cancelled'));
    onDone();
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (policy === null) return;
    setBusy(true);
    setFailure(null);
    try {
      if (policy.sliding.length > 0) {
        await runAdapterTOTPCeremony(ask.operation, [...ask.environmentIds], code.trim());
      }
      for (const environment of policy.passkey) {
        await runAdapterPasskeyCeremony({ operation: ask.operation, environmentId: environment, environmentIds: [...ask.environmentIds] });
      }
      ask.resolve();
      onDone();
    } catch (error) {
      setFailure(adapterRefusalText(error));
      setBusy(false);
    }
  };

  const verb =
    ask.operation === 'adapter.sync' ? 'push to' : ask.operation === 'adapter.adopt' ? 'adopt names for' : 'route';

  return (
    <dialog ref={dialog} className="ceremony adapters__ceremony" aria-labelledby="adapters-ceremony-title" onCancel={cancel}>
      <form onSubmit={(event) => void submit(event)}>
        <h2 className="ceremony__title" id="adapters-ceremony-title">
          Confirm it is you
        </h2>
        <p className="ceremony__lede">
          You are about to {verb} {ask.environmentIds.map(environmentName).join(', ')}. This decision is
          bound to exactly those environments and to this one act.
        </p>
        {failure !== null ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>{failure}</span>
          </p>
        ) : null}
        {policy === null && failure === null ? <p role="status">Reading environment policy…</p> : null}
        {policy !== null && policy.sliding.length > 0 ? (
          <label className="field">
            <span className="field__label">Authenticator code</span>
            <input
              ref={first}
              value={code}
              onChange={(event) => setCode(event.target.value)}
              inputMode="numeric"
              autoComplete="one-time-code"
              required
            />
          </label>
        ) : null}
        {policy !== null && policy.passkey.length > 0 ? (
          <p className="ceremony__scope">
            {policy.passkey.map(environmentName).join(', ')}{' '}
            {policy.passkey.length === 1 ? 'takes' : 'take'} a passkey decision of its own.
          </p>
        ) : null}
        <div className="ceremony__actions">
          <button type="submit" className="btn btn--primary" disabled={busy || policy === null}>
            {busy ? 'Authorising…' : 'Authorise'}
          </button>
          <button type="button" className="btn btn--quiet" onClick={cancel} disabled={busy}>
            Cancel
          </button>
        </div>
      </form>
    </dialog>
  );
}
