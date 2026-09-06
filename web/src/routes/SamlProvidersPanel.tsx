import type { SamlMetadataDiff, SamlMetadataSource, SamlProvider } from '@hikyo/client';
import { useId, useState } from 'react';

import { ApiError } from '../api/client.ts';
import {
  samlFailureText,
  samlProviderInputErrors,
  useDeleteSamlProvider,
  usePatchSamlProvider,
  usePutSamlProvider,
  useRefreshSamlMetadata,
  useSamlProviders,
  type SamlAction,
  type SamlProviderInputDraft,
} from '../api/samlProviders.ts';
import { Alert, Done, Panel, TypedNameConfirm } from './Sections.tsx';

const secondFactor = (error: unknown) => error instanceof ApiError && error.status === 403;
const nondisclosed = (error: unknown) => error instanceof ApiError && error.status === 404;

/** A refusal reporter keyed to the action, so one panel-wide status can name it. */
type ReportFailure = (error: unknown, action: SamlAction) => void;

function metadataSourceOf(value: string): SamlMetadataSource {
  return value === 'url' ? 'url' : 'file';
}

/**
 * The SAML provider inventory and lifecycle (#500), a panel on `instance-admin`.
 *
 * Endpoint-to-action mapping follows the locked contract's own intent so there
 * is one obvious surface for each act: PUT creates, refresh-metadata replaces
 * trust material through the diff-and-confirm ceremony, PATCH changes policy or
 * disables without demanding unreadable metadata, and DELETE removes. The
 * metadata document is only ever an input, the read model never returns it, so
 * it can never be recovered from the browser.
 */
export function SamlProvidersPanel() {
  const providers = useSamlProviders();
  const [feedback, setFeedback] = useState<{ failure: string | null; done: string | null }>({
    failure: null,
    done: null,
  });
  const report: ReportFailure = (error, action) =>
    setFeedback({ failure: samlFailureText(error, action), done: null });
  const ok = (message: string) => setFeedback({ failure: null, done: message });
  const clear = () => setFeedback({ failure: null, done: null });
  const [creating, setCreating] = useState(false);

  return (
    <Panel id="instance-saml-providers" title="SAML providers">
      <p>
        Configure SAML identity providers from pinned metadata. Certificate,
        endpoint and assurance changes ride a diff-and-confirm ceremony; nothing
        that changes who can sign in is applied without an explicit
        confirmation.
      </p>
      {providers.isPending ? <p role="status">Loading SAML providers…</p> : null}
      {secondFactor(providers.error) ? (
        <Alert>
          Listing SAML providers needs a second factor and this authority. If
          you hold it, this session lacks sufficient second-factor assurance;
          present your authenticator code or passkey in the banner above.
        </Alert>
      ) : null}
      {nondisclosed(providers.error) ? (
        <p role="status">SAML providers are not disclosed to this session.</p>
      ) : null}
      {providers.isError && !secondFactor(providers.error) && !nondisclosed(providers.error) ? (
        <Alert>{samlFailureText(providers.error, 'list')}</Alert>
      ) : null}
      {providers.isSuccess && providers.data.providers.length === 0 ? (
        <p role="status">No SAML providers are configured.</p>
      ) : null}

      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Done>{feedback.done}</Done> : null}

      {providers.isSuccess
        ? providers.data.providers.map((provider) => (
            <ProviderRow
              key={provider.slug}
              provider={provider}
              onDone={ok}
              onFailure={report}
              onBusy={clear}
            />
          ))
        : null}

      {creating ? (
        <ProviderCreateForm
          onCancel={() => setCreating(false)}
          onCreated={(slug) => {
            setCreating(false);
            ok(`Configured SAML provider ${slug}. It is now listed above.`);
          }}
          onFailure={report}
        />
      ) : (
        <div className="instance-create-row">
          <button
            type="button"
            className="btn btn--primary"
            aria-label="Configure a new SAML provider"
            onClick={() => {
              clear();
              setCreating(true);
            }}
          >
            + configure SAML provider
          </button>
          <code className="instance-cli">$ hikyo provider saml add</code>
        </div>
      )}
    </Panel>
  );
}

function severityClass(severity: 'warning' | 'error'): string {
  return severity === 'error' ? 'settings-tag settings-tag--danger' : 'settings-tag';
}

