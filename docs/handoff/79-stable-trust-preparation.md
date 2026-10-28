# #79 stable trust, custody and distribution preparation

Status: **unsigned proposal; activation HOLD**. Inspected on 2026-10-28 against
`24e198b32a3ffd8d4013207b2f974d76ec49be6f` in `Hikyo-Org/Hikyo`. This is the
entry point for this task's review packet. It does not select the final 1.0
candidate. Select that candidate after required implementation, governance and
trust PRs merge, including the separately authorized activation PR.

The operative decision is [stable workflow signing](../adr/stable-workflow-signing.md).
Use the [online guide](../release/online-signing.md) and
[Apple distribution guide](../release/apple-signing.md) for the existing ceremony.
The accepted online-generated recovery root stays pinned. The former removable
media/offline-primary procedure is not a prerequisite for this amendment. This
packet grants no waiver for other ADR review obligations.

This task owns public trust preparation only. Canonical release-plan, acceptance
ledger and roadmap records belong to the scope/qualification lanes. GitHub/MCP
live acceptance and final combined validation have separate owners. #158, #969,
#1057, #805, #161, #148 and broad #1051 work remain outside this assignment.
Existing shipped-code tests remain intact.

## Observed authority and release state

Read-only GitHub API inspection on 2026-10-28 returned:

| Contract | Observed value and implication |
| --- | --- |
| Repository | Public `Hikyo-Org/Hikyo`, numeric ID `1316165429`, default branch `main` |
| Owner | Organization `Hikyo-Org`, numeric ID `316726515` |
| Workflow | Active `release-build`, ID `329147028`, `.github/workflows/release.yml` |
| Main ruleset `20539346` | Active, no bypass actors; required signatures, PR/resolved threads, deletion/non-fast-forward protection, `ci-queue-required` and merge queue; source policy validator accepts it |
| Tag ruleset `20539249` | Active update/deletion prohibition on `refs/tags/v*`, no bypass actors |
| Tag ruleset `20539250` | Active creation restriction on `refs/tags/v*`, excluding nightlies; repository administrator role can create |
| Immutable releases | `enabled: true`, `enforced_by_owner: false`; publication must recheck |
| Stable publication | No non-nightly SemVer release or draft in the paginated release inventory; `v1.0.0` ref returns HTTP 404 |
| Stable activation | `stable-policy.json` absent locally and HTTP 404 at the public contents API; stable preflight must refuse |
| Existing metadata | Sequence `1`; highest release/sequence null; no approved releases; `pending_release` is `1.0.0`/sequence `1` with an all-zero placeholder hash |
| Existing catalog | Sequence `3`, binds exact metadata, one nightly policy and two bridge digests |

These are dated settings observations, not a tag-mutation probe, a stable
workflow execution or proof of future service availability. No GitHub setting
was changed. The bootstrap placeholder approves neither a source commit nor
release bytes. Successful nightlies authenticate their own channel only.

Public API rechecks, from the repository root with authenticated `gh` and `jq`:

```sh
set -eu
gh api repos/Hikyo-Org/Hikyo --jq \
  '{full_name,id,owner:{login:.owner.login,id:.owner.id,type:.owner.type},visibility,default_branch}'
gh api repos/Hikyo-Org/Hikyo/immutable-releases
gh api repos/Hikyo-Org/Hikyo/actions/workflows/release.yml --jq '{id,name,path,state}'
gh api repos/Hikyo-Org/Hikyo/rulesets/20539346
gh api repos/Hikyo-Org/Hikyo/rulesets/20539249
gh api repos/Hikyo-Org/Hikyo/rulesets/20539250
```

Identity, ownership or protection drift requires explicit review before signing.
The policy generator inherits numeric identities from the authenticated nightly
policy; it does not query GitHub to detect a transfer or rename.

## Exact unsigned delegation

The following JSON block is the byte-for-byte proposal produced by
`scripts/release/stable policy`. Preserve UTF-8, two-space indentation and one
terminal LF. It is embedded here so documentation remains Markdown and no
unsigned file appears at an active trust path.

