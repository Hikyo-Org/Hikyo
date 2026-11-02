# Development integration and release promotion

Feature pull requests target `dev`. `main` remains the default, trusted release
branch. This flow deliberately permits a temporarily broken integration branch;
only fully validated release candidates may reach `main`.

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

Signed nightlies build from the last fully green `dev` commit, and only when it is
newer than the previous nightly. They need the recovery-authorized v2 policy, which
is the version of the nightly trust policy file format
(`hikyo.dev/nightly-policy/v2`), not a Hikyo product version; Hikyo stays on 1.x.
Until it is installed, ordinary nightlies do not publish and scheduled runs fail at
preflight. The order is: land, run the one-time bridge and upgrade clients, close the
blocker in the [handoff](../handoff/dev-integration-ci.md), then recovery-sign v2. The trusted
publisher continues running on `main`, while `nightly-build.yml` builds the source
in a run on `dev`, so build tooling from dev gets no main-scoped cache access.
The publisher authenticates that run and pins its artifact; the successor nightly policy and manifest bind the
publisher commit separately from the dev source and its exact validation receipt.
The receipt is included in the signed artifact inventory. Publication repeats
live source-proof verification before signing and refuses a changed run/attempt.

Legacy v1 manifests retain their original certificate/source equality rule.
Existing clients receive the new reader from the one-time main bridge nightly,
which is required exactly once before the v2 policy is signed: they cannot read a
v2 manifest, so skipping it strands them. Ordinary nightlies cannot fall back to main, and a broken dev
source fails before publication.

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
it does not have to be a tagged release.

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

## Migrate signed nightlies

These are release/trust actions, separate from merging implementation:

1. While the authorized policy is v1, publish the one-time main bridge nightly
   exactly once, before signing v2. It is required. It is the only nightly built
   from main, because a v1 manifest requires
   its source commit to equal the publisher commit, and it delivers the reader to
   clients. Require normal Apple, installation, compatibility and runtime
   acceptance, and upgrade clients to this reader before v2 adoption; old clients
   cannot consume v2 dev manifests. The bridge needs complete proof for main's tip.
   If main moves after the request starts, the build fails closed on the changed
   tip, and a rerun cannot help because it keeps the old `github.sha`; send a
   fresh request once the new tip has proof.
   After a docs-only landing, obtain it with
   `gh workflow run ci-main.yml --repo Hikyo-Org/Hikyo --ref main`. The schedule
   never runs the bridge, so request it explicitly. A further request after main
   moves would publish another main nightly, so request it once:

   ```sh
   gh workflow run nightly-manual.yml --repo Hikyo-Org/Hikyo --ref main \
     -f source_mode=main-bridge
   ```
2. Set `PROPOSAL_DIRECTORY` to a new output directory outside the checkout and
   prepare exact unsigned successor documents from authenticated current trust:

   ```sh
   go run ./scripts/release/stable nightly-policy-proposal --trust release/trust \
     --out "$PROPOSAL_DIRECTORY"
   ```

   The proposal contains nightly policy, stable policy, metadata, catalog and
   their digest summary. It retains historical v1 authorization, release and
   primary history, bridge authority and rollback floors. No signatures or
   installed trust bytes are changed.
3. Independently review those exact bytes, then use established recovery-signing
   custody to sign `stable-policy.json`, `metadata.json` and `catalog.json`.
   Keep v1 policy authorization for historical nightlies. Verify the complete
   signed snapshot against the retained floor before installing it through the
   existing trust ceremony. The proposal uses the existing recovery `rotation`
   event, advancing metadata/catalog sequences without a new release claim.
4. After reviewed trust lands and its public readback verifies, require:

   ```sh
   go run ./scripts/release/nightly preflight --require-dev-policy
   ```

   Scheduled nightlies, manual requests and chain restarts then build the last
   completely green dev commit, if it is newer than the previous nightly. Main
   bridge requests are refused after v2 activation. Backmerge main's installed
   trust into dev straight after installing it, because scheduled runs start hours
   after their 02:00 UTC cron: a pre-backmerge dev commit
   fails closed in the build job once a v2 nightly exists, because dev's old
   catalog does not authorize the v2 policy. Clients are unaffected, since they
   read the signed snapshot from main.

A weekly `ci-main` run on main, scheduled clear of nightly's start, keeps the main-scoped compile caches
written, because nightlies now reuse proof instead of running the full graph. It
is qualifying evidence: a green run can prove main's tip, and a failed one refuses
proof for that commit until a newer run passes.

Stable tags consume complete proof for the identical landed main commit and
revalidate it during construction. Fresh tag-bound floor/docs checks and actual
artifact acceptance remain required. Missing or stale source proof blocks
publication; full manual `ci-main` is the recovery route on selected main.
Source proof does not establish signing, publication or native acceptance.
