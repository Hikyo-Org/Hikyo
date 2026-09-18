import { useEffect, useState } from 'react';

import { channelName } from '../api/workspace.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';

/**
 * The viewing instance's own callback page (registry surface
 * `workspace-callback`).
 *
 * This is the return leg of the front channel and it does exactly three things:
 * read `code` and `state` off its own URL, broadcast them on the transaction's
 * nonce-named channel, and close itself.
 *
 * It exists at all because the popup is opened with `noopener`: a hostile or
 * compromised remote must not be able to navigate the opener into a phishing
 * page, so `window.opener` is null and there is nothing to post a message
 * back through. A same-origin page plus a `BroadcastChannel`, which only this
 * origin's documents can open, is the return path that costs nothing.
 *
 * What it deliberately does NOT do: redeem. The front channel carries code and
 * state only; the artifact is redeemed by the SHELL, with the PKCE verifier
 * that never left it, and lands in that document's memory rather than this
 * one's.
 */
export function WorkspaceCallback() {
  const params = new URLSearchParams(globalThis.location.search);
  const code = params.get('code') ?? '';
  const state = params.get('state') ?? '';
  // A missing handoff result is a fact of the URL this page was opened with, so
  // it is derived during render rather than written from the effect.
  const failure =
    code === '' || state === ''
      ? 'This page was opened without a handoff result. Close it and start again.'
      : null;
  const [stayedOpen, setStayedOpen] = useState(false);

  useEffect(() => {
    if (code === '' || state === '') {
      return;
    }
    const channel = new BroadcastChannel(channelName(state));
    channel.postMessage({ code, state });
    channel.close();
    // Closing is best-effort: a browser may refuse to close a window this
    // script did not open, and that is a cosmetic failure rather than a
    // functional one, the shell already has what it needs.
    globalThis.close();
    // A refused close() leaves the window open with no error to read; the
    // page checks after the close settles and says so instead of waiting.
    const check = globalThis.setTimeout(() => {
      if (!globalThis.closed) setStayedOpen(true);
    }, 0);
    return () => globalThis.clearTimeout(check);
  }, [code, state]);

  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">Returning to your workspace</h1>
        {failure === null ? (
          <p className="login__lede" role="status">
            {stayedOpen
              ? 'This window could not close itself. Close it to continue.'
              : 'Handing the authorization back. This window closes itself.'}
          </p>
        ) : (
          <Alert>{failure}</Alert>
        )}
        {/* A browser may refuse to close a window this script did not open, so
            the human is given the control rather than left on a dead page. */}
        <Button type="button" onClick={() => globalThis.close()}>
          Close this window
        </Button>
      </div>
    </main>
  );
}
