import './styles/index.ts';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './app/App.tsx';
import { AuthProvider } from './app/AuthProvider.tsx';
import { initTheme } from './app/theme.ts';
import { isDemo } from './demo/config.ts';

// Paint the stored theme choice before first render, so a reload lands on it
// without waiting for a theme-aware control to mount and apply it.
initTheme();

// The fonts are self-hosted npm packages, not a CDN link: `font-src 'self'`
// is part of the CSP baseline, and an external font host would be both a CSP
// relaxation and unsolicited egress the threat model's telemetry stance
// forbids by default.

const host = document.getElementById('root');
if (host === null) {
  // Fail loud: a missing mount point means the document the server served is
  // not the document this bundle was built for.
  throw new Error('hikyo: #root is missing from the document');
}

// Keep this condition local so Vite removes the import itself in normal builds.
if (import.meta.env.MODE === 'demo') {
  const [{ installDemo }, { demoRole }] = await Promise.all([
    import('./demo/api.ts'),
    import('./demo/config.ts'),
  ]);
  installDemo(demoRole());
}

createRoot(host).render(
  <StrictMode>
    <AuthProvider monitorRuntime={!isDemo}>
      <App />
    </AuthProvider>
  </StrictMode>,
);
