# Authentication refusal signals

Hikyo records generic authentication and early refusal traffic as bounded
`auth.request_refused` v1 summaries in the instance security trail. Existing
password, developer, federation, SCIM binding-mismatch and authenticated
`grant.denied` events keep their dedicated ownership and cardinality.

Use the authorized instance query/export interfaces delivered by
[#1035](https://github.com/Hikyo-Org/Hikyo/issues/1035), with instance
`audit-read`. A tenant audit view does not contain these summaries, and a project
automation credential does not gain instance authority. Filter by type and time
window, then inspect the structured `samples` array. Its `request_id` values
correlate with the server's `X-Hikyo-Request-ID` response header. That header is
generated before public admission; caller request-ID headers and JSON-RPC ids
are not correlation authority. Allowlisted workspace origins may read it through
CORS. Each summary represents multiple requests, so its envelope correlation
field is not an individual request id.

Windows use datastore time and last 60 seconds. An active window is durable
staging state and is normally published after its end by the one-second sealer.
Startup recovers pending staging; quiet windows do not require another attack
request to appear. Across all nodes sharing the database, each window retains
at most 32 source/request/operation samples and emits at most five stage events.
Each stage carries `count` and `unsampled_count`; the latter explicitly accounts
for requests beyond the shared sampling bound. Under a flood, a particular
request need not have a sample. These are attempt counts, not distinct people,
tokens, accounts or inferred compromise.

Samples contain canonical source IP, a server request id and a validated
operation name, or `unmatched` for an early refusal. They exclude raw credentials,
DPoP, passwords, attempted usernames, bodies, user-agent prose, raw paths/queries
and token fingerprints. Summary actors have no identified principal or
credential. Proxy headers are trusted only under the existing configured proxy
policy. The existing dedicated events retain their own sanitization contracts;
this feature does not make arbitrary secrets pasted into legacy free-text fields
recognizable. Security retention applies to sealed summaries. Unsealed evidence
is retained in the fixed staging row when sealing fails.

## Collection health

Collect these fixed, label-free series from the existing protected operational
`/metrics` listener. That listener's existing access restrictions remain in force.

| Metric | Meaning |
| --- | --- |
| `hikyo_auth_refusal_accounting_healthy` | 1 after the latest successful accounting attempt; 0 before the first success or after a failure |
| `hikyo_auth_refusal_accounting_failures_total` | Record/seal calls without a confirmed durable result, including queue, deadline and sink failures |
| `hikyo_auth_refusal_pending_window_timestamp_seconds` | Last observed pending datastore window start, or 0 when empty |
| `hikyo_auth_refusal_writers_in_flight` | At most eight signal writers per process, shared across configuration generations |
| `hikyo_auth_refusal_writers_waiting` | At most 64 waiting signal writers per process |

The entire signal-work attempt, including queue acquisition, has a 100 ms
deadline. Failure preserves the original refusal response and never authenticates
anyone. There is no in-memory replacement audit store or unbounded background
queue. Failed seals leave old staging intact and refuse its overwrite.

Failure counters count accounting calls, including seals, not a provably exact
number of lost network requests. A failed or interrupted commit can have an
uncertain outcome. It receives no durability receipt and is not retried merely
because its outcome is unknown. Counts are exact for committed updates; no
lossless claim is made during an outage. Health counters are process-local and
reset on restart; durable summaries and the staging row are database-shared.

Alert on failed accounting, unhealthy collection, scrape failure and an overdue
pending window. For example, an increase in the failure counter over five minutes
or a nonzero pending timestamp older than 65 seconds warrants investigation.
Check collector/server clock health when interpreting age. A zero result during
pending, unavailable or unhealthy collection does not demonstrate no attacks.
External collection, independent retention and actual alert receipts belong to
#1037/#1038 and require their own acceptance evidence.

The canonical admission seam and uniform overload response remain owned by
[#1040](https://github.com/Hikyo-Org/Hikyo/issues/1040). Recording a refusal does
not charge admission again or introduce account lockout. Source tests, hosted CI,
integration of dependency PRs, publication and deployed operation are distinct
evidence classes.
