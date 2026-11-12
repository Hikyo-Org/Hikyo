# Research: client-side decryption and confidential computing for the hosted service

Date: 2026-11-12. Ticket: [Hikyo-Org/Hikyo#1307](https://github.com/Hikyo-Org/Hikyo/issues/1307). Status: research only. This note lists options, what each would take, which threats each removes and what risk remains. It makes no decision.

Sibling note: `hosted-transit-forward-secrecy.md` in this directory (ticket #1306) covers forward secrecy for decision 12. Option C below touches the same key.

## The question

The hosted-service ADR (`docs/adr/hosted-service.md`, locked 2026-11-06) chose server-side decryption during the request (decision 4). It deferred client-side decryption. Its rejected alternatives say: "Client-side decryption first. The strongest option, but it requires reimplementing publish, validation and diff in every client, and a browser still trusts JavaScript the operator serves." It also rejected "Confidential VMs as the baseline. They may later strengthen Sealed against a modified server, but are too costly as a foundation." The honest statement lists what Sealed does not protect: a modified server, capture from memory or at TLS termination, direct datastore writes, metadata, and environments with unattended access.

The owner asked for an investigation of both. This note covers:

1. What each would take for Hikyo.
2. Which of four threats each removes:
   - T1: a modified server.
   - T2: an insider with production access who does not ship a release, for example through memory dumps, `kubectl exec`, logging at TLS termination, or direct datastore writes.
   - T3: a hostile dependency in the server.
   - T4: a hostile dependency in the web bundle, or a modified web bundle.
3. Whether a staged path exists, such as a minimal attested unsealer.

## Short answer

- **Client-side decryption (option A)** removes T1, T2 and T3 for values, but only for clients that Hikyo does not serve live: the CLI and the Kubernetes operator. It does not remove T4 for browser users, because the operator serves the code that holds the keys. 1Password documents this same limit in its own design ([1Password security design, A.1](https://agilebits.github.io/security-design/leopard.html)). Moving the work to clients covers:
  - publish materialization, schema validation, change tokens, scanning fingerprints and diff.
  - In the web app, a crypto stack that WebCrypto does not provide (XChaCha20-Poly1305).
  - Validation moves to clients, and the server can no longer enforce it.

  Transit, SSH CA, server-side MCP, adapter sync and federation cannot work without a server-side key path. A datastore writer can still substitute holder public keys unless clients verify a signed holder list.
- **Confidential computing (option B)** removes nothing against the operator if the operator is also the verifier: an operator-owned AWS KMS key policy, or "Azure compliant CVM" platform attestation. In that case it protects against the cloud host and against insiders who lack deploy and key-policy rights. Two conditions change this:
  - The client verifies the attestation itself, against a measurement that maps to a publicly logged Hikyo release. T1 then becomes "a modified server must be a publicly logged release". That is transparency, not prevention.
  - Holder secrets terminate inside the TEE. T2 is then reduced to datastore writes.

  T3 is not removed for code inside the attested image. Infrastructure surcharges are modest. They are none for Nitro, 10 percent for AWS SEV-SNP, 0 to about 10 percent for the Azure sizes checked, and about $12 to $25 a month per 4 vCPU, 16 GiB node on Google Cloud (figures and assumptions below). The dominant cost is engineering: reproducible or at least logged measurements, attestation verification in clients, images without a shell, and handling of rolling upgrades.
- **A staged attested unsealer (option C)** is feasible. A modified unsealer is still option B2's case: the operator can ship one, attestation only forces it to be a logged release, and its own dependencies are inside the attested image. What the stages remove applies to changes outside the unsealer. Its value depends on what the unsealer returns:
  - The environment key: protects long-lived holder and recovery private keys only.
  - Plaintext values: removes persistent keys from the server, but not per-request exposure.
  - Responses sealed to the client: removes plaintext from the server and from TLS termination, but publish, validation and diff must then move into the unsealer.

  Every stage still needs a signed holder list against datastore key substitution, and none of them fixes slot rollback. To pass the self-hoster test in `docs/adr/oss-mechanics.md`, the unsealer must ship as an open-source artifact with a non-TEE mode.

## What the server does with plaintext today

These facts come from the repository at commit `a8f3cf477`. Line counts exclude `_test.go` files.

- **Publish.**
  - `internal/service/publish.go` (1,603 lines) `materialize` resolves cells and runs key-group validation (`groups.validateResolvedPublish`) and self-config validation. It checks the rendered size (`checkRenderTotal`) and computes the change token through `kr.ChangeToken(...)`. It then seals each cell with `snapshotAAD` or `pendingAAD`.
  - The schema ADR requires validation "at publish, on the resolved value, at the schema revision pinned on the snapshot". A schema change "materializ[es] atomically" across every environment (`docs/adr/schema-model.md`).
- **Validation.** The schema package `internal/schema` is 1,924 lines. Its pattern type is RE2 (`docs/adr/schema-model.md`, "Type" table), and JSON values can carry a JSON Schema.
- **Diff.** `internal/service/revision_diff.go` (175 lines) decrypts two retained snapshots and compares them. A comment notes "Secret equality is never computed until authorization and ceremony complete".
- **Digests.** In Sealed orgs, change tokens and scanning fingerprints are keyed per environment under the environment key (decision 3, property 6). Secret scanning lives in `internal/scanning` (1,287 lines).
- **Crypto.** `internal/crypto` is 6,888 lines. The service layer, which enforces authorization, audit and the ceremony gates, is 70,948 lines.
- **Web app.** It has no crypto dependency today (`web/package.json` lists React, TanStack, zod, yaml and others). A TypeScript client lives in `clients/ts`.
- **Releases.** Releases are signed keyless with Cosign and logged in Rekor. The repository states that this "does not protect against an authorized malicious build" and is not "a reproducible-build comparison" (`docs/adr/stable-workflow-signing.md`, "Accepted limitations"). `docs/adr/oss-mechanics.md` line 148 names reproducible-build verification as "the named upgrade", not claimed for v1.

## Option A: client-side decryption

### What it would take

Under client-side decryption, the server stores and relays ciphertext, wrapped slots and client-computed digests, and never holds an environment key. Every operation that needs plaintext moves to a client.

| Function | Server today | Under client-side decryption |
|---|---|---|
| Fetch, reveal, export | Opens slot, decrypts, returns plaintext | Server returns slot and ciphertext. Client opens the slot with its holder key and decrypts |
| Stage, publish | Server resolves, validates, seals, computes change token | Client resolves the environment state, validates against the pinned schema revision, seals each value with the AAD layout, and computes the change token and scanning fingerprints with per-environment keys. Server checks only structure, authorization, sizes and freshness |
| Schema publish | Validates and materializes every environment at once (decision 5) | A client holding every affected environment key revalidates and re-materializes every environment in one request. This is unchanged from decision 5's key requirement, but the work runs on the client |
| Diff, compare, copy, restore | Server decrypts both sides | Client decrypts both sides |
| Key-group validation, render-size limits | Server, on plaintext | Client. The server can check only ciphertext sizes, which leak approximate value lengths, as they already do at rest |
| Secret scanning | Server-side fingerprints under an environment key | Client-computed fingerprints. The server cannot verify that they match the values |
| Transit, SSH CA | Server performs crypto with key material under the environment key | Cannot run on the server. They need a client-side implementation, an unsealer (option C), or must be refused |
| MCP | Server-side tool calls with a service-account token | Server cannot decrypt. It needs a local MCP server that decrypts on the caller's machine |
| Adapter sync, federation | Decision 11 unattended access | Unchanged. Delivering to a destination with nobody present still needs a server-held slot, or a customer-run agent (open item 2) |
| Environment creation, holder add, rotation | Server generates key, wraps to holders, discards | Client generates and wraps. Adding an absent holder needs that holder's public key from the server, which a datastore writer can replace (see residual risk) |

**Per client:**

- **CLI and Kubernetes operator (Go).** These can import the same `internal/schema`, `internal/crypto` and publish logic, so no second implementation is needed. The work is restructuring publish and diff into a library that runs without the store, and moving key-group, render-size and change-token logic out of `internal/service`.
- **Web app (TypeScript).**
  - WebCrypto `deriveKey` supports X25519 and HKDF ([MDN, SubtleCrypto.deriveKey](https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/deriveKey)). Its encryption algorithms are AES-CTR, AES-CBC, AES-GCM and AES-KW (same page). There is no ChaCha20-Poly1305, and Hikyo's envelope is XChaCha20-Poly1305 (supersession register, encryption-model row).
  - ChaCha20-Poly1305 appears only in a WICG draft that "is not a W3C Standard nor is it on the W3C Standards Track" ([WICG Modern Algorithms in WebCrypto](https://wicg.github.io/webcrypto-modern-algos/)).
  - So the browser needs a JavaScript or WebAssembly AEAD.
  - Two routes:
    1. Compile the Go validator and crypto to WebAssembly (`GOOS=js GOARCH=wasm`). Go's own wiki says "the smallest possible size being around ~2MB" and "10MB+ is common" with imports, about 500 kB gzipped for the minimal example ([Go wiki: WebAssembly](https://go.dev/wiki/WebAssembly)).
    2. Reimplement in TypeScript. That creates a second validator, and JavaScript `RegExp` is not RE2, so `pattern` results can diverge. *Unverified here: the exact RE2 and ECMAScript differences that matter for Hikyo's grammar were not tested.*

### Threats removed

| Threat | Removed? | Remaining risk |
|---|---|---|
| T1 Modified server | **Yes for values, for CLI and operator users.** The server never holds plaintext or environment keys | A modified server can serve a substituted holder public key, so the next wrap, grant or rotation exposes the key. The ADR's rejected "signed holder lists" alternative is needed here, and the ADR itself says it "only makes sense alongside client-side decryption or attested hardware". It can also roll slots and ciphertext back, refuse service, and read metadata. Environments with unattended access are unchanged |
| T2 Insider with production access | **Yes for values.** Memory, logs and traces hold ciphertext | Datastore writes: holder key substitution and rollback, as for T1. Metadata |
| T3 Hostile server dependency | **Yes for values**, same as T1 | Same as T1 |
| T4 Hostile or modified web bundle | **No** for browser users. The bundle holds the keys | Needs code pinning outside the operator's control (see below). CLI and operator users are unaffected |
| Datastore or backup plus root key | Yes, already in Sealed today | None added |

**New risk introduced:** the server can no longer enforce schema, key-group or scanning rules on values. A buggy or hostile client held by an org member can publish values that violate the schema, or false fingerprints. The threat moves from "operator reads values" to "a member writes invalid values". It is an integrity question between members of one org, not a confidentiality one.

**Browser code pinning, the only remedies found:**

- **Isolated Web Apps.** A WICG proposal in which apps are "packaged into Web Bundles, signed by their developer". It targets the case of "the main application server itself being compromised and serving malicious content" ([WICG Isolated Web Apps explainer](https://github.com/WICG/isolated-web-apps/blob/main/README.md)). The explainer states no shipping status. *Unverified: current browser availability. Chrome Status could not be read.*
- **Meta's Code Verify.** A browser extension that compares served JavaScript with hashes published through Cloudflare as a third party. It protects only users who install the extension ([Meta engineering, Code Verify](https://engineering.fb.com/2022/03/10/security/code-verify/)).
- **A desktop app.** It would deliver the web UI as a signed release. *Not researched: Hikyo has a `docs/adr/desktop-app.md`, not read for this note.*

## Option B: confidential computing

### The variable that matters: who verifies the attestation

Attestation proves which code runs only to the party that checks the measurement. Where that party is the operator, the operator can change what is accepted.

- **AWS KMS as verifier.**
  - KMS evaluates `kms:RecipientAttestation:ImageSha384` and `kms:RecipientAttestation:PCR<N>` conditions in the key policy ([KMS condition keys for Nitro Enclaves](https://docs.aws.amazon.com/kms/latest/developerguide/conditions-nitro-enclave.html)).
  - The account that owns the key can replace that policy with `PutKeyPolicy` ([KMS PutKeyPolicy](https://docs.aws.amazon.com/kms/latest/APIReference/API_PutKeyPolicy.html)).
  - So for the operator's own key, attestation through KMS stops only insiders who lack key-policy rights. The policy change is recorded in CloudTrail; that is evidence, not prevention. *Unverified: the specific CloudTrail event shape was not fetched.*
- **Azure Secure Key Release.** Release is gated on Microsoft Azure Attestation claims. The documented sample policy requires `x-ms-attestation-type: sevsnpvm` and `x-ms-compliance-status: azure-compliant-cvm` ([Azure Secure Key Release](https://learn.microsoft.com/en-us/azure/confidential-computing/concept-skr-attestation)). That attests a compliant platform, not a specific application.
- **Client as verifier.** A Hikyo client can verify the document itself:
  1. Check the COSE_Sign1 signature and the certificate chain to the AWS Nitro root ([NSM attestation process](https://github.com/aws/aws-nitro-enclaves-nsm-api/blob/main/docs/attestation_process.md)).
  2. Compare PCR0 with a published measurement.
  3. Seal the holder secret to the `public_key` field carried in the same document.

  This is the only arrangement in which the operator cannot silently widen what is trusted. Apple's Private Cloud Compute uses this model: devices send data only to nodes whose attested measurements appear in a public, append-only transparency log ([Apple Security, Private Cloud Compute](https://security.apple.com/blog/private-cloud-compute/)).

**What a measurement then proves for Hikyo:**

- It proves only "this is a release Hikyo's CI built and logged in Rekor". The repository says this signing does not protect against an authorized malicious build, and that reproducible builds are not claimed.
- Attestation therefore turns T1 into "a modified server must be published as a logged release, visible to anyone". It does not prevent T1.
- Nitro Sandbox PCR6 "changes with every build, unless you pass the same `--build-time` value". AWS advises PCR0 for policies ([Attest a Nitro Sandbox](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nitro-sandboxes-attestation.html)). Matching a measurement to source code needs a pinned, reproducible image build.
- **Rolling upgrades** (decision 9) mean clients must accept at least two measurements at once. That allowlist has to be signed by the release-trust root clients already pin, not served by the operator. Otherwise the operator can add a measurement.

### Platforms

**AWS Nitro Enclaves**

- An enclave is a hardened VM partitioned from an EC2 parent. It has "no persistent storage, interactive access, or external networking", and its memory "cannot be accessed by the processes, applications, or users (root or admin) of the parent instance" ([What is Nitro Enclaves](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave.html)). Up to four enclaves run per parent, and vsock is the only channel ([Nitro Enclaves concepts](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave-concepts.html)).
- The attestation document is signed by the Nitro Hypervisor. It carries PCR0 (image), PCR1 (kernel and bootstrap), PCR2 (application), PCR3 (parent IAM role), PCR4 (parent instance ID) and PCR8 (signing certificate). Debug-mode enclaves report all-zero PCRs ([Cryptographic attestation](https://docs.aws.amazon.com/enclaves/latest/user/set-up-attestation.html)).
- The document has optional `public_key`, `user_data` and `nonce` fields. It is COSE_Sign1 with ECDSA-384, chained to a root with a 30-year lifetime ([NSM attestation process](https://github.com/aws/aws-nitro-enclaves-nsm-api/blob/main/docs/attestation_process.md)).
- KMS `Decrypt`, `DeriveSharedSecret`, `GenerateDataKey`, `GenerateDataKeyPair` and `GenerateRandom` return results encrypted to the attestation document's public key instead of plaintext ([KMS cryptographic attestation](https://docs.aws.amazon.com/kms/latest/developerguide/services-nitro-enclaves.html)).
- On Kubernetes, a device plugin lets pods start enclaves. AWS warns that "All pods and containers in the same Amazon EKS node ... will be able to communicate with the enclave" ([Using Nitro Enclaves with Amazon EKS](https://docs.aws.amazon.com/enclaves/latest/user/kubernetes.html)). Its example reserves 2 vCPUs and 768 MiB of huge pages.
- SDK: AWS ships a Rust crate and a C library (`libnsm`) for the NSM API ([Attest a Nitro Sandbox](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nitro-sandboxes-attestation.html)). *Unverified: no AWS-maintained Go library was found. A Go unsealer would need cgo, a third-party library, or its own NSM ioctl code.*
- Cost: "There are no additional charges for using Nitro Enclaves", and the parent instance's vCPUs and memory pay for the enclave ([What is Nitro Enclaves](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave.html)). The smallest sizes are excluded on most families, for example `m6a.large` and `m7g.medium` (same page).

**AWS Nitro Sandboxes** (announced 2026-11-09, three days before this note; maturity unknown)

- These are per-pod or per-task Firecracker microVMs on the Nitro hypervisor, available "at no additional charge" in all commercial regions ([AWS What's New, 2026-11-09](https://aws.amazon.com/about-aws/whats-new/2026/11/nitro-sandboxes-eks-ecs-ec2/)).
- In confidential mode, "neither the parent instance nor its administrators can read or change that memory". The parent instance "still carries the device traffic, chooses the image, and controls when the Nitro Sandbox runs" ([Nitro Sandboxes](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nitro-sandboxes.html)).
- On instances with two threads per core, "a core runs only one Nitro Sandbox, or only the parent instance", so about half the vCPUs are usable (same page).
- **On EKS, a pod opts in with `runtimeClassName: confidential-sandbox`** ([Sandboxing on Amazon EKS](https://docs.aws.amazon.com/eks/latest/userguide/sandboxing.html)):
  - A Cedar policy pins image digests, arguments, environment and mounts.
  - `exec`, `stdin` and process listing are denied.
  - The policy is measured into attestation.
  - It requires Kubernetes 1.36 or later.

  This matches Hikyo's Kubernetes deployment shape most closely of everything found.
- The workload can request its own attestation document with its own public key, nonce and user data from `/dev/nsm`, for "your own verifier". The chain ends at the same Nitro root ([Attest a Nitro Sandbox](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nitro-sandboxes-attestation.html)). So client-side verification is possible.
- "The Nitro System doesn't measure the contents of block devices". Attested code must live in the initial RAM disk (same page).
- In debug mode, PCR3 and PCR4 keep their values, so a key policy checking only those "also releases secrets to a Nitro Sandbox in debug mode" (same page).

**AWS NitroTPM and attestable AMIs.** These provide measured boot of a whole EC2 instance, verifiable by KMS ([EC2 instance attestation](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nitrotpm-attestation.html)). AWS describes isolation from your own operators as configuration: remove interactive access, make storage read-only, firewall the instance ([Isolate data from your own operators](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/isolate-data-operators.html)). This is integrity of boot, not memory isolation from root on the instance. The page relies on there being no root access at all.

**AWS EC2 with AMD SEV-SNP**

- Available on `m6a`, `c6a` and `r6a` up to fixed sizes. Shared tenancy is offered only in US East (Ohio) and Europe (Ireland).
- Reports are signed with VLEK on shared tenancy, or VCEK on Dedicated Hosts.
- "Hibernation and Nitro Enclaves aren't supported" on the same instance.
- Cost: shared tenancy adds "an additional hourly usage fee that is equivalent to 10 percent of the On-Demand hourly rate". Dedicated Hosts add nothing ([AMD SEV-SNP for Amazon EC2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sev-snp.html)).

**Google Cloud**

- Confidential VM: SEV on N2D, C2D, C3D and C4D. SEV-SNP on N2D only, in zones of asia-southeast1, europe-west3, europe-west4 and us-central1. TDX on C3 and C4. Live migration is supported only for SEV ([Supported configurations](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations)).
- Confidential GKE Nodes support SEV, SEV-SNP and TDX at cluster, node-pool or workload level, and "doesn't change control plane security measures" ([Confidential GKE Nodes](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/confidential-gke-nodes)). The protection is per node, against the host. The cluster administrator keeps full control of pods.
- Confidential Space separates workload operator, workload author and data collaborator. Keys are released through workload identity federation, with attribute conditions on attestation claims ([Confidential Space overview](https://docs.cloud.google.com/confidential-computing/confidential-space/docs/confidential-space-overview)). In that model the operator "has no access to the data". It is a whole-VM, single-container product. *Unverified: whether it fits a multi-node, Kubernetes-managed Hikyo was not checked.*

**Azure**

- Confidential VMs: the page lists DCasv5, DCasv6, ECasv5, ECasv6, DCesv6 and ECesv6 (and variants with local disk). It says attestation covers "either AMD SEV-SNP or Intel TDX". *Inferred, not stated by the page: "a" series are AMD SEV-SNP and "e" series are Intel TDX, by Azure's naming convention.* They do not support live migration, Accelerated Networking or Site Recovery ([About Azure confidential VMs](https://learn.microsoft.com/en-us/azure/confidential-computing/confidential-vm-overview)).
- Secure Key Release works from Key Vault Premium or Managed HSM. A policy can also accept another OIDC attestation authority whose certificate chains to DigiCert ([Azure Secure Key Release](https://learn.microsoft.com/en-us/azure/confidential-computing/concept-skr-attestation)).

### Cost of the infrastructure

The assumptions differ per row, and are stated in each.

| Platform | Surcharge | Example |
|---|---|---|
| AWS Nitro Enclaves, Nitro Sandboxes | None ([1](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave.html), [2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nitro-sandboxes.html)) | Enclave or sandbox vCPUs and memory come out of the node. On two-threads-per-core instances, a sandbox consumes whole cores |
| AWS SEV-SNP, shared tenancy | +10% of the on-demand hourly rate ([source](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sev-snp.html)) | Ohio and Ireland only. *Base prices not fetched* |
| GCP SEV-SNP (N2D) | $0.0027502 per vCPU-hour and $0.0003686 per GiB-hour, on demand ([Confidential VM pricing](https://cloud.google.com/confidential-computing/confidential-vm/pricing)). *The page has a region selector, and the region of these default figures was not confirmed* | 4 vCPU and 16 GiB × 730 h = **$12.34 a month per node**. *GCP base prices were not fetched, so no percentage is given* |
| GCP TDX (C3) | $0.0033982 per vCPU-hour and $0.0004555 per GiB-hour (same source) | 4 vCPU and 16 GiB × 730 h = **$15.24 a month per node** |
| GCP SEV (N2D, C2D, C3D, C4D) | $0.005479 per vCPU-hour and $0.0007342 per GiB-hour (same source). SEV is priced above SEV-SNP as listed on the page | 4 vCPU and 16 GiB × 730 h = **$24.57 a month per node** |
| Azure SEV-SNP (inferred, see above) | West Europe, Linux, on demand: `Standard_D4as_v5` $0.208/h and `Standard_DC4as_v5` $0.208/h, from the [Azure Retail Prices API](https://prices.azure.com/api/retail/prices) on 2026-11-12 | **No difference** at this size and region |
| Azure TDX (inferred, see above) | Same source: `Standard_D4s_v6` $0.242/h and `Standard_DC4es_v6` $0.266/h | +$0.024/h, about **+9.9% or $17.52 a month**. *Not verified as like-for-like CPU generations* |

The 4 vCPU, 16 GiB shape is an assumption for illustration. The ADR fixes 2 to 3 nodes per installation at the start (decision 1), not their size. On that assumption the Google Cloud and Azure surcharges come to tens of dollars a month per installation. Two caveats apply:

- AWS SEV-SNP is not converted to dollars, because its base price was not fetched.
- Nitro has no surcharge, but on two-threads-per-core instances an enclave or sandbox takes whole cores out of the node. In practice that can mean a larger instance.

The cost the ADR calls "too costly as a foundation" is mostly engineering and operations, not hardware:

- reproducible images whose measurements map to source
- signed measurement allowlists for rolling upgrades
- attestation verification in the CLI, the operator and, if pursued, the browser
- images without a shell or debug access, which changes incident response
- platform lock-in: Nitro is AWS-only, and SEV-SNP and TDX attestation chains differ per vendor and cloud.

### Hardware residual risk

- **TEE.fail** uses a DDR5 interposer that "should cost you under $1000". It extracted Intel's per-CPU attestation key, enabling forged SGX and TDX quotes, and ECDSA keys from SEV-SNP guests. Intel and AMD consider interposer attacks out of scope ([TEE.fail](https://tee.fail/)). It needs physical access, so it is a cloud-provider-insider risk.
- **RMPocalypse**, CVE-2025-0033: "Improper access control within AMD SEV-SNP could allow an admin privileged attacker to write to the RMP during SNP initialization" ([NVD API record](https://services.nvd.nist.gov/rest/json/cves/2.0?cveId=CVE-2025-0033)). It references AMD-SB-3020. *That bulletin timed out and was not read.*
- **Nitro.** Nitro Enclaves and Nitro Sandboxes trust AWS's hypervisor rather than CPU memory encryption against the host. AWS states there is "no mechanism for any AWS system or person to log in to Amazon EC2 Nitro hosts" ([Isolate data from your own operators](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/isolate-data-operators.html)). That is a vendor claim, not a hardware guarantee.

### Threats removed by confidential computing

| Threat | B1: whole server in a CVM, operator-verified (KMS policy or platform claims) | B2: whole server in a TEE, client-verified, holder secrets and TLS end inside |
|---|---|---|
| T1 Modified server | **No.** The operator picks the image and the policy | **Made visible, not prevented.** It must be a logged release. Only verifying clients (CLI, operator) benefit |
| T2 Insider with production access | **Partly.** Removes host-level memory reads and, with no-shell images, `kubectl exec`, for people without deploy or key-policy rights. Not logging at an external TLS terminator. Not datastore writes | **Mostly.** Memory, exec and TLS logging are closed. Datastore writes (holder-key substitution, rollback) remain unless the TEE checks a signed holder list |
| T3 Hostile server dependency | **No** | **No.** The dependency is inside the attested image. The trusted computing base is the whole server (the service layer alone is about 71,000 lines) |
| T4 Hostile web bundle | **No** | **No** |
| Cloud provider or host | Yes, within the hardware limits above | Yes, within the hardware limits above |

## Option C: a staged attested unsealer

A small separate process, the unsealer, runs in a TEE such as a Nitro Enclave or a Nitro Sandbox confidential pod. It is the only component that opens slots. The rest of the server keeps authorization, audit and storage. Clients that support it verify the unsealer's attestation and seal holder secrets to the attested key. This extends decision 12: the HPKE recipient moves from an installation-derived key into the unsealer.

### Stages, by what the unsealer returns

| Stage | Unsealer holds | Returns to the server | Removes | Does not remove |
|---|---|---|---|---|
| C1 Key custody | Transit private key, `sealedSlotKey` | The environment key, for that request | Capture of **long-lived holder secrets** by the server outside the unsealer: member private keys, recovery keys and enveloped service-account token bodies. This affects the honest statement's "One captured recovery key opens everything it holds" | A captured environment key, which opens that environment until a holder rotates it. Plaintext in requests and responses. Raw-bearer service-account tokens, which decision 12 still accepts for existing CI, and raw MCP tokens: these never reach the unsealer sealed |
| C2 Value decryption | As C1, plus it generates new environment keys inside | Plaintext values, never the environment key | **Persistent keys on the server.** The honest statement's "One hostile release can leave a known key that survives redeploying a clean binary" applies only to the unsealer | Within one request, a modified server can feed every ciphertext of that environment to the unsealer. Plaintext still passes through the server and TLS termination |
| C3 End-to-end to the unsealer | As C2, plus publish materialization, validation, change tokens, scanning fingerprints, diff | Responses sealed to the client's ephemeral key. Only ciphertext and metadata reach the server | **Plaintext on the server and at TLS termination**, for verifying clients. T1, T2 and T3 are removed for values, for changes outside the unsealer | A modified unsealer or a hostile dependency inside it is visible only, as in B2. The unsealer's trusted computing base grows to include `internal/schema` and parts of publish and diff. Browser T4 remains |

**Needed at every stage:**

- **Signed holder lists.** Otherwise a datastore writer or modified server hands the unsealer an attacker's public key when wrapping for a new holder. The ADR rejected these alone, but says they make sense "alongside client-side decryption or attested hardware". The unsealer would verify a chain rooted at the org's recovery keys before wrapping.
- **Rollback.** Nitro enclaves have "no persistent storage" ([What is Nitro Enclaves](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave.html)). Without an external monotonic counter or a transparency log of slot state, rolling slots back to a removed member's slot stays undetectable. *No primary source on monotonic counters for Nitro was found.*
- **Request binding inside the unsealer.** The decision 12 envelope already binds method, path, body digest, org and session. The unsealer must check that every slot and ciphertext it opens matches that scope. Without that, a server can replay a valid envelope against another environment.

**Transit key distribution.** This decides whether KMS becomes the trust anchor again.

- *Shared key released by KMS to the image* (as decision 12 derives it today):
  - Pros: stateless routing, so any node can serve any request.
  - Cons: the operator can widen the KMS policy to release the key to a debug enclave and decrypt captured envelopes. No forward secrecy.
- *Key generated inside each unsealer and carried in its attestation `public_key`:*
  - Pros: no KMS anchor, and forward secrecy across unsealer restarts.
  - Cons: clients fetch and verify attestation per unsealer, and requests need sticky routing or a key identifier. A shared key between unsealers would need mutual attestation that the unsealer image itself checks.

  This design is a sketch, not a sourced construction. See `hosted-transit-forward-secrecy.md` for forward-secrecy constructions.

**`sealedSlotKey` custody.** Releasing it through KMS to the unsealer image keeps today's assumption that the outer layer is operator-held. The inner wraps still need a holder secret, which only a verified unsealer receives. The KMS policy therefore does not undo C1 to C3 for verifying clients.

**Unattended access (decision 11).** An installation slot opened only inside the unsealer would stop the main server from reading the key, but the adapter worker or federated fetch still receives plaintext. No stage removes the exposure of unattended environments to a modified server. The honest statement's exception stays.

**The self-hoster test.** The test requires that every outcome be "achievable by a self-hoster using only released open-source artifacts and documented public interfaces", and that hosted code "may never contain an exclusive capability" (`docs/adr/oss-mechanics.md`, line 205). An unsealer therefore needs:

- to ship as an open-source release artifact
- a non-TEE mode, for example the same binary as a local sidecar or in-process
- a pluggable attestation backend
- clients that let the installation choose whether attestation is required.

Whether a TEE-only verification policy for the hosted service counts as configuration or as an exclusive capability is for the owner to decide.

**Per-client effect:**

- CLI and operator: can pin the Nitro root and Hikyo's release-trust root, verify attestation and reject unknown measurements.
- Browser: can technically verify an ECDSA-384 signature, but a modified bundle can skip the check. C1 to C3 do not remove T4 for browser users, the same limit as option A.
- MCP clients that send raw tokens: not protected, as in decision 12.

## Comparison

| | T1 Modified server | T2 Production insider | T3 Server dependency | T4 Web bundle | Datastore key substitution | Engineering scope |
|---|---|---|---|---|---|---|
| Today (Sealed) | Not protected | Not protected (capture) | Not protected | Not protected | Not protected | None |
| A Client-side decryption | Removed for values (CLI, operator) | Removed for values | Removed for values | Not removed | Needs signed holder lists | Publish, validation and diff restructured for clients. Web crypto stack. Transit, SSH CA and server MCP lost or moved |
| B1 CVM, operator-verified | Not removed | Partly | Not removed | Not removed | Not removed | Low code, high operations (no-shell images) |
| B2 TEE, client-verified | Visible only (logged release) | Mostly removed | Not removed | Not removed | Needs signed holder lists | Measurements, allowlists, client verification. Whole-server trusted computing base |
| C1 Unsealer, key custody | Outside the unsealer: protects enveloped long-lived holder keys only. The unsealer itself: visible only | Enveloped long-lived holder keys only | Outside the unsealer: enveloped long-lived holder keys only. Inside: not removed | Not removed | Needs signed holder lists | Small unsealer, client verification |
| C2 Unsealer, values | Outside the unsealer: no persistent key, per-request exposure. The unsealer itself: visible only | No persistent key, per-request exposure | Outside: as T1. Inside: not removed | Not removed | Needs signed holder lists | As C1, plus value decryption in the unsealer |
| C3 Unsealer, end-to-end | Outside the unsealer: removed for values. The unsealer itself: visible only | Removed for values | Outside: removed for values. Inside: not removed | Not removed | Needs signed holder lists | As C2, plus publish, validation and diff in the unsealer |

C1 to C3 apply to CLI and operator users who verify attestation. "Visible only" means as in B2: a changed unsealer must be a publicly logged release. Unattended environments, metadata, rollback and raw-bearer tokens stay exposed in every row.

## Reference products (reference only)

- **Bitwarden Secrets Manager.** It describes itself as end-to-end encrypted, with "All encryption and decryption of secrets data ... done client-side" ([Bitwarden SM overview](https://bitwarden.com/help/secrets-manager-overview/)). A machine access token carries "A unique encryption key" that decrypts the organization key locally ([Bitwarden secret decryption](https://bitwarden.com/help/secret-decryption/)). Hikyo's service-account tokens already derive their slot key from the token body (decision 3), so a client-side fetch for tokens follows the same shape.
- **1Password.**
  - It acknowledges that "An attacker capable of changing the web client on the server could deliver a malicious client to the user". Its mitigations are HSTS and "Pin Certificates (not yet implemented)" ([1Password security design, A.1](https://agilebits.github.io/security-design/leopard.html)).
  - Its Connect server decrypts in the customer's environment, with stored credentials "encrypted with a key derived from the bearer token" ([1Password security design, Secrets Automation](https://agilebits.github.io/security-design/secrets.html)). This is a reference for open item 2's customer-run agent.
- **Infisical.**
  - In 2023 it let projects opt out of E2EE, finding it "perceived more as a nice-to-have rather than a must-have feature" and noting API consumers needed "to query and fetch their secrets back in plaintext" ([Infisical update, June 2023](https://infisical.com/blog/infisical-update-june-2023)).
  - Its current security page describes server-side decryption: "Before responding to a client request, the server securely retrieves and decrypts requested data" ([Infisical internals: security](https://infisical.com/docs/internals/security)).
  - *Unverified: whether E2EE was later removed entirely. Only a third-party fork's pull request claimed so, and it was not used.*
- **Apple Private Cloud Compute.** Attested nodes, a public transparency log of measurements, no remote shell, stateless processing ([Apple Security](https://security.apple.com/blog/private-cloud-compute/)). It is the closest published model to B2 and C3 with client verification.

## Unverified and open items

1. The hosted-service ADR does not choose a cloud provider. Nitro options apply only on AWS, Confidential Space only on Google Cloud, and Secure Key Release only on Azure.
2. Nitro Sandboxes are three days old. Their production maturity, the EKS 1.36 requirement in practice, and Cedar policy handling for a stateful Hikyo pod are untested.
3. No AWS-maintained Go library for the NSM API was found. A Go unsealer would need cgo against `libnsm`, a third-party library, or its own code.
4. AWS base on-demand prices for `m6a` and `c6a` were not fetched, so the 10% SEV-SNP surcharge is not converted to dollars.
5. The Azure v6 comparison may not be like-for-like hardware. The mapping of Azure series to SEV-SNP or TDX is inferred from naming. The region behind the Google Cloud list prices was not confirmed, and Google Cloud base prices were not fetched.
6. AMD-SB-3020 was not read. CloudTrail event shapes for `PutKeyPolicy` were not checked.
7. Isolated Web Apps' shipping status is unknown. Hikyo's desktop-app ADR was not read.
8. The divergences between RE2 and ECMAScript regular expressions that matter for Hikyo's schema grammar were not tested.
9. Whether a TEE-required verification policy on the hosted service passes the self-hoster test is an owner decision.
10. Confidential Space's fit for a multi-node Kubernetes deployment was not checked.

## Sources

Repository (commit `a8f3cf477`): `docs/adr/hosted-service.md`, `docs/adr/schema-model.md`, `docs/adr/stable-workflow-signing.md`, `docs/adr/oss-mechanics.md`, `internal/service/publish.go`, `internal/service/revision_diff.go`, `internal/schema/`, `internal/crypto/`, `internal/scanning/`, `web/package.json`.

External, all fetched on 2026-11-12:

- AWS: [What is Nitro Enclaves](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave.html); [Nitro Enclaves concepts](https://docs.aws.amazon.com/enclaves/latest/user/nitro-enclave-concepts.html); [Cryptographic attestation](https://docs.aws.amazon.com/enclaves/latest/user/set-up-attestation.html); [Nitro Enclaves with EKS](https://docs.aws.amazon.com/enclaves/latest/user/kubernetes.html); [NSM attestation process](https://github.com/aws/aws-nitro-enclaves-nsm-api/blob/main/docs/attestation_process.md); [KMS cryptographic attestation](https://docs.aws.amazon.com/kms/latest/developerguide/services-nitro-enclaves.html); [KMS condition keys for Nitro Enclaves](https://docs.aws.amazon.com/kms/latest/developerguide/conditions-nitro-enclave.html); [KMS PutKeyPolicy](https://docs.aws.amazon.com/kms/latest/APIReference/API_PutKeyPolicy.html); [EC2 instance attestation (NitroTPM)](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nitrotpm-attestation.html); [Isolate data from your own operators](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/isolate-data-operators.html); [AMD SEV-SNP for EC2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sev-snp.html); [Nitro Sandboxes](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nitro-sandboxes.html); [Attest a Nitro Sandbox](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nitro-sandboxes-attestation.html); [Sandboxing on EKS](https://docs.aws.amazon.com/eks/latest/userguide/sandboxing.html); [What's New 2026-11-09](https://aws.amazon.com/about-aws/whats-new/2026/11/nitro-sandboxes-eks-ecs-ec2/).
- Google Cloud: [Confidential VM supported configurations](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations); [Confidential VM pricing](https://cloud.google.com/confidential-computing/confidential-vm/pricing); [Confidential GKE Nodes](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/confidential-gke-nodes); [Confidential Space overview](https://docs.cloud.google.com/confidential-computing/confidential-space/docs/confidential-space-overview).
- Azure: [Confidential VM overview](https://learn.microsoft.com/en-us/azure/confidential-computing/confidential-vm-overview); [Secure Key Release](https://learn.microsoft.com/en-us/azure/confidential-computing/concept-skr-attestation); [Azure Retail Prices API](https://prices.azure.com/api/retail/prices).
- Attacks: [TEE.fail](https://tee.fail/); [NVD CVE-2025-0033](https://services.nvd.nist.gov/rest/json/cves/2.0?cveId=CVE-2025-0033).
- Browser and language: [MDN SubtleCrypto.deriveKey](https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/deriveKey); [WICG Modern Algorithms in WebCrypto](https://wicg.github.io/webcrypto-modern-algos/); [Go wiki: WebAssembly](https://go.dev/wiki/WebAssembly); [WICG Isolated Web Apps](https://github.com/WICG/isolated-web-apps/blob/main/README.md); [Meta Code Verify](https://engineering.fb.com/2022/03/10/security/code-verify/).
- Products: [Bitwarden SM overview](https://bitwarden.com/help/secrets-manager-overview/); [Bitwarden secret decryption](https://bitwarden.com/help/secret-decryption/); [1Password security design, Appendix A](https://agilebits.github.io/security-design/leopard.html); [1Password Secrets Automation](https://agilebits.github.io/security-design/secrets.html); [Infisical update June 2023](https://infisical.com/blog/infisical-update-june-2023); [Infisical internals: security](https://infisical.com/docs/internals/security); [Apple Private Cloud Compute](https://security.apple.com/blog/private-cloud-compute/).
