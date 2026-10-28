# #1035: instance security query and export

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/1035>
PR: <https://github.com/Hikyo-Org/Hikyo/pull/1045>

## Scope and integration order

Continue the original PR and its signed history. This lane owns the instance
transport, audit CLI dispatch, browser navigation and controls, and their
negative controls. Merge #1044 before #1045. Its shared completion consumers
and instance producer seam are integrated from signed commit
`1a19f61065bd5d4274a6a84fa7cd146b32237b9a`, including follow-ups through
`220b53a5e86001e9807d9dc840717f0ca784c164`. The exact pushed dependency
binding is `194aaaece59f8eec1ce44858fed224da27ba9850`, whose final commit
changes only its handoff. The PR is stacked on #1044's
original branch for scoped review. After #1044 merges, retarget #1045 to main,
refresh the tested base/head pair and run its queue gates. Later dependency
repairs must be integrated and retested before merge.

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

Validated application revision: `cc02bdfb336a4893cbeb061bc75b646d1ceef33e`.
Story-only follow-up `47fa9abb998bbb84e21fd4a067e48159349941a9` passed the
focused two-theme story suite, typecheck, lint and API/boundary checks.
Later edits correct a Storybook wait across the existing authentication remount
and update this handoff; application and Go source bytes are unchanged. Main was rechecked
at `970ac0db7c6024f91dedcefe34db7ca560b247e3`; its changes after product
baseline `24e198b32a3ffd8d4013207b2f974d76ec49be6f` are documentation only.
The updated TLS governance record remains HOLD and grants no exception here.

| Evidence | Local result |
| --- | --- |
| CLI dispatch and help | PASS: real query/export at supported scopes; conflicting scope, invalid filters, machine instance refusal and incomplete-publication regression |
| Go owner suites | PASS: complete CLI, server, service, authz, API and shared Go receipt-consumer packages |
| SQLite and PostgreSQL 18.6 | PASS: instance real-router/native CLI, core audit, artifact admission, receipt HTTP, access uniformity, original tenant and new instance commit-order/snapshot barrier tests; both engine legs executed |
| TypeScript clients | PASS: typecheck and 67 tests, including generated SDK streaming and partial HTTP refusal |
| Web typecheck/lint | PASS with current Node 26.12.1 and pnpm 11.24.0 |
| Full web unit suite and complete-diff feedback | PASS: 217 files / 2,052 tests; complete driver passed in 159 seconds against the exact #1044 stack base |
| Audit stories and design assertions | PASS: 12 audit stories in each theme, design token/adherence/markup checks; final story-only retry includes typecheck and lint |
| Native desktop/mobile flows | PASS: six desktop and six mobile flows, including native authentication, keyboard detail navigation, loading/empty/denied/export-failure states, both themes and shared org/project/environment export controls |
| Repository boundary suite | PASS: final complete API and boundary suites |

Earlier attempts are retained as failed evidence: the first feedback run stopped
at the Node pin check after Go passed because the ambient runtime was 26.7.0;
the next full web run found the missing instance-navigation fixture expectation
(2,051 tests passed, one failed). The fixture was corrected, preserving the
assertion. The later-page authority-loss regression also failed before its fix:
the page announced end-of-trail beside a denial. The corrected page retains the
denial without announcing completion. A discarded desktop capture caught lazy
loading; final captures wait for the actual settings heading. Neither that image
nor earlier-toolchain browser runs count as the final PR evidence. Two initial
Storybook checks observed a detached heading during the fixture authentication
remount; they now re-read the live heading in the existing wait helper.

Reproduce from the repository root with the pinned toolchain and frozen
client/web dependencies. The shared driver compares the complete stacked PR
diff, including documentation and generated consumers. Queue each heavy batch
through the shared resource queue; the commands below are the batch contents:

