# Handoff: hosted vocabulary (#1334)

## Result

The owner settled the hosted vocabulary in three rounds between 2026-11-19 and 2026-11-27. [GLOSSARY.md](../../GLOSSARY.md) at the repo root is the source of truth. This change applies the terms to shipped identifiers, user copy, the ADRs and the spec.

- **Instance** replaces "installation". "Organisation" is spelled with an s in Hikyo's own copy.
- The protection modes are **Standard** and **Sealed**. "Sealed" means only the protection mode. The sealed webhook is now the **encrypted webhook**.
- **Key holder** and **holder key** replace "holder", "holder secret" and "presented key". **Hoster**, **instance administrator** and **production operator** replace "super admin" and bare "operator" for a person.
- Org states, keys, support, detection and rollback terms are in rounds 2 and 3 of the glossary.

## Compatibility (v1 API frozen)

| Identifier | Code, CLI and UI name | v1 wire and stored value | Until |
|---|---|---|---|
| Adapter provider | `encrypted-webhook`, accepted on input | `sealed-webhook` | v2 |
| Env var | `HIKYO_ENCRYPTED_WEBHOOK_FILE` | `HIKYO_SEALED_WEBHOOK_FILE` is accepted with a warning. Startup fails if the two variables differ. | v2 |
| Role template | `instance-admin`, label "Instance administrator" | `operator` | v2 |
| Personal-value field | `members` | `holders` is still emitted. The schema names `PersonalValueHolder` and `PersonalValueHolderList` are kept. | v2 |

The API freeze guard now compares against the newest stable v1 tag instead of v1.0.0. The old baseline let a v1.1 schema rename pass.

## ADR amendment

Each ADR and spec file this change touches carries one dated terminology banner. The banner records that no decision changed. The owner authorized this in place of the cross-model review that GOVERNANCE.md requires. The governance ticket should record it as a deviation. The sealed webhook spec and runbook moved to `docs/spec/encrypted-webhook.md` and `docs/operations/encrypted-webhook-v2-migration.md`.

## Not verified

- The PostgreSQL tests, the full `internal/app` and `internal/isolation` suites, Playwright and the docs-site build were not run locally. Hosted CI covers them.

## Remaining work

1. v2: switch the wire values to `encrypted-webhook` and `instance-admin`, and drop `holders` and the old env var.
2. Some uses of bare "operator" for a person remain in older ADRs, such as "an operator's own create" in the permission-model ADR. They were left because the meaning was ambiguous. Rewrite them when those ADRs are next amended.
3. Some internal names still carry the old words: the store and sqlc `Holders` names, and the audit event `personal_value.holders_read`. The audit event is a wire value.
