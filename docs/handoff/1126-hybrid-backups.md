# #1126: hybrid post-quantum backups, implementation handoff

Written 2026-11-02 for the draft PR that refs
[#1126](https://github.com/Hikyo-Org/Hikyo/issues/1126). It records what this
round implements, what it proves, and what it leaves. It is evidence for one
candidate, not a live status board. The governing decisions are the
[post-quantum migration register](../adr/post-quantum-migration.md#decision-register)
(DEC-01, 04, 05, 07, 08, 11, 12), the
[profiles](../security/pqc/profiles.md) and the
[encryption model banner](../adr/encryption-model.md). **This needs maintainer
security approval before merge.** Nothing here is a claim of strict backup
restore, PQ signatures or release trust.

## Result

An operator can mint age's native hybrid identity (X25519 + ML-KEM-768), configure
its recipient, export, fingerprint, receipt, drill and restore through the
ordinary flows. Unconfigured installs keep writing X25519 exactly as before
(DEC-05): selecting hybrid is configuring a hybrid recipient, and `keygen`
mints X25519 unless `--hybrid` is passed.

```text
internal/crypto/backup    parse/generate/canonicalize/fingerprint hybrid; recipient
                          policy (all one class, <= 32 hybrid, no duplicates, bounded
                          strings); reader stanza allowlist; frozen legacy fixtures
internal/backupreceipt    two fingerprint namespaces; backup-receipt/v2 for hybrid
internal/service          picks the receipt schema from the inventory's algorithm
internal/runtimeconfig    names the violated recipient bound for managed values
internal/app              backup keygen --hybrid; ValidateBackupPolicy after every config load;
                          automatic and unattended upgrades refuse a hybrid policy
                          over X25519 custody
internal/upgradecustody   vault reads a hybrid identity (RecipientOf); tests only
docs/                     operator guide section, manual-upgrade note, this handoff
```

### Behavior

| Item | Behavior |
| --- | --- |
| Encodings | Recipient `age1pq1...`, 1959 characters. Identity `AGE-SECRET-KEY-PQ-1...`, 77 characters. Parsed only by `age.ParseHybridRecipient` and `age.ParseHybridIdentity`; canonical form is age's `String()`. Prefix dispatch, hybrid first |
| Recipient set | All hybrid or all X25519, never a mix (`ErrMixedRecipients`), at most 32 hybrid (`ErrTooManyHybridRecipients`), no duplicate hybrid recipient, no passphrase beside recipients, joined list at most 65536 bytes (`ErrRecipientsTooLarge`). Checked by `Options.Validate`, before any file exists. Recipients go to age unwrapped, so age's own label check still runs |
| Key strings | A recipient over 2048 characters or an identity over 256 is refused before any bech32 work. Recipient errors name the position, never the value (an operator can paste a private identity where a recipient belongs); this also changes the X25519 recipient error, which used to echo the value |
| Reader rule | `ExtractTo` parses every stanza before any key is tried. A hybrid stanza beside any other type (X25519, scrypt, ssh, plugin, unknown) is `ErrMixedHybridStanzas`, for every unlock mode. A hybrid stanza must have one argument that decodes to 1120 bytes and a body of 32 bytes (`ErrMalformedHybridStanza`). scrypt beside anything stays `ErrMixedStanzas`. Reader bounds are unchanged: 256 stanzas, 256 KiB header |
| Fingerprints | `age-hybrid-mlkem768x25519-sha256:` + SHA-256 of the canonical recipient. `age-x25519-sha256:` is untouched. `backup.ParseFingerprint` is the one parser |
| Receipts | X25519 inventories keep `backup-receipt/v1`. Hybrid inventories use `backup-receipt/v2`. `Receipt.Validate` binds the schema to the algorithm: v1 never carries a hybrid fingerprint, v2 never a classical one, a mixed inventory is refused, a hybrid inventory is capped at 32. The upgrade attestation format is unchanged |
| Keygen | `hikyo backup keygen [--hybrid]`. Same disclosure path, same file mode, destination reserved before the key exists |
| Startup validation | `app.ValidateBackupPolicy` runs after each `config.Load` or `LoadBootstrap` (server, migrate, upgrade, and the `backup export` and `upgrade-export` verbs) with the same refusal the export makes. It acts only when a hybrid recipient is present; an X25519-only list is untouched. `internal/config` is a leaf package (`boundary_test.go`: config may import nothing under `internal/`), so the check cannot live there. Restore, status, reconcile, drill and keygen deliberately skip it, so recovery works on a host whose variable is wrong |
| Automatic and unattended upgrades | Both export to the custody vault's own recipient, which enrollment always mints as X25519, and keep the full-data archive (`automatic_upgrade.go`, `unattended_evidence.go`). With a hybrid `HIKYO_BACKUP_RECIPIENTS` and an X25519 vault identity they now **refuse before any export** (`ErrHybridPolicyClassicalCustody`): `sudo hikyo upgrade` before the journal and before the service stops, the container coordinator before its journal, scratch and maintenance fence and only on runs that would export (a start with nothing pending still boots). The error points at the manual route. X25519 or empty policy behaves as before. Hybrid vault generation is not built |
| Custody | `RecipientOf` accepts either identity, so a vault record holding a hybrid identity opens, rewraps, exports to its recipient and recovers. The vault still generates X25519 (see remaining work) |

### Measured bounds

| Quantity | Measured |
| --- | --- |
| Hybrid recipient / identity | 1959 / 77 characters |
| 32 hybrid recipients, joined with commas | 62,719 bytes, under the 65,536-byte managed value cap |
| Header with 32 hybrid recipients | 49,846 bytes, under the 256 KiB reader bound |
| 33 hybrid recipients | refused by count, no output |
| Custody ciphertext holding a hybrid identity | under the 16 KiB custody bound (asserted) |

## Evidence

Candidate: see the PR head. Base: `26f112bde` (stacked ADR amendment branch).

Validated code revision: `1cbb662c2` (later commits on the branch are
documentation only). Every Go run used `go test -p 2 -parallel 2` through the
shared job queue. PostgreSQL was a task-owned PostgreSQL 18.6 cluster on a
private port with `fsync=on` (Hikyo refuses to boot on `fsync=off`; an earlier
run used `fsync=off` and failed every PostgreSQL leg at boot, which was a fixture
error, not a result, and was rerun). Three queue submissions lost their client
output (orphaned job, daemon timeout, client killed at its wall-clock limit);
those are infrastructure events, not test outcomes, and the results below come
from complete runs.

| Check | Result |
| --- | --- |
| `go build ./...`, `go vet` on the touched packages, `gofmt -l` | clean |
| `internal/crypto/backup` (hybrid round trip, wrong key, cross-algorithm, tamper, truncation at chunk boundaries, mixed set, 32/33 bound, byte bound, malformed keys, duplicates, reader rule, malformed stanza, fingerprints, frozen legacy fixtures) | ok |
| Fuzz, 15 s each: `FuzzHybridExtractTo` (about 87k execs), `FuzzRecipientPolicy` (about 38k execs) | no finding; corpus not committed |
| `internal/backupreceipt` (v1 historical bytes, v2 binding, mixed/unknown/relabeled/oversized inventories, archive authentication for both key classes incl. other-algorithm identity) | ok |
| `internal/upgradecustody` (hybrid identity through rewrap, export and recovery) | ok |
| `internal/runtimeconfig`, `internal/config`, `internal/boundary` | ok |
| `internal/service` (receipt format by algorithm, interrupted export and retry, both engines) | ok, PostgreSQL executed |
| `internal/upgradegate` (full package, both engines, populated and nightly process tests) | ok, PostgreSQL executed |
| `internal/isolation`: `TestBackupRestoreDrill{SQLite,Postgres,HybridSQLite,HybridPostgres}`, `TestRestoreDrillVerb{...}` same four | all PASS, none skipped |
| `internal/app` targeted (`Backup|Restore|UpgradeDrill|Keygen|Disclosure|Export|Drill`): CLI export and restore at every diagnostics level for both key classes, keygen, refusals before any output, restore of a mixed header, `TestUpgradeDrillHybridBothEngineRecovery` (sqlite and postgres, with and without a secret), `TestUpgradeDrillHybridRefusesCustodyMismatch` | ok |
| `internal/store` (`Backup|Restore|Export|Manifest|Upgrade`), `internal/store/migrate` | ok |
| `internal/lint` (full package, reviewed raw-SQL pins after the update) | ok (2893 s under queue load) |

Not run: the full `internal/app` and `internal/isolation` packages (only the
backup, restore and drill tests above), host environment-string limits, a second
independent age implementation.

### Repair round (review of `d05e6e13c`)

Same queue, `go test -p 2 -parallel 2`, PostgreSQL 18.6 with `fsync=on`, `cosign`
available so the signed CLI drill ran. No test skipped. Added coverage:

| Item | Test |
| --- | --- |
| H1 refusal, unattended entry | `TestUnattendedPackagedEnrollmentUpgradeAndIntermediateCredential` (sqlite and postgres, signed child process): hybrid policy over the X25519 vault refuses with no journal, no `backup-*` export and the healthy source unchanged; a later start with nothing pending boots under the same hybrid policy |
| H1 refusal, automatic entry | `TestCustodyBackupPolicyAgainstRealVault` against a real `CreateLocal` vault, the same function `RunAutomaticUpgrade` calls. The entry point itself needs Linux, root and systemd and was not run (NOTRUN) |
| M1 gate verifiers (`upgradegate/evidence.go:31` legacy and `:48` ledger, `preflight.go:150,183`) | `TestGatePopulatedProcessCrashRoutes` for x25519 and hybrid, both engines, four crash routes each; asserts the export wrote the matching receipt schema |
| M1 `VerifyEvidence`, `VerifyLegacyEvidence`, `CheckEvidenceArtifacts` | `TestEvidenceAuthenticatesExactBytesAndCurrentAuthority`, `TestLegacyProposalNeverMasqueradesAsLiveLedger`, `TestEvidencePreflightReadsExactArtifactWithoutCopyOrAuthority` for both classes; `TestSignedEvidenceRefusesSchemaAlgorithmMismatch` (validly signed evidence with a v1 label on hybrid fingerprints, a v2 label on X25519 ones, or a mixed inventory is refused) |
| M1 production evidence loader (`app/upgrade_gate.go:132`) and CLI `upgrade-export` | `TestAutomaticUpgradePackagedNightlyRoute/hybrid-success`: the packaged binary's real `upgrade-export --json`, a real drill, then the real migrate process reading the evidence directory |
| M1 CLI `upgrade-export`, `upgrade-drill` | `TestUpgradeExportDiagnosticsPreserveJSON` (receipt schema per class), `TestUpgradeCLISignsActualDrill` (real `cosign`, x25519 and hybrid) |
| L1 | `TestValidateBackupPolicyRefusesUnsatisfiableHybridSets` |
| L4 | `TestPolicyErrorPredicateAndHybridSelection`; one `backup.IsPolicyError` is used by the upgrade fingerprints, managed configuration and `ValidateBackupPolicy` |

Results (code revision is the PR head at this round): `internal/upgradegate`
`TestGatePopulatedProcessCrashRoutes` ok; `internal/app` targeted set ok (1535 s);
`internal/boundary`, `internal/crypto/backup`, `internal/config`,
`internal/runtimeconfig`, `internal/backupreceipt` ok; `internal/lint` (full package, 4052 s under queue load) ok.

Acceptance criteria, in the issue's order:

| # | Criterion | Status |
| --- | --- | --- |
| 1 | Hybrid generation, validation, canonicalization, lookup, export, restore, protected input | PASS for CLI, service, receipt and drill paths; PARTIAL for the vault (reads, does not generate) |
| 2 | Algorithm-aware versioned fingerprints and receipt schemas; consumers updated | PASS for fingerprints, receipt v2, the gate's legacy and ledger verifiers, `CheckEvidenceArtifacts`, the production evidence loader, CLI `upgrade-export` and `upgrade-drill` (all now exercised with a v2 receipt); PARTIAL for documented offline tooling (none exists in the tree) and for the packaged nightly release path, which a hybrid policy refuses by design |
| 3 | PQ-profile backups contain only hybrid recipients; reject classical, mixed, duplicate, malformed | PARTIAL: mixed, duplicate, malformed and unsupported are refused and labels are preserved; there is no selectable profile, so a classical-only set is still accepted by design (DEC-05) |
| 4 | Zero-recipient, exclusivity, scrypt bound, header and stanza counts, final-chunk authentication, safe file creation | PASS (existing tests unchanged and passing, plus new ones) |
| 5 | About 2000-character recipients, real header size at the supported count, deterministic refusal | PASS: 1959 characters, 49,846-byte header at 32, 33 refused before output |
| 6 | Readers open frozen legacy and hybrid archives; old readers fail clearly; migration docs and preflight set the minimum reader | PARTIAL: frozen fixtures, clear old-reader errors (run against the pre-change code) and docs are done; no preflight enforcement of a reader floor |
| 7 | Round trip, wrong key, corruption, truncation, unknown algorithm or schema, mixed, bounds, interrupted export, restart, custody mismatch | PASS except restart: interrupted publication and retry are tested, a process restart is not |
| 8 | Complete backup/restore and upgrade-custody drills on SQLite and PostgreSQL | PASS for the ordinary drill, the manual-route upgrade drill and the gate with a hybrid identity on both engines. PARTIAL for automatic upgrade custody: it stays X25519 and a hybrid policy refuses it, so no hybrid vault drill exists |
| 9 | Document wrapper scope, passphrase entropy, rewrap limits | PASS |
| 10 | Non-destructive migration, fresh hybrid receipts, rollback demonstration, docs, exact-revision evidence | PARTIAL: procedure and fresh receipts are covered; the rollback is documented, not demonstrated end to end |
| | Strict backup restore provenance | HOLD (E-19) |

## Compatibility

* Frozen legacy archives (`internal/crypto/backup/testdata/legacy`, generated by
  the code before this change, pinned by SHA-256) open under the new reader: an
  X25519 archive and a passphrase archive.
* A historical `backup-receipt/v1` receipt (literal bytes in
  `artifacts_test.go`) validates unchanged and classifies as X25519.
* Older readers fail clearly on new objects. Run against the pre-change code: a
  hybrid identity gives `backup identity is not a valid age X25519 identity`, an
  X25519 identity against a hybrid archive gives `identity did not match any of
  the recipients`, and a hybrid recipient is refused at upgrade export. A v2
  receipt is `invalid backup receipt` to a v1-only parser (by source reading).
* **Minimum capable reader**: the release containing this change, for every host
  that may restore a hybrid archive or verify a v2 receipt. No preflight
  mechanism enforces that floor; it is documented only (remaining work).
* Cross-tool: the `age` 1.3.2 CLI opens an archive Hikyo wrote to a hybrid
  recipient, Hikyo opens one the CLI wrote, and the canonical recipient equals
  `age-keygen -y`. That is the same upstream library, so it shows format
  conformance, not an independent implementation (qualification row stays
  NOTRUN for a second implementation).

## Key retention and rollback

* Keep every X25519 identity that wrote a retained archive. Nothing here
  re-encrypts old archives; there is no rewrap tool.
* Keep the hybrid identity for as long as any hybrid archive is retained.
  Rewrapping cannot revoke a copy already held.
* With a hybrid policy `sudo hikyo upgrade` and the container upgrade refuse
  (above); upgrade by the manual route with a hybrid recipient and a separately
  held hybrid identity, or switch the policy back to X25519 first.
* Rollback to X25519 recipients is a configuration change; hybrid archives already
  written stay readable only with the hybrid identity and a capable binary.
* The 16-byte age file key bounds archive payloads and metadata at 128 bits. The
  custody record that holds root escrow is still age-scrypt sealed (DEC-12 option a).

## Remaining work and holds

| Item | Status | Why |
| --- | --- | --- |
| Strict profile: refuse classical recipients where a PQ profile is selected | NOT BUILT | No profile selector exists in this branch. Hybrid-only is reached by configuring hybrid recipients, and a mix is refused. A selected-profile switch belongs with the profile work |
| Strict backup restore provenance (signed record, receipt signer, pin, quarantine, hold release) | HOLD (E-19) | Not designed here. No integrity claim for an archive restored without a signed receipt |
| PQ signatures on receipts and attestation | HOLD (#1132) | P-256 stays |
| Hybrid identity generation inside the custody vault | NOT BUILT | `create` and enrollment have no selection input. A vault record holding a hybrid identity works; nothing creates one |
| Vault holds one `backup_identity` | NOT BUILT | Keeping an old identity beside a new one needs its own escrow or a vault format change |
| Vault re-seal on root rotation | HOLD (#1154) | Out of scope; age-scrypt kept |
| Secret-scanner rule for `AGE-SECRET-KEY-PQ-1...` | NOT BUILT | Checked with `Ruleset.Scan`: the built-in scanner finds **neither** an X25519 nor a hybrid age identity, because the vendored `age-secret-key` rule is not on `internal/scanning/rules/allowlist.txt` and so is not compiled. A rule for either class means an allowlist and generator change under the secret-scanning ADR (64-rule ceiling), not an edit. The migration policy's statement that only the PQ form is unmatched is incomplete |
| Preflight minimum-reader floor | NOT BUILT | Documented only |
| `scripts/ci/floor-acceptance.sh:27` and `scripts/ci/check-config-rollout-kind.py:1129` (`prepare_age_identity`) | NOT UPDATED, NOTRUN | Both require `AGE-SECRET-KEY-1` and use `age-keygen` without `-pq`. They are classical-custody runbook and rollout-drill checks; neither was run here and neither is changed |
| Archive header versus receipt inventory | REQUIREMENT for the E-19 restore-provenance work | `AuthenticateArchive` (`backupreceipt/archive.go:85-86`) checks that the held identity's fingerprint is listed in the receipt and that the ciphertext digest matches, but never compares the archive header's stanzas with the inventory. An archive can carry extra same-class recipients the receipt does not list. Provenance must bind the header's recipient set to the signed inventory; nothing is built here |
| `backup-receipt/v2` | OPEN QUESTION for the owner | Kept as built. The owner rules on it separately; no ADR is edited. The issue asks for versioned schemas; the locked ADR body names v1 and its banner promises a namespace, not a schema bump |
| `RunAutomaticUpgrade` entry point | NOTRUN | It requires Linux, root and systemd. The refusal there is covered through the shared check against a real vault; the unattended entry (`runPreparedUnattended`) is covered through a real child process |
| Hybrid on the packaged nightly release upgrade (`TestPackagedNightlyReleaseUpgrade`) | NOTRUN | Its custody is the automatic vault, which a hybrid policy now refuses by design |
| Offline tooling for receipt fingerprints | NOT FOUND | No separate offline fingerprint tool exists in the tree; the library is the only parser |
| Host environment-string limits for a 62,719-byte value | NOTRUN | Also `hostupgrade` passes one 1959-character recipient per argv entry |
| Independent second age implementation | NOTRUN | Only the upstream CLI was run |
| Locked ADR text naming `backup-receipt/v1` | NOT EDITED | `signed-upgrade-compatibility.md` names v1 in its body; its 2026-11-02 banner promises an algorithm-bearing namespace, not a schema bump. Hybrid receipts use `backup-receipt/v2` (the issue asks for versioned schemas). Confirm through the amendment route |

## Next authorized step

Maintainer security review of this draft, then the owner decides the profile
selector and the vault generation path. Merge, release and deployment are not
authorized by this PR.
