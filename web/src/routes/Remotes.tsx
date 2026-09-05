import { QueryClientProvider, useQuery } from '@tanstack/react-query';
import { useMemo, useRef, useState, type FormEvent } from 'react';
import { generatePath, Link } from 'react-router';

import { ApiError } from '../api/client.ts';
import { isoDay } from '../api/identities.ts';
import { useProjects } from '../api/settings.ts';
import {
  remoteStateText,
  safeOriginOf,
  stalenessText,
  useAddRemote,
  useAddWorkspaceOrigin,
  useConnections,
  useInstanceDirectory,
  useMintConnection,
  useRemotes,
  useRemoveRemote,
  useRemoveWorkspaceOrigin,
  useRenameRemote,
  useRevokeConnection,
  useWorkspaceOrigins,
  type InstanceConnection,
  type MintedConnectionValue,
  type Remote,
} from '../api/remotes.ts';
import { useOrgs } from '../api/session.ts';
import { WorkspaceContextProvider, withRemote } from '../api/transport.tsx';
import {
  type InstanceUpdateJob,
  jobReadErrorVisible,
  updateJobOutcome,
  useRemoteUpdateJob,
  useRemoteUpdateStatuses,
  useRequestRemoteUpdate,
} from '../api/updates.ts';
import {
  forgetWorkspace,
  livenessPollMs,
  probeWorkspace,
  useWorkspaces,
  WorkspaceError,
  type WorkspaceBearer,
} from '../api/workspace.ts';
import { createWorkspaceClient } from '../api/workspaceClient.ts';
import { writeClipboard } from '../app/clipboard.ts';
import { makeQueryClient } from '../app/queryClient.ts';
import { surfaceById } from '../app/navigation.ts';
import { useNavigationGuard } from './MachineAccess.tsx';
import { useModalDialog } from './useModalDialog.ts';
import { useWorkspaceHandoff, workspaceHandoffAction } from './useWorkspaceHandoff.ts';

/**
 * The multi-instance surface (registry surface `remotes`).
 *
 * Both directions of the relationship live here, because an instance is both at
 * once and there is no "main" anywhere in this design:
 *
 *   - **Directory** (this instance VIEWING others): one card per entry, with
 *     the state, the last-known org and project names, and the "open workspace"
 *     launcher.
 *   - **Consent** (this instance being VIEWED): the origin allowlist, whose
 *     removal is an atomic kill switch rather than a headers change.
 *
 * The cards refresh on a POLL. That is the ADR's own answer and not a
 * simplification: the live channel is `EventSource`, `EventSource` cannot set
 * an `Authorization` header, and weakening SSE authentication to close the gap
 * is forbidden by name.
 */
export function Remotes() {
  const remotes = useRemotes();

  return (
    <>
      <section className="card" aria-labelledby="remotes-title">
        <h1 id="remotes-title">Remotes</h1>
        <p>
          Other Hikyo instances this one knows about. The directory is fetched by this server under
          a pinned credential; opening a workspace is your browser talking to that instance
          directly, as you.
        </p>

        {remotes.isError ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              The remote directory could not be read. You may not hold{' '}
              <span className="mono">instance-directory</span> on this instance.
            </span>
          </p>
        ) : null}

        {remotes.isSuccess && remotes.data.items.length === 0 ? (
          <p role="status">
            No remotes yet. Add one below — you will need its URL, its certificate fingerprint and a
            connection credential, which whoever runs that instance mints from the{' '}
            <strong>Connection credentials</strong> section of its own Remotes page.
          </p>
        ) : null}

        <ul className="remotes">
          {(remotes.data?.items ?? []).map((remote) => (
            <RemoteCard key={remote.id} remote={remote} />
          ))}
        </ul>
      </section>

      <ThisInstance />
      <AddRemote />
      <ConnectionCredentials />
      <OriginAllowlist />
    </>
  );
}

