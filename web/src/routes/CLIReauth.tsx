import { reauthenticateSelfConfig } from '../api/selfConfig.ts';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useSensitiveMutation, useSensitiveState } from '../api/sensitiveMutation.ts';

import {
  approveCLIReauth,
  cliReauthCallbackURL,
  type CliReauthTransaction,
  loadCLIReauthTransaction,
} from '../api/cliReauth.ts';
import { useAuthMethods, useSessionOIDCProvider, useTotpStatus } from '../api/account.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import {
  runDeveloperCredentialPasskeyCeremony,
  runDeveloperCredentialTOTPCeremony,
  runAdapterPasskeyCeremony,
  runAdapterTOTPCeremony,
  runPasskeyCeremony,
  runOIDCCeremony,
  runTOTPCeremony,
} from '../api/values.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { NamedId } from '../ui/NamedId.tsx';
import { ConsentKeys, useConsentNames } from './ConsentScope.tsx';
import { Login } from './Login.tsx';
import { ProviderDiscoveryAlert } from './ProviderDiscoveryAlert.tsx';

/**
 * Browser half of the state + PKCE reauthentication handoff, for the CLI and the
 * desktop app alike. Its wording names neither: the server cannot say which client
 * asked, so the page names the signed-in person and what is being decided.
 */
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
  // Names are read through the authorized endpoints, never taken from the CLI.
  // Only one project is resolved: a handoff spanning several keeps IDs, which
  // are the bound values anyway.
  const environmentPolicies = transaction.data?.environments ?? [];
  const firstEnvironment = environmentPolicies[0];
  const owner =
    firstEnvironment !== undefined &&
    environmentPolicies.every(
      (e) => e.org_id === firstEnvironment.org_id && e.project_id === firstEnvironment.project_id,
    )
      ? firstEnvironment
      : undefined;
  const names = useConsentNames(
    owner?.org_id ?? '',
    owner?.project_id ?? '',
    transaction.data?.purpose !== 'adapter' && transaction.data?.purpose !== 'self-config',
  );
  const approve = useSensitiveMutation({
    mutationFn: async (strategy: 'factor' | 'oidc') => {
      const code = totp;
      setTOTP('');
      const handoff = transaction.data;
      if (handoff === undefined) {
        throw new Error('the CLI authorization transaction is unavailable');
      }
      const environmentIds = handoff.environments.map((environment) => environment.environment_id);
      if (handoff.purpose === 'developer-credential') {
        const target = handoff.developer_credential;
        const environment = handoff.environments[0];
        if (target === undefined || target.consent_current_and_future !== true || environment === undefined || handoff.environments.length !== 1 || strategy === 'oidc') {
          throw new Error('The bounded developer delegation decision is unavailable.');
        }
        const input = { environmentId: environment.environment_id, keyIds: handoff.key_ids, lifetimeSeconds: target.lifetime_seconds, consentCurrentAndFuture: target.consent_current_and_future, personalConsent: target.personal_consent === true };
        if (code.trim() !== '' && !environment.requires_webauthn) {
          await runDeveloperCredentialTOTPCeremony(input, code.trim());
        } else {
          await runDeveloperCredentialPasskeyCeremony(input);
        }
      } else if (handoff.purpose === 'self-config') {
        if (handoff.self_config === undefined) throw new Error('The configuration decision is unavailable.');
        await reauthenticateSelfConfig(handoff.self_config, code.trim() === '' ? { kind: 'passkey' } : { kind: 'totp', code: code.trim() });
      } else if (handoff.purpose === 'adapter') {
        const passkeyEnvironments = code.trim() === ''
          ? handoff.environments
          : handoff.environments.filter((candidate) => candidate.requires_webauthn);
        if (code.trim() !== '') {
          await runAdapterTOTPCeremony(adapterOperation(handoff.operation), environmentIds, code.trim());
        }
        for (const environment of passkeyEnvironments) {
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
    return <CLIReauthMessage title="Nothing to authorize" text="This page has no pending request. Go back to the app or terminal that started it and start again." />;
  }
  if (auth.state.status === 'checking' || auth.state.status === 'transitioning') {
    // The same card shell as the loaded state, so the page does not jump.
    return (
      <main className="login">
        <div className="login__card">
          <h1 className="login__title">Authorize this request</h1>
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

  const developerCredential = transaction.data?.purpose === 'developer-credential';
  const selfConfig = transaction.data?.purpose === 'self-config';
  const adapter = transaction.data?.purpose === 'adapter';
  const disclosure = transaction.data !== undefined && transaction.data.purpose !== 'adapter' && !selfConfig && !developerCredential;
  const slidingEnvironments =
    transaction.data?.environments.filter((environment) => !environment.requires_webauthn) ?? [];
  // An adapter handoff needs a code for every sliding environment (its
  // ceremony is TOTP-or-passkey per policy, as before). A disclosure handoff
  // offers the code only where it can do anything - a sliding environment and
  // an enrolled authenticator - and the passkey otherwise.
  const hasTotp = totpStatus.isSuccess && totpStatus.data.confirmed;
  const offersTOTP =
    (selfConfig || ((adapter || disclosure || developerCredential) && slidingEnvironments.length > 0)) && hasTotp;
  const oidcEligible = disclosure && slidingEnvironments.length > 0 && totpStatus.data?.can_reauth_oidc === true;
  const offersOIDC = oidcEligible && oidcProvider !== null;
  const requiresPasskey = transaction.data?.environments.some((environment) => environment.requires_webauthn) === true;
  const hasPasskey = totpStatus.isSuccess && totpStatus.data.has_passkey;
  const canUseFactor = hasPasskey || (offersTOTP && !requiresPasskey);
  const canUseOIDC = offersOIDC && (!requiresPasskey || hasPasskey);
  const noProof = totpStatus.isSuccess && !canUseFactor && !canUseOIDC &&
    (!oidcEligible || (requiresPasskey && !hasPasskey) || methods.isSuccess);
  const methodsFailed =
    disclosure &&
    slidingEnvironments.length > 0 &&
    (auth.identity?.session.assurance.method.startsWith('oidc:') === true || auth.identity?.session.assurance.method.startsWith('oauth2:') === true) &&
    methods.isError;

  return (
    <main className="login">
      <div className="login__card">
        <h1 className="login__title">Authorize this request</h1>
        {/* A browser signed in as someone else only learns "invalid or expired" from the server, so say who this is. */}
        <p className="login__lede">
          Signed in as <strong><bdi>{auth.identity?.principal.display_name ?? auth.identity?.principal.id}</bdi></strong>.
          Approve only a request you started in the Hikyo app or terminal.
        </p>
        {transaction.isPending ? <p role="status">Loading authorization policy…</p> : null}
        {transaction.isError ? (
          <Alert>This request is invalid, expired, or already used, or it belongs to a different account than the one signed in here. Go back to the app or terminal that started it and start again.</Alert>
        ) : null}
        {transaction.data !== undefined ? (
          <>
            <p className="login__lede">
              {transaction.data.purpose === 'developer-credential' && transaction.data.developer_credential !== undefined ? <>
                Authorize a developer credential for this one environment. Its fixed lifetime is <strong>{developerCredentialLifetime(transaction.data.developer_credential.lifetime_seconds)}</strong>.
                {transaction.data.key_ids.length} {transaction.data.key_ids.length === 1 ? 'key exists' : 'keys exist'} now, listed below. Consent also covers every key published to this environment later, until expiry.{transaction.data.developer_credential.personal_consent === true ? ' It also delivers your own personal values in this environment to this credential.' : ''} This credential survives ordinary logout, and security revocation or lost access prevents the next fetch. It cannot renew or deliver offline. Your fresh proof authorizes this exact delegation once.
              </> : transaction.data.purpose === 'self-config' && transaction.data.self_config !== undefined ? <>Authorize <strong>{transaction.data.self_config.action}</strong> on <code>{transaction.data.self_config.owner_instance_id}</code>, revision r{String(transaction.data.self_config.revision)}, generation {String(transaction.data.self_config.expected_generation)}, schema {transaction.data.self_config.schema_version}. {transaction.data.self_config.to === '' ? '' : `Send to ${transaction.data.self_config.to}.`} {transaction.data.self_config.confirm_restored_credentials ? 'This also confirms reviewed restored credentials and reconciled access grants.' : ''} This decision can be used once.</> : transaction.data.purpose === 'adapter' ? (
                <>
                  Approve <span className="mono">{transaction.data.operation}</span> for the
                  environments below.
                </>
              ) : transaction.data.purpose === 'mint' ? (
                <>
                  A Hikyo client you started asks to <strong>mint a machine credential</strong> whose
                  service account can decrypt secret values in the environment below. The approval
                  is for the environment as a whole, not for individual keys.
                  {offersTOTP
                    ? ' At a protected or 0-window environment a passkey authorises this one decision; otherwise it opens the environment\'s reauthentication window for its duration. Where the window is above 0, a code from your authenticator also opens it, for its duration.'
                    : ' At a protected or 0-window environment a passkey authorises this one decision; otherwise it opens the environment\'s reauthentication window for its duration.'}
                  {offersOIDC
                    ? ' Re-authenticate once per sliding-window environment with your identity provider.'
                    : ''}
                </>
              ) : transaction.data.purpose === 'access' ? (
                <>
                  A Hikyo client you started asks to <strong>take emergency temporary access</strong>{' '}
                  in the environment below without the approval its policy normally requires. The
                  access is time-bound and its reason is audited. The approval is for the
                  environment as a whole, not for individual keys.
                  {offersTOTP
                    ? ' At a protected or 0-window environment a passkey authorises this one decision; otherwise it opens the environment\'s reauthentication window for its duration. Where the window is above 0, a code from your authenticator also opens it, for its duration.'
                    : ' At a protected or 0-window environment a passkey authorises this one decision; otherwise it opens the environment\'s reauthentication window for its duration.'}
                  {offersOIDC
                    ? ' Re-authenticate once per sliding-window environment with your identity provider.'
                    : ''}
                </>
              ) : (
                <>
                  A Hikyo client you started asks to <strong>{transaction.data.purpose}</strong>{' '}
                  {transaction.data.key_ids.length} key
                  {transaction.data.key_ids.length === 1 ? '' : 's'} in the environments below.
                  {offersTOTP
                    ? ' At a protected or 0-window environment a passkey authorises one decision over exactly those keys; otherwise it opens the environment\'s reauthentication window for its duration. Where the window is above 0, a code from your authenticator also opens it, for its duration.'
                    : ' At a protected or 0-window environment a passkey authorises one decision over exactly those keys; otherwise it opens the environment\'s reauthentication window for its duration.'}
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
            {owner === undefined ? null : (
              <p>
                Organisation <NamedId id={owner.org_id} name={names.org} tail />, project{' '}
                <NamedId id={owner.project_id} name={names.project} tail />
              </p>
            )}
            <EnvironmentList environments={transaction.data.environments} />
            {(disclosure && transaction.data.purpose !== 'mint' && transaction.data.purpose !== 'access') || developerCredential ? (
              <ConsentKeys keys={names.keys(transaction.data.key_ids)} />
            ) : null}
            {noProof ? <Alert>This needs a second factor. Sign in through a provider that asserts one, or add an authenticator or password under <a href="/settings?tab=factors">Settings › Security</a>. {requiresPasskey ? 'This policy requires an enrolled passkey.' : 'Provider sign-in alone cannot authorise this operation.'}</Alert> : null}
            {totpStatus.isError ? <Alert>Your credential inventory could not be loaded. Reload to try again.</Alert> : null}
            {offersTOTP ? (
              <div className="field">
                <label htmlFor="cli-reauth-totp">
                  {hasPasskey ? 'Authenticator code (optional; leave empty to use a passkey)' : 'Authenticator code'}
                </label>
                <input id="cli-reauth-totp" required={!hasPasskey} inputMode="numeric" autoComplete="one-time-code" value={totp} onChange={(event) => setTOTP(event.target.value)} />
              </div>
            ) : null}
            {approve.isError ? <Alert>Authorization failed. Nothing was disclosed. Go back to the app or terminal that started this request and try again.</Alert> : null}
            <Button variant="primary" type="button" disabled={approve.isPending || !names.settled || !canUseFactor || (!hasPasskey && totp.trim() === '')} onClick={() => approve.mutate('factor')}>
              {approve.isPending ? 'Authorizing…' : 'Authorize'}
            </Button>
            {offersOIDC ? (
              <Button type="button" disabled={approve.isPending || !names.settled || !canUseOIDC} onClick={() => approve.mutate('oidc')}>
                {approve.isPending ? 'Authorizing…' : `Re-authenticate with ${oidcProvider.display_name}`}
              </Button>
            ) : null}
            <Button type="button" onClick={() => globalThis.close()}>Cancel</Button>
          </>
        ) : null}
      </div>
    </main>
  );
}

type PolicyEnvironment = CliReauthTransaction['environments'][number];

/** Environments by name where this person can read them, one project at a time: a handoff may span several. */
function EnvironmentList({ environments }: { environments: readonly PolicyEnvironment[] }) {
  if (environments.length === 0) return null;
  const projects = new Map<string, PolicyEnvironment[]>();
  for (const environment of environments) {
    const key = JSON.stringify([environment.org_id, environment.project_id]);
    projects.set(key, [...(projects.get(key) ?? []), environment]);
  }
  return (
    <ul aria-label="Environments this decision covers">
      {[...projects.entries()].map(([key, members]) => (
        <ProjectEnvironments key={key} environments={members} />
      ))}
    </ul>
  );
}

function ProjectEnvironments({ environments }: { environments: readonly PolicyEnvironment[] }) {
  const first = environments[0];
  const names = useConsentNames(first?.org_id ?? '', first?.project_id ?? '', false);
  return environments.map((environment) => (
    <li key={environment.environment_id}>
      <NamedId id={environment.environment_id} name={names.environmentName(environment.environment_id)} tail />{' '}
      ({environment.requires_webauthn ? 'passkey required' : 'passkey or authenticator code'})
    </li>
  ));
}

function CLIReauthMessage(input: { title: string; text: string }) {
  return (
    <main className="login"><div className="login__card"><h1 className="login__title">{input.title}</h1><Alert>{input.text}</Alert></div></main>
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

function developerCredentialLifetime(seconds: bigint): string {
  if (seconds % 3600n === 0n) {
    const hours = seconds / 3600n;
    return `${String(hours)} ${hours === 1n ? 'hour' : 'hours'} (${String(seconds)} seconds)`;
  }
  return `${String(seconds)} seconds`;
}
