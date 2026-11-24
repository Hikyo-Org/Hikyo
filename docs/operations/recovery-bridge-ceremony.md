# Recovery bridge ceremony: v1.0.0 to v1.1.0

Owner runbook for [#1422](https://github.com/Hikyo-Org/Hikyo/issues/1422) and
[#1560](https://github.com/Hikyo-Org/Hikyo/issues/1560). It authorizes one
recovery-signed upgrade route for existing v1.0.0 installations to v1.1.0, per
engine (SQLite and PostgreSQL), because no ordinary edge exists between them.
The accepted design is in the [stable workflow signing ADR](../adr/stable-workflow-signing.md#recovery-bridges-1422).

Every command below was rehearsed end to end with a throwaway key and a test
root by `scripts/release/recovery-bridge-rehearsal.sh`. The rehearsal never
reads production key material; this ceremony is the only step that does.

## Read first: constraints the rehearsal exposed

1. **Which key signs is an open owner decision.** A verifier accepts a bridge
   only if it is signed by the recovery key pinned in its own binary, and the
   shipped v1.1.0 pins `recovery-1`. A bridge signed by another key is accepted
   only by binaries that stamp a root pinning that key. This runbook is
   key-agnostic: `RECOVERY_KEY` is whichever key the target binary pins.
2. **The stable policy is not re-signed.** Each statement binds the stable policy
   digest. A policy that listed the statements would change that digest, so the
   bridges live only in a recovery-signed catalog and the policy file and its
   signature stay byte-identical. Metadata and catalog are re-signed as
   recovery-shaped files (next sequences, no `source_commit` or `release_tag`).
3. **Bridges end with the next stable release.** A workflow-signed catalog must
   list exactly the policy's bridges, so `stable prepare` drops recovery-only
   bridges. It refuses instead if the policy lists a bridge the catalog dropped,
   because restoring it would undo a revocation; that needs a policy rotation.
   Until that release ships, `install.sh` can refuse new stable installs with
   "trust rollback refused" (see [signed nightlies](signed-nightlies.md)). Run
   this ceremony close to the next release.
4. **Revocation and expiry have a real limit.** An instance enforces the snapshot
   floor it persisted, and the floor advances only when the instance loads a newer
   snapshot. A v1.0.0 instance that never loaded the revoking snapshot still
   accepts an older bundle that carries the bridge, and the statements carry no
   expiry. Revocation protects bundles assembled from the current snapshot and
   instances that have advanced; it does not recall a bundle already handed out.
   Operators should be told to rebuild bundles from the latest trust tree, and
   the next stable release removes the bridge from every newly built bundle.

Also required: no other stable-policy or release/trust change is pending
(`gh pr list --state open` and `git log -- release/trust internal/releasetrust`
are clean), so the sequences below are exact.

## Roles and custody

- The owner holds the recovery key alone, offline or on hardware.
- A witness watches the signing steps and countersigns the ceremony log.
- **Online machine** (steps 0 to 3 and 6 to 8): network, `gh`, `go`, `jq`, `cosign`.
  It holds no key. **Offline signing machine** (steps 4 and 5): the key, `cosign`,
  `jq`, `go`, a checkout of the same commit and the public working files carried
  over on removable media. Nothing returns from the offline machine except public
  files (statements, signatures, metadata, catalog).
- **Hardware keys.** `sign`, `reauthorize` and `revoke` pass `RECOVERY_KEY` to
  `cosign sign-blob --key`, so a key URI that cosign supports (for example a
  `pkcs11:` or KMS URI) works in place of a file path; cosign handles the PIN
  prompt. The authoring and verifier steps need only the public key.

## Steps

Set once (the private key is never copied, printed or placed in the repository):

```sh
export REPO=/path/to/reviewed/checkout        # clean checkout, commit recorded in the log
export RECOVERY_KEY=/path/to/offline/recovery.key   # or a cosign key URI (hardware)
export WORK=$(mktemp -d)                      # public working files only
cd "$REPO" && git rev-parse HEAD && git status --short   # expect the commit, no changes
cosign version | grep GitVersion              # record it; the pinned CI tool is cosign v3.1.3
```

0. (Online) Confirm nothing else is pending (expect no PRs and no commits listed):

   ```sh
   gh pr list -R Hikyo-Org/Hikyo --state open --json number,files --jq '.[] | select(any(.files[]?; .path | test("release/trust|stable-policy|internal/releasetrust"))) | .number'
   git log --oneline origin/dev -5 -- release/trust internal/releasetrust
   ```

1. (Online) Download the published evidence for both releases. Steps 2 and 7
   authenticate it against the pinned root and the releases' own Sigstore
   signatures, and refuse anything unverified:

   ```sh
   for v in 1.0.0 1.1.0; do
     mkdir -p "$WORK/rel-$v" && gh release download "v$v" -R Hikyo-Org/Hikyo -D "$WORK/rel-$v" \
       -p release-manifest.json -p release-manifest.sigstore.json -p release-candidate.json \
       -p upgrade-compatibility.json -p build-provenance.json -p build-provenance.json.sigstore.json
   done
   ```

2. (Online) Derive the unsigned statements from the published declarations.
   Expect two lines, `postgres <digest>` and `sqlite <digest>`; record both digests:

   ```sh
   scripts/release/recovery-bridge.sh author release/trust "$WORK/rel-1.0.0" "$WORK/rel-1.1.0" "$WORK/statements"
   ```

3. (Online, and again on the offline machine) Run the independent verifier on the
   **unsigned** statements before anything is signed. jq re-derives every field
   from the two published declarations and compares each statement, normalized
   with `jq -S`. Expect `All checks passed.` Any `FAIL` means stop and keep the log:

   ```sh
   scripts/release/verify-recovery-bridge.sh unsigned release/trust "$WORK/statements" "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   ```

   Also review the statements by eye (version, sequence, commit, schema digests,
   `"mode": "maintenance"`). Do not sign anything the review does not explain.

4. (Offline) Sign each statement (prompts for the key password or PIN; expect
   `signed <digest>` twice):

   ```sh
   scripts/release/recovery-bridge.sh sign "$RECOVERY_KEY" "$WORK/statements"
   ```

5. (Offline) Produce the recovery-shaped trust tree (new sequences, bridges listed,
   policy untouched). It is built in `new-trust.partial` and renamed only when
   both signatures succeed. Expect `new trust tree: ...`:

   ```sh
   scripts/release/recovery-bridge.sh reauthorize "$RECOVERY_KEY" release/trust "$WORK/statements" "$WORK/new-trust"
   ```

6. (Online) Run the full independent verifier on the signed proposal. Besides the
   step 3 derivation it checks the signatures, digests, sequences, that only
   metadata, catalog, their signatures and `bridges/` changed, and that the policy
   is byte-identical. Expect `All checks passed.` and exit 0:

   ```sh
   scripts/release/verify-recovery-bridge.sh proposal release/trust "$WORK/new-trust" "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   ```

7. (Online) Prove an operator can consume it (expect `{"releases":2,"bridges":2}`):

   ```sh
   scripts/release/recovery-bridge.sh assemble "$WORK/new-trust" "$WORK/bundle" "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   jq -c '{releases: (.releases|length), bridges: (.bridges|length)}' "$WORK/bundle/index.json"
   ```

8. (Online) Hand over only public files: copy `metadata.json`,
   `metadata.sigstore.json`, `catalog.json`, `catalog.sigstore.json` and `bridges/`
   from `$WORK/new-trust` into a branch of `release/trust/` and open a pull
   request. A second person re-runs step 6 against that branch, using the
   pre-ceremony tree from the base branch as `BASE_TRUST`:

   ```sh
   git fetch origin dev && git worktree add "$WORK/base" origin/dev
   git checkout <proposal-branch>
   scripts/release/verify-recovery-bridge.sh proposal "$WORK/base/release/trust" release/trust "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   ```

   The verifier is the merge gate for that pull request.

## Ceremony log (fill in; commit the public part with the pull request)

```text
date/time (UTC):            location:
owner:                      witness:
checkout commit:            cosign version:
open trust PRs / commits:   none pending (step 0 output pasted)
recovery key id and public-key sha256:
v1.0.0 manifest sha256:     v1.1.0 manifest sha256:
statement digests:          sqlite=...   postgres=...
step 3 verifier output:     All checks passed. (pasted, before signing)
metadata sequence:          before 3  after 4
catalog sequence:           before 5  after 6
step 6 verifier output:     All checks passed. (pasted)
key returned to custody at (time):
owner signature:            witness signature:
```

## Revocation

Revocation is a new recovery-signed snapshot that drops the bridge. It accepts
only recovery-only bridges (a digest the stable policy lists needs a policy
rotation and is refused), and its output is checked by the verifier:

```sh
scripts/release/recovery-bridge.sh revoke "$RECOVERY_KEY" "$WORK/new-trust" "$WORK/revoked" <digest>...
scripts/release/verify-recovery-bridge.sh revocation "$WORK/new-trust" "$WORK/revoked" <digest>...
```

It bumps both sequences, removes the digests from the catalog and deletes their
files. The limit in constraint 4 applies: it binds instances and bundles that
use the new snapshot, not an older bundle already in an operator's hands.

## Using the bridge (operators)

Operators follow [manual upgrades](manual-upgrades.md): assemble the bundle
(step 7) for `HIKYO_UPGRADE_BUNDLE`, take the pre-upgrade backup, and provide the
operator attestation. A bridge always requires maintenance mode and the
attestation; it is never applied automatically.

## Rehearsal

```sh
scripts/release/recovery-bridge-rehearsal.sh [PROOFS_DIR]
```

It generates a throwaway key and re-roots a copy of the committed trust tree
under it, runs steps 2 to 7 plus revocation against the real published
v1.0.0 and v1.1.0 proofs (authenticated by their own Sigstore signatures, never
re-signed), and checks that the verifier refuses seven tamperings: an edited
statement, a wrong schema digest, a missing bridge, an unsigned catalog change,
a policy edited to list a bridge, a statement signed by another key, and a
properly signed and catalogued statement with one wrong field (caught by the
re-derivation alone). It also checks that revoking a policy-listed bridge is
refused.
