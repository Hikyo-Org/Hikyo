# Dev integration CI handoff

Implementation branch: `ci/dev-integration-flow`, targeting `main`.
Source base: `de0d8866d050c6e729bde76e2960eb6a260f1e3a`.
Accepted behavior and operator commands: [branching guide](../development/branching.md).

## Result

Feature PRs target `dev` and require three bounded feedback lanes, signatures,
canonical DCO, fork vouching and trusted workflow approval. They avoid the full
main merge queue. A dev push runs the complete existing graph against its exact
merged commit. One active run completes while newer pushes replace the single
pending run. A frozen `release/` branch promotes through main's full single-entry
queue, including documentation-only promotions.

Nightlies are unchanged for users. They are built and signed from main, with the
same manifest format, signing identity (`nightly.yml@refs/heads/main`) and trust
policy. There is no bridge and no recovery signing. A new workflow,
`nightly-promote.yml`, lands the last green dev commit on main through the existing
merge queue, daily and on demand. `nightly.yml` verifies the complete queue proof of
the main commit it builds instead of running the full graph again. Its existing
identity step already skips a commit that is published, so nightly publishes only
when main has a newer commit.

Stable tags reuse complete validation for the identical landed source. The tag
retains one fresh floor measurement and docs check; repeated full source tests
and the standalone tag floor trigger are removed. Artifact builds, final-payload
scans, upgrade compatibility, native installation, Apple signing/notarization,
publication and runtime acceptance remain separate requirements. No persistent
Go build-cache bucket or paid cache capacity is added.

## Behavior to expect

- Main moves whenever a newer green dev commit exists. A stable release is a tag on
  any main commit, and every main commit passed the full queue.
- A failed promotion leaves main unchanged and means no new nightly. The workflow
  run fails for a missing dev proof, an unexpected state or a refused write. It also
  fails, printing the PR link and the check's title and summary in the job log, when
  an open promotion for the same commit has a refused or failed `ci-queue-required`
  check, or has not landed after 3 hours. A PR younger than that, such as one from a
  manual run, stays green while it waits. A failed queue run leaves the promotion PR
  open with a red `trusted-merge-ci` run; the next run queues it again for the same
  commit, or replaces it when dev has moved.
- A manual promotion takes about one queue run plus the nightly itself.
- `nightly-manual.yml` still publishes main's tip now without promoting anything.
- A promotion that changes `.github/` needs no second approval when it comes from
  `hikyo-nightly-release[bot]` on a branch the `nightly promotion branches require
  Hikyo release app` ruleset protects. Otherwise one independent maintainer must
  approve its exact head. This relies on the app key staying in the `nightly-release`
  environment.
- A promotion that cannot land fails the `nightly-promote` run, even when the run also
  re-arms or replaces it.
- A docs-only landing as the tip of main has only selective queue proof, so the
  nightly's validation fails for that commit until the next promotion lands or a full
  `ci-main` run covers it. Run
  `gh workflow run ci-main.yml --repo Hikyo-Org/Hikyo --ref main`, then request the
  nightly with `gh workflow run nightly-manual.yml --repo Hikyo-Org/Hikyo --ref main`.
- A dev tip that adds only merge commits to main, such as a backmerge, is not
  promoted: the queue's DCO check skips merges and refuses an empty range.
- No backmerge is needed after a clean promotion, and one could not pass admission
  for the same reason. Backmerge main into dev only when main has real commits dev
  lacks, such as CI definitions or a hotfix. If main conflicts with dev, admission
  refuses the promotion PR; resolve it with that reviewed backmerge, never by
  force-pushing.

## Evidence and limits

Local verification covers workflow contracts, source-proof refusal cases, dev
admission, activation and discovery fixtures, the promotion script against a stub
`gh`, and documentation content mappings. Final local and independent review results
are recorded in the delivery PR.

New hosted dev integration and promotion are not established by local fixtures.
Before activation, the required evidence classes are:

| Gate | Required proof |
| --- | --- |
| Source delivery | Signed/DCO PR and independent Standards and Spec reviews |
| Hosted merge | Exact-head feedback, current authority, full queue proof, actual landed commit |
| Dev activation | Installed main definitions, full exact-source proof, effective rules/ref readback |
| Promotion | A real `release/nightly-*` PR authored by the App passes admission, the queue and lands |
| Nightly | Complete queue proof for the promoted commit, then artifact, signature, native and runtime acceptance |

No live rulesets, branch refs, trust files or release pointers are changed by this
PR.

## Activation sequence

