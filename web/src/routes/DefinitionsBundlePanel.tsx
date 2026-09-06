import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import {
  applyBundle,
  bundleRefusalText,
  checkBundle,
  definitionsExportPath,
  planBundle,
  readDefinitionsFile,
  type DefinitionsBundle,
  type DefinitionsDiff,
  type DefinitionsPlan,
} from '../api/definitions-bundle.ts';
import { ApiError, type RefusalFinding } from '../api/client.ts';
import { GIT_DEFINITIONS_NOTICE, type DefinitionsSettings } from '../api/definitions.ts';
import { useTransport, useWorkspaceContext } from '../api/transport.tsx';
import { Alert, ConsequencesDialog, Done } from './Sections.tsx';
import { ScanBlockDialog } from './ScanBlockDialog.tsx';
import { useModalDialog } from './useModalDialog.ts';

type Props = { org: string; project: string; settings: DefinitionsSettings };

export function DefinitionsBundlePanel({ org, project, settings }: Props) {
  const [open, setOpen] = useState(false);
  const workspace = useWorkspaceContext();
  const path = definitionsExportPath({ org, project });
  return (
    <>
      <div className="settings-row">
        <div className="settings-row__copy">
          <span className="settings-row__title">Definitions bundle</span>
          <span className="settings-row__detail">
            download, compare and review catalogue changes from a file
          </span>
        </div>
        <span className="settings-row__spacer" />
        {workspace === null ? (
          <>
            <a className="btn" href={path} download="definitions.json">
              Download definitions bundle
            </a>
            <a className="btn" href={`${path}?portable=true`} download="definitions-portable.json">
              Download portable bundle
            </a>
          </>
        ) : (
          <span>Open settings on the instance itself to download a bundle.</span>
        )}
        <button className="btn" type="button" onClick={() => setOpen(true)}>
          Check a bundle
        </button>
      </div>
      {open ? (
        <BundleDialog
          key={`${org}/${project}`}
          org={org}
          project={project}
          settings={settings}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  );
}

function BundleDialog({ org, project, settings, onClose }: Props & { onClose: () => void }) {
  const dialog = useModalDialog();
  const titleId = useId();
  const fileId = useId();
  const fileInput = useRef<HTMLInputElement>(null);
  const transport = useTransport();
  const queries = useQueryClient();
  const [bundle, setBundle] = useState<DefinitionsBundle | null>(null);
  const [checked, setChecked] = useState<Awaited<ReturnType<typeof checkBundle>> | null>(null);
  const [plan, setPlan] = useState<DefinitionsPlan | null>(null);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [allowDelete, setAllowDelete] = useState(false);
  const [scan, setScan] = useState<{
    findings: readonly RefusalFinding[];
    action: 'plan' | 'apply';
  } | null>(null);
  const active = useRef<AbortController | null>(null);
  useEffect(() => () => active.current?.abort(), []);
  const begin = () => {
    active.current?.abort();
    const controller = new AbortController();
    active.current = controller;
    setBusy(true);
    setFailure(null);
    setDone(null);
    return controller.signal;
  };
  const refused = (error: unknown, action?: 'plan' | 'apply') => {
    if (
      error instanceof ApiError &&
      error.status !== 404 &&
      error.findings.length > 0 &&
      action !== undefined
    )
      setScan({ findings: error.findings, action });
    setFailure(bundleRefusalText(error));
  };
  const selectFile = async (file: File | undefined) => {
    const signal = begin();
    setBundle(null);
    setChecked(null);
    setPlan(null);
    setAllowDelete(false);
    setScan(null);
    try {
      if (file === undefined) return;
      const parsed = await readDefinitionsFile(file);
      if (!signal.aborted) setBundle(parsed);
    } catch (error) {
      if (!signal.aborted)
        setFailure(error instanceof Error ? error.message : 'The file could not be read.');
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  };
  const check = async () => {
    if (bundle === null) return;
    const signal = begin();
    setChecked(null);
    setPlan(null);
    setAllowDelete(false);
    try {
      const result = await checkBundle({ org, project }, bundle, transport, signal);
      if (!signal.aborted) setChecked(result);
    } catch (error) {
      if (!signal.aborted) refused(error);
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  };
  const createPlan = async (tokens: readonly string[] = []) => {
    if (bundle === null || checked === null) return;
    const signal = begin();
    setPlan(null);
    setAllowDelete(false);
    try {
      const result = await planBundle({ org, project }, bundle, transport, signal, tokens);
      if (!signal.aborted) {
        setPlan(result.plan);
        setScan(null);
      }
    } catch (error) {
      if (!signal.aborted) refused(error, 'plan');
      if (tokens.length > 0) throw error;
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  };
  const apply = async (tokens: readonly string[] = []) => {
    if (plan === null) return;
    if (settings.definitions_source !== 'db') {
      setConfirm(false);
      setFailure(
        'Browser apply is refused for this Git-managed project. Apply from the repository.',
      );
      return;
    }
    const signal = begin();
    try {
      const result = await applyBundle(
        { org, project },
        plan,
        allowDelete,
        transport,
        signal,
        tokens,
      );
      if (!signal.aborted) {
        setConfirm(false);
        setPlan(null);
        setChecked(null);
        setBundle(null);
        if (fileInput.current !== null) fileInput.current.value = '';
        setScan(null);
        setDone(
          `Definitions applied at revision ${result.revision}. Published environments: ${result.published.join(', ') || 'none (no changes)'}.`,
        );
        void queries.invalidateQueries();
      }
    } catch (error) {
      if (!signal.aborted) {
        setConfirm(false);
        refused(error, 'apply');
      }
      if (tokens.length > 0) throw error;
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  };
  const git = settings.definitions_source === 'git';
  return (
    <dialog
      className="matrix-editor definitions-bundle"
      ref={dialog}
      aria-labelledby={titleId}
      onClose={onClose}
    >
      <div className="matrix-editor__head">
        <div>
          <h2 id={titleId}>Definitions bundle</h2>
          <p>Compare a file, review its immutable impact plan, then publish atomically.</p>
        </div>
        <button
          className="btn matrix-editor__close"
          type="button"
          aria-label="Close definitions bundle"
          onClick={onClose}
        >
          ✕
        </button>
      </div>
      <div className="definitions-bundle__body">
        {git ? (
          <Alert>
            {monoCommands(GIT_DEFINITIONS_NOTICE)} Checking and planning stay available here;
            browser apply is refused.
          </Alert>
        ) : null}
        {git && settings.last_apply !== undefined ? (
          <LastApplyProvenance lastApply={settings.last_apply} />
        ) : null}
        {failure === null ? null : <Alert>{failure}</Alert>}
        {done === null ? null : <Done>{done}</Done>}
        <div className="field">
          <label htmlFor={fileId}>Definitions bundle file (JSON, up to 1 MiB)</label>
          <input
            id={fileId}
            ref={fileInput}
            type="file"
            accept=".json,application/json"
            disabled={busy}
            onChange={(event) => void selectFile(event.currentTarget.files?.[0])}
          />
        </div>
        <p className="settings-note">
          Portable bundles add definitions; a based bundle can rename or delete existing
          definitions. Files remain only in this open dialog.
        </p>
        <div className="panel__actions">
          <button
            className="btn"
            type="button"
            disabled={bundle === null || busy}
            onClick={() => void check()}
          >
            Check bundle
          </button>
          <button
            className="btn"
            type="button"
            disabled={checked === null || busy}
            onClick={() => void createPlan()}
          >
            Create impact plan
          </button>
        </div>
        {busy ? <p role="status">Checking the instance…</p> : null}
        {checked === null ? null : (
          <section aria-label="Bundle check">
            <h3>Bundle check</h3>
            <p role="status">
              {driftText(checked.state)} Current revision: {checked.current_revision}. Bundle base:{' '}
              {checked.base_revision ?? 'portable (additive)'}.
            </p>
            <DifferenceList differences={checked.differences} />
            {(checked.findings ?? []).length === 0 ? null : (
              <Alert>
                Credential-shaped content detected. Planning will require review or correction of
                the file.
              </Alert>
            )}
            {(checked.findings ?? []).map((finding, index) => (
              <p className="mono" key={index}>
                {finding.rule_id}: {finding.locator}
              </p>
            ))}
          </section>
        )}
        {plan === null ? null : (
          <section aria-label="Immutable impact plan">
            <h3>Immutable impact plan</h3>
            <dl className="remote__facts">
              <dt>Plan</dt>
              <dd className="mono">{plan.id}</dd>
              <dt>Digest</dt>
              <dd className="mono">{plan.digest}</dd>
              <dt>Current revision</dt>
              <dd>{plan.current_revision}</dd>
              <dt>Expires</dt>
              <dd>{new Date(plan.expires_at).toLocaleString()}</dd>
              <dt>Mode</dt>
              <dd>{plan.additive ? 'Portable (additive)' : 'Based bundle'}</dd>
            </dl>
            <DifferenceList differences={plan.diff} />
            <p>Protected environments: {plan.protected_environments.join(', ') || 'none'}.</p>
            {plan.diff.key_deletions.map((key) => (
              <p key={key.name}>
                Delete key {key.name}; live in: {key.live_in.join(', ') || 'no environments'}.
              </p>
            ))}
            {plan.diff.env_deletions.map((env) => (
              <p key={env.name}>
                Delete environment {env.name}; {env.occurrences} occurrences removed.
              </p>
            ))}
            {plan.reveal_required.length === 0 ? null : (
              <Alert>
                Reveal authorization is required for: {plan.reveal_required.join(', ')}.
                Secret-to-config changes require the key detail declassification ceremony.
              </Alert>
            )}
            {plan.deletions_present ? (
              <label className="definitions-bundle__delete">
                <input
                  type="checkbox"
                  checked={allowDelete}
                  disabled={busy || git}
                  onChange={(event) => setAllowDelete(event.currentTarget.checked)}
                />{' '}
                I reviewed and allow the listed deletions.
              </label>
            ) : null}
            <button
              className="btn btn--primary"
              type="button"
              disabled={busy || git || (plan.deletions_present && !allowDelete)}
              onClick={() => setConfirm(true)}
            >
              Review and apply
            </button>
          </section>
        )}
      </div>
      {confirm && plan !== null ? (
        <ConsequencesDialog
          titleId={`${titleId}-apply`}
          title="Apply definitions and publish"
          confirmLabel="Apply and publish"
          busyLabel="Applying definitions…"
          busy={busy}
          failure={null}
          onCancel={() => setConfirm(false)}
          onConfirm={() => void apply()}
        >
          <p>
            This applies the exact reviewed plan and publishes a new schema revision into every
            project environment. Publish permission is checked separately in every environment; any
            refusal rolls back the entire operation.
          </p>
          <p>Protected environments: {plan.protected_environments.join(', ') || 'none'}.</p>
          {plan.deletions_present ? (
            <p>The listed deletions are permanent, including their values and history.</p>
          ) : null}
        </ConsequencesDialog>
      ) : null}
      {scan === null ? null : (
        <ScanBlockDialog
          title="Bundle scanning refused"
          intro="Review these redacted findings. Override only intentional documentation-class content."
          findings={scan.findings}
          onOverride={
            git && scan.action === 'apply'
              ? null
              : (tokens) => (scan.action === 'plan' ? createPlan(tokens) : apply(tokens))
          }
          onClose={() => setScan(null)}
        />
      )}
    </dialog>
  );
}

/**
 * monoCommands renders the spec's notice sentence with its backtick-quoted
 * command names in `.mono`, so the text stays the normative one verbatim and
 * the commands still read as commands.
 */
export function monoCommands(text: string): ReactNode[] {
  return text.split('`').map((part, index) =>
    index % 2 === 1 ? <span className="mono" key={index}>{part}</span> : part,
  );
}

/**
 * The last-applied provenance labels. They are whatever the applying CLI
 * said about itself; Hikyo never verifies them against a repository, and the
 * note says so beside them rather than leaving a commit hash to look like proof.
 */
export function LastApplyProvenance({
  lastApply,
}: {
  lastApply: NonNullable<DefinitionsSettings['last_apply']>;
}) {
  const labels = [
    ['Commit', lastApply.commit],
    ['Ref', lastApply.ref],
    ['Actor', lastApply.actor],
  ].filter((entry): entry is [string, string] => entry[1] !== undefined && entry[1] !== '');
  return (
    <p className="definitions-bundle__provenance">
      Last applied{' '}
      <time dateTime={lastApply.applied_at}>{new Date(lastApply.applied_at).toLocaleString()}</time>
      {labels.map(([label, value]) => (
        <span key={label}>
          {' · '}
          {label} <span className="mono">{value}</span>
        </span>
      ))}
      {labels.length === 0 ? null : (
        <span className="settings-row__detail"> (display only, not verified)</span>
      )}
    </p>
  );
}

function driftText(state: 'equal' | 'file_ahead' | 'db_ahead' | 'diverged'): string {
  switch (state) {
    case 'equal':
      return 'Equal: the file matches the database.';
    case 'file_ahead':
      return 'File ahead: the file contains changes.';
    case 'db_ahead':
      return 'Database ahead: download the current bundle before editing.';
    case 'diverged':
      return 'Diverged: reconcile file and database changes before planning.';
  }
}
function DifferenceList({ differences }: { differences: DefinitionsDiff }) {
  const kinds = [
    { name: 'Keys', diff: differences.keys },
    { name: 'Environments', diff: differences.environments },
    { name: 'Key groups', diff: differences.key_groups },
  ];
  return (
    <>
      {kinds.map(({ name, diff }) => (
        <div key={name}>
          <h4>{name}</h4>
          {diff.creates.length + diff.updates.length + diff.renames.length + diff.deletes.length ===
          0 ? (
            <p>No differences.</p>
          ) : (
            <ul>
              {diff.creates.map((item) => (
                <li key={`create:${item}`}>
                  Create <span className="mono">{item}</span>
                </li>
              ))}
              {diff.updates.map((item) => (
                <li key={`update:${item}`}>
                  Update <span className="mono">{item}</span>
                </li>
              ))}
              {diff.renames.map((item) => (
                <li key={`rename:${item.id}`}>
                  Rename <span className="mono">{item.from}</span> to{' '}
                  <span className="mono">{item.to}</span>
                </li>
              ))}
              {diff.deletes.map((item) => (
                <li key={`delete:${item}`}>
                  Delete <span className="mono">{item}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      ))}
    </>
  );
}
