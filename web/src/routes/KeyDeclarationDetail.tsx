import type { KeyRule } from '@hikyo/client';
import { useEffect, useId, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react';
import { generatePath, Link, useNavigate } from 'react-router';

import {
  catalogueRefusalText,
  presenceImpact,
  presenceImpactIsEmpty,
  useKeyGroups,
  useSetKeyGroup,
  useUpdateKeyDeclaration,
  type KeyDeclaration,
  type KeyPresenceRules,
  type PresenceMode,
} from '../api/catalogue.ts';
import { GIT_DEFINITIONS_NOTICE, useDefinitionsSettings } from '../api/definitions.ts';
import { historyHref } from '../api/history.ts';
import {
  KEY_GONE_REFUSAL,
  keyLifecycleRefusalText,
  keyMetadataRefusalText,
  useDeleteKey,
  useKey,
  useReclassifyKey,
  useRenameKey,
  useUpdateKeyMetadata,
  type KeyImpact,
  type MatrixKey,
  type MatrixRef,
} from '../api/matrix.ts';
import { ApiError, type RefusalFinding } from '../api/client.ts';
import { useWorkspaceContext, withRemote } from '../api/transport.tsx';
import type { EnvironmentList } from '../api/values.ts';
import { surfaceById } from '../app/navigation.ts';
import { ScanBlockDialog } from './ScanBlockDialog.tsx';
import { Alert, Done, TypedNameConfirm } from './Sections.tsx';
import { useModalDialog } from './useModalDialog.ts';

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
  impact,
  impactReady,
  openerRef,
}: {
  refData: MatrixRef;
  keyId: string;
  environments: readonly Environment[];
  /**
   * This key's cross-environment occupancy, assembled by the matrix from the
   * cells it already holds (#494). It is value-free by construction — only the
   * ids of the environments a lifecycle action would disturb — and drives the
   * delete/reclassify impact previews. `impactReady` is false while the matrix
   * rows are still loading, and the destructive actions stay disabled until it
   * is true so a preview never understates what an action affects (fail-closed).
   */
  impact: KeyImpact;
  impactReady: boolean;
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

  // Editing is available ONLY once the source is confirmed `db` by a read that
  // is CURRENT. This fails closed on both the initial unresolved/errored state
  // (no data) AND a failed refetch that left stale data behind — react-query
  // keeps a prior success's data through a refetch error, so `isError` alone
  // stays false there; `isRefetchError` is what catches it. A stale `db` value
  // must not keep a live Save on a project whose source may since have become
  // Git-managed (where declarations are read-only). The Git read-only notice is
  // always safe to show, so it need not be as strict.
  const source = definitions.data?.definitions_source;
  const sourceUntrusted = definitions.isError || definitions.isRefetchError;
  const editable = definitions.isSuccess && !sourceUntrusted && source === 'db';
  const gitManaged = source === 'git';

  return (
    <aside
      className="key-detail"
      aria-label="Key declaration"
      onKeyDown={(event) => {
        // Escape closes the panel back to the matrix — but NOT while a modal
        // dialog owns the top layer. The Surface-2 scanning block (#183) is a
        // native <dialog>; its Escape is the dialog's own dismiss and must not
        // also collapse the surface behind it. The keydown still bubbles here,
        // so guard on an open dialog rather than let both fire.
        if (event.key === 'Escape' && document.querySelector('dialog[open]') === null) {
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
          impact={impact}
          impactReady={impactReady}
          editable={editable}
          gitManaged={gitManaged}
          sourceResolved={definitions.isSuccess}
          sourceFailed={sourceUntrusted}
          gitProvenance={definitions.data?.last_apply}
          historyPath={historyPath}
          matrixPath={matrixPath}
        />
      )}
    </aside>
  );
}

/**
 * A load failure that stays recoverable: a deleted or renamed-away key (404) is
 * a stale link, not a dead end, so it names the state and offers the way back;
 * a refusal (403) quotes the server; anything else is reported with its status.
 * The 404 uses the ONE canonical missing-key sentence every key 404 shares — a
 * surface that worded it differently would be a distinguishable existence signal.
 */
function KeyLoadError({ error, matrixPath }: { error: Error; matrixPath: string }) {
  const message =
    error instanceof ApiError && error.status === 404
      ? KEY_GONE_REFUSAL
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
  impact,
  impactReady,
  editable,
  gitManaged,
  sourceResolved,
  sourceFailed,
  gitProvenance,
  historyPath,
  matrixPath,
}: {
  refData: MatrixRef;
  keyId: string;
  record: MatrixKey;
  environments: readonly Environment[];
  impact: KeyImpact;
  impactReady: boolean;
  editable: boolean;
  gitManaged: boolean;
  sourceResolved: boolean;
  sourceFailed: boolean;
  gitProvenance:
    | { commit?: string; ref?: string; actor?: string; applied_by: string }
    | undefined;
  historyPath: string;
  matrixPath: string;
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
        <>
          <MetadataEditor refData={refData} keyId={keyId} record={record} />
          <DeclarationEditor
            refData={refData}
            keyId={keyId}
            record={record}
            environments={environments}
          />
          <GroupEditor refData={refData} keyId={keyId} record={record} />
          <RenameKey refData={refData} keyId={keyId} record={record} />
          <ReclassifyKey
            refData={refData}
            keyId={keyId}
            record={record}
            impact={impact}
            impactReady={impactReady}
            environmentName={environmentName}
          />
          <DeleteKey
            refData={refData}
            keyId={keyId}
            record={record}
            impact={impact}
            impactReady={impactReady}
            environmentName={environmentName}
            matrixPath={matrixPath}
          />
        </>
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
  // The Surface-2 scanning block (#183): a refused write carries redacted
  // findings, never the flagged material. `findings` drives the dialog; the
  // value the operator typed is never passed there.
  const [scanBlock, setScanBlock] = useState<{
    readonly findings: readonly RefusalFinding[];
    readonly onOverride: ((tokens: readonly string[]) => Promise<void>) | null;
  } | null>(null);

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
    // Snapshot the changed fields ONCE: a Surface-2 override must resubmit the
    // exact same content, because each acknowledgement token is bound to it.
    // Changing a field between the block and the override invalidates the token,
    // which the server then rejects by name.
    const changed = {
      ...(folderChanged ? { folderPath } : {}),
      ...(descriptionChanged ? { description } : {}),
    };
    // The mutation is callback-shaped; wrap one attempt as a promise so the
    // block dialog's override can await the resubmit and surface the server's
    // named refusal on rejection.
    const attempt = (acknowledgements: readonly string[]): Promise<void> =>
      new Promise((resolve, reject) => {
        update.mutate(
          { ...changed, ...(acknowledgements.length === 0 ? {} : { acknowledgements }) },
          { onSuccess: () => resolve(), onError: (error) => reject(error) },
        );
      });
    void attempt([])
      .then(() => setDone(true))
      .catch((error: unknown) => {
        // A scanner block carries findings; route them to the block dialog,
        // which shows ONLY the redacted rule id + locator and offers an audited
        // override. Any other refusal stays inline in its own words. A 404 is
        // canonicalized to the uniform missing-key sentence BEFORE findings are
        // considered — a 404 that carried findings must never render details, or
        // it becomes the existence oracle the reveal gate closes.
        if (error instanceof ApiError && error.status !== 404 && error.findings.length > 0) {
          const findings = error.findings;
          setScanBlock({
            findings,
            onOverride: findings.every((finding) => finding.acknowledgement !== undefined)
              ? (tokens) =>
                  attempt(tokens).then(() => {
                    setDone(true);
                    setScanBlock(null);
                  })
              : null,
          });
          return;
        }
        setRefusal(keyMetadataRefusalText(error instanceof Error ? error : new Error('save failed')));
      });
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

      {scanBlock === null ? null : (
        <ScanBlockDialog
          title="Declaration blocked by secret scanning"
          intro={`This field is exported to Git and treated as public. Saving ${record.name}’s declaration was refused because it looks like it carries secret material.`}
          findings={scanBlock.findings}
          onOverride={scanBlock.onOverride}
          onClose={() => setScanBlock(null)}
        />
      )}
    </form>
  );
}