Policy SHA-256: `0fe2cb70af971a694e15f68ddfa9a92646e0584a360f3df1893da12290f37203`.
The proposed `stable-trusted-root.json` is an exact copy of the existing
`release/trust/nightly/trusted-root.json`, SHA-256
`6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66`.
There is deliberately no production policy signature in this packet.

<!-- unsigned-stable-policy:start -->
```json
{
  "schema": "hikyo.dev/stable-policy/v1",
  "trusted_root_sha256": "6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66",
  "issuer": "https://token.actions.githubusercontent.com",
  "repository_uri": "https://github.com/Hikyo-Org/Hikyo",
  "repository_id": "1316165429",
  "repository_owner_uri": "https://github.com/Hikyo-Org",
  "repository_owner_id": "316726515",
  "workflow_path": ".github/workflows/release.yml",
  "protected_ref": "refs/tags/v*",
  "runner_environment": "github-hosted",
  "require_sct": true,
  "rekor_log_id": "c0d23d6ad406973f9559f3ba2d1ca01f84147d8ffc5b8445c224f98b9591801d",
  "checkpoint_origin": "rekor.sigstore.dev - 1193050959916656506",
  "revoked_manifests": [],
  "minimum_release_sequence": 1,
  "primary_keys_sha256": "e175859df29efce67963df1125ceb53e091ed30874a1d501426df597bdb762cd",
  "nightly_policies": [
    "afaeb19fc0299f700234a74aa0817f77ce0833853b327a9d9d9d126d25d5b04f"
  ],
  "bridges": [
    "0094b5d9ec5bbae50905f4bcf8a4ae6ef23320457c02c4efebdb241d641ab5a1",
    "bc6e5f79252864e2ffb73c0335f17d29f2cdd7cf8b1e57e87f0f5f0cf0c41bfd"
  ]
}
```
<!-- unsigned-stable-policy:end -->

The proposal delegates release-sequence floor `1`, not metadata/catalog sequence
`1`. With the unchanged bootstrap, the first workflow release will advance
metadata `1 -> 2`, catalog `3 -> 4`, and release sequence `0 -> 1`; it clears the
pending placeholder and binds its actual manifest. Setup itself adds only the
three stable delegation files, preserving metadata/catalog and their signatures.
Recompute rather than signing this proposal if those inventories change.

The primary inventory digest is the generator's `PrimaryKeysDigest`, over Go's
compact JSON encoding of the ordered primary entries. Do not substitute a hash
of pretty-printed metadata or `jq -S` output. It preserves `primary-1`, public
key hash, range from release sequence `1`, no upper bound and `revoked: false`.
The ordered nightly and bridge inventories must match exactly. Their presence
does not assert that old bridge-target downloads are available or that an
installation has completed its own backup, restore and writer-stop drill.

The delegation pins these verifier constraints:

| Constraint | Enforcement and review boundary |
| --- | --- |
| Issuer/repository/owner | Exact OIDC issuer and certificate extensions for both numeric IDs and both repository/owner URIs |
| Workflow/ref/source | Exact `.github/workflows/release.yml@refs/tags/vVERSION` SAN, signer/config URIs and digests, source ref and full source SHA; `refs/tags/v*` alone grants no nightly authority |
| Runner | `github-hosted`; workflow signing occurs in `build-signed-draft` on `ubuntu-latest` |
| Fulcio | Exact pinned root JSON, containing public-good Fulcio chains and CT log keys; no freshly fetched root is substituted |
| SCT | Explicit `require_sct: true`; certificate timestamp verification remains required |
| Rekor | Log ID `c0d23d6ad406973f9559f3ba2d1ca01f84147d8ffc5b8445c224f98b9591801d`, origin `rekor.sigstore.dev - 1193050959916656506` |
| Transparency | One entry with signed integrated time, SET, inclusion proof and signed checkpoint; checkpoint origin/tree size and pinned log identity must match; Rekor v1 global and shard-local indices need not be equal |
| Inventory authority | Workflow metadata cannot change primary authority, grant nightly/bridge authority, or replace recovery delegation; catalog binds exact policy bytes |
| Rollback/revocation | Preserve persistent floors for metadata/catalog/highest release and same-height digests; use current authorization and revocation for historical verification |

