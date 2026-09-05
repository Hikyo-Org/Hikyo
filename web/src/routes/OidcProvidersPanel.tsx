import { useId, useState } from 'react';

import { ApiError } from '../api/client.ts';
import {
  oidcProviderRefusalText,
  putOidcProvider,
  useDeleteOidcProvider,
  useOidcProviders,
  validateProviderDraft,
  type OidcProvider,
  type OidcProviderDraft,
  type OidcProviderField,
} from '../api/oidcProviders.ts';
import { Alert, Done, Panel, TypedNameConfirm } from './Sections.tsx';
import { useFeedback, useModalDialog } from './useModalDialog.ts';

/**
 * OIDC provider administration (#499).
 *
 * The whole panel is `instance-config`, which is MFA-mandatory, so its
 * capability gate is the LIST READ: a session without the second factor gets
 * the honest "second factor required" state and no controls, never an empty
 * list that would answer a question the server refused. A read that is not
 * disclosed at all (404) says exactly that.
 *
 * The client secret is write-only and never returned. The editor never prefills
 * it and always requires it, because every PUT re-seals whatever secret it
 * carries — there is no path that keeps the old one.
 */

const secondFactor = (error: unknown) => error instanceof ApiError && error.status === 403;
const nondisclosed = (error: unknown) => error instanceof ApiError && error.status === 404;

/** A refusal already rendered into its final sentence (server or field error). */
class Refusal extends Error {}

const emptyDraft: OidcProviderDraft = {
  slug: '',
  displayName: '',
  issuer: '',
  clientId: '',
  clientSecret: '',
  scopes: 'openid',
  assurancePolicy: '',
  enabled: true,
};

function draftFrom(provider: OidcProvider): OidcProviderDraft {
  return {
    slug: provider.slug,
    displayName: provider.display_name,
    issuer: provider.issuer,
    clientId: provider.client_id,
    // The secret is never returned, so it starts blank and must be re-entered.
    clientSecret: '',
    scopes: provider.scopes,
    assurancePolicy: provider.assurance_policy ?? '',
    enabled: provider.enabled,
  };
}

type EditorTarget =
  | { readonly kind: 'create' }
  | { readonly kind: 'reconfigure'; readonly provider: OidcProvider };

export function OidcProvidersPanel() {
  const providers = useOidcProviders();
  const feedback = useFeedback((error) =>
    error instanceof Refusal ? error.message : oidcProviderRefusalText(error, 'save-oidc-provider'),
  );
  const [editor, setEditor] = useState<EditorTarget | null>(null);
  const [deleting, setDeleting] = useState<OidcProvider | null>(null);
  // Set while a post-conflict refetch is in flight. The provider PUT is a
  // full replace by slug with no client row-version, so the server's CAS only
  // guards a write racing inside its own transaction — it cannot stop an admin
  // acting on STALE displayed data. So after a fail-closed refusal we latch the
  // action controls shut until the list has refetched: without it, a stale
  // editor could reopen during the refresh and silently overwrite a concurrent
  // admin's change (for example re-enabling a provider they just disabled).
  const [refreshingAfterConflict, setRefreshingAfterConflict] = useState(false);

  const openCreate = () => {
    feedback.clear();
    setEditor({ kind: 'create' });
  };
  const openReconfigure = (provider: OidcProvider) => {
    feedback.clear();
    setEditor({ kind: 'reconfigure', provider });
  };

  return (
    <Panel id="instance-oidc" title="Identity providers">
      <p>
        OpenID Connect providers advertised on the sign-in page. Configuring one makes it a
        &ldquo;Continue with&rdquo; option; disabling or deleting one removes that option and ends
        every session that authenticated through it. Local password and second-factor sign-in is
        never affected.
      </p>

      {providers.isPending ? <p role="status">Loading identity providers…</p> : null}
      {secondFactor(providers.error) ? (
        <Alert>
          Administering identity providers needs a second factor. This session does not have
          sufficient second-factor assurance; present your authenticator code or passkey in the
          banner above.
        </Alert>
      ) : null}
      {nondisclosed(providers.error) ? (
        <p role="status">The identity-provider directory is not disclosed to this session.</p>
      ) : null}
      {providers.isError && !secondFactor(providers.error) && !nondisclosed(providers.error) ? (
        <Alert>{oidcProviderRefusalText(providers.error, 'list-oidc-providers')}</Alert>
      ) : null}

      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Done>{feedback.done}</Done> : null}

      {providers.isSuccess && providers.data.providers.length === 0 ? (
        <p role="status">No identity providers are configured.</p>
      ) : null}
      {providers.isSuccess
        ? providers.data.providers.map((provider) => (
            <div className="settings-row" key={provider.slug}>
              <div className="settings-row__copy">
                <span className="settings-row__title">{provider.display_name}</span>
                <span className="settings-row__detail mono">
                  {provider.slug} · {provider.issuer}
                </span>
              </div>
              <span className="settings-row__spacer" />
              <span
                className={`settings-tag mono${provider.enabled ? '' : ' settings-tag--danger'}`}
              >
                {provider.enabled ? 'enabled' : 'disabled'}
              </span>
              <button
                type="button"
                className="btn"
                aria-label={`Reconfigure ${provider.display_name}`}
                disabled={refreshingAfterConflict}
                onClick={() => openReconfigure(provider)}
              >
                Reconfigure
              </button>
              <button
                type="button"
                className="btn btn--danger"
                aria-label={`Delete ${provider.display_name}`}
                disabled={refreshingAfterConflict}
                onClick={() => {
                  feedback.clear();
                  setDeleting(provider);
                }}
              >
                Delete
              </button>
            </div>
          ))
        : null}

      {refreshingAfterConflict ? (
        <p role="status">Refreshing the provider list after a conflicting change…</p>
      ) : null}

      {providers.isSuccess && editor === null ? (
        <div className="panel__actions">
          <button
            type="button"
            className="btn btn--primary"
            disabled={refreshingAfterConflict}
            onClick={openCreate}
          >
            + add identity provider
          </button>
          <code className="instance-cli">$ hikyo oidc-provider put</code>
        </div>
      ) : null}

      {editor !== null && providers.isSuccess ? (
        <ProviderEditor
          target={editor}
          existing={providers.data.providers}
          onCancel={() => {
            feedback.clear();
            setEditor(null);
          }}
          onSaved={(describe) => {
            setEditor(null);
            void providers.refetch();
            feedback.ok(describe);
          }}
          onFailure={(refusal) => feedback.report(refusal)}
          onFailClosed={() => {
            // A stale, forbidden, or ended-session refusal: close the editor and
            // refetch, latching the action controls until fresh data lands so no
            // retry proceeds against the stale list in the meantime.
            setEditor(null);
            setRefreshingAfterConflict(true);
            void providers.refetch().finally(() => setRefreshingAfterConflict(false));
          }}
        />
      ) : null}

      {deleting !== null ? (
        <DeleteProviderDialog
          provider={deleting}
          onCancel={() => setDeleting(null)}
          onDeleted={(name) => {
            setDeleting(null);
            feedback.ok(`Deleted ${name}. Its sign-in option is gone and every session that used it has ended.`);
          }}
          onFailure={(error) =>
            feedback.report(new Refusal(oidcProviderRefusalText(error, 'delete-oidc-provider')))
          }
        />
      ) : null}
    </Panel>
  );
}

