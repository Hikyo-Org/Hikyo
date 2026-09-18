import type { ReactElement } from 'react';
import { Link, useParams } from 'react-router';

import { useSystemScope, type SystemScopeSurface } from '../api/selfConfig.ts';
import { useWorkspaceContext } from '../api/transport.tsx';
import { useInstanceOperator } from '../app/AuthProvider.tsx';
import { surfaceById } from '../app/navigation.ts';
import { Alert } from '../ui/Alert.tsx';

/**
 * The Hikyo system organisation and project carry the instance's own
 * configuration; the permission-model ADR (2026-09-06 amendment) refuses
 * machine consumers, adapters and SCIM there outright, whatever the caller
 * holds. The sidebar omits these entries in that scope (sidebar-model.ts);
 * this gate answers a deep link with the reason instead of a refusal dressed
 * as a denial.
 */
const REASON: Record<SystemScopeSurface, string> = {
  'machine-access':
    'Machine access is not available here: the system configuration project has no machine consumers, service accounts or dynamic providers.',
  adapters:
    'Deployment adapters are not available here: the system configuration project is applied to this instance, never pushed to a CI provider.',
  scim: 'SCIM provisioning is not available here: the system configuration organisation is not provisioned from an identity provider.',
};

/**
 * Whether the route's organisation (and project, when the surface is
 * project-scoped) is the Hikyo system scope. Null binding means unknown, which
 * renders the ordinary page: the server still refuses, and its copy names
 * the capability, so an operator without configuration disclosure sees the
 * same answer as before.
 */
export function useInSystemScope(org: string, project: string | null): 'pending' | 'system' | 'ordinary' {
  const operator = useInstanceOperator();
  // A remote workspace is the viewing side: the operator hint is this
  // instance's, the transport is the remote's, and its system scope is the
  // remote's own concern (and an audited denial there). Never read it.
  const local = useWorkspaceContext() === null;
  const { pending, scope } = useSystemScope(local && operator === true);
  if (!local) return 'ordinary';
  if (operator === null || pending) return 'pending';
  if (scope === null || scope.org !== org) return 'ordinary';
  return project === null || scope.project === project ? 'system' : 'ordinary';
}

/** The deep-link answer for a surface the system scope refuses. */
export function SystemScopeRefusal({ surface }: { surface: SystemScopeSurface }) {
  const instance = surfaceById('instance-admin');
  return (
    <div className="page page--chrome">
      <h1>{surfaceById(surface).label}</h1>
      <Alert>
        <strong>Hikyo system configuration.</strong> {REASON[surface]} Create an organisation of
        your own for this under <Link to={instance.path}>{instance.label}</Link>.
      </Alert>
    </div>
  );
}

/**
 * Wraps a route component so a deep link into the system scope renders the
 * refusal instead of the page. A wrapper rather than an early return inside
 * the page: the binding resolves after first render, and a page whose hook
 * list changes with it would break React's ordering rule.
 */
export function gateSystemScope(surface: SystemScopeSurface, Page: () => ReactElement) {
  const projectScoped = surface !== 'scim';
  return function Gated() {
    const params = useParams();
    const answer = useInSystemScope(params['org'] ?? '', projectScoped ? (params['project'] ?? '') : null);
    // Nothing until the binding read settles: rendering the page first would
    // fire its reads and then replace it, a flash of a refusal it never needed.
    if (answer === 'pending') return <p role="status">Checking this scope…</p>;
    return answer === 'system' ? <SystemScopeRefusal surface={surface} /> : <Page />;
  };
}
