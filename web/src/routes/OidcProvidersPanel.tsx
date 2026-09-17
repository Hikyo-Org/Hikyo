import { useState } from 'react';

import { useSensitiveState } from '../api/sensitiveMutation.ts';
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
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Input } from '../ui/Input.tsx';
import { Textarea } from '../ui/Textarea.tsx';
import { Panel, TypedNameConfirm } from './Sections.tsx';
import { useFeedback } from './useFeedback.ts';

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
 * carries, there is no path that keeps the old one.
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
  // guards a write racing inside its own transaction, it cannot stop an admin
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
      {feedback.done !== null ? <Alert tone="done">{feedback.done}</Alert> : null}

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
              <Button
                type="button"
                aria-label={`Reconfigure ${provider.display_name}`}
                disabled={refreshingAfterConflict}
                onClick={() => openReconfigure(provider)}
              >
                Reconfigure
              </Button>
              <Button
                type="button"
                variant="danger"
                aria-label={`Delete ${provider.display_name}`}
                disabled={refreshingAfterConflict}
                onClick={() => {
                  feedback.clear();
                  setDeleting(provider);
                }}
              >
                Delete
              </Button>
            </div>
          ))
        : null}

      {refreshingAfterConflict ? (
        <p role="status">Refreshing the provider list after a conflicting change…</p>
      ) : null}

      {providers.isSuccess && editor === null ? (
        <div className="panel__actions">
          <Button
            type="button"
            variant="primary"
            disabled={refreshingAfterConflict}
            onClick={openCreate}
          >
            + add identity provider
          </Button>
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
          onClearFailure={() => feedback.clear()}
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
  onClearFailure,
  onFailClosed,
}: {
  target: EditorTarget;
  existing: readonly OidcProvider[];
  onCancel: () => void;
  onSaved: (describe: string) => void;
  onFailure: (refusal: Refusal) => void;
  /** Drops the form-level sentence: a field refusal must not sit beside a stale one. */
  onClearFailure: () => void;
  onFailClosed: () => void;
}) {
  const original = target.kind === 'reconfigure' ? target.provider : null;
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useSensitiveState<OidcProviderDraft>(
    original === null ? emptyDraft : draftFrom(original),
  );
  // A refusal that names one control lives under that control, not in the
  // form-level alert: the alert carries only what is about the whole save.
  const [fieldRefusal, setFieldRefusal] = useState<{
    readonly field: OidcProviderField;
    readonly message: string;
  } | null>(null);

  const set = <K extends keyof OidcProviderDraft>(key: K, value: OidcProviderDraft[K]) => {
    setFieldRefusal(null);
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const submit = () => {
    const result = validateProviderDraft(draft, original, existing);
    if (!result.ok) {
      // Drop whatever the last save said: it is about a request this one
      // replaces, and two refusals on screen at once name no single cause.
      onClearFailure();
      setFieldRefusal({ field: result.field, message: result.message });
      return;
    }
    setFieldRefusal(null);
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
        // The server's row-version CAS is the ultimate guard, a stale write is
        // refused there too, so this is defence in depth, not the only line.
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
  const refusalFor = (field: OidcProviderField) =>
    fieldRefusal?.field === field ? fieldRefusal.message : undefined;

  return (
    <div className="oidc-editor">
      <h3>{original === null ? 'New identity provider' : `Reconfigure ${original.display_name}`}</h3>
      {original === null ? (
        <Input
          label="Slug"
          mono
          value={draft.slug}
          onChange={(event) => set('slug', event.target.value)}
          hint="Lowercase letters, digits and hyphens; it appears in the callback URL and cannot change."
          error={refusalFor('slug')}
        />
      ) : (
        <div className="settings-row">
          <div className="settings-row__copy">
            <span className="settings-row__title">Slug</span>
            <span className="settings-row__detail mono">{original.slug}</span>
          </div>
        </div>
      )}

      <Input
        label="Display name"
        value={draft.displayName}
        onChange={(event) => set('displayName', event.target.value)}
        error={refusalFor('display_name')}
      />

      <Input
        label="Issuer URL"
        mono
        value={draft.issuer}
        disabled={original !== null}
        onChange={(event) => set('issuer', event.target.value)}
        hint={
          original === null
            ? 'Its OpenID configuration is fetched and validated on save.'
            : 'The issuer is immutable after create; every linked identity is keyed by it.'
        }
        error={refusalFor('issuer')}
      />

      <Input
        label="Client ID"
        mono
        value={draft.clientId}
        onChange={(event) => set('clientId', event.target.value)}
        error={refusalFor('client_id')}
      />

      <Input
        label="Client secret"
        type="password"
        autoComplete="off"
        value={draft.clientSecret}
        onChange={(event) => set('clientSecret', event.target.value)}
        hint="Write-only: it is never displayed, so it must be entered on every save, including when only disabling."
        error={refusalFor('client_secret')}
      />

      <Input
        label="Scopes"
        mono
        value={draft.scopes}
        onChange={(event) => set('scopes', event.target.value)}
        error={refusalFor('scopes')}
      />

      <Textarea
        label="Assurance policy (JSON, optional)"
        mono
        value={draft.assurancePolicy}
        onChange={(event) => set('assurancePolicy', event.target.value)}
        error={refusalFor('assurance_policy')}
      />

      <Checkbox
        label="Enabled (advertised on the sign-in page)"
        checked={draft.enabled}
        onChange={(event) => set('enabled', event.target.checked)}
      />

      {disabling ? (
        <p className="policy-impact" role="alert">
          Disabling removes &ldquo;Continue with {original.display_name}&rdquo; from the sign-in
          page and ends every session that authenticated through it. Linked identities can no
          longer sign in through it. Local password and second-factor sign-in is unaffected.
        </p>
      ) : null}

      <div className="panel__actions">
        <Button type="button" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button type="button" variant="primary" onClick={submit} disabled={busy}>
          {original === null ? 'Configure provider' : 'Save provider'}
        </Button>
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
  const del = useDeleteOidcProvider();

  return (
    <Dialog
      title={`Delete ${provider.display_name}?`}
      lede={
        <>
          &ldquo;Continue with {provider.display_name}&rdquo; leaves the sign-in page and every
          session that authenticated through it ends immediately; its live transactions cascade.
          Identities linked through it can no longer sign in. Local password and second-factor
          sign-in is unaffected. This cannot be undone.
        </>
      }
      onCancel={(event) => {
        event.preventDefault();
        if (!del.isPending) {
          onCancel();
        }
      }}
      actions={
        <Button type="button" onClick={onCancel} disabled={del.isPending}>
          Cancel
        </Button>
      }
    >
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
    </Dialog>
  );
}
