import type { WorkspaceHandoffStepUp, WorkspaceHandoffTransaction } from '@hikyo/client';
import { approveWorkspaceHandoffOp, showWorkspaceHandoffOp } from '@hikyo/operations';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useEffect, useState, type FormEvent } from 'react';

import { parsed } from '../api/client.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { ceremonyRefusalText, runPasskeyCeremony, runTOTPCeremony } from '../api/values.ts';
import { Login } from './Login.tsx';
import { useCeremonyTask, type CeremonyTask } from './useCeremonyTask.ts';

function isWorkspaceHandoffStepUp(
  transaction: WorkspaceHandoffTransaction,
): transaction is WorkspaceHandoffStepUp {
  return transaction.purpose === 'step-up';
}

/** Consent has a local expiry wall as well as the authoritative server check. */
function useHandoffExpiry(expiresAt: string | undefined): boolean {
  const [clock, setClock] = useState(() => Date.now());
  const deadline = expiresAt === undefined ? Number.NaN : Date.parse(expiresAt);
  const expired = !Number.isFinite(deadline) || Math.max(clock, Date.now()) >= deadline;
  useEffect(() => {
    if (expired) return;
    const timer = globalThis.setTimeout(
      () => setClock(Date.now()),
      Math.min(2_147_483_647, Math.max(0, deadline - Date.now())),
    );
    return () => globalThis.clearTimeout(timer);
  }, [deadline, expired]);
  return expired;
}

/** A refreshed response cannot silently change what the human just approved. */
function sameHandoffSummary(
  a: WorkspaceHandoffTransaction,
  b: WorkspaceHandoffTransaction,
): boolean {
  if (
    a.state !== b.state ||
    a.requesting_origin !== b.requesting_origin ||
    a.expires_at !== b.expires_at ||
    a.purpose !== b.purpose
  )
    return false;
  if (a.purpose === 'establishment') return true;
  return (
    b.purpose === 'step-up' &&
    a.operation === b.operation &&
    a.environment === b.environment &&
    a.key_ids.length === b.key_ids.length &&
    a.key_ids.every((key, index) => key === b.key_ids[index])
  );
}

/**
 * The SERVING instance's authorization page (registry surface
 * `workspace-approve`).
 *
 * This is the page the popup lands on, served by the instance being operated,
 * on that instance's own origin. Everything about the human's authentication
 * happens here and only here: this instance's password, its TOTP, its passkeys,
 * its OIDC — never the viewing instance's, which has no way to authenticate to
 * this one and no code path that could.
 *
 * Two shapes land here, distinguished by the `purpose` the transaction was
 * opened under:
 *
 *  - **establishment** — a first workspace. The human is signed in (or signs in
 *    here, in place) and approves; the redemption mints a workspace session.
 *  - **step-up** — an ELEVATION of a workspace already open. A disclosure over
 *    there needs a fresh reauthentication over here first, so the human runs
 *    THIS instance's own #58 ceremony over the bound environment — which opens
 *    the reauth window the approval's server-side freshness gate then requires —
 *    and only then approves. The page reads purpose, environment and key set
 *    from the server-owned transaction by opaque state; the server validates
 *    the fresh window against that same bound environment.
 *
 * Three details are load-bearing:
 *
 *  1. **It renders the sign-in form itself when there is no session.** A first
 *     establishment arrives with no cookies for this instance, and redirecting
 *     to `/login` would drop the `state` this transaction is addressed by.
 *  2. **Approval is an ordinary same-origin, cookie-authenticated POST** with
 *     the synchronizer token — the shared client's rules, unchanged. Nothing
 *     the viewing origin sent is trusted here beyond the opaque state value,
 *     which the server resolves against its own transaction row.
 *  3. **The redirect target comes from the SERVER**, never from the URL. The
 *     callback authority is the allowlist entry the transaction was opened
 *     against; a redirect URI supplied by whoever opened this page would be an
 *     open redirector with a fresh authorization code attached.
 */
