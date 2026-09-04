import { useIsMutating } from '@tanstack/react-query';
import { useEffect, useId, useState } from 'react';
import { generatePath, Link, useNavigate, useParams } from 'react-router';

import {
  GIT_DEFINITIONS_NOTICE,
  parseDefinitionsSource,
  useDefinitionsSettings,
  useSetDefinitionsSettings,
  type DefinitionsSettings,
} from '../api/definitions.ts';
import {
  cloneEnvironmentRefusalText,
  createEnvironmentRefusalText,
  cryptoFailureText,
  deleteEnvironmentRefusalText,
  environmentTopologyMutationKey,
  renameEnvironmentRefusalText,
  reorderEnvironmentsRefusalText,
  settingsFailureText,
  settingsOperationFailure,
  useCloneEnvironment,
  useCreateEnvironment,
  useDeleteEnvironment,
  useDeleteProject,
  useEnvironmentSettings,
  useEnvironments,
  useOrgRetention,
  useProject,
  useProjectRetention,
  useReencryptProject,
  useRenameEnvironment,
  useRenameProject,
  useReorderEnvironments,
  useRotateDek,
  useSetEnvironmentSettings,
  useSetProjectRetention,
  type ProjectRetentionPolicy,
  type RetentionPolicy,
  type SettingsOperation,
} from '../api/settings.ts';
import { surfaceById } from '../app/navigation.ts';
import { ChromeIdentityControls } from './ChromeIdentityControls.tsx';
import { Alert, ConsequencesDialog, Done, JumpIndex, Panel, TypedNameConfirm } from './Sections.tsx';
import { useFeedback } from './useModalDialog.ts';
import { useReencryptDrain } from './useReencryptDrain.ts';

const prototypeMode = import.meta.env.MODE === 'prototype';

/**
 * Project settings (registry surface `project-settings`, #60; locked prototype
 * app-chrome iteration 15, retention panel from iteration 16).
 *
 * Definitions governance does live here. Definition authoring does not yet
 * exist in the web app, so this is the only surface that needs the persistent
 * Git-mode explanation; matrix editing remains available because it writes
 * values, never definitions.
 */
