import type { ReactNode } from 'react';
import { Link, Navigate, useLocation } from 'react-router';
import type { SurfaceId } from '../app/navigation.ts';
import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import { DEMO_MATRIX_PATH } from './config.ts';
import { demoSurfacePolicy } from './surfaces.ts';
import './demo.css';

/** Keep the actual screens and their navigation, with configuration controls disabled. */
export function DemoSurface({ id, children }: { id: SurfaceId; children: ReactNode }) {
  const location = useLocation();
  const policy = demoSurfacePolicy(id);
  if (policy.mode === 'excluded' && policy.redirect === 'matrix') {
    return <Navigate to={DEMO_MATRIX_PATH} replace />;
  }
  if (policy.mode === 'excluded' || new URLSearchParams(location.search).has('remote')) {
    return (
      <section className="page page--chrome">
        <h1>Not available in this demo</h1>
        <p className="page__lede">
          This playground focuses on browsing, editing and publishing sample configuration.
          {' '}{policy.mode === 'excluded' ? policy.reason : 'Connecting remote instances requires a full Hikyo installation.'}
        </p>
        <Link className="btn" to={DEMO_MATRIX_PATH}>Back to the environment matrix</Link>
      </section>
    );
  }
  if (policy.mode === 'interactive') return children;
  if (policy.audit === true) {
    return <div className="demo-readonly">
      <p className="demo-readonly__notice">Read-only sample audit events. Explore the filters and event details; export is disabled.</p>
      {children}
    </div>;
  }
  return (
    <ReadOnlyContext.Provider value={true}>
      <div className="demo-readonly" onSubmitCapture={(event) => {
        // Mutation forms can submit with Enter even when their buttons are disabled.
        // Audit filters are deliberately outside this boundary: their submit is a read.
        event.preventDefault();
        event.stopPropagation();
      }}>
        <p className="demo-readonly__notice">Read-only sample data. Explore the tabs and details; actions that change configuration are disabled.</p>
        {children}
      </div>
    </ReadOnlyContext.Provider>
  );
}