type ScanBlockState = {
  readonly findings: readonly RefusalFinding[];
  readonly onOverride: ((tokens: readonly string[]) => Promise<void>) | null;
};

/** scanBlockFrom turns a scanner-refused write into the block dialog's state:
 *  the redacted findings, and an override only when every finding carries a
 *  token. `resubmit` re-runs the SAME content with those tokens (each token is
 *  bound to the content that produced it). Returns null for any non-scanner
 *  refusal, which the caller shows inline instead. */
function scanBlockFrom(
  error: unknown,
  resubmit: (tokens: readonly string[]) => Promise<void>,
): ScanBlockState | null {
  // A 404 is canonicalized to the uniform missing-key refusal by the caller and
  // must NEVER open a findings dialog, even if one somehow rode a 404 — that
  // would leak existence through the reveal mask. Only a genuine scan refusal
  // (which carries findings on a non-404) becomes a block.
  if (!(error instanceof ApiError) || error.status === 404 || error.findings.length === 0) {
    return null;
  }
  const findings = error.findings;
  return {
    findings,
    onOverride: findings.every((finding) => finding.acknowledgement !== undefined)
      ? (tokens) => resubmit(tokens)
      : null,
  };
}

/**
 * RenameKey changes the key's name. Identity is the immutable id, so nothing
 * that references the key breaks — but the delivered payload's key set changes,
 * an advertised schema change. The new name is exported to Git and treated as
 * public, so the same Surface-2 scanning block the metadata editor carries
 * attaches here: a refused rename renders the redacted finding and offers the
 * audited override. A collision or any other refusal stays inline in the
 * server's own caller-safe words.
 */
