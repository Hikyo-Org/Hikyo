import type { SurfaceId } from '../app/navigation.ts';

export type DemoSurfacePolicy =
  | { readonly mode: 'interactive' }
  | { readonly mode: 'read-only'; readonly controls: 'fieldset' | 'actions' | 'audit' }
  | { readonly mode: 'excluded'; readonly reason: string; readonly redirect?: 'matrix' };

/** Every application route needs an explicit demo decision when the registry grows. */
export const DEMO_SURFACE_POLICIES = {
  login: { mode: 'excluded', reason: 'Demo accounts are selected in the toolbar.', redirect: 'matrix' },
  signup: { mode: 'excluded', reason: 'Demo accounts are selected in the toolbar.', redirect: 'matrix' },
  'signup-verify': { mode: 'excluded', reason: 'Email verification requires a full Hikyo installation.' },
  'establish-credential': { mode: 'excluded', reason: 'Creating real account credentials requires a full Hikyo installation.' },
  overview: { mode: 'excluded', reason: 'The demo opens directly in the sample matrix.', redirect: 'matrix' },
  projects: { mode: 'interactive' },
  remotes: { mode: 'read-only', controls: 'fieldset' },
  members: { mode: 'read-only', controls: 'fieldset' },
  'org-settings': { mode: 'read-only', controls: 'fieldset' },
  scim: { mode: 'read-only', controls: 'actions' },
  audit: { mode: 'read-only', controls: 'audit' },
  'instance-admin': { mode: 'excluded', reason: 'Instance administration requires a full Hikyo installation.' },
  'instance-config': { mode: 'excluded', reason: 'Server configuration requires a full Hikyo installation.' },
  'instance-members': { mode: 'excluded', reason: 'Instance membership administration requires a full Hikyo installation.' },
  settings: { mode: 'read-only', controls: 'fieldset' },
  matrix: { mode: 'interactive' },
  history: { mode: 'interactive' },
  'key-detail': { mode: 'interactive' },
  values: { mode: 'interactive' },
  'machine-access': { mode: 'read-only', controls: 'actions' },
  'change-approvals': { mode: 'read-only', controls: 'actions' },
  'temporary-access': { mode: 'read-only', controls: 'actions' },
  adapters: { mode: 'read-only', controls: 'actions' },
  'project-audit': { mode: 'read-only', controls: 'audit' },
  'project-settings': { mode: 'read-only', controls: 'fieldset' },
  'cli-reauth': { mode: 'excluded', reason: 'Authenticating a CLI requires a full Hikyo installation.' },
  'workspace-approve': { mode: 'excluded', reason: 'Connecting remote instances requires a full Hikyo installation.' },
  'workspace-callback': { mode: 'excluded', reason: 'Connecting remote instances requires a full Hikyo installation.' },
  'oidc-done': { mode: 'excluded', reason: 'Signing in with an identity provider requires a full Hikyo installation.' },
  'saml-done': { mode: 'excluded', reason: 'Signing in with an identity provider requires a full Hikyo installation.' },
} satisfies Record<SurfaceId, DemoSurfacePolicy>;

export function demoSurfacePolicy(id: SurfaceId): DemoSurfacePolicy {
  return DEMO_SURFACE_POLICIES[id];
}

export function demoSurfaceAvailable(id: string): boolean {
  // The sidebar's project-members row is a filtered projection of members,
  // not a separate application route or a second policy decision.
  const surfaceId = id === 'project-members' ? 'members' : id;
  return Object.entries(DEMO_SURFACE_POLICIES).some(([candidate, value]) => {
    const policy: DemoSurfacePolicy = value;
    return candidate === surfaceId && (policy.mode !== 'excluded' || policy.redirect === 'matrix');
  });
}
