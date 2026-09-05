# Default development startup regression

## Trigger and root cause

The actual command `hikyo server --dev` from a fresh working directory uses relative `hikyo-dev.db` and `hikyo-dev.rootkey` paths. `upgradeRequest` derived `.hikyo-development` by applying `filepath.EvalSymlinks` to the DB parent `.`. That function retained a relative path; `devupgrade.Open` correctly rejected it with `development custody parent must be an absolute canonical path`. Web end-to-end setup exposed this before any UI flow could run.

## Fix

`internal/app/upgrade_gate.go` first makes the existing DB parent absolute, then resolves parent symlinks and appends `.hikyo-development`. The custody directory itself is never symlink-resolved. Its existing descriptor-relative nofollow, owner, type, and permission checks remain unchanged. Explicit custody configuration and operator rotation code were not modified.

Question: loosen leaf custody validation or canonicalize the default caller path? Choice: canonicalize only the existing DB parent, preserving strict custody validation.

## Actual verification

`GOMAXPROCS=2 go test -p 1 ./internal/app -race -run 'TestActualDefaultDevCLIStartsAndRestarts|TestDefaultDevelopmentCustodyRefusesSymlink' -count=1 -v` passed: 2 tests, no skips; app package 39.810 seconds.

- Builds the actual CLI, runs `server --dev --listen localhost:0 --operational-listen 127.0.0.1:0` in a fresh temporary working directory with all inherited HIKYO configuration removed. `/readyz` returns 200 and the actual ledger is healthy under the development trust domain.
- Abrupt termination and a fresh second boot preserve root-key bytes and release generation.
- A symlink at the default `.hikyo-development` name remains rejected, leaves its external target unchanged, and creates no datastore.

Source was handed back to the parent for the UI CLI rebuild and prototype verification. No commit/push from this worker.

## Explicit development host commands

The next actual CLI step exposed a separate regression: `admin create` loaded only production configuration, so a newly started development instance could not create its first administrator. The shared host-command dispatcher now accepts exactly one leading group-level `--dev`: `hikyo admin --dev create ...`, `hikyo backup --dev export ...`, and `hikyo restore --dev status`. Later arguments remain verb data. No environment-based development switch or implicit datastore adoption was added.

Question: infer development from the database or require explicit selection? Choice: require the leading group flag. Help, README, and getting-started instructions now show the syntax; getting-started also uses the operational health port.

`GOMAXPROCS=2 go test -p 1 ./cmd/hikyo ./internal/app -race -run 'TestHostCommandDevelopmentOptInIsOnlyLeadingGroupFlag|TestActualDefaultDevCLIStartsAndRestarts' -count=1 -v` passed with zero skips: command package 2.300 seconds, app package 19.748 seconds. Six dispatcher cases cover all three groups with and without the prefix, including an unsupported `HIKYO_DEV` environment value and a later literal `--dev`. The actual CLI test proves production command refusal leaves the development ledger unchanged, explicit development admin creation publishes a private authority file, and restart preserves root bytes and generation.
