import { useId, useState } from 'react';

import { ApiError } from '../api/client.ts';
import {
  issuerCreateRefusalText,
  issuerDeleteRefusalText,
  issuerFieldRefusal,
  issuerUpdateRefusalText,
  useCreateFederationIssuer,
  useDeleteFederationIssuer,
  useFederationIssuers,
  useUpdateFederationIssuer,
  type FederationIssuer,
  type FederationIssuerType,
  type FederationJwksMode,
} from '../api/federationIssuers.ts';
import { notifySuccess } from '../app/notifications.tsx';
import { Alert, Done, Panel } from './Sections.tsx';

const secondFactor = (error: unknown) => error instanceof ApiError && error.status === 403;
const nondisclosed = (error: unknown) => error instanceof ApiError && error.status === 404;

const ISSUER_TYPES: ReadonlyArray<{ readonly id: FederationIssuerType; readonly label: string }> = [
  { id: 'kubernetes', label: 'Kubernetes' },
  { id: 'forgejo', label: 'Forgejo Actions' },
  { id: 'github-actions', label: 'GitHub Actions' },
];

const JWKS_MODES: ReadonlyArray<{ readonly id: FederationJwksMode; readonly label: string }> = [
  { id: 'discovery', label: 'Discovery — fetch and cache the keys' },
  { id: 'static', label: 'Static — supply the JWKS document' },
];

/** audiencesFrom splits the one-per-line textarea into trimmed, non-empty lines. */
function audiencesFrom(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '');
}

/**
 * FederationIssuersPanel is the instance-scoped issuer lifecycle: configure,
 * inspect, edit the mutable half, and delete an OIDC federation issuer. It is
 * MFA-mandatory (`instance-config`), so a password-only session reads every
 * mutation as the second-factor refusal it is.
 *
 * The create/edit split follows the contract: `issuer` and `issuer_type` are
 * immutable and shown read-only on edit; only the JWKS source and refused
 * audiences move. The JWKS document never round-trips, so its field is always
 * blank and, under static mode, always re-entered.
 */
