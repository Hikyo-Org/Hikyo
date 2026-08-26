import { useEffect, useId, useState } from 'react';
import { generatePath, Link, useNavigate, useParams } from 'react-router';

import { useOrgGrants } from '../api/access.ts';
import {
  retentionSentence,
  settingsFailureText,
  settingsOperationFailure,
  useDeleteOrg,
  useOrg,
  useOrgRetention,
  useProjectRetentions,
  useProjects,
  useRenameOrg,
  useSetOrgRetention,
  type RetentionPolicy,
  type SettingsOperation,
} from '../api/settings.ts';
import { surfaceById } from '../app/navigation.ts';
import { notifySuccess } from '../app/notifications.tsx';
import { ChromeIdentityControls } from './ChromeIdentityControls.tsx';
import { Alert, Done, JumpIndex, Panel, TypedNameConfirm } from './Sections.tsx';
import { useFeedback } from './useModalDialog.ts';

const prototypeMode = import.meta.env.MODE === 'prototype';

/**
 * Organisation settings (registry surface `org-settings`, #60; locked
 * prototype app-chrome iteration 14, retention panel from iteration 16).
 *
 * The one thing to know before reading the identity panel: renaming and
 * deleting an organisation carry `instance-config@instance`, because the
 * locked capability set holds NO org-lifecycle atom (`manage-projects` is
 * explicitly "create and delete projects"). An organisation administrator
 * therefore cannot rename or delete the organisation they administer and gets
 * the uniform 404 — a standing consequence #48 and #55 both carried to human
 * disposition rather than amending the ADR in code. The surface states it in
 * the panel instead of discovering it as a mysterious refusal.
 */
export function OrgSettings() {
  const navigate = useNavigate();
  const params = useParams();
  const org = params.org === undefined ? '' : params.org;
  const orgQuery = useOrg(org);
  const retention = useOrgRetention(org);
  const grants = useOrgGrants(org);
  const projects = useProjects(org);
  const projectPolicies = useProjectRetentions(
    org,
    projects.isSuccess ? projects.data.items : [],
  );
  const rename = useRenameOrg();
  const remove = useDeleteOrg(() => {
    notifySuccess('Organisation deleted. Sign in again to continue.');
    navigate(surfaceById('projects').path);
  });
  const setRetention = useSetOrgRetention(org);
  const nameId = useId();

  const [name, setName] = useState('');
  const feedback = useFeedback(settingsFailureText);

  const current = orgQuery.data;
  useEffect(() => {
    setName('');
  }, [org]);
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
      <h1>Org settings · {current?.name ?? 'organization'}</h1>
      <p className="page__lede">
        Organization identity and lifecycle. Access lives on its own surface; the danger zone is
        deliberately last.
      </p>

      <JumpIndex
        sections={[
          { id: 'org-identity', label: 'Identity' },
          { id: 'org-retention', label: 'Policy' },
          { id: 'org-members', label: 'Members' },
          { id: 'org-danger', label: 'Danger zone' },
        ]}
      />

      {orgQuery.isError ? (
        <Alert>
          This organisation could not be read. It may not exist, or it may not be yours to
          administer — the two answers are deliberately the same.
        </Alert>
      ) : null}
      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Done>{feedback.done}</Done> : null}

      <Panel id="org-identity" title="Identity">
        <ChromeIdentityControls
          identityId={current?.id ?? org}
          name={current?.name ?? 'organization'}
          kind="org"
        >
          <div className="field identity-name">
            <label htmlFor={nameId}>Name</label>
            <input
              id={nameId}
              value={name}
              disabled={current === undefined}
              onChange={(event) => setName(event.target.value)}
              onBlur={() => {
                if (current === undefined || name === '' || name === current.name) return;
                rename.mutate(
                  { org, name },
                  {
                    onSuccess: (result) => feedback.ok(`Renamed to ${result.name}.`),
                    onError: (error) => report('rename-org', error),
                  },
                );
              }}
            />
          </div>
        </ChromeIdentityControls>
      </Panel>

      <Panel id="org-retention" title="Policy">
        {retention.isPending ? <p role="status">Loading the retention policy…</p> : null}
        {retention.isError ? (
          <Alert>The retention policy could not be read for this organisation.</Alert>
        ) : null}
        {retention.data === undefined ? null : (
          <CompactOrgRetention
            policy={retention.data}
            busy={setRetention.isPending}
            onSave={(next) =>
              setRetention.mutate(next, {
                onSuccess: (saved) => feedback.ok(`Retention saved. ${retentionSentence(saved)}`),
                onError: (error) => report('set-org-retention', error),
              })
            }
          />
        )}
        <ProjectRetentionList
          org={org}
          projects={projects}
          policies={projectPolicies}
          cap={retention.data?.last_revisions}
        />
      </Panel>

      <Panel id="org-members" title="Members">
        <div className="settings-row">
          <div className="settings-row__copy">
            <span className="settings-row__title">Org members &amp; grants</span>
            <span className="settings-row__detail">
              {prototypeMode
                ? '4 org-scoped grant lines'
                : grants.isSuccess
                ? `${String(grants.data.count)} org-scoped grant lines`
                : 'membership listing unavailable'}
            </span>
          </div>
          <span className="settings-row__spacer" />
          <Link className="btn" to={generatePath(surfaceById('members').path, { org })}>
            open members →
          </Link>
        </div>
        <p className="settings-note">
          Entry point only: granting, revoking and inspection live on the members surface.
        </p>
      </Panel>

      <Panel id="org-danger" title="Danger zone" danger>
        {prototypeMode ? (
          <div className="settings-row">
            <div className="settings-row__copy">
              <span className="settings-row__title">Rename slug</span>
              <span className="settings-row__detail">Changing the slug changes every URL under it.</span>
            </div>
            <span className="settings-row__spacer" />
            <input
              className="settings-input settings-input--compact mono"
              aria-label="Organization slug"
              defaultValue={org}
            />
            <button type="button" className="btn" onClick={() => feedback.ok('Slug renamed (demo).')}>
              rename
            </button>
          </div>
        ) : null}
        <TypedNameConfirm
          key={current === undefined ? `pending-${org}` : current.id}
          label="Delete this organisation"
          expect={current === undefined ? null : current.name}
          action="Delete organisation"
          busy={remove.isPending}
          hint={prototypeMode
            ? <>Deletes every project, environment and value in it. Grants and audit history follow the retention policy.</>
            : <>
                Deletion never cascades into projects or their contents. Authority scoped inside an
                otherwise empty organisation is removed with it, and affected sessions are revoked.
              </>}
          onConfirm={() =>
            remove.mutate(
              { org },
              {
                onError: (error) => report('delete-org', error),
              },
            )
          }
        />
      </Panel>
    </div>
  );
}

