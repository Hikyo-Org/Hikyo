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

The definitions settings read now carries the caller's own affordance:

- `api/openapi.yaml`: `DefinitionsSettings.can_declare_keys` (optional boolean).
- `internal/service/definitions.go`: `callerCanDeclareKeys` evaluates
  `CallerHolds(OpKeyCreate)` and then `CallerHolds(OpValuePublish)` for each
  environment, the same two halves the write evaluates. Set on `GetSettings` only.
- `internal/authz/registry.go`: `OpDefinitionsSettingsGet` gained
  `StoreEnvironmentsList` for that enumeration.
- Generated code refreshed: `api/apigen`, `clients/ts/src/generated`.

The settings read was chosen over the project read because every declaring
surface already fetches it for the Git-mode check, so no surface needs a new
request.

Surfaces gated on the flag:

| Surface | File | Withheld when the flag is not `true` |
|---|---|---|
| Matrix header, group header, empty state | `web/src/routes/Matrix.tsx` | "+ New key", "+ Key", "Declare first key", "Cleanup" |
| Scan warning | `web/src/routes/ScanWarnDialog.tsx` | "Reclassify as secret" |
| Import wizard | `web/src/routes/ImportWizard.tsx` | new keys are skipped and named, as in Git mode |
| Folders and linked keys | `web/src/routes/CatalogueManageDialog.tsx` | create, rename, delete |
| Key declaration detail | `web/src/routes/KeyDeclarationDetail.tsx` | every editor; the panel says the caller lacks permission |

## Behaviour change to know about

An absent or failed settings read used to leave declaration AVAILABLE, so that a
failed read never fabricated Git mode. The Git notice still follows that rule.
The declare actions now follow the opposite default: they appear only once the
server has said the caller may declare.

## Known ceiling

One flag covers every declaration edit. Folder edits, rename and metadata edits
need only `definitions-edit`, so a caller who holds it without `publish` on every
environment is also refused those in the UI, though the server would allow them.
Split the flag if that combination turns out to matter.

## Sensitivity inventory

`web/src/api/sensitiveInventory.json` re-pinned for `routes/ImportWizard.tsx`
with a review note: the change swaps the boolean that skips new-key declaration;
no mutation surface, cache boundary or plaintext ownership changed.

## Tests

- `internal/conformance/catalogue_test.go`, `scenarioCanDeclareKeys`: reader,
  publisher, maintainer missing `publish` on one environment, and full
  maintainer. Asserts the flag AND that the create agrees with it (refusals are
  `ErrNotFound`).
- `web/src/routes/Matrix.git-managed.test.tsx`: declare actions withdrawn when
  the flag is false, without the Git notice.
- `web/src/routes/KeyDeclarationDetail.test.tsx`: editors withheld and the
  permission sentence shown.
