# Key declaration permission gate (`can_declare_keys`)

## Reported symptom

A user clicked "+ New key" in the matrix and the create answered 404.
The action was offered although the user could not perform it.

## What a key declaration needs

`POST /api/v1/orgs/{org}/projects/{project}/keys` (`key.create`) authorizes twice:

1. `definitions-edit` on the project (`internal/authz/registry.go`, `OpKeyCreate`).
2. `publish` on EVERY environment of the project. A declaration fans a schema
   publish out to each environment (`fanOutSchemaPublish` in
   `internal/service/publish.go`) and each one is authorized for
   `OpValuePublish` immediately before commit.

Role templates: only `maintainer` and `admin` carry `definitions-edit`
(`internal/domain/permission.go`, mirrored in `web/src/api/access-templates.ts`).
`viewer`, `editor` and `publisher` cannot declare keys. A `maintainer` granted on
one environment only still fails step 2 for the others.

A refusal is 404 by design, never 403: a tenant-scoped refusal is the uniform
nonexistent outcome (`internal/authz/authorize.go`, locked by
`TestUniformNonexistentAtEveryLevel`). That contract is unchanged.

## The fix

The definitions settings read now carries two affordances about the caller:

- `can_declare_keys`: `definitions-edit` on the project AND `publish` on every
  environment. `callerCanDeclareKeys` in `internal/service/definitions.go`
  evaluates `CallerHolds(OpKeyCreate)` and then `CallerHolds(OpValuePublish)`
  for each environment, the same two halves the write evaluates.
- `can_edit_definitions`: `definitions-edit` alone
  (`CallerHolds(OpKeyUpdateMetadata)`).

Which edit needs which flag follows from whether the service call ends in
`publisher.fanOut` (`internal/service/keys.go`):

| Republishes, needs `can_declare_keys` | Republishes nothing, needs `can_edit_definitions` |
|---|---|
| key create, rename, value rules and presence, reclassify, set linked-key set, delete; linked-key set delete | folder create, rename, delete; linked-key set create and rename; key metadata (folder, description, deprecation) |

Both are set on `GetSettings` only.

Supporting changes:

- `internal/authz/registry.go`: `OpDefinitionsSettingsGet` gained
  `StoreEnvironmentsList` for that enumeration.
- Generated code refreshed: `api/apigen`, `clients/ts/src/generated`.

The settings read was chosen over the project read because every declaring
surface already fetches it for the Git-mode check, so no surface needs a new
request.

Surfaces gated on the flag:

| Surface | File | Withheld when the flag is not `true` |
|---|---|---|
| Matrix header, group header, empty state | `web/src/routes/Matrix.tsx` | "+ New key", "+ Key", "Declare first key"; "Cleanup" follows `can_edit_definitions` |
| Scan warning | `web/src/routes/ScanWarnDialog.tsx` | "Reclassify as secret" |
| Import wizard | `web/src/routes/ImportWizard.tsx` | new keys are skipped and named, as in Git mode |
| Folders and linked keys | `web/src/routes/CatalogueManageDialog.tsx` | linked-key set delete; everything else follows `can_edit_definitions` |
| Key declaration detail | `web/src/routes/KeyDeclarationDetail.tsx` | every editor except metadata, which follows `can_edit_definitions`; the panel says which permission is missing |

## Behaviour change to know about

An absent or failed settings read used to leave declaration AVAILABLE, so that a
failed read never fabricated Git mode. The Git notice still follows that rule.
The declare actions now follow the opposite default: they appear only once the
server has said the caller may declare.

## Sensitivity inventory

`web/src/api/sensitiveInventory.json` re-pinned for `routes/ImportWizard.tsx`
with a review note: the change swaps the boolean that skips new-key declaration;
no mutation surface, cache boundary or plaintext ownership changed.

## Tests

- `internal/conformance/catalogue_test.go`, `scenarioCanDeclareKeys`: reader,
  publisher, maintainer missing `publish` on one environment, and full
  maintainer. Asserts both flags AND that a key create and a folder create
  agree with them (refusals are `ErrNotFound`).
- `web/src/routes/Matrix.git-managed.test.tsx`: declare actions withdrawn when
  the flag is false, without the Git notice.
- `web/src/routes/KeyDeclarationDetail.test.tsx`: editors withheld and the
  permission sentence shown; metadata editor kept for `can_edit_definitions`.
- `web/src/routes/ImportWizard.test.tsx`: new keys skipped with the permission
  sentence and no Git notice.
- `web/src/routes/ScanWarnDialog.test.tsx`: no reclassify action without the
  callback.
- `web/src/routes/CatalogueManageDialog.test.tsx`: a pending folder or
  linked-key set delete confirmation is withdrawn when its gate closes.

## Review record

- CodeRabbit: three findings over three rounds (username in this document,
  one flag over-hiding folder edits, confirmation outliving its gate), all
  fixed; approved on the fixed head.
- Cross-provider adversarial review: skipped by owner decision. Not run, so
  not CLEAN.
- Browser verification: there is no per-PR preview environment, so the check
  is the Playwright flow `declare-gate` in `web/e2e/flows/matrix.spec.ts`,
  run against the real binary on desktop and mobile. A user holding the
  `publisher` template sees the matrix without "+ New key" and "+ Key"; the
  administrator sees both. The flow fails when the gate is removed from
  `Matrix.tsx`.
- Readiness probe (advisory): nearly ready, risk area authorization. Its
  untested-logic signal was real and is closed by the import wizard and scan
  warning tests.
