import { useScopeNames } from '../api/scopeNames.ts';
import { useMemo, useRef, useState, type FormEvent } from 'react';
import { useParams, useSearchParams } from 'react-router';

import { useSensitiveState } from '../api/sensitiveMutation.ts';
import {
  optionByValue,
  scopeOptions,
  templatesAt,
  type Level,
  type ScopeOption,
  type ScopeRef,
} from '../api/access.ts';
import { isoDay } from '../api/identities.ts';
import {
  scimMintFailureText,
  scimMutationFailureText,
  scimReadFailureText,
  useCreateScimBinding,
  useCreateScimMapping,
  useDeleteScimBinding,
  useDeleteScimMapping,
  useMintScimCredential,
  useRevokeScimCredential,
  useScimBindings,
  useScimCredentials,
  useScimDirectoryGroups,
  useScimDirectoryUsers,
  useScimMappings,
  useUpdateScimMapping,
  type MintedScimCredential,
  type ScimAttention,
  type ScimBinding,
  type ScimBlastWarning,
  type ScimCredential,
  type ScimDirectoryUser,
  type ScimMapping,
  type ScimMappingResult,
} from '../api/scim.ts';
import { useOrg, useOrgTopology } from '../api/settings.ts';
import { writeClipboard } from '../app/clipboard.ts';
import { Alert, Done, Explain, JumpIndex, Panel, TypedNameConfirm } from './Sections.tsx';
import { useFeedback, useModalDialog } from './useModalDialog.ts';
import { useNavigationGuard } from './MachineAccess.tsx';

/**
 * SCIM provisioning administration (registry surface `scim`, #501; #73,
 * scim-provisioning ADR §8).
 *
 * The org admin's whole browser answer to "manage SCIM": binding CRUD, the
 * group→template mapping table, the provisioning-credential lifecycle, and the
 * provisioned directory. Every operation here is `manage-members` held at ORG
 * SCOPE EXACTLY and is MFA-mandatory, so the surface renders the server's
 * refusals rather than second-guessing the caller's authority, a 403 IS the
 * second-factor refusal, a 404 is the uniform "not available or absent".
 *
 * The consequence language a human is TOLD is server-authored on purpose: a
 * mapping mutation returns `warnings`, and this surface renders them verbatim
 * rather than composing a second, unreviewed policy. What the surface states on
 * its own, the binding teardown, the revocation bite, is factual and drawn
 * from the contract, never a guess about scope reach.
 */
export function ScimProvisioning() {
  const params = useParams();
  const org = params['org'] ?? '';
  const [search, setSearch] = useSearchParams();
  // The selected binding is ROUTE DATA in the query string, an id, never a
  // secret, so a reload and a shared link land on the same binding.
  const selected = search.get('binding') ?? '';

  const bindings = useScimBindings(org);
  const items = bindings.data?.items ?? [];
  const active = items.find((binding) => binding.id === selected) ?? null;

  const sections = [
    { id: 'scim-bindings', label: 'Bindings' },
    ...(active === null
      ? []
      : [
          { id: 'scim-mappings', label: 'Mappings' },
          { id: 'scim-credentials', label: 'Provisioning credentials' },
          { id: 'scim-directory', label: 'Directory' },
        ]),
  ];

  const select = (binding: string) => {
    const next = new URLSearchParams(search);
    if (binding === '') {
      next.delete('binding');
    } else {
      next.set('binding', binding);
    }
    setSearch(next, { replace: true });
  };

  return (
    <div className="page page--chrome">
      <h1>SCIM provisioning</h1>
      <p className="page__lede">
        Bind an identity provider, map its groups to capability templates, mint the credentials it
        presents, and watch what it has provisioned. Every action here needs organisation member
        management and a second factor.
      </p>

      <JumpIndex sections={sections} />

      <Panel id="scim-bindings" title="Bindings">
        <BindingsSection
          org={org}
          bindings={items}
          isError={bindings.isError}
          error={bindings.error}
          isSuccess={bindings.isSuccess}
          selected={selected}
          onSelect={select}
        />
      </Panel>

      {active === null ? null : (
        // Keyed by binding id so switching the administered binding REMOUNTS
        // these sections: an in-flight mint or an open display-once dialog can
        // never carry one binding's token into another binding's panel. A mint
        // still committing when the operator switches loses the dialog, but the
        // credentials list was already invalidated, so the new credential shows
        // as revocable metadata rather than a wrong-panel disclosure.
        <div key={active.id}>
          <MappingsSection org={org} binding={active} />
          <CredentialsSection org={org} binding={active} />
          <DirectorySection org={org} binding={active} />
        </div>
      )}
    </div>
  );
}