The pinned Sigstore root contains both historical and current trust material.
Its presence is not evidence that a future Fulcio/Rekor signing request will
succeed. The first separately approved draft must satisfy these exact constraints.
The chart's optional keyless admission policy verifies a smaller set of claims;
full Go verification and the production bundle boundary remain necessary.

## Reproduce and compare without signing

Use Go `1.27.3` from `go.mod`, `jq`, Git and the existing shared heavy queue.
No Node/Python source is changed by this documentation task. No application
persistence is changed; both-engine release qualification remains downstream.
Run from the repository root. The directory below is ignored and contains only
public evidence; choose a fresh directory if it already exists.

```sh
set -eu
packet=.ci-cache/reports/79-stable-trust-recheck
test ! -e "$packet"
mkdir -p "$packet"
awk '/^<!-- unsigned-stable-policy:start -->$/{inside=1;next}
     /^<!-- unsigned-stable-policy:end -->$/{inside=0}
     inside && !/^```/{print}' \
  docs/handoff/79-stable-trust-preparation.md > "$packet/reviewed-policy.json"
agent-heavy -- go run -p 2 ./scripts/release/stable policy \
  --trust release/trust --out "$packet/stable-policy.json"
cmp "$packet/reviewed-policy.json" "$packet/stable-policy.json"
shasum -a 256 "$packet/stable-policy.json" release/trust/nightly/trusted-root.json
scripts/release/require-synced-trust.sh Hikyo-Org/Hikyo release/trust/metadata.json
```

Expected: comparison succeeds, the two hashes match this packet, and the helper
reports published versions/authenticated metadata synchronized. Generation
authenticates the baseline recovery-signed metadata/catalog and validates the
proposal without granting it authority. The sync helper is an inventory check,
not a substitute for signature verification or remote asset verification.

`scripts/release/ceremony.sh status` currently exits nonzero with exactly:

```text
stable release: stable keyless policy has not been recovery-authorized; run the one-time setup
```

That refusal is required. Do not install an unsigned proposal in `release/trust`
to make status pass. Incomplete policy/signature/root sets also fail closed.

## Source binding

The following `shasum -a 256` inventory binds the public trust and principal
producer/verifier inputs at the inspected source. The Git commit above binds
the remaining consumers, fixtures, guides and pinned dependencies. After the
reproduction block above, run this from a clean current-main checkout:

```sh
set -eu
packet=.ci-cache/reports/79-stable-trust-recheck
test -d "$packet"
awk '/^<!-- stable-source-inputs:start -->$/{inside=1;next}
     /^<!-- stable-source-inputs:end -->$/{inside=0}
     inside && !/^```/{print}' \
  docs/handoff/79-stable-trust-preparation.md > "$packet/source-inputs.sha256"
shasum -a 256 -c "$packet/source-inputs.sha256"
```

Expected: every input reports `OK`. Review every changed input; a successful
policy comparison alone does not approve a changed workflow or verifier.

