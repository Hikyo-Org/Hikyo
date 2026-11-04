# Session-bound callback overload and faults

Repairs the defect listed as remaining work in PR #1145's handoff
(`docs/handoff/live-session-source-admission.md`). It began from `main`, independent
of #1143 and #1145. Both have since merged to `dev`, so it is rebased on `dev` and
targets it. The authorized endpoint is a reviewable PR.
Maintainer security review and the human merge gate remain required. No deployment
or production load exercise is claimed.

## Reproduced behavior

A test through the production router, service and authorizer, on SQLite and
PostgreSQL, against `main` at `b22eb0adb`. The same result held for a session-bound
OIDC and OAuth2 link callback, over the source allowance and over the session budget.

| Observation | Before | After |
| --- | --- | --- |
| Status | 401 `unauthenticated` | 429 `too_many_requests` |
| `Retry-After` | absent | present |
| Ceremony | consumed | not consumed |
| Audit | `auth.*_refused` with cause `binding` | no binding refusal. A source refusal emits the admission signal. A budget refusal on `main` emits none; #1145 adds one, covered below |
| Same code and state after the window | 401, ceremony spent | 200, ceremony completes |

The session budget case also reproduces on `main`: `AdmitOperation` charges it after
resolution, so #1145 is not needed to reach it.

## With #1145

#1145 lets a session this node has already proven live spend its request budget
instead of the source allowance, and refuses it on that budget before any read. The
scenarios above start a ceremony through the service, so only a later presentation in
them reaches that path. `TestCallbackAdmission` therefore also proves the session live
with a real request first:

| Session proven live on this node | Answer | Ceremony |
| --- | --- | --- |
| Over its budget, any well-formed state | 429 with `Retry-After`, no session or transaction read, one admission signal, not audited `binding` | Not consumed, retry completes |
| Source exhausted, budget left | Completes. A stranger presenting the same state on that source gets 429 | Consumed by the session's callback only |
| Lost from the node's memory during the exchange, still live | Completes, with no source charge | Consumed |
| Lost from the node's memory during the exchange, dead | 401. Reauth and establish also audit `binding`; a link has no such row | Consumed |

The last two rows needed a repair. A known session spends only its budget in Phase A,
so if the memory lost it during the exchange, Phase C charged the source for the first
time and could answer 429 for a ceremony already spent. `AdmitSessionPresentation` now
remembers the verifier that paid, for that request. A different artifact in the same
request still charges the source.

Removing the live-session memory from the server fails the refusal-before-read, outlast
and lost-session cases, and so does removing the paid verifier for the lost-session
cases. Treating overload as a credential refusal at the callback fails the over-limit
cases.

## Contract

A callback now resolves any presented credential first in its Phase A transaction,
before it reads the state (`resolveCallbackCaller`).

| Caller, at the callback | Answer | Ceremony |
| --- | --- | --- |
| Grammatical credential, over the source allowance or session budget, any well-formed state | 429 with `Retry-After`, no transaction read | Not consumed, retryable within its lifetime |
| Same, with the browser marker cookie | 303 to the done page with `error=too_many_requests`, and one admission signal | Not consumed |
| Different live session, or dead, unknown, absent or class-refused credential, session-bound state | Uniform 401, audited `binding` | Consumed |
| Datastore fault resolving the credential | 500 as JSON, also for a browser, since only overload is promoted to the redirect | Not consumed, retryable |
| No credential, browser-bound login, exhausted source | Completes as before, with no source or budget charge. `Enter` still applies | Consumed |

**Propagation alone would have opened an oracle.** Returning the error from the
binding check made an over-limit caller get 429 for a live session-bound state and
401 for every other state, and a test confirmed it. A stolen state could then be
probed for its purpose and lifecycle, which the uniformity comment in `OidcCallback`
and `ErrOverloaded`'s rule forbid. Charging on the credential, before the state, makes
the answer independent of the state. The test asserts identical status, body and
`Retry-After` across a live session-bound, a consumed, a browser-bound and an unknown
state, with no transaction reads. A malformed state keeps its earlier 401, because
its grammar check comes first and reveals nothing about a transaction.

