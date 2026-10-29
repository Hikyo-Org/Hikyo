# Independent audit collection reference (#1037)

Status: **reference implementation and local product/Unix rehearsal, operational
acceptance HOLD**. Entry point and evidence: [issue handoff](../handoff/1037-independent-evidence.md).
The [deployment preparation](audit-collector-deployment.md) describes the executable
host assets and approval boundaries.
This is an external scheduled-export example, not a server sink, SIEM, new machine
identity class, or a locked-ADR amendment. Keep [#1037](https://github.com/Hikyo-Org/Hikyo/issues/1037)
open. Alert delivery belongs to [#1038](https://github.com/Hikyo-Org/Hikyo/issues/1038).

## Authority and prerequisites

Read the [audit model](../adr/audit-model.md), [threat model](../adr/threat-model.md),
[operations contract](../adr/ops-spec.md), and [amendment procedure](../adr/oss-mechanics.md)
before adopting this example. It uses the existing scheduled-export mitigation;
it does not change their trust or retention boundaries.

| Read | Existing authority | Operational limit |
| --- | --- | --- |
| One project's tenant slice | Project-owned `automation` service account, project `audit-read`, ordinary credential expiry/revocation | One fixed project or environment per configured stream. No instance or cross-project grant. |
| Org-wide tenant slice | Authorized human, org `audit-read` and current assurance | Supervised collection; a project automation credential cannot replace this human. |
| Instance trail | Authorized human, instance `audit-read` and current assurance | Requires #1035's supported route. No unattended instance collector credential is authorized here. |

At inspected main `ce5e85835383b6428676ff595e2a0c1a72f6a088` on 2026-10-29,
#1035's instance route and #1036's negotiated completion protocol are merged,
and both issues are closed. Collector PR #1079 is also integrated through #1087.
The example still refuses older servers, including HTTP 200 with valid JSONL
without a verified receipt. Local product exports now exercise both engines;
protected-host and final release-candidate acceptance remain separate and pending.

## Separate the three identities

1. **Hikyo runtime/operator domain:** owns the producer. It receives neither the
   destination SSH private key nor destination filesystem/admin credentials.
2. **Polling identity:** runs on another host, holds only its scoped Hikyo read
   credential and a destination submit-only SSH key. It owns a private spool.
3. **Evidence administrator:** independent person/account, recovery authority and
   storage domain. Owns the destination database, backups, retention, read access
   and deletion. The submitter cannot update or remove these files, change the
   receiver/configuration, open an unrestricted shell, or administer this account.

The destination runs a fixed receiver command as a separate evidence account.
Use an OpenSSH forced command and `restrict`, with no shell, SFTP, forwarding,
agent forwarding, user-controlled command/configuration, or writable code path.
The evidence account owns its private database directory. An administrator can
grant that exact receiver invocation through a fixed sudo rule if necessary;
never grant generic Python, shell, `init`, arbitrary arguments, or `status` through
the submit-only key. The evidence administrator tests refusal of all those paths.
Do not install such access on a real host under this assignment.

Hikyo runtime credentials alone must be unable to authenticate to this SSH service
or delete destination evidence. Sharing host root, cloud/storage administration,
backup deletion credentials or a recovery account defeats the separation, even
with different Unix usernames. SSH exit success alone is insufficient: the poller
also verifies a complete destination acknowledgment against the pending unit.

## Example configuration and commands

The reference is [receiver.py](../../scripts/ops/audit-collector/receiver.py).
Requirements: POSIX Python 3.11 or later with `sqlite3` and `fcntl`, curl supporting
the listed options, and OpenSSH with forced commands and strict host-key checking.
Local rehearsal used Python 3.14.4, curl 8.7.1 and OpenSSH 10.3p1. Python owns
bounded validation and transactions; curl and OpenSSH own TLS/authentication.
The receiver database is independent destination storage, not Hikyo's datastore.
Use a local durable filesystem with working SQLite locks and fsync, not a shared
network filesystem. Validate crash/power-loss behavior for the chosen storage.

On the isolated destination, the evidence administrator installs reviewed code
under an administrator-owned directory and creates a private database directory.
A fixed stream configuration, readable only by appropriate accounts, looks like:

```json
{
  "source_id": "fixture-installation-a",
  "trail": "tenant",
  "scope": {"org_id": "org_fixture", "project_id": "prj_fixture"},
  "database": "/var/lib/hikyo-evidence/project-a.sqlite",
  "max_bytes": 16777216,
  "max_rows": 20000
}
```

`source_id` is an independent administrator's pinned installation identifier,
bound in the acceptance record to the intended origin, TLS trust, installation
identity and selected build. It is not an identity asserted by a received event.
Change it for a restored/replaced installation until continuity is reconciled.
For an instance stream use `"trail":"instance","scope":{}` and its own database.
For org-wide collection use only `org_id`; for an environment add `env_id`.
The receiver checks every row against the fixed scope. Inventory streams outside
Hikyo, so a stopped or missing stream cannot disappear from the monitor's list.

Initialize once, as the evidence account, from the installed code directory:

```sh
umask 077
python3 -I -B receiver.py init /etc/hikyo-evidence/project-a.json
```

Expected: exit 0, a private SQLite file. Reinitializing an existing path fails;
never delete/reinitialize it to clear an error. The forced command is the fixed
installed invocation of `receiver.py receive` with that configuration. Use one
key/forced configuration per stream. Keep the evidence code/configuration outside
the submitter's writable paths, and invoke Python with `-I` to exclude ambient
module search paths.
Bound concurrent receiver sessions at the SSH service, and wrap the fixed receive
command in an administrator-owned deadline (for example GNU `timeout
--kill-after=5s 180s` on a host where those flags have been verified). Otherwise a
client holding a submit key could leave stdin open indefinitely. This host-policy
and permission exercise remains part of protected-destination acceptance.

On the independent polling host, install the same reviewed script and stream
binding. Its `database` setting is unused by `collect`; it grants no destination
access. Pre-create a private spool directory owned by the polling account.
Provision a private curl configuration through the operator's existing protected
credential mechanism. Its only credential setting is an `Authorization` header;
use an actual authorized credential, never a placeholder that could be deployed.
Do not put its value in argv, environment dumps, shell history, Git or this report.
Do not enable verbose tracing, redirects or relaxed certificate validation.
Use the correct system trust or an administrator-provisioned CA. Never use a
project credential for the instance stream or manufacture a long-lived human session.

Pin the evidence server's host key through an independent authenticated channel.
The private SSH configuration defines the `evidence-project-a` host alias, User,
IdentityFile, IdentitiesOnly and UserKnownHostsFile. It must not request a remote
command; the server's forced command selects the receiver. An example finite poll
from the installed script directory is:

```sh
python3 -I -B receiver.py collect /etc/hikyo-evidence/project-a.json \
  --url 'https://hikyo.example.invalid/api/v1/orgs/org_fixture/projects/prj_fixture/audit/export?receipt=v1' \
  --curl-config /etc/hikyo-collector/project-a.curl \
  --ssh-config /etc/hikyo-collector/ssh_config \
  --ssh-host evidence-project-a \
  --spool /var/lib/hikyo-collector/project-a
```

Replace the fixture origin/IDs with the authorized isolated fixture values.
Paths above are deployment examples, not local checkout locations. For the
instance route the URL is `/api/v1/instance/audit/export?receipt=v1`, after #1035.
The script refuses HTTP, userinfo, foreign routes and all other query parameters.
In particular it refuses `after_seq`, `from`, `to` and restrictive event filters.
Do not add automatic assurance bypass/refresh for human collection.

Expected success: one JSON acknowledgment with source, trail, scope, batch ID,
packet SHA-256, row count and receiver `received_ns`, then exit 0. Failure: nonzero
exit and a fixed HOLD diagnostic with no producer content. Treat every nonzero
exit, missing acknowledgment or mismatched digest as incomplete. Keep the spool.

## Complete units, retries and ordering

The receiver packet contains three framing lines (`Hikyo-Audit-Receipt`,
`Hikyo-Audit-Export-ID`, `Hikyo-Audit-Snapshot-Time`), a blank line, then the exact
negotiated JSONL body. These are local transport framing, never events inserted
into Hikyo's legacy JSONL. The poller creates them only after verifying HTTP 200,
`application/x-ndjson`, and exactly one of each required response header, with no
redirect. The receipt version must be `v1`.
The final body line must be exactly one version-1 `export_receipt` with a nonempty
`started_event_id` and `snapshot_time` matching those headers, the exact event-line count, `outcome=success`,
`outcome_recorded=true`, and no failure cause. Missing/duplicate/trailing receipts,
partial lines, invalid UTF-8, lines above the protocol's 1 MiB limit, wrong scopes,
conflicting IDs or exceeded local limits fail closed.
These are shape/completion checks, not a second copy of Hikyo's event-schema registry.
Treat the received content as sensitive, untrusted data; never execute/render it.

1. Acquire a nonblocking per-spool OS lock. If pending bytes exist, retry them
   before contacting the source. Otherwise download one full retained-scope export
   under existing server budgets and fixed local byte/row limits.
2. Verify completion, then write pending bytes through fsync, atomic rename and
   directory fsync. Partial downloads never become pending complete units.
3. Submit those exact bytes through the pinned submit-only SSH channel. The
   receiver independently validates them, then atomically commits events, the
   original packet, its digest, receipt identity and receiver time with SQLite
   `synchronous=EXTRA`. No acknowledgment is sent before commit returns.
4. Deduplicate by the fixed stream's stable event ID and canonical JSON content.
   Same ID/different content is HOLD, never replacement. Same batch ID/different
   bytes is HOLD. An identical retry returns its original acknowledgment/time.
5. Verify that acknowledgment against source/trail/scope, batch ID, digest and
   row count. Fsync the poller's checkpoint, then unlink/fsync pending bytes.
   A crash at either boundary retries safely. Never acknowledge local send success.

The event index stores a typed structural comparison of JSON, preserving arbitrary
integer and decimal precision; investigate using the original packet bytes.
Different numeric spellings may conservatively require reconciliation. Receiver
refusal timestamps and rejected-packet digests are retained independently; a new
successful unit does not delete that refusal history. The status verdict describes
current receipt freshness, not resolution of earlier unknown intervals.

The checkpoint identifies an acknowledged complete export unit. It is **not a
sequence, time or global source-completeness watermark**. Every new poll replays
the full currently retained authorized scope. Source SQLite serializes writes;
PostgreSQL allocation `seq` can precede commit. The server's internal `commit_seq`,
snapshot registration and writer barrier own a single export's commit-order
coverage. The public allocation sequence and wall-clock timestamps cannot safely
resume across exports. No fixed overlap interval repairs that in general. The
reference's late-lower-sequence test is a collector replay test, not proof that a
PostgreSQL server was exercised.

Local limits intentionally bound this small example. If the retained scope exceeds
20,000 rows or 16 MiB, stop and size/review an alternative; never silently narrow
the time window or advance past an oversized batch. The receipt, serialization
and transaction costs need capacity measurement before a real deployment.
Export INTENT/OUTCOME events themselves contribute to that growth, even without
other activity. The proposed five-minute fixture cadence is not capacity proof
for retaining a year's audit history.

## Independent freshness, outages and retention

Proposed fixture schedule: one poll per 5 minutes, serialized per stream, with
bounded retry backoff (30 seconds, 2 minutes, then the normal 5-minute schedule).
Do not retry indefinitely in-process or exceed existing export budgets. Offset
streams to respect per-principal/org/instance limits. This is a proposal for the
isolated exercise, not an activated deployment or a new product default.

An independently scheduled monitor, administered with the evidence destination,
runs the following for **every expected stream**, even while polling is stopped:

```sh
python3 -I -B receiver.py status /etc/hikyo-evidence/project-a.json --max-age 900
```

It exits 1 for never received, receipt older than 15 minutes, a newer rejected
submission, a future receiver timestamp, or an unavailable/misbound journal.
An unchanged replay cannot refresh this clock. Source `recorded_at`, HTTP success
and poll-process liveness cannot clear it. Monitor invocation failure is also a
failed check. The supervising monitor needs its own dead-man check outside the
poller/Hikyo failure domain; #1038 owns routing and delivery acknowledgment.
No alert was sent or delivery tested here.

The added `monitor.py` checks every independently configured expected stream,
the poller's last finite attempt and free destination capacity. It also observes
the cumulative receiver refusal count and durable source-attempt history, so a
success between monitor checks cannot hide a refusal or failed/unknown poll.
Stale/unavailable collection and missed detector intervals also latch HOLD.
Receiver and poll failure counts survive later success; receiver history remains
visible after monitor state recreation. Protect monitor state from deletion. Removed expected streams remain HOLD in its durable inventory.
There is intentionally no automatic reconciliation/reset command. The independent
owner must retain the original failure history and review any future reconciliation
mechanism before clearing that interval. `status` itself describes current
freshness; a newer success may clear its current failure flag.

`health.py` serves only PASS/HOLD over operator-provisioned TLS. It refuses a
missing, future or older-than-120-second monitor snapshot. A separate watchdog
must observe the endpoint, including detector/health-service outage. This is a
preparation interface for #1038; neither an HTTP health check nor a local sink
assertion establishes external notification delivery or human acknowledgment.

`receipt_age_seconds` measures receiver freshness, **not event-generation lag**.
A fresh empty success proves a completed read, not that a producer emitted all
events. Record newest source event time only as an untrusted diagnostic if desired.
Independent missing-event detection needs independently expected activity or a
trusted producer-completeness protocol; this example cannot derive it from gaps.

Candidate destination retention: 365 days for entire received batches, with the
evidence administrator explicitly approving access-class/IP/user-agent retention,
capacity, encrypted backups, recovery and deletion. This reference deletes nothing.
Only that independent administrator may implement retention and acknowledge its
expiry. Backup/restore must preserve this journal's custody and test read access;
copy a live SQLite journal through SQLite's backup API, never an uncoordinated
file copy. Protect disk/backup encryption independently of Hikyo's root key.

Hikyo source retention remains 90 days access / 365 days security by default and
can be changed independently. Pruning does not wait for this acknowledgment.
Org deletion can remove its trail. At cold start inventory the available baseline
and record that older/pruned history is unknown. After any outage, retention
change, org deletion or uncertain collection interval, reconcile the independent
copies and recorded pruning/deletion events. Mark possible loss **HOLD/unknown**;
later successful exports do not prove that the interval was recovered. Rows may
commit and be pruned between polls without detection. Do not promise complete
coverage merely because the outage was shorter than a retention setting.

## Acceptance exercise and launch claim

Run the portable synthetic rehearsal without credentials or network destinations:

```sh
python3 -I -B -m unittest discover -s scripts/ops/audit-collector -p 'test_*.py' -v
```

It exercises real local destination transactions and receiver subprocess crashes;
curl/SSH calls in the poller tests are synthetic transport controls. It does not
prove real TLS/SSH access restrictions, independent administration, a live Hikyo
receipt, both source engines or notification delivery. The
[handoff](../handoff/1037-independent-evidence.md) separates those acceptance gates.

Recommended **claim proposal**, requiring maintainer acceptance before use:

> Operators can use the external reference recipe to retain acknowledged,
> completed audit exports under independent administration and monitor receipt
> freshness. Hikyo does not ship a native external sink or an independently
> complete/tamper-proof producer history. Automated collection remains project
> scoped; instance and org-wide collection require an authorized human.

Do not use even that operational claim until the protected-destination exercise
and prerequisite integrations pass. Until then describe this as a tested local
reference with operational acceptance pending. Keep #1037's general acceptance
open. Stronger unattended instance authority, pruning gated on acknowledgment,
or independently complete forwarding needs its own approved contract and the
normal ADR process, not an implicit waiver in this collector PR.
