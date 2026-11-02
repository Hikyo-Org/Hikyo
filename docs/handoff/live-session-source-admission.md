# Live-session source admission

Owner decision, after #1040 made the 60 per minute source allowance the effective
ceiling for browsers: only failed or unknown credentials count against that
allowance, and live sessions are bound by the 300 per minute session budget. The
authorized endpoint is a reviewable PR. Maintainer security review and the human
merge gate remain required. No deployment or production load exercise is claimed.

## Contract

Charging only after a failed resolution would reopen #1040: a flood of unknown
artifacts would reach the three resolver reads past the allowance. So a charge
stays before the first read, and what changes is which limit a session spends once
this node has proven it live.

| Presented artifact | Charged before the first read | Past that limit |
| --- | --- | --- |
| Unknown, expired, revoked or stale, not known live | Source allowance | 429, no reads |
| Live session, first presentation on this node | Source allowance | 429, no reads |
| Session known live on this node | Its request budget | 429, no reads |
| Session that died while known live | Its request budget, until one resolution forgets it | 429, no reads |
| Machine, instance, developer and SCIM credentials | Source allowance, unchanged from #1040 | 429, no reads |
| Session artifact on MCP, which attaches no memory | Source allowance | 429, no reads |
| Public discovery and runtime status | Source allowance, unchanged | 429 |

Known sessions are still resolved on every admitted request, so revocation bites at
the next admitted request. A budget refusal now emits the #1034 `admission` signal.

`admission.Limiter` holds the memory, keyed on a hash of the session verifier. It is
per node, is inherited across generation handoff, and holds at most
`MaxTrackedSubjects` entries, dropping the least recently proven. A resolution that
finds the session dead forgets the entry, while a datastore fault leaves it, since the
session's budget still bounds retries. Losing an entry is safe: the session is charged
once and remembered again. Only a live session can add an entry, so an attacker without one
cannot grow or churn the memory.

## Accepted tradeoffs

- **Charging is no longer independent of liveness.** #1040 charged all grammatical
  artifacts alike. On an exhausted source, a known session is admitted and everything
  else gets a 429 with no resolver reads and only the bounded refusal write. Someone holding a list of leaked
  artifacts can therefore sort recently live ones at line rate, not 60 per minute.
  Blind guessing stays infeasible, and a holder can already test an artifact by using
  it, subject to the allowance. In HA a known request also skips the shared-store round
  trip, a timing signal to the same holder.
- **A leaked live session is bound by its session budget**, from any number of source
  addresses. That budget is per node, so N nodes allow N x 300 per minute and N x the
  burst, where the source allowance it no longer spends is installation-wide.
- **A first presentation can still be refused.** After a restart, on another HA node,
  after eviction, or straight after login, a session is charged to the source once, so a saturated
  source can delay it until a slot frees. A parallel cold load can charge a few slots
  before the first resolution completes.
- **Cross-model review was not run** for this amendment. The ADR records that, and
  `GOVERNANCE.md` makes it a disposition for the owner.

## Validation

Focused checks ran directly with two workers because the shared heavy-job queue is
paused:

```sh
go test -p 2 -parallel 2 ./internal/admission ./internal/operation ./internal/authz
go test -p 2 -parallel 2 ./internal/isolation -run '^(TestPreauthentication|TestRequestRefusal)'
go test -p 2 -parallel 2 ./internal/server ./internal/mcpserver
```

All passed on SQLite and disposable PostgreSQL. `live_session_outlasts_exhausted_source`
fails when the live-session hook is removed. Workspace sessions share the resolver but
have no case of their own, and a fault during resolution has no regression test. Hosted CI is the first full-suite run.

## Remaining work

1. Maintainer security review of the tradeoffs above, and the owner's disposition on
   cross-model review.
2. After deployment, confirm on an instance that a browser session passes 60 requests
   per minute without a 429, and that an unknown bearer still gets one past the allowance.
3. Fix source attribution behind chained proxies separately. Public discovery can
   still exhaust a shared bucket, which delays first presentations and status reads.
4. Session-bound OIDC and OAuth2 callbacks collapse an overload into a binding refusal
   (`internal/service/oidc_flow.go`, `internal/service/oauth2_flow.go`). This predates
   the change and needs its own fix, so a rate limit reads as a rate limit.
