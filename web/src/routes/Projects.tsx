import { useId, useState, type FormEvent } from 'react';
import { generatePath, Link, useOutletContext } from 'react-router';

import { createProjectRefusalText, useCreateProject, useProjects } from '../api/settings.ts';
import { surfaceById } from '../app/navigation.ts';
import { Alert, JumpIndex, Panel } from './Sections.tsx';

/** Projects is a real data surface; keeping it out of Placeholder preserves the chrome skeleton seam. */
export function Projects() {
  const { activeOrgId } = useOutletContext<{ readonly activeOrgId: string }>();
  const projects = useProjects(activeOrgId);
  const items = projects.data?.items ?? [];

  return (
    <div className="page page--chrome projects">
      <h1>Projects</h1>
      <p className="page__lede">
        Every project in this organisation. Open a project's matrix to work in it, or its settings
        to shape it.
      </p>
      {activeOrgId === '' ? (
        <Panel id="projects-none" title="No organisation yet">
          <p role="status" className="hint-wrap">
            Ask an instance administrator to invite you to an organisation. They can create an
            organisation under Instance settings and manage access under Members. A change to
            your access ends the current session.
          </p>
        </Panel>
      ) : (
        <>
          <JumpIndex
            sections={[
              { id: 'projects-list', label: 'All projects' },
              { id: 'projects-new', label: 'New project' },
            ]}
          />
          <Panel id="projects-list" title="Projects">
            {projects.isPending ? <p role="status">Loading projects…</p> : null}
            {projects.isError ? (
              <Alert>Projects could not be loaded. Reload to try again.</Alert>
            ) : null}
            {projects.isSuccess && items.length === 0 ? (
              <p role="status">No projects yet. Create the first one below.</p>
            ) : null}
            {items.length === 0 ? null : <ProjectList org={activeOrgId} items={items} />}
          </Panel>
          <NewProjectForm org={activeOrgId} />
        </>
      )}
    </div>
  );
}

/** One list of project rows with their matrix and settings links, shared with the overview. */
export function ProjectList({
  org,
  items,
}: {
  readonly org: string;
  readonly items: readonly { readonly id: string; readonly name: string }[];
}) {
  return (
    <ul className="projects__list">
      {items.map((project) => (
        <li key={project.id}>
          <div>
            <strong>{project.name}</strong>
          </div>
          <span className="projects__actions">
            <Link
              className="btn"
              to={generatePath(surfaceById('matrix').path, { org, project: project.id })}
            >
              Open matrix
            </Link>
            {/* Project settings addresses ONE project, so this is where it
                is reached from: no static sidebar entry could know which
                project to mean. */}
            <Link
              className="btn"
              aria-label={`Settings for ${project.name}`}
              to={generatePath(surfaceById('project-settings').path, { org, project: project.id })}
            >
              Settings
            </Link>
          </span>
        </li>
      ))}
    </ul>
  );
}

/**
 * NewProjectForm creates a project in the active organisation.
 *
 * Success and refusal never rest on colour: the created row is announced
 * through `role="status"` and a refusal is `role="alert"` text carrying the
 * glyph, naming the `manage-projects` capability the server checked.
 */
export function NewProjectForm({ org }: { readonly org: string }) {
  const create = useCreateProject(org);
  const nameId = useId();
  const [name, setName] = useState('');

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed === '') {
      return;
    }
    create.mutate({ name: trimmed }, { onSuccess: () => setName('') });
  };

  return (
    <form
      className="card panel projects__new"
      id="projects-new"
      tabIndex={-1}
      onSubmit={onSubmit}
      noValidate
      aria-labelledby="new-project-title"
    >
      <h2 id="new-project-title">New project</h2>
      {create.isError ? <Alert>{createProjectRefusalText(create.error)}</Alert> : null}
      {create.isSuccess ? (
        <p role="status">Project {create.data.name} created.</p>
      ) : null}
      <div className="field">
        <label htmlFor={nameId}>Project name</label>
        <input
          id={nameId}
          name="name"
          required
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
      </div>
      <button
        className="btn btn--primary"
        type="submit"
        disabled={create.isPending || name.trim() === ''}
      >
        {create.isPending ? 'Creating…' : 'Create project'}
      </button>
    </form>
  );
}