function ProviderRow({
  provider,
  onDone,
  onFailure,
  onBusy,
}: {
  provider: SamlProvider;
  onDone: (message: string) => void;
  onFailure: ReportFailure;
  onBusy: () => void;
}) {
  const [mode, setMode] = useState<'idle' | 'policy' | 'refresh' | 'remove'>('idle');
  const remove = useDeleteSamlProvider();
  const validUntil = provider.metadata_valid_until;
  const toggle = (next: 'policy' | 'refresh' | 'remove') => {
    onBusy();
    setMode((current) => (current === next ? 'idle' : next));
  };

  return (
    <div className="settings-row settings-row--stacked" data-saml-provider={provider.slug}>
      <div className="settings-row__copy">
        <span className="settings-row__title">{provider.display_name}</span>
        <span className="settings-row__detail mono">{provider.slug}</span>
        <span className="settings-row__detail mono">entityID: {provider.entity_id}</span>
        <span className="settings-row__detail mono">ACS: {provider.acs_url}</span>
        <span className="settings-row__detail mono">
          metadata: {provider.metadata_source}
          {provider.metadata_url ? ` · ${provider.metadata_url}` : ''} ·{' '}
          {provider.metadata_signed ? 'signed' : 'unsigned'}
          {validUntil ? ` · valid until ${new Date(validUntil).toLocaleString()}` : ''}
        </span>
        <span className="settings-row__detail mono">
          {provider.signing_certificate_fingerprints.length} signing certificate
          {provider.signing_certificate_fingerprints.length === 1 ? '' : 's'} ·{' '}
          {provider.assurance_policy && provider.assurance_policy.length > 0
            ? `assurance: ${provider.assurance_policy.join(', ')}`
            : 'single-factor'}{' '}
          · {provider.allow_email_nameid ? 'email NameID allowed' : 'opaque NameID only'} ·{' '}
          {provider.force_sign_requests ? 'signed AuthnRequests' : 'metadata-driven signing'}
        </span>
        {provider.warnings.map((warning) => (
          <span
            className="settings-row__detail"
            role={warning.severity === 'error' ? 'alert' : 'status'}
            key={`${warning.code}:${warning.fingerprint ?? ''}`}
          >
            <span className={severityClass(warning.severity)}>{warning.severity}</span> {warning.message}
          </span>
        ))}
      </div>
      <span className="settings-row__spacer" />
      <span className={provider.enabled ? 'settings-tag' : 'settings-tag settings-tag--danger'}>
        {provider.enabled ? 'enabled' : 'disabled'}
      </span>
      <div className="panel__actions">
        <button type="button" className="btn" onClick={() => toggle('policy')}>Edit policy</button>
        <button type="button" className="btn" onClick={() => toggle('refresh')}>Refresh metadata</button>
        <button type="button" className="btn btn--danger" onClick={() => toggle('remove')}>Remove</button>
      </div>

      {mode === 'policy' ? (
        <ProviderPolicyForm
          provider={provider}
          onCancel={() => setMode('idle')}
          onSaved={(message) => {
            setMode('idle');
            onDone(message);
          }}
          onFailure={onFailure}
        />
      ) : null}

      {mode === 'refresh' ? (
        <RefreshMetadataForm
          provider={provider}
          onCancel={() => setMode('idle')}
          onApplied={() => {
            setMode('idle');
            onDone(`Refreshed the metadata for ${provider.slug}. The confirmed trust state is now stored.`);
          }}
          onFailure={onFailure}
        />
      ) : null}

      {mode === 'remove' ? (
        <div className="danger-zone">
          <p className="danger-zone__hint">
            Removing {provider.slug} deletes every session authenticated through it and
            withdraws it from the advertised sign-in methods. Locally-authenticated,
            local-floor access is unaffected. Linked identities from this provider stop
            resolving to a sign-in. This cannot be undone.
          </p>
          <TypedNameConfirm
            label={`Type ${provider.slug} to remove this provider`}
            expect={provider.slug}
            action="Remove provider"
            hint="The provider slug, exactly."
            busy={remove.isPending}
            onConfirm={() =>
              remove.mutate(provider.slug, {
                onSuccess: () => {
                  setMode('idle');
                  onDone(`Removed SAML provider ${provider.slug}; its sessions are swept and it no longer advertises.`);
                },
                onError: (error) => onFailure(error, 'delete-provider'),
              })
            }
          />
        </div>
      ) : null}
    </div>
  );
}