// --- bindings ---------------------------------------------------------------

function BindingsSection({
  org,
  bindings,
  isError,
  error,
  isSuccess,
  selected,
  onSelect,
}: {
  org: string;
  bindings: readonly ScimBinding[];
  isError: boolean;
  error: unknown;
  isSuccess: boolean;
  selected: string;
  onSelect: (binding: string) => void;
}) {
  return (
    <>
      <p>
        At most one binding per identity provider. Creating one also mints its provisioning
        connection and that connection&apos;s structural grant, in one transaction.
      </p>

      {isError ? <Alert>{scimReadFailureText(error)}</Alert> : null}

      {isSuccess && bindings.length === 0 ? (
        <p role="status">
          No bindings yet. Create one below to begin provisioning from an identity provider.
        </p>
      ) : null}

      <ul className="scim-bindings">
        {bindings.map((binding) => (
          <BindingCard
            key={binding.id}
            org={org}
            binding={binding}
            selected={binding.id === selected}
            onSelect={() => onSelect(binding.id)}
          />
        ))}
      </ul>

      <CreateBindingForm org={org} />
    </>
  );
}

function BindingCard({
  org,
  binding,
  selected,
  onSelect,
}: {
  org: string;
  binding: ScimBinding;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <li className={`card scim-binding${selected ? ' scim-binding--selected' : ''}`}>
      <div className="scim-binding__head">
        <h3 className="scim-binding__slug">
          <span className="mono">{binding.provider_slug}</span>
          <span className="badge" data-state={binding.provider_kind}>
            {binding.provider_kind}
          </span>
        </h3>
        <button
          type="button"
          className={selected ? 'btn btn--primary' : 'btn'}
          aria-pressed={selected}
          onClick={onSelect}
        >
          {selected ? 'Administering' : 'Administer'}
        </button>
      </div>
      <dl className="scim-binding__facts">
        <dt>Issuer</dt>
        <dd className="mono">{binding.provider_issuer}</dd>
        <dt>Subject source</dt>
        <dd className="mono">{binding.subject_source}</dd>
        <dt>Created</dt>
        <dd>{isoDay(binding.created_at)}</dd>
        <dt>Last contact</dt>
        <dd>
          {binding.last_contact_at === undefined ? 'never contacted' : isoDay(binding.last_contact_at)}
        </dd>
      </dl>
      <AttentionList attention={binding.attention} subjectPrefix="" />
      {selected ? <DeleteBinding org={org} binding={binding} /> : null}
    </li>
  );
}

/** The six attention states each name their cause AND a server-authored fix. */
function AttentionList({
  attention,
  subjectPrefix,
}: {
  attention: readonly ScimAttention[];
  subjectPrefix: string;
}) {
  if (attention.length === 0) {
    return null;
  }
  return (
    <ul className="scim-attention" aria-label={`${subjectPrefix}attention states`}>
      {attention.map((state, index) => (
        <li key={`${state.state}-${state.subject_ref}-${index}`} className="scim-attention__row">
          <span className="badge" data-state={state.state}>
            {state.state.replace(/_/g, ' ')}
          </span>
          <span className="scim-attention__fix">{state.remediation}</span>
        </li>
      ))}
    </ul>
  );
}

function DeleteBinding({ org, binding }: { org: string; binding: ScimBinding }) {
  const remove = useDeleteScimBinding(org);
  const feedback = useFeedback(scimMutationFailureText);
  return (
    <div className="scim-binding__danger">
      <h4>Delete this binding</h4>
      <p>
        One transaction, in order: every credential is revoked so no new sync can begin; every SCIM
        origin is released (lockout conversion included); the provisioning connection and its grant
        are retired; the directory, mapping table and binding row go. Identity links and accounts{' '}
        <strong>survive</strong>; they are account property.
      </p>
      {feedback.failure === null ? null : <Alert>{feedback.failure}</Alert>}
      <TypedNameConfirm
        label="Confirm by typing the provider slug"
        expect={binding.provider_slug}
        action="Delete binding"
        busy={remove.isPending}
        hint={
          <>
            This runs the teardown above and cannot be undone. Type{' '}
            <span className="mono">{binding.provider_slug}</span> to enable it.
          </>
        }
        onConfirm={() => {
          feedback.clear();
          remove.mutate(binding.id, {
            onError: (caught) => feedback.report(caught),
          });
        }}
      />
    </div>
  );
}

