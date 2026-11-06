import { useId, useRef, useState, type FormEvent } from 'react';

import {
  emailInvitationTemplate,
  emailInvitationTemplates,
  sendEmailInvitation,
  sendFailureText,
} from '../api/emailInvitations.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Input } from '../ui/Input.tsx';
import { Select } from '../ui/Select.tsx';

/**
 * Invite an existing account by email (#1207). The server answers the same
 * 202 for every address, so the recorded stage is worded to be true whether or
 * not the address matched: it never says "sent to" or "found".
 */
export function EmailInviteDialog({
  org,
  scopeName,
  onDone,
  onCancel,
}: {
  org: string;
  scopeName: string;
  /** Called when the recorded stage is closed, so the page can refresh its list. */
  onDone: () => void;
  onCancel: () => void;
}) {
  const [email, setEmail] = useState('');
  const [template, setTemplate] = useState('');
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [recorded, setRecorded] = useState(false);
  const first = useRef<HTMLInputElement>(null);
  const formId = useId();

  if (recorded) {
    return (
      <Dialog
        title="Invitation recorded"
        onCancel={(event) => {
          event.preventDefault();
          onDone();
        }}
        actions={<Button type="button" variant="primary" onClick={onDone}>Close</Button>}
      >
        <p>
          The invitation to {scopeName} is recorded. If the address belongs to an account with a
          verified email on this instance, the person receives it by email and joins by accepting
          it while signed in. Nothing here tells you whether the address matched.
        </p>
        <p className="field__hint">
          It stays pending for 7 days. Revoke it from the pending list, or send again to replace it.
        </p>
      </Dialog>
    );
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFailure(null);
    const address = email.trim();
    const chosen = emailInvitationTemplate(template);
    if (address === '') {
      setFailure('Enter the email address of the person to invite.');
      return;
    }
    if (chosen === null) {
      setFailure('Choose a role template: without one the person would join with no access.');
      return;
    }
    setPending(true);
    try {
      await sendEmailInvitation(org, { email: address, template: chosen });
      setRecorded(true);
    } catch (error) {
      setFailure(sendFailureText(error));
    } finally {
      setPending(false);
    }
  };

  const cancel = () => {
    if (!pending) onCancel();
  };

  return (
    <Dialog
      title={`Invite by email to ${scopeName}`}
      lede="Invites a person who already has an account here. Only accounts that signed up with a verified email can be reached; invited, SCIM-provisioned and federated accounts cannot."
      initialFocus={first}
      onCancel={(event) => {
        event.preventDefault();
        cancel();
      }}
      actions={
        <>
          <Button type="button" disabled={pending} onClick={cancel}>
            Cancel
          </Button>
          <Button
            type="submit"
            form={formId}
            variant="primary"
            disabled={pending}
            aria-busy={pending ? true : undefined}
          >
            {pending ? 'Inviting…' : 'Invite by email'}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={(event) => void submit(event)} noValidate>
        {failure === null ? null : <Alert>{failure}</Alert>}
        <Input
          ref={first}
          label="Email address"
          name="email"
          type="email"
          autoComplete="off"
          spellCheck={false}
          required
          disabled={pending}
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          hint="The exact address the person verified when they signed up."
        />
        <Select
          label="Role template"
          disabled={pending}
          value={template}
          onChange={(event) => setTemplate(event.target.value)}
          hint={`Required. Applied at ${scopeName} when the person accepts; each grant stays individually revocable.`}
        >
          <option value="" disabled>Choose a template</option>
          {emailInvitationTemplates().map((candidate) => (
            <option key={candidate.id} value={candidate.id}>
              {candidate.id}
            </option>
          ))}
        </Select>
      </form>
    </Dialog>
  );
}
