# Handoff: delivery-target reporting (#789, #790), Retry-After (#806), push-delivery ADR (#805)

Branch: `feat/789-790-805-delivery-target-reporting`.

Spec: `docs/adr/k8s-condition-reporting.md` (D1 to D11, locked) for #789 and
#790; issue #806; issue #805. Server half: `docs/handoff/delivery-target-condition-reporting.md` (#788, PR #796).
Still open after this branch: #791 (kind + Playwright end-to-end validation).

## #789 operator reporter

Code: `internal/operator/reporter.go`, hook in `reconcile_helpers.go` (`done()`).

- **Placement:** the report runs inside `done()`, after the status subresource
  write succeeds, never inside the Secret, workload or cursor sequence. A failed
  status write sends nothing. A failed fetch still reports after the status
  write. Reporting never changes the reconcile result or error.
- **Capability:** `/meta` is probed per HikyoInstance UID and held 10 minutes,
  failed probes included (registered as cache `operator.delivery-capabilities`,
  invariant 12). The operator reports in the highest vocabulary both sides know;
  none means no report and no Event.
- **When:** a report is sent when reportable content (every D4 field except
  `reported_at`) changed since the last attempt, or when the heartbeat
  `max(resyncInterval, 5 min)`, capped at 24 h, is due. Failed attempts count as
  attempts.
- **Requeue:** with reporting enabled (`r.reporter != nil`) every resync is
  capped at 24 h in `resyncResult`, whether or not the server advertises the
  capability or the probe failed (D9 keys on the operator setting).
