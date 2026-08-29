# Issue #511: panic-recovery middleware honouring the uniform internal-error contract

Issue: https://github.com/Hikyo-Org/Hikyo/issues/511 (ox-alpha audit, P1). Base:
`f32bc23096f66e05f2ac8f44a8efd3816768d426`.

## Contract

- `a.recoverPanics` (`internal/server/recovery.go`) leads `a.Middleware()`
  (`internal/server/api.go`), outermost of the API stack, so an invariant panic
  anywhere below it — contract validation included — becomes the uniform
  `internal` refusal instead of a dropped connection.
- The recovered answer is rendered by the same writer a fault uses today
  (`writeError` + `wirePolicyForCode(apigen.ErrorCodeInternal)`), so the body is
  byte-identical to every other fault: fixed "internal error" message,
  `redactDetail` policy, no panic text, no stack on the wire.
- The panic value and stack land in the slog pipeline via
  `a.Log.ErrorContext` (JSON in production), not the std logger net/http
  recovers into. The op is named method + path because the contract operation
  is not yet in context that far out.
- `securityHeaders` and `workspaceCORS` are router-level, one layer up from the
  recovery leg, so recovered refusals keep the static security baseline and
  cross-origin readability.
- A panic after the response was committed (e.g. a fault mid-advisory-stream)
  leaves the committed writer alone — no second WriteHeader, no appended body —
  and logs `handler panic after response committed`. `recoveryWriter` tracks
  commitment and forwards `Flush`, so SSE streaming in `revisions.go` keeps
  working through the wrapped writer.
- No new imports beyond stdlib (`runtime/debug`) plus the existing `apigen`.

## Tests

`internal/server/recovery_test.go`:

- `TestRecoveryRendersUniformInternalBody` — injected panic behind the full
  middleware stack: 500, body byte-identical to the strict server's fault leg
  (asserted via `renderContractError`), no detail member, panic value + stack
  present in the slog JSON pipeline, and a subsequent normal request still
  answers 200.
- `TestRecoveryCoversTheLiveRouter` — a nil-service invariant panic through
  `NewPublic`: 500, security baseline intact (`nosniff`, CSP), body
  byte-identical to the fault leg, panic logged with method + path.
- `TestRecoveryLeavesACommittedResponseAlone` — a panic after
  `WriteHeader(418)`: status and committed bytes unchanged.

## Verification

- `go build ./...`, `go vet ./internal/server/` clean.
- `go test ./...` green locally, full uncached run, against the PR head.
