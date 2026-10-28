# Hikyo — Ops catalogue additions landed at synthesis (2026-08-06)

[ops-spec.md](../adr/ops-spec.md) owns every concrete operational value; several post-lock ADRs declared **categories** into its composable-maxima catalogue and delegated the **values** to synthesis. This document lands those values under ops-spec's rules: every bound is a named user-visible refusal; every default is overridable at the stated scope unless marked fixed; all values are sane at the Pi-4/4 GB calibration floor. These rows join the ops-spec decision inventory; contradictions reopen the owning ticket.

## Row numbering collision (reported, not corrected)

The ops-spec inventory carries **two rows numbered 15** (the secret-scanning post-lock insertion collided with plans/pins/grants). Rows are referenced by *name* throughout the corpus, so no reference is ambiguous; renumbering the locked ADR's inventory is an editorial amendment under the oss-mechanics procedure, not a synthesis act — recorded here and in [open-items.md](./open-items.md), values untouched.

## Authentication refusal summaries (#1034)

Owner-approved 2026-10-28; operative with the
[refusal signal amendment](../adr/auth-refusal-signals.md). These are signal-work
bounds, not changes to authentication or #1040 admission quotas.

| Entry | Value | Scope |
| --- | --- | --- |
| Datastore summary window | 60 seconds | fixed |
| Durable staging rows | one instance singleton | fixed |
| Staging state size | 16,384 bytes | fixed |
| Source/request/operation samples | 32 across the whole window | fixed |
| Sealed events | at most five per window, one per nonempty stage | fixed |
| Sealer tick | one second, plus immediate startup recovery | fixed |
| Signal writers | eight in flight and 64 waiting per process | fixed |
| Signal-work deadline | 100 ms including queue acquisition | fixed |
| Retention | existing instance security retention | instance-config |

The [operator guide](../operations/authentication-refusal-signals.md) names the
five fixed health series, sample/correlation lookup and explicit uncertainty on
failed accounting. Pending evidence is never overwritten to start another
window. Existing dedicated refusals and authenticated denials keep their own
durability and cardinality.

## SAML (defaults fixed in [saml-sp.md](../adr/saml-sp.md); catalogued here as tunable entries)

| Entry | Default | Scope |
|---|---|---|
| Clock skew | 60 s | instance-config |
| Transaction TTL (login/reauth/link) | 10 min | instance-config |
| `IssueInstant` max age (Response + Assertion) | 5 min | instance-config |
| Reauth `AuthnInstant` freshness | 5 min (+ skew) | instance-config |
| Replay-cache retention | assertion `NotOnOrAfter` + skew | fixed |
| Document bounds | ≤ 256 KiB decoded · depth ≤ 32 · ≤ 50 000 XML tokens | fixed |

## SCIM ([scim-provisioning.md](../adr/scim-provisioning.md) § 9 delegation)

| Entry | Default | Scope |
|---|---|---|
| Binding staleness threshold (no IdP write → stale badge; never revokes) | 24 h | instance-config |
| Wire request body cap | 256 KiB | fixed |
| Page size (`count` clamp, list responses) | 100 (max 200) | fixed |
| Per-binding rate limit | 120 req/min, burst 240 (uniform 429 beyond) | instance-config |

## Multi-instance directory + workspace ([multi-instance.md](../adr/multi-instance.md) delegation)

| Entry | Default | Scope |
|---|---|---|
| Per-remote fetch deadline | 10 s | instance-config |
| Per-remote response size cap | 1 MiB | fixed |
| Remote count cap | 25 | instance-config |
| Parallel fan-out cap | 4 | instance-config |
| Coalescing window (duplicate view triggers) | 5 s | fixed |
| Per-viewer trigger rate | 6/min | instance-config |
| Instance-wide aggregate trigger rate | 60/min | instance-config |
| Workspace session idle / absolute lifetime | 15 min / 4 h (mirrors the reveal window row: hard-short) | instance-config, capped by remote |
| Handoff transaction expiry | 5 min, single-use | fixed |

## Import connectors ([import-paths.md](../adr/import-paths.md) bounds-existence delegation)

