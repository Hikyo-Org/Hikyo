# #158 trusted runner: CLI framing and Go security patch

Updated 2026-10-21. This covers the trusted-runner framing repair and a Go
security pin update, not AWS acceptance.

## Merge-queue security blocker and Go pin

Overnight merge-queue
[run 39530844704](https://github.com/Hikyo-Org/Hikyo/actions/runs/39530844704)
failed both core and UI binary vulnerability checks on
[`GO-2026-6809`](https://pkg.go.dev/vuln/GO-2026-6809), affecting
`crypto/x509.Certificate.Verify`. The official advisory was published at
`2026-10-21T00:27:29Z`, after the earlier CI, and identifies Go 1.27.3 as the fix
within the Go 1.27 line. The canonical `go.mod` pin and the three current
installation prerequisites now require 1.27.3. Module dependencies, `go.sum`,
synthetic version fixtures and AWS runtime behavior are unchanged.

Local validation selected Go 1.27.3, passed the Go-patch contract and four
focused CI compiler-policy tests, built the real SPA and both core/UI
binaries, verified both compiler metadata records, and passed both binary
`govulncheck` scans on Darwin arm64. Exact-head remote full CI and protected
merge-queue validation remain required. The recorded 39 cleanup/runner test
result below covers the unchanged Python repair and predates this pin update.
The frozen AWS candidate `391c4b053661e4d84569a304bafa8bbb521b7fc2` still pins
Go 1.27.2 and must incorporate the reviewed security patch before another
fixture run.

The owner's banked merge approval applies only to
`9df4d38529e0ddde1530bc5114f123e1887c8488`. The revised candidate needs a new
exact-head human merge decision after validation and CI. AWS provisioning,
dispatch and human deployment review remain separate gates.

## Problem and resulting behavior

The historical-secret helper rejected an otherwise exact AWS CLI
`DescribeSecret` `ResourceNotFoundException` when an LF or CRLF framed
the diagnostic. It now accepts at most one such line ending on each side while
retaining full-message matching, exit code 254, empty stdout, the exact operation/error, and bounded
output. Other diagnostics, nonblank extra output, foreign selectors, uncertain
deletes and expired authority still refuse. The one-delete intent is unchanged.

The regression failed before this change. All 39 cleanup/runner tests pass,
including 16 valid framing combinations, an official-formatter output fixture,
and refusal cases for repeated blank lines or extra output.
Independent adversarial source review of the original repair found no material
findings. The review follow-up narrows framing and supplies the evidence below.

## Official CLI framing evidence

AWS CLI 2.38.1's
[`write_error`](https://github.com/aws/aws-cli/blob/10b7b71c678dd01942a88071be7cddb683daa802/awscli/errorformat.py#L37-L41)
writes one leading and one trailing newline. Its
[`ClientErrorHandler`](https://github.com/aws/aws-cli/blob/10b7b71c678dd01942a88071be7cddb683daa802/awscli/errorhandler.py#L255-L277)
uses that formatter and returns the client-error exit code 254. Offline
execution of that official handler with a synthetic `DescribeSecret`
`ResourceNotFoundException` confirmed the prefixed diagnostic, empty stdout,
and LF/CRLF stream rendering in both legacy and enhanced modes, with and
without the zero-retry suffix. No AWS client/session, credentials or provider
call was used. A regression fixture preserves the rendered bytes. This is
CLI-format evidence, not a claim about the failed hosted run's unknown stderr.

## Actual provider evidence and remaining gate

[Run 39495071346](https://github.com/Hikyo-Org/Hikyo/actions/runs/39495071346)
failed during historical cleanup before the real suite began. Its sanitized
artifact recorded zero new creations and no test results. That artifact does
not establish this parsing defect as the actual failure cause.

Subsequent native metadata independently confirmed the exact old secret absent.
The eight fixture roles, OIDC provider and unused boundary were retired. The
latest key is scheduled for deletion at 2026-10-27T21:42:12.444Z; both older
schedules were preserved. The setup user/MFA and protected GitHub environment
remain; consumed execution variables were cleared. No run was retried.

Issue #158 remains open. All 112 conservative lifetime reservations remain
charged. Merging this repair launches no AWS job and grants no additional
fixture, creation allowance or resource access. A further real-provider run
needs a separately approved bounded plan and human deployment review. The
owner has now approved one such additional fixture (at most 32 creations,
reaching 144 lifetime); no new reservation, provisioning or dispatch has
occurred. Mandatory CI and the human merge/deployment gates remain separate.
