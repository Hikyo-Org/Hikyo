import { useRef, useState, useSyncExternalStore } from 'react';

import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import {
  continueInBrowserReason,
  dismissContinueInBrowser,
  shellOpenCurrentPage,
  subscribeContinueInBrowser,
} from './shell.ts';

/**
 * The desktop app's answer to what its window cannot do on its own origin
 * (ADR D7): offer to open this page in the system browser. A modal dialog, so
 * the offer stays reachable above a ceremony or editor that is already open.
 * Renders nothing in a browser, where nothing raises the offer.
 */
export function ContinueInBrowser() {
  const reason = useSyncExternalStore(subscribeContinueInBrowser, continueInBrowserReason, continueInBrowserReason);
  const [failure, setFailure] = useState<string | null>(null);
  // Only the newest request may close the offer or set its error: dismissing or retrying invalidates the rest.
  const latest = useRef(0);
  if (reason === null) return null;
  const close = () => {
    latest.current += 1;
    setFailure(null);
    dismissContinueInBrowser();
  };
  const open = () => {
    setFailure(null);
    const mine = ++latest.current;
    shellOpenCurrentPage().then(
      () => {
        if (latest.current === mine) close();
      },
      () => {
        if (latest.current === mine) setFailure('The desktop app could not open your browser. Try again.');
      },
    );
  };
  return (
    <Dialog
      title="Continue in your browser"
      lede={reason}
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
      actions={
        <>
          <Button type="button" onClick={close}>Not now</Button>
          <Button type="button" variant="primary" onClick={open}>Continue in browser</Button>
        </>
      }
    >
      <p>
        This page opens on your instance under a separate browser session. Signing in there is
        independent of this app, and some changes made there sign this app out so it can sign in again.
      </p>
      {failure === null ? null : <Alert>{failure}</Alert>}
    </Dialog>
  );
}
