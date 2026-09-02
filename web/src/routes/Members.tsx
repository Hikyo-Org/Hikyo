import { useEffect, useId, useState } from 'react';
import { useParams, useSearchParams } from 'react-router';

import {
  blastRadius,
  capabilitiesAt,
  defaultScopeValue,
  expandTemplate,
  grantFailureText,
  grantOutcomeSummary,
  grantScopeLabel,
  membershipFailureText,
  membershipRows,
  optionByValue,
  ROLE_TEMPLATES,
  revokeOutcomeText,
  scopeOf,
  scopeOptions,
  scopeValue,
  templatesAt,
  useApplyTemplate,
  useCreateGrants,
  useInstanceGrants,
  useOrgGrants,
  useRevokeGrant,
  whoCan,
  type Names,
  type ScopeOption,
} from '../api/access.ts';
import { ApiError } from '../api/client.ts';
import type { Grant } from '../api/identities.ts';
import { useOrg, useOrgTopology } from '../api/settings.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Alert, Done, Explain, JumpIndex, Panel } from './Sections.tsx';
import { useFeedback, useModalDialog } from './useModalDialog.ts';

const prototypeMode = import.meta.env.MODE === 'prototype';
const prototypeDefaultPrincipal = 'prn_44444444-4444-4444-8444-444444444444';

const prototypePrincipalNames = prototypeMode
  ? new Map([
      ['prn_11111111-1111-4111-8111-111111111111', 'alex'],
      ['prn_44444444-4444-4444-8444-444444444444', 'dana'],
      ['prn_55555555-5555-4555-8555-555555555555', 'sam'],
      ['prn_66666666-6666-4666-8666-666666666666', 'priya'],
    ])
  : null;

const projectCapabilityOrder = new Map([
  ['read', 0],
  ['edit', 1],
  ['publish', 2],
  ['manage-members', 3],
  ['reveal', 4],
  ['reveal-history', 5],
  ['audit-read', 6],
]);

const projectInspectorCapabilities = [
  'reveal',
  'read',
  'publish',
  'edit',
  'manage-members',
  'audit-read',
];

const projectGrantCapabilities = [
  'read',
  'edit',
  'publish',
  'reveal',
  'reveal-history',
  'definitions-edit',
  'project-settings',
  'manage-members',
  'audit-read',
];

/**
 * Members & grants (registry surface `members`, #55 + #60; locked prototype
 * app-chrome iteration 18).
 *
 * One surface for the whole organisation, and that is a decision rather than a
 * shortcut: `listOrgGrants` answers org-, project- AND environment-scoped
 * lines in one read, and there is deliberately no `grant.list-env` — "who can
 * reach this environment" has to include the lines above it, so an
 * environment-only listing could only answer a narrower question while looking
 * like the whole one. Project settings therefore links to a filtered projection
 * of this same organisation-wide read rather than growing a second permission
 * editor.
 *
 * What the listing does NOT contain is stated on the page, not only here:
 * instance-scope grants reach this organisation by inheritance and are absent
 * by design, because revoking an instance operator from a page with no
 * authority over one is a trap.
 */
/**
 * The scope a Members surface administers (#567). `org` reads the organisation
 * from the route and the optional `?project=` projection from the query, so
 * deep links keep working; `instance` reads no route data at all.
 */
export type MembersScope = { readonly kind: 'org' } | { readonly kind: 'instance' };