export function ProjectSettings() {
  const navigate = useNavigate();
  const params = useParams();
  const org = params.org === undefined ? '' : params.org;
  const project = params.project === undefined ? '' : params.project;
  const projectQuery = useProject(org, project);
  const environments = useEnvironments(org, project);
  const orgRetention = useOrgRetention(org);
  const projectRetention = useProjectRetention(org, project);
  const definitionsSettings = useDefinitionsSettings(org, project);
  const setDefinitionsSettings = useSetDefinitionsSettings(org, project);
  const rename = useRenameProject(org);
  const remove = useDeleteProject(org, () => navigate(surfaceById('projects').path));
  const nameId = useId();

  const [name, setName] = useState('');
  const feedback = useFeedback(settingsFailureText);

  const current = projectQuery.data;
  useEffect(() => {
    setName('');
  }, [org, project]);
  useEffect(() => {
    if (current !== undefined) {
      setName(current.name);
    }
  }, [current]);

  const report = (operation: SettingsOperation, error: unknown) => {
    feedback.report(settingsOperationFailure(operation, error));
  };

  return (
    <div className="page page--chrome">
      <h1>Project settings · {current?.name ?? 'project'}</h1>
      <p className="page__lede">
        Project identity and metadata. Access management is its own surface: one entry point, no
        second permission editor here.
      </p>

      <JumpIndex
        sections={[
          { id: 'project-identity', label: 'Identity' },
          { id: 'project-metadata', label: 'Metadata' },
          { id: 'project-environments', label: 'Environments' },
          { id: 'project-policy', label: 'Policy' },
          { id: 'project-access', label: 'Access' },
          ...(prototypeMode ? [] : [{ id: 'project-keys', label: 'Keys & crypto' }]),
          { id: 'project-danger', label: 'Danger zone' },
        ]}
      />

      {projectQuery.isError ? (
        <Alert>
          This project could not be read. It may not exist, or it may not be yours to reach — the
          two answers are deliberately the same.
        </Alert>
      ) : null}
      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Done>{feedback.done}</Done> : null}

      <Panel id="project-identity" title="Identity">
        <ChromeIdentityControls
          identityId={current?.id ?? project}
          name={current?.name ?? 'project'}
          kind="project"
        />
      </Panel>

      <Panel id="project-metadata" title="Metadata" tight>
        <div className="settings-grid">
        <div className="field">
          <label htmlFor={nameId}>Name</label>
          <input
            id={nameId}
            value={name}
            disabled={current === undefined}
            onChange={(event) => setName(event.target.value)}
            onBlur={() => {
              if (current === undefined || name === '' || name === current.name) return;
              rename.mutate(
                { project, name },
                {
                  onSuccess: (result) => feedback.ok(`Renamed to ${result.name}.`),
                  onError: (error) => report('rename-project', error),
                },
              );
            }}
          />
        </div>
          <div className="field">
            <label htmlFor={`${nameId}-description`}>Description</label>
            <input
              id={`${nameId}-description`}
              value={prototypeMode ? 'Demo project for the spec prototypes' : ''}
              placeholder={prototypeMode ? undefined : 'Description is not available in the API'}
              disabled={!prototypeMode}
              readOnly
              aria-readonly="true"
            />
          </div>
        </div>
      </Panel>

      <Panel id="project-environments" title="Environments">
        <p className="settings-note">
          An environment is a named column of the matrix — every key gets its own explicit value in
          each one. A project starts with none; add the first here before declaring keys.
        </p>
        {/* The environments-read failure is surfaced once, by the Policy panel
            below (a visual-contract test pins the alert there); both panels read
            the same query, so repeating it here would show it twice. */}
        {environments.isPending ? <p role="status">Loading environments…</p> : null}
        {environments.isSuccess && environments.data.items.length === 0 ? (
          <p role="status">This project holds no environments yet.</p>
        ) : null}
        {environments.isSuccess && environments.data.items.length > 0 ? (
          <ul className="factors">
            {environments.data.items.map((environment) => (
              <li className="factor" key={environment.id}>
                <strong className="mono">{environment.name}</strong>
                <EnvironmentLifecycleActions
                  org={org}
                  project={project}
                  environment={environment}
                  environments={environments.data.items}
                  onDone={feedback.ok}
                />
              </li>
            ))}
          </ul>
        ) : null}
        <NewEnvironment
          org={org}
          project={project}
          disabled={current === undefined}
          onDone={feedback.ok}
        />
      </Panel>

      <Panel id="project-policy" title="Policy">
        {environments.isError ? (
          <Alert>This project&apos;s environments could not be read.</Alert>
        ) : null}
        {environments.isPending ? <p role="status">Loading environment policies…</p> : null}
        {environments.isSuccess ? (
          <EnvironmentPolicy
            org={org}
            project={project}
            environments={environments.data.items}
            onDone={feedback.ok}
            onError={(error) => report('set-environment-settings', error)}
          />
        ) : null}
        {definitionsSettings.isPending ? (
          <p role="status">Loading definitions policy…</p>
        ) : null}
        {definitionsSettings.isError ? (
          <Alert>This project&apos;s definitions policy could not be read.</Alert>
        ) : null}
        {definitionsSettings.data === undefined ? null : (
          <DefinitionsPolicy
            settings={definitionsSettings.data}
            busy={setDefinitionsSettings.isPending}
            onApply={(source) =>
              setDefinitionsSettings.mutate(source, {
                onSuccess: (result) =>
                  feedback.ok(
                    result.definitions_source === 'git'
                      ? 'Definitions are now managed through Git apply.'
                      : 'Definitions can now be edited through the database-backed interfaces.',
                  ),
                onError: (error) => report('set-definitions-settings', error),
              })
            }
          />
        )}
        <div id="project-retention">
        {orgRetention.isPending ? <p role="status">Loading the organisation cap…</p> : null}
        {orgRetention.isError ? (
          <Alert>The organisation retention cap could not be read.</Alert>
        ) : null}
        {projectRetention.isError ? (
          <Alert>This project&apos;s retention policy could not be read.</Alert>
        ) : null}
        {projectRetention.data === undefined || orgRetention.data === undefined ? null : (
          <CompactProjectRetention
            org={org}
            project={project}
            policy={projectRetention.data}
            orgPolicy={orgRetention.data}
            onDone={feedback.ok}
            onError={(error) => report('set-project-retention', error)}
          />
        )}
        </div>
        <p className="settings-note">
          These are the project-settings capability&apos;s contents: the guards on the definitions
          editor live apart from the editor they restrain.
        </p>
      </Panel>

      <Panel id="project-access" title="Access">
        <div className="settings-row">
          <div className="settings-row__copy">
            <span className="settings-row__title">Members</span>
            <span className="settings-row__detail">
              {prototypeMode
                ? '7 grant lines on this project'
                : 'Open the members surface to inspect project grants'}
            </span>
          </div>
          <span className="settings-row__spacer" />
          <Link
            className="btn"
            to={`${generatePath(surfaceById('members').path, { org })}?project=${encodeURIComponent(project)}`}
          >
            open members →
          </Link>
        </div>
        <p className="settings-note">
          Entry point only: granting, revoking and inspection live on the members surface.
        </p>
      </Panel>

      {prototypeMode ? null : (
        <Panel id="project-keys" title="Keys &amp; crypto">
          <ProjectCryptoMaintenance
            org={org}
            project={project}
            disabled={current === undefined}
            onDone={feedback.ok}
          />
        </Panel>
      )}

      <Panel id="project-danger" title="Danger zone" danger>
        {prototypeMode ? (
          <div className="settings-row">
            <div className="settings-row__copy">
              <span className="settings-row__title">Rename slug</span>
              <span className="settings-row__detail">Changing the slug changes every URL under it.</span>
            </div>
            <span className="settings-row__spacer" />
            <input
              className="settings-input settings-input--compact mono"
              aria-label="Project slug"
              defaultValue={project}
            />
            <button type="button" className="btn" onClick={() => feedback.ok('Slug renamed (demo).')}>
              rename
            </button>
          </div>
        ) : null}
        <TypedNameConfirm
          key={current === undefined ? `pending-${project}` : current.id}
          label="Delete this project"
          expect={current === undefined ? null : current.name}
          action="Delete project"
          busy={remove.isPending}
          hint={prototypeMode
            ? <>Deletes every environment and value in it. Grants and audit history follow the retention policy.</>
            : <>
                Deletion never cascades: a project that still holds any environment is refused.
                Delete its environments first, deliberately and one at a time.
              </>}
          onConfirm={() =>
            remove.mutate(
              { project },
              {
                onError: (error) => report('delete-project', error),
              },
            )
          }
        />
      </Panel>
    </div>
  );
}