1. Land this PR through the existing main gate. Use the landed checkout and
   [local verification prerequisites](../../CONTRIBUTING.md#local-verification).
   Do not execute activation from an unlanded candidate.
2. Keep the app key only in the `nightly-release` environment, because a branch
   workflow could read an organization copy, and anyone with write access could then
   author promotions as the app. These are human steps in GitHub: generate a new private
   key in the App's settings, replace `NIGHTLY_RELEASE_APP_PRIVATE_KEY` in the
   environment, delete the old key from the App, then delete the organization-level
   secret of that name (organization Settings, Secrets and variables, Actions).
3. Give the nightly release App the pull request permission, before the next
   22:17 UTC run. The promotion mints its token first, so until this is done the
   scheduled run fails, even while `dev` does not exist. This is a human step in
   GitHub: open the organization's Settings, then Developer settings, GitHub Apps,
   the nightly release App, Permissions & events, Repository permissions, set
   Pull requests to Read and write, and save. Then accept the new permission on the
   installation (organization Settings, GitHub Apps, Configure; GitHub shows a
   request to review). To verify, run step 7: `actions/create-github-app-token` fails
   with HTTP 422 while the installation lacks the permission. If creating the branch
   fails with a message about workflow files, also grant Workflows: Read and write,
   and add `permission-workflows: write` to the token step. That also needs
   `nightly_promote_workflow_test.go` updated, because it forbids
   `permission-workflows` (line 106) and pins the token step to exactly six inputs
   (line 95).
4. Authenticate main's exact complete validation and activate the dev rules with
   `scripts/release/configure-dev.sh Hikyo-Org/Hikyo`. The helper verifies its own
   installed inputs, preserves stronger live review/App bindings and checks the
   final effective policy. It refuses divergent unprotected dev history.
   Run this straight after landing, because dev history from before its ruleset is
   not protected. Expect `0` from
   `gh api repos/Hikyo-Org/Hikyo/actions/workflows/ci-dev.yml/runs --jq .total_count`
   before running it. If it is not, stop and investigate.
5. Protect the promotion branches, from the landed checkout:

   ```sh
   gh api --method POST repos/Hikyo-Org/Hikyo/rulesets \
     --input release/repository/nightly-promotion-branches.json --jq .name
   gh api 'repos/Hikyo-Org/Hikyo/rules/branches/release%2Fnightly-20000101-0000000' \
     --jq '[.[].type] | sort | join(" ")'
   ```

   Expect the ruleset's name, then `creation update`. Do not use the full
   `configure-repository.sh` for this: it also resets CodeQL's languages.
6. Open feature PRs against dev. Confirm the `ci-dev-required` result is bound to
   the current PR/head and all three feedback lanes. Merge one accepted change
   and verify complete `ci-dev` execution for the resulting SHA. Confirm one
   active run completes and only the newest pending push remains during a burst.
   The CI speedup is live from this step.
7. Run the first promotion:

   ```sh
   gh workflow run nightly-promote.yml --repo Hikyo-Org/Hikyo --ref main
   ```

   Confirm the job summary names a `release/nightly-*` PR, that the PR shows
   `ci-queue-required` admission, that the queue lands it, and that the
   `nightly-release` run starts from the landing. The promotion adds only a merge
   commit to main, so no backmerge is needed afterwards.

## Pre-activation repairs

Review of the unlanded branch against a real promotion found these, all fixed:

- Every dev run died resolving `refs/heads/dev`, which checkout by SHA never creates.
- A wide promotion or backmerge could not fit the feedback budget.
- A CI definition changed on dev stalled dev proof with no way to promote out of it.
- A failed rerun of an older run could hide behind a newer run's success.
- Dependabot targets dev, so a human-opened promotion failed DCO.
- Squash or rebase merges on dev left unrepairable DCO failures.
- The main queue gate left a check on every dev PR head.
- A full dev run held all 20 runners for about 15 minutes, starving PR feedback.
- Nightly reuse removed the only automatic writer of main compile caches.
- The stable-workflow fixture's stub compared a shell-dependent expansion that never
  matched.

Not established before the first hosted dev run: the replay-modelled runner headroom,
whether creating `dev` succeeds under its new ruleset, the publisher's 60-second
budget on a backmerge touching many CI definitions, source proof's job inventory
against a real `ci-dev` run, and whether the main ruleset's extra approval for
unattributed changes affects an App-authored promotion. If creating `dev` fails
during activation, stop and report it. The helper's checks leave main untouched.

## Recovery and maintenance

Keep main as default and keep its mandatory queue. CI/publisher definitions land
through main first, followed by a reviewed main-to-dev merge. Fix or revert red
dev integration before a new promotion; do not force-reset shared branch history.
A promotion fails with no green proof while main's CI definitions are ahead of dev.

If identical landed-source proof is unavailable, dispatch full `ci-main` on the
selected main commit and obtain new exact-source proof. Selective/docs-only,
cancelled, missing-job or failed newer attempts never substitute for full proof.
The source-proof inventory is closed: changes to job names, matrix sizes,
required execution steps or reusable workflows must update its authenticated
inventory and fixtures with the canonical graph. Keep native/platform release
acceptance separate from source-validation receipts.
