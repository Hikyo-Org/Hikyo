# Private PKI: proposed ML-DSA lifecycle amendment

Status: **draft, not accepted or operative**. Issue:
[#1133](https://github.com/Hikyo-Org/Hikyo/issues/1133), under
[#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123).
Source baseline: `de0d8866d050c6e729bde76e2960eb6a260f1e3a`, checked
2026-11-01. [Implementation handoff and source map](../handoff/1133-pki-ml-dsa.md).

## Authority and delivery boundary

The accepted [private PKI ADR](./pki.md) governs current behavior. This document
proposes its scoped amendment; it neither enables ML-DSA nor changes any current
algorithm, default, profile, schema, trust anchor or compatibility claim.

#1133 explicitly blocks implementation until the decisions in
[#1124](https://github.com/Hikyo-Org/Hikyo/issues/1124) and the behavior in
[#1127](https://github.com/Hikyo-Org/Hikyo/issues/1127) are accepted. At this
baseline both remain open with their acceptance checklists unresolved. Source
exploration and bounded planning are permitted. This draft records that work;
none of #1133's feature acceptance criteria is claimed complete.

The policy owner and maintainer security reviewer must approve P1-P6 and the
scoped amendment with immutable decision links before dependent implementation
begins. P7 also requires post-implementation acceptance evidence. An issue
closure, draft PR, available library or automated review alone does not clear
a gate. Re-read the prerequisites before resuming.

| Decision | Required accepted input | Owner / dependency | Status |
| --- | --- | --- | --- |
| P1: algorithms and strength | Exact issuer and leaf parameter sets, issuer-signature / subject-key combinations, lifetimes and security strength; permitted usages; maintained implementation and version | #1124 policy owner and maintainer security reviewer | HOLD |
| P2: encodings and identity | PKCS#8, CSR and SPKI encodings/OIDs, signature schemes, algorithm identifiers, fingerprint format/version and maintained reference vectors | #1124 policy owner, applied by #1133 | HOLD |
| P3: chain authority | Approved root provisioning and authenticated trust distribution; full-path verification policy including anchors; cross-signed or hybrid composition only if explicitly accepted | #1124 policy owner and maintainer security reviewer | HOLD |
| P4: compatibility | Supported reader/verifier versions, strict versus compatibility behavior, configuration precedence, historical certificates, mixed-version and rollback floor | #1124 policy owner, #1133 lifecycle owner | HOLD |
| P5: bounded work | Per-key/CSR/certificate/chain/CRL bounds, chain depth/count, request and response budgets, concurrency, signing/verification CPU/memory and benchmark thresholds | #1124 policy owner, #1133 lifecycle owner | HOLD |
| P6: TLS integration | Accepted controlled-hop policy and actual consuming Go client/server behavior; separate certificate authentication and negotiated KEX evidence | #1127 owner and maintainer security reviewer | HOLD |
| P7: acceptance | Scoped PKI amendment approval, security review, supported-platform and both-engine lifecycle evidence, operator offline-root procedure | #1133 owner and maintainer security reviewer | HOLD |

No parameter set, new identifier, numeric limit or compatibility default is
selected by this draft. P1-P6 are inputs to the eventual accepted amendment;
P7's runtime evidence is a feature acceptance gate after implementation.

## Proposed policy obligations after approval

### Issuer, subject key and chain are separate policy dimensions

An approved leaf key does not establish the issuer signature's algorithm or
strength. A classical root signing an ML-DSA intermediate does not establish
a PQ-authenticated chain. Qualification must evaluate the selected trust path,
each required certificate signature and the authenticated trust anchor under
P1/P3, independently of the leaf's subject key and TLS key exchange.

Use maintained X.509, CSR, PKCS#8 and CRL implementations and approved reference
vectors. Do not invent a signature composition, substitute a primitive or
interpret successful parsing as approval. Reject unknown or mismatched
algorithms, unsupported encodings, invalid proof of possession, wrong keys,
invalid paths, tampered signatures and unsupported relying parties. A path
builder selecting a classical alternative cannot satisfy a PQ-required policy.
Cross-signed/hybrid-chain behavior remains unavailable until P3 explicitly
defines which signatures and paths must verify and how stripping is refused.

Import and intermediate installation must bind the supplied CA certificate to
the sealed key and approved policy, check CA/CRL usages, validity and path
constraints, and verify the required signatures. Trust-anchor handling needs
an explicit check: a chain verifier accepting an anchor is not proof that its
self-signature or provisioning met the authenticity policy. A CSR's names
cannot override explicit request fields and the bound profile.

The approved policy must distinguish certificate subject-name/SAN constraints
from X.509 intermediate name constraints. The existing ADR defers adding name
constraints to Hikyo-signed intermediates; this draft does not approve that
expansion. Existing imported-chain constraints must continue to be verified.

### Preserve custody, authority and profile narrowing

Retain the existing instance-owned issuer/profile objects, explicit
project/environment profile bindings, `instance-config` administration and
`issue-certificate` environment authority. Machines still require the
profile's opt-in; generated-key human issuance retains its environment-bound
reauthentication. Enforce the current SAN, EKU, TTL, organization and name
policy before signing, including the 90-day leaf ceiling.

Adding an algorithm to an existing profile is widening. The narrowing-only
update rule must remain decidable and fail closed; wider authority requires
an explicitly created and bound new profile. Changing issuer signature policy
must not bypass that rule through a profile's issuer-name reference.

Issuer private keys remain non-exportable software custody: sealed PKCS#8 with
the existing instance AAD, protected import input, instance DEK version fences
and registered re-encryption. No API/CLI/diagnostic/audit export is introduced.
Generated leaf keys remain display-once and never persisted; CSR issuance
does not receive a private key. Apply existing best-effort memory hygiene,
without claiming hardware isolation or guaranteed erasure of Go key objects.

### Preserve the full state machine

| Stage | Required behavior to extend and qualify |
| --- | --- |
| Generate/import | Evaluation-only online roots; generated/imported intermediates; strict algorithm/encoding classification before storage; protected import and no private-key output |
| Pending/install | Generate the intermediate CSR inside custody; offline root signs outside Hikyo; wrong key, unsupported chain or policy mismatch cannot activate it |
| Activate/issue | At most one active issuer version per name; authorize before expensive key work, re-authorize both issuance transactions and retain instance-DEK and issuer-state fences; no success before durable outcome |
| Renew | Revalidate current profile and names; CAS gives one successor; reuse the existing leaf public key and renew through the current active issuer version. Changing a leaf algorithm requires a new key and explicit new issuance, not implicit renewal conversion |
| Rotate/overlap | Activate N+1 and retire N from minting atomically; N continues required CRL coverage. Reject stale signing plans after rotate/revoke/hold, including across nodes |
| Revoke/unknown | Unknown issuance remains a fail-closed revocation entry; revocation stays available when policy narrows or stored SAN data is corrupt; issuer compromise destroys its key and revokes affected certificates |
| Retire | Retain keys while existing retirement rules require revocation coverage for certificates or child issuers. Destroy only eligible issuer key ciphertext; retain required public history and CRLs |
| Restore/restart | Hold every restored issuer before minting or fresh CRL signing. Stored CRLs remain readable, even if stale; reconciled operator authority and known revocations precede audited hold release |

Preserve redacted durable INTENT/OUTCOME auditing, authenticated CRL reads,
monotonic CRL numbers, revocation snapshot/publication CAS and runtime
maintenance exclusion for the whole CRL signing act. Re-encryption must retain
AAD and key identity while moving only the protected envelope's DEK version.
None of these invariants can be inferred from an isolated signing test.

### Bound expansion at each owner

The current [OpenAPI contract](../../api/openapi.yaml) caps issuer private-key
PEM at 16,384 characters, certificate/CSR PEM inputs at 65,536 and chain PEM
at 262,144. The [CLI](../../internal/cli/pki.go) separately caps CA key input
at 16,384 bytes; its public-file reads use `os.ReadFile`.
The [HTTP validator](../../internal/server/api.go) caps the aggregate request
at 2 MiB before decoding. These are baseline observations, not approved PQ
budgets; PEM, DER, JSON escaping, counts and total allocation differ.

P5 must decide bounded inputs and outputs for parsers, CLI reads, API/client
validation, certificate chains, CRL assembly/publication and TLS consumers.
Measure representative revocation populations, not only a single leaf.
Review response/storage/worker bounds separately from request admission.
Do not raise the shared request limit, remove admission, loosen unrelated
routes or accept unbounded crypto work merely to accommodate larger keys.
Boundary, just-over-boundary and malformed-input cases must prove refusal
before prohibited allocation/signing and preserve cancellation/deadlines.

The SQL schemas currently have no closed algorithm CHECK to widen:
`key_algorithm` is `TEXT NOT NULL` in both engines. They do enforce custody,
state, key-presence and uniqueness constraints. Any accepted policy/schema
change needs a new roll-forward migration, generated queries/types and
compatibility declarations as applicable, preserving those constraints.
Do not rewrite migration 00064 or assume free-form storage means approval.

### Reader-first rollout and rollback contract

This is a qualification sequence, not authorization to operate an environment.
After P1-P6 acceptance, use a disposable topology and record each phase:

1. Validate compatible readers/verifiers, API/CLI/client surfaces, stored-key
   parsing and CRL consumers before enabling new issuers or writes. Include
   every node and the mixed-version cohort; an incompatible reader blocks
   progression rather than silently accepting or misclassifying an algorithm.
2. Provision the approved offline root and distribute its authenticated trust
   to the named relying parties through separately authorized custody. Root
   identity, exact versions and trust-install receipts must precede issuer
   activation; an online evaluation root is not production-root acceptance.
3. Create/import the approved intermediate, verify/install it, bind a new
   policy where widening is needed, and demonstrate issue/renew/revoke/CRL
   flows with real relying parties. Preserve classical identities under the
   explicitly accepted compatibility profile.
4. Exercise N/N+1 overlap, concurrent issuance/rotation/revocation, crashes,
   restart and multi-node failover on SQLite and PostgreSQL. Restore an older
   archive, retain the issuer hold, reconcile revocations and verify prior
   issuer CRLs and historical identity validation.
5. Rehearse abort and rollback at each phase. Stop new writes on unsafe policy,
   reader, trust, bounds, audit or fence failures. Once new formats are written,
   refuse rollback below the approved reader/format floor. Keep a compatible
   reader and existing trust/CRLs; never strip mandatory PQ verification,
   revive revoked authority or destroy retained keys to make rollback work.

Operator evidence must state which keys, trust anchors, CRLs, public history
and backups remain needed, their custody owner and retirement conditions.
Issuer retirement is constrained by existing revocation coverage; an obsolete
binary is not a recovery strategy. Production trust activation, root signing,
deployment and provider changes still require separate authorization.

## Requirement-to-evidence contract

All rows below are **NOTRUN** for PQ behavior in this draft. Existing classical
fixtures are starting points, not evidence that ML-DSA lifecycle support exists.

| #1133 criterion | Required evidence after prerequisites |
| --- | --- |
| AC1: policy amendment | Accepted P1-P6 values and immutable scoped owner/security decisions; separate issuer/leaf/path policy and compatibility claims |
| AC2: maintained lifecycle | Generate and import an approved issuer, PKCS#8 seal/open, evaluation root, offline intermediate CSR/install, activation, issue and renewal; wrong-key and malformed/unknown-algorithm refusals |
| AC3: consumers and bounds | Complete enum/type/schema/client inventory; generator freshness where affected; fingerprint compatibility; key/CSR/cert/chain/CRL boundary and resource-exhaustion tests |
| AC4: existing guarantees | Cross-tenant/environment, missing capability, withdrawn machine opt-in, missing reauth, SAN/EKU/TTL/name violation and widening refusals; prove no issuer export or persisted/disclosed leaf-key replay |
| AC5: certificate/CRL lifecycle | Independent maintained-library path/signature and CRL verification; tampered signatures/CSRs, mismatched public keys, revoked/unknown serials, stale CRLs, overlap/retirement and unsupported-peer refusal |
| AC6: actual TLS | Compatible Go client/server performing authenticated application exchange, with chain and signature-scheme evidence; record negotiated KEX separately through #1127. Untrusted/expired/wrong-host/classical-only peers must fail as required before canary application data |
| AC7: durability | Both engines: DEK rotate/re-encrypt and corruption refusal, issue/rotate/revoke races, multi-node restart, unknown issuance, restored issuer hold, reconciliation and prior-version CRL signing/publication |
| AC8: migration | Reader/verifier-first mixed versions, authenticated CA distribution, retained classical compatibility, policy-refused alternate chains, interrupted activation and rollback above the format floor; cross-signing only if approved |
| AC9: budgets and operations | Exact Go/library/OS/architecture and peer versions; DER/PEM chain, handshake and representative CRL sizes; sign/verify/handshake latency, CPU, memory and concurrency against approved budgets on amd64/arm64; validated offline-root runbook |
| AC10: scope | Retain private short-lived service identity only; no public CA, ACME, OCSP, SSH signature migration, HSM/KMIP product or production root activation |

The compatibility matrix must name the exact Go toolchain, client/server
builds, signature scheme, verified path/anchor, negotiated KEX, CRL checker,
effective profile, observed result, exception owner and recheck trigger.
Browser, OpenSSH and external TLS stacks remain **unqualified** until separately
proved; #1134 owns external compatibility disposition, and #1135 aggregates
qualification. ML-DSA certificate support alone proves neither PQ key exchange
nor universal interoperability, compliance validation or deployed acceptance.

Record candidate commit/tree and artifact digests for every evidence row;
keep private keys, credentials and protected payloads out of logs and reports.
Local source/check evidence, hosted CI, independent automated review, maintainer
security approval, actual TLS/lifecycle observations and deployment receipts
remain separate. Missing evidence stays HOLD/NOTRUN rather than becoming a
capability badge. This draft creates no executable PQ harness or runtime proof.
