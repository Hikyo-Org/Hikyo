# Cryptographic inventory, draft v0.4

Status and source pin: [contract README](README.md). Inventory IDs (I01 to I32)
are stable within this draft and are the keys used by [profiles](profiles.md),
[migration](migration.md) and [qualification](qualification.md). This records
source behavior at the pin, not deployed keys or negotiated connections.
Parameters below are current source constraints; none is a PQ profile.

## Census method and reconciliation

Read-only searches, not a dependency scan or runtime test. `rg` exit 1 means no
match; exit 2 is an incomplete search and must be corrected.

```sh
git rev-parse HEAD HEAD^{tree}
# 1. keyed primitives by import
rg -l '"(crypto(/[^"]*)?|golang.org/x/crypto/[^"]*|filippo.io/age[^"]*|github.com/(ProtonMail|sigstore|go-webauthn|coreos|getsops|pquerna|go-jose)/[^"]*)"' --glob '*.go' --glob '!*_test.go' .
# 2. TLS owned by an SDK or a default transport (no crypto import needed)
rg -n 'rest\.(Config|InClusterConfig|TransportFor)|clientcmd\.|kubernetes\.NewForConfig|baoapi\.|http\.DefaultTransport|TLSClientConfig|tls\.Config|InsecureSkipVerify|CurvePreferences' --glob '*.go' --glob '!*_test.go' internal cmd
# 3. deployment manifests, charts and installers
rg -n -i 'tls|ssl|cosign|kyverno|verifyImages|trust|pubkey|\.pub|ca\.crt|authority' chart deploy install
# 4. formats and constraints; 5. scripts and release tooling
rg -n -i 'x25519|ed25519|ml.kem|ml.dsa|rsa|ecdsa|argon2|scrypt|hmac|signature|fingerprint' api internal/store/migrations clients web/src
rg -n -i 'cosign|sigstore|codesign|notarytool|gpg|checksum|sha256' scripts install chart release .github go.mod
# 6. source supply chain
rg -n -i 'required_signatures|gpgsign|sumdb|GONOSUMDB|GOFLAGS' .github AGENTS.md CONTRIBUTING.md go.mod Makefile
```

