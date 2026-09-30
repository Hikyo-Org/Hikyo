import { CeremonyNotice } from '../../ui/CeremonyNotice.tsx';
import { useState } from 'react';
import {
  createServiceAccountFailureText,
  deleteServiceAccountFailureText,
  serviceAccountNameRefusal,
  useCreateServiceAccount,
  useDeleteServiceAccount,
  useRefreshGrants,
  useRefreshServiceAccounts,
  type ProjectRef,
  type ServiceAccount,
} from '../../api/identities.ts';
import { TypedNameConfirm } from '../Sections.tsx';
import { useNavigationGuard } from '../../app/useNavigationGuard.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { Dialog } from '../../ui/Dialog.tsx';

/**
 * CreateAccountDialog seeds a project's machine inventory from the browser.
 *
 * The body is exactly the locked create contract, `{ name, kind }`. There is
 * no description field because the contract has none, and `kind` is a form field
 * rather than an edit because it is immutable at creation. The name is refused
 * HERE (empty or over 64) rather than as a 400, and the trimmed name is what is
 * sent so the length checked is the length the server sees.
 */
export function CreateAccountDialog({
  project,
  onClose,
  onCreated,
}: {
  project: ProjectRef;
  onClose: () => void;
  onCreated: (name: string, kind: ServiceAccount['kind']) => void;
}) {
  const create = useCreateServiceAccount(project);
  const refresh = useRefreshServiceAccounts(project);
  const [name, setName] = useState('');
  const [kind, setKind] = useState<ServiceAccount['kind']>('workload');
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    // Validated and sent byte-for-byte, untrimmed: what is checked is what the
    // server stores, so the client never changes the name contract behind the
    // operator's back.
    const nameRefusal = serviceAccountNameRefusal(name);
    if (nameRefusal !== null) {
      setFailure(nameRefusal);
      return;
    }
    setBusy(true);
    setFailure(null);
    try {
      await create.mutateAsync({ name, kind });
      onCreated(name, kind);
    } catch (error) {
      // A create that returned a lost or unparseable response may still have
      // committed, and the inventory is the only place it would show. Refresh
      // regardless, harmless on a clean refusal, and let the failure text draw
      // the may-have-committed distinction.
      refresh();
      setFailure(createServiceAccountFailureText(error));
    } finally {
      setBusy(false);
    }
  };

  // An in-flight create is not dismissible: Escape, Back or unload here would
  // hide a mutation that may commit.
  useNavigationGuard(busy, () => {});

  return (
    <Dialog
      title="Create service account"
      lede="A machine principal this project owns. Its kind is fixed at creation: a workload delivers to a running process, an automation runs off-box on a schedule or in CI. A fresh account holds no grants and reaches nothing until one is added."
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
      actions={
        <>
          <Button type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            variant="primary"
            type="button"
            disabled={busy}
            onClick={() => void submit()}
          >
            {busy ? 'Creating…' : 'Create service account'}
          </Button>
        </>
      }
    >
      <fieldset className="machine__lock" disabled={busy}>
        <div className="field">
          <label htmlFor="create-account-name">Name</label>
          <input
            id="create-account-name"
            className="mono"
            value={name}
            autoComplete="off"
            spellCheck={false}
            maxLength={64}
            onChange={(event) => {
              setName(event.target.value);
              setFailure(null);
            }}
          />
        </div>

        <div className="field">
          <label htmlFor="create-account-kind">Kind (immutable)</label>
          <select
            id="create-account-kind"
            value={kind}
            onChange={(event) => {
              setKind(event.target.value === 'automation' ? 'automation' : 'workload');
              setFailure(null);
            }}
          >
            <option value="workload">workload: delivers to a running process</option>
            <option value="automation">automation: runs off-box, on a schedule or in CI</option>
          </select>
        </div>
      </fieldset>

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

    </Dialog>
  );
}

/**
 * DeleteAccountDialog deprovisions a machine principal behind a typed-name
 * confirmation.
 *
 * The server delete is a CASCADE, not a dependency refusal: every credential is
 * revoked and every grant released in one transaction, then the principal is
 * removed. So the dialog states that truth, how many live credentials go, and
 * that the grants go with them, rather than warning of a refusal the contract
 * does not raise. There is deliberately NO passkey here: deprovisioning runs
 * under the plain capability with no disclosure gate, because requiring a
 * ceremony to kill a compromised workload would be a self-inflicted delay.
 */
export function DeleteAccountDialog({
  project,
  account,
  onClose,
  onDeleted,
}: {
  project: ProjectRef;
  account: ServiceAccount;
  onClose: () => void;
  onDeleted: (name: string) => void;
}) {
  const remove = useDeleteServiceAccount(project);
  const refreshAccounts = useRefreshServiceAccounts(project);
  const refreshGrants = useRefreshGrants(project);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    setFailure(null);
    try {
      await remove.mutateAsync(account.id);
      onDeleted(account.name);
    } catch (error) {
      // A delete that committed removed the account and released its grants, so
      // both surfaces are re-read on the failure path, never the credential
      // listing, which would race the account refetch into a 404 and flip the
      // whole surface to error. The failure text says whether the delete may
      // still have landed.
      refreshAccounts();
      refreshGrants();
      setFailure(deleteServiceAccountFailureText(error));
    } finally {
      setBusy(false);
    }
  };

  // An in-flight delete is not dismissible by Escape, Back or unload.
  useNavigationGuard(busy, () => {});

  const live = account.live_credentials;

  return (
    <Dialog
      title={`Delete service account · ${account.name}`}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
      actions={
        <Button type="button" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
      }
    >
      {/* The cap comes first, so the dim lede below it stays in the body
          rather than moving above the sentence it qualifies. */}
      <CeremonyNotice>
        {`This deletes ${account.name} and everything attached to it in one act: ${String(live)} live credential${live === 1 ? '' : 's'} revoked, each stops authenticating at once, and every environment grant released. It does not cascade to anything else, and it cannot be undone.`}
      </CeremonyNotice>
      <p className="dialog__lede">
        Any bearer token or federated binding this account issued authenticates nothing the moment
        the delete lands. Distribute the replacement first if a workload still depends on it.
      </p>

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

      <TypedNameConfirm
        label="Confirm the account name to delete it"
        expect={account.name}
        action="Delete service account"
        hint={
          <>
            Type <span className="mono">{account.name}</span> to enable deletion.
          </>
        }
        busy={busy}
        onConfirm={() => void submit()}
      />
    </Dialog>
  );
}