export function Members({ scope }: { scope: MembersScope }) {
  const params = useParams();
  const [search] = useSearchParams();
  const instance = scope.kind === 'instance';
  const org = instance ? '' : params.org === undefined ? '' : params.org;
  const projectId = instance ? '' : search.get('project') ?? '';
  const orgQuery = useOrg(org);
  const orgGrants = useOrgGrants(org);
  const instanceGrants = useInstanceGrants(instance);
  const grants = instance ? instanceGrants : orgGrants;
  const topology = useOrgTopology(org);
  const auth = useAuth();
  const revoke = useRevokeGrant();
  const feedback = useFeedback(grantFailureText);
  const [modal, setModal] = useState<'none' | 'grant' | 'blast'>('none');

  const orgName = orgQuery.data?.name ?? org;
  const project = topology.projects.find((candidate) => candidate.id === projectId);
  const projectName = project?.name ?? projectId;
  const scopeName = instance ? 'Instance' : projectId === '' ? orgName : projectName;
  // Instance-scope lines never name an organisation (`listInstanceGrants`
  // returns instance rows only), so no MFA-mandatory org directory read is
  // provoked here; the org name is the addressed org's.
  const names = {
    org: () => orgName,
    project: (id: string) => topology.projects.find((p) => p.id === id)?.name ?? id,
    environment: (id: string) =>
      topology.projects.flatMap((p) => p.environments).find((e) => e.id === id)?.name ?? id,
  };
  // Instance scope has exactly one option: there is nothing narrower to prefer,
  // and the modal's level-driven capability and template lists follow from it.
  const instanceOption: ScopeOption = {
    value: scopeValue({ kind: 'instance' }),
    label: 'This instance (every organisation)',
    scope: { kind: 'instance' },
    level: 'instance',
    group: 'Instance',
    isProtected: null,
  };
  const allOptions = instance
    ? [instanceOption]
    : scopeOptions(orgQuery.data?.id ?? org, orgName, topology.projects);
  // No topology gates an instance grant: the scope is the whole instance.
  const topologyReady = instance ? true : topology.ready;
  const topologyPending = instance ? false : topology.isPending;
  const topologyError = instance ? false : topology.isError;
  const projectOptions = projectId === ''
    ? []
    : allOptions.filter((option) =>
        (option.scope.kind === 'project' || option.scope.kind === 'environment') &&
        option.scope.project === projectId,
      );
  const inspectOptions = instance
    ? allOptions
    : projectId === ''
    ? prototypeMode
      ? prototypeProjectOptions(allOptions, names, ['production', 'staging', 'project', 'org'])
      : allOptions
    : prototypeMode
      ? prototypeProjectOptions(projectOptions, names, ['production', 'staging', 'project'])
      : projectOptions;
  const grantOptions = instance
    ? allOptions
    : projectId === ''
    ? allOptions
    : prototypeMode
      ? prototypeProjectOptions(
          [...projectOptions, ...allOptions.filter((option) => option.scope.kind === 'org')],
          names,
          ['staging', 'production', 'project', 'org'],
        )
      : [...projectOptions, ...allOptions.filter((option) => option.scope.kind === 'org')];
  const lines = grants.data?.items ?? [];
  const visibleLines = projectId === ''
    ? lines
    : lines.filter((grant) => {
        const scope = scopeOf(grant);
        return scope.kind !== 'instance' && scope.kind !== 'org' && scope.project === projectId;
      });
  const rows = membershipRows(visibleLines, names);
  const me = auth.identity?.principal.id ?? '';
  // The prototype's compact project presentation never applies at instance
  // scope: there is no project to be compact about.
  const compactPresentation = projectId !== '' || (prototypeMode && !instance);
  const secondFactorRefused =
    instance && grants.error instanceof ApiError && grants.error.status === 403;
  const nondisclosed = instance && grants.error instanceof ApiError && grants.error.status === 404;

  const [draft, setDraft] = useState<GrantDraft>({
    principal: '',
    capabilities: [],
    template: '',
    mode: 'capabilities',
    scope: '',
  });

  // The safe default is computed from the topology, so it can only settle once
  // the environments — and their protection — have actually been read.
  useEffect(() => {
    if (modal === 'grant' && topologyReady && draft.scope === '' && grantOptions.length > 0) {
      // Instance scope has one option and nothing narrower to prefer, so it
      // IS the safe default; everywhere else the narrowest confirmed-unprotected
      // environment wins, or nothing does.
      const safe = instance ? instanceOption.value : defaultScopeValue(grantOptions);
      if (safe !== '') {
        setDraft((current) => (current.scope === '' ? { ...current, scope: safe } : current));
      }
    }
  }, [draft.scope, grantOptions, instance, instanceOption.value, modal, topologyReady]);

  const onRevoke = (grant: Grant) => {
    feedback.clear();
    revoke.mutate(
      { grant },
      {
        onSuccess: async () => {
          const refreshed = await grants.refetch();
          if (refreshed.data === undefined || refreshed.isError) {
            feedback.ok(
              `The revoke was accepted, but the membership listing could not be refreshed. Reload before deciding whether ${grant.capability} remains effective.`,
            );
            return;
          }
          const address = scopeValue(scopeOf(grant));
          const survivor = refreshed.data.items.find(
            (candidate) =>
              candidate.principal_id === grant.principal_id &&
              candidate.capability === grant.capability &&
              scopeValue(scopeOf(candidate)) === address,
          );
          feedback.ok(revokeOutcomeText(grant, survivor, names));
        },
        onError: feedback.report,
      },
    );
  };

  return (
    <div className="page page--chrome page--members">
      <h1>{`Members\u00a0·\u00a0${scopeName}`}</h1>
      <p className="page__lede">
        One row per member per scope; each capability chip is still its own revocable grant; roles
        are templates that expand at grant time, so revoking a chip never drags a bundle with it.
      </p>

      <JumpIndex
        sections={[
          { id: 'members-inspect', label: 'Who can…?' },
          { id: 'members-list', label: 'Members' },
        ]}
      />

      {secondFactorRefused ? (
        <Alert>
          Instance grants require a second factor. Present your authenticator code or passkey in
          the banner above.
        </Alert>
      ) : nondisclosed ? (
        <p role="status">Instance grants are not disclosed to this session.</p>
      ) : grants.isError ? (
        <Alert>{membershipFailureText(grants.error)}</Alert>
      ) : null}
      {!instance && orgQuery.isError ? (
        <Alert>The organisation could not be read. Reload before managing its grants.</Alert>
      ) : null}
      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Done>{feedback.done}</Done> : null}

      <Inspect
        options={inspectOptions}
        grants={lines}
        names={names}
        grantsPending={grants.isPending}
        grantsSucceeded={grants.isSuccess}
        projectContext={compactPresentation}
        level={instance ? 'instance' : 'org'}
      />

      <Panel id="members-list" title="Members">
        {instance ? (
          <p>
            Every grant written at instance scope. These inherit downward into every organisation;
            each origin and its subject remain visible so incident provenance is not reduced to a
            colour.
          </p>
        ) : null}
        {!instance && projectId === '' ? (
          <p className="visually-hidden">
            Every grant line scoped inside {orgName}. Instance-scope grants reach this organisation
            by inheritance and are deliberately absent: this page has no authority over an instance
            operator, so it does not offer to revoke one.
          </p>
        ) : null}

        {grants.isPending ? <p role="status">Loading members…</p> : null}

        {grants.isSuccess && rows.length === 0 ? (
          <p role="status">
            {instance
              ? 'No instance-scope grants.'
              : projectId === ''
              ? 'No grants inside this organisation yet. Everyone reaching it does so from instance scope.'
              : 'No direct project or environment grants yet. Organisation and instance grants may still reach this project; use Who can…? to inspect inherited access.'}
          </p>
        ) : null}

        {rows.length === 0 ? null : (
          <table className="grants">
            <caption className="visually-hidden">
              Members of {scopeName}, one row per principal and scope
            </caption>
            <thead>
              <tr>
                <th scope="col">Member</th>
                <th scope="col">Scope</th>
                <th scope="col">Capabilities</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const protectedScope = row.grants.some((grant) => {
                  const scope = scopeOf(grant);
                  return scope.kind === 'environment' && project?.environments.some(
                    (environment) =>
                      environment.id === scope.environment && environment.isProtected === true,
                  ) === true;
                });
                const visibleScopeLabel = compactPresentation
                  ? compactGrantScopeLabel(row.grants, row.scopeLabel, names)
                  : row.scopeLabel;
                return (
                  <tr key={row.key}>
                    <td>
                      <span className={compactPresentation ? 'member-name' : 'mono'}>
                        {principalLabel(row.principal)}
                      </span>
                      {row.principal === me && !compactPresentation ? <span className="badge">you</span> : null}
                    </td>
                    <td>
                      <span
                        className={row.level === 'org'
                          ? 'chip chip--wide'
                          : protectedScope ? 'chip chip--protected' : 'chip'}
                        aria-label={protectedScope ? `${visibleScopeLabel}, protected` : undefined}
                      >
                        {visibleScopeLabel}
                      </span>
                    </td>
                    <td>
                      <ul className="capabilities">
                        {orderedMembershipGrants(row.grants, compactPresentation).map((grant) => {
                          const revoking =
                            revoke.isPending && revoke.variables?.grant.id === grant.id;
                          const revokeLabel = `${revoking ? 'Revoking' : 'Revoke'} ${grant.capability} on ${row.scopeLabel} for ${row.principal}`;
                          const revokeText = compactPresentation
                            ? (revoking ? '…' : '✕')
                            : (revoking ? 'Revoking…' : 'Revoke');
                          return (
                            <li
                              key={grant.id}
                              className={compactPresentation
                                ? 'capability capability--compact'
                                : 'capability'}
                              title={compactPresentation
                                ? `${grant.capability} · ${grant.origins.map((origin) => `${origin.kind}: ${origin.subject}`).join(', ')}`
                                : undefined}
                            >
                              <span className="capability__name mono">{grant.capability}</span>
                              {/* Origin chips per capability line: the SCIM
                                  amendment's own requirement, and the one thing
                                  that tells a break-glass grant from an ordinary
                                  one after an incident. */}
                              {!compactPresentation ? grant.origins.map((origin) => (
                                  <span
                                    className="badge"
                                    key={`${origin.kind}:${origin.subject}`}
                                  >
                                    {origin.kind}: {origin.subject}
                                  </span>
                                )) : (
                                  <>
                                    <span className="visually-hidden">
                                      Origins: {grant.origins
                                        .map((origin) => `${origin.kind}: ${origin.subject}`)
                                        .join(', ')}.
                                    </span>
                                    {grant.origins
                                      .filter((origin) => origin.kind !== 'manual')
                                      .map((origin) => (
                                        <span
                                          className="badge capability__origin"
                                          key={`${origin.kind}:${origin.subject}`}
                                        >
                                          ! {origin.kind}
                                        </span>
                                      ))}
                                  </>
                                )}
                              <button
                                type="button"
                                className={compactPresentation
                                  ? 'capability__revoke'
                                  : 'btn btn--quiet'}
                                disabled={revoking}
                                aria-busy={revoking ? true : undefined}
                                aria-label={revokeLabel}
                                onClick={() => onRevoke(grant)}
                              >
                                {revokeText}
                              </button>
                            </li>
                          );
                        })}
                      </ul>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}

        <div className="panel__actions">
          {topologyPending ? (
            <p role="status">Loading the complete organisation topology before a new grant can open…</p>
          ) : topologyError ? (
            <Alert>The organisation topology could not be read completely. Reload before granting anything.</Alert>
          ) : null}
          <button
            type="button"
            className="btn btn--primary"
            disabled={!topologyReady}
            onClick={() => {
              feedback.clear();
              setDraft(freshDraft(
                grantOptions,
                projectId !== '' && prototypeMode ? prototypeDefaultPrincipal : '',
              ));
              setModal('grant');
            }}
          >
            {compactPresentation ? '+ new grant' : 'New grant'}
          </button>
        </div>
      </Panel>

      {modal === 'none' ? null : (
        <GrantModal
          orgName={scopeName}
          options={grantOptions}
          projectContext={compactPresentation}
          draft={draft}
          stage={modal}
          projects={topology.projects}
          topologyReady={topologyReady}
          topologyPending={topologyPending}
          topologyError={topologyError}
          known={[...new Set(lines.map((line) => line.principal_id))]}
          onDraft={setDraft}
          onStage={setModal}
          onDone={(text) => {
            feedback.ok(text);
            setModal('none');
          }}
        />
      )}
    </div>
  );
}

// --- who can …? -------------------------------------------------------------

function Inspect({
  options,
  grants,
  names,
  grantsPending,
  grantsSucceeded,
  projectContext,
  level,
}: {
  options: readonly ScopeOption[];
  grants: readonly Grant[];
  names: Names;
  grantsPending: boolean;
  grantsSucceeded: boolean;
  projectContext: boolean;
  level: 'org' | 'instance';
}) {
  const capabilityId = useId();
  const scopeId = useId();
  const [capability, setCapability] = useState('reveal');
  const [scope, setScope] = useState('');

  const fallback = projectContext
    ? options.find((option) => option.isProtected === true) ?? options[0]
    : options[0];
  const chosen = optionByValue(options, scope) ?? fallback;
  const answer = chosen === undefined ? [] : whoCan(grants, capability, chosen.scope);
  const capabilityOptions = projectContext
    ? projectInspectorCapabilities.flatMap((id) =>
        capabilitiesAt('org').filter((atom) => atom.id === id),
      )
    : capabilitiesAt(level);

  return (
    <Panel id="members-inspect" title="Who can…? Answer by inspection">
      {projectContext ? null : level === 'instance' ? (
        <p>
          Answered by inspection over the instance-scope lines below. Grants inherit downward, so
          every line here reaches every organisation, project and environment.
        </p>
      ) : (
        <p>
          Answered by inspection over the lines below, including the ones ABOVE the scope you pick:
          grants inherit downward, so an organisation-scoped grant answers for every environment in
          it.
        </p>
      )}
      <div className="inspect">
        <div className="field">
          <label htmlFor={capabilityId}>Capability</label>
          <select
            id={capabilityId}
            value={capability}
            onChange={(event) => setCapability(event.target.value)}
          >
            {capabilityOptions.map((atom) => (
              <option key={atom.id} value={atom.id}>
                {atom.id}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={scopeId}>{projectContext ? 'On scope' : 'On'}</label>
          <select
            id={scopeId}
            value={chosen === undefined ? '' : chosen.value}
            onChange={(e) => setScope(e.target.value)}
          >
            {options.map((option) => (
              <option key={option.value} value={option.value}>
                {projectContext ? compactScopeLabel(option) : option.label}
              </option>
            ))}
          </select>
        </div>
      </div>
      {!grantsSucceeded ? (
        grantsPending ? (
          <p className="inspect__answer" role="status">
            Loading grants before answering who can reach this scope…
          </p>
        ) : null
      ) : (
        <p className="inspect__answer" role="status">
          {chosen === undefined ? (
            'This organisation holds no scope to inspect yet.'
          ) : projectContext && answer.length !== 0 ? (
            <>
              <strong className="mono">{capability}</strong> on{' '}
              <strong className="mono">{compactScopeLabel(chosen)}</strong> →{' '}
              {answer.map((grant, index) => (
                <span key={grant.id}>
                  {index === 0 ? null : ', '}
                  <strong>{principalLabel(grant.principal_id)}</strong>{' '}
                  <span className="chrome-meta">
                    (via {compactGrantScopeLabel([grant], grantScopeLabel(grant, names), names)})
                  </span>
                </span>
              ))}
            </>
          ) : projectContext ? (
            <>
              <strong className="mono">{capability}</strong> on{' '}
              <strong className="mono">{compactScopeLabel(chosen)}</strong> →{' '}
              <strong>nobody</strong>
            </>
          ) : answer.length === 0 && level === 'instance' ? (
          <>
            <strong>Nobody</strong> holds <span className="mono">{capability}</span> at instance
            scope.
          </>
        ) : answer.length === 0 ? (
          <>
            <strong>Nobody</strong> holds <span className="mono">{capability}</span> on{' '}
            {chosen.label} through a grant inside this organisation. An instance-scope holder would
            not appear here.
          </>
        ) : (
          <>
            <strong>{answer.length}</strong> grant {answer.length === 1 ? 'line' : 'lines'} give{' '}
            <span className="mono">{capability}</span> on {chosen.label}:{' '}
            {answer
              .map((grant) => `${grant.principal_id} (via ${grantScopeLabel(grant, names)})`)
              .join(', ')}
            .
          </>
        )}
        </p>
      )}
    </Panel>
  );
}

function compactScopeLabel(option: ScopeOption): string {
  if (option.scope.kind === 'environment') {
    return `env · ${option.label.replace(/ \((?:protected|protection unreadable)\)$/, '')}`;
  }
  if (option.scope.kind === 'project') {
    return `project · ${option.label.replace(/ \(every environment\)$/, '')}`;
  }
  return option.label;
}

function compactGrantOptionLabel(option: ScopeOption, orgName: string): string {
  if (option.scope.kind === 'org') return `org · ${orgName}`;
  return compactScopeLabel(option);
}

function prototypeProjectOptions(
  options: readonly ScopeOption[],
  names: Names,
  order: readonly ('production' | 'staging' | 'project' | 'org')[],
): readonly ScopeOption[] {
  const key = (option: ScopeOption) => {
    if (option.scope.kind === 'environment') return names.environment(option.scope.environment);
    return option.scope.kind;
  };
  return order.flatMap((name) => options.filter((option) => key(option) === name));
}

function compactGrantScopeLabel(
  grants: readonly Grant[],
  fallback: string,
  names: Names,
): string {
  const grant = grants[0];
  if (grant === undefined) return fallback;
  const scope = scopeOf(grant);
  if (scope.kind === 'environment') return `env · ${names.environment(scope.environment)}`;
  if (scope.kind === 'project') return `project · ${names.project(scope.project)}`;
  if (scope.kind === 'org') return `org · ${names.org(scope.org)}`;
  return fallback;
}

function principalLabel(principal: string): string {
  return prototypePrincipalNames?.get(principal) ?? principal;
}

function orderedMembershipGrants(
  grants: readonly Grant[],
  projectContext: boolean,
): readonly Grant[] {
  if (!projectContext) return grants;
  return [...grants].sort(
    (left, right) =>
      (projectCapabilityOrder.get(left.capability) ?? Number.MAX_SAFE_INTEGER) -
      (projectCapabilityOrder.get(right.capability) ?? Number.MAX_SAFE_INTEGER),
  );
}

// --- the grant modal --------------------------------------------------------

type GrantDraft = {
  principal: string;
  capabilities: string[];
  template: string;
  mode: 'capabilities' | 'template';
  scope: string;
};

function freshDraft(options: readonly ScopeOption[], principal = ''): GrantDraft {
  return {
    principal,
    capabilities: [],
    template: '',
    mode: 'capabilities',
    scope: defaultScopeValue(options),
  };
}

/**
 * GrantModal is the composition and its org-scope warning, in one component so
 * that "back, change scope" PRESERVES what was composed.
 *
 * That was iteration 13's fix and it is not cosmetic: a warning that threw the
 * composition away would train people to click through it, which is the exact
 * opposite of what a blast-radius warning is for.
 */
function GrantModal({
  orgName,
  options,
  draft,
  stage,
  projects,
  topologyReady,
  topologyPending,
  topologyError,
  known,
  onDraft,
  onStage,
  onDone,
  projectContext,
}: {
  orgName: string;
  options: readonly ScopeOption[];
  draft: GrantDraft;
  stage: 'grant' | 'blast';
  projects: ReturnType<typeof useOrgTopology>['projects'];
  topologyReady: boolean;
  topologyPending: boolean;
  topologyError: boolean;
  known: readonly string[];
  onDraft: (draft: GrantDraft) => void;
  onStage: (stage: 'none' | 'grant' | 'blast') => void;
  onDone: (text: string) => void;
  projectContext: boolean;
}) {
  const dialog = useModalDialog();
  const [failure, setFailure] = useState<string | null>(null);
  const create = useCreateGrants();
  const applyTemplate = useApplyTemplate();
  const principalId = useId();
  const scopeId = useId();
  const templateId = useId();

  const chosen = optionByValue(options, draft.scope);
  const atoms = projectContext && prototypeMode
    ? projectGrantCapabilities.flatMap((id) =>
        capabilitiesAt('org').filter((atom) => atom.id === id),
      )
    : chosen === undefined ? [] : capabilitiesAt(chosen.level);
  const templates = chosen === undefined ? [] : templatesAt(chosen.level);
  const mutationPending = create.isPending || applyTemplate.isPending;
  const submitBlocked = mutationPending || !topologyReady;

  const selectedTemplate = ROLE_TEMPLATES.find((template) => template.id === draft.template);
  const composed =
    draft.mode === 'template' && selectedTemplate !== undefined && chosen !== undefined
      ? expandTemplate(selectedTemplate.id, chosen.level).join(', ')
      : draft.capabilities.join(', ');

  const submit = () => {
    setFailure(null);
    if (draft.principal.trim() === '') {
      setFailure('Name the principal this grant is for: a human or a service-account id.');
      return;
    }
    if (chosen === undefined) {
      setFailure(
        'Choose a scope. Nothing is preselected when no environment could be confirmed unprotected.',
      );
      return;
    }
    if (!topologyReady && topologyPending) {
      setFailure(
        'The organisation topology is still loading. Wait for every project and environment before confirming this scope.',
      );
      return;
    }
    if (!topologyReady && topologyError) {
      setFailure('The organisation topology could not be read completely. Reload before granting anything.');
      return;
    }
    if (!topologyReady) {
      setFailure('The organisation topology is not ready. Reload before granting anything.');
      return;
    }
    if (draft.mode === 'capabilities' && draft.capabilities.length === 0) {
      setFailure('Pick at least one capability. Each one becomes its own revocable line.');
      return;
    }
    if (draft.mode === 'template' && draft.template === '') {
      setFailure('Pick a role template, or switch back to choosing capabilities.');
      return;
    }
    if (chosen.level === 'org' && stage !== 'blast') {
      onStage('blast');
      return;
    }
    perform();
  };

  const perform = () => {
    if (chosen === undefined) {
      return;
    }
    const scope = chosen.scope;
    const principal = draft.principal.trim();
    if (draft.mode === 'template') {
      applyTemplate.mutate(
        { scope, principal, template: draft.template },
        {
          onSuccess: (result) =>
            onDone(
              `Applied ${draft.template} to ${principal} on ${chosen.label}: ${grantOutcomeSummary(result.items)} Each grant line remains independently revocable.`,
            ),
          onError: (error) => {
            onStage('grant');
            setFailure(grantFailureText(error));
          },
        },
      );
      return;
    }
    create.mutate(
      { scope, principal, capabilities: draft.capabilities },
      {
        onSuccess: (done) =>
          onDone(
            `Grant results for ${principal} on ${chosen.label}: ${grantOutcomeSummary(done)} Each grant line remains independently revocable.`,
          ),
        onError: (error) => {
          onStage('grant');
          setFailure(grantFailureText(error));
        },
      },
    );
  };

  if (stage === 'blast') {
    return (
      <dialog
        className="ceremony blast"
        ref={dialog}
        aria-labelledby="blast-title"
        onCancel={(event) => {
          event.preventDefault();
          if (!mutationPending) {
            onStage('none');
          }
        }}
      >
        <h2 id="blast-title">Organisation-scoped grant: check the blast radius</h2>
        <p className="ceremony__lede">
          <strong>{draft.principal}</strong> would get <span className="mono">{composed}</span> on{' '}
          <strong>every project and environment in {orgName}</strong>, current and future. Grants
          inherit downward and there are no deny rules, so there is no per-project exception under
          an organisation grant.
        </p>
        {topologyPending ? (
          <p role="status" aria-live="polite">
            Loading every project, environment, and protection state before showing the blast radius…
          </p>
        ) : topologyError ? (
          <Alert>The full organisation topology could not be read. Reload before confirming this grant.</Alert>
        ) : (
          <ul className="blast__list" aria-label="What an organisation-scoped grant reaches">
            {blastRadius(projects).map((line) => (
              <li key={line.project}>
                <span className="blast__project">{line.project}</span>
                <span className="blast__envs">{line.environments}</span>
              </li>
            ))}
          </ul>
        )}
        <p className="ceremony__cap" role="status">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            Narrower option: grant on one project, or on one environment. Production protection
            rests on granting narrowly, not on the protected flag alone.
          </span>
        </p>
        {failure !== null ? <Alert>{failure}</Alert> : null}
        <div className="ceremony__actions">
          <button type="button" className="btn" disabled={mutationPending} onClick={() => onStage('none')}>
            Cancel
          </button>
          <button type="button" className="btn" disabled={mutationPending} onClick={() => onStage('grant')}>
            Back, change scope
          </button>
          <button type="button" className="btn btn--danger" disabled={submitBlocked} onClick={perform}>
            Grant at organisation scope
          </button>
        </div>
      </dialog>
    );
  }

  return (
    <dialog
      className="ceremony grant-modal"
      ref={dialog}
      aria-labelledby="grant-title"
      onCancel={(event) => {
        event.preventDefault();
        if (!mutationPending) {
          onStage('none');
        }
      }}
    >
      <h2 id="grant-title">New grant</h2>
      <p className="ceremony__lede">
        {projectContext && prototypeMode ? (
          <>Each checked capability becomes its <strong>own revocable grant</strong>. Roles are templates doing exactly this with a preset checklist.</>
        ) : (
          <>Pick any number of capabilities: each becomes its <strong>own revocable line</strong> at this scope, never a bundle. A role template does exactly this with a preset list.</>
        )}
      </p>

      {failure !== null ? <Alert>{failure}</Alert> : null}
      {topologyPending ? (
        <p role="status" aria-live="polite">
          Loading every project, environment, and protection state before a grant can be submitted…
        </p>
      ) : topologyError ? (
        <Alert>The organisation topology could not be read completely. Reload before granting anything.</Alert>
      ) : null}

      <div className="field">
        <label htmlFor={principalId}>Principal</label>
        {projectContext && prototypeMode ? (
          <select
            id={principalId}
            value={draft.principal}
            onChange={(event) => onDraft({ ...draft, principal: event.target.value })}
          >
            {known.flatMap((id) => {
              const label = prototypePrincipalNames?.get(id);
              return label === undefined || label === 'alex'
                ? []
                : [<option key={id} value={id}>{label}</option>];
            })}
          </select>
        ) : (
          <>
            <input
              id={principalId}
              list={`${principalId}-known`}
              value={draft.principal}
              autoComplete="off"
              spellCheck={false}
              onChange={(event) => onDraft({ ...draft, principal: event.target.value })}
            />
            <datalist id={`${principalId}-known`}>
              {known.map((id) => (
                <option key={id} value={id} />
              ))}
            </datalist>
            <p className="field__hint">
              The principal id of a person or a service account; the list offers the ones already
              holding something here.
            </p>
          </>
        )}
      </div>

      {projectContext ? null : (
        <fieldset className="grant-modal__mode">
          <legend>What to grant</legend>
          <label className="chk">
            <input
              type="radio"
              name="grant-mode"
              checked={draft.mode === 'capabilities'}
              onChange={() => onDraft({ ...draft, mode: 'capabilities' })}
            />
            <span>Choose capabilities</span>
          </label>
          <label className="chk">
            <input
              type="radio"
              name="grant-mode"
              checked={draft.mode === 'template'}
              onChange={() => onDraft({ ...draft, mode: 'template' })}
            />
            <span>Apply a role template</span>
          </label>
        </fieldset>
      )}

      {projectContext || draft.mode === 'capabilities' ? (
        <ul className="capgrid" aria-label="Capabilities to grant">
          {atoms.map((atom) => (
            <li className="capitem" key={atom.id}>
              <label className="chk">
                <input
                  type="checkbox"
                  checked={draft.capabilities.includes(atom.id)}
                  onChange={(event) =>
                    onDraft({
                      ...draft,
                      capabilities: event.target.checked
                        ? [...draft.capabilities, atom.id]
                        : draft.capabilities.filter((id) => id !== atom.id),
                    })
                  }
                />
                <span className="mono">{atom.id}</span>
              </label>
              <Explain label={atom.id} text={atom.covers} />
            </li>
          ))}
        </ul>
      ) : (
        <div className="field">
          <label htmlFor={templateId}>Role template</label>
          <select
            id={templateId}
            value={draft.template}
            onChange={(event) => onDraft({ ...draft, template: event.target.value })}
          >
            <option value="">Choose a template…</option>
            {templates.map((template) => (
              <option key={template.id} value={template.id}>
                {template.id}
              </option>
            ))}
          </select>
          <p className="field__hint">
            {draft.template === '' || selectedTemplate === undefined || chosen === undefined
              ? 'A template is expanded by the server at grant time; what lands is grants.'
              : `Seeds: ${expandTemplate(selectedTemplate.id, chosen.level).join(', ')}.`}
          </p>
        </div>
      )}

      <div className="field">
        <label htmlFor={scopeId}>Scope</label>
        <select
          id={scopeId}
          value={draft.scope}
          onChange={(event) => {
            const next = optionByValue(options, event.target.value);
            if (next === undefined) {
              onDraft({ ...draft, scope: '', capabilities: [], template: '' });
              return;
            }
            onDraft({
              ...draft,
              scope: next.value,
              capabilities: draft.capabilities.filter((id) =>
                capabilitiesAt(next.level).some((atom) => atom.id === id),
              ),
              template: templatesAt(next.level).some((template) => template.id === draft.template)
                ? draft.template
                : '',
            });
          }}
        >
          {projectContext && prototypeMode ? null : <option value="">Choose a scope…</option>}
          {[...new Set(options.map((option) => option.group))].map((group) => (
            <optgroup key={group} label={group}>
              {options
                .filter((option) => option.group === group)
                .map((option) => (
                  <option key={option.value} value={option.value}>
                    {projectContext && prototypeMode
                      ? compactGrantOptionLabel(option, orgName)
                      : option.label}
                  </option>
                ))}
            </optgroup>
          ))}
        </select>
        {projectContext && prototypeMode ? null : chosen?.level === 'instance' ? (
          <p className="field__hint">
            Instance scope reaches every organisation, current and future.
          </p>
        ) : (
          <p className="field__hint">
            Narrowest first. A protected environment is last in its project and is never
            preselected; an organisation scope reaches every project, current and future.
          </p>
        )}
      </div>

      <div className="ceremony__actions">
        <button type="button" className="btn" disabled={mutationPending} onClick={() => onStage('none')}>
          Cancel
        </button>
        <button type="button" className="btn btn--primary" disabled={submitBlocked} onClick={submit}>
          {mutationPending ? 'Granting…' : 'Grant'}
        </button>
      </div>
    </dialog>
  );
}
