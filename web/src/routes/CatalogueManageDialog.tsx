import { useId, useState } from 'react';

import type { RefusalFinding } from '../api/client.ts';
import {
  catalogueRefusalText,
  scanFindings,
  useCreateFolder,
  useCreateKeyGroup,
  useDeleteFolder,
  useDeleteKeyGroup,
  useFolders,
  useKeyGroups,
  useRenameFolder,
  useRenameKeyGroup,
  type CatalogueAction,
  type Folder,
  type KeyGroup,
} from '../api/catalogue.ts';
import { GIT_DEFINITIONS_NOTICE, useDefinitionsSettings } from '../api/definitions.ts';
import type { MatrixRef } from '../api/keys.ts';
import { Alert, Done } from './Sections.tsx';
import { ScanBlockDialog } from './ScanBlockDialog.tsx';
import { useModalDialog } from './useModalDialog.ts';

type ScanBlockState = {
  readonly findings: readonly RefusalFinding[];
  readonly onOverride: ((tokens: readonly string[]) => Promise<void>) | null;
};

/** Route a refused catalogue write: a scanner block opens the dialog, else null. */
function toScanBlock(
  error: unknown,
  retry: (tokens: readonly string[]) => Promise<void>,
): ScanBlockState | null {
  const findings = scanFindings(error);
  if (findings === null) return null;
  return {
    findings,
    onOverride: findings.every((finding) => finding.acknowledgement !== undefined) ? retry : null,
  };
}

/**
 * CatalogueManageDialog is the folder and key-group lifecycle surface (#493).
 *
 * Folders and groups are project-scoped organisation, not per-key facts, so
 * they live here, reachable from the matrix, including its empty state, which
 * is what makes the lifecycle complete from an empty project. A folder is a
 * namespace label that owns no value and blocks no key; a group couples keys
 * and dissolves without deleting them. Both names are Surface-2 scanning
 * ingresses (#74), so create/rename route through the shared block dialog.
 */
export function CatalogueManageDialog({
  refData,
  onClose,
}: {
  refData: MatrixRef;
  onClose: () => void;
}) {
  const dialog = useModalDialog();
  const folders = useFolders(refData);
  const groups = useKeyGroups(refData);
  const definitions = useDefinitionsSettings(refData.org, refData.project);
  const readOnly = definitions.data?.definitions_source === 'git';

  return (
    <dialog className="matrix-editor catalogue-manage" ref={dialog} onClose={onClose}>
      <div className="matrix-editor__head">
        <div>
          <h2>Folders &amp; linked keys</h2>
          <p>Folders organise keys by path. Linked keys enforce publishing and presence rules.</p>
        </div>
        <button
          type="button"
          className="btn matrix-editor__close"
          aria-label="Close folders and linked keys"
          onClick={onClose}
        >
          ✕
        </button>
      </div>

      {readOnly ? <Alert>{GIT_DEFINITIONS_NOTICE}</Alert> : null}

      <section className="catalogue-manage__section" aria-labelledby="catalogue-folders">
        <h3 id="catalogue-folders">Folders</h3>
        <p>Organise keys without changing how they publish or which values must be set.</p>
        {folders.isPending ? <p role="status">Loading folders…</p> : null}
        {folders.isError ? <Alert>Folders could not be read.</Alert> : null}
        {folders.data !== undefined && folders.data.items.length === 0 ? (
          <p role="status" className="catalogue-manage__empty">
            No folders yet.
          </p>
        ) : null}
        <ul className="catalogue-manage__list">
          {(folders.data?.items ?? []).map((folder) => (
            <FolderRow key={folder.id} refData={refData} folder={folder} readOnly={readOnly} />
          ))}
        </ul>
        {readOnly ? null : <CreateFolder refData={refData} />}
      </section>

      <section className="catalogue-manage__section" aria-labelledby="catalogue-groups">
        <h3 id="catalogue-groups">Linked keys</h3>
        <p>Pending changes publish together. All linked keys must be set together in each environment.</p>
        {groups.isPending ? <p role="status">Loading linked keys…</p> : null}
        {groups.isError ? <Alert>Linked keys could not be read.</Alert> : null}
        {groups.data !== undefined && groups.data.items.length === 0 ? (
          <p role="status" className="catalogue-manage__empty">
            No linked keys yet. Create a set, then link keys from each key’s detail page.
          </p>
        ) : null}
        <ul className="catalogue-manage__list">
          {(groups.data?.items ?? []).map((group) => (
            <GroupRow key={group.id} refData={refData} group={group} readOnly={readOnly} />
          ))}
        </ul>
        {readOnly ? null : <CreateKeyGroup refData={refData} />}
      </section>
    </dialog>
  );
}

