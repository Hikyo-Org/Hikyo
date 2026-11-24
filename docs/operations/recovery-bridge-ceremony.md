# Recovery bridge ceremony: v1.0.0 to v1.1.0

Owner runbook for [#1422](https://github.com/Hikyo-Org/Hikyo/issues/1422) and
[#1560](https://github.com/Hikyo-Org/Hikyo/issues/1560). It authorizes one
recovery-signed upgrade route for existing v1.0.0 installations to v1.1.0, per
engine (SQLite and PostgreSQL), because no ordinary edge exists between them.
The accepted design is in the [stable workflow signing ADR](../adr/stable-workflow-signing.md#recovery-bridges-1422).

Every command below was rehearsed end to end with a throwaway key and a test
root by `scripts/release/recovery-bridge-rehearsal.sh`. The rehearsal never
reads production key material; this ceremony is the only step that does.

## Read first: three constraints the rehearsal exposed

1. **Who may sign.** A verifier accepts a bridge only if it is signed by the
   recovery key pinned in its own binary. The shipped v1.1.0 pins `recovery-1`.
   A bridge signed by `recovery-2` is accepted only by binaries that stamp a root
   pinning `recovery-2`, so it cannot open the v1.0.0 to v1.1.0 route. The owner
   must pick before this ceremony: (R1) `recovery-1` signs these two statements
   under a narrow, logged exception, or (R2) ship a release that pins
   `recovery-2` first and bridge into that release. This runbook is written for
   whichever key is pinned in the target binary; `RECOVERY_KEY` below is that key.
2. **The stable policy is not re-signed.** Each statement binds the stable policy
   digest. A policy that listed the statements would change that digest, so the
   bridges live only in a recovery-signed catalog and the policy file and its
   signature stay byte-identical. Metadata and catalog are re-signed as
   recovery-shaped files (next sequences, no `source_commit` or `release_tag`).
3. **Bridges end with the next stable release.** A workflow-signed catalog must
   list exactly the policy's bridges, so `stable prepare` drops recovery-only
   bridges. Until that release ships, `install.sh` can refuse new stable installs
   with "trust rollback refused" (see [signed nightlies](signed-nightlies.md)).
   Run this ceremony close to the next release, and re-run it if a release ships
   first and v1.0.0 installations still need the route.

Also required: no other stable-policy or release/trust change is pending
(`gh pr list --state open` and `git log -- release/trust internal/releasetrust`
are clean), so the sequences below are exact.

## Roles and custody

- The owner holds the recovery key alone, offline or on hardware.
- A witness watches the signing steps and countersigns the ceremony log.
- The signing machine needs `cosign` 3.x, `jq`, `go` and a checkout of the
  reviewed commit. Nothing leaves it except public files (statements,
  signatures, metadata, catalog).

## Steps

Set once (the private key is never copied, printed or placed in the repository):

```sh
export REPO=/path/to/reviewed/checkout        # clean checkout, commit recorded in the log
export RECOVERY_KEY=/path/to/offline/recovery.key
export WORK=$(mktemp -d)                      # public working files only
cd "$REPO" && git rev-parse HEAD && git status --short   # expect the commit, no changes
```

1. Download the published evidence for both releases. Steps 2 and 7 authenticate
   it against the pinned root and the releases' own Sigstore signatures, and
   refuse anything unverified:

   ```sh
   for v in 1.0.0 1.1.0; do
     mkdir -p "$WORK/rel-$v" && gh release download "v$v" -R Hikyo-Org/Hikyo -D "$WORK/rel-$v" \
       -p release-manifest.json -p release-manifest.sigstore.json -p release-candidate.json \
       -p upgrade-compatibility.json -p build-provenance.json -p build-provenance.json.sigstore.json
   done
   ```

2. Derive the unsigned statements from the published declarations. Expect two
   lines, `postgres <digest>` and `sqlite <digest>`; record both digests:

   ```sh
   scripts/release/recovery-bridge.sh author release/trust "$WORK/rel-1.0.0" "$WORK/rel-1.1.0" "$WORK/statements"
   ```

3. Review the statements against the declarations by eye (version, sequence,
   commit, schema digests, `"mode": "maintenance"`), then run the independent
   verifier in step 6 **before** signing by pointing it at the proposal after
   step 5. Do not sign anything the review does not explain.

4. Sign each statement (prompts for the key password; expect `signed <digest>` twice):

   ```sh
   scripts/release/recovery-bridge.sh sign "$RECOVERY_KEY" "$WORK/statements"
   ```

5. Produce the recovery-shaped trust tree (new sequences, bridges listed, policy
   untouched). Expect `new trust tree: ...`:

   ```sh
   scripts/release/recovery-bridge.sh reauthorize "$RECOVERY_KEY" release/trust "$WORK/statements" "$WORK/new-trust"
   ```

6. Run the independent verifier. It shares no code with the authoring tool: jq
   re-derives every statement field from the two published declarations and
   compares the bytes, cosign checks the signatures, sha256 checks the digests.
   Expect `All checks passed.` and exit 0. Any `FAIL` line means stop, do not
   publish, and keep the log:

   ```sh
   scripts/release/verify-recovery-bridge.sh release/trust "$WORK/new-trust" "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   ```

7. Prove an operator can consume it (expect `{"releases":2,"bridges":2}`):

   ```sh
   scripts/release/recovery-bridge.sh assemble "$WORK/new-trust" "$WORK/bundle" "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   jq -c '{releases: (.releases|length), bridges: (.bridges|length)}' "$WORK/bundle/index.json"
   ```

8. Hand over only public files: copy `metadata.json`, `metadata.sigstore.json`,
   `catalog.json`, `catalog.sigstore.json` and `bridges/` from `$WORK/new-trust`
   into a branch of `release/trust/`, open a pull request, and have a second
   person re-run step 6 on the pull request checkout. The verifier is the
   merge gate for that pull request.

## Ceremony log (fill in; commit the public part with the pull request)

```text
date/time (UTC):            location:
owner:                      witness:
checkout commit:            cosign version:
open trust PRs / commits:   none pending (command output pasted)
recovery key id and public-key sha256:
v1.0.0 manifest sha256:     v1.1.0 manifest sha256:
statement digests:          sqlite=...   postgres=...
metadata sequence:          before 3  after 4
catalog sequence:           before 5  after 6
verifier output:            All checks passed. (pasted)
signing machine action after: key returned to custody at (time)
owner signature:            witness signature:
```

## Revocation

Revocation is a new recovery-signed snapshot that drops the bridge:

```sh
scripts/release/recovery-bridge.sh revoke "$RECOVERY_KEY" "$WORK/new-trust" "$WORK/revoked" <digest>...
```

It bumps both sequences, removes the digests from the catalog and deletes their
files. A bundle that still carries a revoked bridge fails against the higher
sequence floor. The next workflow-signed stable release also drops them.

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
re-signed), and checks that the verifier refuses six tamperings: an edited
statement, a wrong schema digest, a missing bridge, an unsigned catalog change,
a policy edited to list a bridge, and a statement signed by another key.
