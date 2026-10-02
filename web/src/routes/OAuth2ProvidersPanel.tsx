import { useState } from 'react';
import { ApiError } from '../api/client.ts';
import {
  useSensitiveMutation,
  useSensitiveState,
} from '../api/sensitiveMutation.ts';
import {
  deleteOAuth2Provider,
  putOAuth2Provider,
  useOAuth2Providers,
  type OAuth2Provider,
} from '../api/oauth2Providers.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Input } from '../ui/Input.tsx';
import { Panel, TypedNameConfirm } from './Sections.tsx';

export function OAuth2ProvidersPanel() {
  const query = useOAuth2Providers();
  const [editing, setEditing] = useState<OAuth2Provider | 'create' | null>(
    null,
  );
  const [deleting, setDeleting] = useState<OAuth2Provider | null>(null);
  const [message, setMessage] = useState('');
  const remove = useSensitiveMutation({
    mutationFn: deleteOAuth2Provider,
    onSuccess: () => {
      setDeleting(null);
      setMessage('Provider deleted. Its sessions have ended.');
      void query.refetch();
    },
  });
  const forbidden =
    query.error instanceof ApiError && query.error.status === 403;
  return (
    <Panel id="instance-oauth2" title="GitHub sign-in">
      <p>
        GitHub identities use one sign-in factor. Enrol a local authenticator or
        passkey for protected actions.
      </p>
      {query.isPending ? <p role="status">Loading GitHub providers…</p> : null}
      {query.isError ? (
        <Alert>
          {forbidden
            ? 'Provider administration requires a second factor.'
            : 'Providers could not be loaded.'}
        </Alert>
      ) : null}
      {message ? <p role="status">{message}</p> : null}
      {query.isSuccess ? (
        <>
          {query.data.providers.length === 0 ? (
            <p role="status">No GitHub provider configured.</p>
          ) : null}
          {query.data.providers.map((p) => (
            <div className="settings-row" key={p.slug}>
              <div className="settings-row__copy">
                <span className="settings-row__title">{p.display_name}</span>
                <span className="settings-row__detail mono">
                  {p.slug} · {p.profile}
                </span>
              </div>
              <span className="settings-row__spacer" />
              <Badge>{p.enabled ? 'enabled' : 'disabled'}</Badge>
              <Button
                type="button"
                onClick={() => {
                  setMessage('');
                  setEditing(p);
                }}
              >
                Reconfigure {p.display_name}
              </Button>
              <Button
                type="button"
                variant="danger"
                onClick={() => {
                  remove.reset();
                  setDeleting(p);
                }}
              >
                Delete {p.display_name}
              </Button>
            </div>
          ))}
          {editing === null ? (
            <Button
              type="button"
              onClick={() => {
                setMessage('');
                setEditing('create');
              }}
            >
              Add GitHub provider
            </Button>
          ) : (
            <OAuth2Editor
              key={editing === 'create' ? 'create' : editing.slug}
              provider={editing === 'create' ? null : editing}
              onCancel={() => setEditing(null)}
              onSaved={() => {
                setEditing(null);
                setMessage(
                  'Provider saved. Existing provider sessions have ended.',
                );
                void query.refetch();
              }}
            />
          )}
        </>
      ) : null}
      {deleting ? (
        <Dialog
          title={`Delete ${deleting.display_name}?`}
          lede="The sign-in option disappears and its sessions end. Linked identities remain for operator reconciliation."
          onCancel={(event) => {
            event.preventDefault();
            if (!remove.isPending) setDeleting(null);
          }}
          actions={
            <Button
              type="button"
              disabled={remove.isPending}
              onClick={() => setDeleting(null)}
            >
              Cancel
            </Button>
          }
        >
          {remove.isError ? (
            <Alert>Provider could not be deleted.</Alert>
          ) : null}
          <TypedNameConfirm
            label="Type the provider slug to confirm"
            expect={deleting.slug}
            hint="Deletion uses the immutable provider slug."
            action="Delete provider"
            busy={remove.isPending}
            onConfirm={() => remove.mutate(deleting.slug)}
          />
        </Dialog>
      ) : null}
    </Panel>
  );
}
function OAuth2Editor({
  provider,
  onCancel,
  onSaved,
}: {
  provider: OAuth2Provider | null;
  onCancel: () => void;
  onSaved: () => void;
}) {
  const [slug, setSlug] = useState(provider?.slug ?? 'github');
  const [name, setName] = useState(provider?.display_name ?? 'GitHub');
  const [client, setClient] = useState(provider?.client_id ?? '');
  const [secret, setSecret] = useSensitiveState('');
  const [enabled, setEnabled] = useState(provider?.enabled ?? true);
  const save = useSensitiveMutation({
    mutationFn: (value: string) =>
      putOAuth2Provider(slug, {
        profile: 'github',
        issuer: 'https://github.com',
        display_name: name,
        client_id: client,
        client_secret: value,
        enabled,
      }),
    onSuccess: onSaved,
    onSettled: () => setSecret(''),
  });
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        save.mutate(secret);
        setSecret('');
      }}
    >
      <p>Profile: GitHub · Origin: https://github.com</p>
      <Input
        id="oauth2-slug"
        label="Provider slug"
        value={slug}
        disabled={provider !== null || save.isPending}
        pattern="[a-z0-9][a-z0-9-]{0,63}"
        required
        onChange={(event) => setSlug(event.target.value)}
      />
      <Input
        id="oauth2-name"
        label="Display name"
        value={name}
        required
        disabled={save.isPending}
        onChange={(event) => setName(event.target.value)}
      />
      <Input
        id="oauth2-client"
        label="Client ID"
        value={client}
        required
        disabled={save.isPending}
        onChange={(event) => setClient(event.target.value)}
      />
      <Input
        id="oauth2-secret"
        label="Client secret"
        type="password"
        autoComplete="new-password"
        value={secret}
        required
        disabled={save.isPending}
        hint="Write-only. Re-enter the secret for every save."
        onChange={(event) => setSecret(event.target.value)}
      />
      <Checkbox
        label="Enable GitHub sign-in"
        checked={enabled}
        disabled={save.isPending}
        onChange={(event) => setEnabled(event.target.checked)}
      />
      {save.isError ? (
        <Alert>
          Provider could not be saved. Refresh the provider list before
          retrying.
        </Alert>
      ) : null}
      <Button variant="primary" type="submit" disabled={save.isPending}>
        {save.isPending ? 'Saving…' : 'Save provider'}
      </Button>
      <Button type="button" disabled={save.isPending} onClick={onCancel}>
        Cancel
      </Button>
    </form>
  );
}
