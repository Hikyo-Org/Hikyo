# #158 historical cleanup: AWS CLI error framing

Updated 2026-10-21. This is a trusted-runner repair, not AWS acceptance.

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
