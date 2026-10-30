# External security alert reference route (#1038)

Entry point and acceptance packet: [1038 handoff](../../../docs/handoff/1038-alert-escalation.md).

## Supported launch claim and holds

This is a separately run **reference consumer with synthetic local validation**.
It is not integrated detection shipped in the Hikyo server, an independently
qualified notification service, or evidence that #1038 is complete. No new
runtime telemetry, auth signal, collection authority, automatic containment or
general SIEM is introduced. The server's locked audit, egress and permission
boundaries remain in force.

The proposed external path is:

```text
Hikyo audit exports (#1034 signals / #1035 authorized readers / #1036 completion)
  -> separately administered #1037 receiver (protected SQLite, complete batches)
  -> this read-only consumer + private durable outbox
  -> operator-owned HTTPS receiver with idempotent durable receipts
  -> operator acknowledgment and independent escalation supervisor
```

Prerequisites remain HOLD until their owners deliver and validate them. In
particular, ordinary invalid/revoked session and machine bearer use is silent
under the accepted audit-model MCP amendment. This consumer does not invent a
revoked-token event or treat a 4xx metric as attributable evidence. #1034 needs
its required ADR disposition and real refusal-to-record proof. Instance audit
reads depend on #1035; unattended instance collection has no newly granted
machine authority. Project automation stays within its project subtree.

This proposed claim does not narrow the issue's mandatory acceptance. The
maintainer owns the concrete launch disposition: finish the blocked integrations
and actual delivery exercise, or explicitly record a narrower 1.0 claim and the
owner of the remaining #1038 work. Until that disposition, **full issue and
integrated-alerting launch acceptance remain HOLD**. No general ADR waiver is
inferred from stabilization approval. Maintainer security review is required.

## Exact sources and custody

Supported input is the independent receiver database schema implemented by
`scripts/ops/audit-collector/receiver.py` in #1037's separate lane. That dependency
is not copied into this change. The reader uses `mode=ro`, a five-second snapshot
deadline, the exact `source_id`/trail/scope binding, `batches` and `failure` tables.
The alert `instance` must equal the receiver's `source_id`; the alert `id` is the
local stream label. Paths resolve from the command's working directory.

The latest batch must be at most 32 MiB / 50,000 rows, with a matching SHA-256,
the negotiated receipt/version, export ID and snapshot headers, and terminal
`export_receipt` version 1. The response-bound ID/snapshot and row count must
match, with `outcome=success` and `outcome_recorded=true`. Its receiver-assigned
timestamp must be at most 900 seconds old and cannot be ahead of the check clock.
A receiver failure at or after the last receipt is unhealthy.
Empty complete exports refresh health; replaying an old batch must not do so.
Unknown schemas, missing authorization, missing database, oversized or partial
exports, changed binding, corruption, stale receipts and future clocks fail
visibly as `collector-unavailable`. No sequence or wall clock proves complete
producer emission. #1037 owns full-scope replay and commit-order coverage.

Only schema-version-1 audit envelopes are admitted. Every event ID and retained
actor, credential and scope reference must be a UUID. Non-UUID object locators
are omitted, leaving the event ID as the investigation reference. Tenant events
must match the configured org and optional project; instance events must contain
no tenant chain. Raw payload, user agent, names, URLs, email, secret values and
tokens never enter the outbox or notification. Source IP is parsed as an IP and
hashed for grouping. Its unkeyed hash is pseudonymous and guessable, not anonymous.
The original protected collector still contains audit metadata and needs its
own access, retention and deletion controls.

The receiver administrator owns retained evidence. A separate notification
operator owns the endpoint, receipts and acknowledgments. Grant the alert runner
read-only receiver access, private outbox access, and a destination-only bearer.
Do not give Hikyo runtime credentials deletion access to those copies. The
notification token is read from the configured environment variable; use the
documented protected credential manager/service environment, never command-line
token arguments or tracked files. No maintainer-controlled destination exists.

## Bounded rules

All windows are **fixed UTC five-minute buckets**, not sliding windows. They use
server `recorded_at`; client-asserted occurrence times cannot set the window.
The lookback is 25 minutes, admitting out-of-order records within that bound.
Older re-exported history is ignored. An outage longer than this bound requires
manual investigation of retained history; recovery is not retrospective proof.

