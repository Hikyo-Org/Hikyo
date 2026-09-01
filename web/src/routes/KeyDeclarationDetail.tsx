import { useEffect, useRef, useState, type RefObject } from 'react';
import { generatePath, Link, useNavigate } from 'react-router';

import { GIT_DEFINITIONS_NOTICE, useDefinitionsSettings } from '../api/definitions.ts';
import { historyHref } from '../api/history.ts';
import {
  keyMetadataRefusalText,
  useKey,
  useUpdateKeyMetadata,
  type MatrixKey,
  type MatrixRef,
} from '../api/matrix.ts';
import { ApiError } from '../api/client.ts';
import { useWorkspaceContext, withRemote } from '../api/transport.tsx';
import type { EnvironmentList } from '../api/values.ts';
import { surfaceById } from '../app/navigation.ts';
import { Alert, Done } from './Sections.tsx';

type Environment = EnvironmentList['items'][number];

/**
 * The catalogue declaration detail (#491) — the routable, reload-safe surface a
 * key name opens onto, and the shared foundation later create/edit/lifecycle
 * tickets extend.
 *
 * It inspects EVERY declaration and organisation field of one key. Nothing here
 * can leak a secret VALUE: the key record carries only declaration metadata (no
 * value field exists on it), and values live behind the reveal ceremony on the
 * Values surface. The one write is the metadata edit (folder, description,
 * deprecation) — the smallest complete journey, and the ingress a Surface-2
 * scanning block attaches to: a refused edit renders the server's caller-safe
 * detail (rule id + locator, never the matched text), and any findings already
 * recorded on the key are shown the same redacted way.
 *
 * Source mode governs action availability: a database-managed project exposes
 * the editor; a Git-managed project is read-only for declarations (values stay
 * editable elsewhere) behind the standing Git notice plus the last-applied
 * provenance labels, which are display-only and never trusted.
 */
export function KeyDeclarationDetail({
  refData,
  keyId,
  environments,
  openerRef,
}: {
  refData: MatrixRef;
  keyId: string;
  environments: readonly Environment[];
  openerRef: RefObject<HTMLAnchorElement | null>;
}) {
  const navigate = useNavigate();
  const workspace = useWorkspaceContext();
  const remote = workspace?.remote ?? '';
  const key = useKey(refData, keyId);
  const definitions = useDefinitionsSettings(refData.org, refData.project);

  const heading = useRef<HTMLHeadingElement>(null);

  // Back into the matrix, keeping the workspace: closing inside a workspace
  // must land on the remote's matrix, not this instance's (#71).
  const matrixPath = withRemote(generatePath(surfaceById('matrix').path, refData), remote);
  const historyPath = withRemote(historyHref({ ...refData, keyId }), remote);

  useEffect(() => {
    // Snapshot the opener at MOUNT and return focus to it on unmount — the
    // matrix stays behind this panel, and closing to it must return focus to
    // the key name that opened it. The matrix key list is the fallback when
    // that row has since re-rendered away.
    const opener = openerRef.current;
    heading.current?.focus();
    return () => {
      requestAnimationFrame(() => {
        // Do not steal focus from a surface that replaced this one: navigating
        // to the revision-history drawer unmounts this panel while the drawer
        // focuses its own heading. Only restore focus when it was left nowhere
        // (returned to the matrix), never on top of the next surface.
        const active = document.activeElement;
        if (active !== null && active !== document.body) {
          return;
        }
        if (opener?.isConnected === true) {
          opener.focus();
          return;
        }
        document.querySelector<HTMLAnchorElement>('.matrix__key')?.focus();
      });
    };
  }, [openerRef, keyId]);

  // Editing is available ONLY once the source is confirmed `db`. An unresolved
  // or failed settings query must fail closed — never show a live edit action
  // on a project that might be Git-managed (where declarations are read-only).
  const source = definitions.data?.definitions_source;
  const editable = source === 'db';
  const gitManaged = source === 'git';

  return (
    <aside
      className="key-detail"
      aria-label="Key declaration"
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.preventDefault();
          void navigate(matrixPath);
        }
      }}
    >
      <div className="key-detail__head">
        <h2 id="key-detail-title" ref={heading} tabIndex={-1}>
          {key.data?.name ?? 'Key declaration'}
        </h2>
        <Link className="btn key-detail__close" to={matrixPath} aria-label="Close key declaration">
          ✕ Close
        </Link>
      </div>

      {key.isPending ? (
        <p className="key-detail__state" role="status">
          Loading the key declaration…
        </p>
      ) : key.isError ? (
        <KeyLoadError error={key.error} matrixPath={matrixPath} />
      ) : (
        <KeyDeclarationBody
          refData={refData}
          keyId={keyId}
          record={key.data}
          environments={environments}
          editable={editable}
          gitManaged={gitManaged}
          sourceResolved={definitions.isSuccess}
          sourceFailed={definitions.isError}
          gitProvenance={definitions.data?.last_apply}
          historyPath={historyPath}
        />
      )}
    </aside>
  );
}

