# Research: external key custody for per-org and root keys (KMS, HSM, customer-held keys)

Date: 2026-11-12. Ticket: [Hikyo-Org/Hikyo#1303](https://github.com/Hikyo-Org/Hikyo/issues/1303).

Scope: how Hikyo's hosted service could hold its root key, and one key per org, outside the database and its backups, in EU regions. This document compares options and trade-offs. It does not choose one.

All vendor facts come from vendor documentation, pricing pages or APIs, specifications, or source code. They were read on 2026-11-12. Anything not confirmed in a primary source is marked **Unverified**.

## 1. Where Hikyo stands today

These points come from the repository's own ADRs, at commit `a8f3cf477056a5877356ae90000caf01c409b8d6`.

- The root key is operator-provided and never stored in the datastore (`docs/adr/encryption-model.md, line 41`). Its custody domain must stay separate from the database and backups (line 163).
- **External KMS is interface-reserved, not implemented.** A `wrap(keyBytes) / unwrap(blob)` seam exists at the root-key boundary only (line 161).
- **Per-scope KMS routing is a future format version, not a drop-in.** A per-project or per-org KMS unwrap needs a provider identifier and a key reference in the wrapped-key record. Today's format has neither (line 50).
- **Key destruction cannot reach a backup that has already been written.** The wrapped key hierarchy travels inside every backup (line 284). The ADR names external KMS or HSM custody as the general route to real crypto-shredding. It rejected that route for v1 because the KMS becomes a hard dependency for restore. It asks for this section to be revisited "if a KMS provider ever ships" (line 286). Transit keys follow the same rule (line 21).
- **The hosted service is `sealed-only`** (`docs/adr/hosted-service.md`, decision 2). Sealed orgs use one key per environment, opened by a customer holder secret together with the installation (decision 3, property 1).
- **Three keys already affect every org on an installation:** the root key, the master key, and `sealedSlotKey`. `sealedSlotKey` forms the outer layer of every slot, and "losing it destroys every slot in the installation" (decision 3, property 2).
- **Hikyo keeps no copy of a Sealed customer's secret, and support cannot recover a Sealed org** (decision 8). An exception is an environment with unattended access, where the installation holder opens it without the customer (decision 11).

### What this means for the question

- **Moving the root key into a KMS or HSM does not remove the "single key affecting all orgs" property.** One external root still opens the master, and the master opens `sealedSlotKey`. External custody changes who can use the root and where it lives. It does not split the blast radius.
- **A per-org kill switch that reaches backups needs per-org material that is not in any database backup.** Destroying a database row can never do this, by the ADR's own reasoning (line 284).

## 2. The options

| Option | What holds the key | EU regions |
|---|---|---|
| A. Status quo | Operator file or secret mount, plus the mandatory backup-retention bound | Any |
| B. Cloud KMS, multi-tenant | AWS KMS, Google Cloud KMS (software or HSM), Azure Key Vault (Standard or Premium) | Yes, see section 9 |
| C. EU-headquartered KMS | OVHcloud KMS, Scaleway Key Manager | Yes |
| D. Single-tenant HSM | AWS CloudHSM, GCP Single-tenant Cloud HSM, Azure Managed HSM, own hardware | Yes, with limits |
| E. OpenBao or Vault transit | Self-run transit engine, optionally backed by an HSM | Wherever it is run |
| F. Customer key in the customer's own cloud KMS (cross-account "BYOK") | The customer's AWS, GCP or Azure key, used by Hikyo through a grant | Customer's choice |
| G. Hold your own key (HYOK) | Customer's HSM outside the cloud, through AWS XKS, GCP Cloud EKM or Azure Managed HSM EKM | Customer's choice |
| H. Imported key material (BYOK into Hikyo's KMS) | Hikyo's KMS, with the customer or Hikyo holding the original | As B |

## 3. Non-exportability

| Option | Can raw key material leave? | Source |
|---|---|---|
| AWS KMS | No. "There is no mechanism to export plaintext KMS keys." The HSMs are FIPS 140-3 validated, and an administrative action needs a quorum of AWS employees. | [AWS KMS design goals](https://docs.aws.amazon.com/kms/latest/cryptographic-details/design-goals.html), [intro](https://docs.aws.amazon.com/kms/latest/cryptographic-details/intro.html) |
| AWS KMS, imported material | AWS never exports it. The importer keeps the original "for additional durability and disaster recovery". Symmetric ciphertext cannot be decrypted outside KMS, because the format is unpublished. | [Importing key material](https://docs.aws.amazon.com/kms/latest/developerguide/importing-keys.html) |
| Google Cloud KMS | No. Key material "can never be viewed or exported". Cloud HSM uses FIPS 140-2 Level 3 HSMs. **Unverified:** a search snippet says material cannot move between software, multi-tenant HSM, single-tenant HSM and EKM. No fetched page confirmed it. | [resources.proto @ac683aa](https://github.com/googleapis/googleapis/blob/ac683aa72f2329dd8bae638500c3e06b9fdafc29/google/cloud/kms/v1/resources.proto#L321-L324), [Cloud HSM](https://docs.cloud.google.com/kms/docs/hsm) |
| Azure Key Vault and Managed HSM | Not exportable by default. Export happens only for keys marked exportable, through Secure Key Release to an attested workload. Vault software keys are FIPS 140-2 Level 1, and new Premium HSM keys are FIPS 140-3 Level 3. Key Vault **backup blobs** are a second copy: they "cannot be decrypted outside of Azure", but they restore into any vault in the same subscription and geography. | [About keys](https://learn.microsoft.com/en-us/azure/key-vault/keys/about-keys), [Managed HSM overview](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/overview), [service limits (backup)](https://learn.microsoft.com/en-us/azure/key-vault/general/service-limits) |
| OVHcloud KMS | **Exportable in wrapped form.** Both tiers document BYOK import and export using RSA-OAEP wrapping. HSM tier: Thales Luna, FIPS 140-3 Level 3. Software tier: FIPS 140-3 Level 1. | [OVHcloud KMS](https://www.ovhcloud.com/en-ie/identity-security-operations/key-management-service/) |
| Scaleway Key Manager | HSM-backed keys are generated in FIPS 140-3 Level 3 HSMs, Paris only. An export path is **Unverified**: the FAQ does not mention one. | [Scaleway Key Manager FAQ @08390de](https://github.com/scaleway/docs-content/blob/08390debab5db2224c8dd635f1dc7c0feae9dfb7/pages/key-manager/faq.mdx) |
| AWS CloudHSM | Backups leave the HSM encrypted, and "cannot be decrypted by AWS". | [CloudHSM backups](https://docs.aws.amazon.com/cloudhsm/latest/userguide/backups.html) |
| OpenBao transit | Not exportable unless `exportable` or `allow_plaintext_backup` is set at creation, and neither can be turned off once set. **Every key lives in OpenBao's own storage**, encrypted under the barrier. A Raft snapshot captures "the OpenBao data", so a snapshot is a copy of every transit key. | [OpenBao transit API @92de51f](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/website/content/docs/api/secret/transit.mdx), [transit concepts](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/website/content/docs/secrets/transit/index.mdx), [raft snapshot](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/website/content/docs/commands/operator/raft.mdx) |
| Vault transit | Same `exportable` and `allow_plaintext_backup` semantics. HSM-held "managed keys" are Enterprise only. | [Vault transit API](https://developer.hashicorp.com/vault/api-docs/secret/transit) |
| OpenBao External Keys (2.7.0) | Transit can delegate operations to a KMS or HSM through plugins "without storing key material in OpenBao". The PKCS#11 and Securosys plugins are separate installs. | [CHANGELOG 2.7.0](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/CHANGELOG.md), [External Keys](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/website/content/docs/concepts/external-keys.mdx), [plugins](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/website/content/docs/api/system/external-keys/plugins/index.mdx) |
| AWS XKS / GCP EKM / Azure EKM | The key never enters the cloud provider. AWS XKS: "Your cryptographic key material never leaves your external key manager." GCP: "never cached or stored within Google Cloud". Azure: the KEK "never resides in or transits Microsoft infrastructure". | [AWS XKS](https://docs.aws.amazon.com/kms/latest/developerguide/keystore-external.html), [GCP EKM](https://docs.cloud.google.com/kms/docs/ekm), [Azure EKM](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/external-key-management-overview) |

## 4. Latency per unwrap, and caching

### Published latency

- **AWS, Google and Microsoft publish no per-call latency figure for their standard KMS.** **Unverified**: any number has to come from Hikyo's own benchmark in the target region. AWS says only that KMS offers "latency and throughput levels suitable for use by other services in AWS" ([design goals](https://docs.aws.amazon.com/kms/latest/cryptographic-details/design-goals.html)).
- **AWS XKS has a hard 250 ms timeout.** "If KMS does not receive a response from the XKS proxy for any API call within 250ms, KMS will time out and the corresponding KMS API call will fail." Ping latency to the external manager "SHOULD be 35ms or less" ([XKS proxy spec @f7604f6](https://github.com/aws/aws-kms-xksproxy-api-spec/blob/f7604f6544ed398a3e5c5df2bc53a176d26dbbe5/xks_proxy_api_spec.md)).
- **GCP EKM:** going over the internet "can lead to problems with reliability, availability, and latency" ([Cloud EKM](https://docs.cloud.google.com/kms/docs/ekm)).
- **Azure EKM:** latency is "Higher (proxy + HSM round-trip added to each operation)" ([Azure EKM](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/external-key-management-overview)).
- **OpenBao transit:** no published figure. **Unverified.**

### Request quotas (the real ceiling on uncached unwraps)

| Service | Default limit | Adjustable | Source |
|---|---|---|---|
| AWS KMS symmetric operations | 20,000/s shared in eu-central-1, eu-west-2 and eu-south-2. 100,000/s in eu-west-1. 10,000/s elsewhere. | Yes | [AWS request quotas](https://docs.aws.amazon.com/kms/latest/developerguide/requests-per-second.html) |
| AWS custom key store (CloudHSM or XKS) | 1,800/s per key store. At most 10 key stores per account and region. | **No** for the rate | Same page; [resource quotas](https://docs.aws.amazon.com/kms/latest/developerguide/resource-limits.html) |
| AWS CreateKey / ScheduleKeyDeletion | 5/s / 15/s | Yes | [AWS request quotas](https://docs.aws.amazon.com/kms/latest/developerguide/requests-per-second.html) |
| GCP Cloud KMS, current (since 16 Feb 2026) | Regional, token-based: software 6,000,000 TPM, HSM 3,000,000 TPM, external 10,000 TPS. HSM create/import and all EKM requests are hard-enforced. | Yes | [Cloud KMS quotas](https://docs.cloud.google.com/kms/quotas) |
| GCP, superseded regime | HSM symmetric 500 QPS, EKM 100 QPS per region | n/a | Same page. **Do not use for planning.** **Unverified:** how many tokens one unwrap costs, so a requests-per-second conversion is unknown. |
| Azure Key Vault (vault) | Per vault per region, per 10 s: software RSA-2048 4,000, HSM RSA-2048 2,000, HSM create 10. Subscription total is 5 times the vault limit. | Not stated | [Azure service limits](https://learn.microsoft.com/en-us/azure/key-vault/general/service-limits) |

### Caching

Every option reaches acceptable latency only if Hikyo caches the unwrapped org key in process memory. The cache sets how long a revoked key keeps working.

- AWS states the general rule: when a KMS key becomes unusable, data keys already decrypted are "not affected until the KMS key is used again" ([unusable KMS keys](https://docs.aws.amazon.com/kms/latest/developerguide/unusable-kms-keys.html)). AWS XKS documentation says the same for services that cache: data keys "continue to work until you deactivate the resource or the cache expires" ([AWS XKS](https://docs.aws.amazon.com/kms/latest/developerguide/keystore-external.html)).
- AWS's multi-tenant pattern, the Hierarchical keyring, uses one branch key per tenant. A "cache limit TTL" sets "how often the client calls AWS KMS to authorize use of the branch keys", and the examples use 600 s. The data key caching page lists the "AWS Encryption SDK for Go" (version 0.1.x and later) among the SDKs that support it. The branch keys live in a DynamoDB table, so they have the same backup issue as Hikyo's datastore ([Hierarchical keyring](https://docs.aws.amazon.com/encryption-sdk/latest/developer-guide/use-hierarchical-keyring.html)).
- On Azure, a 403 or 404 from Key Vault requires a consuming service to flush its cached DEKs. Microsoft notes that "revocation isn't instantaneous", and gives the example that Azure Monitor storage becomes unavailable within one hour ([encryption at rest](https://learn.microsoft.com/en-us/azure/security/fundamentals/encryption-atrest)).
- AWS warns that data key caching should be used "cautiously", only as much as performance needs ([data key caching](https://docs.aws.amazon.com/encryption-sdk/latest/developer-guide/data-key-caching.html)).

**Arithmetic, not a measurement.** Suppose every one of N active orgs is unwrapped once per TTL on each of k nodes. The worst-case steady unwrap rate is then `N × k / TTL`. For N = 100,000, k = 3 and TTL = 300 s, that is 1,000 unwraps/s.

- AWS Frankfurt's 20,000/s covers that.
- A single AWS custom key store (1,800/s) covers it only just.
- A single Azure vault (about 400/s for software RSA-2048, about 200/s for HSM) does not.

Real load depends on how many orgs are active, which is **Unverified**.

## 5. Cost per org

Unit prices in EU regions:

| Item | Price | Source |
|---|---|---|
| AWS KMS key (eu-central-1) | $1 per key per month. Each of the first two rotations adds $1 per month, capped at $3 per key per month. | [AWS KMS pricing](https://aws.amazon.com/kms/pricing/); [AWS price list API, eu-central-1](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/awskms/current/eu-central-1/index.json) |
| AWS KMS requests | $0.03 per 10,000 symmetric. 20,000 free per month. | Same |
| AWS CloudHSM hsm2m (Frankfurt) | $1.92 per HSM-hour, about $1,402 per month. Two HSMs for HA cost about $2,803 per month. | [AWS price list API, CloudHSM eu-central-1](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/CloudHSM/current/eu-central-1/index.json) |
| GCP key version, symmetric | SOFTWARE $0.000082192/h (about $0.06/month). HSM $0.001369863/h (about $1.00). EXTERNAL $0.004109589/h (about $3.00). Versions that are disabled or scheduled for destruction are still billed. Destroyed versions are free. | [GCP KMS pricing](https://cloud.google.com/kms/pricing) |
| GCP operations | $0.03 per 10,000. **Unverified:** a fourth price column of $0.15 per 10,000 appears in the table without a clear label. | Same |
| GCP Single-tenant Cloud HSM | $4.794520548/h, about $3,500 per month, with 15,000 key versions included. Each version above that costs about $1 per month. No per-operation fee. | Same |
| Azure Key Vault Standard, software RSA key | No per-key fee. $0.03 per 10,000 operations. | [Azure Retail Prices API, westeurope](https://prices.azure.com/api/retail/prices?$filter=serviceName%20eq%20'Key%20Vault'%20and%20armRegionName%20eq%20'westeurope'%20and%20priceType%20eq%20'Consumption') |
| Azure Key Vault Premium, HSM RSA-2048 key | $1 per key version per month, charged "only [for] actively used HSM protected keys (used in prior 30-day period)". Advanced key types are tiered from $5 down to $0.40. | Same; [Azure pricing page footnote](https://azure.microsoft.com/en-us/pricing/details/key-vault/) |
| Azure Managed HSM (Standard B1 pool) | $3.20 per hour, about $2,336 per month | [Azure Retail Prices API](https://prices.azure.com/api/retail/prices?$filter=serviceName%20eq%20'Key%20Vault'%20and%20armRegionName%20eq%20'westeurope'%20and%20priceType%20eq%20'Consumption') |
| Azure vault oct-HSM (AES) | In public preview. **Unverified** price. | [About keys](https://learn.microsoft.com/en-us/azure/key-vault/keys/about-keys) |
| OVHcloud KMS | €0.06 per key per month (software), €1 per key per month (HSM). Unlimited API calls. | [OVHcloud KMS](https://www.ovhcloud.com/en-ie/identity-security-operations/key-management-service/) |
| Scaleway Key Manager | €0.06 per key version per month (software), €0.85 (HSM). €0.03 per 10,000 operations. | [Scaleway FAQ @08390de](https://github.com/scaleway/docs-content/blob/08390debab5db2224c8dd635f1dc7c0feae9dfb7/pages/key-manager/faq.mdx) |
| AWS XKS, GCP EKM, Azure EKM | Cloud fee as above (AWS $1 per key, GCP $3 per version, Azure Managed HSM pool). The customer also pays for the HSM, proxy and network. | Pages in section 3 |
| OpenBao | No license fee. Infrastructure and HSM cost is **Unverified**. | n/a |

Monthly key-holding cost at one key per org, one active version, before request charges:

| Option | 1,000 orgs | 10,000 orgs | 100,000 orgs | Quota wall to note |
|---|---|---|---|---|
| AWS KMS | $1,000 | $10,000 | $100,000 (up to $300,000 if rotated twice) | 100,000 keys per account per region by default. Adjustable. ([resource quotas](https://docs.aws.amazon.com/kms/latest/developerguide/resource-limits.html)) |
| GCP software | $60 | $600 | $6,000 | No limit on key count ([quotas](https://docs.cloud.google.com/kms/quotas)) |
| GCP HSM | $1,000 | $10,000 | $100,000 | Same |
| GCP EXTERNAL (HYOK) | $3,000 + partner | $30,000 + partner | $300,000 + partner | EKM requests hard-enforced |
| GCP single-tenant HSM | $3,500 (1 instance) | $3,500 | About $3,500 + 85,000 × $1 ≈ $88,500 | 15,000 versions included per instance |
| Azure vault, software RSA | about $0 | about $0 | about $0 | Vault throughput (section 4) |
| Azure vault, HSM RSA-2048 | ≤ $1,000 | ≤ $10,000 | ≤ $100,000 (only keys used in 30 days are billed) | Same |
| Azure Managed HSM | $2,336 (1 pool) | $4,672 (2 pools) | $46,720 (20 pools) | 5,000 keys per pool and 5 pools per subscription per region, so 25,000 keys before splitting subscriptions ([limits](https://learn.microsoft.com/en-us/azure/key-vault/general/service-limits)) |
| AWS CloudHSM, 2 HSMs per cluster | $2,803 | $2,803 | About $16,819 (6 clusters) | 16,666 keys per cluster. Default 4 clusters and 6 HSMs, adjustable ([CloudHSM quotas](https://docs.aws.amazon.com/cloudhsm/latest/userguide/limits.html)) |
| OVHcloud software / HSM | €60 / €1,000 | €600 / €10,000 | €6,000 / €100,000 | **Unverified**: rate limits |
| Scaleway software / HSM | €60 / €850 | €600 / €8,500 | €6,000 / €85,000 | HSM in Paris only |
| OpenBao transit | Infrastructure only | Same | Same | **Unverified** |

**Request cost formula, not an estimate:** `unwraps per month × $0.03 / 10,000`. At the 1,000 unwraps/s worst case from section 4, that is about 2.6 billion requests, or about $7,900 per month. With longer TTLs or fewer active orgs, it falls proportionally.

**Provisioning rate caps migration.** AWS CreateKey is limited to 5/s, so 100,000 keys take at least about 5.6 hours ([request quotas](https://docs.aws.amazon.com/kms/latest/developerguide/requests-per-second.html)). Azure allows 10 HSM-key creates per 10 s per vault ([service limits](https://learn.microsoft.com/en-us/azure/key-vault/general/service-limits)).

## 6. Revocation and destruction

This is the core of the per-org kill switch. Each option has three timings:

1. **Revoke:** use stops, and the change can be undone.
2. **Destroy:** irreversible at the API.
3. **Last copy gone:** the longest-surviving copy of the key material is gone. Only then is every database backup permanently unreadable.

| Option | Revoke | Destroy | Last copy gone | Source |
|---|---|---|---|---|
| A. Status quo | n/a | Live row deleted only | Never for backups already written. Bounded only by backup retention. | `docs/adr/encryption-model.md, line 284` |
| AWS KMS | Disable: "almost immediate (subject to eventual consistency)" | After a 7 to 30 day waiting period, plus up to 24 h. Cannot be used while pending. | **Unverified.** No AWS statement on purging deleted material from AWS's own backups. | [Deleting keys](https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html), [unusable keys](https://docs.aws.amazon.com/kms/latest/developerguide/unusable-kms-keys.html) |
| AWS KMS, imported material | `DeleteImportedKeyMaterial` or expiry makes the key unusable right away. Re-importing reverses it. | Deleting the KMS key itself is irreversible. Symmetric ciphertext can never be decrypted again, even with the same material. | Depends on the importer's copy. If an escrow copy exists, only deleting the KMS key (7 to 30 days) makes the material useless for that key's ciphertext. | [Deleting keys](https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html), [importing](https://docs.aws.amazon.com/kms/latest/developerguide/importing-keys.html) |
| GCP Cloud KMS | Disable is reversible. A version scheduled for destruction cannot be used. | After the scheduled period: default 30 days, minimum 24 h, maximum 120 days, fixed at key creation. Import-only keys may use 0. | "within 45 days" of destruction from "both active systems and data center backups" | [Key states](https://docs.cloud.google.com/kms/docs/key-states), [destroy and restore](https://docs.cloud.google.com/kms/docs/destroy-restore) |
| Azure Key Vault | Disable. Deletion soft-deletes for 7 to 90 days, default 90, set at vault creation. | Purge is "immediate and irrecoverable", unless purge protection is on, which forces the full retention period. | **Not final if a backup blob exists.** A key backed up and restored elsewhere "remains fully functional". **Unverified:** Microsoft-internal purge timing. | [Soft delete](https://learn.microsoft.com/en-us/azure/key-vault/general/soft-delete-overview), [encryption at rest](https://learn.microsoft.com/en-us/azure/security/fundamentals/encryption-atrest) |
| Azure Managed HSM | As above | As above | A purged key can be brought back by a "selective key restore" from a full backup in the customer's storage account. The last copy is gone only when those backups are. **Unverified:** Managed HSM soft-delete range. | [Managed HSM backup and restore](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/backup-restore) |
| AWS CloudHSM | **Unverified** | **Unverified** for the live cluster | Cluster backups keep it. Retention is 7 to 379 days, and the last backup is never deleted. A deleted backup stays restorable for 7 more days. "AWS KMS does not delete the key material from cluster backups." | [Backup retention](https://docs.aws.amazon.com/cloudhsm/latest/userguide/manage-backup-retention.html), [delete backups](https://docs.aws.amazon.com/cloudhsm/latest/userguide/delete-restore-backup.html), [Deleting keys](https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html) |
| Scaleway | **Unverified** | 7-day pending deletion, then "permanently deleted" | **Unverified** for internal backups | [Scaleway FAQ](https://github.com/scaleway/docs-content/blob/08390debab5db2224c8dd635f1dc7c0feae9dfb7/pages/key-manager/faq.mdx) |
| OVHcloud | **Unverified** | **Unverified** | **Unverified** | [OVHcloud KMS](https://www.ovhcloud.com/en-ie/identity-security-operations/key-management-service/) states none |
| OpenBao / Vault transit | Soft delete (reversible), or raise `min_decryption_version` | `DELETE` with `deletion_allowed`, or `trim` older versions | Every Raft snapshot taken before the delete restores it. The last copy goes only when the snapshot retention runs out. | [OpenBao transit API](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/website/content/docs/api/secret/transit.mdx), [raft snapshot](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/website/content/docs/commands/operator/raft.mdx) |
| AWS XKS | Customer disconnects or denies at the proxy. "AWS loses all access … until you restore it." | Customer deletes the external key. "All ciphertext … becomes unrecoverable." Ciphertext left in AWS is "effectively crypto-shredded". | Governed by the customer's HSM backups. | [AWS XKS](https://docs.aws.amazon.com/kms/latest/developerguide/keystore-external.html) |
| GCP EKM | Customer revokes Google's permission in the EKM | Destroying either the Cloud EKM version or the external key is enough: "If you lose access to either key, your data cannot be recovered." | Customer's EKM | [Cloud EKM](https://docs.cloud.google.com/kms/docs/ekm) |
| Azure Managed HSM EKM | Customer disables at the proxy or HSM | Customer destroys the KEK | Customer's HSM. Managed HSM backup and restore of external keys is not supported. | [Azure EKM](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/external-key-management-overview) |
| F. Customer key in their own cloud account | The customer edits the key policy or disables the key. Both the key policy and Hikyo's IAM policy are needed. | As for that cloud's KMS | As for that cloud's KMS | [AWS cross-account](https://docs.aws.amazon.com/kms/latest/developerguide/key-policy-modifying-external-accounts.html) |

**Add Hikyo's own cache TTL to every "revoke" figure**, because a cached unwrapped key keeps working until it expires (section 4).

**Pattern across all options:** for any key held on Hikyo's side, time to full shredding is bounded by the longest of three things: the vendor's deletion delay, the vendor's own backup purge, and the retention of every backup Hikyo or the vendor keeps of that key store. A separate key store with short backup retention can therefore make shredding happen sooner than with the database's own retention. It does not make it instant.

## 7. Availability when the key service is down

| Option | Behaviour | Source |
|---|---|---|
| AWS KMS | SLA 99.999% monthly uptime per region. **Custom key stores (CloudHSM and XKS) are excluded.** | [AWS KMS SLA](https://aws.amazon.com/kms/sla/) |
| GCP Cloud KMS / Cloud HSM | SLA 99.99% for service-account operations, 99.95% for end-user operations | [GCP KMS SLA](https://cloud.google.com/kms/sla) |
| Azure Key Vault | SLA 99.99% (raised in 2021). **Unverified:** the Managed HSM percentage. | [Azure update](https://azure.microsoft.com/en-gb/updates/akv-sla-raised-to-9999/) |
| AWS XKS (customer HSM) | "You are responsible for the availability, durability, and latency of key operations." AWS warns that for most customers, "the greater risk to availability and latency will … exceed the perceived security benefits". | [AWS XKS](https://docs.aws.amazon.com/kms/latest/developerguide/keystore-external.html) |
| GCP EKM | An unavailable external key returns `FAILED_PRECONDITION`. Google support "can't directly resolve issues on external systems". | [Cloud EKM](https://docs.cloud.google.com/kms/docs/ekm) |
| Azure EKM | "No end-to-end Microsoft SLA for external keys." Azure calls it "a last-resort option". | [Azure EKM](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/external-key-management-overview) |
| OpenBao / Vault | Hikyo operates it. A sealed or unreachable cluster blocks every unwrap. | n/a |

What this means for Hikyo:

- **With a per-org external key**, an outage longer than the cache TTL makes that org unreadable. For a customer-held key (F or G), it is the customer's outage that breaks the customer's org. Hikyo needs a defined error state for this, and must keep it from blocking other orgs.
- **With an external root key**, an outage blocks boot and `rotate-root-key` only, because the root is used at boot and at rotation and is then zeroed (`docs/adr/encryption-model.md, line 310`). A node that is already running keeps serving. A node restarting during the outage cannot start.
- **Restore needs the key service.** This is the "KMS gone means data gone, backups included" failure mode the ADR already records (line 286).

## 8. Escrow

- **AWS KMS-generated keys and GCP keys cannot be escrowed**, because nothing can be exported (section 3). The only route to escrow is to import material Hikyo or the customer generated, and keep the original.
  - AWS lists keeping "the original copy … outside of AWS for additional durability" as a reason to import ([importing](https://docs.aws.amazon.com/kms/latest/developerguide/importing-keys.html)).
  - GCP can re-import a destroyed imported version from the same original ([destroy and restore](https://docs.cloud.google.com/kms/docs/destroy-restore)).
- **Escrow and crypto-shredding pull against each other.** An escrow copy is a surviving copy. Shredding then needs either the escrow destroyed too, or a cloud-side key that cannot be recreated. AWS symmetric keys qualify: a new key with the same material cannot decrypt the old ciphertext ([deleting keys](https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html)).
- **Azure:** Key Vault backup blobs and Managed HSM full backups act as escrow within the same geography and security domain ([service limits](https://learn.microsoft.com/en-us/azure/key-vault/general/service-limits), [Managed HSM backup](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/backup-restore)).
- **OpenBao transit:** `allow_plaintext_backup` plus `/transit/backup/:name` gives a plaintext export that can never be disabled. `byok-export` wraps a key to another OpenBao instance ([transit API](https://github.com/openbao/openbao/blob/92de51f7a0586759215d299bc79118874ea54fb1/website/content/docs/api/secret/transit.mdx)).
- **HYOK (G):** escrow is the customer's responsibility. Hikyo cannot help them recover.
- **Hikyo's existing decision 8** already means Sealed orgs have no Hikyo-side escrow. A per-org external key does not change that. It adds one more customer-held or Hikyo-held dependency that must exist at restore time.

## 9. Region pinning (EU)

- **AWS KMS:** keys are regional, and "key usage can be isolated within an AWS Region" ([design goals](https://docs.aws.amazon.com/kms/latest/cryptographic-details/design-goals.html)). Multi-Region keys replicate on purpose ([deleting keys](https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html)). The **AWS European Sovereign Cloud** offers KMS. CloudHSM key stores and ML-KEM are not available there ([ESC KMS differences](https://docs.aws.eu/esc/latest/userguide/kms.html)). XKS works in every KMS region except China, and AWS recommends a round-trip time of 35 ms or less to the external manager ([AWS XKS](https://docs.aws.amazon.com/kms/latest/developerguide/keystore-external.html)).
- **GCP:**
  - "Key material for Cloud KMS and Cloud HSM keys is confined to the selected region."
  - Regions: europe-west1, 3, 4, 9 and 10 list full Cloud HSM support. Several more EU regions, plus the `europe` multi-region, list multi-tenant HSM only. **Unverified:** single-tenant support in those five regions is inferred from the Cloud HSM column. The page says only that single-tenant is "available in limited regions".
  - EKM over VPC is regional only ([locations](https://docs.cloud.google.com/kms/docs/locations)).
- **Azure:** Managed HSM "doesn't store/process customer data outside the region" ([Managed HSM overview](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/overview)). Key Vault backup blobs restore only within the same geography ([service limits](https://learn.microsoft.com/en-us/azure/key-vault/general/service-limits)). EKM is available in all Azure public regions ([Azure EKM](https://learn.microsoft.com/en-us/azure/key-vault/managed-hsm/external-key-management-overview)).
- **OVHcloud:** "keys never leave your jurisdiction". The page names no specific locations ([OVHcloud KMS](https://www.ovhcloud.com/en-ie/identity-security-operations/key-management-service/)).
- **Scaleway:** HSM-backed keys are in Paris (FR-PAR) only ([Scaleway FAQ](https://github.com/scaleway/docs-content/blob/08390debab5db2224c8dd635f1dc7c0feae9dfb7/pages/key-manager/faq.mdx)).
- **OpenBao:** placed wherever Hikyo runs it.

## 10. Customer-held keys (F, G, H) in more detail

- **F. Customer key in their own cloud KMS.** The customer creates a key in their AWS account and grants Hikyo's account use of it. Both the key policy and an IAM policy on Hikyo's side are required. Hikyo's account bears the request quota ([AWS cross-account](https://docs.aws.amazon.com/kms/latest/developerguide/key-policy-modifying-external-accounts.html), [request quotas](https://docs.aws.amazon.com/kms/latest/developerguide/requests-per-second.html)). On GCP, billing goes "to the project that owns the key", so the customer pays ([GCP pricing](https://cloud.google.com/kms/pricing)).
  - Hikyo would need a client for each cloud, plus cross-cloud credentials. The encryption-model ADR names this as the cost it declined in v1 (line 161).
- **G. HYOK.**
  - AWS documents XKS keys for client-side encryption, including the AWS Encryption SDK, so an application can call them ([AWS XKS](https://docs.aws.amazon.com/kms/latest/developerguide/keystore-external.html)). **Unverified** for GCP EKM: the page read does not say whether an application can call those keys directly.
  - AWS XKS uses double encryption: neither AWS nor the external key owner "can decrypt double-encrypted ciphertext alone". A key deleted from XKS cannot be recreated, even with the same external material ([AWS XKS](https://docs.aws.amazon.com/kms/latest/developerguide/keystore-external.html)).
  - **Unverified:** whether an application such as Hikyo can call Azure Managed HSM EKM keys directly. Microsoft documents it for "Azure services that support CMKs with Managed HSM".
- **H. Imported material.** Gives an instant, reversible kill switch on AWS: delete the imported material, or let it expire. Shredding becomes permanent by deleting the KMS key after 7 to 30 days ([importing](https://docs.aws.amazon.com/kms/latest/developerguide/importing-keys.html), [deleting keys](https://docs.aws.amazon.com/kms/latest/developerguide/deleting-keys.html)).

## 11. Shapes for the per-org wrap

These are trade-offs, not a recommendation. They assume the format change the ADR requires (line 50).

1. **Org key only.** The org's keys are wrapped only by its external key.
   - Gain: no installation key reads the org.
   - Cost: the org depends fully on that key's availability, and Hikyo's root-key escrow cannot recover it.
2. **Nested under the master.** The org's keys are wrapped by the master, and the result is wrapped again by the org's external key.
   - Gain: destroying the external key shreds the org in every backup once the last copy is gone.
   - Cost: a root-key holder still needs that org's external key to read it, which is the intent. Availability is as in shape 1.
3. **Either key opens it.** The master or the external key alone can unwrap.
   - Gain: availability.
   - Cost: destroying the external key shreds nothing, because the master path stays in every backup. This defeats the kill switch.

**Root key alone.** External custody of the root (one key, one unwrap at boot) costs about $1 to $3 per month on any KMS. It improves access control and audit of the root. As section 1 notes, it does not remove the single key that affects all orgs, and it does not split `sealedSlotKey`.

## 12. What this means for the decision

1. **The per-org kill switch and "no single key affecting all orgs" are two requirements.** External custody of the root key meets neither. Only per-org external material in shape 1 or 2 meets the kill switch. Even then, the master and `sealedSlotKey` remain installation-wide unless the Sealed protocol specification changes them.
2. **No option shreds instantly across backups.** The fastest documented end-to-end bounds:
   - GCP: 24 h minimum schedule (0 for import-only), plus 45 days to purge Google's backups.
   - AWS KMS: 7 days minimum, and AWS's internal purge is **Unverified**.
   - Customer HYOK: the timing depends on the customer's own HSM backups.

   Revocation, which is reversible, is near-instant everywhere, plus Hikyo's cache TTL.
3. **At 100,000 orgs, the monthly cost of holding the keys differs by option** (section 5, before requests and rotation):
   - AWS KMS: $100,000.
   - GCP HSM: $100,000.
   - Azure Premium HSM: up to $100,000.
   - GCP single-tenant HSM: about $88,500.
   - Azure Managed HSM: about $46,700, split across subscriptions.
   - AWS CloudHSM: about $16,800, needs quota increases.
   - Software tiers: about $6,000 on GCP, OVHcloud or Scaleway, and roughly nothing on an Azure Standard vault.

   Throughput limits differ more than prices do (section 4).
4. **Self-run OpenBao transit moves the backup problem rather than solving it.** Its snapshots contain every transit key. Its benefit is that its snapshot retention can be set shorter than the database's. External Keys (OpenBao 2.7.0) can push the material into an HSM, and then the HSM's backup policy governs.
5. **Customer-held keys (F, G) make the customer's key service an availability dependency of their org.** Every cloud vendor warns about this for HYOK. Hikyo would need to define behaviour for outages lasting minutes and days, and decide what support can do.
6. **Escrow and shredding conflict for any key Hikyo can recover.** If escrow is wanted, it needs a documented destruction path for the escrow copy, or a cloud-side key that cannot be recreated (AWS symmetric).
7. **Existing ADR sections would need revisiting.** Encryption-model § *Erasure* asks for this itself (line 286). The per-scope KMS format note (line 50) and the root-key-only KMS seam (line 161) would also be affected.

## Not verified

- Per-call unwrap latency for AWS KMS, Google Cloud KMS, Azure Key Vault, OVHcloud, Scaleway and OpenBao. No vendor publishes one, except the AWS XKS 250 ms timeout.
- How many quota tokens one GCP unwrap costs.
- AWS and Microsoft statements on purging deleted keys from their own internal backups.
- Azure Managed HSM SLA percentage and soft-delete range. Azure vault oct-HSM pricing, which is in preview.
- Whether an application can call Azure Managed HSM EKM keys directly.
- OVHcloud deletion semantics and rate limits. Scaleway export.
- The label of the $0.15 column on the GCP pricing page.
- OpenBao infrastructure cost, and Vault Enterprise Managed Keys pricing.
- Whether GCP key material can move between protection levels. Which EU regions have GCP single-tenant HSM, inferred only from the Cloud HSM column. Whether an application can call GCP EKM keys directly.
- The live-cluster delete semantics of AWS CloudHSM, and Scaleway revocation.
