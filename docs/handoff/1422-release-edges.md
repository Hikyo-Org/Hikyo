# Handoff: stable installs have no upgrade route to the next release (#1422)

**Owner decision (2026-11-23): option B.** This change is the forward
v1.1.0 -> next edge only and does not give v1.0.0 installs a route. They get a
separate recovery-signed bridge v1.0.0 -> v1.1.0, designed in
[#1422 (comment)](https://github.com/Hikyo-Org/Hikyo/issues/1422#issuecomment-7115533447)
and implemented in a later PR after the owner answers key custody. This PR does
not close #1422 (`Refs #1422`).

## Result

- **Confirmed at runtime, SQLite and PostgreSQL 18.** An installation created by
  the released, verified v1.0.0 (applied identity recorded as
  `{"release":{"profile":"stable/v1","version":"1.0.0",...}}` in
  `upgrade_control`) cannot be migrated by v1.1.0: `hikyo migrate` fails with
  `no authenticated upgrade route`, `hikyo server` refuses with
  `migration required; auto-migrate disabled`, nothing is written, and v1.0.0
  still boots. The cause is the signed `upgrade-compatibility.json` of v1.0.0
  and v1.1.0: both list only the `fresh/v1` and `legacy/v1` genesis sources.
- **A shipped release cannot gain an edge.** The edge sits in the declaration of
  the upgrade's *target*, which is signed with that release. No change to
  `sources.json` can fix v1.0.0 → v1.1.0.
- **This change** adds the reviewed v1.1.0 release-source edge per engine, so
  the next stable release is reachable from v1.1.0 (the release planner and
  `check-compatibility-unchanged.sh` require it); makes `nightly sources` drop
  stable edges (with the edge present the nightly generator failed with
  `profile transitions require a recovery bridge`); documents how edges are
  authored; and makes `check-compatibility-unchanged_test.sh` build its
  baseline and missing-edge cases from the genesis edges only.

## Evidence

Raw logs and the script that produced them: `repro.sh` and `logs/` in the
scratchpad `repro1422` directory (not committed). Summary, per engine:

| Step | SQLite | PostgreSQL 18 (`postgres:18@sha256:06cad38a...`) |
| --- | --- | --- |
| v1.0.0 `migrate` (fresh) | exit 0, `applied_json` `{"genesis":"fresh/v1"}` | same |
| v1.0.0 server boots, `admin create` | exit 0, principals 1, `applied_json` release v1.0.0, `maintenance` 0 | same |
| v1.1.0 `migrate` | `no authenticated upgrade route`, exit 1 | same |
| v1.1.0 `server --auto-migrate=false` | `refusing to serve: migration required`, exit 1 | same |
| After the attempt | `applied_json` still release v1.0.0, principals 1 | same |
| v1.0.0 `server` again | `server ready` | same |

The planner skip for a trust-policy change does not apply to a v1.0.0 edge: the
policy digest is per signing key class (`internal/releasetrust/release.go`),
and both manifests carry `signing_key_id` `github-actions-stable`.

Checks for the change: `go test ./scripts/release/nightly`,
`sh scripts/release/check-compatibility-unchanged_test.sh`,
`sh scripts/release/security-patch-plan_test.sh`; the edges-only check against
the real v1.1.0 baseline passes with the edge and names both engines without it;
the stable generator (candidate 1.1.1) and the nightly generator both succeed
with the edge present.

## Restore finding

Restoring a v1.0.0 archive into v1.1.0 is refused:
`archive source has no authenticated embedded schema route`. Option A
("nothing, restore into a newer release") therefore strands v1.0.0 data
entirely and is not a fallback.

## Owner question (answered 2026-11-23: B): how should v1.0.0 installs reach later releases?

- **A. Nothing.** v1.0.0 installs stay on v1.0.0. Restore into a newer release
  is refused (see above). Rejected.
- **B. Recovery-signed bridge v1.0.0 → v1.1.0**, one statement per engine, in a
  new offline-signed catalog (`hikyo.dev/recovery-bridge/v2`, with a signed `not_after`). It reaches
  exactly the missing hop, changes no release check, and needs the recovery-key
  ceremony plus operator attestation at upgrade time. A v1.0.0 install then
  takes two hops (to v1.1.0, then to the next release).
- **C. The next release also carries a v1.0.0 edge** in `sources.json`. No
  ceremony and a single hop, but it departs from the pipeline's rule of exactly
  one baseline edge: the full compatibility check reports the extra edge as a
  difference to confirm, and the patch planner refuses a patch whose
  `sources.json` gains more than the baseline edge, so that release must be a
  minor one. The planner also needs the source release (v1.0.0) authenticated
  in the bundle (`plan.go:183-186`). Older edges would also accumulate.

**Recommendation: B if any v1.0.0 installation still has to be supported,
otherwise A.** B fixes only the hop that is missing, leaves the release
pipeline's invariants alone and is a one-off. C is cheaper per release but
changes what the checks mean for every later release. How many v1.0.0
installs exist is the deciding fact and is not known here.

## Delivery

The tag workflow (`release.yml`) runs no edge check. The next stable release
must be built from a commit that contains this edge: promote `dev` to `main`,
or add the edge to `main` first through a same-repository `hotfix/` branch. That
hotfix must carry the whole change (edge and `nightly sources` filter, e.g. by
cherry-picking this commit): the scheduled nightly runs on `main` and fails on a
stable edge without the filter. The edge is checked before tagging by the
release planner (`security-patch-plan.sh`) and by the `release-compatibility-check`
workflow (full `check-compatibility-unchanged.sh`).

## Remaining work

- Owner decision (#1561): the legacy nightly bridge now carries a signed `not_after`
  (schema `legacy-nightly-bridge/v2`, at most 30 days; v1 is refused at runtime), and
  a 404 for the revocation list counts as no answer (route plans with a warning, expiry
  is the limit). The two legacy v1 statements in the committed catalog
  (`release/trust/BRIDGES.md`) therefore plan no route until the owner re-signs v2
  statements offline (`scripts/release/nightly legacy-bridges --not-after ...`) and
  catalogues them. The v1 digests are policy-listed, so they cannot leave the catalog
  without a policy rotation; the runtime refuses v1, so they stay inert. Agents never
  sign or publish this. The rehearsal does not yet exercise a legacy statement.
- Revocation state (#1561 review): the accepted list and signature are one file
  (`bridge-revocations-store.json`, one rename); unusable stored state is ignored with
  a warning, `off` never reads it, and a valid newer list replaces it (the floor is lost
  until then; operator recovery is in the runbook and `manual-upgrades.md`). The
  native security maintenance service reads the revocation policy from the host's
  `hikyo.env`. The fetcher honors `HTTPS_PROXY`.

- Bridge for v1.0.0 (option B): tooling, independent verifier, rehearsal and
  ceremony runbook are in the bridge PR (`docs/operations/recovery-bridge-ceremony.md`).
  The ceremony itself (owner, #1560) is not done. The owner chose
  `recovery-2` as the signing key (ADR R2). Open: a real-signature database upgrade
  with operator attestation, and restore through `VerifyEmbeddedUpgradeSource` are untested.
- No supported command writes a stable release edge; it is still authored by
  hand (documented in `docs/operations/upgrade-artifacts.md`).
- PostgreSQL 17 fails first migrate with `upgrade: corrupt control state`
  (supported versions are undocumented): [#1524](https://github.com/Hikyo-Org/Hikyo/issues/1524).
- The actual v1.1.0 → next-release upgrade on a running binary can only be
  shown once a signed next release exists.