/**
 * A load failure that stays recoverable: a deleted or renamed-away key (404) is
 * a stale link, not a dead end, so it names the state and offers the way back;
 * a refusal (403) quotes the server; anything else is reported with its status.
 */
function KeyLoadError({ error, matrixPath }: { error: Error; matrixPath: string }) {
  const message =
    error instanceof ApiError && error.status === 404
      ? 'This key no longer exists — it may have been deleted or renamed. Its link is stale.'
      : error instanceof ApiError && error.status === 403
        ? 'You are not permitted to view this key.'
        : error instanceof ApiError
          ? `The key could not be loaded (error ${String(error.status)}).`
          : 'The key could not be loaded.';
  return (
    <div className="key-detail__body">
      <Alert>{message}</Alert>
      <Link className="btn" to={matrixPath}>
        Back to the matrix
      </Link>
    </div>
  );
}

function KeyDeclarationBody({
  refData,
  keyId,
  record,
  environments,
  editable,
  gitManaged,
  sourceResolved,
  sourceFailed,
  gitProvenance,
  historyPath,
}: {
  refData: MatrixRef;
  keyId: string;
  record: MatrixKey;
  environments: readonly Environment[];
  editable: boolean;
  gitManaged: boolean;
  sourceResolved: boolean;
  sourceFailed: boolean;
  gitProvenance:
    | { commit?: string; ref?: string; actor?: string; applied_by: string }
    | undefined;
  historyPath: string;
}) {
  const environmentName = (id: string) =>
    environments.find((environment) => environment.id === id)?.name ?? id;

  return (
    <div className="key-detail__body">
      <dl className="key-detail__facts">
        <Fact term="Name" value={record.name} mono />
        <Fact
          term="Classification"
          value={record.classification === 'secret' ? '🔒 secret' : 'config'}
        />
        <Fact term="Folder" value={record.folder_path === '' ? '(none)' : record.folder_path} mono />
        <Fact term="Group" value={record.group_id === '' ? '(ungrouped)' : record.group_id} mono />
        <Fact
          term="Description"
          value={record.description === '' ? '(none)' : record.description}
        />
        <Fact
          term="Deprecation"
          value={
            record.deprecated
              ? record.deprecation_note === ''
                ? 'deprecated'
                : `deprecated — ${record.deprecation_note}`
              : 'active'
          }
        />
      </dl>

      <section className="key-detail__section" aria-labelledby="key-detail-rules">
        <h3 id="key-detail-rules">Value rules</h3>
        <ValueRules declaration={record.declaration} />
      </section>

      <section className="key-detail__section" aria-labelledby="key-detail-presence">
        <h3 id="key-detail-presence">Presence</h3>
        <p className="key-detail__presence">
          <span className="key-detail__presence-label">Required in</span>{' '}
          {presenceText(record.presence.required_in, environmentName)}
        </p>
        <p className="key-detail__presence">
          <span className="key-detail__presence-label">Forbidden in</span>{' '}
          {presenceText(record.presence.forbidden_in, environmentName)}
        </p>
      </section>

      {record.findings === undefined || record.findings.length === 0 ? null : (
        <section className="key-detail__section" aria-labelledby="key-detail-findings">
          <h3 id="key-detail-findings">Scanning findings</h3>
          <ul className="key-detail__findings">
            {record.findings.map((finding, index) => (
              <li key={`${finding.rule_id}:${finding.locator}:${String(index)}`}>
                <span className="mono">{finding.rule_id}</span> at{' '}
                <span className="mono">{finding.locator}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <p className="key-detail__history">
        <Link className="btn" to={historyPath}>
          View this key’s revision history
        </Link>
      </p>

      {editable ? (
        <MetadataEditor refData={refData} keyId={keyId} record={record} />
      ) : gitManaged ? (
        <section className="key-detail__section" aria-labelledby="key-detail-git">
          <h3 id="key-detail-git">Declarations are read-only</h3>
          <Alert>{GIT_DEFINITIONS_NOTICE}</Alert>
          {gitProvenance === undefined ? null : (
            <p className="key-detail__provenance">
              Last applied by <span className="mono">{gitProvenance.applied_by}</span>
              {gitProvenance.actor === undefined ? '' : ` · actor ${gitProvenance.actor}`}
              {gitProvenance.commit === undefined ? '' : ` · commit ${gitProvenance.commit}`}
              {gitProvenance.ref === undefined ? '' : ` · ref ${gitProvenance.ref}`}
            </p>
          )}
        </section>
      ) : (
        // Source not yet confirmed: fail closed — no edit action until we know
        // this is a database-managed project. Pending is quiet; a failed
        // settings read says so, because "why can't I edit" must be answerable.
        <section className="key-detail__section" aria-labelledby="key-detail-source">
          <h3 id="key-detail-source">Editing unavailable</h3>
          {sourceFailed ? (
            <Alert>
              The project’s definitions source could not be read, so declaration editing is
              unavailable. Reload to try again.
            </Alert>
          ) : (
            <p className="key-detail__state" role="status">
              {sourceResolved
                ? 'Declaration editing is unavailable for this project.'
                : 'Checking whether declarations can be edited…'}
            </p>
          )}
        </section>
      )}
    </div>
  );
}

function Fact({
  term,
  value,
  mono = false,
}: {
  term: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="key-detail__fact">
      <dt>{term}</dt>
      <dd className={mono ? 'mono' : undefined}>{value}</dd>
    </div>
  );
}

/** presenceText renders a presence rule symbolically — `all` covers future
 *  environments, so it is never expanded to today's id list. */
function presenceText(
  presence: MatrixKey['presence']['required_in'],
  environmentName: (id: string) => string,
): string {
  switch (presence.mode) {
    case 'all':
      return 'every environment, including any created later';
    case 'none':
      return 'no environments';
    case 'explicit':
      return (presence.environment_ids ?? []).length === 0
        ? 'no environments'
        : (presence.environment_ids ?? []).map(environmentName).join(', ');
  }
}

function ValueRules({ declaration }: { declaration: MatrixKey['declaration'] }) {
  if (declaration.rule !== undefined) {
    return <RuleLine rule={declaration.rule} />;
  }
  if (declaration.any_of !== undefined) {
    return (
      <div>
        <p className="key-detail__rules-note">Valid if it matches any one of:</p>
        <ul className="key-detail__rules">
          {declaration.any_of.map((rule, index) => (
            <li key={index}>
              <RuleLine rule={rule} />
            </li>
          ))}
        </ul>
      </div>
    );
  }
  return <p className="key-detail__rules-note">No value rules declared.</p>;
}

/** RuleLine names a rule's type and every constraint set on it, so a reader
 *  sees the whole shape without opening the raw declaration. */
function RuleLine({ rule }: { rule: NonNullable<MatrixKey['declaration']['rule']> }) {
  const parts: string[] = [];
  if (rule.min_length !== undefined) parts.push(`min length ${String(rule.min_length)}`);
  if (rule.max_length !== undefined) parts.push(`max length ${String(rule.max_length)}`);
  if (rule.pattern !== undefined) parts.push(`pattern ${rule.pattern}`);
  // Both explicit states are shown: `allow_empty: false` is a real constraint,
  // and dropping it renders identically to a rule that never declared it.
  if (rule.allow_empty !== undefined) {
    parts.push(rule.allow_empty ? 'empty allowed' : 'empty not allowed');
  }
  if (rule.min !== undefined) parts.push(`min ${String(rule.min)}`);
  if (rule.max !== undefined) parts.push(`max ${String(rule.max)}`);
  if (rule.members !== undefined) parts.push(`one of ${rule.members.join(', ')}`);
  if (rule.schemes !== undefined) parts.push(`schemes ${rule.schemes.join(', ')}`);
  return (
    <>
      <p className="key-detail__rule">
        <span className="mono">{rule.type}</span>
        {parts.length === 0 ? '' : ` · ${parts.join(' · ')}`}
      </p>
      {rule.json_schema === undefined ? null : (
        // The full schema, not the word "schema": two keys with different JSON
        // schemas must not render identically. React escapes the text; the key
        // record carries no value, so this is declaration shape, never a secret.
        <pre className="key-detail__json-schema mono">{rule.json_schema}</pre>
      )}
    </>
  );
}

/**
 * The metadata editor — the shared editor foundation's one live write. It edits
 * only the organisational and documentation fields (folder, description,
 * deprecation); classification changes run their own reclassification ceremony,
 * and rules/presence/rename/delete are later tickets that extend this surface.
 */
function MetadataEditor({
  refData,
  keyId,
  record,
}: {
  refData: MatrixRef;
  keyId: string;
  record: MatrixKey;
}) {
  const update = useUpdateKeyMetadata(refData, keyId);
  const [folderPath, setFolderPath] = useState(record.folder_path);
  const [description, setDescription] = useState(record.description);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  // Reload the fields when the underlying record changes (a concurrent edit
  // invalidated the query) so the form never silently overwrites fresh data
  // with the values it opened with.
  useEffect(() => {
    setFolderPath(record.folder_path);
    setDescription(record.description);
  }, [record.folder_path, record.description]);

  // Send only the fields that actually changed. updateKeyMetadata is a partial
  // update, and writing an untouched field back would clobber a value another
  // editor changed between this form's load and its submit.
  const folderChanged = folderPath !== record.folder_path;
  const descriptionChanged = description !== record.description;
  const dirty = folderChanged || descriptionChanged;

  const submit = () => {
    if (!dirty) return;
    setRefusal(null);
    setDone(false);
    update.mutate(
      {
        ...(folderChanged ? { folderPath } : {}),
        ...(descriptionChanged ? { description } : {}),
      },
      {
        onSuccess: () => setDone(true),
        onError: (error) => setRefusal(keyMetadataRefusalText(error)),
      },
    );
  };

  return (
    <form
      className="key-detail__editor"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <h3>Edit declaration</h3>
      {/* ui-spec § Declaration authoring: free-text declaration fields are
          exported to Git and are to be treated as public. */}
      <p className="key-detail__public-note">
        Descriptions and other free-text fields are exported to Git and treated as public — never
        paste secret values here.
      </p>

      <label className="field">
        <span>Folder</span>
        <input
          className="mono"
          value={folderPath}
          disabled={update.isPending}
          onChange={(event) => setFolderPath(event.target.value)}
        />
      </label>

      <label className="field">
        <span>Description</span>
        <textarea
          value={description}
          disabled={update.isPending}
          onChange={(event) => setDescription(event.target.value)}
        />
      </label>

      {refusal === null ? null : <Alert>{refusal}</Alert>}
      {done ? <Done>Saved.</Done> : null}

      <button type="submit" className="btn btn--primary" disabled={update.isPending || !dirty}>
        {update.isPending ? 'Saving…' : 'Save declaration'}
      </button>
    </form>
  );
}
