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
  createEnvironmentRefusalText,
  retentionSentence,
  settingsFailureText,
  settingsOperationFailure,
  useCreateEnvironment,
  useDeleteProject,
  useEnvironmentSettings,
  useEnvironments,
  useOrgRetention,
  useProject,
  useProjectRetention,
  useRenameProject,
  useSetEnvironmentSettings,
  useSetProjectRetention,
  type ProjectRetentionPolicy,
  type RetentionPolicy,
  type SettingsOperation,
} from '../api/settings.ts';
import { surfaceById } from '../app/navigation.ts';
import { ChromeIdentityControls } from './ChromeIdentityControls.tsx';
import { Alert, Done, JumpIndex, Panel, TypedNameConfirm } from './Sections.tsx';
import { useFeedback } from './useModalDialog.ts';

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

      <Panel id="project-metadata" title="Metadata">
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
            <span className="settings-row__title">Members &amp; grants</span>
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
          disabled={disabled || create.isPending}
          onChange={(event) => setName(event.target.value)}
        />
        <button
          type="submit"
          className="btn btn--primary"
          disabled={disabled || create.isPending || trimmed === ''}
        >
          Create
        </button>
      </form>
      {failure === null ? null : <Alert>{failure}</Alert>}
    </>
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
