# Research: forward-secret transit keys for stateless server nodes

Date: 2026-11-12. Ticket: [Hikyo-Org/Hikyo#1306](https://github.com/Hikyo-Org/Hikyo/issues/1306). Status: research only. This note presents options and trade-offs and makes no decision.

## The question

Decision 12 of the hosted-service ADR (`docs/adr/hosted-service.md`, section "12. Presented keys are encrypted to the installation") seals every holder secret in an HPKE envelope to an hourly epoch key. Each epoch key pair is derived by HKDF from one tier-3 `holderTransitKey`, which is wrapped by the master key. The ADR states the consequence: "Anyone who later obtains the root key can open every archived capture of these envelopes, recovery keys included, however old." Acceptance gate 11 tests refusal of non-envelope secrets, expired epochs and the 30-second window. It does not test forward secrecy.

The ticket asks which constructions give forward secrecy while any node can serve any request. It also asks how they compare on state, failure modes and node start-up, and how established HPKE libraries and cloud KMSs support them.

Repository facts that bound the answer (all paths relative to the repository root):

- The ADR says losing `holderTransitKey` "costs nothing, because no stored data is under it" (`docs/adr/hosted-service.md`, decision 12). Any option may therefore treat losing a transit key as a brief availability event rather than data loss.
- The key encapsulation mechanism (KEM) is not fixed by decision 12. It names HPKE (RFC 9180) and "an established implementation". X25519 appears in the ADR only for slot record wrapping (`docs/adr/hosted-service.md`, lines 83-84 and 282).
- Backups are a logical `hikyo export`, an age-encrypted archive (`docs/adr/encryption-model.md`, "Backups and exports"). The backup drill builds its raw dump from per-table `COPY` output, not `pg_dump` (`internal/isolation/backup_drill_e2e_test.go`, around line 815). Backup retention is 180 days (`docs/adr/ops-spec.md`, "Backups"). `chart/`, `deploy/`, `docs/` and `internal/` contain no pg_basebackup, pgBackRest, Barman, VolumeSnapshot or CloudNativePG configuration (`rg` search; the only CloudNativePG hit is a competitor note in `docs/reports/deployment-landscape-2026-10-31.md`). The operator's PostgreSQL HA backup method (PITR, volume snapshots) is therefore operator-supplied, and I mark it **unknown**.
- External KMS is reserved only "at the root-key boundary" (`docs/adr/encryption-model.md`, "External KMS is interface-reserved, not implemented, in v1"). A KMS call on the request path would be a new seam.
- The repo is on Go 1.27.3 (`go.mod`, `go 1.27.3`). `filippo.io/hpke v0.4.0` and `github.com/cloudflare/circl v1.6.4` are already indirect dependencies (through `filippo.io/age` and `internal/pairing`). The web app has no HPKE dependency yet (`web/package.json`).

## Short answer

1. HPKE has no recipient forward secrecy of its own. Forward secrecy comes only from deleting epoch private keys, as RFC 9458 recommends for Oblivious HTTP. The options differ only in where the short-lived private keys live, which then decides how long they survive deletion.
2. The forward-secrecy horizon is set by the longest-lived copy of a deleted key: PostgreSQL WAL and backups, KMS deletion waiting periods, or process memory. The hourly rotation sets only the floor.
3. Two options keep keys out of PostgreSQL's durable history:
   - (B) random epoch keys in an `UNLOGGED` table, which PostgreSQL excludes from WAL, standbys and physical base backups. B needs nothing new.
   - (F) random epoch keys wrapped by a short-lived KMS key that is then deleted. F needs a KMS, kept off the hot path, which is a new seam.
   Each has a caveat, listed below.
4. Per-request KMS decapsulation (E) works with Go's standard `crypto/hpke` through its opaque-key interfaces, but among cloud KMSs only AWS offers a usable operation: ECDH on NIST curves or ML-KEM. A self-run PKCS#11 HSM can do the same in principle. Google Cloud KMS offers only a combined X-Wing decapsulation that Go's API cannot use, and Azure offers neither ECDH nor a KEM.
5. Per-org derivation gives no forward secrecy. Puncturable and forward-secure public-key encryption has no established implementation, is not RFC 9180, and needs shared writable key state.

## Background facts

**HPKE is not forward secret for the recipient.** RFC 9180 section 9.7.4: "HPKE ciphertexts are not forward secret with respect to recipient compromise in any mode. This means that compromise of long-term recipient secrets allows an attacker to decrypt past ciphertexts encrypted under said secrets." Source: https://www.rfc-editor.org/rfc/rfc9180.html#section-9.7.4.

The PSK modes are no help here. In them, secrecy is expected to hold as long as the recipient private key skR and the pre-shared key are not both compromised (RFC 9180 section 9.1, https://www.rfc-editor.org/rfc/rfc9180.html#section-9.1). Hikyo's clients are public clients, so a shared PSK would be known to every client and gives no forward secrecy.

**Rotation plus deletion is the standard remedy.** RFC 9458 (Oblivious HTTP) section 6.6: "A measure of forward secrecy can be provided by generating a new key configuration then deleting the old keys after a suitable period." Source: https://www.rfc-editor.org/rfc/rfc9458.html#section-6.6.

The same RFC's section 6.4 handles a replaced or unknown key ID with HTTP 422. That gives Hikyo a ready precedent for a "refetch the key and retry" client path: https://www.rfc-editor.org/rfc/rfc9458.html#section-6.4.

**Deterministic derivation is what removes forward secrecy.** RFC 9180 `DeriveKeyPair(ikm)` is deterministic (section 7.1.3, https://www.rfc-editor.org/rfc/rfc9180.html#section-7.1.3), so anyone who holds the input keying material can rebuild every epoch key. That is the current design.

**Lifetime of one epoch key.** Under decision 12, the key for epoch *e* is published as "next" during *e−1* and accepted as "previous" during *e+1*, plus a short skew. Each private key must therefore exist for about three hours. An envelope captured in epoch *e* stays openable until the end of *e+1* at the earliest. This is the best horizon any option below can reach with hourly epochs and the current acceptance rule. Shortening it means changing the acceptance rule, not the storage.

**Go does not guarantee that key memory is erased.** `runtime/secret` exists "in the service of forward secrecy", but it is experimental, "only exists when building with the GOEXPERIMENT=runtimesecret environment variable set", and is "Currently only supported on linux/amd64 and linux/arm64". Source: Go 1.27.3 `src/runtime/secret/doc.go` and `secret.go`, https://github.com/golang/go/tree/go1.27.3/src/runtime/secret (read locally from the 1.27.3 toolchain). Every option that holds private keys in Go memory therefore has a memory-remanence caveat. A memory dump is already outside decision 12's protection.

## PostgreSQL facts that bound the "store keys and delete them" options

- **A DELETE does not erase the row.** "In PostgreSQL, an UPDATE or DELETE of a row does not immediately remove the old version of the row." Standard VACUUM "removes dead row versions … and marks the space available for future reuse"; the docs do not say it overwrites the bytes. Source: https://www.postgresql.org/docs/current/routine-vacuuming.html.
- **The WAL records every write.** "The log records every change made to the database's data files", and a base backup plus archived WAL supports point-in-time recovery to "any time since your base backup was taken". Source: https://www.postgresql.org/docs/current/continuous-archiving.html. A key row written to an ordinary table therefore lives as long as the WAL archive and PITR window.
- **Unlogged tables stay out of the WAL and standbys.** "Data written to unlogged tables is not written to the write-ahead log … an unlogged table is automatically truncated after a crash or unclean shutdown. The contents of an unlogged table are also not replicated to standby servers." Source: https://www.postgresql.org/docs/current/sql-createtable.html.
- **Physical base backups exclude unlogged data.** `BASE_BACKUP` excludes "Unlogged relations, except for the init fork which is required to recreate the (empty) unlogged relation on recovery." Source: https://www.postgresql.org/docs/current/protocol-replication.html.
- **`pg_dump` includes unlogged data by default.** It dumps unlogged table data unless `--no-unlogged-table-data` is given, and always excludes it when dumping from a standby. Source: https://www.postgresql.org/docs/current/app-pgdump.html.
- **Not verified:** whether storage or volume snapshots (cloud disk snapshots, CSI VolumeSnapshots) include unlogged table files. They copy the block device, so they very probably do, but I found no primary source.

## Options

Each option below keeps decision 12's envelope, AAD and window. Only where the epoch private key comes from changes.

### Baseline: HKDF from `holderTransitKey` (decision 12 as locked)

- **State:** one wrapped tier-3 key.
- **Forward secrecy:** none. The root key plus any backup that holds the wrapped `holderTransitKey` rebuilds every epoch key.
- **Node start-up:** unwrap once, then derive locally.
- **Failure mode:** none beyond the master key's.
- **Library fit:** `KEM.DeriveKeyPair` exists in Go's `crypto/hpke` (https://pkg.go.dev/crypto/hpke#KEM) and `Scheme.DeriveKeyPair` exists in circl (https://pkg.go.dev/github.com/cloudflare/circl/kem#Scheme).

### A. Random epoch keys in an ordinary (logged) PostgreSQL table, deleted after use

**Construction.** The first node to need epoch *e+1* generates a random key pair (`KEM.GenerateKey`), wraps the private key under the master key, and inserts it with `INSERT … ON CONFLICT (epoch) DO NOTHING`. All nodes then read the winning row. Rows are deleted once epoch *e+2* begins.

- **State:** about three rows. Generation needs no lock beyond the unique constraint.
- **Forward-secrecy horizon:** whichever copy lives longest:
  - dead tuples on disk, until the space is reused;
  - the WAL archive and PITR window;
  - every operator backup taken while the row existed.

  If `hikyo export` includes the table, the horizon becomes the 180-day retention (`docs/adr/ops-spec.md`). Hikyo could exclude the table from its own export, but it cannot exclude it from operator-level PITR. Against the ADR's own attacker ("a backup and the root key together", `docs/adr/hosted-service.md`, decision 2 and "The honest statement"), this option gives forward secrecy only after backup and WAL retention, not after one hour.
- **Node start-up:** read the rows and unwrap them with the master key. No new infrastructure.
- **Failure mode:** like any datastore write. A lost row is regenerated, and clients refetch.
- **Library fit:** any HPKE library. Go `crypto/hpke` provides `GenerateKey`, `PrivateKey.Bytes()` and `KEM.NewPrivateKey` (https://pkg.go.dev/crypto/hpke).

### B. Random epoch keys in an `UNLOGGED` PostgreSQL table

**Construction.** Same as A, but the table is `UNLOGGED`.

- **State:** about three rows, shared through the primary only.
- **Forward-secrecy horizon:**
  - **Excluded from:** the WAL, standbys and physical base backups (sources above).
  - **Still exposed in:** the primary's data files until vacuum reuses the space, and a `pg_dump` run against the primary without `--no-unlogged-table-data`.
  - **Not verified:** storage snapshots (see the PostgreSQL facts above).
  - **Hikyo's own export:** `hikyo export` must also skip the table.
  - **Overall:** if the operator's backups are physical or PITR and exports skip the table, the horizon approaches the three-hour key lifetime plus vacuum delay. The operator's actual backup method is **unknown** from the repository.
- **Wrapping trade-off:**
  - Wrapping rows under the master key means a stolen disk image or dump opens them only together with the root key. It costs one unwrap per epoch per node.
  - Leaving rows unwrapped means a disk image alone opens current keys.
  - Either way, a later root-key leak without such a copy opens nothing, because no durable copy exists.
- **Node start-up:** read the rows. No new infrastructure.
- **Failure mode:** a crash, unclean shutdown or failover empties the table ("automatically truncated", "not replicated to standby servers"). Nodes regenerate keys and clients holding the old public key get refused once. This is acceptable only because decision 12 says transit-key loss costs nothing, but it requires a client "refetch key and retry" path, like RFC 9458's 422. A node that still holds keys in memory could reinsert them, which makes the failover shorter but keeps keys alive longer.
- **Reads must go to the primary:** a hot standby holds no unlogged data, and I have not verified whether querying such a table during recovery returns empty or errors. Every node must therefore read this table from the primary. Decision 10 says "Read replicas are not a goal" (`docs/adr/hosted-service.md`, line 186), so this fits the current design.
- **Library fit:** same as A.

### C. Random epoch keys held only in node memory, shared peer to peer

**Construction.** Keys never touch PostgreSQL. Nodes exchange them over an authenticated node-to-node channel. PostgreSQL may still serve as a lock or directory holding only public keys.

- **State:** in process memory only.
- **Forward-secrecy horizon:** about three hours, plus Go memory remanence (see `runtime/secret` above). Option E reaches the same three hours, but only against an attacker who can call ECDH or decapsulate and cannot cancel key deletion.
- **Node start-up:** a new node must fetch keys from a live peer, which needs peer discovery and mutual authentication between nodes. Hikyo nodes do not have either today. I found no node-to-node channel in the ADRs I read; mark this **not checked** beyond those.
- **Failure mode:** if every node restarts at once, all keys are lost. In-flight envelopes fail and clients refetch. Two nodes generating the same epoch concurrently need a tie-break, for example an advisory lock or a public-key row in PostgreSQL.
- **Variant:** each node keeps its own key pair and publishes only the public key. The client then seals to every node, which grows the envelope with the node count. Otherwise the client cannot know which node will serve it.
- **Library fit:** any HPKE library.

### D. Hash-chain ratchet (key evolution)

**Construction.** Store one seed *s_e*. Each epoch, set *s_{e+1} = H(s_e)*, overwrite the row, and derive the epoch pair with `DeriveKeyPair(KDF(s_e))`. This is a forward-secure pseudorandom generator. Bellare and Yee identify "forward-secure pseudorandom bit generators as the central primitive" of forward security for private-key cryptography (https://eprint.iacr.org/2001/035).

- **State:** one row. To serve "previous", keep *s_{e−1}* or the derived previous private key.
- **Forward-secrecy horizon:** an overwritten seed in an ordinary table survives in dead tuples, the WAL and backups, the same as A. In an `UNLOGGED` table it behaves like B.
- **Difference from A and B:** a leaked current seed yields every future key until a reseed, so there is no post-compromise security. Random keys leak only the keys present at the time. In exchange, a node holding *s_e* can compute *e+1* without coordination.
- **Node start-up:** read one row.
- **Failure mode:** the same as A or B, depending on which table holds the seed.
- **Library fit:** `DeriveKeyPair` in Go `crypto/hpke` and circl. RFC 9180 says an IKM to `DeriveKeyPair` "MUST NOT be reused elsewhere" (section 9.2.3, https://www.rfc-editor.org/rfc/rfc9180.html#section-9.2.3).

### E. Per-request decapsulation in a KMS or HSM, one KMS key per epoch, deleted after the window

**Construction.** The epoch private key is created in the KMS and is not exportable. Every request that carries a holder secret makes one KMS call (ECDH or decapsulate). After the window, the KMS key is scheduled for deletion.

- **State:** KMS key IDs per epoch, which could be stored in PostgreSQL. No private key material is held by Hikyo.
- **Forward-secrecy horizon:**
  - A leaked Hikyo root key opens nothing, because the epoch keys never existed outside the KMS.
  - KMS custody alone does not give forward secrecy. While a key exists, anyone with a live KMS credential can use it, which is equivalent to a root-key leak. Only deletion gives forward secrecy.
  - **AWS:** a waiting period of "7 – 30 days … might be up to 24 hours longer", during which "A KMS key pending deletion cannot be used in any cryptographic operations" (https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html). An attacker with only DeriveSharedSecret permission is therefore cut off at the end of the window. One who can call `CancelKeyDeletion` keeps access through the waiting period.
  - **GCP:** a destroy-scheduled duration with a minimum of 24 hours and a maximum of 120 days, fixed at key creation. Versions cannot be used in that state (https://docs.cloud.google.com/kms/docs/key-states). Key material can remain in Google systems "up to 45 days from the scheduled destruction time" (https://docs.cloud.google.com/kms/docs/destroy-restore).
- **Hot-path cost:**
  - One KMS round trip per holder-secret request. Latency was **not measured**.
  - The AWS quota for ECC operations, `DeriveSharedSecret` included, is "1,000 (shared)" requests per second per account and Region. ML-KEM `Decapsulate` has its own 1,000 per second (https://docs.aws.amazon.com/kms/latest/developerguide/requests-per-second.html).
  - This bears on decision 10's throughput prerequisites.
- **Key cost:** AWS charges "$1/month (prorated hourly)" per key and nothing while pending deletion (https://aws.amazon.com/kms/pricing/). About three active hours per epoch key comes to roughly 72 key-hours a day. That is my arithmetic from the published rate, not a quote. The per-request price for DeriveSharedSecret and Decapsulate is **not verified**: the pricing page did not list it.
- **Failure mode:** a KMS outage or throttling fails every holder-secret request in that period. Key creation must run ahead of the epoch; the `CreateKey` quota is 5 per second, which is enough.
- **Node start-up:** cloud credentials only.
- **Self-hosting:** this ties the request path to a cloud provider. It is a new seam beyond the root-key boundary in `docs/adr/encryption-model.md`.
- **Library fit:** see the KMS section below. Only AWS ECDH (DHKEM over NIST curves) and AWS ML-KEM plug into Go's `crypto/hpke` without hand-written HPKE code.

### F. Random epoch keys wrapped by a short-lived KMS key, decapsulated locally

**Construction.** As in A, a node generates random epoch keys locally and stores them in PostgreSQL, which may be logged. The wrapping key is a KMS symmetric key for one day or one batch of epochs, used with `Encrypt`/`Decrypt` and the epoch bound in the encryption context. That KMS key is scheduled for deletion once all of its epochs have expired. Each node unwraps each epoch key once and decapsulates in memory.

- **State:** wrapped rows in PostgreSQL, plus one KMS key per batch.
- **Forward-secrecy horizon:**
  - The KMS key's lifetime plus its deletion waiting period: AWS at least 7 days; GCP at least 24 hours, plus up to 45 days of residual material.
  - Plus Go memory remanence.
  - PostgreSQL backups and WAL no longer matter. They hold only ciphertext under a deleted KMS key, and a later root-key leak opens nothing.
  - A live KMS credential before deletion opens the wrapped rows from any backup, the same caveat as E.
- **Hot-path cost:** none. KMS is called only at node start-up and once per epoch per node. The AWS symmetric quota is 10,000 to 100,000 per second, depending on Region (source as in E).
- **Failure mode:** a KMS outage blocks only unwrapping new epochs and starting new nodes. Running nodes keep serving until their cached keys expire.
- **Node start-up:** read the rows and make one KMS `Decrypt` per live epoch.
- **Library fit:** any HPKE library and any KEM, X25519 included, because decapsulation is local. GCP's equivalent is a symmetric key, one version per batch, destroyed by version (https://docs.cloud.google.com/kms/docs/destroy-restore).
- **Self-hosting:** same seam concern as E, but off the hot path.

### G. Per-session keys

**Construction.** At session creation, the server generates an HPKE key pair and returns its public key. The private key lives with the session (in B-style or F-style storage) and is deleted at logout or expiry. Decision 12's AAD already binds the session.

- **Forward-secrecy horizon:** the session lifetime plus the storage horizon of whichever store holds the key.
- **Gaps:** this does not cover requests without a session, such as service-account token bodies sent by the CLI and operator, or the holder secret presented while establishing a session. Those still need epoch keys. It adds per-session state.
- **Library fit:** any HPKE library.

### H. Per-org derivation

- **HKDF from one root:** deriving per-org transit keys from one root by HKDF gives **no** forward secrecy. The root still rebuilds every org's keys.
- **Independent random per-org keys:** these are option A or B multiplied by the number of orgs. They limit the blast radius of a leak of a single org's key, but do nothing against a root-key leak that opens all of them.
- **Disclosure (inference, not sourced):** clients would have to fetch an org-specific public key, so the public key route would take an org identifier.
- **Sealed orgs:** a Sealed org holds no server-side secret without a presented key, so there is nothing per-org that the server could use to wrap a forward-secret key independently of the root.

### I. Puncturable or forward-secure public-key encryption

What the papers propose:

- Canetti, Halevi and Katz, "A Forward-Secure Public-Key Encryption Scheme" (Eurocrypt 2003, https://eprint.iacr.org/2003/083): "secret keys are updated at regular periods of time" and "all parameters grow at most logarithmically with the total number of time periods".
- Green and Miers, "Forward Secure Asynchronous Messaging from Puncturable Encryption" (IEEE S&P 2015, https://ieeexplore.ieee.org/document/7163033, PDF https://cs.umd.edu/~imiers/pdf/forwardsec.pdf): recipients update decryption keys to revoke selected messages or periods.
- Günther, Hale, Jager and Lauer, "0-RTT Key Exchange with Full Forward Secrecy" (Eurocrypt 2017, https://eprint.iacr.org/2017/223).
- Derler, Gellert, Jager, Slamanig and Striecks, "Bloom Filter Encryption" (Eurocrypt 2018 and JoC 2021, https://eprint.iacr.org/2018/199): "Forward secrecy is achieved by 'puncturing' the secret key after each decryption operation."

Fit for Hikyo:

- **Shared writable state:** puncturing on each decryption mutates the secret key. Stateless nodes would need serialized writes to shared key state on every request, and that state would then sit in PostgreSQL, WAL and backups like A.
- **Not HPKE:** these schemes are not RFC 9180. Decision 12 requires "an established implementation", and I found no maintained Go or browser library for any of them (search not exhaustive).
- **Little gain over rotation:** for hourly epochs, CHK-style time-based forward secrecy adds nothing over options B, C or F except one fixed public key. Decision 12 already publishes keys hourly.
- **Not checked:** I read only the abstracts. Key sizes, false-positive rates and costs are not checked.

## Comparison

"Horizon" means how long after capture a later compromise of Hikyo's root key or KMS can still open the envelope. *W* is the roughly three-hour key lifetime under decision 12.

| Option | Horizon, and what sets it | Shared state | Keys lost → | Node start-up | Hot-path cost | Go `crypto/hpke` / circl / browser | KMS needed |
|---|---|---|---|---|---|---|---|
| Baseline (HKDF) | Unbounded: the root rebuilds every key | 1 wrapped key | n/a | Unwrap once | None | All | No |
| A. Random keys, logged PG | WAL/PITR and backup retention (up to 180 days for `hikyo export` if included) | ~3 rows | Regenerate, clients refetch | Read rows | None | All | No |
| B. Random keys, unlogged PG | *W* plus vacuum, if backups are physical and `pg_dump`/snapshots and export skip it (operator method **unknown**) | ~3 rows, primary only | Failover or crash empties table, one refused request per client | Read rows | None | All | No |
| C. Memory only, peer to peer | *W* plus memory remanence | Process memory | Full restart loses all, clients refetch | Fetch from a peer (new infrastructure) | None | All | No |
| D. Hash ratchet | Same as A or B for its table; future keys exposed by a leak | 1 row | As A or B | Read 1 row | None | All (`DeriveKeyPair`) | No |
| E. KMS decapsulation per request | *W*, or *W* + deletion wait for an attacker who can cancel deletion (AWS 7–30 days; GCP ≥24 h, +45 days residual) | KMS key IDs | KMS outage fails all holder-secret requests | Credentials only | 1 KMS call per request; AWS ECC 1,000 rps per account | Go stdlib only (AWS P-256 ECDH, or AWS ML-KEM draft); circl no; browser P-256 yes | Yes, AWS only |
| F. KMS-wrapped epoch keys | Life of the KMS wrapping key + deletion wait, independent of PG backups | Wrapped rows + 1 KMS key per batch | KMS outage blocks new epochs/nodes only | 1 KMS `Decrypt` per epoch | None | All | Yes (symmetric; AWS or GCP) |
| G. Per-session keys | Session lifetime + its store's horizon | Per-session rows | Session re-establish | Read on demand | None | All | Optional |
| H. Per-org derivation | Same as underlying option; HKDF variant has none | × orgs | As underlying | As underlying | None | All | No |
| I. Puncturable / fs-PKE | Bounded by puncture state's storage | Mutated per decryption | n/a | n/a | Write per request | None established | No |

## Library support

**Go standard library `crypto/hpke`.** New in Go 1.26: "implements Hybrid Public Key Encryption (HPKE) as specified in RFC 9180, including support for post-quantum hybrid KEMs" (https://go.dev/doc/go1.26). The repo's Go 1.27.3 has it. API from https://pkg.go.dev/crypto/hpke, checked against `go doc` locally:

- **KEMs:** DHKEM over P-256, P-384, P-521 and X25519. Also ML-KEM-768, ML-KEM-1024, MLKEM768-X25519, MLKEM768-P256 and MLKEM1024-P384 "from draft-ietf-hpke-pq".
- **Key handling:** `GenerateKey`, `DeriveKeyPair`, `NewPrivateKey` and `PrivateKey.Bytes()` cover options A to D and F to H.
- **Opaque ECDH keys:** `NewDHKEMPrivateKey(priv ecdh.KeyExchanger)` is "meant for applications that already have … another implementation of a ecdh.KeyExchanger (e.g. a hardware key)". `crypto/ecdh.KeyExchanger` is "an interface for an opaque private key … For example, an ECDH key kept in a hardware module" (Go 1.26 release notes).
  - Source check (Go 1.27.3 `src/crypto/hpke/kem.go`, `dhKEMPrivateKey.decap`): decapsulation calls only `k.priv.ECDH(pubEph)` and `k.priv.PublicKey()`, so a KMS-backed `KeyExchanger` works for option E.
  - `Bytes()` returns "ecdh: private key does not support Bytes" for such keys, as expected.
- **Opaque ML-KEM keys:** `NewMLKEMPrivateKey(priv crypto.Decapsulator)` accepts an opaque ML-KEM key. Its `Encapsulator()` must return `*mlkem.EncapsulationKey768` or `*mlkem.EncapsulationKey1024` (`src/crypto/hpke/pq.go`, line 481 in 1.27.3).
- **Opaque hybrid keys:** `NewHybridPrivateKey(pq crypto.Decapsulator, t ecdh.KeyExchanger)` calls the ML-KEM part and the ECDH part separately (`pq.go`, around lines 338-351). A KMS that exposes only a combined X-Wing decapsulation therefore cannot plug in.

**`github.com/cloudflare/circl/hpke`.** `Suite.NewReceiver(skR kem.PrivateKey, info)` (https://pkg.go.dev/github.com/cloudflare/circl/hpke).

- **Supported:** P-256, P-384, P-521, X25519, X448 and X-Wing KEMs, and the Base, Auth, PSK and AuthPSK modes. The package "does not implement the 'Export-Only' mode".
- **No opaque keys:** source check (v1.6.4 `hpke/xkem.go` line 29 `SK := sk.(*xKEMPrivKey)`, and `hpke/shortkem.go` holding `priv *ecdh.PrivateKey`) shows decapsulation type-asserts circl's own key types. An external or KMS key cannot be supplied, so circl fits options A to D and F to H but not E.

**`filippo.io/hpke` v0.4.0.** Already in the module graph. It exposes the same `NewDHKEMPrivateKey(ecdh.KeyExchanger)` and hybrid constructors with opaque-key support, and its interfaces "will be added to the crypto package in Go 1.26+" (https://pkg.go.dev/filippo.io/hpke). It appears to be the precursor of the standard-library package, an inference from the matching API.

**Browser (web app).** hpke-js is "built on top of Web Cryptography API".

- **Supported:** `@hpke/core` provides DHKEM P-256, P-384, P-521 and X25519.
- **Experimental:** `@hpke/ml-kem` is marked "experimental, not standardized" and `@hpke/hybridkem-x-wing` "experimental" (https://github.com/dajiaji/hpke-js).
- **Not checked:** current browser support for WebCrypto X25519.

**Other HPKE stacks** (OpenSSL, BoringSSL, Rust crates) were not checked.

**Post-quantum HPKE is still a draft.** draft-ietf-hpke-pq is at -05 (6 July 2026), with IESG state "I-D Exists" (https://datatracker.ietf.org/doc/draft-ietf-hpke-pq/). Any option whose KMS forces ML-KEM therefore conflicts with decision 12's "established implementation", on the browser side at least. I report the conflict without resolving it.

## KMS and HSM support

**AWS KMS: usable for options E and F.**

- **ECDH:** `DeriveSharedSecret` runs ECDH with a `KEY_AGREEMENT` key and returns "the raw shared secret". It supports only NIST curves (P-256, P-384, P-521) and SM2 in China Regions. Ed25519 keys are signing-only, and there is no X25519 (https://docs.aws.amazon.com/kms/latest/APIReference/API_DeriveSharedSecret.html, https://docs.aws.amazon.com/kms/latest/developerguide/symm-asymm-choose-key-spec.html).
  - The peer key must be DER SubjectPublicKeyInfo, so a server-side `KeyExchanger` has to convert the HPKE `enc` point.
  - **Not verified:** that AWS's raw `SharedSecret` is byte-identical to the RFC 9180 `DH()` output (the x-coordinate). Test this against RFC 9180 test vectors before relying on it.
  - The optional `Recipient` parameter returns the secret encrypted to a Nitro enclave instead of in plaintext.
  - Result: option E on AWS means DHKEM(P-256), not X25519.
- **ML-KEM:** keys `ML_KEM_512`, `ML_KEM_768` and `ML_KEM_1024` with `Decapsulate`, which returns a 32-byte shared secret with implicit rejection (https://docs.aws.amazon.com/kms/latest/APIReference/API_Decapsulate.html). This fits Go's `NewMLKEMPrivateKey`, subject to the draft status above.
- **Deletion and limits:** see option E. Quotas: https://docs.aws.amazon.com/kms/latest/developerguide/requests-per-second.html.
- **Not checked:** whether AWS retains key material after the waiting period, and whether keys pending deletion count against the KMS key resource quota.

**Google Cloud KMS: usable for option F only.**

- **No ECDH:** there is no key-agreement purpose. EC keys are signing-only. The purposes are ENCRYPT_DECRYPT, ASYMMETRIC_SIGN, ASYMMETRIC_DECRYPT, RAW_ENCRYPT_DECRYPT, MAC, KEY_ENCAPSULATION and AES_WRAPPING (https://docs.cloud.google.com/kms/docs/reference/rest/v1/projects.locations.keyRings.cryptoKeys, https://docs.cloud.google.com/kms/docs/algorithms).
- **KEMs:** `KEM_XWING`, `ML_KEM_768` and `ML_KEM_1024`, at the software protection level only. HSM levels list no KEM algorithms. "Cloud KMS doesn't provide encapsulation capabilities" (https://docs.cloud.google.com/kms/docs/key-encapsulation-mechanisms).
- **Why option E fails here:** X-Wing decapsulation is one combined call, which Go's `NewHybridPrivateKey` cannot use. Using it would mean writing the RFC 9180 key schedule by hand, contrary to decision 12. ML-KEM-only HPKE would fit `NewMLKEMPrivateKey`, but it is draft and software-only.
- **Deletion:** see option E.
- **Not verified:** pricing (the fetched page was truncated). The quota page lists decapsulate only under token-based quotas after 16 February 2026 (https://docs.cloud.google.com/kms/quotas).

**Azure Key Vault and Managed HSM: usable for option F only.** EC keys support Sign/Verify only. The only asymmetric decryption is RSA (OAEP). No ECDH or KEM operation is listed (https://learn.microsoft.com/en-us/azure/key-vault/keys/about-keys-details, https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/about-keys-details). Option E would need RSA-OAEP, which is not an RFC 9180 KEM.

**Self-run HSM (PKCS#11).** PKCS#11 3.1 defines `CKM_ECDH1_DERIVE`, Montgomery-curve key generation (`CKM_EC_MONTGOMERY_KEY_PAIR_GEN`), and `CKD_NULL`, which "produces a raw shared secret value without applying any key derivation function" (https://docs.oasis-open.org/pkcs11/pkcs11-spec/v3.1/os/pkcs11-spec-v3.1-os.html). A PKCS#11-backed `ecdh.KeyExchanger` is therefore possible in principle.

- **Extractable output:** the derived secret is created as a key object and must be extractable so that HPKE's key schedule can use it.
- **Not checked:**
  - which HSMs support X25519;
  - which Go PKCS#11 binding to use; the standard library has none;
  - HSM throughput.

**HashiCorp Vault / OpenBao transit.** Not checked.

## Failure modes common to all forward-secret options

- **Clients need a retry path.** Every option that can lose or replace keys needs a defined refusal for an unknown or expired key, and a client that refetches and retries once, as RFC 9458 section 6.4 does with 422. Gate 11 already requires refusing expired epochs. It does not yet test the client retry.
- **Forward secrecy is only as good as the longest-lived copy.** Logged tables, `pg_dump`, volume snapshots, KMS waiting periods, residual cloud copies, swap and core dumps all extend the horizon. "The Hikyo root key opens nothing" holds only for options B, C, E and F, and for B only under the backup conditions stated above.
- **Out of scope.** None of the options changes what decision 12 already excludes: a modified server, a memory dump during the window, replay within 30 seconds, and TLS-terminating components that replace the public key.

## Unverified or not checked

- The operator's PostgreSQL backup method for the hosted service (physical/PITR, `pg_dump`, or volume snapshots). This decides whether B's horizon holds.
- Whether storage or volume snapshots include `UNLOGGED` table files.
- Whether AWS `DeriveSharedSecret` output encoding matches RFC 9180 `DH()`, and the latency of AWS ECDH and decapsulate calls.
- AWS per-request prices for DeriveSharedSecret and Decapsulate; GCP KMS pricing.
- AWS residual retention after deletion; whether keys pending deletion count against the AWS key quota.
- Vault/OpenBao transit capabilities; PKCS#11 HSM X25519 support; Go PKCS#11 bindings.
- Browser WebCrypto X25519 support; HPKE stacks other than Go and hpke-js.
- Details of puncturable encryption beyond the paper abstracts.
- Whether any established HPKE library accepts a whole-KEM opaque X-Wing decapsulator. I checked only Go `crypto/hpke` and circl, and neither does.

## Sources

- RFC 9180, HPKE: https://www.rfc-editor.org/rfc/rfc9180.html (sections 7.1.3, 9.1, 9.7.3, 9.7.4)
- RFC 9458, Oblivious HTTP: https://www.rfc-editor.org/rfc/rfc9458.html (sections 6.4, 6.6, 6.7)
- draft-ietf-hpke-pq: https://datatracker.ietf.org/doc/draft-ietf-hpke-pq/
- Go 1.26 release notes: https://go.dev/doc/go1.26
- Go `crypto/hpke`: https://pkg.go.dev/crypto/hpke; source https://github.com/golang/go/tree/go1.27.3/src/crypto/hpke
- Go `crypto/ecdh.KeyExchanger`: https://pkg.go.dev/crypto/ecdh#KeyExchanger
- Go `runtime/secret` (experimental): https://github.com/golang/go/tree/go1.27.3/src/runtime/secret
- circl HPKE: https://pkg.go.dev/github.com/cloudflare/circl/hpke; circl KEM: https://pkg.go.dev/github.com/cloudflare/circl/kem; source https://github.com/cloudflare/circl/tree/v1.6.4/hpke
- filippo.io/hpke: https://pkg.go.dev/filippo.io/hpke
- hpke-js: https://github.com/dajiaji/hpke-js
- AWS KMS DeriveSharedSecret: https://docs.aws.amazon.com/kms/latest/APIReference/API_DeriveSharedSecret.html
- AWS KMS Decapsulate: https://docs.aws.amazon.com/kms/latest/APIReference/API_Decapsulate.html
- AWS KMS key specs: https://docs.aws.amazon.com/kms/latest/developerguide/symm-asymm-choose-key-spec.html
- AWS KMS key deletion: https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html
- AWS KMS request quotas: https://docs.aws.amazon.com/kms/latest/developerguide/requests-per-second.html
- AWS KMS pricing: https://aws.amazon.com/kms/pricing/
- Google Cloud KMS KEMs: https://docs.cloud.google.com/kms/docs/key-encapsulation-mechanisms
- Google Cloud KMS algorithms: https://docs.cloud.google.com/kms/docs/algorithms
- Google Cloud KMS CryptoKey reference: https://docs.cloud.google.com/kms/docs/reference/rest/v1/projects.locations.keyRings.cryptoKeys
- Google Cloud KMS key states: https://docs.cloud.google.com/kms/docs/key-states
- Google Cloud KMS destroy and restore: https://docs.cloud.google.com/kms/docs/destroy-restore
- Google Cloud KMS quotas: https://docs.cloud.google.com/kms/quotas
- Azure Key Vault keys: https://learn.microsoft.com/en-us/azure/key-vault/keys/about-keys-details
- Azure Managed HSM keys: https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/about-keys-details
- PKCS#11 3.1: https://docs.oasis-open.org/pkcs11/pkcs11-spec/v3.1/os/pkcs11-spec-v3.1-os.html
- PostgreSQL routine vacuuming: https://www.postgresql.org/docs/current/routine-vacuuming.html
- PostgreSQL continuous archiving: https://www.postgresql.org/docs/current/continuous-archiving.html
- PostgreSQL CREATE TABLE (UNLOGGED): https://www.postgresql.org/docs/current/sql-createtable.html
- PostgreSQL replication protocol (BASE_BACKUP exclusions): https://www.postgresql.org/docs/current/protocol-replication.html
- PostgreSQL pg_dump: https://www.postgresql.org/docs/current/app-pgdump.html
- Bellare and Yee, Forward-Security in Private-Key Cryptography: https://eprint.iacr.org/2001/035
- Canetti, Halevi and Katz, A Forward-Secure Public-Key Encryption Scheme: https://eprint.iacr.org/2003/083
- Green and Miers, Forward Secure Asynchronous Messaging from Puncturable Encryption: https://ieeexplore.ieee.org/document/7163033
- Günther, Hale, Jager and Lauer, 0-RTT Key Exchange with Full Forward Secrecy: https://eprint.iacr.org/2017/223
- Derler, Gellert, Jager, Slamanig and Striecks, Bloom Filter Encryption: https://eprint.iacr.org/2018/199
