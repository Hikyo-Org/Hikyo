import { useEffect, useRef, useState } from 'react';
import { isoDay, type ProjectRef } from '../../api/identities.ts';
import {
  leaseActionRefusalText,
  leaseMintFailureText,
  leaseMintRefusalText,
  mintLease,
  useRefreshLeases,
  useRenewLease,
  useRevokeLease,
  useSettleLease,
  type DynamicLease,
  type DynamicProvider,
  type LeaseMinted,
} from '../../api/dynamic.ts';
import { writeClipboard } from '../../app/clipboard.ts';
import { useNavigationGuard } from '../../app/useNavigationGuard.ts';
import { runPasskeyCeremony } from '../../api/values.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import {
  type IsMintSubmitting,
  type MintBoundaryFields,
  type MintLifecycle,
  type MoveMint,
} from '../mintLifecycle.ts';

/** A queued lease-lifecycle action awaiting confirmation. */
export type LeaseAction = {
  readonly verb: 'renew' | 'revoke' | 'settle';
  readonly environmentId: string;
  readonly environmentName: string;
  readonly lease: DynamicLease;
};

/** An environment as the lease-mint form addresses one. */
type EnvOption = { readonly id: string; readonly name: string };

/**
 * The frozen inputs of a lease mint. Like MintRequest it carries only the
 * addressed provider/environment and the labels the review panel shows, never a
 * response field, so the disclosed password lives only in the lifecycle result.
 */
export type LeaseMintRequest = MintBoundaryFields & {
  readonly providerId: string;
  readonly providerLabel: string;
  readonly environmentId: string;
  readonly environmentName: string;
  readonly maxTtlSeconds: number;
};

/**
 * LeaseMintDialog is the display-once lease mint.
 *
 * It reuses the mint lifecycle (request-addressed completion, stored-confirmation
 * gate, boundary masking) that the credential mint runs on. A human mint takes a
 * passkey reauthentication over the chosen environment, then the server discloses
 * the role name and its password EXACTLY once, a retry is a new lease, never the
 * old secret.
 */
