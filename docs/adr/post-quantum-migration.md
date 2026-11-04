# Post-quantum migration: policy, decision register and proposed amendments (ADR, proposed 2026-11-02)

> **Status: Proposed. Not operative. Nothing here is locked or authorized.** The
> owner accepted the register as recommended and gave maintainer security approval
> on 2026-11-02. It is not operative until each ADR amendment lands. This file
> records the
> decisions that
> [#1124](https://github.com/Hikyo-Org/Hikyo/issues/1124) needs from the owner
> and, for each affected locked ADR, the amendment text that would land through the
> governance route. No locked ADR body or banner is edited by this
> file. Locking follows the [governance route](#governance-route).

## Context

[#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123) migrates Hikyo's public
key cryptography to post-quantum (PQ) confidentiality and authenticity. The
supporting documents are indexed by the
[contract README](../security/pqc/README.md), which holds the source pin; the
dated PR state is in the [handoff](../handoff/1124-pqc-contract.md#state-on-2026-11-02)
and the governance recheck is below: [inventory](../security/pqc/inventory.md),
[profiles](../security/pqc/profiles.md),
[migration](../security/pqc/migration.md) and
[qualification](../security/pqc/qualification.md). The program coordination is
the [program draft](../design/post-quantum-program.md); sources read are in the
[handoff](../handoff/1124-pqc-contract.md#primary-documents).

Two claims are defined separately and neither implies the other:
quantum-resilient **confidentiality** and quantum-resistant **authenticity**.

## Owner decisions already made (2026-11-02)

Recorded as made; no others are implied.

| Decision | Effect |
| --- | --- |
| The PQ program runs no cross-provider review. The owner said "No cross provider review" on 2026-11-02; signed commit `13068ab2460c44d226111322a99ca65b20a3d02e` on PR #1148 records the decision in its own words ("the PQ program runs no cross-provider review") | Each amendment banner records cross-provider review as **SKIPPED by explicit owner instruction**. It is not CLEAN. Independent adversarial review (Standards, Spec and security) remains required |
| PR #1148 stays a draft | The program draft is not merge-ready; this contract is stacked on it |
| DEC-01 to DEC-20 are accepted as recommended. The owner replied "As recommended" on 2026-11-02 to the register at `383290434d2cf614cd8aa31600c1f79d1e53f4d7`, after being told its final repairs were not re-reviewed | The Decision column below is the owner decision. This commit is the record |
| Maintainer security approval of this contract at `27a16c276c1834b4edb059d10414b13cd4e90729`. The owner replied "Continue as recommended" on 2026-11-02 to approving that text, after being told what the final pass changed and that its last repairs had no independent review | Covers the policy text only. Each implementation PR needs its own maintainer security approval. Owner and maintainer are one person, so this is not independent human review |

This is a **waiver of part of the amendment procedure, not a match with it**
(see below). It is also broader than its precedent: the
[developer credentials ADR](./developer-credentials.md) waived cross-provider
review for one decision (#807) and says the waiver is task-specific, whereas the
2026-11-02 instruction covers the whole program.

## Governance route

The [amendment procedure](../../GOVERNANCE.md#amendment-procedure) says a locked
ADR "may be amended only by reopening its originating ticket, running the same
adversarial cross-model review that locked it, and recording the amendment in
the ADR itself". For each ADR below: (1) owner decision in this register, (2)
reopen the originating ticket, (3) independent adversarial review, with
cross-provider review waived as above, (4) banner and body update inside that
ADR, (5) operative when the implementing PR merges, matching the banner
convention. Step 2 applies to the existing ADRs below; this proposed ADR itself
locks under #1124, which is still open. Whether adversarial review by models of one provider satisfies "the
same cross-model review" after the waiver is not settled by GOVERNANCE; under
DEC-18 the owner accepts it for this program. Marc Went is owner and sole maintainer, so
owner approval and maintainer security approval come from one person and are not
independent human review; adversarial review compensates (see
[CONTRIBUTING](../../CONTRIBUTING.md#security-sensitive-contributions)).

Neither GOVERNANCE nor #1123 states that a reopened ticket covers only its
amendment; DEC-18 records that as the owner's reading. Recheck on 2026-11-02:
[#26](https://github.com/Hikyo-Org/Hikyo/issues/26) is closed;
[#79](https://github.com/Hikyo-Org/Hikyo/issues/79) and
[#527](https://github.com/Hikyo-Org/Hikyo/issues/527) are open and record the
1.0.0 publication and roadmap. This plan changes no 1.0 membership, milestone,
freeze or gate, reopens no delivered feature (#75, #76, #154, #155, #156, #198)
and adds no release blocker.

## Decision register

Decision owner for every row: **Marc Went**. Status of every row: **owner
decided and security approved, not operative**. A decision takes effect only after
its ADR amendment and the implementing PR, which needs its own security approval. Details are in the
linked documents.

| ID | Topic | Options | Decision | Consequence |
| --- | --- | --- | --- | --- |
| DEC-01 | Allowed KEM sets ([profiles](../security/pqc/profiles.md#candidate-parameter-sets)) | (a) hybrid X25519 + ML-KEM-768 only; (b) also ML-KEM-1024 hybrids for TLS; (c) pure ML-KEM | (a) | NIST's default ML-KEM-768 (category 3) and existing library paths; (c) drops the classical hedge and has no age path |
| DEC-02 | Allowed signature sets | (a) ML-DSA-65 only; (b) 65 default, 87 for roots; (c) include ML-DSA-44; (d) SLH-DSA | (a); revisit (b) at DEC-14 | ML-DSA-44 is category 2; SLH-DSA is not in the Go standard library. Pure ML-DSA with context, no pre-hash |
| DEC-03 | Composition and stripping ([profiles](../security/pqc/profiles.md#signature-composition-and-stripping-resistance-dec-03)) | (a) all required signatures verify, required set from verifier trust state, suite-bound bytes; (b) PQ-only for new surfaces; (c) either signature suffices; (d) composite | (a) in the dual window, (b) as end state; (c) rejected | (c) gives no PQ forgery resistance; (d) has no stdlib support |
| DEC-04 | Symmetric hierarchy and category-1 floor ([profiles](../security/pqc/profiles.md#symmetric-floor-and-assumptions)) | (a) retain it and declare a category-1 floor on the named objects (age file keys, `operator.age`, TLS 1.3 records); (b) patch or fork age; (c) add a 256-bit layer around archives | (a) | Archive payload and metadata, webhook payloads, the custody record holding root escrow and TLS records are 128-bit strength; fields keep 256-bit only while no `operator.age` copy holds a root that opens them, because a `root_escrow` opens the whole hierarchy. (b) is a custom primitive change excluded by #1123 |
| DEC-05 | Profile structure and default | (a) two opt-in modes, existing behavior unchanged; (b) hybrid writes by default once qualified; (c) strict default | (a) now; reconsider (b) after #1135 | No silent change for existing installs |
| DEC-06 | Strict TLS profile and hop scope | (a) TLS 1.3, explicit `[X25519MLKEM768]`, tickets off, `VerifyConnection` for authenticity; (b) also other Go hybrids; (c) Go defaults. Scope: the single [hop table](../security/pqc/profiles.md#hop-starting-profiles); strict first only on the listener and owned clients; pin-based hops excluded from authenticity | (a) with that table | (c) rejected: `GODEBUG` can remove defaults. Go has no signature-scheme setting. Every hop has one starting profile. The listener presents a chain and verifies no client chain. A strict ML-DSA listener certificate may exclude clients (E-18); pin-based owned clients get no authenticity claim (E-20) |
| DEC-07 | Encodings, fingerprints, domain separation, identifiers | (a) new algorithm-bearing namespaces and labels; (b) reuse existing; (c) per child | (a) | (b) would reinterpret `age-x25519-sha256:`; (c) lets each child invent policy |
| DEC-08 | Size and work bounds ([profiles](../security/pqc/profiles.md#size-and-work-bounds-per-consumer)) | (a) smallest fit, no global raise: at most 32 hybrid recipients (managed value cap 65536 bytes), webhook v2 field bounds; (b) raise global caps; (c) keep 256 or 64 | (a) | 33 recipients fit a managed value, 64 do not; deterministic refusal before output |
| DEC-09 | Vectors, errors, maintenance, errata | (a) pinned Wycheproof plus age/HPKE vectors, closed error classes, errata review per Go minor and NIST publication; (b) library tests only | (a) | (b) misses #1135's reference-vector requirement; FIPS 203/204 carry open errata notes |
| DEC-10 | Precedence, downgrade, stale-node fencing, exceptions | (a) managed > bootstrap > default, with host-authority recovery registered (E-21); stale nodes fenced; expiry 180 days, two renewals then an ADR-recorded decision; (b) environment overrides managed; (c) longer or no expiry | (a) | Single approver is a residual risk |
| DEC-11 | Rollout, retirement, floor, historical access ([migration](../security/pqc/migration.md)) | (a) the ladder with observable gates; floor persistence stated as a child requirement; where strict is selected, archive restore needs receipt-bound provenance or quarantine, for hybrid and classical alike, never for default compat restores; re-encrypted copies; classical evidence read-only after a quantum break, countersigning an option for the owning child; (b) same without any countersigning | (a) | Floor is one-way. Provenance mechanism, receipt signer, pin and hold release are not designed here, and today only upgrade exports write a receipt, so strict backups stay HOLD (E-19). Audit exports are forgeable today; #1135 records the non-claim (E-12). Unobservable readers and local floors are registered (E-17); classical archive copies E-10 |
| DEC-12 | Key generation, escrow, custody, `operator.age` ([migration](../security/pqc/migration.md#operator-custody-vault-operatorage-i08)) | Vault: (a) keep age-scrypt sealing and `Rewrap`; (b) seal with the existing 256-bit envelope keyed from the root. Consequences are in migration; the design is left to #1126. Custody: software only vs a named external provider first | (a) now, (b) as target; software custody | Old passphrase copies hold a live `root_escrow`, so retiring them needs root rotation; old identities still open history; `Rewrap` keeps stale escrow that drills use; removing the prompt strands interrupted hosts; under either option root rotation does not re-seal, so a root-sealed vault fails to unlock afterwards until #1126 re-seals it. The vault is outside "fields keep 256-bit". E-09, E-16 |
| DEC-13 | Pairing SRP and DPoP | (a) HOLD with E-01; (b) invent a composition; (c) redesign pairing | (a) | (b) excluded by the issue; `go-jose` lacks ML-DSA; ML-DSA-65 DPoP exceeds its 8192-byte cap. **#1135 is blocked by #1130, so (a) deadlocks #1135 unless the owner also narrows scope with linked residual work, which #1123 permits: decided, with #1130 as the linked residual work** |
| DEC-14 | Release trust and artifact signing ([migration](../security/pqc/migration.md#release-trust-and-install-paths)) | (a) requirements on #1132: PQ-capable trust documents, sequence-only progression, claim only after persisted all-required verification, every binary-replacement path closed or registered, anchor feasibility examined; maintainer-held stable signer; ML-DSA recovery key custody HOLD until the owner names it before #1132; (b) offline root ceremony; (c) defer | (a) | The PQ trust-anchor bootstrap is an unsolved gap and the release-authenticity claim stays HOLD until #1132 closes every replacement path (E-11, E-15). Nightly has no PQ signer. Losing the sole ML-DSA recovery key leaves no PQ-signed reinstall (E-18) |
| DEC-15 | Sigstore and classical supply chain | (a) exceptions (E-03, including source supply chain I32) plus first-party PQ signature in addition; (b) no artifact PQ claim yet; (c) drop Sigstore | (a) | No claim these ecosystems migrated |
| DEC-16 | SSH CA, federation, authenticators, imports | (a) exceptions E-02, E-04 to E-06 and E-13; no `@golang.org` certificate names; (b) ship those names; (c) hold the profile | (a) | OpenSSH has no PQ signatures |
| DEC-17 | Budgets (PQ-11) | (a) byte bounds now; latency, CPU and memory thresholds set by the owner from each child's own baseline before that child's acceptance (#1127 TLS, #1129 Transit, #1132 release, #1133 PKI), #1135 verifies; (b) numbers now | (a) | Breaks the #1127 and #1135 cycle; (b) would invent numbers |
| DEC-18 | Amendment governance | (a) one reopened ticket per ADR, that amendment only, adversarial review, cross-provider waiver as above, owner accepts the waiver as sufficient; (b) bundled amendment; (c) reopen all together | (a) | Waiver is explicit, not a match; 1.0 unchanged |
| DEC-19 | Private PKI root (#1133) | (a) offline ML-DSA root, operator-held, signs intermediates; (b) online evaluation root only; (c) classical root over PQ intermediate | (a) | (c) is not a PQ-authenticated chain; no HSM assumed |
| DEC-20 | Rollout authority (#1131) | (a) in-band transition "through the existing authenticated authority", as #1131 words it; (b) mailbox announcement; (c) operator re-enrollment out of band | (c), **a departure from #1131's wording, accepted by the owner** | (a): the only algorithm required today is Ed25519, so a forger installs its own ML-DSA key, and the controller cannot check MFA. (c): enrollment is an immutable ConfigMap; a new key or id fails the existing journal check and emptying the journal loses the plan Restore needs; these are #1131 requirements (E-14) |

## Proposed amendments to locked ADRs

Proposed text only, copied into the named ADR when its ticket is reopened. Backups and
TLS come first: the encryption model, the confidentiality part of the threat model and
the backup custody sentence of signed upgrade compatibility are recorded in those ADRs. Common review sentence: "Independent adversarial review is
required; cross-provider review is SKIPPED by explicit owner instruction
(2026-11-02) and is not CLEAN."

| ADR (originating ticket) | Sections affected | Depends on |
| --- | --- | --- |
| [encryption-model](./encryption-model.md) (#14, per its title and the #930 amendment) | Backups and exports; Primitives; Erasure | DEC-01, 04, 05, 08, 11, 12 |
| [threat-model](./threat-model.md) (#8) | Attacker classes; Compromise assumptions; boundaries 2 and 5 | DEC-01, 05, 06 |
| [transit](./transit.md) (#156, named in its decision text) | D2, D3, D4, D5, D9, D10 | DEC-02, 03, 07, 08 |
| [remote-instance-pairing](./remote-instance-pairing.md) (its record names #8 and #71 as reopened) | Cryptographic profile; Limits of the guarantee | DEC-13 |
| [pki](./pki.md) (#154) | D2, D3, D5, D7, D9 | DEC-02, 03, 06, 08, 19 |
| [signed-upgrade-compatibility](./signed-upgrade-compatibility.md) (#638, its lock record) | Release identity and trust; Backup and restore custody | DEC-03, 11, 14, 15 |
| [stable-workflow-signing](./stable-workflow-signing.md) (PR #714 and the owner's 2026-09-08 decision; it has no grilling issue) | Decision (keyless stable signing, recovery-signed policy) | DEC-14, 15 |
| [system-architecture](./system-architecture.md) (#22; "Packaging & release artifacts" trust model, already superseded in part by stable-workflow-signing) | Signing trust model wording | DEC-14, 15 |
| [oss-mechanics](./oss-mechanics.md) (#33; "Signing" custody, ceremony, rotation, compromise) | Same | DEC-14, 15 |
| [ops-spec](./ops-spec.md) (#32; recipient hygiene "exactly one age identity per retention class") | Listed because DEC-08 and DEC-12 touch recipients; no change proposed | DEC-08 |

Non-ADR owners: `docs/spec/sealed-webhook.md` (#1128) and the self-configuration
specs (#1131); DEC-18 does not state their revision route, so the owning child
proposes it: HOLD.

### Encryption model

Landed as the 2026-11-02 banner on [encryption-model](./encryption-model.md), which is the only copy.

### Threat model

The recording-adversary class landed as the 2026-11-02 banner on [threat-model](./threat-model.md). Still proposed: a future quantum forger class for the surfaces of the authenticity profile, added with the authenticity children.

### Transit

> **Proposed amendment (not operative; #1124, 2026-11-02).** D2 gains the signing
> algorithm `mldsa65`, as DEC-02 selects, with a new immutable key
> identity; Ed25519 keys stay Ed25519. D3: the ML-DSA seed is derived with
> HKDF-SHA256 from the 32-byte version material under the existing
> `hikyo/transit-key/v1` pattern (algorithm, org, project, environment, key and
> version in the info), as the Ed25519 seed is; the material, not the seed, is
> what is sealed and stored. D4: the signature carries algorithm and parameter
> set; the context string is the Transit label (at most 255 bytes). D5: rotation
> appends a version of the same algorithm and never converts a key; version
> windows and compromise rules are unchanged. D9: bounds unchanged. D10:
> verification consumers exist before the first key. Unavailable custody still
> refuses.

### Remote-instance pairing

> **Proposed amendment (not operative; #1124, 2026-11-02).** The cryptographic
> profile is unchanged. A recorded exception states that SRP-2048 and Ed25519
> DPoP give classical authenticity only, that no PQ PAKE composition is
> authorized, and that the transcript is protected only by the TLS pin. Pairing
> starts at compatibility, so no hybrid group is required of it.

### Private PKI

> **Proposed amendment (not operative; #1124, 2026-11-02).** D5's closed key set
> gains `mldsa65` for issuers and leaves only if the #1133 relying-party
> matrix passes; adding an algorithm is widening and uses a new bound
> profile. D2: issuer keys stay sealed seed-only PKCS#8. D3: a PQ chain needs an
> offline PQ root (DEC-19); a classical root over an ML-DSA intermediate is not a
> PQ-authenticated chain. D7: CRLs signed by the issuer algorithm. D9
> (Surfaces): the closed API enum gains the algorithm; PEM and request caps are
> OpenAPI and server constants outside the ADR and suffice at ML-DSA-65 sizes
> [estimate].

### Signed upgrade compatibility and stable workflow signing

Landed as the 2026-11-02 banners on [signed-upgrade-compatibility](./signed-upgrade-compatibility.md#release-identity-and-trust), [stable-workflow-signing](./stable-workflow-signing.md), [system-architecture](./system-architecture.md) and [oss-mechanics](./oss-mechanics.md), with the proposal text verbatim and operative only when #1132 merges. The proposal block as it stood is in this file at commit `4bad16b59`.

## Locked constraints preserved

Symmetric hierarchy and envelope; immutable key purposes; tenant and environment
authorization; admission and egress limits; sealed custody; durable redacted
audit; recovery holds and restore reconciliation; multi-node fences; no custom
primitive, ad hoc PAKE, mandatory HSM or KMS, public CA, ACME or OCSP expansion,
or FIPS certification.
