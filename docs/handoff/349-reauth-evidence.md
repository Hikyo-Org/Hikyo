# Handoff: #349 reauthentication evidence model

Issue: https://github.com/Hikyo-Org/Hikyo/issues/349 (parent #326; audit finding
`F-S07-2`, after merged prerequisite #335).

## Contract

- `ReauthEvidence` is a tagged three-state value: local-authority exemption,
  TOTP evidence `(row id, row version, step)`, or password evidence `(row
  version, credential epoch)`. Its zero value fails closed.
- TOTP consumption retains the row-and-step CAS and the named
  `ErrTOTPCodeAlreadyUsed` replay outcome.
- Password consumption re-reads both the credential row and live instance
  epoch inside the caller's write transaction. A password row replaced after
  verification is refused with uniform `domain.ErrUnauthenticated`.
- `GenerateRecoveryCodes` authenticates the bearer before the local-authority
  exemption can apply, then uses the shared verification/consumption owner.
  Missing or wrong proof remains a uniform unauthenticated refusal.
- `proofSelection` and all wire/audit values are unchanged. Generated outputs:
  none.

## Coverage

- A focused service test pins zero-value evidence refusal.
- SQLite and PostgreSQL isolation fixtures pin password replacement between
  verification and consumption.
- SQLite and PostgreSQL recovery fixtures pin missing-proof mapping, recovery
  code single use, and named TOTP-step replay during batch regeneration.

## Validation

```text
gofmt -w <changed Go files>                                    clean
go test -count=1 ./internal/service ./internal/server          490 passed
go test -count=1 ./internal/isolation -run <issue-349 set>      6 passed
go test -count=1 ./...                         3,533 passed / 61 packages
go vet ./...                                                   passed
git diff --check                                               clean
```
