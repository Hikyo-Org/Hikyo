# Hikyo hosted service: shared installations and Sealed orgs (ADR)

Status: **proposed. Direction decided by the owner on 2026-11-04, and the governance disposition is recorded in the [review record](#review-record). Not locked until the owner confirms resolution 9 in the [author resolutions](#author-resolutions).** Each decision becomes operative when its implementation PR merges. Nothing here describes shipped behavior.

**Conflict of interest, disclosed under [GOVERNANCE.md](../../GOVERNANCE.md):** the maintainer intends to sell the hosted service this ADR enables.

**Supersedes, for the sections named in the [supersession register](#supersession-register-normative), a set of locked ADRs.** Each carries a banner pointing here. This follows the [flat-model.md](./flat-model.md) form: a new ADR plus a normative register. Where this ADR and a named section conflict, this ADR wins once operative. Everything the register does not name stays in force.

**Advances** [mvp-boundary.md](./mvp-boundary.md) section 3, which commits to a hosted follow-on after 1.0. v1.0.0 shipped 2026-11-01. Billing remains undecided, and stays out of the core.

Context: the locked ADRs describe one trusted operator, 1 to 3 orgs, and a full stop for schema upgrades. A paid hosted service breaks all three. It serves many unrelated customers, the owner must not be able to read their secrets, and an upgrade outage reaches every customer. The owner chose a shared PostgreSQL-backed deployment that scales horizontally, rather than a separate instance per customer.

## Vocabulary

- **Installation**: the existing term for one datastore, one root key and its server nodes. This ADR avoids "pod", which already has a meaning in the [k8s-integration ADR](./k8s-integration.md).
- **Org**: unchanged. The tenant boundary and the billing customer.
- **Protection mode**: a property of an org, either **Managed** or **Sealed**.
- **Environment key**: the data key protecting one environment's secret material in a Sealed org.
- **Slot**: one stored copy of an environment key, wrapped for one holder.
- **Holder**: a person, file or machine credential able to open slots.
- **Unattended access**: an optional state of one environment in a Sealed org, in which a holder has added an installation holder so that the server can open that environment without a presented secret (decision 11).

## Decisions

### 1. Topology: many orgs per installation, more installations at the ceiling

The hosted service is a normal Hikyo HA installation on PostgreSQL, starting with 2 to 3 server nodes on one database cluster, and serving many orgs. Nodes scale horizontally. The primary database scales vertically. When an installation nears its ceiling, the service adds another independent installation instead of stretching the first.

An org lives on exactly one installation, and installations share nothing. A person with orgs on two installations has two logins. Both are accepted costs.

This bounds the blast radius of a bad release or datastore failure, lets a release reach one installation first, and makes write capacity grow by adding installations.

Hosted code may only schedule and operate released artifacts through documented public interfaces, as [GOVERNANCE.md](../../GOVERNANCE.md) and the [oss-mechanics](./oss-mechanics.md) self-hoster test require. Directing a new customer to an installation means sending them to that installation's public signup. Every capability below ships in the open-source server. Only configuration differs.

### 2. Protection mode is fixed at org creation

| | Managed | Sealed |
|---|---|---|
| Who can decrypt at rest | The server, with the root key | Only with a presented holder secret, except in an environment with unattended access |
| Key for secret material | Project DEK, exactly as today | Environment key |
| Features needing background decryption | Available | Refused, except adapter sync and federation in an environment with unattended access (decision 11) |
| Instance grants reach the org | Yes, as today | Lifecycle only (decision 6) |

An instance setting chooses what new orgs get:

| Setting value | New org | Default for |
|---|---|---|
| `managed-default` | Managed. The creator may choose Sealed | Self-hosted |
| `sealed-default` | Sealed. The creator may choose Managed | Optional |
| `sealed-only` | Sealed, with no choice | The hosted service |

The setting applies only when an org is created. Changing it never alters an existing org. The protected `Hikyo` system org that holds self-configuration is always Managed and is exempt.

**Sealed is enforced by keys, not by a flag.** An environment in a Sealed org has no server slot unless a holder adds one (decision 11). Without one, if it never had one or has rotated since, changing the setting, self-granting, using break-glass, reading the datastore or holding a backup and the root key together yields none of that environment's plaintext. The project sealer refuses a Sealed org, and the store refuses any update to the mode column, on both engines.

**Sealed protects data at rest and against application-level access. It does not stop capture from production or tampering.** An operator cannot read a Sealed environment that has never had unattended access, or has rotated since, at rest, or through grants or application settings. Anyone with production access can still get in: capturing holder secrets in transit or in memory, by logging at TLS termination, a memory dump or a modified server, or writing to the live datastore to forge the mode or a holder's public key, and waiting for the customer's next write, grant or rotation. This is more work and a deliberate act, but this design does not prevent it. An operator able to ship a server can do this regardless, so a protocol defending only the datastore path would add much complexity for little gain against the operator. The owner accepted this on 2026-11-04: as with any hosted service that decrypts on the server, customers trust the operator not to capture or tamper. Sealed raises what a breach of that trust takes, from a setting or a grant to deliberately capturing or changing something in production.

**There is no conversion in either direction.** Retained backups hold the old server-wrapped keys, so sealing an existing org would promise something false, and unsealing would need a server slot. A customer who wants a different mode creates a new org and imports, which re-encrypts every value.

The mode appears in `whoami`, in the UI, and in the org's audit trail at creation.

### 3. Environment keys and slots

**This section fixes required properties and a design sketch. A separate Sealed protocol specification, reviewed under gate 9, must fix the exact construction before implementation.**

A Sealed org uses one random 256-bit key per environment. The encryption-model ADR rejected per-environment DEKs because project-default values belonged to no environment. The flat model deleted that layer and every value row now has an environment, so that reason no longer applies. Managed orgs keep project DEKs and need no migration.

The environment key seals `value_entries`, `pending_changes`, `snapshot_entries`, transit key material and SSH CA keys.

**Required properties:**

1. **Both keys.** Opening a slot needs a holder's secret and the installation. An either-key design was rejected, because the root key alone would still read everything. The only exception is the installation holder, which a holder adds on purpose for one environment (decision 11).
2. **Outer-layer rotation needs no customer.** A new tier-3 `sealedSlotKey` with its own purpose, wrapped by the master key, forms the outer layer of every slot. The server can rotate it without any holder. It lies under existing root-key custody and escrow. Losing it destroys every slot in the installation.
3. **Binding on both layers.** The outer layer's AAD and the inner wrap each bind org, project, environment, key version, slot and holder, and the environment key has a key commitment. A root-key holder who strips the outer layer cannot reuse one environment's inner wrap in another's slot.
4. **Holders can be added while absent where listed below**, so that the server can create a new environment without a presented secret: it generates the key, wraps it to the org's recovery holders, the creating member and members holding all of that project's other environments, and discards it.
5. **No stored wrap may be derivable from a human-chosen secret alone.** An operator could otherwise brute-force weak passphrases. A passphrase may protect a file on the customer's own device, as an age passphrase file. It never reaches the server.
6. **No plaintext-derived digest under a server key.** Otherwise a dump plus the root key would let someone test guesses. Each environment has its own change-token key and scanning-fingerprint key under the environment key. Key hygiene therefore causes no rollout wave, and a holder may deliberately replace them, accepting one wave.

**Holders in the sketch:**

| Holder | Inner wrap | Added while absent | On environment key rotation |
|---|---|---|---|
| Recovery | To an X25519 public key. The client generates the pair and sends only the public key | Yes | Re-wrapped |
| Member | To the member's X25519 public key. The private key is unlocked on the member's device by a passkey PRF or a generated member key | Yes | Re-wrapped |
| Service-account token (`wl`, `au`) | Symmetric, under HKDF over the token body with its own label. The token format is unchanged, and the server still stores only the SHA-256 verifier | No. A new or widened scope needs a re-mint with a holder present | **Re-minted** |
| Installation (decision 11) | None. Only the outer layer, under `sealedSlotKey`, with the slot kind bound in the AAD | No. A holder must be present | Re-wrapped by the holder driving the rotation |

**Rotation has a fixed order:** add the new key version, mint new tokens, distribute them, re-encrypt, then retire the old version. An old token can open only the old version, so it fails on rows already moved. Tokens must be replaced before re-encryption starts.

**Rotation alone does not cure a captured holder key.** Its private key must also be replaced.

### 4. The server decrypts during the request

A credential presents its secret with each request needing plaintext. For a person or a recovery key, that secret is the holder's private key, unlocked on their device. The server authorizes first, opens the slot, does the work and discards the key. Nothing is cached across requests, shared between nodes or written to the datastore, so nodes stay stateless.

Secrets arrive in a dedicated header over TLS, never in a URL. They use the redacting types, and the envelope package stays the only caller of a cryptographic primitive.

**A key is required in addition to a grant, never instead of one.** The grant table stays the only access policy. A caller with a grant and no key gets a locked result. A caller with a key and no grant gets the uniform nonexistent result. This narrows the tenant-isolation ADR's rejection of per-request key release: release never authorizes.

**Which credential presents what.** A service-account token's own body is its secret. A federated machine credential has no bearer value, so it works only through unattended access (decision 11). Every other artifact (`cli`, `br`, `ws`, `dev`) carries no slot and must present a holder secret separately.

Client-side decryption was deferred, not rejected. See [Rejected alternatives](#rejected-alternatives).

### 5. What a Sealed org does and refuses

**Every path works with a presented key, works through unattended access where decision 11 allows it, or is refused. No other secret of a Sealed org silently stays readable by the server.** The keyring refuses project sealers, and every server-keyed digest, for a Sealed org, so that an unlisted path fails closed.

| Feature | Sealed behavior |
|---|---|
| Fetch, reveal, stage, publish, pin, revision detail, diff, restore, export, import, transit, SSH CA | Work with a presented key |
| MCP | Works with a service-account token |
| Environment creation | No key needed, unless cloning. A clone needs the source key |
| Copy, compare between environments | Need a key for each environment in one request |
| Schema publish, key delete, rename, reclassify, declassify, group change | **Need keys for every affected environment in one request, or are refused.** A recovery key or a member holding them all can do this. An automation token spanning all environments must be re-minted after a new environment is created |
| Rotating or re-encrypting an environment key | Driven by a holder in batches, each carrying the key. The installation-wide `reencrypt` skips Sealed orgs |
| Backup | Exports ciphertext. A restore drill must prove `sealedSlotKey` by opening a sampled slot's outer layer, and may not report "no secret" when Sealed orgs exist |
| Adapter sync | **Needs unattended access on the target environment** (decision 11), and is refused without it |
| OIDC-federated machine credentials, including Kubernetes ServiceAccount federation | **A fetch needs unattended access on the environment fetched** (decision 11). Fetching an environment without it is refused, and other environments are unaffected. For those, the Kubernetes operator uses a bootstrap Secret holding a token |
| Dynamic-secret providers | **Refused.** They are not a consumer of unattended access at launch |
| Automatic transit key rotation | **Refused.** Rotation needs a holder |
| PKI | **Not offered.** Issuer keys are instance-owned |

**Schema behavior is unchanged.** The schema-model ADR validates and materializes every environment at once, and aborts on any invalid one, with verdicts gated by `reveal`. Requiring all keys keeps those rules as they are. The cost is that a schema editor without keys for all environments gets a locked result.

Self-configuration, upgrade configuration and sign-in factors stay server-held, so taking over an account gains a login and not a key. Names, descriptions, schemas and audit metadata stay unencrypted, as the encryption-model ADR lists.

**On `sealed-only`, adapter sync and Kubernetes workload identity stay available, per environment, through unattended access.** An environment without it delivers through the CLI, the compose integration and the Kubernetes operator with a bootstrap Secret. Keeping such an environment sealed while syncing it needs a customer-run path, which is open item 2.

### 6. The operator's reach into a Sealed org

Instance-scope grants cover only **org lifecycle** for a Sealed org: its existence, plan, limits, usage totals, suspension and deletion. They do not cover its members, projects, environments, keys, names, values, audit trail or settings. An instance grant also cannot add its holder to the org, or approve its own support request.

- **Support access** is a time-limited grant approved by an org administrator, and mirrored to the org's audit trail. It conveys no key.
- **Evidence.** Every instance-scope action on a Sealed org is recorded in that org's trail. A direct datastore edit leaves no such record, so this is evidence and not prevention.
- **Unattended access.** An instance grant or support grant cannot enable it, add a consumer to it or open its slot. Decision 11 lists who can.
- **Slots.** Adding or removing a holder needs a presented holder secret. An org can never remove its way below two holders or one recovery key. If removing a person's membership, including through SCIM, leaves fewer than two usable holders, the org blocks new publishes until it has two again.
- **Remote directory.** Paired remote instances do not see Sealed org names.

Direct datastore access still reveals metadata. Blocking it inside the application does not change that, and the honest statement says so.

### 7. Signup and limits

- A new org is created through the existing registration policy, or the documented instance API. On `sealed-only`, the client generates recovery keys locally before the org is created. A signup that bypasses this is refused, including the `fresh-org` landing without a proof.
- **Limits are set when the org is created**, from the registration policy's default limits. The hosted control plane changes them after a purchase, using a documented instance-scope API. Self-hosters have the same API.
- Limits are stored where an org administrator cannot change them, and enforced in the write transaction: projects, members, environments, keys, service accounts, federation issuers, storage and rates. A limit refuses a write, and never damages data.
- Instance-wide signup rates become configurable.

### 8. Key loss

**Outside unattended access, Hikyo keeps no copy of a customer's secret, and support cannot recover a Sealed org in any case.** To reduce the chance of loss:

- Org creation does not complete until a recovery key has been saved and proven by re-entering it.
- **An org must have two holders before its first publish:** a member or a second recovery key, and may not drop below that. Hikyo can count keys, but cannot prove that two people hold them. An installation holder never counts, and is never used for recovery or by support.
- Credential reset restores login only, and says so.

**Removing a member does not un-know a key.** The product prompts the org to rotate the environment key and the secrets themselves. Rotation also forces machine tokens to be re-minted.

**A recovery key is tied to its installation.** It opens nothing without that installation's `sealedSlotKey`, so it is not an offline copy. A customer who wants one uses `values export`, or moves through a holder-driven export and import.

**Restore has a known cost.** Restore makes every machine credential inert by design, so their slots die with them, and each Sealed org re-mints through a holder. An org-level restore also brings back removed members' slots and retired key versions, so it must prompt rotation. Recovery that avoids a restore is preferred.

### 9. Rolling upgrades are the default on PostgreSQL HA

Kubernetes is a first-class target, and a full stop on a shared installation takes every customer offline. This applies to self-hosted PostgreSQL HA too, not only to the hosted service:

- Every release supports running beside the previous one, as a tested and declared pair.
- Schema changes follow expand and contract. A release may add only what the previous one tolerates, and removal waits for a later release.
- Admission accepts the declared pair, not only one exact schema.
- The chart defaults to `RollingUpdate` under HA. The backup required before a migration must not depend on a stop window that grows with the data.
- A release that cannot meet this declares a full-stop route through the existing durable maintenance procedure.

SQLite and single-node installations keep the full stop. Release identity, signing and recovery rules are unchanged.

### 10. Throughput prerequisites

These apply to all PostgreSQL deployments. Figures come from an unpublished local `pgbench` run against the real v1.0.0 schema and triggers, on 2 CPUs in Docker with a simulated 1 ms commit, whose method is in the [handoff](../handoff/hosted-service-adr.md). They show shape, not capacity.

- **The tenant trail's commit order (`commit_seq`, migration `00011`) becomes per org.** That lock also serializes the two trails' writers, so a replacement must not reintroduce the deadlock it prevents when one transaction writes both, as decision 6's mirrored events will. The current global commit lock held throughput near 500 audited commits per second at 16 and 64 clients, against about 1,700 to 2,350 with a per-org lock.
- **Audit writes must not conflict across orgs.** At SERIALIZABLE, concurrent inserts into the tenant trail failed serialization checks across unrelated orgs. With four attempts, 2% failed at 200 per second and 8% at 400 per second.
- **Rate budgets become installation-wide and configurable**, reusing the shared admission counters transit already uses.
- **The connection path works through a transaction-mode pooler.**
- **Pre-authentication work has an installation-level bound.**
- **Adapter work and federation key refresh are fair per org.** One org's sync backlog cannot delay another's. One org's slow issuers cannot hold shared pre-authentication slots, or evict another org's cached keys and the stale-key protection they give.
- **One org can be exported and restored** without affecting the others.

Read replicas are not a goal. Every machine fetch writes audit rows, so replicas would not relieve the primary.

### 11. Unattended access: adapter sync and workload identity in a Sealed org

The owner requires adapter sync and Kubernetes workload identity on the hosted service. Both need plaintext when no customer is present, so the server must hold a way to open those environments. This decision makes that a visible choice per environment, instead of a silent exception or a whole-org downgrade.

**This is Managed behavior for that environment, and the product says so.** A datastore dump plus the root key opens an environment with unattended access. The mode stays per org, because the operator's reach (decision 6), the keyring's refusal of project sealers and the per-environment digest keys all stay in force for it.

- **Off by default.** Enabling or removing it needs `project-settings(project)`, `reveal(E)`, reauthentication and a presented key for that environment. Machines cannot do it, and a key alone never authorizes it. Enabling lists every adapter target and federated principal that becomes a consumer, and the person confirms them. It then adds an installation holder and records an event in the org's trail. The UI, `whoami` and the environment listing show the state.
- **Fixed consumers.** The installation slot opens only for the adapter worker serving a target on that environment, and for a delivery fetch by a federated principal holding grants there. It never opens for a person, an instance-scope principal, a support grant, a `cli`, `br`, `ws` or `dev` artifact, a service-account token, MCP, or a schema change. So decision 4's rule still holds for everyone else: they present a key or get a locked result.
- **Removal does not seal until rotation.** Removing it deletes the slot, stops its consumers and replaces the environment's digest keys, accepting one rollout wave. Retained backups still hold the old slot, and so the unchanged environment key. Until a holder rotates that key, the environment shows as exposed, and a backup plus the root key can open values published after removal. After rotation, values from before it stay exposed that way. Rotation does not change the values, so the product also prompts replacing the secrets themselves.
- **Restore does not revive it.** After any restore, an installation slot stays inert until it is re-confirmed under the enabling formula, listing its consumers again.

**Changing who receives plaintext needs a key.** Otherwise someone with the right grants and no key, including a taken-over login or a support grant, could send the values to a destination they control or forge a token. Each of these needs, in addition to what the other ADRs require, a presented key for every environment affected:

- Creating an adapter target, and every adapter change that the [permission-model](./permission-model.md) ADR's adapter rules list as changing where plaintext goes: destination or recipient, provider account, outbound credential, environment selection and delivery routing. This includes origin and destination moves, and resuming them.
- Widening an adapter target's keys, name prefix, recipient visibility or provider protection flags. Narrowing, pausing and teardown need no key.
- Creating, replacing or re-activating a federation binding.
- Any grant change that increases what a federated principal receives, including `read`, which delivers every `config` value. The existing machine-widening check does not count `read`, so it cannot simply be reused.
- Turning on machine reveal in a project with federated principals.

**Adapter credentials.** Adapters are project-scoped and seal their outbound credential under the project sealer, which a Sealed org refuses. In a Sealed org, the credential is sealed under `sealedSlotKey`, bound to org, project and adapter, and is exposed to a dump plus the root key. The worker opens it, as do the adapter operations that test or plan against the provider, under their existing authorization. `sealedSlotKey` rotation covers it. Removing unattended access scrubs each target on that environment, or leaves it orphaned if the person explicitly chooses that, and the credential is erased once no target remains.

**Adapter limits on the hosted service:**

- Token-based adapters are available: GitHub Actions, GitLab, Forgejo, Vault KV, Cloudflare, and AWS with static keys.
- `HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY` must stay off, because it would let a tenant act with the server's own AWS identity.
- `HIKYO_ADAPTER_EGRESS_POLICY_FILE` grants no private ranges, because that file is instance-wide. So a destination on a customer's private network is unreachable, until open item 2.
- Sealed webhooks are not offered, because their receivers come from an operator-owned file, `HIKYO_SEALED_WEBHOOK_FILE`.

**Federation issuers become org-owned.** Today `federation_issuers` is instance-scoped, `issuer` is globally unique, and a binding is unique on issuer and subject. That cannot serve unrelated customers: clusters commonly share a default issuer URL, and ServiceAccount subjects like `system:serviceaccount:default:app` repeat between them, so two customers would collide or be confused for each other.

- An org administrator of a Sealed org registers an issuer in that org, unique per org, under a new org-level capability and routes. Managed orgs get neither and keep instance-level issuers, unchanged, so this adds no new authority in a Managed org.
- **On every deployment, a binding in a Sealed org may name only an issuer owned by that org.** Otherwise an instance administrator could edit an instance-level issuer's keys, forge a token and read an unattended environment.
- **A binding is pinned to its issuer's key source.** It records a digest of the key source and CA bundle at mint, and stops if they change. Minting shows the person the current static key fingerprints or the discovery origin. Changing the key source or CA bundle while any binding names the issuer, live or revoked, needs the binding mint formula over every environment those bindings reach (`manage-identities(project)`, `reveal(E)` and reauthentication) and a presented key for each. Otherwise someone could swap the keys, wait for a holder to re-mint a binding that went down, and forge tokens for it.
- A federated request already names its org in the delivery path. A token authenticates only into that org, and only through that org's own binding. **In a Sealed org, a binding's audience must equal `<installation origin>/orgs/<org id>`, checked at mint**, so that two orgs trusting the same public issuer, such as GitHub Actions, cannot use one token for both. A `HikyoInstance` has one audience, so it then serves one org. The existing byte-exact matching stays.
- A private cluster's discovery endpoint is unreachable from the hosted service, so it registers static JWKS, a mode that already exists. **When the cluster's signing keys change, someone must update them under that same formula**, which records the new digest, and the bindings stop until then. Discovery keeps today's egress bounds, with no private-range exceptions.
- Events naming an org-owned issuer go to that org's trail, aggregated under flood. The instance trail keeps counts only.

Dynamic-secret providers, automatic transit rotation and PKI stay out of scope. The owner did not ask for them, and each would need its own review as a consumer.

## The honest statement (documentation requirement)

Hikyo's documentation and hosted marketing MUST state this, and may not imply more.

**Protected in a Sealed org, for environments that have never had unattended access, or have rotated their key since it was removed (a backup from before that rotation, plus the root key, still opens the values it holds):** a stolen datastore or backup, even with the root key; an operator or staff member reading data at rest; instance grants, self-grants and break-glass; a cross-tenant authorization bug reaching values. **These protections cover data at rest and application access. Sealed does not stop anyone with production access who captures secrets or tampers.**

**Not protected in a Sealed org:**

- A modified server, which can capture holder secrets and plaintext from requests, and can retain a key it generated. One hostile release can leave a known key that survives redeploying a clean binary, until every affected environment key, holder key and digest key is replaced.
- Capture of holder secrets or plaintext at TLS termination, by logging or tracing tools, or from server memory, none of which needs a code change. One captured recovery key opens everything it holds until it is replaced.
- Direct write access to the live datastore. It can forge a mode, replace a holder's public key so that a later grant or rotation exposes the data, or roll slots back to an earlier state.
- Metadata such as names and schemas, at the datastore level.
- Anything already delivered to a workload, or synced to an adapter's destination.
- **An environment with unattended access**, and any adapter credential, against a stolen datastore or backup together with the root key, and against an operator reading data at rest. Instance and support grants do not open it through the application. Anyone able to obtain a token for a bound identity, including the issuer or the customer's CI, can fetch it.
- Change tokens seen by a removed member or hostile release can still be used to test guesses, until that environment's digest keys are replaced.
- A customer who loses every holder, whose data is gone, apart from what workloads can still fetch through unattended access. Support cannot recover it either way.

**Never claimed:** zero knowledge. The server sees plaintext during requests. The approved wording is "sealed at rest; we store no key that opens your secret values, except for environments you enable or have enabled for unattended sync or workload identity, and the destination credentials you give us for them". The exception may not be dropped where a customer can have such an environment. "We cannot access your data" is not approved. Keys must not be presented as a tenant access control. Isolation is authorization first, and for Sealed orgs keys add a second, independent requirement on values.

## Rejected alternatives

- **One instance per customer.** Simpler to isolate. Rejected by the owner because of the fleet it creates. It stays available to anyone, since an org can have an installation to itself.
- **One unbounded global installation.** Every customer shares one failure, one write ceiling and one upgrade, with no first-installation canary.
- **An instance setting that hides environments from the operator.** Whoever holds the setting can reverse it. Kept only as the creation default.
- **Mode per environment.** The operator's reach and the refusal of project sealers should stay one rule for the whole org. Unattended access is narrower: one visible holder, with fixed consumers, in an otherwise Sealed org.
- **Refusing adapter sync and workload identity on the hosted service.** Rejected by the owner, who requires both.
- **`sealed-default` on the hosted service, with Managed for customers needing sync.** No new protocol, but a trap: a customer who later needs one sync must re-create the org, because modes do not convert, and every environment and the operator's reach would be affected.
- **A customer-run sync agent as the only path.** It keeps environments sealed, but outbox, ledger and fence logic live in the server, so this is a large refactor, and customers would have to run something. Kept as open item 2.
- **Signed holder lists against datastore tampering.** Review found this needs delegated signing, request-level binding to a verified list and a design for clients that cannot verify. It would not stop a modified server, so it only makes sense alongside client-side decryption or attested hardware.
- **A single symmetric secret per holder.** The server could not add a holder who is absent, including at environment creation, and could not rotate a key without all of them present.
- **Key escrow held by the operator as the default.** That is Managed under another name. Unattended access is that same trade, made by a holder for one environment, and labeled as such.
- **Client-side decryption first.** The strongest option, but it requires reimplementing publish, validation and diff in every client, and a browser still trusts JavaScript the operator serves. Deferred.
- **Confidential VMs as the baseline.** They may later strengthen Sealed against a modified server, but are too costly as a foundation.

## Supersession register (normative)

"Sealed" means the entry applies only to Sealed orgs. "All PostgreSQL" means it binds self-hosted PostgreSQL deployments too. Tickets are the originating tickets recorded for each ADR.

| ADR (ticket) | Superseded | Scope | Unchanged |
|---|---|---|---|
| [threat-model](./threat-model.md) (#8) | "Trusted: the instance operator", narrowed for data at rest and application access. An operator who captures secrets from production or tampers stays operator-equivalent | Sealed | Everything for Managed orgs. A running-server compromise remains full compromise. The "revisit if SaaS" residual stays open (open item 1) |
| [encryption-model](./encryption-model.md) (#14) | Key hierarchy gains environment keys, slots and `sealedSlotKey`. Closed envelope-kind set gains the slot kinds. Primitive set gains X25519 record wrapping. The per-environment DEK rejection. The claim that the server can always decrypt | Sealed | XChaCha20-Poly1305, format, existing kinds, Managed rotation, backups, "never zero knowledge" |
| [tenant-isolation](./tenant-isolation.md) (#23) | Rejection of per-request key release, narrowed by decision 4. "No per-org backup" (all deployments). Documentation rule, per the honest statement | Sealed, except as marked | Proof-carrying authorization, filter-in-query, CI invariants. Row-level security stays rejected pending open item 1 |
| [permission-model](./permission-model.md) (#15) | "Instance scope inherits downward", per decision 6. Instance-scope `org.create` remains, with a signup path per decision 7. Adds the formula to enable unattended access, the key requirements of decision 11, and an org-level issuer capability | Sealed | Grants are the only policy |
| [audit-model](./audit-model.md) (#24) | The tenant commit order spanning orgs (`commit_seq`, from the export-ordering migration), which becomes per org. Instance actions reach the tenant trail | All PostgreSQL; Sealed for visibility; org-owned issuer events per decision 11 | Event shapes, durability, append-only stance, the absence of a cross-trail order |
| [signed-upgrade-compatibility](./signed-upgrade-compatibility.md) (#638) | "Default is full stop on PostgreSQL HA" and the absence of an N-1 window | All PostgreSQL HA | Release identity, signing, custody, restore, maintenance as the declared fallback |
| [system-architecture](./system-architecture.md) (#22) | The 1 to 3 orgs, 25 users, 10,000 entries envelope | Hosted installations | Modular monolith, one process per node |
| [ops-spec](./ops-spec.md) (#32) | Node-local rate budgets and fixed instance-wide values. JWKS bounds and refresh become fair per org | All PostgreSQL | Everything else |
| [schema-model](./schema-model.md) (#12) | Nothing. Decision 5 adds a key requirement, and refuses when keys are missing | None | All of it |
| [machine-identities](./machine-identities.md) (#17) | Machine slots; re-mint on rotation; federation needs unattended access. Issuers are org-owned, bindings are pinned to an issuer's key source, the audience has a required value, and re-activating a binding needs a presented key, per decision 11 | Sealed | Token grammar, verifiers, byte-exact binding, and everything for Managed orgs |
| [k8s-integration](./k8s-integration.md) (#19) | ServiceAccount federation needs unattended access. A private cluster registers static JWKS in its org. A `HikyoInstance` serves one Sealed org, because of the required audience | Sealed | Bootstrap Secret path, operator design |
| [mcp-server](./mcp-server.md) (#627), [mcp-write](./mcp-write.md) (#742) | Tool calls need a service-account token with a slot | Sealed | Everything else |
| [developer-credentials](./developer-credentials.md), [source-of-truth](./source-of-truth.md) (#13) | A `dev` credential must present a holder secret separately. A definitions apply that changes schemas needs keys per decision 5 | Sealed | Everything else |
| [secret-scanning](./secret-scanning.md) (#39) | The scanning fingerprint key is per environment | Sealed | Rules, scanning surfaces |
| [revision-model](./revision-model.md) (#11) | The change-token key is per environment | Sealed | Snapshot identity, token semantics |
| [transit](./transit.md) (#156) | Automatic rotation refused; material under the environment key | Sealed | Everything else |
| [human-auth](./human-auth.md) (#16) | The `fresh-org` landing gains the recovery-key proof of decision 7. Instance-scoped providers stay, pending open item 3 | `sealed-only` | Everything else |
| [api-cli-surface](./api-cli-surface.md) (#25) | Adds the holder-secret header and the locked result. Adds routes for unattended access and org-owned issuers | Sealed | Everything else |
| [multi-instance](./multi-instance.md) | The remote directory omits Sealed org names | Sealed | Everything else |
| [deployment-adapter](./deployment-adapter.md), [github-adapter](./github-adapter.md), [gitlab-adapter](./gitlab-adapter.md), [vault-kv-adapter](./vault-kv-adapter.md) | A target needs unattended access. The outbound credential is sealed under `sealedSlotKey`, per decision 11 | Sealed | Seam, converge, fences, egress bounds, provider behavior |
| [pki](./pki.md) | Nothing in its design. It is unavailable in a Sealed org | None | All of it |

## Author resolutions

The owner's decisions left these open. The owner confirmed them on 2026-11-04, except as marked.

| # | Question | ADR assumes |
|---|---|---|
| 1 | "Community" in the owner's note | Means org. The mode is per org, while keys remain per environment |
| 2 | "For SaaS it should be sealed" | `sealed-only`. No Managed orgs on the hosted service. Corrected by the owner: adapter sync and workload identity must stay available, which resolution 9 covers |
| 3 | "It should stay sealed" | No conversion in either direction. Managed to Sealed is by new org and import |
| 4 | A required second holder | A hard gate before the first publish. A second recovery key counts |
| 5 | Rolling upgrades | Default for all PostgreSQL HA, including self-hosters |
| 6 | "Signup limits" | Per-org quotas set from the registration policy, then changed by the control plane through an instance API |
| 7 | "No access to customer environments" | Includes metadata inside the application. Datastore-level metadata remains exposed |
| 8 | Schema changes in a Sealed org | Need keys for every affected environment, or are refused. This keeps the schema-model rules, but limits who can edit schemas |
| 9 | **Pending owner confirmation.** How adapter sync and workload identity work in a Sealed org | Unattended access per environment, as in decision 11 |

## Open items, each needing its own owner decision

1. **Postgres row-level security.** The tenant-isolation ADR says to revisit it for SaaS. A cross-tenant bug is now far more costly, and Sealed protects values but not metadata.
2. **A customer-run sync and workload-identity path**, so that an environment can be delivered unattended while staying sealed, and can reach a private-network destination. One candidate for Kubernetes: the operator keeps a holder key in the cluster, federation authenticates, and the key opens the slot.
3. **Per-org identity providers.** A customer on a shared installation will expect its own SSO. The human-auth ADR makes providers instance-scoped on purpose, because an org-scoped provider could mint identities across the instance, so this needs a design that keeps that closed.
4. **Browser unlock:** the exact passkey PRF and generated-key flows, and how long a browser retains a key.

## Acceptance gates

No hosted service may accept paying customers until all of these hold:

1. A datastore dump plus the root key yields no plaintext from a Sealed environment that has never had unattended access, or from a dump taken after its key was rotated following removal, and allows no offline guess testing of plaintext through server-keyed digests, tested on both engines.
2. Instance grants, self-grants and break-glass grants yield no values and no org-owned metadata through the application, including from an environment with unattended access. Attempts are visible in the org's trail.
3. The keyring refuses project sealers and server-keyed digests for a Sealed org. The store refuses a mode update. No path creates an installation holder without a presented holder secret and an audit event. Enforced by test or analyzer.
4. Every refusal in decision 5 is a clear error with no partial effect.
5. Customer secrets never reach logs, audit payloads, metrics or error text, extending the existing redaction and dump-grep invariants to the service's own ingress and tracing configuration.
6. Moving a slot, inner wrap or sealed ciphertext across orgs, projects, environments, key versions, holders or slots fails to decrypt, even for a root-key holder. An installation slot is outside the root-key part of this gate, because a root-key holder can open it.
7. A multi-node load test on a real multi-org installation meets a declared target, with no cross-org audit failures at that load.
8. A rolling upgrade across a declared pair completes under load, within a declared error budget.
9. The Sealed protocol specification exists, an independent cryptographic review of it and of decisions 3 to 5 and 11 is complete, and its findings are resolved.
10. An installation slot opens only for its listed consumers, every change listed in decision 11, including an issuer key source change, is refused without a presented key, a binding stops when its issuer's key source changes, and a restored slot stays inert until re-confirmed under the enabling formula. A token authenticates only into the org its request names, through that org's own binding. A binding in a Sealed org cannot name an instance-level issuer, and its audience must equal the required value. Enforced by test.

## Review record

Independent adversarial reviews by `claude-opus-5-5` ran on 2026-11-04. Round 1 (Standards, Spec) found two critical errors: symmetric slots could not be added for an absent holder, and a datastore writer could forge the mode, defeating the claim in decision 2. Round 2 found one critical error: forged holder keys could make a later rotation expose existing data. It also found the deferred schema validation that was then removed. Round 3 found that a signed manifest added in response could not hold: it had no signer for routine changes, and the server was not bound to it. That revision withdrew the manifest and narrowed the security claim to passive access.

The owner then required adapter sync and workload identity on the hosted service, which added decision 11. Round 4 reviewed that decision and the narrowed claim, and found three critical gaps: re-pointing an adapter and changing an issuer's keys needed no key, and an instance-level issuer could back a Sealed org. It also found that the claim was overstated, because holder secrets can be captured in transit with no code change. Round 5 re-checked the fixes and found that a keyless issuer change could still lead to a forged binding, and that shipping org-owned issuers to Managed orgs would add a forging path there. This revision pins bindings to the issuer's key source and limits org-owned issuers to Sealed orgs. Round 6 confirmed those fixes and found one remaining false claim: a backup from before a rotation still opens its values. This revision states that. The owner's confirmation of decision 11 remains open, as resolution 9.

**Owner waiver (2026-11-04):** the owner waived reopening the originating tickets and cross-provider review for this ADR. Cross-provider review has **not run**, and is not recorded as clean. [GOVERNANCE.md](../../GOVERNANCE.md) requires both and has no waiver clause, so this follows the owner waivers recorded in other ADRs, such as [machine-identities](./machine-identities.md). The owner is also the party with the conflict of interest disclosed above.