Result on 2026-11-02: search 1 finds 179 non-test Go files in 71 directories.
**Search 1 alone missed a boundary**: `internal/storagehealth/kubernetes.go`
(kubelet TLS with a bearer token through client-go) imports no crypto package;
search 2 finds it, the in-cluster and kubeconfig Kubernetes clients, and the
default-transport users (now I30). The first draft also looked for `charts`
instead of the real `chart/hikyo`; that path is now searched (search 3) and is
I31 (open PR #1113 is a separate Helm distribution change). The toolchain is `go 1.27.3`; `go.mod` also pins `gosaml2`,
`goxmldsig`, `go-mail`, `go-smtp`, `pgx` and the AWS SDK, reached indirectly.

Matches from the Go-source searches were assigned to an ID or a class below;
shell, YAML and installer files (`install/install.sh.in`,
`install/cloud/compose`, `install/updater`) are covered by I26, I27 and I31 where
they verify or fetch trust material, and were otherwise not classified.
Completeness beyond these searches is not claimed (other SDKs reading ambient
TLS settings, code reached through reflection):

| Class | Matches and reason |
| --- | --- |
| Digest only, no key | `crypto/sha256` in `internal/{admission,definitions,importer,lint,repscan,releaseidentity,selfupdate,hostupgrade}`, `internal/scanning/gen`, `internal/operator/binding.go`, `internal/server/saml.go`, `internal/store/store.go`, `scripts/**` except the release verifiers owned by I26 (commitments, manifests, inventories; I03, I29) |
| Fixture or test tooling | `internal/{tlstest,mailtest,mailtest/cmd,oidctest,samltest,devupgrade,releasetrust/testfixture}`, `scripts/ci/**`, `scripts/acceptance/**`, `scripts/security/**`, `scripts/mcp-public-smoke`; `internal/crypto/registry_password.go` (bcrypt, disposable registry fixture, not account passwords) |
| Format metadata | `crypto/sha1` in `scripts/release/verify-native-packages.go` (APK-TOOLS checksum); TOTP SHA-1 is I05 |
| Randomness or comparison only | `crypto/rand` and `crypto/subtle` sites (`internal/operator/secret.go`, `internal/server/browser.go`, `internal/compose`, `internal/filesync`, `internal/dynamic`) belong to the surrounding ID |
| Configuration only | `chart/hikyo/crds`, `values.schema.json`, RBAC and network-policy templates (no key or trust root) |
| Local only, no TLS | `internal/updater` (unix socket), `internal/hostupgrade` and `internal/app/unattended_process.go` HTTP probes |
| Keyed primitives and TLS | I01 to I32 below |

New matches in a later census must join a row or this table. A deployed
inventory cannot be called exhaustive without the operator's topology, custody
and retention inputs.

Provenance codes: **F** first-party code, **L:name** maintained library, **X**
external provider or ecosystem contract Hikyo cannot change; an X row names its
external owner in the owner cell. Unless a row says pin, TLS server certificates
are X.509 chains verified against system or configured roots. Library capability
is not product readiness.

## Boundaries

`C` is confidentiality, `A` authenticity or authority. `L` key lifetime, `H`
confidentiality or verification horizon. Unknown deployment values are operator
inputs, not zero. Generated entropy uses `crypto/rand`; imported material's
entropy is not proven by a parser. Go memory zeroization is not asserted.

| ID, boundary and purpose | Algorithms, format, entropy | Owner, custody, trust anchor, provenance, external owner | L; H | Recovery path |
| --- | --- | --- | --- | --- |
| I01 Storage root and master: encrypt all stored fields | 32-byte random keys; format 1, algorithm 1 XChaCha20-Poly1305; 24-byte random nonce, 16-byte tag; authenticated header and AAD | Operator-held root outside datastore; master wrapped by root. F + L:x/crypto | L: explicit rotation; H: all retained wrapped data | Root from separate custody plus archive; dual-wrap root rotation |
| I02 Project and instance DEKs: field encryption | 32-byte DEKs; same envelope, row, kind, key, version, tenant binding | Keyring in datastore wrapped by master. F | L: versioned rotation; H: live and retained fields | Rewrap from master; `reencrypt`; restore |
| I03 Derived integrity keys: tokens, scanning, commitments | HKDF/HMAC-SHA256, context-separated labels, existing truncation rules | Root, token or owning key; no public authority. F | L: owning key epoch; H: verifier or binding lifetime | Re-derive from root; rotate owning key |
| I04 Bearers and browser/CLI auth: authenticate callers | `hik_1_...` 32-byte random body, SHA-256 verifier; PKCE SHA-256 | Client-held bearer, server verifier rows. F | L: type-specific; H: validity plus retained verifier copies | Reissue; recovery codes; restore invalidates pre-restore artifacts |
| I05 Passwords and second factor: user authentication | Argon2id 16-byte salt, 32-byte output, floor 64 MiB / 3 / 2; TOTP 20-byte seed, SHA-1, 6 digits, 30 s | Instance-sealed verifier and seed. F + L:x/crypto, L:otp | L: rotation; H: verifier secrecy | Reset flow; recovery codes; re-enroll |
| I06 Offline client data and MCP cursor: local cache integrity and confidentiality | XChaCha20-Poly1305; HKDF/HMAC stamp; credential-derived context | Client secure files; server instance key. F | L: credential or cache policy; H: retained snapshots | Delete cache and resync; invalidate cursor |
| I07 Backup container: protect exports | age X25519 recipients/identities (`age1...`) or exclusive scrypt passphrase (work factor 18); parsers concrete to X25519 (`internal/crypto/backup`, `internal/runtimeconfig/config.go:64`) | Backup identity held apart from root and datastore. F + L:age v1.3.2 | L/H: retained archive lifetime, possibly indefinite | Archive + backup identity + root key |
| I08 Upgrade receipt and operator custody: prove a restorable backup before upgrade | `age-x25519-sha256:` fingerprint = SHA-256 of canonical recipient string; receipt and attestation; custody record `operator.age` (`hikyo.operator-custody/v1`) holds backup identity, P-256 attestation key and root escrow, scrypt-sealed (`internal/upgradecustody/vault.go`); in automatic mode the sealing secret is derived from the root key, in legacy vaults it is a passphrase | Operator, backup custodian, release roots. F + L:age, L:sigstore | L: attestation at most 24 h; H: archive copies | Rebuild custody from held identity and root; legacy passphrase vaults are rewrapped once |
| I09 Native ingress: serve API and UI over TLS | `tls.Config` `MinVersion` TLS 1.2, no `CurvePreferences` (`internal/app/tls.go`); X.509 PEM. Go 1.27.3 defaults offer `X25519MLKEM768`, `SecP256r1MLKEM768`, `SecP384r1MLKEM1024` and advertise `MLDSA44/65/87`; library default, not Hikyo policy; `GODEBUG=tlsmlkem=0` removes it unless `CurvePreferences` is explicit | Admin stages certificate; managed generation owns it. F + L:crypto/tls | L: leaf validity; H: delivered credentials | Re-stage certificate; generation rollback |
| I10 Proxy ingress/backend: external TLS termination | Example `deploy/mcp/nginx.conf`: `ssl_certificate`, no `ssl_protocols`, proxies to plain `http://hikyo_backend` | Deployment operator. X | L/H: HOLD until topology supplied | Operator reconfigures |
| I11 CLI, operator, API, MCP clients: authenticated calls to Hikyo and updates | `MinVersion` TLS 1.2 (`internal/cli/{client,trust}.go`, `internal/operator/client`, `internal/remotefetch`, `internal/updatecheck`); system or custom roots, or a leaf SPKI pin with `InsecureSkipVerify` (`internal/cli/client.go:188`, `internal/remotefetch/remotefetch.go:248`), where `VerifiedChains` is empty | Operator trust files, OS CA store. F | L: certificate and token; H: disclosed payloads | Re-trust CA or pin; re-login |
| I12 Primary database: store state | SQLite local; remote PostgreSQL requires `sslmode=verify-full` (`internal/config/config.go`); pgx | Datastore operator and CA. F + L:pgx | L: credential; H: database copies | Database restore + root hierarchy |
| I13 Dynamic PostgreSQL credentials: issue short-lived DB users | pgx `verify-full`, `MinVersion` TLS 1.2, issued password (`internal/dynamic/postgres`) | Target DB admin and CA. F + L:pgx + X | L: lease TTL; H: target secrets | Revoke and reissue lease |
| I14 Adapter transport: push to providers | HTTPS `MinVersion` TLS 1.2: GitHub Actions, GitLab, Forgejo, Vault KV, AWS Secrets Manager, Cloudflare, sealed webhook | Provider CA and API. F + L:AWS SDK + X | L: provider credential; H: plaintext retained by provider | Credential refresh; module revalidation |
| I15 Provider payload encryption: GitHub secret upload | GitHub requires libsodium encryption (docs fetched 2026-11-02); source uses NaCl anonymous sealed box (`golang.org/x/crypto/nacl/box`) | Provider public key and contract. F + L:x/crypto + X | L: provider key version; H: retained payload | Re-fetch provider key |
| I16 Mail: send tokens and notices | SMTP over TLS or STARTTLS, `MinVersion` TLS 1.2 (`internal/mail`) | Mail operator and relay. F + L:go-mail + X | L: SMTP credentials; H: retained mail | Update credentials |
| I17 Federation HTTP and JWKS: fetch IdP metadata and keys | OIDC discovery, JWKS (1 MiB cap, `internal/jwkssource`), token, userinfo, OAuth over `internal/federationhttp` (`MinVersion` TLS 1.2); SAML metadata fetched by its own transport (`internal/service/saml_providers.go:977`, TLS 1.2); OAuth2 code exchange through the default transport (`internal/oauth2rp`) | Federation admin and IdP. F + L:go-oidc, L:go-jose + X | L: issuer keys; H: tokens | Re-fetch; admin reconfigures |
| I18 OIDC and SAML authenticity: verify assertions and tokens | OIDC accepts RS256/384/512 and ES256/384/512, no EdDSA; SAML accepts RSA-SHA256/384/512 and ECDSA-SHA256/384/512 (`internal/samlsp/response.go`); SP key RSA-3072, certificate 10-year validity | IdP signing roots, confirmed metadata; SP key instance-sealed. F + L:gosaml2, L:goxmldsig + X | L: issuer key validity; H: replay window and records | Metadata re-confirmation; SP rollover |
| I19 WebAuthn: second factor and passkeys | `go-webauthn` v0.18.2; on go1.27 it verifies ML-DSA (AKP) keys, library capability only | User authenticator vendors, RP origin. F + L:go-webauthn + X | L: until removal; H: authority | Other credentials; recovery codes |
| I20 Remote pairing and proofs: connect instances | `go-srp` v0.0.7 2048-bit SRP group, signed modulus; SHA-256 context salt; independent Ed25519 key; EdDSA DPoP via `go-jose` with the public JWK in the header, max age 1 min, skew 5 s, 8192 bytes | Instance pin, confirmed invitation. F + L:go-srp, L:go-jose | L: invitation and connection; H: paired authority | Revoke connection; re-pair. Pinned `go-jose` v4.1.6 has no ML-DSA |
| I21 Sealed webhook: deliver encrypted payloads to receivers | Protocol 1: age X25519, PKCS#8 Ed25519 envelope and ack keys, domain-separated fingerprints; file-configured targets (`internal/config/sealed_webhook.go`); `docs/spec/sealed-webhook.md` | Sender credentials; receiver identity and ack key external. F + L:age | L: envelope at most 5 min; H: retained ciphertext | New generation moves the destination id; targets fail closed |
| I22 Transit: sign, MAC, encrypt as a service | Closed `xchacha20-poly1305`, `ed25519`, `hmac-sha256`; 32-byte version material sealed under the project DEK, HKDF per operation; `hikyo:v` wire | Software material; external custody seam, mock only. F | L: version and floor; H: ciphertext and signature policy | Key rotation; restore holds |
| I23 Configuration rollout: authorize node changes | Ed25519 PKCS#8, controller enrollment, domain `hikyo.configuration-rollout.command.v1`, record cap 512 KiB (`internal/configrollout`) | Operator or controller signer, node enrollment. F | L: command deadline; H: executable authority | Re-enroll; takeover; journal |
| I24 Private PKI: issue certificates | Leaves ECDSA P-256/384, Ed25519, RSA-2048/3072/4096; CAs ECDSA P-256/384, RSA-3072/4096; X.509, PKCS#8 (`internal/pki/keys.go`) | Instance-sealed issuer, imported or offline root. F + L:crypto/x509 | L: TTLs, CRL; H: chain history | Issuer rotation; CRL; restore hold |
| I25 SSH CA: issue SSH user certificates | Ed25519, ECDSA P-256, RSA at least 3072; OpenSSH certificates, KRL (`internal/sshca`). `x/crypto/ssh` v0.58.0 implements ML-DSA key algorithms after draft-sfluhrer-ssh-mldsa-08 but its certificate algorithms use `@golang.org` vendor names no specification defines; OpenSSH has no PQ signatures (page fetched 2026-11-02) | Instance-sealed CA; sshd operator (OpenSSH) trust. F + L:x/crypto/ssh + X | L: certificate TTL; H: login authority | CA rollover; KRL |
| I26 Release, install, update: authenticate software | ECDSA P-256 primary and recovery keys (`release/trust/*.pub`), signed metadata, catalog, roots, bridges; stable releases through a Sigstore keyless workflow identity under a recovery-signed policy; the pinned-key parser accepts ECDSA P-256/384/521, RSA at least 2048, Ed25519; recovery key alone signs metadata and catalog (`internal/releasetrust/snapshot.go:146,183`) | Release and recovery custodians (keys generated online under a recorded exception), workflow delegation, Sigstore roots. F + L:sigstore-go + X | L: sequence floor (no clock-based expiry found in `internal/releasetrust`); H: installs and rollback | Recovery-signed bridge; reinstall (`release/trust/BOOTSTRAP.md`) |
| I27 Platform and package trust: OS and registry verification | cosign v3.1.3 blob and OCI signatures with Fulcio-issued certificates and Rekor (`.github/workflows/nightly.yml`, `release.yml`, `install/upgrade-nightly.sh`); Apple codesign and notarization, OS package signatures and APK-TOOLS SHA-1 checksums (parameters not inspected); provenance attestations | Apple, OS vendors, registries, Sigstore operators. X + F scripts | L/H: platform policy | Vendor process |
| I28 Import and external ciphertext: bring secrets in | SOPS decryption and key sources (age, PGP, KMS), Kubernetes and local files | Source key owners, KMS. L:sops + X | L: source policy; H: retained source ciphertext | Re-import from source |
| I29 Digests and audit evidence: commitments | SHA-256 object, manifest, receipt, backup, file-sync commitments; audit export integrity separately scoped | Producing subsystem. F | L/H: evidence retention | Recompute; independent evidence custodian |
| I30 SDK-owned and default-transport TLS: reach Kubernetes and import sources | `internal/storagehealth/kubernetes.go` (kubelet, cluster CA, rotating bearer token), in-cluster Kubernetes clients (`cmd/hikyo/config_rollout.go`, `internal/app/self_config_deployment.go`, `internal/operator/{manager,credential}.go` with short-lived TokenRequest tokens), kubeconfig import (`internal/importer/k8s.go`), OpenBao/Vault import through the SDK's ambient `VAULT_*` settings (`internal/importer/vault.go`), default-transport callers (`internal/diagnostics`); Go defaults for groups, no Hikyo `CurvePreferences` | Cluster operator, source operators, service-account token. F + L:client-go, L:OpenBao SDK + X | L: token rotation; H: delivered tokens and data | Token refresh; kubeconfig reissue |
| I31 Helm chart `chart/hikyo`: deploy and verify Hikyo on Kubernetes | Trust ConfigMap (`files/release/trust/{root.json,primary.pub}`; the in-tree `primary.pub` is a placeholder the release packaging replaces); Kyverno `verifyImages` policy, keyed with `primary.pub` and `ignoreTlog: true`, or keyless GitHub Actions identity with Rekor; listener TLS Secret (`tls.crt`, `tls.key`), database CA mount (`sslrootcert`), kubelet CA, rollout enrollment with an operator-installed Ed25519 `authority.pub` and an authority key Secret | Cluster operator. F + L:Kyverno, cosign + X | L: image signature and certificate validity; H: deployed image authenticity | Re-render chart; reinstall Secrets |
| I32 Source supply chain: authenticate commits and modules | GitHub ruleset `required_signatures` on `main` (`release/repository/main-ci-gate.json:34`): signature key types are chosen by contributors (`CONTRIBUTING.md`, not inspected); Go module checksums (`go.sum`, `GOFLAGS=-mod=readonly`) are checked by the `go` tool against the Go checksum database, whose algorithm is the Go project's and was not inspected | GitHub, contributors, Go project. X | L/H: repository history, module versions | Re-sign or re-verify; vendor process |

The same field encryption may cover several rows, but the external decryption
or authentication doors remain distinct. Crypto-shredding must account for all
recipients, exports, old keys and offline copies. Retention's last-successful
and uncertain-custody exceptions are not a hard 180-day erasure guarantee.

## Source and consumer trace

Repo-relative paths at the pin. Primitive and constraining handlers move
together.

| IDs | Primitive or format owner | Writers | Readers and verifiers |
| --- | --- | --- | --- |
| I01-I02 | `internal/crypto/{crypto,envelope,aad,keyring,rootkey}.go` | `internal/service`, `internal/store/keyring`, rotation and re-encryption | Same packages; `internal/store/upgrade`; both engines |
| I03 | `internal/crypto/{token,stamp,scanning,delivery,scim_subject,import_commitment,custody,snapshot_binding}.go` | Publishing, scanner, SCIM, import, delivery services | Same services; cache and restore readers |
| I04-I05 | `internal/crypto/{bearer,password,totp,recovery}.go` | `internal/service/{auth,cli_login}.go`, `internal/cli` | `internal/store/authn`, `internal/server/browser.go`, OIDC/OAuth flows |
| I06 | `internal/crypto/{client,client_secure_unix,mcp_cursor,offline_receipt}.go` | `internal/compose`, `internal/cli/{compose,file_sync}.go`, `internal/filesync` | Same plus MCP pagination |
| I07 | `internal/crypto/backup/{backup,recipients}.go` | `internal/{app,service}/backup*.go`, scheduled export, `internal/runtimeconfig` (recipient validation) | CLI restore, preflight, drill, offline tools, `internal/store/upgrade` |
| I08 | `internal/backupreceipt`, `internal/upgradecustody` | `internal/app/{backup_upgrade,automatic_upgrade}.go`, `internal/upgradegate`, config rollout upgrade | Operator verifier, installer pin, `internal/devupgrade` (fixture) |
| I09-I10 | `internal/app/{tls,runtime_listener,upgrade_window}.go`, `internal/tlspolicy`, `internal/config/managed_node.go` | Managed certificate staging, `install/compose` | Clients and proxies; `deploy/mcp/nginx.conf` |
| I11 | `internal/cli/{client,trust}.go`, `internal/operator/client`, `internal/remotefetch`, `internal/updatecheck` | CLI and operator callers | Hikyo server; `internal/isolation/cli_tls_consumer_test.go` |
| I12-I13 | `internal/config/config.go`, `internal/store`, `internal/dynamic/postgres` | Store, dynamic service | PostgreSQL servers |
| I14-I15 | `internal/adapter/*`, `internal/crypto/sealed_box.go` | Adapter modules | Providers |
| I16-I19 | `internal/mail`, `internal/federationhttp`, `internal/jwkssource`, `internal/oidc{rp,fed}`, `internal/oauth2rp`, `internal/samlsp`, `internal/webauthnrp` | `internal/service/{saml_providers,federation}.go` (configuration) | `internal/service/{auth,saml,oidc,oidc_flow,oauth2_flow}.go`, `internal/server/saml.go` |
| I20 | `internal/pairing/{srp,phrase,dpop}.go` | `internal/service/remote_pairing.go`, `internal/remotefetch` | `internal/store/authn/remote_pairing.go`, `internal/app/pairing_identity.go`, migration 00079 |
| I21 | `internal/crypto/sealedhook/sealedhook.go` | `internal/adapter/sealedwebhook`, `internal/config/sealed_webhook.go`, `internal/app/sealed_webhook.go` | `internal/sealedreceiver`, `cmd/hikyo-receiver-example`, ack verifiers |
| I22 | `internal/crypto/transit.go`, `internal/transit/{transit,software}.go` | `internal/service/transit*.go`, API, CLI, UI | Same, generated clients, both 00065 migrations |
| I23 | `internal/configrollout/{transport,controller,custody,bootstrap,upgrade,kubernetes}.go` | `cmd/hikyo/config_rollout.go`, self-config service | Controller, `internal/app/self_config_deployment.go`, journals |
| I24 | `internal/pki/{keys,sign,policy,pem}.go` | `internal/service/pki*.go`, API, CLI, UI | Relying parties, CRL readers, both 00064 migrations |
| I25 | `internal/sshca/{keys,cert,krl}.go` | SSH CA service, API, CLI, UI | sshd, KRL |
| I26 | `internal/releasetrust`, `internal/releaseidentity`, `release/trust` | `scripts/release`, release workflows | `internal/{selfupdate,hostupgrade,updatecheck,backupreceipt,upgradecustody}`, `install/*.sh`, `internal/upgradeassembly`, `internal/upgradebundle`, `internal/upgradecompat`, `internal/buildcompat` (they carry and bind authenticated release evidence; no keyed primitive of their own) |
| I27 | `scripts/release`, `install/upgrade-nightly.sh` | Release workflows | OS, registries, installers |
| I28 | `internal/importer/{sops,artifacts,k8s}.go` | Import service | Import apply |
| I29 | `internal/definitions`, `internal/backupreceipt`, release and CI tooling | Producing subsystems | Verifiers, audit collectors |
| I30 | `internal/storagehealth/kubernetes.go`, `internal/importer/{k8s,vault}.go`, `internal/operator/{manager,credential}.go`, `internal/diagnostics` | Same | Cluster API, kubelet, OpenBao/Vault |
| I31 | `chart/hikyo/templates/{kyverno-verify-images,release-trust-configmap,rollout-custody,rollout-admission,deployment,operator-deployment,storage-monitoring-rbac}.yaml`, `files/release/trust` | Release packaging, chart renderer | Kyverno, cosign, pods, controller |
| I32 | `release/repository/main-ci-gate.json`, `go.mod`, `go.sum` | Contributors, CI | GitHub, `go` tool |

## Current format and bound constraints

The home for current source bounds; required PQ sizes and proposed maxima are in
[profiles](profiles.md#size-and-work-bounds-per-consumer).

| Consumer | Bound (source) |
| --- | --- |
| Backup header and recipients | 256 KiB header, 256 recipients (`backup.go`); upgrade custody 64 recipients (`recipients.go`); receipt artifact 64 KiB, signature 1 MiB, ciphertext 1 TiB, 32 bridges (`backupreceipt/artifacts.go`) |
| Managed values | 65536 bytes per value (`schema.MaxValueBytes`, enforced in `internal/runtimeconfig/config.go:44`); `HIKYO_BACKUP_RECIPIENTS` is a managed owner value (`internal/config/variables.go`) |
| Operator custody record | ciphertext 16 KiB, plaintext 8 KiB, identity 256 bytes, signing key 1024 bytes, secret 1024 bytes (`upgradecustody/vault.go`) |
| Sealed webhook v1 | ciphertext 1 MiB, envelope about 1.33 MiB + 64 KiB, ack 64 KiB, 256 names, lifetime 5 min, signature 64 bytes |
| DPoP | 8192 bytes, 1 min age, EdDSA only |
| Transit | message 64 KiB, context 1 KiB, wire 128 KiB; closed CHECK constraints and API enums |
| Rollout | record 512 KiB; Ed25519 shapes |
| PKI | issuer key PEM 16384 chars, certificate or CSR PEM 65536, chain 262144, request 2 MiB; `key_algorithm` is `TEXT` without CHECK in both engines |
| Release trust | document 4 MiB; parsers accept classical key types only (ECDSA, RSA, Ed25519) |
| JWKS | 1 MiB |

Do not raise a global limit or grow a frozen closed enum incidentally. Compare
any new encoding's total wire size (base64, JSON, PEM) with these consumers,
database constraints, API freeze, admission and audit redaction.
