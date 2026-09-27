import { useState } from 'react';

import { ApiError } from '../api/client.ts';
import {
  parsePolicy,
  pkiAdminRefusalText,
  policyText,
  useBindPkiProfile,
  useCreatePkiProfile,
  useDeletePkiProfile,
  usePkiProfiles,
  useUnbindPkiProfile,
  useUpdatePkiProfile,
  type PkiProfile,
} from '../api/pki.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';
import { Textarea } from '../ui/Textarea.tsx';
import { Panel, TypedNameConfirm } from './Sections.tsx';

const secondFactor = (error: unknown) => error instanceof ApiError && error.status === 403;

const TEMPLATE = `{
  "allowed_issuers": ["issuing"],
  "dns_patterns": ["*.svc.example.com"],
  "ip_ranges": [],
  "uri_patterns": [],
  "allow_wildcard_names": false,
  "key_algorithms": ["ecdsa-p256"],
  "key_usages": ["digital-signature"],
  "ext_key_usages": ["server-auth", "client-auth"],
  "max_ttl_seconds": 259200,
  "default_ttl_seconds": 86400,
  "renew_window_seconds": 28800,
  "allow_csr": true,
  "allow_generated_key": false,
  "machine_issuance": false,
  "organization": ""
}`;

type Feedback = { failure: string | null; done: string | null };

/**
 * PkiProfilesPanel is the certificate-profile lifecycle (#154), a panel on
 * `instance-admin`. A profile is closed policy (exact names or one-label
 * wildcards, CIDRs, URI prefixes, algorithms, usages and TTL bounds) and an
 * update may only narrow it: the server refuses any widening, and any change
 * it cannot prove is a narrowing. A profile reaches a tenant only through an
 * explicit binding.
 */
export function PkiProfilesPanel() {
  const profiles = usePkiProfiles();
  const create = useCreatePkiProfile();
  const [feedback, setFeedback] = useState<Feedback>({ failure: null, done: null });
  const [name, setName] = useState('');
  const [draft, setDraft] = useState(TEMPLATE);
  const [draftError, setDraftError] = useState<string | null>(null);
  const report = (error: unknown, action: string) => setFeedback({ failure: pkiAdminRefusalText(error, action), done: null });
  const ok = (message: string) => setFeedback({ failure: null, done: message });
  const clear = () => setFeedback({ failure: null, done: null });

  return (
    <Panel id="instance-pki-profiles" title="Certificate profiles">
      <p>
        A profile is the policy certificates are issued under. Updates may only narrow it; to widen,
        create a new profile. Bind a profile to a project (or one environment of it), then grant
        issue-certificate there.
      </p>
      {profiles.isPending ? <p role="status">Loading certificate profiles…</p> : null}
      {secondFactor(profiles.error) ? (
        <Alert>
          Reading certificate profiles needs instance-config and a second factor. If you hold it,
          present your authenticator code or passkey in the banner above.
        </Alert>
      ) : null}
      {profiles.isError && !secondFactor(profiles.error) ? (
        <Alert>{pkiAdminRefusalText(profiles.error, 'list the certificate profiles')}</Alert>
      ) : null}
      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Alert tone="done">{feedback.done}</Alert> : null}
      {profiles.isSuccess && profiles.data.profiles.length === 0 ? (
        <p role="status">No certificate profiles yet. Create one below.</p>
      ) : null}
      {profiles.isSuccess
        ? profiles.data.profiles.map((profile) => (
            <ProfileRow key={profile.id} profile={profile} onDone={ok} onFailure={report} onBusy={clear} />
          ))
        : null}

      <fieldset className="machine__lock" disabled={create.isPending}>
        <legend className="machine__subhead">Create a certificate profile</legend>
        <Input label="Name" mono value={name} placeholder="web" onChange={(event) => setName(event.target.value)} />
        <Textarea
          label="Policy (JSON)"
          mono
          rows={12}
          value={draft}
          error={draftError ?? undefined}
          onChange={(event) => {
            setDraft(event.target.value);
            setDraftError(null);
          }}
        />
        <div className="panel__actions">
          <Button
            type="button"
            variant="primary"
            disabled={name.trim() === ''}
            onClick={() => {
              clear();
              const policy = parsePolicy(draft);
              if (typeof policy === 'string') {
                setDraftError(policy);
                return;
              }
              create.mutate(
                { name, policy },
                {
                  onSuccess: (profile) => {
                    setName('');
                    ok(`Created profile ${profile.name}. Bind it to a project to use it.`);
                  },
                  onError: (error) => report(error, 'create the profile'),
                },
              );
            }}
          >
            Create
          </Button>
        </div>
      </fieldset>
    </Panel>
  );
}

