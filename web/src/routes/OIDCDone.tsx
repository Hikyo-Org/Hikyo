import { useEffect } from 'react';

import { announceSessionChange } from '../api/sessionEpoch.ts';
import { oidcChannelName, peekOIDCReturn, takeOIDCReturn } from '../api/oidcChannel.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';

type Purpose = 'login' | 'link' | 'reauth';

function purposeFromLocation(): Purpose | null {
  const purpose = new URLSearchParams(globalThis.location.search).get('purpose');
  return purpose === 'login' || purpose === 'link' || purpose === 'reauth' ? purpose : null;
}

/** The one-line outcome per purpose, so "Signed in." never reads as a reauth. */
function successLede(purpose: Purpose | null): string {
  switch (purpose) {
    case 'login':
      return 'Signed in.';
    case 'link':
      return 'Identity linked.';
    case 'reauth':
      return 'Reauthentication completed. You can close this window.';
    case null:
      return 'Returning.';
  }
}

const NO_TRANSACTION = 'This page was opened without an OIDC transaction. Close it and start again.';

/** Same-origin return page for browser-started OIDC login/link/reauth flows. */
export function OIDCDone() {
  const params = new URLSearchParams(globalThis.location.search);
  const purpose = purposeFromLocation();
  const state = params.get('state') ?? '';
  const error = params.get('error');
  const invalid = state === '' || purpose === null;

  // Both the message and the return link are pure functions of the URL, so
  // they are derived here rather than set from the effect. The effect owns only
  // the irreversible side effects: the opener broadcast, consuming the stored
  // return target, and the navigation or window close on a successful return.
  const failure = invalid
    ? NO_TRANSACTION
    : error === null
      ? null
      : purpose === 'login'
        ? 'Your identity provider refused this sign-in. Return to sign in and try again.'
        : purpose === 'link'
          ? 'Your identity provider refused this link. Return to account security and try again.'
          : 'Your identity provider refused this reauthentication. Go back and try again.';
  const returnTarget =
    invalid || error === null ? null : purpose === 'login' ? '/login' : peekOIDCReturn(state);

  useEffect(() => {
    if (invalid) {
      return;
    }
    if (purpose === 'login') {
      // A refused sign-in only shows the derived failure; it neither broadcasts
      // nor navigates. A success announces the new session and lands home.
      if (error !== null) {
        return;
      }
      announceSessionChange();
      globalThis.location.replace('/');
      return;
    }

    const channel = new BroadcastChannel(oidcChannelName(state));
    channel.postMessage(error === null ? { state, ok: true } : { state, ok: false, error });
    channel.close();
    if (error !== null) {
      // A refused link/reauth stays on screen with its return link; the opener
      // already heard the refusal. The single-use return nonce is left in
      // storage (harmless) so the derived return target stays stable.
      return;
    }
    const returnTo = takeOIDCReturn(state);
    if (purpose === 'link') {
      globalThis.location.replace(returnTo);
      return;
    }
    globalThis.close();
    // If this was a same-tab fallback, close() is refused. Return to the page
    // that started the transaction after the broadcast has been sent.
    globalThis.setTimeout(() => globalThis.location.assign(returnTo), 0);
  }, [purpose, state, error, invalid]);

  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">Returning from your identity provider</h1>
        {failure === null ? (
          <p className="login__lede" role="status">
            {successLede(purpose)}
          </p>
        ) : (
          <Alert>{failure}</Alert>
        )}
        {returnTarget !== null ? (
          <a className="btn" href={returnTarget}>
            {purpose === 'login'
              ? 'Return to sign in'
              : purpose === 'link'
                ? 'Return to account security'
                : 'Back'}
          </a>
        ) : (
          <Button type="button" onClick={() => globalThis.close()}>
            Close this window
          </Button>
        )}
      </div>
    </main>
  );
}
