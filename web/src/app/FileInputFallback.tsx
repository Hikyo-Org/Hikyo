import { Button } from '../ui/Button.tsx';
import { isAppMode, offerContinueInBrowser } from './shell.ts';

/**
 * The desktop shell exposes no way to ask whether the window can open a file
 * picker (wire contract: docs/spec/desktop-shell-protocol.md), and a picker that
 * cannot open fails silently. So in app mode every file input carries this
 * offer beside it. Renders nothing in a browser.
 */
export function FileInputFallback({ what }: { what: string }) {
  if (!isAppMode()) return null;
  return (
    <Button
      type="button"
      variant="quiet"
      onClick={() => { offerContinueInBrowser(`If the file picker does not open in the desktop app, ${what} in your browser instead.`); }}
    >
      File picker not opening? Continue in browser
    </Button>
  );
}