function ProviderPolicyForm({
  provider,
  onCancel,
  onSaved,
  onFailure,
}: {
  provider: SamlProvider;
  onCancel: () => void;
  onSaved: (message: string) => void;
  onFailure: ReportFailure;
}) {
  const patch = usePatchSamlProvider();
  const nameId = useId();
  const assuranceId = useId();
  const [displayName, setDisplayName] = useState(provider.display_name);
  const [assurance, setAssurance] = useState((provider.assurance_policy ?? []).join('\n'));
  const [allowEmail, setAllowEmail] = useState(provider.allow_email_nameid);
  const [forceSign, setForceSign] = useState(provider.force_sign_requests);
  const [enabled, setEnabled] = useState(provider.enabled);

  const submit = () => {
    const trimmedName = displayName.trim();
    if (trimmedName === '') return onFailure(new ApiError(400, 'invalid'), 'update-provider');
    const policy = assurance
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== '');
    patch.mutate(
      {
        slug: provider.slug,
        patch: {
          display_name: trimmedName,
          assurance_policy: policy.length === 0 ? null : policy,
          allow_email_nameid: allowEmail,
          force_sign_requests: forceSign,
          enabled,
        },
      },
      {
        onSuccess: () =>
          onSaved(
            enabled
              ? `Updated ${provider.slug} policy.`
              : `Disabled ${provider.slug}; its sessions are swept and it no longer advertises for sign-in.`,
          ),
        onError: (error) => onFailure(error, 'update-provider'),
      },
    );
  };

  return (
    <div className="saml-editor">
      <p className="field__hint">
        Policy and disable ride PATCH: the entity ID and pinned metadata are not
        touched, so no unreadable XML is required to disable a provider.
      </p>
      <div className="field">
        <label htmlFor={nameId}>Display name</label>
        <input id={nameId} value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
      </div>
      <div className="field">
        <label htmlFor={assuranceId}>Accepted AuthnContextClassRef values (one per line; empty = single-factor)</label>
        <textarea id={assuranceId} className="mono" rows={2} value={assurance} onChange={(event) => setAssurance(event.target.value)} />
      </div>
      <div className="field chk">
        <input id={`${nameId}-email`} type="checkbox" checked={allowEmail} onChange={(event) => setAllowEmail(event.target.checked)} />
        <label htmlFor={`${nameId}-email`}>Allow opaque emailAddress NameID values</label>
      </div>
      <div className="field chk">
        <input id={`${nameId}-sign`} type="checkbox" checked={forceSign} onChange={(event) => setForceSign(event.target.checked)} />
        <label htmlFor={`${nameId}-sign`}>Force signed AuthnRequests</label>
      </div>
      <div className="field chk">
        <input id={`${nameId}-enabled`} type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
        <label htmlFor={`${nameId}-enabled`}>Enabled (advertises for sign-in)</label>
      </div>
      <div className="panel__actions">
        <button type="button" className="btn" onClick={onCancel}>Cancel</button>
        <button type="button" className="btn btn--primary" disabled={patch.isPending} onClick={submit}>Save policy</button>
      </div>
    </div>
  );
}

function MetadataDiff({ diff }: { diff: SamlMetadataDiff }) {
  const rows: [string, readonly string[]][] = [
    ['New endpoints', diff.endpoints_added],
    ['Removed endpoints', diff.endpoints_removed],
    ['New signing certificates', diff.certs_added_fps],
    ['Removed signing certificates', diff.certs_removed_fps],
  ];
  const anyChange = rows.some(([, values]) => values.length > 0);
  return (
    <div className="policy-impact" role="alert">
      <p>
        This metadata changes trust state. Nothing has been applied yet; confirm to
        commit exactly the changes below.
      </p>
      {anyChange ? (
        rows
          .filter(([, values]) => values.length > 0)
          .map(([label, values]) => (
            <div key={label}>
              <strong>{label}</strong>
              <ul>{values.map((value) => <li key={value} className="mono">{value}</li>)}</ul>
            </div>
          ))
      ) : (
        <p>No endpoint or certificate change; only the validity window moves.</p>
      )}
      {diff.valid_until ? (
        <p className="field__hint mono">valid until {new Date(diff.valid_until).toLocaleString()}</p>
      ) : null}
    </div>
  );
}

type PendingDiff = {
  readonly diff: SamlMetadataDiff;
  readonly fingerprints: readonly string[];
  readonly endpoints: readonly string[];
};

