# #1038 bounded external detection and escalation handoff

**Disposition: reference implementation prepared; full issue and integrated
alerting launch acceptance remain HOLD.** No real notification or external
message has been sent. Merge remains a maintainer decision.

Start with the [operator runbook](../../scripts/security/alert-route/README.md)
and [notification test packet](../../scripts/security/alert-route/notification-test-packet.example.json). The packet is
the executable acceptance checklist and receipt-chain template. Fill a private
copy with resource-specific authorization and actual receipts; do not commit
credentials, private destination details, acknowledgments or fixture evidence.

## Task contract

Canonical repository: `Hikyo-Org/Hikyo`. Inspected base and live main on
2026-10-28: `24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
Own only the alert consumer, sender, configuration, tests and operator packet.
No producer/collector, application schema, authority, release ledger, roadmap,
containment or UI changes. #158/#969/#1057 and existing accepted deferrals remain
outside this assignment. No release, deployment, paid usage or production change.

## What changed

`scripts/security/alert-route` is an independent finite Go command. It reads
#1037's protected receiver SQLite in a read-only snapshot, validates its exact
source/trail/scope, completion/digest and health, and evaluates bounded failure,
disclosure and sensitive-change rules. Missing source attribution is explicit.
Only event/actor/credential/scope UUID references leave the consumer. A private
durable outbox binds configuration, deduplicates stable alert identities and
persists ambiguous delivery intent before HTTPS requests. A matching destination
receipt is required; human acknowledgment is a separate operator assertion.

This is an external reference route, not a new Hikyo server sink or a general
SIEM. It creates no automatic authority actions. Existing audit and threat-model
ADRs still govern producers and collection credentials.

## Evidence and exact limits

Local synthetic tests cover thresholds and benign traffic, suspicious successful
disclosure by a watched synthetic credential, sensitive changes, wrong scope,
duplicates, out-of-order rows, restart/backoff/deduplication, stale/partial/corrupt
collection, independent receive failure, bounded outbox, TLS destination failure,
missing/wrong receipt, timeout, terminal/exhausted states, redaction, missing
acknowledgment escalation and a deliberately broken-rule negative control.

The optional `TestCollectorProducerCompatibility` invokes the reviewed #1037
receiver's real `init` and `receive` commands against disposable fixtures, then
consumes its protected record. This establishes local receiver-consumer shape
compatibility only. Record both source tree identities when rerunning it. It
does not prove actual Hikyo export, protected remote transport, or independently
administered live receipt. No test counts HTTP request completion as delivery.

The changed persistence belongs solely to the private reference outbox. Hikyo's
SQLite/PostgreSQL schema and readers are unchanged. Both application engines'
real source-to-collector exercises remain owned by #1035/#1036/#1037 and are
NOTRUN for this route. Local test sinks and synthetic acknowledgment assertions
are not real destination delivery or human acknowledgment.

## Local validation record

The focused synthetic suite, including the opt-in producer-consumer check,
passed with Go 1.27.3 on 2026-10-28. The receiver source used for that check was
frozen before execution; its SHA-256 was
`d74f0d520458548e032117adc178e6847368a17970ca3a5d2d672e7ed11cad08`.
Its checkout base was the inspected main above, with uncommitted #1037 work.
That file digest, rather than the shared base SHA alone, identifies the tested
receiver bytes. Requalify against #1037's final committed revision before launch.

Full-diff fast feedback, race validation and vet are submitted together as shared
queue job 2592. Their result and exact validated revision belong in the PR's
current evidence record; queue admission itself is not a pass. Hosted checks,
maintainer security review, merge and operational acceptance are separate gates.

## Remaining gates and owners

| Gate | Owner | State / exact requirement |
| --- | --- | --- |
| Attributable rejected-token evidence | #1034 / ADR owner | HOLD: ordinary invalid and revoked bearer silence is an accepted contract; amend through required governance, then integrate the actual signal and rule. Aggregate 4xx is insufficient. |
| Instance readers and completion | #1035 / #1036 | HOLD: consume merged routes and negotiated receipts, preserving authority and export tests. |
| Protected collection | #1037 | HOLD: integrate its final receiver contract, both-engine real exports and independent protected receipt/health; no new unattended instance authority is granted. |
| Independent outage supervisor | Operator | NOTRUN: qualify a separate failure domain/channel for stopped detector, collector loss and failed sink; retain escalation receipts. |
| Actual destination and human acknowledgment | Notification operator | NOTRUN: approve exact isolated fixture and destination, supply credentials privately, run the packet and retain receipt chain. |
| Launch claim / any mandatory deferral | Maintainer and release-scope owner | REQUIRED: complete the gates, or explicitly disposition a narrower claim and name remaining owners. This change does not close or silently defer full #1038. |
| Maintainer security review | Hikyo maintainer | Required before merge. Automated/local review is supporting evidence. |
| Cross-provider review | Required ADR owner where applicable | SKIPPED: Anthropic subscription review unavailable, no paid/provider fallback authorized. No review or ADR gate is called CLEAN. |

Focused human-only request, once the packet is reviewed: authorize one isolated
synthetic exercise for a named operator-controlled HTTPS destination, independent
receipt owner, acknowledgment operator and escalation owner, with zero paid usage
and the packet's exact data exposure/retention. Credentials must use the private
secret request channel. Until then, use plan mode and local tests only. This
request does not bypass prerequisite ADR or maintainer review gates.

## Reproduce and hand back

From the repository root, with the pinned Go toolchain:

```sh
scripts/ci/fast-feedback.sh --working-tree --plan
go test -p 2 -parallel 2 ./scripts/security/alert-route -count=1
```

For the cross-lane consumer test, set `HIKYO_ALERT_COLLECTOR_SCRIPT` to the reviewed
receiver script in a local checkout, then rerun that package. The variable is an
operator-local path, never a committed checkout location. Use the shared heavy
queue for race validation, broad checks or heavy compilation as required by
agent policy. Run fast feedback from the complete PR diff after the final edit;
record the tested revision separately from hosted CI and merge status.

Keep any offline HTML companion, raw test output and private receipt evidence
outside Git. Test-created SQLite files, TLS sinks and temporary state directories
are disposable and automatically torn down. Preserve pending operator packets
and evidence through review. This handoff changes no canonical acceptance ledger.
