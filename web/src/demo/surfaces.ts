import type { SurfaceId } from '../app/navigation.ts';

export type DemoSurfacePolicy =
  | { readonly mode: 'interactive' }
  | { readonly mode: 'read-only'; readonly audit?: true }
  | { readonly mode: 'excluded'; readonly reason: string; readonly redirect?: 'matrix' };

/** Every application route needs an explicit demo decision when the registry grows. */
export const DEMO_SURFACE_POLICIES = {
  login: { mode: 'excluded', reason: 'Demo accounts are selected in the toolbar.', redirect: 'matrix' },
  signup: { mode: 'excluded', reason: 'Demo accounts are selected in the toolbar.', redirect: 'matrix' },
  'signup-verify': { mode: 'excluded', reason: 'Email verification requires a full Hikyo installation.' },
  'establish-credential': { mode: 'excluded', reason: 'Creating real account credentials requires a full Hikyo installation.' },
  overview: { mode: 'excluded', reason: 'The demo opens directly in the sample matrix.', redirect: 'matrix' },
  projects: { mode: 'interactive' },
  remotes: { mode: 'read-only' },
  members: { mode: 'read-only' },
  'org-settings': { mode: 'read-only' },
  scim: { mode: 'read-only' },
  audit: { mode: 'read-only', audit: true },
  'instance-admin': { mode: 'excluded', reason: 'Instance administration requires a full Hikyo installation.' },
  'instance-config': { mode: 'excluded', reason: 'Server configuration requires a full Hikyo installation.' },
  'instance-members': { mode: 'excluded', reason: 'Instance membership administration requires a full Hikyo installation.' },
  settings: { mode: 'read-only' },
  matrix: { mode: 'interactive' },
  history: { mode: 'interactive' },
  'key-detail': { mode: 'interactive' },
  values: { mode: 'interactive' },
  'machine-access': { mode: 'read-only' },
  'change-approvals': { mode: 'read-only' },
  'temporary-access': { mode: 'read-only' },
  adapters: { mode: 'read-only' },
  'project-audit': { mode: 'read-only', audit: true },
  'project-settings': { mode: 'read-only' },
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
