# #1134: external provider, SSH and federation compatibility, handoff

Status: **discovery and register; documentation and tests only; not an
operative capability claim.**
Refs [#1134](https://github.com/Hikyo-Org/Hikyo/issues/1134); the PR does not
close it. On 2026-11-04 the owner (Marc Went) authorized merge to `dev` when green
and accepted publication in the next nightly; that is the maintainer security
approval for this PR only. It does not decide F-09 or any other open item, and it
does not authorize release or deployment. Open and undecided: F-09 (per-route
strict selection, [#1183](https://github.com/Hikyo-Org/Hikyo/issues/1183)), the
other F-nn dispositions (F-01 to F-11 are #1175 to #1185), and every proposed
exception term, which stays ineffective until its peer is named (DEC-10).
Independent adversarial review was done on `claude-opus-5-5` (see Independent
review); cross-provider review is SKIPPED by explicit owner instruction
(2026-11-02) and is not CLEAN.

Stacked on PR #1156 (`dunky13/pqc-1127-transport-policy`). The register and the
tests were written and run on its head `f5d5491829d6`; the base has since moved
and the route sweep was not rerun on it.
No ADR amendment is proposed for this ticket (the register names none for SSH,
federation or authenticators), so no locked ADR is touched.

**Tested SHA for the Go tests and lint: `613b3978ed6fd8df9525824d04d3ecd2d23edf48`.**
Every commit since touches documentation only, so the Go results stand.

## Result

The register is [external-compatibility](../security/pqc/external-compatibility.md):
24 routes (X01 to X24), two tables, residual risk by route class, an allowed
capability statement, a named HOLD (F-09), proposed exception terms (none
effective) and a local qualification record. No route is verified supported.
The parent claim stays blocked.

## Independent review

Three review passes on `claude-opus-5-5`, plus a fourth on 2026-11-04:

| Pass | Reviewed | Outcome | Repair |
| --- | --- | --- | --- |
| 1 | `374e7e6bc` | Defective | `613b3978e` |
| 2 | `74d1fcc82` | Four medium and five low | `da1c3f357` |
| 3 | `da1c3f357` | Six findings, none above medium | `9b4517d66`, `8117b44d0`, `912b4f5c8` |
| 4 (2026-11-04) | `912b4f5c8` | Six findings (stale filing wording; X23 framed as a quantum risk although the default IMDS and ECS endpoints are plain HTTP; DEC-03 misquoted; `internal/diagnostics` callers; stale handoff status; an undisclosed edit) | The commit that carries this text |

The repairs for passes 3 and 4 were made after checking each finding against
source and have no further independent review. Cross-provider review: SKIPPED by
explicit owner instruction (2026-11-02).

## Disclosed edit outside this ticket's files

`613b3978e` edited the TLS hop row in `docs/security/pqc/qualification.md` (the
qualification matrix of an owner-approved package) without saying so in the first
handoff or the PR. The change narrows the claim: strict-only refusal is stated for
strict-marked hops, and on compat hops the group is observed and nothing is
refused. The wording matches the #1127 transport policy.

## Facts that differ from the register (owner decides)

1. OpenSSH 10.6 (2026-10-06) enables the composite PQ signature
   `ssh-mldsa44-ed25519`; the 10.6 tag defines `ssh-mldsa44-ed25519-cert` and
   lists both in its default key algorithms. The PQ page is stale. E-02 stands
   (ML-DSA-44 is excluded by DEC-02, composites not selected by DEC-03, no
   `x/crypto/ssh` support), but the DEC-16 and migration text "no PQ
   signatures" is wrong, and the successor draft
   `draft-ietf-sshm-composite-sigs-00` is an active SSHM Standards Track draft.
2. RFC 9964 (May 2026): IANA COSE algorithms -48/-49/-50 (Recommended: Yes; AKP 7
   is in the key-type registry, which has no such column) and JOSE `alg`
   `ML-DSA-44/65/87` (Implementation Requirements: Optional); `go-jose` v4.1.6
   has none.
3. WebAuthn L3 is a Recommendation (2026-08-25) and says nothing about ML-DSA.
4. `HIKYO_TLS_PROFILE` is a server setting for the listener only; the CLI uses
   the trust entry `tls_profile`; the operator client has a library option that
   its factory never passes, so no deployed operator can select strict. No
   external hop reads any selector.
5. GitHub's SSH offers `mlkem768x25519-sha256` (changelog 2026-09-22, enabled
   2026-10-14, US region excluded); a credential-free handshake on 2026-11-03
   negotiated it with GitHub's published host key.
6. GitHub documents "Libsodium sealed boxes" (concepts page), not X25519.
7. PostgreSQL 18's default `ssl_groups` is `X25519:prime256v1`.
8. OIDC also accepts PS256/384/512; inventory I18 omits them (not edited here).
9. AWS KMS has ML-KEM key specs; Hikyo uses no KMS key for custody. SOPS imports
   can contact KMS, Vault Transit and other key services (X15).
10. The AWS blog covers KMS, ACM and Secrets Manager only; Hikyo also calls STS on
    a separate endpoint and uses SDK-owned ambient credential sources (X23, read
    from code, not run). The installers use host `curl` (X24).
11. The SSH composite draft lineage has four steps, starting with an ML-DSA-65
    composite draft that the current draft dropped.

## Code changed (tests only)

| File | Purpose |
| --- | --- |
| `internal/crypto/sealed_box_test.go` | Pins the libsodium sealed-box wire shape (48-byte overhead, fresh ephemeral key, tamper refused) |
| `internal/sshca/openssh_negotiation_test.go` | Real OpenSSH client and sshd: per CA algorithm (Ed25519, ECDSA P-256, RSA 3072) records the negotiated KEX and host key, the offered user key type, and the CA signature family from `ssh-keygen -L`; asserts only on those lines; classical-only client accepted by default sshd and refused by a hybrid-only sshd; an ML-DSA SSH key is refused. `HIKYO_SSHE2E_REQUIRED=1` fails instead of skipping on an old client |
| `internal/sshca/openssh_e2e_test.go` | `startSSHD` takes extra config, `login` wraps `loginWith` returning output; behavior unchanged |

No production code changed. Default behavior is unchanged (DEC-05). No lane sets
`HIKYO_SSHE2E_REQUIRED`, so no hosted lane is known to run the OpenSSH tests with
OpenSSH 10 (NOTRUN); they are local evidence.

## Acceptance checklist

| Item | Status | Notes |
| --- | --- | --- |
| Inventory every route with contract, versions, lifetime, KEX, signatures, anchor, wrapping, owner, evidence | PARTIAL | X01 to X24 from a stated sweep (register "Sweep scope"; Go plus two installer scripts); a later census can add rows. X19 to X22 were added after the first review, X23 (AWS STS and ambient chain) and X24 (installers) after the second. X23's default IMDS and ECS endpoints are plain HTTP, so that leg needs no quantum capability to attack; the re-sweep also found the SAML metadata proxy leg (in X11) |
| Verify current documentation; distinguish GitHub sealed box from storage and TLS, OpenSSH KEX from signatures | DONE | Read 2026-11-03; provider storage stays unknown |
| Cover SSH CA and KRL, WebAuthn/COSE, OIDC/JWKS, SAML, database, cloud wrapping, trust services | DONE | X08 to X16; X14 and X16 now cite primary AWS and Sigstore documents; X16 is unknown, not blocked |
| Classify each route with reason, owner, trigger, impact, follow-up; residual risk; deferral has owner disposition | PARTIAL | Owner named; the owner has not recorded dispositions. F-01 to F-11 are filed as #1175 to #1185 and defined in the register. Residual risk is per route class, with X16, X18, X20, X23 and X24 stated separately |
| Qualify a supported route with actual-client fixtures | PARTIAL | Local OpenSSH 10.3p1 and libsodium 1.0.22 only. 10.6, real providers, PostgreSQL hybrid, federation and mail: NOTRUN |
| Strict refusal before secrets; unrelated routes stay up; exception nameable | HOLD | F-09: per-route selector and X01 meaning are design decisions; nothing built. **#1134 can close only after the owner records a disposition for F-09** (narrow it under DEC-06, or move it to a named ticket with an owner) |
| Keep least privilege, egress, redaction; no replacement envelope | DONE | No production change |
| Recovery, enrollment and rotation paths; separate forgery from confidentiality risk | DONE | Existing paths only; nothing created |
| No real-provider write or trust change | DONE | One credential-free SSH handshake to github.com offered no key; no write |
| Publish matrix and capability statement; parent claim blocked | DONE | Statement in the register |

## Evidence by class

| Class | Result |
| --- | --- |
| source | Register rows; pinned versions from `go.mod`; module source searched for `go-jose` ML-DSA; OpenSSH `V_10_6_P1` source |
| local | Tested SHA above: `go test -p 2 -count=1 ./internal/sshca/ ./internal/crypto/` PASS (OpenSSH 10.3p1, Go 1.27.3 darwin/arm64); one-off libsodium 1.0.22 interop PASS both ways, helper not committed |
| local, repository checks | On the tested SHA: `go test -p 2 -count=1 -timeout 40m ./internal/lint/...` PASS (1387 s). A first run with Go's default 10 minute timeout FAILED at 601 s under host load and is not counted; `scripts/ci/fast-feedback.sh` through the queue with base `origin/dunky13/pqc-1127-transport-policy` PASS (diff check, Go formatting, test compile of `internal/crypto` and `internal/sshca`); `check-commit-signatures.sh` and `check-dco.sh` against that base PASS for the two commits then on the branch |
| hosted CI | At `912b4f5c8`: `feedback` (go, tooling, frontend) and `repository invariants (boundary, lint, CI policy)` PASS. `ci-queue-required` FAILED with "Only the protected default branch can enter this merge queue", because this PR is stacked on a non-default base; it is not a content failure. The commits after `912b4f5c8` are documentation only; read their checks on the PR |
| NOTRUN | `scripts/ci/check-doc-status.mjs` (docs/site dependencies not installed; no status ledger change); provider, peer, runtime, deployment |

## Key retention and rollback

No key, certificate, authenticator or trust anchor is created or changed. Rollback
is reverting the documentation and the three test files.

## Next step

Owner authorized merge on 2026-11-04 once required checks are green, bottom-up
after #1156. Still open for the owner: record dispositions for F-01 to F-11 (F-09
first, since #1134 can close only after it) and name peers for the exception
terms. Authorized peer captures (F-02) need a named environment.
