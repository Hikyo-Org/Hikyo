import type { ReactNode } from 'react';

import { Button } from '../ui/Button.tsx';
import { notifyFailure } from './notifications.tsx';
import { isAppMode, shellDownloadPath } from './shell.ts';

/**
 * A download of an API path. A browser follows the link; the desktop app's
 * window cannot save a file by itself, so the shell fetches the path through
 * its proxy and shows the save panel (ADR D11).
 */
export function DownloadLink({ href, filename, children }: { href: string; filename: string; children: ReactNode }) {
  if (!isAppMode()) return <a className="btn" href={href} download={filename}>{children}</a>;
  return (
    <Button
      type="button"
      onClick={() => {
        shellDownloadPath(href, filename).catch(() => notifyFailure('The file could not be saved. Try again.'));
      }}
    >
      {children}
    </Button>
  );
}