/** What this instance publishes under instance-directory, never tenant-list surrogates. */
export function ThisInstance() {
  const directory = useInstanceDirectory();
  return (
    <section className="card" aria-labelledby="this-instance-title">
      <h2 id="this-instance-title">This instance</h2>
      <p>The identity and directory this instance shares with connected instances.</p>
      {directory.isPending ? <p role="status">Loading this instance's directory…</p> : null}
      {directory.isError ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">!</span>
          <span>{directory.error instanceof ApiError && directory.error.status === 403
            ? 'You do not hold instance-directory on this instance. Its directory is not available to you.'
            : "This instance's directory could not be read. Reload to try again."}</span>
        </p>
      ) : directory.data === undefined ? null : (
        <>
          <dl className="remote__facts">
            <dt>Identity</dt><dd className="mono">{directory.data.identity}</dd>
            <dt>Version</dt><dd className="mono">{directory.data.version}</dd>
            <dt>Organisations</dt><dd>{directory.data.org_count}</dd>
            <dt>Projects</dt><dd>{directory.data.project_count}</dd>
          </dl>
          {directory.data.orgs.length === 0 ? <p>No organisations on this instance.</p> : (
            <ul className="remote__orgs">
              {directory.data.orgs.map((org) => (
                <li key={org.name}>
                  <span className="mono">{org.name}</span>
                  <span className="remote__projects">{org.projects.join(', ') || 'no projects'}</span>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </section>
  );
}

function RemoteCard({ remote }: { remote: Remote }) {
  const rename = useRenameRemote();
  const remove = useRemoveRemote();
  const workspaces = useWorkspaces();
  const [failure, setFailure] = useState<string | null>(null);
  const [ended, setEnded] = useState(false);
  const origin = safeOriginOf(remote.url);
  const live = workspaces.find((w) => w.origin === origin);
  const handoff = useWorkspaceHandoff(origin, {
    // A live card hides the launcher and must not stage an unused transaction.
    preparation:
      live === undefined
        ? { kind: 'establishment' }
        : { kind: 'unavailable', message: 'This workspace is already open.' },
    onFailMessage: (error) =>
      error instanceof WorkspaceError
        ? error.message
        : 'The workspace could not be opened. Check that this instance allowlists this origin.',
  });
  const updateStatuses = useRemoteUpdateStatuses(live === undefined ? [] : [live]);
  const updateProbe = updateStatuses[0];
  const update = updateProbe?.status;
  const requestUpdate = useRequestRemoteUpdate();
  const [updateJobID, setUpdateJobID] = useState<string>();
  const updateJob = useRemoteUpdateJob(origin, updateJobID);
  const updateOutcome =
    updateJob.data === undefined ? undefined : updateJobOutcome(updateJob.data);
  const staleness = stalenessText(remote);
  useWorkspaceLiveness(live, () => setEnded(true));
  const handoffAction = workspaceHandoffAction(
    handoff,
    {
      ready: `Continue to ${origin} to sign in`,
      authorising: 'Waiting for sign-in…',
    },
    () => {
      setEnded(false);
      handoff.retry();
    },
  );

  const applyUpdate = async () => {
    if (update?.latest_version === undefined) {
      return;
    }
    setFailure(null);
    try {
      const job = await requestUpdate.mutateAsync({ origin, version: update.latest_version });
      setUpdateJobID(job.id);
    } catch (error) {
      setFailure(
        error instanceof ApiError && error.status === 403
          ? `Sign out and back in on ${origin}, then reconnect to confirm this update with fresh authentication.`
          : error instanceof ApiError && error.status === 409
            ? 'The remote updater refused the request because another update is active.'
            : error instanceof ApiError
              ? `The remote updater request failed with HTTP ${error.status}.`
              : 'The remote updater request failed. Check the remote instance logs.',
      );
    }
  };

  return (
    <li className="remote">
      <div className="remote__head">
        <h2 className="remote__name">{remote.name}</h2>
        {/* The state is TEXT first. The badge's colour is decoration on top of
            a sentence that already says everything. */}
        <span className="badge" data-state={remote.state}>
          {remote.state}
        </span>
      </div>
      <p className="mono remote__url">{remote.url}</p>

      <p className={remote.state === 'ok' ? 'remote__state' : 'alert'} role="status">
        {remote.state === 'ok' ? null : (
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
        )}
        <span>
          {remoteStateText(remote)}
          {/* A rejected credential is fixed on the PEER's own Connection
              credentials section — link straight to it rather than describe
              where it lives (AC#1). */}
          {remote.state === 'credential-rejected' ? (
            <>
              {' '}
              <a href={`${origin}/remotes#connection-credentials`} target="_blank" rel="noreferrer">
                Manage connection credentials on {origin}
              </a>
              .
            </>
          ) : null}
        </span>
      </p>
      {staleness === null ? null : (
        <p className="remote__stale" role="status">
          {staleness}
        </p>
      )}

      <dl className="remote__facts">
        <dt>Identity</dt>
        <dd className="mono">{remote.identity ?? 'not yet observed'}</dd>
        <dt>Version</dt>
        <dd className="mono">{remote.version ?? 'unknown'}</dd>
        <dt>Organisations</dt>
        <dd>{remote.org_count ?? 0}</dd>
        <dt>Projects</dt>
        <dd>{remote.project_count ?? 0}</dd>
      </dl>

      {remote.orgs === undefined || remote.orgs.length === 0 ? null : (
        <ul className="remote__orgs">
          {remote.orgs.map((org) => (
            <li key={org.name}>
              <span className="mono">{org.name}</span>
              <span className="remote__projects">{org.projects.join(', ') || 'no projects'}</span>
            </li>
          ))}
        </ul>
      )}

      {failure === null ? null : (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{failure}</span>
        </p>
      )}

      {live !== undefined || handoff.phase.kind !== 'failed' ? null : (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{handoff.phase.message}</span>
        </p>
      )}

      {updateProbe?.error === null || updateProbe?.error === undefined ? null : (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">!</span>
          <span>The remote update check failed. Reload or inspect the remote instance logs.</span>
        </p>
      )}

      {ended && live === undefined ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            Workspace session ended — that instance revoked it, withdrew consent for this origin,
            or became unreachable. Reconnect to continue.
          </span>
        </p>
      ) : null}

      {live !== undefined && update?.available === true ? (
        <div className="remote__update">
          <p>
            Hikyo <span className="mono">{update.latest_version}</span> is available on the{' '}
            {update.channel} channel.
          </p>
          {update.prerelease || update.channel !== 'stable' ? (
            <p>Prerelease builds are notification-only and cannot be remotely applied.</p>
          ) : update.apply_supported ? (
            <button
              className="btn btn--primary"
              type="button"
              onClick={applyUpdate}
              disabled={requestUpdate.isPending || updateOutcome?.kind === 'running'}
            >
              {requestUpdate.isPending ? 'Submitting…' : `Update remote to ${update.latest_version}`}
            </button>
          ) : (
            <p>
              {update.apply_error ??
                'This remote has no local updater helper configured; notification remains available.'}
            </p>
          )}
          {updateJobID === undefined ? null : (
            <UpdateJobStatus jobID={updateJobID} job={updateJob.data} />
          )}
          {jobReadErrorVisible(updateJob.isError, updateJob.data) ? (
            <p className="alert" role="alert">
              The update job status could not be read. Inspect the remote instance logs before retrying.
            </p>
          ) : null}
        </div>
      ) : null}

      <div className="remote__actions">
        {live === undefined ? (
          <button
            className="btn btn--primary"
            type="button"
            onClick={handoffAction.onClick}
            disabled={handoffAction.disabled}
          >
            {handoffAction.label}
          </button>
        ) : null}
        {live === undefined ? null : (
          <>
            <span className="badge" role="status">
              Workspace open
            </span>
            <button className="btn" type="button" onClick={() => forgetWorkspace(origin)}>
              Close workspace
            </button>
          </>
        )}
        <button
          className="btn"
          type="button"
          onClick={() => {
            const next = globalThis.prompt('New display name', remote.name);
            if (next !== null && next !== '' && next !== remote.name) {
              rename.mutate({ remote: remote.name, name: next });
            }
          }}
        >
          Rename
        </button>
        <button className="btn" type="button" onClick={() => remove.mutate(remote.name)}>
          Remove
        </button>
      </div>
      {live === undefined ? null : <WorkspacePicker origin={origin} remoteName={remote.name} />}
      {remove.isSuccess ? (
        <p role="status">
          Removed here. That does <strong>not</strong> revoke the credential — revoke it in the
          Connection credentials section of{' '}
          <a href={`${origin}/remotes#connection-credentials`} target="_blank" rel="noreferrer">
            {origin}
          </a>{' '}
          too.
        </p>
      ) : null}
    </li>
  );
}

/**
 * Renders a terminal update-job outcome. A `failed` outcome (`failed`,
 * `rolled-back`, or `rollback-failed`) surfaces as an `alert` region carrying
 * the diagnostic `failure_code` — the instance did not reach the requested
 * version and needs an operator. Everything else stays a plain status line.
 * The `isError` alert (query could not read the job) is a separate concern the
 * caller renders — and the caller suppresses it while this shows a `failed`
 * outcome, since a refetch error can arrive with the last terminal `data` still
 * cached and the two must not double up.
 */
export function UpdateJobStatus({
  jobID,
  job,
}: {
  jobID: string;
  job: InstanceUpdateJob | undefined;
}) {
  const outcome = job === undefined ? undefined : updateJobOutcome(job);
  if (outcome?.kind === 'failed') {
    return (
      <p className="alert" role="alert">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          Update job <span className="mono">{jobID}</span> {job?.state}
          {job?.phase === undefined ? '' : ` (${job.phase})`}
          {outcome.failureCode === undefined ? '' : ` — ${outcome.failureCode}`}. Inspect the
          remote instance logs.
        </span>
      </p>
    );
  }
  return (
    <p role="status">
      Update job <span className="mono">{jobID}</span>: {job?.state ?? 'queued'}
      {job?.phase === undefined ? '' : ` (${job.phase})`}
    </p>
  );
}

export function AddRemote() {
  const add = useAddRemote();
  const remotes = useRemotes();
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [pin, setPin] = useState('');
  const [credential, setCredential] = useState('');
  const [validationFailure, setValidationFailure] = useState<string | null>(null);

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const trimmedURL = url.trim();
    const submittedOrigin = remoteOriginForSubmit(trimmedURL);
    if (submittedOrigin === null) {
      setValidationFailure(
        'Enter a bare HTTPS origin, for example https://hikyo.example, with no path, query, fragment, or user information.',
      );
      return;
    }
    const duplicate = remotes.data?.items.find(
      (remote) => safeOriginOf(remote.url) === submittedOrigin,
    );
    if (duplicate !== undefined) {
      setValidationFailure(`This origin is already added as ${duplicate.name}.`);
      return;
    }

    setValidationFailure(null);
    add.mutate(
      { name, url: submittedOrigin, spkiPin: pin, credential },
      {
        onSuccess: () => {
          setName('');
          setUrl('');
          setPin('');
          setCredential('');
        },
      },
    );
  };

  return (
    <section className="card" aria-labelledby="add-remote-title">
      <h2 id="add-remote-title">Add a remote</h2>
      <p>
        The URL must be <span className="mono">https</span> and the fingerprint is checked on every
        connection — it replaces the certificate authority as the trust root, which is what makes a
        self-signed instance on your own network safe to point at.
      </p>
      <form className="form" onSubmit={onSubmit} noValidate>
        {validationFailure !== null || add.isError ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              {validationFailure ?? addFailureText(add.error)}
            </span>
          </p>
        ) : null}
        <div className="field">
          <label htmlFor="remote-name">Name</label>
          <input id="remote-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </div>
        <div className="field">
          <label htmlFor="remote-url">URL</label>
          <input
            id="remote-url"
            type="url"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
              setValidationFailure(null);
            }}
            placeholder="https://hikyo.example"
            aria-invalid={validationFailure !== null}
            required
          />
        </div>
        <div className="field">
          <label htmlFor="remote-pin">Certificate fingerprint</label>
          <input id="remote-pin" value={pin} onChange={(e) => setPin(e.target.value)} required />
        </div>
        <div className="field">
          <label htmlFor="remote-credential">Connection credential</label>
          <input
            id="remote-credential"
            type="password"
            autoComplete="off"
            value={credential}
            onChange={(e) => setCredential(e.target.value)}
            required
          />
        </div>
        <button className="btn btn--primary" type="submit" disabled={add.isPending}>
          {add.isPending ? 'Verifying…' : 'Add remote'}
        </button>
      </form>
    </section>
  );
}

/**
 * The origin allowlist: what THIS instance consents to be operated from.
 *
 * Removal is presented as what it is. De-allowlisting revokes every workspace
 * session bound to that origin in the same transaction, so the confirmation
 * says "kill" rather than "remove" and the result reports the body count.
 */
function OriginAllowlist() {
  const origins = useWorkspaceOrigins();
  const add = useAddWorkspaceOrigin();
  const remove = useRemoveWorkspaceOrigin();
  const [origin, setOrigin] = useState('');

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    add.mutate(origin, { onSuccess: () => setOrigin('') });
  };

  return (
    <section className="card" aria-labelledby="allowlist-title">
      <h2 id="allowlist-title">Workspace origins</h2>
      <p>
        The sites allowed to operate <strong>this</strong> instance from a browser. Exact origins
        only — no wildcards, no subdomain matching. What you are trusting is the code served at that
        origin.
      </p>

      {origins.isError ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            The allowlist could not be read. It is gated on{' '}
            <span className="mono">instance-config</span>.
          </span>
        </p>
      ) : null}

      {origins.isSuccess && origins.data.items.length === 0 ? (
        <p role="status">No origins allowlisted. No browser can operate this instance remotely.</p>
      ) : null}

      <ul className="origins">
        {(origins.data?.items ?? []).map((entry) => (
          <li key={entry.origin} className="origin">
            <span className="mono">{entry.origin}</span>
            <button
              className="btn"
              type="button"
              aria-label={`Remove ${entry.origin} and kill its workspace sessions`}
              onClick={() => remove.mutate(entry.origin)}
            >
              Remove
            </button>
          </li>
        ))}
      </ul>

      {remove.isSuccess ? (
        <p role="status">
          Removed <span className="mono">{remove.data.origin}</span> and revoked{' '}
          {remove.data.sessions_revoked} workspace session
          {remove.data.sessions_revoked === 1 ? '' : 's'} bound to it.
        </p>
      ) : null}

      <form className="form form--inline" onSubmit={onSubmit} noValidate>
        {add.isError ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>That origin was refused. It must be a bare scheme, host and port.</span>
          </p>
        ) : null}
        <div className="field">
          <label htmlFor="origin">Origin</label>
          <input
            id="origin"
            value={origin}
            onChange={(e) => setOrigin(e.target.value)}
            placeholder="https://shell.example"
            required
          />
        </div>
        <button className="btn btn--primary" type="submit" disabled={add.isPending}>
          Allow origin
        </button>
      </form>
    </section>
  );
}

