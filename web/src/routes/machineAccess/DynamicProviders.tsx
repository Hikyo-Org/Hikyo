import { useState } from 'react';
import { useSensitiveState } from '../../api/sensitiveMutation.ts';
import { type ProjectRef } from '../../api/identities.ts';
import { TypedNameConfirm } from '../Sections.tsx';
import { ApiError } from '../../api/client.ts';
import {
  createDynamicProvider,
  createProviderRefusalText,
  deleteProviderRefusalText,
  revokeCredentialRefusalText,
  setCredentialRefusalText,
  setDynamicProviderCredential,
  useDeleteDynamicProvider,
  useRefreshProviders,
  useRevokeDynamicProviderCredential,
  type DynamicProvider,
} from '../../api/dynamic.ts';
import { useNavigationGuard } from '../../app/useNavigationGuard.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { Dialog } from '../../ui/Dialog.tsx';

/**
 * CreateProviderDialog configures a dynamic-secret provider.
 *
 * The server PROBES the origin with the admin credential before it stores
 * anything, so a create that succeeds is a provider Hikyo could reach and
 * authenticate against, the dialog says so rather than promising a store that
 * might be unreachable. There is no passkey here: configuring standing project
 * authority is `manage-identities`, not a per-environment disclosure.
 */
export function CreateProviderDialog({
  project,
  onClose,
  onCreated,
}: {
  project: ProjectRef;
  onClose: () => void;
  onCreated: (origin: string) => void;
}) {
  const refresh = useRefreshProviders(project);
  const [origin, setOrigin] = useState('');
  const [grantRole, setGrantRole] = useState('');
  const [credential, setCredential] = useSensitiveState('');
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (origin.trim() === '' || grantRole.trim() === '' || credential === '') {
      setFailure('Origin, grant role and the admin credential are all required. Nothing was created.');
      return;
    }
    setBusy(true);
    setFailure(null);
    try {
      await createDynamicProvider(project, {
        kind: 'postgres',
        origin,
        grant_role: grantRole,
        credential,
      });
      refresh();
      onCreated(origin);
    } catch (error) {
      // A create whose response was lost may still have committed; re-read the
      // listing so a committed provider shows even when the dialog reports failure.
      refresh();
      setFailure(createProviderRefusalText(error));
    } finally {
      setCredential('');
      setBusy(false);
    }
  };

  useNavigationGuard(busy, () => {});

  return (
    <Dialog
      title="Configure dynamic-secret provider"
      lede={
        <>
          PostgreSQL is the only provider kind. Hikyo connects over <code>verify-full</code> TLS and
          mints each lease role <code>IN ROLE</code> the grant role, so the lease inherits exactly
          the access you granted that parent. The admin credential is write-only: it is never
          returned, and the server dials the origin with it before storing anything.
        </>
      }
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
            {busy ? 'Configuring…' : 'Configure provider'}
          </Button>
        </>
      }
    >
      <fieldset className="machine__lock" disabled={busy}>
        <div className="field">
          <label htmlFor="create-provider-origin">Origin (host:port/dbname)</label>
          <input
            id="create-provider-origin"
            className="mono"
            value={origin}
            autoComplete="off"
            spellCheck={false}
            maxLength={2048}
            placeholder="db.internal:5432/app"
            onChange={(event) => {
              setOrigin(event.target.value);
              setFailure(null);
            }}
          />
        </div>

        <div className="field">
          <label htmlFor="create-provider-grant-role">Grant role</label>
          <input
            id="create-provider-grant-role"
            className="mono"
            value={grantRole}
            autoComplete="off"
            spellCheck={false}
            maxLength={63}
            onChange={(event) => {
              setGrantRole(event.target.value);
              setFailure(null);
            }}
          />
        </div>

        <div className="field">
          <label htmlFor="create-provider-credential">Admin credential (write-only)</label>
          <input
            id="create-provider-credential"
            className="mono"
            type="password"
            value={credential}
            autoComplete="off"
            spellCheck={false}
            maxLength={4096}
            onChange={(event) => {
              setCredential(event.target.value);
              setFailure(null);
            }}
          />
        </div>
      </fieldset>

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

    </Dialog>
  );
}

/**
 * SetCredentialDialog replaces a provider's write-only admin credential. It
 * re-probes on set, so a success means the new credential authenticates; the
 * prior one is never shown and is overwritten, not archived.
 */
export function SetCredentialDialog({
  project,
  provider,
  onClose,
  onSet,
}: {
  project: ProjectRef;
  provider: DynamicProvider;
  onClose: () => void;
  onSet: (origin: string) => void;
}) {
  const refresh = useRefreshProviders(project);
  const [credential, setCredential] = useSensitiveState('');
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (credential === '') {
      setFailure('The credential is required. The stored credential is unchanged.');
      return;
    }
    setBusy(true);
    setFailure(null);
    try {
      await setDynamicProviderCredential(project, { provider: provider.id, credential });
      refresh();
      onSet(provider.origin);
    } catch (error) {
      // A set whose response was lost may still have replaced the credential;
      // re-read so the credential-set date reflects a write that may have landed.
      refresh();
      setFailure(setCredentialRefusalText(error));
    } finally {
      setCredential('');
      setBusy(false);
    }
  };

  useNavigationGuard(busy, () => {});

  return (
    <Dialog
      title={`${provider.credential_present ? 'Replace' : 'Set'} admin credential · ${provider.origin}`}
      lede="Write-only: the credential is never read back. Hikyo dials the provider with it before storing it, so a failure here leaves the current credential in place."
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
            {busy ? 'Saving…' : 'Save credential'}
          </Button>
        </>
      }
    >
      <fieldset className="machine__lock" disabled={busy}>
        <div className="field">
          <label htmlFor="set-credential-value">Admin credential</label>
          <input
            id="set-credential-value"
            className="mono"
            type="password"
            value={credential}
            autoComplete="off"
            spellCheck={false}
            maxLength={4096}
            onChange={(event) => {
              setCredential(event.target.value);
              setFailure(null);
            }}
          />
        </div>
      </fieldset>

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

    </Dialog>
  );
}

