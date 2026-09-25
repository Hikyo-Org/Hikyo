import { useId, useRef, useState, type FormEvent, type RefObject } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { templatesAt } from '../api/access.ts';
import type { RoleTemplateId } from '../api/access-templates.ts';
import { useAuthMethods } from '../api/account.ts';
import { ApiError, transportRefusalText } from '../api/client.ts';
import {
  deleteRegistrationPolicy,
  inactiveText,
  preconditionProvider,
  putRegistrationPolicy,
  registrationFailureText,
  registrationKey,
  requirementLine,
  resaveBody,
  signupLink,
  useRegistrationPolicy,
  type EntryKind,
  type PolicyView,
  type RegistrationPolicyPutRequest,
  type RegistrationScope,
} from '../api/registration.ts';
import { writeClipboard } from '../app/clipboard.ts';
import { Alert } from '../ui/Alert.tsx';
import { ProofDialog } from '../ui/auth/ProofDialog.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Input } from '../ui/Input.tsx';
import { Radio } from '../ui/Radio.tsx';
import { Select } from '../ui/Select.tsx';
import { Panel } from './Sections.tsx';

/**
 * The Open registration panel and its editor (#606, #579 d10; locked
 * prototype social-signin iteration 2). It lives on Members at organisation
 * and instance scope, beside invite: a policy is a standing delegation of
 * the viewer's own `manage-members`, re-checked against the authority's
 * current grants at every sign-up.
 *
 * Every mutation (save, re-save as authority, close) is reauthentication-
 * gated: the blue "Confirm it's you" step asks for a fresh code (or the
 * password where no authenticator stands) and nothing else. No session is
 * purged. The inactive cause renders here and only here; the public login
 * page says only "Sign-up is paused." (#587 d3).
 */
export function OpenRegistrationPanel({
  scope,
  scopeName,
  origin,
  authorityName,
  onChanged,
}: {
  scope: RegistrationScope;
  scopeName: string;
  origin: string;
  /** Renders a principal id for a human (the member list's names). */
  authorityName: (principal: string) => string;
  onChanged: (text: string) => void;
}) {
  const policy = useRegistrationPolicy(scope, true);
  const queries = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [gate, setGate] = useState<null | { what: string; run: (proof: string) => Promise<string> }>(null);
  const [gateFailure, setGateFailure] = useState<string | null>(null);
  const [gatePending, setGatePending] = useState(false);
  const [copied, setCopied] = useState<string | null>(null);
  const current = policy.data ?? null;

  const refresh = async (text: string) => {
    await queries.invalidateQueries({ queryKey: registrationKey(scope) });
    onChanged(text);
  };
  const openGate = (what: string, run: (proof: string) => Promise<string>) => {
    setGateFailure(null);
    setGate({ what, run });
  };

  return (
    <Panel id="members-registration" title="Open registration">
      {policy.isPending ? <p role="status">Loading the registration policy…</p> : null}
      {policy.isError ? (
        <Alert>{transportRefusalText(policy.error) ?? 'The registration policy could not be read. Reload to try again.'}</Alert>
      ) : null}
      {policy.isSuccess && current === null ? (
        <div className="registration">
          <p className="registration__status">
            <Badge>closed</Badge>{' '}
            {scope.kind === 'instance'
              ? 'No one can sign up on this instance without an invitation.'
              : `No one can sign up into ${scopeName} without an invitation.`}
          </p>
          <p className="registration__note">
            No policy means closed. Opening registration is an audited, reauth-gated change; the
            policy is a standing delegation re-checked against your grants at every sign-up.
          </p>
          <div className="panel__actions">
            <Button type="button" onClick={() => setEditing(true)}>
              Open registration…
            </Button>
          </div>
        </div>
      ) : null}
      {current === null ? null : (
        <PolicySummary
          policy={current}
          scope={scope}
          scopeName={scopeName}
          origin={origin}
          authority={current.authority === '' ? 'no one' : authorityName(current.authority)}
          copied={copied}
          onCopy={async (link) => {
            setCopied((await writeClipboard(link)) === 'ok' ? 'Sign-up link copied.' : 'Copy refused by the browser; select the link instead.');
          }}
          onEdit={() => setEditing(true)}
          onResave={() =>
            openGate('re-save this policy as its authority', async (proof) => {
              await putRegistrationPolicy(scope, resaveBody(current, proof));
              return 'Policy saved. You are now its authority.';
            })
          }
          onClose={() =>
            openGate('close registration', async (proof) => {
              await deleteRegistrationPolicy(scope, proof);
              return 'Registration closed. The policy and its pending sign-ups are deleted.';
            })
          }
        />
      )}
      {editing ? (
        <RegistrationEditor
          scope={scope}
          scopeName={scopeName}
          current={current}
          onCancel={() => setEditing(false)}
          onSaved={async (saved, created) => {
            setEditing(false);
            await refresh(
              `Policy saved (registration.policy_${created ? 'created' : 'updated'}). You are its authority${saved.state.state === 'active' ? '.' : '; it is inactive, see the cause below.'}`,
            );
          }}
        />
      ) : null}
      {gate === null ? null : (
        <ProofDialog
          lede={`To ${gate.what}, enter the code from your authenticator, or your password if you have none. Fresh proof, every time.`}
          field="code-or-password"
          reauth
          pending={gatePending}
          failure={gateFailure}
          onCancel={() => setGate(null)}
          onSubmit={(proof, clear) => {
            setGatePending(true);
            setGateFailure(null);
            gate
              .run(proof)
              .then(async (text) => {
                setGate(null);
                await refresh(text);
              })
              .catch((error: unknown) => {
                setGateFailure(registrationFailureText(error));
                clear();
              })
              .finally(() => setGatePending(false));
          }}
        />
      )}
    </Panel>
  );
}

