import { useId, useRef, useState, type FormEvent } from 'react';
import { Link } from 'react-router';

import {
  inviteFailureText,
  inviteMember,
  templatesAt,
  type InviteScope,
  type IssuedAuthority,
  type Level,
} from '../api/access.ts';
import { surfaceById } from '../app/navigation.ts';
import { Alert, DisplayOnceCopy } from './Sections.tsx';
import { useModalDialog } from './useModalDialog.ts';

/**
 * The member invitation ceremony (#568): the human-auth ADR's one account-
 * creation path, from the Members surface.
 *
 * Two stages. The form names the login handle, an optional display name and
 * an optional role template expanded at THIS scope in the same transaction.
 * The issued stage is display-once: the authority exists in component state
 * for exactly as long as this dialog is open, is never written to a query
 * cache (`inviteMember` is a plain async call, #498), and closing the dialog
 * is the last time anyone sees it.
 */
export function InviteDialog({
  scope,
  scopeName,
  origin,
  onDone,
  onCancel,
}: {
  scope: InviteScope;
  scopeName: string;
  /** The instance origin the establish hint names, e.g. `https://hikyo.example`. */
  origin: string;
  onDone: (text: string) => void;
  onCancel: () => void;
}) {
  const level: Level = scope.kind === 'instance' ? 'instance' : 'org';
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [template, setTemplate] = useState('');
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [issued, setIssued] = useState<{ principalId: string; authority: IssuedAuthority } | null>(
    null,
  );

  if (issued !== null) {
    const handle = username.trim();
    const doneText =
      template === ''
        ? `Invited ${handle} at ${scopeName} with no grants yet: they can sign in once they establish a credential, and see nothing until someone grants them something. The authority was shown once.`
        : `Invited ${handle} at ${scopeName} as ${template}. The authority was shown once; if it lapses, reset their credential.`;
    return (
      <IssuedAuthorityDialog
        title={`Invitation for ${handle}`}
        lede={`Hand this authority to ${handle} out of band. It is shown once, and it only ever establishes a password: no session, no assurance.`}
        username={handle}
        principalId={issued.principalId}
        issued={issued.authority}
        origin={origin}
        onClose={() => onDone(doneText)}
      />
    );
  }

  return (
    <InviteForm
      level={level}
      scopeName={scopeName}
      username={username}
      displayName={displayName}
      template={template}
      pending={pending}
      failure={failure}
      onUsername={setUsername}
      onDisplayName={setDisplayName}
      onTemplate={setTemplate}
      onCancel={() => {
        if (!pending) onCancel();
      }}
      onSubmit={async () => {
        setFailure(null);
        const handle = username.trim();
        if (handle === '') {
          setFailure('Name the login handle the invitee will sign in with.');
          return;
        }
        setPending(true);
        try {
          const result = await inviteMember(scope, {
            username: handle,
            displayName: displayName.trim(),
            template,
          });
          setIssued({
            principalId: result.principal_id,
            authority: { authority: result.authority, expiresAt: result.expires_at },
          });
        } catch (error) {
          setFailure(inviteFailureText(error));
        } finally {
          setPending(false);
        }
      }}
    />
  );
}

