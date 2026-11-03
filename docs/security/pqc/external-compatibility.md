# External compatibility and residual-risk register, v1 (2026-11-03)

Status: **draft for review, not operative. Owner dispositions are proposed, not
recorded.** Tracking [#1134](https://github.com/Hikyo-Org/Hikyo/issues/1134),
under [#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123). Stacked on the
transport policy (PR #1156, head `f5d5491829d6`; main merge-base
`b22eb0adb6480b3b2c61b8c86dee6df1a5a552a8`). Route rows `Xnn` are new here;
`Inn` is [inventory](inventory.md), `E-nn` and `DEC-nn` are the
[migration register](migration.md#exception-register) and the
[ADR register](../../adr/post-quantum-migration.md#decision-register). This
document changes no DEC row and builds no mechanism. A claim here is only what
a row proves. "Read" dates are when the primary source was read for this
document: **2026-11-03**.

## Facts that differ from the register

1. **OpenSSH now has a PQ signature.** The register says OpenSSH has none
   (migration "SSH, federation, authenticators", inventory I25, DEC-16). The
   [release notes](https://www.openssh.org/releasenotes.html) say 10.4
   (2026-07-06) added experimental `mldsa44-ed25519` (ML-DSA-44 plus Ed25519,
   composite) and 10.6 (2026-10-06) enabled `ssh-mldsa44-ed25519` under that name.
   The upstream source (master, not the 10.6 tag) has a matching certificate
   type. The [PQ page](https://www.openssh.org/pq.html) still says "in the
   future", so it is stale. **E-02 stands, for different reasons**: the scheme is
   ML-DSA-44 (DEC-02 excludes it), its draft is an individual submission
   replaced by another draft, and `golang.org/x/crypto` v0.58.0 has no composite
   type (only pure ML-DSA with `@golang.org` certificate names). Local OpenSSH is
   10.3p1, so 10.6 behavior is NOTRUN.
2. **RFC 9964 is published** (ML-DSA for JOSE and COSE, May 2026). IANA lists
   COSE `ML-DSA-44/65/87` = -48/-49/-50 and key type AKP = 7 as permanent
   (registry updated 2026-08-25), and the JOSE `alg` `ML-DSA-65`. Migration says
   the JOSE identifier status "was not checked". `go-jose` v4.1.6 still has no
   ML-DSA, so the HOLD on #1130 and E-04 hold, but "no identifier exists" is no
   longer the reason.
3. **WebAuthn Level 3 is a W3C Recommendation (2026-08-25).** Its full text
   contains no mention of ML-DSA or post-quantum (0 matches, whole document
   read locally). ML-DSA credentials ride the generic COSE algorithm field.
4. **The strict profile reaches no external hop.** `HIKYO_TLS_PROFILE` is read
   by the listener, CLI and operator client only. No adapter, federation, mail,
   SAML metadata, PostgreSQL or SDK transport reads it (searched under
   `internal/{adapter,federationhttp,mail,oidcfed,oidcrp,oauth2rp,jwkssource,dynamic}`
   and `internal/service/saml_providers.go`). This is by decision (hop table,
   E-08) but it means **no external route is refused or protected by strict
   today**; see [strict-route refusal](#strict-route-refusal).
5. **GitHub's own SSH uses `sntrup761x25519-sha512`**, not ML-KEM, and not in
   the US data-residency region ([GitHub blog](https://github.blog/engineering/platform-security/post-quantum-security-for-ssh-access-on-github/),
   2025-09-15). The GitHub secrets docs name only "libsodium"; X25519 is a
   property of libsodium's sealed box, not a statement GitHub makes.
6. **PostgreSQL 18's default `ssl_groups` is `X25519:prime256v1`**: no hybrid
   group unless the operator sets one.
7. Statements now stale, for the owner's next version, not edited here:
   migration "SSH, federation, authenticators" ("no PQ signatures"), inventory
   I25, the DEC-16 consequence column, and migration's "JOSE ML-DSA identifier's
   status was not checked". A dated pointer in migration links this document.

## Classifications

| Class | Meaning here |
| --- | --- |
| verified supported | An actual client and peer pair ran with the claimed PQ property and downgrade behavior observed |
| compatibility-only | Works today; no PQ claim is possible or made; the peer or provider controls the algorithm |
| blocked | A PQ path needs something unavailable (standard, library, owner decision) |
| unknown | Not read or not run; no claim |
| out of profile | Hikyo is not a party to the cryptography or the owner excluded it |

**No route is "verified supported".** Local fixtures qualified two partial
properties (X01 wire format, X07 negotiation) and none qualified a PQ
authenticity property.

## Table A: facts

Lifetime `H` is the confidentiality or verification horizon from the
[inventory](inventory.md). "Peer/library" lists what was read or pinned.
"Anchor" is what vouches for the peer.

| ID | Route (inventory) | Protocol or contract | Peer / library version | H | KEX / encryption | Signature family | Anchor | Wrapping and recovery |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| X01 | GitHub Actions secret submission (I15) | `PUT .../actions/secrets` with `encrypted_value` and `key_id`, value "encrypted with LibSodium" ([docs](https://docs.github.com/en/rest/actions/secrets)) | Hikyo `x/crypto` v0.58.0 `nacl/box`; libsodium 1.0.22 (local) | Until the secret is rotated or the repo key is | Sealed box: X25519 + XSalsa20-Poly1305, ephemeral sender key, no sender authentication ([libsodium](https://libsodium.gitbook.io/doc/public-key_cryptography/sealed_boxes)). Provider key version via `key_id` | none (anonymous box); transport authenticity is X02 | Provider public key fetched over X02 | Provider-held decryption key; recovery is re-fetch and re-submit (`internal/crypto/sealed_box.go:20`, `internal/adapter/githubactions/module.go:27`) |
| X02 | GitHub REST API TLS and provider storage (I14) | HTTPS, `MinVersion` TLS 1.2 (`githubactions/client.go:312`) | GitHub endpoint version unknown; Go 1.27.3 offers `X25519MLKEM768` by default ([Go docs](https://pkg.go.dev/crypto/tls)) | Provider retains plaintext after decrypt | Negotiated group unknown (NOTRUN). Provider storage encryption not documented in sources read | Classical X.509 chain, system roots | WebPKI | Credential refresh |
| X03 | GitLab, Forgejo (I14) | HTTPS REST (`gitlab/client.go:194`, `forgejo/client.go:110`) | Peer-defined (SaaS or self-hosted) | As X02 | Peer-controlled; unknown | Classical chain, optional pin or bundle (GitLab) | Operator CA or WebPKI | Credential refresh |
| X04 | Vault KV, OpenBao (I14, I30) | HTTPS (`vaultkv/credential.go:133`); import via SDK `openbao/api/v2` v2.7.1 | Peer build unknown | As X02 | Peer-controlled; unknown | Classical chain | Operator CA | Credential refresh |
| X05 | AWS Secrets Manager (I14) | HTTPS via `aws-sdk-go-v2` v1.47.5 (`awssm/client.go:166`) | AWS docs: PQ TLS on Secrets Manager endpoints in all Regions except China ([doc](https://docs.aws.amazon.com/secretsmanager/latest/userguide/pqtls.html)); AWS blog (search summary only, not read in full): non-FIPS endpoints | As X02 | Documented: hybrid ML-KEM-768 on supported endpoints; client must offer it. Hikyo's Go TLS offers it by default. Negotiated group NOTRUN | Classical chain | WebPKI | Credential refresh; AWS-side KMS wrapping not read |
| X06 | Cloudflare API (I14) | HTTPS (`cloudflare/client.go:185`) | Cloudflare docs: sites and APIs served through Cloudflare over TLS 1.3 support hybrid ([doc](https://developers.cloudflare.com/ssl/post-quantum-cryptography/)); `api.cloudflare.com` is not named | As X02 | Documented for Cloudflare-served APIs; this host unknown | ML-DSA only Cloudflare-to-origin; visitor side "still under development" | WebPKI | Token refresh |
| X07 | OpenSSH session key exchange (I25; Hikyo is not a party) | SSH transport; KEX named per release | Local OpenSSH 10.3p1 (LibreSSL 3.3.6); doc: `mlkem768x25519-sha256` default since 10.0, RFC 10042 (Informational, Aug 2026) | Session lifetime | **Hybrid**: `mlkem768x25519-sha256` observed (fixture below). GitHub's SSH: `sntrup761x25519-sha512` | Host key classical (`ssh-ed25519` in fixture) | Operator `known_hosts` | n/a |
| X08 | SSH CA certificates and KRL (I25) | OpenSSH user certificates and KRL (`internal/sshca`) | Hikyo `x/crypto/ssh` v0.58.0; OpenSSH 10.6 has `ssh-mldsa44-ed25519` (read) | Certificate TTL | n/a | **Classical**: CA Ed25519, ECDSA P-256 or RSA at least 3072 (`sshca/keys.go:39-45`). User signature `ED25519-CERT` observed | sshd `TrustedUserCAKeys` / KRL | CA rollover; KRL; sealed CA key |
| X09 | WebAuthn / COSE (I19) | WebAuthn L3 REC 2026-08-25; COSE (RFC 9964 IANA entries) | `go-webauthn` v0.18.2 (verifies ML-DSA AKP on go1.27); authenticator versions unknown | Credential lifetime | n/a | RP offers the library default list, classical (`webauthnrp.go:104`, no `WithCredentialParameters`); no ML-DSA credential is created | Authenticator and RP ID | Other credentials; recovery codes |
| X10 | OIDC id_token and JWKS signing (I17, I18) | OIDC/JOSE; RFC 9964 defines JOSE `ML-DSA-65` | `go-oidc` v3.21.0, `go-jose` v4.1.6 (no ML-DSA, searched module) | Token validity | n/a | Accepts RS256/384/512, ES256/384/512 only (`oidcrp.go:34`, `oidcfed.go:119,128`) | IdP JWKS over X11 | Admin reconfigures; re-fetch |
| X11 | Federation TLS: discovery, JWKS, token, userinfo, SAML metadata (I17) | HTTPS (`federationhttp/client.go:137`, `ca.go:57`, `saml_providers.go:980`, `oidcfed.go:343`) | IdP peer unknown | Token/assertion | Peer-controlled; Go default offered; unknown | Classical chain | IdP CA or WebPKI | Re-fetch |
| X12 | SAML SP and IdP signing (I18) | XML-DSig; allowlist `samlsp/response.go:43` | `gosaml2` v0.12.0, `goxmldsig` v1.6.1; no XML-DSig ML-DSA identifier found | Assertion window; SP key 10 years | n/a | RSA-SHA256/384/512, ECDSA-SHA256/384/512; SP key RSA-3072 | IdP metadata, confirmed | SP key rollover |
| X13 | Database client crypto: primary and dynamic PostgreSQL (I12, I13) | PostgreSQL TLS; `verify-full` enforced (`config.go:1404`, `postgres.go:94`) | `pgx` v5.11.0; server PostgreSQL 18 default `ssl_groups` `X25519:prime256v1` ([docs](https://www.postgresql.org/docs/current/runtime-config-connection.html)) | Database copies | Server-controlled; hybrid needs an operator-set group on an OpenSSL build that has it (not verified) | Classical chain | Operator CA | Database restore |
| X14 | Cloud and provider key wrapping (I28 sources; provider storage) | AWS KMS, SOPS KMS key sources; Hikyo custody is software only (DEC-12) | AWS KMS ML-DSA signing keys GA June 2025 (AWS page, search summary only); ML-KEM only as TLS transport | Source retention | Not Hikyo's; provider-defined | Provider-defined | Provider | Re-import from source |
| X15 | SOPS and ambient SDK imports (I28, I30; E-13) | SOPS, kubeconfig, ambient `VAULT_*` | `sops` v3.13.3, `client-go` v0.37.1 | Source ciphertext | Source key policy (age, PGP, KMS) | Source | Source operator | Re-import |
| X16 | Third-party trust services (I26, I27, I32; E-03) | Sigstore, Apple, OS packages, Go checksum database, commit signing | Sigstore 2025 blog (search summary only): ML-DSA support pending Go; no statement that the public-good service issues PQ certificates | Release history | n/a | Classical (ECDSA) | Service operators | Vendor process |
| X17 | Mail relay (I16) | SMTP over TLS or STARTTLS, `MinVersion` TLS 1.2 (`mail.go:107`) | `go-mail` v0.8.1, `go-smtp` v0.25.0; relay unknown | Retained mail | Relay-controlled; unknown | Classical chain | Relay CA | Credential update |
| X18 | Adapter and TLS terminators outside the process (I10, I14 proxies) | Reverse proxy, ingress, CDN | Operator topology unknown | Unknown | Outside the process | Operator | Operator | Operator |

## Table B: disposition

Classification, evidence class (source / local / provider), owner, recheck
trigger, consumer impact if a strict-style policy is demanded, concrete
follow-up, and the exception. Owner for every row is **Marc Went** (default
under DEC-10). Follow-ups `F-nn` are **proposed, unfiled**; this work created
no issue.

| ID | Class | Evidence (read 2026-11-03) | Recheck trigger | Consumer impact | Follow-up | Exception |
| --- | --- | --- | --- | --- | --- | --- |
| X01 | compatibility-only | source: GitHub docs, libsodium doc; local: wire-shape test, libsodium 1.0.22 interop both ways; provider NOTRUN | GitHub docs change the algorithm, name PQ or a new `key_id` scheme | A strict policy can only disable the route; no PQ alternative may replace the envelope | F-01: owner decides whether strict means "route disabled" (HOLD) | E-06 |
| X02 | unknown | source: Go docs; no GitHub HTTPS evidence read; provider NOTRUN | GitHub states a PQ TLS group, or a peer capture is authorized | Unknown | F-02: authorized peer capture, read-only, named environment | E-07, E-08 |
| X03 | unknown | none beyond source | A self-hosted peer's build is named | Unknown | F-02 | E-07, E-08 |
| X04 | unknown | none beyond source | Peer build named | Unknown | F-02 | E-07, E-08, E-13 |
| X05 | compatibility-only | source: AWS doc; client offer from Go docs; negotiated group NOTRUN; FIPS endpoints excluded by AWS | Authorized capture; AWS changes endpoint support | Strict would work only on non-FIPS endpoints if the group is negotiated | F-02 | E-07, E-08 |
| X06 | unknown | source: Cloudflare doc does not name `api.cloudflare.com` | Capture or doc names the host | Unknown | F-02 | E-07, E-08 |
| X07 | out of profile | local: OpenSSH 10.3p1 negotiated `mlkem768x25519-sha256`; classical-only client accepted by default sshd; refused with `KexAlgorithms mlkem768x25519-sha256`; `ssh -Q kex` lists `sntrup761x25519-sha512` and `mlkem768x25519-sha256` | OpenSSH adds PQ signatures to the default set; operator names a peer fleet | Operator-controlled by sshd `KexAlgorithms` and `WarnWeakCrypto` (sshd option from 10.6, notes); Hikyo carries no session | F-03: runbook line for sshd operators (#1135) | E-02 limits only the signatures |
| X08 | blocked | source: closed set refuses ML-DSA names; local: Ed25519 CA certificate and KRL accepted by real sshd; 10.6 composite NOTRUN | x/crypto adds the OpenSSH composite; OpenSSH adds ML-DSA-65; owner revisits DEC-02; draft status changes | Existing classical CAs unchanged; no strict possible | F-04: owner decision on the composite (ML-DSA-44, outside DEC-02) before any code | E-02 |
| X09 | unknown | source: IANA, W3C, go-webauthn; authenticator support only from vendor blogs (not primary), NOTRUN | A primary authenticator or platform statement; owner decides credential parameters | RP creates classical credentials only; offering library PQC parameters would include ML-DSA-44 (excluded by DEC-02) | F-05: owner decision on RP credential parameters and recovery path | E-05 |
| X10 | blocked | source: RFC 9964 and Hikyo allowlists; `go-jose` has no ML-DSA | `go-jose` adds ML-DSA; an IdP issues one | Federation with a PQ-only IdP would fail at verify (closed allowlist) | F-06: #1130/#1132 note: library path | E-04 |
| X11 | compatibility-only | source only | As X02 | Unknown | F-02 | E-04, E-08 |
| X12 | blocked | source: allowlist; no XML-DSig ML-DSA identifier found by search | A standards body defines the identifier; `goxmldsig` supports it | Same as X10 | F-06 | E-04 |
| X13 | compatibility-only | source: PostgreSQL 18 docs; `verify-full` enforced; negotiated group NOTRUN | Authorized fixture (disposable PostgreSQL with hybrid group); operator names server build | Strict is not selectable; server-controlled | F-07: disposable-server fixture (HOLD until a hybrid-capable PostgreSQL is named) | E-08 |
| X14 | out of profile | source: AWS page (search summary) | External custody is designed (not decided) | None for Hikyo keys | none | E-13 |
| X15 | unknown | source only | Source key policy named per import | Imports carry the source's policy | F-08: import capability record states "no PQ claim" (#1135) | E-13 |
| X16 | blocked | source: E-03 | Sigstore states PQ issuance | None | none (E-03) | E-03 |
| X17 | unknown | source only | Relay named | Unknown | F-02 | E-07 |
| X18 | out of profile | none | Topology supplied | Outside the claim | none | E-08 |

## Allowed capability statement

Candidate: this branch, on `f5d5491829d6`. For every route in this register:

- Hikyo claims **no post-quantum confidentiality and no post-quantum
  authenticity**. No route is verified supported. "PQ-ready" is not allowed.
- Hikyo's own Go clients offer the library-default hybrid group
  (`X25519MLKEM768`) on external hops, which is Go's default, not Hikyo policy.
  Whether it is negotiated is the peer's choice and was **not observed in
  production** on any route (NOTRUN).
- X01: the value is sealed to the provider's X25519 key with a libsodium
  sealed box. Confidentiality of that payload rests on X25519 and is classical.
  The route is not replaced or wrapped. Provider storage and TLS are unknown.
- X07/X08: Hikyo issues only classical SSH CA certificates. A PQ session key
  exchange protects traffic only where the operator's sshd negotiates it
  (observed locally with OpenSSH 10.3p1). Hikyo makes no claim about it.
- X09 to X12: signatures verified by Hikyo are RSA, ECDSA or Ed25519. A PQ
  signature from an IdP or authenticator is refused (OIDC, SAML) or not
  requested (WebAuthn). No PQ authenticity claim.
- Authentication by the peer is separate from key exchange everywhere.
- **Unresolved first-party gaps keep the parent claim (#1123) blocked**: this
  document does not close any, and adds a HOLD (below).

## Strict-route refusal

What #1156 enforces: `Profile.Apply` on the native listener, CLI and operator
client; `requireStrictTLS` refusal at startup, managed Apply and certificate
preparation; a strict CLI trust entry refuses a classical-only or TLS 1.2 peer
before any request byte or credential (`internal/cli/client_tls_profile_internal_test.go`,
`internal/tlspolicy/profile_test.go` `TestHandshakeMatrix`). Those are the only
routes with refusal.

What exists elsewhere and fails closed today for identity: the closed SSH
algorithm set (`ErrUnsupportedKey`), the OIDC and SAML signature allowlists,
`verify-full` for remote PostgreSQL. An unknown algorithm is refused before it
is accepted.

**HOLD (not built, no code):** per-route strict selection for adapters,
federation, mail, SAML metadata, PostgreSQL and SDK hops, and runtime exception
naming. Why: it needs (a) a per-hop selector, because wiring `HIKYO_TLS_PROFILE`
into those transports extends DEC-06's scope (strict only on the listener and
owned clients) and would let a strict listener break unrelated routes; (b) a
peer-evidence row per hop (E-08); (c) a decision whether "strict" for a
provider-required envelope (X01) means the route is disabled. All are design
decisions. Owner: Marc Went. Operators name an exception today by `Xnn` and
`E-nn` in the exception terms below, not by a runtime control. Unrelated routes
are unaffected by `HIKYO_TLS_PROFILE=strict` today, and so are the unsafe ones.

Added instead, as executable evidence of existing fail-closed behavior:
`TestParseAlgorithmRefusesMLDSA`, `TestParsePublicKeyRefusesAnMLDSAKey`
(`internal/sshca`) and `TestSealAnonymousBoxIsTheLibsodiumSealedBoxShape`
(`internal/crypto`).

## Local qualification record

Candidate: this branch, Go 1.27.3 darwin/arm64, local only, loopback sshd, no
real provider, no host or trust change.

| Check | Result |
| --- | --- |
| `go test -p 2 -count=1 -v ./internal/sshca/ -run 'OpenSSH\|ParsePublicKey\|ParseAlgorithm'` with OpenSSH 10.3p1 (LibreSSL 3.3.6) | PASS. Negotiated `kex=mlkem768x25519-sha256`, host key `ssh-ed25519`, CA `ssh-ed25519`, user signature `ED25519-CERT`. Classical-only client (`curve25519-sha256`): default sshd accepts, hybrid-only sshd refuses with "no matching key exchange method" before authentication |
| `ssh -Q kex` / `ssh -Q key-cert` on 10.3p1 | KEX list includes `sntrup761x25519-sha512` and `mlkem768x25519-sha256`; no ML-DSA key or certificate type (predates 10.4) |
| libsodium 1.0.22 helper (built in a scratch directory, not committed), temporary Go test | Hikyo `SealAnonymousBox` output opened by `crypto_box_seal_open`; libsodium `crypto_box_seal` output (63 bytes) opened by `box.OpenAnonymous`. Both PASS. The helper and test were removed; the repo test pins the 48-byte overhead and freshness |
| GitHub real endpoint, any real provider, OpenSSH 10.6, PostgreSQL 18 with a hybrid group, federation or mail peer | **NOTRUN**: no authorization or fixture. Not contacted |

## Exception terms (proposed)

Effective only when the owner records them (DEC-10). The expiry is 180 days from
2026-11-03: **2027-05-02**, at most two renewals. Owner: Marc Went. These terms
are mirrored as a pointer in the [migration register](migration.md#exception-terms-1134).

| ID | Routes | Exact peer and version | Recheck trigger |
| --- | --- | --- | --- |
| E-02 | X07, X08 | OpenSSH 10.3p1 local; 10.6 `ssh-mldsa44-ed25519` unqualified; `x/crypto/ssh` v0.58.0 | x/crypto adds composite; OpenSSH adds ML-DSA-65; DEC-02 revisited |
| E-04 | X10, X11, X12 | `go-oidc` v3.21.0, `go-jose` v4.1.6, `gosaml2` v0.12.0, `goxmldsig` v1.6.1; IdP unnamed | `go-jose` ML-DSA; XML-DSig identifier; an IdP issues PQ |
| E-05 | X09 | `go-webauthn` v0.18.2; authenticators unnamed | Primary authenticator statement; DEC-02 decision on parameters |
| E-06 | X01 | GitHub REST (docs read 2026-11-03), libsodium 1.0.22 | GitHub docs change algorithm |
| E-07 | X02 to X06, X11, X17 | Peers unnamed; Go 1.27.3 client | A peer is named and captured |
| E-13 | X04, X14, X15 | `sops` v3.13.3, `client-go` v0.37.1, `openbao/api/v2` v2.7.1 | Source key policy named |

## Recovery, enrollment and rotation

Existing paths are unchanged and listed in Table A: SSH CA rollover with two
trusted CAs and KRL (exercised by `TestOpenSSHAuthenticationKRLExpiryAndRotation`),
SAML SP rollover, WebAuthn other credentials and recovery codes, GitHub
re-fetch and re-submit. Future live-authentication forgery (SSH, SAML, OIDC,
WebAuthn) is separate from captured-traffic confidentiality (X01, X02, X07);
this document makes neither claim. No credential, certificate, authenticator or
trust anchor was created, rotated or enrolled.

## Sources read 2026-11-03

OpenSSH [PQ page](https://www.openssh.org/pq.html) and
[release notes](https://www.openssh.org/releasenotes.html) (10.6, 2026-10-06; the
page and notes disagree), OpenSSH portable source `ssh-mldsa-eddsa.c`, `sshkey.c`
(master); [draft-sfluhrer-ssh-mldsa-08](https://datatracker.ietf.org/doc/draft-sfluhrer-ssh-mldsa/)
(individual, not adopted);
[RFC 10042](https://datatracker.ietf.org/doc/draft-ietf-sshm-mlkem-hybrid-kex/);
draft-miller-sshm-mldsa44-ed25519-composite-sigs-00 (replaced);
[IANA COSE registry](https://www.iana.org/assignments/cose/cose.xhtml);
[RFC 9964](https://datatracker.ietf.org/doc/draft-ietf-cose-dilithium/);
[WebAuthn L3](https://www.w3.org/TR/webauthn-3/); GitHub
[secrets guide](https://docs.github.com/en/rest/guides/encrypting-secrets-for-the-rest-api),
[REST reference](https://docs.github.com/en/rest/actions/secrets),
[SSH blog](https://github.blog/engineering/platform-security/post-quantum-security-for-ssh-access-on-github/);
[libsodium sealed boxes](https://libsodium.gitbook.io/doc/public-key_cryptography/sealed_boxes);
[AWS Secrets Manager PQ TLS](https://docs.aws.amazon.com/secretsmanager/latest/userguide/pqtls.html);
[Cloudflare PQ](https://developers.cloudflare.com/ssl/post-quantum-cryptography/);
[PostgreSQL 18](https://www.postgresql.org/docs/current/runtime-config-connection.html);
[Go `crypto/tls`](https://pkg.go.dev/crypto/tls). Seen only as search-result
summaries, not read in full, so they support no row alone: AWS KMS and ACM ML-KEM
blog, AWS KMS ML-DSA announcement, Sigstore 2025 blog, FIDO vendor commentary.