<!-- stable-source-inputs:start -->
```text
6ef6ae8d643359bae441c2708fb0950571edc579a8fef7cd7455d20dbcda266e  release/trust/root.json
1eb7ad2092668b73621c21a1eeb801ed6391bc794df1909abce8e1d45e03a229  release/trust/recovery-1.pub
a91737eeacfb9a756a8c2705e4c43187512762a59bc094a6883bef31436c55e5  release/trust/primary-1.pub
cc7470eb6d2aac727bdaff9f5bb30c5c1fbbbaa3de9d2e8a0b5a9bb77a4c8d09  release/trust/metadata.json
0f4fbb2a8ed206b60d1466e7d5a65888a50f7f98ed9791ebb53b87b62c26938a  release/trust/metadata.sigstore.json
6ed2a82d5d09f618ca3c4ac444eab79dd40ce47915e19e88b069a0be810aa2c3  release/trust/catalog.json
28a9787e9226cd5bcf4f121bcad97cd13bb3b14b1aba1da849a4aceaf4ed58c3  release/trust/catalog.sigstore.json
afaeb19fc0299f700234a74aa0817f77ce0833853b327a9d9d9d126d25d5b04f  release/trust/nightly/policy.json
6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66  release/trust/nightly/trusted-root.json
0094b5d9ec5bbae50905f4bcf8a4ae6ef23320457c02c4efebdb241d641ab5a1  release/trust/bridges/0094b5d9ec5bbae50905f4bcf8a4ae6ef23320457c02c4efebdb241d641ab5a1/statement.json
499e1434b63a6c35acf030e6cd1dfb941774e9dba1bdc52786691784124ba8ab  release/trust/bridges/0094b5d9ec5bbae50905f4bcf8a4ae6ef23320457c02c4efebdb241d641ab5a1/statement.sigstore.json
bc6e5f79252864e2ffb73c0335f17d29f2cdd7cf8b1e57e87f0f5f0cf0c41bfd  release/trust/bridges/bc6e5f79252864e2ffb73c0335f17d29f2cdd7cf8b1e57e87f0f5f0cf0c41bfd/statement.json
a44ab42e2f6123d880f38e604b99633be0151f6f11f95740be80c03f3ce0f712  release/trust/bridges/bc6e5f79252864e2ffb73c0335f17d29f2cdd7cf8b1e57e87f0f5f0cf0c41bfd/statement.sigstore.json
33ad7263a56caa30834a874ad254b5c61ffee7a0bfee3df4bdf9e50a9442f749  .github/workflows/release.yml
2a73d8d6c2f0eca161f5bd299cf3fb826722d0e621cd2a09e4059e8475732145  scripts/release/stable/main.go
60243d0917b93d5493c79d3caec765ebdab3fb40f59bf4eb3c172bb461865f49  internal/releasetrust/stable_keyless.go
aac5a1804d18b34612a5fa82509fb4ae0e7a8f7cb44d86526f2c7653e1ced208  internal/releasetrust/nightly.go
b63c47eed86919de1e138a2db01601594531e515075fc1b48f3dd38d694d9312  internal/releasetrust/snapshot.go
ad0052f0ebdf087decff6ef5abfe8ce28b7f716503ef97625ae49e9e5e549057  scripts/release/stable-ceremony.sh
bd140e9c0b2c358b324faeab8956577536db1ea3cd1d1233295e16c3a5ae88fb  scripts/release/publish-stable-draft.sh
b73586d7248f953ccc60baaafd09cc1ca9c37948beee3f83754f10a86fc0c545  scripts/release/sign-stable-draft.sh
ffb9c395d3784d81237702c2edc7eff2174b5e8d3ae0ba91ac8cfa1bb7ac3fbb  scripts/release/require-synced-trust.sh
c3b194cf5d0fbb884edc898712edb75876b7ab14e4ad90d9e1aefd956ba6d584  go.mod
f4086599664e327ed18ba92807d7b0d1f8ab04b70f2983f2b65d70b3a9877819  go.sum
```
<!-- stable-source-inputs:end -->

## Custody backup, restore and harmless drill

**NOTRUN under this assignment.** No recovery private key, passphrase, Keychain
item or vault attachment was read. Only executable presence of the documented
signer and versions of public tools were checked. The public Cosign `v3.1.3`
binary already exists in the documented bootstrap tools directory. The normal
PATH binary was `v3.1.4`; select the pinned binary explicitly for the ceremony.

After specific custody authorization, the operator must:

1. Confirm the existing `recovery-1` custody using the public fingerprint above
   and the documented bootstrap inventory. Use the existing signer or an
   explicitly approved restored signer implementing
   `sign recovery-1 INPUT OUTPUT_BUNDLE`. Keep passphrases inside protected
   custody; request any missing secret through the secure secret mechanism.
   Do not export plaintext key material into evidence directories or shell args.
2. Save the encrypted key attachment and recovery information in the selected
   Vaultwarden custody. Record vault/item/attachment identifiers only in the
   private operator record. Keeping ciphertext and passphrase in one unlocked
   vault accepts a common compromise domain; it is not independent custody.
