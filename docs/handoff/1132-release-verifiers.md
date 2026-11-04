# #1132: PQ-capable release trust verifiers, implementation handoff

Written 2026-11-03 for the draft PR that refs
[#1132](https://github.com/Hikyo-Org/Hikyo/issues/1132), stacked on
[#1158](https://github.com/Hikyo-Org/Hikyo/pull/1158) (#1126, hybrid backups). It
is evidence for one candidate, not a live status board. Governing decisions: the
[register](../adr/post-quantum-migration.md#decision-register) (DEC-02, 03, 07,
08, 11, 14, 15, 17) and [migration](../security/pqc/migration.md#release-trust-and-install-paths).

**Scope: readers, verifiers and nonproduction fixtures only.** No signer goes
live, no real key exists, nothing under `release/trust/` changed, and nothing on
any shipping path calls the new code or requires a PQ signature. DEC-14 keeps
the ML-DSA recovery key custody on **HOLD** until the owner names it; E-11 (the
PQ trust-anchor bootstrap) and E-15 (nightly track, `sudo hikyo upgrade`, image
replacement) limit every `pqc-auth` claim. **The PQ release-authenticity claim
stays HOLD.** Security approval: not given. Independent adversarial review is
required; cross-provider review is SKIPPED by explicit owner instruction
(2026-11-02) and is not CLEAN.

## Candidate

| Item | Value |
| --- | --- |
| Base | `origin/dunky13/pqc-1126-hybrid-backups` at `4bad16b59` (PR #1158) |
| Code candidate | `fb915616b` (verifier, fixtures, tests; round one was `24d827709`, repaired after independent review). Later commits are documentation only and touch no Go file |
| Toolchain | Go 1.27.3 darwin/arm64, `crypto/mldsa` from the standard library, no new module |
| Files | `internal/releasetrust/pq.go`, `pq_root.go`, `pq_verify.go`, `testfixture/pq.go`, `pq_*_test.go` (including fuzz targets), `internal/selfupdate/pq_coexistence_test.go` |
| Not touched | `parsePinnedKey`, `Snapshot.Digest()`, `SnapshotFloor`, every v1 schema and decoder, `release/trust/`, the installers, `scripts/release/`, `internal/lint/testdata/reviewed_pins.json`, macOS signing |
| Base | The base branch has moved since this was opened; it was not merged because this diff does not need it |
| Other owner | PR #1140 (`release/sync-trust-1.0.0`) edits `release/trust/{catalog,metadata}*` and `scripts/release/nightly/`. This change touches none of those, so there is nothing to overwrite or merge there |

## What was built

A PQ-capable form of the trust documents, as readers and verifiers. Everything
below is new schema or new code; a v1 reader refuses it instead of ignoring it.

| Piece | Behavior |
| --- | --- |
| PQ root, `hikyo.dev/trust-root/v2` | Extends the v1 root: the same P-256 recovery key plus exactly one ML-DSA-65 recovery key, a generation and the profile `pqc-auth/1`. `NewPQPin` refuses other schemas, other algorithms or sizes (ML-DSA-44/87, P-384 under the P-256 label), key-file digest mismatch, a non-canonical PEM, a reused id, a third member and unknown members. The required set is derived from these pinned bytes only |
| Signature set, `hikyo.dev/pq-signature-set/v1` | Sidecar of `{algorithm, key_id, signature}`. Closed algorithms `ecdsa-p256-sha256` and `mldsa65`; at most 4 entries, 64 KiB, exact ML-DSA length; one entry per required algorithm. Missing, duplicate, extra, unknown, wrong-key-id or invalid entries refuse the object |
| Signed bytes | One input for all algorithms: length-prefixed label, profile, pin digest, generation, ordered `(algorithm, key id)` suite, then kind, channel, version, subject, source commit, sequence, policy digest and the SHA-256 of the exact bytes. The ML-DSA context is the label (`hikyo/release-trust/<kind>/v1`). A signature under suite {A,B} does not verify as {A}. `PQSigningInput` exposes the input so a future signer cannot drift |
| Key identity | `<algorithm>:` plus SHA-256 over length-prefixed domain, algorithm and canonical SPKI |
| Roles | **Recovery set** (P-256 and ML-DSA, both required): PQ keyset, stable policy, nightly policy, bridges, and metadata and catalog on the recovery-signed track. **Release set** (one maintainer-held ML-DSA key from the keyset): stable manifest, artifacts, and metadata and catalog under a keyless delegation. Each role is refused in the other's place. CI holds neither. The role for metadata and catalog is chosen from the authenticated metadata's signer field, a document claim: it can only move the requirement between two sets that both need an ML-DSA signature, and the delegated branch also needs the recovery-signed policy |
| Keyset, `hikyo.dev/pq-release-keys/v1` | Recovery-signed, sequence-only list of release keys with release-sequence ranges and a revoked flag. Exactly one non-revoked key may cover a release sequence; a revoked key cannot sign for sequences it once covered. Rotation and revocation are new sequences, each needing the full recovery set |
| Verifiers | `VerifyPQKeyset`, `VerifyPQSnapshot` (runs after the unchanged `VerifySnapshot`), `VerifyPQRelease`, `VerifyPQArtifact` (exact bytes, name, version, channel, sequence, source, policy), `VerifyPQBridge` (after the unchanged `VerifyBridge`). Historical keyed releases and nightlies are refused as having no PQ signer (E-12, E-15) |
| PQ floor, `PQFloor` | A separate record: format, profile, generation, pin digest, and for keyset, metadata and catalog a sequence, a digest and the pin it was verified under, plus the highest PQ-verified release sequence and its pin. Sequence-only; refuses rollback, equivocation, a lower generation, the same generation with different pin bytes and a newer format than the reader knows; an empty record is refused on decode. `AuthenticityClaim` reports true only for a record that says keyset, metadata, catalog and a release each verified under exactly this pin, so a generation change needs every stage again. It does not authenticate the record: a hand-written or restored file can state anything (E-17). A refusal that follows an authenticated keyset (for example a revocation of every key covering the release) returns the keyset-advanced floor beside the error, and a caller persists it so an older keyset cannot be replayed |
| Closed errors | `ErrPQUnsupported`, `Malformed`, `Bound`, `Scope`, `Signature`, `Missing`, `Rollback`. No key, signature or payload bytes in any message |

### Departures and additions beyond the proposal (flagged for the reviewer)

- The proposal says PQ keys for stable signing come from a maintainer-held key at
  the local publish step. The **keyset document, the role split and the separate
  PQ floor are the implementer's shape** for that sentence, chosen so that no
  existing schema, `SnapshotFloor` or `parsePinnedKey` changes. The decided
  inputs do not say whether the release key may equal the recovery key; the
  fixtures and verifier require them distinct. Owner decision needed.
- `SnapshotFloor` is persisted in the upgrade control row and the shared
  `release-trust.json`; a new member there is a migration on both engines and
  moves the lint pins. The PQ floor is therefore a **sidecar record**
  (`PQFloorPath` names `release-trust-pq.json` beside the classical file) and
  **persisting it on a real host is NOT BUILT** (E-17). Tests persist it in a
  disposable directory only.
- Policy bindings use only the policy's own fields: the stable policy binds its
  `MinimumReleaseSequence` and the nightly policy the constant sequence 1, both
  with no version, source commit or catalog sequence. Recovery keys therefore sign
  a policy once, not every release (tested with a second release). These are
  bindings, not document sequence floors. Only keyset, metadata, catalog and
  release carry floors. Metadata and catalog sets do bind version and source and
  are signed per release by the release role (the recovery set on the legacy
  track, as the recovery key signs those documents today).
- **Departure from migration.md ("Any trust change ... requires every required
  algorithm of the current pinned set").** A higher PQ generation passes the floor
  check with no signature, and `NewPQPin` verifies none: the root is whatever the
  operator installs. That is exactly the E-11 limit and not an authenticated
  transition. Because signatures bind the pin digest and generation, a new
  generation invalidates every existing set, not only the keyset: after a recovery
  replacement the keyset, metadata, catalog, policies, bridges, the manifest and
  every artifact set must be signed again under the new pin, and hosts still on
  the old pin fail against the same sidecar names (one name per document). Old
  releases' sidecars stop verifying under the new pin. The fixture's `Resign`
  hides this cost. Registered here for the owner.
- A revocation is remembered by the keyset floor, but revoked key identities are
  not: a later keyset signed by the full recovery set can list a revoked key
  again. "Never re-enable a revoked key" is a runbook rule, not something the
  verifier enforces. Registered gap.
- The P-256 recovery key now signs two inputs: the raw JSON of the legacy bundle
  and the PQ input. Domain separation is structural: every PQ input begins with a
  4-byte length prefix and a `hikyo/release-trust/` label, and a JSON document
  cannot begin with a zero byte.
- **Expiry is not implemented.** `releasetrust` has no clock and the migration
  policy proposes no cutoff, so "expired" is "superseded by sequence". A host
  that is served stale-but-current evidence indefinitely (a freeze) is not
  detected. Registered as R-13 in the exception record, not closed.

## Inventory (ticket item 1)

Every row says where the trust lives today, what the PQ reader does to it in
this change, and what stays externally dependent. "Reader" means a verifier now
exists in `internal/releasetrust`; **no row below calls it**.

### Roots, keys and delegations

| Item | Today | PQ status here | Externally dependent |
| --- | --- | --- | --- |
| Pinned root `release/trust/root.json`, 373 bytes, hash pinned in `install/install.sh.in` and stamped into binaries (`cmd/hikyo/main.go`, `internal/buildcompat/trust.go`) | v1 schema; `ValidateRoot` | Reader: v2 root and `NewPQPin`. Not stamped anywhere. v1 readers refuse v2 | HTTPS carries the script that carries the hash (E-11) |
| Recovery key `recovery-1.pub` (P-256) | Signs root-level documents: metadata and catalog on the legacy track, stable policy, bridges. Also checked with `openssl` in the first-use bridge script (hash pinned there) | Reader: recovery set = this key plus an ML-DSA key | ML-DSA custody **HOLD** (DEC-14, E-18) |
| Bootstrap primary `primary-1.pub` (P-256) | Signs historical keyed stable releases; the chart keeps `primary.pub` for keyed admission policy | None. Historical verification only (E-12) | None |
| Operator keys | Per-instance P-256 attestation key in the custody vault (`operator.age`); the vault signs attestations in `internal/upgradecustody/vault.go` (`Sign`, around line 295) and checks them against the pin; pinned by `backupreceipt.PinOperator`; rotation is verified against the current P-256 pin (`backupreceipt/operator.go`) | None. Operator attestation stays classical (E-16). The signature-set envelope is generic but unwired here | Operator host custody |
| Stable workflow delegation `stable-policy.json` + `stable-trusted-root.json` (6,787 bytes) | Recovery-signed; pins workflow, repository, ref, SCT, the primary-key digest, and must equal the catalog's nightly and bridge inventories | Reader: recovery set must sign the policy; CI gains no authority over it | Sigstore Fulcio/Rekor (E-03) |
| Nightly policy `nightly/policy.json` + trusted root | Recovery-authorized by digest in the catalog | Reader: recovery set signs the policy. No PQ signer for nightlies themselves (E-15) | Sigstore |
| Bridges `release/trust/bridges/*`, `BRIDGES.md` | Recovery-signed, digest-listed in the catalog | Reader: `VerifyPQBridge` after `VerifyBridge`; release role refused | None |
| Metadata `metadata.json`, catalog `catalog.json` | Recovery-signed (legacy) or keyless under delegation | Reader for both tracks; recovery set or release set by track | Sigstore on the keyless track |

### Verifiers, installers and update paths

| Path | How it verifies today | PQ status here |
| --- | --- | --- |
| `internal/releasetrust` (`VerifySnapshot`, `VerifyStable`, `VerifyNightly`, `VerifyBridge`, `VerifyKeySignature`) | Maintained Sigstore verifiers and pinned-key verify; the pinned-key parser refuses ML-DSA by design | PQ verifiers added beside them; the parser is unchanged and a test pins its refusal |
| `scripts/release/stable` (`verify`, `prepare`) and the pinned `hikyo-release-verifier_<platform>` binaries | Go verifier that `install.sh` delegates to for stable | **Not wired.** It would need a reviewed `--pq` input and would refuse unlisted release files today |
| `install/install.sh.in` (fresh stable install) | curl over HTTPS; pins root, verifier and library hashes; cosign and jq; delegates to `verify-bundle.sh` | Unchanged. Shell verifier has a closed artifact-kind list, so a manifest listing a new kind would be refused by it: PQ sidecars must live outside the manifest |
| `install/upgrade-nightly.sh` (first-use bridge, Linux systemd, root) | Embedded Python plus `openssl dgst` against the pinned P-256 key, and cosign with a pinned hash for the manifest | Unchanged, and deliberately given no ML-DSA reader (the target OpenSSL is the distribution's; 3.0 has none). Registered R-10 |
| `internal/selfupdate` stable (`trust.go`, `stable_prepare.go`) | Pinned stamped root and recovery key, floor in `release-trust.json` | Unchanged; tested to ignore sidecars and refuse a v2 root without touching state |
| `internal/selfupdate` nightly and `sudo hikyo upgrade` (`app/automatic_release.go`) | Nightlies only, keyless verification | No PQ signer (E-15) |
| Unattended image replacement (`app/unattended_upgrade.go`) | The replacement image verifies itself with its own stamped trust | No PQ signer (E-15) |
| Gates (`upgradegate/gate.go`, `app/upgrade_gate.go`) | Refuse when the pinned root digest differs from the persisted release root | Unchanged. Replacing the v1 root bytes is refused; a second stamped value beside them is the only shape that leaves the digest alone, and the stamped slot does not exist |
| `internal/updater` and `install/updater/*` | Remote apply disabled; the host adapters are five-line scripts that refuse every phase | Closed by retirement. No trust decision is made there |
| Homebrew cask, OS packages, OCI image, Helm chart, Kyverno policy | SHA-256 in the cask; packages only through the bundle; cosign on OCI payloads; keyed or keyless Kyverno | Classical exceptions R-04 to R-06 |
| Apple signing and notarization | `docs/release/apple-signing.md` policy | **Not touched** (R-07) |
| Offline ceremony `scripts/release/ceremony.sh` and `stable-ceremony.sh` | Legacy offline signing ceremony and the stable ceremony that fronts it; hold or produce the recovery and primary signatures | Unchanged. No ceremony produces a PQ signature; the runbook is the plan (not executable) |
| `scripts/release/nightly`, `scripts/release/assemble-upgrade`, `scripts/release/compatibility` | Prepare and verify public nightly evidence, assemble the offline layout the runtime gate reads, and build compatibility declarations; none signs | Unchanged; they consume `releasetrust` v1 only |
| `install/compose/server.yaml`, `server-unattended.yaml` | The image is `HIKYO_SERVER_IMAGE`, a verified `repository@sha256` digest the operator supplies; the compose file performs no signature check itself | Unchanged. The operator's verification of that digest is the only authenticity step, and it is classical |

### Signed receipt and preflight consumers

| Consumer | Today | PQ status here |
| --- | --- | --- |
| `backupreceipt` (`VerifyEvidence`, `VerifyLegacyEvidence`, operator pin and transition) | P-256 operator signature; `backup-receipt/v1` and `v2` from #1126 | Unchanged. PQ signatures on receipts and attestations stay HOLD (E-16, E-19); no schema was relabelled |
| `upgradegate/evidence.go`, `preflight.go`, `upgradecompat`, `upgradebundle`, `upgradeassembly` | Consume `PinnedTrust`, `SnapshotFloor`, `VerifiedRelease` | Unchanged |
| `devupgrade/evidence.go` | Development-only fixture signer | Unchanged |
| `scripts/security/release-evidence` | Reads release evidence | Unchanged |

## Bridge: what the legacy channel gives, and whether a bridge can exist (item 4)

**What the legacy authenticated channel guarantees.** Against an attacker who
cannot forge P-256 ECDSA, Sigstore certificate and log signatures, or HTTPS to
the first hop: integrity of the downloaded bytes, fail-closed refusal of unknown
or missing schemas, and refusal of rollback and equivocation by persisted
sequence floors (a local claim, not authority).

**What it cannot guarantee against a quantum-capable attacker.** All of it is
forgeable: recovery-signed metadata, catalog, policy and bridges; the keyless
bundle; and the first-hop HTTPS that carries the install script and the root
hash. A forger can therefore also forge the very statement that introduces a PQ
key, and can present its own ML-DSA key as "the recovery key". Hosts at the
pre-PQ generation cannot reject forged classical documents, and the bootstrap
script cannot read ML-DSA at all.

**Can a bridge reachable from a shipped version exist? Not one that closes the
claim, so the claim stays HOLD.**

1. *What is reachable.* A shipped version already accepts a new binary through
   its classical path. That binary can carry a PQ pin. Doing so by **replacing**
   the v1 root bytes is refused: the gate compares the root digest to the
   persisted release root (`upgradegate/gate.go`, `app/upgrade_gate.go`). Carrying the pin as an **additional** stamped value leaves the v1 root
   digest unchanged, but no such slot exists (`buildcompat` and
   `selfupdate.Config` hold two values), so it needs a build and config change
   that is not built here.
2. *What it would protect.* The first hop is authenticated only classically.
   A host that completes it before an attacker can forge P-256 holds a PQ pin it
   can trust afterwards, provided it keeps the persisted PQ floor and the
   installed binary performs the next verification. A host that completes it
   after a forger exists cannot tell. That is trust on first use over a
   classical channel (E-11).
3. *What would still be open.* Every other replacement path would need to
   require PQ: nightly (no PQ signer), `sudo hikyo upgrade`, image replacement
   with its own stamped trust, the first-use bridge script and the chart policy
   (E-15). Third-party, offline and older copies cannot be observed (E-17).
4. *No new bootstrap protocol is invented.* The only non-forgeable introduction
   is an operator-installed root from an independently verified source (an
   offline reinstall), which is recovery, not a bridge.

Clients without the reader: given a PQ root, the shipped stable installer
**fails closed and changes no state** (tested). Given only sidecars they ignore
them and keep accepting classical-only releases; Hikyo cannot force them
forward. The recovery path for the first case is the offline reinstall above.

Minimum-version bridge: the reviewed minimum reader version cannot be named
until a release carries these readers. It is not built.

## Measurements (item 5, DEC-17: baseline only, no thresholds)

Machine: Apple M2 Pro (12 cores), darwin/arm64, Go 1.27.3, a shared host under
other load. Command: `go test -run '^$' -bench 'PQ|Legacy' -benchtime=300x -count=3
./internal/releasetrust/`. Fixtures use a test-local Sigstore CA, so the legacy
number is a relative figure, not a production latency.

| Measurement | Result |
| --- | --- |
| ML-DSA-65 public key | raw 1,952 B; SPKI DER 1,974 B; PEM 2,726 B; seed-only PKCS#8 54 B |
| ML-DSA-65 signature | 3,309 B |
| Signature set file, ML-DSA only (release role) | 4,592 B |
| Signature set file, P-256 plus ML-DSA (recovery role) | 4,831 B |
| v2 root / v1 root (fixture names) | 696 B / 373 B (`release/trust/root.json`) |
| Keyset with one release key | 307 B |
| Existing signed documents (repo `release/trust`) | metadata 796 B, catalog 421 B, stable policy 1,104 B, trusted root 6,787 B, each classical envelope 118 B |
| Scale of one real artifact set (nightly 105) | 24 assets, 724,830,312 B; Linux archive 46,243,319 B; manifest 5,371 B; keyless bundle 9,509 B; compatibility declaration 146,804 B |
| Added by manifest-level PQ signing | about 4.6 KB per release |
| Added by signing every artifact | about 4.6 KB times N; the installer comment says a stable release lists about 36 artifacts, so about 165 KB (estimate) |
| Verify keyset (P-256 + ML-DSA, key parse) | 0.26 to 0.30 ms, 78 KB, 366 allocations |
| Verify snapshot, PQ sets over metadata, catalog, policy, keyset | 0.76 to 0.80 ms, 234 KB, 746 allocations |
| Legacy `VerifySnapshot` on the same fixture | 1.57 to 1.68 ms, 713 KB, 7,984 allocations |
| Verify release manifest set | 0.145 ms, 50 KB, 97 allocations |
| Verify one artifact signature (small payload) | 0.16 to 0.22 ms, 83 KB |
| SHA-256 of a 46,243,319 B artifact (separately timed) | about 19 ms |

NOTRUN: memory and latency on the target Linux hosts and arm64 Pi-class devices,
verification of full-size archives through the real path, and any figure from a
production-sized release.

## Tests and evidence by class

Local (this host, Go 1.27.3):

| Check | Result |
| --- | --- |
| `go test -p 2 -parallel 2 ./internal/releasetrust/... ./internal/selfupdate/... ./scripts/release/stable/... ./internal/backupreceipt/...` | pass |
| `go test -p 2 ./internal/boundary/...` | pass |
| `install/upgrade-nightly_test.sh`, `scripts/release/render-installer_test.sh`, `verify-bundle-stable_test.sh`, `create-upgrade-catalog_test.sh` | pass, unchanged (frozen-legacy byte compatibility) |
| `go test -fuzz` on `FuzzNewPQPin`, `FuzzDecodePQFloor`, `FuzzVerifyPQKeyset`, 15 s each | no failure; a short local run only, CI fuzzes every target |
| Other checks | see "Final checks" below |

Hosted CI, real-provider checks, runtime acceptance, deployment: **NOTRUN or not
yet read**; the PR description states what was readable when it was opened.

Negative evidence (all in `internal/releasetrust/pq*_test.go`): stripped
classical or ML-DSA signature; suite downgraded to classical only; release key
signing the keyset, policy or a bridge; recovery key signing in the release role;
document bytes altered; a set bound to another sequence, policy, channel,
version, source or name; a catalog set replayed as metadata; unknown, ML-DSA-87,
duplicate, oversized or short signatures; substituted key id; every `NewPQPin`
substitution listed above; rollback and equivocation of keyset, metadata, catalog
and release; a newer floor format; a lower pin generation; a different pin at the
same generation; truncated and substituted artifacts; a revoked key; two keys
covering one sequence; a v1 reader refusing the v2 root; the pinned-key parser
and operator key id refusing ML-DSA; a classical-only snapshot that every v1 check
accepts failing the PQ requirement.

Lifecycle in a disposable directory (`pq_lifecycle_test.go`). **This is a
test-local sequencer, not the product**: `host.apply` in the test file calls the
exported verifiers in a plausible order and persists two files with the shipping
lock and atomic-rename pattern. It is a test double for the installer, so it
proves what the verifiers do when sequenced that way, not what any installer
does. The "bridge" is modeled by passing the pin as an argument; no real
delivery of a pin exists. The fixtures are generated per run with nonproduction
keys; they are **not frozen metadata**. Frozen legacy metadata and real-path
tampering of an installer are **NOTRUN**. Cases: legacy update with no PQ state;
the first PQ claim; a subsequent update at a higher sequence; a crash before and
between the two floor writes, each converging on retry; a torn (half-written)
PQ floor refusing, which proves a truncated file is refused and not that every
damaged or missing file is (a missing file is a zero floor by design, and a
restored file can state anything, E-17); bad ML-DSA signature, stripped sidecars
and random bytes each leaving both state files byte-identical, with the error
class asserted; rollback refused by both floors; release-key rotation; replay of
the older keyset refused; a revocation that is remembered, so replaying the
older keyset with fresh metadata the revoked key signed is refused (without the
returned floor the same replay is accepted, which a control assertion shows);
offline recovery by replacing the release key (never un-revoking it) and
installing a higher-generation root, after which the retired pin is refused.

What the shipped code does run: `selfupdate.Installer.verifyStable` against the
delegated legacy fixture, with PQ sidecars published beside the release. It
ignores them, writes only the classical floor and leaves a sentinel PQ floor
untouched. A build stamped with a v2 root refuses before writing state, but the
root is stamped at build time, so no shipped client is ever handed a v2 root:
that case describes a build that does not exist. Real old clients keep accepting
classical-only releases (R-11). The shell installer suites run unchanged and
**prove nothing about PQ**: those scripts never see a sidecar or a v2 root. They
show only that nothing legacy moved.

NOTRUN: systemd and root install of `upgrade-nightly.sh`; Docker, Helm and
Kubernetes install targets; Homebrew; OS packages; macOS; any real publication;
frozen legacy metadata and real-path tampering of any installer or updater; a
second signed manifest and artifact set (the lifecycle reissues metadata and
catalog at a higher sequence; one test publishes metadata for a later release but
signs no second manifest); a long fuzz campaign.

## Acceptance checklist

| # | Criterion | Status | Why |
| --- | --- | --- | --- |
| 1 | Inventory, externally dependent items | PARTIAL | Above, plus [release trust exceptions](../security/pqc/release-trust-exceptions.md). Independent review found omissions that were then added; completeness has not been independently confirmed |
| 2 | Security review before enabling a signer | PARTIAL | Design and adversarial tests by the implementer only. No signer is enabled. Maintainer security approval **not given**; independent review follows |
| 3 | Readers and verifiers before requiring; reviewed minimum-version bridge; legacy guarantees stated; unsupported clients fail with a recovery path | PARTIAL | Readers DONE (unwired). Guarantees stated. Bridge **HOLD**: not closable (E-11, E-15). Unsupported-client refusal is **NOT ACHIEVABLE for shipped clients**: the root is stamped at build time, so they are never handed a v2 root and keep accepting classical-only releases (R-11). Only a hypothetical build is shown to refuse. No minimum version can be named yet |
| 4 | Independent authority for primary/recovery versus CI; unknown roots and schemas fail closed | DONE in the verifier | Role split and closed schemas tested. CI gains no keyset, policy, bridge or root authority |
| 5 | Verify PQ artifact signatures on every official in-scope path | NOT BUILT / HOLD | `VerifyPQArtifact` exists; no official path calls it, by direction. Paths listed in the inventory |
| 6 | Algorithm-aware encodings and evidence schemas, no mislabelled history | DONE for release trust; HOLD for receipts | New identifiers only; `backup-receipt/v1` and `v2` and operator attestation untouched (E-16, E-19) |
| 7 | Reject stripped, substituted, stale, expired or revoked, mixed channels, partial downloads, rollback below the format floor | PARTIAL | Tested, including a remembered revocation that blocks keyset replay. Not done: **expiry** (sequence-only; no clock; R-13), revoked key ids being remembered across a later recovery-signed keyset, and rollback on a binary built before the floor, which cannot refuse (E-17) |
| 8 | Lifecycle across supported installation targets in disposable fixtures | PARTIAL | A test-local sequencer over the verifiers, plus the real stable installer's verification step ignoring sidecars. Not frozen metadata, not real-path tampering, no other target. Shell suites prove only that legacy is unchanged. Everything else NOTRUN |
| 9 | Measure sizes and latency; update tests and signing workflow fixtures; secrets in custody | PARTIAL | Measured. No signing workflow was changed because no signer exists. No secret was created; fixture keys live in test memory |
| 10 | External limitations as exceptions with owners; macOS gate | PARTIAL | R-01 to R-13 drafted with owner, trigger and ticket, but the record is "drafted for review, not operative" until the owner fills peer, version, expiry and trigger. macOS not touched |
| 11 | Maintainer-reviewed runbook and exact artifact verification evidence | PARTIAL | [Runbook](../operations/pqc-release-signing-runbook.md) drafted, marked not executable, not yet maintainer-reviewed. No release artifact was verified |

Amendment banners landed on `signed-upgrade-compatibility` (release identity and
trust only; backup custody keeps its #1155 banner), `stable-workflow-signing`
(which has no grilling issue; it was decided by the owner on 2026-09-08 through
PR #714, so the banner cites the originating PR), `system-architecture` and
`oss-mechanics`. Each says it becomes operative only when this implementation
merges and that it activates no signer or root. Tickets #638, #22 and #33 are
open for these. The banners now carry the proposal text verbatim (784
characters), with the implementation's design choices in a separate note marked
not owner-locked. The proposal has no text specific to `system-architecture` or
`oss-mechanics`, so the shared text is used there unchanged; the owner may want
different wording. The proposal block in the migration ADR is a pointer that
cites the commit where the text stood.

## Final checks

| Check | Result |
| --- | --- |
| `go test` of `internal/releasetrust`, `internal/selfupdate` at `fb915616b` | pass |
| `internal/lint` locally | **not run again** by instruction. The round-one local run timed out at 40 minutes under host load at `TestArtifactDisposableDatabaseProtocolsRejectDrift` and is not a pass |
| Hosted `repository invariants (boundary, lint, CI policy)` | read for the new head and recorded in the PR description |
| `go test -p 2 ./internal/boundary/...`, `fast-feedback.sh` (base `dunky13/pqc-1126-hybrid-backups`, Node from `.nvmrc`), `check-commit-signatures.sh`, `check-dco.sh`, `check-doc-status.mjs --check` | rerun for the new head; results in the PR description |

## Key retention and rollback

No key was created, so nothing needs retention today. Test keys exist only in
test process memory. Rollback of this change is a revert: no state, schema or
trust file was written. When a signer exists: do not lower a persisted floor,
re-enable a revoked key or destroy a retained key; see the runbook.

## Next authorized steps (each needs its own authority)

1. Maintainer security review of this verifier; owner answers on release key
   versus recovery key and on signing granularity.
2. Owner names ML-DSA recovery custody (blocks the runbook).
2a. Owner sets DEC-17 latency, CPU and memory thresholds from the baseline above
   before #1132 is accepted; none is proposed here.
3. A reviewed `--pq` input for `scripts/release/stable`, which also needs the
   unlisted-sidecar rule changed.
4. A second stamped PQ pin slot and a persisted PQ floor on real hosts (both
   NOT BUILT; the floor needs the owning ledger to store it).
5. A nightly PQ signer decision (E-15).
6. Name the minimum reader version only after a release ships the readers.