/** A create/rename form's scanning-block state and the retry that clears it. */
function useNameWrite(action: CatalogueAction) {
  const [refusal, setRefusal] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const [scanBlock, setScanBlock] = useState<ScanBlockState | null>(null);
  const run = (
    attempt: (acknowledgements: readonly string[]) => Promise<void>,
    okText: string,
  ): void => {
    setRefusal(null);
    setDone(null);
    void attempt([])
      .then(() => setDone(okText))
      .catch((error: unknown) => {
        const block = toScanBlock(error, (tokens) =>
          attempt(tokens).then(() => {
            setDone(okText);
            setScanBlock(null);
          }),
        );
        if (block !== null) {
          setScanBlock(block);
          return;
        }
        setRefusal(catalogueRefusalText(error, action));
      });
  };
  return { refusal, done, scanBlock, closeScanBlock: () => setScanBlock(null), run };
}

function CreateFolder({ refData }: { refData: MatrixRef }) {
  const create = useCreateFolder(refData);
  const write = useNameWrite('create the folder');
  const id = useId();
  const [path, setPath] = useState('');
  const trimmed = path.trim();
  return (
    <form
      className="catalogue-manage__create"
      onSubmit={(event) => {
        event.preventDefault();
        if (trimmed === '') return;
        write.run(
          (acknowledgements) =>
            new Promise<void>((resolve, reject) =>
              create.mutate(
                { path: trimmed, ...(acknowledgements.length === 0 ? {} : { acknowledgements }) },
                { onSuccess: () => resolve(), onError: (error) => reject(error) },
              ),
            ).then(() => setPath('')),
          `Folder ${trimmed} created.`,
        );
      }}
    >
      <label className="visually-hidden" htmlFor={id}>
        New folder path
      </label>
      <input
        id={id}
        className="mono"
        value={path}
        placeholder="app/db"
        disabled={create.isPending}
        onChange={(event) => setPath(event.currentTarget.value)}
      />
      <button type="submit" className="btn btn--primary" disabled={create.isPending || trimmed === ''}>
        Add folder
      </button>
      {write.refusal === null ? null : <Alert>{write.refusal}</Alert>}
      {write.done === null ? null : <Done>{write.done}</Done>}
      {write.scanBlock === null ? null : (
        <ScanBlockDialog
          title="Folder name blocked by secret scanning"
          intro="A folder name is exported to Git and treated as public. This one was refused because it looks like it carries secret material."
          findings={write.scanBlock.findings}
          onOverride={write.scanBlock.onOverride}
          onClose={write.closeScanBlock}
        />
      )}
    </form>
  );
}

function FolderRow({
  refData,
  folder,
  readOnly,
}: {
  refData: MatrixRef;
  folder: Folder;
  readOnly: boolean;
}) {
  const rename = useRenameFolder(refData, folder.id);
  const remove = useDeleteFolder(refData, folder.id);
  const write = useNameWrite('rename the folder');
  const [path, setPath] = useState(folder.path);
  const [confirming, setConfirming] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const trimmed = path.trim();
  const busy = rename.isPending || remove.isPending;
  return (
    <li className="catalogue-manage__row">
      <div className="catalogue-manage__row-main">
        <input
          className="mono"
          aria-label={`Folder path for ${folder.path}`}
          value={path}
          disabled={readOnly || busy}
          onChange={(event) => setPath(event.currentTarget.value)}
        />
        <button
          type="button"
          className="btn"
          disabled={readOnly || busy || trimmed === '' || trimmed === folder.path}
          onClick={() =>
            write.run(
              (acknowledgements) =>
                new Promise<void>((resolve, reject) =>
                  rename.mutate(
                    { path: trimmed, ...(acknowledgements.length === 0 ? {} : { acknowledgements }) },
                    { onSuccess: () => resolve(), onError: (error) => reject(error) },
                  ),
                ),
              `Folder renamed to ${trimmed}.`,
            )
          }
        >
          Rename
        </button>
        {confirming ? (
          <button
            type="button"
            className="btn btn--danger"
            disabled={busy}
            onClick={() => {
              setDeleteError(null);
              remove.mutate(undefined, {
                onError: (error) => setDeleteError(catalogueRefusalText(error, 'delete the folder')),
              });
            }}
          >
            Confirm delete
          </button>
        ) : (
          <button
            type="button"
            className="btn"
            disabled={readOnly || busy}
            onClick={() => setConfirming(true)}
          >
            Delete
          </button>
        )}
      </div>
      {write.refusal === null ? null : <Alert>{write.refusal}</Alert>}
      {deleteError === null ? null : <Alert>{deleteError}</Alert>}
      {write.done === null ? null : <Done>{write.done}</Done>}
      {write.scanBlock === null ? null : (
        <ScanBlockDialog
          title="Folder name blocked by secret scanning"
          intro="A folder name is exported to Git and treated as public. This one was refused because it looks like it carries secret material."
          findings={write.scanBlock.findings}
          onOverride={write.scanBlock.onOverride}
          onClose={write.closeScanBlock}
        />
      )}
    </li>
  );
}

