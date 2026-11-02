# Development integration and release promotion

Feature pull requests target `dev`. `main` remains the default, trusted release
branch. This flow deliberately permits a temporarily broken integration branch;
only commits that passed the full merge queue may reach `main`.

## Required behavior

1. Feature PRs require bounded feedback, resolved review threads, cryptographic
   commit signatures, matching DCO sign-offs, fork vouching where applicable, and
   trusted workflow authority. They do not enter the full merge queue.
2. Every merge to `dev` requests the complete validation graph against its exact
   merged SHA. One active full run finishes; only the newest pending run is kept.
   Opening or updating an unrelated feature PR does not cancel integration proof.
3. A release candidate is a fixed branch created from a selected `dev` commit.
   Further movement of `dev` cannot mutate that branch or cancel its validation.
   Its PR to `main` enters the existing mandatory single-entry queue, which runs
   the full graph on the exact current-main integration result.
4. A complete, authenticated full validation for the identical landed commit may
   satisfy the source-validation requirement of main and stable-tag consumers.
   Docs-only or selective proof, missing jobs, failed or cancelled attempts,
   changed workflow authority and ambiguous provenance cannot qualify. A newer
   failed qualifying attempt cannot hide behind an older success.
5. Release-shaped builds, final-artifact vulnerability scans, upgrade compatibility,
   native installation, signing, notarization and publication retain their own
   acceptance requirements. Reused source proof does not establish those results.
6. The implementation operates within the existing GitHub storage limit. It does
   not add persistent Go compiler-cache buckets or paid cache capacity. Evidence
   receipts remain small; existing compiler-cache ownership is preserved.
7. Main hotfixes follow the full promotion gate and are merged back into `dev`
   before the next release is prepared.
8. PRs merge into `dev` with merge commits only. Squash and rebase rewrite
   signed-off commits, which promotion's DCO range check would refuse with no way
   to repair protected history.
9. Dev runs cap their widest matrices (race 7, isolation 2, web 4) so PR feedback
   still gets runners under the 20-runner limit. Replaying recent queue runs at
   those caps gives about 30 minutes instead of 27-29, with about 5-6 minutes
   instead of 15 of completely full runners. Merge-queue and main runs stay
   uncapped. These figures are a replay model until measured on hosted dev runs.

## Rollout

Land the workflow, checker and protection tooling on `main` before enabling the
new `dev` policy. Activation must prove the exact installed workflow bytes and
read back the resulting branch protection. It must preserve main's queue,
signatures, DCO, fork and workflow-approval boundaries. Existing default-branch
release workflows keep their privileged execution on trusted code.

