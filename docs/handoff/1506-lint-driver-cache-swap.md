# Handoff: #1506 lint driver Go cache replaces the release-snapshot cache

PR: https://github.com/Hikyo-Org/Hikyo/pull/1506 (hotfix to `main`; `ci.yml`
is source-proof policy, so a `main` into `dev` backmerge must follow).

## What shipped

- `release-snapshot` no longer restores or saves a Go build cache. Its
  2.71 GB entry cut GoReleaser by about 4 to 11 minutes per warm run (one
  warm sample, job 137619726503, against eight cold runs on 2026-11-20..22).
- `.github/workflows/ci-lint-cache.yml` (`cache-seed-lint`) is the single,
  trusted-main writer of `go-lintdriver-v2-...-<module hash>-<ISO week>`. It
  runs daily and on dispatch, compiles `prepare-lint-cache` plus
  `go test ./internal/lint` into an empty default cache, logs `du` and `df`,
  and saves once per exact key.
- `test_core_serial` (`ci.yml`) and `ci-advisory-invariants` restore it read
  only, by module prefix as `key` and runner prefix as `restore-keys`, right
  after the `go-test-v2` restore. Invariants now reserves disk first.
- Policy: `check-cache-policy_test.sh` and the Go workflow tests pin the seed,
  the readers, the weekly generation and the key's first `-vN-` segment.
  `requireReuseSeed` also limits seed workflows to `name`, `on`,
  `permissions`, `concurrency` and `jobs`. The snapshot upload check is now
  bounded by its job. Frozen coverage hashes are unchanged; the normalizer
  `normalizeReleaseSnapshotReuseRemoval` maps the new shape back.

## Evidence

- Lint compile cost without the seed: `prepare-lint-cache` 700 to 925 s and
  lint tests 135 to 375 s per run, on about 516 reader runs per 14 days.
- Local cold seed: 9.2 GB raw, 1243 MiB zstd on macOS; expect 1.6 to 1.8 GB
  compressed on Linux.
- Disk: the invariants run on bd159dc reported 86417768 KiB available
  against 20971520 KiB required.
- `go test -count=1 -p 2 ./scripts/ci/...`, the shell policy scripts under
  bash and sh, shellcheck and actionlint pass. Two independent reviews ran;
  their findings are fixed in this PR.

## Remaining work

1. After merge, dispatch the first seed:
   `gh workflow run ci-lint-cache.yml --repo Hikyo-Org/Hikyo --ref main`.
   Read its `du`/`df` lines and `gh cache list --repo Hikyo-Org/Hikyo --key go-lintdriver`.
2. Confirm on the next `test_core_serial` and invariants runs that the
   restore logs `Cache restored from key: go-lintdriver-v2-...` and that
   `Prepare lint compiler cache` drops well below 700 s.
3. Budget: usage was 9.89 GiB of 10 GB before the swap. The retired
   `go-release-snapshot-v2` entry is left to age out (owner choice). Expect
   up to two lint seeds during a weekly rotation, plus one per module change;
   check `gh cache list` after the first rotation and before adding any
   other cache family.
4. Open the `main` into `dev` backmerge so dev's `ci.yml` matches main.
