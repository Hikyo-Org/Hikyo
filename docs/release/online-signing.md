# Online stable release guide

Run `scripts/release/ceremony.sh --help` to inspect the supported commands.
This command needs no key, changes no GitHub state, and does not start a release.

The implementation combines SOPS-style keyless signatures and provenance with
OpenBao-style review of a draft before publication. No SOPS or OpenBao service
is installed. GitHub's short-lived OIDC identity signs routine releases; the
existing recovery key authorizes that delegation once.

**The [latest stable release](https://github.com/Hikyo-Org/Hikyo/releases/latest)
is published.** The 1.0.0 [publication record](1.0.0.md) records that release's
identity and remaining closeout.
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

Optional pre-check: to learn before tagging whether `VERSION` changes upgrade
compatibility, run the `release-compatibility-check` workflow from main with
that version (`gh workflow run release-compatibility-check.yml --ref main -f version=VERSION`),
after the sync-trust change that records the latest release has merged.
It compares the compatibility declaration generated from main's tip with the one
the highest earlier stable release published, verified through the signed trust.
It must equal that declaration plus exactly one reviewed release-source edge per
engine from that release (see below). Green means that; red means a difference, a
missing or wrong edge, an earlier release that was not signed under the
`github-actions-stable` policy (it needs a recovery bridge, not an edge, and the
check refuses it with exit 1), an unverifiable earlier release or a failed lookup,
and the log says which. Once a promotion has brought unreleased migrations to main, expect
"differs" until a release carries them. It is read-only and tags nothing. Run it
again if main moves before you tag. The workflow reaches main by promotion from
dev, so it exists to dispatch only after a promotion has landed it.

**Release-source edge (required for a patch as well as a minor).** An upgrade
records the installed release by its identity, and the planner only routes an
installed release to the next one through an edge whose source is that exact
release; the `fresh/v1` and `legacy/v1` genesis edges never match. So each release
needs, per engine in `release/compatibility/sources.json`, an edge whose
`source.release` is the previous stable release's identity (version, sequence,
commit, and the SHA-256 of its published `release-manifest.json` and
`upgrade-compatibility.json`) and whose `migrations` and `schema_sha256` are the
ones that release's own declaration carries. The check prints the identity it
expects. Mode is the reviewer's decision (`maintenance`, or `restart` when the
target keeps the same migrations and schema). No command authors a stable edge:
`scripts/release/nightly sources` verifies nightly directories only. Write it by
hand from the verified earlier release's files and have it reviewed.

**Security patch prompt.** After a Dependabot security update merges to main,
the `security-patch-plan` workflow opens one issue, "Security patch release:
maintainer action needed", with the exact commands for the release: the signed
tag, then review, publish, trust synchronization and the Homebrew update. Use
the commands in the latest version of that issue.

- **Step 0: a person queues each security update.** GitHub's built-in token
  cannot add a pull request to the merge queue, so nothing is merged
  automatically. The issue lists every open Dependabot security pull request into
  main that qualifies (alert-linked, opened by Dependabot, bound by numeric id,
  verified Dependabot commits committed by GitHub, not a draft, admission check
  not failed) with its head SHA; open each one and click **Merge when ready**.
  One the merge queue removed after its newest commit is listed separately and
  needs a person to find out why. The plan job needs only read permissions
  (`pull-requests: read`, `checks: read`) for this.
- **Patch or minor.** The plan first tries the next patch. When the generated
  compatibility declaration differs from the latest stable release's (once a
  promotion brings unreleased migrations, every patch is refused), the issue
  proposes the next minor `X.(Y+1).0` instead and lists a checklist of
  prerequisites that block tagging: read the declaration difference (the issue
  embeds it, cut at 200 lines), review the edge modes against the compatibility
  ADR, accept that everything on the commit ships. The release notes are step 2:
  `publish` refuses the placeholder. A minor ships everything on its commit, not
  only the security fix.
