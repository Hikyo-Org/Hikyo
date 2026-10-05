import type { ReactNode } from 'react';
import { Link } from 'react-router';
import { useWorkspaceContext } from '../api/transport.tsx';

/** Settings use the destination instance's session and full page, outside the workspace. */
export function WorkspaceSettingsLink({
  path, className, children,
}: {
  readonly path: string;
  readonly className?: string;
  readonly children: ReactNode;
}) {
  const workspace = useWorkspaceContext();
  return workspace === null
    ? <Link className={className} to={path}>{children}</Link>
    : <a className={className} href={new URL(path, workspace.origin).href}>{children}</a>;
}
