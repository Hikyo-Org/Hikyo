# Handoff: #336 adapter ceremony owner

Issue: https://github.com/Hikyo-Org/Hikyo/issues/336 (parent #326; audit finding
`F-S11-1`). Implementation base:
`3b342e5a657f8a9d75cfe5d973a89eb23d28f0a2`.

## Contract

- `requireAdapterCeremony` owns adapter-environment authorization, the
  fail-closed nil-auth check, intent construction, and reauthentication-window
  consumption for configure, sync, credential replacement, and adoption.
- Missing, expired, mismatched, and spent reauthentication windows map to
  `ErrReauthRequired` with the environment identifier retained in the message.
- Other ceremony-seam failures, including context cancellation and database
  errors, remain their original error instead of telling the caller to repeat a
  ceremony that cannot repair the failure.

## Coverage

- `TestAdapterCeremonyErrorClassification` injects a canceled ceremony
  consumer through the shared owner and proves cancellation is not reclassified.
  It also proves sync, credential replacement, and adoption still require
  reauthentication when no window exists.
- The four existing adapter ceremony, adoption, credential fencing, and manual
  sync regressions named by #336 pass unchanged.
- No wire, audit, schema, or generated artifact changed. Local verification:
  focused service tests, `go build ./...`, `go vet ./...`, and
  `go test -count=1 ./...` passed.
