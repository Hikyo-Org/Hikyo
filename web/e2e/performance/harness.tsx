import '@fontsource-variable/instrument-sans';
import '@fontsource/ibm-plex-mono/400.css';
import '@fontsource/ibm-plex-mono/500.css';
import '../../src/styles/tokens.css';
import '../../src/styles/app.css';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router';
import { AuthProvider } from '../../src/app/AuthProvider.tsx';
import { initTheme } from '../../src/app/theme.ts';
import { Matrix } from '../../src/routes/Matrix.tsx';
import { Shell } from '../../src/routes/Shell.tsx';
import { installMatrixFixture, matrixPath } from './fixture.ts';
import { installTiming } from './timing.ts';

declare global {
  interface Window {
    matrixPerformance: ReturnType<typeof installTiming> & { failures: string[] };
  }
}
const fixture = installMatrixFixture();
window.matrixPerformance = { ...installTiming(), failures: fixture.failures };
initTheme();
const host = document.getElementById('root');
if (host === null) throw new Error('Missing performance harness root');
createRoot(host).render(<AuthProvider><MemoryRouter initialEntries={[matrixPath]}>
  <Routes><Route element={<Shell session={fixture.identity} />}>
    <Route path="/orgs/:org/projects/:project/matrix" element={<Matrix />} />
  </Route></Routes>
</MemoryRouter></AuthProvider>);
const refresh = document.getElementById('fixture-refresh');
if (refresh === null) throw new Error('Missing fixture refresh control');
let generation = 0;
refresh.addEventListener('click', () => { generation++; fixture.refresh(`live-update-${String(generation)}`); });