function CreateBindingForm({ org }: { org: string }) {
  const create = useCreateScimBinding(org);
  const feedback = useFeedback(scimMutationFailureText);
  const [providerKind, setProviderKind] = useState<'oidc' | 'saml'>('oidc');
  const [providerSlug, setProviderSlug] = useState('');
  const [subjectSource, setSubjectSource] = useState('externalId');

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const slug = providerSlug.trim();
    const source = subjectSource.trim();
    if (slug === '' || source === '' || create.isPending) {
      return;
    }
    feedback.clear();
    create.mutate(
      { providerKind, providerSlug: slug, subjectSource: source },
      {
        onSuccess: (binding) => {
          feedback.ok(`Bound ${binding.provider_slug}. Select it above to map groups and mint credentials.`);
          setProviderSlug('');
          setSubjectSource('externalId');
        },
        onError: (caught) => feedback.report(caught),
      },
    );
  };

  return (
    <form className="form" onSubmit={onSubmit} noValidate>
      <h3>Create a binding</h3>
      {feedback.failure === null ? null : <Alert>{feedback.failure}</Alert>}
      {feedback.done === null ? null : <Done>{feedback.done}</Done>}
      <fieldset className="field">
        <legend>Provider kind</legend>
        <div className="chk">
          <input
            id="scim-kind-oidc"
            type="radio"
            name="scim-kind"
            checked={providerKind === 'oidc'}
            onChange={() => setProviderKind('oidc')}
          />
          <label htmlFor="scim-kind-oidc">OIDC</label>
        </div>
        <div className="chk">
          <input
            id="scim-kind-saml"
            type="radio"
            name="scim-kind"
            checked={providerKind === 'saml'}
            onChange={() => setProviderKind('saml')}
          />
          <label htmlFor="scim-kind-saml">SAML</label>
        </div>
      </fieldset>
      <div className="field">
        <label htmlFor="scim-provider-slug">Provider slug</label>
        <input
          id="scim-provider-slug"
          className="mono"
          value={providerSlug}
          onChange={(event) => setProviderSlug(event.target.value)}
          maxLength={256}
          autoComplete="off"
          spellCheck={false}
          required
        />
      </div>
      <div className="field">
        <label htmlFor="scim-subject-source">
          Subject source{' '}
          <Explain
            label="subject source"
            text="The SCIM attribute path carrying identity material. externalId or a declared extension path; userName is refused by name. Immutable after creation."
          />
        </label>
        <input
          id="scim-subject-source"
          className="mono"
          value={subjectSource}
          onChange={(event) => setSubjectSource(event.target.value)}
          maxLength={256}
          autoComplete="off"
          spellCheck={false}
          required
        />
      </div>
      <button
        className="btn btn--primary"
        type="submit"
        disabled={create.isPending || providerSlug.trim() === '' || subjectSource.trim() === ''}
      >
        {create.isPending ? 'Creating…' : 'Create binding'}
      </button>
    </form>
  );
}

// --- mappings ---------------------------------------------------------------

function MappingsSection({ org, binding }: { org: string; binding: ScimBinding }) {
  const mappings = useScimMappings(org, binding.id);
  const groups = useScimDirectoryGroups(org, binding.id);
  const rows = mappings.data?.items ?? [];
  // The delete outcome lives HERE, not in the row: deleting releases the row on
  // the settled refetch, and feedback stored inside it would vanish with it, so
  // the consequence a delete reports has to outlive the row it describes.
  const [deleteOutcome, setDeleteOutcome] = useState<string | null>(null);
  const groupNames = useMemo(() => {
    const map = new Map<string, string>();
    for (const group of groups.data?.items ?? []) {
      map.set(group.id, group.display_name);
    }
    return map;
  }, [groups.data]);

  return (
    <Panel id="scim-mappings" title="Mappings">
      <p>
        Each row grants a provider group&apos;s members the capabilities a template expands into, at
        one scope. Adding a row grants the group&apos;s current members immediately; the consequence
        language is returned by the server and shown after the change.
      </p>

      {mappings.isError ? <Alert>{scimReadFailureText(mappings.error)}</Alert> : null}
      {deleteOutcome === null ? null : <Done>{deleteOutcome}</Done>}

      {mappings.isSuccess && rows.length === 0 ? (
        <p role="status">No mappings yet. Map a provisioned group to a template below.</p>
      ) : null}

      <ul className="scim-mappings">
        {rows.map((row) => (
          <MappingRow
            key={row.id}
            org={org}
            binding={binding.id}
            row={row}
            groupName={groupNames.get(row.group_id) ?? row.group_id}
            onDeleted={(released) =>
              setDeleteOutcome(`Deleted. ${String(released)} origins released.`)
            }
          />
        ))}
      </ul>

      <CreateMappingForm org={org} binding={binding.id} />
    </Panel>
  );
}