export function FederationIssuersPanel() {
  const issuers = useFederationIssuers();
  const create = useCreateFederationIssuer();
  const update = useUpdateFederationIssuer();
  const remove = useDeleteFederationIssuer();

  // One editor at a time: 'create', or an issuer id being edited, or null.
  const [editor, setEditor] = useState<'create' | { id: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<FederationIssuer | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const busy = create.isPending || update.isPending || remove.isPending;

  const closeAll = () => {
    setEditor(null);
    setConfirmDelete(null);
  };

  const doDelete = (issuer: FederationIssuer) => {
    setFailure(null);
    remove.mutate(issuer.id, {
      onSuccess: () => {
        const message = `Deleted the issuer ${issuer.issuer}. No binding named it, so nothing that authenticated has been orphaned.`;
        closeAll();
        setDone(message);
        notifySuccess(message);
      },
      onError: (error) => {
        setConfirmDelete(null);
        setFailure(issuerDeleteRefusalText(error));
      },
    });
  };

  return (
    <Panel id="instance-federation" title="Federation issuers · instance-config">
      <p className="settings-note">
        Each issuer is one external OpenID Connect authority this whole instance trusts. It is
        instance-scoped on purpose: an org- or project-scoped issuer would let a tenant admit a new
        identity provider the instance never reviewed. The issuer string is matched byte-for-byte,
        so a trailing slash is a different issuer.
      </p>

      {failure !== null ? <Alert>{failure}</Alert> : null}
      {done !== null ? <Done>{done}</Done> : null}

      {issuers.isPending ? <p role="status">Loading federation issuers…</p> : null}
      {secondFactor(issuers.error) ? (
        <Alert>
          Listing federation issuers is instance-config work and needs a second factor. This session
          does not have sufficient second-factor assurance; present your authenticator code or
          passkey in the banner above.
        </Alert>
      ) : null}
      {nondisclosed(issuers.error) ? (
        <p role="status">Federation issuers are not disclosed to this session.</p>
      ) : null}
      {issuers.isError && !secondFactor(issuers.error) && !nondisclosed(issuers.error) ? (
        <Alert>{issuerCreateRefusalText(issuers.error)}</Alert>
      ) : null}

      {issuers.isSuccess ? (
        issuers.data.items.length === 0 ? (
          <p className="settings-note">
            No federation issuers configured. Nothing external can present a token here until one
            exists.
          </p>
        ) : (
          <ul className="settings-list">
            {issuers.data.items.map((issuer) => (
              <li key={issuer.id} className="settings-row settings-row--stacked">
                <div className="settings-row__copy">
                  <code className="settings-row__title mono">{issuer.issuer}</code>
                  <span className="settings-row__detail">
                    {issuer.issuer_type} · JWKS {issuer.jwks_mode} · refuses{' '}
                    {issuer.refused_audiences.join(', ')} ·{' '}
                    {issuer.live_bindings === 0
                      ? 'no bindings name it'
                      : `${String(issuer.live_bindings)} binding${issuer.live_bindings === 1 ? '' : 's'} name it (live or historical)`}
                  </span>
                </div>
                <div className="panel__actions">
                  <button
                    type="button"
                    className="btn"
                    disabled={busy}
                    onClick={() => {
                      setFailure(null);
                      setDone(null);
                      setConfirmDelete(null);
                      setEditor({ id: issuer.id });
                    }}
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    className="btn btn--danger"
                    disabled={busy}
                    onClick={() => {
                      setFailure(null);
                      setDone(null);
                      setEditor(null);
                      setConfirmDelete(issuer);
                    }}
                  >
                    Delete
                  </button>
                </div>

                {confirmDelete?.id === issuer.id ? (
                  <div className="policy-impact" role="alert">
                    <p>
                      {issuer.live_bindings === 0
                        ? `Delete ${issuer.issuer}? No binding names it, so nothing that authenticates depends on it.`
                        : `${String(issuer.live_bindings)} binding${issuer.live_bindings === 1 ? '' : 's'} — live or revoked — still name ${issuer.issuer}. The delete is refused until every one is revoked from Machine Access, because erasing the issuer erases what those bindings trusted.`}
                    </p>
                    <div className="panel__actions">
                      <button
                        type="button"
                        className="btn"
                        disabled={busy}
                        onClick={() => setConfirmDelete(null)}
                      >
                        Cancel
                      </button>
                      <button
                        type="button"
                        className="btn btn--danger"
                        disabled={busy}
                        onClick={() => doDelete(issuer)}
                      >
                        Delete issuer
                      </button>
                    </div>
                  </div>
                ) : null}

                {typeof editor === 'object' && editor?.id === issuer.id ? (
                  <IssuerForm
                    key={`edit-${issuer.id}`}
                    issuer={issuer}
                    busy={busy}
                    onCancel={() => setEditor(null)}
                    onSubmit={(input) => {
                      const refusal = issuerFieldRefusal({
                        jwksMode: input.jwksMode,
                        staticJwks: input.staticJwks,
                        refusedAudiences: input.refusedAudiences,
                      });
                      if (refusal !== null) {
                        setFailure(refusal);
                        return;
                      }
                      setFailure(null);
                      update.mutate(
                        {
                          id: issuer.id,
                          jwksMode: input.jwksMode,
                          ...(input.jwksMode === 'static'
                            ? { staticJwks: input.staticJwks }
                            : {}),
                          refusedAudiences: input.refusedAudiences,
                        },
                        {
                          onSuccess: () => {
                            const message = `Updated ${issuer.issuer}. The issuer string and platform type are unchanged — those are a replacement, not an edit.`;
                            setEditor(null);
                            setDone(message);
                            notifySuccess(message);
                          },
                          onError: (error) => setFailure(issuerUpdateRefusalText(error)),
                        },
                      );
                    }}
                  />
                ) : null}
              </li>
            ))}
          </ul>
        )
      ) : null}

      {issuers.isSuccess && editor !== 'create' ? (
        <div className="panel__actions">
          <button
            type="button"
            className="btn btn--primary"
            disabled={busy}
            onClick={() => {
              setFailure(null);
              setDone(null);
              setConfirmDelete(null);
              setEditor('create');
            }}
          >
            Configure issuer
          </button>
        </div>
      ) : null}

      {editor === 'create' ? (
        <IssuerForm
          key="create"
          issuer={null}
          busy={busy}
          onCancel={() => setEditor(null)}
          onSubmit={(input) => {
            const refusal = issuerFieldRefusal({
              issuer: input.issuer,
              jwksMode: input.jwksMode,
              staticJwks: input.staticJwks,
              refusedAudiences: input.refusedAudiences,
            });
            if (refusal !== null) {
              setFailure(refusal);
              return;
            }
            setFailure(null);
            create.mutate(
              {
                issuer: input.issuer,
                issuerType: input.issuerType,
                jwksMode: input.jwksMode,
                ...(input.jwksMode === 'static' ? { staticJwks: input.staticJwks } : {}),
                refusedAudiences: input.refusedAudiences,
              },
              {
                onSuccess: (result) => {
                  const message = `Configured ${result.issuer}. It can now take federated bindings from Machine Access.`;
                  setEditor(null);
                  setDone(message);
                  notifySuccess(message);
                },
                onError: (error) => setFailure(issuerCreateRefusalText(error)),
              },
            );
          }}
        />
      ) : null}
    </Panel>
  );
}

type IssuerFormValue = {
  readonly issuer: string;
  readonly issuerType: FederationIssuerType;
  readonly jwksMode: FederationJwksMode;
  readonly staticJwks: string;
  readonly refusedAudiences: string[];
};

/**
 * IssuerForm is create and edit in one shape. On edit the issuer string and
 * platform type are read-only — the contract has no member to move them — and
 * the JWKS document field is blank because the read shape never returns it.
 */
function IssuerForm({
  issuer,
  busy,
  onCancel,
  onSubmit,
}: {
  issuer: FederationIssuer | null;
  busy: boolean;
  onCancel: () => void;
  onSubmit: (value: IssuerFormValue) => void;
}) {
  const editing = issuer !== null;
  const issuerId = useId();
  const typeId = useId();
  const modeId = useId();
  const jwksId = useId();
  const audiencesId = useId();

  const [url, setUrl] = useState(issuer?.issuer ?? '');
  const [type, setType] = useState<FederationIssuerType>(issuer?.issuer_type ?? 'kubernetes');
  const [mode, setMode] = useState<FederationJwksMode>(issuer?.jwks_mode ?? 'discovery');
  const [staticJwks, setStaticJwks] = useState('');
  const [audiences, setAudiences] = useState(issuer?.refused_audiences.join('\n') ?? '');

  return (
    <fieldset className="machine__lock" disabled={busy}>
      <legend className="machine__subhead">
        {editing ? `Edit ${issuer.issuer}` : 'Configure a federation issuer'}
      </legend>

      <div className="field">
        <label htmlFor={issuerId}>Issuer (https URL, matched byte-for-byte)</label>
        {editing ? (
          <p className="field__hint mono">{issuer.issuer}</p>
        ) : (
          <input
            id={issuerId}
            className="mono"
            value={url}
            placeholder="https://token.actions.githubusercontent.com"
            onChange={(event) => setUrl(event.target.value)}
          />
        )}
        {editing ? (
          <p className="field__hint">
            Immutable: changing the issuer re-points every binding underneath at a different
            authority, which is a replacement, not an edit.
          </p>
        ) : null}
      </div>

      <div className="field">
        <label htmlFor={typeId}>Platform type</label>
        {editing ? (
          <p className="field__hint mono">{issuer.issuer_type}</p>
        ) : (
          <select
            id={typeId}
            value={type}
            onChange={(event) => setType(event.target.value as FederationIssuerType)}
          >
            {ISSUER_TYPES.map((entry) => (
              <option key={entry.id} value={entry.id}>
                {entry.label}
              </option>
            ))}
          </select>
        )}
        {editing ? (
          <p className="field__hint">
            Immutable: the per-platform binding rules differ, so the type is declared, never
            inferred, and cannot move under existing bindings.
          </p>
        ) : null}
      </div>

      <div className="field">
        <label htmlFor={modeId}>JWKS source</label>
        <select
          id={modeId}
          value={mode}
          onChange={(event) => setMode(event.target.value as FederationJwksMode)}
        >
          {JWKS_MODES.map((entry) => (
            <option key={entry.id} value={entry.id}>
              {entry.label}
            </option>
          ))}
        </select>
      </div>

      {mode === 'static' ? (
        <div className="field">
          <label htmlFor={jwksId}>JWKS document</label>
          <textarea
            id={jwksId}
            className="mono"
            rows={5}
            value={staticJwks}
            placeholder='{"keys":[…]}'
            onChange={(event) => setStaticJwks(event.target.value)}
          />
          <p className="field__hint">
            The key set this instance verifies against. It is never returned by any read, so it is
            always entered here in full — there is no keep-the-old-document path, and there cannot be
            one that silently retains a key set nobody rotates.
          </p>
        </div>
      ) : null}

      <div className="field">
        <label htmlFor={audiencesId}>Refused audiences, one per line</label>
        <textarea
          id={audiencesId}
          className="mono"
          rows={3}
          value={audiences}
          onChange={(event) => setAudiences(event.target.value)}
        />
        <p className="field__hint">
          The issuer&apos;s default audiences, which no binding may name and no token may carry. At
          least one is required: the default-audience rule turns on the instance knowing what the
          default is, and it is not derivable.
        </p>
      </div>

      <div className="panel__actions">
        <button type="button" className="btn" onClick={onCancel}>
          Cancel
        </button>
        <button
          type="button"
          className="btn btn--primary"
          onClick={() =>
            onSubmit({
              issuer: url,
              issuerType: type,
              jwksMode: mode,
              staticJwks,
              refusedAudiences: audiencesFrom(audiences),
            })
          }
        >
          {editing ? 'Save issuer' : 'Configure issuer'}
        </button>
      </div>
    </fieldset>
  );
}