/**
 * ProjectCryptoMaintenance exposes the project-scoped half of the remotely
 * operable cryptographic jobs (#503): rotate this project's DEK, then walk its
 * ciphertext onto the new version. The two are paired — a DEK rotation is
 * incomplete until the re-encryption runs — and both are the same
 * grant-evaluated network operations the CLI verbs call.
 */
function ProjectCryptoMaintenance({
  org,
  project,
  disabled,
  onDone,
}: {
  org: string;
  project: string;
  disabled: boolean;
  onDone: (text: string) => void;
}) {
  const dek = useRotateDek();
  const reencrypt = useReencryptProject(org, project);
  const titleId = useId();
  const [confirmRotate, setConfirmRotate] = useState(false);
  const [dialogFailure, setDialogFailure] = useState<string | null>(null);

  const drain = useReencryptDrain(reencrypt, { operation: 'reencrypt-project', noun: 'Project', onDone });

  return (
    <>
      <p className="settings-note">
        Both jobs run over the network, guarded by capability plus session second-factor assurance;
        no key material crosses the wire. A DEK rotation is incomplete until the re-encryption below
        walks the project&apos;s ciphertext forward.
      </p>
      <div className="settings-row">
        <div className="settings-row__copy">
          <span className="settings-row__title">Data-encryption key (project)</span>
          <span className="settings-row__detail">
            Appends a new DEK version for this project. New writes seal under it immediately; existing
            ciphertext stays readable until you re-encrypt.
          </span>
        </div>
        <span className="settings-row__spacer" />
        <code className="instance-cli">$ hikyo rotate-dek --scope project</code>
        <button type="button" className="btn" disabled={disabled} onClick={() => { setDialogFailure(null); setConfirmRotate(true); }}>
          Rotate the project DEK
        </button>
      </div>
      <div className="settings-row">
        <div className="settings-row__copy">
          <span className="settings-row__title">Project re-encryption</span>
          <span className="settings-row__detail">
            Walks every ciphertext in this project onto the active DEK version and retires the
            superseded ones. Chunked and resumable: safe to re-run, and complete once it moves no rows.
          </span>
        </div>
        <span className="settings-row__spacer" />
        <code className="instance-cli">$ hikyo reencrypt --project</code>
        <button type="button" className="btn" disabled={disabled || drain.running} onClick={drain.run}>
          {drain.running ? 'Re-encrypting…' : 'Re-encrypt the project'}
        </button>
      </div>
      {drain.running ? <p role="status" className="field__hint">Re-encrypting… run {drain.runs}, {String(drain.total)} row{drain.total === 1n ? '' : 's'} moved so far. Safe to leave and resume later.</p> : null}
      {drain.failure === null ? null : <Alert>{drain.failure}</Alert>}

      {confirmRotate ? (
        <ConsequencesDialog
          titleId={titleId}
          title="Rotate this project's DEK?"
          confirmLabel="Rotate the DEK"
          busyLabel="Rotating the project DEK…"
          busy={dek.isPending}
          failure={dialogFailure}
          onCancel={() => { setDialogFailure(null); setConfirmRotate(false); }}
          onConfirm={() => {
            setDialogFailure(null);
            dek.mutate({ scope: 'project', org, project }, {
              onSuccess: (result) => {
                onDone(`This project's DEK was rotated (version ${String(result.key_version)}). New writes seal under it; existing ciphertext stays readable until you run the project re-encryption to complete the rotation.`);
                setConfirmRotate(false);
              },
              onError: (error) => setDialogFailure(cryptoFailureText(error, 'rotate-dek')),
            });
          }}
        >
          <p>
            A new DEK version is appended for this project. New writes seal under it immediately;
            existing ciphertext stays readable under the previous version until the project
            re-encryption walks it forward. The rotation is incomplete until you run that
            re-encryption.
          </p>
        </ConsequencesDialog>
      ) : null}
    </>
  );
}