function ProviderEditor({
  target,
  existing,
  onCancel,
  onSaved,
  onFailure,
  onFailClosed,
}: {
  target: EditorTarget;
  existing: readonly OidcProvider[];
  onCancel: () => void;
  onSaved: (describe: string) => void;
  onFailure: (refusal: Refusal) => void;
  onFailClosed: () => void;
}) {
  const original = target.kind === 'reconfigure' ? target.provider : null;
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<OidcProviderDraft>(
    original === null ? emptyDraft : draftFrom(original),
  );
  const [invalidField, setInvalidField] = useState<OidcProviderField | null>(null);
  const ids = {
    slug: useId(),
    displayName: useId(),
    issuer: useId(),
    clientId: useId(),
    clientSecret: useId(),
    scopes: useId(),
    assurance: useId(),
  };

  const set = <K extends keyof OidcProviderDraft>(key: K, value: OidcProviderDraft[K]) => {
    setInvalidField(null);
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const submit = () => {
    const result = validateProviderDraft(draft, original, existing);
    if (!result.ok) {
      setInvalidField(result.field);
      onFailure(new Refusal(result.message));
      return;
    }
    setInvalidField(null);
    const verb = original === null ? 'Configured' : 'Reconfigured';
    const describe = `${verb} ${result.input.displayName}. ${result.input.enabled ? 'It is advertised on the sign-in page.' : 'It is disabled and not advertised.'}`;
    setBusy(true);
    void putOidcProvider(result.slug, result.input).then(
      () => {
        // The parent unmounts this editor on save, dropping the draft (and its
        // secret) from state; no local reset is needed on the success path.
        onSaved(describe);
      },
      (error: unknown) => {
        // The secret is write-only: never retain it after a failed save.
        setDraft((current) => ({ ...current, clientSecret: '' }));
        setBusy(false);
        onFailure(new Refusal(oidcProviderRefusalText(error, 'save-oidc-provider')));
        // Stale (409), forbidden (403), or ended-session (401) refusals are
        // fail-closed: close and refetch so no retry proceeds on stale state.
        // The server's row-version CAS is the ultimate guard — a stale write is
        // refused there too — so this is defence in depth, not the only line.
        if (
          error instanceof ApiError &&
          (error.status === 401 || error.status === 403 || error.status === 409)
        ) {
          onFailClosed();
        }
      },
    );
  };

  const disabling = original !== null && original.enabled && !draft.enabled;
  const invalid = (field: OidcProviderField) => (invalidField === field ? true : undefined);

  return (
    <div className="oidc-editor">
      <h3>{original === null ? 'New identity provider' : `Reconfigure ${original.display_name}`}</h3>
      {original === null ? (
        <div className="field">
          <label htmlFor={ids.slug}>Slug</label>
          <input
            id={ids.slug}
            className="mono"
            value={draft.slug}
            aria-invalid={invalid('slug')}
            onChange={(event) => set('slug', event.target.value)}
          />
          <p className="field__hint">Lowercase letters, digits and hyphens; it appears in the callback URL and cannot change.</p>
        </div>
      ) : (
        <div className="settings-row">
          <div className="settings-row__copy">
            <span className="settings-row__title">Slug</span>
            <span className="settings-row__detail mono">{original.slug}</span>
          </div>
        </div>
      )}

      <div className="field">
        <label htmlFor={ids.displayName}>Display name</label>
        <input
          id={ids.displayName}
          value={draft.displayName}
          aria-invalid={invalid('display_name')}
          onChange={(event) => set('displayName', event.target.value)}
        />
      </div>

      <div className="field">
        <label htmlFor={ids.issuer}>Issuer URL</label>
        <input
          id={ids.issuer}
          className="mono"
          value={draft.issuer}
          aria-invalid={invalid('issuer')}
          disabled={original !== null}
          onChange={(event) => set('issuer', event.target.value)}
        />
        <p className="field__hint">
          {original === null
            ? 'Its OpenID configuration is fetched and validated on save.'
            : 'The issuer is immutable after create — every linked identity is keyed by it.'}
        </p>
      </div>

      <div className="field">
        <label htmlFor={ids.clientId}>Client ID</label>
        <input
          id={ids.clientId}
          className="mono"
          value={draft.clientId}
          aria-invalid={invalid('client_id')}
          onChange={(event) => set('clientId', event.target.value)}
        />
      </div>

      <div className="field">
        <label htmlFor={ids.clientSecret}>Client secret</label>
        <input
          id={ids.clientSecret}
          type="password"
          autoComplete="off"
          value={draft.clientSecret}
          aria-invalid={invalid('client_secret')}
          onChange={(event) => set('clientSecret', event.target.value)}
        />
        <p className="field__hint">
          Write-only: it is never displayed, so it must be entered on every save — including when
          only disabling.
        </p>
      </div>

      <div className="field">
        <label htmlFor={ids.scopes}>Scopes</label>
        <input
          id={ids.scopes}
          className="mono"
          value={draft.scopes}
          aria-invalid={invalid('scopes')}
          onChange={(event) => set('scopes', event.target.value)}
        />
      </div>

      <div className="field">
        <label htmlFor={ids.assurance}>Assurance policy (JSON, optional)</label>
        <textarea
          id={ids.assurance}
          className="mono"
          value={draft.assurancePolicy}
          aria-invalid={invalid('assurance_policy')}
          onChange={(event) => set('assurancePolicy', event.target.value)}
        />
      </div>

      <div className="field chk">
        <input
          id={`${ids.slug}-enabled`}
          type="checkbox"
          checked={draft.enabled}
          onChange={(event) => set('enabled', event.target.checked)}
        />
        <label htmlFor={`${ids.slug}-enabled`}>Enabled (advertised on the sign-in page)</label>
      </div>

      {disabling ? (
        <p className="policy-impact" role="alert">
          Disabling removes &ldquo;Continue with {original.display_name}&rdquo; from the sign-in
          page and ends every session that authenticated through it. Linked identities can no
          longer sign in through it. Local password and second-factor sign-in is unaffected.
        </p>
      ) : null}

      <div className="panel__actions">
        <button type="button" className="btn" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button type="button" className="btn btn--primary" onClick={submit} disabled={busy}>
          {original === null ? 'Configure provider' : 'Save provider'}
        </button>
      </div>
    </div>
  );
}

function DeleteProviderDialog({
  provider,
  onCancel,
  onDeleted,
  onFailure,
}: {
  provider: OidcProvider;
  onCancel: () => void;
  onDeleted: (name: string) => void;
  onFailure: (error: unknown) => void;
}) {
  const dialog = useModalDialog();
  const del = useDeleteOidcProvider();

  return (
    <dialog
      className="ceremony"
      ref={dialog}
      aria-labelledby="delete-oidc-title"
      onCancel={(event) => {
        event.preventDefault();
        if (!del.isPending) {
          onCancel();
        }
      }}
    >
      <h2 id="delete-oidc-title">Delete {provider.display_name}?</h2>
      <p className="ceremony__lede">
        &ldquo;Continue with {provider.display_name}&rdquo; leaves the sign-in page and every
        session that authenticated through it ends immediately; its live transactions cascade.
        Identities linked through it can no longer sign in. Local password and second-factor
        sign-in is unaffected. This cannot be undone.
      </p>
      <TypedNameConfirm
        label="Type the provider slug to confirm"
        expect={provider.slug}
        action="Delete provider"
        busy={del.isPending}
        hint={
          <>
            Deletion is by the immutable slug <span className="mono">{provider.slug}</span>, not the
            display name.
          </>
        }
        onConfirm={() =>
          del.mutate(
            { slug: provider.slug },
            {
              onSuccess: () => onDeleted(provider.display_name),
              onError: onFailure,
            },
          )
        }
      />
      <div className="ceremony__actions">
        <button type="button" className="btn" onClick={onCancel} disabled={del.isPending}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}