export function WorkspaceApprove() {
  const auth = useAuth();
  const [query] = useState(() => new URLSearchParams(globalThis.location.search));
  const state = query.get('state') ?? '';

  // Purpose and any step-up scope come from the SERVER-BOUND transaction, read
  // by state — never from the URL, which carries only opaque state. That keeps a
  // large reveal-all off the URL-length ceiling and makes one source choose the
  // ceremony. Gated on authentication because the transaction read is audited
  // as the human who is about to approve it.
  const transaction = useQuery({
    queryKey: ['workspace-handoff', state],
    queryFn: () => parsed(showWorkspaceHandoffOp, { path: { state } }),
    enabled: state !== '' && auth.state.status === 'authenticated',
  });

  const tasks = useCeremonyTask([
    state,
    auth.state.status,
    auth.state.status === 'authenticated' ? auth.state.sessionEpoch : '',
  ]);
  const expired = useHandoffExpiry(transaction.data?.expires_at);
  const wrongState = transaction.data !== undefined && transaction.data.state !== state;

  const approve = useMutation({
    mutationFn: async ({ task, revision }: { task: CeremonyTask; revision: number }) => {
      const requireCurrent = () => {
        if (!tasks.isCurrent(task) || auth.captureTransition().revision !== revision) {
          throw new Error('The authorization surface is no longer active.');
        }
      };
      requireCurrent();
      const displayed = transaction.data;
      if (
        displayed === undefined ||
        wrongState ||
        expired ||
        Date.parse(displayed.expires_at) <= Date.now()
      ) {
        throw new Error('The authorization request is no longer valid.');
      }
      // Re-read immediately before approval, including after an asynchronous
      // reauthentication. Consumed/expired/changed summaries require starting
      // again. The server's atomic approval still decides a concurrent race.
      const current = await parsed(showWorkspaceHandoffOp, {
        path: { state },
        signal: task.signal,
      });
      requireCurrent();
      if (!sameHandoffSummary(displayed, current) || Date.parse(current.expires_at) <= Date.now()) {
        throw new Error('The authorization request changed or expired.');
      }
      const result = await parsed(approveWorkspaceHandoffOp, {
        body: { state },
        signal: task.signal,
      });
      requireCurrent();
      // The code goes to the pre-registered callback and nowhere else. Building
      // the URL from the SERVER's `redirect_uri` is what makes that true.
      const target = new URL(result.redirect_uri);
      target.searchParams.set('code', result.code);
      target.searchParams.set('state', state);
      globalThis.location.assign(target.toString());
    },
  });

  const authorize = () =>
    approve.mutate({ task: tasks.begin(['approve']), revision: auth.captureTransition().revision });

  if (state === '') {
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Nothing to authorize</h1>
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              This page was opened without a handoff transaction. Start the workspace from the
              instance you were browsing.
            </span>
          </p>
        </div>
      </main>
    );
  }

  if (auth.state.status === 'checking' || auth.state.status === 'transitioning') {
    return (
      <p className="login" role="status">
        Loading…
      </p>
    );
  }

  // No session on THIS instance: authenticate here, on this origin, with this
  // instance's own ceremonies. The URL — and with it the state — survives.
  if (auth.state.status === 'anonymous') {
    return <Login />;
  }

  if (
    transaction.isError ||
    approve.isError ||
    wrongState ||
    (transaction.data !== undefined && expired)
  ) {
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Authorization unavailable</h1>
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              This authorization request could not be read. It may have expired or been used
              already. Close this window and start again from the instance you were browsing.
            </span>
          </p>
        </div>
      </main>
    );
  }

  if (transaction.data === undefined) {
    return (
      <p className="login" role="status">
        Loading…
      </p>
    );
  }

  const name = auth.identity?.principal.display_name ?? auth.identity?.principal.id ?? '';
  const stepUpTransaction = isWorkspaceHandoffStepUp(transaction.data)
    ? transaction.data
    : undefined;
  const isStepUp = stepUpTransaction !== undefined;

  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">
          {isStepUp ? 'Authorize this disclosure' : 'Authorize this workspace'}
        </h1>
        <p className="login__lede workspace-consent__origin">
          Requesting origin:{' '}
          <strong className="mono">
            <bdi dir="ltr">{transaction.data.requesting_origin}</bdi>
          </strong>
        </p>
        {stepUpTransaction !== undefined ? (
          <section className="workspace-consent__scope" aria-label="Requested scope" tabIndex={0}>
            <dl>
              <dt>Environment</dt>
              <dd className="mono">{stepUpTransaction.environment}</dd>
              <dt>Keys</dt>
              <dd>
                {stepUpTransaction.key_ids.length === 0 ? (
                  'Entire environment'
                ) : (
                  <ul>
                    {stepUpTransaction.key_ids.map((key) => (
                      <li className="mono" key={key}>
                        {key}
                      </li>
                    ))}
                  </ul>
                )}
              </dd>
            </dl>
          </section>
        ) : null}
        <p className="login__lede">
          {isStepUp ? (
            <>
              Signed in as <span className="mono">{name}</span>. A workspace you have open elsewhere
              is asking to <strong>{stepUpTransaction.operation}</strong> over{' '}
              {stepUpTransaction.key_ids.length === 0
                ? 'this environment'
                : `${stepUpTransaction.key_ids.length} key${stepUpTransaction.key_ids.length === 1 ? '' : 's'}`}
              . Reauthenticate here to allow it. This is a disclosure, not a new sign-in, and it
              covers only this one act.
            </>
          ) : (
            <>
              Signed in as <span className="mono">{name}</span>. Approving lets the requesting
              origin shown above operate this instance <strong>as you</strong>, for as long as the
              session lives or until it is revoked. Everything it does will appear in this
              instance&apos;s audit trail under your name.
            </>
          )}
        </p>

        {stepUpTransaction !== undefined ? (
          <StepUpReauth
            operation={stepUpTransaction.operation}
            environmentId={stepUpTransaction.environment}
            keyIds={stepUpTransaction.key_ids}
            key={auth.state.sessionEpoch}
            onReauthed={authorize}
            approving={approve.isPending}
          />
        ) : (
          <>
            <button
              className="btn btn--primary"
              type="button"
              onClick={authorize}
              disabled={approve.isPending}
            >
              {approve.isPending ? 'Authorizing…' : 'Authorize'}
            </button>
            <button className="btn" type="button" onClick={() => globalThis.close()}>
              Cancel
            </button>
          </>
        )}
      </div>
    </main>
  );
}

