import { useId, type ReactNode } from 'react';
import { generatePath, Link, Navigate, useOutletContext } from 'react-router';

import { useProjects } from '../api/settings.ts';
import { surfaceById } from '../app/navigation.ts';
import { ProjectList } from './Projects.tsx';
import { ProjectQueryFeedback } from './ProjectQueryFeedback.tsx';
import { Panel } from './Sections.tsx';

/**
 * The content wells of the chrome skeleton.
 *
 * Each one is a named landing point with the page anatomy every chrome
 * surface shares (h1, lede, panel), so the navigation is testable and the
 * accessibility tree is honest. The ticket that owns a surface replaces the
 * body and leaves the route alone.
 */
function Placeholder({
  title,
  lede,
  children,
}: {
  title: string;
  lede: ReactNode;
  children?: ReactNode;
}) {
  const id = useId();
  return (
    <div className="page page--chrome">
      <h1 id={id}>{title}</h1>
      <p className="page__lede">{lede}</p>
      {children}
    </div>
  );
}

export function Overview() {
  const { activeOrgId } = useOutletContext<{ readonly activeOrgId: string }>();
  const projects = useProjects(activeOrgId);
  const readable = activeOrgId !== '' && projects.isSuccess && projects.data !== null;
  const items = activeOrgId !== '' && projects.isSuccess && projects.data !== null ? projects.data.items : [];
  // One project leaves nothing to choose between, so the landing (and the
  // return after sign-in, which lands here) opens that project's matrix.
  const only = items.length === 1 ? items[0] : undefined;
  if (only !== undefined) {
    return (
      <Navigate replace to={generatePath(surfaceById('matrix').path, { org: activeOrgId, project: only.id })} />
    );
  }
  return (
    <Placeholder
      title="Overview"
      lede={
        readable ? <>
          <Link to={surfaceById('projects').path}>Choose a project</Link> to open its environment
          matrix, values, and access surfaces.
        </> : 'Your organisation’s projects and environment matrices.'
      }
    >
      {activeOrgId === '' ? (
        <p role="status">Ask an instance administrator to invite you to an organisation.</p>
      ) : (
        <ProjectQueryFeedback query={projects} empty="No projects yet." />
      )}
      {items.length === 0 ? null : (
        <Panel id="overview-projects" title="Projects">
          <ProjectList org={activeOrgId} items={items} />
        </Panel>
      )}
    </Placeholder>
  );
}

export function NotFound() {
  return (
    <Placeholder
      title="Not found"
      lede="That page does not exist. Use the sections on the left to get back."
    />
  );
}