3. Create/verify an independently recoverable encrypted backup outside that
   vault instance and failure domain. Include a consistent database snapshot,
   attachment files, configuration and all decryption/restore dependencies.
   Confirm the bootstrap decryption information is accessible without the
   unavailable vault. A database-only export or backup-job success is insufficient.
4. Restore into an isolated, operator-approved scratch target. Prevent outbound
   mail/webhooks and production integration. Never overwrite the running vault.
   Recover the encrypted attachment and passphrase using only the independent
   recovery route. Verify the attachment's private checksum without publishing
   the attachment, checksum, passphrase or identifiers. Record backup receipt,
   restore result and failure-domain separation privately.
5. Invoke the restored custody path for a harmless, domain-labelled challenge,
   verify with the already pinned public key, and require a modified challenge
   to fail. Merely invoking the original workstation signer does not prove the
   backup recovered anything. Use the commands below only after authorization.
6. Retain a redacted receipt with date, operator, backup/restore receipt IDs,
   challenge digest, public key fingerprint, tool versions, positive/negative
   verification status and cleanup result. Keep detailed custody locations
   private. Repeat annually and after custody changes. Clean up only the isolated
   restored target, private scratch and any task-owned temporary credentials;
   retain the independent backup and public drill evidence.

Run this Bash block from a clean repository root only after setting
`HIKYO_RECOVERY_SIGNER` to the approved **restored** signer, `COSIGN_BIN` to the
existing pinned public tool, and `HIKYO_CUSTODY_DRILL_DIR` to a new private scratch
directory. The signer path and private restore target are operator inputs,
not values to commit. The challenge names no policy/manifest schema and carries
no release authorization.

```bash
set -euo pipefail
: "${HIKYO_RECOVERY_SIGNER:?select the authorized restored signer}"
: "${COSIGN_BIN:?select Cosign v3.1.3}"
: "${HIKYO_CUSTODY_DRILL_DIR:?select a new private scratch directory}"
test -x "$HIKYO_RECOVERY_SIGNER"
test -x "$COSIGN_BIN"
test ! -e "$HIKYO_CUSTODY_DRILL_DIR"
umask 077
mkdir -p "$HIKYO_CUSTODY_DRILL_DIR"
ulimit -c 0
printf 'Hikyo recovery custody drill only; no release authority\n%s\n%s\n' \
  "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$(uuidgen)" \
  > "$HIKYO_CUSTODY_DRILL_DIR/challenge.txt"
"$HIKYO_RECOVERY_SIGNER" sign recovery-1 \
  "$HIKYO_CUSTODY_DRILL_DIR/challenge.txt" \
  "$HIKYO_CUSTODY_DRILL_DIR/challenge.sigstore.json"
"$COSIGN_BIN" verify-blob --key release/trust/recovery-1.pub \
  --bundle "$HIKYO_CUSTODY_DRILL_DIR/challenge.sigstore.json" \
  --insecure-ignore-tlog "$HIKYO_CUSTODY_DRILL_DIR/challenge.txt"
cp "$HIKYO_CUSTODY_DRILL_DIR/challenge.txt" "$HIKYO_CUSTODY_DRILL_DIR/changed.txt"
printf 'modified\n' >> "$HIKYO_CUSTODY_DRILL_DIR/changed.txt"
if "$COSIGN_BIN" verify-blob --key release/trust/recovery-1.pub \
  --bundle "$HIKYO_CUSTODY_DRILL_DIR/challenge.sigstore.json" \
  --insecure-ignore-tlog "$HIKYO_CUSTODY_DRILL_DIR/changed.txt"; then
  printf 'FAIL: modified challenge verified\n' >&2
  exit 1
fi
shasum -a 256 "$HIKYO_CUSTODY_DRILL_DIR/challenge.txt" release/trust/recovery-1.pub
```