`credentialRefused` keeps `ErrUnauthenticated` and `ErrNotFound` as refusals, since
`ErrNotFound` is the class-admission refusal of an enrolment-gated session or another
credential class. Everything else propagates.

## Sites changed

| Site | Reachable failure | Coverage |
| --- | --- | --- |
| OIDC and OAuth2 Phase A binding | Overload and fault | Handler tests, both flows |
| `completeReauth`, `completeFederatedEstablish` (Phase C) | Fault only: Phase A's admission covers the whole request, including when the live-session memory loses the session during the exchange | Handler tests with a fault during the exchange. The ceremony is already spent, so this is JSON 500, or a 303 with `error=internal` for a browser-started ceremony, and cannot be retried |
| `SAMLACS` initiator check | Fault only: `AuthenticateSessionByID` charges nothing | Service test with a broken session row |
| `RedeemCLIReauth` | Fault only, same reason | Service test with a broken session row |
| `StartCLIReauth`, `ApproveCLIReauth` | Overload and fault. The wire status was already right, but each wrote a false `unauthenticated` audit row | Service test with an overload hook |

The tests inject a fault by breaking one session row, which leaves the rest of the
datastore working. A cancelled request context could not stand in for it: the refusal
path a wrong caller would take then also fails, so the tests passed even with the
repair removed. With a broken row, removing each repair fails its test on SQLite;
PostgreSQL runs the same tests against the repaired code.

## Accepted tradeoffs

- **A credential on a browser-bound callback now counts. The maintainer accepted this
  on 2026-11-03.** A browser finishing a login spends one source or budget slot
  it did not spend before, and over the limit it gets the refusal instead of
  completing, where `main` completed it. That includes a dead `__Host-hikyo` cookie, the
  usual re-login after expiry, and the source allowance can be shared behind NAT. The
  ceremony stays unspent. For a source refusal the exposure is narrow, because the login
  page's own `AuthMethods` discovery spends the same allowance and is refused first. Uniformity
  requires this: exempting browser-bound states would make the answer depend on the
  state again.
- **Resolution moved into every callback that presents a credential**, including
  browser-bound ones, so a class-refused credential there now leaves one
  `auth.artifact_class_refused` row.
- **The browser leg has no `Retry-After`**, because it is the existing redirect to the
  done page. `OIDCDone` also renders `too_many_requests` as "Your identity provider
  refused this sign-in", so a browser user still does not see a rate limit. That page
  is out of this repair's scope and needs its own copy change.
- **A browser user cannot easily retry.** The callback URL is not in history and the
  marker cookie is cleared, so in practice they restart the flow. The ceremony is left
  unspent and expires on its own. A non-browser caller can retry directly.

The `caller.SessionID == ""` guard in the binding checks is fail-closed defense. No
test can reach it through a real path, because a session-bound row must carry an
initiating session.

## Validation

Run through the shared heavy-job queue on the rebased branch. PostgreSQL was a
disposable, task-owned container, since removed.

```sh
go test -p 2 -parallel 2 ./internal/isolation -count=1 -run '^(TestCallback|TestOIDC|TestPreauthentication|TestRequestRefusal)'
go test -p 2 -parallel 2 ./internal/isolation -count=1 -run '^(TestOAuth2|TestSAML|TestCredentialEstablish|TestReauth|TestCLI|TestFederated|TestClaim|TestBrowserSession)'
go test -p 2 -parallel 2 ./internal/admission ./internal/operation ./internal/authz ./internal/store/authn ./internal/boundary ./internal/service ./internal/server ./internal/mcpserver -count=1
go test -p 2 -parallel 2 ./internal/lint -count=1 -timeout 25m
scripts/ci/fast-feedback.sh
```

The isolation selection runs as two commands because together it can pass Go's
10-minute test binary limit on a loaded machine. The exact validated revisions are
recorded in the PR.

Not run locally: the rest of `./internal/isolation`, any full suite, and a real
browser or identity provider. Merging to `dev` starts the full `ci-dev` validation.

## Remaining work

1. Maintainer security review, especially the `AdmitSessionPresentation` change.
2. Change the `OIDCDone` copy so `too_many_requests` reads as a rate limit.
