# Handoff: delivery-target condition reporting, server half (#788)

PR: https://github.com/Hikyo-Org/Hikyo/pull/796

Spec: `docs/adr/k8s-condition-reporting.md` (D1 to D11, locked) and issue #788.
Unblocks #789, #790 and #791.

## What landed

- **Permission (D1):** `report-delivery-status` at environment scope, workload
  allowlist only, refused for humans on every grant path, never implied by
  `read`. Only org or instance `manage-members` can grant it (grant-unheld rule);
  the #790 setup journey must account for that.
- **Vocabulary (D4, D5):** `internal/deliverytarget`. Closed vocabulary 1, pinned
  to the operator's condition constants. Condition `type`/`reason` are bounded
  on the wire by the Kubernetes `metav1.Condition` grammar, not an enum, so a
  well-formed value outside the vocabulary reaches the service and gets the
  ADR's 422 (the operator's per-CR suppression, D9, keys on 422, not 400).
  `lifecycle` and the reporter enum stay closed (400).
- **Store (D3, D6):** migration `00059`, latest-state rows keyed by principal and
  three UIDs, per-principal quota notices, audit index for the D2 "observed by
  server" layer. Principal and environment deletion cascade.
- **Service:** `internal/service/delivery_targets.go`. Report (upsert),
  tombstone, list with derived state, hourly `delivery_target_purge` scheduler
  job. `reported_at` is canonicalised to storage precision before ordering.
- **Wire:** three OpenAPI operations, `internal/server/delivery.go`, new error
  codes `unprocessable` (422) and `payload_too_large` (413), `/meta` token
  `delivery-target-report/1`.
- **Audit and budget (D8):** events for a new row, tombstone, purge and each
  refusal. Authorization refusals are the chokepoint's own `grant.denied`, so
  there is no `authorization` refusal cause. Separate bucket, 60/min per
  principal and 300/min per org, charged after authorization.

## Decisions taken where the ADR was silent

- An oversize body is authorized before the 413, so an unauthorized caller gets
  the uniform 404.
- Vocabulary is checked before ordering: an out-of-order report with a bad
  vocabulary records `refused` on the row instead of getting a 409.
- The list names a quota-refused principal with no rows in the environment only
  if it holds `report-delivery-status` there (`TxAuthorizer.DeliveryReporterHolds`).
- `unknown` is not a wire state: no row can carry it; the client derives it.

## Open at merge time

- **Migration order:** `00059` skips `00057`/`00058` (#794 claims 57 and
  reserves 58). Goose runs without out-of-order support, so #794 must land
  first, or whichever merges second renumbers and regenerates
  `internal/buildcompat/development.json`
  (`go run ./scripts/release/compatibility --development --out <file>` with
  `HIKYO_RELEASE_SCHEMA_POSTGRES_DSN` set) and the upgrade-drill list in
  `internal/app/backup_upgrade_drill_test.go`.
- **Generated constant rename:** the new `Retained` lifecycle value made
  oapi-codegen prefix the `RetentionConsequence` constants
  (`apigen.RetentionConsequenceCollectionEligible`). Open branches using the old
  names need the rename.

## CI fix folded in

`scripts/compose-demo.sh` now waits on `/readyz` again after the offline
`admin --dev create`/`grant`. The offline admin adopts the running server's seed
into a managed self-configuration binding, and the server answers 503 until its
next 2 s reconcile tick. The TOTP step wait usually hid that window.

Not changed, open for a decision: the CLI does not honour `Retry-After` on 503.
The contract allows a retry but does not require one, and a blanket retry would
hide the condition from `doctor`.