/**
 * The receiving-side credential surface (#498): mint, inventory and revoke the
 * connection credentials THIS instance issues for a peer to hold.
 *
 * It is deliberately NOT the same thing as a directory card. A card is this
 * instance VIEWING a peer, holding a credential minted over there. This is the
 * SERVING side: the write-only credentials a peer presents at our own directory
 * fetch. The two look alike — both say "credential" — so the copy names the
 * distinction rather than trusting the layout to carry it.
 */
/** A minted value paired with the label the operator typed for it. */
type Disclosed = MintedConnectionValue & { readonly label: string };

export function ConnectionCredentials() {
  const connections = useConnections();
  const mint = useMintConnection();
  const [disclosed, setDisclosed] = useState<Disclosed | null>(null);
  const [revoking, setRevoking] = useState<InstanceConnection | null>(null);

  const items = connections.data?.items ?? [];

  return (
    <section className="card" id="connection-credentials" aria-labelledby="connections-title">
      <h2 id="connections-title">Connection credentials</h2>
      <p>
        The credentials <strong>this</strong> instance issues for another to hold. A peer presents
        one at this instance&apos;s server-side directory fetch — it is the write-only counterpart to
        the connection credential you paste into <strong>Add a remote</strong> over on the viewing
        instance. It is <strong>not</strong> a directory card: a card is this instance reading a
        peer, this is a peer reading us.
      </p>

      {connections.isError ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            The connection credentials could not be read. It is gated on{' '}
            <span className="mono">instance-config</span>.
          </span>
        </p>
      ) : null}

      {connections.isSuccess && items.length === 0 ? (
        <p role="status">
          None minted. A peer cannot fetch this instance&apos;s directory until you mint one here and
          hand the value over.
        </p>
      ) : null}

      <ul className="connections">
        {items.map((connection) => (
          <ConnectionRow
            key={connection.id}
            connection={connection}
            onRevoke={() => setRevoking(connection)}
          />
        ))}
      </ul>

      <MintConnectionForm mint={mint} onMinted={setDisclosed} />

      {disclosed === null ? null : (
        <ConnectionMintDialog minted={disclosed} onClose={() => setDisclosed(null)} />
      )}
      {revoking === null ? null : (
        <RevokeConnectionDialog connection={revoking} onClose={() => setRevoking(null)} />
      )}
    </section>
  );
}

