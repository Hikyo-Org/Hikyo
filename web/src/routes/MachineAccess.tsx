import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { grantOutcomeSummary } from '../api/access.ts';
import { useDeliveryTargets } from '../api/deliveryTargets.ts';
import {
  identityRefusalText,
  isoDay,
  postStateReach,
  scopeOf,
  useCredentials,
  useProjectGrants,
  useRevokeCredential,
  useServiceAccounts,
  type MachineCredential,
  type MachineEnvScope,
  type ProjectRef,
  type ServiceAccount,
} from '../api/identities.ts';
import { useCertificates } from '../api/pki.ts';
import { CertificatesTab, certificateCount } from './CertificatesTab.tsx';
import { DeliveryTargetsPanel } from './DeliveryTargets.tsx';
import { SSHCertificatesPanel } from './SSHCertificates.tsx';
import { ApiError } from '../api/client.ts';
import { gateSystemScope } from './SystemScope.tsx';
import {
  useDynamicProviders,
  useLeases,
  type DynamicProvider,
  type LeaseMinted,
} from '../api/dynamic.ts';
import { useMachineReveal } from '../api/machineReveal.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { useResetOnChange } from '../app/useResetOnChange.ts';
import { useEnvironments } from '../api/values.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Glyph } from '../ui/Glyph.tsx';
import { TabPanel, Tabs, type TabItem } from '../ui/Tabs.tsx';
import {
  type IsMintSubmitting,
  type MintBoundary,
  type MoveMint,
  useMintLifecycle,
} from './mintLifecycle.ts';

import { PolicyStrip } from './machineAccess/MachineRevealPolicy.tsx';
import { ExpandableRow, ExpansionBody } from './machineAccess/AccountRows.tsx';
import { MintDialog } from './machineAccess/Credentials.tsx';
import { BindingDialog, BindingCard } from './machineAccess/FederationBindings.tsx';
import { GrantDialog } from './machineAccess/EnvironmentGrants.tsx';
import { CreateAccountDialog, DeleteAccountDialog } from './machineAccess/AccountDialogs.tsx';
import {
  CreateProviderDialog,
  SetCredentialDialog,
  RevokeCredentialDialog,
  DeleteProviderDialog,
} from './machineAccess/DynamicProviders.tsx';
import {
  LeaseMintDialog,
  LeaseActionDialog,
  type LeaseAction,
  type LeaseMintRequest,
} from './machineAccess/DynamicLeases.tsx';


/**
 * The machine-access surface (#67, locked prototype #31 iteration 3).
 *
 * The structure is the prototype's, and each part of it is a rule rather than a
 * layout preference:
 *
 *  - **A tabbed inventory** (service accounts, federation, Kubernetes targets)
 *    with the per-project machine-reveal policy stated above the table.
 *  - **Write-only credential rows.** Prefix hint, kind, expiry in words,
 *    last used. Never the value: no route returns one after the mint, so there
 *    is nothing here that could render it.
 *  - **Row expansion leads with credentials and bindings (left), delivery
 *    targets and actions (right), and the five-step setup journey full-width
 *    below**, iteration 3's resolution, journey underneath rather than on top.
 *  - **Display-once mint.** A step-up naming the post-state formula, then the
 *    value exactly once, with a stored-confirmation checkbox gating dismiss.
 *    Rotation is the same flow: the prior value is never returned.
 *
 * Permission readiness is distinct from observed delivery health. The
 * per-project reveal opt-in is actionable here; grants remain a separate act.
 * Kubernetes targets show what each controller reported and what this server
 * observed, as two layers (DeliveryTargets.tsx), never an invented health
 * result. Binding quarantine is rendered from its actual recovery metadata.

 */

type Tab = 'accounts' | 'federation' | 'kubernetes' | 'providers' | 'leases' | 'ssh' | 'certificates';

/** The route's one-at-a-time dialog selector (not the `ui/Dialog` atom). */
type DialogState =
  | { kind: 'binding'; account: ServiceAccount; replaces?: MachineCredential }
  | { kind: 'grant'; account: ServiceAccount }
  | { kind: 'create' }
  | { kind: 'delete'; account: ServiceAccount }
  | { kind: 'create-provider' }
  | { kind: 'set-credential'; provider: DynamicProvider }
  | { kind: 'revoke-credential'; provider: DynamicProvider }
  | { kind: 'delete-provider'; provider: DynamicProvider };

