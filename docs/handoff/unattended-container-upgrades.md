# Unattended container upgrades and maintenance UI

## User authorization and scope

The operator explicitly approved unattended container upgrades as a first-install
opt-in, with encrypted local custody unlocked by the existing root key. The
approved scope is Docker/SQLite and singleton Kubernetes/PostgreSQL, with a
separate dedicated scratch database for PostgreSQL restore verification. Existing
manual installations are not silently enrolled. HA and competing configuration
rollout authority are rejected.

The same conversation authorized merge on green and nightly publication after
the merged main commit passes CI. Nightly image support was delivered separately
in PR #735; PR #736 repaired a malformed Docker login action pin and added a
workflow pin regression guard.

## Upgrade implementation

- `internal/app/unattended_*.go` coordinates the exact running image, local
  enrollment, signed route discovery, encrypted export, actual scratch restore,
  migration subprocesses, and durable retries. It retains installation exclusion
  through the normal server lifetime.
- `internal/upgradecustody/local.go` stores encrypted recovery and signing keys
  owned by the non-root runtime UID. Fresh enrollment binds once to the instance
  identity and its operator pin.
- `backup-preparing` freezes ordinary writes before export. Verified evidence
  converts the operation to the historical `prepared` representation before
  historical binaries execute. Uncertain schema writes retain maintenance and
  require operator recovery.
- PostgreSQL scratch ownership is enrolled only on an empty, separately named
  database. Reuse requires its enrolled DSN and complete authenticated schema;
  unknown objects refuse cleanup.

## Maintenance contract

`GET /api/v1/runtime/status` returns only `state` and nullable `phase`, with
`Cache-Control: no-store`. Normal serving checks the durable control record and
this process's admission. Storage failure is unavailable, never guessed ready.

Temporary maintenance listeners use authenticated managed listener and TLS
configuration. They expose static UI and status, refuse tenant API/MCP traffic,
keep `/healthz` live and `/readyz` unavailable, and drain before normal serving.
Resumed maintenance may read only the exact operation's saved configuration.
An unreadable partial schema cannot trigger bootstrap transport fallback.

The browser distinguishes confirmed maintenance, operator intervention, and
connection loss. A native modal blocks editing, retires sensitive disclosures,
preserves ordinary drafts and the URL, and waits for bounded session
and active-query revalidation before unblocking. Requests and mutations are never replayed.
With no ready Kubernetes endpoint, ingress may show its own unavailable page on
fresh navigation; an already-loaded UI can continue its reconnect loop.

## Validation record

Implementation validation is in progress. Do not treat this handoff as a release
or deployment claim until the remaining acceptance checks are recorded.

Verified so far:

- Public status contract, TLS maintenance, readiness, socket handoff, and
  existing candidate configuration checks pass.
- Real SQLite and PostgreSQL tests confirm control status remains readable while
  tenant admission is fenced. Maintenance configuration readers lose authority
  after phase changes and session closure; missing saved configuration refuses.
- Browser checks at desktop and mobile sizes verify a native modal, inert
  background, existing typography/colors, and automatic return without URL change.
  These browser checks used explicit prototype response fixtures.
- An independent maintenance review found a stalled authentication request could
  stop polling. The fix passes focused abort/retry tests. This was an ordinary
  independent review, not cross-provider review.
- Generated client verification, API parity, chart structural/mutation checks,
  Compose validation, ShellCheck, and docs check/build pass.

Additional verified results:

- Packaged enrollment, real encrypted backup/scratch restore, migration,
  same-image restart, downgrade refusal and missing-volume refusal passed on
  SQLite and PostgreSQL (293.700 seconds). This is process-level acceptance,
  not proof of Docker or Kubernetes orchestration.
- Linux sealed-memfd root delivery passed in a real distroless arm64 container
  with UID 65532, read-only root filesystem and all capabilities dropped.
- Preflight rejects unsupported historical executables before journal creation
  or fencing. The SQLite packaged refusal and replacement regression passed
  (275.502 seconds). Route discovery also rehydrates all selected executables
  when resuming from an already complete evidence bundle.
- Offline release cache verification derives executable authority from the
  authenticated archive. Substituted descriptors, archives and executables
  refuse; the full selfupdate race suite passed.
- Web typecheck and 976 tests pass, including stalled authentication and
  pending/failed cached-query recovery. The edit fence stays until both succeed.
- Full configuration, authorization, API and repository lint tests pass.
- Ordinary independent Standards and Spec review findings were fixed. The
  cross-provider review was skipped because quota was unavailable; it was not
  marked CLEAN.

Full app (310.420 seconds), server, service, store, command and UI-tagged API/server
suites passed. Docs check/build pass; the docs package now declares the Vite
module its Astro configuration imports instead of relying on a transitive link.

The full upgradegate package twice reached its default ten-minute timeout; a
third diagnostic run was stopped during the same host/VM slowdown. All twelve
crash-boundary cases passed together in 18.43 seconds. Native PostgreSQL monitoring
recorded 621 client samples without Lock or IO waits, while the 16 GiB host used
15.2 GiB of swap and the Docker VM consumed over six cores. No timeout was
increased. Remaining full gate and deployment acceptance execute on CI workers.

Actual distroless Docker and singleton Helm replacement harnesses are wired into
`scripts/ci/k8s-e2e.sh`, after the existing cluster is removed. The exporter builds
three real UI-enabled Linux servers and signs their exact archives with ephemeral
test trust. Only artifact acquisition is preseeded offline; the production
entrypoint reauthenticates archives and executes the actual coordinator. The
harnesses assert same-image restart, A-to-B-to-C replacement, durable instance and
custody, encrypted recovery proof, and exact healthy admission. Docker also
asserts older-image refusal. The Kubernetes harness uses TLS PostgreSQL and a
separate scratch database. Local fixture generation/static checks passed, but
local Docker/kind execution was stopped during host memory pressure before
application acceptance. CI must provide the deployment result before support
is advertised.
Remaining delivery steps: signed/DCO commit, exact-head CI, merge, and feature
release availability.

## Nightly publication evidence

PRs #735 and #736 merged. Main `55ba25b985e8f42f4026571fb10db31783798bc7`
passed CI run `34779819826`; nightly run `34780657301` succeeded. Version
`0.0.1-nightly.20260913.41.g55ba25b9` and `ghcr.io/hikyo-org/hikyo:nightly`
resolve to `sha256:01e9cb6b51bfcbc953906daa733e4e4b5cefe23f5df80cc5419beca0b6064aeb`.
After the operator made the package public, anonymous manifest reads and pulls,
external Cosign verification, arm64 version output and the static UI smoke test
all passed. That image predates this unattended-upgrade feature.

## Operational entry points

- [Operator runbook](../operations/unattended-container-upgrades.md)
- [Execution design and acceptance criteria](../design/unattended-container-upgrades.md)
- [Signed upgrade ADR](../adr/signed-upgrade-compatibility.md)
- `install/compose/server-unattended.yaml`
- Helm `upgrade.unattended.enabled`, default `false`