function landingText(policy: PolicyView, scopeName: string): string {
  const landing = policy.landing;
  switch (landing.kind) {
    case 'org-template':
      return `${scopeName} · ${landing.template} template`;
    case 'none':
      return 'no organisation · zero grants until an administrator grants access';
    case 'fresh-org': {
      if (policy.mintedOrgs === null) throw new Error('a fresh-org policy arrived without its org count');
      const count = policy.mintedOrgs;
      return `a new organisation per sign-up · ${String(count)} / ${String(landing.cap)} minted${count >= landing.cap ? ' · cap reached, sign-ups refused' : ''}`;
    }
  }
}

function PolicySummary({
  policy,
  scope,
  scopeName,
  origin,
  authority,
  copied,
  onCopy,
  onEdit,
  onResave,
  onClose,
}: {
  policy: PolicyView;
  scope: RegistrationScope;
  scopeName: string;
  origin: string;
  authority: string;
  copied: string | null;
  onCopy: (link: string) => void;
  onEdit: () => void;
  onResave: () => void;
  onClose: () => void;
}) {
  const state = policy.state;
  const link = scope.kind === 'org' ? signupLink(origin, scope.org) : null;
  // Re-saving makes the editor the authority, which cures only an authority
  // cause; a failing precondition needs its own fix.
  const resavable = state.state === 'inactive' &&
    (state.inactive_cause === 'authority-lost' || state.inactive_cause === 'authority-unassigned');
  return (
    <div className="registration">
      <p className="registration__status">
        {state.state === 'inactive' ? (
          <Badge tone="danger">{`inactive · ${state.inactive_cause}`}</Badge>
        ) : (
          <Badge tone="ok">active</Badge>
        )}
      </p>
      {state.state === 'inactive' ? <Alert>{inactiveText(state, authority)}</Alert> : null}
      <dl className="registration__facts">
        <dt>Who may sign up</dt>
        <dd>
          <ul className="registration__entries">
            {policy.external.map((entry) => (
              <li key={`${entry.provider.kind}:${entry.provider.slug}`}>
                <span className="registration__entry">
                  {entry.name} <Badge mono>{entry.provider.kind}</Badge>
                </span>
                {entry.allow === null ? null : (
                  <span className="registration__allow mono">{`${entry.allow.claim} ∈ {${entry.allow.values.join(', ')}}`}</span>
                )}
                <span className="registration__requirement">{requirementLine(entry.provider.kind)}</span>
              </li>
            ))}
            {policy.local === null ? null : (
              <li>
                <span className="registration__entry">
                  Email + password <Badge mono>local</Badge>
                </span>
                <span className="registration__allow">
                  {policy.local.domains.length === 0 ? 'any address' : `@${policy.local.domains.join(', @')} only`}
                </span>
                <span className="registration__requirement">{requirementLine('local')}</span>
              </li>
            )}
          </ul>
        </dd>
        <dt>Landing</dt>
        <dd>{landingText(policy, scopeName)}</dd>
        <dt>Authority</dt>
        <dd>
          {authority} · re-checked against their current grants at every sign-up · any edit makes the
          editor the authority
        </dd>
        {link === null ? null : (
          <>
            <dt>Sign-up link</dt>
            <dd>
              <code className="registration__link">{link}</code>
            </dd>
          </>
        )}
      </dl>
      {copied === null ? null : <p role="status">{copied}</p>}
      <div className="panel__actions">
        {link === null ? null : (
          <Button type="button" onClick={() => onCopy(link)}>
            Copy sign-up link
          </Button>
        )}
        {resavable ? (
          <Button type="button" className="btn--reauth" onClick={onResave}>
            Re-save as authority
          </Button>
        ) : null}
        <Button type="button" onClick={onEdit}>
          Edit…
        </Button>
        <Button type="button" variant="danger" onClick={onClose}>
          Close registration
        </Button>
      </div>
      <p className="registration__note">
        Sign-ups arrive under registration.* in the {scope.kind === 'instance' ? 'instance' : 'tenant'}{' '}
        trail. Refusals (no verified email, not on the allowlist, budget, cap) are read there; the
        panel shows no per-identity state.
      </p>
    </div>
  );
}