/** ConnectionRow is one inventory entry: bounded metadata, no plaintext. */
function ConnectionRow({
  connection,
  onRevoke,
}: {
  connection: InstanceConnection;
  onRevoke: () => void;
}) {
  const revokedAt = connection.revoked_at;
  const revoked = revokedAt !== undefined;
  const state = revoked ? 'revoked' : connection.live ? 'live' : 'expired';
  return (
    <li className="connection">
      <div className="connection__head">
        <h3 className="connection__label">{connection.label}</h3>
        {/* State as text first; the badge colour is decoration on a word. */}
        <span className="badge" data-state={state}>
          {state}
        </span>
      </div>
      <p className="mono connection__prefix">{connection.prefix_hint}…</p>
      <dl className="connection__facts">
        <dt>Kind</dt>
        <dd className="mono">{connection.kind}</dd>
        <dt>Lifetime</dt>
        <dd>
          {connection.lifetime === 'indefinite'
            ? 'indefinite'
            : connection.expires_at === undefined
              ? 'finite'
              : `expires ${isoDay(connection.expires_at)}`}
        </dd>
        <dt>Created</dt>
        <dd>
          {isoDay(connection.created_at)} by <span className="mono">{connection.created_by}</span>
        </dd>
        <dt>Last used</dt>
        <dd>{connection.last_used_at === undefined ? 'never used' : isoDay(connection.last_used_at)}</dd>
        {revokedAt === undefined ? null : (
          <>
            <dt>Revoked</dt>
            <dd>{isoDay(revokedAt)}</dd>
          </>
        )}
      </dl>
      {revoked ? null : (
        <div className="connection__actions">
          <button
            className="btn"
            type="button"
            aria-label={`Revoke ${connection.label}`}
            onClick={onRevoke}
          >
            Revoke
          </button>
        </div>
      )}
    </li>
  );
}

