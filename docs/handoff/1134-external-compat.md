# #1134: external provider, SSH and federation compatibility, handoff

Status: **draft, discovery and register. Not merge-ready, not operative.**
Refs [#1134](https://github.com/Hikyo-Org/Hikyo/issues/1134); the PR does not
close it. Security approval: not given. Independent adversarial review is
required; cross-provider review is SKIPPED by explicit owner instruction
(2026-11-02) and is not CLEAN.

Stacked on PR #1156 (`dunky13/pqc-1127-transport-policy`, head `f5d5491829d6`).
No ADR amendment is proposed for this ticket (the register names none for SSH,
federation or authenticators), so no locked ADR is touched.

## Result

The register is [external-compatibility](../security/pqc/external-compatibility.md):
18 routes, two tables, an allowed capability statement, exception terms and a
local qualification record. No route is verified supported. The parent claim
stays blocked.

## Facts that differ from the register (owner decides)

1. OpenSSH 10.6 (2026-10-06) enables the composite PQ signature
   `ssh-mldsa44-ed25519` with a certificate type; the PQ page is stale. E-02
   stands (ML-DSA-44 is excluded by DEC-02, the draft is replaced, `x/crypto/ssh`
   v0.58.0 cannot produce it), but the DEC-16 and migration text "no PQ
   signatures" is wrong.
2. RFC 9964 (May 2026) registers the JOSE `ML-DSA-65` and COSE -48/-49/-50
   identifiers permanently; `go-jose` v4.1.6 has none.
3. WebAuthn L3 is a Recommendation (2026-08-25) and says nothing about ML-DSA.
4. `HIKYO_TLS_PROFILE=strict` reaches no external hop; no adapter, federation,
   mail, SAML, PostgreSQL or SDK transport reads it.
5. PostgreSQL 18's default `ssl_groups` is `X25519:prime256v1`.
6. GitHub's SSH uses `sntrup761x25519-sha512`; its secrets docs say only
   "libsodium".

## Code changed (tests only)

| File | Purpose |
| --- | --- |
| `internal/crypto/sealed_box_test.go` | Pins the libsodium sealed-box wire shape (48-byte overhead, fresh ephemeral key, tamper refused) |
| `internal/sshca/openssh_negotiation_test.go` | Real OpenSSH client and sshd: records KEX, host key and certificate signature family; classical-only client accepted by default sshd and refused by a hybrid-only sshd; ML-DSA algorithm names and keys refused |
| `internal/sshca/openssh_e2e_test.go` | `startSSHD` takes extra config, `login` wraps `loginWith` returning output; behavior unchanged |

No production code changed. Default behavior is unchanged (DEC-05).

## Acceptance checklist

| Item | Status | Notes |
| --- | --- | --- |
| Inventory every route with contract, versions, lifetime, KEX, signatures, anchor, wrapping, owner, evidence | DONE | Tables A and B |
| Verify current documentation; distinguish GitHub sealed box from storage and TLS, OpenSSH KEX from signatures | DONE | Read 2026-11-03; storage and provider TLS stay unknown |
| Cover SSH CA and KRL, WebAuthn/COSE, OIDC/JWKS, SAML, database, cloud wrapping, trust services | DONE | X08 to X16 |
| Classify each route with reason, owner, trigger, impact, follow-up; deferral has owner disposition | PARTIAL | Owner is named; the owner has not recorded dispositions, so exception terms are "proposed". F-01 to F-08 are proposed and **unfiled** |
| Qualify a supported route with actual-client fixtures | PARTIAL | Local OpenSSH 10.3p1 and libsodium 1.0.22 only. 10.6, real providers, PostgreSQL hybrid, federation and mail: NOTRUN |
| Strict refusal before secrets; unrelated routes stay up; exception nameable | HOLD | Per-route selector and X01 meaning are design decisions. Nothing built. Exceptions are nameable by `Xnn` and `E-nn`, not by a runtime control |
| Keep least privilege, egress, redaction; no replacement envelope | DONE | No production change |
| Recovery, enrollment and rotation paths; separate forgery from confidentiality risk | DONE | Existing paths only; nothing created |
| No real-provider write or trust change | DONE | None contacted |
| Publish matrix and capability statement; parent claim blocked | DONE | Statement in the register |

## Evidence by class

| Class | Result |
| --- | --- |
| source | Register rows; pinned versions from `go.mod`; module source searched for `go-jose` ML-DSA |
| local | New tests PASS on OpenSSH 10.3p1 and Go 1.27.3 darwin/arm64; one-off libsodium 1.0.22 interop PASS both ways, helper not committed |
| hosted CI | NOTRUN |
| provider, peer, runtime, deployment | NOTRUN |

Checks run before the PR are recorded in the PR body.

## Key retention and rollback

No key, certificate, authenticator or trust anchor is created or changed. Rollback
is reverting the documentation and the three test files.

## Next authorized step

Independent Standards and Spec review, then the owner records or amends the
exception terms and dispositions for F-01 to F-08. Authorized peer captures
(F-02) need a named environment.
