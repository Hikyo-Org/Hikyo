# Handoff: delivery-target reporting, controller to browser (#791)

Branch: `test/791-delivery-target-reporting-e2e`. Test and CI only; no product
code changed.

Spec: `docs/adr/k8s-condition-reporting.md` (D1 to D11, locked), validation
ticket 5. Builds on #788 (`delivery-target-condition-reporting.md`) and #789,
#790 (`789-790-806-reporting-and-retry-after.md`).

## What landed

| File | Role |
| --- | --- |
| `internal/isolation/k8s_reporting_e2e_test.go` | `TestK8sReportingBrowser` (`-tags k8se2e`): server, operator, wire capture, the scenario steps |
| `web/e2e/reporting.config.ts` | separate Playwright config, outside the flow registry |
| `web/e2e/reporting/reporting.spec.ts` | the twelve browser tests and their screenshots |
| `scripts/ci/reporting-e2e.sh` | kind cluster, SPA build, `go test` |
| `.github/workflows/reporting-e2e.yml` | weekly and `workflow_dispatch` job, uploads the evidence |

## Shape

Three real parties:

- **Operator:** the shipped `hikyo operator` process, built with
  `-X main.version=0.0.0-reporting-e2e`, run on the host against kind with its
  own leader election. Stopped with SIGTERM, restarted with
  `HIKYO_OPERATOR_STATUS_REPORTING=false` and back.
- **Server:** the real router over a real SQLite datastore, in the test
  process. That is what makes `service.Delivery.Now` reachable: staleness and
  the purge move that clock and never sleep. The shipped binary has no clock
  setting, so a spawned server could only have reached those states by
  rewriting rows.
- **Browser:** Chromium under Playwright, signed in through the login form,
  reading the built bundle (`internal/webui/dist`) the server serves.

Playwright owns the order. Each test calls `POST /step/<name>` on a control
endpoint; the step runs on the Go test goroutine, arranges cluster and server
state and asserts the wire; the test then asserts the page. The Go test fails
if Playwright fails or if any step never ran.

The operator has its own TLS listener, the front. It records every
`/delivery-targets` body and the status answered. Crafted replays go to the
browser's listener, so the record holds only what the operator sent. Closing
the front is "server unreachable": the browser keeps working.

Each step reply carries the delivery clock's offset and the spec sets the
page clock to match, so "received 16 minutes ago" on a screenshot agrees with
the state beside it.

## Run it

```sh
pnpm --dir web run e2e:install   # once: Chromium for the locked Playwright
./scripts/ci/reporting-e2e.sh
```

Needs docker, kind, go and pnpm. About two minutes after the cluster is up.
Evidence lands in `web/test-results/reporting-e2e/` (or
`HIKYO_REPORTING_E2E_ARTIFACTS`): `01-` to `14-*.png`, `operator.log`,
`wire.log` (status, path and body of every captured request).

Against a cluster you already have:

```sh
./scripts/ci/build-spa.sh
HIKYO_K8S_E2E_KUBECONFIG=/path/to/kubeconfig \
  go test -count=1 -tags k8se2e -run '^TestK8sReportingBrowser$' ./internal/isolation/ -timeout 20m -v
```

## Scenario map