function RefreshMetadataForm({
  provider,
  onCancel,
  onApplied,
  onFailure,
}: {
  provider: SamlProvider;
  onCancel: () => void;
  onApplied: () => void;
  onFailure: ReportFailure;
}) {
  const refresh = useRefreshSamlMetadata();
  const documentId = useId();
  const fileBacked = provider.metadata_source === 'file';
  const [replacement, setReplacement] = useState('');
  // The pending diff carries the exact document it was computed for; confirming
  // resends that snapshot, never the live textarea.
  const [pending, setPending] = useState<{ diff: PendingDiff; document: string | null } | null>(null);

  // Never leave the write-only replacement document behind once the ceremony ends.
  const finish = () => {
    setReplacement('');
    setPending(null);
    refresh.reset();
  };
  const cancel = () => {
    finish();
    onCancel();
  };

  const submit = (document: string | null, confirm: PendingDiff | null) => {
    refresh.mutate(
      {
        slug: provider.slug,
        metadataDocument: document,
        ...(confirm ? { confirmedFingerprints: confirm.fingerprints, confirmedEndpoints: confirm.endpoints } : {}),
      },
      {
        onSuccess: (result) => {
          if (result.applied) {
            finish();
            onApplied();
            return;
          }
          setPending({
            diff: {
              diff: result.diff,
              fingerprints: result.required_fingerprints,
              endpoints: result.required_endpoints,
            },
            document,
          });
        },
        onError: (error) => onFailure(error, 'refresh-metadata'),
      },
    );
  };

  const preview = () => {
    if (fileBacked && replacement.trim() === '') {
      return onFailure(new ApiError(400, 'missing document'), 'refresh-metadata');
    }
    submit(fileBacked ? replacement : null, null);
  };
  const confirm = () => {
    if (pending === null) return;
    submit(pending.document, pending.diff);
  };

  return (
    <div className="saml-editor">
      <p className="field__hint">
        {fileBacked
          ? 'File-backed provider: paste the replacement metadata document.'
          : 'URL-backed provider: refreshing performs one WebPKI fetch. Off-origin redirects are refused.'}
      </p>
      {fileBacked ? (
        <div className="field">
          <label htmlFor={documentId}>Replacement metadata XML</label>
          <textarea
            id={documentId}
            className="mono"
            rows={4}
            value={replacement}
            onChange={(event) => {
              setPending(null);
              setReplacement(event.target.value);
            }}
          />
        </div>
      ) : null}
      {pending ? <MetadataDiff diff={pending.diff.diff} /> : null}
      <div className="panel__actions">
        {/* Disabled while a request is in flight: cancel resets the mutation,
            and resetting a still-pending request would leave it to settle later
            with the document in cache. */}
        <button type="button" className="btn" disabled={refresh.isPending} onClick={cancel}>Cancel</button>
        {pending ? (
          <button type="button" className="btn btn--danger" disabled={refresh.isPending} onClick={confirm}>
            Confirm and apply the trust change
          </button>
        ) : (
          <button type="button" className="btn btn--primary" disabled={refresh.isPending} onClick={preview}>
            {fileBacked ? 'Preview metadata change' : 'Fetch and preview metadata'}
          </button>
        )}
      </div>
    </div>
  );
}

