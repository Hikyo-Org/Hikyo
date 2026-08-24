# Handoff: #431 release channels and administrator updates

Pull request: https://github.com/Hikyo-Org/Hikyo/pull/431. Implementation
base: `7def9de5f5f5f4ee8d58c1636d80c379ed4e390c`.

## Contract

- Stable releases remain manually promoted and recovery/primary signed.
  Nightlies are generated from the next minor version, are notification-only,
  and never enter the privileged apply path.
- Local and directly connected remote instances expose stable, nightly, or off
  update channels. The CLI reports available updates; signed-in administrators
  receive one fleet-level toast and retain a profile badge after dismissal.
- A remote stable update is an explicit `POST`, requires a browser session
  authenticated within the reauthentication window before workspace
  connection, and records durable intent and outcome audit events.
- The server talks only to a root-owned local Unix helper. The helper owns one
  active job, a durable journal, bounded commands, and the common Plan, Backup,
  Verify, Apply, Health, and Rollback lifecycle. The server receives no host,
  Docker, Git, or cluster credentials.
- Shipped Compose, systemd, and Flux adapters verify the signed release bundle,
  back up through encrypted Hikyo export, health-check the new revision, and
  invoke an installation-specific root-owned database restore command during
  rollback. Flux additionally requires an exact, clean upstream checkout.
- A background reconciler copies terminal helper outcomes into the instance
  audit trail even when the requesting browser disconnects. Deterministic event
  IDs make a lost acknowledgement safe to retry.

Generated outputs: OpenAPI Go server/client bindings, the TypeScript client,
and sqlc models/queries. Database migrations: `00035` persists the authenticating
browser session time on remote workspaces. Audit registry additions cover update
intent, success, rollback, rollback failure, and refusal.

## Operational setup

Install the helper, one adapter, its root-owned environment file, and the
systemd unit using the exact commands in the upgrade guide. Configure an
absolute root-owned `HIKYO_ROLLBACK_BIN`; applying an update is refused if the
adapter cannot complete the installation-specific encrypted database restore
contract.

## Regression evidence

- Full `go test ./...` passed 3,978 tests in 65 packages; updater, service, and
  server race tests passed 596 tests. `go vet ./...` passed.
- WebUI typecheck, 331 tests, and production build passed. TypeScript client
  generation, typecheck, and 14 tests passed.
- Docs, generated-file checks, release fixtures, shellcheck for all shipped
  adapters, and `git diff --check` passed.
- Six explicit CGO-disabled CLI cross-builds cover Linux, macOS, and Windows on
  amd64 and arm64.

## Review and merge gate

Standards and specification review findings were repaired, including durable
outcome reconciliation, authentication-time propagation, aggregate toast
behavior, helper shutdown, release-source failure visibility, and real adapter
delivery. Because this changes authentication, authorization, audit, and a
privileged host boundary, a Hikyo maintainer security review is required before
merge. No production tag or remote update is created by this pull request.
