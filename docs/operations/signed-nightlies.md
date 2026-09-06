# Automated signed nightlies

Nightlies use GitHub OIDC and Cosign under the recovery-authorized `nightly/v1`
profile. The workflow embeds production trust and compatibility declarations,
signs the complete payload inventory, and verifies it with the runtime verifier
before publication. A missing trust root or signature fails the run before a
tag or release is created. No offline project private key belongs in Actions.

This automation requires the public trust bootstrap below. The repository's
`.example` files are deliberately invalid placeholders; they do not authorize
production releases.

## One-time public trust authorization

Complete the existing [offline trust bootstrap](../release/signing.md#one-time-trust-bootstrap)
for the project recovery and primary keys. Use its protected removable-media
and offline signing procedure. Nightlies do not require a stable release to
exist, but they do require the recovery-signed metadata and primary public key
inventory. Keep stable release counters unchanged when publishing a nightly.

While online and before mounting private key media, use the pinned Cosign
version to retrieve Sigstore's authenticated TUF targets (`cosign initialize`
with its embedded public-good TUF root). Preserve the authenticated
`trusted_root.json` target as `release/trust/nightly/trusted-root.json`.
Its cache location includes the TUF repository name and may include the target
hash; do not substitute an unauthenticated HTTP download. See
[Cosign initialization](https://github.com/sigstore/cosign/blob/v3.1.3/cmd/cosign/cli/initialize/init.go).

Copy `release/trust/nightly/policy.json.example` to `policy.json`. Independently
confirm the repository and owner numeric IDs, and replace its three placeholders
with the SHA-256 of those exact trusted-root bytes, the authorized Rekor v1 log
key ID, and the exact signed checkpoint origin. Retain `require_sct: true`.
Review the Fulcio/CT/Rekor roots and their validity ranges. The workflow explicitly
uses `https://rekor.sigstore.dev`: TSA-only Rekor v2 bundles do not satisfy this
profile. A service/key transition requires a reviewed policy and recovery
authorization; never turn verification off to clear a failed run.

Prepare a directory containing only the reviewed policy JSON and a separate
directory containing the current bridge statement JSONs (empty initially).
Generate the catalog with an advancing catalog sequence:

```sh
scripts/release/create-upgrade-catalog.sh release/trust/metadata.json 1 \
  /review/nightly-policies /review/bridge-statements /review/catalog.json
```

Use the next sequence if a catalog already exists. Offline, recovery-sign the
exact catalog with the [catalog ceremony](upgrade-artifacts.md#offline-trust-snapshot).
Publish only public files at these paths, preserving their signed bytes:

```text
release/trust/root.json
release/trust/<root-named recovery public key>
release/trust/<metadata-named primary public keys>
release/trust/metadata.json
release/trust/metadata.sigstore.json
release/trust/catalog.json
release/trust/catalog.sigstore.json
release/trust/nightly/policy.json
release/trust/nightly/trusted-root.json
```

Run `go run ./scripts/release/nightly preflight` against those files before
merging the public bootstrap. Review/merge is the trust activation boundary.
The existing nightly GitHub App still owns tag/release publication. The workflow
uses `id-token: write` for signing and obtains the App's short-lived write token
only after the verification and startup gates pass.

## What each run proves

The checkout must equal the workflow's OIDC source SHA and have exact-commit
green main CI. Both actual database engines produce the compatibility stamp.
GoReleaser builds all six platform archives and eight native Linux packages with
that stamp and the same production root. The signed manifest binds all twenty
payloads: those fourteen executables/packages, checksums, binary provenance,
compatibility, policy, Sigstore roots and release notes. Only the manifest and
its signature envelope are excluded from their own inventory.

The runtime verifier checks the full inventory, certificate identity, signed
integrated time, SCT and log inclusion/checkpoint. CI then assembles a runtime
bundle and boots/restarts the packaged Linux binary in production mode on both
engines. After the first signed nightly, it also runs the previous published
binary, populates an encrypted secret, exports and drills a backup, signs local
operator evidence, upgrades with the candidate binary, and checks readiness,
secret readability and restart. The first signed release has no authenticated
predecessor, so only its fresh-install route is automatically exercised.

## Revoke one published nightly

Every nightly bundles the policy it was built under, and a GitHub release is
immutable, so a revocation cannot be written into the release itself. Online
verifiers (`hikyo upgrade`, `hikyo update`, the first-use bootstrap and the
release preflight) therefore also fetch the currently published
`release/trust/nightly/policy.json` and refuse any manifest it lists in
`revoked_manifests`, even when the release's own bundled policy is clean.

To revoke a nightly, add its `release-manifest.json` SHA-256 to
`revoked_manifests` in the reviewed policy, regenerate the catalog with the next
sequence while keeping the earlier policy digest listed, and recovery-sign the
catalog. Keeping the earlier digest lets other nightlies from that policy period
keep verifying; dropping it refuses all of them at once. Publish the policy and
catalog together. The offline server boot gate keeps using the bundled policy;
a revoked release that is already installed remains inspectable and is never
selected again as an upgrade target.

## Bridge existing unsigned installations once

A pre-ledger database remains `legacy/v1`, identified by its inspected schema
and exact migration inventory. It is never relabeled as a signed source release.
An ordinary nightly declaration cannot admit it. The first hop needs a
recovery-signed `hikyo.dev/legacy-nightly-bridge/v1` statement for its engine,
exact legacy schema/migrations, signed target identity and target policy digest.
It also needs the installation's fresh backup/drill attestation.

After downloading and verifying the first signed nightly, generate public
proposals from its reviewed legacy schema candidates:

```sh
go run ./scripts/release/nightly legacy-bridges \
  --directory /downloads/first-signed-nightly --out /review/legacy-bridges
```

The command creates unsigned digest-named statements for both engines. During
the offline recovery ceremony, sign each exact statement:

```sh
cosign sign-blob --yes --new-bundle-format=false --tlog-upload=false \
  --use-signing-config=false --key /offline/recovery.key \
  --bundle /review/bridge.sigstore.json /review/legacy-bridges/STATEMENT_SHA256.json
```

Place the statement and signature at
`release/trust/bridges/STATEMENT_SHA256/statement.json` and
`statement.sigstore.json`. Add every approved statement digest to a new,
recovery-signed catalog. Include its exact signed target release in the manual
runtime bundle. This is a second offline authorization after the target manifest
exists; subsequent nightlies under the same policy need no project-key ceremony.
Never reuse a signature after reformatting or changing the statement.

## Download and install

For the supported Linux systemd/SQLite deployment, `sudo hikyo upgrade` performs
download, bundle assembly, local encrypted operator custody, backup restoration
to scratch, migration and verified restart. Its first-use bootstrap also covers
older binaries without the command. See the [one-command upgrade instructions](https://hikyo.app/docs/upgrades/).
The remaining steps describe manual preparation for other deployment types.

In clients carrying the new trust stamp, `hikyo update check` verifies and stages
the complete signed nightly and assembles its runtime bundle in the CLI state
directory. It preserves the installed executable and reports both paths. Unsigned assets, rollback,
equivocation or missing trust refuse. Older clients cannot safely bootstrap this
trust through their old binary-only self-update flow; perform the first
installation with independently authenticated public trust and downloads.

Assemble all required signed nightly downloads, including the installed source,
every intermediate release and the target:

```sh
scripts/release/assemble-nightly.sh /operator/trust /operator/public/bundle \
  /downloads/source /downloads/target
```

The trust directory uses the public layout above, including every currently
authorized bridge. The assembler authenticates every payload and bridge,
publishes a new directory durably and refuses an existing output. Use repeatable
`--nightly` together with `--release` on the underlying
`scripts/release/assemble-upgrade` command for mixed-profile routes.

Follow [manual verified upgrades](manual-upgrades.md) to provision the runtime
operator pin and installation state, stop all writers, export/drill the source,
stage public evidence and run the exact target. Install each verified route hop
explicitly. Preserve the bundle for every boot. A legacy bridge is project
authorization of a route, not a substitute for the instance's backup proof.