function ProjectRetentionList({
  org,
  projects,
  policies,
  cap,
}: {
  org: string;
  projects: ReturnType<typeof useProjects>;
  policies: ReturnType<typeof useProjectRetentions>;
  cap: number | null | undefined;
}) {
  return (
    <div className="project-retention-list">
      {projects.isPending ? <p role="status">Loading project retention policies…</p> : null}
      {projects.isError ? (
        <Alert>Projects could not be read, so their retention policies are unavailable.</Alert>
      ) : null}
      {projects.isSuccess && projects.data.items.length === 0 ? (
        <p role="status">This organisation has no projects.</p>
      ) : null}
      {projects.isSuccess ? (
        <div>
          {projects.data.items.map((project) => {
            const state = policies.get(project.id);
            const policy = state?.status === 'ready' ? state.policy : undefined;
            const revisions = policy?.last_revisions;
            const capped = !policy?.inherited && cap !== null && cap !== undefined
              && revisions !== null && revisions !== undefined && revisions > cap;
            return (
              <div className="settings-row" key={project.id}>
                <div className="settings-row__copy">
                  <span className="settings-row__title mono">{project.name}</span>
                  {state === undefined || state.status === 'pending' ? (
                    <span role="status">Loading effective bounds…</span>
                  ) : state.status === 'error' ? (
                    <span className="alert" role="alert">
                      This project&apos;s retention policy could not be read.
                    </span>
                  ) : (
                    <span className="visually-hidden">{retentionSentence(state.policy)}</span>
                  )}
                </div>
                <span className="settings-row__spacer" />
                <Link
                  className={`settings-row__detail${capped ? ' text-danger' : ''}`}
                  aria-label={`Settings for ${project.name}`}
                  to={generatePath(surfaceById('project-settings').path, {
                    org,
                    project: project.id,
                  })}
                >
                  {policy === undefined
                    ? 'unavailable'
                    : policy.inherited
                      ? `inherits → ${String(revisions ?? cap ?? 'unlimited')}`
                      : capped
                        ? `custom ${String(revisions)} — capped to ${String(cap)} by org ⚠`
                        : `custom ${String(revisions ?? 'unlimited')}`}
                </Link>
              </div>
            );
          })}
        </div>
      ) : null}
      <p className="settings-note">
        Lowering the default never rewrites a project&apos;s own value — it caps it. Older values past
        a window are collected; history itself is permanent. Changes are audited.
      </p>
    </div>
  );
}

function CompactOrgRetention({
  policy,
  busy,
  onSave,
}: {
  policy: RetentionPolicy;
  busy: boolean;
  onSave: (next: RetentionPolicy) => void;
}) {
  const inputId = useId();
  const [count, setCount] = useState(String(policy.last_revisions ?? 6));

  useEffect(() => {
    setCount(String(policy.last_revisions ?? 6));
  }, [policy.last_revisions]);

  return (
    <div className="settings-row">
      <div className="settings-row__copy">
        <span className="settings-row__title">Default revision retention</span>
        <span className="settings-row__detail">
          new projects start here; projects still inheriting follow this value when it changes
        </span>
      </div>
      <span className="settings-row__spacer" />
      <label htmlFor={inputId}>keep last</label>
      <input
        id={inputId}
        className="settings-input settings-input--compact settings-input--retention"
        type="number"
        min="1"
        max="99"
        value={count}
        disabled={busy}
        aria-label="Org default revisions kept"
        onChange={(event) => setCount(event.currentTarget.value)}
        onBlur={() => {
          const revisions = Number(count);
          if (!Number.isInteger(revisions) || revisions < 1) return;
          onSave({ ...policy, last_revisions: revisions });
        }}
      />
      <span>revisions</span>
    </div>
  );
}