function RenameKey({
  refData,
  keyId,
  record,
}: {
  refData: MatrixRef;
  keyId: string;
  record: MatrixKey;
}) {
  const rename = useRenameKey(refData, keyId);
  const [name, setName] = useState(record.name);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [scanBlock, setScanBlock] = useState<ScanBlockState | null>(null);

  // Reload the field when the record's name changes underneath (a concurrent
  // rename, or this rename's own success invalidating the query) so the form
  // never re-submits a name that is already applied.
  useEffect(() => {
    setName(record.name);
  }, [record.name]);

  const dirty = name !== record.name && name !== '';

  const submit = () => {
    if (!dirty) return;
    setRefusal(null);
    setDone(false);
    const attempt = (acknowledgements: readonly string[]): Promise<void> =>
      new Promise((resolve, reject) => {
        rename.mutate(
          { name, ...(acknowledgements.length === 0 ? {} : { acknowledgements }) },
          { onSuccess: () => resolve(), onError: (error) => reject(error) },
        );
      });
    void attempt([])
      .then(() => setDone(true))
      .catch((error: unknown) => {
        const block = scanBlockFrom(error, (tokens) =>
          attempt(tokens).then(() => {
            setDone(true);
            setScanBlock(null);
          }),
        );
        if (block !== null) {
          setScanBlock(block);
          return;
        }
        setRefusal(
          keyLifecycleRefusalText(error instanceof Error ? error : new Error('rename failed'), 'rename'),
        );
      });
  };

  return (
    <form
      className="key-detail__editor"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <h3>Rename key</h3>
      <p className="key-detail__public-note">
        The name is part of the delivered payload and is exported to Git — treat it as public. The
        key’s identity does not change, so nothing that references it breaks.
      </p>
      <label className="field">
        <span>Name</span>
        <input
          className="mono"
          value={name}
          disabled={rename.isPending}
          onChange={(event) => setName(event.target.value)}
        />
      </label>

      {refusal === null ? null : <Alert>{refusal}</Alert>}
      {done ? <Done>Renamed.</Done> : null}

      <button type="submit" className="btn btn--primary" disabled={rename.isPending || !dirty}>
        {rename.isPending ? 'Renaming…' : 'Rename key'}
      </button>

      {scanBlock === null ? null : (
        <ScanBlockDialog
          title="Rename blocked by secret scanning"
          intro={`The name is exported to Git and treated as public. Renaming ${record.name} was refused because the new name looks like it carries secret material.`}
          findings={scanBlock.findings}
          onOverride={scanBlock.onOverride}
          onClose={() => setScanBlock(null)}
        />
      )}
    </form>
  );
}

/**
 * ReclassifyKey runs the reclassification ceremony in the one direction that is
 * available — a `secret` key can only become `config`, a `config` key only
 * `secret` — and renders that direction's distinct consequences before
 * committing.
 *
 * Tightening (`config` → `secret`) re-secures every occurrence and drops the
 * key's config-scanning dismissals. Declassifying (`secret` → `config`) is a
 * disclosure: the value becomes readable under ordinary config read, so the
 * server requires a recent second factor. The UI cannot pre-check that — the
 * server is the source of truth — so it states the requirement, attempts the
 * ceremony, and on refusal surfaces the reauth need (403) or the uniform
 * missing-key sentence (404, which also masks a missing reveal grant). On a
 * successful declassification the response's Surface-1 warnings for the
 * re-materialised occurrences are rendered redacted (rule id + locator).
 */