| Entry | Default | Scope |
|---|---|---|
| Per-file size (file mode) | 10 MiB | instance-config |
| Per-response size (live mode) | 5 MiB (runtime response-cap row) | fixed |
| Decoded-bytes cap per run | 50 MiB | instance-config |
| Record count per run | 50 000 | instance-config |
| Tree depth | 32 (matches declaration-depth row) | fixed |
| Per-request deadline (live) | 30 s | instance-config |
| Whole-run deadline | 10 min | instance-config |
| Page/request cap (live pagination) | 1 000 pages | fixed |
| Wizard session aggregate (wall clock / decoded bytes) | 30 min / 100 MiB | fixed |

## GitHub adapter ([github-adapter.md](../adr/github-adapter.md) ops delegation)

GitHub's own headers drive runtime pacing (the ADR's decision tree); these values size the defaults and bounds around it, composable with the adapter-outbox rows (row 19: outbox 30 s → 1 h jittered, depth 1000/target, concurrency 1/target · 4/org):

| Entry | Default | Scope |
|---|---|---|
| Mutation spacing | serial per credential, ≥ 1 s (fixed in the ADR) | fixed |
| Converge retry/backoff | 30 s → 1 h jittered, 8 attempts, then `failed` naming the step | instance-config |
| Headerless 403/429 wait | ≥ 1 min exponential, capped at 1 h | fixed |
| Single-converge wall-clock bound | 1 h; expiry = the ADR's named non-terminal (plaintext already discarded before any wait; resume re-authorizes) | instance-config |
| Pagination page size | 100 (GitHub's `per_page` maximum; partial list = fail closed by name) | fixed |
| Provider response size cap | 1 MiB (shared adapter row) | fixed |

Pi-4 fit: one serial pacer per credential at ≥ 1 s spacing bounds adapter CPU to negligible; memory rides the existing outbox depth and response-cap rows; no new resource class.

## Member invitation ([human-auth.md](../adr/human-auth.md) account-creation path, #568)

| Entry | Default | Scope |
|---|---|---|
| Invitation authority lifetime | the credential-reset lifetime (`service.ResetLifetime`) | fixed |
| Invitation authority use | single use; refused after consumption or expiry with the uniform `establishCredential` 401 | fixed |
| Username collision | `409 conflict`; the transaction leaves no principal, account or grant behind | fixed |
| Delivery | out of band (HTTP response to the inviter, or the CLI print triad); no email channel | fixed |

## Open registration and social sign-in ([ops-spec.md](../adr/ops-spec.md) amendment 2026-09-03, [#589](https://github.com/Hikyo-Org/Hikyo/issues/589); spec [social-signin.md](./social-signin.md))

| Entry | Default | Scope |
|---|---|---|
| `signup` budget (rate-only, instance-wide, charged at the first state-creating leg: local request, or unknown identity on a `login` callback with intent `sign-up`) | 20/hour | instance-config |
| Sign-up verification token lifetime | the credential-reset lifetime (`service.ResetLifetime`, 24 h); single use; hashed; fragment-carried; GET never consumes | fixed |
| Establish window (session stamp left by an `establish` callback; spent by the first local-credential mutation) | 5 min unused TTL | instance-config |
| Mail per-send deadline | 15 s (go-mail default, set explicitly) | instance-config |
| Operator test send (`mail-test` budget) | 5/hour per principal, 1 concurrent per instance; instance scope, audited (`registration.mail_intent` + `mail_outcome`); the only reachability probe (boot never dials) | instance-config |
| Pending sign-up reaper | the existing hourly pruning run; prunes expired rows only (verification deletes the row it consumes) | fixed |
| Fresh-org cap (per instance policy with landing `fresh-org`; live count of orgs carrying the policy id, inside the serialized transaction; `0` refused at write) | 100 | per policy, `manage-members@instance` |
| Mailer transport | `HIKYO_MAIL_ADDR` `host:port`; `HIKYO_MAIL_TLS` ∈ `implicit` \| `starttls`; `HIKYO_MAIL_CA_FILE`; `HIKYO_MAIL_USER`; `HIKYO_MAIL_PASSWORD_FILE` or `HIKYO_MAIL_PASSWORD` (exactly one); `HIKYO_MAIL_FROM` (RFC 5322); `HIKYO_MAIL_EHLO`; `HIKYO_MAIL_ALLOWED_CIDRS` | process config; every name in `knownEnv` |
| "Mailer configured" | static predicate: transport well-formed, `From` parses, `HIKYO_EXTERNAL_ORIGIN` explicit; reachability excluded | fixed |
| Entra app registration | tenant-specific GUID issuer rows; one multi-tenant app; `email` scope on the row; `email` + `xms_edov` optional claims (plus `auth_time` + `amr` for a reauth-capable row); ≤256 redirect URIs (100 with personal accounts) bounds the tenant count; never delete + recreate the app | runbook |
| Google client | `email` scope; HTTPS public-suffix redirect host, no IP literal (tighter than the `HIKYO_EXTERNAL_ORIGIN` validator); idle clients deleted after six months | runbook |
| GitHub OAuth app | scope `user:email` only; up to 10 callback URLs; GHES documents no PKCE support (GHES rows refused until verified) | runbook |

## Backup and disaster recovery ([ops-spec.md](../adr/ops-spec.md) section 11 delegation, #145)

Scheduling is enabled exactly when an export policy is configured
(`HIKYO_BACKUP_RECIPIENTS` + `HIKYO_BACKUP_DIR`). Each knob has a default and a
range; a value outside the range is a startup error, never a silent clamp. The
retention policy is ops-spec section 2 as amended by #930: an independent
180-day ceiling overrides retained count, with the recovery exceptions below.
There is no unlimited setting because the key hierarchy travels in every
archive. "off-host destination" is a
mounted directory the operator manages, not a native object-store client
(ops-spec section 2: Hikyo cannot reach off-box files).

| Entry | Default | Scope |
|---|---|---|
| `HIKYO_BACKUP_INTERVAL` (export cadence) | 24h (minimum 1h) | env (startup) |
| `HIKYO_BACKUP_RPO` (monitored recovery point target) | 26h (at least the interval) | env (startup) |
| `HIKYO_BACKUP_RETAIN_COUNT` (newest archives protected within the 180-day ceiling) | 7 (minimum 1) | env (startup) |
| `HIKYO_BACKUP_RETAIN_DAYS` (earlier age threshold outside retained count) | 180 (maximum 180) | env (startup) |
| Managed archive age ceiling (overrides retained count) | 180 days; exact equality does not trigger this ceiling, but the earlier soft rule still applies | fixed, subject to recovery exceptions below |
| `HIKYO_BACKUP_RTO_TARGET` (restore-drill verdict clock) | 30m | env (startup) |
| Restore-drill staleness warning | 90 days | fixed |

All managed complete archives for the current engine use this policy, including
scheduled, manual and pre-migration exports. This explicitly supersedes the
unimplemented historical separate pre-migration `count > 3` rule. Count is a
protection within the ceiling, not a maximum that deletes young archives.

Preserve the last durably recorded successful archive until a durable replacement
exists, even when it exceeds 180 days. Filenames and unrecorded publications are
not success evidence. Success records bind the ciphertext SHA-256 digest; legacy
records without a digest remain uncertain until a fresh successful export.
If success state is missing/unreadable, the recorded archive
is absent or clock/custody is uncertain, retain candidates and report uncertainty;
more than one expired candidate can remain. An expired protected archive is an
immediate retention breach. No successful-prune timestamp advances while expired
archives remain or compliance cannot be established. Persist the last prune
attempt, bounded failure reason and retained-expired count separately from export
outcomes; health API, doctor, bounded metrics and logs expose breach, unverified
and stale state. These exceptions can persist indefinitely, so 180 days is not
an unconditional erasure deadline. See the [#930 amendment and evidence
handoff](../handoff/930-backup-retention.md).

The backup object on `GET /instance/retention-health` exposes:

| Field | Meaning |
| --- | --- |
| `retention_breach` | Boolean; true when expired archives remain or compliance cannot currently be verified, including stale checks |
| `retention_reason` | Bounded string describing breach or uncertainty |
| `retained_expired_archives` | Last-observed count of complete managed archives remaining over 180 days; zero alone does not prove compliance |
| `last_prune_attempt_at` | Nullable timestamp of the latest attempt, including incomplete or failed attempts |

Doctor reports `backup-retention` with severity `error` for breached or unverified
retention. Label-free gauges `hikyo_backup_retention_breach` and
`hikyo_backup_retained_expired_archives` expose the verdict and last-observed
count. `hikyo_last_backup_prune_success_timestamp_seconds` does not advance after
failed checks.

Final publication, success recording and pruning use a directory advisory file lock across the
filesystem verification/deletion and datastore result. Every publication/record/prune writer,
including writers on other HA nodes, needs reliable cross-process advisory
locking on the shared backup mount. These checks read ciphertext and public
metadata, never custody keys.

Pi-4 fit: the two scheduled jobs ride the existing hourly scheduler and its
10-minute per-job deadline. An export writes one age-encrypted snapshot. A prune
lists the directory and reads/hashes the entire protected ciphertext archive
before deletion, so storage throughput and archive size affect its duration.
Hashing checks cancellation between reads outside the bounded datastore
transaction, while retaining the directory lock until an active read returns.
A blocked filesystem read can still delay cancellation; use storage with
reliable advisory locking and bounded operational latency.

An absent managed backup schedule alone produces a `not_scheduled` warning,
not a retention breach or doctor error. This applies both before any managed
backup exists and after a fresh successful manual export/prune. Recorded prune
evidence remains visible; a false breach flag does not certify an unchecked
installation. Observed expired archives, failed/incomplete or uncertain prune
attempts, and clock uncertainty remain errors even after scheduling is disabled.
A configured schedule with missing or stale checks also remains unhealthy.

## Key-name bound (grammar restated in [domain-model.md](./domain-model.md))

| Entry | Default | Scope |
|---|---|---|
| Key name length | ≤ 128 bytes (safe under the K8s Secret data-key limit with adapter prefixes applied) | fixed |

## Delivery-target condition reporting ([k8s-condition-reporting.md](../adr/k8s-condition-reporting.md), [#683](https://github.com/Hikyo-Org/Hikyo/issues/683))

| Entry | Default | Scope |
|---|---|---|
| Target rows per service-account principal | 100 (named 409 refusal beyond) | fixed |
| Report body size | ≤ 8 KiB | fixed |
| Report budget per principal | 60/min, separate from delivery fetches | fixed |
| Report budget per org | 300/min, separate from delivery fetches; binding above about 1500 reporting CRs per org (heartbeat load `CRs / 5` per min), beyond which rows read stale | fixed |
| Report heartbeat interval | `max(spec.resyncInterval, 5 min)`, at most 24 h; requeue at `min(spec.resyncInterval, 24 h)` while reporting | fixed |
| Staleness threshold | `2 × report_interval + 5 min` (server clamps `report_interval` to [5 min, 24 h]) | fixed |
| `reported_at` future skew | 5 min | fixed |
| Purge after last accepted report | 30 days | fixed |
| Per-CR suppression after 401/404/413/422 | 1 h, or until generation, credential reference or reportable content changes, or a successful fetch after a 401 | fixed |
| Operator report timeout | 5 s, one attempt per reconcile | fixed |
| Operator `/meta` capability cache | 10 min per `HikyoInstance` | fixed |

Pi-4 fit: at most one small upsert per CR per 5 min heartbeat, plus
change-driven reports, on a latest-state table; the purge rides the existing hourly scheduler.

## Required measurement gate (not deferrable past implementation freeze)

Per the declared [#77 amendment](https://github.com/Hikyo-Org/Hikyo/issues/77#issuecomment-5354008780), all *(measured)* entries require the native ARM64 `floor-bench` CI artifact before implementation freeze. CPU measurements use committed conservative factors (4.0 until optional physical Pi calibration); memory uses factor 1.0. Missing factors or measurements refuse acceptance. Physical Pi calibration is optional; estimates must not be described as Pi measurements. Tracked in [open-items.md](./open-items.md).


## Bounded developer credentials (#807)

[developer-credentials.md](../adr/developer-credentials.md) owns the lifecycle;
the owner-locked ops-spec amendment admits these values with the implementation.

| Entry | Default | Scope |
| --- | --- | --- |
| Fixed lifetime | 8 hours | mint may shorten; instance ceiling may lower |
| Hard lifetime maximum | 8 hours | fixed |
| Minimum instance ceiling | 1 second | fixed |
| Live credentials per human per instance | 4 | fixed |
| Renewal, offline receipts and snapshot delivery | forbidden | fixed |

Lowering the ceiling durably clamps existing expiry in the serialized policy
transaction. Raising it never restores expiry or revives an expired bearer.
