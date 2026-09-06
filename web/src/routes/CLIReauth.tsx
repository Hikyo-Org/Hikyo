import { reauthenticateSelfConfig } from '../api/selfConfig.ts';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useSensitiveMutation, useSensitiveState } from '../api/sensitiveMutation.ts';

import {
  approveCLIReauth,
  cliReauthCallbackURL,
  loadCLIReauthTransaction,
} from '../api/cliReauth.ts';
import { useAuthMethods, useSessionOIDCProvider, useTotpStatus } from '../api/account.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import {
  runAdapterPasskeyCeremony,
  runAdapterTOTPCeremony,
  runPasskeyCeremony,
  runOIDCCeremony,
  runTOTPCeremony,
} from '../api/values.ts';
import { Login } from './Login.tsx';
import { ProviderDiscoveryAlert } from './ProviderDiscoveryAlert.tsx';

/** Browser half of the CLI's state + PKCE reauthentication handoff. */
export function CLIReauth() {
  const auth = useAuth();
  const [state] = useState(
    () => new URLSearchParams(globalThis.location.search).get('transaction') ?? '',
  );
  const [totp, setTOTP] = useSensitiveState('');
  const totpStatus = useTotpStatus();
  const methods = useAuthMethods();
  const oidcProvider = useSessionOIDCProvider();
  const transaction = useQuery({
    queryKey: ['cli-reauth', state] as const,
    queryFn: () => loadCLIReauthTransaction(state),
    enabled: state !== '' && auth.state.status === 'authenticated',
  });
  const approve = useSensitiveMutation({
    mutationFn: async (strategy: 'factor' | 'oidc') => {
      const code = totp;
      setTOTP('');
      const handoff = transaction.data;
      if (handoff === undefined) {
        throw new Error('the CLI authorization transaction is unavailable');
      }
      const environmentIds = handoff.environments.map((environment) => environment.environment_id);
      if (handoff.purpose === 'self-config') {
        if (handoff.self_config === undefined) throw new Error('The configuration decision is unavailable.');
        await reauthenticateSelfConfig(handoff.self_config, code.trim() === '' ? { kind: 'passkey' } : { kind: 'totp', code: code.trim() });
      } else if (handoff.purpose === 'adapter') {
        if (handoff.environments.some((environment) => !environment.requires_webauthn)) {
          await runAdapterTOTPCeremony(adapterOperation(handoff.operation), environmentIds, code);
        }
        for (const environment of handoff.environments.filter(
          (candidate) => candidate.requires_webauthn,
        )) {
          await runAdapterPasskeyCeremony({
            operation: adapterOperation(handoff.operation),
            environmentId: environment.environment_id,
            environmentIds,
          });
        }
      } else {
        // A disclosure handoff: the SAME purpose-bound, enumerated-key-set
        // ceremony the Values page runs, one decision per environment over
        // exactly the keys the terminal named. A 0-window environment takes
        // the passkey; a sliding environment accepts the authenticator code.
        // A sliding environment takes the passkey too, or an authenticator
        // code where one was typed - the code opens the environment-wide
        // window TOTP always opens (human-auth ADR: TOTP is a per-step gate,
        // not a per-operation one), never a per-key decision.
        for (const environment of handoff.environments) {
          if (strategy === 'oidc' && !environment.requires_webauthn && oidcProvider !== null) {
            await runOIDCCeremony(oidcProvider.slug, environment.environment_id);
          } else if (!environment.requires_webauthn && code.trim() !== '') {
            await runTOTPCeremony(environment.environment_id, code.trim());
          } else {
            await runPasskeyCeremony({
              operation: handoff.purpose,
              environmentId: environment.environment_id,
              keyIds: handoff.key_ids,
            });
          }
        }
        if (strategy === 'oidc') await auth.refreshSession();
      }
      const approved = await approveCLIReauth(handoff.state);
      return cliReauthCallbackURL(handoff, approved);
    },
    onSuccess: (callback) => { globalThis.location.assign(callback); },
  });

  if (state === '') {
    return <CLIReauthMessage title="Nothing to authorize" text="This page has no CLI transaction. Return to the terminal and start again." />;
  }
  if (auth.state.status === 'checking' || auth.state.status === 'transitioning') {
    // The same card shell as the loaded state, so the page does not jump.
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Authorize CLI</h1>
          <p className="login__lede" role="status">
            Loading…
          </p>
        </div>
      </main>
    );
  }
  if (auth.state.status === 'anonymous') {
    return <Login />;
  }

  const selfConfig = transaction.data?.purpose === 'self-config';
  const disclosure = transaction.data !== undefined && transaction.data.purpose !== 'adapter' && !selfConfig;
  const slidingEnvironments =
    transaction.data?.environments.filter((environment) => !environment.requires_webauthn) ?? [];
  // An adapter handoff needs a code for every sliding environment (its
  // ceremony is TOTP-or-passkey per policy, as before). A disclosure handoff
  // offers the code only where it can do anything - a sliding environment and
  // an enrolled authenticator - and the passkey otherwise.
  const hasTotp = totpStatus.isSuccess && totpStatus.data.confirmed;
  const requiresTOTP = !disclosure && !selfConfig && slidingEnvironments.length > 0;
  const offersTOTP = (selfConfig || (disclosure && slidingEnvironments.length > 0)) && hasTotp;
  const offersOIDC =
    disclosure && slidingEnvironments.length > 0 && oidcProvider !== null;
  const methodsFailed =
    disclosure &&
    slidingEnvironments.length > 0 &&
    auth.identity?.session.assurance.method.startsWith('oidc:') === true &&
    methods.isError;

  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">Authorize CLI</h1>
        {transaction.isPending ? <p role="status">Loading authorization policy…</p> : null}
        {transaction.isError ? (
          <p className="alert" role="alert"><span className="alert__glyph" aria-hidden="true">!</span><span>This CLI transaction is invalid, expired, or already used. Return to the terminal and start again.</span></p>
        ) : null}
        {transaction.data !== undefined ? (
          <>
            <p className="login__lede">
              {transaction.data.purpose === 'self-config' && transaction.data.self_config !== undefined ? <>Authorize <strong>{transaction.data.self_config.action}</strong> on <code>{transaction.data.self_config.owner_instance_id}</code>, revision r{String(transaction.data.self_config.revision)}, generation {String(transaction.data.self_config.expected_generation)}, schema {transaction.data.self_config.schema_version}. {transaction.data.self_config.to === '' ? '' : `Send to ${transaction.data.self_config.to}.`} {transaction.data.self_config.confirm_restored_credentials ? 'This also confirms reviewed restored credentials and reconciled access grants.' : ''} This decision can be used once.</> : transaction.data.purpose === 'adapter' ? (
                <>
                  Approve <span className="mono">{transaction.data.operation}</span> for the
                  environments below.
                </>
              ) : (
                <>
                  The terminal asks to <strong>{transaction.data.purpose}</strong>{' '}
                  {transaction.data.key_ids.length} key
                  {transaction.data.key_ids.length === 1 ? '' : 's'} in the environments below.
                  {offersTOTP
                    ? ' A passkey authorises one decision over exactly those keys. A code from your authenticator instead opens the environment-wide window the policy allows, for its duration.'
                    : ' A passkey authorises one decision over exactly those keys.'}
                  {offersOIDC
                    ? ' Re-authenticate once per sliding-window environment with your identity provider.'
                    : ''}
                </>
              )}
            </p>
            {transaction.data.self_config?.plan_digest === undefined ? null : <p>{transaction.data.self_config.action === 'rollout-restore' ? 'Restore deployment resources. The desired configuration stays fenced until a separate repair Apply.' : 'Controlled rollout.'} Prepared plan <code className="self-config-plan">{transaction.data.self_config.plan_digest}</code>. Authorization applies only to this exact plan.</p>}
            {methodsFailed ? (
              <ProviderDiscoveryAlert onRetry={() => void methods.refetch()} />
            ) : null}
            {transaction.data.purpose !== 'adapter' ? (
              <ul className="mono">
                {transaction.data.key_ids.map((keyId) => (
                  <li key={keyId}>{keyId}</li>
                ))}
              </ul>
            ) : null}
            <ul>
              {transaction.data.environments.map((environment) => (
                <li key={environment.environment_id}>
                  <span className="mono">{environment.environment_id}</span>{' '}
                  ({environment.requires_webauthn ? 'passkey required' : 'TOTP required'})
                </li>
              ))}
            </ul>
            {requiresTOTP || offersTOTP ? (
              <div className="field">
                <label htmlFor="cli-reauth-totp">
                  {requiresTOTP ? 'Authenticator code' : 'Authenticator code (optional; leave empty to use a passkey)'}
                </label>
                <input id="cli-reauth-totp" inputMode="numeric" autoComplete="one-time-code" value={totp} onChange={(event) => setTOTP(event.target.value)} required />
              </div>
            ) : null}
            {approve.isError ? <p className="alert" role="alert"><span className="alert__glyph" aria-hidden="true">!</span><span>Authorization failed. No CLI credential was disclosed; return to the terminal and try again.</span></p> : null}
            <button className="btn btn--primary" type="button" disabled={approve.isPending || (requiresTOTP && totp.trim() === '')} onClick={() => approve.mutate('factor')}>
              {approve.isPending ? 'Authorizing…' : 'Authorize CLI'}
            </button>
            {offersOIDC ? (
              <button className="btn" type="button" disabled={approve.isPending} onClick={() => approve.mutate('oidc')}>
                {approve.isPending ? 'Authorizing…' : `Re-authenticate with ${oidcProvider.display_name}`}
              </button>
            ) : null}
            <button className="btn" type="button" onClick={() => globalThis.close()}>Cancel</button>
          </>
        ) : null}
      </div>
    </main>
  );
}

function CLIReauthMessage(input: { title: string; text: string }) {
  return (
    <main className="login"><div className="login__card"><h1 className="login__title">{input.title}</h1><p className="alert" role="alert"><span className="alert__glyph" aria-hidden="true">!</span><span>{input.text}</span></p></div></main>
  );
}

type AdapterOperation = 'adapter.configure' | 'adapter.credential-set' | 'adapter.adopt' | 'adapter.sync';

/** adapterOperation narrows the transaction's operation to the adapter set without a cast. */
function adapterOperation(operation: string): AdapterOperation {
  switch (operation) {
    case 'adapter.configure':
    case 'adapter.credential-set':
    case 'adapter.adopt':
    case 'adapter.sync':
      return operation;
    default:
      throw new Error(`an adapter handoff named a non-adapter operation: ${operation}`);
  }
}
