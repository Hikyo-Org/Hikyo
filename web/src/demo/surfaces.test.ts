import { describe, expect, it } from 'vitest';
import { SURFACES } from '../app/navigation.ts';
import { DEMO_SURFACE_POLICIES, demoSurfaceAvailable, demoSurfacePolicy } from './surfaces.ts';

describe('demo surface policy', () => {
  it('covers the application registry exactly, with explicit reasons for exclusions', () => {
    expect(Object.keys(DEMO_SURFACE_POLICIES).sort()).toEqual(SURFACES.map((surface) => surface.id).sort());
    for (const surface of SURFACES) {
      const policy = demoSurfacePolicy(surface.id);
      if (policy.mode === 'excluded') expect(policy.reason.trim()).not.toBe('');
    }
  });

  it('derives sidebar projections and redirects from the same policy', () => {
    expect(demoSurfaceAvailable('project-members')).toBe(demoSurfaceAvailable('members'));
    expect(Object.hasOwn(DEMO_SURFACE_POLICIES, 'project-members')).toBe(false);
    expect(demoSurfaceAvailable('overview')).toBe(true);
    expect(demoSurfaceAvailable('instance-config')).toBe(false);
  });

  it('refuses unknown surface names including object prototype properties', () => {
    expect(demoSurfaceAvailable('future-screen')).toBe(false);
    expect(demoSurfaceAvailable('toString')).toBe(false);
    expect(demoSurfaceAvailable('__proto__')).toBe(false);
  });
});
