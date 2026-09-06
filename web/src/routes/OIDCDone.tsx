import { useEffect, useState } from 'react';

import { announceSessionChange } from '../api/sessionEpoch.ts';
import { oidcChannelName, takeOIDCReturn } from '../api/oidcChannel.ts';

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

/** Same-origin return page for browser-started OIDC login/link/reauth flows. */
export function OIDCDone() {
  const [failure, setFailure] = useState<string | null>(null);
  const [returnTarget, setReturnTarget] = useState<string | null>(null);
  const purpose = purposeFromLocation();

  useEffect(() => {
    const params = new URLSearchParams(globalThis.location.search);
    const state = params.get('state') ?? '';
    const error = params.get('error');

    if (state === '' || purpose === null) {
      setFailure('This page was opened without an OIDC transaction. Close it and start again.');
      return;
    }
    if (purpose === 'login') {
      if (error !== null) {
        setFailure('Your identity provider refused this sign-in. Return to sign in and try again.');
        setReturnTarget('/login');
        return;
      }
      announceSessionChange();
      globalThis.location.replace('/');
      return;
    }

    const channel = new BroadcastChannel(oidcChannelName(state));
    channel.postMessage(error === null ? { state, ok: true } : { state, ok: false, error });
    channel.close();
    const returnTo = takeOIDCReturn(state);
    if (purpose === 'link') {
      if (error !== null) {
        setFailure('Your identity provider refused this link. Return to account security and try again.');
        setReturnTarget(returnTo);
      } else {
        globalThis.location.replace(returnTo);
      }
      return;
    }
    if (error !== null) {
      // A refused reauthentication stays on screen: the broadcast above told
      // the opener, and closing or navigating here would hide the refusal.
      setFailure('Your identity provider refused this reauthentication. Go back and try again.');
      setReturnTarget(returnTo);
      return;
    }
    globalThis.close();
    // If this was a same-tab fallback, close() is refused. Return to the page
    // that started the transaction after the broadcast has been sent.
    globalThis.setTimeout(() => globalThis.location.assign(returnTo), 0);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- runs once on the callback URL
  }, []);

  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">Returning from your identity provider</h1>
        {failure === null ? (
          <p className="login__lede" role="status">
            {successLede(purpose)}
          </p>
        ) : (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{failure}</span>
          </p>
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
          <button className="btn" type="button" onClick={() => globalThis.close()}>
            Close this window
          </button>
        )}
      </div>
    </main>
  );
}
