# Glossary

This file gives each Hikyo term one meaning. Use these terms in specs, ADRs, UI copy and documentation.

The owner settled round 1 on 2026-11-19 and rounds 2 and 3 on 2026-11-27 in [#1334](https://github.com/Hikyo-Org/Hikyo/issues/1334).

A retired word is a word that Hikyo's own copy no longer uses for that meaning. Locked ADRs and shipped code can still contain retired words. Read them with the meaning that this glossary gives. Shipped identifiers follow these terms. A renamed identifier keeps its old name as a deprecated alias until v2.

## Settled terms (round 1)

| Term | Meaning | Retired words |
|---|---|---|
| **Instance** | One running Hikyo service: its datastore, its root key and its server nodes. | "installation", and "deployment" for this meaning |
| **Organisation**, **org** | The tenant boundary. On the hosted service, an org is also the billing customer. | "organization" in Hikyo's own copy |
| **Protection mode** | A property of an org. The value is **Standard** or **Sealed**. | None |
| **Standard** | The protection mode in which the server can decrypt the org's values with the root key. | "Managed" |
| **Sealed** | The protection mode in which the server needs a holder key to decrypt the org's values. "Sealed", with a capital S, has only this meaning. | "Sealed" for anything that is not the protection mode |
| **Switch to Standard** | The act of leaving Sealed mode. | "unseal" |
| **Encrypted webhook** | The adapter that pushes values over HTTPS, encrypted to a receiver. Outside this term, user copy writes "encrypted", not "sealed". | "sealed webhook" |
| **Key holder** | A person, a file or a machine credential that can open slots. | bare "holder" |
| **Holder key** | The secret that a key holder presents to open a slot. | "holder secret", "presented key" |
| **Hoster** | The organisation that runs an instance for other organisations. | None |
| **Instance administrator** | The in-product role that holds instance-scope grants. | "super admin" |
| **Production operator** | A person with production access to an instance. | "super admin", and bare "operator" for a person |

## Settled terms (round 2)

| Term | Meaning | Retired words |
|---|---|---|
| **`instance-admin`** | The role template for an instance administrator: crypto custody plus instance member management. The v1 API still carries it as `operator`. | the `operator` template |
| **Members with a value** | The members who have set their own copy of a personal value. | "holders" for personal values |
| **Cut off** | The reversible action, and the org state, in which the org's custody keys are disabled so nothing in the org can be decrypted. | None |
| **Shred** | The irreversible action of destroying keys so the data they protect can never be read again, for an org or a project. | "crypto-shred" |
| **Shred pending** | The org state during the window in which a shred can still be cancelled. The org stays cut off. | "shredding" |
| **Shredded** | The final org state after a shred. | None |
| **Key required** | The result a member gets when they hold the grant but not the key. | "locked", "locked result" |
| **Org key** | The one key per org that wraps every slot in the org and encrypts its adapter credentials. | "slot key", "org slot key" |
| **Custody key** | One of the external KMS keys that wrap an org key. Cutting off disables them; shredding destroys them. | "KMS key" for this meaning |
| **Epoch key** | A short-lived transit key pair that is deleted to give forward secrecy. | None |
| **Instance key holder** | The key holder that lets the instance open an environment for unattended access. | "installation holder" |
| **Master key** | The instance's key that wraps each org key. | None |
| **Root key** | The top key of an instance. | "root key file" |
| **Recovery key** | An org's key holder of last resort, tied to its instance. | "recovery code" (an MFA term) |
| **Support grant** | Time-limited, read-only access to one org's metadata, given to one named instance administrator. | "time-limited grant" |
| **Tripwire** | Any signal allowed to trigger an automatic cut-off. | None |
| **Tripwire value** | An opt-in value whose read alerts every admin of the org. | "tripwire slot" |
| **Chain head** | The latest hash of an org's audit chain. | None |
| **Tamper detector** | The service that checks audit copies against chain heads and key-service logs. | bare "detector" |
| **Canary org** | A hoster-run Sealed org whose every key use outside a drill raises an alarm. | None |
| **Honeytoken** | A fake credential held in a canary org whose use proves data left the instance. | None |
| **Production access feed** | The record, visible to every org, of each production session: who, why, start and end. | None |

### Notes on the settled terms

- **Instance and region.** The regions ticket decides how an instance relates to a region.
- **Instance trail.** The audit trail of one instance. The name agrees with "instance administrator".
- **Third-party names.** A third-party name keeps its own spelling, for example a GitHub organization.
- **Managed configuration.** "Managed" left the protection mode because it collides with managed configuration.
- **Operator as software.** "Operator" still names software, for example the Kubernetes operator. It does not name a person.
- **Identifiers.** Renamed identifiers, for example `sealed-webhook`, `HIKYO_SEALED_WEBHOOK_FILE` and the `operator` template, are accepted as deprecated aliases until v2. New output shows only the new names.
- **Org lifecycle.** Whether a suspended org is its own state is decided by the org lifecycle ticket.

## Settled terms (round 3)

| Term | Meaning | Retired words |
|---|---|---|
| **Floor** | The oldest state a server or client still accepts; anything older is refused as a rollback. | None |
| **Org checkpoint** | The signed per-org record that commits to all of an org's security state and environment records. | "org head" |
| **Environment record** | The per-environment entry an org checkpoint commits to: key version, key holders, slots, values and rotation due. | "state record" |
| **Rotation due** | The environment state, after a key holder leaves, in which nothing new is encrypted under the old key until a key holder rotates it. | "forced rotation" |
| **Restore epoch** | A signed record that starts a new timeline after a restore, so older checkpoints are refused. | None |
| **Genesis entry** | The first evidence-store entry for an org, written before its first change. | None |
| **Evidence store** | The off-instance write-once store that holds audit copies, chain heads and floors. | "off-host store", "anchor store" |
| **Evidence store unavailable** | The refusal state when the evidence store has been unreachable for too long. | None |
| **Key service unavailable** | The refusal state when the custody keys have been unreachable for too long. | None |
