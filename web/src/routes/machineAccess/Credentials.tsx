import { CeremonyNotice } from '../../ui/CeremonyNotice.tsx';
import { useEffect, useRef } from 'react';
import {
  expiryLabel,
  identityRefusalText,
  mintCredential,
  mintFailureText,
  useRefreshAccount,
  type MachineCredential,
} from '../../api/identities.ts';
import { writeClipboard } from '../../app/clipboard.ts';
import { useNavigationGuard } from '../../app/useNavigationGuard.ts';
import { runPasskeyCeremony } from '../../api/values.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { type IsMintSubmitting, type MintLifecycle, type MoveMint } from '../mintLifecycle.ts';

/** The expiry in words, with the tier echoed by colour beside them, never instead. */
export function ExpiryBadge({ credential, now }: { credential: MachineCredential; now: Date }) {
  const expiry = expiryLabel(credential, now);
  return (
    <Badge tone={expiry.tier === 'danger' ? 'danger' : expiry.tier === 'warn' ? 'changed' : 'neutral'}>
      {expiry.text}
    </Badge>
  );
}

/**
 * MintDialog renders the display-once ceremony. Its parent lifecycle is the
 * only SPA state that can ever hold a credential value, and only while that
 * lifecycle is `disclosed`.
 *
 * The step-up names the POST-STATE formula rather than what the mint adds: a
 * mint adds no grants, so a replacement credential is not a smaller act than a
 * new one, it hands the same reach to a fresh value. When the account reaches
 * no plaintext the disclosure conjunct is vacuous and the server asks for no
 * reauthentication, which the panel says in words rather than performing a
 * ceremony that authorises nothing.
 */
export function MintDialog({
  lifecycle,
  move,
  isSubmitting,
}: {
  lifecycle: Exclude<MintLifecycle, { readonly kind: 'idle' }>;
  move: MoveMint;
  isSubmitting: IsMintSubmitting;
}) {
  const request = lifecycle.request;
  const refresh = useRefreshAccount({ org: request.org, project: request.project });
  const confirmation = useRef<HTMLInputElement>(null);
  const disclosed = lifecycle.kind === 'disclosed' ? lifecycle : null;
  const disclosedValue = disclosed?.result.value ?? null;
  const busy = lifecycle.kind === 'submitting';
  const failure = lifecycle.kind === 'failed' ? lifecycle.error : null;

  // The panel the value arrives on replaces the control that was focused, so
  // focus goes to the one decision left: the stored-confirmation checkbox.
  useEffect(() => {
    if (disclosedValue !== null) {
      confirmation.current?.focus();
    }
  }, [disclosedValue]);

  const run = async () => {
    const started = move({ type: 'submit' });
    if (!started.accepted || started.state.kind !== 'submitting') {
      return;
    }
    const active = started.state.request;
    // `issued` is the difference between "nothing happened" and "something may
    // have". Once the request leaves, a failure says nothing about whether the
    // server committed, and a mint that committed and whose response was lost
    // is a live credential whose value is gone forever.
    let issued = false;
    try {
      // One reauthentication per environment the account reaches in the
      // post-state, which is exactly the set the server will consume. An empty
      // reach runs no ceremony because there is no disclosure to authorise.
      for (const environment of active.reach) {
        await runPasskeyCeremony({
          operation: 'mint',
          environmentId: environment.id,
          keyIds: [],
        });
        if (!isSubmitting(active.id)) {
          return;
        }
      }
      if (!isSubmitting(active.id)) {
        return;
      }
      issued = true;
      const minted = await mintCredential(
        { org: active.org, project: active.project },
        active.accountId,
      );
      move({ type: 'succeeded', requestId: active.id, result: minted });
      refresh(active.accountId);
    } catch (error) {
      if (issued) {
        // Re-read the rows so the operator can see, and revoke, whatever may
        // have landed.
        refresh(active.accountId);
        move({ type: 'failed', requestId: active.id, error: mintFailureText(error) });
      } else {
        move({ type: 'failed', requestId: active.id, error: identityRefusalText(error) });
      }
    }
  };

  const dismiss = () => move({ type: 'dismiss' });

  // Back, reload and tab close are dismissals too, an in-flight mint or an
  // unstored value must not be lost to a navigation the buttons would refuse.
  useNavigationGuard(busy || (disclosed !== null && !disclosed.stored), dismiss);

  return (
    <Dialog
      title={
        disclosed === null
          ? `${request.rotating ? 'Rotate' : 'Mint'} credential · ${request.accountName}`
          : 'Credential minted, shown exactly once'
      }
      onCancel={(event) => {
        // Escape must not be a way to lose a value nothing can return.
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
              {busy
                ? 'Minting…'
                : request.reach.length === 0
                  ? 'Mint credential'
                  : 'Use a passkey and mint'}
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
              <strong>Confirm it&apos;s you.</strong> The value is delivered display-once, to you.
              The formula is manage-identities on this project and a disclosure capability over
              every environment this account reaches in the resulting post-state, not only the ones
              a mint adds, because a mint adds none.
            </span>
          </p>
          <p className="ceremony__scope">
            {request.reach.length === 0
              ? 'This account reaches no plaintext in the resulting post-state, so no disclosure capability and no reauthentication are required. Its deliveries stay configuration and secret presence only.'
              : `This account decrypts ${request.reach.map((r) => r.name).join(', ')}. Each takes its own passkey reauthentication before the value is minted.`}
          </p>
          {request.rotating ? (
            <p className="dialog__lede">
              The prior value is never returned. The predecessor keeps authenticating until you
              revoke it; rotation and revocation are separate, deliberate acts, so a mint that
              lands and a revoke that does not leaves two live credentials rather than none.
            </p>
          ) : null}
          {failure !== null ? (
            <Alert>{failure}</Alert>
          ) : null}
        </>
      ) : (
        <>
          <p className="mono machine__token">{disclosed.result.value}</p>
          <p className="cred__meta">
            {disclosed.result.expires_at === null
              ? 'Expiry: indefinite.'
              : disclosed.result.expires_at === undefined
                ? 'Expiry: unknown.'
                : <>Expires <time dateTime={disclosed.result.expires_at}>{new Date(disclosed.result.expires_at).toLocaleString()}</time></>}
          </p>
          {disclosed.result.clamped ? (
            <Alert tone="warn">
              The instance lifetime ceiling shortened this credential. It expires earlier than the
              default asked for, said now rather than discovered when it dies.
            </Alert>
          ) : null}
          <CeremonyNotice>
            This value is never retrievable again. The list shows metadata only and rotation never
            returns it. Store it in the consuming system now; if it is lost, revoke this
            credential and mint a fresh one.
          </CeremonyNotice>
          <Button
            type="button"
            onClick={async () => {
              const result = await writeClipboard(disclosed.result.value);
              move({
                type: 'copy-status',
                requestId: request.id,
                message:
                  result === 'ok'
                    ? 'Copied. The clipboard is now the only copy outside its target system.'
                    : 'This browser refused clipboard access, so nothing was copied.',
              });
            }}
          >
            Copy to clipboard
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
            id="mint-stored"
            label="I have stored this credential in its target system."
            ref={confirmation}
            checked={disclosed.stored}
            onChange={(event) => {
              move({ type: 'confirm-stored', stored: event.target.checked });
            }}
          />
          {disclosed.heldBack ? (
            <Alert>Confirm you have stored it: there is no second look at this value.</Alert>
          ) : null}
        </>
      )}
    </Dialog>
  );
}