type LifetimeChoice = 'default' | 'custom' | 'indefinite';

/**
 * MintConnectionForm names lifetime as a RADIO so the both-named 400 is
 * unreachable: the contract refuses a request that carries `lifetime_seconds`
 * and `indefinite` at once, so the UI can only ever send one of them.
 */
function MintConnectionForm({
  mint,
  onMinted,
}: {
  mint: ReturnType<typeof useMintConnection>;
  onMinted: (result: Disclosed) => void;
}) {
  const [label, setLabel] = useState('');
  const [choice, setChoice] = useState<LifetimeChoice>('default');
  const [days, setDays] = useState('30');

  // The converted seconds, or null when the custom field is not a positive
  // whole-second lifetime the contract (minimum 1) would accept. `0.0001` days
  // rounds to zero and a huge value overflows to non-finite; both are refused
  // here rather than sent for the server to 400.
  const customSeconds = customLifetimeSeconds(days);
  const customInvalid = choice === 'custom' && customSeconds === null;

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmed = label.trim();
    if (trimmed === '' || mint.pending || customInvalid) {
      return;
    }
    const input =
      choice === 'indefinite'
        ? { label: trimmed, indefinite: true }
        : choice === 'custom' && customSeconds !== null
          ? { label: trimmed, lifetimeSeconds: customSeconds }
          : { label: trimmed };
    void mint.mint(input).then(
      (result) => {
        onMinted({ ...result, label: trimmed });
        setLabel('');
        setChoice('default');
        setDays('30');
      },
      () => {
        // The failure is surfaced from mint.error; nothing to do here.
      },
    );
  };

  return (
    <form className="form" onSubmit={onSubmit} noValidate>
      <h3>Mint a credential</h3>
      <p>
        The label names the peer for the audit trail. It is descriptive, not enforced — this
        instance cannot verify who holds the value.
      </p>
      {mint.error !== null ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{mintFailureText(mint.error)}</span>
        </p>
      ) : null}
      <div className="field">
        <label htmlFor="connection-label">Label</label>
        <input
          id="connection-label"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          maxLength={200}
          required
        />
      </div>
      <fieldset className="field">
        <legend>Lifetime</legend>
        <div className="chk">
          <input
            id="lifetime-default"
            type="radio"
            name="lifetime"
            checked={choice === 'default'}
            onChange={() => setChoice('default')}
          />
          <label htmlFor="lifetime-default">Instance default</label>
        </div>
        <div className="chk">
          <input
            id="lifetime-custom"
            type="radio"
            name="lifetime"
            checked={choice === 'custom'}
            onChange={() => setChoice('custom')}
          />
          <label htmlFor="lifetime-custom">Expires after</label>
          <input
            id="lifetime-days"
            type="number"
            min={1}
            value={days}
            onChange={(e) => setDays(e.target.value)}
            disabled={choice !== 'custom'}
            aria-invalid={customInvalid}
            aria-label="Lifetime in days"
          />
          <span>days (clamped to the instance ceiling)</span>
        </div>
        <div className="chk">
          <input
            id="lifetime-indefinite"
            type="radio"
            name="lifetime"
            checked={choice === 'indefinite'}
            onChange={() => setChoice('indefinite')}
          />
          <label htmlFor="lifetime-indefinite">
            Never expires (only if this instance allows it)
          </label>
        </div>
      </fieldset>
      <button
        className="btn btn--primary"
        type="submit"
        disabled={mint.pending || label.trim() === '' || customInvalid}
      >
        {mint.pending ? 'Minting…' : 'Mint credential'}
      </button>
    </form>
  );
}