function ProfileRow({
  profile,
  onDone,
  onFailure,
  onBusy,
}: {
  profile: PkiProfile;
  onDone: (message: string) => void;
  onFailure: (error: unknown, action: string) => void;
  onBusy: () => void;
}) {
  const [mode, setMode] = useState<'idle' | 'edit' | 'bind' | 'delete'>('idle');
  const [draft, setDraft] = useState(() => policyText(profile.policy));
  // The row version the draft was seeded from: saving sends it, so an edit
  // made against a policy that changed underneath is refused as a conflict.
  const [editedVersion, setEditedVersion] = useState(() => Number(profile.row_version));
  const [draftError, setDraftError] = useState<string | null>(null);
  const [target, setTarget] = useState({ org: '', project: '', environment: '' });
  const update = useUpdatePkiProfile();
  const remove = useDeletePkiProfile();
  const bind = useBindPkiProfile();
  const unbind = useUnbindPkiProfile();
  const policy = profile.policy;
  const toggle = (next: typeof mode) => {
    onBusy();
    if (next === 'edit' && mode !== 'edit') {
      setDraft(policyText(profile.policy));
      setEditedVersion(Number(profile.row_version));
      setDraftError(null);
    }
    setMode((current) => (current === next ? 'idle' : next));
  };

  return (
    <div className="settings-row settings-row--stacked" data-pki-profile={profile.name}>
      <div className="settings-row__copy">
        <span className="settings-row__title mono">{profile.name}</span>
        <span className="settings-row__detail">
          issuers {policy.allowed_issuers.join(', ')} · max {String(Number(policy.max_ttl_seconds) / 3600)}h ·{' '}
          {[...policy.dns_patterns, ...policy.ip_ranges, ...policy.uri_patterns].join(', ')}
        </span>
        {profile.bindings.map((binding) => (
          <span key={binding.id} className="settings-row__detail mono">
            bound to {binding.org_id}/{binding.project_id}
            {binding.environment_id == null ? '' : `/${binding.environment_id}`}{' '}
            <Button
              type="button"
              variant="quiet"
              disabled={unbind.isPending}
              onClick={() => {
                onBusy();
                unbind.mutate(
                  { name: profile.name, binding: binding.id },
                  {
                    onSuccess: () => onDone(`Removed a binding of ${profile.name}.`),
                    onError: (error) => onFailure(error, 'remove the binding'),
                  },
                );
              }}
            >
              Unbind
            </Button>
          </span>
        ))}
      </div>
      <span className="settings-row__spacer" />
      {policy.machine_issuance ? <Badge>machines</Badge> : null}
      <div className="panel__actions">
        <Button type="button" onClick={() => toggle('edit')}>
          Narrow
        </Button>
        <Button type="button" onClick={() => toggle('bind')}>
          Bind
        </Button>
        <Button type="button" variant="danger" onClick={() => toggle('delete')}>
          Delete
        </Button>
      </div>
      {mode === 'edit' ? (
        <fieldset className="machine__lock" disabled={update.isPending}>
          <Textarea
            label={`Policy of ${profile.name} (JSON)`}
            mono
            rows={12}
            value={draft}
            error={draftError ?? undefined}
            hint="Only a narrowing is accepted. Widening, or a change that cannot be proven a narrowing, is refused."
            onChange={(event) => {
              setDraft(event.target.value);
              setDraftError(null);
            }}
          />
          <div className="panel__actions">
            <Button
              type="button"
              variant="primary"
              onClick={() => {
                const next = parsePolicy(draft);
                if (typeof next === 'string') {
                  setDraftError(next);
                  return;
                }
                update.mutate(
                  { name: profile.name, policy: next, rowVersion: editedVersion },
                  {
                    onSuccess: () => {
                      setMode('idle');
                      onDone(`Narrowed ${profile.name}.`);
                    },
                    onError: (error) => onFailure(error, `narrow ${profile.name}`),
                  },
                );
              }}
            >
              Save narrowing
            </Button>
          </div>
        </fieldset>
      ) : null}
      {mode === 'bind' ? (
        <fieldset className="machine__lock" disabled={bind.isPending}>
          <Input label="Organization id" mono value={target.org} onChange={(event) => setTarget({ ...target, org: event.target.value })} />
          <Input label="Project id" mono value={target.project} onChange={(event) => setTarget({ ...target, project: event.target.value })} />
          <Input
            label="Environment id (optional)"
            mono
            value={target.environment}
            hint="Leave empty to bind every environment of the project."
            onChange={(event) => setTarget({ ...target, environment: event.target.value })}
          />
          <div className="panel__actions">
            <Button
              type="button"
              variant="primary"
              disabled={target.org.trim() === '' || target.project.trim() === ''}
              onClick={() =>
                bind.mutate(
                  { name: profile.name, ...target },
                  {
                    onSuccess: () => {
                      setMode('idle');
                      onDone(`Bound ${profile.name}.`);
                    },
                    onError: (error) => onFailure(error, `bind ${profile.name}`),
                  },
                )
              }
            >
              Bind
            </Button>
          </div>
        </fieldset>
      ) : null}
      {mode === 'delete' ? (
        <TypedNameConfirm
          label="Delete this profile"
          expect={profile.name}
          action="Delete"
          busy={remove.isPending}
          hint={
            <>
              Deleting removes every binding. Certificates it issued stay valid until they expire
              but can no longer be renewed through it. Type{' '}
              <span className="mono">{profile.name}</span> to confirm.
            </>
          }
          onConfirm={() =>
            remove.mutate(profile.name, {
              onSuccess: () => onDone(`Deleted ${profile.name}.`),
              onError: (error) => onFailure(error, `delete ${profile.name}`),
            })
          }
        />
      ) : null}
    </div>
  );
}
