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

Under the v2 nightly policy, nightlies select the last completely green dev
source; under v1 they keep publishing fully proven main. Trusted main tooling
authenticates the producer, exact attempt, complete job inventory, immutable
workflow definitions and ancestry, then repeats that verification before signing.
The v2 nightly manifest binds distinct source and publisher commits and includes
the exact validation receipt in its signed inventory. Legacy v1 verification
retains source/certificate equality and rejects successor-only fields.
The first-use installer, container verifier, Apple receipt/recovery state and
post-publication security campaign all preserve those separate identities.

Stable tags reuse complete validation for the identical landed source. The tag
retains one fresh floor measurement and docs check; repeated full source tests
and the standalone tag floor trigger are removed. Artifact builds, final-payload
scans, upgrade compatibility, native installation, Apple signing/notarization,
publication and runtime acceptance remain separate requirements. No persistent
Go build-cache bucket or paid cache capacity is added.

## Evidence and limits

Local verification covers workflow contracts, real-signature v1/v2 trust and
revocation fixtures, unsigned policy-rotation conservation, source-proof refusal
cases, dev admission, activation/discovery fixtures, safe payload restoration
and documentation content mappings. Final local and independent review results
are recorded in the delivery PR.

New hosted dev integration and publication are not established by local fixtures.
Before activation, the required evidence classes are:

| Gate | Required proof |
| --- | --- |
| Source delivery | Signed/DCO PR and independent Standards and Spec reviews |
| Hosted merge | Exact-head feedback, current authority, full queue proof, actual landed commit |
| Dev activation | Installed main definitions, full exact-source proof, effective rules/ref readback |
| Reader bridge | Explicit v1 main nightly publication and actual client upgrade acceptance |
| Trust migration | Reviewed successor bytes, three recovery signatures, verified snapshot/floor and public readback |
| Dev nightly | Complete selected dev CI plus artifact, signature, native and runtime acceptance |

No live rulesets, branch refs, production trust files or release pointers are
changed by this PR. An unsigned proposal is a review artifact and cannot activate
the policy.

## Activation sequence

1. Land this PR through the existing main gate. Use the landed checkout and
   [local verification prerequisites](../../CONTRIBUTING.md#local-verification).
   Do not execute activation from an unlanded candidate.
2. Authenticate main's exact complete validation and activate the dev rules with
   `scripts/release/configure-dev.sh Hikyo-Org/Hikyo`. The helper verifies its own
   installed inputs, preserves stronger live review/App bindings and checks the
   final effective policy. It refuses divergent unprotected dev history.
3. Open feature PRs against dev. Confirm the `ci-dev-required` result is bound to
   the current PR/head and all three feedback lanes. Merge one accepted change
   and verify complete `ci-dev` execution for the resulting SHA. Confirm one
   active run completes and only the newest pending push remains during a burst.
   The CI speedup is live from this step. Steps 4-6 move nightlies to dev and can
   wait, because nightlies keep publishing fully proven main under v1.
4. Let ordinary v1 main nightlies deliver the reader. Verify actual installation
   and upgrade clients to the reader before enabling v2.
5. Generate a fresh unsigned policy proposal against current authenticated trust:

   ```sh
   go run ./scripts/release/stable nightly-policy-proposal --trust release/trust \
     --out "$PROPOSAL_DIRECTORY"
   ```

   Set `PROPOSAL_DIRECTORY` to a new directory outside the checkout first.
   Independently review `proposal.json` and all four proposed documents. Use the
   existing recovery custody to sign stable policy, metadata and catalog, then
   verify the complete successor snapshot against the retained floor. Install
   through the established trust ceremony and verify public readback. Retain the
   old policy digest for historical v1 nightlies. Regenerate the proposal if trust
   changes before signatures are collected.
6. Backmerge reviewed main policy/tooling changes into dev. Require
   `go run ./scripts/release/nightly preflight --require-dev-policy` on main and a
   complete green dev run. Installing v2 switches every nightly to dev. Verify the
   published v2 source matches the selected receipt, while its certificate binds
   the trusted main publisher. Main bridge requests are refused under v2.

The installed policy selects the source. After v2, schedules and requests cannot
fall back to main: an unavailable dev source fails before publication.

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
- Scheduled nightlies would fail from landing until the v2 trust ceremony.
- A dev nightly ran dev code inside a run on main, where it could poison main-scoped
  caches. The build now runs in `nightly-build.yml` on the source's own branch.

Not established before the first hosted dev run: the replay-modelled runner headroom,
whether creating `dev` succeeds under its new ruleset, the publisher's 60-second
budget on a backmerge touching many CI definitions, source proof's job inventory
against a real `ci-dev` run, and the dispatched `nightly-build` run, which GitHub can
only start once the workflow is on main. If creating `dev` fails during activation, stop and
report it. The helper's checks leave main untouched.

## Recovery and maintenance

Keep main as default and keep its mandatory queue. CI/publisher definitions land
through main first, followed by a reviewed main-to-dev merge. Fix or revert red
dev integration before a new promotion; do not force-reset shared branch history.

If identical landed-source proof is unavailable, dispatch full `ci-main` on the
selected main commit and obtain new exact-source proof. Selective/docs-only,
cancelled, missing-job or failed newer attempts never substitute for full proof.
The source-proof inventory is closed: changes to job names, matrix sizes,
required execution steps or reusable workflows must update its authenticated
inventory and fixtures with the canonical graph.

Source rollback alone cannot roll back an already signed trust floor. Preserve
v2 reader support and authorized historical policies after activation; any later
policy change follows the recovery ceremony. Keep native/platform release
acceptance separate from source-validation receipts.