function scopeOfMapping(row: ScimMapping): { project?: string; environment?: string } {
  return {
    ...(row.project_id === undefined || row.project_id === '' ? {} : { project: row.project_id }),
    ...(row.environment_id === undefined || row.environment_id === ''
      ? {}
      : { environment: row.environment_id }),
  };
}

function scopeLabel(row: ScimMapping, names: ReturnType<typeof useScopeNames>): string {
  if (row.environment_id !== undefined && row.environment_id !== '') {
    return `environment ${names.project} / ${names.environment}`;
  }
  if (row.project_id !== undefined && row.project_id !== '') {
    return `project ${names.project}`;
  }
  return 'organisation (widest)';
}

function scopeLevel(row: ScimMapping): Level {
  if (row.environment_id !== undefined && row.environment_id !== '') {
    return 'environment';
  }
  if (row.project_id !== undefined && row.project_id !== '') {
    return 'project';
  }
  return 'org';
}

function MappingRow({
  org,
  binding,
  row,
  groupName,
  onDeleted,
}: {
  org: string;
  binding: string;
  row: ScimMapping;
  groupName: string;
  onDeleted: (originsReleased: number) => void;
}) {
  const names = useScopeNames(org, row.project_id ?? '', row.environment_id ?? '');
  const update = useUpdateScimMapping(org, binding);
  const remove = useDeleteScimMapping(org, binding);
  const feedback = useFeedback(scimMutationFailureText);
  const [result, setResult] = useState<ScimMappingResult | null>(null);
  const [editing, setEditing] = useState(false);
  const [template, setTemplate] = useState(row.template);

  const templates = templatesAt(scopeLevel(row));

  const onRetarget = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (update.isPending) {
      return;
    }
    feedback.clear();
    setResult(null);
    update.mutate(
      { groupId: row.group_id, template, ...scopeOfMapping(row) },
      {
        onSuccess: (next) => {
          setResult(next);
          setEditing(false);
          feedback.ok(mappingChangeSummary(next));
        },
        onError: (caught) => feedback.report(caught),
      },
    );
  };

  const onDelete = () => {
    feedback.clear();
    setResult(null);
    remove.mutate(
      { group: row.group_id, ...scopeOfMapping(row) },
      {
        // Report UP: this row is about to be removed by the settled refetch, so
        // its own feedback would disappear with it.
        onSuccess: (next) => onDeleted(next.origins_released),
        onError: (caught) => feedback.report(caught),
      },
    );
  };

  return (
    <li className="card scim-mapping">
      <div className="scim-mapping__head">
        <h3 className="scim-mapping__group">
          {groupName}
          {row.inert ? (
            <span className="badge" data-state="inert">
              inert
            </span>
          ) : null}
        </h3>
        <span className="mono scim-mapping__template">{row.template}</span>
      </div>
      <p className="scim-mapping__scope">{scopeLabel(row, names)}</p>
      {row.inert ? (
        <p className="notice" role="status">
          The provider group behind this row no longer exists. It grants nothing until it is edited
          or deleted; it is never removed automatically.
        </p>
      ) : null}
      <ul className="scim-mapping__caps">
        {row.capabilities.map((capability) => (
          <li key={capability} className="capability">
            <span className="capability__name mono">{capability}</span>
            {(row.capability_origins ?? []).filter((origin) => origin.capability === capability).map((origin) => (
              <span className="badge mono" key={`${origin.binding_id}:${origin.mapping_id}:${origin.group_id}`} title={`Binding ${origin.binding_id}, mapping ${origin.mapping_id}`}>
                {origin.kind}: {origin.group_id === row.group_id ? groupName : origin.group_id}
              </span>
            ))}
          </li>
        ))}
      </ul>

      {feedback.failure === null ? null : <Alert>{feedback.failure}</Alert>}
      {feedback.done === null ? null : <Done>{feedback.done}</Done>}
      {result === null ? null : <MappingWarnings warnings={result.warnings} />}

      {editing ? (
        <form className="form scim-mapping__edit" onSubmit={onRetarget} noValidate>
          <div className="field">
            <label htmlFor={`retarget-${row.id}`}>Retarget template</label>
            <select
              id={`retarget-${row.id}`}
              value={template}
              onChange={(event) => setTemplate(event.target.value)}
            >
              {templates.map((option) => (
                <option key={option.id} value={option.id}>
                  {option.id}
                </option>
              ))}
            </select>
          </div>
          <div className="scim-mapping__actions">
            <button className="btn btn--primary" type="submit" disabled={update.isPending}>
              {update.isPending ? 'Retargeting…' : 'Save template'}
            </button>
            <button
              className="btn"
              type="button"
              onClick={() => {
                setEditing(false);
                setTemplate(row.template);
              }}
            >
              Cancel
            </button>
          </div>
        </form>
      ) : (
        <div className="scim-mapping__actions">
          <button
            className="btn"
            type="button"
            onClick={() => setEditing(true)}
            aria-label={`Retarget ${groupName}`}
          >
            Retarget
          </button>
          <button
            className="btn btn--danger"
            type="button"
            disabled={remove.isPending}
            aria-label={`Delete mapping for ${groupName}`}
            onClick={onDelete}
          >
            {remove.isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      )}
    </li>
  );
}

