import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type MutableRefObject,
  type ReactNode,
} from 'react';
import { useParams } from 'react-router';

import { useSensitiveState } from '../api/sensitiveMutation.ts';
import type { FederatedClaimPin, GrantResult } from '@hikyo/client';

import { grantFailureText, grantOutcomeSummary } from '../api/access.ts';
import {
  BINDING_LIFETIMES,
  bindingFailureText,
  CI_EVENTS,
  createServiceAccountFailureText,
  deleteServiceAccountFailureText,
  expiryLabel,
  FEDERATION_PRESETS,
  grantableFor,
  grantSubmittable,
  grantWideningReach,
  identityRefusalText,
  isoDay,
  KUBERNETES_PRESET,
  lastUsedLabel,
  mintCredential,
  mintFailureText,
  parseClaimNumber,
  postStateReach,
  presetFieldId,
  pullRequestRefusal,
  scopeOf,
  serviceAccountNameRefusal,
  setupJourney,
  useCreateBinding,
  useCreateServiceAccount,
  useCredentials,
  useDeleteServiceAccount,
  useGrantEnvironment,
  useKeyCatalogue,
  useProjectGrants,
  useRefreshAccount,
  useRefreshGrants,
  useRefreshServiceAccounts,
  useRevokeCredential,
  useServiceAccounts,
  type ClaimPin,
  type FederationPreset,
  type JourneyAction,
  type MachineCredential,
  type MachineEnvScope,
  type ProjectRef,
  type ServiceAccount,
} from '../api/identities.ts';
import { TypedNameConfirm } from './Sections.tsx';
import { ApiError } from '../api/client.ts';
import {
  createDynamicProvider,
  createProviderRefusalText,
  deleteProviderRefusalText,
  leaseActionRefusalText,
  leaseMintFailureText,
  leaseMintRefusalText,
  mintLease,
  revokeCredentialRefusalText,
  setCredentialRefusalText,
  setDynamicProviderCredential,
  useDeleteDynamicProvider,
  useDynamicProviders,
  useLeases,
  useRefreshLeases,
  useRefreshProviders,
  useRenewLease,
  useRevokeDynamicProviderCredential,
  useRevokeLease,
  useSettleLease,
  type DynamicLease,
  type DynamicProvider,
  type LeaseMinted,
} from '../api/dynamic.ts';
import { useMachineReveal, useSetMachineReveal } from '../api/machineReveal.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { writeClipboard } from '../app/clipboard.ts';
import { runPasskeyCeremony, useEnvironments } from '../api/values.ts';
import {
  type IsMintSubmitting,
  type MintBoundary,
  type MintBoundaryFields,
  type MintLifecycle,
  type MoveMint,
  useMintLifecycle,
} from './mintLifecycle.ts';
import { useModalDialog } from './useModalDialog.ts';

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
 * Kubernetes condition state is unavailable in this view, so operators are
 * directed to kubectl rather than shown an invented health result. Binding
 * quarantine is rendered from its actual recovery metadata.

 */

type Tab = 'accounts' | 'federation' | 'kubernetes' | 'providers' | 'leases';

type Dialog =
  | { kind: 'binding'; account: ServiceAccount; replaces?: MachineCredential }
  | { kind: 'grant'; account: ServiceAccount }
  | { kind: 'create' }
  | { kind: 'delete'; account: ServiceAccount }
  | { kind: 'create-provider' }
  | { kind: 'set-credential'; provider: DynamicProvider }
  | { kind: 'revoke-credential'; provider: DynamicProvider }
  | { kind: 'delete-provider'; provider: DynamicProvider };