/**
 * ConnectionMintDialog is the display-once ceremony. The value exists only in
 * this dialog's props for as long as it is open: copy is best-effort, the
 * stored-confirmation gates dismissal, and a navigation while unstored is
 * routed through the same guard as Escape so the one disclosure is not lost to
 * a Back press or a reload.
 */
function ConnectionMintDialog({
  minted,
  onClose,
}: {
  minted: Disclosed;
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

  // Back, reload and tab close must not silently lose a value nothing returns.
  useNavigationGuard(!stored, dismiss);

  return (
    <dialog
      className="ceremony"
      aria-labelledby="connection-mint-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        dismiss();
      }}
    >
      <h2 className="ceremony__title" id="connection-mint-title">
        Connection credential minted — shown exactly once
      </h2>
      <p className="ceremony__scope">
        For <strong>{minted.label}</strong>. Hand this value to that peer; it goes into its{' '}
        <strong>Add a remote</strong> form as the connection credential.
      </p>
      <p className="mono machine__token">{minted.value}</p>
      {minted.clamped ? (
        <p className="notice" role="status">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>
            The instance lifetime ceiling shortened this credential. It expires earlier than asked
            for — said now rather than discovered when it dies.
          </span>
        </p>
      ) : null}
      <p className="ceremony__cap" role="status">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          This value is never retrievable again. The list shows metadata only. Store it in the
          consuming instance now; if it is lost, revoke this credential and mint a fresh one.
        </span>
      </p>
      <button
        className="btn"
        type="button"
        onClick={async () => {
          const result = await writeClipboard(minted.value);
          setCopyStatus(
            result === 'ok'
              ? 'Copied. The clipboard is now the only copy outside its target instance.'
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
          id="connection-stored"
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
        <label htmlFor="connection-stored">I have stored this credential in its target instance.</label>
      </div>
      {heldBack ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>Confirm you have stored it — there is no second look at this value.</span>
        </p>
      ) : null}
      <div className="ceremony__actions">
        <button className="btn btn--primary" type="button" onClick={dismiss}>
          Done
        </button>
      </div>
    </dialog>
  );
}

/**
 * RevokeConnectionDialog states the consequence before it commits (AC#4).
 *
 * Revocation retires the credential and its principal. It does NOT touch
 * workspace sessions — those are governed by the origin allowlist, not this
 * credential — so the copy says exactly what it does and does not do, and a
 * double revoke surfaces the 409 rather than pretending a second act happened.
 */
function RevokeConnectionDialog({
  connection,
  onClose,
}: {
  connection: InstanceConnection;
  onClose: () => void;
}) {
  const cancel = useRef<HTMLButtonElement>(null);
  const dialog = useModalDialog(cancel);
  const revoke = useRevokeConnection();

  const run = () => {
    revoke.mutate(connection.id, { onSuccess: onClose });
  };

  return (
    <dialog
      className="ceremony"
      aria-labelledby="connection-revoke-title"
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        if (!revoke.isPending) {
          onClose();
        }
      }}
    >
      <h2 className="ceremony__title" id="connection-revoke-title">
        Revoke {connection.label}?
      </h2>
      <p className="ceremony__scope">
        The credential and its principal are retired together. The peer holding it loses this
        instance&apos;s directory fetch at its <strong>next</strong> presentation — its card over
        there flips to <span className="mono">credential rejected</span>. Active workspace sessions
        are <strong>unaffected</strong>: those follow the origin allowlist, not this credential.
      </p>
      {revoke.isError ? (
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>{revokeFailureText(revoke.error)}</span>
        </p>
      ) : null}
      <div className="ceremony__actions">
        <button
          className="btn btn--primary"
          type="button"
          onClick={run}
          disabled={revoke.isPending}
        >
          {revoke.isPending ? 'Revoking…' : 'Revoke credential'}
        </button>
        <button
          className="btn"
          type="button"
          ref={cancel}
          onClick={onClose}
          disabled={revoke.isPending}
        >
          Cancel
        </button>
      </div>
    </dialog>
  );
}