function DefinitionsPolicy({
  settings,
  busy,
  onApply,
}: {
  settings: DefinitionsSettings;
  busy: boolean;
  onApply: (source: DefinitionsSettings['definitions_source']) => void;
}) {
  const sourceId = useId();
  const [source, setSource] = useState(settings.definitions_source);

  useEffect(() => {
    setSource(settings.definitions_source);
  }, [settings.definitions_source]);

  return (
    <div className="settings-row">
      <div className="settings-row__copy">
        <span className="settings-row__title">Definitions source</span>
        <span className="settings-row__detail">
          definitions edited in the UI; switch to git for a review gate
        </span>
      </div>
      <span className="settings-row__spacer" />
      <label className="visually-hidden" htmlFor={sourceId}>Definitions source</label>
      <select
        id={sourceId}
        className="settings-select mono"
        value={source}
        disabled={busy}
        onChange={(event) => {
          const next = parseDefinitionsSource(event.currentTarget.value);
          setSource(next);
          onApply(next);
        }}
      >
        <option value="db">db</option>
        <option value="git">git</option>
      </select>
      {settings.definitions_source === 'git' ? <Alert>{GIT_DEFINITIONS_NOTICE}</Alert> : null}
    </div>
  );
}

/**
 * NewEnvironment is the create affordance the empty matrix points to
 * ("Project settings › New environment"). The refusal stays local to the form
 * so it lands beside the input that produced it; success is reported through
 * the page feedback the query invalidation then fills with the new row.
 */