function ReclassifyKey({
  refData,
  keyId,
  record,
  impact,
  impactReady,
  environmentName,
}: {
  refData: MatrixRef;
  keyId: string;
  record: MatrixKey;
  impact: KeyImpact;
  impactReady: boolean;
  environmentName: (id: string) => string;
}) {
  const reclassify = useReclassifyKey(refData);
  const target: 'secret' | 'config' = record.classification === 'secret' ? 'config' : 'secret';
  const declassify = target === 'config';
  const [confirming, setConfirming] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);
  // The success text is derived from the DIRECTION THAT RAN, captured here, not
  // from the live `declassify`: the mutation invalidates the key, the record
  // refetches with the new classification, and `declassify` flips — which would
  // otherwise flip the persistent "Reclassified as …" message to the wrong verb.
  const [doneClassification, setDoneClassification] = useState<'secret' | 'config' | null>(null);
  const [warnings, setWarnings] = useState<readonly RefusalFinding[]>([]);

  const run = () => {
    // Fail closed: never submit while the impact preview is not trustworthy, even
    // if it went unready after the dialog opened (a concurrent invalidation).
    if (!impactReady) return;
    setRefusal(null);
    setDoneClassification(null);
    setWarnings([]);
    reclassify.mutate(
      { key: keyId, classification: target },
      {
        onSuccess: (key) => {
          setConfirming(false);
          setDoneClassification(target);
          // Only a declassification carries Surface-1 warnings, for the
          // occurrences re-materialised as config; a tightening carries none.
          setWarnings(declassify ? (key.findings ?? []) : []);
        },
        onError: (error: unknown) => {
          setConfirming(false);
          setRefusal(
            keyLifecycleRefusalText(
              error instanceof Error ? error : new Error('reclassify failed'),
              declassify ? 'declassify' : 'reclassify',
            ),
          );
        },
      },
    );
  };

  return (
    <section className="key-detail__section" aria-labelledby="key-detail-reclassify">
      <h3 id="key-detail-reclassify">Reclassify</h3>
      <p>
        This key is classified{' '}
        <strong>{record.classification === 'secret' ? '🔒 secret' : 'config'}</strong>.
      </p>

      {refusal === null ? null : <Alert>{refusal}</Alert>}
      {doneClassification === null ? null : (
        <Done>
          {doneClassification === 'config' ? 'Reclassified as config.' : 'Reclassified as secret.'}
        </Done>
      )}

      {warnings.length === 0 ? null : (
        <div className="key-detail__section" aria-label="Declassification scanning warnings">
          <p className="key-detail__public-note">
            Now readable as config, these occurrences look like they carry secret material. Review
            them — nothing is blocked.
          </p>
          <ul className="key-detail__findings">
            {warnings.map((finding, index) => (
              <li key={`${finding.rule_id}:${finding.locator}:${String(index)}`}>
                <span className="mono">{finding.rule_id}</span> at{' '}
                <span className="mono">{finding.locator}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      <button
        type="button"
        className="btn"
        // Fail closed in BOTH directions: tightening drops the key's config
        // dismissals, so its impact preview matters as much as a declassification's.
        disabled={reclassify.isPending || !impactReady}
        onClick={() => {
          setDoneClassification(null);
          setConfirming(true);
        }}
      >
        {declassify ? 'Reclassify as config…' : 'Reclassify as secret…'}
      </button>

      {confirming ? (
        <ConfirmDialog
          title={declassify ? 'Reclassify this secret as config?' : 'Reclassify this key as secret?'}
          confirmLabel={declassify ? 'Reclassify as config' : 'Reclassify as secret'}
          busy={reclassify.isPending}
          // If the impact preview goes unready while the dialog is open, keep the
          // confirm disabled — the preview shows "Checking…" and must not be
          // actioned against a stale blast radius.
          confirmDisabled={!impactReady}
          danger={declassify}
          onConfirm={run}
          onClose={() => setConfirming(false)}
        >
          {declassify ? (
            <>
              <p>
                The value becomes readable under ordinary config read in every environment that
                holds it. This is a disclosure and cannot be undone by re-securing the key later —
                anything already served as config has been served.
              </p>
              <p>
                It requires a recent second-factor sign-in; if you have not reauthenticated lately
                you will be asked to before it can proceed.
              </p>
            </>
          ) : (
            <p>
              Every occurrence is re-secured and handled as a secret, and the key’s existing
              config-scanning dismissals are dropped — a value that looks secret will warn again.
            </p>
          )}
          <ImpactPreview
            impact={impact}
            impactReady={impactReady}
            environmentName={environmentName}
          />
        </ConfirmDialog>
      ) : null}
    </section>
  );
}

/**
 * DeleteKey removes the declaration, its explicit presence rows and its group
 * membership. It previews exactly which environments the deletion disturbs
 * (a delivered value, or an unpublished draft) and gates the act behind typing
 * the key's name — the same danger-zone confirm the project and org deletions
 * use — WITHOUT ever revealing a value. On success it returns to the matrix, so
 * no route is left pointing at the deleted key.
 */
function DeleteKey({
  refData,
  keyId,
  record,
  impact,
  impactReady,
  environmentName,
  matrixPath,
}: {
  refData: MatrixRef;
  keyId: string;
  record: MatrixKey;
  impact: KeyImpact;
  impactReady: boolean;
  environmentName: (id: string) => string;
  matrixPath: string;
}) {
  const navigate = useNavigate();
  const remove = useDeleteKey(refData, keyId);
  const [refusal, setRefusal] = useState<string | null>(null);

  const confirmDelete = () => {
    setRefusal(null);
    remove.mutate(undefined, {
      // Navigate FIRST so the useKey observer unmounts before anything could
      // re-fetch the now-deleted key: the AC's "no stale key route" is exactly
      // this ordering. The hook's own onSuccess refreshes the matrix lists.
      onSuccess: () => {
        void navigate(matrixPath);
      },
      onError: (error: unknown) => {
        setRefusal(
          keyLifecycleRefusalText(error instanceof Error ? error : new Error('delete failed'), 'delete'),
        );
      },
    });
  };

  return (
    <section className="key-detail__section" aria-labelledby="key-detail-delete">
      <h3 id="key-detail-delete">Delete key</h3>
      <p>
        Removes the declaration, its presence rules and its group membership across the whole
        project. No value is shown, and this cannot be undone.
      </p>
      <ImpactPreview impact={impact} impactReady={impactReady} environmentName={environmentName} />

      {refusal === null ? null : <Alert>{refusal}</Alert>}

      <TypedNameConfirm
        label="Confirm the key name to delete it"
        // Fail closed: no typed target until the impact is loaded, so a delete
        // cannot be armed while the preview still understates what it affects.
        expect={impactReady ? record.name : null}
        action="Delete key"
        hint={
          <>
            Type <span className="mono">{record.name}</span> to enable deletion.
          </>
        }
        busy={remove.isPending}
        onConfirm={confirmDelete}
      />
    </section>
  );
}

/** ImpactPreview lists the environments a lifecycle action disturbs — a
 *  delivered value or an unpublished draft — by name, never by value. Until the
 *  matrix rows load it says so, and the caller keeps the action disabled. */
function ImpactPreview({
  impact,
  impactReady,
  environmentName,
}: {
  impact: KeyImpact;
  impactReady: boolean;
  environmentName: (id: string) => string;
}) {
  if (!impactReady) {
    return (
      <p className="key-detail__state" role="status">
        Checking which environments this affects…
      </p>
    );
  }
  const set = impact.setEnvironmentIds;
  const pending = impact.pendingEnvironmentIds;
  return (
    <ul className="key-detail__impact">
      <li>
        {set.length === 0
          ? 'No environment currently delivers a value for this key.'
          : `Delivers a value in ${String(set.length)} ${set.length === 1 ? 'environment' : 'environments'}: ${set.map(environmentName).join(', ')}.`}
      </li>
      {pending.length === 0 ? null : (
        <li>
          {`Unpublished drafts touch ${String(pending.length)} ${pending.length === 1 ? 'environment' : 'environments'}: ${pending.map(environmentName).join(', ')}.`}
        </li>
      )}
    </ul>
  );
}

/**
 * ConfirmDialog is the native-`<dialog>` confirm the reclassification ceremony
 * opens. Native so the platform gives the focus trap, the inert backdrop and
 * Escape — and so the aside's Escape guard (which only collapses the panel when
 * no dialog owns the top layer) generalises to it without a per-dialog change.
 */
function ConfirmDialog({
  title,
  confirmLabel,
  busy,
  confirmDisabled = false,
  danger,
  onConfirm,
  onClose,
  children,
}: {
  title: string;
  confirmLabel: string;
  busy: boolean;
  confirmDisabled?: boolean;
  danger: boolean;
  onConfirm: () => void;
  onClose: () => void;
  children: ReactNode;
}) {
  const dialog = useModalDialog();
  const titleId = useId();
  return (
    <dialog className="matrix-editor" ref={dialog} aria-labelledby={titleId} onClose={onClose}>
      <div className="matrix-editor__head">
        <div>
          <h2 id={titleId}>{title}</h2>
        </div>
        <button
          type="button"
          className="btn matrix-editor__close"
          aria-label="Close"
          onClick={onClose}
        >
          ✕
        </button>
      </div>
      {children}
      <div className="matrix-editor__actions">
        <button
          type="button"
          className={danger ? 'btn btn--danger' : 'btn btn--primary'}
          disabled={busy || confirmDisabled}
          onClick={onConfirm}
        >
          {busy ? 'Working…' : confirmLabel}
        </button>
        <button type="button" className="btn" disabled={busy} onClick={onClose}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}

// ---------------------------------------------------------------------------
// #493 editors — value rules, presence, and group membership. They extend the
// #491/#494 foundation, reusing its ScanBlockDialog and scanBlockFrom helper.
// ---------------------------------------------------------------------------

const RULE_TYPES = ['string', 'integer', 'boolean', 'enum', 'url', 'json'] as const;
type RuleType = (typeof RULE_TYPES)[number];
type ReadRule = NonNullable<MatrixKey['declaration']['rule']>;

/**
 * A JSON signature that tolerates bigint. The read model infers int64 bounds
 * (min/max) as bigint, and a plain JSON.stringify throws on those — so a bound
 * integer declaration would crash the panel. A tag keeps a bigint distinct from
 * the same-digit number so the signature never conflates them.
 */
function stableSignature(value: unknown): string {
  return JSON.stringify(value, (_key, entry) =>
    typeof entry === 'bigint' ? `${entry.toString()}n` : entry,
  );
}

/** Raised when an int64 bound cannot cross into a JS number without loss. */
class UnsafeBoundError extends Error {}

/**
 * boundToNumber crosses the read model's bigint bound into the write model's
 * number EXACTLY or not at all — an int64 outside the safe-integer range would
 * round silently, so it fails loudly rather than writing a corrupted rule.
 */
function boundToNumber(bound: bigint): number {
  const asNumber = Number(bound);
  if (!Number.isSafeInteger(asNumber) || BigInt(asNumber) !== bound) {
    throw new UnsafeBoundError(
      "This key's integer bounds are outside the range the browser can edit safely. Edit its rules with `hikyo definitions`.",
    );
  }
  return asNumber;
}

function ruleToWrite(rule: ReadRule): KeyRule {
  return {
    type: rule.type,
    ...(rule.min_length === undefined ? {} : { min_length: rule.min_length }),
    ...(rule.max_length === undefined ? {} : { max_length: rule.max_length }),
    ...(rule.pattern === undefined ? {} : { pattern: rule.pattern }),
    ...(rule.allow_empty === undefined ? {} : { allow_empty: rule.allow_empty }),
    ...(rule.min === undefined ? {} : { min: boundToNumber(rule.min) }),
    ...(rule.max === undefined ? {} : { max: boundToNumber(rule.max) }),
    ...(rule.members === undefined ? {} : { members: [...rule.members] }),
    ...(rule.schemes === undefined ? {} : { schemes: [...rule.schemes] }),
    ...(rule.json_schema === undefined ? {} : { json_schema: rule.json_schema }),
  };
}

/** The existing declaration as a writable value — used when only presence changes. */
function declarationToWrite(declaration: MatrixKey['declaration']): KeyDeclaration {
  if (declaration.rule !== undefined) {
    return { rule: ruleToWrite(declaration.rule) };
  }
  if (declaration.any_of !== undefined) {
    return { any_of: declaration.any_of.map(ruleToWrite) };
  }
  return {};
}

type RuleDraft = {
  readonly type: RuleType;
  readonly minLength: string;
  readonly maxLength: string;
  readonly pattern: string;
  readonly allowEmpty: boolean;
  readonly min: string;
  readonly max: string;
  readonly members: string;
  readonly schemes: string;
  readonly jsonSchema: string;
};

function ruleDraftFrom(rule: ReadRule | { type: RuleType }): RuleDraft {
  const full = rule as Partial<ReadRule> & { type: RuleType };
  return {
    type: full.type,
    minLength: full.min_length === undefined ? '' : String(full.min_length),
    maxLength: full.max_length === undefined ? '' : String(full.max_length),
    pattern: full.pattern ?? '',
    allowEmpty: full.allow_empty ?? false,
    min: full.min === undefined ? '' : String(full.min),
    max: full.max === undefined ? '' : String(full.max),
    members: (full.members ?? []).join('\n'),
    schemes: (full.schemes ?? []).join(', '),
    jsonSchema: full.json_schema ?? '',
  };
}

/** Parse a bounded integer field, or report why it cannot. */
function parseBound(label: string, raw: string): { value?: number; error?: string } {
  const trimmed = raw.trim();
  if (trimmed === '') return {};
  if (!/^-?\d+$/.test(trimmed)) return { error: `${label} must be a whole number.` };
  const value = Number(trimmed);
  if (!Number.isSafeInteger(value)) {
    return { error: `${label} is too large — keep it within ±9,007,199,254,740,991.` };
  }
  return { value };
}

/** Build a writable rule from the draft, or the first reason it is not valid yet. */
function buildRule(draft: RuleDraft): { rule?: KeyRule; error?: string } {
  const rule: KeyRule = { type: draft.type };
  switch (draft.type) {
    case 'string': {
      const min = parseBound('Minimum length', draft.minLength);
      if (min.error !== undefined) return { error: min.error };
      const max = parseBound('Maximum length', draft.maxLength);
      if (max.error !== undefined) return { error: max.error };
      if (min.value !== undefined) rule.min_length = min.value;
      if (max.value !== undefined) rule.max_length = max.value;
      if (draft.pattern.trim() !== '') rule.pattern = draft.pattern;
      if (draft.allowEmpty) rule.allow_empty = true;
      break;
    }
    case 'integer': {
      const min = parseBound('Minimum', draft.min);
      if (min.error !== undefined) return { error: min.error };
      const max = parseBound('Maximum', draft.max);
      if (max.error !== undefined) return { error: max.error };
      if (min.value !== undefined) rule.min = min.value;
      if (max.value !== undefined) rule.max = max.value;
      break;
    }
    case 'enum': {
      const members = draft.members
        .split('\n')
        .map((member) => member.trim())
        .filter((member) => member !== '');
      if (members.length === 0) return { error: 'An enum needs at least one member.' };
      rule.members = members;
      break;
    }
    case 'url': {
      const schemes = draft.schemes
        .split(',')
        .map((scheme) => scheme.trim())
        .filter((scheme) => scheme !== '');
      if (schemes.length > 0) rule.schemes = schemes;
      break;
    }
    case 'json': {
      if (draft.jsonSchema.trim() !== '') rule.json_schema = draft.jsonSchema;
      break;
    }
    case 'boolean':
      break;
  }
  return { rule };
}

type PresenceDraft = {
  readonly requiredMode: PresenceMode;
  readonly requiredIds: ReadonlySet<string>;
  readonly forbiddenMode: PresenceMode;
  readonly forbiddenIds: ReadonlySet<string>;
};

function presenceDraftFrom(presence: MatrixKey['presence']): PresenceDraft {
  return {
    requiredMode: presence.required_in.mode,
    requiredIds: new Set(presence.required_in.environment_ids ?? []),
    forbiddenMode: presence.forbidden_in.mode,
    forbiddenIds: new Set(presence.forbidden_in.environment_ids ?? []),
  };
}

function presenceRuleValue(mode: PresenceMode, ids: ReadonlySet<string>) {
  return mode === 'explicit' ? { mode, environment_ids: [...ids] } : { mode };
}

function buildPresence(draft: PresenceDraft): KeyPresenceRules {
  return {
    required_in: presenceRuleValue(draft.requiredMode, draft.requiredIds),
    forbidden_in: presenceRuleValue(draft.forbiddenMode, draft.forbiddenIds),
  };
}

function toggleId(ids: ReadonlySet<string>, id: string): ReadonlySet<string> {
  const next = new Set(ids);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
}

/**
 * Toggle is a pressed-state button, not a checkbox: a native checkbox cannot
 * meet the 44px coarse-pointer touch floor without distortion, and this panel
 * is asserted at a phone viewport. The on-state carries a ✓ so it never depends
 * on colour alone (DESIGN.md), and it reuses `.settings-tag`, which the touch
 * and focus gates already cover.
 */
function Toggle({
  label,
  on,
  disabled,
  onChange,
}: {
  label: string;
  on: boolean;
  disabled: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <button
      type="button"
      className={`settings-tag${on ? ' settings-tag--on' : ''}`}
      aria-pressed={on}
      disabled={disabled}
      onClick={() => onChange(!on)}
    >
      {on ? '✓ ' : ''}
      {label}
    </button>
  );
}

/**
 * DeclarationEditor edits the value rules and presence — one endpoint, one save
 * (#493). `any_of` alternatives are shown read-only with a pointer to
 * `hikyo definitions`; a single rule is fully editable. The before/after impact
 * names the environments a presence change adds or drops, and the server's
 * atomic refusal (surfaced verbatim) catches an invalid draft before commit.
 */
function DeclarationEditor({
  refData,
  keyId,
  record,
  environments,
}: {
  refData: MatrixRef;
  keyId: string;
  record: MatrixKey;
  environments: readonly Environment[];
}) {
  const update = useUpdateKeyDeclaration(refData, keyId);
  const isAnyOf = record.declaration.any_of !== undefined;
  const singleRule = record.declaration.rule;

  const [ruleDraft, setRuleDraft] = useState<RuleDraft>(() =>
    singleRule === undefined ? ruleDraftFrom({ type: 'string' }) : ruleDraftFrom(singleRule),
  );
  const [presence, setPresence] = useState<PresenceDraft>(() => presenceDraftFrom(record.presence));
  const [invalid, setInvalid] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [scanBlock, setScanBlock] = useState<ScanBlockState | null>(null);

  // Re-seed only when the SERVER's declaration/presence actually change (a
  // concurrent edit), keyed on a bigint-safe content signature — an unrelated
  // sibling save (metadata, group, rename) refetches the record but leaves
  // these signatures untouched, so an in-progress edit here is preserved.
  const declSignature = stableSignature(record.declaration);
  const presSignature = stableSignature(record.presence);
  useEffect(() => {
    setRuleDraft(
      record.declaration.rule === undefined
        ? ruleDraftFrom({ type: 'string' })
        : ruleDraftFrom(record.declaration.rule),
    );
    setPresence(presenceDraftFrom(record.presence));
    setInvalid(null);
    // `done` is deliberately NOT reset here: the successful-save refetch changes
    // these signatures, and clearing it would wipe the "Saved." it just set.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- content signatures stand in for the objects
  }, [declSignature, presSignature]);

  const envIds = useMemo(() => environments.map((environment) => environment.id), [environments]);
  const proposedPresence = buildPresence(presence);
  const impact = presenceImpact(record.presence, proposedPresence, envIds);
  const envName = (id: string): string =>
    environments.find((environment) => environment.id === id)?.name ?? id;

  const submit = (): void => {
    setInvalid(null);
    setRefusal(null);
    setDone(false);
    let declaration: KeyDeclaration;
    try {
      if (isAnyOf) {
        declaration = declarationToWrite(record.declaration);
      } else {
        const result = buildRule(ruleDraft);
        if (result.error !== undefined || result.rule === undefined) {
          setInvalid(result.error ?? 'The rule is incomplete.');
          return;
        }
        declaration = { rule: result.rule };
      }
    } catch (error) {
      setInvalid(error instanceof Error ? error.message : 'This declaration cannot be edited here.');
      return;
    }
    const attempt = (acknowledgements: readonly string[]): Promise<void> =>
      new Promise((resolve, reject) => {
        update.mutate(
          {
            declaration,
            presence: proposedPresence,
            ...(acknowledgements.length === 0 ? {} : { acknowledgements }),
          },
          { onSuccess: () => resolve(), onError: (error) => reject(error) },
        );
      });
    void attempt([])
      .then(() => setDone(true))
      .catch((error: unknown) => {
        const block = scanBlockFrom(error, (tokens) =>
          attempt(tokens).then(() => {
            setDone(true);
            setScanBlock(null);
          }),
        );
        if (block !== null) {
          setScanBlock(block);
          return;
        }
        setRefusal(catalogueRefusalText(error, 'update the rules'));
      });
  };

  // Any draft change clears "Saved." so it never sits over an unsaved edit; the
  // successful-save refetch reseeds without clearing it (the effect leaves it).
  const editRule = (next: RuleDraft): void => {
    setRuleDraft(next);
    setDone(false);
  };
  const editPresence = (updater: (current: PresenceDraft) => PresenceDraft): void => {
    setPresence(updater);
    setDone(false);
  };

  return (
    <form
      className="key-detail__editor"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <h3>Edit value rules &amp; presence</h3>

      {isAnyOf ? (
        <p className="key-detail__rules-note">
          This key declares alternatives (<span className="mono">any_of</span>). Edit the
          alternatives with <span className="mono">hikyo definitions</span>; presence stays editable
          here.
        </p>
      ) : (
        <RuleFields draft={ruleDraft} disabled={update.isPending} onChange={editRule} />
      )}

      <fieldset className="key-detail__presence-editor" disabled={update.isPending}>
        <legend>Presence</legend>
        <PresenceControl
          label="Required in"
          hint="Where a value must resolve to set. `all` covers environments created later."
          mode={presence.requiredMode}
          ids={presence.requiredIds}
          environments={environments}
          onMode={(mode) => editPresence((current) => ({ ...current, requiredMode: mode }))}
          onToggle={(id) =>
            editPresence((current) => ({ ...current, requiredIds: toggleId(current.requiredIds, id) }))
          }
        />
        <PresenceControl
          label="Forbidden in"
          hint="Where a value must be absent."
          mode={presence.forbiddenMode}
          ids={presence.forbiddenIds}
          environments={environments}
          onMode={(mode) => editPresence((current) => ({ ...current, forbiddenMode: mode }))}
          onToggle={(id) =>
            editPresence((current) => ({
              ...current,
              forbiddenIds: toggleId(current.forbiddenIds, id),
            }))
          }
        />
      </fieldset>

      <div className="key-detail__presence-impact" aria-live="polite">
        <p className="key-detail__presence-impact-title">Before → after</p>
        {presenceImpactIsEmpty(impact) ? (
          <p>Presence unchanged.</p>
        ) : (
          <ul className="key-detail__presence-impact-list">
            {impact.requiredAdded.length > 0 ? (
              <li>Newly required in: {impact.requiredAdded.map(envName).join(', ')}</li>
            ) : null}
            {impact.requiredRemoved.length > 0 ? (
              <li>No longer required in: {impact.requiredRemoved.map(envName).join(', ')}</li>
            ) : null}
            {impact.forbiddenAdded.length > 0 ? (
              <li>Newly forbidden in: {impact.forbiddenAdded.map(envName).join(', ')}</li>
            ) : null}
            {impact.forbiddenRemoved.length > 0 ? (
              <li>No longer forbidden in: {impact.forbiddenRemoved.map(envName).join(', ')}</li>
            ) : null}
          </ul>
        )}
        <p className="key-detail__presence-impact-note">
          The save is atomic: if a value already set in a newly-forbidden environment, or a required
          environment left unset, would become invalid, the server refuses the whole change and
          names it — nothing commits until it is valid.
        </p>
      </div>

      {invalid === null ? null : <Alert>{invalid}</Alert>}
      {refusal === null ? null : <Alert>{refusal}</Alert>}
      {done ? <Done>Saved.</Done> : null}

      <button type="submit" className="btn btn--primary" disabled={update.isPending}>
        {update.isPending ? 'Saving…' : 'Save value rules & presence'}
      </button>

      {scanBlock === null ? null : (
        <ScanBlockDialog
          title="Declaration blocked by secret scanning"
          intro={`This field is exported to Git and treated as public. Saving ${record.name}’s rules was refused because it looks like it carries secret material.`}
          findings={scanBlock.findings}
          onOverride={scanBlock.onOverride}
          onClose={() => setScanBlock(null)}
        />
      )}
    </form>
  );
}

function RuleFields({
  draft,
  disabled,
  onChange,
}: {
  draft: RuleDraft;
  disabled: boolean;
  onChange: (next: RuleDraft) => void;
}) {
  const set = <K extends keyof RuleDraft>(field: K, value: RuleDraft[K]): void =>
    onChange({ ...draft, [field]: value });
  return (
    <fieldset className="key-detail__rule-editor" disabled={disabled}>
      <legend>Value rule</legend>
      <label className="field">
        <span>Type</span>
        <select
          className="mono"
          value={draft.type}
          onChange={(event) => set('type', event.currentTarget.value as RuleType)}
        >
          {RULE_TYPES.map((type) => (
            <option key={type} value={type}>
              {type}
            </option>
          ))}
        </select>
      </label>
      {draft.type === 'string' ? (
        <>
          <label className="field">
            <span>Minimum length</span>
            <input
              inputMode="numeric"
              value={draft.minLength}
              onChange={(event) => set('minLength', event.currentTarget.value)}
            />
          </label>
          <label className="field">
            <span>Maximum length</span>
            <input
              inputMode="numeric"
              value={draft.maxLength}
              onChange={(event) => set('maxLength', event.currentTarget.value)}
            />
          </label>
          <label className="field">
            <span>Pattern (RE2, anchored)</span>
            <input
              className="mono"
              value={draft.pattern}
              onChange={(event) => set('pattern', event.currentTarget.value)}
            />
          </label>
          <Toggle
            label="Allow empty value"
            on={draft.allowEmpty}
            disabled={disabled}
            onChange={(on) => set('allowEmpty', on)}
          />
        </>
      ) : null}
      {draft.type === 'integer' ? (
        <>
          <label className="field">
            <span>Minimum</span>
            <input
              inputMode="numeric"
              value={draft.min}
              onChange={(event) => set('min', event.currentTarget.value)}
            />
          </label>
          <label className="field">
            <span>Maximum</span>
            <input
              inputMode="numeric"
              value={draft.max}
              onChange={(event) => set('max', event.currentTarget.value)}
            />
          </label>
        </>
      ) : null}
      {draft.type === 'enum' ? (
        <label className="field">
          <span>Members (one per line)</span>
          <textarea
            className="mono"
            rows={4}
            value={draft.members}
            onChange={(event) => set('members', event.currentTarget.value)}
          />
        </label>
      ) : null}
      {draft.type === 'url' ? (
        <label className="field">
          <span>Allowed schemes (comma-separated)</span>
          <input
            className="mono"
            placeholder="https, http"
            value={draft.schemes}
            onChange={(event) => set('schemes', event.currentTarget.value)}
          />
        </label>
      ) : null}
      {draft.type === 'json' ? (
        <label className="field">
          <span>JSON Schema (2020-12)</span>
          <textarea
            className="mono"
            rows={6}
            value={draft.jsonSchema}
            onChange={(event) => set('jsonSchema', event.currentTarget.value)}
          />
        </label>
      ) : null}
    </fieldset>
  );
}

function PresenceControl({
  label,
  hint,
  mode,
  ids,
  environments,
  onMode,
  onToggle,
}: {
  label: string;
  hint: string;
  mode: PresenceMode;
  ids: ReadonlySet<string>;
  environments: readonly Environment[];
  onMode: (mode: PresenceMode) => void;
  onToggle: (id: string) => void;
}) {
  return (
    <div className="key-detail__presence-control">
      <label className="field">
        <span>{label}</span>
        <select value={mode} onChange={(event) => onMode(event.currentTarget.value as PresenceMode)}>
          <option value="none">none</option>
          <option value="all">all</option>
          <option value="explicit">explicit</option>
        </select>
      </label>
      <p className="key-detail__hint">{hint}</p>
      {mode === 'explicit' ? (
        environments.length === 0 ? (
          <p className="key-detail__state" role="status">
            This project has no environments yet.
          </p>
        ) : (
          <div className="key-detail__presence-envs">
            {environments.map((environment) => (
              <Toggle
                key={environment.id}
                label={environment.name}
                on={ids.has(environment.id)}
                disabled={false}
                onChange={() => onToggle(environment.id)}
              />
            ))}
          </div>
        )
      ) : null}
    </div>
  );
}

/**
 * GroupEditor sets or clears a key's group membership (#493). Membership is
 * coupling — a schema change — but not a scanning ingress, so a refusal stays
 * inline. Changing the selection commits immediately.
 */
function GroupEditor({
  refData,
  keyId,
  record,
}: {
  refData: MatrixRef;
  keyId: string;
  record: MatrixKey;
}) {
  const groups = useKeyGroups(refData);
  const setGroup = useSetKeyGroup(refData, keyId);
  const id = useId();
  const [refusal, setRefusal] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  return (
    <section className="key-detail__section" aria-labelledby={`${id}-group`}>
      <h3 id={`${id}-group`}>Group membership</h3>
      <label className="field">
        <span>Key group</span>
        <select
          value={record.group_id}
          disabled={setGroup.isPending || !groups.isSuccess}
          onChange={(event) => {
            const groupId = event.currentTarget.value;
            setRefusal(null);
            setDone(false);
            setGroup.mutate(
              { groupId },
              {
                onSuccess: () => setDone(true),
                onError: (error) => setRefusal(catalogueRefusalText(error, 'change the group')),
              },
            );
          }}
        >
          <option value="">(no group)</option>
          {(groups.data?.items ?? []).map((group) => (
            <option key={group.id} value={group.id}>
              {group.name}
            </option>
          ))}
        </select>
      </label>
      {groups.isError ? <Alert>The project’s key groups could not be read.</Alert> : null}
      {refusal === null ? null : <Alert>{refusal}</Alert>}
      {done ? <Done>Group updated.</Done> : null}
    </section>
  );
}
