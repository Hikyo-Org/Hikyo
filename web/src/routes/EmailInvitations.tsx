import { listFailureText, useEmailInvitations, useRevokeEmailInvitation, revokeFailureText } from '../api/emailInvitations.ts';
import { formatWhen } from '../lib/formatWhen.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Panel } from './Sections.tsx';

/**
 * The organisation's pending email invitations (#1207). Rows carry only what
 * the administrator typed: the list never reveals whether an address matched.
 */
export function EmailInvitations({
  org,
  invitedByName,
  onRevoked,
}: {
  org: string;
  invitedByName: (principal: string) => string;
  onRevoked: (email: string) => void;
}) {
  const invitations = useEmailInvitations(org, true);
  const revoke = useRevokeEmailInvitation(org);
  const items = invitations.data?.items ?? [];

  return (
    <Panel id="members-email-invitations" title="Pending email invitations">
      {invitations.isPending ? <p role="status">Loading invitations…</p> : null}
      {invitations.isError ? <Alert>{listFailureText(invitations.error)}</Alert> : null}
      {revoke.isError ? <Alert>{revokeFailureText(revoke.error)}</Alert> : null}
      {invitations.isSuccess && items.length === 0 ? <p role="status">No pending email invitations.</p> : null}
      {items.length === 0 ? null : (
        <div className="table-scroll">
          <table className="values__table">
            <caption className="visually-hidden">Pending email invitations for this organisation</caption>
            <thead>
              <tr>
                <th scope="col">Email</th>
                <th scope="col">Template</th>
                <th scope="col">Invited by</th>
                <th scope="col">Expires</th>
                <th scope="col"><span className="visually-hidden">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>{item.email}</td>
                  <td>{item.template}</td>
                  <td>{invitedByName(item.invited_by)}</td>
                  <td>{formatWhen(item.expires_at)}</td>
                  <td>
                    <Button
                      type="button"
                      variant="danger"
                      aria-label={`Revoke the invitation for ${item.email}`}
                      disabled={revoke.isPending}
                      onClick={() => revoke.mutate(item.id, { onSuccess: () => onRevoked(item.email) })}
                    >
                      Revoke
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}