const TABS: ReadonlyArray<{ id: Tab; label: string }> = [
  { id: 'accounts', label: 'Service accounts' },
  { id: 'federation', label: 'Federation' },
  { id: 'kubernetes', label: 'Kubernetes targets' },
  { id: 'providers', label: 'Providers' },
  { id: 'leases', label: 'Leases' },
  { id: 'ssh', label: 'SSH certificates' },
  { id: 'certificates', label: 'Certificates' },
];

/**
 * tabLabel spells a count the surface knows and says "unknown" for one it does
 * not (a pending or failed listing is not zero).
 */
export function tabLabel(label: string, count: number | 'unknown'): string {
  return `${label} (${count === 'unknown' ? 'unknown' : String(count)})`;
}

/**
 * accountsRefusalText names the listing failure without inventing a cause.
 * A 403 or 404 is the permission answer (the system scope never reaches
 * here: gateSystemScope answers it first). Anything else is not a
 * permission question at all, so it does not name a capability.
 */
export function accountsRefusalText(error: unknown): string {
  if (error instanceof ApiError && (error.status === 403 || error.status === 404)) {
    return 'The service accounts could not be listed. Listing them needs manage-identities on this project.';
  }
  return 'The service accounts could not be listed. Reload to try again.';
}

/** Deep links into the Hikyo system project answer with the profile refusal. */
export const MachineAccess = gateSystemScope('machine-access', MachineAccessPage);

