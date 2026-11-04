# Pairing post-quantum feasibility (#1130), 2026-11-03

Status: **HOLD. No post-quantum pairing is operative, and no protocol is selected or
designed here.** This note records the threat analysis and the feasibility survey that
[#1130](https://github.com/Hikyo-Org/Hikyo/issues/1130) asks for first. It
establishes no post-quantum pairing: the ticket says "A feasibility note alone
does not establish PQ pairing", so #1130 stays open. The owner's decision is
[DEC-13](../../adr/post-quantum-migration.md#decision-register) option (a), HOLD
with exception E-01. Option (b), inventing a composition, is excluded by the
issue.

What does take effect when the pull request carrying this note merges is the
E-01 banner on the pairing ADR. The register says a decision takes effect only
after its amendment and the implementing change, "which needs its own security
approval" ([register](../../adr/post-quantum-migration.md#decision-register)).
The owner gave that maintainer security approval on 2026-11-04 by authorizing this merge. The register records DEC-13 (a); options A to D, the DEC-13 wording and the E-01 fields stay open. The E-01 row is also not yet
effective as an exception; see [E-01 as registered](#e-01-as-registered-and-a-proposed-row).

Source read at `f5d549182` (PR #1156 head). The four named files, and
`internal/authz/remote_pairing.go`, have no diff against the issue baseline
`2ac7bb36633d3ae0a99d7724968ca8c3cbf18334`. Evidence tags: **[src]** Hikyo or module
source read in this session; **[arith]** arithmetic on [src] values; **[doc]**
document text fetched directly from its primary host and read as raw text;
**[web-tool]** read through a page-summarizing fetch tool on 2026-11-03, so the
wording is unverified; **[data]** machine-readable registry data fetched directly;
**[measured]** run in a scratch module outside the repository on 2026-11-03 (Go
1.27.3, one core of an Apple arm64 machine); **[derived]** reasoning from a
protocol's structure, not run. A search summary is never used as
evidence for presence, and a negative search is stated as "none found", never "none
exists".

## Result

1. No standardized post-quantum PAKE was found. The only candidate found is an
   individual Internet-Draft with no IETF or IRTF stream. Its own text says the
   hybrid protocol "has not yet received significant peer review (compared to the
   non-hybrid PAKEs)" and that "the core sequential-composition design has been
   independently analyzed by at least three different groups". Section 2 names
   long-lived passwords as the primary use case of its CPaceOQUAKE+ variant, and it calls one-time device pairing "a
   comparatively weak motivating example". DEC-13's PAKE blocker stands.
2. **One DEC-13 premise has changed.** The register says "`go-jose` lacks ML-DSA".
   `go-jose` v4.1.8, published 2026-11-02T17:56:45Z (the day of the decision), adds
   ML-DSA behind `//go:build go1.27`, and RFC 9964 (Proposed Standard, 2026-05-20)
   defines the JOSE identifiers. Hikyo builds with Go 1.27.3 but pins `go-jose`
   v4.1.6. The library blocker for a post-quantum proof of possession is gone; the
   size bounds, closed key and algorithm sets, and the context-string rule below
   are not.
3. The PAKE and the proof signature are separable parts with different blockers
   (below). Separating them is an owner decision with a cost the owner must accept:
   pairing enrollment would remain classical.

## What each layer guarantees today

[src] `internal/pairing/srp.go`, `dpop.go`, `internal/service/remote_pairing.go`,
`internal/remotefetch/pairing.go`, `internal/authz/remote_pairing.go`.

| Layer | What it gives today | What it does not give |
| --- | --- | --- |
| TLS with leaf SPKI pin (`remotefetch.go` `httpClient`, `InsecureSkipVerify` plus `VerifyPeerCertificate`) | Authenticates the server's classical leaf key to the requester, and protects every message, including the capability, the SRP messages and the minted bearer credential. The pin is read over a classical chain (`pairing_identity.go:58`) | No SRP session key, channel binding or exporter is bound to it. Confidentiality is post-quantum only if the hop negotiated `X25519MLKEM768`, which this hop does not enforce (E-08). The pin authenticates a classical key (E-01) |
| SRP-2048 PAKE (`srp.go`, go-srp v0.0.7, version 4) | Proves both sides know the phrase (client proof M1 checked by the server, server proof M2 checked by the requester). A transcript gives no offline phrase test against a classical adversary. Salt is a hash of invite id, origin, pin, requester thumbprint and invite salt, so a relay cannot enroll another key or serving identity | The SRP session key is discarded; nothing else is derived from it. Security rests on the discrete logarithm in a 2048-bit group |
| Proof signatures (Ed25519 DPoP, `dpop.go`) | At `finish`: proof of possession of the key whose thumbprint is in the SRP context, bound to the exchange id (`ath`) and a server nonce. Afterwards: every directory request carries a one-time proof (durable `jti` replay store) bound to the credential hash | The enrolled public key is sent in every proof header. Anyone who can compute an Ed25519 private key from a public key can sign |
| Symmetric artifacts (capability, bearer credential, verifiers) | High-entropy random values stored as verifiers; replay records are hashes | Not a quantum concern at these sizes |

Bounds that matter below: invitation lifetime at most one hour, credential
lifetime at most 365 days (`remote_pairing.go:240`); at most eight starts per
invitation (`ClaimRemotePairingAttempt`, `attempts<8`), four live exchanges
(`remote_pairing.go:437`), exchange lifetime two minutes; the exchange is consumed
even on a bad proof (`Finish`); replay store 256 live entries per key, 10,000
overall (`authz/remote_pairing.go`).

**Reading "protected only by the TLS pin".** The pairing ADR banner keeps the
owner's wording. Precisely, two different protections are involved: the TLS key
exchange protects the transcript against a **recorder** (and is post-quantum only if
the negotiated group is hybrid), while the pin only stops an **active forger** of the
server's classical key. The first is not claimed on this hop (E-08); the second is
classical (E-01).

## Threat analysis

Quantum columns assume a classical TLS key exchange on the hop (the hop is at
compat, so no confidentiality is claimed, E-08) and a classical pinned leaf key. A
hybrid group on the hop changes only the recorder column, not the forger column.

| Threat | Held by | Today (classical adversary) | Quantum recorder (harvest now, break later) | Quantum active forger |
| --- | --- | --- | --- | --- |
| Invitation theft | PAKE and attempt budget | The capability alone cannot redeem; the thief needs the phrase, with at most eight starts. Phrase plus invitation redeems with the thief's own key (ADR "Limits of the guarantee") | Unchanged: the capability is a random symmetric value and is consumed on redemption | Unchanged |
| Malicious peer | TLS pin, PAKE | A wrong server fails the pin before any byte is sent. A requester without the phrase fails the proof | Unchanged | Pin forgery below |
| Passive attacker | TLS | Sees ciphertext only | Decrypts the recording once TLS is broken: capability, public JWK, SRP messages, the minted bearer credential from the `finish` response, **and the bearer credential and JWK-bearing proof of every later directory fetch** (each fetch is a new connection, `DisableKeepAlives`, and sends `Authorization: DPoP <token>` and the proof, `remotefetch/pairing.go:64-65`) | n/a |
| Weak or user-chosen phrase | PAKE and attempt budget | Online guessing only, capped at eight starts per invitation. Phrases of one byte or more are accepted by design | Offline guessing becomes possible, at the key-derivation cost per guess (next section) | Same |
| Relay or MITM | TLS pin, PAKE context binding, DPoP at finish | A relay cannot swap the requester key (it changes the salt) or the serving identity; it needs the real server's key to terminate TLS | The credential in a recorded relay is exposed with the rest | A forged pinned leaf terminates TLS, reads the capability, SRP messages and later token, and can serve the requester false directory data |
| Offline guessing | PAKE | None from a transcript | Yes, after the discrete logarithm of the SRP group is solved [derived] | Yes, from messages the forger receives |
| Future transcript exposure | TLS | None | Capability (consumed, low value), a guessable phrase (low value after redemption unless reused), **bearer credential and the public JWK**, from the pairing transcript or from any one recorded directory fetch (high value until expiry or revocation). A hybrid group on the pairing hop does not protect later fetches that negotiate a classical group | n/a |
| Replay | Server state, DPoP, nonce | Single-use invitation, exchange consumed even on failure, one-time `jti` per enrolled key | Replay bookkeeping is hashes and timestamps: unaffected | Unaffected, but a forged signature is a fresh proof, not a replay |
| Authority restoration | Credential epoch | Restore advances the epoch once (`service/restore.go`); invitations, exchanges and connections stamped with an earlier epoch are not live (`Finish`, `i.Live`) | Unchanged | Unchanged |

### Offline guessing against a recorded SRP-2048 transcript

[derived] from the SRP-6a structure used by go-srp: the requester sends
`A = g^a` and a proof M1 that is a hash of `A`, `B` and the shared value `S`; the
server sends `B = k*v + g^b`. The salt is derived from public context, and the
pairing challenge carries that context. A transcript therefore holds `A`, `B`, the
salt and M1, which is everything but `a`, `b`, `x` (the password-derived exponent)
and `S`.

- A classical adversary holding the transcript cannot test a phrase guess without
  `a` or `b`. That is the property the PAKE is for, and it is why the ADR can accept
  short phrases.
- An adversary able to compute discrete logarithms in the 2048-bit group (Shor)
  recovers `a` from `A`. For each guess it computes `x` (one key derivation: bcrypt
  at cost 10, `go-srp` v0.0.7 `hash.go:51`, **72.1 ms per hash [measured]**), then
  `S = (B - k*g^x)^(a + u*x) mod N`, and compares the hash with M1. The PAKE
  becomes a guessable hash of the phrase; only the key-derivation cost slows it.
  What falls depends on the phrase, because `phrase.go` accepts any nonempty string
  up to 4096 bytes: the default generated phrase has 6 x log2(7776) = 77.55 bits
  [arith] and is out of reach at that cost; five lowercase letters (26^5, about
  11.9 million guesses) take about 10 core-days and five printable ASCII characters
  (95^5, about 7.7 billion) about 18 core-years [arith on the measured cost]. The
  exposed case is a guessable phrase, which the ADR deliberately allows. The
  prehash to 43 characters (`srpPassword`) does not change the bcrypt cost.
- The general statement is in the draft below: "for any PAKE that is not
  unconditionally password hiding", breaking the assumption "allows an attacker to
  mount an offline dictionary attack against that transcript, provided the attacker
  also has some way to check a candidate password against it" [doc,
  draft-vos-cfrg-pqpake-03 section 11.3]; an explicit password-confirmation message
  is such a check, and SRP's M1 is one. Section 11.2 says CPace is "only
  quantum-annoying: a quantum attacker must solve a discrete logarithm per password
  guess". SRP is weaker than that: one discrete logarithm per transcript, then only
  the key derivation per guess [derived; the draft does not discuss SRP].
- What the attacker needs first: the transcript, which sits inside TLS. A hybrid
  group with a pin the attacker cannot forge keeps it out of reach. SRP therefore
  adds no independent post-quantum layer; its quantum resistance is whatever the
  TLS hop provides.
- What it buys after the fact: the invitation is consumed and the phrase is
  single-purpose, so a recovered phrase matters mainly if the owner reused it. The
  larger prize is the bearer credential and the JWK. It is not limited to the
  pairing transcript: every directory fetch sends `Authorization: DPoP <token>` and
  a proof carrying the JWK on a fresh connection, so **one recorded classical
  exchange within the credential's lifetime suffices**. Shor on Curve25519 turns the
  public JWK into a signing key, and the forged proof needs no server nonce
  (`authz/remote_pairing.go:49` expects none), so bearer credential plus signing
  key authenticate the directory connection until it expires (up to 365 days) or is
  revoked. Its authority is the existing directory connection class only (ADR
  "Outcome"); it grants no secret values. That is why the proof signature is the
  separable piece, and why hybrid on the pairing hop alone does not close it.

The stored verifier and the phrase never cross the wire **between the two
instances**: on the remote both are sealed under the instance keyring and the
enrollment is computed at `Start` (`remote_pairing.go:458`). The phrase does cross
two management hops, each outside this analysis: the administrator enters it in
`CreateInvite` (`remote_pairing.go:233`) and `Prepare` returns it to the requester's
caller (`remote_pairing.go:223`).

## Feasibility survey

Fetched 2026-11-03.

| Item | Finding | Evidence |
| --- | --- | --- |
| Standardized PQ PAKE (IETF, CFRG) | None found. CFRG RFCs that are PAKEs: RFC 8125 (requirements), RFC 9382 (SPAKE2), RFC 9807 (OPAQUE). CPace is `draft-irtf-cfrg-cpace-21`, in the RFC Editor queue. All are classical; RFC 9807 names post-quantum AKEs only as a possible future integration | [web-tool] CFRG document list; [data] CPace revision 21, IRTF stream |
| PQ PAKE draft | `draft-vos-cfrg-pqpake-03` (OQUAKE, CPaceOQUAKE, plus augmented variants), revision 03 dated 2026-10-14, expires 2027-04-17, **no IETF or IRTF stream**, individual draft. **Scope (section 2):** long-lived-password pairing is "the primary use case targeted by CPaceOQUAKE+", and the draft also defines symmetric variants; device pairing with a one-time high-entropy password is "a comparatively weak motivating example" because recovering a one-time password "has little value to an attacker", and an ephemeral password where the PAKE does not establish the channel is "not a motivating use case". A long-lived password confirmed over an already-secure channel is a separate case, for which "the full machinery of a PQ aPAKE is not required". Hikyo's shape (one-time phrase, over TLS, session key discarded) is close to the one-time cases, a reused phrase is close to the already-secure-channel case, and short, user-chosen phrases fall outside the draft's high-entropy case. **Review (11.1):** "the hybrid protocol is comparatively new and has not yet received significant peer review (compared to the non-hybrid PAKEs). However, the core sequential-composition design has been independently analyzed by at least three different groups". **Quantum (11.2):** proofs are UC in the classical random-oracle model; [BJ26] shows OQUAKE-style protocols do not realize the standard UC PAKE functionality against quantum random-oracle access, "this does not lead to an attack that recovers passwords or keys", and [BJ26] proves "stand-alone game-based security in the quantum random oracle model for a closely related protocol". Open items remain (11.7.2; section 12: an unpublished bare-PAKE proof extension and identity requirements). Appendix E carries test vectors from a reference implementation | [data] revision, date, stream, expiry; [doc] quoted text (sections 1, 2, 11.1 to 11.3, 11.7.2, 12) |
| EAP use of it | `draft-tschofenig-emu-pqpake-00`, 2026-11-02, individual draft (no stream in the registry), Experimental: an EAP method built on CPaceOQUAKE+. It shows early downstream interest, not maturity or a Go library | [data] date, stream; [doc] status and abstract |
| Kemeleon encoding | `draft-irtf-cfrg-kemeleon-02`, 2026-07-03, IRTF stream (CFRG), Informational: encodes ML-KEM encapsulation keys and ciphertexts as random bytestrings. OQUAKE depends on it; it is an encoding, not a PAKE | [data] date, stream; [doc] abstract |
| NIST | The post-quantum project page lists FIPS 203, 204, 205 (final, August 2024) and ongoing Falcon and HQC work. No PAKE standard or project found | [web-tool] |
| Maintained Go implementation | None found by search on 2026-11-03 for OQUAKE, CPaceOQUAKE or any ML-KEM PAKE; the search summary is not evidence of absence. Go `crypto/mlkem` and the HPKE hybrid KEM are KEMs, not PAKEs. `go-srp` v0.0.7 is the latest tag and is classical | [web-tool] search; [web-tool] tags |
| ML-DSA in JOSE | RFC 9964, "ML-DSA for JOSE and COSE", Proposed Standard, 2026-05-20 (IETF stream). `alg` `ML-DSA-44`/`-65`/`-87`, `kty` `AKP`, members `alg`, `pub` (required), `priv` (32-byte seed). Empty context string, no HashML-DSA. The same identifiers and the JWK shape appear in `go-jose` source | [data] number, date, level; [web-tool] contents; [src] corroborated by `go-jose` |
| ML-DSA in DPoP | RFC 9449 requires an asymmetric JWS algorithm and a `jwk` header and allows one `DPoP` header. It does not name ML-DSA. No DPoP profile for ML-DSA found. RFC 9964 does not mention DPoP | [web-tool] |
| `go-jose` | v4.1.8 (module time 2026-11-02T17:56:45Z, sum `h1:k38WifTutpGuqt6Imq9xW1OgCvnWFri0Nz7FnfiACM8=`) adds `mldsa.go` (`//go:build go1.27`): AKP JWK parse and marshal, the three algorithms, thumbprint input `{"alg":...,"kty":"AKP","pub":...}`, signing with an empty context. Without Go 1.27 a stub reports unsupported. Hikyo pins v4.1.6 | [src] module source |
| Reverse proxy header limits | nginx defaults `large_client_header_buffers 4 8k`; a request header field cannot exceed one buffer or the client gets 400 | [web-tool] nginx documentation |

Not read: RFC 9964 beyond its first 100,000 characters, OPAQUE's security section,
any security audit of `go-jose`'s ML-DSA code, and those parts of the three drafts
not named in the Evidence column (the draft-vos test-vector appendices and
protocol bodies were not reviewed).

## The separable parts

| Part | Blocker | Status |
| --- | --- | --- |
| PAKE (SRP-2048) | No standardized, maintained, reviewed PQ PAKE. A KEM added to SRP is excluded by the issue. The password guarantee to preserve is: weak phrases are only ever guessed online. Only a PAKE gives that, so a non-PAKE replacement reduces it | HOLD. Dependency: a standard or reviewed profile with a maintained Go implementation and vectors |
| Proof of possession (Ed25519 DPoP) | See the table below. The library is no longer the blocker | Not blocked by a missing standard; blocked by Hikyo bounds and owner decisions |
| TLS pin and its source | The pin is a hash of a classical public key read over a classical chain. A PQ-authenticated pin needs a PQ chain (#1133) or a PQ-authenticated channel | HOLD under E-01 and E-20 |

### What blocks a PQ proof, specifically

[measured] with `go-jose` v4.1.8 on Go 1.27.3, building the proof as `dpop.go` does
(`PublicJWK` sets `use` to `sig`, `phrase.go:72`, so every JWK below includes it;
URL `https://remote.example/api/v1/instance/directory`, a 32-character `jti`, a
43-character `ath`; ML-DSA-65 public key 1952 bytes, signature 3309 bytes):

| Quantity | Ed25519 now | ML-DSA-44 | ML-DSA-65 | ML-DSA-87 | Bound in code |
| --- | --- | --- | --- | --- | --- |
| JWK JSON | 105 | 1802 | **2655** | 3508 | `ParsePublicJWK` 2048 (`dpop.go:178`) |
| Header JSON | 144 | 1845 | **2698** | 3551 | decoded header 2048 (`dpop.go:120`) |
| Whole compact proof | 520 | 5929 | **8252** | 11147 | `MaxProofBytes` 8192 (`dpop.go:24`) |

A longer origin or a nonce adds to the 8252 further. `VerifyDPoP` checks the total
first (`dpop.go:112`), so ML-DSA-65 fails there; the JWK and header bounds would
still fail if the total were raised, and so would the OpenAPI caps below.
ML-DSA-44 fits all of them and is excluded by DEC-02 (category 2); that is why the
bounds bind, not a preference for it.

| Blocker | Detail | Owner decision needed |
| --- | --- | --- |
| Size bounds | Three constants above are closed, and the generated API repeats two: `public_jwk` `maxLength: 2048` (`api/openapi.yaml:16076`) and `proof` `maxLength: 8192` (`:16108`), so generated clients follow. Raising any is a new bound profile (DEC-08 chose "smallest fit, no global raise") | A reviewed bound for this surface |
| Closed key and algorithm sets | `ParsePublicJWK` accepts only `OKP`/`Ed25519`; `VerifyDPoP` requires `alg == EdDSA` and `ParseSignedCompact(..., EdDSA)`; `PublicKeyThumbprint` accepts only an Ed25519 key. 64-byte Ed25519 private keys are hard-coded where the paired credential is stored and where the requester state is opened (`remotefetch/pairing.go:30` and `:57`, `service/remote_pairing.go:701`). A new algorithm is widening | Whether and how the allowed set widens |
| Context string | `go-jose` signs with an empty ML-DSA context, as RFC 9964 specifies. DEC-02 and DEC-07 say the label is the ML-DSA `Options.Context` and the first signed field. A JOSE ML-DSA proof cannot carry a domain label in the context | Accept RFC 9964's empty context for this surface, or hold |
| `jwk` is mandatory in the header | RFC 9449 puts the full public key in every proof; it cannot be moved out. The proof carries 1952 bytes of key every request | Accept the size, or a different wire shape |
| Dual-signature composition | DEC-03 (a) requires all required signatures during the dual window. DPoP allows one header and the verifier accepts one compact signature (`len(signed.Signatures) != 1`). No existing form carries two. DEC-03 (b) allows PQ-only for new surfaces; an enrolled binding already stores a thumbprint, and the AKP thumbprint includes `alg` | Which of DEC-03 (a) or (b) applies to credentials, and for which surface |
| Proxy limits | An 8258-byte header line (`DPoP: ` plus the 8252-byte proof) exceeds the 8k per-field default of nginx. Hikyo's own server allows 64 KiB (`app.go:589`) | Header qualification of supported edges |
| Dependency | `go-jose` v4.1.6 to v4.1.8 changes a pinned cryptographic dependency; the v4.1.7 release notes (read through the summarizing tool, unverified) list five security fixes | Dependency review and approval |
| Wire and storage | The AKP thumbprint is still 32 bytes, so `ContextSalt` (decodes to exactly 32) and the `thumbprint` columns fit. A stored paired credential holding a 32-byte seed would be well within its 8192 limit (`remotefetch/pairing.go:45`) but would fail the 64-byte key-length checks above. Not tested | None |
| Enrollment | Even with a PQ proof, enrollment is authorized by classical SRP over a classical pin. A forger holding the pinned key can relay the exchange and read the token. Enrolling its own PQ key still needs the phrase, because the requester thumbprint is in the SRP salt: one it knows, or can guess within the invitation's lifetime and attempt budget | Accept as the residual gap (option B below) |

## Options for the owner

No option is selected here. DEC-13 is the owner's.

| | Option | Consequence |
| --- | --- | --- |
| A | Stay HOLD under E-01; #1130 open; #1135 qualifies I20 at its classical profile only and states no PQ claim for it (scope-narrowing text in the [handoff](../../handoff/1130-pairing-hold.md)) | No change to code or behavior. Pairing carries no PQ authenticity, pin or password claim. A recorded classical pairing or directory exchange exposes the bearer credential and public key, and any guessable phrase, to a future adversary. Honest and cheap; the gap stays |
| B | A, and additionally authorize a separate ticket for a PQ proof of possession only, leaving SRP and the pin classical | Closes the long-lived part: the credential could no longer be forged from a public key. Needs the owner decisions above (bounds, widening, context, DEC-03 form, dependency). Does not make pairing PQ: enrollment and the pin stay classical, so the ticket's "PQ-required pairing cannot negotiate a classical exchange" cannot be met. Needs its own security approval |
| C | Wait for a standardized or CFRG-reviewed PQ PAKE and a maintained Go implementation with vectors; recheck at the triggers | The only route to the ticket as written. No date can be given. Until then A or B. Scope caution: the only candidate's own text names long-lived passwords as CPaceOQUAKE+'s primary use case and calls one-time device pairing a comparatively weak motivating example, so a draft adopted for that purpose may not fit Hikyo's short, user-chosen phrases without further review; the exposure analysis above likewise weighs the PAKE less than the proof |
| D | Redesign pairing (DEC-13 (c)) | Not an implementation of #1130 as written; a new protocol design needing its own ADR and review |

A hybrid KEM wrapped around SRP, or a KEM-derived key in place of the PAKE, is
the excluded construction (DEC-13 (b)) and is not an option.

## Pairing under strict transport (what the code does now)

PR #1156 sets this hop at compat (E-08, hop table rows "Pairing fetch to a remote
Hikyo" and "Pairing proxy hop"). [src] what that means:

- `tlspolicy.Profile.Apply` is called only for the native listener
  (`internal/app/tls.go:51`), `cli.NewClient` (`internal/cli/client.go:217`) and
  `opclient.NewClient` (`internal/operator/client/client.go:159`). The pairing
  clients build their own configs: `remotefetch.httpClient` (`remotefetch.go:246`),
  the proxy dial (`remotefetch.go:303`) and the identity probe
  (`pairing_identity.go:58`), each with `MinVersion` TLS 1.2 and no group list.
- So a strict profile on the **requesting** instance changes nothing for pairing:
  it cannot select strict for the fetch client and cannot make it refuse a
  classical remote.
- A strict profile on the **remote's** native listener makes that listener refuse a
  requester that offers no `X25519MLKEM768` (a requester run with
  `GODEBUG=tlsmlkem=0`, which Go honours, `crypto/tls/defaults.go`), before any
  request byte is delivered. A default Go 1.27 requester offers the group, so it
  would normally negotiate it. That is a property of the listener and Go's
  defaults, not of the pairing client.
- If the public origin is a proxy or ingress terminator, the listener's profile
  says nothing about that hop, and the SPKI pin the invitation carries is the
  terminator's key, read over a classical chain.
- Strict gives confidentiality only here (E-20): the pin replaces chain
  verification, so no authenticity claim exists on this hop. Nothing in strict
  touches SRP or DPoP.
- The negotiated group on a real pairing hop was not observed: NOTRUN, the same as
  every hop in the #1127 matrix.

Reproduce the source facts, from the repository root:

```sh
rg -n 'tlspolicy' internal/remotefetch --glob '!*_test.go'   # import and SPKIFingerprint
rg -n '\.Apply\(' internal/app/tls.go internal/cli/client.go internal/operator/client/client.go
rg -n 'MinVersion' internal/remotefetch --glob '!*_test.go'
go test -p 2 -count=1 ./internal/pairing/ ./internal/remotefetch/
```

Run on 2026-11-03 at `f5d549182`: the first command shows two matches (the import
and the fingerprint helper, `remotefetch.go:37` and `:155`), the second the three
listed `Apply` sites, the third the three TLS 1.2 floors and no group setting, and
both test packages pass (local evidence). These establish only that the pairing
clients are outside the policy seam; they do not observe a handshake.

## Recheck triggers

A PQ PAKE adopted by CFRG or published as an RFC, or an independent security
analysis of OQUAKE not already cited by `draft-vos-cfrg-pqpake-03`; a maintained Go implementation with published vectors; a
`go-jose` release touching ML-DSA or DPoP, or a Go minor release; an IETF draft or
registry entry for ML-DSA in DPoP; an owner decision on the DPoP bounds (DEC-08),
the empty-context rule against DEC-07, or the DEC-03 form for credentials; header
qualification of supported edges; NIST action on PAKEs; a published practical attack
on SRP-2048 parameters or on ML-DSA. A trigger starts a recheck, not a build.

## E-01 as registered, and a proposed row

[migration](migration.md#exception-register) says an exception is "effective only
once its row has its peer, version, expiry and recheck trigger, as DEC-10
requires", and DEC-10 caps expiry at 180 days or the next trigger. The E-01 row
(Surface "Pairing, DPoP, SRP, pins (I20)"; Limitation "Classical authenticity;
active forger captures transcript; pins from a classical chain"; Child #1130;
Decision DEC-13) has none of those fields, as no row in that table does. Its
limitation also omits the recorder case established above. So the pairing ADR
banner records an exception that is not yet effective. The owner can accept the
fields below; they are a proposal and nothing in the register or in
`migration.md` is edited here.

| Field | Proposed value |
| --- | --- |
| Owner | Marc Went |
| Peer | Any remote Hikyo instance reached by pairing (`/api/v1/auth/pairing/*`) or by a paired directory fetch (`/api/v1/instance/directory`) |
| Version | Invitation bundle version 1, SRP version 4 (`go-srp` v0.0.7) and the Ed25519 DPoP profile, as in the source at `f5d549182`; the owner names the first Hikyo release it applies to |
| Limitation | Classical authenticity only. (1) Active forger: a forged classical pinned leaf receives the capability, the SRP messages and the bearer credential. (2) Recorder: an adversary who later breaks the classical key exchange of any recorded pairing or directory exchange obtains the bearer credential and public key, and with a quantum computer the DPoP signing key, and can authenticate the connection until the credential expires or is revoked (at most 365 days). A guessable phrase in a recorded pairing is recoverable offline. (3) The pin is read over a classical chain |
| Claim limitation | No PQ authenticity, pin or password-exchange claim for pairing. No confidentiality claim for the pairing hop at compat (E-08); a hybrid group protects only exchanges that were hybrid when recorded |
| Expiry | At most 180 days from acceptance or the next trigger (2027-05-02 if accepted on 2026-11-03); at most two renewals, then an ADR-recorded decision (DEC-10) |
| Recheck trigger | The triggers above |