function mappingChangeSummary(result: ScimMappingResult): string {
  const created = result.grants_created;
  const released = result.origins_released;
  return (
    `Applied to ${String(result.members_affected)} member(s): ` +
    `${String(created)} grants created, ${String(released)} origins released.`
  );
}

/** Server-authored consequence language, rendered VERBATIM (never composed here). */
function MappingWarnings({ warnings }: { warnings: readonly ScimBlastWarning[] }) {
  if (warnings.length === 0) {
    return null;
  }
  return (
    <ul className="scim-warnings" aria-label="Consequences">
      {warnings.map((warning, index) => (
        <li
          key={`${warning.code}-${index}`}
          className={warning.severity === 'critical' ? 'alert' : 'notice'}
          role={warning.severity === 'critical' ? 'alert' : 'status'}
        >
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{warning.message}</span>
        </li>
      ))}
    </ul>
  );
}

function CreateMappingForm({ org, binding }: { org: string; binding: string }) {
  const create = useCreateScimMapping(org, binding);
  const groups = useScimDirectoryGroups(org, binding);
  const orgQuery = useOrg(org);
  const topology = useOrgTopology(org);
  const feedback = useFeedback(scimMutationFailureText);
  const [result, setResult] = useState<ScimMappingResult | null>(null);
  const [groupId, setGroupId] = useState('');
  const [scope, setScope] = useState('');

  const options = useMemo<ScopeOption[]>(
    () => scopeOptions(org, orgQuery.data?.name ?? org, topology.projects),
    [org, orgQuery.data, topology.projects],
  );
  const chosen = scope === '' ? null : (optionByValue(options, scope) ?? null);
  const level: Level = chosen?.level ?? 'org';
  const templates = templatesAt(level);
  const [template, setTemplate] = useState('');
  const templateValid = templates.some((option) => option.id === template);

  const groupItems = groups.data?.items ?? [];

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (groupId === '' || !templateValid || create.isPending) {
      return;
    }
    feedback.clear();
    setResult(null);
    const target = scopeToTarget(chosen?.scope);
    create.mutate(
      { groupId, template, ...target },
      {
        onSuccess: (next) => {
          setResult(next);
          feedback.ok(mappingChangeSummary(next));
          setGroupId('');
          setTemplate('');
        },
        onError: (caught) => feedback.report(caught),
      },
    );
  };

  return (
    <form className="form" onSubmit={onSubmit} noValidate>
      <h3>Map a group to a template</h3>
      <p>
        Absent scope means organisation, the widest a binding reaches. Nothing preselects a scope
        for you.
      </p>
      {feedback.failure === null ? null : <Alert>{feedback.failure}</Alert>}
      {feedback.done === null ? null : <Done>{feedback.done}</Done>}
      {result === null ? null : <MappingWarnings warnings={result.warnings} />}

      {groups.isSuccess && groupItems.length === 0 ? (
        <p role="status">
          No provisioned groups to map yet. A group appears here after the identity provider pushes
          it over SCIM.
        </p>
      ) : null}

      <div className="field">
        <label htmlFor="mapping-group">Provisioned group</label>
        <select
          id="mapping-group"
          value={groupId}
          onChange={(event) => setGroupId(event.target.value)}
          required
        >
          <option value="">Choose a group…</option>
          {groupItems.map((group) => (
            <option key={group.id} value={group.id}>
              {group.display_name} ({group.member_count})
            </option>
          ))}
        </select>
      </div>

      <div className="field">
        <label htmlFor="mapping-scope">Scope</label>
        <select
          id="mapping-scope"
          value={scope}
          onChange={(event) => {
            setScope(event.target.value);
            setTemplate('');
          }}
        >
          <option value="">Organisation (every project and environment)</option>
          {options
            .filter((option) => option.level !== 'org')
            .map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
        </select>
      </div>

      <div className="field">
        <label htmlFor="mapping-template">Template</label>
        <select
          id="mapping-template"
          value={template}
          onChange={(event) => setTemplate(event.target.value)}
          required
        >
          <option value="">Choose a template…</option>
          {templates.map((option) => (
            <option key={option.id} value={option.id}>
              {option.id}
            </option>
          ))}
        </select>
      </div>

      <button
        className="btn btn--primary"
        type="submit"
        disabled={create.isPending || groupId === '' || !templateValid}
      >
        {create.isPending ? 'Mapping…' : 'Add mapping'}
      </button>
    </form>
  );
}

