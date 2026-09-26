# Handoff: GitLab CI/CD variable adapter (#159)

Branch: `claude/modest-feynman-hm8knz`. Spec: `docs/adr/gitlab-adapter.md`
(deltas over `deployment-adapter.md`, amendment banners on that ADR and on
`mvp-boundary.md`). Built on the #157 multi-target workflow.

## Where things live

- **Provider:** `internal/adapter/gitlab` (`client.go` closed `API` and
  `operationRegistry`, SPKI pin and CA bundle in `TLSConfig`; `module.go` the
  four seam operations). Registered in `internal/adapter/provider.go`,
  `internal/app/adapter_provider.go`, and `ValidateProviderManifest`
  (`ValidateGitLabManifest`, masking rule pinned once as `GitLabMaskable`).
- **Transport policy:** `adapter.Config` gained `SPKIPin`, `CABundlePEM`, and
  `AllowPersonalToken`. The module factory refuses them for any other provider.
  Stored on `adapters` (`spki_pin`, `ca_bundle_pem`, `allow_personal_token`),
  read back as `store.AdapterTransport` by the worker loader, plan, test, and
  move paths.
- **Target fields:** `adapter.Destination.Scope` (GitLab `environment_scope`)
  and `adapter.Target.Options` (`Protected`, `Hidden`, `Expand`). Stored on
  `adapter_targets` (`destination_scope`, `variable_*`). GitLab projects and
  groups reuse the `repository` and `organization` destination kinds, which
  avoided rebuilding four SQLite tables for a CHECK change.
- **Ledger:** `adapter_ledger.destination_scope` joins the active-name unique
  index, and `refuseDestinationNameCollision` compares scope, so one project can
  hold the same key under different scopes. Scope is immutable on update
  (store refuses a change).
- **Migration:** `00060_gitlab_adapter.sql` on both engines. The legacy upgrade
  drill (`internal/app/backup_upgrade_drill_test.go`) reverses it, and
  `internal/buildcompat/development.json` was regenerated with
  `go run ./scripts/release/compatibility --development` against PostgreSQL
  18.4 (PostgreSQL 16 produces a different schema digest).
- **Service:** `normalizeTargetInput` defaults the scope to `*`, restricts
  GitLab to project and group kinds, and refuses GitLab-only fields elsewhere.
  Dropping `protected` on update takes the full ceremony. The create audit
  payload records `spki_pin` and `personal_credential_accepted` (a field name
  containing `token` is forbidden by the audit registry).
- **API/CLI/web:** optional OpenAPI fields (`destination_scope`,
  `variable_*`, `spki_pin`, `ca_bundle`, `allow_personal_token`; response
  `ca_bundle_present`), regenerated `apigen` and `clients/ts`. CLI
  `--provider gitlab`, `--kind project|group`, `--scope`, `--protected`,
  `--hidden`, `--expand-variables`, `--spki-pin`, `--ca-bundle-file`,
  `--allow-personal-token`. The browser adapter form and `TargetForm` gained
  the same; `web/src/api/sensitiveInventory.json` re-pinned with a review note.

## Tests

- `internal/adapter/gitlab`: closure (method set plus route scan), TLS pin and
  CA, error classification (taken, 401, 429 headers), token policy, masking,
  ledger state machine (taken conflict, owned-missing, ambiguous replay, prune
  and teardown order, moved project).
- `internal/service/adapters_gitlab_test.go`, `internal/cli/adapters_gitlab_internal_test.go`,
  `web/src/routes/Adapters.test.tsx`.
- `internal/isolation/gitlab_e2e_test.go` (`TestGitLabRealLifecycle`): gated
  by `HIKYO_TEST_GITLAB_URL/TOKEN/PROJECT/GROUP` (plus optional `_CA_FILE`,
  `_SPKI_PIN`, `_ALLOWED_CIDR`, `_ALLOW_PERSONAL_TOKEN`); skips locally and
  fails when `HIKYO_TEST_GITLAB_REQUIRED=1`. It runs the durable store journal
  on both engines. It was exercised here against an in-process GitLab
  emulator only; the real-instance run is `.github/workflows/gitlab-e2e.yml`
  (weekly and on dispatch), which boots `gitlab/gitlab-ce` over HTTPS with a
  throwaway certificate via `scripts/ci/start-gitlab.sh` and mints a group
  access token so the default personal-token refusal stays on.

## Known limits

- One Hikyo environment maps to at most one scope per project (the target
  uniqueness key does not include scope).
- Route-move claim and activation collision checks are scope-blind, which is
  conservative: a GitLab destination move can be refused where a scoped
  comparison would allow it.
- Pin and CA bundle are fixed per adapter; changing them means a new adapter.
- File-type variables and group-to-project inheritance management are out of
  scope, as the issue states.
