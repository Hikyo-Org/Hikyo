# External compatibility and residual-risk register, v2 (2026-11-03)

Status: **draft for review, not operative. Owner dispositions are proposed, not
recorded.** Tracking [#1134](https://github.com/Hikyo-Org/Hikyo/issues/1134),
under [#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123). Stacked on the
transport policy (PR #1156, head `f5d5491829d6`; main merge-base
`b22eb0adb6480b3b2c61b8c86dee6df1a5a552a8`). The tested candidate is named in the
[handoff](../../handoff/1134-external-compat.md). Route rows `Xnn` are new here;
`Inn` is [inventory](inventory.md), `E-nn` and `DEC-nn` are the
[migration register](migration.md#exception-register) and the
[ADR register](../../adr/post-quantum-migration.md#decision-register). This
document changes no DEC row and builds no mechanism. A claim here is only what
a row proves. "Read" means the primary source was read for this document on
**2026-11-03**.

## Facts that differ from the register

1. **OpenSSH now has a PQ signature.** The register says OpenSSH has none
   (migration "SSH, federation, authenticators", inventory I25, DEC-16). The
   [release notes](https://www.openssh.org/releasenotes.html) say 10.4
   (2026-07-06) added experimental `mldsa44-ed25519` (ML-DSA-44 plus Ed25519,
   composite) and 10.6 (2026-10-06) enabled `ssh-mldsa44-ed25519`. The 10.6 tag
   (`V_10_6_P1`) defines the certificate type `ssh-mldsa44-ed25519-cert`
   (`ssh-mldsa-eddsa.c`) and lists both names in the default key algorithms
   (`myproposal.h`). The [PQ page](https://www.openssh.org/pq.html) still says
   "in the future", so it is stale. **E-02 stands, for different reasons**:
   - the scheme is ML-DSA-44, which DEC-02 excludes (category 2), and DEC-03 did not select a
     composite (option (d));
   - `golang.org/x/crypto` v0.58.0 has no composite type, only pure ML-DSA with
     `@golang.org` certificate names;
   - the standards track is moving: the first draft
     (draft-miller-sshm-mldsa44-ed25519-composite-sigs-00) is replaced by
     [draft-ietf-sshm-composite-sigs-00](https://datatracker.ietf.org/doc/draft-ietf-sshm-composite-sigs/)
     (SSHM working group stream, Standards Track intended, 2026-08-21), which also
     defines `ssh-mldsa87-p384`. It defines key and signature names, not
     certificate names; OpenSSH's certificate type is its own.

   Local OpenSSH is 10.3p1, so 10.6 behavior is NOTRUN.
2. **RFC 9964 is published** ([ML-DSA for JOSE and COSE](https://www.rfc-editor.org/rfc/rfc9964),
   May 2026). The [IANA COSE registry](https://www.iana.org/assignments/cose/cose.xhtml)
   lists `ML-DSA-44/65/87` = -48/-49/-50 and key type `AKP` = 7, all with
   reference RFC 9964 and, for the algorithms, "Recommended: Yes" (registered
   2025-04-24, updated 2025-10-24). RFC 9964 registers JOSE `alg` `ML-DSA-65`
   (implementation requirement Optional; the IANA JOSE registry was not read).
   Migration says the JOSE identifier status "was not checked". `go-jose` v4.1.6
   still has no ML-DSA, so the HOLD on #1130 and E-04 hold, but "no identifier
   exists" is no longer the reason.
3. **WebAuthn Level 3 is a W3C Recommendation (2026-08-25).** Its full text
   contains no mention of ML-DSA or post-quantum (0 matches, whole document
   read locally). ML-DSA credentials ride the generic COSE algorithm field.
4. **The strict profile reaches no external hop, and not every selector is
   deployable.** Each selector: the server reads `HIKYO_TLS_PROFILE`
   (`internal/config/config.go:537`) for the listener only; the CLI uses the
   per-origin trust entry field `tls_profile` (`internal/cli/trust.go:96`); the
   operator client has a library option `WithTLSProfile` that its
   `defaultClientFactory` (`internal/operator/clientadapter.go:22`) never
   passes, so **no deployed operator can select strict** (as
   [transport policy](../../operations/transport-policy.md) says). No adapter,
   federation, mail, SAML metadata, PostgreSQL, update, pairing or SDK transport
   reads any of them. This is by decision (hop table, E-08) but **no external
   route is refused or protected by strict today**; see
   [strict-route refusal](#strict-route-refusal).
5. **GitHub's SSH now offers `mlkem768x25519-sha256`.** The
   [GitHub changelog of 2026-09-22](https://github.blog/changelog/2026-09-22-security-improvements-for-ssh/)
   says it will be enabled on github.com and GitHub Enterprise Cloud with data
   residency, **except the US region**, on 2026-10-14, and in GitHub Enterprise
   Server 3.24. The [2025-09-15 blog](https://github.blog/engineering/platform-security/post-quantum-security-for-ssh-access-on-github/)
   had announced `sntrup761x25519-sha512` (also not in the US region). A
   credential-free handshake from OpenSSH 10.3p1 to github.com on 2026-11-03
   negotiated `mlkem768x25519-sha256` with host key `ssh-ed25519`, fingerprint
   `SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU`, equal to the Ed25519
   fingerprint in `api.github.com/meta`; it ended at "Permission denied
   (publickey)" with no key offered. Whether `sntrup761x25519-sha512` is still
   offered was not tested. Hikyo's GitHub adapter uses HTTPS, not SSH, so this
   is peer context (X07), not a Hikyo route.
6. **GitHub documents "Libsodium sealed boxes", not X25519.** The
   [concepts page](https://docs.github.com/en/actions/concepts/security/secrets)
   says "Secrets use Libsodium sealed boxes, so that they are encrypted before
   reaching GitHub"; the [REST guide](https://docs.github.com/en/rest/guides/encrypting-secrets-for-the-rest-api)
   says "encrypt the secret value using libsodium". Neither names X25519; that
   is a property of [libsodium's sealed box](https://libsodium.gitbook.io/doc/public-key_cryptography/sealed_boxes).
7. **PostgreSQL 18's default `ssl_groups` is `X25519:prime256v1`**: no hybrid
   group unless the operator sets one.
8. **OIDC also accepts PS256/384/512.** `internal/oidcrp/oidcrp.go:34-38`
   allows RS, ES and PS families; **inventory I18 omits PS** and is wrong by that
   much. The signature family is still classical.
9. **AWS KMS has ML-KEM key specs.** The [key spec reference](https://docs.aws.amazon.com/kms/latest/developerguide/asymmetric-key-specs.html)
   lists `ML_KEM_512/768/1024` (Encapsulate, Decapsulate) beside `ML_DSA_44/65/87`.
   Hikyo uses no KMS key for its own custody (software, DEC-12), so this is out of profile
   today but matters if an external custody seam is ever designed.
10. Statements now stale, for the owner's next version, not edited here:
    migration "SSH, federation, authenticators" ("no PQ signatures"), inventory
    I25 and I18, the DEC-16 consequence column, and migration's "JOSE ML-DSA
    identifier's status was not checked". A dated pointer in migration links this
    document.

## Classifications

| Class | Meaning here |
| --- | --- |
| verified supported | An actual client and peer pair ran with the claimed PQ property and downgrade behavior observed |
| compatibility-only | A primary document or a first-party decision describes the peer, the route works, and no PQ claim is possible or made |
| blocked | A PQ path needs something unavailable (standard, library, owner decision) |
| unknown | Peer behavior not read or not run; no claim |
| out of profile | Hikyo is not a party to the cryptography or the owner excluded it |

**No route is "verified supported".** Local fixtures qualified two partial
properties (X01 wire format, X07/X08 negotiation) and none qualified a PQ
authenticity property.

## Sweep scope

Routes were gathered by searching non-test Go under `internal` and `cmd` for
`http.Client{`, `http.Transport{`, `DefaultTransport`, `DefaultClient`, `tls.Dial`,
`tls.Config{`, `net.Dial`, `.DialContext`, `ProxyFromEnvironment`,
`rest.InClusterConfig`, `rest.Config`, `clientcmd`, `kubernetes.New`,
`http.NewRequest`, `http.Get(`, `.Do(req`. Every hit is a row below or local-only:
the CLI and operator clients to Hikyo (I11, owned, #1127), `internal/updater`
(unix socket), `internal/hostupgrade` and `internal/app/unattended_process.go`
(local health probes configured by the operator), `internal/netpolicy` (library),
`internal/mailtest`, `internal/tlstest` (fixtures). `internal/repscan` runs a
local `git` with remote access disabled. Inbound listeners are I09 (#1127). A
later census that finds another outbound client must add a row.

## Table A: facts

Lifetime `H` is the confidentiality or verification horizon from the
[inventory](inventory.md). "Chain" cells say "not observed" where no handshake
was captured: Go's verifier also accepts ML-DSA chains (profiles.md TLS detail),
so a classical chain is never assumed.

| ID | Route (inventory) | Protocol or contract | Peer / library version | H | KEX / encryption | Signature family | Anchor | Wrapping and recovery |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| X01 | GitHub Actions secret submission (I15) | `PUT .../actions/secrets` with `encrypted_value` and `key_id`, value "encrypted with LibSodium" ([REST reference](https://docs.github.com/en/rest/actions/secrets)) | Hikyo `x/crypto` v0.58.0 `nacl/box`; libsodium 1.0.22 (local) | Until the secret or repo key is rotated | Sealed box: X25519 + XSalsa20-Poly1305, ephemeral sender key, no sender authentication (libsodium doc). Provider key version via `key_id` | none (anonymous box); transport authenticity is X02 | Provider public key fetched over X02 | Provider-held decryption key; recovery is re-fetch and re-submit (`internal/crypto/sealed_box.go:20`, `internal/adapter/githubactions/module.go:27`) |
| X02 | GitHub REST API TLS and provider storage (I14) | HTTPS, `MinVersion` TLS 1.2 (`githubactions/client.go:312`) | GitHub endpoint version unknown; Go 1.27.3 offers `X25519MLKEM768` by default ([Go docs](https://pkg.go.dev/crypto/tls)) | Provider retains plaintext after decrypt | Negotiated group unknown (NOTRUN). Provider storage encryption not documented in sources read | Chain not observed | WebPKI | Credential refresh |
| X03 | GitLab, Forgejo (I14) | HTTPS REST (`gitlab/client.go:194`, `forgejo/client.go:110`) | Peer-defined (SaaS or self-hosted) | As X02 | Peer-controlled; unknown | Chain not observed; optional pin or bundle (GitLab) | Operator CA or WebPKI | Credential refresh |
| X04 | Vault KV adapter (I14) | HTTPS (`vaultkv/credential.go:133`) | Peer build unknown | As X02 | Peer-controlled; unknown | Chain not observed | Operator CA | Credential refresh |
| X05 | AWS Secrets Manager (I14) | HTTPS via `aws-sdk-go-v2` v1.47.5 (`awssm/client.go:166`) | [AWS blog 2025-04-07](https://aws.amazon.com/blogs/security/ml-kem-post-quantum-tls-now-supported-in-aws-kms-acm-and-secrets-manager/): ML-KEM (`X25519MLKEM768`) on **non-FIPS** endpoints in all `aws`-partition Regions, client must offer it; FIPS endpoints are not mentioned. [Secrets Manager doc](https://docs.aws.amazon.com/secretsmanager/latest/userguide/pqtls.html): all Regions except China, no group named | As X02 | Documented: hybrid on supported endpoints. Hikyo's Go TLS offers it by default. Negotiated group NOTRUN | Chain not observed | WebPKI | Credential refresh |
| X06 | Cloudflare API (I14) | HTTPS (`cloudflare/client.go:185`) | [Cloudflare docs](https://developers.cloudflare.com/ssl/post-quantum-cryptography/): sites and APIs served through Cloudflare over TLS 1.3 support hybrid; `api.cloudflare.com` is not named | As X02 | Documented for Cloudflare-served APIs; this host unknown | ML-DSA only Cloudflare-to-origin; visitor side "still under development" (doc) | WebPKI | Token refresh |
| X07 | OpenSSH session key exchange (I25; Hikyo is not a party) | SSH transport | Local OpenSSH 10.3p1 (LibreSSL 3.3.6); `mlkem768x25519-sha256` default since 10.0 ([RFC 10042](https://www.rfc-editor.org/rfc/rfc10042), Informational, Aug 2026); github.com offers it (fact 5) | Session lifetime | **Hybrid**: `mlkem768x25519-sha256` observed (fixture) | Host key `ssh-ed25519` in the fixture (a fixture property); a 10.6 client also offers `ssh-mldsa44-ed25519` | Operator `known_hosts` | n/a |
| X08 | SSH CA certificates and KRL (I25) | OpenSSH user certificates and KRL (`internal/sshca`) | Hikyo `x/crypto/ssh` v0.58.0; OpenSSH 10.6 composite (fact 1) | Certificate TTL | n/a | **Classical** CA, observed with `ssh-keygen -L`: Ed25519 (`ssh-ed25519`), ECDSA P-256 (`ecdsa-sha2-nistp256`), RSA 3072 (`rsa-sha2-512`) (`sshca/keys.go:39-45`). The user key in the fixture is Ed25519 (`ED25519-CERT` is its type, not the CA's signature) | sshd `TrustedUserCAKeys` / KRL | CA rollover; KRL; sealed CA key |
| X09 | WebAuthn / COSE (I19) | WebAuthn L3 REC 2026-08-25; COSE IANA entries (fact 2) | `go-webauthn` v0.18.2 (verifies ML-DSA AKP on go1.27); authenticator versions unknown | Credential lifetime | n/a | RP offers the library default list, classical (`webauthnrp.go:104`, no `WithCredentialParameters`); no ML-DSA credential is created | Authenticator and RP ID | Other credentials; recovery codes |
| X10 | OIDC id_token and JWKS signing (I17, I18) | OIDC/JOSE; RFC 9964 defines JOSE `ML-DSA-65` | `go-oidc` v3.21.0, `go-jose` v4.1.6 (no ML-DSA, module searched) | Token validity | n/a | Accepts RS256/384/512, ES256/384/512, PS256/384/512 only (`oidcrp.go:34-38`, `oidcfed.go:119,128`) | IdP JWKS over X11 | Admin reconfigures; re-fetch |
| X11 | Federation TLS: discovery, JWKS, token, userinfo, SAML metadata (I17) | HTTPS (`federationhttp/client.go:137`, `ca.go:57`, `saml_providers.go:980`, `oidcfed.go:343`, `oauth2rp`) | IdP peer unknown | Token or assertion | Peer-controlled; Go default offered; unknown | Chain not observed | IdP CA or WebPKI | Re-fetch |
| X12 | SAML SP and IdP signing (I18) | XML-DSig; allowlist `samlsp/response.go:43` | `gosaml2` v0.12.0, `goxmldsig` v1.6.1; no XML-DSig ML-DSA identifier found | Assertion window; SP key 10 years | n/a | RSA-SHA256/384/512, ECDSA-SHA256/384/512; SP key RSA-3072 | IdP metadata, confirmed | SP key rollover |
| X13 | Database client crypto: primary and dynamic PostgreSQL (I12, I13) | PostgreSQL TLS; `verify-full` required except `localhost`, loopback IPs and unix sockets (`config.go:1395-1408`, `postgres.go:94`) | `pgx` v5.11.0; PostgreSQL 18 default `ssl_groups` `X25519:prime256v1` ([docs](https://www.postgresql.org/docs/current/runtime-config-connection.html)) | Database copies | Server-controlled; hybrid needs an operator-set group on an OpenSSL build that has it (not verified) | Chain not observed | Operator CA | Database restore |
| X14 | Cloud and provider key wrapping (I28 sources; provider storage) | AWS KMS, SOPS KMS key sources; Hikyo custody is software only (DEC-12) | AWS KMS [key specs](https://docs.aws.amazon.com/kms/latest/developerguide/asymmetric-key-specs.html) (undated page): `ML_DSA_44/65/87`, `ML_KEM_512/768/1024`, `SYMMETRIC_DEFAULT` AES-256-GCM | Source retention | Provider-defined; Hikyo uses none | Provider-defined | Provider | Re-import from source |
| X15 | SOPS, kubeconfig and OpenBao imports, ambient SDK sources (I28, I30) | SOPS, kubeconfig, ambient `VAULT_*` (`importer/k8s.go:251`, `importer/vault.go`) | `sops` v3.13.3, `client-go` v0.37.1, `openbao/api/v2` v2.7.1 | Source ciphertext | Source key policy (age, PGP, KMS) and source TLS | Source | Source operator | Re-import |
| X16 | Third-party trust services (I26, I27, I32; E-03) | Sigstore, Apple, OS packages, Go checksum database, commit signing | [Sigstore blog 2025-06-06](https://blog.sigstore.dev/post-quantum-2025/): the public good instance "will wait for reliable and vetted PQCA to be part of Go's `crypto` package"; no later statement read. Others not read | Release history | n/a | Not read for Apple, OS packages, sumdb, commit signing | Service operators | Vendor process |
| X17 | Mail relay (I16) | SMTP over TLS or STARTTLS, `MinVersion` TLS 1.2 (`mail.go:107`) | `go-mail` v0.8.1, `go-smtp` v0.25.0; relay unknown | Retained mail | Relay-controlled; unknown | Chain not observed | Relay CA | Credential update |
| X18 | TLS terminators outside the process (I10) | Reverse proxy, ingress, CDN | Operator topology unknown | Unknown | Outside the process | Operator | Operator | Operator |
| X19 | Sealed webhook delivery to a receiver (I14, I21) | HTTPS push of an age-encrypted envelope (`sealedwebhook/client.go:118-120`, public-egress dialer) | Receiver build unknown; protocol 1 payload is age X25519 (first-party, PQ version is #1128) | Receiver retention; envelope at most 5 min | Payload: age X25519 (classical). TLS hop: receiver-controlled; unknown | Chain not observed; ack Ed25519 | Receiver CA or WebPKI | New generation moves the destination id |
| X20 | Update check, release download, self-update (I11, I26) | HTTPS to GitHub release hosts (`updatecheck/source.go:88`, `releaseDownloadHost`; `selfupdate/selfupdate.go:61`), public dialer, TLS 1.2 minimum | GitHub endpoint unknown | Public data; authenticity is I26 | Peer-controlled; unknown | Authenticity is signed metadata (P-256, E-11, E-15), not the TLS chain | WebPKI plus pinned release trust | Reinstall |
| X21 | Pairing: remote fetch, directory, identity probe, proxy leg (I11, I20) | HTTPS to another Hikyo instance with a leaf SPKI pin (`remotefetch/remotefetch.go:248`, `pairing.go`, `directory.go`, `pairing_identity.go:58`) | Peer is a Hikyo instance; its listener may be strict (#1127) | Pairing credentials | Client at compat (Go default offered); peer may be strict | Pin replaces chain verification (E-20); SRP and DPoP classical (E-01) | Pin read over a classical chain | Revoke, re-pair |
| X22 | Kubernetes API and kubelet (I30) | Bearer-token HTTPS via client-go (`cmd/hikyo/config_rollout.go:59`, `app/self_config_deployment.go:106`, `operator/manager.go:124`, `storagehealth/kubernetes.go:60`) | `client-go` v0.37.1; API server and kubelet builds unknown | Short-lived tokens | Cluster-controlled; unknown | Cluster CA, chain not observed | Cluster CA | Token refresh |

## Table B: disposition

Classification, evidence, recheck trigger, consumer impact if a strict-style
policy is demanded, concrete follow-up, and the exception. Owner for every row is
**Marc Went** (default under DEC-10). Follow-ups `F-nn` are **proposed, unfiled**;
this work created no issue.

| ID | Class | Evidence (read 2026-11-03) | Recheck trigger | Consumer impact | Follow-up | Exception |
| --- | --- | --- | --- | --- | --- | --- |
| X01 | compatibility-only | source: GitHub docs, libsodium doc; local: wire-shape test, libsodium 1.0.22 interop both ways; provider NOTRUN | GitHub docs name a different algorithm or PQ, or a new `key_id` scheme | A strict policy can only disable the route; no PQ alternative may replace the envelope | F-01 (HOLD) | E-06 |
| X02 | unknown | source: Go docs; no GitHub HTTPS evidence read; provider NOTRUN | GitHub states a PQ TLS group, or a capture is authorized | Unknown | F-02 | E-07, E-08 |
| X03 | unknown | none beyond source | A peer's build is named | Unknown | F-02 | E-07, E-08 |
| X04 | unknown | none beyond source | Peer build named | Unknown | F-02 | E-07, E-08 |
| X05 | compatibility-only | source: AWS blog and doc; negotiated group NOTRUN; FIPS endpoints not mentioned by either | Authorized capture; AWS states FIPS endpoint support | Strict could work only where a non-FIPS endpoint negotiates the group | F-02 | E-07, E-08 |
| X06 | unknown | source: Cloudflare doc does not name `api.cloudflare.com` | Capture or doc names the host | Unknown | F-02 | E-07, E-08 |
| X07 | out of profile | local: OpenSSH 10.3p1 negotiated `mlkem768x25519-sha256`; classical-only client accepted by default sshd; refused with `KexAlgorithms mlkem768x25519-sha256`; github.com handshake (fact 5) | **Fired:** OpenSSH 10.6 offers a PQ signature by default; remaining: an operator names a fleet | The control is sshd `KexAlgorithms` (fixture). `WarnWeakCrypto` only logs a weak client and is not a control. Hikyo carries no SSH session | F-03: runbook line for sshd operators (#1135) | E-02 limits only the signatures |
| X08 | blocked | source: closed set refuses ML-DSA keys; local: Ed25519, ECDSA and RSA CA certificates accepted by real sshd, CA family read with `ssh-keygen -L`; 10.6 composite NOTRUN | `x/crypto` adds the OpenSSH composite; the composite draft becomes an RFC; OpenSSH or the draft adds ML-DSA-65; owner revisits DEC-02 or DEC-03 | Existing classical CAs unchanged; no strict possible | F-04: owner decision on the composite (ML-DSA-44, outside DEC-02, composite rejected by DEC-03) before any code | E-02 |
| X09 | unknown | source: IANA, W3C, go-webauthn; no primary authenticator or platform statement read | A primary authenticator statement; owner decides credential parameters | RP creates classical credentials only; offering library PQC parameters would include ML-DSA-44 (excluded by DEC-02) | F-05 | E-05 |
| X10 | blocked | source: RFC 9964 and Hikyo allowlists; `go-jose` has no ML-DSA | `go-jose` adds ML-DSA; an IdP issues one | A PQ-only IdP fails at verify (closed allowlist) | F-06: #1130/#1132 note | E-04 |
| X11 | unknown | source only | As X02 | Unknown | F-02 | E-04, E-08 |
| X12 | blocked | source: allowlist; no XML-DSig ML-DSA identifier found by search | A standards body defines the identifier; `goxmldsig` supports it | As X10 | F-06 | E-04 |
| X13 | compatibility-only | source: PostgreSQL 18 docs; `verify-full` enforced; negotiated group NOTRUN | Authorized fixture; operator names the server build | Strict is not selectable; server-controlled | F-07 (HOLD until a hybrid-capable PostgreSQL is named) | E-08 |
| X14 | out of profile | source: AWS key spec reference; Hikyo uses no KMS key | An external custody seam is designed | None for Hikyo keys | none | E-13 |
| X15 | unknown | source only | Source key policy named per import | Imports carry the source's policy | F-08 (#1135) | E-13 |
| X16 | unknown | source: Sigstore blog is 17 months old; nothing current read for the others | Sigstore or a platform states PQ issuance | None | F-02 | E-03 |
| X17 | unknown | source only | Relay named | Unknown | F-02 | E-07, E-08 |
| X18 | out of profile | none | Topology supplied | Outside the claim | none | E-08 |
| X19 | unknown | source only; payload PQ is #1128 | Receiver named; #1128 lands | Strict would affect only the TLS leg; envelope is first-party | F-02; payload: #1128 | E-07, E-08 |
| X20 | unknown | source: hop table says compat; peer not read | GitHub states a TLS group | Strict not selectable | F-02 | E-08; authenticity E-11, E-15 (#1132) |
| X21 | compatibility-only | source: hop table and #1127 handoff (first-party peer, pin-based) | #1130 decides; peer listener strict observed | Strict gives confidentiality only (E-20) | #1130 | E-01, E-08, E-20 (owned by #1130, #1127) |
| X22 | unknown | source only | Cluster build named | Unknown | F-02 | E-08 |

### Residual risk by route class

What a recording adversary gets today, what a future forger gets, and the claim
that limits.

| Routes | A recording adversary gets (classical KEX negotiated or payload classical) | A future quantum forger gets | Claim limited |
| --- | --- | --- | --- |
| X01 | The sealed box is classical X25519 whatever the TLS group: a recorded or provider-stored value is readable once X25519 breaks, because the provider public key is public. Provider storage unknown | Nothing here (anonymous box) | No PQ confidentiality for GitHub-delivered secrets (E-06, E-07) |
| X02 to X06, X11, X17, X19, X20, X22 | The session: API calls, bearer credentials, and for adapters the plaintext secret values or the age ciphertext inside TLS, if the peer negotiates classical | Impersonation of the peer to Hikyo if the chain is classical | None possible; rotate long-lived credentials that crossed these hops after a strict migration (E-07 prior exposure) |
| X07 | Nothing if both ends negotiate the hybrid KEX; the classical exchange is accepted by default sshd | Forged host or user signatures | Operator-owned; Hikyo makes none |
| X08 | Not applicable (no confidentiality) | A forged CA, user or host signature opens sshd for any user the CA principals allow until the certificate expires; a classical CA key is forgeable | No PQ authenticity for SSH CA certificates (E-02) |
| X09, X10, X12 | Not applicable | Forged assertions, tokens or WebAuthn signatures log in as any user | No PQ authenticity for federated or passkey login (E-04, E-05) |
| X13 | The database session (queries, stored values) if classical, or if the server group is unset | Impersonated server | None; server-controlled (E-08) |
| X14 to X16, X18 | Source-defined | Source-defined | Non-claims (E-03, E-13, E-08) |
| X21 | The pairing exchange if the TLS group is classical; SRP transcript for an active forger | Forged pin or peer | No PQ claim for pairing (E-01, E-20) |

## Allowed capability statement

Candidate: the tested head named in the handoff, on `f5d5491829d6`. For every
route in this register:

- Hikyo claims **no post-quantum confidentiality and no post-quantum
  authenticity**. No route is verified supported. "PQ-ready" is not allowed.
- Hikyo's own Go clients offer the library-default hybrid group
  (`X25519MLKEM768`) on external hops, which is Go's default, not Hikyo policy.
  Whether it is negotiated is the peer's choice and was **not observed in
  production** on any route (NOTRUN).
- X01: the value is sealed to the provider's X25519 key with a libsodium
  sealed box. Confidentiality of that payload rests on X25519 and is classical.
  The route is not replaced or wrapped. Provider storage and TLS are unknown.
- X07/X08: Hikyo issues only classical SSH CA certificates (Ed25519, ECDSA
  P-256, RSA 3072). A PQ session key exchange protects traffic only where the
  operator's sshd negotiates it (observed locally with OpenSSH 10.3p1). Hikyo
  makes no claim about it, and none about OpenSSH 10.6's composite signature.
- X09 to X12: signatures verified by Hikyo are RSA, ECDSA or Ed25519. A PQ
  signature from an IdP or authenticator is refused (OIDC, SAML) or not
  requested (WebAuthn). No PQ authenticity claim.
- Authentication by the peer is separate from key exchange everywhere.
- **Unresolved first-party gaps keep the parent claim (#1123) blocked**: this
  document closes none, and adds a HOLD (below).

## Strict-route refusal

What #1156 enforces: `Profile.Apply` on the native listener (server config
`HIKYO_TLS_PROFILE`) and on the CLI client (per-origin trust entry `tls_profile`);
`requireStrictTLS` refusal at startup, managed Apply and certificate preparation.
A strict CLI trust entry refuses a classical-only or TLS 1.2 peer before any
request byte or credential (`internal/cli/client_tls_profile_internal_test.go`
`TestCLIStrictProfileAgainstRealPeers`; `internal/tlspolicy/profile_test.go`
`TestHandshakeMatrix`). The operator client implements the same option, but no
deployed operator selects it (fact 4). Those are the only routes with refusal.

What exists elsewhere and fails closed today for identity: the closed SSH
algorithm set (`ErrUnsupportedKey`), the OIDC and SAML signature allowlists,
`verify-full` for non-local PostgreSQL. An unknown algorithm is refused before
it is accepted.

**HOLD F-09 (not built, no code): per-route strict selection** for adapters,
federation, mail, SAML metadata, PostgreSQL, update, pairing and SDK hops, and
runtime exception naming.

- Why held: it needs (a) a per-hop selector, because wiring `HIKYO_TLS_PROFILE`
  into those transports extends DEC-06's scope (strict only on the listener and
  owned clients) and would let a strict listener break unrelated routes; (b) a
  peer-evidence row per hop (E-08); (c) a decision whether "strict" for a
  provider-required envelope (X01) means the route is disabled. All are design
  decisions.
- Owner: Marc Went. Owning ticket: none yet; the nearest is #1127's hop table
  (E-08). Proposed: file under #1134's parent or extend #1127.
- Recheck trigger: the owner decides to extend DEC-06 scope, or a route gains a
  peer-evidence row showing it can negotiate the strict group.
- Residual risk: every external route stays at compat; strict neither protects
  nor refuses any of them; operators name an exception today only by `Xnn` and
  `E-nn`, not by a runtime control. Unrelated routes are unaffected by
  `HIKYO_TLS_PROFILE=strict` today, and so are the unsafe ones.
- Claim limit: no external route may be called strict, hybrid-enforced or
  PQ-protected (the same limit as E-07, E-08).
- **Closure condition:** #1134 can close only after the owner records a
  disposition for F-09: either narrowing it under DEC-06 (strict stays on the
  listener and owned clients; external hops are an explicit non-claim) or moving
  it to a named ticket with an owner.

Added instead, as executable evidence of existing fail-closed behavior:
`TestParsePublicKeyRefusesAnMLDSAKey` (`internal/sshca`) and
`TestSealAnonymousBoxIsTheLibsodiumSealedBoxShape` (`internal/crypto`).

## Local qualification record

Go 1.27.3 darwin/arm64, local only, loopback sshd, no real provider write, no
host or trust change. The one real-provider contact is a credential-free SSH
handshake to github.com that offered no key (fact 5).

| Check | Result |
| --- | --- |
| `go test -p 2 -count=1 -v ./internal/sshca/` with OpenSSH 10.3p1 (LibreSSL 3.3.6) | PASS. Per CA algorithm: negotiated `kex=mlkem768x25519-sha256`, host key `ssh-ed25519`, user key `ED25519-CERT`, signing CA from `ssh-keygen -L`: `ED25519 using ssh-ed25519`, `ECDSA using ecdsa-sha2-nistp256`, `RSA using rsa-sha2-512`. Classical-only client (`curve25519-sha256`): default sshd accepts, hybrid-only sshd refuses with "no matching key exchange method" before authentication. `HIKYO_SSHE2E_REQUIRED=1` turns an old client into a failure. No workflow or script sets it, so no hosted lane is known to run these tests |
| `ssh -Q kex` / `ssh -Q key-cert` on 10.3p1 | KEX list includes `sntrup761x25519-sha512` and `mlkem768x25519-sha256`; no ML-DSA key or certificate type (predates 10.4) |
| libsodium 1.0.22 helper (built in a scratch directory, not committed), temporary Go test | Hikyo `SealAnonymousBox` output opened by `crypto_box_seal_open`; libsodium `crypto_box_seal` output (63 bytes) opened by `box.OpenAnonymous`. Both PASS. The helper and test were removed; the repo test pins the 48-byte overhead and freshness |
| `ssh -vv` to github.com without credentials | `mlkem768x25519-sha256`, host key matches the published Ed25519 fingerprint (fact 5) |
| Any real-provider API call, OpenSSH 10.6, PostgreSQL with a hybrid group, federation or mail peer | **NOTRUN**: no authorization or fixture |

## Exception terms (proposed, none effective)

DEC-10 makes an exception effective only with an exact peer and version, an
expiry and a recheck trigger. **None of these rows is effective**, and recording
this table does not make one effective: each stays ineffective until the owner
names its peer and version. A date of 180 days from 2026-11-03 would be
**2027-05-02**, at most two renewals; the clock starts when the peer is named.
Owner: Marc Went. Rows owned by other tickets (E-01, E-08, E-11, E-15, E-20) are
not repeated. These terms are pointed to from the
[migration register](migration.md#exception-terms-1134).

| ID | Routes (same as Table B) | Peer and version known | Why ineffective | Recheck trigger |
| --- | --- | --- | --- | --- |
| E-02 | X07, X08 | OpenSSH 10.3p1 local only; `x/crypto/ssh` v0.58.0; no deployed sshd named | Peer unnamed | `x/crypto` adds composite; draft becomes an RFC; DEC-02 or DEC-03 revisited |
| E-03 | X16 | None named | Peers unnamed, no versions | A service states PQ issuance |
| E-04 | X10, X11, X12 | Libraries: `go-oidc` v3.21.0, `go-jose` v4.1.6, `gosaml2` v0.12.0, `goxmldsig` v1.6.1; no IdP | IdP unnamed | `go-jose` ML-DSA; XML-DSig identifier; an IdP issues PQ |
| E-05 | X09 | `go-webauthn` v0.18.2; no authenticator | Authenticator unnamed | Primary authenticator statement; DEC-02 decision on parameters |
| E-06 | X01 | github.com REST; API version not read | Version not read | GitHub docs change algorithm |
| E-07 | X02 to X06, X17, X19 | Go 1.27.3 client; no peer | Peers unnamed | A peer is named and captured |
| E-13 | X14, X15 | `sops` v3.13.3, `client-go` v0.37.1, `openbao/api/v2` v2.7.1; no source | Sources unnamed | Source key policy named |

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
page and notes disagree), OpenSSH portable source at tag `V_10_6_P1`
(`ssh-mldsa-eddsa.c`, `myproposal.h`) and master (`sshkey.c`);
[draft-sfluhrer-ssh-mldsa-08](https://datatracker.ietf.org/doc/draft-sfluhrer-ssh-mldsa/)
(individual, not adopted);
[draft-ietf-sshm-composite-sigs-00](https://datatracker.ietf.org/doc/draft-ietf-sshm-composite-sigs/);
[RFC 10042](https://www.rfc-editor.org/rfc/rfc10042);
[RFC 9964](https://www.rfc-editor.org/rfc/rfc9964);
[IANA COSE registry](https://www.iana.org/assignments/cose/cose.xhtml);
[WebAuthn L3](https://www.w3.org/TR/webauthn-3/); GitHub
[concepts](https://docs.github.com/en/actions/concepts/security/secrets),
[secrets guide](https://docs.github.com/en/rest/guides/encrypting-secrets-for-the-rest-api),
[REST reference](https://docs.github.com/en/rest/actions/secrets),
[2025 SSH blog](https://github.blog/engineering/platform-security/post-quantum-security-for-ssh-access-on-github/),
[2026-09-22 changelog](https://github.blog/changelog/2026-09-22-security-improvements-for-ssh/);
[libsodium sealed boxes](https://libsodium.gitbook.io/doc/public-key_cryptography/sealed_boxes);
AWS [blog](https://aws.amazon.com/blogs/security/ml-kem-post-quantum-tls-now-supported-in-aws-kms-acm-and-secrets-manager/),
[Secrets Manager PQ TLS](https://docs.aws.amazon.com/secretsmanager/latest/userguide/pqtls.html),
[KMS key specs](https://docs.aws.amazon.com/kms/latest/developerguide/asymmetric-key-specs.html);
[Cloudflare PQ](https://developers.cloudflare.com/ssl/post-quantum-cryptography/);
[PostgreSQL 18](https://www.postgresql.org/docs/current/runtime-config-connection.html);
[Go `crypto/tls`](https://pkg.go.dev/crypto/tls);
[Sigstore](https://blog.sigstore.dev/post-quantum-2025/). FIDO vendor commentary
was seen only in search results and supports no row.