- **The release-source edge.** For a patch as well as a minor, the plan checks
  (before generating anything) that the target's `release/compatibility/sources.json`
  carries a sound edge from the latest stable release per engine. When it does not,
  the state is `blocked` and the issue names the missing edge and how to write it.
  No supported command authors a stable edge today (`scripts/release/nightly
  sources` reads verified nightly directories only); this is a known gap. The
  recheck enforces the same check against the commit you tag, and it also runs the
  fallback-channel gate that `release.yml` asserts.
- **The recheck.** The first command, `security-patch-plan.sh recheck KIND
  VERSION COMMIT`, is run with your own login on a clean checkout. It refuses,
  tagging nothing, when the checkout is dirty or not at the planned commit,
  when `main` moved (green tip), when the version is not the kind's successor of
  the latest stable release, when trust is not synchronized, when a newer
  security merge exists, when a security merge cannot be placed in main's history
  or a fixed alert has no linked merge, when the release tools its commands run
  differ between the commit and the main tip, when the release-source edge or the
  fallback-channel record fails, or when a patch would carry a migration or a
  compatibility change other than the release-source edge. It does not regenerate the compatibility
  declaration (the plan did, for the planned commit; that needs scratch
  PostgreSQL) and it cannot see what happens between the recheck and the tag push:
  that window is seconds to minutes, and a merge that lands in it is caught only by
  the ceremony's own build checks. It is a guard on stale instructions, not a lock.
- **When the tip of main is not green**, the issue offers a signed tag of the
  newest green commit that holds every pending security merge (after the signature
  check) instead of `ceremony.sh build`, because `build` can only tag the tip.
- **A failed tag build.** A pushed tag is never moved or recreated. If
  `release-build` fails, inspect the run first. If it already created the draft
  release, delete that draft before anything else: re-running the failed jobs
  cannot create the release while it exists, and an outstanding stable draft stops
  the trust synchronization check. Delete it with
  `gh release delete vX.Y.Z --repo Hikyo-Org/Hikyo --yes` only after
  `gh release view vX.Y.Z --repo Hikyo-Org/Hikyo --json isDraft -q .isDraft`
  prints `true` (the issue prints both for the exact version), and never
  pass `--cleanup-tag`, which deletes the tag. Then re-run its failed jobs: a failure before
  the build (validation, docs, floor-bench, everything `build-artifacts` needs) is
  almost always rerunnable. Only if the byte set cannot be completed safely is the
  version burned, and you then use a new version; the issue repeats this and names
  the failed run. When every `release-build` run of a tag has failed and no
  published release exists for it (a draft does not count: the plan cannot list
  drafts, `recheck` can, and both must pick the same version), the plan skips that
  version (a patch or a minor, each noted on its own) and plans the next one. A
  burned minor tag does not force a minor release: the patch is still planned
  unless the compatibility declaration differs. If a re-run then succeeds, the skip
  clears. A tag whose run succeeded is shown as staged, and a tag with an
  unfinished run, or none, is shown with the run link and the way out.
- **An unlinked fixed alert.** A Dependabot alert fixed after the latest stable
  release without a linked merged pull request on main (a promotion that bumped
  the dependency, or a fix by hand) raises a standing alarm, the plan offers no tag
  command and `recheck` refuses. A linked pull request counts only when it passes
  the same filter as the listing (Dependabot's numeric id, base `main`, a
  `dependabot/` branch). Decide by hand whether the fix is released; to clear it,
  publish the fix in a stable release through the ordinary ceremony (`ceremony.sh
  build VERSION`),
  because the alarm counts only alerts fixed after the latest stable release was
  published.
- **After the release.** The issue closes itself once the security merges are
  released and trust records the latest release. The Homebrew tap update stays a
  manual step (`ceremony.sh homebrew VERSION`), by owner decision of 2026-11-16,
  which replaces the earlier "Homebrew follows the latest release automatically"
  decision for now; no tap credential exists.

The workflow tags and publishes nothing. It reaches main by promotion from dev and
runs from main only, so no issue appears before a promotion has landed it.

1. **Build the draft:** from clean current main, run
   `scripts/release/ceremony.sh build VERSION`. The helper requires green CI
   for the exact commit and synchronized published trust. Review the commit
   and confirm the signed tag push. The release workflow builds and signs a
   complete draft, including image/chart signatures and provenance. Wait for
   `release-build` to succeed. A draft is not official publication.
