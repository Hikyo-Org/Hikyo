# Online stable release guide

Run `scripts/release/ceremony.sh --help` to inspect the supported commands.
This command needs no key, changes no GitHub state, and does not start a release.

The implementation combines SOPS-style keyless signatures and provenance with
OpenBao-style review of a draft before publication. No SOPS or OpenBao service
is installed. GitHub's short-lived OIDC identity signs routine releases; the
existing recovery key authorizes that delegation once.

**[Hikyo 1.0.0 is published](https://github.com/Hikyo-Org/Hikyo/releases/tag/v1.0.0).**
The [publication record](1.0.0.md) records its identity and remaining closeout.
Public verification and signed trust synchronization follow publication;
confirm the selected checkout's authenticated trust state before verification
or another release. The commands below remain the maintainer procedure.

## One-time setup

1. Use a clean checkout of merged `main`. Install the pinned tools listed in
   `.github/workflows/release.yml`: Go, Cosign, GitHub CLI, jq and Git with
   cryptographic signing enabled. Authenticate `gh` as the release maintainer.
   Configure the [Apple Developer ID and notarization credentials](apple-signing.md#one-time-credential-setup)
   before a stable build. macOS signing is required for both the downloadable
   DMGs and the Homebrew cask; enrollment alone does not configure credentials.
   The repository must retain protected main, immutable version tags and
   immutable public releases. Publication checks immutable-release settings,
   signed tags and exact-commit CI; the build probes tag immutability. These
   checks do not silently change repository controls.
2. Confirm the existing encrypted recovery key and its passphrase are
   recoverable. The default signer is
   `~/Library/Application Support/hikyo/nightly-bootstrap/tools/key-custody`.
   It retrieves the existing Keychain passphrase without printing it. A custom
   `HIKYO_RECOVERY_SIGNER` must implement
   `sign recovery-1 INPUT OUTPUT_BUNDLE`, using the existing recovery key and
   keyed Cosign bundle format. Never put a passphrase in a command argument.
3. Run `scripts/release/ceremony.sh setup`. Inspect the displayed public policy
   before typing its exact authorization phrase. It binds the repository and
   owner numeric IDs, stable workflow, tag namespace, hosted runner, OIDC
   issuer, Fulcio/Rekor roots, SCT requirement, existing primary inventory,
   nightly policy and bridge inventory. The helper recovery-signs the proposal,
   verifies it, and opens a signed, DCO-signed-off public trust PR.
4. Review and merge that PR after green CI. Pull main and run
   `scripts/release/ceremony.sh status`. Success means the stable workflow
   delegation authenticates; it is not evidence of a published stable release.

Activation adds only `stable-policy.json`, `stable-policy.sigstore.json` and
`stable-trusted-root.json` under `release/trust/`. It preserves the deployed
root, nightly policy, bridges and historical release authorization. Ordinary
builds refuse to run until this delegation authenticates.

## Routine releases

Use the actual approved version in place of `VERSION`, without a leading `v`.
Do not run `build 1.0.0` until designs and final-candidate acceptance are done.

1. **Build the draft:** from clean current main, run
   `scripts/release/ceremony.sh build VERSION`. The helper requires green CI
   for the exact commit and synchronized published trust. Review the commit
   and confirm the signed tag push. The release workflow builds and signs a
   complete draft, including image/chart signatures and provenance. Wait for
   `release-build` to succeed. A draft is not official publication.
2. **Review:** run `scripts/release/ceremony.sh review VERSION`. It downloads the
   complete draft, verifies signatures, identity, closed inventory, provenance,
   rollback state and actual OCI signatures, and prints the local directory
   and manifest SHA-256. Inspect release notes, acceptance evidence and source
   changes. Keep that exact hash.
3. **Publish:** run
   `scripts/release/ceremony.sh publish VERSION REVIEWED_MANIFEST_SHA256`.
   The helper downloads and verifies again, rejects a changed hash, and asks
   for confirmation of the exact version and hash. The local publication helper
   from clean current main checks the tag, CI, draft, OCI subjects and asset
   inventory immediately before publishing. It uses your existing administrator
   `gh` login to read immutable-release settings; GitHub's standard workflow
   token cannot read that administration endpoint. It requires immutable
   releases, then redownloads and verifies the public result. Wait for the
   command to pass.
4. **Record published trust:** run
   `scripts/release/ceremony.sh sync-trust VERSION`. It re-verifies the public
   release and opens a PR containing its signed metadata and catalog. Review
   and merge it promptly after green CI, then pull main. Merging redeploys
   hikyo.app: the landing page and docs name the new version, and the demo
   is rebuilt with them. That deployment fails unless the live landing page
   links the new version. The next build
   refuses to start until this synchronization is complete, and it checks the
   same landing page link.

   Once the stable-release App is provisioned, a ruleset lets only that App and
   the repository admin role write `release/sync-trust-*`. This command's push
   therefore still works for an admin; a non-admin maintainer's push is refused.
   The [unattended reconciler](#unattended-security-patch-releases) syncs trust
   itself after a human release too. If it has not, open the PR from a branch
   with another name, or roll the App back (`configure-repository.sh` without
   `HIKYO_STABLE_RELEASE_APP_ID` deletes that ruleset).

There is one human review/publication boundary. With one maintainer it does not
provide independent two-person approval. GitHub account security, protected
workflow changes and careful source review remain part of release security.

## Unattended security patch releases

A merged Dependabot security update that targets `main` releases itself as the
next PATCH version. Everything below is inert until the owner sets the
`STABLE_RELEASE_APP_CLIENT_ID` and `STABLE_RELEASE_APP_BOT_ID` repository
variables; until then every job in `stable-release-reconciler` is skipped.

The two variables have different jobs. `STABLE_RELEASE_APP_BOT_ID` arms the PR
gate: every job that runs `pr-gate` (the queue gate and worker, the dev gate and
the check retirement) reads it, and it refuses every pull request authored by
the App, and every commit by the App in anyone else's pull request, except the
trust-sync shape. Set it before the App is installed or granted contents write.
`STABLE_RELEASE_APP_CLIENT_ID` is the on/off switch of the reconciler and is
set last. If the client id is set and the bot id is empty, the PR gate fails
closed and the reconciler's `misconfigured` job fails every run, so a half
configured App cannot go unnoticed. Never delete the bot id while the App is
installed: that would disarm the gate for a key that still has contents write.

**Accepted risk (owner, 2026-11-15).** An unattended patch has no
administrator checkpoint: nobody pushes the tag or reviews the draft. It
releases the newest green commit of `main` at or after the security merge, so
it carries everything merged since the previous stable release. Main's required
CI and merge queue are the only controls on what ships; main requires 0
approvals and admits same-repo `hotfix/` and `release/` branches. See the
[ADR](../adr/stable-workflow-signing.md).

The reconciler runs on every push to `main`, after `release-build` and `docs`
runs, hourly, and by hand. Each run reads live state and does only the next
step, so a lost trigger or an overlapping run repeats idempotent work:

1. **Queue:** Dependabot security PRs that GitHub's own alerts prove
   (labels are not proof) are armed for auto-merge through the merge queue.
   Every commit of the PR must be authored by Dependabot, committed by GitHub's
   `web-flow` and verified by GitHub (the shape of a real Dependabot commit;
   all bound by numeric user id, never by login), and the PR's last commit must
   be the head the queue would merge. A security PR that fails this opens the
   stall issue at once, naming the PR. Alerts are listed with explicit states,
   and a fixed alert that no Dependabot PR is linked to opens the issue too, so
   that "no pending merge" never hides a security fix.
2. **Tag:** the newest main commit at or after the merged update that has
   complete green source proof and already records the latest release in its
   trust gets an annotated tag created by the App. The walk back from the tip
   stops at the commit holding the security merge, and after 60 commits. The
   planner runs every refusal the tag job runs (outstanding drafts, higher
   tags, patch-only, unused version, and the compatibility gate below) first,
   so it never plans a tag that would be refused. It also needs the docs live
   and a current fallback channel test record.
3. **Wait:** `release-build` stages a signed draft and uploads its manifest
   SHA-256.
4. **Publish:** the reconciler publishes that exact run's draft through the same
   helper the ceremony uses, but only for a run whose actor and triggering
   actor are the stable-release bot at the tag commit, whose own commit
   carries the actor-based `require-signed-tag.sh` (an older commit's workflow
   would trust an unsigned tag from anyone). Just before publishing it reads
   the alerts again: the tag commit must contain at least one merged security
   update missing from the previous release, and every one that was merged more
   than five minutes before the build started. A merge that raced the tag is
   not required, because the tag is immutable. A draft of a person's tag waits
   for the ceremony.
5. **Sync trust:** an App-signed `createCommitOnBranch` commit on
   `release/sync-trust-VERSION` records the metadata and catalog in a PR with
   auto-merge, re-armed for up to three hours if the queue rejects it. The
   merged branch is deleted afterwards. Nothing waits for the docs here: the
   next tag defers while a docs deployment runs or hikyo.app fails the live
   docs gate, so a tag can never start on stale docs.
6. **Homebrew:** the stable cask follows every latest stable release, human or
   unattended, and its PR is merged once the newest `ci-required` run passes (a
   later failure or a re-run in progress outranks an older success). The tap
   commit and the squash commit carry the App's DCO sign-off.

**The compatibility gate.** A patch is unattended only when it changes nothing
about upgrade compatibility. The planner starts scratch PostgreSQL, generates the
declaration of the commit it would tag with the real
`scripts/release/compatibility` tool, and compares it with the declaration the
previous stable release published, through `check-unattended-compatibility.sh`.
Both documents are normalized with `jq -S`, only the version, sequence and
source commit are removed, and the whole remaining `engines` subtree must be
identical, so a new migration, a schema digest, a source edge or a new field
all fail closed. The previous release is the highest stable version below the
candidate in the authenticated trust (never a prerelease, never GitHub's
"latest" flag) and is fetched through the signature-verified release path. A
difference, a missing baseline or an unverifiable one gives no tag and the
stall issue "a maintainer must release this version". `release-build` runs the
same script again for every authority except a verified person.

One issue titled "Unattended stable patch release is stalled" is opened and
updated in place, and closed when the stall clears. It opens when a merged
security update has waited over 24 hours in any stuck state (build not started
or running, digest artifact missing, docs not live, stale fallback channel
record, no commit with green proof or synchronized trust, a tag that would be
refused), when a build fails, when a person re-ran the app's build, when any
reconciler job fails, times out or is cancelled, when a security pull request
carries a commit that is not Dependabot's or a fixed alert has no linked pull
request (these two open it at once and a later "nothing to release" never
closes it), and when a published release has waited more than 24
hours for its trust sync, with or without a pending security merge. A trust
pull request the queue keeps rejecting is re-armed for three hours counted from
the first pull request ever opened from that branch, so closing and reopening
it never restarts the clock; after that the sync fails visibly.

Refusals the build can only find while it runs (the signing and notarization
steps, a failing build input, a moved branch or an unavailable environment)
cannot be pre-checked, so they can still burn a tag; the stall issue then
carries the recovery below. Everything that depends only on repository state
is checked by the planner first.

Recovering a stall:

- **A failed app build.** The tag is immutable and the app cannot rerun it. If
  the build staged a draft, delete it first, or it blocks every later release:
  `gh release delete vX.Y.Z --repo Hikyo-Org/Hikyo --yes`. Then release the next
  patch by hand with the ceremony above. After a human release, the next
  unattended patch builds on it automatically.
- **A person re-ran the app's build.** GitHub then reports the person as the
  triggering actor, and the tag authority refuses the draft for the reconciler
  and for the ceremony alike. The planner says so at once (state "re-run by a
  person", with the issue open). The tag is immutable: delete the draft with the
  command above, then release the next patch with the ceremony. Do not re-run an
  app build.
- **A stray `v*` tag (for example `v99.0.0` made with a stolen App key).** Any
  stable tag at or above the next version blocks every unattended release, and
  the planner's stall message names this section. A repository admin deletes the
  tag (admins can create `v*` tags, but the immutability ruleset forbids deleting
  them, so temporarily disable "release tags are immutable", delete the tag and
  re-enable it, then run `configure-repository.sh` to prove the policy is back),
  and rotates the App key. Check `git log` and the draft list for anything the
  key produced.
- **Finishing an app-tagged draft by hand.** Export the bot id first
  (`export STABLE_RELEASE_APP_BOT_ID=<numeric bot user id>`), then run the
  ceremony `publish` as usual. The tag authority reads the build run, sees the
  bot, and applies the unattended rules, which it would refuse for a minor.
- **Fallback channel record expired.** Run and record a new fallback channel
  test; the next run proceeds.
- **Granting the App.** Order matters. Set `STABLE_RELEASE_APP_BOT_ID` first
  (before the App is installed or granted contents write, so the PR gate is
  armed from the first moment the key exists) and `STABLE_RELEASE_APP_CLIENT_ID`
  last. `configure-repository.sh` refuses to give
  the App its tag and trust-branch bypass until the highest stable tag's own
  commit contains the actor-based `require-signed-tag.sh`; an older stable line
  would accept the App's unsigned tag as a person's. So: merge this feature,
  release a stable version from that main with the ceremony, then run
  `HIKYO_STABLE_RELEASE_APP_ID=<id> scripts/release/configure-repository.sh`.
  Rolling back (the variable unset) needs no marker.
- **Rolling the whole feature back.** Unset `STABLE_RELEASE_APP_CLIENT_ID`,
  which skips every job, and run `configure-repository.sh` without
  `HIKYO_STABLE_RELEASE_APP_ID` so the tag policy is admin-only again and the
  app-only trust branch ruleset is deleted. Keep `STABLE_RELEASE_APP_BOT_ID`
  set until the App is uninstalled and its key revoked; deleting it earlier
  disarms the PR gate for a key that still has contents write.

Dry run: dispatch `stable-release-reconciler` with `dry_run` left on to print
the next step without changing anything. The owner setup and live verification
steps are in the pull request that introduced this section.

## Verification and retries

Public downloads and proposals are retained under
`~/Library/Application Support/hikyo/stable-releases`, overridable with
`HIKYO_RELEASE_STATE_DIR`. These contain public evidence only. Rollback state
is separate from per-version download directories at
`${XDG_STATE_HOME:-~/Library/Application Support}/hikyo/release-trust.json`,
overridable with `HIKYO_RELEASE_TRUST_STATE`; do not delete it to make an old
release pass. The verifier's `--state PATH` option selects an explicit persistent
file.

`review` may be repeated for the same authenticated bytes, including before the
trust-update PR merges. `publish` may be retried after a network interruption:
an already public immutable release is verified against the reviewed hash
without modifying it. A differing manifest, changed assets, invalid identity,
revocation or rollback is a failure, not permission to regenerate evidence.

Setup and trust-sync PR retries reuse matching verified trust proposals and
existing PRs. Divergent branch content is refused for manual inspection. Do
not force-push or reset a release branch to bypass that refusal.

An outstanding stable draft blocks a second candidate, preventing two routine
builds from reusing the same release sequence. Publish and synchronize the
existing draft before starting another version.

An existing version tag is never recreated. If a build fails before all OCI
and draft assets exist, inspect the original run. Rerunning may fail closed
where versioned OCI subjects already exist. If the original byte set cannot
be completed safely, use a new version. Publication never rebuilds artifacts.

Old binaries without stable-workflow policy support cannot validate this new
profile. Update the verifier/runtime through an already authenticated path;
never treat a signature failure as permission to disable verification. The
new verifier retains explicit historical keyed verification and current
revocation checks.

Homebrew remains a downstream convenience channel. After public verification,
run `scripts/release/ceremony.sh homebrew VERSION`, confirm the proposed tap
PR, and review its protected PR separately. The online ceremony does not automatically merge the tap PR.
The cask consumes the same signed, notarized, stapled DMGs as direct downloads.

## Custody and recovery

The existing root was generated on an online Mac, with encrypted local Cosign
keys and separate Keychain passphrases. Do not describe it as an offline or
hardware-isolated root. Routine release signing no longer decrypts either
long-lived key. The primary key remains relevant to historical trust; the
recovery key still authorizes policy changes and revocation.

Vaultwarden can hold an encrypted recovery-key attachment and the recovery
information. Putting ciphertext and its passphrase in the same vault accepts
that a compromise of the unlocked vault can expose both. This is the selected
convenience tradeoff, not independent custody. There is no USB requirement.

Keep an independent encrypted backup outside the same Vaultwarden instance and
failure domain. Include attachment files as well as a consistent database
backup, configuration and the information needed to decrypt and restore it.
Do not make recovering the only vault backup depend on a password stored only
inside that unavailable vault. Test restore and a harmless sign/verify drill
annually and after changing custody. This implementation has not inspected or
changed the running Vaultwarden instance or its backups.

For a compromised GitHub account or workflow, first stop release authority and
revoke affected GitHub credentials. Audit source, workflow changes, tags and
published subjects. Use recovery-authorized policy revocation for affected
manifest hashes; do not assume keyless signing makes malicious releases safe.
Policy changes require an explicitly reviewed recovery-signed update, not
another ordinary release signature. A root change remains an out-of-band
rebootstrap for already installed verifiers.

Loss of recovery custody requires out-of-band recovery-root replacement.
Workstation compromise can also expose local recovery custody; assess it as a
possible root compromise. Updated revocations protect verifiers only after
they receive authenticated new trust. A permanently disconnected verifier
cannot learn that an old release was revoked.

The provenance is a signed first-party statement from the build workflow. It
binds source, producer and artifact hashes, but is not an independently isolated
builder, reproducible-build proof or a certified SLSA level. See the
[design decision](../adr/stable-workflow-signing.md) and
[primary-source comparison](../research/release-signing-ceremonies.md).
