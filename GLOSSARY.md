# Glossary

This file gives each Hikyo term one meaning. Use these terms in specs, ADRs, UI copy and documentation.

The owner settled round 1 on 2026-11-19 in [#1334](https://github.com/Hikyo-Org/Hikyo/issues/1334). Round 2 is still open. The [open terms](#open-terms-round-2) section lists it.

A retired word is a word that Hikyo's own copy no longer uses for that meaning. Locked ADRs and shipped code can still contain retired words. Read them with the meaning that this glossary gives. This glossary does not rename code identifiers. A later change renames the shipped identifiers.

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

### Notes on the settled terms

- **Instance and region.** The regions ticket decides how an instance relates to a region.
- **Instance trail.** The audit trail of one instance. The name agrees with "instance administrator".
- **Third-party names.** A third-party name keeps its own spelling, for example a GitHub organization.
- **Managed configuration.** "Managed" left the protection mode because it collides with managed configuration.
- **Operator as software.** "Operator" still names software, for example the Kubernetes operator. It does not name a person.
- **Identifiers.** The shipped sealed webhook keeps its identifiers, for example `HIKYO_SEALED_WEBHOOK_FILE`, until the rename change merges. Key names such as `sealedSlotKey` and `holderTransitKey` stay until round 2 settles them. The `operator` role template keeps its name.

## Open terms (round 2)

These terms are not settled. Do not treat a working name below as final.

| Area | Working names | Source |
|---|---|---|
| Org states | **cut off** (reversible, KMS keys disabled), **shred** and **shredded** (irreversible, KMS keys destroyed), **locked** | [#1315](https://github.com/Hikyo-Org/Hikyo/issues/1315) |
| Key names | `sealedSlotKey`, `holderTransitKey` | [#1314](https://github.com/Hikyo-Org/Hikyo/issues/1314) |
| Support access | the support grant | [#1316](https://github.com/Hikyo-Org/Hikyo/issues/1316) |
| Breach detection | tripwire slot, chain head, detector, production access feed | [#1317](https://github.com/Hikyo-Org/Hikyo/issues/1317) |
| Rollback protection | floor, org head, state record, rotation due, restore epoch, genesis entry | [#1345](https://github.com/Hikyo-Org/Hikyo/issues/1345) |