function InviteForm({
  level,
  scopeName,
  username,
  displayName,
  template,
  pending,
  failure,
  onUsername,
  onDisplayName,
  onTemplate,
  onCancel,
  onSubmit,
}: {
  level: Level;
  scopeName: string;
  username: string;
  displayName: string;
  template: string;
  pending: boolean;
  failure: string | null;
  onUsername: (value: string) => void;
  onDisplayName: (value: string) => void;
  onTemplate: (value: string) => void;
  onCancel: () => void;
  onSubmit: () => void;
}) {
  const first = useRef<HTMLInputElement>(null);
  const dialog = useModalDialog(first);
  const titleId = useId();
  const usernameId = useId();
  const displayNameId = useId();
  const templateId = useId();
  const templateHintId = useId();
  const templates = templatesAt(level);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onSubmit();
  };

  return (
    <dialog
      className="ceremony"
      ref={dialog}
      aria-labelledby={titleId}
      onCancel={(event) => {
        // Escape closes the native dialog on its own; without this the page
        // would still believe the ceremony is open.
        event.preventDefault();
        onCancel();
      }}
    >
      <form onSubmit={submit} noValidate>
        <h2 id={titleId} className="ceremony__title">
          Invite a member to {scopeName}
        </h2>
        <p className="ceremony__lede">
          Creates their account now. They set their own password with the authority you hand
          them; no email is sent.
        </p>
        {failure === null ? null : <Alert>{failure}</Alert>}
        <div className="field">
          <label htmlFor={usernameId}>Username</label>
          <input
            id={usernameId}
            ref={first}
            autoComplete="off"
            spellCheck={false}
            required
            disabled={pending}
            value={username}
            onChange={(event) => onUsername(event.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor={displayNameId}>Display name (optional)</label>
          <input
            id={displayNameId}
            autoComplete="off"
            disabled={pending}
            value={displayName}
            onChange={(event) => onDisplayName(event.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor={templateId}>Role template</label>
          <select
            id={templateId}
            aria-describedby={templateHintId}
            disabled={pending}
            value={template}
            onChange={(event) => onTemplate(event.target.value)}
          >
            <option value="">No initial grants</option>
            {templates.map((candidate) => (
              <option key={candidate.id} value={candidate.id}>
                {candidate.id}
              </option>
            ))}
          </select>
          <p id={templateHintId} className="field__hint">
            Expanded at {scopeName} in the same transaction; each grant stays individually
            revocable. With no template the account exists but reaches nothing.
          </p>
        </div>
        <div className="ceremony__actions">
          <button type="submit" className="btn btn--primary" disabled={pending} aria-busy={pending ? true : undefined}>
            {pending ? 'Inviting…' : 'Invite'}
          </button>
          <button type="button" className="btn" disabled={pending} onClick={onCancel}>
            Cancel
          </button>
        </div>
      </form>
    </dialog>
  );
}

/**
 * IssuedAuthorityDialog shows a credential-establishment authority exactly
 * once. Shared by the invitation and the Reset credential row action, which
 * differ only in their heading and in whether the invitee's handle is known.
 */
export function IssuedAuthorityDialog({
  title,
  lede,
  username,
  principalId,
  issued,
  origin,
  onClose,
}: {
  title: string;
  lede: string;
  /** The account's login handle when known; null renders a placeholder in the CLI hint. */
  username: string | null;
  /** The principal the authority belongs to, when the caller knows it. */
  principalId: string | null;
  issued: IssuedAuthority;
  origin: string;
  onClose: () => void;
}) {
  const dialog = useModalDialog();
  const titleId = useId();
  const expires = new Date(issued.expiresAt);
  const handle = username ?? '<their username>';

  return (
    <dialog
      className="ceremony"
      ref={dialog}
      aria-labelledby={titleId}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <h2 id={titleId} className="ceremony__title">
        {title}
      </h2>
      <p className="ceremony__lede">{lede}</p>
      {principalId === null ? null : (
        <p className="field__hint">
          Principal <span className="mono" data-testid="issued-principal">{principalId}</span>
        </p>
      )}
      <code className="mono display-once__value" data-testid="issued-authority">
        {issued.authority}
      </code>
      <DisplayOnceCopy
        value={issued.authority}
        success="Authority copied. Hand it over out of band; clipboard history may retain it."
      />
      <p className="field__hint">
        Expires {Number.isNaN(expires.getTime()) ? issued.expiresAt : expires.toLocaleString()}.
        Single use: once a password is set with it, it is spent.
      </p>
      <p className="field__hint">
        They establish it in the browser at {origin}
        {surfaceById('establish-credential').path}, or from a terminal:
      </p>
      <code className="instance-cli">
        $ hikyo account establish-credential --instance {origin} --as {handle}
      </code>
      <div className="ceremony__actions">
        <button type="button" className="btn btn--primary" onClick={onClose}>
          Close
        </button>
        <Link className="btn" to={surfaceById('establish-credential').path}>
          Open the establish page
        </Link>
      </div>
    </dialog>
  );
}