2. **Review:** run `scripts/release/ceremony.sh review VERSION`. It downloads the
   complete draft, verifies signatures, identity, closed inventory, provenance,
   rollback state and actual OCI signatures, and prints the local directory
   and manifest SHA-256. Replace the draft's placeholder text with the release
   notes on GitHub; publish refuses the placeholder or empty notes. Inspect the notes,
   acceptance evidence and source changes. Keep that exact hash.
3. **Publish:** run
   `scripts/release/ceremony.sh publish VERSION REVIEWED_MANIFEST_SHA256`.
   The helper downloads and verifies again, rejects a changed hash, and asks
   for confirmation of the exact version and hash. The local publication helper
   from clean current main checks the tag, CI, draft, OCI subjects, asset
   inventory and release notes immediately before publishing. It uses your existing administrator
   `gh` login to read immutable-release settings; GitHub's standard workflow
   token cannot read that administration endpoint. It requires immutable
   releases, then redownloads and verifies the public result. Wait for the
   command to pass.
   For stable versions it then publishes the staged `desktop-v<version>` draft
   (see [Desktop app](#desktop-app-release)); it is never marked Latest.
4. **Record published trust:** run
   `scripts/release/ceremony.sh sync-trust VERSION`. It re-verifies the public
   release and opens a PR containing its signed metadata and catalog. On that
   PR's branch, update `CAP-PUBLIC-RELEASE` in `docs/status/ledger.json` to
   name the new version and cite its release URL. Run
   `pnpm --dir docs/site install --frozen-lockfile` (the checker needs the
   site's Node dependencies), then
   `node scripts/ci/check-doc-status.mjs --write`, and push the result as a
   signed commit. Docs CI refuses the trust PR until the ledger names the
   release. Rerunning `sync-trust` keeps that ledger commit and refuses any
   other change on the branch. Review
   and merge it promptly after green CI, then pull main. Merging redeploys
   hikyo.app: the landing page and docs name the new version, and the demo
   is rebuilt with them. That deployment fails unless the live landing page
   links the new version. The next build
   refuses to start until this synchronization is complete, and it checks the
   same landing page link.

There is one human review/publication boundary. With one maintainer it does not
provide independent two-person approval. GitHub account security, protected
workflow changes and careful source review remain part of release security.

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

## Desktop app release

Every stable tag run also stages a `desktop-v<version>` draft (see
[Apple signing](apple-signing.md#desktop-app)). A desktop job failure fails the
run and blocks CLI publication until the failed jobs are rerun. If a check says the
draft is not ready, first make sure the run has finished, then rerun the failed
jobs. If a partial rerun loses the other architecture's outputs (the draft job
cannot find its artifact name or digest), use GitHub's **Re-run all jobs**
instead. Re-running all jobs also re-runs `build-signed-draft`, which cannot
create the `vX.Y.Z` draft while it exists, so first delete that draft as
described under "A failed tag build" in [routine releases](#routine-releases).

Once, before the first stable tag, an administrator runs
`scripts/release/configure-repository.sh Hikyo-Org/Hikyo` to apply the
`desktop-v*` tag creation and immutability rulesets. The desktop publisher
refuses to run until both are active.

`review` and `publish` both check the staged draft first
(`publish-desktop-draft.sh --check-only`: the draft targets the same commit as
the `v<version>` manifest, its inventory signature verifies, and the images match
it). Either command refuses while that fails, and `publish` does so before
anything is public.

Before `publish`, download the staged draft's DMG and verify it as the
[desktop app page](https://hikyo.app/docs/desktop-app/#verify-the-download)
describes. `publish` retries are safe: if `v<version>` is already public it
still publishes the desktop draft, and an already published matching desktop
release is verified and left unchanged.

The legacy `ceremony.sh --offline` publication runs the same check before
`v<version>` can become public, but it does not publish the desktop draft. For a
stable version it prints the follow-up (also when resuming an already published
release), which you must run after it
reports success, from the repository root:

```sh
scripts/release/publish-desktop-draft.sh Hikyo-Org/Hikyo VERSION release/trust
```
