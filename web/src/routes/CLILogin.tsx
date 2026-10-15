import { approveCliLoginOp, showCliLoginTransactionOp } from '@hikyo/operations';
import { useQuery } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useSearchParams } from 'react-router';

import { parsed } from '../api/client.ts';
import { useSensitiveMutation } from '../api/sensitiveMutation.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';
import { Login } from './Login.tsx';
import { useCeremonyTask, type CeremonyTask } from './useCeremonyTask.ts';

const USER_CODE = /^[0-9A-HJKMNP-TV-Z]{8}$/;
const STATE = /^[A-Za-z0-9_-]{43}$/;

/** Browser consent for a separate CLI session after any ordinary sign-in method. */
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
          <h1 className="login__title">Sign in to the CLI</h1>
          <p className="login__lede">
            Enter the eight-character code shown in your terminal. Only continue for a CLI sign-in
            you started.
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
          <Alert>Start a sign-in from the CLI, then open the link it provides.</Alert>
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
          <h1 className="login__title">CLI sign-in cancelled</h1>
          <p className="login__lede">
            No approval was sent. Cancel the command in your terminal. Start a new CLI sign-in if
            you want to try again.
          </p>
        </div>
      </main>
    );
  if (approved)
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">CLI sign-in approved</h1>
          <p className="login__lede">
            Return to your terminal to continue. You remain signed in to this browser.
          </p>
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
          <h1 className="login__title">CLI sign-in could not be completed</h1>
          <Alert>
            This request may have expired or already been used. Start again from the CLI.
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
        <h1 className="login__title">Authorize CLI sign-in</h1>
        <p className="login__lede">
          Signed in as{' '}
          <strong>{auth.identity?.principal.display_name ?? auth.identity?.principal.id}</strong>.
          Approve only a sign-in you started in your terminal.
        </p>
        <dl>
          <dt>Client</dt>
          <dd>Hikyo CLI</dd>
          <dt>Instance</dt>
          <dd className="mono workspace-consent__origin">
            <bdi dir="ltr">{transaction.data.instance}</bdi>
          </dd>
          <dt>Purpose</dt>
          <dd>Login</dd>
          <dt>Session type</dt>
          <dd>Separate CLI session</dd>
          {transaction.data.transport === 'device' ? (
            <>
              <dt>Device code</dt>
              <dd className="mono">{reference}</dd>
            </>
          ) : null}
        </dl>
        <p className="login__lede">
          The CLI will act as you until its session expires or is revoked. Your browser stays signed
          in.
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
          {approve.isPending ? 'Authorizing…' : 'Authorize CLI sign-in'}
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
