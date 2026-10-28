# Darwin DMG and Homebrew handoff

## Outcome and scope

The macOS release pipeline already creates separate Apple Silicon and Intel
DMGs containing the CLI and license. This change repairs ticket delivery on
the hosted notarization runners and adds an explicitly selected development
Homebrew cask, `hikyo@nightly`. The `hikyo` cask remains stable-only.

These are local changes on `dunky13/darwin-dmg-brew-fix`, based on
`885f460e6425d8c81edb07e411a814386c33dc6f`. No release or tap contents are
changed by editing this branch. Public activation requires the normal PR,
release and tap review steps below.

## Reproduced failures

`brew info --cask hikyo-org/tap/hikyo` reports that
`Casks/hikyo.rb` does not exist. The public tap exists but has no cask; stable
publication was intentionally deferred until the first stable release.

In [nightly run 39913001603, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/39913001603),
Apple accepted both DMGs for source
`cd2352d2cb5967955b02d730f00b2cd4811da5a6`. Apple Silicon stapling and
Gatekeeper assessment passed. Intel stapling failed with
`NSURLErrorDomain -1005` and `Error 68` while contacting Apple's CloudKit ticket
service. The existing fail-closed staging policy omitted both DMGs from the
published nightly. The latest three published nightlies had no DMG assets at
the time of inspection.

## Implementation

