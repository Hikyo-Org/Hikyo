import { approveCliLoginOp, showCliLoginTransactionOp } from '@hikyo/operations';
import { useQuery } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useSearchParams } from 'react-router';

import { ApiError, parsed } from '../api/client.ts';
import { useLogout } from '../api/session.ts';
import { useSensitiveMutation } from '../api/sensitiveMutation.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';
import { Login } from './Login.tsx';
import { useCeremonyTask, type CeremonyTask } from './useCeremonyTask.ts';

const USER_CODE = /^[0-9A-HJKMNP-TV-Z]{8}$/;
const STATE = /^[A-Za-z0-9_-]{43}$/;

/**
 * Browser consent for a separate session after any ordinary sign-in method. The
 * client may be the CLI or the desktop app, and the server cannot say which, so the
 * wording names neither.
 */
export function CLILogin() {
  const auth = useAuth();
  const [query] = useSearchParams();
  const epoch = auth.state.status === 'authenticated' ? auth.state.sessionEpoch : auth.state.status;
  // A new request or browser session retires all one-shot mutation state,
  // including a pending result or refusal from the previous consent page.
  return <CLILoginRequest key={JSON.stringify([query.toString(), epoch])} />;
}

function CLILoginRequest() {
  const auth = useAuth();
  const logout = useLogout();
  const [query, setQuery] = useSearchParams();
  const state = query.get('state');
  const userCode = query.get('user_code');
  const reference = state ?? userCode ?? '';
  const deviceEntry = query.get('device') === '1' && state === null && userCode === null;
  const valid =
    state !== null
      ? STATE.test(state) && userCode === null
      : userCode !== null && USER_CODE.test(userCode);
  const providerHint = query.get('provider') ?? undefined;
  const [code, setCode] = useState('');
  const sessionEpoch = auth.state.status === 'authenticated' ? auth.state.sessionEpoch : '';
  const [outcome, setOutcome] = useState<{
    reference: string;
    sessionEpoch: string;
    status: 'approved' | 'cancelled';
  } | null>(null);
  const approved =
    outcome?.reference === reference &&
    outcome.sessionEpoch === sessionEpoch &&
    outcome.status === 'approved';
  const cancelled =
    outcome?.reference === reference &&
    outcome.sessionEpoch === sessionEpoch &&
    outcome.status === 'cancelled';
  const transaction = useQuery({
    queryKey: ['cli-login', reference],
    queryFn: ({ signal }) => parsed(showCliLoginTransactionOp, { path: { reference }, signal }),
    enabled: valid && auth.state.status === 'authenticated' && !approved && !cancelled,
  });
  const validExpiry = Number.isFinite(Date.parse(transaction.data?.expires_at ?? ''));
  const tasks = useCeremonyTask([
    reference,
    auth.state.status,
    auth.state.status === 'authenticated' ? auth.state.sessionEpoch : '',
  ]);
  const approve = useSensitiveMutation({
    mutationFn: async ({ task, revision }: { task: CeremonyTask; revision: number }) => {
      const requireCurrent = () => {
        if (!tasks.isCurrent(task) || auth.captureTransition().revision !== revision)
          throw new Error('The sign-in request is no longer active.');
      };
      requireCurrent();
      const displayed = transaction.data;
      if (displayed === undefined || !validExpiry || displayed.reference !== reference)
        throw new Error('The sign-in request expired.');
      // The server checks expiry on this refresh and again in the atomic
      // approval. Browser wall-clock skew must not reject a live request.
      const current = await parsed(showCliLoginTransactionOp, {
        path: { reference },
        signal: task.signal,
      });
      requireCurrent();
      if (
        current.reference !== displayed.reference ||
        current.transport !== displayed.transport ||
        current.redirect_uri !== displayed.redirect_uri ||
        current.instance !== displayed.instance ||
        current.expires_at !== displayed.expires_at
      ) {
        throw new Error('The sign-in request changed or expired.');
      }
      const result = await parsed(approveCliLoginOp, { body: { reference }, signal: task.signal });
      requireCurrent();
      if (current.transport === 'device') {
        if (result.code !== '' || result.redirect_uri !== '' || result.state !== reference)
          throw new Error('Invalid device approval response.');
        setOutcome({ reference, sessionEpoch, status: 'approved' });
      } else {
        if (
          result.state !== reference ||
          result.redirect_uri !== current.redirect_uri ||
          result.code === ''
        )
          throw new Error('Invalid CLI callback.');
        const target = new URL(current.redirect_uri);
        if (
          target.protocol !== 'http:' ||
          !['127.0.0.1', '[::1]'].includes(target.hostname) ||
          target.username !== '' ||
          target.password !== ''
        )
          throw new Error('Invalid CLI loopback callback.');
        target.searchParams.set('code', result.code);
        target.searchParams.set('state', result.state);
        globalThis.location.assign(target.toString());
      }
    },
  });

  if (deviceEntry) {
    const submit = (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      const normalized = code.toUpperCase().replace(/[\s-]/g, '');
      if (!USER_CODE.test(normalized)) return;
      const next = new URLSearchParams({ user_code: normalized });
      if (providerHint !== undefined) next.set('provider', providerHint);
      setQuery(next);
    };
    return (
      <main className="login">
        <form className="login__card" onSubmit={submit}>
          <h1 className="login__title">Approve a sign-in</h1>
          <p className="login__lede">
            Enter the eight-character code shown on the device or app you are signing in. Only
            continue for a sign-in you started.
          </p>
          <Input
            label="Device code"
            name="user_code"
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            required
            value={code}
            onChange={(event) => setCode(event.target.value)}
          />
          <Button
            variant="primary"
            type="submit"
            disabled={!USER_CODE.test(code.toUpperCase().replace(/[\s-]/g, ''))}
          >
            Continue
          </Button>
        </form>
      </main>
    );
  }
  if (!valid)
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Nothing to authorize</h1>
          <Alert>Start a sign-in from the Hikyo app or CLI, then open the link it provides.</Alert>
        </div>
      </main>
    );
  if (auth.state.status === 'checking' || auth.state.status === 'transitioning')
    return (
      <p className="login" role="status">
        Loading…
      </p>
    );
  if (auth.state.status === 'anonymous')
    return (
      <Login signInOnly returnTo={`/login/cli?${query.toString()}`} providerHint={providerHint} />
    );
  if (cancelled)
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Sign-in cancelled</h1>
          <p className="login__lede">
            No approval was sent. Cancel the sign-in on the app or terminal that started it. Start
            a new sign-in if you want to try again.
          </p>
        </div>
      </main>
    );
  if (approved)
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Sign-in approved</h1>
          <p className="login__lede">
            Return to the app or terminal that started it to continue. You remain signed in to this browser.
          </p>
        </div>
      </main>
    );
  // A verified browser session may lack the enrolled local factor. This safe
  // refusal is distinct from an invalid/expired handoff (403). Keep its query
  // while the existing logout and sign-in ceremonies replace that session.
  if (
    approve.error instanceof ApiError &&
    approve.error.status === 400 &&
    approve.error.operation === approveCliLoginOp &&
    approve.error.code === 'cli_login_factor_required'
  )
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Complete your sign-in</h1>
          {/* The server's detail names `hikyo login` commands, which mean nothing to someone approving from the app. */}
          <Alert>This sign-in needs an enrolled second factor. Sign out, sign in again here with a passkey or authenticator code, then approve the request again.</Alert>
          {logout.isError ? <Alert>Sign-out failed. Try again to continue signing in.</Alert> : null}
          <Button type="button" disabled={logout.isPending} onClick={() => logout.mutate()}>
            {logout.isPending ? 'Signing out…' : 'Sign out and sign in again'}
          </Button>
        </div>
      </main>
    );
  if (
    transaction.isError ||
    approve.isError ||
    (transaction.data !== undefined && (!validExpiry || transaction.data.reference !== reference))
  )
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Sign-in could not be completed</h1>
          <Alert>
            This request may have expired, already been used, or belong to a different account than the one signed in here. Start again from the app or CLI.
          </Alert>
        </div>
      </main>
    );
  if (transaction.data === undefined)
    return (
      <p className="login" role="status">
        Loading…
      </p>
    );
  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">Authorize sign-in</h1>
        <p className="login__lede">
          Signed in as{' '}
          <strong><bdi>{auth.identity?.principal.display_name ?? auth.identity?.principal.id}</bdi></strong>.
          Approve only a sign-in you started in the Hikyo app or terminal.
        </p>
        <dl>
          <dt>Client</dt>
          <dd>Hikyo app or CLI</dd>
          <dt>Instance</dt>
          <dd className="mono workspace-consent__origin">
            <bdi dir="ltr">{transaction.data.instance}</bdi>
          </dd>
          <dt>Purpose</dt>
          <dd>Login</dd>
          <dt>Session type</dt>
          <dd>Separate session for that client</dd>
          {transaction.data.transport === 'device' ? (
            <>
              <dt>Device code</dt>
              <dd className="mono">{reference}</dd>
            </>
          ) : null}
        </dl>
        {transaction.data.transport === 'device' ? (
          <p className="login__lede" role="note">
            You are approving a <strong>different device</strong>, not this browser. Approve only if
            the code above is the one that device is showing.
          </p>
        ) : null}
        <p className="login__lede">
          That client will act as you until its session expires or is revoked. Your browser stays
          signed in.
        </p>
        <Button
          variant="primary"
          type="button"
          disabled={approve.isPending}
          onClick={() =>
            approve.mutate({
              task: tasks.begin(['approve']),
              revision: auth.captureTransition().revision,
            })
          }
        >
          {approve.isPending ? 'Authorizing…' : 'Authorize sign-in'}
        </Button>
        <Button
          type="button"
          disabled={approve.isPending}
          onClick={() => setOutcome({ reference, sessionEpoch, status: 'cancelled' })}
        >
          Cancel
        </Button>
      </div>
    </main>
  );
}
