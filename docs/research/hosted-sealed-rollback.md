# Research: preventing rollback of sealed slot and signed org policy state

Date: 2026-11-12. Ticket: [Hikyo-Org/Hikyo#1342](https://github.com/Hikyo-Org/Hikyo/issues/1342). Status: research only. This note lists options, what each costs, what each needs from clients and what stays undetectable. It makes no decision.

Sibling notes: `hosted-client-side-and-confidential-computing.md` (ticket #1307, branch `research/hosted-client-side-and-confidential-computing`) found that rollback "stays undetectable" without an external counter or a transparency log. The separate ticket "Research: provider-agnostic TEEs" picks TEE families and availability. This note reports only each family's rollback and freshness primitives.

## The question

The hosted plan for Sealed orgs is an end-to-end attested unsealer (C3). Values are decrypted only inside it, and responses are sealed to the client's ephemeral key. The unsealer has no persistent storage. On every request it is handed state by the datastore: holder slots, the one signed org policy (R6) and ciphertext. A signature or an AEAD tag proves that a record is authentic. It does not prove that the record is the latest one.

So a datastore writer can hand the unsealer an older, authentic state: a policy that still lists a removed member or an old adapter destination, a slot for a removed holder, or an older snapshot. Owner requirement 5 assumes the hoster controls datastore, infrastructure and cloud account, with supply-chain compromise and insider access. A counter or ledger the operator runs therefore helps only if its integrity is rooted outside the operator.

The note compares four families of defence and asks, for each, about cost, client requirements, self-host non-TEE mode and residual blind spots.

## Short answer

- **No TEE family offers a counter that a stateless, multi-node, multi-region unsealer can use.**
  - SEV-SNP's firmware ABI has per-session message sequence numbers and sealing keys, but no guest-usable persistent counter was found.
  - TDX offers platform-bound sealing keys that are "not migratable". No counter was found.
  - The Nitro enclave NSM API has seven requests, and none of them is a counter or storage.
  - The COCONUT SVSM vTPM is rebuilt on every boot and keeps no state today. Its planned persistent state would sit on the host, and its developers list replay of an old state file as an open threat. Azure (VMGS disk) and NitroTPM keep vTPM state on the platform, per VM or per instance.
  - Every local counter is tied to one machine. None survives an HA, multi-region or rolling-upgrade deployment.
- **Distributed TEE counters (ROTE, Narrator, Nimble, CCF) can be rooted outside the operator's control of storage**, because their state lives in attested TEE memory that the host cannot rewind.
  - The operator can still kill every replica, which costs liveness and not safety. It can also start a fresh instance, which is a rollback to genesis unless the instance's identity is pinned where the operator cannot change it.
  - Nimble is the closest fit. It runs over untrusted crash-tolerant storage and lets the operator replace nodes without affecting safety. Each read is a quorum round trip, with reported latencies between 3.2 ms and 30 ms. It is research code, and changing the endorsers' own code needs customers to audit and accept the change.
- **A transparency log of policy and per-environment state, cosigned by independent witnesses (C2SP tlog-witness), roots append-only history and split-view resistance outside the operator.**
  - It bounds staleness only as tightly as the cosignature age the verifier accepts. Witnesses "SHOULD NOT delay for more than an hour", and enforcing any age bound needs a trusted clock.
  - Logs prove inclusion, not latest-ness. "Latest policy for org X" needs a per-org log, a verifiable map (AKD, CONIKS) or an equivalent.
  - Hikyo already verifies Rekor inclusion and pinned checkpoints for nightly releases, but no consistency-proof or witness check was found in the repository.
- **Client-held floors (version vectors, fork consistency) protect an honest client from being served stale data.** Hikyo already uses this pattern for release metadata.
  - They do nothing when the requester is the attacker. A removed member colluding with a datastore writer has no reason to present a floor.
  - They also fail at first contact: ephemeral CI runners, new pods and browsers with cleared storage.
- **Org-signed freshness tokens with expiry (the TUF timestamp-role pattern)** bound rollback to the token lifetime. They need an online org signer and trusted time in the unsealer.
- **R6 reduces slot rollback to policy freshness only under two conditions.** The policy must bind each holder's public key, and the unsealer must check it. Freshness of the current key version and the current snapshot pointer is still needed separately.
  - The repository prompts rotation on member removal but does not force it.
  - Without rotation, a removed member regaining access to new values depends on policy freshness, and on whether `sealedSlotKey` ever leaves the unsealer.
- **A legitimate restore is indistinguishable from a rollback** unless it is recorded as a forward entry signed under the org's recovery-key chain. Every option needs that re-anchor path.

## What the repository says today

Read at `origin/dev`, commit `c6e2accab`. No Sealed code exists yet.

- **Slots and binding.** Decision 3, property 3: "The outer layer's AAD and the inner wrap each bind org, project, environment, key version, slot and holder" (`docs/adr/hosted-service.md:74`). Rotation adds a new key version and later retires the old one, "which deletes those slots" (`:88`).
- **Value AAD binds location, not version.**
  - `snapshotAAD` binds org, project, environment, key, snapshot and row (`internal/service/publish.go:1585-1595`).
  - `pendingAAD` drops the snapshot (`:1597-1603`).
  - `valueAAD` binds org, project, environment, key and row (`internal/service/values.go:310-319`).
  - An older snapshot therefore stays authentic. Which snapshot is current is a datastore row that AEAD does not cover.
- **Removal and rotation.**
  - "Removing a membership, including through SCIM, always proceeds" (`hosted-service.md:136`).
  - "Removing a member does not un-know a key. The product prompts the org to rotate the environment key and the secrets themselves" (`:156`). Rotation is prompted, not forced.
  - "An org-level restore also brings back removed members' slots and retired key versions, so it must prompt rotation" (`:160`).
  - Removing unattended access "does not seal until rotation" (`:198`).
- **Honest statement.** Sealed does not protect against "Direct write access to the live datastore. It can forge a mode, replace a holder's public key ..., or roll slots back to an earlier state" (`:251`).
- **Signed holder lists were rejected.** The ADR rejected them because they need "delegated signing, request-level binding to a verified list and a design for clients that cannot verify". It added that they only make sense "alongside client-side decryption or attested hardware" (`:269`). C3 and R6 are that case.
- **Stateless nodes (decision 4).** "Nothing is cached across requests, shared between nodes or written to the datastore, so nodes stay stateless" (`:94`). This is the repository's own reason an unsealer would not remember the latest state.
- **Envelope (decision 12).** The transit envelope is bound to its request and has a 30-second window (`:229-238`). It is about replay of a request, not freshness of stored state.
- **Transactions.** "Publish-class writes run SERIALIZABLE on postgres and BEGIN IMMEDIATE on sqlite; the retried unit is the whole closure, and no external effect ... may escape before commit" (`internal/store/tx/tx.go:2-5`). A ledger append or witness call is such an external effect, so it must follow commit.
- **Advisory lock 84.**
  - `pg_advisory_xact_lock(1464159830, 84)` is the installation-global serialization point for audit `commit_seq` (`internal/store/migrations/postgres/00011_audit_export_order.sql:6-10,92,111`).
  - The audit ADR calls the hash-chain hook "schema headroom, not an ordering guarantee" (`docs/adr/audit-model.md:138`), and says "No hash chain and no external sink ship in v1" (`:170`). The threat model records audit as "NOT tamper-proof against the instance operator" (`docs/adr/threat-model.md:72`).
  - A future chain could reuse that serialization point. Without an anchor outside the operator, it gives nothing against requirement 5.
- **Release trust already uses client floors and Rekor.**
  - "Metadata and catalog sequence floors detect rollback and same-height equivocation" and new metadata "cannot lower locally remembered floors" (`docs/adr/stable-workflow-signing.md:50-52`).
  - `internal/releasetrust/verification_floor.go` persists floors. `internal/releasetrust/pq_verify.go:72-86,148-174` refuses lower generations and sequences with `ErrPQRollback`, and refuses equal ones with different bytes as equivocation.
  - `internal/releasetrust/nightly.go:281-299` requires exactly one Rekor entry with a signed entry timestamp, an inclusion proof and a checkpoint whose log ID and origin match pinned values.
  - A search of `internal/releasetrust` found no consistency-proof or witness-cosignature check. Reuse is therefore limited to the inclusion-proof and checkpoint parsing code, through `sigstore-go` and `transparency-dev/merkle`.

## Threat split

| Case | What is re-served | Effect | Detectable by signature or AEAD alone? |
|---|---|---|---|
| (a) Policy rollback | An older R6 policy, validly signed under the org chain | Unsealer honours a removed holder, an old adapter destination, an old issuer binding or old unattended consumers | No. Old and new are both validly signed |
| (b) Slot rollback | A wrapped slot for a holder since removed, or for a retired key version | Unsealer opens the environment key for that holder | No. The slot's AAD binds holder and key version, not "still current" |
| (c) Value rollback | An older snapshot, older pending rows, or an older "current snapshot" pointer | Workloads receive a revoked or rotated secret; an old snapshot is published again | No. AAD binds location, not version (see above) |
| (d) Removed member access | Any of the above, to a removed member | Old values: already known, cannot be revoked by any option. New values: the real risk | Depends on (a), (b), rotation and write-path freshness |

**Does R6 reduce (b) to (a)?** Partly, under two conditions.

1. The unsealer opens a slot only if the slot's holder public key and key version appear in the latest policy.
2. The slot AAD binds holder and key version, which decision 3 already requires.

Under those conditions, a re-served slot for a removed holder is refused whenever the unsealer holds the latest policy. Slot freshness then equals policy freshness.

That covers holders. It does not cover:

- **The current key version and the current snapshot pointer for each environment.** These change on every rotation and every publish, so they are not in an org-wide policy unless the policy, or a per-environment state root, carries them.
- **A write-path variant of (d).** A writer who rolls back an environment's current key version can make the unsealer seal new values under the old key. A removed holder whose old-version slot is still in a retained backup can then read those new values. This survives rotation unless the write path also checks key-version freshness.

**Dependence on rotation.** If removal rotates the key and the unsealer knows the current key version, rollback of policy and slot yields only the old key, and so only old values. If removal does not rotate, the environment key is unchanged. A removed member's access to new values then depends on two things:

- Policy freshness.
- Whether the slot's outer layer, `sealedSlotKey`, ever leaves the unsealer. Under decision 3 it is a tier-3 server key wrapped by the master key (`hosted-service.md:73`). Where it lives under C3 is not decided.

The repository prompts rotation rather than forcing it (`:156`).

**Cross-cutting requirements, whichever option is chosen.**

- **Restore must be a forward step.** An org-level restore brings back removed slots and retired key versions (`:160`), which is exactly what a rollback looks like. Each option needs restore recorded as a new, higher entry, signed under the org's recovery-key chain. Otherwise it either flags the restore as an attack or lets an attack pass as a restore. Nimble requires the customer to agree explicitly to a recovery snapshot after a total disaster ([Nimble §9.1](https://www.usenix.org/system/files/osdi23-angel.pdf)). In CCF, members vote and submit recovery shares, and the service identity changes ([CCF recovery](https://ccf.dev/main/operations/recovery.html)).
- **Pin the anchor's identity.** A fresh counter, ledger or log is a rollback to genesis. Its identity must be pinned somewhere the operator cannot change: the signed policy chain, the unsealer measurement or the client configuration. CCF recovery produces a new service certificate that "must be distributed to all existing and new users" ([CCF recovery](https://ccf.dev/main/operations/recovery.html)). If Hikyo is the consortium, member approval is operator approval.
- **R6 allows many signers.** Any single admin may sign a change, so two admins can sign competing next versions. The anchor must order them, and the second must rebase. Transparency makes a hostile signer visible but does not prevent the signing.
- **Trusted time.** Expiring tokens and cosignature age bounds need a clock the host cannot move. Nonce-based reads, as in Nimble, avoid this. No source on trusted time per TEE family was fetched (open item 6).
- **Crash ordering.** Under `tx.go`, the datastore commit comes first and the anchor append second. Nimble's update protocol stores `(S', c+1, σ)` first, so that storage "holds a counter that is at most one higher than the counter stored on the ledger". After a crash, the application "uses S′, c + 1, and σ from the storage service to complete its pending append" ([Nimble §3](https://www.usenix.org/system/files/osdi23-angel.pdf)). For Hikyo, σ would be the org signature for policy changes and an unsealer signature for environment state. The reverse order leaves the anchor ahead of the datastore. That is safe, but the environment stays stuck until the state is recovered.

## Option 1: TEE-local and platform counters

**AMD SEV-SNP.**

- The firmware ABI (Publication 56860, revision 1.58, May 2025) gives each guest four VMPCKs. "Each message contains a sequence number per VMPCK", and messages out of order are rejected. This is an in-session channel counter for guest-firmware messages.
- `MSG_KEY_REQ` derives keys the guest may use "for any purpose it chooses such as sealing keys" ([AMD 56860](https://www.amd.com/content/dam/amd/en/documents/developer/56860.pdf)). The spec's rollback language concerns firmware versions and SVNs (`BAD_VERSION`, "Firmware version anti-rollback").
- A text search of the spec found no guest-usable persistent monotonic counter. Sealing gives integrity and confidentiality, not freshness: the host stores the sealed blob and can replay it.

**SEV-SNP with an SVSM vTPM (COCONUT).**

- The LPC 2024 talk by the SVSM vTPM developers states: "SVSM vTPM state is not loaded/persisted" and "SVSM vTPM is manufactured on every boot".
- Planned persistent state lives on the untrusted host. The talk lists "rollback: reuse an old SVSM state" as an attack and notes "TPM monotonic counters could be unreliable". Its proposed mitigation is a "boot counter released by remote attestation server" ([LPC 2024 slides](https://lpc.events/event/18/contributions/1917/attachments/1596/3421/2024-lpc-vtpm-v2.pdf)). That moves the counter to an external service.
- The CocoonFs format keeps an update counter "for supporting the design of rollback protection protocols". It leaves enforcement to a remote party that would "track the minimum update counter ever reported to it and prohibit going back to earlier values" ([CocoonFs spec](https://coconut-svsm.github.io/cocoon-tpm/cocoonfs/cocoonfs-format.html)).
- A vTPM NV counter is therefore only as fresh as the state file the host supplies.

**Intel TDX.**

- `TDG.MR.KEY.GET` lets a TD "request a persistent sealing key". "TDX sealing keys are bound to the current platform and are not migratable" (TDX Module Base Architecture Specification 348549-008US, §12.8).
- This was read from a third-party mirror, because Intel's own URL returned 403 (open item 2). No counter was found in a text search.
- Platform-bound keys also conflict with rescheduling an unsealer onto another host.
- No TDX-specific vTPM source was fetched. Cloud TDX vTPMs fall under the Azure and Google entries below (open item 3).

**AWS Nitro Enclaves and Nitro Sandboxes.**

- Enclaves "have no persistent storage, interactive access, or external networking" ([What is Nitro Enclaves](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave.html)).
- The NSM API `Request` enum has seven variants: `DescribePCR`, `ExtendPCR`, `LockPCR`, `LockPCRs`, `DescribeNSM`, `Attestation` and `GetRandom` ([NSM API source](https://raw.githubusercontent.com/aws/aws-nitro-enclaves-nsm-api/main/src/api/mod.rs)). None is a counter or a storage operation, which closes the #1307 note's gap.
- Nitro Sandboxes can use the parent instance's block devices, and "The parent instance can read every block device that it presents to a Nitro Sandbox". They "do not support snapshot and restore" ([Nitro Sandboxes](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nitro-sandboxes.html)).
- NitroTPM is a TPM 2.0 device for whole EC2 instances ([NitroTPM](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nitrotpm.html)). Its state "is not included in Amazon EBS snapshots", and BitLocker volumes sealed with it "can only be used on the original instance" ([NitroTPM requirements](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/enable-nitrotpm-prerequisites.html)).
- So a NitroTPM NV counter would be bound to one instance. The operator cannot decrement it, but can terminate the instance or start another with a fresh TPM. The pages read do not say whether NitroTPM reaches enclaves or sandboxes (open item 4).

**Azure confidential VMs.**

- The vTPM "runs in a secure environment outside the reach of any VM". Its state sits in a "small encrypted virtual machine guest state (VMGS) disk", encrypted with a platform-managed or customer-managed key ([Azure CVM overview](https://learn.microsoft.com/en-us/azure/confidential-computing/confidential-vm-overview)).
- The `NonPersistedTPM` type, available only in the TDX preview, keeps a vTPM whose state "isn't persisted through reboots" ([Azure CVM FAQ](https://learn.microsoft.com/en-us/azure/confidential-computing/confidential-vm-faq)).
- Whether the host or the account owner can substitute an older VMGS disk is not documented on the pages read (open item 3).

**Google Cloud.** Confidential VMs use the Shielded VM vTPM's NV storage "to store attestation key certificates and templates" ([vTPM NV storage](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/vtpm-nv-storage)). Persistence and rollback behaviour are not stated (open item 3).

**General limit.** The Nimble authors list why TEE counters do not suit cloud services:

- Increment latencies are 80 to 250 ms, and the counters "wear out in a few days".
- "removing the BIOS battery or reinstalling TEE software often resets these counters".
- "monotonic counters are specific to a given machine so a crashed application cannot be launched on a different machine", which the authors call "unacceptable in cloud settings" ([Nimble §2](https://www.usenix.org/system/files/osdi23-angel.pdf)).

## Option 2: distributed TEE counters and ledgers

- **ROTE.** "A single platform cannot efficiently prevent rollback, but ... multiple processors can be enrolled to assist each other". The result is that "the only way to violate integrity is to reset all participating platforms to their initial state" ([ROTE, USENIX Security 2017](https://www.usenix.org/conference/usenixsecurity17/technical-sessions/presentation/matetic)).
- **LCM.** It "enables the detection of rollback attacks", "enforces the consistency notion of fork-linearizability" and "maintains only small, constant storage at the clients" ([LCM, arXiv 1701.00981](https://arxiv.org/abs/1701.00981)).
- **Narrator.** It "uses the blockchain to initialize a distributed system of TEEs". The authors report "about 6k state updates per second" at "3−8ms" latency with 5 nodes on a LAN, from an SGX proof of concept ([Narrator, CCS 2022, doi:10.1145/3548606.3560639](https://pw0rld.github.io/files/Narrator_ccs_22.pdf)).
- **Nimble (Microsoft Research, OSDI 2023).**
  - Design: "a simple state machine running in a TEE in conjunction with a crash fault-tolerant storage service", replicated so it stays available "even if a minority of state machines crash". Its reconfiguration protocol lets "a cloud provider ... replace the set of nodes ... without affecting safety".
  - Platforms and figures: the trusted state machine runs on SGX and SEV-SNP. Reported results range from "tens of thousands of requests/sec with an end-to-end latency of under 3.2 ms" (in-memory store) to "several thousands of requests/sec with a latency of 30ms" (Azure Table) ([Nimble abstract](https://www.usenix.org/conference/osdi23/presentation/angel)).
  - Losing a majority: it "must either give up safety or liveness" (§3). Nimble keeps safety, because "endorsers cannot be restarted" (§9.1).
  - Total disaster: recovery has "no playbook". The customer can agree to a snapshot it trusts, and "a provider cannot do this unilaterally" (§9.1).
  - Changing endorser code: "Customers must audit code changes and verify attestation reports, and decide whether to accept the latest set of endorsers" (§9.2) ([paper](https://www.usenix.org/system/files/osdi23-angel.pdf)).
  - The repository says Nimble "can also be used as a generic tamper-proof fault-tolerant append-only ledger" ([microsoft/Nimble](https://github.com/microsoft/Nimble)).
- **CCF and Azure Confidential Ledger.**
  - CCF is open source. Nodes run in TEEs "such as AMD SEV-SNP", and "A CCF network is governed by a consortium of Members". Ledger integrity "is guaranteed by a Merkle Tree" whose root the primary signs ([What is CCF](https://ccf.dev/main/overview/what_is_ccf.html)).
  - Recovery is started by operators and approved by members, who vote and submit recovery shares. Operators choose which ledger to recover from, timeouts "can lead to multiple forks of the service", and "the identity of the network has changed" ([CCF recovery](https://ccf.dev/main/operations/recovery.html)).
  - Azure Confidential Ledger is a CCF-based Azure service. "One ledger spans three or more identical instances". The connection "terminates inside ... Intel SGX enclaves", data is "stored in Azure-backed file storage", and data is replicated to Azure regional pairs ([ACL overview](https://learn.microsoft.com/en-us/azure/confidential-ledger/overview)). As a service it is Azure-only.

**Applied to Hikyo.**

- The anchor would hold, per org, the policy sequence and hash. Per environment it would hold a state root: current key version, holder-set hash and current snapshot digest.
- On each read, the unsealer sends a fresh nonce and receives a quorum-signed tail. It then checks that the state handed to it by the datastore matches.
- If Hikyo runs every endorser, safety rests on the TEE vendor and the endorser measurement, the same root as C3 itself. The operator can still deny service, or start a new ledger, which pinning closes.
- A variant is to run the ROTE-style counter inside the unsealer fleet itself. Then "reset all participating platforms" is something the hoster can do at will, for example during a full outage or an all-at-once upgrade. After it, the fleet must re-anchor from the org chain or a witnessed log.

## Option 3: transparency log with independent witnesses

**Primitives.**

- A checkpoint is a signed note with origin, tree size and root hash. "A log or cosigner MUST NOT sign any checkpoint which is inconsistent with any checkpoint it previously signed" ([tlog-checkpoint](https://github.com/C2SP/C2SP/blob/main/tlog-checkpoint.md)).
- A witness verifies an RFC 6962 consistency proof from the last checkpoint it cosigned. It "is only required to keep track of the latest checkpoint it observed and verified", must "persist the new checkpoint before responding", and "SHOULD NOT delay for more than an hour" ([tlog-witness](https://github.com/C2SP/C2SP/blob/main/tlog-witness.md)).
- "Log clients can verify a quorum of cosignatures to prevent split-view attacks". Checkpoint cosignatures carry a timestamp ([tlog-cosignature](https://github.com/C2SP/C2SP/blob/main/tlog-cosignature.md)).
- Sigsum builds on these C2SP specifications and lists "offline verify" among its tools ([Sigsum docs](https://www.sigsum.org/docs/)).
- Tessera is a Go library for tile-based logs with GCP, AWS and POSIX drivers. It collects witness cosignatures before publishing, and "If the policy cannot be satisfied then no checkpoint will be published" ([Tessera](https://github.com/transparency-dev/tessera)).
- The Witness Network brings logs and witnesses together. A maintainer must approve each log, and the network is labelled "experimental / work-in-progress" ([witness-network.org](https://witness-network.org/)).

**Client model (Go checksum database).**

- The `go` command caches the latest signed tree. If it sees inconsistent tree heads, it will "complain loudly on standard error and fail the build".
- A forked log must be served to its victim "forever". Gossip is sketched but not implemented, and the design says little about clients with no prior state ([sumdb design](https://go.googlesource.com/proposal/+/master/design/25530-sumdb.md)).

**A log is not a map.** An inclusion proof shows that policy version N was logged. It does not show that no N+1 exists. Three ways to answer "latest for org X":

1. A log per org or per environment, whose newest leaf is the current state.
2. A verifiable map with epochs. WhatsApp's AKD batches changes into epochs and publishes audit proofs "for anyone interested" ([WhatsApp key transparency](https://engineering.fb.com/2023/04/13/security/whatsapp-key-transparency/), [AKD](https://github.com/facebook/akd)). In CONIKS, users "monitor their own key bindings" and "collectively audit providers for non-equivocation" ([CONIKS](https://www.usenix.org/conference/usenixsecurity15/technical-sessions/presentation/melara)).
3. One global log whose verifier reads every entry after the one it trusts.

**Rekor reuse.**

- Hikyo's nightly verifier already parses Rekor checkpoints and inclusion proofs (see above).
- Public Rekor v1 is "in maintenance mode", v2 moves to Tessera, and uploads are limited to 100KB ([Rekor](https://github.com/sigstore/rekor)).
- Logging per-org state in a public log would expose the number of orgs and how often they change, unless entries are blinded.
- A global log needs the map construction above to prove latest-ness. The Rekor page read does not mention witnessing.

**Applied to Hikyo.**

- Each policy change and each environment state change appends a leaf, after the datastore commit.
- The unsealer refuses state that is not the newest leaf under a checkpoint cosigned by k of n pinned witnesses within an age bound.
- Writes wait for cosignatures. Reads need no witness round trip if the server supplies the cosigned checkpoint, but rollback inside the age bound stays possible.

## Option 4: client-held version vectors and fork consistency

**Mechanism.**

- Each client remembers, per org, the policy sequence and hash. Per environment it remembers the state sequence and root.
- It sends them with each request. The unsealer refuses state older than the client's floor, and treats the same sequence with a different hash as equivocation.
- This is Hikyo's release-floor pattern applied to org state.

**Theory.** SUNDR's fork consistency "guarantees that clients can detect any integrity or consistency failures as long as they see each other's file modifications" ([SUNDR](https://www.usenix.org/conference/osdi-04/secure-untrusted-data-repository-sundr)). LCM gives the same detection with constant client storage (above).

**What it protects.** An honest client's own reads: case (c), and (a) when the reader is honest.

**What it does not protect.**

- It does not help when the requester is the attacker. In case (d), the removed member's client decides what floor to present.
- A client that has never seen newer state has no floor. Examples: a new GitHub Actions runner, a new Kubernetes pod without a persisted floor, a browser with cleared storage, and every first login.
- A fork that keeps one client on an old branch is detected only if that client later sees the other branch.

**Variant.** Writers, the admins who sign R6 changes, always see their own writes. They could push their floor to every other client through the witnessed log, which is gossip by another name.

## Option 5: org-signed freshness tokens

- TUF's timestamp role exists "to prevent an adversary from replaying an out-of-date signed metadata file whose signature has not yet expired" and "is frequently re-signed".
- Clients "MUST NOT replace a metadata file with a version number less than the one currently trusted" and "MUST NOT trust an expired file" ([TUF specification](https://theupdateframework.github.io/specification/latest/)).
- For Hikyo, an org key would re-sign "policy N and state roots are current until T" at a fixed interval. The unsealer would refuse tokens that are expired or older than the latest it knows.
- Rollback is bounded to T.
- The signer must be online and held by the org: either a person, or org-run automation, which is open item 2 of the hosted ADR. Inside Hikyo infrastructure it would be operator-controlled.
- When the signer is offline, every read fails at expiry, including unattended sync.
- The unsealer also needs trusted time.

## Comparison

| Option | Who can roll it back | Cost (infra, engineering) | Needs from clients | Per-read cost and availability |
|---|---|---|---|---|
| 1 Local TEE or vTPM counter | Host or platform that keeps vTPM state (SVSM, VMGS, NitroTPM). SNP, TDX and enclave NSM have no counter | No infra. High engineering, and it does not fit HA | Nothing | Local call. Nimble cites 80 to 250 ms per increment. Lost with the machine |
| 2 Distributed TEE counter (Nimble, ROTE, Narrator, CCF) | Nobody without breaking the TEE or a quorum. The operator can kill a quorum (liveness) or start a fresh instance (needs pinning) | Endorser TEEs across fault domains. High engineering: research code, reconfiguration, re-anchor | Nothing beyond verifying the unsealer | One quorum round trip per read. Reported 3.2 to 30 ms. Unavailable without a majority |
| 3 Witnessed transparency log | The log operator cannot rewind witnessed history. Witnesses colluding past the quorum can | Log (Tessera) plus k independent witnesses. Medium to high engineering: map or per-org log, witness relationships | Optional. Clients that verify checkpoints strengthen it | No witness call per read. Staleness up to the accepted cosignature age. Writes stall if the witness quorum is unavailable |
| 4 Client-held floors | Anyone, for a client without a floor | Low infra. Medium engineering (pattern exists in `releasetrust`) | Persistent state per org and environment | None extra. A fork stays undetected until clients compare |
| 5 Org-signed freshness token | Any datastore writer, by replaying an older token until it expires. The org signer, at any time | Low infra. Low to medium engineering. The org must run a signer | Nothing | None extra. Every read fails when tokens expire |

| Option | HA, multi-region, restart, rolling upgrade | Crash between counter and commit | Self-host non-TEE mode | Undetectable residue | First contact and forks (unattended, browsers) |
|---|---|---|---|---|---|
| 1 | Fails. Per machine, platform-bound (TDX not migratable) | Counter ahead of state: environment stuck | No TEE, no counter. A TPM counter is operator-controlled | Replay of host-held state. Any reschedule | Covered for all clients, but only on one machine |
| 2 | Designed for it. Nimble replaces nodes without losing safety. Code upgrades need customers to accept the new endorsers (§9.2) | Store `(S', c+1, σ)`, then append, then complete on restart (Nimble §3) | Can run without TEEs, but then it is operator-trusted | Rollback within a replaced or unpinned instance. Total disaster without org-signed re-anchor | Covered: the unsealer checks, not the client |
| 3 | Good. A log is region-independent, with one writer per log | Commit, then append. A logged but uncommitted leaf leaves state stuck until repaired | Works unchanged if witnesses accept self-hosters' logs (not checked) | Rollback within the age bound. Witness collusion. A per-org log not pinned | Covered when the unsealer requires cosigned latest-ness. Weak if only clients check |
| 4 | Fine. State lives at the clients | The client advances its floor only after a confirmed write | Works, and detects restores and bugs. The operator is trusted anyway | Attacker as requester. Any new client. Undetected forks | Not covered: ephemeral runners, new pods, browsers |
| 5 | Fine. Only a token check | Not applicable | Works | Rollback within T. A compromised org signer | Covered within T, including browsers |

## Which combinations close which cases

This section states coverage only. It does not choose.

- **Policy rollback (a) and slot rollback (b) with the attacker as requester** close only when the unsealer itself learns freshness. That means option 2, option 3 with the unsealer requiring cosigned latest-ness, or option 5. Option 4 alone does not close it.
- **Value rollback (c) for honest readers** closes with option 4 for clients that keep state. It closes for every client, including unattended ones, only if per-environment state roots go to the same anchor as the policy.
- **New-value access by a removed member (d)** needs three things together:
  - (a) closed.
  - Write-path freshness of the current key version.
  - Either forced rotation on removal, or `sealedSlotKey` held only inside the unsealer.
- **Restore** needs an org-signed forward entry in whichever anchor is used.
- **Self-host non-TEE mode** gains detection of accidental rollback from options 3 to 5. It gains nothing against its own operator, which matches the stated trust model ("your admin can read values").

## Unverified and open items

1. No source on whether NitroTPM is available inside Nitro Enclaves or Nitro Sandboxes, or on which NV index types it supports.
2. The Intel TDX module specification was read from a third-party mirror (348549-008US). Intel's own copy (348549-007US) returned 403. The TDX statements here need confirming against an Intel-hosted copy.
3. Whether the Azure host or the account owner can substitute an older VMGS disk, and whether Google's vTPM NV state persists or can be rolled back. Neither was stated on the pages read.
4. COCONUT SVSM persistent vTPM status after LPC 2024 is unknown. The project README does not mention it.
5. A text search of AMD 56860 rev. 1.58 found no guest-usable persistent counter. A full reading was not done.
6. Trusted time inside each TEE family, against a host that controls timers, was not researched. Options 3 and 5 depend on it.
7. Sigsum's trust-policy and proof formats are marked "work in progress". The Witness Network is "experimental". Neither has an available list of production witnesses, nor terms for witnessing many small per-org logs.
8. ROTE, LCM, SUNDR and CONIKS were read from abstracts only. Fault thresholds, cost and client protocols beyond the abstracts were not checked.
9. Nimble's production maturity and maintenance status were not checked. The repository presents it as OSDI 2023 artifact code.
10. Where `sealedSlotKey` lives under C3, and whether removal must force rotation, are owner decisions outside this note.
11. How a per-environment state root would be signed (by the unsealer, or by an org key) was not designed, nor how many leaves per second a hosted installation would append.

## Sources

Repository, `origin/dev` at commit `c6e2accab`: `docs/adr/hosted-service.md` (lines 62-90, 94, 136, 156, 160, 198, 229-238, 251, 269), `docs/adr/audit-model.md` (138, 170), `docs/adr/threat-model.md` (72), `docs/adr/stable-workflow-signing.md` (50-52, 92-98), `internal/service/publish.go` (1585-1603), `internal/service/values.go` (310-319), `internal/store/tx/tx.go` (1-5), `internal/store/migrations/postgres/00011_audit_export_order.sql` (1-15, 92, 111), `internal/releasetrust/nightly.go` (281-299), `internal/releasetrust/verification_floor.go`, `internal/releasetrust/pq_verify.go` (72-86, 148-174). Prior note: `docs/research/hosted-client-side-and-confidential-computing.md` on `origin/research/hosted-client-side-and-confidential-computing`.

External, all fetched on 2026-11-12:

- TEE families: [AMD SEV-SNP Firmware ABI 56860 rev. 1.58](https://www.amd.com/content/dam/amd/en/documents/developer/56860.pdf); [SVSM vTPM, LPC 2024 slides](https://lpc.events/event/18/contributions/1917/attachments/1596/3421/2024-lpc-vtpm-v2.pdf); [CocoonFs format specification](https://coconut-svsm.github.io/cocoon-tpm/cocoonfs/cocoonfs-format.html); [COCONUT SVSM repository](https://github.com/coconut-svsm/svsm); Intel TDX Module Base Architecture Specification 348549-008US (third-party mirror `kib.kiev.ua/x86docs/Intel/TDX/348549-008.pdf`; the [Intel copy](https://cdrdv2-public.intel.com/867568/intel-tdx-module-base-spec-348549007.pdf) returned 403); [What is Nitro Enclaves](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave.html); [NSM API source](https://raw.githubusercontent.com/aws/aws-nitro-enclaves-nsm-api/main/src/api/mod.rs); [Nitro Sandboxes](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nitro-sandboxes.html); [NitroTPM](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nitrotpm.html); [NitroTPM requirements](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/enable-nitrotpm-prerequisites.html); [Azure CVM overview](https://learn.microsoft.com/en-us/azure/confidential-computing/confidential-vm-overview); [Azure CVM FAQ](https://learn.microsoft.com/en-us/azure/confidential-computing/confidential-vm-faq); [Google Cloud vTPM NV storage](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/vtpm-nv-storage).
- Distributed counters and ledgers: [ROTE](https://www.usenix.org/conference/usenixsecurity17/technical-sessions/presentation/matetic); [LCM](https://arxiv.org/abs/1701.00981); [Narrator](https://pw0rld.github.io/files/Narrator_ccs_22.pdf); [Nimble abstract](https://www.usenix.org/conference/osdi23/presentation/angel); [Nimble paper](https://www.usenix.org/system/files/osdi23-angel.pdf); [microsoft/Nimble](https://github.com/microsoft/Nimble); [What is CCF](https://ccf.dev/main/overview/what_is_ccf.html); [CCF recovery](https://ccf.dev/main/operations/recovery.html); [Azure Confidential Ledger](https://learn.microsoft.com/en-us/azure/confidential-ledger/overview).
- Transparency: [C2SP tlog-witness](https://github.com/C2SP/C2SP/blob/main/tlog-witness.md); [C2SP tlog-cosignature](https://github.com/C2SP/C2SP/blob/main/tlog-cosignature.md); [C2SP tlog-checkpoint](https://github.com/C2SP/C2SP/blob/main/tlog-checkpoint.md); [Sigsum docs](https://www.sigsum.org/docs/); [Go checksum database design](https://go.googlesource.com/proposal/+/master/design/25530-sumdb.md); [Tessera](https://github.com/transparency-dev/tessera); [transparency-dev witness](https://github.com/transparency-dev/witness); [Witness Network](https://witness-network.org/); [Rekor](https://github.com/sigstore/rekor); [AKD](https://github.com/facebook/akd); [WhatsApp key transparency](https://engineering.fb.com/2023/04/13/security/whatsapp-key-transparency/); [CONIKS](https://www.usenix.org/conference/usenixsecurity15/technical-sessions/presentation/melara).
- Fork consistency and freshness: [SUNDR](https://www.usenix.org/conference/osdi-04/secure-untrusted-data-repository-sundr); [TUF specification](https://theupdateframework.github.io/specification/latest/).