export function MachineAccessPage() {
  const params = useParams();
  const project: ProjectRef = {
    org: params['org'] ?? '',
    project: params['project'] ?? '',
  };

  const accountsQuery = useServiceAccounts(project);
  const grantsQuery = useProjectGrants(project);
  const machineRevealQuery = useMachineReveal(project.org, project.project);
  const auth = useAuth();
  const liveSessionId = auth.identity?.session.id ?? null;
  const boundarySignature = `${project.org}\u0000${project.project}\u0000${liveSessionId}`;
  const reportGrant = auth.identity?.capabilities.delivery_report_grant;
  const machineReveal = machineRevealQuery.data?.enabled ?? false;
  const environmentsQuery = useEnvironments({ ...project, environment: '' });

  const accounts = useMemo(() => accountsQuery.data?.items ?? [], [accountsQuery.data]);
  const environments = useMemo(
    () => environmentsQuery.data?.items ?? [],
    [environmentsQuery.data],
  );
  const leases = useLeases(project, environments);
  const certificates = useCertificates(project, environments);
  const deliveryTargets = useDeliveryTargets(project, environments);
  // Until the environments are read the fan-out is empty, which would read as
  // "no reports": the reports are known only once the server says it accepts
  // them and every listing settled.
  const targetsKnown =
    environmentsQuery.isSuccess &&
    deliveryTargets.support === 'supported' &&
    !deliveryTargets.isPending &&
    deliveryTargets.failures.length === 0;
  const providersQuery = useDynamicProviders(project);
  const providers = useMemo(
    () => providersQuery.data?.items ?? [],
    [providersQuery.data],
  );
  const grants = useMemo(() => grantsQuery.data?.items ?? [], [grantsQuery.data]);
  const credentials = useCredentials(project, accounts);

  const [tab, setTab] = useState<Tab>('accounts');
  const [expanded, setExpanded] = useState<string | null>(null);
  const [dialog, setDialog] = useState<DialogState | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [leaseMintOpen, setLeaseMintOpen] = useState(false);
  const [leaseAction, setLeaseAction] = useState<LeaseAction | null>(null);
  const mintBoundary: MintBoundary = {
    sessionId: liveSessionId,
    org: project.org,
    project: project.project,
  };
  // The display-once mint lifecycle, with its request-addressed completion and
  // its navigation/session/unmount boundary clears (see useMintLifecycle).
  const mint = useMintLifecycle(mintBoundary);
  const moveMint: MoveMint = mint.moveMint;
  const isMintSubmitting: IsMintSubmitting = mint.isSubmitting;
  // A second, independent lifecycle for the dynamic-lease mint. It shares the
  // same session/project boundary, so a route or session change clears the
  // display-once lease password on exactly the same terms as a credential.
  const leaseMint = useMintLifecycle<LeaseMintRequest, LeaseMinted>(mintBoundary);

  // A create, delete, binding or grant dialog carries a decision scoped to ONE
  // project and session. A boundary crossing, a route change, or a session
  // replacement, must close any open one, or a submit after the boundary would
  // target the new project (its form still mounted, now reading the new prop) or
  // act under a replaced session. The mint has its own lifecycle clear above;
  // this covers the setDialog-based dialogs.
  useResetOnChange(boundarySignature, () => {
    setDialog(null);
    setLeaseMintOpen(false);
    setLeaseAction(null);
  });

  const revoke = useRevokeCredential(project);
  const now = useMemo(() => new Date(), []);

  // currentAccount re-reads a captured row from the live listing so dialogs
  // reflect refetches that landed after they opened.
  const currentAccount = (sa: ServiceAccount): ServiceAccount =>
    accounts.find((candidate) => candidate.id === sa.id) ?? sa;
  const scopeFor = (sa: ServiceAccount): MachineEnvScope[] =>
    scopeOf(grants, sa.principal_id, environments);
  const credentialsFor = (sa: ServiceAccount): readonly MachineCredential[] =>
    credentials.byAccount.get(sa.id) ?? [];
  // Un-revoked, which is NOT the same as live: an expired credential is
  // revoked_at-less and authenticates nothing. The count an operator is told is
  // the server's own `live_credentials`, which applies the whole liveness
  // predicate, epoch, revocation and expiry. This filter only decides which
  // rows are worth showing.
  const showable = (rows: readonly MachineCredential[]) =>
    rows.filter((c) => c.revoked_at === undefined);
  const bearers = (rows: readonly MachineCredential[]) =>
    showable(rows).filter((c) => c.kind === 'hikyo-token');
  const bindings = (rows: readonly MachineCredential[]) =>
    showable(rows).filter((c) => c.kind === 'oidc-federation');

  const reviewMint = (account: ServiceAccount, rotating: boolean) => {
    if (liveSessionId === null) {
      setRefusal('The current session could not be read. Reload before minting a credential.');
      return;
    }
    moveMint({
      type: 'review',
      request: {
        id: mint.nextRequestId(),
        sessionId: liveSessionId,
        org: project.org,
        project: project.project,
        accountId: account.id,
        accountName: account.name,
        rotating,
        reach: postStateReach(scopeFor(account)),
      },
    });
  };

  const allBindings = accounts.flatMap((sa) =>
    bindings(credentialsFor(sa)).map((credential) => ({ account: sa, credential })),
  );

  /**
   * inputsReady gates every act on the surface.
   *
   * Each dialog's warning is an assertion about state, how many credentials a
   * grant re-scopes, which environments a mint's formula ranges over, and a
   * query that failed answers those questions with a confident zero. Refusing
   * to act on a half-read surface is the only honest option: the alternative is
   * a warning that understates what the operator is about to do.
   */
  const inputsReady =
    liveSessionId !== null &&
    accountsQuery.isSuccess &&
    grantsQuery.isSuccess &&
    environmentsQuery.isSuccess &&
    !credentials.isPending &&
    !credentials.isError;

  /**
   * canAdminister gates create and delete, and it is deliberately lighter than
   * inputsReady.
   *
   * Create and delete are NARROWINGS, neither carries a warning that quantifies
   * reach, so neither needs the grant, environment or credential reads that
   * inputsReady waits on. Coupling them to that predicate would let an
   * unreadable membership surface (a `manage-identities` admin without
   * `manage-members`) block seeding a fresh project, exactly the inert
   * inventory this surface exists to remove. They need only a live session and a
   * known account listing.
   */
  const canAdminister = liveSessionId !== null && accountsQuery.isSuccess;

  // Provider administration needs only a live session and a readable provider
  // listing, the same lighter gate as create/delete above, for the same reason.
  const canAdministerProviders = liveSessionId !== null && providersQuery.isSuccess;

  // A lease can be minted only against a provider that is active AND holds a
  // credential (the mint dials the provider), and only into an environment.
  const mintableProviders = useMemo(
    () => providers.filter((p) => p.state === 'active' && p.credential_present),
    [providers],
  );
  const canMintLease =
    liveSessionId !== null &&
    providersQuery.isSuccess &&
    environmentsQuery.isSuccess &&
    mintableProviders.length > 0 &&
    environments.length > 0;

  const tabCount: Record<Tab, number | 'unknown'> = {
    accounts: accountsQuery.isSuccess ? accounts.length : 'unknown',
    // Unknown is rendered as unknown: "Federation (0)" on a failed listing
    // reads as "there are none", which is the one thing it does not know.
    federation: credentials.isPending || credentials.isError ? 'unknown' : allBindings.length,
    // Reported targets across the readable environments; a listing that is
    // pending or failed makes the total unknown, never a smaller number.
    kubernetes: targetsKnown
      ? deliveryTargets.reports.reduce((sum, r) => sum + r.list.targets.length, 0)
      : 'unknown',
    providers: providersQuery.isSuccess ? providers.length : 'unknown',
    leases: leases.isPending || leases.isError ? 'unknown' : leases.rows.length,
    // SSH objects are per environment; the tab carries no project-wide count.
    ssh: 'unknown',
    certificates: certificateCount(certificates),
  };
  const unknownLeases = leases.rows.filter((row) => row.lease.state === 'unknown').length;
  const countedTabs: readonly TabItem<Tab>[] = TABS.map((entry) => ({
    id: entry.id,
    label: entry.id === 'ssh' ? entry.label : tabLabel(entry.label, tabCount[entry.id]),
  }));

  const doRevoke = (account: ServiceAccount, credential: MachineCredential) => {
    setRefusal(null);
    revoke.mutate(
      { serviceAccount: account.id, credential: credential.id },
      {
        onSuccess: () =>
          setNotice(
            'Revoked. It stops authenticating at the next request, never at expiry; grants and sibling credentials are untouched.',
          ),
        onError: (error) => setRefusal(identityRefusalText(error)),
      },
    );
  };

  const activeMintLifecycle = mint.active;

  return (
    <section className="card card--wide machine" aria-labelledby="machine-title">
      <header className="values__head">
        <h1 id="machine-title">Machine access</h1>
      </header>

      {accountsQuery.isError ? (
        <Alert>{accountsRefusalText(accountsQuery.error)}</Alert>
      ) : null}

      {grantsQuery.isError ? (
        <Alert>
          The grant rows could not be read, so no scope is shown below. Reading the membership
          surface needs manage-members on this project, a separate authority from administering
          identities.
        </Alert>
      ) : null}

      {environmentsQuery.isError ? (
        <Alert>
          The project&apos;s environments could not be read, so no scope is shown below; an empty
          scope column here would say &ldquo;this account reaches nothing&rdquo;, which is not
          something this page knows.
        </Alert>
      ) : null}

      {credentials.isError ? (
        <Alert>
          At least one service account&apos;s credentials could not be listed. Counts and the
          federation tab are incomplete, and the actions are held back until the listing succeeds.
        </Alert>
      ) : null}

      {refusal !== null ? (
        <Alert>{refusal}</Alert>
      ) : null}

      {credentials.isPending && !credentials.isError && accounts.length > 0 ? (
        <p className="notice" role="status">{/* markup-check: in-flight status, transient */}
          <span className="alert__glyph" aria-hidden="true">
            <Glyph name="ellipsis" />
          </span>
          <span>Reading credentials…</span>
        </p>
      ) : null}

      {notice !== null ? (
        <Alert tone="done">{notice}</Alert>
      ) : null}

      <Tabs
        label="Machine access sections"
        idPrefix="machine"
        tabs={countedTabs}
        selected={tab}
        onSelect={setTab}
      />

      <TabPanel idPrefix="machine" selected={tab}>
        {tab === 'accounts' ? (
          <>
            <PolicyStrip key={boundarySignature} project={project} />
            <p className="machine__actions">
              <Button
                variant="primary"
                type="button"
                disabled={!canAdminister}
                onClick={() => setDialog({ kind: 'create' })}
              >
                Create service account
              </Button>
            </p>
            <table className="values__table machine__table">
              <caption className="visually-hidden">
                The project&apos;s service accounts. Credential values are never listed: a value is
                displayed once, at mint.
              </caption>
              <thead>
                <tr>
                  <th scope="col">Service account</th>
                  <th scope="col">Kind</th>
                  <th scope="col">Read scope (◆ = reveal)</th>
                  <th scope="col" className="col-secondary">
                    Credentials
                  </th>
                  <th scope="col" className="col-secondary">
                    Last used
                  </th>
                  <th scope="col">Setup</th>
                </tr>
              </thead>
              <tbody>
                {accounts.map((sa) => {
                  const rows = credentialsFor(sa);
                  const scope = scopeFor(sa);
                  const open = expanded === sa.id;
                  const newest = showable(rows)
                    .map((c) => c.last_used_at)
                    .filter((at): at is string => at !== undefined)
                    .sort()
                    .at(-1);
                  return (
                    <ExpandableRow
                      key={sa.id}
                      account={sa}
                      scope={scope}
                      open={open}
                      scopeKnown={grantsQuery.isSuccess && environmentsQuery.isSuccess}
                      machineReveal={machineReveal}
                      lastUsed={newest === undefined ? 'never' : isoDay(newest)}
                      onToggle={() => setExpanded(open ? null : sa.id)}
                    >
                      <ExpansionBody
                        account={sa}
                        scope={scope}
                        machineReveal={machineReveal}
                        deliveryTargets={deliveryTargets}
                        targetsKnown={targetsKnown}
                        reportingUnsupported={deliveryTargets.support === 'unsupported'}
                        bearers={bearers(rows)}
                        bindings={bindings(rows)}
                        now={now}
                        ready={inputsReady}
                        canDelete={canAdminister}
                        onMint={(rotating) => reviewMint(sa, rotating)}
                        onBind={() => setDialog({ kind: 'binding', account: sa })}
                        onGrant={() => setDialog({ kind: 'grant', account: sa })}
                        onRevoke={(credential) => doRevoke(sa, credential)}
                        onReplaceBinding={(credential) =>
                          setDialog({ kind: 'binding', account: sa, replaces: credential })
                        }
                        onDelete={() => setDialog({ kind: 'delete', account: sa })}
                      />
                    </ExpandableRow>
                  );
                })}
              </tbody>
            </table>
            {accounts.length === 0 && !accountsQuery.isPending && !accountsQuery.isError ? (
              <p role="status">
                No service accounts on this project yet. Create one above: a browser operator seeds
                this inventory, no CLI required.
              </p>
            ) : null}
            <p className="machine__footnote">
              The credential list is metadata only: prefix, kind, scope, expiry, last used. Values
              are write-only: displayed exactly once at mint, never retrievable, and rotation never
              returns the prior value.
            </p>
          </>
        ) : null}

        {tab === 'federation' ? (
          <>
            <h2>Federated bindings</h2>
            <p className="machine__lede">
              An external OIDC identity is bound to exactly one service account by a byte-exact
              (issuer, subject) pair, no wildcards, no case folding, no just-in-time provisioning.
              An unbound identity is not a login. A binding expires on the same terms as a bearer
              credential and is immutable: renewal is a mint.
            </p>
            {accounts.length > 0 ? (
              <p className="machine__actions">
                <Button
                  variant="primary"
                  type="button"
                  disabled={!inputsReady}
                  onClick={() => {
                    const first = accounts[0];
                    if (first !== undefined) {
                      setDialog({ kind: 'binding', account: first });
                    }
                  }}
                >
                  New binding
                </Button>
              </p>
            ) : null}
            {credentials.isPending || credentials.isError ? (
              <p role="status">
                {credentials.isError
                  ? 'The bindings could not be listed, so none are shown. This is not the same as there being none.'
                  : 'Reading bindings…'}
              </p>
            ) : allBindings.length === 0 ? (
              <p role="status">
                No federated bindings on this project. Add one from a service account&apos;s row.
              </p>
            ) : (
              <ul className="machine__bindings">
                {allBindings.map(({ account, credential }) => (
                  <li key={credential.id}>
                    <BindingCard
                      account={account}
                      credential={credential}
                      now={now}
                      ready={inputsReady}
                      onReplace={(predecessor) =>
                        setDialog({ kind: 'binding', account, replaces: predecessor })
                      }
                      onRevoke={(target) => doRevoke(account, target)}
                    />
                  </li>
                ))}
              </ul>
            )}
          </>
        ) : null}

        {tab === 'kubernetes' ? (
          <DeliveryTargetsPanel
            project={project}
            view={deliveryTargets}
            known={targetsKnown}
            accounts={accounts}
            now={now}
          />
        ) : null}

        {tab === 'providers' ? (
          <>
            <h2>Dynamic-secret providers</h2>
            <p className="machine__lede">
              Where Hikyo mints short-lived credentials. A provider is project-scoped, always
              connects over <code>verify-full</code> TLS, and its admin credential is write-only,
              set or replaced, never read back. Every minted lease role inherits the grant role.
            </p>
            <p className="machine__actions">
              <Button
                variant="primary"
                type="button"
                disabled={!canAdministerProviders}
                onClick={() => {
                  setRefusal(null);
                  setNotice(null);
                  setDialog({ kind: 'create-provider' });
                }}
              >
                Configure provider
              </Button>
            </p>
            <table className="values__table machine__table">
              <caption className="visually-hidden">
                The project&apos;s dynamic-secret providers. The admin credential is never listed.
              </caption>
              <thead>
                <tr>
                  <th scope="col">Kind</th>
                  <th scope="col">Origin</th>
                  <th scope="col" className="col-secondary">
                    Grant role
                  </th>
                  <th scope="col">Credential</th>
                  <th scope="col" className="col-secondary">
                    State
                  </th>
                  <th scope="col">Actions</th>
                </tr>
              </thead>
              <tbody>
                {providers.map((provider) => (
                  <tr key={provider.id}>
                    <td>{provider.kind}</td>
                    <td>
                      <code>{provider.origin}</code>
                    </td>
                    <td className="col-secondary">
                      <code>{provider.grant_role}</code>
                    </td>
                    <td>
                      {provider.credential_present
                        ? provider.credential_set_at === undefined ||
                          provider.credential_set_at === null
                          ? 'set'
                          : `set ${isoDay(provider.credential_set_at)}`
                        : 'credential absent'}
                    </td>
                    <td className="col-secondary">{provider.state}</td>
                    <td>
                      {provider.state === 'active' ? (
                        <span className="machine__row-actions">
                          <Button
                            type="button"
                            disabled={!canAdministerProviders}
                            onClick={() => {
                              setRefusal(null);
                              setNotice(null);
                              setDialog({ kind: 'set-credential', provider });
                            }}
                          >
                            {provider.credential_present
                              ? 'Replace credential'
                              : 'Set credential'}
                          </Button>
                          {provider.credential_present ? (
                            <Button
                              type="button"
                              disabled={!canAdministerProviders}
                              onClick={() => {
                                setRefusal(null);
                                setNotice(null);
                                setDialog({ kind: 'revoke-credential', provider });
                              }}
                            >
                              Revoke credential
                            </Button>
                          ) : null}
                          <Button
                            type="button"
                            disabled={!canAdministerProviders}
                            onClick={() => {
                              setRefusal(null);
                              setNotice(null);
                              setDialog({ kind: 'delete-provider', provider });
                            }}
                          >
                            Delete
                          </Button>
                        </span>
                      ) : (
                        <span className="values__absent">none</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {providersQuery.isError ? (
              <p role="status">The providers could not be read.</p>
            ) : providers.length === 0 && !providersQuery.isPending ? (
              <p role="status">
                No dynamic-secret providers on this project yet. Configure one to mint leased
                credentials.
              </p>
            ) : null}
          </>
        ) : null}

        {tab === 'certificates' ? (
          <CertificatesTab project={project} environments={environments} view={certificates} />
        ) : null}

        {tab === 'leases' ? (
          <>
            <h2>Dynamic-secret leases</h2>
            <p className="machine__lede">
              Short-lived credentials Hikyo minted at a provider. Status and metadata only: the
              password is disclosed once, at mint, and never shown again. Renew, revoke and settle
              are queued: the worker carries them out and the row moves when it does.
            </p>
            <p className="machine__actions">
              <Button
                variant="primary"
                type="button"
                disabled={!canMintLease}
                onClick={() => {
                  setRefusal(null);
                  setNotice(null);
                  setLeaseMintOpen(true);
                }}
              >
                Mint lease
              </Button>
              {!canMintLease && providersQuery.isSuccess && mintableProviders.length === 0 ? (
                <span className="machine__hint">
                  Configure a provider with a credential first: a lease is minted against one.
                </span>
              ) : null}
            </p>
            <table className="values__table machine__table">
              <caption className="visually-hidden">
                The project&apos;s dynamic-secret leases across every environment. No secret value is
                listed.
              </caption>
              <thead>
                <tr>
                  <th scope="col">Environment</th>
                  <th scope="col">Handle</th>
                  <th scope="col">State</th>
                  <th scope="col" className="col-secondary">
                    Expires
                  </th>
                  <th scope="col" className="col-secondary">
                    Principal
                  </th>
                  <th scope="col">Actions</th>
                </tr>
              </thead>
              <tbody>
                {leases.rows.map((row) => {
                  const state = row.lease.state;
                  const canRenew = state === 'active';
                  const canRevoke = !['revoked', 'expired', 'failed', 'revoking'].includes(state);
                  const canSettle = state === 'unknown';
                  const openAction = (verb: LeaseAction['verb']) => {
                    setRefusal(null);
                    setNotice(null);
                    setLeaseAction({
                      verb,
                      environmentId: row.lease.environment_id,
                      environmentName: row.environmentName,
                      lease: row.lease,
                    });
                  };
                  return (
                    <tr key={row.lease.id}>
                      <td>{row.environmentName}</td>
                      <td>
                        <code>{row.lease.provider_handle}</code>
                      </td>
                      <td>
                        {state === 'unknown' ? (
                          <Badge tone="changed">unknown, awaiting reconcile</Badge>
                        ) : (
                          state
                        )}
                      </td>
                      <td className="col-secondary">
                        {row.lease.expires_at === undefined || row.lease.expires_at === null
                          ? <span className="values__absent">absent</span>
                          : isoDay(row.lease.expires_at)}
                      </td>
                      <td className="col-secondary">{row.lease.principal_class}</td>
                      <td>
                        {liveSessionId !== null && (canRenew || canRevoke || canSettle) ? (
                          <span className="machine__row-actions">
                            {canRenew ? (
                              <Button type="button" onClick={() => openAction('renew')}>
                                Renew
                              </Button>
                            ) : null}
                            {canSettle ? (
                              <Button
                                type="button"
                                onClick={() => openAction('settle')}
                              >
                                Settle
                              </Button>
                            ) : null}
                            {canRevoke ? (
                              <Button
                                type="button"
                                onClick={() => openAction('revoke')}
                              >
                                Revoke
                              </Button>
                            ) : null}
                          </span>
                        ) : (
                          <span className="values__absent">none</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {unknownLeases === 0 ? null : (
              <p role="status" className="machine__hint">
                {`${String(unknownLeases)} lease${unknownLeases === 1 ? '' : 's'} awaiting reconcile`}
              </p>
            )}
            {leases.isError ? (
              <p role="status">The leases could not be read for one or more environments.</p>
            ) : leases.rows.length === 0 && !leases.isPending ? (
              <p role="status">
                No dynamic-secret leases on this project yet. Configure a provider and mint one.
              </p>
            ) : null}
          </>
        ) : null}
        {tab === 'ssh' ? (
          <SSHCertificatesPanel
            org={project.org}
            project={project.project}
            sessionId={liveSessionId}
            environments={environments.map((environment) => ({ id: environment.id, name: environment.name }))}
            requesterOptions={[
              ...(auth.identity === null || auth.identity === undefined
                ? []
                : [{ id: auth.identity.principal.id, label: 'me' }]),
              ...accounts.map((account) => ({ id: account.principal_id, label: account.name })),
            ]}
          />
        ) : null}
      </TabPanel>

      {activeMintLifecycle.kind !== 'idle' ? (
        <MintDialog
          lifecycle={activeMintLifecycle}
          move={moveMint}
          isSubmitting={isMintSubmitting}
        />
      ) : null}

      {dialog?.kind === 'binding' ? (
        <BindingDialog
          project={project}
          accounts={accounts}
          initial={dialog.account}
          replaces={dialog.replaces}
          reachFor={(accountId) => {
            const sa = accounts.find((candidate) => candidate.id === accountId);
            return sa === undefined ? [] : postStateReach(scopeFor(sa));
          }}
          onClose={() => setDialog(null)}
          onCreated={(message) => {
            setDialog(null);
            setNotice(message);
          }}
        />
      ) : null}

      {dialog?.kind === 'grant' ? (
        <GrantDialog
          project={project}
          account={currentAccount(dialog.account)}
          scope={scopeFor(dialog.account)}
          machineReveal={machineReveal}
          mayGrantReporting={
            reportGrant !== undefined &&
            (reportGrant.instance || reportGrant.orgs.includes(project.org))
          }
          // The SERVER's count, which applies the whole liveness predicate,
          // revocation, the credential epoch and expiry. Counting un-revoked
          // rows here would tell an operator that a grant re-scopes credentials
          // that stopped authenticating weeks ago. Read it from the live
          // listing, not the row captured at click time: a dialog opened a
          // moment after a mint would otherwise report the pre-mint count for
          // as long as it stays open.
          liveCredentials={currentAccount(dialog.account).live_credentials}
          onClose={() => setDialog(null)}
          onGranted={(environment, results) => {
            setDialog(null);
            setNotice(
              `Grant result for ${environment}: ${grantOutcomeSummary(results)}`,
            );
          }}
        />
      ) : null}

      {dialog?.kind === 'create' ? (
        <CreateAccountDialog
          project={project}
          onClose={() => setDialog(null)}
          onCreated={(name, kind) => {
            setDialog(null);
            setNotice(
              `Created ${name} (${kind}). It is ready to mint credentials and take federated bindings; its kind is immutable from here.`,
            );
          }}
        />
      ) : null}

      {dialog?.kind === 'delete' ? (
        <DeleteAccountDialog
          project={project}
          account={dialog.account}
          onClose={() => setDialog(null)}
          onDeleted={(name) => {
            setDialog(null);
            // The expanded row is gone; collapse before its listing is removed
            // so nothing tries to render a deleted account's expansion.
            if (expanded === dialog.account.id) {
              setExpanded(null);
            }
            setNotice(
              `Deleted ${name}. Every credential it held is revoked and every grant released, atomically, and this cannot be undone.`,
            );
          }}
        />
      ) : null}

      {dialog?.kind === 'create-provider' ? (
        <CreateProviderDialog
          project={project}
          onClose={() => setDialog(null)}
          onCreated={(origin) => {
            setDialog(null);
            setNotice(
              `Configured provider ${origin}. Hikyo reached it and authenticated; it can now mint leased credentials.`,
            );
          }}
        />
      ) : null}

      {dialog?.kind === 'set-credential' ? (
        <SetCredentialDialog
          project={project}
          provider={dialog.provider}
          onClose={() => setDialog(null)}
          onSet={(origin) => {
            setDialog(null);
            setNotice(
              `Replaced the admin credential for ${origin}. Hikyo re-probed the provider and authenticated with the new one.`,
            );
          }}
        />
      ) : null}

      {dialog?.kind === 'revoke-credential' ? (
        <RevokeCredentialDialog
          project={project}
          provider={dialog.provider}
          onClose={() => setDialog(null)}
          onRevoked={(origin) => {
            setDialog(null);
            setNotice(
              `Cleared the admin credential for ${origin}. Existing leases stay minted at the provider, but Hikyo cannot renew, revoke or expire them until a credential is set.`,
            );
          }}
        />
      ) : null}

      {dialog?.kind === 'delete-provider' ? (
        <DeleteProviderDialog
          project={project}
          provider={dialog.provider}
          liveLeaseCount={leases.rows.filter(
            (row) =>
              row.lease.provider_id === dialog.provider.id &&
              !['revoked', 'expired', 'failed'].includes(row.lease.state),
          ).length}
          leasesKnown={!leases.isPending && !leases.isError}
          onClose={() => setDialog(null)}
          onDeleted={(origin, revokedCount) => {
            setDialog(null);
            setNotice(
              revokedCount === 0
                ? `Deleted provider ${origin}.`
                : `Deleted provider ${origin}. ${String(revokedCount)} live lease${revokedCount === 1 ? ' was' : 's were'} queued for revocation as part of the delete.`,
            );
          }}
        />
      ) : null}

      {leaseMintOpen || leaseMint.active.kind !== 'idle' ? (
        <LeaseMintDialog
          project={project}
          sessionId={liveSessionId}
          providers={mintableProviders}
          environments={environments}
          lifecycle={leaseMint.active}
          move={leaseMint.moveMint}
          isSubmitting={leaseMint.isSubmitting}
          nextRequestId={leaseMint.nextRequestId}
          onClose={() => setLeaseMintOpen(false)}
        />
      ) : null}

      {leaseAction !== null ? (
        <LeaseActionDialog
          project={project}
          action={leaseAction}
          onClose={() => setLeaseAction(null)}
          onDone={(message) => {
            setLeaseAction(null);
            setNotice(message);
          }}
        />
      ) : null}
    </section>
  );
}