/**
 * RevokeCredentialDialog clears a provider's admin credential.
 *
 * It states the fail-closed consequence the ticket requires: existing leases are
 * NOT torn down, their roles stay minted at the provider, but the worker can
 * no longer renew, revoke or expire them until a replacement credential is set.
 * A lease revocation that needs the credential will strand until then.
 */
export function RevokeCredentialDialog({
  project,
  provider,
  onClose,
  onRevoked,
}: {
  project: ProjectRef;
  provider: DynamicProvider;
  onClose: () => void;
  onRevoked: (origin: string) => void;
}) {
  const revoke = useRevokeDynamicProviderCredential(project);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    setFailure(null);
    try {
      await revoke.mutateAsync(provider.id);
      onRevoked(provider.origin);
    } catch (error) {
      setFailure(revokeCredentialRefusalText(error));
    } finally {
      setBusy(false);
    }
  };

  useNavigationGuard(busy, () => {});

  return (
    <Dialog
      title={`Revoke admin credential · ${provider.origin}`}
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
            {busy ? 'Revoking…' : 'Revoke credential'}
          </Button>
        </>
      }
    >
      <p className="ceremony__cap" role="status">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          This clears the stored credential. Existing leases stay minted at the provider, but Hikyo
          can no longer renew, revoke or expire them, a revocation that needs the credential will
          strand, until you set a replacement. New mints are refused while there is no credential.
        </span>
      </p>

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

    </Dialog>
  );
}

/**
 * DeleteProviderDialog removes a provider behind a typed-origin confirmation.
 *
 * The server refuses while the provider has live leases unless the cascade is
 * confirmed, in which case it queues every one of them for revocation. So the
 * dialog states that truth, how many live leases go, and requires the cascade
 * checkbox when there are any, rather than letting the operator meet the refusal
 * as a 409.
 */
export function DeleteProviderDialog({
  project,
  provider,
  liveLeaseCount,
  leasesKnown,
  onClose,
  onDeleted,
}: {
  project: ProjectRef;
  provider: DynamicProvider;
  liveLeaseCount: number;
  leasesKnown: boolean;
  onClose: () => void;
  onDeleted: (origin: string, revokedCount: number) => void;
}) {
  const remove = useDeleteDynamicProvider(project);
  const [revokeAll, setRevokeAll] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // A lease that appeared AFTER the cached count was read makes the server refuse
  // with 409 even though this dialog thought there were none. Latch that so the
  // cascade option appears and the operator can confirm and retry, rather than
  // being stuck behind a hidden checkbox.
  const [serverDemandsCascade, setServerDemandsCascade] = useState(false);

  // With a known live-lease count the cascade is a required, explicit tick; when
  // the lease listing could not be read the count is unknown, so the checkbox is
  // offered; and a 409 from the server's own guard forces it on either way.
  const cascadeRequired = (leasesKnown && liveLeaseCount > 0) || serverDemandsCascade;
  const showCascade = cascadeRequired || !leasesKnown;
  const confirmDisabled = busy || (cascadeRequired && !revokeAll);

  const submit = async () => {
    setBusy(true);
    setFailure(null);
    try {
      const result = await remove.mutateAsync({ provider: provider.id, revokeAll });
      onDeleted(provider.origin, result.revoked_lease_ids.length);
    } catch (error) {
      // The server's own live-leases guard (409) can fire on a lease that
      // appeared after the cached count: reveal the cascade so a retry can carry it.
      if (error instanceof ApiError && error.status === 409) {
        setServerDemandsCascade(true);
      }
      setFailure(deleteProviderRefusalText(error));
    } finally {
      setBusy(false);
    }
  };

  useNavigationGuard(busy, () => {});

  return (
    <Dialog
      title={`Delete provider · ${provider.origin}`}
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
      <p className="ceremony__cap" role="status">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          {leasesKnown
            ? liveLeaseCount === 0
              ? 'This provider has no live leases. Deleting it removes its configuration and its stored credential. This cannot be undone.'
              : `This provider has ${String(liveLeaseCount)} live lease${liveLeaseCount === 1 ? '' : 's'}. Deleting it queues ${liveLeaseCount === 1 ? 'that lease' : 'each of them'} for revocation, then removes the provider. This cannot be undone.`
            : 'The lease listing could not be read, so the number of live leases is unknown. If any exist, confirm the cascade below or the delete will be refused.'}
        </span>
      </p>

      {showCascade ? (
        <Checkbox
          label="Revoke every live lease of this provider as part of the delete."
          checked={revokeAll}
          disabled={busy}
          onChange={(event) => {
            setRevokeAll(event.target.checked);
            setFailure(null);
          }}
        />
      ) : null}

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

      <TypedNameConfirm
        label="Confirm the provider origin to delete it"
        expect={provider.origin}
        action="Delete provider"
        hint={
          <>
            Type <span className="mono">{provider.origin}</span> to enable deletion.
          </>
        }
        busy={confirmDisabled}
        onConfirm={() => void submit()}
      />
    </Dialog>
  );
}