/**
 * StepUpReauth runs THIS instance's own #58 reauthentication over the bound
 * environment, then hands off to the approval.
 *
 * It offers a passkey and a code both, and does not try to know in advance which
 * the environment allows: a protected environment refuses the code with a 409
 * the ceremony copy already explains, and asking the server is one more request
 * that can fail before the human has done anything. On success the reauth window
 * is open on this session and the approval's freshness gate will accept it.
 */
function StepUpReauth({
  operation,
  environmentId,
  keyIds,
  onReauthed,
  approving,
}: {
  operation: WorkspaceHandoffStepUp['operation'];
  environmentId: string;
  keyIds: readonly string[];
  onReauthed: () => void;
  approving: boolean;
}) {
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  const auth = useAuth();
  const tasks = useCeremonyTask([
    operation,
    environmentId,
    keyIds,
    auth.state.status,
    auth.state.status === 'authenticated' ? auth.state.sessionEpoch : '',
  ]);

  const attempt = async (run: () => Promise<void>) => {
    const task = tasks.begin(['reauthenticate']);
    const revision = auth.captureTransition().revision;
    const current = () => tasks.isCurrent(task) && auth.captureTransition().revision === revision;
    setBusy(true);
    setFailure(null);
    try {
      await run();
      if (!current()) return;
      // Reauth is done; hand off to the approval and release our own busy flag
      // so `working` now reflects only the approve mutation. If that mutation
      // fails (an expired or already-consumed transaction), the buttons must
      // come back — leaving busy latched here would strand the human with Cancel
      // disabled and no way out but closing the window.
      setBusy(false);
      onReauthed();
    } catch (err) {
      if (!current()) return;
      setFailure(ceremonyRefusalText(err));
      setBusy(false);
    } finally {
      if (current()) {
        setCode('');
        tasks.finish(task);
      }
    }
  };

  const onPasskey = () =>
    void attempt(() => runPasskeyCeremony({ operation, environmentId, keyIds: [...keyIds] }));

  const onCode = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const proof = code;
    setCode('');
    void attempt(() => runTOTPCeremony(environmentId, proof));
  };

  const working = busy || approving;

  return (
    <>
      {failure === null ? null : (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      )}
      <div className="ceremony__actions">
        <button className="btn btn--primary" type="button" onClick={onPasskey} disabled={working}>
          {working ? 'Working…' : 'Use a passkey'}
        </button>
        <button className="btn" type="button" onClick={() => globalThis.close()} disabled={working}>
          Cancel
        </button>
      </div>
      <form className="ceremony__totp" onSubmit={onCode}>
        <div className="field">
          <label htmlFor="approve-code">Or a code from your authenticator</label>
          <input
            id="approve-code"
            name="code"
            inputMode="numeric"
            autoComplete="one-time-code"
            value={code}
            disabled={working}
            onChange={(e) => setCode(e.target.value)}
          />
        </div>
        <button className="btn" type="submit" disabled={working || code.length < 6}>
          Authorise with a code
        </button>
      </form>
    </>
  );
}
