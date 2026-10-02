import { useEffect, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';

import { ApiError } from '../api/client.ts';
import { useSensitiveMutation, useSensitiveState } from '../api/sensitiveMutation.ts';
import { signupFailureText, verifySignup } from '../api/signup.ts';
import { surfaceById } from '../app/navigation.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';

/** Fragment values describe the form only. The server's pending row authorizes every landing. */
export function SignupVerify() {
  const location = useLocation();
  const navigate = useNavigate();
  const [token, setToken] = useSensitiveState('');
  const [password, setPassword] = useSensitiveState('');
  const [repeat, setRepeat] = useSensitiveState('');
  const [email] = useState(() => new URLSearchParams(location.hash.slice(1)).get('email') ?? '');
  const [landing] = useState(() => new URLSearchParams(location.hash.slice(1)).get('landing') ?? '');
  const [org] = useState(() => new URLSearchParams(location.hash.slice(1)).get('org'));
  const [displayName, setDisplayName] = useState('');
  const [orgName, setOrgName] = useState('');
  const [failure, setFailure] = useState<string | null>(null);
  const [refused, setRefused] = useState(false);
  const verify = useSensitiveMutation({ mutationFn: verifySignup });
  useEffect(() => {
    if (location.hash === '') return;
    const fragment = new URLSearchParams(location.hash.slice(1));
    setToken(fragment.get('token') ?? '');
    // Remove bearer material from the current history entry after capture.
    void navigate(`${location.pathname}${location.search}`, { replace: true });
  }, [location.hash, location.pathname, location.search, navigate, setToken]);
  // Retain only the non-authoritative scope hint when requesting a new link.
  const signupPath = surfaceById('signup').path;
  const recoveryPath = org === null ? signupPath : `${signupPath}?${new URLSearchParams({ org }).toString()}`;
  const fresh = landing === 'fresh-org' && org === null;
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFailure(null);
    if (displayName.trim() === '' || (fresh && orgName.trim() === '')) {
      setFailure('Enter your display name and, when shown, organisation name.');
      return;
    }
    if (password.length < 12) { setFailure('Choose a password of at least 12 characters.'); return; }
    if (password !== repeat) { setFailure('The two passwords differ. Type the same password twice.'); return; }
    verify.mutate({ token, password, display_name: displayName.trim(), ...(landing === 'none' || landing === 'org' || landing === 'fresh-org' ? { landing } : {}), ...(fresh ? { org_name: orgName.trim() } : {}) }, {
      onSuccess: () => { setToken(''); void navigate(surfaceById('login').path, { replace: true }); },
      onError: (error) => {
        setFailure(signupFailureText(error));
        if (error instanceof ApiError && error.status === 401) { setRefused(true); setToken(''); }
      },
      onSettled: () => { setPassword(''); setRepeat(''); },
    });
  };
  return <main className="login"><form className="login__card" onSubmit={submit} noValidate>
    <h1 className="login__title">Finish creating your account</h1>
    {failure === null ? null : <Alert>{failure}</Alert>}
    {refused || token === '' ? <>
      {failure === null ? <Alert>This link can't be used. Start again from the sign-up page.</Alert> : null}
      <Link className="btn btn--primary" to={recoveryPath}>Go to sign-up</Link>
    </> : <>
      <p className="login__landing">{fresh ? 'You’ll get your own organisation, with you as its first administrator.' : landing === 'org' ? 'You’ll join this organisation.' : 'You’ll get an account with no organisation yet. An administrator grants access afterwards.'}</p>
      <Input label="Email" name="email" type="email" autoComplete="username" readOnly value={email} />
      <Input label="Display name" name="display_name" autoComplete="name" required disabled={verify.isPending} value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
      {fresh ? <Input label="Organisation name" name="org_name" required hint="You can rename it later under Settings." disabled={verify.isPending} value={orgName} onChange={(event) => setOrgName(event.target.value)} /> : null}
      <Input label="Password" name="password" type="password" autoComplete="new-password" required hint="At least 12 characters." disabled={verify.isPending} value={password} onChange={(event) => setPassword(event.target.value)} />
      <Input label="Repeat password" name="repeat" type="password" autoComplete="new-password" required disabled={verify.isPending} value={repeat} onChange={(event) => setRepeat(event.target.value)} />
      <Button variant="primary" type="submit" disabled={verify.isPending}>{verify.isPending ? 'Creating account…' : 'Create account'}</Button>
      <p>Creating the account signs nothing in. You sign in afterwards like anyone else.</p>
    </>}
  </form></main>;
}