function ProviderCreateForm({
  onCancel,
  onCreated,
  onFailure,
}: {
  onCancel: () => void;
  onCreated: (slug: string) => void;
  onFailure: ReportFailure;
}) {
  const put = usePutSamlProvider();
  const ids = {
    slug: useId(),
    name: useId(),
    entity: useId(),
    source: useId(),
    document: useId(),
    url: useId(),
    assurance: useId(),
  };
  const [slug, setSlug] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [entityId, setEntityId] = useState('');
  const [metadataSource, setMetadataSource] = useState<SamlMetadataSource>('file');
  const [metadataDocument, setMetadataDocument] = useState('');
  const [metadataUrl, setMetadataUrl] = useState('');
  const [assurance, setAssurance] = useState('');
  const [allowEmail, setAllowEmail] = useState(false);
  const [forceSign, setForceSign] = useState(false);
  const [enabled, setEnabled] = useState(true);
  const [clientErrors, setClientErrors] = useState<readonly string[]>([]);
  // The pending diff carries the EXACT draft it was computed for. Confirming
  // resends that snapshot, never the live form state, so an edit made while a
  // preview is in flight can never be applied under an earlier diff's
  // confirmation (it forces a fresh preview instead).
  const [pending, setPending] = useState<{ diff: PendingDiff; draft: SamlProviderInputDraft } | null>(null);

  // Never leave the write-only metadata document in the form, the pending
  // snapshot, or React Query's mutation variables once the ceremony ends.
  const finish = () => {
    setMetadataDocument('');
    setPending(null);
    put.reset();
  };
  const cancel = () => {
    finish();
    onCancel();
  };
  const clearPending = () => setPending(null);

  const preview = () => {
    const errors = samlProviderInputErrors({
      creating: true,
      slug,
      displayName,
      entityId,
      metadataSource,
      metadataDocument,
      metadataUrl,
    });
    setClientErrors(errors);
    if (errors.length > 0) return;
    const policy = assurance
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== '');
    submit({
      slug,
      displayName,
      entityId,
      metadataSource,
      metadataDocument,
      metadataUrl,
      assurancePolicy: policy.length === 0 ? null : policy,
      allowEmailNameid: allowEmail,
      forceSignRequests: forceSign,
      enabled,
    });
  };

  const confirm = () => {
    if (pending === null) return;
    submit({
      ...pending.draft,
      confirmedFingerprints: pending.diff.fingerprints,
      confirmedEndpoints: pending.diff.endpoints,
    });
  };

  const submit = (draft: SamlProviderInputDraft) => {
    put.mutate(draft, {
      onSuccess: (result) => {
        if (result.applied) {
          const created = draft.slug;
          finish();
          onCreated(created);
          return;
        }
        setPending({
          diff: {
            diff: result.diff,
            fingerprints: result.required_fingerprints,
            endpoints: result.required_endpoints,
          },
          draft,
        });
      },
      onError: (error) => onFailure(error, 'save-provider'),
    });
  };

  return (
    <div className="saml-editor" data-saml-create>
      <h3>Configure a SAML provider</h3>
      <div className="field">
        <label htmlFor={ids.slug}>Slug (immutable; addresses this provider)</label>
        <input id={ids.slug} className="mono" value={slug} onChange={(event) => { clearPending(); setSlug(event.target.value); }} />
      </div>
      <div className="field">
        <label htmlFor={ids.name}>Display name</label>
        <input id={ids.name} value={displayName} onChange={(event) => { clearPending(); setDisplayName(event.target.value); }} />
      </div>
      <div className="field">
        <label htmlFor={ids.entity}>IdP entityID (byte-exact; immutable after create)</label>
        <input id={ids.entity} className="mono" value={entityId} onChange={(event) => { clearPending(); setEntityId(event.target.value); }} />
      </div>
      <div className="field">
        <label htmlFor={ids.source}>Metadata source</label>
        <select id={ids.source} value={metadataSource} onChange={(event) => { clearPending(); setMetadataSource(metadataSourceOf(event.target.value)); }}>
          <option value="file">file: paste XML</option>
          <option value="url">url: one-shot https fetch</option>
        </select>
      </div>
      {metadataSource === 'file' ? (
        <div className="field">
          <label htmlFor={ids.document}>Metadata XML</label>
          <textarea id={ids.document} className="mono" rows={4} value={metadataDocument} onChange={(event) => { clearPending(); setMetadataDocument(event.target.value); }} />
        </div>
      ) : (
        <div className="field">
          <label htmlFor={ids.url}>Metadata URL (https only)</label>
          <input id={ids.url} className="mono" value={metadataUrl} onChange={(event) => { clearPending(); setMetadataUrl(event.target.value); }} />
        </div>
      )}
      <div className="field">
        <label htmlFor={ids.assurance}>Accepted AuthnContextClassRef values (one per line; empty = single-factor)</label>
        <textarea id={ids.assurance} className="mono" rows={2} value={assurance} onChange={(event) => setAssurance(event.target.value)} />
      </div>
      <div className="field chk">
        <input id={`${ids.slug}-email`} type="checkbox" checked={allowEmail} onChange={(event) => setAllowEmail(event.target.checked)} />
        <label htmlFor={`${ids.slug}-email`}>Allow opaque emailAddress NameID values</label>
      </div>
      <div className="field chk">
        <input id={`${ids.slug}-sign`} type="checkbox" checked={forceSign} onChange={(event) => setForceSign(event.target.checked)} />
        <label htmlFor={`${ids.slug}-sign`}>Force signed AuthnRequests</label>
      </div>
      <div className="field chk">
        <input id={`${ids.slug}-enabled`} type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
        <label htmlFor={`${ids.slug}-enabled`}>Enabled (advertises for sign-in)</label>
      </div>
      {clientErrors.length > 0 ? <Alert>{clientErrors.join(' ')}</Alert> : null}
      {pending ? <MetadataDiff diff={pending.diff.diff} /> : null}
      <div className="panel__actions">
        {/* Disabled while a request is in flight: see RefreshMetadataForm. */}
        <button type="button" className="btn" disabled={put.isPending} onClick={cancel}>Cancel</button>
        {pending ? (
          <button type="button" className="btn btn--danger" disabled={put.isPending} onClick={confirm}>
            Confirm trust and configure provider
          </button>
        ) : (
          <button type="button" className="btn btn--primary" disabled={put.isPending} onClick={preview}>
            Preview and configure
          </button>
        )}
      </div>
    </div>
  );
}