function NewEnvironment({
  org,
  project,
  disabled,
  onDone,
}: {
  org: string;
  project: string;
  disabled: boolean;
  onDone: (text: string) => void;
}) {
  const create = useCreateEnvironment(org, project);
  // Any in-flight topology change (a rename/reorder/clone/delete on a row, or
  // another create) disables creation too: all these writes share one mutation
  // key, so creating mid-reorder cannot race the ordered set the server holds.
  const topologyBusy =
    useIsMutating({ mutationKey: environmentTopologyMutationKey(org, project) }) > 0;
  const nameId = useId();
  const [name, setName] = useState('');
  const [failure, setFailure] = useState<string | null>(null);
  const trimmed = name.trim();

  const submit = () => {
    if (trimmed === '') return;
    setFailure(null);
    create.mutate(
      { name: trimmed },
      {
        onSuccess: (result) => {
          setName('');
          onDone(`Environment ${result.name} created.`);
        },
        onError: (error) => setFailure(createEnvironmentRefusalText(error)),
      },
    );
  };

  return (
    <>
      <form
        className="settings-row"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <div className="settings-row__copy">
          <span className="settings-row__title">New environment</span>
          <span className="settings-row__detail">
            a lowercase name like <span className="mono">production</span> or{' '}
            <span className="mono">staging</span>
          </span>
        </div>
        <span className="settings-row__spacer" />
        <label className="visually-hidden" htmlFor={nameId}>
          New environment name
        </label>
        <input
          id={nameId}
          className="settings-input settings-input--compact mono"
          value={name}
          placeholder="production"
          disabled={disabled || topologyBusy}
          onChange={(event) => setName(event.target.value)}
        />
        <button
          type="submit"
          className="btn btn--primary"
          disabled={disabled || topologyBusy || trimmed === ''}
        >
          Create
        </button>
      </form>
      {failure === null ? null : <Alert>{failure}</Alert>}
    </>
  );
}

/**
 * One environment-lifecycle refusal, tagged with the action that raised it so
 * the alert renders beside that control rather than once at the top.
 */
type LifecycleFailure = {
  readonly scope: 'rename' | 'order' | 'clone' | 'delete';
  readonly text: string;
};

/**
 * EnvironmentLifecycleActions gathers a project's environment-topology
 * mutations under one per-row disclosure: rename, whole-set reorder,
 * clone-at-creation, and typed-name delete. Each keeps its refusal beside the
 * control that raised it; a success is announced through the page feedback the
 * query invalidation then fills with the changed list.
 */