Signed nightlies are unchanged for users: they are built and signed from `main`,
with the same manifest format, signing identity (`nightly.yml@refs/heads/main`)
and trust policy. [Nightlies](#nightlies) describes how dev reaches main for them.

Repository changes are delivered through a signed PR. Live branch-policy
activation follows the landed trusted implementation; creating this document or
a PR does not itself prove hosted CI, activation, release or publication.

## Feature work

Keep main as the repository default so privileged workflow definitions and policy
authority stay on the trusted branch. Select dev explicitly for feature PRs:

```sh
git fetch hikyo-upstream main dev
git switch -c feat/example-change hikyo-upstream/dev
# Make and validate the change; commit with git commit -S -s.
git push --set-upstream origin HEAD
gh pr create --repo Hikyo-Org/Hikyo --base dev
```

The required `ci-dev-required` check verifies all three feedback lanes, canonical
DCO, signatures, fork admission and current workflow authority. Main's queue is
not part of this path. Fix or revert red integration before promoting a release.
CI/publisher definitions land through main first, then a reviewed main-to-dev
backmerge supplies those approved definitions to integration. Dev admission refuses
a PR that changes them otherwise. Dev proof stays unavailable between that main
landing and a green run of the backmerge, so backmerge promptly. Do not force-reset
branches to synchronize them.

A red dev run opens one `Dev integration is red` issue listing the failed jobs
and the PRs merged since the last green commit. Later red runs update it, and the
first green run containing that commit closes it.

## Freeze and promote

Create a separate fixed branch from a verified dev commit. This example uses
`release/1.1.0`; select the actual version first. Keep the receipt temporary:

```sh
promotion_work=$(mktemp -d)
trap 'rm -rf "$promotion_work"' EXIT HUP INT TERM
go run ./scripts/ci/source-proof --repository Hikyo-Org/Hikyo \
  --mode dev --latest --output "$promotion_work/source.json"
promotion_commit=$(jq -er '.commit' "$promotion_work/source.json")
git fetch hikyo-upstream "$promotion_commit"
git switch -c release/1.1.0 "$promotion_commit"
git push --set-upstream origin HEAD
gh pr create --repo Hikyo-Org/Hikyo --base main --head release/1.1.0
rm -rf "$promotion_work"
trap - EXIT HUP INT TERM
```

Promote regularly. Main-scoped compile caches follow main, so dev runs and PR
feedback recompile more as dev drifts from it. A promotion is one full run, and
it does not have to be a tagged release. [Nightlies](#nightlies) promote green dev
commits daily.

Feedback lanes select nothing on a same-repository `release/` PR to main, because
its whole diff cannot fit their budget and the full queue graph runs next. They
do the same on a `main` to `dev` backmerge. Admission still checks DCO on both,
exempting only Dependabot's commits. Do not add the `ci:full` label to a promotion.

If `main` conflicts with `dev` while `ci-dev-gate.yml` differs, dev admission
accepts only an exact `main` head, so a conflict-fixing branch is refused. Promote
first, cutting the release branch from a green `ci-dev` commit, which resolves the
conflict on main. Then backmerge.

New dev pushes do not move that release branch. Branches under `release/` always
select the full main queue graph, including docs-only promotions. A changed
candidate or base needs fresh integration proof. Main retains its single-entry
queue and protected merge requirements. Backmerge main hotfixes before the next
promotion.

## Activate dev protection

After the implementation lands on main, from an authenticated administrator
checkout containing the landed code, run:

```sh
scripts/release/configure-dev.sh Hikyo-Org/Hikyo
```

The helper verifies installed immutable main definitions and complete source
proof, initializes missing dev history only at verified main, installs distinct
fast-admission rules and checks final live rules/ref readback. It preserves
stronger review/App bindings and refuses unprotected divergent dev history.
Main and tag protection remain intact. The admission controller refuses
unfinished bootstrap state.

## Nightlies

`nightly-promote.yml` lands the last fully green dev commit on main through the
existing merge queue. `nightly.yml` then builds and signs that main commit exactly
as it did before. There is no bridge, no second trust policy and no recovery
signing, and installed clients need no change.

Promotion runs daily at 22:17 UTC, well before the 02:00 UTC nightly, and on demand:

```sh
gh workflow run nightly-promote.yml --repo Hikyo-Org/Hikyo --ref main
```

It does the following:

1. Exits quietly while `dev` does not exist. The App token is minted before this
   check, so a missing App permission fails the run even then.
2. Selects the last dev commit with complete verified source proof. This fails the
   run if a CI definition on main has not been backmerged into dev yet.
3. Exits when main already contains that commit, or when the commit adds only merge
   commits to main, such as a backmerge: the queue's DCO check skips merges and
   refuses an empty range. Main has merge commits dev lacks, so the histories
   ordinarily diverge and this is a real check.
4. Closes an open `release/nightly-*` PR for an older commit and deletes its branch.
   A PR for the same commit is queued again instead, which retries a failed queue run.
   It then fails the run, printing the PR link and the check's title and summary in
   the job log, if that PR's `ci-queue-required` check was refused or failed, or if it
   has not landed after 3 hours. A younger PR, such as one from a manual run, stays green.
5. Creates `release/nightly-<date>-<short commit>` at that commit, opens a PR to
   main as the nightly App and queues it. The queue runs the full graph on the exact
   current-base result.

When the promotion merges, a second job in the same workflow starts the nightly at
once and deletes the branch. The 02:00 run covers any miss. `nightly.yml` verifies
the complete queue proof of its own main commit instead of running the graph a
second time, and its existing identity step skips a commit that is already
published, so a day without a newer main commit publishes nothing.

What this means for main:

- Main moves whenever a newer green dev commit exists. A stable release is a tag on
  any main commit, and every main commit passed the full queue.
- A failed promotion leaves main unchanged. There is then no new nightly: a failed
  queue run shows as a red `trusted-merge-ci` run on the promotion PR, which stays
  open. The next promotion queues it again for the same commit, or replaces it.
- A manual promotion takes about one queue run plus the nightly itself.
- `nightly-manual.yml` still publishes main's tip now without promoting anything.
- If the tip of main is a docs-only landing, the queue produced only selective
  proof for it and the nightly's validation fails until the next promotion lands or a
  full `ci-main` run covers it. Obtain full proof with
  `gh workflow run ci-main.yml --repo Hikyo-Org/Hikyo --ref main`, then request the
  nightly with `nightly-manual.yml`.

A promotion that changes `.github/` needs no second approval when its head is a green
tip of protected dev, because such a tip contains only what admitted PRs merged, and
each of those passed the same check. The exemption applies only to the
`hikyo-nightly-release[bot]` user, a canonical `release/nightly-*` head into main, a
commit with its own successful `ci-dev` push run, and a dev ruleset that is still
active with no bypass actors. If any condition fails, one independent maintainer must
approve the promotion PR's exact head, as for any workflow edit.

A promotion that cannot land fails the `nightly-promote` run: a refused admission, or
a PR over three hours old, even when the run also re-arms or replaces it.

When main conflicts with dev, admission refuses the promotion PR for merge
conflicts. Resolve it with a reviewed backmerge of main into dev (a PR from `main`
to `dev`), not by force-pushing either branch. The next promotion then proceeds.
Backmerge only when main has real commits dev lacks, such as CI definitions or a
hotfix. A backmerge after a clean promotion is unnecessary and cannot pass admission,
because it adds only a merge commit.

A weekly `ci-main` run on main, scheduled clear of nightly's start, keeps the main-scoped compile caches
written, because nightlies reuse proof instead of running the full graph. It
is qualifying evidence: a green run can prove main's tip, while a failed one, or one
still running, refuses proof for that commit until a newer run passes or it completes.

Stable tags consume complete proof for the identical landed main commit and
revalidate it during construction. Fresh tag-bound floor/docs checks and actual
artifact acceptance remain required. Missing or stale source proof blocks
publication; full manual `ci-main` is the recovery route on selected main.
Source proof does not establish signing, publication or native acceptance.