function scopeToTarget(scope: ScopeRef | undefined): { projectId?: string; environmentId?: string } {
  if (scope === undefined || scope.kind === 'org' || scope.kind === 'instance') {
    return {};
  }
  if (scope.kind === 'project') {
    return { projectId: scope.project };
  }
  return { projectId: scope.project, environmentId: scope.environment };
}

// --- credentials ------------------------------------------------------------

function CredentialsSection({ org, binding }: { org: string; binding: ScimBinding }) {
  const credentials = useScimCredentials(org, binding.id);
  const mint = useMintScimCredential(org, binding.id);
  const [disclosed, setDisclosed] = useSensitiveState<MintedScimCredential | null>(null);
  const items = credentials.data?.items ?? [];

  return (
    <Panel id="scim-credentials" title="Provisioning credentials">
      <p>
        The bearer tokens the identity provider presents at this binding&apos;s SCIM endpoint. The
        list holds metadata only; the token exists in the mint response and nowhere else, ever.
        Several may be live at once, which is how overlap rotation works.
      </p>

      {credentials.isError ? <Alert>{scimReadFailureText(credentials.error)}</Alert> : null}

      {credentials.isSuccess && items.length === 0 ? (
        <p role="status">None minted. Mint one below and configure it at the identity provider.</p>
      ) : null}

      <ul className="scim-credentials">
        {items.map((credential) => (
          <CredentialRow key={credential.id} org={org} binding={binding.id} credential={credential} />
        ))}
      </ul>

      <MintCredentialForm mint={mint} onMinted={setDisclosed} />

      {disclosed === null ? null : (
        <MintDialog minted={disclosed} onClose={() => setDisclosed(null)} />
      )}
    </Panel>
  );
}

function CredentialRow({
  org,
  binding,
  credential,
}: {
  org: string;
  binding: string;
  credential: ScimCredential;
}) {
  const revoke = useRevokeScimCredential(org, binding);
  const feedback = useFeedback(scimMutationFailureText);
  const revoked = credential.revoked_at !== undefined;
  const state = revoked ? 'revoked' : credential.live ? 'live' : 'expired';
  return (
    <li className="scim-credential">
      <div className="scim-credential__head">
        <span className="mono scim-credential__id">{credential.id}</span>
        <span className="badge" data-state={state}>
          {state}
        </span>
      </div>
      <dl className="scim-credential__facts">
        <dt>Created</dt>
        <dd>{isoDay(credential.created_at)}</dd>
        <dt>Expires</dt>
        <dd>{credential.expires_at === undefined ? 'never' : isoDay(credential.expires_at)}</dd>
        <dt>Last used</dt>
        <dd>{credential.last_used_at === undefined ? 'never used' : isoDay(credential.last_used_at)}</dd>
        {credential.revoked_at === undefined ? null : (
          <>
            <dt>Revoked</dt>
            <dd>{isoDay(credential.revoked_at)}</dd>
          </>
        )}
      </dl>
      {feedback.failure === null ? null : <Alert>{feedback.failure}</Alert>}
      {revoked ? null : (
        <div className="scim-credential__actions">
          <button
            className="btn btn--danger"
            type="button"
            disabled={revoke.isPending}
            aria-label={`Revoke credential ${credential.id}`}
            onClick={() => {
              feedback.clear();
              revoke.mutate(credential.id, { onError: (caught) => feedback.report(caught) });
            }}
          >
            {revoke.isPending ? 'Revoking…' : 'Revoke'}
          </button>
          <p className="scim-credential__note">Revoking bites at the provider&apos;s next request.</p>
        </div>
      )}
    </li>
  );
}