export function EnvironmentLifecycleActions({
  org,
  project,
  environment,
  environments,
  onDone,
}: {
  readonly org: string;
  readonly project: string;
  readonly environment: { readonly id: string; readonly name: string };
  readonly environments: readonly { readonly id: string; readonly name: string }[];
  readonly onDone: (text: string) => void;
}) {
  const rename = useRenameEnvironment(org, project);
  const remove = useDeleteEnvironment(org, project, () =>
    onDone(`Environment ${environment.name} deleted.`),
  );
  const reorder = useReorderEnvironments(org, project);
  const clone = useCloneEnvironment(org, project);
  // Shared across every row: any topology write in flight (this row's or a
  // sibling's) disables all of them, so two open disclosures cannot submit
  // whole-set reorders from the same stale snapshot and silently undo each
  // other. All four hooks plus create carry one mutation key for this count.
  const busy =
    useIsMutating({ mutationKey: environmentTopologyMutationKey(org, project) }) > 0;
  const renameId = useId();
  const cloneId = useId();
  const [renameName, setRenameName] = useState('');
  const [cloneName, setCloneName] = useState('');
  const [failure, setFailure] = useState<LifecycleFailure | null>(null);

  const index = environments.findIndex((candidate) => candidate.id === environment.id);
  if (index === -1) {
    throw new Error(`environment ${environment.id} is missing from its project order`);
  }

  // Reorder is a whole-set replacement: send every id once, with this
  // environment and its neighbour swapped. A partial list would drop the
  // environments it omits.
  const move = (offset: -1 | 1) => {
    const target = environments[index + offset];
    if (target === undefined) return;
    setFailure(null);
    reorder.mutate(
      {
        environmentIds: environments.map((candidate) => {
          if (candidate.id === environment.id) return target.id;
          if (candidate.id === target.id) return environment.id;
          return candidate.id;
        }),
      },
      {
        onSuccess: () =>
          onDone(`Environment ${environment.name} moved ${offset === -1 ? 'up' : 'down'}.`),
        onError: (error) =>
          setFailure({ scope: 'order', text: reorderEnvironmentsRefusalText(error) }),
      },
    );
  };

  return (
    <details className="environment-lifecycle">
      <summary>Manage {environment.name}</summary>

      <form
        className="settings-row"
        onSubmit={(event) => {
          event.preventDefault();
          const name = renameName.trim();
          if (name === '' || name === environment.name) return;
          setFailure(null);
          rename.mutate(
            { environment: environment.id, name },
            {
              onSuccess: (renamed) => {
                setRenameName('');
                onDone(`Environment ${environment.name} renamed to ${renamed.name}.`);
              },
              onError: (error) =>
                setFailure({ scope: 'rename', text: renameEnvironmentRefusalText(error) }),
            },
          );
        }}
      >
        <div className="settings-row__copy">
          <span className="settings-row__title">Rename</span>
          <span className="settings-row__detail">give {environment.name} a new name</span>
        </div>
        <span className="settings-row__spacer" />
        <label className="visually-hidden" htmlFor={renameId}>
          New name for {environment.name}
        </label>
        <input
          id={renameId}
          name="rename-environment"
          className="settings-input settings-input--compact mono"
          value={renameName}
          disabled={busy}
          onChange={(event) => setRenameName(event.target.value)}
        />
        <button
          type="submit"
          className="btn"
          disabled={busy || renameName.trim() === '' || renameName.trim() === environment.name}
        >
          Rename environment
        </button>
      </form>
      {failure?.scope === 'rename' ? <Alert>{failure.text}</Alert> : null}

      <div className="settings-row">
        <div className="settings-row__copy">
          <span className="settings-row__title">Order</span>
          <span className="settings-row__detail">
            move sends the complete project order as one atomic change
          </span>
        </div>
        <span className="settings-row__spacer" />
        <button
          type="button"
          className="btn"
          aria-label={`Move ${environment.name} up`}
          disabled={busy || index === 0}
          onClick={() => move(-1)}
        >
          Move up
        </button>
        <button
          type="button"
          className="btn"
          aria-label={`Move ${environment.name} down`}
          disabled={busy || index === environments.length - 1}
          onClick={() => move(1)}
        >
          Move down
        </button>
      </div>
      {failure?.scope === 'order' ? <Alert>{failure.text}</Alert> : null}

      <form
        className="settings-row"
        onSubmit={(event) => {
          event.preventDefault();
          const name = cloneName.trim();
          if (name === '') return;
          setFailure(null);
          clone.mutate(
            { sourceEnvironment: environment.id, name },
            {
              onSuccess: (result) => {
                setCloneName('');
                const copied = result.copied.length;
                const omitted = result.uncopied_secrets.length;
                onDone(
                  `Environment ${environment.name} cloned to ${result.environment.name}. Copied ${copied} ${copied === 1 ? 'value' : 'values'}; ${omitted} ${omitted === 1 ? 'secret' : 'secrets'} could not be copied.`,
                );
              },
              onError: (error) =>
                setFailure({ scope: 'clone', text: cloneEnvironmentRefusalText(error) }),
            },
          );
        }}
      >
        <div className="settings-row__copy">
          <span className="settings-row__title">Clone</span>
          <span className="settings-row__detail">
            copy {environment.name} into a new environment
          </span>
        </div>
        <span className="settings-row__spacer" />
        <label className="visually-hidden" htmlFor={cloneId}>
          Clone {environment.name} into
        </label>
        <input
          id={cloneId}
          name="clone-environment"
          className="settings-input settings-input--compact mono"
          value={cloneName}
          disabled={busy}
          onChange={(event) => setCloneName(event.target.value)}
        />
        <button type="submit" className="btn" disabled={busy || cloneName.trim() === ''}>
          Clone environment
        </button>
      </form>
      {failure?.scope === 'clone' ? <Alert>{failure.text}</Alert> : null}

      <TypedNameConfirm
        key={environment.id}
        label={`Delete ${environment.name}`}
        expect={environment.name}
        action="Delete environment"
        busy={busy}
        hint={
          <>
            This permanently deletes the environment and its values, drafts, revision history,
            pins, and snapshots. Type its name exactly to continue.
          </>
        }
        onConfirm={() => {
          setFailure(null);
          remove.mutate(
            { environment: environment.id },
            {
              onError: (error) =>
                setFailure({ scope: 'delete', text: deleteEnvironmentRefusalText(error) }),
            },
          );
        }}
      />
      {failure?.scope === 'delete' ? <Alert>{failure.text}</Alert> : null}
    </details>
  );
}

