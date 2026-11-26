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

1. **`recovery-2` signs (R2, owner decision on #1422).** A verifier accepts a
   bridge only if it is signed by the recovery key pinned in its own binary. Rotate
   `recovery-2` in first, ship a release that pins it, and bridge into that
   release. `recovery-1` never signs the bridge. `RECOVERY_KEY` below is
   `recovery-2`.
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
4. **Every bridge expires and can be revoked.** Statements are v2 with a signed
   `not_after`, at most 30 days after signing (author's default is 29 days 23
   hours). An expired bridge stops planning offline. A signed revocation list
   (below) withdraws a bridge earlier on instances that can reach the endpoint;
   unreachable falls back to the expiry, a bad answer refuses. Choose the window
   at the ceremony and log it. A lapsed bridge is re-signed with the steps under
   "Re-signing an expired bridge".

Also required: no other stable-policy or release/trust change is pending
(`gh pr list -L 500 --state open` and `git log HEAD..origin/dev -- release/trust internal/releasetrust`
are clean), so the sequences below are exact.

## Roles and custody

- The owner holds the recovery key alone, offline or on hardware.
- Two-person custody split before 1.2: until the 1.2 release one owner holds the key
  and runs the ceremony alone, witnessed and logged. Before 1.2 ships, signing and
  publishing separate: the key holder signs offline and cannot publish the trust
  asset or merge the trust pull request, and a second person publishes and merges
  but holds no signing key. This split is the ADR's condition for ending solo custody.
- A witness watches the signing steps and countersigns the ceremony log.
- **Online machine** (steps 0 to 3 and 6 to 8): network, `gh`, `go`, `jq`, `cosign`.
  It holds no key. **Offline signing machine** (steps 4 and 5): the key, `cosign`,
  `jq`, a checkout of the same commit and the public working files carried
  over on removable media. Nothing returns from the offline machine except public
  files (statements, signatures, metadata, catalog).
- **Hardware keys.** `sign`, `reauthorize` and `revoke` pass `RECOVERY_KEY` to
  `cosign sign-blob --key`, so a PIV or PKCS#11 hardware key works in place of a
  file path. That needs the separate Linux cosign build with the
  `pivkey`/`pkcs11key` tags, a `pkcs11:` key URI and a module path (the URI's
  `module-path` or `COSIGN_PKCS11_MODULE_PATH`); cosign handles the PIN prompt.
  KMS URIs are out of scope because they contradict offline custody. The authoring and verifier steps need only the public key.

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
   # --json files lists only each PR's first 100 files; also scan the PR titles by eye.
   gh pr list -R Hikyo-Org/Hikyo -L 500 --state open --json number,files --jq '.[] | select(any(.files[]?; .path | test("release/trust|stable-policy|internal/releasetrust"))) | .number'
   git fetch origin dev
   git log --oneline "$(git rev-parse HEAD)"..origin/dev -- release/trust internal/releasetrust
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
   scripts/release/recovery-bridge.sh author release/trust "$WORK/rel-1.0.0" "$WORK/rel-1.1.0" "$WORK/statements" [NOT_AFTER]
   ```

   `NOT_AFTER` is a canonical UTC RFC 3339 time (for example `2026-12-24T00:00:00Z`)
   at most 30 days away; omit it for 29 days 23 hours from now. It is signed into
   both statements. Record the chosen window in the ceremony log. The verifier
   refuses a `not_after` that is past or more than 30 days from the verification
   time, so verify close to signing.

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
   # A re-sign appends the lapsed digests, so they leave the catalog with the new statements:
   # reauthorize ... "$WORK/new-trust" <lapsed-digest>...
   ```

6. (Online) Run the full independent verifier on the signed proposal. Besides the
   step 3 derivation it checks the signatures, digests, sequences, that only
   metadata, catalog, their signatures and `bridges/` changed, and that the policy
   is byte-identical. Expect `All checks passed.` and exit 0:

   ```sh
   scripts/release/verify-recovery-bridge.sh proposal release/trust "$WORK/new-trust" "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   ```

7. (Online) Prove an operator can consume it (expect `releases` 2 and `bridges` equal to the base catalog's bridges plus the two new ones):

   ```sh
   scripts/release/recovery-bridge.sh assemble "$WORK/new-trust" "$WORK/bundle" "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   jq -c '{releases: (.releases|length), bridges: (.bridges|length)}' "$WORK/bundle/index.json"
   ```

8. (Online) Hand over only public files: copy `metadata.json`,
   `metadata.sigstore.json`, `catalog.json`, `catalog.sigstore.json` and `bridges/`
   from `$WORK/new-trust` into a branch of `release/trust/` and open a pull
   request. A second person first runs step 1 (their own download of the proofs), then re-runs step 6 against that branch, using the
   pre-ceremony tree from the base branch as the base and the step 1 proofs
   (`$WORK/rel-1.0.0`, `$WORK/rel-1.1.0`, downloaded again independently):

   ```sh
   git fetch origin dev <proposal-branch>
   base=$(git merge-base origin/dev origin/<proposal-branch>)
   git worktree add "$WORK/base" "$base"
   git diff --quiet "$base" origin/dev -- release/trust   # nonzero: dev changed the trust tree, rebase the proposal and stop
   git checkout --detach origin/<proposal-branch>
   # Only release/trust may change; the verifier is the base's own, never the proposal's.
   test -z "$(git diff --name-only "$base" HEAD | grep -v '^release/trust/')" || { echo 'proposal touches files outside release/trust'; false; }
   "$WORK/base/scripts/release/verify-recovery-bridge.sh" proposal "$WORK/base/release/trust" release/trust "$WORK/rel-1.0.0" "$WORK/rel-1.1.0"
   ```

   The base is the proposal's merge base with `dev`, not the moving tip of `dev`,
   so the check stays valid after `dev` advances, provided the `git diff --quiet` line above
   shows that `dev` has not changed `release/trust` since that base. The verifier is the merge gate
   for that pull request; it refuses any file outside the four metadata and
   catalog files and the new bridge directories (two files each), any symlink,
   unreadable or empty directory, unusual file name or duplicate JSON key. Do not
   add `BRIDGES.md` or any other file to the trust pull request; if a record there
   must change, do it in a separate pull request after the trust one merges.

## Ceremony log (fill in; attach the public part to #1560, or commit it in a separate docs-only pull request, never in the trust pull request)

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
files. This binds instances and bundles that use the new snapshot, not an older
bundle already in an operator's hands: use the revocation list for those.

## Revocation list (apply-time)

Sign and publish the list with the recovery key, then verify it. `SEQUENCE` is the
published list's sequence plus one (1 for the first), `PRIOR` the published
sequence (0 if none). Publish `recovery-revocations.json` and
`recovery-revocations.sigstore.json` as assets of the fixed `trust` release in
`Hikyo-Org/Hikyo`, which is the default endpoint
(`https://github.com/Hikyo-Org/Hikyo/releases/download/trust/recovery-revocations`
plus `.json` and `.sigstore.json`; operators may mirror it with
`HIKYO_UPGRADE_REVOCATION_URL`). Uploading or replacing those assets is the
owner's step after signing; agents never publish them. Clients follow GitHub's
redirect to its asset hosts only. Publish the first list (sequence 1) with the
bridge so a revocation can later reach hosts that fetch it. Until the assets exist
the endpoint answers 404, which counts as no answer (like an unreachable endpoint):
a route through an unexpired bridge plans with a logged warning and the signed
`not_after` is the only limit. A list that is present but invalid (bad signature,
malformed, wrong root, signature missing) still refuses. Every bridge, the legacy
nightly one included, carries a `not_after`:

```sh
scripts/release/recovery-bridge.sh revocation-list "$RECOVERY_KEY" "$WORK/new-trust" <SEQUENCE> "$WORK/list" <digest>...
scripts/release/verify-recovery-bridge.sh revocation-list "$WORK/new-trust" "$WORK/list" <PRIOR> "$(shasum -a 256 release/trust/root.json | cut -d' ' -f1)" "$(shasum -a 256 release/trust/$(jq -r .recovery.public_key release/trust/root.json) | cut -d' ' -f1)"
scripts/release/recovery-bridge.sh check-revocations "$WORK/new-trust" "$WORK/state" "$WORK/list" <digest>...
```

The list is signed by the pinned recovery key and bound to the root, so a host
compromise can withhold it but not forge or redirect it. The last two verifier
arguments pin the committed root and recovery key (take them from the committed
tree, not from a proposal). An instance that has stored a list refuses a lower
sequence and a different list at the same sequence. An instance that has stored
nothing (a v1.0.0 install applying the bridge) cannot tell a replay by sequence,
so the list carries a signed `issued_at`: anything older than 7 days is stale and
behaves like an unreachable endpoint (the bridge's own `not_after` decides; the
list's entries still apply). Sign a list when a bridge is published and again
whenever something is revoked, never on a schedule: freshness buys no security,
because whoever controls the endpoint can already force the unreachable path and
fall back to `not_after`, and a stale list's revocations still apply, so a stale
list only makes the recorded outcome (`stale-fell-back-to-expiry`) honest. The
verifier refuses a list dated in the future and a list older than the newest
catalogued bridge it covers (that bridge's `not_after` minus 30 days), and has no
other age limit. Residual: a host can replay an older validly signed list to such
an instance, delaying a newer revocation until the bridge's `not_after`. `check-revocations`
runs the real checker against a local list directory (`-` for the unreachable case).

Operator recovery for stored state: an installation keeps the last accepted list in
`HIKYO_UPGRADE_STATE_DIR/bridge-revocations-store.json` (one file, fsynced and replaced by one
rename). Stored state that exists but does not verify refuses bridge routes under `off`, with an
unreachable endpoint and with a stale list (a security rollback is never blocked); only a fresh,
non-stale, verified list, not below the sequence floor (`bridge-revocations-floor.json`, raised only by a stored list and bound to the trust root; an unreadable one refuses), replaces it. The floor is enforced whenever it exists: a removed store, or a store behind the floor, is unusable too. Either reach the
endpoint, or stop the service and remove that file and the floor record (and any older `bridge-revocations.json` and
`bridge-revocations.sigstore.json`) as an explicit operator action, accepting that revocations known
only to that list are lost and that until the next list is stored an older validly signed list is
bounded only by its 7-day `issued_at`. A list bound to another (older) trust root is unusable too until a fresh
list for this root replaces it. The fetcher honors `HTTPS_PROXY` and `NO_PROXY` (on the native security path, from
the host's `hikyo.env`); the signature, not the path, is the authority.

Expiry and revocation govern admission, not an upgrade already admitted. An
upgrade admitted before `not_after` can resume and finish after it: a resume
checks that the journaled route digest belongs to a valid path in the
authenticated graph, ignoring expiry (it does not re-run the route search, which
could pick another route once other bridges have lapsed or been revoked). A bridge
revoked since the admission still refuses the forward resume. A security rollback
checks the journaled route the same way and is never blocked by expiry or
revocation: it returns to the journaled source whatever the bridge's state.
Re-sign an expired bridge only for installations that have not been admitted yet.

## Legacy nightly bridge

The legacy nightly bridge (`hikyo.dev/legacy-nightly-bridge/v2`) expires like every
other bridge, so a signing ceremony for it includes choosing its `not_after`: the
owner picks a canonical UTC instant at most 30 days after signing and passes it as
`scripts/release/nightly legacy-bridges --not-after ...`; the tool refuses a missing,
past, non-canonical or longer-than-30-day value. The owner's offline steps: author
the unsigned statements with that command, sign each with `recovery-bridge.sh sign`
(the recovery key never leaves the signing machine), catalogue the digests with
`recovery-bridge.sh reauthorize`, and publish the first revocation list with them. A
lapsed legacy bridge is re-authored with a new `--not-after` and re-signed the same
way; the new statement is a new digest on the same pair, and the old one is dropped
from the catalog in the same step (a revoked or lapsed statement and its replacement
may share a pair only while the revocation list names the old one). The committed `v1`
digests are policy-listed, so they cannot be dropped without a policy rotation. Rotating
the policy is an owner step (a tidy-up, not a merge or release blocker): until then the runtime
loads the `v1` statements as verified but always expired, so they count in the inventory,
every bundle still loads, and no legacy-nightly route plans through them. The rehearsal does
not yet cover this path with a legacy statement; the generator and the verifier's age
selector do. Agents never sign or publish; repository fixtures use throwaway keys. The
expiry-free `legacy-nightly-bridge/v1` statements in the committed catalog stay unusable
until the owner re-signs `v2` statements and rotates the policy. The revocation-list verifier counts these
statements' `not_after` toward its freshness bound, but it does not re-derive them
(the generator and the signer's review do).

## Re-signing an expired bridge

When `not_after` is near or has passed, repeat steps 1 to 6 against the same
published proofs with a fresh window (`author ... [NOT_AFTER]`), signed with
`recovery-2`, and pass the lapsed digests to step 5 so `reauthorize` removes
them from the catalog and the tree. The new statement has a new digest, so it is
a new catalog entry; leaving the old one beside it puts two bridges on one pair
and the verifier refuses that tree. A bundle that already holds the lapsed bridge
keeps planning other routes: an expired or revoked bridge is skipped, not fatal.

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
re-signed), and checks that the verifier refuses each tampering by the check that targets
it: statements edited, mis-derived, missing, unsigned or signed by another key;
a policy edited to list a bridge; validly signed metadata or catalog edits, skipped
sequences, dropped base files, swapped recovery keys; symlinks, extra files or empty
directories, unusual file names, unreadable directories, duplicate JSON keys; and a
properly signed and catalogued statement with one wrong field (caught by the
re-derivation alone); expired, missing, malformed, impossible, offset and over-30-day
`not_after` values and a v1 downgrade; and the revocation list (bad signature, older
sequence, another root, unsorted, edited after signing) plus the real checker's
unreachable fallback, revocation, offline memory, rollback refusal and bad-signature
refusal. The rehearsal also covers the production shape (two legacy
bridges plus two new). It also checks that revoking a policy-listed bridge is
refused.