| Issue scenario | Playwright test | Go step | Screenshot |
| --- | --- | --- | --- |
| 1 happy path | `1 happy path: a reconciled CR is reported with its namespace and name` | `stepHappyPath` | `01-reported` |
| 2 cross-tenant | `2 cross-tenant: a report naming another tenant is refused and shows nowhere` | `stepCrossTenant` | `02-cross-tenant-project-b` |
| 3 grant revoked | `3 revoked grant: the row turns reporter-revoked` | `stepGrantRevoked` | `03-grant-revoked` |
| 3 token revoked | `3 revoked credential: its row is reporter-revoked while other CRs keep reporting` | `stepCredentialRevoked` | `04-credential-revoked` |
| 3 principal deleted | `3 deleted principal: its rows are gone` | `stepPrincipalDeleted` | `05-principal-deleted` |
| 4 ordering, 409 | `4 out-of-order reports are refused 409 and never shown as refused` | `stepOutOfOrder` | `06-out-of-order-unchanged` |
| 4 unknown reason, 422 | `4 unknown reason: refused 422 and the row says refused` | `stepUnknownReason` | `07-refused` |
| 5 stale | `5 stale: after the operator stops, the row is stale` | `stepOperatorStopped` | `08-stale` |
| 5 never reported | `5 never reported: a CR created while reporting is disabled has no row` | `stepReportingDisabled` | `09-never-reported-tab`, `10-never-reported-account` |
| 6 tombstone | `6 deletion: the tombstone removes the row` | `stepTombstone` | `11-tombstoned` |
| 6 unreachable at delete | `6 deletion with the server unreachable: the row stays, goes stale, and is purged` | `stepUnreachableDelete`, `stepStaleAfterDelete`, `stepPurge` | `12-orphaned-reported`, `13-orphaned-stale`, `14-orphaned-purged` |
| 7 secret-safe payload | `7 secret-safe payload: every body on the wire is value-free` | `stepWireAudit` | none (`wire.log`) |

## Deviations from the issue wording

- **Scenario 5, `unknown`.** The issue expects a CR created while reporting
  is disabled to show `unknown`. The ADR (D5) and the implementation say
  otherwise: `unknown` is "no row", it is not a wire state, and the list names
  only principals that own rows or carry a quota notice. Such a CR therefore
  has no row and no `unknown` badge. The test asserts what ships: no row on
  the Kubernetes tab, and its service account's expansion reads "No reports
  from this account in the environments you can read. No report is not
  health." The `unknown` badge itself is reachable only for a quota-refused
  principal without rows, which is service-level coverage
  (`TestDeliveryTargetQuotaNoticeAcrossEnvironments`).
- **Scenario 6, "purges later".** The purge is the scheduler's own job
  function, `PurgeExpiredTargets`, called once with a clock thirty days on.
  The hourly scheduler is not part of the in-process server. The happy CR
  reports at a delivery clock 29 days on first, so the purge must spare it:
  the orphaned row goes, the happy row stays. The browser then reads at the
  real clock again (a session would not survive 29 days), so screenshot 14
  shows the happy row received in the future.
- **Scenario 7, condition messages.** The denied set is built from the run
  itself: every condition message, cursor, cursor binding, stamp and managed
  Secret UID any CR held after any step, every minted bearer, and the four key
  names and values. Bodies are also decoded strictly into the contract types.
- **Revocations are made through the services**, as the administrator the
  browser is signed in as, not by clicking. The grant dialog and revoke
  buttons are the flow suite's subject (`machine-access.spec.ts`).

## Things a fresh context should not re-derive

- **Every screenshot carries one alert**, "The grant rows could not be read".
  Reading grant rows is MFA-mandatory and the harness runs with the second
  factor optional, so the session is password-only. It is unrelated to the
  Kubernetes tab.
- **Service accounts are created by the signed-in administrator**, not the
  isolation suite's `usr_ident`: the SPA parses `created_by` and refuses an id
  that is not production-shaped, which fails the whole account listing.
- **A reconcile is forced by changing `spec.resyncInterval`**, kept under five
  minutes. Above it the reported heartbeat grows and the staleness threshold
  with it.
- **After a 401 or 404 a CR is suppressed for an hour** (D9), so each
  revocation leg has its own CR and service account.
- **An operator restart re-reports every CR**: its reporting state is in
  memory. After the restart in scenario 6 the never-reported CR gets a row;
  scenario 5 asserts before that.
- **A killed operator leaves its lease**, so each restart waits for it
  (about 20 s). That is startup, not a threshold wait.
- **The suite is not a PR gate.** No pull-request job has both kind and
  Chromium, and `scripts/ci/ci-job-registry.json` is read from the base
  commit. `k8s-e2e.sh` runs `TestK8sOperator` only, so it does not pick this
  test up.

## Product bugs found

None.