function EnvironmentPolicy({
  org,
  project,
  environments,
  onDone,
  onError,
}: {
  org: string;
  project: string;
  environments: readonly { id: string; name: string }[];
  onDone: (text: string) => void;
  onError: (error: unknown) => void;
}) {
  const protection = useEnvironmentSettings(org, project, environments);
  const save = useSetEnvironmentSettings(org, project);
  const windowId = useId();
  const [windowValue, setWindowValue] = useState('900');

  if (environments.length === 0) {
    return <p role="status">This project holds no environments yet.</p>;
  }

  return (
    <>
      <div className="settings-row">
        <div className="settings-row__copy">
          <span className="settings-row__title">Protected environments</span>
          <span className="settings-row__detail">
            a protected environment caps the reveal window at 0: passkey per disclosure, no TOTP
          </span>
        </div>
        <span className="settings-row__spacer" />
        {environments.map((environment) => {
          const state = protection.get(environment.id);
          const ready = state?.status === 'ready';
          const protectedFlag = ready && state.protected;
          return (
            <span className="environment-policy-control" key={environment.id}>
              <button
                type="button"
                className={`settings-tag${protectedFlag ? ' settings-tag--danger' : ''}`}
                disabled={!ready || save.isPending}
                aria-pressed={protectedFlag}
                onClick={() =>
                  save.mutate(
                    {
                      environment: environment.id,
                      protectedFlag: !protectedFlag,
                      reauthWindowSeconds: protectedFlag ? Number(windowValue) : 0,
                    },
                    {
                      onSuccess: (saved) =>
                        onDone(
                          saved.protected
                            ? `${environment.name} is now protected.`
                            : `${environment.name} is no longer protected.`,
                        ),
                      onError,
                    },
                  )
                }
              >
                {protectedFlag ? '🔒 ' : ''}{environment.name}
              </button>
              {state?.status === 'unreadable' ? (
                <span role="status">This environment&apos;s policy could not be read.</span>
              ) : null}
              {state?.status === 'forbidden' ? (
                <Alert>You are not permitted to read this environment&apos;s policy.</Alert>
              ) : null}
              {state?.status === 'error' ? (
                <Alert>This environment&apos;s policy failed to load. Reload to try again.</Alert>
              ) : null}
            </span>
          );
        })}
      </div>
      <div className="settings-row">
        <div className="settings-row__copy">
          <span className="settings-row__title">Reveal reauth window</span>
          <span className="settings-row__detail">
            how long a successful reveal reauth stands for further disclosures
          </span>
        </div>
        <span className="settings-row__spacer" />
        <label className="visually-hidden" htmlFor={windowId}>Reveal reauth window</label>
        <select
          id={windowId}
          className="settings-select"
          value={windowValue}
          disabled={save.isPending}
          onChange={(event) => {
            const next = event.currentTarget.value;
            setWindowValue(next);
            const editable = environments.filter((environment) => {
              const state = protection.get(environment.id);
              return state?.status === 'ready' && !state.protected;
            });
            editable.forEach((environment, index) =>
              save.mutate(
                {
                  environment: environment.id,
                  protectedFlag: false,
                  reauthWindowSeconds: Number(next),
                },
                {
                  onSuccess: () => {
                    if (index === editable.length - 1) {
                      onDone(`Reveal reauthentication window changed to ${next} seconds.`);
                    }
                  },
                  onError,
                },
              ),
            );
          }}
        >
          <option value="0">0 (every disclosure)</option>
          <option value="300">5m</option>
          <option value="900">15m</option>
          <option value="3600">60m</option>
        </select>
      </div>
    </>
  );
}