The keyed recovery bundle format intentionally has no transparency-log entry;
the scoped `--insecure-ignore-tlog` here verifies a pinned-key custody challenge.
It is not a permitted flag for stable workflow, SCT, Rekor or OCI acceptance.
Public existing recovery bundles were used to validate the pinned CLI flags;
the real restored-key drill is still NOTRUN.

## Separately authorized activation sequence

1. Finish review/merge of this preparation PR through the maintainer gate.
   Recheck clean current main, public identities/protections, source bindings,
   policy hash and synchronized trust. Investigate drift and update the packet
   before authorizing different bytes. No final release candidate is frozen yet.
2. Obtain specific authority for the custody restore/drill and, conditional on
   success, the recovery signature on this exact proposal. Stop on missing,
   failed or ambiguous custody evidence. A documented signer is not a backup.
3. With that authorization, run `scripts/release/ceremony.sh setup` from clean
   current main. It regenerates the policy, displays its path/content and asks
   for `authorize stable release workflow`. Before answering, independently hash
   that displayed file and compare with the reviewed digest. Do not feed an
   unattended confirmation. It invokes the protected signer and authenticates
   the resulting delegation before opening the signed/DCO trust PR.
4. Review that PR's exact bytes. Its only active trust additions must be
   `release/trust/stable-policy.json`, `stable-policy.sigstore.json` and
   `stable-trusted-root.json`. Verify full-range commit signatures and GitHub
   Verified status, current-head CI and existing review obligations. If the fixed
   activation branch already exists, inspect/reuse matching verified content;
   divergent content is HOLD, never force-reset or re-sign to bypass the refusal.
5. Stop for the maintainer merge gate. After explicit approval and merge, update
   main and require `scripts/release/ceremony.sh status` to report
   `Authenticated stable keyless release policy`. Preserve verifier floor state.
   Activation is not a published stable release. Only then select and qualify
   the final source candidate after the other required lanes integrate.

## Later distribution boundary and retention

These are future steps needing separate authority; they were not executed here.

1. `ceremony.sh build VERSION` requires clean current main, synchronized trust,
   green exact-commit main CI, an unused version and a cryptographically verified
   signed tag. Its confirmation authorizes pushing that immutable tag, which
   starts the release workflow and the 1.0 API/CLI freeze when version is 1.0.0.
2. The workflow validates/floor-tests/docs-checks, builds canonical artifacts,
   signs/notarizes the two Apple architectures and verifies native installation,
   then builds/signs the complete draft. **Draft preparation pushes versioned
   image and Helm chart OCI subjects to GHCR before GitHub publication.** Those
   registry writes can be externally visible and must be explicitly included in
   draft authorization. A private GitHub draft is not a private build boundary.
3. Retain the original source/tag, candidate and manifest digests, GitHub run ID
   and attempt, artifact IDs/digests, release/asset IDs and inventories, OCI index
   and chart digests/signatures, submitted Apple hashes/UUIDs/logs and final
   stapled download hashes. The latter can differ from submitted hashes. Preserve
   the exact bytes and receipts before their retention deadline.

| Stable workflow handoff | Configured retention |
| --- | --- |
| Canonical `release-inputs-SHA-ATTEMPT` | **1 day** |
| Signed `apple-signed-release-inputs-SHA-ATTEMPT` | **30 days** |
| Finalized `apple-release-inputs-SHA-ATTEMPT` | **30 days** |
| `apple-notary-state-RUN-ATTEMPT-STAGE` checkpoints and `apple-notarization-stable-RUN-ATTEMPT` diagnostics | **90 days**, subject to repository limits |

The longer Apple retention does not preserve every canonical non-Apple input
or establish a completed draft. Do not lengthen retention silently, rebuild an
expired candidate, re-sign to replace missing bytes or recreate/move its tag.
For incomplete runs inspect the original artifact/receipt lineage first.
Existing OCI tags make retries fail closed; unavailable complete original bytes
mean HOLD and a separately chosen new version, not replacement of the old one.
For unresolved Apple uploads use the retained UUID, or strict history/diagnostic
reconciliation when no UUID exists. Never infer a safe resubmission from timeout.