type EntryDraft = { on: boolean; claim: string; values: string };

type Draft = {
  entries: Record<string, EntryDraft>;
  local: boolean;
  domains: string;
  landing: 'org-template' | 'none' | 'fresh-org';
  template: RoleTemplateId;
  cap: string;
};

type ProviderChoice = { key: string; kind: EntryKind; slug: string; name: string };

function draftOf(scope: RegistrationScope, current: PolicyView | null): Draft {
  const entries: Record<string, EntryDraft> = {};
  for (const entry of current?.external ?? []) {
    entries[`${entry.provider.kind}:${entry.provider.slug}`] = {
      on: true,
      claim: entry.allow?.claim ?? '',
      values: entry.allow?.values.join(', ') ?? '',
    };
  }
  const landing = current?.landing;
  return {
    entries,
    local: current !== null && current.local !== null,
    domains: current?.local?.domains.join(', ') ?? '',
    landing: landing?.kind ?? (scope.kind === 'org' ? 'org-template' : 'none'),
    template: landing?.kind === 'org-template' ? landing.template : 'viewer',
    cap: landing?.kind === 'fresh-org' ? String(landing.cap) : '100',
  };
}

function splitList(text: string): string[] {
  return text
    .split(',')
    .map((part) => part.trim())
    .filter((part) => part !== '');
}

function entryKind(kind: string): EntryKind | null {
  switch (kind) {
    case 'oidc':
    case 'oauth2':
      return kind;
    default:
      return null;
  }
}

/**
 * The editor. One policy per scope: who may sign up (instance-enabled
 * providers, the local entry) and where they land. Validation that needs no
 * server runs here first; what only the server knows (a provider without the
 * email scope, a missing mailer) comes back as a 400 naming the row, shown on
 * that row with focus moved to it.
 */