function CreateKeyGroup({ refData }: { refData: MatrixRef }) {
  const create = useCreateKeyGroup(refData);
  const write = useNameWrite('create the linked-key set');
  const id = useId();
  const [name, setName] = useState('');
  const trimmed = name.trim();
  return (
    <form
      className="catalogue-manage__create"
      onSubmit={(event) => {
        event.preventDefault();
        if (trimmed === '') return;
        write.run(
          (acknowledgements) =>
            new Promise<void>((resolve, reject) =>
              create.mutate(
                { name: trimmed, ...(acknowledgements.length === 0 ? {} : { acknowledgements }) },
                { onSuccess: () => resolve(), onError: (error) => reject(error) },
              ),
            ).then(() => setName('')),
          `Linked-key set ${trimmed} created.`,
        );
      }}
    >
      <label className="visually-hidden" htmlFor={id}>
        New linked-key set name
      </label>
      <input
        id={id}
        value={name}
        placeholder="database"
        disabled={create.isPending}
        onChange={(event) => setName(event.currentTarget.value)}
      />
      <button type="submit" className="btn btn--primary" disabled={create.isPending || trimmed === ''}>
        Add linked-key set
      </button>
      {write.refusal === null ? null : <Alert>{write.refusal}</Alert>}
      {write.done === null ? null : <Done>{write.done}</Done>}
      {write.scanBlock === null ? null : (
        <ScanBlockDialog
          title="Linked-key set name blocked by secret scanning"
          intro="A linked-key set name is exported to Git and treated as public. This one was refused because it looks like it carries secret material."
          findings={write.scanBlock.findings}
          onOverride={write.scanBlock.onOverride}
          onClose={write.closeScanBlock}
        />
      )}
    </form>
  );
}

function GroupRow({
  refData,
  group,
  readOnly,
}: {
  refData: MatrixRef;
  group: KeyGroup;
  readOnly: boolean;
}) {
  const rename = useRenameKeyGroup(refData, group.id);
  const remove = useDeleteKeyGroup(refData, group.id);
  const write = useNameWrite('rename the linked-key set');
  const [name, setName] = useState(group.name);
  const [confirming, setConfirming] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const trimmed = name.trim();
  const busy = rename.isPending || remove.isPending;
  return (
    <li className="catalogue-manage__row">
      <div className="catalogue-manage__row-main">
        <input
          aria-label={`Name for linked-key set ${group.name}`}
          value={name}
          disabled={readOnly || busy}
          onChange={(event) => setName(event.currentTarget.value)}
        />
        <span className="catalogue-manage__meta">
          {String(group.members.length)} {group.members.length === 1 ? 'key' : 'keys'}
          {group.inert ? <span className="catalogue-manage__inert"> · needs at least two keys</span> : null}
        </span>
        <button
          type="button"
          className="btn"
          disabled={readOnly || busy || trimmed === '' || trimmed === group.name}
          onClick={() =>
            write.run(
              (acknowledgements) =>
                new Promise<void>((resolve, reject) =>
                  rename.mutate(
                    { name: trimmed, ...(acknowledgements.length === 0 ? {} : { acknowledgements }) },
                    { onSuccess: () => resolve(), onError: (error) => reject(error) },
                  ),
                ),
              `Linked-key set renamed to ${trimmed}.`,
            )
          }
        >
          Rename
        </button>
        {confirming ? (
          <button
            type="button"
            className="btn btn--danger"
            disabled={busy}
            onClick={() => {
              setDeleteError(null);
              remove.mutate(undefined, {
                onError: (error) => setDeleteError(catalogueRefusalText(error, 'delete the linked-key set')),
              });
            }}
          >
            Confirm delete
          </button>
        ) : (
          <button
            type="button"
            className="btn"
            disabled={readOnly || busy}
            onClick={() => setConfirming(true)}
          >
            Delete
          </button>
        )}
      </div>
      {write.refusal === null ? null : <Alert>{write.refusal}</Alert>}
      {deleteError === null ? null : <Alert>{deleteError}</Alert>}
      {write.done === null ? null : <Done>{write.done}</Done>}
      {write.scanBlock === null ? null : (
        <ScanBlockDialog
          title="Linked-key set name blocked by secret scanning"
          intro="A linked-key set name is exported to Git and treated as public. This one was refused because it looks like it carries secret material."
          findings={write.scanBlock.findings}
          onOverride={write.scanBlock.onOverride}
          onClose={write.closeScanBlock}
        />
      )}
    </li>
  );
}
