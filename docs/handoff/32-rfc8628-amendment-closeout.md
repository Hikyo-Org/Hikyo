# #32: RFC 8628 polling amendment closeout

This handoff records the formal review disposition for the 2026-10-15
[#612 polling amendment](https://github.com/Hikyo-Org/Hikyo/issues/32#issuecomment-6197758465).
It becomes operative when the accompanying documentation PR merges. Shared
issue #32 remains open for the separate #930 and #805 amendments.

## Owner decision and review scope

The [original owner ruling](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-6197746788)
selected RFC 8628 compliance: start at five seconds, add five seconds on every
`slow_down`, retain the increased interval for subsequent requests, and impose
no thirty-second cap. Exponential backoff remains the separate connection-timeout
recommendation. That policy is unchanged.

On 2026-10-16, the owner explicitly
[accepted the scoped review exception](https://github.com/Hikyo-Org/Hikyo/issues/32#issuecomment-6220244515):
Astra high verification plus independent GPT-6.1 Sol high adversarial review
qualifies for this #612 amendment only under the
[locked-ADR amendment procedure](../adr/oss-mechanics.md).
Cross-provider review was unavailable and remains historically skipped, not
CLEAN. The earlier [#612 handoff](./612-cli-login-transports.md) and its review
limitations remain intact. Neither general merge authorization nor the separate
#930/#805 approvals supplied this exception.

The original operational specification was already locked. This closeout does
not reopen its original delegation list, change polling behavior, or approve
other amendments. The dated closeout note in [ops-spec](../adr/ops-spec.md)
preserves the complete historical body and the existing owner-approved banner.

## Source and delivery evidence

Astra high verification and independent Sol 6.1 high source review examined
`c4c626a105d0139fcc8a6f5444e585e7a4e0a761`. Both found no polling discrepancy.
The independent review identified the missing scoped review decision and the
need to keep shared #32 open for the other amendments. The explicit decision
above resolves the former; the closeout sequencing below preserves the latter.

| Requirement | Inspected evidence | Result |
| --- | --- | --- |
| Owning issue reopened | #32 comment 6197758465 | Explicit #612 polling scope |
| Normative protocol | `docs/adr/api-cli-surface.md`, login transports; [RFC 8628 section 3.5](https://www.rfc-editor.org/rfc/rfc8628.html#section-3.5) | Cumulative five-second increases; timeout backoff separate |
| Historical text preserved | `docs/adr/ops-spec.md`, first banner and section 4 login row | Banner supersedes capped doubling without rewriting history |
| CLI polling | `internal/cli/login_transport.go`, `pollCLIDevice` | Five-second increases; timeout-only doubling; throttling cannot lower the retained interval |
| Durable server pacing | `internal/service/cli_login.go`, `PollCLIDevice` | Serialized update persists increased interval and last-poll time |
| Distinct polling regressions | `internal/cli/login_transport_test.go`, `TestDevicePollingSlowDownTimeoutAndThrottleAreDistinct` | Existing assertions reach forty seconds and distinguish timeouts and throttling |
| Both-engine lifecycle | `internal/isolation/cli_login_e2e_test.go`, `TestCLILoginDevice` and `TestCLILoginExpiryAndConcurrentDevice` | Existing assertions cover repeated slowdown, redemption and concurrent durable increments |
| Wire admission | `internal/isolation/cli_login_wire_test.go`, `TestCLILoginDeviceWireAdmission` | Existing assertions retain a pending transaction across throttling and eventual redemption |

These are source-verification results, not fresh test executions. Historical
CLI, SQLite/PostgreSQL and desktop/mobile validation remains recorded in the
[#612 implementation handoff](./612-cli-login-transports.md), with its original
limitations. No runtime code or tests change in this documentation closeout.

[PR #924](https://github.com/Hikyo-Org/Hikyo/pull/924) reports merged at
`2026-10-15T22:03:28Z`, merge
`ad4c7b124addae47d79bc715f70d51ba326cf271`. Its ancestry in the reviewed main
revision was verified. The ops-spec banner, CLI poller and polling tests,
service poller, and the two named isolation test files have no changes between
that merge and the reviewed revision. All five PR review threads are resolved.
Some old PR-head status contexts still showed queued when inspected, so this
handoff does not claim every exposed old check is green. Actual merge state and
ancestry establish delivery; queue admission alone would not.

## Closeout sequencing

The documentation PR records this scoped exception and receives its own final
diff review and required checks. Signed DCO commits, protected queue validation,
and a verified main merge remain delivery requirements. The public PR and the
subsequent #32 comment record its final head, checks and merge SHA; those results
are not inferred from this handoff or from PR #924's earlier merge.

After the closeout PR is verified merged, record that #612's formal polling
amendment is complete and notify the #589 owner. That discharges the #612
amendment dependency without closing shared #32. The #930 owner retains backup
retention/encryption work, and the #805 owner retains webhook operational values.
Their own reviewed and merged acceptance is required before final #32 closure.
No release, deployment, or fresh runtime acceptance is claimed here.