/**
 * customLifetimeSeconds converts the days field to seconds, or null when the
 * result is not a lifetime the contract accepts. The contract's floor is one
 * second; the field's floor is one day, so this also mirrors `min={1}`. It
 * refuses fractional-day inputs that round below a second and any value large
 * enough to overflow a safe integer.
 */
function customLifetimeSeconds(days: string): number | null {
  const parsed = Number(days);
  if (!Number.isFinite(parsed) || parsed < 1) {
    return null;
  }
  const seconds = Math.round(parsed * 86_400);
  return Number.isSafeInteger(seconds) && seconds >= 1 ? seconds : null;
}

function mintFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return 'That mint was refused: the label must be present, and a finite lifetime and “never expires” cannot both be named. “Never expires” also requires this instance to allow indefinite credentials.';
      case 429:
        return 'Too many attempts right now. Wait a moment and try again.';
      default:
        return `The credential could not be minted (server error ${error.status}).`;
    }
  }
  return 'The credential could not be minted: the server could not be reached, or it answered something this client does not understand.';
}

function revokeFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 409:
        return 'This credential was already revoked — nothing more to do, and no second event recorded.';
      case 429:
        return 'Too many attempts right now. Wait a moment and try again.';
      default:
        return `The credential could not be revoked (server error ${error.status}).`;
    }
  }
  return 'The credential could not be revoked: the server could not be reached, or it answered something this client does not understand.';
}

