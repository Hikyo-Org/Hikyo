import { useId, useRef, useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { templatesAt } from '../api/access.ts';
import type { RoleTemplateId } from '../api/access-templates.ts';
import { useAuthMethods } from '../api/account.ts';
import {
  deleteRegistrationPolicy,
  inactiveText,
  preconditionProvider,
  providerLabel,
  putRegistrationPolicy,
  registrationFailureText,
  registrationKey,
  requirementLine,
  resaveBody,
  signupLink,
  useRegistrationPolicy,
  type RegistrationPolicy,
  type RegistrationPolicyPutRequest,
  type RegistrationScope,
} from '../api/registration.ts';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { writeClipboard } from '../app/clipboard.ts';
import { ApiError } from '../api/client.ts';
import { Alert } from '../ui/Alert.tsx';
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
  const [copied, setCopied] = useState<string | null>(null);
  const instance = scope.kind === 'instance';
  const current = policy.data ?? null;

  const refresh = async (text: string) => {
    await queries.invalidateQueries({ queryKey: registrationKey(scope) });
    onChanged(text);
  };

  return (
    <Panel id="members-registration" title="Open registration">
      {policy.isPending ? <p role="status">Loading the registration policy…</p> : null}
      {policy.isError ? <Alert>{registrationFailureText(policy.error)}</Alert> : null}
      {policy.isSuccess && current === null ? (
        <div className="registration">
          <p className="registration__status">
            <Badge>closed</Badge>{' '}
            {instance
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
          authority={authorityName(current.authority_principal_id)}
          copied={copied}
          onCopy={async (link) => {
            setCopied((await writeClipboard(link)) === 'ok' ? 'Sign-up link copied.' : 'Copy refused by the browser; select the link instead.');
          }}
          onEdit={() => setEditing(true)}
          onResave={() =>
            setGate({
              what: 're-save this policy as its authority',
              run: async (proof) => {
                await putRegistrationPolicy(scope, { ...resaveBody(current), proof });
                return 'Policy saved. You are now its authority.';
              },
            })
          }
          onClose={() =>
            setGate({
              what: 'close registration',
              run: async (proof) => {
                await deleteRegistrationPolicy(scope, proof);
                return 'Registration closed. The policy and its pending sign-ups are deleted.';
              },
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
              `Policy saved (registration.policy_${created ? 'created' : 'updated'}). You are its authority${saved.state === 'active' ? '.' : '; it is inactive, see the cause below.'}`,
            );
          }}
        />
      ) : null}
      {gate === null ? null : (
        <ProofDialog
          what={gate.what}
          onCancel={() => setGate(null)}
          onProof={async (proof) => {
            const text = await gate.run(proof);
            setGate(null);
            await refresh(text);
          }}
        />
      )}
    </Panel>
  );
}

function landingText(policy: RegistrationPolicy, scopeName: string): string {
  const landing = policy.landing;
  if (landing.kind === 'org-template') return `${scopeName} · ${landing.template ?? ''} template`;
  if (landing.kind === 'none') return 'no organisation · zero grants until an administrator grants access';
  const count = policy.fresh_org_count ?? 0;
  const cap = landing.cap ?? 0;
  return `a new organisation per sign-up · ${String(count)} / ${String(cap)} minted${count >= cap ? ' · cap reached, sign-ups refused' : ''}`;
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
  policy: RegistrationPolicy;
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
  const inactive = policy.state === 'inactive';
  const link = scope.kind === 'org' ? signupLink(origin, scope.org) : null;
  return (
    <div className="registration">
      <p className="registration__status">
        {inactive ? (
          <Badge tone="danger">{`inactive · ${policy.inactive_cause ?? ''}`}</Badge>
        ) : (
          <Badge tone="ok">active</Badge>
        )}
      </p>
      {inactive ? <Alert>{inactiveText(policy, authority)}</Alert> : null}
      <dl className="registration__facts">
        <dt>Who may sign up</dt>
        <dd>
          <ul className="registration__entries">
            {policy.external.map((entry) => (
              <li key={`${entry.provider.kind}:${entry.provider.slug}`}>
                <span className="registration__entry">
                  {providerLabel(entry)} <Badge mono>{entry.provider.kind}</Badge>
                </span>
                {entry.claim === undefined ? null : (
                  <span className="registration__allow mono">{`${entry.claim} ∈ {${(entry.values ?? []).join(', ')}}`}</span>
                )}
                <span className="registration__requirement">{requirementLine(entry.provider.kind)}</span>
              </li>
            ))}
            {policy.local === undefined ? null : (
              <li>
                <span className="registration__entry">
                  Email + password <Badge mono>local</Badge>
                </span>
                <span className="registration__allow">
                  {(policy.local.domains ?? []).length === 0
                    ? 'any address'
                    : `@${(policy.local.domains ?? []).join(', @')} only`}
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
        {/* Re-saving makes the editor the authority, which cures only an
            authority cause; a failing precondition needs its own fix. */}
        {policy.inactive_cause === 'authority-lost' || policy.inactive_cause === 'authority-unassigned' ? (
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

function draftOf(scope: RegistrationScope, current: RegistrationPolicy | null): Draft {
  const entries: Record<string, EntryDraft> = {};
  for (const entry of current?.external ?? []) {
    entries[`${entry.provider.kind}:${entry.provider.slug}`] = {
      on: true,
      claim: entry.claim ?? '',
      values: (entry.values ?? []).join(', '),
    };
  }
  return {
    entries,
    local: current?.local !== undefined,
    domains: (current?.local?.domains ?? []).join(', '),
    landing: current?.landing.kind ?? (scope.kind === 'org' ? 'org-template' : 'none'),
    template: current?.landing.template ?? 'viewer',
    cap: String(current?.landing.cap ?? 100),
  };
}

function splitList(text: string): string[] {
  return text
    .split(',')
    .map((part) => part.trim())
    .filter((part) => part !== '');
}

/**
 * The editor. One policy per scope: who may sign up (instance-enabled
 * providers, the local entry) and where they land. Validation that needs no
 * server runs here first; what only the server knows (a provider without the
 * email scope, a missing mailer) comes back as a 400 naming the row, and is
 * shown on that row.
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
  current: RegistrationPolicy | null;
  onCancel: () => void;
  onSaved: (saved: RegistrationPolicy, created: boolean) => void;
}) {
  const methods = useAuthMethods();
  const [draft, setDraft] = useState<Draft>(() => draftOf(scope, current));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [proofing, setProofing] = useState(false);
  const formId = useId();
  const instance = scope.kind === 'instance';
  // Org-scope entries pick from the instance-enabled providers (#579 d5);
  // an entry the current policy names but that is no longer enabled stays
  // listed so it can be removed.
  const enabled = (methods.data?.providers ?? [])
    .filter((provider) => provider.kind === 'oidc' || provider.kind === 'oauth2')
    .map((provider) => ({ key: `${provider.kind}:${provider.slug}`, kind: provider.kind, slug: provider.slug, name: provider.display_name }));
  const stale = (current?.external ?? [])
    .map((entry) => ({ key: `${entry.provider.kind}:${entry.provider.slug}`, kind: entry.provider.kind, slug: entry.provider.slug, name: providerLabel(entry) }))
    .filter((entry) => !enabled.some((candidate) => candidate.key === entry.key));
  const providers = [...enabled, ...stale];

  const entry = (key: string): EntryDraft => draft.entries[key] ?? { on: false, claim: '', values: '' };
  const setEntry = (key: string, next: Partial<EntryDraft>) =>
    setDraft((prev) => ({ ...prev, entries: { ...prev.entries, [key]: { ...entry(key), ...next } } }));

  const build = (): { body: RegistrationPolicyPutRequest; errors: Record<string, string> } => {
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
        what="save the registration policy"
        onCancel={() => setProofing(false)}
        onProof={async (proof) => {
          try {
            const saved = await putRegistrationPolicy(scope, { ...build().body, proof });
            onSaved(saved, current === null);
          } catch (error) {
            // Proof refusals stay on the proof step; anything the policy
            // itself got wrong goes back to the editor, on its row.
            if (error instanceof ApiError && error.status === 400) {
              const detail = error.detail ?? '';
              const provider = preconditionProvider(detail);
              setErrors(provider === null
                ? detail.startsWith('mailer-unconfigured') ? { local: registrationFailureText(error) } : { form: registrationFailureText(error) }
                : { [provider]: registrationFailureText(error) });
              setProofing(false);
              return;
            }
            throw error;
          }
        }}
      />
    );
  }

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const built = build();
    setErrors(built.errors);
    if (Object.keys(built.errors).length === 0) setProofing(true);
  };

  return (
    <Dialog
      title={`${scopeName} · open registration`}
      size="wide"
      lede="One policy per scope. Who may sign up, and where they land. Saving needs fresh proof and makes you the policy's authority."
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
        {errors['form'] === undefined ? null : <Alert>{errors['form']}</Alert>}
        <fieldset className="registration-editor__group">
          <legend>Who may sign up</legend>
          {methods.isPending ? <p role="status">Loading the instance providers…</p> : null}
          {providers.map((provider) => {
            const e = entry(provider.key);
            return (
              <div className="registration-editor__entry" key={provider.key}>
                <div className="registration-editor__top">
                  <Checkbox
                    label={provider.name}
                    checked={e.on}
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
                label="Email + password"
                checked={draft.local}
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
                  label="Cap on organisations minted"
                  type="number"
                  min={1}
                  value={draft.cap}
                  error={errors['cap']}
                  hint={errors['cap'] === undefined
                    ? `${String(current?.fresh_org_count ?? 0)} minted so far. Reaching the cap refuses sign-ups until an idle org is deleted.`
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

/**
 * The blue "Confirm it's you" step every policy mutation takes (#579 d8):
 * fresh proof, spent by the one write it authorizes. The proof is
 * component-owned plaintext and dies with the dialog.
 */
function ProofDialog({
  what,
  onCancel,
  onProof,
}: {
  what: string;
  onCancel: () => void;
  onProof: (proof: string) => Promise<void>;
}) {
  const [proof, setProof] = useSensitiveState('');
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const first = useRef<HTMLInputElement>(null);
  const formId = useId();
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (proof.trim() === '' || pending) return;
    setPending(true);
    setFailure(null);
    try {
      await onProof(proof.trim());
    } catch (error) {
      setProof('');
      setFailure(registrationFailureText(error));
    } finally {
      setPending(false);
    }
  };
  return (
    <Dialog
      title="Confirm it's you"
      className="dialog--reauth"
      lede={`To ${what}, enter the code from your authenticator, or your password if you have none. Fresh proof, every time.`}
      initialFocus={first}
      onCancel={(event) => {
        event.preventDefault();
        if (!pending) onCancel();
      }}
      actions={
        <>
          <Button type="button" disabled={pending} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            type="submit"
            form={formId}
            className="btn--reauth"
            disabled={pending || proof.trim() === ''}
            aria-busy={pending ? true : undefined}
          >
            {pending ? 'Confirming…' : 'Confirm'}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={(event) => void submit(event)} noValidate>
        {failure === null ? null : <Alert>{failure}</Alert>}
        <Input
          ref={first}
          label="Authenticator code or password"
          type="password"
          autoComplete="one-time-code"
          value={proof}
          disabled={pending}
          onChange={(event) => setProof(event.target.value)}
        />
      </form>
    </Dialog>
  );
}
