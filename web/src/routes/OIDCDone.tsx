import { useEffect, useState } from 'react';

import { oidcChannelName, takeOIDCReturn } from '../api/oidcChannel.ts';

/** Same-origin return page for browser-started OIDC login/link/reauth flows. */
export function OIDCDone() {
  const [failure, setFailure] = useState<string | null>(null);
  const [returnTarget, setReturnTarget] = useState<string | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(globalThis.location.search);
    const state = params.get('state') ?? '';
    const purpose = params.get('purpose') ?? '';
    const error = params.get('error');

    if (state === '' || !['login', 'link', 'reauth'].includes(purpose)) {
      setFailure('This page was opened without an OIDC transaction. Close it and start again.');
      return;
    }
    if (purpose === 'login') {
      if (error !== null) {
        setFailure('Your identity provider refused this sign-in. Return to sign in and try again.');
        return;
      }
      globalThis.location.replace('/');
      return;
    }

    const channel = new BroadcastChannel(oidcChannelName(state));
    channel.postMessage(error === null ? { state, ok: true } : { state, ok: false, error });
    channel.close();
    if (purpose === 'link') {
      const returnTo = takeOIDCReturn(state);
      if (error !== null) {
        setFailure('Your identity provider refused this link. Return to account security and try again.');
        setReturnTarget(returnTo);
      } else {
        globalThis.location.replace(returnTo);
      }
      return;
    }
    if (error !== null) setFailure('Your identity provider refused this reauthentication.');
    const returnTo = takeOIDCReturn(state);
    globalThis.close();
    // If this was a same-tab fallback, close() is refused. Return to the page
    // that started the transaction after the broadcast has been sent.
    globalThis.setTimeout(() => globalThis.location.assign(returnTo), 0);
  }, []);

  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">Returning from your identity provider</h1>
        {failure === null ? (
          <p className="login__lede" role="status">
            Reauthentication completed. You can close this window.
          </p>
        ) : (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{failure}</span>
          </p>
        )}
        {returnTarget !== null ? (
          <button className="btn" type="button" onClick={() => globalThis.location.assign(returnTarget)}>
            Return to account security
          </button>
        ) : purposeFromLocation() === 'login' && failure !== null ? (
          <button className="btn" type="button" onClick={() => globalThis.location.assign('/login')}>
            Return to sign in
          </button>
        ) : (
          <button className="btn" type="button" onClick={() => globalThis.close()}>
            Close this window
          </button>
        )}
      </div>
    </main>
  );
}

function purposeFromLocation(): string {
  return new URLSearchParams(globalThis.location.search).get('purpose') ?? '';
}