4. `ceremony.sh review VERSION` authenticates the complete downloaded draft,
   provenance, inventory, floors and actual OCI subjects, then prints the exact
   manifest hash. Review acceptance evidence and notes against those bytes.
   First-party provenance does not establish an isolated/reproducible builder.
5. A separate publication authorization names version and reviewed manifest
   digest. Publication re-downloads, rechecks signed tag/exact CI, signatures,
   actual OCI subjects, immutable-release setting and immediate asset inventory,
   then verifies public immutable downloads. An already public retry verifies
   the same bytes without mutation; mismatches remain failures.
6. Reverify public output and run `ceremony.sh sync-trust VERSION`, review/merge
   the metadata/catalog PR and pull main before another build. An outstanding
   stable draft or unsynchronized public release blocks the next sequence.
   Homebrew changes remain a separate authorized downstream PR. Hosted Apple
   receipts/native tests remain distinct from physical clean Apple Silicon/Intel
   download, first-launch and Homebrew lifecycle qualification.

## Validation and remaining gates

All local checks below passed against the inspected source, with only this
Markdown handoff added. No production tooling repair was justified by the audit.

- Public generator authenticated the real metadata/catalog and produced exactly
  the embedded proposal. Stable preflight failed with its required inactive-policy
  refusal. The read-only synchronized-trust helper passed.
- Pinned Cosign `v3.1.3` verified existing public metadata, catalog and both bridge
  recovery signatures. A signature checked against a different public payload
  failed. No production signing was performed.
- Shared-queue batch `2566` passed uncached tests for `internal/releasetrust`,
  `scripts/release/stable`, `internal/selfupdate`, `internal/upgradebundle` and
  `internal/upgradeassembly`, using `-p 2 -parallel 2` and the pinned Cosign
  interop fixture. Synthetic fixtures exercise identity/SCT/log/checkpoint
  substitution, inventory escalation, rollback/equivocation, revocation,
  incomplete delegation, artifact substitution and historical verification.
- Existing `stable-workflow_test.sh`, `stable-ceremony_test.sh` and
  `verify-bundle-stable_test.sh` passed. Their fake GitHub/registry/signer inputs
  establish refusal/retry/dispatch contracts, not external acceptance.
- Complete-diff `fast-feedback.sh --working-tree --plan` selects whitespace only
  for this handoff. The driver and explicit docs preparation read-only consumer
  check passed. Extracted shell instructions pass Bash syntax/ShellCheck checks;
  the signing block was not executed. No rendered UI changed; screenshot evidence
  is inapplicable.

The temporary local verifier executable was removed by its exit trap. Public
proposal/root, read-only observations, validation output and the offline report
companion remain in ignored storage for review. Test fixture directories are
removed by the existing test cleanup paths. No task-owned service or container
remains.

| Gate | Disposition |
| --- | --- |
| Public identity/protection/release inventory | Observed read-only on 2026-10-28; recheck at ceremony |
| Existing public recovery signatures, unsigned generation and local synthetic contracts | PASS within the local/synthetic scope described above |
| Preparation PR current-head hosted checks | Reported on the PR; no final-main release proof claimed |
| Independent cross-provider review | SKIPPED: Anthropic subscription unavailable, no paid/provider substitute authorized; no general ADR waiver |
| Custody backup/isolated restore/restored-key challenge | NOTRUN; specific operator authorization and private custody evidence required |
| Exact policy recovery signature and activation PR | HOLD; authorize only after source/identity recheck and successful custody drill |
| Activation PR merge and authenticated main status | HOLD; separate maintainer merge approval |
| Final-candidate combined/both-engine/live-provider acceptance | NOTRUN in this lane; required after integration |
| Signed tag, OCI pushes, Apple submissions, public release, downstream Homebrew | NOTRUN; distinct later authorizations |

Next focused decision: authorize the isolated custody backup/restore and
harmless `recovery-1` sign/verify drill, followed **only on success** by signing
policy `0fe2cb70af971a694e15f68ddfa9a92646e0584a360f3df1893da12290f37203` and opening its three-file activation PR.
Keep merge, tags, Apple, registry/release publication and Homebrew held.