```sh
set -eu
export GOFLAGS='-p=2'
export VITEST_MAX_WORKERS=2
export FAST_FEEDBACK_BASE=194aaaece59f8eec1ce44858fed224da27ba9850
scripts/ci/fast-feedback.sh --plan
scripts/ci/fast-feedback.sh
go test -p 2 -parallel 2 ./internal/cli ./internal/server ./internal/service ./internal/authz ./api/... ./internal/auditexport
go test -p 2 -parallel 2 ./api/... ./internal/boundary
pnpm --dir web run design:check
pnpm --dir web exec vitest run --project storybook src/routes/Audit.stories.tsx --maxWorkers=1
STORYBOOK_THEME=light pnpm --dir web exec vitest run --project storybook src/routes/Audit.stories.tsx --maxWorkers=1
```

For both-engine validation, supply an authorized disposable PostgreSQL 18
`HIKYO_TEST_POSTGRES_DSN` before this command. The harness resets derived test
databases; never use durable application storage.

```sh
go test -p 2 -parallel 2 ./internal/isolation -run '^(TestInstanceAuditWire|TestAuditCore|TestPostgresAuditExportCommitOrder|TestPostgresInstanceAuditExportCommitOrder|TestArtifactClassAdmissionWire|TestAuditExportReceiptWire|TestAccessWireUniformity)$' -count=1
```

Build the production SPA, then run the instance and existing tenant audit flows
in each native browser project. These use isolated synthetic identities and
native authentication. Filtered runs do not establish full flow-registry closure.

```sh
pnpm --dir web run build
pnpm --dir web exec playwright test --project=desktop flows/instance-admin.spec.ts flows/members.spec.ts flows/machine-access.spec.ts --grep 'instance audit security trail|pages the org trail, inspects an event and exports it|reads the project trail, narrows it to one environment, and exports each scope'
pnpm --dir web exec playwright test --project=mobile flows/instance-admin.spec.ts flows/members.spec.ts flows/machine-access.spec.ts --grep 'instance audit security trail|pages the org trail, inspects an event and exports it|reads the project trail, narrows it to one environment, and exports each scope'
```

Whole-repository `go test ./...`, race, vet, prepared full lint, full unfiltered
browser coverage and the merge-candidate matrix are NOTRUN in this lane. The
#1036 handoff records its own supplemental lint timeout and prepared retry;
those do not become passing proof for #1035. Hosted checks must be rechecked on
the exact pushed head, then again after main retarget and queue integration.

Maintainer security review and the human merge gate remain required. Cross-
provider review is SKIPPED while the authorized subscription path is
unavailable; no alternate provider or paid fallback was used. Hosted feedback,
queue admission and full candidate proof are distinct. Release, publication,
deployment, live-provider and external-destination acceptance are NOTRUN here.

## Evidence upload and cleanup handoff

The PR remains draft/HOLD until the required matched desktop/mobile before/after
images are attached in its description. The original API-only baseline is
reconstructed honestly as described above. Additional instance filter, event
and detail captures accompany it. The GitHub CLI can update the PR, but the
shared browser lacks an authenticated GitHub upload session, and no existing
approved artifact store was found. The one focused human requirement is to sign
in to GitHub for attachment upload, or identify that approved store. Do not send
passwords or tokens in chat. The local review packet contains the exact image
bytes, SHA-256 manifest and insertion instructions; no attachment URL is invented.
Screenshots and the complete offline HTML companion remain ignored local
artifacts. Do not stage them or put machine paths in this handoff.

Cleanup is verified: the disposable PostgreSQL container and its anonymous
volume are absent; native fixtures have torn down and all five owned ports
refuse connections. The baseline archive, task-built embedded SPA output and
synthetic session files were removed. Shared tool caches and the existing
PostgreSQL image remain. Local review deliverables and failed-attempt logs are
retained for review/upload; the task's exact recovery stash remains until the
reviewed stack is integrated, then can be discarded by its recorded identity.
No production or external destination was touched.
