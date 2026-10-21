# Release signing runbook

Stable releases use GitHub OIDC signing, followed by maintainer review of a
signed draft and explicit publication of its exact manifest hash. The laptop
stays online. Routine Cosign signing needs neither USB media nor the long-lived
primary/recovery keys. macOS distribution separately uses Apple credentials.

The [online release guide](online-signing.md) is the operator entry point.
The [Apple signing guide](apple-signing.md) covers Developer ID credentials,
notarized DMGs, and Homebrew tap installation. Stable and nightly macOS artifacts require
Apple signing before final artifact hashing and Cosign signing.
The design combines SOPS-style keyless signing and provenance with OpenBao-style
review before publication. It does not deploy SOPS or an OpenBao server.

**Implementation is available; stable policy activation and 1.0 publication are
still pending.** The owner delayed 1.0 for design work. A successful test or a
signed draft does not mean a release is public.

## One-time trust bootstrap

The existing recovery root remains pinned. Its encrypted local custody was
created online, as recorded in
[the bootstrap record](https://github.com/Hikyo-Org/Hikyo/blob/main/release/trust/BOOTSTRAP.md).
One recovery signature authorizes the stable workflow's exact identity and its
limited authority to add release records. Recovery custody is needed again for
policy changes, revocation and recovery, not routine releases.

After the implementation merges, follow [one-time setup](online-signing.md#one-time-setup).
Do not replace the deployed root or delete the nightly trust files.

## Per-release ceremony

From a clean checkout of current main, after acceptance and design approval:

```sh
scripts/release/ceremony.sh build VERSION
scripts/release/ceremony.sh review VERSION
scripts/release/ceremony.sh publish VERSION REVIEWED_MANIFEST_SHA256
scripts/release/ceremony.sh sync-trust VERSION
```

Wait for the draft build before review, inspect the release notes and evidence,
and wait for local publication verification before syncing trust. Merge the trust PR after review
and green CI before the next release. The [complete guide](online-signing.md)
explains each boundary and retry behavior.

The manifest authenticates the six platform archives, two macOS disk images, eight Linux packages,
installer, standalone verifiers, image/chart digest evidence, SBOMs, compatibility
artifacts and first-party build provenance. Metadata and catalog signatures bind
the release to recovery-authorized policy and monotonic trust state. Hash and
signature checks establish identity and consistency; they do not prove that a
compromised authorized workflow built honest source.

Installers verify the downloaded verifier against its embedded hash before
execution. Preserve the verifier's cross-release state file: removing it loses
remembered rollback protection. An older authorized release requires explicit
historical verification; it cannot be presented as latest.

## Nightlies, legacy releases and incident recovery

[Signed nightlies](https://github.com/Hikyo-Org/Hikyo/blob/main/docs/operations/signed-nightlies.md)
retain their separate workflow policy. Historical keyed releases retain their
primary-key and revocation checks. The
[legacy offline runbook](legacy-offline-signing.md) documents that former flow;
`ceremony.sh --offline` is an explicit compatibility entry point.

The [online guide](online-signing.md#custody-and-recovery) covers Vaultwarden,
independent backups and recovery. The
[stable workflow trust ADR](https://github.com/Hikyo-Org/Hikyo/blob/main/docs/adr/stable-workflow-signing.md)
records authority boundaries and accepted compromises.
