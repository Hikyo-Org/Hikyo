# Handoff: #329 known environment completeness

Issue: https://github.com/Hikyo-Org/Hikyo/issues/329 (parent #326; audit
finding `F-S25-1`).

## Contract

- `knownEnv` recognizes the consumed server keys `HIKYO_NEW_ROOT_KEY_FILE` and
  `HIKYO_DIRECTORY_PROXY` and the consumed client keys `HIKYO_TOKEN` and
  `HIKYO_COMPOSE_DOCKER`.
- A repository-source guard scans non-test Go files under `internal/` and
  `cmd/` for literal `HIKYO_*` getter calls and reports every consumed key that
  is absent from `knownEnv`.
- `internal/operator/` remains excluded because the operator has a separate
  environment surface.
- Unknown `HIKYO_*` keys continue to warn; recognized consumed keys do not.
- Contract and migration decisions: no external contract or datastore
  migration changes.
- Generated outputs: none.

## Validation

```text
go test -count=1 ./internal/config -run
  'TestKnownEnvCoversEveryGetenv|TestConsumedServerEnvKeysDoNotWarn'  2 passed
go test -count=1 ./internal/config                               33 passed
go vet ./...                                                      passed
go test -count=1 ./...                     3,456 passed in 61 packages
```

## Review

Round 1 found that `LookupEnv` could evade the source guard and that indexed
test-pair assertions obscured their expected values. The guard now recognizes
both getter forms and the regression uses named expectations. Round 2 returned
Standards `CLEAN` and Spec `SOUND`.