function MintCredentialForm({
  mint,
  onMinted,
}: {
  mint: ReturnType<typeof useMintScimCredential>;
  onMinted: (minted: MintedScimCredential) => void;
}) {
  const [proof, setProof] = useSensitiveState('');
  const [indefinite, setIndefinite] = useState(false);

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (proof.trim() === '' || mint.pending) {
      return;
    }
    void mint
      .mint({ proof: proof.trim(), ...(indefinite ? { indefinite: true } : {}) })
      .then(
        (minted) => {
          onMinted(minted);
          setProof('');
          setIndefinite(false);
        },
        () => {
          // Surfaced from mint.error below; a retry needs a fresh proof.
          setProof('');
        },
      );
  };

  return (
    <form className="form" onSubmit={onSubmit} noValidate>
      <h3>Mint a credential</h3>
      <p>
        Minting reauthenticates: present a current authenticator code, or your account password if
        you have no factor enrolled. It is never stored and never appears again.
      </p>
      {mint.error !== null ? <Alert>{scimMintFailureText(mint.error)}</Alert> : null}
      <div className="field">
        <label htmlFor="scim-proof">Reauthentication proof</label>
        <input
          id="scim-proof"
          type="password"
          value={proof}
          onChange={(event) => setProof(event.target.value)}
          maxLength={512}
          autoComplete="one-time-code"
          required
        />
      </div>
      <div className="field chk">
        <input
          id="scim-indefinite"
          type="checkbox"
          checked={indefinite}
          onChange={(event) => setIndefinite(event.target.checked)}
        />
        <label htmlFor="scim-indefinite">
          Never expires (refused unless this instance allows indefinite credentials)
        </label>
      </div>
      <button
        className="btn btn--primary"
        type="submit"
        disabled={mint.pending || proof.trim() === ''}
      >
        {mint.pending ? 'Minting…' : 'Mint credential'}
      </button>
    </form>
  );
}

/**
 * The display-once ceremony. The token lives only in this dialog's props while
 * it is open: copy is best-effort, a stored-confirmation gates dismissal, and a
 * Back press or reload is routed through the same guard so the one disclosure is
 * not lost.
 */
function MintDialog({
  minted,
  onClose,
}: {
  minted: MintedScimCredential;
  onClose: () => void;
}) {
  const confirmation = useRef<HTMLInputElement>(null);
  const dialog = useModalDialog(confirmation);
  const [stored, setStored] = useState(false);
  const [heldBack, setHeldBack] = useState(false);
  const [copyStatus, setCopyStatus] = useState<string | null>(null);

  const dismiss = () => {
    if (!stored) {
      setHeldBack(true);
      return;
    }
    onClose();
  };

  useNavigationGuard(!stored, dismiss);

  return (
    <dialog
      className="ceremony"
      aria-labelledby="scim-mint-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        dismiss();
      }}
    >
      <h2 className="ceremony__title" id="scim-mint-title">
        Provisioning credential minted, shown exactly once
      </h2>
      {minted.rotated ? (
        <p className="notice" role="status">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            This joined an already-live credential; that is overlap rotation. Update the identity
            provider to this value, then revoke the old one.
          </span>
        </p>
      ) : null}
      <p className="mono machine__token">{minted.token}</p>
      <p className="ceremony__cap" role="status">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          This value is never retrievable again. The list shows metadata only. Configure it at the
          identity provider now; if it is lost, revoke this credential and mint a fresh one.
        </span>
      </p>
      <button
        className="btn"
        type="button"
        onClick={async () => {
          const result = await writeClipboard(minted.token);
          setCopyStatus(
            result === 'ok'
              ? 'Copied. The clipboard is now the only copy outside the identity provider.'
              : 'This browser refused clipboard access, so nothing was copied.',
          );
        }}
      >
        Copy to clipboard
      </button>
      {copyStatus === null ? null : (
        <p className="notice" role="status">
          <span className="alert__glyph" aria-hidden="true">
            ⧉
          </span>
          <span>{copyStatus}</span>
        </p>
      )}
      <div className="field chk">
        <input
          id="scim-stored"
          type="checkbox"
          ref={confirmation}
          checked={stored}
          onChange={(event) => {
            setStored(event.target.checked);
            if (event.target.checked) {
              setHeldBack(false);
            }
          }}
        />
        <label htmlFor="scim-stored">I have configured this credential at the identity provider.</label>
      </div>
      {heldBack ? (
        <Alert>Store the credential first. It cannot be shown again once this closes.</Alert>
      ) : null}
      <button className="btn btn--primary" type="button" disabled={!stored} onClick={dismiss}>
        Done
      </button>
    </dialog>
  );
}