- Both notarization workflows use `with-apple-ticket-network.sh`. It follows
  the TCP fallback documented in
  [actions/runner-images#14924](https://github.com/actions/runner-images/issues/14924),
  using a temporary child packet-filter anchor and a separately released
  enablement token. It preserves parent rules, refuses local or self-hosted
  runners, and propagates command and cleanup failures. Signing, Apple
  acceptance, ticket validation, Gatekeeper checks and publication gates remain
  mandatory.
- The cask renderer accepts an explicit `--nightly` mode and requires the
  nightly manifest schema, profile and version format. It binds both DMG
  filenames and hashes to that manifest. Stable and nightly casks declare their
  executable conflict.
- The publisher's `--nightly` mode authenticates the complete bundle with the
  existing recovery-authorized nightly verifier before any tap mutation. It
  only opens a version-specific protected tap PR. It cannot update the stable
  cask or merge a PR. Stable prerelease skipping and existing branch protection
  remain covered by fixtures.
- Installation docs distinguish a missing tap from a missing cask and describe
  direct DMG installation and the separate nightly command. Release and
  governance docs record the selected metadata-only nightly channel.

## Local validation

- Shared fast feedback passed: Go formatting and test compilation, ShellCheck
  0.11.0, actionlint and the documentation content/link check.
- Apple signing, phase transfer, notarization evidence, nightly publication
  outcome and nightly signing workflow owner tests passed.
- Hosted runner/cache policy fixture and stable cask renderer/publisher
  fixtures passed.
- Nightly fixtures cover authentication failure, absent DMGs, channel
  confusion, independent cask identity, integrity failures and refusal to merge.
- Network fixtures cover command failure, absent parent anchor, failed rule
  inspection/loading/enablement, cleanup and token-release failures, and refusal
  on a self-hosted runner. They never change the development host's firewall.
- The installed Homebrew cask loader parsed the generated nightly cask and
  accepted its token, architecture-specific URL, executable and stable conflict.
  This is parser evidence, not an installation from the public tap.

Hosted CI for this branch, the hosted TCP fallback, publication of new release
assets and a Homebrew install from a merged nightly tap cask remain unverified.

## Recovered installer acceptance

Both original signed DMGs for nightly 91 were recovered from authenticated
Actions artifact `12487052290` of run `39913001603`, attempt 1. The downloaded
ZIP matched GitHub's recorded SHA-256
`7be45f3b4db586da1aaf0397aaf9d38f4dd82c7809b9b13e070d6bfb3e0350b4`.
Each unstapled image matched its retained Apple submission receipt before
local stapling. No signing private key was acquired or used.

Both images passed Developer ID identity/team verification, `stapler staple`,
`stapler validate`, Gatekeeper's `Notarized Developer ID` assessment, read-only
mounting, executable architecture checks, and binary/license comparison with
their retained signed tarballs. The Apple Silicon executable reported
`0.0.1-nightly.20261023.91.gcd2352d2` and source
`cd2352d2cb5967955b02d730f00b2cd4811da5a6`. Intel execution and a clean-Mac
first launch were not tested.

The Apple Silicon executable was also copied from the mounted DMG into an
isolated `bin` directory. Its copied signature verified, and `hikyo --help`
worked through that directory's executable search path. No shared application
or executable installation directory was modified.

Final stapled DMG SHA-256 values:

```text
arm64   76ea34c03633a4eb0777a316409079c30fdc090976edcf38c2b7962de475e718
x86_64  60a35448046dc5ca74d41483f1ca9c6adbba6f8d06a32826d1c9c779e3b58fa2
```

These are locally recovered installers for the existing nightly, not newly
published assets or an expansion of that nightly's authenticated manifest.
They are delivered separately in chat. The source repair still requires fresh
hosted proof and a new public nightly before the tap can consume its DMGs.

## Agent Job Queue reference comparison

The user supplied [Dunky13/agent-job-queue](https://github.com/Dunky13/agent-job-queue)
as a working reference. Its
[distribution run 40026971766](https://github.com/Dunky13/agent-job-queue/actions/runs/40026971766)
completed notarization, both `macos-15` and `macos-15-intel` distribution tests,
publication and Homebrew delivery successfully. The compared source is pinned
to `b5dac59a30d6365d2edb37e77ba14ff3a2a94416`.

Its durable CI path uses Apple's Notary API with persisted submission IDs and
ordered payload, installer and DMG checkpoints. This addresses an observed
native `notarytool submit` crash and preserves uncertain-upload boundaries.
It still calls native `stapler staple`; its narrowly classified transient
CloudKit retry applies to ticket validation, not to the stapling command that
failed in Hikyo. The successful reference run does not exercise Hikyo's pending
hosted TCP fallback and is not proof that this branch passes CI.

The useful immediate recovery improvement is its 30-day artifact retention.
Hikyo now retains the signed and finalized handoffs for both stable and nightly
workflows for 30 days instead of one day. The archives exclude private signing
scratch. This keeps the original files available after provider failures,
without importing a second notarization client or changing Hikyo's current
credential separation and publication gates. Fresh ARM/Intel distribution and
Homebrew installation checks in the reference remain useful acceptance
criteria for Hikyo's public activation sequence below.

## Delivery sequence

PR [1052](https://github.com/Hikyo-Org/Hikyo/pull/1052) review identified two
additional gaps. The nightly publisher now downloads both public DMGs and
checks them against the authenticated local manifest before any tap mutation.
Regressions first reproduced publication with a missing public Apple Silicon
image, then covered missing and tampered public files for both architectures.
The installation page now provides separate reader-facing stable and nightly
bundle-verification procedures, with independently authenticated trust,
persistent stable verification state and DMG presence checks before opening
Finder. Mocked/script checks do not establish a new public release or clean-Mac
installation proof.

1. Commit with cryptographic signing and DCO, open a PR, and satisfy the current
   review and CI gates. Merge requires the applicable authorization.
2. Publish a newly authorized signed nightly through the existing protected
   pipeline. Require both DMGs, accepted Apple submissions, successful stapler
   validation and Gatekeeper assessment. Do not alter an existing nightly's
   authenticated inventory to add the recovered images.
3. Follow the [nightly Homebrew publication command](./package-manager-releases.md#nightly-homebrew-publication)
   against its complete public bundle, review the resulting tap PR and merge
   it through the tap's required gates.
4. Verify `brew install --cask hikyo-org/tap/hikyo@nightly` on a clean Mac and
   run `hikyo --help`. Verify direct DMG download separately on both
   architectures. Do not report unavailable Intel execution as passed.

Local installer acceptance, if performed, is separate from immutable release
publication and is delivered in chat without committing machine-specific
artifact paths or HTML documentation.

The R2 standards review caught an empty-input gap in the stable verification
example: an empty state path disables persistence in the verifier. The command
now requires a nonempty state path before downloading. A mocked execution of
the exact documented snippet reproduced the download before the fix and
confirmed that empty input fails before any download afterward.