- **Suppression (D9):** in memory, per CR. 401, 404, 413 or 422 pauses that CR
  for 1 h, lifted early by a change of generation, credential reference (includes
  the referenced object's UID and resourceVersion), reportable content, or a
  successful fetch after a 401.
- **Vocabulary:** a condition outside the advertised vocabulary skips the whole
  report and emits `StatusReportSkipped`. Other failures emit a
  `StatusReportFailed` Event at most once per CR per hour; never a condition.
- **Tombstone:** no finalizer. When the CR is gone, the last reported state is
  looked up in memory, the credential re-acquired through the designation checks
  (split out of `acquireBootstrap`/`acquireFederation` so they write no status),
  and one tombstone sent. A restart between the last report and deletion loses
  the tombstone; the row goes stale and purges after 30 days (D6).
- **Not reported:** refusals before a credential exists (Designation=False,
  invalid `resyncInterval`, missing instance, native Secret types disabled). The
  operator holds no credential of its own (D1).
- **Startup:** cluster id is the `kube-system` Namespace UID, read once; failure
  refuses to start. With reporting on, a build whose version is not SemVer
  (`dev`) refuses to start; remedy is `-X main.version=<semver>` or
  `HIKYO_OPERATOR_STATUS_REPORTING=false`. `scripts/ci/operator-floor.sh`
  builds with `0.0.0-operator-floor`.
- **Chart:** `operator.statusReporting` (default `true`, boolean enforced), env
  `HIKYO_OPERATOR_STATUS_REPORTING`, ClusterRole `get` on `namespaces` with
  `resourceNames: [kube-system]` only when enabled. `scripts/ci/check-chart.sh`
  asserts both render modes.
- **Tests:** the harness decodes every report and tombstone strictly into
  `apigen` types and fails on any condition message, key name, cursor, binding,
  stamp, managed Secret UID or bearer in the bytes, across the whole reconcile
  suite.

## #790 web UI

Code: `web/src/api/deliveryTargets.ts`, `web/src/routes/DeliveryTargets.tsx`,
grant dialog in `web/src/routes/MachineAccess.tsx`.

- **Capability first:** `useReportingSupport()` reads `/meta` once
  (`['meta', 'protocol-capabilities']`). Without `delivery-target-report/<n>` the
  tab says reporting is unsupported and issues no list calls.
- **List:** one call per environment. 404 means unreadable, so the environment is
  absent (D7); any other failure renders `unknown`. Counts say `unknown` until
  every listing has been read.
- **Treatment:** no state uses the healthy style; `reported` is neutral
  (D2/D11). Age is from `received_at` (server clock).
- **Grant gating:** whoami carries `capabilities.delivery_report_grant`
  `{instance, orgs}`, computed by `unheldGrantReach` in
  `internal/service/grants.go`, the same function `mayGrantUnheld` uses. The
  dialog offers `report-delivery-status` only when the server advertises
  reporting and `instance || orgs.includes(project.org)`. It can be added after
  the fact to an account that already reads. The earlier `GET /orgs/{org}/grants`
  probe was dropped: it was an audited read and an audited denial for project
  admins.
- **Hint cost and failure rule:** `Auth.Identity` computes both whoami hints
  (`instance_operator`, `delivery_report_grant`) and fails on a read error
  rather than rendering either false (on PostgreSQL a failed statement aborts
  the transaction anyway). `auditPrincipal` also resolves through
  `Auth.Identity`, so each audit list or export page pays one extra indexed
  grant-rows read; accepted, the same as the pre-existing operator hint.
- **Remote workspaces:** Machine Access is not mounted under `WorkspaceScope`, so
  no remote transport code ships for this tab.
- **Parity:** `listDeliveryTargets` is `{webui: machine-access}` in `api/parity.yaml`.

## #806 Retry-After and CLI exit code 7

- **Server:** `admission.RateLimitedError{Cause, Wait}` unwraps to its cause, so
  `errors.Is(err, ErrOverloaded)` still holds. `WireError` carries the seconds;
  body is byte-identical to before. Derived waits: per-IP sliding window
  (`kept[n-allowance] + 1 min - now`), HA fixed window (next minute), discovery,
  every budget bucket, federated sign-up, test email. Fixed 5 s stays for queue
  full, concurrency caps, tracking saturation, in-flight cap, shared counter
  unreachable, and per-account backoff (a real delay would disclose attempt
  counts). MCP keeps its fixed 60 (upper bound on its window limiters).
- **CLI:** HTTP 429 exits 7 (`ExitRateLimited`), stderr `hikyo: too many
  requests; Retry-After: N`. Only machine `values export` retries: at most
  `maxThrottledRetries = 2`, never beyond `maxThrottledWait = 60s` total, no
  retry without a usable header, cancellation during the wait reports
  cancellation. Pinned in `exit.go`, `help.txt` golden, `cli-reference.mdx`,
  `docs/spec/api-cli-spellings.md`.
- **ADRs:** banners in `api-cli-surface.md` (exit 7; 6 no longer covers 429,
  owner to confirm before the exit-code freeze) and `ops-spec.md`.
- **Docs:** rate limits and shared-egress CI advice in `cli-reference.mdx`,
  `machine-identities.mdx`, `troubleshooting.mdx`. A plain bearer machine
  credential does not hit the 10/min Argon2id limiter; OIDC federation does.

## #805 push delivery

`docs/adr/workload-push-delivery.md`, status Proposed. Nothing is locked; it
needs grilling. Key open points: `change_token` excluded (revision-model
forbids it on the metadata channel), HMAC not a Hikyo-signed statement (no
runtime tenant signing key), at-least-once with a stable id, private-network
receivers only via the operator egress policy file, and four ADR amendments.

## Review

Fable 5.1 adversarial review round 1: NOT CLEAN, five findings, all fixed on
this branch (24 h requeue on probe failure, test registered as a cache,
audited org-grants probe, missing handoff, cancel during Retry-After wait).
Round 2: three minors. Fixed: the two whoami hints had opposite failure
policies (both fail loud now). Accepted and noted above: the extra read on the
audit path. Rejected: re-gofmt `internal/config/config_test.go`; the pinned
formatter `scripts/ci/check-go-imports.sh` (run in CI) requires main's layout
and a newer local gofmt disagrees with it.
