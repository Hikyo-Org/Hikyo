# Handoff: #1133 ML-DSA private certificate lifecycle draft

Entry point: [proposed PKI amendment](../adr/pki-ml-dsa.md). Issue:
[#1133](https://github.com/Hikyo-Org/Hikyo/issues/1133), parent
[#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123).

## Delivered scope and contract

The deliverable is a non-operative policy/lifecycle draft and a current-source
consumer map. It supplies the bounded planning allowed by #1133 while its
prerequisites remain unresolved. It does not implement ML-DSA or satisfy the
feature acceptance checklist. The accepted [PKI ADR](../adr/pki.md) remains
authoritative and links to the proposed amendment without changing its policy.

Target: Hikyo-Org/Hikyo. Phase: draft documentation only. Authorized endpoint:
signed and DCO-signed-off commits, push, draft PR and review follow-up. Merge,
issue closure, release/publication, deployment, production key/trust activation,
provider configuration and external private messages are outside this task.
No rendered UI change or screenshot evidence is involved.

## Source and prerequisite snapshot

On 2026-11-01 canonical main read back as
`de0d8866d050c6e729bde76e2960eb6a260f1e3a`, matching the implementation base.
The issue's older assessed revision is not substituted for that source pin.
#1133, #1123, #1124 and #1127 bodies/comments were read; no accepted ML-DSA
profile or named prototype was supplied by those records. #1124 and #1127
were OPEN with their acceptance checklists unresolved. Recheck their current
decisions and behavior before continuing; open/closed state alone is not proof.

The open PR inventory and an all-state #1133 PR search found no existing
ticket implementation at the start of this draft. The unrelated maintenance,
Helm and release-documentation/trust PRs retain their owners and are not bases
or substitutes for the PKI prerequisite decisions.

| Current source owner | Verified baseline and eventual affected work |
| --- | --- |
| [Key policy and fingerprints](../../internal/pki/keys.go) | Six classical leaf algorithms; narrower four-algorithm CA set. Parse/generate/classify are closed. SPKI and certificate fingerprints use `sha256:` plus base64url digest; any change needs approved identity compatibility |
| [Signing and validation](../../internal/pki/sign.go), [PEM/PKCS#8](../../internal/pki/pem.go) | Maintained `crypto/x509` calls for certificates, CSR proof of possession, key binding, chain and CRL operations; private-key import classification must agree with approved CA policy |
| [Profile policy](../../internal/pki/policy.go), [profile service](../../internal/service/pki_profiles.go) | Closed algorithms/usages, SAN/name/TTL checks and decidable narrowing; adding an algorithm cannot be smuggled through an update |
| [Issuer service](../../internal/service/pki.go), [certificate service](../../internal/service/pki_certificates.go) | Protected sealed PKCS#8, instance-DEK fencing, pending/install/rotation, environment authority, two-phase issuance and issuer fence, current-profile renewal and display-once leaf key |
| [Re-encryption](../../internal/service/reencrypt.go), [restore](../../internal/store/authn/restore.go) | `pki_issuers` is registered with the existing AAD/versioned re-sealer; restored issuers are held, including compatibility with pre-PKI archives |
| [SQLite schema](../../internal/store/migrations/sqlite/00064_pki.sql), [PostgreSQL schema](../../internal/store/migrations/postgres/00064_pki.sql) | `key_algorithm TEXT NOT NULL`, not a closed database algorithm enum. State/custody/uniqueness constraints and public history remain binding; no migration is justified before the approved data model |
| [SQLite queries](../../internal/store/queries/sqlite/pki_repository.sql), [PostgreSQL queries](../../internal/store/queries/postgres/pki_repository.sql), [row adapters](../../internal/store/pki_store_rows.go) | Generated query consumers preserve raw algorithm strings; inspect both engines and regenerate from query/schema owners if the accepted implementation changes them |
| [Worker](../../internal/service/pki_worker.go), [runtime](../../internal/store/pki_runtime.go), [app worker](../../internal/app/pki.go) | Unknown/expiry transitions, CRL snapshots, restore/maintenance exclusion, CAS publication and multi-node execution must cover the new algorithm |
| [OpenAPI](../../api/openapi.yaml), [API mapping](../../internal/server/pki.go), [request admission](../../internal/server/api.go) | Closed create/rotate/profile/issue enums and size limits; generated Go API and TS types/Zod/SDK derive from the schema. Shared 2 MiB request bound remains separate from output/worker limits |
| [CLI](../../internal/cli/pki.go), [web API](../../web/src/api/pki.ts), [issuer UI](../../web/src/routes/PkiIssuersPanel.tsx), [profiles UI](../../web/src/routes/PkiProfilesPanel.tsx), [certificates UI](../../web/src/routes/CertificatesTab.tsx) | CLI help/private-key input bounds, typed unions, selectors and default examples are classical. Enable only accepted choices together with generated clients and consumers; future rendered changes require matching before/after evidence |
| [Operator guide](../site/src/content/docs/docs/private-pki.mdx) | Existing offline-root and CRL distribution procedures need exact-version qualification before new algorithm commands can be documented as supported |

This is a PKI-scoped map, not the whole-product cryptographic inventory owned
by #1124. It does not change Transit, backup formats, pairing, SSH CA or TLS
policy; #1127 owns the transport implementation used for later integration.

## Validation and evidence limits

The complete draft diff contains Markdown only. No TypeScript, Python, Go,
SQL, generated client or runtime behavior is changed. Apply documentation
checks to this range, not unrelated language lint/test suites. Use the current
[contributor instructions](../../CONTRIBUTING.md#local-verification) and
[CI guide](../development/ci.md) when the scope changes.

From the repository root, inspect selection before executing feedback:

```sh
FAST_FEEDBACK_BASE=de0d8866d050c6e729bde76e2960eb6a260f1e3a scripts/ci/fast-feedback.sh --working-tree --plan
FAST_FEEDBACK_BASE=de0d8866d050c6e729bde76e2960eb6a260f1e3a scripts/ci/fast-feedback.sh --working-tree
```

Expected for this documentation range: no executable owner checks selected;
feedback still checks whitespace and must exit zero. Inspect relative source
links and draft/acceptance wording as well. The feedback planner does not select
the published-site validator for these ADR/handoff paths, and the site's
content preparation does not publish these files. This is not a full docs-site
build or feature test.

Candidate identity is the signed draft PR head and tree; retain the exact
base/head/tree, complete diff manifest, local check receipts and independent
Standards/Spec review in the PR evidence and retained review packet. A later
repair invalidates prior final-candidate review and needs fresh validation.
An artifact digest or candidate SHA must be observed, never predicted inside
the commit that creates it.

| Evidence class | Disposition for this deliverable |
| --- | --- |
| Current-source/prerequisite inspection | Performed at the baseline above; no accepted prerequisite evidence found |
| Local documentation checks | Record actual final-range results in the draft PR; no result is asserted here ahead of execution |
| Independent model review | Required: Codex Astra High, Standards and Spec over exact final range; final receipt belongs with the PR candidate |
| Hosted CI | Record actual candidate status separately in the PR; draft delivery does not imply a passing full suite |
| ML-DSA keys, certificates, CRLs, TLS, benchmarks and both-engine lifecycle | NOTRUN; no executable behavior was introduced |
| Policy/security acceptance | HOLD: #1124 decisions, #1127 accepted behavior and scoped maintainer security approval |
| Merge, publication, deployment, production trust | NOTRUN, outside authorization |

Existing classical regression starting points include
[core chain/CSR/CRL tests](../../internal/pki/pki_test.go),
[lifecycle](../../internal/isolation/pki_e2e_test.go),
[real CLI/restart](../../internal/isolation/pki_cli_e2e_test.go),
[stored DEK versions](../../internal/isolation/pki_stored_versions_test.go),
[restore/CRL hold](../../internal/isolation/pki_restore_crl_hold_test.go),
[retirement coverage](../../internal/isolation/pki_crl_coverage_test.go),
[SAN corruption](../../internal/isolation/pki_san_corruption_test.go),
[query scope/CAS](../../internal/isolation/pki_generated_queries_test.go),
[retained history](../../internal/isolation/pki_parent_retention_test.go) and
[renewal bounds](../../internal/isolation/pki_renewal_ttl_test.go).
Their existence does not establish execution in this draft or PQ coverage.
The amendment maps each AC to additional evidence required after approval.

## Next authorized step and retention

1. The #1124 policy owner and maintainer security reviewer accept concrete
   P1-P5 decisions, with immutable evidence; the #1127 owner demonstrates and
   obtains acceptance for P6. The #1133 owner then lands the scoped accepted
   amendment before dependent algorithm changes. Unresolved decisions keep
   implementation on HOLD.
2. Re-pin source and PR ownership, implement only the accepted PKI delta and
   extend owner/consumer fixtures through both persistence engines. Independently
   validate the exact final candidate and keep the maintainer security gate.
3. Qualify the disposable lifecycle, TLS, migration, rollback and benchmark
   matrix before making support claims. Pass its exact receipts to #1135;
   external-stack dispositions belong to #1134. Production use still needs
   named environment, key/trust custody and separate authorization.

No issuer keys, databases, containers, runtime services or production trust are
created by this draft. Retain its active branch/worktree and ignored local
review evidence while review is pending. Future fixtures must track exact
resource ownership and teardown, keep secrets protected, and preserve keys,
trust/CRLs and archives required by the approved rollback/retention contract.
