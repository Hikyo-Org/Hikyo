import { useState, type FormEvent } from 'react';
import { Link } from 'react-router';

import { surfaceById } from '../app/navigation.ts';
import { ApiError, transportRefusalText } from '../api/client.ts';
import { useSensitiveMutation } from '../api/sensitiveMutation.ts';
import { testInstanceMail, useInstanceMail } from '../api/signup.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';
import { ProofDialog } from '../ui/auth/ProofDialog.tsx';
import { Panel } from './Sections.tsx';

function mailFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 401) return 'That proof was not accepted. Enter a fresh authenticator code, or your password if you have no authenticator.';
    if (error.status === 403) return 'Sending a test needs instance configuration authority and a second factor. Present your authenticator code or passkey in the banner above.';
    if (error.status === 404) return 'Mailer settings are not disclosed to this session.';
    if (error.status === 400) return error.detail ?? 'Check the recipient address and mailer configuration.';
    if (error.status === 409) return 'Another test send is running. Wait for it to finish.';
  }
  return transportRefusalText(error) ?? 'The test mail could not be sent. Check the mailer configuration and try again.';
}

export function InstanceMailPanel() {
  const mail = useInstanceMail();
  const send = useSensitiveMutation({ mutationFn: testInstanceMail });
  const [to, setTo] = useState('');
  const [proofing, setProofing] = useState(false);
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (send.isPending || mail.data?.configured !== true || !event.currentTarget.reportValidity()) return;
    send.reset();
    setProofing(true);
  };
  return <Panel id="instance-mail" title="Mailer">
    {mail.isPending ? <p role="status">Loading mailer status…</p> : null}
    {mail.isError ? <Alert>{mailFailureText(mail.error)}</Alert> : null}
    {mail.isSuccess ? <>
      <p>Mailer <Badge>{mail.data.configured ? 'configured' : 'not configured'}</Badge></p>
      <p className="field__hint">Configured means the active configuration has valid mail settings. Only a test send checks reachability and delivery.</p>
      {!mail.data.configured ? <>
        <p>Configure mail and apply the changes. Before managed configuration is adopted, bootstrap mail uses HIKYO_MAIL_* environment settings. An email sign-up entry stays inactive until the mailer is configured.</p>
        <Link className="btn" to={surfaceById('instance-config').path}>Hikyo configuration</Link>
      </> : null}
      <form onSubmit={submit}>
        <Input label="Test recipient" name="to" type="email" required disabled={!mail.data.configured || send.isPending} value={to} onChange={(event) => setTo(event.target.value)} />
        <Button type="submit" disabled={!mail.data.configured || send.isPending}>Send test…</Button>
      </form>
      {send.isSuccess ? <Alert tone="done">Test mail sent. Check the recipient inbox.</Alert> : null}
      <p className="field__hint">Up to five tests per hour per operator, with one send at a time on the instance.</p>
    </> : null}
    {proofing ? <ProofDialog reauth field="code-or-password" lede="To send a test mail, enter the code from your authenticator, or your password if you have no authenticator." pending={send.isPending} failure={send.isError ? mailFailureText(send.error) : null} onCancel={() => setProofing(false)} onSubmit={(proof, clear) => {
      send.mutate({ to: to.trim(), proof }, { onSuccess: () => setProofing(false), onError: () => clear() });
    }} /> : null}
  </Panel>;
}
