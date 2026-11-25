# CI coverage cuts (2026-11-25)

Status: **owner-approved on 2026-11-25.** Cut 1 is implemented by the
`hotfix/desktop-to-nightly` pull request. Cuts 2 and 3 are **pending**; each lands in
its own pull request and this file is updated when it does. These are deliberate
reductions of what blocks dev and the merge queue. They are not fixes, and each
names the coverage given up.

Evidence: the CI speed investigation of 2026-11-25 (dev and queue timings for the
last two weeks). The figures below were measured on real runs unless marked.

## Owner decisions

All three decided on 2026-11-25.

1. **Desktop validation moves to nightly (implemented).** The `desktop` job
   leaves `ci.yml`, so it is no longer a `ci-dev` or merge-queue gate, and the
   `desktop` registry domain and plan key go with it. `nightly-desktop.yml` runs the
   same steps on macOS against the dev and main heads. A failure turns that run red and
   opens or updates one tracking issue, `ci: nightly desktop validation failing`; the
   next green night closes it.
   - *Why:* the `macos-15` job was never acquired in 5 of 21 `ci-dev` attempts (24%).
     GitHub cancelled each after 15 to 46 minutes. Run 45653285809 needed a third
     attempt, so dev stayed red. When a runner was acquired it started within 8
     minutes. The merge queue's 17 `macos-latest` `scan-xplat` jobs had no failure.
   - *Coverage given up:* a desktop regression is found at the next nightly, not
     before a merge. The module boundary check (`check-desktop-deps.sh`) also moves
     there, and its classifier-coverage step is dropped because no domain remains to
     classify. Root changes that desktop imports no longer select any `ci.yml` job
     for desktop.
   - *Kept:* the `desktop-compile` fast-feedback check (Ubuntu, per PR head, only
     where `desktop/go.mod` exists) and the advisory `ci-advisory-golangci-desktop`
     lint. Neither is on the dev or queue path.
   - *Not changed:* promotion. `nightly-promote` and `nightly-release` read none of
     the nightly desktop results, so a red night does not block either. See
     [CI guide](../development/ci.md#desktop-shell-macos).

2. **Drop the duplicate non-race `internal/app` and `test_core` runs (pending).**
   `test_core_serial` and the non-race test step of `test_core` rerun packages that
   the race shards already cover (about 17 and 13 runner-minutes; the serial step's
   maximum was 33 minutes). *Coverage given up:* the non-race build and its timing
   signal. Its own pull request follows.

3. **Move floor-bench off the merge queue (pending).** It becomes a post-merge or
   nightly check. The queue still requires benchmark smoke. *Coverage given up:* a
   floor regression is found after merge. Its own pull request follows.

## Landing order

CI policy files must match between main and dev for source proof, so each cut lands
on main first (`hotfix/` branch) and reaches dev through the main-to-dev backmerge.
Until that backmerge merges, dev's policy bytes differ from main's and dev source
proof, and with it `nightly-promote`, cannot pass. The `ci-required` gate carries a
removal shim for cut 1 that adapts only a base which still registers `desktop`;
delete it with its `TestDesktopRemovalTrustedBaseMigration` cases once the protected
base registry lacks desktop.

## Review record

Cross-provider review: not run. This records owner decisions on measured evidence;
the implementation pull request carries its own review.