/** A queued lease-lifecycle action awaiting confirmation. */
type LeaseAction = {
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
type LeaseMintRequest = MintBoundaryFields & {
  readonly providerId: string;
  readonly providerLabel: string;
  readonly environmentId: string;
  readonly environmentName: string;
  readonly maxTtlSeconds: number;
};

const TABS: ReadonlyArray<{ id: Tab; label: string }> = [
  { id: 'accounts', label: 'Service accounts' },
  { id: 'federation', label: 'Federation' },
  { id: 'kubernetes', label: 'Kubernetes targets' },
  { id: 'providers', label: 'Providers' },
  { id: 'leases', label: 'Leases' },
];

/**
 * tabLabel spells a count the surface knows, says "unknown" for one it does
 * not (a pending or failed listing is not zero), and carries no count at all
 * for a tab that reports no status: the Kubernetes panel explains that absence
 * of status is not health, so a "(0)" there would be the invented number.
 */
export function tabLabel(label: string, count: number | 'unknown' | null): string {
  return count === null ? label : `${label} (${count === 'unknown' ? 'unknown' : String(count)})`;
}

/** nextTab is the APG roving order for a horizontal tablist: arrows wrap, Home/End jump. */
export function nextTab<T>(tabs: readonly T[], current: T, key: string): T | null {
  const index = tabs.indexOf(current);
  if (index < 0 || tabs.length === 0) {
    return null;
  }
  switch (key) {
    case 'ArrowRight':
      return tabs[(index + 1) % tabs.length] ?? null;
    case 'ArrowLeft':
      return tabs[(index - 1 + tabs.length) % tabs.length] ?? null;
    case 'Home':
      return tabs[0] ?? null;
    case 'End':
      return tabs[tabs.length - 1] ?? null;
    default:
      return null;
  }
}

export function MachineAccess() {
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
  const machineReveal = machineRevealQuery.data?.enabled ?? false;
  const environmentsQuery = useEnvironments({ ...project, environment: '' });

  const accounts = useMemo(() => accountsQuery.data?.items ?? [], [accountsQuery.data]);
  const environments = useMemo(
    () => environmentsQuery.data?.items ?? [],
    [environmentsQuery.data],
  );
  const leases = useLeases(project, environments);
  const providersQuery = useDynamicProviders(project);
  const providers = useMemo(
    () => providersQuery.data?.items ?? [],
    [providersQuery.data],
  );
  const grants = useMemo(() => grantsQuery.data?.items ?? [], [grantsQuery.data]);
  const credentials = useCredentials(project, accounts);

  const [tab, setTab] = useState<Tab>('accounts');
  const [expanded, setExpanded] = useState<string | null>(null);
  const [dialog, setDialog] = useState<Dialog | null>(null);
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
  useEffect(() => {
    setDialog(null);
    setLeaseMintOpen(false);
    setLeaseAction(null);
  }, [project.org, project.project, liveSessionId]);

  const revoke = useRevokeCredential(project);
  const now = useMemo(() => new Date(), []);

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
        reach: postStateReach(scopeFor(account)).map(({ id, name }) => ({ id, name })),
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

  const tabCount: Record<Tab, number | 'unknown' | null> = {
    accounts: accountsQuery.isSuccess ? accounts.length : 'unknown',
    // Unknown is rendered as unknown: "Federation (0)" on a failed listing
    // reads as "there are none", which is the one thing it does not know.
    federation: credentials.isPending || credentials.isError ? 'unknown' : allBindings.length,
    // No status is reported here, so no count: the panel says why.
    kubernetes: null,
    providers: providersQuery.isSuccess ? providers.length : 'unknown',
    leases: leases.isPending || leases.isError ? 'unknown' : leases.rows.length,
  };
  const unknownLeases = leases.rows.filter((row) => row.lease.state === 'unknown').length;
  const tabRefs = useRef(new Map<Tab, HTMLButtonElement>());

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
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            The service accounts could not be listed. Listing them needs manage-identities on this
            project.
          </span>
        </p>
      ) : null}

      {grantsQuery.isError ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            The grant rows could not be read, so no scope is shown below. Reading the membership
            surface needs manage-members on this project, a separate authority from administering
            identities.
          </span>
        </p>
      ) : null}

      {environmentsQuery.isError ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            The project&apos;s environments could not be read, so no scope is shown below; an empty
            scope column here would say &ldquo;this account reaches nothing&rdquo;, which is not
            something this page knows.
          </span>
        </p>
      ) : null}

      {credentials.isError ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            At least one service account&apos;s credentials could not be listed. Counts and the
            federation tab are incomplete, and the actions are held back until the listing succeeds.
          </span>
        </p>
      ) : null}

      {refusal !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{refusal}</span>
        </p>
      ) : null}

      {credentials.isPending && !credentials.isError && accounts.length > 0 ? (
        <p className="notice" role="status">
          <span className="alert__glyph" aria-hidden="true">
            ⋯
          </span>
          <span>Reading credentials…</span>
        </p>
      ) : null}

      {notice !== null ? (
        <p className="notice" role="status">
          <span className="alert__glyph" aria-hidden="true">
            ✓
          </span>
          <span>{notice}</span>
        </p>
      ) : null}

      {/* APG tabs: one tab stop, arrows move and select, Home/End jump. */}
      <div
        className="tabs"
        role="tablist"
        aria-label="Machine access sections"
        onKeyDown={(event) => {
          const target = nextTab(TABS.map((entry) => entry.id), tab, event.key);
          if (target === null) {
            return;
          }
          event.preventDefault();
          setTab(target);
          tabRefs.current.get(target)?.focus();
        }}
      >
        {TABS.map((entry) => (
          <button
            key={entry.id}
            ref={(node) => {
              if (node === null) {
                tabRefs.current.delete(entry.id);
              } else {
                tabRefs.current.set(entry.id, node);
              }
            }}
            type="button"
            role="tab"
            id={`machine-tab-${entry.id}`}
            className="tab"
            aria-selected={tab === entry.id}
            aria-controls="machine-panel"
            tabIndex={tab === entry.id ? 0 : -1}
            onClick={() => setTab(entry.id)}
          >
            {tabLabel(entry.label, tabCount[entry.id])}
          </button>
        ))}
      </div>

      <div className="tabpanel" role="tabpanel" id="machine-panel" aria-labelledby={`machine-tab-${tab}`}>
        {tab === 'accounts' ? (
          <>
            <PolicyStrip project={project} />
            <p className="machine__actions">
              <button
                className="btn btn--primary"
                type="button"
                disabled={!canAdminister}
                onClick={() => setDialog({ kind: 'create' })}
              >
                Create service account
              </button>
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
                <button
                  className="btn btn--primary"
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
                </button>
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
          <>
            <h2>Kubernetes delivery targets</h2>
            <p className="machine__lede">
              One managed Secret per delivery target. Inspect each HikyoSecret condition with kubectl.
            </p>
            <p role="status">
              No delivery targets are reported in this view. Check HikyoSecret conditions with
              kubectl; an empty list here means no status is available here, never that everything
              is healthy.
            </p>
          </>
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
              <button
                className="btn btn--primary"
                type="button"
                disabled={!canAdministerProviders}
                onClick={() => {
                  setRefusal(null);
                  setNotice(null);
                  setDialog({ kind: 'create-provider' });
                }}
              >
                Configure provider
              </button>
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
                          <button
                            className="btn"
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
                          </button>
                          {provider.credential_present ? (
                            <button
                              className="btn"
                              type="button"
                              disabled={!canAdministerProviders}
                              onClick={() => {
                                setRefusal(null);
                                setNotice(null);
                                setDialog({ kind: 'revoke-credential', provider });
                              }}
                            >
                              Revoke credential
                            </button>
                          ) : null}
                          <button
                            className="btn"
                            type="button"
                            disabled={!canAdministerProviders}
                            onClick={() => {
                              setRefusal(null);
                              setNotice(null);
                              setDialog({ kind: 'delete-provider', provider });
                            }}
                          >
                            Delete
                          </button>
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

        {tab === 'leases' ? (
          <>
            <h2>Dynamic-secret leases</h2>
            <p className="machine__lede">
              Short-lived credentials Hikyo minted at a provider. Status and metadata only: the
              password is disclosed once, at mint, and never shown again. Renew, revoke and settle
              are queued: the worker carries them out and the row moves when it does.
            </p>
            <p className="machine__actions">
              <button
                className="btn btn--primary"
                type="button"
                disabled={!canMintLease}
                onClick={() => {
                  setRefusal(null);
                  setNotice(null);
                  setLeaseMintOpen(true);
                }}
              >
                Mint lease
              </button>
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
                          <span className="badge badge--warn">unknown, awaiting reconcile</span>
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
                              <button className="btn" type="button" onClick={() => openAction('renew')}>
                                Renew
                              </button>
                            ) : null}
                            {canSettle ? (
                              <button
                                className="btn"
                                type="button"
                                onClick={() => openAction('settle')}
                              >
                                Settle
                              </button>
                            ) : null}
                            {canRevoke ? (
                              <button
                                className="btn"
                                type="button"
                                onClick={() => openAction('revoke')}
                              >
                                Revoke
                              </button>
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
      </div>

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
          account={dialog.account}
          scope={scopeFor(dialog.account)}
          machineReveal={machineReveal}
          // The SERVER's count, which applies the whole liveness predicate,
          // revocation, the credential epoch and expiry. Counting un-revoked
          // rows here would tell an operator that a grant re-scopes credentials
          // that stopped authenticating weeks ago.
          liveCredentials={dialog.account.live_credentials}
          onClose={() => setDialog(null)}
          onGranted={(environment, result) => {
            setDialog(null);
            setNotice(
              `Grant result for ${environment}: ${grantOutcomeSummary([result])}`,
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

/**
 * PolicyStrip is the per-project machine-reveal opt-in (source-of-truth ADR:
 * "an explicit, documented, per-project operator opt-in, never a default").
 *
 * It is a toggle with a ceremony both ways. Enabling states, at opt-in time,
 * what the machine-identities ADR requires it to state: a machine principal
 * holding reveal is a standing decryption capability, while workload
 * reveal-history is admitted only for an active non-current pin. Withdrawing
 * states the other half: both disclosure atoms go inert on the next fetch,
 * and every machine cursor moves. The write is
 * project-settings ∧ reveal at project depth, so it is MFA-mandatory; a
 * session short of that is told so by the server and the strip repeats it.
 */
function PolicyStrip({ project }: { project: ProjectRef }) {
  const query = useMachineReveal(project.org, project.project);
  const write = useSetMachineReveal(project.org, project.project);
  const [confirming, setConfirming] = useState<boolean | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  if (query.isPending) {
    return (
      <p className="notice machine__policy" role="status">
        <span className="alert__glyph" aria-hidden="true">
          ◆
        </span>
        <span>Reading the project&apos;s machine-reveal opt-in…</span>
      </p>
    );
  }
  if (query.isError) {
    return (
      <p className="alert machine__policy" role="alert">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          The project&apos;s machine-reveal opt-in could not be read: {identityRefusalText(query.error)}
        </span>
      </p>
    );
  }
  const enabled = query.data.enabled;
  const commit = (next: boolean) => {
    setFailure(null);
    write.mutate(next, {
      onSuccess: () => setConfirming(null),
      onError: (error) => setFailure(identityRefusalText(error)),
    });
  };
  return (
    <>
      <p className="notice machine__policy" role="status">
        <span className="alert__glyph" aria-hidden="true">
          ◆
        </span>
        <span>
          <strong>
            Machine secret delivery (per-project opt-in): {enabled ? 'on' : 'off'}.
          </strong>{' '}
          {enabled
            ? 'Workload and automation principals may hold reveal; a workload may also hold reveal-history while pinned to a non-current revision. Either can deliver secret plaintext.'
            : 'Every workload delivery is configuration and secret presence only; the grant API refuses both machine disclosure capabilities until this is on.'}
        </span>
        <button
          id="machine-policy-toggle"
          type="button"
          className="btn machine__policy-toggle"
          onClick={() => setConfirming(!enabled)}
          disabled={write.isPending}
        >
          {enabled ? 'Withdraw the opt-in…' : 'Enable the opt-in…'}
        </button>
      </p>
      {confirming !== null ? (
        <MachineRevealDialog
          enable={confirming}
          busy={write.isPending}
          failure={failure}
          onConfirm={() => commit(confirming)}
          onClose={() => {
            setConfirming(null);
            setFailure(null);
          }}
        />
      ) : null}
    </>
  );
}

function MachineRevealDialog({
  enable,
  busy,
  failure,
  onConfirm,
  onClose,
}: {
  enable: boolean;
  busy: boolean;
  failure: string | null;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const dialog = useModalDialog();
  const [acknowledged, setAcknowledged] = useState(false);
  return (
    <dialog
      ref={dialog}
      className="ceremony"
      aria-labelledby="machine-reveal-title"
      onClose={onClose}
      onCancel={(event) => {
        if (busy) {
          event.preventDefault();
        }
      }}
    >
      <h2 className="ceremony__title" id="machine-reveal-title">
        {enable ? 'Enable machine secret delivery' : 'Withdraw machine secret delivery'}
      </h2>
      {enable ? (
        <>
          <p>
            A machine principal holding <strong>reveal</strong> is a standing decryption capability:
            no second factor, no ceremony, every current fetch delivers plaintext while the grant
            stands. A CI runner holding it is that capability in the most-attacked box in the system.
          </p>
          <p>
            Enabling admits <strong>reveal</strong> grants onto workload and automation principals.
            It also admits <strong>reveal-history</strong> onto a workload only while that workload
            has an active non-current pin. Each grant still runs its own widening ceremony. Nothing
            is granted by this act alone.
          </p>
          <label className="ceremony__ack">
            <input
              type="checkbox"
              checked={acknowledged}
              onChange={(event) => setAcknowledged(event.target.checked)}
              disabled={busy}
            />{' '}
            I understand this admits standing decryption capabilities onto machine principals in
            this project.
          </label>
        </>
      ) : (
        <p>
          Withdrawing makes every machine <strong>reveal</strong> and{' '}
          <strong>reveal-history</strong> grant in this project inert on the next fetch and moves
          every machine cursor. Workloads keep receiving configuration and secret presence only.
          Grant rows stay where they are so the withdrawal is reversible by the same act.
        </p>
      )}
      {failure !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      ) : null}
      <div className="ceremony__actions">
        <button type="button" className="btn" onClick={onClose} disabled={busy}>
          Cancel
        </button>
        <button
          type="button"
          className="btn btn--primary"
          onClick={onConfirm}
          disabled={busy || (enable && !acknowledged)}
        >
          {enable ? 'Enable the opt-in' : 'Withdraw the opt-in'}
        </button>
      </div>
    </dialog>
  );
}

function ExpandableRow({
  account,
  scope,
  open,
  scopeKnown,
  machineReveal,
  lastUsed,
  onToggle,
  children,
}: {
  account: ServiceAccount;
  scope: readonly MachineEnvScope[];
  open: boolean;
  /** False when the grants or the environments could not be read: unknown is not "none". */
  scopeKnown: boolean;
  /** The project's machine-reveal opt-in, as the server reports it. */
  machineReveal: boolean;
  lastUsed: string;
  onToggle: () => void;
  children: ReactNode;
}) {
  const reading = scope.filter((s) => s.read);
  const journey = setupJourney(account.kind, scope, machineReveal);
  const outstanding = journey?.findIndex((step) => step.state !== 'done') ?? -1;
  return (
    <>
      <tr>
        <th scope="row">
          <button
            className="values__keyname mono"
            type="button"
            aria-expanded={open}
            onClick={onToggle}
          >
            {`${open ? '▾' : '▸'} ${account.name}`}
          </button>
        </th>
        <td>
          <span className="badge">{account.kind}</span>
        </td>
        <td>
          {!scopeKnown ? (
            <span className="machine__none">unknown</span>
          ) : reading.length === 0 ? (
            <span className="machine__none">no environment</span>
          ) : (
            <span className="machine__chips">
              {reading.map((s) => (
                <span className="machine__scope" key={s.id}>
                  <span className="badge">{s.reveal ? `${s.name} ◆` : s.name}</span>
                  {/* Origin chips per scope, as Members renders them: the one
                      thing that tells a break-glass grant from an ordinary one. */}
                  {s.origins.map((origin) => (
                    <span className="badge mono" key={`${origin.kind}:${origin.subject}`}>
                      {origin.kind}: {origin.subject}
                    </span>
                  ))}
                </span>
              ))}
            </span>
          )}
        </td>
        {/* The SERVER's live count: it applies revocation, the credential epoch
            and expiry, none of which a client filtering on `revoked_at` sees. */}
        <td className="col-secondary">{String(account.live_credentials)}</td>
        <td className="col-secondary">{lastUsed}</td>
        <td>
          <span className="badge">
            {journey === null
              ? 'not applicable'
              : !scopeKnown
                ? 'unknown'
                : outstanding === -1
                  ? 'complete'
                  : `step ${String(outstanding + 1)} of 5`}
          </span>
        </td>
      </tr>
      {open ? (
        <tr className="machine__sub">
          <td colSpan={6}>{children}</td>
        </tr>
      ) : null}
    </>
  );
}

function ExpansionBody({
  account,
  scope,
  machineReveal,
  bearers,
  bindings,
  now,
  ready,
  canDelete,
  onMint,
  onBind,
  onGrant,
  onRevoke,
  onReplaceBinding,
  onDelete,
}: {
  account: ServiceAccount;
  scope: readonly MachineEnvScope[];
  machineReveal: boolean;
  bearers: readonly MachineCredential[];
  bindings: readonly MachineCredential[];
  now: Date;
  /** Every query this surface's warnings are computed from has succeeded. */
  ready: boolean;
  /**
   * Delete's lighter gate: a session and a known listing. It is separate from
   * `ready` because deprovisioning is a narrowing that needs no scope read,
   * requiring one would keep an operator from deleting a compromised account
   * when the membership surface is unreadable.
   */
  canDelete: boolean;
  onMint: (rotating: boolean) => void;
  onBind: () => void;
  onGrant: () => void;
  onRevoke: (credential: MachineCredential) => void;
  onReplaceBinding: (credential: MachineCredential) => void;
  onDelete: () => void;
}) {
  const journey = setupJourney(account.kind, scope, machineReveal);
  return (
    <>
      <div className="machine__grid">
        <div>
          <h2 className="machine__subhead">Credentials</h2>
          {bearers.length === 0 ? (
            <p className="machine__none">
              No bearer credentials. This account authenticates by federation only, or not at all
              until one is minted.
            </p>
          ) : (
            <ul className="machine__creds">
              {bearers.map((credential) => (
                <li className="cred" key={credential.id}>
                  <code className="mono">{`${credential.prefix_hint ?? 'unknown'}…`}</code>
                  <span className="badge">bearer</span>
                  <ExpiryBadge credential={credential} now={now} />
                  <span className="cred__meta">{lastUsedLabel(credential)}</span>
                  <button
                    className="btn"
                    type="button"
                    disabled={!ready}
                    onClick={() => onMint(true)}
                  >
                    {`Rotate ${account.name}`}
                  </button>
                  <button className="btn" type="button" onClick={() => onRevoke(credential)}>
                    {`Revoke ${credential.prefix_hint ?? credential.id}`}
                  </button>
                </li>
              ))}
            </ul>
          )}

          {bindings.length > 0 ? (
            <>
              <h2 className="machine__subhead">Federated bindings</h2>
              <ul className="machine__bindings">
                {bindings.map((credential) => (
                  <li key={credential.id}>
                    <BindingCard
                      account={account}
                      credential={credential}
                      now={now}
                      ready={ready}
                      onReplace={onReplaceBinding}
                      onRevoke={onRevoke}
                    />
                  </li>
                ))}
              </ul>
            </>
          ) : null}
        </div>

        <div>
          <h2 className="machine__subhead">Delivery targets</h2>
          <p role="status" className="machine__none">
            No Kubernetes delivery targets are reported for this account in this view. Check its
            HikyoSecret conditions with kubectl.
          </p>
          {ready ? null : (
            <p className="machine__none" role="status">
              The actions are held back until the accounts, grants, environments and credentials
              have all been read: every one of their warnings is a statement about that state, and a
              query that failed would make it an understatement.
            </p>
          )}
          <div className="machine__actions">
            <button
              className="btn btn--primary"
              type="button"
              disabled={!ready}
              onClick={() => onMint(false)}
            >
              {`Mint credential for ${account.name}`}
            </button>
            <button className="btn" type="button" disabled={!ready} onClick={onBind}>
              {`Add federated binding to ${account.name}`}
            </button>
            <button className="btn" type="button" disabled={!ready} onClick={onGrant}>
              {`Add environment grant to ${account.name}`}
            </button>
            <button
              className="btn btn--danger"
              type="button"
              disabled={!canDelete}
              onClick={onDelete}
            >
              {`Delete ${account.name}`}
            </button>
          </div>
        </div>
      </div>

      <h2 className="machine__subhead">Setup journey</h2>
      {journey === null ? (
        <p className="machine__none">
          Automation principal: it runs off-box, on a schedule or in CI, and never delivers to a
          workload, so it has no setup journey. Its capability allowlist admits read, edit, publish
          and definitions-edit; never any manage- or instance capability.
        </p>
      ) : (
        <ol className="journey">
          {journey.map((step) => (
            <li className={`journey__step journey__step--${step.state}`} key={step.title}>
              <span className="journey__state">
                {step.state === 'done'
                  ? 'done'
                  : step.state === 'next'
                    ? 'next'
                    : 'blocked'}
              </span>
              <span className="journey__body">
                <span className="journey__title">{step.title}</span>
                <span className="journey__note">{step.note}</span>
                {/* The step's act lives on the step: the same dialog or control
                    the buttons above open, reached from where the gap is named. */}
                {step.action === undefined ? null : (
                  <JourneyActionButton action={step.action} ready={ready} onGrant={onGrant} />
                )}
              </span>
            </li>
          ))}
        </ol>
      )}
    </>
  );
}

function journeyActionLabel(action: JourneyAction): string {
  switch (action) {
    case 'grant-read':
      return 'Grant read…';
    case 'grant-reveal':
      return 'Grant reveal…';
    case 'enable-opt-in':
      return 'Go to the opt-in…';
  }
}

/**
 * The opt-in control sits in the PolicyStrip at the top of this tab, which is
 * the only tab the journey renders on, so focusing it is a scroll and a focus,
 * not a navigation. A grant step opens the same grant dialog the actions do.
 */
function JourneyActionButton({
  action,
  ready,
  onGrant,
}: {
  action: JourneyAction;
  ready: boolean;
  onGrant: () => void;
}) {
  return (
    <button
      className="btn journey__action"
      type="button"
      disabled={action !== 'enable-opt-in' && !ready}
      onClick={() => {
        if (action !== 'enable-opt-in') {
          onGrant();
          return;
        }
        const toggle = document.getElementById('machine-policy-toggle');
        toggle?.scrollIntoView({ block: 'center' });
        toggle?.focus();
      }}
    >
      {journeyActionLabel(action)}
    </button>
  );
}

function BindingCard({
  account,
  credential,
  now,
  ready,
  onReplace,
  onRevoke,
}: {
  account: ServiceAccount;
  credential: MachineCredential;
  now: Date;
  /**
   * Every query the replacement's warning is computed from has succeeded.
   * Replace mints a new binding, so it carries the same post-state reach the
   * first mint did; revoke is a narrowing and needs no such read, so it is not
   * gated on it.
   */
  ready: boolean;
  onReplace: (credential: MachineCredential) => void;
  onRevoke: (credential: MachineCredential) => void;
}) {
  return (
    <div className="bindrow">
      <p className="bindrow__head">
        <code className="mono">{account.name}</code>
        <span className="badge">federated</span>
        <ExpiryBadge credential={credential} now={now} />
        <span className="cred__meta">
          matched byte-for-byte, no wildcards, no case folding; renewal is a mint
        </span>
      </p>
      {/* Every pair is wrapped: a `dl` takes either dt/dd children or `div`
          children, never a mixture, and axe checks exactly that. */}
      <dl className="kv">
        {[
          { term: 'issuer', value: credential.issuer ?? 'unknown' },
          { term: 'subject', value: credential.subject ?? 'unknown' },
          { term: 'audience', value: credential.audience ?? 'unknown' },
          ...(credential.required_claims ?? []).map((pin) => ({
            term: pin.claim,
            value: claimText(pin),
          })),
        ].map((pair) => (
          <div className="kv__pair" key={pair.term}>
            <dt>{pair.term}</dt>
            <dd className="mono">{pair.value}</dd>
          </div>
        ))}
      </dl>
      {credential.reactivated_at === undefined ? null : (
        <p className="notice" role="status">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            {`Quarantined since the restore on ${isoDay(credential.reactivated_at)}: this binding permanently refuses any token issued at or before that instant plus the accepted clock skew.`}
          </span>
        </p>
      )}
      <div className="machine__actions">
        <button
          className="btn"
          type="button"
          disabled={!ready}
          onClick={() => onReplace(credential)}
        >
          {`Replace binding on ${account.name}`}
        </button>
        <button className="btn" type="button" onClick={() => onRevoke(credential)}>
          {`Revoke binding on ${account.name}`}
        </button>
      </div>
    </div>
  );
}

/** The expiry in words, with the tier echoed by colour beside them, never instead. */
function ExpiryBadge({ credential, now }: { credential: MachineCredential; now: Date }) {
  const expiry = expiryLabel(credential, now);
  return (
    <span className={`badge${expiry.tier === 'none' ? '' : ` badge--${expiry.tier}`}`}>
      {expiry.text}
    </span>
  );
}

function claimText(pin: ClaimPin): string {
  if (pin.string_value !== undefined) {
    return pin.string_value;
  }
  if (pin.number_value !== undefined) {
    return String(pin.number_value);
  }
  if (pin.bool_value !== undefined) {
    return String(pin.bool_value);
  }
  return 'unpinned';
}

/**
 * presetForBinding recovers the platform of a binding being replaced. The
 * credential row carries no platform type, that lives on the issuer, not the
 * binding, so the platform is inferred from the claims it pinned: the preset
 * whose required claims the predecessor pins the most of. It is only ever used
 * to choose which fields the replace form renders; the claim VALUES are seeded
 * straight from the predecessor, so a misdetection would show a spare field,
 * never bind the wrong identity.
 */
export function presetForBinding(credential: MachineCredential): FederationPreset {
  const pinned = new Set((credential.required_claims ?? []).map((pin) => pin.claim));
  let best = KUBERNETES_PRESET;
  let bestScore = -1;
  for (const candidate of FEDERATION_PRESETS) {
    const score = candidate.claims.filter((field) => pinned.has(field.claim)).length;
    if (score > bestScore) {
      best = candidate;
      bestScore = score;
    }
  }
  return best;
}

/**
 * seedClaims fills the replace form's claim inputs from the predecessor. The
 * read-shape `number_value` is a bigint (an int64 repository id does not
 * survive a float), and `claimText` stringifies it losslessly, so the seeded
 * text round-trips back through the numeric parse on submit.
 */
export function seedClaims(
  preset: FederationPreset,
  credential: MachineCredential,
): Record<string, string> {
  const byClaim = new Map((credential.required_claims ?? []).map((pin) => [pin.claim, pin] as const));
  const seeded: Record<string, string> = {};
  for (const field of preset.claims) {
    const pin = byClaim.get(field.claim);
    seeded[field.claim] = pin === undefined ? '' : claimText(pin);
  }
  return seeded;
}

/**
 * carriedClaims are the predecessor's pins that no preset field renders, a
 * custom claim a CLI operator added beyond the platform's required set. A
 * replacement must carry EVERY one of them verbatim: dropping a pin the form
 * cannot show would silently weaken the successor's identity constraints, which
 * is precisely the re-point-without-review the immutable-binding rule forbids.
 * The form displays them read-only so the preservation is visible, not silent.
 */
export function carriedClaims(
  preset: FederationPreset,
  credential: MachineCredential,
): FederatedClaimPin[] {
  const rendered = new Set(preset.claims.map((field) => field.claim));
  return (credential.required_claims ?? [])
    .filter((pin) => !rendered.has(pin.claim))
    .map(toRequestPin);
}

/**
 * toRequestPin converts one READ-shape pin (whose `number_value` is a bigint)
 * to the REQUEST shape (a plain number). The int64→number narrowing is the
 * generated client's own boundary, the wire type is a number, so it is no
 * lossier here than a first mint of the same claim, and a real repository id
 * sits far below the safe-integer ceiling.
 */
function toRequestPin(pin: ClaimPin): FederatedClaimPin {
  if (pin.string_value !== undefined) {
    return { claim: pin.claim, string_value: pin.string_value };
  }
  if (pin.number_value !== undefined) {
    return { claim: pin.claim, number_value: Number(pin.number_value) };
  }
  if (pin.bool_value !== undefined) {
    return { claim: pin.claim, bool_value: pin.bool_value };
  }
  return { claim: pin.claim };
}

/** requestPinText renders a request-shape pin for the read-only preserved list. */
function requestPinText(pin: FederatedClaimPin): string {
  if (pin.string_value !== undefined) {
    return pin.string_value;
  }
  if (pin.number_value !== undefined) {
    return String(pin.number_value);
  }
  if (pin.bool_value !== undefined) {
    return String(pin.bool_value);
  }
  return 'unpinned';
}

/**
 * useNavigationGuard keeps navigation from destroying what dismissal is not
 * allowed to.
 *
 * `dismissDecision` gates Escape and the buttons, but the browser has two more
 * ways to unmount this dialog: unload (reload, tab close, external navigation)
 * and the Back button, which pops the route out from under the component. The
 * first gets the platform's `beforeunload` confirmation; the second gets a
 * history sentinel, a duplicate entry pushed while the guard is active, so a
 * Back press consumes the sentinel instead of the route, the URL never changes,
 * and the press is surfaced as a dismissal ATTEMPT routed through the same
 * gate as Escape. Deactivating consumes the sentinel again so Back is not a
 * double-press afterwards.
 */
export function useNavigationGuard(active: boolean, onAttempt: () => void) {
  const attempt = useRef(onAttempt);
  attempt.current = onAttempt;
  useEffect(() => {
    if (!active) {
      return;
    }
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    const onPopState = () => {
      history.pushState(null, '', window.location.href);
      attempt.current();
    };
    history.pushState(null, '', window.location.href);
    window.addEventListener('beforeunload', onBeforeUnload);
    window.addEventListener('popstate', onPopState);
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload);
      window.removeEventListener('popstate', onPopState);
      history.back();
    };
  }, [active]);
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
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="mint-title"
      ref={dialog}
      onCancel={(event) => {
        // Escape must not be a way to lose a value nothing can return.
        event.preventDefault();
        dismiss();
      }}
    >
      {disclosed === null ? (
        <>
          <h2 className="ceremony__title" id="mint-title">
            {`${request.rotating ? 'Rotate' : 'Mint'} credential · ${request.accountName}`}
          </h2>
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
            <p className="ceremony__lede">
              The prior value is never returned. The predecessor keeps authenticating until you
              revoke it; rotation and revocation are separate, deliberate acts, so a mint that
              lands and a revoke that does not leaves two live credentials rather than none.
            </p>
          ) : null}
          {failure !== null ? (
            <p className="alert" role="alert">
              <span className="alert__glyph" aria-hidden="true">
                !
              </span>
              <span>{failure}</span>
            </p>
          ) : null}
          <div className="ceremony__actions">
            <button
              className="btn btn--primary"
              type="button"
              disabled={busy}
              onClick={() => void run()}
            >
              {busy
                ? 'Minting…'
                : request.reach.length === 0
                  ? 'Mint credential'
                  : 'Use a passkey and mint'}
            </button>
            <button className="btn" type="button" onClick={dismiss} disabled={busy}>
              Cancel
            </button>
          </div>
        </>
      ) : (
        <>
          <h2 className="ceremony__title" id="mint-title">
            Credential minted, shown exactly once
          </h2>
          <p className="mono machine__token">{disclosed.result.value}</p>
          <p className="cred__meta">
            {disclosed.result.expires_at === null
              ? 'Expiry: indefinite.'
              : disclosed.result.expires_at === undefined
                ? 'Expiry: unknown.'
                : <>Expires <time dateTime={disclosed.result.expires_at}>{new Date(disclosed.result.expires_at).toLocaleString()}</time></>}
          </p>
          {disclosed.result.clamped ? (
            <p className="notice" role="status">
              <span className="alert__glyph" aria-hidden="true">
                !
              </span>
              <span>
                The instance lifetime ceiling shortened this credential. It expires earlier than the
                default asked for, said now rather than discovered when it dies.
              </span>
            </p>
          ) : null}
          <p className="ceremony__cap" role="status">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              This value is never retrievable again. The list shows metadata only and rotation never
              returns it. Store it in the consuming system now; if it is lost, revoke this
              credential and mint a fresh one.
            </span>
          </p>
          <button
            className="btn"
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
          </button>
          {disclosed.copyStatus === null ? null : (
            <p className="notice" role="status">
              <span className="alert__glyph" aria-hidden="true">
                ⧉
              </span>
              <span>{disclosed.copyStatus}</span>
            </p>
          )}
          <div className="field chk">
            <input
              id="mint-stored"
              type="checkbox"
              ref={confirmation}
              checked={disclosed.stored}
              onChange={(event) => {
                move({ type: 'confirm-stored', stored: event.target.checked });
              }}
            />
            <label htmlFor="mint-stored">I have stored this credential in its target system.</label>
          </div>
          {disclosed.heldBack ? (
            <p className="alert" role="alert">
              <span className="alert__glyph" aria-hidden="true">
                !
              </span>
              <span>Confirm you have stored it: there is no second look at this value.</span>
            </p>
          ) : null}
          <div className="ceremony__actions">
            <button className="btn btn--primary" type="button" onClick={dismiss}>
              Done
            </button>
          </div>
        </>
      )}
    </dialog>
  );
}

/**
 * BindingDialog is the federation form.
 *
 * Two of its rules come straight off the server and are asked for HERE so an
 * operator meets them as a form rather than as a 400: the audience is mandatory
 * and may not be the issuer's default, and each platform's immutable
 * identifiers must be pinned. The third, the pull-request refusal, is the
 * load-bearing one: the protection comes from the pinned `event_name`, never
 * from the subject's shape, because a `pull_request_target` token carries the
 * ordinary ref-form subject a production binding names.
 */
function BindingDialog({
  project,
  accounts,
  initial,
  replaces,
  reachFor,
  onClose,
  onCreated,
}: {
  project: ProjectRef;
  accounts: readonly ServiceAccount[];
  initial: ServiceAccount;
  /**
   * The binding this mint supersedes, when the operator chose Replace. Bindings
   * are IMMUTABLE, so a replacement is a fresh mint naming `replaces`: the
   * server revokes the predecessor and inserts the successor in ONE
   * transaction, so there is never a gap with no binding nor an overlap with
   * two. The form pre-seeds from it and locks the target account, the
   * predecessor belongs to exactly one, and hides the platform picker, since
   * the platform cannot move under a replacement.
   */
  replaces?: MachineCredential;
  /**
   * The selected account's post-state reach. A binding is a mint (#62), so the
   * server demands the same disclosure formula the credential mint does: one
   * fresh window per environment the account can decrypt in the resulting
   * state. Vacuous today for the same reason the mint's is, nothing a machine
   * can hold reaches plaintext, but the leg exists so the form does not start
   * failing with a bare 403 the day the reveal opt-in lands.
   */
  reachFor: (accountId: string) => readonly MachineEnvScope[];
  onClose: () => void;
  onCreated: (message: string) => void;
}) {
  const dialog = useModalDialog();
  const create = useCreateBinding(project);
  const refresh = useRefreshAccount(project);
  const replacing = replaces !== undefined;
  // A replacement's platform is the predecessor's, detected from the claims it
  // pinned; a fresh binding starts on Kubernetes. The account is locked to the
  // row the replace was launched from, because a binding belongs to one.
  const seedPreset = replaces === undefined ? KUBERNETES_PRESET : presetForBinding(replaces);
  // Predecessor pins no form field renders, carried verbatim so a replacement
  // never silently drops an identity constraint the form could not show.
  const carried = replaces === undefined ? [] : carriedClaims(seedPreset, replaces);
  const [account, setAccount] = useState(initial.id);
  const [preset, setPreset] = useState<FederationPreset>(seedPreset);
  const [issuer, setIssuer] = useState(replaces?.issuer ?? seedPreset.issuer);
  const [subject, setSubject] = useState(replaces?.subject ?? seedPreset.subject);
  const [audience, setAudience] = useState(replaces?.audience ?? '');
  const [claims, setClaims] = useState<Record<string, string>>(() =>
    replaces === undefined ? blankClaims(KUBERNETES_PRESET) : seedClaims(seedPreset, replaces),
  );
  const [lifetime, setLifetime] = useState('default');
  const [deliberate, setDeliberate] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // A refusal describes the form as it was when it was refused. Editing any
  // field makes it stale, and a stale refusal sitting beside a fresh one is two
  // alerts saying different things about one form.
  const edited = () => setFailure(null);

  const choose = (next: FederationPreset) => {
    setPreset(next);
    setIssuer(next.issuer);
    setSubject(next.subject);
    setClaims(blankClaims(next));
    setDeliberate(false);
    edited();
  };

  const eventName = claims['event_name'] ?? '';
  const refusal = pullRequestRefusal(eventName);

  /**
   * pinsOrRefusal builds the pinned claims, or names the first field that
   * cannot be one.
   *
   * The numeric fields are the ones worth refusing over: an immutable
   * repository id is what stops a renamed-and-reused path inheriting this
   * binding, and a field that quietly became 0, or rounded to a neighbouring
   * id past 2^53, would bind this service account to somebody else's
   * repository while looking like it worked.
   */
  const pinsOrRefusal = (): FederatedClaimPin[] | string => {
    const pins: FederatedClaimPin[] = [];
    for (const field of preset.claims) {
      const raw = claims[field.claim] ?? '';
      if (field.kind === 'number') {
        const value = parseClaimNumber(raw);
        if (value === null) {
          return `${field.label} (${field.claim}) must be a whole number the issuer actually mints: digits only, and inside the range this contract can carry exactly. Nothing was bound.`;
        }
        pins.push({ claim: field.claim, number_value: value });
        continue;
      }
      if (raw.trim() === '') {
        return `${field.label} (${field.claim}) is required: this issuer's bindings must pin it, and the server refuses a binding without it. Nothing was bound.`;
      }
      pins.push({ claim: field.claim, string_value: raw });
    }
    return pins;
  };

  const submit = async () => {
    if (refusal !== null && !deliberate) {
      setFailure(
        'This binding pins a pull-request event. Acknowledge deliberately below, or pin another event.',
      );
      return;
    }
    // Mandatory, and refused HERE rather than as a 400: a token minted for
    // another consumer must not authenticate here, and an empty field is the
    // most likely way to end up with no such constraint at all.
    if (audience.trim() === '') {
      setFailure(
        'An audience is mandatory, and it may never be the issuer’s default. A token minted for another consumer must not authenticate here. Nothing was bound.',
      );
      return;
    }
    if (issuer.trim() === '') {
      setFailure(
        'An issuer is mandatory because federated bindings match it byte-for-byte. Nothing was bound.',
      );
      return;
    }
    if (subject.trim() === '') {
      setFailure(
        'A subject is mandatory because federated bindings match it byte-for-byte. Nothing was bound.',
      );
      return;
    }
    const pins = pinsOrRefusal();
    if (typeof pins === 'string') {
      setFailure(pins);
      return;
    }
    // Carried pins are appended, never merged: a preset field and a carried
    // claim can never share a name (carried is exactly the complement), so
    // there is nothing to reconcile.
    const allPins = [...pins, ...carried];
    setBusy(true);
    setFailure(null);
    // Same issued-vs-nothing-happened line the mint draws: once the request
    // leaves, a failure says nothing about whether a live external login path
    // now exists, and the row list is the only place it would show.
    let issued = false;
    try {
      // A binding is a mint: one reauthentication per environment the account
      // decrypts in the post-state, in the same purpose the server consumes.
      // Empty today, no machine reaches plaintext, so no ceremony runs.
      for (const environment of reachFor(account)) {
        await runPasskeyCeremony({
          operation: 'mint',
          environmentId: environment.id,
          keyIds: [],
        });
      }
      const seconds = BINDING_LIFETIMES.find((entry) => entry.id === lifetime)?.seconds;
      issued = true;
      const result = await create.mutateAsync({
        serviceAccount: account,
        issuer,
        subject,
        audience,
        requiredClaims: allPins,
        ...(seconds === undefined ? {} : { lifetimeSeconds: seconds }),
        ...(replaces === undefined ? {} : { replaces: replaces.id }),
      });
      const clampNote = result.clamped
        ? ' The instance ceiling shortened the requested lifetime.'
        : '';
      onCreated(
        replacing
          ? `Replaced. The predecessor was revoked and the successor inserted in one transaction: no gap, no overlap. The new binding matches ${subject} byte-for-byte.${clampNote}`
          : `Bound. The binding matches ${subject} byte-for-byte and nothing else.${clampNote}`,
      );
    } catch (error) {
      if (issued) {
        refresh(account);
        setFailure(bindingFailureText(error));
      } else {
        setFailure(identityRefusalText(error));
      }
    } finally {
      setBusy(false);
    }
  };

  // An in-flight binding is not dismissible: Escape, Back or unload here would
  // hide a mutation that may commit, the operator stays until it resolves.
  useNavigationGuard(busy, () => {});

  return (
    <dialog className="ceremony" aria-labelledby="binding-title" ref={dialog} onCancel={(e) => {
      e.preventDefault();
      if (!busy) {
        onClose();
      }
    }}>
      <h2 className="ceremony__title" id="binding-title">
        {replacing ? 'Replace federated binding' : 'Add federated binding'}
      </h2>
      <p className="ceremony__lede">
        {replacing
          ? 'Bindings are immutable, so this replaces the predecessor: the server revokes it and inserts this successor in one transaction, with no gap with no binding and no overlap with two. The fields are seeded from the predecessor; change what the replacement should carry.'
          : 'A byte-exact (issuer, subject) pair naming exactly one service account. The audience is mandatory and may not be the issuer’s default: a token minted for another consumer must not authenticate here.'}
      </p>

      {/* One native latch for the whole target: an issued request is for the
          form as submitted, so nothing here may change until it resolves,
          otherwise the success or failure sentence describes one account while
          the operator is looking at another. */}
      <fieldset className="machine__lock" disabled={busy}>
      {replacing ? null : (
        <div className="machine__presets">
          {FEDERATION_PRESETS.map((entry) => (
            <button
              key={entry.id}
              className="btn"
              type="button"
              aria-pressed={preset.id === entry.id}
              onClick={() => choose(entry)}
            >
              {preset.id === entry.id ? <span aria-hidden="true">✓ </span> : null}
              {entry.label}
            </button>
          ))}
        </div>
      )}

      <div className="field">
        <label htmlFor="binding-account">Service account</label>
        <select
          id="binding-account"
          value={account}
          // A replacement belongs to exactly one account, the predecessor's,
          // so the target is fixed and the selector is locked.
          disabled={replacing}
          onChange={(event) => {
            // The acknowledgement is consent for ONE account's fetch authority.
            // A different target is a different decision, so it does not carry.
            setAccount(event.target.value);
            setDeliberate(false);
            edited();
          }}
        >
          {accounts.map((sa) => (
            <option key={sa.id} value={sa.id}>
              {sa.name}
            </option>
          ))}
        </select>
      </div>

      <div className="field">
        <label htmlFor="binding-issuer">Issuer</label>
        <input
          id="binding-issuer"
          className="mono"
          value={issuer}
          onChange={(event) => {
            setIssuer(event.target.value);
            edited();
          }}
        />
      </div>

      <div className="field">
        <label htmlFor="binding-subject">Subject, matched byte-for-byte</label>
        <input
          id="binding-subject"
          className="mono"
          value={subject}
          onChange={(event) => {
            setSubject(event.target.value);
            edited();
          }}
        />
      </div>

      <div className="field">
        <label htmlFor="binding-audience">Audience</label>
        <input
          id="binding-audience"
          className="mono"
          value={audience}
          onChange={(event) => {
            setAudience(event.target.value);
            edited();
          }}
        />
      </div>

      {preset.claims.map((field) =>
        field.kind === 'event' ? (
          <div className="field" key={field.claim}>
            <label htmlFor="binding-event">{field.label}</label>
            <select
              id="binding-event"
              value={eventName}
              onChange={(event) => {
                setClaims((current) => ({ ...current, event_name: event.target.value }));
                setDeliberate(false);
                edited();
              }}
            >
              {CI_EVENTS.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          </div>
        ) : (
          <div className="field" key={field.claim}>
            <label htmlFor={presetFieldId(field.claim)}>{`${field.label} (${field.claim})`}</label>
            <input
              id={presetFieldId(field.claim)}
              className="mono"
              inputMode={field.kind === 'number' ? 'numeric' : 'text'}
              value={claims[field.claim] ?? ''}
              onChange={(event) => {
                setClaims((current) => ({ ...current, [field.claim]: event.target.value }));
                edited();
              }}
            />
          </div>
        ),
      )}

      {carried.length > 0 ? (
        <div className="field">
          <span className="field__label">
            Preserved pins, carried byte-for-byte from the binding being replaced
          </span>
          <dl className="kv">
            {carried.map((pin) => (
              <div className="kv__pair" key={pin.claim}>
                <dt>{pin.claim}</dt>
                <dd className="mono">{requestPinText(pin)}</dd>
              </div>
            ))}
          </dl>
        </div>
      ) : null}

      <div className="field">
        <label htmlFor="binding-lifetime">Binding lifetime</label>
        <select
          id="binding-lifetime"
          value={lifetime}
          onChange={(event) => {
            setLifetime(event.target.value);
            edited();
          }}
        >
          {BINDING_LIFETIMES.map((entry) => (
            <option key={entry.id} value={entry.id} disabled={entry.disabled === true}>
              {entry.label}
            </option>
          ))}
        </select>
      </div>

      {refusal === null ? null : (
        <>
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>{refusal}</span>
          </p>
          <div className="field chk">
            <input
              id="binding-deliberate"
              type="checkbox"
              checked={deliberate}
              onChange={(event) => setDeliberate(event.target.checked)}
            />
            <label htmlFor="binding-deliberate">
              I am deliberately binding a pull-request identity and accept that pull-request authors
              reach this account&apos;s scope.
            </label>
          </div>
        </>
      )}
      </fieldset>

      {failure !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      ) : null}

      <p className="machine__footnote">
        No wildcards, no namespace patterns, no case folding; canonicalisation merges distinct
        external identities. Bindings are immutable and expire on the same terms as a bearer
        credential: renewal is a mint, never an edit.
      </p>

      <div className="ceremony__actions">
        <button className="btn btn--primary" type="button" disabled={busy} onClick={() => void submit()}>
          {busy
            ? replacing
              ? 'Replacing…'
              : 'Binding…'
            : replacing
              ? 'Replace this binding'
              : 'Bind this identity'}
        </button>
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}

/**
 * GrantDialog is the grant-mutation warning.
 *
 * A grant attaches to the SERVICE ACCOUNT, never to a credential, so it
 * re-scopes every credential already in circulation the moment it lands. The
 * warning therefore names two numbers the operator cannot see anywhere else:
 * how many live credentials that is, and exactly which keys become reachable.
 *
 * Only `read` is offered in this setup journey. Conditional disclosure grants
 * are separate operator acts: `reveal` needs the live project opt-in, while
 * `reveal-history` additionally needs an active non-current workload pin.
 */
function GrantDialog({
  project,
  account,
  scope,
  machineReveal,
  liveCredentials,
  onClose,
  onGranted,
}: {
  project: ProjectRef;
  account: ServiceAccount;
  scope: readonly MachineEnvScope[];
  /** The project's machine-reveal opt-in: reveal is grantable only while it is on. */
  machineReveal: boolean;
  liveCredentials: number;
  onClose: () => void;
  onGranted: (environment: string, result: GrantResult) => void;
}) {
  const dialog = useModalDialog();
  const grantable =
    grantableFor(scope, 'read', machineReveal).length > 0 ||
    grantableFor(scope, 'reveal', machineReveal).length > 0;
  // The in-flight latch lives here because the <dialog>'s cancel event does,
  // while the mutation lives in GrantBody, a ref, because the gate needs the
  // truth at event time, not a render.
  const inFlight = useRef(false);

  return (
    <dialog className="ceremony" aria-labelledby="grant-title" ref={dialog} onCancel={(e) => {
      e.preventDefault();
      if (!inFlight.current) {
        onClose();
      }
    }}>
      <h2 className="ceremony__title" id="grant-title">
        {`Add environment grant · ${account.name}`}
      </h2>
      <p className="ceremony__lede">
        Grants attach to the service account, never to a credential.
      </p>

      {!grantable ? (
        <p role="status">
          {machineReveal
            ? 'This account already reads and reveals every environment in the project. There is nothing to widen.'
            : 'This account already reads every environment in the project. There is nothing to widen; reveal needs the machine-reveal opt-in first.'}
        </p>
      ) : (
        <GrantBody
          project={project}
          account={account}
          scope={scope}
          machineReveal={machineReveal}
          liveCredentials={liveCredentials}
          inFlight={inFlight}
          onClose={onClose}
          onGranted={onGranted}
        />
      )}
    </dialog>
  );
}

/**
 * GrantBody exists so its queries and selection state are never mounted when
 * there is nothing grantable. Hooks cannot be conditional, and a query fired
 * for a dialog that only says "nothing to widen" would be a request the
 * surface makes and then ignores.
 */
function GrantBody({
  project,
  account,
  scope,
  machineReveal,
  liveCredentials,
  inFlight,
  onClose,
  onGranted,
}: {
  project: ProjectRef;
  account: ServiceAccount;
  scope: readonly MachineEnvScope[];
  machineReveal: boolean;
  liveCredentials: number;
  /** GrantDialog's Escape gate, held while the mutation is in flight. */
  inFlight: MutableRefObject<boolean>;
  onClose: () => void;
  onGranted: (environment: string, result: GrantResult) => void;
}) {
  const grant = useGrantEnvironment(project);
  const refreshGrants = useRefreshGrants(project);
  // Reveal is offered only while the project opt-in is on: the UI grants read
  // by default, and the select widens to reveal under the same ceremony.
  const [capability, setCapability] = useState<'read' | 'reveal'>(
    grantableFor(scope, 'read', machineReveal).length > 0 ? 'read' : 'reveal',
  );
  const grantable = grantableFor(scope, capability, machineReveal);
  const [environment, setEnvironment] = useState(grantable[0]?.id ?? '');
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // The inputs can move under an open dialog (the opt-in withdrawn, the
  // account's scope refreshed). Fold the selection back onto what is still
  // grantable so no stale choice survives to the submit.
  useEffect(() => {
    if (capability === 'reveal' && !machineReveal) {
      setCapability('read');
      return;
    }
    if (environment !== '' && !grantable.some((s) => s.id === environment)) {
      setEnvironment(grantable[0]?.id ?? '');
    }
  }, [capability, environment, grantable, machineReveal]);

  // An in-flight grant is not dismissible by Back or unload either: a widening
  // that commits behind a dismissed dialog is invisible at the moment it most
  // needs review.
  useNavigationGuard(busy, () => {});

  /**
   * What a `read` grant actually delivers, read off the delivery surface rather
   * than guessed: the whole key CATALOGUE, every key's name and its
   * classification, and no value of any classification, config included. So
   * the newly reachable set is every key, not only the secrets. The catalogue
   * endpoint is used rather than the value listing on purpose: a value listing
   * is authorized for the HUMAN reading it and carries config plaintext this
   * dialog never renders, and a fetch is a cached copy (see useKeyCatalogue).
   */
  const values = useKeyCatalogue(project);
  // A read grant reaches the whole catalogue by name; a reveal grant adds
  // standing decryption of the SECRETS only (read is already held, so config
  // values are not new). Presence per environment is not in this read, so the
  // sentence says "when set" rather than claiming plaintext for every row.
  const catalogue = values.data?.items ?? [];
  const reachable =
    capability === 'reveal' ? catalogue.filter((key) => key.classification === 'secret') : catalogue;
  const chosen = grantable.find((s) => s.id === environment);
  // The opt-in can be withdrawn while this dialog is open: the capability
  // select disappears, but a stale reveal choice or a stale environment must
  // not stay submittable.
  const submittable = grantSubmittable(scope, environment, capability, machineReveal);
  // The mint formula's conjunct for a WIDENING is the delta, not the whole
  // post-state, which is what the server computes in checkMachineWidening.
  const widening = grantWideningReach(scope, environment, capability);

  const submit = async () => {
    setBusy(true);
    inFlight.current = true;
    setFailure(null);
    // Issued-vs-nothing-happened, the mint's line: once the request leaves, a
    // failure does not mean the widening did not land, and a widening that
    // landed re-scoped every live credential the moment it did.
    let issued = false;
    try {
      // Each newly reachable environment takes its own reauthentication, in the
      // same purpose the server consumes. Empty today, and for the same reason
      // the mint's is: nothing this account can hold reaches plaintext.
      for (const widened of widening) {
        await runPasskeyCeremony({
          operation: 'mint',
          environmentId: widened.id,
          keyIds: [],
        });
      }
      issued = true;
      const result = await grant.mutateAsync({
        environment,
        principal: account.principal_id,
        capability,
      });
      onGranted(chosen?.name ?? environment, result);
    } catch (error) {
      if (issued) {
        refreshGrants();
        setFailure(grantFailureText(error));
      } else {
        setFailure(identityRefusalText(error));
      }
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };

  const chooseCapability = (next: 'read' | 'reveal') => {
    setCapability(next);
    setEnvironment(grantableFor(scope, next, machineReveal)[0]?.id ?? '');
  };

  return (
    <>
      {machineReveal ? (
        <div className="field">
          <label htmlFor="grant-capability">Capability</label>
          <select
            id="grant-capability"
            value={capability}
            onChange={(event) =>
              chooseCapability(event.target.value === 'reveal' ? 'reveal' : 'read')
            }
          >
            <option value="read">read (configuration and secret presence)</option>
            <option value="reveal">reveal (standing secret plaintext)</option>
          </select>
        </div>
      ) : null}
      <div className="field">
        <label htmlFor="grant-environment">{`Environment (${capability})`}</label>
        <select
          id="grant-environment"
          value={environment}
          onChange={(event) => setEnvironment(event.target.value)}
        >
          {grantable.length === 0 ? <option value="">Nothing left to widen</option> : null}
          {grantable.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </div>

      <p className="ceremony__cap" role="status">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          {`This grant re-scopes every credential already in circulation. ${account.name} has ${String(liveCredentials)} live credential${liveCredentials === 1 ? '' : 's'}, and each one gains ${
            capability === 'read'
              ? 'read (configuration and secret presence)'
              : 'reveal (standing secret plaintext decryption)'
          } on ${chosen?.name ?? 'that environment'} the moment this lands.`}
        </span>
      </p>

      <p className="ceremony__stepup">
        <span className="alert__glyph" aria-hidden="true">
          ⚿
        </span>
        <span>
          <strong>The formula.</strong> manage-identities on this project, manage-members over the
          environment, and a disclosure capability over every environment this grant NEWLY lets the
          account decrypt, the delta, not the whole post-state, because that is what the grant
          adds.{' '}
          {widening.length === 0
            ? 'This grant newly decrypts nothing, so the disclosure conjunct is vacuous and no reauthentication is required.'
            : `It newly decrypts ${widening.map((w) => w.name).join(', ')}, so each takes its own passkey reauthentication before the grant lands.`}
        </span>
      </p>

      {/* FAIL CLOSED. A pending or failed catalogue read cannot be rendered as
          "nothing becomes reachable": that is the one answer it does not have,
          and it is the answer that makes the grant look harmless. */}
      {values.isSuccess ? (
        <>
          <p className="ceremony__scope">
            {reachable.length === 0
              ? capability === 'reveal' && catalogue.length > 0
                ? 'This project declares no secrets today, so the grant decrypts nothing yet, and every secret declared later.'
                : 'This project declares no keys, so the grant reaches an empty catalogue today, and every key declared later.'
              : capability === 'read'
                ? 'Newly reachable: every key below, by name and classification. A read grant delivers configuration values and secret presence; plaintext needs reveal.'
                : 'Newly decryptable: every secret below, as standing authority over its value wherever it is set. Configuration keys are not listed: read already reaches them.'}
          </p>
          {reachable.length === 0 ? null : (
            <ul className="ceremony__keys" aria-label="Keys this grant makes reachable">
              {reachable.map((key) => (
                <li className="mono" key={key.id}>
                  {`${key.name} · ${key.classification}`}
                </li>
              ))}
            </ul>
          )}
        </>
      ) : (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            {values.isError
              ? 'The key catalogue could not be read, so what this grant makes reachable cannot be named, and a grant whose blast radius is unknown is not one to make from here.'
              : 'Reading what this grant would make reachable…'}
          </span>
        </p>
      )}

      {failure !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      ) : null}

      <div className="ceremony__actions">
        <button
          className="btn btn--primary"
          type="button"
          disabled={busy || !submittable || !values.isSuccess}
          onClick={() => void submit()}
        >
          {busy ? 'Granting…' : `Grant ${capability}`}
        </button>
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </>
  );
}

/**
 * CreateAccountDialog seeds a project's machine inventory from the browser.
 *
 * The body is exactly the locked create contract, `{ name, kind }`. There is
 * no description field because the contract has none, and `kind` is a form field
 * rather than an edit because it is immutable at creation. The name is refused
 * HERE (empty or over 64) rather than as a 400, and the trimmed name is what is
 * sent so the length checked is the length the server sees.
 */
function CreateAccountDialog({
  project,
  onClose,
  onCreated,
}: {
  project: ProjectRef;
  onClose: () => void;
  onCreated: (name: string, kind: ServiceAccount['kind']) => void;
}) {
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="create-account-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
    >
      <h2 className="ceremony__title" id="create-account-title">
        Create service account
      </h2>
      <p className="ceremony__lede">
        A machine principal this project owns. Its kind is fixed at creation: a workload delivers to
        a running process, an automation runs off-box on a schedule or in CI. A fresh account holds
        no grants and reaches nothing until one is added.
      </p>

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
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      ) : null}

      <div className="ceremony__actions">
        <button
          className="btn btn--primary"
          type="button"
          disabled={busy}
          onClick={() => void submit()}
        >
          {busy ? 'Creating…' : 'Create service account'}
        </button>
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
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
function DeleteAccountDialog({
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
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="delete-account-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
    >
      <h2 className="ceremony__title" id="delete-account-title">
        {`Delete service account · ${account.name}`}
      </h2>
      <p className="ceremony__cap" role="status">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          {`This deletes ${account.name} and everything attached to it in one act: ${String(live)} live credential${live === 1 ? '' : 's'} revoked, each stops authenticating at once, and every environment grant released. It does not cascade to anything else, and it cannot be undone.`}
        </span>
      </p>
      <p className="ceremony__lede">
        Any bearer token or federated binding this account issued authenticates nothing the moment
        the delete lands. Distribute the replacement first if a workload still depends on it.
      </p>

      {failure !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
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

      <div className="ceremony__actions">
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}

/** blankClaims is a preset's pin fields, empty except the event a CI binding defaults to. */
function blankClaims(preset: FederationPreset): Record<string, string> {
  return Object.fromEntries(
    preset.claims.map((field) => [field.claim, field.kind === 'event' ? 'push' : '']),
  );
}

/**
 * CreateProviderDialog configures a dynamic-secret provider.
 *
 * The server PROBES the origin with the admin credential before it stores
 * anything, so a create that succeeds is a provider Hikyo could reach and
 * authenticate against, the dialog says so rather than promising a store that
 * might be unreachable. There is no passkey here: configuring standing project
 * authority is `manage-identities`, not a per-environment disclosure.
 */
function CreateProviderDialog({
  project,
  onClose,
  onCreated,
}: {
  project: ProjectRef;
  onClose: () => void;
  onCreated: (origin: string) => void;
}) {
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="create-provider-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
    >
      <h2 className="ceremony__title" id="create-provider-title">
        Configure dynamic-secret provider
      </h2>
      <p className="ceremony__lede">
        PostgreSQL is the only provider kind. Hikyo connects over <code>verify-full</code> TLS and
        mints each lease role <code>IN ROLE</code> the grant role, so the lease inherits exactly the
        access you granted that parent. The admin credential is write-only: it is never returned, and
        the server dials the origin with it before storing anything.
      </p>

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
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      ) : null}

      <div className="ceremony__actions">
        <button
          className="btn btn--primary"
          type="button"
          disabled={busy}
          onClick={() => void submit()}
        >
          {busy ? 'Configuring…' : 'Configure provider'}
        </button>
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}

/**
 * SetCredentialDialog replaces a provider's write-only admin credential. It
 * re-probes on set, so a success means the new credential authenticates; the
 * prior one is never shown and is overwritten, not archived.
 */
function SetCredentialDialog({
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
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="set-credential-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
    >
      <h2 className="ceremony__title" id="set-credential-title">
        {`${provider.credential_present ? 'Replace' : 'Set'} admin credential · ${provider.origin}`}
      </h2>
      <p className="ceremony__lede">
        Write-only: the credential is never read back. Hikyo dials the provider with it before
        storing it, so a failure here leaves the current credential in place.
      </p>

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
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      ) : null}

      <div className="ceremony__actions">
        <button
          className="btn btn--primary"
          type="button"
          disabled={busy}
          onClick={() => void submit()}
        >
          {busy ? 'Saving…' : 'Save credential'}
        </button>
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
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
function RevokeCredentialDialog({
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
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="revoke-credential-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
    >
      <h2 className="ceremony__title" id="revoke-credential-title">
        {`Revoke admin credential · ${provider.origin}`}
      </h2>
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
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      ) : null}

      <div className="ceremony__actions">
        <button
          className="btn btn--primary"
          type="button"
          disabled={busy}
          onClick={() => void submit()}
        >
          {busy ? 'Revoking…' : 'Revoke credential'}
        </button>
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
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
function DeleteProviderDialog({
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
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="delete-provider-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
    >
      <h2 className="ceremony__title" id="delete-provider-title">
        {`Delete provider · ${provider.origin}`}
      </h2>
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
        <div className="field chk">
          <input
            id="delete-provider-cascade"
            type="checkbox"
            checked={revokeAll}
            disabled={busy}
            onChange={(event) => {
              setRevokeAll(event.target.checked);
              setFailure(null);
            }}
          />
          <label htmlFor="delete-provider-cascade">
            Revoke every live lease of this provider as part of the delete.
          </label>
        </div>
      ) : null}

      {failure !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
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

      <div className="ceremony__actions">
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}

/**
 * LeaseMintDialog is the display-once lease mint.
 *
 * It reuses the mint lifecycle (request-addressed completion, stored-confirmation
 * gate, boundary masking) that the credential mint runs on. A human mint takes a
 * passkey reauthentication over the chosen environment, then the server discloses
 * the role name and its password EXACTLY once, a retry is a new lease, never the
 * old secret.
 */
function LeaseMintDialog({
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
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="lease-mint-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        dismiss();
      }}
    >
      {disclosed === null ? (
        <>
          <h2 className="ceremony__title" id="lease-mint-title">
            Mint dynamic-secret lease
          </h2>
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
            <p className="alert" role="alert">
              <span className="alert__glyph" aria-hidden="true">
                !
              </span>
              <span>{formError}</span>
            </p>
          ) : null}
          {failure !== null ? (
            <p className="alert" role="alert">
              <span className="alert__glyph" aria-hidden="true">
                !
              </span>
              <span>{failure}</span>
            </p>
          ) : null}

          <div className="ceremony__actions">
            <button
              className="btn btn--primary"
              type="button"
              disabled={busy}
              onClick={() => void run()}
            >
              {busy ? 'Minting…' : 'Use a passkey and mint'}
            </button>
            <button className="btn" type="button" onClick={dismiss} disabled={busy}>
              Cancel
            </button>
          </div>
        </>
      ) : (
        <>
          <h2 className="ceremony__title" id="lease-mint-title">
            Lease minted, shown exactly once
          </h2>
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
            <p className="notice" role="status">
              <span className="alert__glyph" aria-hidden="true">
                !
              </span>
              <span>{`This lease expires ${isoDay(disclosed.result.expires_at)}. The provider may have shortened the lifetime you asked for.`}</span>
            </p>
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
          <button
            className="btn"
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
          </button>
          {disclosed.copyStatus === null ? null : (
            <p className="notice" role="status">
              <span className="alert__glyph" aria-hidden="true">
                ⧉
              </span>
              <span>{disclosed.copyStatus}</span>
            </p>
          )}
          <div className="field chk">
            <input
              id="lease-mint-stored"
              type="checkbox"
              ref={confirmation}
              checked={disclosed.stored}
              onChange={(event) => {
                move({ type: 'confirm-stored', stored: event.target.checked });
              }}
            />
            <label htmlFor="lease-mint-stored">
              I have stored this password in its target workload.
            </label>
          </div>
          {disclosed.heldBack ? (
            <p className="alert" role="alert">
              <span className="alert__glyph" aria-hidden="true">
                !
              </span>
              <span>Confirm you have stored it: there is no second look at this password.</span>
            </p>
          ) : null}
          <div className="ceremony__actions">
            <button className="btn btn--primary" type="button" onClick={dismiss}>
              Done
            </button>
          </div>
        </>
      )}
    </dialog>
  );
}

/**
 * LeaseActionDialog confirms and queues a renew, revoke or settle. Each is
 * QUEUED: the server records the intent and the worker carries it out, so the
 * copy says "queued" rather than promising a done state the row does not yet
 * show. Renew offers an optional new ceiling; revoke and settle are one-tap.
 */
function LeaseActionDialog({
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
  const dialog = useModalDialog();
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
    <dialog
      className="ceremony"
      aria-labelledby="lease-action-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
    >
      <h2 className="ceremony__title" id="lease-action-title">
        {title}
      </h2>
      <p className="ceremony__lede">{lede}</p>

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
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      ) : null}

      <div className="ceremony__actions">
        <button
          className="btn btn--primary"
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
        </button>
        <button className="btn" type="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}