// --- directory --------------------------------------------------------------

function DirectorySection({ org, binding }: { org: string; binding: ScimBinding }) {
  const users = useScimDirectoryUsers(org, binding.id);
  const groups = useScimDirectoryGroups(org, binding.id);
  const [filter, setFilter] = useState('');

  const userItems = users.data?.items ?? [];
  const groupItems = groups.data?.items ?? [];
  const needle = filter.trim().toLowerCase();
  // The server bounds the list; the filter narrows what came back, in the
  // browser. The directory takes no search parameter, so this invents none.
  const shownUsers =
    needle === ''
      ? userItems
      : userItems.filter((user) => user.user_name.toLowerCase().includes(needle));

  return (
    <Panel id="scim-directory" title="Directory">
      <p>
        What the identity provider has provisioned into this organisation. A deprovisioned user is
        flagged loudly: the provider declared them gone, but grants made by hand here remain and are
        still usable until a human decides about them.
      </p>

      {users.isError ? <Alert>{scimReadFailureText(users.error)}</Alert> : null}
      {groups.isError ? <Alert>{scimReadFailureText(groups.error)}</Alert> : null}

      <div className="field">
        <label htmlFor="scim-directory-filter">Filter users by name</label>
        <input
          id="scim-directory-filter"
          type="search"
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          autoComplete="off"
        />
      </div>

      <h3>Users ({userItems.length})</h3>
      {users.isSuccess && userItems.length === 0 ? (
        <p role="status">No users provisioned yet.</p>
      ) : null}
      {users.isSuccess && userItems.length > 0 && shownUsers.length === 0 ? (
        <p role="status">No provisioned user matches that filter.</p>
      ) : null}
      <ul className="scim-directory-users">
        {shownUsers.map((user) => (
          <DirectoryUserRow key={user.id} user={user} />
        ))}
      </ul>

      <h3>Groups ({groupItems.length})</h3>
      {groups.isSuccess && groupItems.length === 0 ? (
        <p role="status">No groups provisioned yet.</p>
      ) : null}
      <ul className="scim-directory-groups">
        {groupItems.map((group) => (
          <li key={group.id} className="scim-directory-group">
            <span className="scim-directory-group__name">{group.display_name}</span>
            <span className="scim-directory-group__count">{group.member_count} members</span>
            <span className="mono scim-directory-group__id">{group.id}</span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

export function DirectoryUserRow({ user }: { user: ScimDirectoryUser }) {
  return (
    <li className="scim-directory-user">
      <div className="scim-directory-user__head">
        <span className="scim-directory-user__name">{user.user_name}</span>
        {user.active ? (
          <span className="badge" data-state="active">
            active
          </span>
        ) : user.attention.some((item) => item.state === 'manual_grants_remain') ? (
          <span className="badge" data-state="inactive">
            <span aria-hidden="true">! </span>deprovisioned, manual grants remain
          </span>
        ) : (
          <span className="badge" data-state="inactive">
            deprovisioned
          </span>
        )}
      </div>
      <p className="scim-directory-user__groups">
        {user.groups.length === 0
          ? 'in no groups'
          : user.groups.length === 1
            ? 'in 1 group'
            : `in ${String(user.groups.length)} groups`}
      </p>
      <AttentionList attention={user.attention} subjectPrefix={`${user.user_name} `} />
    </li>
  );
}
