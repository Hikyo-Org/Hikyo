# Private PKI: policy-bound X.509 certificate lifecycle (ADR)

Status: **accepted 2026-09-26** ([#154](https://github.com/Hikyo-Org/Hikyo/issues/154)).
Operative with the implementing PR. Owner decision recorded in
[mvp-boundary.md](./mvp-boundary.md) §4.1 (PKI / CA row redrawn: In).

Handoff and progress: [docs/handoff/154-pki.md](../handoff/154-pki.md).

## Context

The MVP boundary listed "PKI / CA" as Out, "different product, no trigger, new
map only". The owner redrew that row for #154: Hikyo gains a self-hostable,
narrow private PKI for **short-lived service identity certificates** (mTLS
between workloads, internal TLS endpoints). It is not a general-purpose CA
product: no ACME server, no OCSP responder, no SSH certificates, no public
trust, no HSM/KMIP (the HSM row stays Out), no encryption-as-a-service.

Everything below reuses existing machinery: SAML SP key custody (sealed
PKCS#8 under the instance tier-3 key, fingerprint-only views, CAS rotation),
the dynamic-secrets lifecycle (display-once disclosure, INTENT/OUTCOME audit,
row-fenced worker that is safe on every #146 node), the closed capability
table, and the restore reconciliation boundary.

## Decisions

### D1. Three objects, two scopes

| Object | Table | Scope | Governs |
|---|---|---|---|
| Issuer (one CA key version) | `pki_issuers` | instance | `instance-config` |
| Profile (issuance policy) + bindings | `pki_profiles`, `pki_profile_bindings` | instance | `instance-config` |
| Certificate (issuance record) | `pki_certificates` | environment | `issue-certificate@environment` |

Issuers and profiles are instance policy: a project administrator can never
widen what a CA will sign. A profile reaches a tenant only through an explicit
**binding** to a project (optionally one environment of it). Issuance is an
environment-scoped act by a principal holding the new atom (D4) on that
environment, through a profile bound there.

### D2. Issuer custody: sealed, non-exportable, versioned

- An issuer row is **one key version** of a named CA (`name`, `version`).
  Profiles reference issuer *names*, so rotation never edits a profile.
- The private key is PKCS#8, sealed with
  `InstanceFieldAAD{OwnerTable: "pki_issuers", OwnerRowID: <id>, FieldTag: "private_key"}`
  under `PurposeInstance`. No new AAD kind, so no encryption-model amendment.
  The column is registered for `rotate-dek --instance` re-encryption and every
  sealing write fences on the instance DEK version.
- **Non-exportable** is defined here, since encryption-model.md disclaims memory
  secrecy: *software custody*. The private key never crosses the service
  boundary. No API, CLI, audit payload, log line, metric, diagnostic or
  backup diagnostic carries it. There is **no export operation**. Backups carry
  only the ciphertext, exactly like every other sealed column. Hardware custody
  (HSM, KMIP, PKCS#11) is out; the signer is a Go `crypto.Signer` opened only
  inside the service for the duration of one signature and zeroed after.
- **Separately authorized:** issuer management (`pki-issuer.*`) is
  `instance-config`; issuance is `issue-certificate@environment`. Holding one
  never implies the other.
- Private material enters only as **protected input** (request body, CLI
  `--key-file`/`--stdin`, never argv) and only through `pki-issuer.import`.

Issuer states (closed):

| State | Mints leaves | Signs CRLs | Key held |
|---|---|---|---|
| `pending` | no | no | yes (awaiting an externally signed certificate) |
| `active` | yes | yes | yes |
| `retiring` | no | yes | yes |
| `retired` | no | no | **destroyed** (ciphertext set to NULL) |
| `revoked` | no | no | **destroyed** |

At most one `active` version per name (partial unique index). A `restore_hold`
flag (D8) suspends minting on any state.

### D3. Root, intermediate, offline root

- **Evaluation root:** `pki issuer create-root` generates a self-signed CA in
  Hikyo. Documented as evaluation-grade: a root whose key lives on an online
  server is a single point of compromise.
- **Hikyo-signed intermediate:** `pki issuer create-intermediate --parent <name>`
  generates the key and signs it with the parent's active version
  (`MaxPathLen=0`, so it can sign only leaves).
- **Offline root (production):** `pki issuer create-intermediate` without
  `--parent` generates the key in Hikyo and returns a CSR; the version is
  `pending`. The operator signs the CSR with an offline root (runbook in the
  docs page) and runs `pki issuer install-cert`, which verifies that the
  certificate's public key matches the sealed key, that it is a CA with
  `KeyUsageCertSign`, and that the supplied chain verifies it, then activates it.
- **Import:** `pki issuer import` accepts an existing CA key and certificate as
  protected input for operators migrating an intermediate.
- **Rotation (overlap):** `pki issuer rotate <name>` creates version N+1
  (generated root, Hikyo-signed intermediate, or `pending` with a CSR). When
  N+1 becomes `active`, version N moves to `retiring` in the same transaction.
  Leaves of N stay valid and N keeps signing its CRL; renewals move to N+1.
  `pki issuer retire` finishes the overlap: it refuses while N still has live
  (unexpired, unrevoked) leaves, then destroys the key.
- **Compromise:** `pki issuer revoke` is terminal: key destroyed, every live
  leaf of that version marked `revoked` (reason `cACompromise`), no further
  CRLs.

### D4. New capability atom `issue-certificate`

Closed atom table gains `issue-certificate`, deepest level **environment**.
permission-model.md amendment (banner in this PR): workload and automation
allowlists admit it. It grants no read of any value, definition or grant. A
machine additionally needs the profile's `machine_issuance` opt-in (D5); a
withdrawn opt-in answers the uniform not-found response, the
`machine_reveal` precedent.

### D5. Profiles are closed, decidable policy

A profile is a closed record (unknown fields refused):

- `allowed_issuers`: issuer names; the leaf is signed by the named issuer's
  `active` version.
- `dns_patterns`: exact names or `*.suffix` (exactly one leftmost label).
  Wildcard *certificates* (a requested SAN starting with `*.`) are refused
  unless `allow_wildcard_names`. The common name, when present, must equal one
  of the requested DNS SANs.
- `ip_ranges`: CIDRs; each IP SAN must fall inside one.
- `uri_patterns`: exact URIs or `prefix/*` (for SPIFFE IDs).
- `key_algorithms` (closed): `ecdsa-p256`, `ecdsa-p384`, `ed25519`,
  `rsa-2048`, `rsa-3072`, `rsa-4096`.
- `key_usages` (closed): `digital-signature`, `key-encipherment`,
  `key-agreement`; `ext_key_usages` (closed): `server-auth`, `client-auth`.
  Leaves are never CAs.
- `max_ttl` (hard ceiling 90 days), `default_ttl`, `renew_window`
  (strictly less than `max_ttl`).
- `allow_csr`, `allow_generated_key` (at least one), `machine_issuance`.
- `organization`: an optional fixed subject O, set by the profile and never
  by the requester.

Patterns are deliberately **not regular expressions**, so containment is
decidable. **Policy updates may only narrow.** The server computes whether the
new profile is a subset of the old one (every new pattern covered by an old
one, no new algorithm/usage/issuer, no longer TTL or renewal window, no newly
enabled flag). Anything else, including anything it cannot decide, is refused
as `pki_profile_widening` (409). Widening is an explicit new profile. Binding a
profile to a scope is its own audited operation.

### D6. Issuance, renewal, revocation, expiry

- **Issue** (`certificate.issue`, `issue-certificate@environment`): the
  request names a bound profile and either a PEM CSR (proof of possession is
  checked; only the public key is used, and names come from the explicit
  request fields) or `generate_key` with an allowed algorithm. The server
  generates keys before any transaction, after a cheap authorization pass (the
  SAML SP rotate precedent: not a CPU oracle).
  - Tx1 re-authorizes, applies the caller-class gate (machine: profile opt-in;
    human with `generate_key`: the env-bound mint reauthentication ceremony,
    `NewMintReauthIntent(env, nil)`, the dynamic-lease precedent), validates
    against the profile, reserves a 128-bit random serial in an `issuing` row,
    and writes the `pki.certificate_transition_intent` event.
  - The leaf is signed outside the transaction.
  - Tx2 re-authorizes, then **fences on the issuer**: a conditional
    `UPDATE pki_issuers SET issued_count = issued_count + 1 WHERE id = ? AND
    state = 'active' AND NOT restore_hold`. A concurrent retire, revoke or
    rotation on any node makes it touch zero rows (SERIALIZABLE on
    PostgreSQL, single writer on SQLite). The row flips `issuing -> issued`
    with the DER, and the outcome event is written. Only then does the
    response carry the certificate, and the generated private key exactly
    once.
  - If Tx2 fails, or the process dies between Tx1 and Tx2, the row stays
    `issuing`. The worker flips it to **`unknown`** after the issuance deadline.
    `unknown` is listed, counted in metrics and doctor, and **published on the
    CRL as revoked** (fail closed: Hikyo cannot prove the leaf never escaped).
    It is never reported as success.
- **No private key is ever stored.** The CSR path never sees one; the
  generated path returns PKCS#8 PEM once in the response and zeroes it.
- **Renew** (`certificate.renew`): allowed only inside the profile's
  `renew_window` before `not_after`. It reuses the certificate's public key,
  re-validates names against the **current** profile (a narrowed profile
  refuses a renewal it no longer allows) and signs with the **current active**
  issuer version (overlap rotation). A claim column (`renewed_by`) set by CAS
  makes renewal idempotent and single-winner across nodes: a second renew
  returns the successor.
- **Revoke** (`certificate.revoke`): CAS from `issued`, `renewed` or `unknown`
  to `revoked` with a closed RFC 5280 reason. Revoking a revoked certificate
  is a success that returns the existing record. Revocation never re-checks
  that the profile still permits the names (fail-safe direction).
- **Expiry:** the worker CASes `issued`/`renewed` past `not_after` to
  `expired`.

### D7. CRL publication and status

- Each `active` or `retiring` issuer version publishes a CRL
  (`x509.CreateRevocationList`) listing `revoked` and `unknown` leaves whose
  `not_after` has not passed. The CRL number is `max(previous + 1, now in
  seconds)`, so it stays monotonic across a restore from an older backup.
- The worker regenerates a CRL when a revocation newer than the published one
  exists, or when half of its validity (24 hours) has elapsed, and stores the
  DER on the issuer row under a CAS on the prior CRL number. Serving a CRL is
  a read of that stored DER and never touches key material.
- The CRL is served **authenticated**: to operators (`pki-issuer.inspect`,
  `instance-config`) and, per certificate, to any principal holding `read` on
  the certificate's environment (`certificate.inspect`, the audit-free
  metadata read that also lists and shows certificates), because the set of
  unauthenticated routes is closed.
  Operators copy it to a public distribution point; an issuer's optional
  `crl_distribution_url` is embedded into its leaves.
- Status inspection: `pki issuer show` and `pki cert show` return public
  metadata and certificates only.

### D8. Backup, restore and multi-node

- Backup exports the tables as stored: public inventory and sealed CA keys.
- `CompleteRestore` sets `restore_hold` on every issuer, which suspends minting,
  because a restore can resurrect certificates that were revoked after the
  backup was taken. The hold clears only through `pki issuer release-hold <name>`
  (`instance-config`, audited), after the operator has re-applied known
  revocations. This is the credential reconciliation boundary applied to CAs.
  Restored principals stay inert until reconciled, as they already are.
  Releasing the hold is a network operation, unlike `restore reconcile`, which
  stays local-host authority: only an operator whose own principal was already
  reconciled (the local-host act) can call it, so the boundary holds
  transitively. It is deliberately not named "reconcile", a word reserved for
  that local-host restore surface.
- Every lifecycle transition is a CAS on the row's state. The worker
  (`app.pkiWorker`, every node) uses only row-level CAS writes, so it needs no
  singleton scheduler lease: the dynamic-worker argument, verbatim.

### D9. Surfaces

- API: `/api/v1/instance/pki/issuers...`, `/api/v1/instance/pki/profiles...`,
  `/api/v1/orgs/{org}/projects/{project}/environments/{environment}/certificates...`.
- CLI: `hikyo pki issuer ...`, `hikyo pki profile ...`, `hikyo cert ...`. Private
  keys use the display-once print triad (`internal/disclose`).
- Web UI: instance administration gains a PKI section (issuers, profiles,
  bindings); the environment gains a Certificates view (issue by CSR or
  generated key with the display-once dialog, renew, revoke).
- Metrics (label-free): `hikyo_pki_certificates_live`,
  `hikyo_pki_certificates_unknown`, `hikyo_pki_issuers_on_hold`, guarded by
  `hikyo_pki_gauges_known`. Doctor warns on unknown certificates and held
  issuers.
- Audit: `pki.issuer`, `pki.profile`, `pki.certificate_transition_intent`,
  `pki.certificate_transition_outcome`, `pki.certificate_key_disclosed`,
  `pki.crl_published`. Payloads carry fingerprints, serials, names and states,
  never key material.

## Consequences

- Private PKI becomes an operated surface: CRL distribution and the offline
  root ceremony are operator responsibilities, both documented.
- The in-process signer means an attacker with code execution on a Hikyo node
  and the root key can sign. That is the same trust boundary as every other
  sealed secret in Hikyo, which is why production deployments should use an
  offline root and short-lived, narrowly bound intermediates.
- OCSP, ACME, name constraints on intermediates, and hardware custody are
  follow-ups that each need their own amendment.