/**
 * useWorkspaceLiveness keeps the card honest about a session it does not own.
 *
 * Both of the remote's kill switches — de-allowlisting this origin and revoking
 * the session in its own active-session list — take effect at the remote's next
 * request. This IS that request, so a workspace killed over there stops
 * claiming to be open over here within one poll rather than at the next thing
 * the human tries to do.
 */
function useWorkspaceLiveness(bearer: WorkspaceBearer | undefined, onEnded: () => void): void {
  useQuery({
    queryKey: ['workspace-liveness', bearer?.origin ?? '', bearer?.session ?? ''],
    queryFn: async () => {
      if (bearer === undefined) {
        return true;
      }
      const alive = await probeWorkspace(bearer);
      if (!alive) {
        onEnded();
      }
      return alive;
    },
    enabled: bearer !== undefined,
    refetchInterval: livenessPollMs,
    staleTime: 0,
  });
}

/**
 * WorkspacePicker is the bridge from "workspace open" to "operating a project":
 * it reads the REMOTE's own orgs and projects over the bearer and renders each
 * as a deep link into the matrix, tagged `?remote=<name>`.
 *
 * It reads live rather than from the directory snapshot for a load-bearing
 * reason: the matrix is addressed by org and project IDS, and the snapshot
 * carries only names. The ids exist only on the remote, so the shell resolves
 * them there, over the same bearer everything else in the workspace uses — which
 * is why this renders inside the workspace transport with its own isolated
 * cache, exactly like the surfaces it links to.
 */
function WorkspacePicker({ origin, remoteName }: { origin: string; remoteName: string }) {
  const [queries] = useState(() => makeQueryClient());
  const client = useMemo(() => createWorkspaceClient(origin), [origin]);
  return (
    <QueryClientProvider client={queries}>
      <WorkspaceContextProvider value={{ origin, remote: remoteName, client }}>
        <PickerBody remoteName={remoteName} />
      </WorkspaceContextProvider>
    </QueryClientProvider>
  );
}

function PickerBody({ remoteName }: { remoteName: string }) {
  const orgs = useOrgs(true);
  if (orgs.isPending) {
    return (
      <p role="status" className="remote__picker">
        Loading this instance&apos;s projects…
      </p>
    );
  }
  if (orgs.isError) {
    return (
      <p className="alert" role="alert">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>The remote&apos;s projects could not be read. Your grants over there may not cover them.</span>
      </p>
    );
  }
  if (orgs.data.items.length === 0) {
    return (
      <p role="status" className="remote__picker">
        You have access to no organisations on this remote.
      </p>
    );
  }
  return (
    <div className="remote__picker">
      <h3>Open a project</h3>
      {orgs.data.items.map((org) => (
        <OrgProjects key={org.id} orgId={org.id} orgName={org.name} remoteName={remoteName} />
      ))}
    </div>
  );
}

function OrgProjects({
  orgId,
  orgName,
  remoteName,
}: {
  orgId: string;
  orgName: string;
  remoteName: string;
}) {
  const projects = useProjects(orgId);
  const items = projects.data?.items ?? [];
  if (items.length === 0) {
    return null;
  }
  return (
    <div className="remote__picker-org">
      <p className="mono">{orgName}</p>
      <ul>
        {items.map((project) => (
          <li key={project.id}>
            <Link
              className="btn"
              to={withRemote(
                generatePath(surfaceById('matrix').path, { org: orgId, project: project.id }),
                remoteName,
              )}
            >
              {project.name}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Mirror the server's bare-HTTPS-origin grammar for client pre-flight checks. */
function remoteOriginForSubmit(url: string): string | null {
  if (!/^https:\/\/[^\s/?#@]+\/?$/.test(url)) {
    return null;
  }
  try {
    const parsed = new URL(url);
    if (
      parsed.protocol !== 'https:' ||
      parsed.hostname === '' ||
      parsed.username !== '' ||
      parsed.password !== '' ||
      parsed.pathname !== '/' ||
      parsed.search !== '' ||
      parsed.hash !== ''
    ) {
      return null;
    }
    return parsed.origin;
  } catch {
    return null;
  }
}

function addFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return 'That entry was refused: the URL must be a bare https origin and the fingerprint must be the base64 SHA-256 of the public key.';
      case 409:
        return 'The server could not verify this remote. Check that it is reachable, its credential and fingerprint are current, and it is not this instance or an instance already listed under another URL.';
      case 429:
        return 'Too many attempts right now. Wait a moment and try again.';
      default:
        return `The remote could not be added (server error ${error.status}).`;
    }
  }
  return 'The remote could not be added: the server could not be reached, or it answered something this client does not understand.';
}
