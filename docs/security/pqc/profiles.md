# Target profiles, parameters and policy, draft v0.6

Status: **owner decided, not operative; maintainer security approval HOLD**. The
owner accepted the [register](../../adr/post-quantum-migration.md#decision-register) (`DEC-nn`) as
recommended on 2026-11-02. Text that says "proposal" follows that decision and
takes effect only when the register's conditions are met.
Inventory IDs `Inn` are in [inventory](inventory.md); rollout is in
[migration](migration.md). "Candidate" means named for review, not selected.

Basis tags: **[src]** read from Go 1.27.3, age v1.3.2, `filippo.io/hpke` v0.4.0
or Hikyo source at the pin; **[arith]** arithmetic on [src] values; **[NIST]**
NIST IR 8547 (final, October 2026; PDF text extracted locally 2026-11-02);
**[est]** estimate, not measured.

## Two claims, defined separately

| | (a) Quantum-resilient confidentiality | (b) Quantum-resistant authenticity |
| --- | --- | --- |
| Protects against | Harvest now, decrypt later: recording ciphertext or a key exchange today for a future quantum computer | A future quantum adversary forging a signature, proof, certificate, command or artifact, or substituting a key |
| Needs | Every independent path to the plaintext rests on a PQ-resistant KEM combined with a classical one; symmetric layers at or above the [symmetric floor](#symmetric-floor-and-assumptions) | Every signature the verifier accepts includes a PQ signature, and the anchor that vouches for the PQ key is itself PQ-authenticated |
| Does not imply | Authentic peers, certificates, commands or artifacts | Confidential past traffic or backups |
| Limited everywhere by | Copies already harvested | The update path: a forged classical update can replace the binary that does the verifying, so every `pqc-auth` claim is conditional on E-11 and E-15 until #1132 closes them, including surfaces whose delivery does not depend on #1132 |
| Time pressure | Now, for long-lived secrets (horizon `H` in the inventory) | Before a quantum forger exists; trust roots live long, so bootstrap starts early ([migration](migration.md#release-trust-and-install-paths)) |

A hybrid key exchange over a classical certificate chain is (a) only. A PQ
signature under a root authorized by a classical signature is (b) only to the
extent of that bridge. Claims are per surface, direction and peer version, never
a "PQ-ready" badge.

## Required strength and compliance obligations

| Topic | Proposal | Basis |
| --- | --- | --- |
| KEM strength | ML-KEM-768 (category 3, 192-bit classical strength); NIST recommends it as the default | [NIST] Table 5 |
| Signature strength | ML-DSA-65 (category 3). ML-DSA-87 (category 5) is a candidate for roots only if DEC-02 option (b) is chosen. ML-DSA-44 excluded: category 2, reduced to 1 if the random bit generator gives under 192 bits | [NIST] Table 3 |
| Hybrid | Result at least as strong as the stronger part. NIST: hybrids are expected to be temporary and add complexity; a composite KEM can be FIPS 140-validated if one input is approved | [NIST] 3.2, 4.1.2 |
| Obligations | The issue says NIST milestones are not a universal deadline for Hikyo deployments; this plan adds no 1.0 gate. NIST will deprecate quantum-vulnerable public-key algorithms after 2030 and disallow them after 2035 for its standards; federal high-value and high-impact systems have earlier dates under EO 14412 and OMB M-26-15 (key establishment 2030-12-31, signatures 2031-12-31) | [NIST] 1, 4.1 |
| Validation | PQ algorithm use is not FIPS validation ([below](#pq-use-versus-fips-validation)) | Go `lib/fips140` |

## Candidate parameter sets

DEC-01 and DEC-02 select hybrid X25519 + ML-KEM-768 and ML-DSA-65; the other rows are
for comparison. Not operative. Sizes in bytes.

| Role | Candidate | Public / ciphertext or signature | Private form | Surface | Basis |
| --- | --- | --- | --- | --- | --- |
| KEM, files | age `HybridRecipient`: ML-KEM-768 + X25519 via HPKE `MLKEM768-X25519` (id `0x647a`), label `postquantum` | Public key 1216 (1184 + 32); HPKE encapsulation 1120 (1088 + 32); wrapped file key 32 | 32-byte seed | I07, I08, I21 | [src] age `pq.go`, hpke `pq.go`, `crypto/mlkem` |
| KEM, TLS | `X25519MLKEM768` (CurveID 4588); Go also has `SecP256r1MLKEM768` (4587), `SecP384r1MLKEM1024` (4589) and pure `MLKEM1024` (514) | Client share 1216, server share 1120 | n/a | I09-I11, I13, I14, I30 | [src] `crypto/tls` |
| Signature | ML-DSA-65; ML-DSA-87 only under DEC-02 (b); ML-DSA-44 excluded | pk 1952 / sig 3309 (65); 2592 / 4627 (87) | 32-byte seed | I21-I26 | [src] `crypto/mldsa` |
| Not candidates | SLH-DSA (not in the Go standard library); pre-hash ML-DSA and external-mu; composite single-signature schemes | n/a | n/a | n/a | [src] |

Signing mode: pure ML-DSA with a context string through `Sign`, which ignores
its `io.Reader` and reads 32 random bytes from the module's DRBG, so signatures
are hedged; `SignDeterministic` (zero randomness) is for test vectors only
[src `crypto/internal/fips140/mldsa`].

## Encodings, fingerprints, domain separation, identifiers

Candidates (DEC-07). Existing identifiers keep their meaning:
`age-x25519-sha256:` is never reused for another algorithm.

| Item | Candidate rule | Basis |
| --- | --- | --- |
| Backup recipient | Native `age1pq1...` (1959 characters) and `AGE-SECRET-KEY-PQ-1...` (77 characters), parsed only by `ParseHybridRecipient` and `ParseHybridIdentity`, canonicalized by `String()` | [arith] 1216 bytes = 1946 five-bit groups + 6 checksum + `age1pq1`; 32-byte seed = 52 + 6 + `AGE-SECRET-KEY-PQ-1` |
| Stanza allowlist | For **new exports and for objects claimed PQ-protected**, a strict reader accepts exactly one stanza type, `mlkem768x25519`, with one argument and a 32-byte body; any other type (X25519, scrypt, ssh, plugin, unknown) is refused, because plugin stanza types are arbitrary. This does not govern restore of frozen classical archives, which is [migration](migration.md#mixed-paths-and-archive-provenance); default compatibility restores are unchanged (DEC-05). Recipients are passed to age natively; wrapping a recipient to hide `WrapWithLabels` (age documents this as an unsafe bypass) is banned | [src] age `pq.go`, `age.go` |
| Backup fingerprint | `age-hybrid-mlkem768x25519-sha256:` + hex SHA-256 of the canonical recipient string | [src] `recipients.go` pattern |
| Other fingerprints | Existing length-prefixed domain-separated form with algorithm and parameter set added and a new domain version; a changed fingerprint moves `DestinationID` and old targets fail closed (existing behavior) | [src] `sealedhook.go` |
| ML-DSA public key | SPKI with OIDs 2.16.840.1.101.3.4.3.17/.18/.19; text form `mldsa65:` + hex | [src] `x509.go` |
| ML-DSA private key | Seed-only PKCS#8 only (Go rejects expanded and seed+expanded forms); sealed under the existing hierarchy | [src] `x509/pkcs8.go` |
| Key identity | `SHA-256(LP(domain) ‖ LP(algorithm) ‖ LP(canonical public key))` with an algorithm-bearing prefix | proposal |
| Domain separation | Labels `hikyo/<surface>/<purpose>/v<N>`. A label belongs to one surface and purpose and is never reused for another; the algorithm identifiers appear inside the signed bytes. The label is also the ML-DSA `Options.Context` (at most 255 bytes) and the first length-prefixed field of every signature input | [src] `mldsa.Options` |
| Identifiers | `pqc-conf/1`, `pqc-auth/1`, modes `compat` and `strict`; wire formats carry a version and an algorithm id; unknown ones are refused | proposal |
| TLS code points | Groups above; schemes `MLDSA44 0x0904`, `MLDSA65 0x0905`, `MLDSA87 0x0906` | [src] `tls/common.go` |

## Size and work bounds per consumer

Current bounds are in [inventory](inventory.md#current-format-and-bound-constraints);
this table gives required size, proposed maximum and fit (DEC-08).

| Consumer | Required size | Proposed maximum | Fits? |
| --- | --- | --- | --- |
| Backup header, hybrid | About 1557 bytes per stanza: 18-byte prefix + 1494-character unpadded base64 of the 1120-byte encapsulation + newline, plus a 43-character body line [arith/est] | At most 32 hybrid recipients (about 49 KiB) | About 168 fit in 256 KiB [est]; the binding limit is the managed value cap below |
| `HIKYO_BACKUP_RECIPIENTS` | 1959 characters per recipient plus a comma: 32 recipients = 62,719 bytes [arith] | 32 | The managed value cap is 65536 bytes, so 33 fit and 64 (125,439 bytes) cannot. Host environment-string limits not checked (NOTRUN) |
| Count versus byte conflict | n/a | Refuse deterministically at configuration validation and again at export, before any output, naming the violated bound | Required by #1126 |
| Upgrade custody | Fingerprints are fixed-length hash text; identity 77 characters | 32 (same number as exports) | Yes |
| Webhook v2 | Signature 3309 (65), 4627 (87) decoded, 4412 or 6170 base64 characters; public key 1952 or 2592 | Signature field 3309 bytes decoded, public key field 1952 bytes decoded (ML-DSA-65, DEC-02; ML-DSA-87 would need 4627 and 2592 and a new profile version), ciphertext 1 MiB and ack 64 KiB unchanged, envelope cap recomputed | v1's 64-byte signature check cannot carry it: new protocol version |
| DPoP | If the JWK embeds the raw 1952-byte key: header about 3.6 KB, signature 4412 characters, payload a few hundred: 8.1 to 8.3 KB total [arith, assumed JWK form; no JOSE ML-DSA format verified] | n/a (HOLD) | At or above the 8192 cap even for ML-DSA-65; any path needs a new bound; `go-jose` v4.1.6 has no ML-DSA |
| Transit | sig 3309 | unchanged | Yes; closed CHECK constraints and enums need a roll-forward migration |
| Rollout record | sig 3309 | unchanged (512 KiB) | Yes; other mailbox boundaries not traced |
| PKI | Leaf signed by ML-DSA-65 about 5.7 KB DER [est] (1952 + 3309 + about 400); 3-certificate chain about 17 KB DER, 23 KB PEM [est] | unchanged | Yes |
| Release trust | pk 1952 / sig 3309 | unchanged (4 MiB) | Yes |
| TLS handshake | ClientHello +1184 bytes (1216 versus 32); ML-DSA chain about +5.7 KB per certificate [est] | n/a | Middlebox behavior unqualified |

CPU, memory and concurrency budgets are DEC-17; no measurement exists (NOTRUN).

## Test vectors, errors, maintenance and errata (DEC-09)

| Topic | Proposal |
| --- | --- |
| Vectors | Go's ML-KEM and ML-DSA tests load Wycheproof vectors at test time from a module fetched at runtime or `-wycheproof-dir`; they are not vendored in the tree. Hikyo fixtures pin vector source, version and digest. Add age and HPKE vectors from the pinned modules; NIST ACVP optional (not fetched) |
| Cross-implementation | A second maintained implementation reads each new format before it is called interoperable (for example the age CLI for backups); NOTRUN |
| Errors | Closed classes: unsupported algorithm or version, malformed, over bound, wrong key or scope, invalid signature, missing required signature. Callers outside the authorized scope get one generic refusal; no secret, key or ciphertext bytes in errors, logs or audit; refuse before allocation or expensive work |
| Libraries | Standard-library `crypto/mlkem`, `crypto/mldsa`, `crypto/tls`, `crypto/x509`; `filippo.io/age`. No hand-written lattice code. Toolchain and modules pinned |
| Errata | FIPS 203 carries a NIST note (2025-11-17) and FIPS 204 a note (2026-07-31) that issues will be corrected in a future revision; errata sheets not read. Review at each Go minor release and NIST publication; a behavior change needs a new profile version |
| Reopen triggers | Go or age release touching these packages; NIST change to FIPS 203/204/205 or IR 8547; published practical attack; a peer or library change in an exception row |

## Symmetric floor and assumptions

DEC-04 proposes retaining the XChaCha20-Poly1305 hierarchy. NIST says its
symmetric standards are less vulnerable and no transition is expected
[NIST 4.1.3]; it also says all NIST-approved symmetric primitives with at least
128 bits of classical security are *believed* to meet at least category 1. The
program therefore declares a **category-1 symmetric floor** and names every
object, so no claim implies uniform 256-bit strength. ChaCha20-Poly1305 is not
a NIST-approved algorithm, so any category given to it is Hikyo's inference:

| Object | Symmetric strength | Basis |
| --- | --- | --- |
| Stored fields, DEKs, master, root (I01-I06) | 256-bit keys; category 5 by key size, **inference** (IR 8547 lists AES-256, not XChaCha20). **Only where no `operator.age` copy holds the current root:** a `root_escrow` opens the whole hierarchy under a 16-byte file key, so while such a copy exists the field floor is category 1 | [src], [NIST] Table 6 |
| age file key: backup archives (I07), upgrade archives (I08), sealed webhook payloads (I21) | 16 bytes: category 1 | [src] age `fileKeySize = 16` |
| `operator.age` custody record (I08), which holds backup identity and root escrow | Sealed by age scrypt, so bounded by the lesser of the 16-byte file key (category 1) and the entropy of the sealing secret; the "fields keep 256-bit" statement does not apply to it (options in [migration](migration.md#operator-custody-vault-operatorage-i08)) | [src] `vault.go:126` |
| Truncated client stamp (I06) | 128-bit truncated HMAC tag: no 256-bit claim | [src] `internal/crypto/stamp.go:102` |
| TLS 1.3 records (I09-I14, I30) | Suites are not configurable (`Config.CipherSuites` is TLS 1.2 and below only); Go's default order puts AES-128-GCM first and ChaCha20-Poly1305 first without AES hardware: category 1 by default | [src] `tls/defaults.go` |

An archive holds field values still encrypted at 256 bits but exposes metadata
(names, sizes; encryption-model ADR) at category 1. Proposal: accept and
document; do not fork age (DEC-04). Other assumptions:

| Assumption | Statement |
| --- | --- |
| Quantum key search | About 2^128 sequential iterations for a 256-bit key in the idealized Grover model and 2^64 for a 128-bit key; NIST's FAQ says Grover gives little or no advantage against AES in practice because the speedup needs serial execution (fetched 2026-11-02) |
| AEAD bounds | 128-bit Poly1305 tag. Random 192-bit nonces: collision probability about q^2 / 2^193, which is 2^-113 at q = 2^40 [arith]. Usage cap per DEK not re-derived: NOTRUN, owner and security reviewer to confirm. VM snapshot nonce replay is an existing documented hazard |
| Password entropy | Argon2id verifiers fall to offline guessing if the database is stolen; Grover gives at most a quadratic gain and memory hardness limits parallelism. Floors unchanged. Scrypt passphrases are only as strong as the passphrase, which no parser can measure |

### PQ use versus FIPS validation

| Fact | Basis |
| --- | --- |
| Go 1.27.3 lists the certified FIPS module alias as `v1.0.0-c2097c7c`; `v1.26.0` is in process (`lib/fips140/certified.txt`, `inprocess.txt`) | [src] |
| `crypto/mldsa` is unavailable under module v1.0.0 (stubs return an error) and available from v1.26.0, which is not certified | [src] |
| ML-KEM source is in the v1.0.0 snapshot; whether the certificate covers it was not checked | [src] |
| The encryption-model ADR says FIPS 140-3 alignment is not a goal | ADR |

No Hikyo document may say "FIPS validated" for a PQ feature; a build against
FIPS module v1.0.0 (tag `fips140v1.0`) fails ML-DSA visibly. FIPS conformance is
separate scope.

## Compatibility and strict profiles

Both are opt-in per surface and **existing behavior is unchanged by default**
(DEC-05). **Compatibility** is today's behavior: classical and hybrid both work,
nothing is claimed, and hybrid is used only where configured (TLS key exchange
uses Go's library default, which is not policy). **Strict** refuses anything
weaker, before credentials or application data leave the process. Claim wording
is in [qualification](qualification.md#claim-statement-template).

| Surface (IDs) | Confidentiality strict (a) | Authenticity strict (b) | Dependencies and limits |
| --- | --- | --- | --- |
| Backups (I07, I08) | New exports: hybrid recipients only, stanza allowlist, no scrypt export, no wrappers. Where strict is selected, restore of any archive, hybrid or classical, needs receipt-bound provenance or is quarantined ([migration](migration.md#mixed-paths-and-archive-provenance)); compat restores are unchanged. A PQ-protected object never has a classical door | Proposed target for #1132: attestation signed with ML-DSA, all-required with P-256 in the dual window. Until then P-256 (E-16), and receipts are unsigned (E-19) | #1126, #1132. `operator.age` options in [migration](migration.md#operator-custody-vault-operatorage-i08); the scrypt refusal covers exports only. Strict backup claims HOLD (E-19) |
| TLS hops (I09 to I14, I16, I17, I30) | Per the [hop table](#hop-starting-profiles): TLS 1.3 only, explicit hybrid group, classical-only peers refused, on hops marked strict | Owned clients on strict-marked hops: an ML-DSA leaf and chain to a PQ-authenticated anchor, enforced in `VerifyConnection`. I09 can only present such a chain: it sets no `ClientAuth` (`internal/app/tls.go:31`), so it has no peer chain to verify. Pin-based clients and every other hop: no authenticity claim | #1127, #1133. A single listener certificate may exclude non-ML-DSA clients (E-18) |
| Helm chart (I31) | Listener Secret, database CA and kubelet CA follow their hops; image policy has no confidentiality claim | Image verification classical (E-03) | #1132 |
| Sealed webhook (I21) | New protocol version, age hybrid recipient | ML-DSA envelope and ack, all-required | #1128, receiver first |
| Transit (I22) | n/a | New ML-DSA key algorithm and identity | #1129 |
| Rollout (I23) | n/a | ML-DSA command signature; trust transition per DEC-20. The enrollment anchor arrives over classical cluster access, so no PQ claim covers how it got there (E-14) | #1131 |
| Private PKI (I24) | n/a | ML-DSA issuer and leaf, PQ anchor (DEC-19) | #1133 |
| Release and update (I26) | n/a | ML-DSA on trust documents and artifacts, all-required with the classical signature; **the claim is HOLD until #1132 closes every binary-replacement path** (E-11, E-15) | #1132 |
| Pairing (I20) | HOLD | HOLD | #1130; E-01 |
| SSH CA, GitHub payloads, IdP and authenticator signatures, source supply chain (I15, I18, I19, I25, I32) | None | Exception | #1134; [register](migration.md#exception-register) |
| Import and evidence (I28, I29) | None: sources are read over their own TLS (kubeconfig, ambient OpenBao SDK settings; I30) or SOPS key sources (E-13) | None: a digest has no signer | Non-claims |
| Storage, tokens, passwords (I01-I06) | Out of scope, unchanged | Out of scope | Symmetric retention |

### TLS profile detail (DEC-06)

| Item | Proposal |
| --- | --- |
| Version | Strict: `MinVersion` and `MaxVersion` TLS 1.3. Compatibility keeps TLS 1.2 (no silent default raise) |
| Groups | Strict: explicit `CurvePreferences` allowlist, initially `X25519MLKEM768`; a classical-only peer then fails with no common group before application data. Never rely on Go defaults or `GODEBUG` |
| Authentication | Go has no signature-scheme setting, so strict authenticity refuses through `VerifyConnection`, walking the verified chain: leaf key, every certificate's signature algorithm and the anchor must be ML-DSA-65 (Go also accepts ML-DSA-44 and 87, which DEC-02 does not select). A server without client authentication has no peer chain to walk. **Pin-based hops** (`InsecureSkipVerify` plus a leaf SPKI pin: `internal/cli/client.go:188`, `internal/remotefetch/remotefetch.go:248`) have empty `VerifiedChains`, and pairing pins are read over a classical chain unless overridden (`internal/remotefetch/pairing_identity.go:58`): they are excluded from strict authenticity claims (E-01, E-20). Compatibility only reports what was observed. TLS 1.3 alone or a hybrid group with a classical certificate is never labelled PQ authentication |
| Observation | After the handshake read `ConnectionState.CurveID` and `LocalCertificate`; record per hop. Client and server logs are separate evidence |
| Resumption | Strict disables session tickets until profile-change invalidation is demonstrated |
| Library | Go 1.27.3 exposes `MinVersion`, `MaxVersion` and `CurvePreferences`; no other group or version policy setting was found |
| Terminators | Claims name the exact hop and terminator; a proxy or database outside the process is outside the claim |
| Platform verifier | On macOS and Windows, system-root verification uses OS APIs; setting `SSL_CERT_FILE` or `SSL_CERT_DIR` selects Go's verifier instead, and `GODEBUG=x509sslcertoverrideplatform=0` restores the platform one (`crypto/x509/cert_pool.go`). The OS verifiers' handling of ML-DSA chains is unverified; qualification tests each claimed OS in both modes |

### Hop starting profiles

One table, one starting profile per hop (DEC-06). Strict starts only where both ends
are Hikyo-controlled and the hop is opt-in; everything else starts at compatibility
and moves only with peer evidence (exception E-08).

| Hop | IDs | Starting profile | Why |
| --- | --- | --- | --- |
| Native listener | I09 | strict (opt-in) | Owned server, managed generation |
| CLI, operator and MCP clients calling Hikyo | I11 | strict (opt-in) | Owned client |
| Pairing fetch to a remote Hikyo (leaf pin) | I11, I20 | compat | Pin read over a classical chain; no strict authenticity claim |
| Pairing proxy hop (`remotefetch.go:303`) | I11, I20 | compat | TLS to a proxy with system roots; proxy outside the claim |
| Update check | I11 | compat | External peer |
| Primary and dynamic PostgreSQL | I12, I13 | compat | Hikyo enforces `verify-full`, not the server's groups |
| Provider adapters | I14 | compat | External peers |
| Mail | I16 | compat | Relay hops external |
| Federation, JWKS, OAuth2 exchange | I17 | compat | IdP-controlled |
| Kubernetes API, kubelet, OpenBao import | I30 | compat | SDK-owned, cluster-controlled |
| Proxy and ingress terminators | I10 | compat | Outside the process |
| Plaintext public listener in trusted-proxy mode (`internal/config/config.go:616`) | I10 | compat, no claim | No TLS at this process. Requirement on #1127: selecting strict with no TLS certificate fails closed at startup and is refused at managed Apply |

### Configuration, downgrade, rollout, exceptions (DEC-10)

| Topic | Proposal |
| --- | --- |
| Precedence | 1. Adopted managed configuration (changed through the MFA-bound plan and Apply, or by host-authority recovery selecting a published revision, which can select one below strict: E-21); 2. bootstrap file or environment before adoption; 3. default `compat`. `GODEBUG`, Go defaults and per-request flags never relax an adopted strict setting |
| Downgrade rejection | Lowering strict is a managed change with durable redacted audit. Writers refuse classical output under strict; readers refuse objects below the persisted profile or format floor. Outside strict, frozen historical archives stay restorable (DEC-05); where strict is selected, receipt-or-quarantine applies to them like any other archive, so an archive without a receipt waits on the undesigned hold release (E-19) |
| Stale nodes | A node whose active generation is below the desired one is fenced: it does not admit credentials or data on the strict surface (fails closed) and is reported as not enforcing. Losing only the claim while still serving is not enough |
| Managed rollout | Policy joins the prepared and active application generation, as certificates do |
| Exceptions | Each names a person owner (default Marc Went), exact peer and version, scope, claim limitation, expiry and recheck trigger. **Expiry: at most 180 days or the next trigger; at most two renewals, a third requires an ADR-recorded scope decision.** The cap is a recorded rule, not a control: the sole approver can waive it by a recorded decision, which no mechanism prevents. Mitigation is only the dated record and the adversarial review of renewals |
| Evidence to claim enforcement | Effective policy (sanitized); the observed negotiated group or verified signature set on the real path; a negative test showing the classical-only peer or stripped signature refused before data; candidate commit and peer versions. Evidence classes are separate ([qualification](qualification.md#evidence-classes)) |

## Signature composition and stripping resistance (DEC-03)

An **either-signature policy** (accept if the classical OR the PQ signature
verifies) gives no PQ forgery resistance: a quantum forger forges the classical
signature and presents it alone.

| Rule | Detail |
| --- | --- |
| Required set | The verifier derives the required algorithms from its own pinned trust state (profile, key registry, generation), never from the presented object. This, not the signed bytes, is what stops stripping |
| All must verify | Every required algorithm has exactly one valid signature; a missing, duplicate, malformed or (in strict) unknown algorithm fails the object |
| Suite binding | All signatures cover identical canonical bytes including the label, profile id and the ordered required algorithm and key ids. A signature made under suite {A, B} does not verify as suite {A}. A forger can still sign a fresh object under suite {A}, so a verifier whose pinned set is {A} accepts it: only raising the pinned set prevents that |
| Domain separation | ML-DSA `Context` = label; the classical input starts with the same length-prefixed label |
| PQ-only | End state; dropping the classical algorithm is a trust-generation change with a monotonic floor ([migration](migration.md#rollback-floor-and-historical-verification)) |
| Key substitution | The PQ key is authorized by the pinned trust state; an object announcing its own key is not an anchor |
| Trust documents | Roots, metadata, catalogs, policies and bridges follow the same rule (see [release trust](migration.md#release-trust-and-install-paths)); revocation and key replacement are trust changes that need every required algorithm |
