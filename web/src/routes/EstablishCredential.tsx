import { useId, useState, type FormEvent } from 'react';
import { Link } from 'react-router';

import { establishCredential, establishFailureText } from '../api/session.ts';
import { surfaceById } from '../app/navigation.ts';

/**
 * The public credential-establishment page (#568, registry surface
 * `establish-credential`).
 *
 * Where a display-once authority — from an invitation, a credential reset,
 * bootstrap or break-glass — becomes a password. Chromeless and sessionless
 * like login: the holder has no session yet, and a 204 here establishes none;
 * they sign in afterwards like anyone else.
 *
 * Refusals are one sentence on purpose. The server answers an expired, spent,
 * unknown or malformed authority uniformly, and this page keeps that oracle
 * closed rather than reopening it with helpful wording.
 */
export function EstablishCredential() {
  const [authority, setAuthority] = useState('');
  const [password, setPassword] = useState('');
  const [repeat, setRepeat] = useState('');
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const authorityId = useId();
  const passwordId = useId();
  const repeatId = useId();

  const onSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFailure(null);
    if (authority.trim() === '' || password === '') {
      setFailure('Paste the authority you were handed and choose a password.');
      return;
    }
    if (password.length < 12) {
      setFailure('Choose a password of at least 12 characters.');
      return;
    }
    if (password !== repeat) {
      // Decided here, before any request: the server never sees a password
      // the holder did not type twice.
      setFailure('The two passwords differ. Type the same password twice.');
      return;
    }
    setPending(true);
    try {
      await establishCredential(authority.trim(), password);
      setAuthority('');
      setPassword('');
      setRepeat('');
      setDone(true);
    } catch (error) {
      setFailure(establishFailureText(error));
    } finally {
      setPending(false);
    }
  };

  if (done) {
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Credential established</h1>
          <p className="login__lede" role="status">
            Sign in with your username and the password you just set. The authority is spent.
          </p>
          <Link className="btn btn--primary" to={surfaceById('login').path}>
            Sign in
          </Link>
        </div>
      </main>
    );
  }

  return (
    <main className="login">
      <form className="login__card" onSubmit={onSubmit} noValidate>
        <h1 className="login__title">Establish your credential</h1>
        <p className="login__lede">
          Paste the setup authority you were handed. It works once, and it only sets a
          password: you sign in afterwards like anyone else.
        </p>

        {failure === null ? null : (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>{failure}</span>
          </p>
        )}

        <div className="field">
          <label htmlFor={authorityId}>Setup authority</label>
          <input
            id={authorityId}
            name="authority"
            autoComplete="off"
            spellCheck={false}
            required
            disabled={pending}
            value={authority}
            onChange={(event) => setAuthority(event.target.value)}
          />
        </div>

        <div className="field">
          <label htmlFor={passwordId}>New password</label>
          <input
            id={passwordId}
            name="password"
            type="password"
            autoComplete="new-password"
            required
            disabled={pending}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </div>

        <div className="field">
          <label htmlFor={repeatId}>Repeat the password</label>
          <input
            id={repeatId}
            name="repeat"
            type="password"
            autoComplete="new-password"
            required
            disabled={pending}
            value={repeat}
            onChange={(event) => setRepeat(event.target.value)}
          />
        </div>

        <button className="btn btn--primary" type="submit" disabled={pending}>
          {pending ? 'Establishing…' : 'Establish credential'}
        </button>
        <Link className="btn" to={surfaceById('login').path}>
          Back to sign in
        </Link>
      </form>
    </main>
  );
}