export function LeaseMintDialog({
  project,
  sessionId,
  providers,
  environments,
  lifecycle,
  move,
  isSubmitting,
  nextRequestId,
  onClose,
}: {
  project: ProjectRef;
  sessionId: string | null;
  providers: readonly DynamicProvider[];
  environments: readonly EnvOption[];
  lifecycle: MintLifecycle<LeaseMintRequest, LeaseMinted>;
  move: MoveMint<LeaseMintRequest, LeaseMinted>;
  isSubmitting: IsMintSubmitting;
  nextRequestId: () => number;
  onClose: () => void;
}) {
  const refreshLeases = useRefreshLeases(project);
  const confirmation = useRef<HTMLInputElement>(null);
  const [providerId, setProviderId] = useState(providers[0]?.id ?? '');
  const [environmentId, setEnvironmentId] = useState(environments[0]?.id ?? '');
  const [ttl, setTtl] = useState('3600');
  const [formError, setFormError] = useState<string | null>(null);

  const disclosed = lifecycle.kind === 'disclosed' ? lifecycle : null;
  const disclosedValue = disclosed?.result.password ?? null;
  const busy = lifecycle.kind === 'submitting';
  const failure = lifecycle.kind === 'failed' ? lifecycle.error : null;

  useEffect(() => {
    if (disclosedValue !== null) {
      confirmation.current?.focus();
    }
  }, [disclosedValue]);

  const run = async () => {
    if (sessionId === null) {
      setFormError('The current session could not be read. Reload before minting a lease.');
      return;
    }
    const provider = providers.find((p) => p.id === providerId);
    const environment = environments.find((e) => e.id === environmentId);
    if (provider === undefined || environment === undefined) {
      setFormError('Choose a provider and an environment.');
      return;
    }
    const seconds = Number(ttl);
    if (!Number.isInteger(seconds) || seconds <= 0) {
      setFormError('Enter the maximum lifetime as a whole number of seconds.');
      return;
    }
    setFormError(null);
    const request: LeaseMintRequest = {
      id: nextRequestId(),
      sessionId,
      org: project.org,
      project: project.project,
      providerId: provider.id,
      providerLabel: provider.origin,
      environmentId: environment.id,
      environmentName: environment.name,
      maxTtlSeconds: seconds,
    };
    const reviewed = move({ type: 'review', request });
    if (!reviewed.accepted || reviewed.state.kind !== 'reviewing') {
      return;
    }
    const started = move({ type: 'submit' });
    if (!started.accepted || started.state.kind !== 'submitting') {
      return;
    }
    const active = started.state.request;
    // `issued` is the difference between "nothing happened" and "a live role
    // whose password is gone forever": once the mint request leaves, a failure
    // says nothing about whether the server committed.
    let issued = false;
    try {
      await runPasskeyCeremony({
        operation: 'mint',
        environmentId: active.environmentId,
        keyIds: [],
      });
      if (!isSubmitting(active.id)) {
        return;
      }
      issued = true;
      const minted = await mintLease(
        { org: active.org, project: active.project, environment: active.environmentId },
        { providerId: active.providerId, maxTtlSeconds: active.maxTtlSeconds },
      );
      move({ type: 'succeeded', requestId: active.id, result: minted });
      refreshLeases(active.environmentId);
    } catch (error) {
      if (issued) {
        refreshLeases(active.environmentId);
        move({ type: 'failed', requestId: active.id, error: leaseMintFailureText(error) });
      } else {
        move({ type: 'failed', requestId: active.id, error: leaseMintRefusalText(error) });
      }
    }
  };

  const dismiss = () => {
    if (lifecycle.kind === 'idle') {
      onClose();
      return;
    }
    const result = move({ type: 'dismiss' });
    if (result.state.kind === 'idle') {
      onClose();
    }
  };

  useNavigationGuard(busy || (disclosed !== null && !disclosed.stored), dismiss);

  return (
    <Dialog
      title={disclosed === null ? 'Mint dynamic-secret lease' : 'Lease minted, shown exactly once'}
      onCancel={(event) => {
        event.preventDefault();
        dismiss();
      }}
      actions={
        disclosed === null ? (
          <>
            <Button type="button" onClick={dismiss} disabled={busy}>
              Cancel
            </Button>
            <Button
              variant="primary"
              type="button"
              disabled={busy}
              onClick={() => void run()}
            >
              {busy ? 'Minting…' : 'Use a passkey and mint'}
            </Button>
          </>
        ) : (
          <Button variant="primary" type="button" onClick={dismiss}>
            Done
          </Button>
        )
      }
    >
      {disclosed === null ? (
        <>
          <p className="ceremony__stepup">
            <span className="alert__glyph" aria-hidden="true">
              ⚿
            </span>
            <span>
              <strong>Confirm it&apos;s you.</strong> The password is delivered display-once, to you.
              A human mint takes a passkey reauthentication over the chosen environment and a
              disclosure capability there.
            </span>
          </p>

          <fieldset className="machine__lock" disabled={busy}>
            <div className="field">
              <label htmlFor="lease-mint-provider">Provider</label>
              <select
                id="lease-mint-provider"
                value={providerId}
                onChange={(event) => {
                  setProviderId(event.target.value);
                  setFormError(null);
                }}
              >
                {providers.map((provider) => (
                  <option key={provider.id} value={provider.id}>
                    {provider.origin}
                  </option>
                ))}
              </select>
            </div>

            <div className="field">
              <label htmlFor="lease-mint-environment">Environment</label>
              <select
                id="lease-mint-environment"
                value={environmentId}
                onChange={(event) => {
                  setEnvironmentId(event.target.value);
                  setFormError(null);
                }}
              >
                {environments.map((environment) => (
                  <option key={environment.id} value={environment.id}>
                    {environment.name}
                  </option>
                ))}
              </select>
            </div>

            <div className="field">
              <label htmlFor="lease-mint-ttl">Maximum lifetime (seconds)</label>
              <input
                id="lease-mint-ttl"
                className="mono"
                inputMode="numeric"
                value={ttl}
                onChange={(event) => {
                  setTtl(event.target.value);
                  setFormError(null);
                }}
              />
            </div>
          </fieldset>
          <p className="ceremony__scope">
            The provider clamps the lifetime to its own ceiling, and PostgreSQL enforces expiry with{' '}
            <code>VALID UNTIL</code> even if Hikyo is down.
          </p>

          {formError !== null ? (
            <Alert>{formError}</Alert>
          ) : null}
          {failure !== null ? (
            <Alert>{failure}</Alert>
          ) : null}

        </>
      ) : (
        <>
          <div className="field">
            <label htmlFor="lease-mint-username">Role name</label>
            <p className="mono machine__token" id="lease-mint-username">
              {disclosed.result.username}
            </p>
          </div>
          <div className="field">
            <label htmlFor="lease-mint-password">Password</label>
            <p className="mono machine__token" id="lease-mint-password">
              {disclosed.result.password}
            </p>
          </div>
          {disclosed.result.expires_at !== undefined && disclosed.result.expires_at !== null ? (
            <Alert tone="warn">{`This lease expires ${isoDay(disclosed.result.expires_at)}. The provider may have shortened the lifetime you asked for.`}</Alert>
          ) : null}
          <p className="ceremony__cap" role="status">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              This password is never retrievable again. The list shows metadata only, and a renewal
              never returns it. Store it in the consuming workload now; if it is lost, revoke this
              lease and mint a fresh one.
            </span>
          </p>
          <Button
            type="button"
            onClick={async () => {
              const result = await writeClipboard(disclosed.result.password);
              move({
                type: 'copy-status',
                requestId: disclosed.request.id,
                message:
                  result === 'ok'
                    ? 'Copied. The clipboard is now the only copy outside its target system.'
                    : 'This browser refused clipboard access, so nothing was copied.',
              });
            }}
          >
            Copy password
          </Button>
          {disclosed.copyStatus === null ? null : (
            <p className="notice" role="status">{/* markup-check: copy receipt, not feedback */}
              <span className="alert__glyph" aria-hidden="true">
                ⧉
              </span>
              <span>{disclosed.copyStatus}</span>
            </p>
          )}
          <Checkbox
            label="I have stored this password in its target workload."
            ref={confirmation}
            checked={disclosed.stored}
            onChange={(event) => {
              move({ type: 'confirm-stored', stored: event.target.checked });
            }}
          />
          {disclosed.heldBack ? (
            <Alert>Confirm you have stored it: there is no second look at this password.</Alert>
          ) : null}
        </>
      )}
    </Dialog>
  );
}

