import { useEffect, useRef, useState, type FormEvent } from 'react';

import { requestSignup, signupFailureText } from '../../api/signup.ts';
import { useSensitiveMutation } from '../../api/sensitiveMutation.ts';
import { Alert } from '../Alert.tsx';
import { Button } from '../Button.tsx';
import { Input } from '../Input.tsx';

/** A 202 acknowledges the request, never delivery or whether an account exists. */
export function LocalSignupForm({ landing, org, onBack }: {
  landing: string | null;
  org?: string;
  onBack: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  const [email, setEmail] = useState('');
  const [sent, setSent] = useState(false);
  const [resent, setResent] = useState(false);
  useEffect(() => { heading.current?.focus(); }, [sent]);
  const request = useSensitiveMutation({ mutationFn: requestSignup });
  const submit = (event?: FormEvent<HTMLFormElement>) => {
    event?.preventDefault();
    request.mutate({ email: email.trim(), ...(org === undefined ? {} : { org }) }, {
      onSuccess: () => { setResent(sent); setSent(true); },
    });
  };
  return <form className="login__card" onSubmit={submit}>
    <Button type="button" variant="quiet" className="login__back" disabled={request.isPending} onClick={onBack}>‹ Other ways to create an account</Button>
    <h1 className="login__title" ref={heading} tabIndex={-1}>{sent ? 'Check your mail' : 'Create an account with email'}</h1>
    {request.isError ? <Alert>{signupFailureText(request.error)}</Alert> : null}
    {sent ? <>
      <p className="login__lede">If this address can sign up, a link will arrive at <strong>{email.trim()}</strong>. It works once and expires 24 hours after it was sent.</p>
      <p>If that address already has an account, the mail says so instead of carrying a link.</p>
      {resent ? <p role="status">Requested another link. Use the newest mail; the previous link stops working.</p> : null}
      <Button type="button" disabled={request.isPending} onClick={() => submit()}>{request.isPending ? 'Requesting…' : 'Send it again'}</Button>
      <a className="btn" href="/login">Back to sign in</a>
    </> : <>
      {landing === null ? null : <p className="login__landing">{landing}</p>}
      <Input label="Email" name="email" type="email" autoComplete="email" required disabled={request.isPending} value={email} onChange={(event) => setEmail(event.target.value)} />
      <p className="field__hint">We verify this address with a link before creating an account.</p>
      <Button variant="primary" type="submit" disabled={request.isPending}>{request.isPending ? 'Requesting…' : 'Send sign-up link'}</Button>
    </>}
  </form>;
}