function CompactProjectRetention({
  org,
  project,
  policy,
  orgPolicy,
  onDone,
  onError,
}: {
  org: string;
  project: string;
  policy: ProjectRetentionPolicy;
  orgPolicy: RetentionPolicy;
  onDone: (text: string) => void;
  onError: (error: unknown) => void;
}) {
  const save = useSetProjectRetention(org, project);
  const [customRevisions, setCustomRevisions] = useState(
    String(policy.last_revisions ?? orgPolicy.last_revisions ?? 6),
  );
  const effective = policy.last_revisions ?? orgPolicy.last_revisions ?? 6;
  // Lowering the org default never rewrites a project's own number — it caps
  // what that number can deliver. The org list already names this state; a
  // project reading "custom 20" while it actually keeps 10 disagrees with its
  // own organisation about the same fact.
  const cap = orgPolicy.last_revisions;
  const own = policy.last_revisions;
  const capped =
    !policy.inherited
    && cap !== null
    && cap !== undefined
    && own !== null
    && own !== undefined
    && own > cap;

  const apply = (inherited: boolean, revisions: number) => {
    save.mutate(
      {
        inherited,
        maxAgeSeconds:
          inherited ? null : policy.max_age_seconds ?? orgPolicy.max_age_seconds ?? null,
        lastRevisions: inherited ? null : revisions,
      },
      {
        onSuccess: () => onDone('Revision retention saved.'),
        onError,
      },
    );
  };

  return (
    <>
      <div className="settings-row">
        <div className="settings-row__copy">
          <span className="settings-row__title">Revision retention</span>
          <span className={`settings-row__detail${capped ? ' text-danger' : ''}`}>
            {policy.inherited
              ? `inherits the org default — values kept for the last ${String(effective)} revisions per environment; follows org changes`
              : capped
                ? `custom ${String(own)}, capped to ${String(cap)} by the org — a project may never keep more than the org allows`
                : `custom ${String(effective)} — values kept for the last ${String(effective)} revisions per environment; detached from later org changes`}
          </span>
        </div>
        <span className="settings-row__spacer" />
        <select
          className="settings-select"
          aria-label="Retention mode"
          value={policy.inherited ? 'inherit' : 'custom'}
          disabled={save.isPending}
          onChange={(event) => {
            const inherit = event.currentTarget.value === 'inherit';
            apply(inherit, Number(customRevisions));
          }}
        >
          <option value="inherit">inherit org ({String(orgPolicy.last_revisions ?? 6)})</option>
          <option value="custom">custom</option>
        </select>
        {policy.inherited ? null : (
          <input
            className={`settings-input settings-input--compact${capped ? ' settings-input--capped' : ''}`}
            type="number"
            min="1"
            max={orgPolicy.last_revisions ?? undefined}
            aria-label="Revisions kept per environment"
            value={customRevisions}
            disabled={save.isPending}
            onChange={(event) => setCustomRevisions(event.currentTarget.value)}
            onBlur={() => apply(false, Number(customRevisions))}
          />
        )}
      </div>
      <p className="settings-note">
        Older revisions lose their values as collection runs (pinned ones always stay); who-changed-what
        history is permanent. A custom value may not exceed the org default. Changes are audited.
      </p>
    </>
  );
}