/**
 * LeaseActionDialog confirms and queues a renew, revoke or settle. Each is
 * QUEUED: the server records the intent and the worker carries it out, so the
 * copy says "queued" rather than promising a done state the row does not yet
 * show. Renew offers an optional new ceiling; revoke and settle are one-tap.
 */
export function LeaseActionDialog({
  project,
  action,
  onClose,
  onDone,
}: {
  project: ProjectRef;
  action: LeaseAction;
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const renew = useRenewLease(project);
  const revoke = useRevokeLease(project);
  const settle = useSettleLease(project);
  const [ttl, setTtl] = useState('');
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const handle = action.lease.provider_handle;

  const submit = async () => {
    setBusy(true);
    setFailure(null);
    try {
      if (action.verb === 'renew') {
        let seconds: number | null = null;
        if (ttl.trim() !== '') {
          const parsed = Number(ttl);
          if (!Number.isInteger(parsed) || parsed <= 0) {
            setFailure('Enter the new ceiling as a whole number of seconds, or leave it blank.');
            setBusy(false);
            return;
          }
          seconds = parsed;
        }
        await renew.mutateAsync({
          environment: action.environmentId,
          lease: action.lease.id,
          maxTtlSeconds: seconds,
        });
        onDone(`Queued a renewal of ${handle}. The worker extends the role; the row moves when it does.`);
      } else if (action.verb === 'revoke') {
        await revoke.mutateAsync({ environment: action.environmentId, lease: action.lease.id });
        onDone(`Queued the revocation of ${handle}. The worker drops the role; the row moves when it does.`);
      } else {
        await settle.mutateAsync({ environment: action.environmentId, lease: action.lease.id });
        onDone(`Queued a reconcile of ${handle}. The worker re-probes the provider and settles the lease.`);
      }
    } catch (error) {
      setFailure(leaseActionRefusalText(action.verb, error));
    } finally {
      setBusy(false);
    }
  };

  useNavigationGuard(busy, () => {});

  const title =
    action.verb === 'renew'
      ? `Renew lease · ${handle}`
      : action.verb === 'revoke'
        ? `Revoke lease · ${handle}`
        : `Settle lease · ${handle}`;

  const lede =
    action.verb === 'renew'
      ? 'Queues a renewal: the worker extends the role at the provider, never past the lease’s maximum TTL. Renewal re-checks read over this environment.'
      : action.verb === 'revoke'
        ? 'Queues a revocation: the worker drops the role at the provider. This is the fail-safe teardown: it succeeds even after the workload’s grants are gone.'
        : 'This lease is in an ambiguous state. Settling re-triggers reconcile: the worker re-probes the provider and settles the lease to its true state.';

  return (
    <Dialog
      title={title}
      lede={lede}
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
            {busy
              ? 'Queuing…'
              : action.verb === 'renew'
                ? 'Queue renewal'
                : action.verb === 'revoke'
                  ? 'Queue revocation'
                  : 'Queue reconcile'}
          </Button>
        </>
      }
    >
      {action.verb === 'renew' ? (
        <fieldset className="machine__lock" disabled={busy}>
          <div className="field">
            <label htmlFor="lease-action-ttl">New maximum lifetime (seconds, optional)</label>
            <input
              id="lease-action-ttl"
              className="mono"
              inputMode="numeric"
              value={ttl}
              placeholder="leave blank to keep the current ceiling"
              onChange={(event) => {
                setTtl(event.target.value);
                setFailure(null);
              }}
            />
          </div>
        </fieldset>
      ) : null}

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

    </Dialog>
  );
}