function RegistrationEditor({
  scope,
  scopeName,
  current,
  onCancel,
  onSaved,
}: {
  scope: RegistrationScope;
  scopeName: string;
  current: PolicyView | null;
  onCancel: () => void;
  onSaved: (saved: PolicyView, created: boolean) => void;
}) {
  const methods = useAuthMethods();
  const [draft, setDraft] = useState<Draft>(() => draftOf(scope, current));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [proofing, setProofing] = useState(false);
  const [proofFailure, setProofFailure] = useState<string | null>(null);
  const [proofPending, setProofPending] = useState(false);
  const formId = useId();
  // The element focus returns to when the editor re-opens on a refusal: the
  // errored row's control, or the form-level alert.
  const errorInput = useRef<HTMLInputElement>(null);
  const errorAlert = useRef<HTMLDivElement>(null);
  const instance = scope.kind === 'instance';
  // Org-scope entries pick from the instance-enabled providers (#579 d5);
  // an entry the current policy names but that is no longer enabled stays
  // listed so it can be removed. SAML is not a sign-up kind.
  const enabled: ProviderChoice[] = [];
  for (const provider of methods.data?.providers ?? []) {
    const kind = entryKind(provider.kind);
    if (kind !== null) enabled.push({ key: `${kind}:${provider.slug}`, kind, slug: provider.slug, name: provider.display_name });
  }
  const stale: ProviderChoice[] = (current?.external ?? [])
    .map((entry) => ({ key: `${entry.provider.kind}:${entry.provider.slug}`, kind: entry.provider.kind, slug: entry.provider.slug, name: entry.name }))
    .filter((entry) => !enabled.some((candidate) => candidate.key === entry.key));
  const providers = [...enabled, ...stale];
  const firstError = [...providers.map((provider) => provider.key), 'local', 'cap', 'form'].find((key) => errors[key] !== undefined);

  const entry = (key: string): EntryDraft => draft.entries[key] ?? { on: false, claim: '', values: '' };
  const setEntry = (key: string, next: Partial<EntryDraft>) =>
    setDraft((prev) => ({ ...prev, entries: { ...prev.entries, [key]: { ...entry(key), ...next } } }));
  const focusFor = (key: string): RefObject<HTMLInputElement | null> | undefined =>
    key === firstError ? errorInput : undefined;

  const build = (): { body: Omit<RegistrationPolicyPutRequest, 'proof'>; errors: Record<string, string> } => {
    const found: Record<string, string> = {};
    const external = providers
      .filter((provider) => entry(provider.key).on)
      .map((provider) => {
        const e = entry(provider.key);
        const claim = e.claim.trim();
        const values = splitList(e.values);
        if (claim === 'email') {
          found[provider.key] = 'email is not accepted as an allowlist claim: it is never a linking key.';
        } else if ((claim === '') !== (values.length === 0)) {
          found[provider.key] = 'An allowlist claim needs at least one accepted value, and values need a claim.';
        }
        return {
          provider: { kind: provider.kind, slug: provider.slug },
          ...(claim === '' ? {} : { claim, values }),
        };
      });
    if (external.length === 0 && !draft.local) {
      found['form'] = 'Admit at least one way to sign up, or close registration instead.';
    }
    const cap = Number(draft.cap);
    if (draft.landing === 'fresh-org' && !(Number.isInteger(cap) && cap > 0)) {
      found['cap'] = 'A cap of at least 1 is required for this landing.';
    }
    const landing =
      draft.landing === 'org-template'
        ? { kind: 'org-template' as const, template: draft.template }
        : draft.landing === 'fresh-org'
          ? { kind: 'fresh-org' as const, cap }
          : { kind: 'none' as const };
    return {
      body: {
        external,
        ...(draft.local ? { local: { domains: splitList(draft.domains).map((d) => d.toLowerCase()) } } : {}),
        landing,
      },
      errors: found,
    };
  };

  if (proofing) {
    return (
      <ProofDialog
        lede="To save the registration policy, enter the code from your authenticator, or your password if you have none. Fresh proof, every time."
        field="code-or-password"
        reauth
        pending={proofPending}
        failure={proofFailure}
        onCancel={() => setProofing(false)}
        onSubmit={(proof, clear) => {
          setProofPending(true);
          setProofFailure(null);
          putRegistrationPolicy(scope, { ...build().body, proof })
            .then((saved) => onSaved(saved, current === null))
            .catch((error: unknown) => {
              // Anything the policy itself got wrong goes back to the editor,
              // on its row; a refused proof stays on the proof step.
              if (error instanceof ApiError && error.status === 400) {
                const detail = error.detail ?? '';
                const provider = preconditionProvider(detail);
                const key = provider !== null ? provider : detail.startsWith('mailer-unconfigured') ? 'local' : 'form';
                setErrors({ [key]: registrationFailureText(error) });
                setProofing(false);
                return;
              }
              setProofFailure(registrationFailureText(error));
              clear();
            })
            .finally(() => setProofPending(false));
        }}
      />
    );
  }

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const built = build();
    setErrors(built.errors);
    if (Object.keys(built.errors).length === 0) {
      setProofFailure(null);
      setProofing(true);
    }
  };

  return (
    <Dialog
      title={`${scopeName} · open registration`}
      size="wide"
      lede="One policy per scope. Who may sign up, and where they land. Saving needs fresh proof and makes you the policy's authority."
      initialFocus={firstError === undefined ? undefined : firstError === 'form' ? errorAlert : errorInput}
      onCancel={(event) => {
        event.preventDefault();
        onCancel();
      }}
      actions={
        <>
          <Button type="button" onClick={onCancel}>
            Cancel
          </Button>
          <Button type="submit" form={formId} className="btn--reauth">
            Save
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} noValidate className="registration-editor">
        {errors['form'] === undefined ? null : (
          <div tabIndex={-1} ref={errorAlert}>
            <Alert>{errors['form']}</Alert>
          </div>
        )}
        <fieldset className="registration-editor__group">
          <legend>Who may sign up</legend>
          {methods.isPending ? <p role="status">Loading the instance providers…</p> : null}
          {providers.map((provider) => {
            const e = entry(provider.key);
            return (
              <div className="registration-editor__entry" key={provider.key}>
                <div className="registration-editor__top">
                  <Checkbox
                    ref={focusFor(provider.key)}
                    label={provider.name}
                    checked={e.on}
                    aria-invalid={errors[provider.key] === undefined ? undefined : true}
                    onChange={(event) => setEntry(provider.key, { on: event.target.checked })}
                  />
                  <Badge mono>{provider.kind}</Badge>
                </div>
                {e.on && provider.kind === 'oidc' ? (
                  <div className="registration-editor__sub">
                    <Input
                      label="Allowlist claim"
                      placeholder="hd"
                      value={e.claim}
                      onChange={(event) => setEntry(provider.key, { claim: event.target.value })}
                    />
                    <Input
                      label="Accepted values"
                      placeholder="acme.example, acme.test"
                      value={e.values}
                      onChange={(event) => setEntry(provider.key, { values: event.target.value })}
                    />
                  </div>
                ) : null}
                {e.on ? <p className="registration__requirement">{requirementLine(provider.kind)}</p> : null}
                {errors[provider.key] === undefined ? null : <Alert>{errors[provider.key]}</Alert>}
              </div>
            );
          })}
          <div className="registration-editor__entry">
            <div className="registration-editor__top">
              <Checkbox
                ref={focusFor('local')}
                label="Email + password"
                checked={draft.local}
                aria-invalid={errors['local'] === undefined ? undefined : true}
                onChange={(event) => setDraft((prev) => ({ ...prev, local: event.target.checked }))}
              />
              <Badge mono>local</Badge>
            </div>
            {draft.local ? (
              <>
                <Input
                  label="Email domain allowlist"
                  hint="Comma-separated; empty admits any address."
                  placeholder="acme.example"
                  value={draft.domains}
                  onChange={(event) => setDraft((prev) => ({ ...prev, domains: event.target.value }))}
                />
                <p className="registration__requirement">{requirementLine('local')}</p>
              </>
            ) : null}
            {errors['local'] === undefined ? null : <Alert>{errors['local']}</Alert>}
          </div>
        </fieldset>
        <fieldset className="registration-editor__group">
          <legend>Landing</legend>
          {instance ? (
            <>
              <Radio
                name={`${formId}-landing`}
                label="No organisation: the account exists with zero grants; an administrator grants access later"
                checked={draft.landing === 'none'}
                onChange={() => setDraft((prev) => ({ ...prev, landing: 'none' }))}
              />
              <Radio
                name={`${formId}-landing`}
                label="A new organisation per sign-up: the signer becomes its first administrator, under your org.create grant"
                checked={draft.landing === 'fresh-org'}
                onChange={() => setDraft((prev) => ({ ...prev, landing: 'fresh-org' }))}
              />
              {draft.landing === 'fresh-org' ? (
                <Input
                  ref={focusFor('cap')}
                  label="Cap on organisations minted"
                  type="number"
                  min={1}
                  value={draft.cap}
                  error={errors['cap']}
                  hint={errors['cap'] === undefined && current !== null && current.mintedOrgs !== null
                    ? `${String(current.mintedOrgs)} minted so far. Reaching the cap refuses sign-ups until an idle org is deleted.`
                    : undefined}
                  onChange={(event) => setDraft((prev) => ({ ...prev, cap: event.target.value }))}
                />
              ) : null}
            </>
          ) : (
            <Select
              label={`Role template in ${scopeName}`}
              hint="Bounded by your own grants: you can hand out only what you hold."
              value={draft.template}
              onChange={(event) => {
                const picked = templatesAt('org').find((template) => template.id === event.target.value);
                if (picked !== undefined) setDraft((prev) => ({ ...prev, template: picked.id }));
              }}
            >
              {templatesAt('org').map((template) => (
                <option key={template.id} value={template.id}>
                  {template.id}
                </option>
              ))}
            </Select>
          )}
        </fieldset>
      </form>
    </Dialog>
  );
}