| Rule | Trigger | References and limits |
| --- | --- | --- |
| `failed-login-burst` | At least five `auth.login` failures from one normalized source IP in one bucket | Event IDs and resolved actor IDs where present. Covers same-source guessing and spray-like bursts. No attempted usernames, distinct unknown-account spray detection, distributed spray or per-attacker identity claim. NAT and shared proxies can cause false positives. |
| `unattributed-login` | A login failure has no parseable source IP | Attribution gap, not an attributed attack. Fix trusted proxy/source configuration in its owner. |
| `disclosure-burst` | At least 20 successful per-key disclosure events for one actor + credential + exact scope in one bucket | `disclosure.value_revealed`, `disclosure.values_exported`, `identity.disclosure`. Counts events, not distinct keys or an exfiltration verdict. Normal large fetches can trigger. |
| `watched-disclosure` | One successful disclosure by a configured watched credential UUID | Supports a controlled suspicious-success exercise without plaintext or heuristic account risk scoring. Watchlist enrollment is operator-owned. |
| `disclosure-attribution-gap` | Successful online disclosure lacks actor or credential identity | Evidence gap, not proof of credential theft. Offline-reconciled assertions are excluded from current online disclosure rules. |
| `sensitive-change` | One successful change in the closed list below | Actor, authenticating credential, scope and opaque object references when present. Object/event references identify the affected resource; no arbitrary payload forwarding. |
| `collector-unavailable` | Source validation/read/health failure | One alert per configured source per five-minute failure bucket. Independent supervisor also escalates the failed invocation. |

Sensitive list: `grant.created`, `grant.modified`, `grant.revoked`,
`grant.template_applied`, `auth.provider_changed`, `auth.saml_provider_configure`,
`auth.saml_provider_remove`, `identity.credential_minted`,
`identity.credential_revoked`, `identity.developer_credential_minted`,
`identity.developer_credential_revoked`, `identity.grant_widened`,
`auth.password_changed`, `adapter.credential_replace`, `adapter.credential_revoke`,
`dynamic.provider_configured`, `dynamic.provider_credential_replace`,
`dynamic.provider_credential_revoke`. This is a bounded list, not all possible
sensitive events. Review additions against the event registry and real emitters.

Known benign negatives include isolated failures below five, login success,
ordinary audit reads, disclosure below 20, failed sensitive changes, old history
and offline assertions. A successful sensitive change intentionally always
alerts, including an authorized admin's change. Threshold and boundary tradeoffs
are explicit: a burst split across buckets can escape detection. Tune with
recorded benign/malicious fixture results, then change the versioned rules and
requalify. Do not silently suppress maintenance actors. Credential watchlists
and source configuration are bound into state identity; changes require an
operator-reviewed migration with preserved evidence.

## Delivery, retries and independent escalation

The receiver contract is one operator-controlled HTTPS `POST` with a bearer,
JSON alert and `Idempotency-Key` equal to the stable alert ID. It must durably
store that ID and body before returning HTTP 200/201 and:

```json
{"alert_id":"<exact alert ID>","receiver":"<configured receiver>","receipt_id":"<opaque durable receipt reference>"}
```

The receipt reference is 1..64 ASCII letters, digits, dots, underscores or
hyphens, starting with a letter or digit. It must not contain notification
content, addresses or secrets. The receiver must return the same durable receipt
on duplicate requests. A basic chat/email webhook that merely returns 200 is
not this supported contract. A bridge is an operator-owned dependency whose
downstream notification receipt must be qualified; an enqueue receipt must not
be described as human delivery. No bridge or paid service is silently assumed.

The outbox persists intent before network I/O. Received means a matching
receiver receipt, never HTTP send success or human acknowledgment. HTTP 4xx
other than 408/429 is `terminal`; timeout, redirects, 5xx, 202, mismatched or
missing receipts are `uncertain`. Retry after 30 seconds, then 120 seconds, at
most three total attempts across restarts. After that, `exhausted-uncertain`
requires reconciliation using the stable ID; never reset state to force retry.
No redirects, ambient proxy, cookies or unconfigured destination are used. TLS
uses the system trust store and a ten-second request deadline. There are at most
four attempts per invocation, oldest first. Backlog makes latency unbounded and
is reported as pending/uncertain, requiring independent escalation.

Run each minute on the independent evidence host. With five-minute collection polling and independently observed receipts
within 900 seconds, a healthy scheduler and no backlog, initial decision and
receipt should arrive within another 60 seconds plus at most 40 seconds for
four sequential attempts. This is a configuration target, not measured live
destination performance or an application-to-notification completeness bound.

Private state is capped at 50,000 recent event IDs, 5,000 outbox alerts and
32 MiB. Event IDs and received alerts expire after 24 hours; unfinished alerts
remain until reconciliation, with capacity exhaustion failing visibly. Rule
observations retain at most the 25-minute lookback. Each notification carries
the threshold-time count and at most 20 sorted event references. Later rows in
the same bucket do not generate another notification or update the first one.

An exclusive lock rejects concurrent writers. State is replaced with file and
directory synchronization. First enrollment requires the explicit `-enroll` command. Missing state stops
a sender; never use enrollment to replace a lost outbox. Preserve the private directory across restart. After
a crash, confirm no runner owns `run.lock` before the operator removes that lock.
Malformed state, config drift or persistence failure stops delivery. Back up
state under the same restricted custody; there is no automatic reset/fallback.

