import { useId, useState, type FormEvent } from 'react';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Dialog } from '../ui/Dialog.tsx';

export function AccountPasswordDialog({ first, proof, pending, failure, onCancel, onSubmit }: {
  first: boolean;
  proof: string;
  pending: boolean;
  failure: string | null;
  onCancel: () => void;
  onSubmit: (password: string, proof: string) => void;
}) {
  const formId = useId();
  const passwordId = useId();
  const repeatId = useId();
  const [password, setPassword] = useSensitiveState('');
  const [repeat, setRepeat] = useSensitiveState('');
  const [mismatch, setMismatch] = useState(false);
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (pending) return;
    setMismatch(password !== repeat);
    if (password !== repeat) return;
    onSubmit(password, proof);
    setPassword('');
    setRepeat('');
  };
  return <Dialog title={first ? 'Set password' : 'Change password'}
    lede="Every other session of yours ends. This session is reissued with no open verification windows."
    onCancel={(event) => { event.preventDefault(); if (!pending) onCancel(); }}
    actions={<>
      <Button type="button" disabled={pending} onClick={onCancel}>Cancel</Button>
      <Button type="submit" form={formId} variant="primary" disabled={pending}>{pending ? 'Saving…' : first ? 'Set password' : 'Change password'}</Button>
    </>}>
    <form id={formId} onSubmit={submit}>
      {failure === null ? null : <Alert>{failure}</Alert>}
      {mismatch ? <Alert>The passwords do not match. Enter them again.</Alert> : null}
      <div className="field"><label htmlFor={passwordId}>New password</label>
        <input id={passwordId} name="password" type="password" autoComplete="new-password" minLength={12} maxLength={1024} required disabled={pending} value={password} onChange={(event) => { setPassword(event.target.value); setMismatch(false); }} />
        <p className="field__hint">At least 12 characters.</p>
      </div>
      <div className="field"><label htmlFor={repeatId}>Repeat new password</label>
        <input id={repeatId} name="repeat-password" type="password" autoComplete="new-password" required disabled={pending} value={repeat} onChange={(event) => { setRepeat(event.target.value); setMismatch(false); }} />
      </div>
    </form>
  </Dialog>;
}
