# #1035: instance security query and export

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/1035>
PR: <https://github.com/Hikyo-Org/Hikyo/pull/1045>

## Scope and integration order

Continue the original PR and its signed history. This lane owns the instance
transport, audit CLI dispatch, browser navigation and controls, and their
negative controls. Merge #1044 before #1045. Its shared completion consumers
and instance producer seam are integrated from signed commit
`1a19f61065bd5d4274a6a84fa7cd146b32237b9a`; later dependency repairs must be
integrated and retested before merge.

#1036 owns the completion protocol. #1034 owns additional authentication
refusal signals. #1037 owns independent collection/destination acceptance.
This change neither authorizes an external collection destination nor grants
unattended instance collection. It does not change release scope, activate
stable policy, publish artifacts or establish deployed acceptance.

## Authority and supported surfaces

`GET /api/v1/instance/audit` and `/api/v1/instance/audit/export` require a human
session with instance `audit-read` and the existing MFA floor. No extra reveal
ceremony is added. Tenant grants cannot satisfy the instance formula. Missing
or invalid artifacts retain authentication refusal, ineligible artifacts retain
the uniform artifact refusal, and missing instance grants retain instance denial.
The API revision is 13 for the new routes; no migration is introduced.

The browser entry is Instance settings → Audit → Open instance audit, also
available in instance navigation. It shares the existing tenant filter, bounded
paging and detail owner. Source IP, user agent, credential, actor class, origin
and authority are visible in event details, including explicit absent values.
Tenant views explain that login failures belong to the instance trail.

The optional `whoami.capabilities.instance_audit_read` hint reflects the caller's
existing instance grant. It lets an audit reader discover the trail without
holding instance-config, and exposes only audit navigation in that case.
Configuration polling remains gated by the separate instance_operator hint.

`hikyo audit query` and `hikyo audit export` are real dispatcher leaves. Select
the server with `--instance NAME`; select the trail with explicit
`--instance-scope` or `--org ORG [--project PROJECT [--env ENV]]`. Instance scope
overrides inherited tenant selections but refuses conflicting explicit flags.
The same time, actor/name, operation, outcome, resource and correlation filters
apply to API, CLI and browser. CLI times are RFC3339; browser controls display
local time and send UTC. Query limits are 1 to 1000. JSON query output retains
the generated page contract and its `next_after_seq`, `upper_seq`, `exhausted`.

Project/environment audit routes previously admitted only human sessions,
blocking the audit ADR's promised scheduled project exports. They now also
admit the existing machine credential artifact. Existing automation-only
`audit-read` eligibility and project ownership still constrain that authority.
Organisation and instance routes remain human-only. No new machine class,
capability, grant rule or instance collector is introduced.

## Pagination, completion and failure

Queries retain allocation order and the first page's pinned `to_seq` ceiling.
A PostgreSQL transaction that commits below an already-passed allocation cursor
can require a fresh query. Exports keep the existing fixed snapshot,
commit-order paging and writer barrier, with fresh bearer resolution and proof
on every page. Public sequence numbers are not export continuation tokens.

The instance route uses `InstanceExportActorReceipt` through the shared
`auditExportStream`. It exposes #1036's `receipt=v1` and response identity,
snapshot and version headers. CLI exports stage privately on disk and call
`auditexport.Consume`; stdout is published only after verified completion.
Browser controls call `downloadAuditExport` and save only its verified Blob.
The browser consumer caps event bytes at 64 MiB. Larger exports need the CLI.
Cancellation on filter changes/navigation aborts the browser export. Loading,
empty, denied and export-failure states are text and ARIA, with no saved partial
download on protocol failure.

INTENT is durable before streaming. Existing authorized terminal OUTCOME and
revocation reconciliation behavior are preserved. A revoked caller cannot
manufacture a terminal proof: `ErrExportUnpaired` retains the started event
and durable denial, with no successful completion. See the #1036 handoff for
the full receipt contract and its separate security review obligations.

## Validation and review gates

Before repair, `TestAuditQueryDispatch` reproduced exit 2, unknown command
`audit`. The original API-only instance wire test passed on SQLite. Matching
desktop/mobile before screenshots were reconstructed from signed merge
`7d86f389` of the original PR head and main `24e198b32`.

Final integrated validation is pending. Required commands include:

```sh
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree
go test -p 2 -parallel 2 ./internal/cli ./internal/server ./internal/authz ./api/... ./internal/auditexport
go test -p 2 -parallel 2 ./internal/isolation -run '^(TestInstanceAuditWire|TestAuditCore|TestPostgresAuditExportCommitOrder|TestArtifactClassAdmissionWire|TestAuditExportReceiptWire|TestAccessWireUniformity)$' -count=1
pnpm --dir web exec playwright test --project=desktop --grep 'instance audit security trail'
pnpm --dir web exec playwright test --project=mobile --grep 'instance audit security trail'
```

Supply the documented disposable `HIKYO_TEST_POSTGRES_DSN` for both-engine
validation, install frozen dependencies with the pinned toolchain, and build
the production SPA before browser flows. Queue heavy commands through the
shared resource queue. Include the existing tenant audit browser flows because
they consume the shared export controls. Record final source/head and results
in this PR after the last edit.

Maintainer security review and the human merge gate remain required. Cross-
provider review is SKIPPED while the authorized subscription path is
unavailable; no alternate provider or paid fallback was used. Hosted feedback,
queue admission and full candidate proof are distinct. Release, publication,
deployment, live-provider and external-destination acceptance are NOTRUN here.

PR screenshots belong only in its description, with local image/HTML artifacts
kept outside Git. The task's temporary baseline archive is removed after
capture. Disposable database and browser fixture resources must be stopped
and checked before final handoff; retain review deliverables only.