The configured sources are the independent expected-stream inventory. Do not
remove a failing stream to obtain a healthy result. The collection owner runs
`receiver.py status CONFIG --max-age 900` independently for every expected
stream; this reader checks those same stored receipt/failure facts and additionally
validates the packet. Poller source-auth/transport/completion errors remain
nonzero and also need independent supervision. Neither receipt age nor poller
liveness measures producer generation lag or complete emission.

**Independent supervisor requirement:** on a different failure domain, observe
the finite runner's exit status, last completed report timestamp, and last
operator acknowledgment. Escalate nonzero exit or no completed invocation for
180 seconds to the configured escalation owner using the operator's separately
qualified channel. Any terminal delivery or received alert without matching
human acknowledgment after five minutes also escalates. The JSON report lists
`unhealthy_sources`, every delivery state and `escalate` IDs. A collector outage
can notify through the healthy sink; a failed sink cannot notify through itself.
A stopped detector cannot report its own death. This tool does not implement or
claim that independent supervisor/channel. Its setup and deliberate killed-runner
negative control are mandatory acceptance holds, not implied healthy defaults.

Human acknowledgment is a private JSON array supplied via `-ack-file`:

```json
[{"alert_id":"<exact alert ID>","receipt_id":"<matching receipt>","operator":"oncall-operator-ref","acknowledged_at":"2026-10-28T12:05:00Z"}]
```

This records an operator assertion, not cryptographic proof of reading. Store
the actual acknowledgment evidence and destination receipt with the test packet.
Retain notification payloads, receipts and acks for the operator-approved period
(proposed fixture: 24 hours); apply that policy separately at the destination.
Unfinished incidents may require longer justified retention. No application
audit pruning policy waits for export or notification acknowledgment.

## Deploy, test and disable

1. Keep this route on HOLD until #1034/#1035/#1036/#1037 contracts and authority
   are settled, maintainer security review passes, and an owner approves the
   [notification test packet](notification-test-packet.example.json).
   Use an isolated Hikyo fixture with synthetic credentials only. No production
   mutation, external message or new paid usage is authorized by this PR.
2. From the repository root, with Go pinned by `go.mod`, run:

   ```sh
   go test -p 2 -parallel 2 ./scripts/security/alert-route -count=1
   ```

   Expected: self-contained tests pass; the optional cross-lane consumer check
   is NOTRUN unless `HIKYO_ALERT_COLLECTOR_SCRIPT` names the reviewed #1037 script.
   Those tests use disposable local SQLite and TLS sinks, not a real destination.
3. On the independent host, prepare the authorized #1037 streams and read-only
   access. Copy `config.example.json` to an untracked operator configuration,
   bind its exact instance/trail/org/project and receiver identities, and enable
   it only after all required fields are concrete. Disabled/incomplete configs
   deliberately fail. Run with an explicit repository working directory:

   ```sh
   go run ./scripts/security/alert-route -config operator-alerts.json
   ```

   Expected: `mode=plan-no-delivery`, source health and decisions, no network or
   outbox writes. Source failures produce a report and nonzero exit. Plan mode
   does not consume prior durable state or establish delivery/acknowledgment.
4. After separate resource-specific notification authorization, create a new
   private state directory with `mkdir -m 700 operator-alert-state`. Supply the
   bearer through protected service environment configuration. Explicitly enroll
   the empty state once without network activity:

   ```sh
   go run ./scripts/security/alert-route -config operator-alerts.json \
     -state operator-alert-state -enroll
   ```

   Expected: `enrolled-no-delivery`; repeated enrollment refuses existing state.
   The authorized finite sender command is:

   ```sh
   go run ./scripts/security/alert-route -config operator-alerts.json \
     -state operator-alert-state -send -ack-file operator-acknowledgments.json
   ```

   Initialize the acknowledgment file as `[]` with private permissions. Keep
   stdout/stderr as private evidence. Expect `received` only after matching
   receipt; nonzero means source, delivery, escalation or configuration needs
   action. Run under the independent one-minute scheduler, prohibit overlap,
   and qualify the independent supervisor. Check exact endpoint trust and the
   receiver's idempotent durable semantics before a real test.
5. Execute each packet case. For revoked-token replay, preserve the actual
   refusal record from #1034 or mark HOLD; never synthesize an acceptance event.
   Record event ID, protected batch receipt, rule, alert ID, retry state, actual
   destination receipt and named operator acknowledgment. Break both collector
   and sink independently, then kill the detector and prove the external
   supervisor notices. Resume and reconcile uncertain IDs before further sends.
6. To disable, stop the sender's scheduler, confirm its process has exited and
   inform the independent escalation owner through the already approved process.
   Disable its supervisor only as an explicit maintenance decision. Preserve
   outbox and receipts until reconciled; revoke only the notification credential
   through its owner. Keep evidence collection running if still required. Delete
   disposable fixture resources only after verifying ownership and retention.

After producer compromise, retained copies remain useful, but this route cannot
prove missing events were emitted, distinguish forged producer facts, recall
disclosed secrets, establish per-person identity from NAT IPs, detect all attacks,
or contain authority. Use #1039's separately authorized containment runbook.
