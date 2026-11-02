# Session-bound callback overload and faults

Repairs the defect listed as remaining work in PR #1145's handoff
(`docs/handoff/live-session-source-admission.md`). The change branches from `main`
and depends on neither #1143 nor #1145. The authorized endpoint is a reviewable PR.
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
| Audit | `auth.*_refused` with cause `binding` | no binding refusal. A source refusal emits the admission signal; a budget refusal on `main` emits no signal, and #1145 adds one |
| Same code and state after the window | 401, ceremony spent | 200, ceremony completes |

The session budget case also reproduces on `main`: `AdmitOperation` charges it after
resolution, so #1145 is not needed to reach it.

## Contract

A callback now resolves any presented credential first in its Phase A transaction,
before it reads the state (`resolveCallbackCaller`).

| Caller, at the callback | Answer | Ceremony |
| --- | --- | --- |
| Grammatical credential, over the source allowance or session budget, any well-formed state | 429 with `Retry-After`, no transaction read | Not consumed, retryable within its lifetime |
| Same, with the browser marker cookie | 303 to the done page with `error=too_many_requests` | Not consumed |
| Different live session, or dead, unknown, absent or class-refused credential, session-bound state | Uniform 401, audited `binding` | Consumed |
| Datastore fault resolving the credential | 500 as JSON, also for a browser, since only overload is promoted to the redirect | Not consumed, retryable |
| No credential, browser-bound login, exhausted source | Completes as before, no charge | Consumed |

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
| `completeReauth`, `completeFederatedEstablish` (Phase C) | Fault only: the request's admission receipt is memoized in Phase A | Handler tests with a fault during the exchange. The ceremony is already spent, so this is 500 and cannot be retried |
| `SAMLACS` initiator check | Fault only: `AuthenticateSessionByID` charges nothing | Service test with a broken session row |
| `RedeemCLIReauth` | Fault only, same reason | Service test with a broken session row |
| `StartCLIReauth`, `ApproveCLIReauth` | Overload and fault. The wire status was already right, but each wrote a false `unauthenticated` audit row | Service test with an overload hook |

The tests inject a fault by breaking one session row, which leaves the rest of the
datastore working. A cancelled request context could not stand in for it: the refusal
path a wrong caller would take then also fails, so the tests passed even with the
repair removed. With a broken row, removing each repair fails its test on SQLite;
PostgreSQL runs the same tests against the repaired code.

## Accepted tradeoffs

- **A credential on a browser-bound callback now counts. This needs an explicit
  maintainer decision.** A browser finishing a login spends one source or budget slot
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

The shared heavy-job queue was unavailable when validation began, so the first runs
went directly with two workers. It returned before the final candidate. PostgreSQL was
a disposable, task-owned container, since removed.

```sh
go test -p 2 -parallel 2 ./internal/isolation -count=1 -run '^(TestCallback|TestOIDC|TestPreauthentication|TestRequestRefusal)'
go test -p 2 -parallel 2 ./internal/isolation -count=1 -run '^(TestOAuth2|TestSAML|TestCredentialEstablish|TestReauth|TestCLI|TestFederated|TestClaim|TestBrowserSession)'
go test -p 2 -parallel 2 ./internal/lint ./internal/admission ./internal/operation ./internal/authz ./internal/store/authn ./internal/boundary ./internal/service ./internal/server ./internal/mcpserver -count=1
scripts/ci/fast-feedback.sh
```

The isolation selection runs as two commands because together it can pass Go's
10-minute test binary limit on a loaded machine. One combined run hit that limit
during steady progress, with the running test 6 seconds old. The exact validated
revision is recorded in the PR.

`./internal/lint` did not finish on the final candidate. It passed on an earlier
revision that differs only in tests, one error wrap and a comment, then ran out of
time three times on a heavily loaded machine, at 10 minutes and at 30 minutes, while
still progressing through its repository package loads. Hosted CI is its proof.

Not run locally: the rest of `./internal/isolation`, any full suite, and a real
browser or identity provider. Hosted CI is the first full-suite run.

## Remaining work

1. Maintainer security review, especially the tradeoffs above.
2. Change the `OIDCDone` copy so `too_many_requests` reads as a rate limit.
3. If #1145 merges first, rebase and rerun `TestCallbackAdmission`. A trial merge with
   #1145 at `4617854a` merged cleanly, and `TestCallbackAdmission` and
   `TestPreauthentication` passed on both engines, including the budget refusal path.
