import { useEffect, useState } from 'react';

import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { ShellError, hasSignedOut, shellSignIn } from './shell.ts';

/**
 * The desktop app never runs the login form (ADR D6). A dead session asks the
 * shell for a new one through the system browser, then reloads this route.
 */
export function AppSignIn() {
  const [attempt, setAttempt] = useState(0);
  const [failure, setFailure] = useState<string | null>(null);
  // After an explicit sign-out the person decides when to sign in again.
  const [waiting, setWaiting] = useState(hasSignedOut);
  useEffect(() => {
    if (waiting) return undefined;
    let current = true;
    shellSignIn().then(
      () => { if (current) globalThis.location.reload(); },
      (error: unknown) => {
        if (current) setFailure(error instanceof ShellError && error.code === 'cancelled'
          ? 'Sign-in was cancelled.' : 'Signing in through your browser did not finish.');
      },
    );
    return () => { current = false; };
  }, [attempt, waiting]);
  return (
    <main className="login">
      {waiting ? (
        <>
          <p role="status">You signed out.</p>
          <Button type="button" variant="primary" onClick={() => setWaiting(false)}>Sign in</Button>
        </>
      ) : failure === null ? (
        <p role="status">Signing in through your browser…</p>
      ) : (
        <Alert action={<Button type="button" onClick={() => { setFailure(null); setAttempt((n) => n + 1); }}>Try again</Button>}>{failure}</Alert>
      )}
    </main>
  );
}
