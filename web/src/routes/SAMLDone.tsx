import { useEffect, useRef, useState } from 'react';

import { peekSAMLTransaction, takeSAMLTransaction } from '../api/samlTransaction.ts';
import { announceSessionChange } from '../api/sessionEpoch.ts';
import { Alert } from '../ui/Alert.tsx';

/** Browser ACS completion. The URL names state only, never authority or a destination. */
export function SAMLDone() {
  const state = new URLSearchParams(globalThis.location.search).get('state') ?? '';
  const [transaction] = useState(() => peekSAMLTransaction(state));
  const finished = useRef(false);

  useEffect(() => {
    if (finished.current || transaction === null) return;
    finished.current = true;
    const consumed = takeSAMLTransaction(state);
    if (consumed === null) return;
    // The server installed the HttpOnly session and its CSRF companion.
    // Other tabs retire their old owner; the reload adopts through uncached whoami.
    announceSessionChange();
    globalThis.location.replace(consumed.returnTo);
  }, [state, transaction]);

  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">Returning from your identity provider</h1>
        {transaction === null ? (
          <Alert>This sign-in transaction is missing or already completed. Return to sign in and try again.</Alert>
        ) : (
          <p className="login__lede" role="status">Signed in.</p>
        )}
        <a className="btn" href="/login">Return to sign in</a>
      </div>
    </main>
  );
}
