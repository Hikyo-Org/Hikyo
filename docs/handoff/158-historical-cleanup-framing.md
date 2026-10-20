# #158 historical cleanup: AWS CLI error framing

Updated 2026-10-21. This is a trusted-runner repair, not AWS acceptance.

## Problem and resulting behavior

The historical-secret helper rejected an otherwise exact AWS CLI
`DescribeSecret` `ResourceNotFoundException` when blank LF or CRLF lines framed
the diagnostic. It now accepts those blank lines while retaining full-message
matching, exit code 254, empty stdout, the exact operation/error, and bounded
output. Other diagnostics, nonblank extra output, foreign selectors, uncertain
deletes and expired authority still refuse. The one-delete intent is unchanged.

The regression failed before this change. All 38 cleanup/runner tests pass,
including 16 valid framing combinations and refusal cases for extra output.
Independent adversarial source review found no material findings.

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
needs explicit approval, a new bounded plan, and human deployment review.
