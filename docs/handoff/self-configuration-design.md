# PR #686: Hikyo self-configuration handoff

Status: scope expanded by the user's D11 correction. The earlier nine-key implementation passed CI at `152373212c6d78a3bcae91e3300097ff0d893acf`. The user now requires every Hikyo variable with remote Apply, only by the target instance administrator using passkey or TOTP. Full-variable activation is not implemented. Do not merge the earlier implementation as if it satisfies this revision. No merge or production deployment has occurred.

## D11 scope expansion

The complete [implementation design](../spec/self-configuration-full-catalogue.md) records the application-generation and managed deployment integration requirements. All 65 recognized environment inputs have metadata in `internal/config/variables.go`; the report generator consumes `variable-inventory.json`. This inventory does not expand the runtime catalogue, which still has nine keys.

The current changes close an MFA gap on ordinary protected-project operations, navigation and capability hints. Exact Apply/adoption/test evidence is restricted to fresh local TOTP or user-verifying passkeys, with the existing owner/revision binding and single-use consumption. Targeted both-engine, race, and independent review evidence belongs in the report validation record.

The next implementation work is the full-catalogue design's application lifecycle and bootstrap integration. Required seams include node overlays, canonical content settings, graph prepare/activate/dispose, exact plan binding, deployment-provider authority and receipts, and recoverable root/datastore transitions. A provider stub or inventory alone does not complete D11. The user delegated design recommendations; do not ask them to reapprove this scope.


## Integration evidence

Merged current main `72f5a71f` while preserving its account-profile and adapter
findings work. The unreleased managed-configuration migration is now 50; the
historical recovery cutoff and legacy archive fixture match. Full core checks,
focused both-engine authorization/audit/formula checks, Go vet, all 919 web tests,
SDK checks and fresh desktop local/independent-owner browser journeys pass.
The pinned Go formatter fixes the initial CI format failure. Exact-head CI is
recorded on PR #686.

## CI follow-up corrections

Browser closure now executes all four required configuration assertion runs.
Approval scheduler claims retain a provisional fence on commit error and require
exact renewal before any job can run; deterministic both-engine and race tests
cover lost responses, rollback, successors and shutdown. Schema fan-out avoids
repeated project-storage sums using attempt-local exact accounting; every
environment retains the existing high-water check. Three alternating local
100,000-cell benchmarks improved median time by 15.21%. CI floor limits and
lease/takeover timings remain unchanged. See the report validation record for
measurements and exact failure history.

## User intent and authority

Hikyo should provision an organization/project during setup that manages its own environment variables and secrets. Changing email settings should be possible through the existing interface, followed by applying changes without restarting the container.

The user explicitly chose UI application and approved instance-admin control of the system organization. The user then authorized the remaining design recommendations: "I trust your instincts, assume I agree with the coming recommendations." The user subsequently clarified separate configuration for independent remote instances, with sharing only among HA replicas of one logical instance. All 27 decisions are resolved. The root management view is a delegated interpretation, not an explicit request for a central secret store. Do not restart the interview or ask for approval of those same decisions again.

The user subsequently instructed "Lgtm, build it", authorizing implementation. Continue through validation and signed delivery without reopening the design interview. Merge and production rollout remain separate actions.

## Read first

1. [Standalone report](../reports/self-configuration/index.html), including D01 through D27. Each contains alternatives, recommendation, decision, rationale, sources and acceptance proof.
2. [Design summary](../spec/self-configuration-proposal.md) and [structured decision data](../reports/self-configuration/report-data.json).
3. [Report validation](../reports/self-configuration/validation.md) and [report build/access instructions](../reports/self-configuration/README.md).

## Important selected boundaries

- Each independent owner has a real `Hikyo / <instance-name> / Production` hierarchy with a stable owner-instance/org/project/environment binding and normal project encryption/matrix workflow. One root management view aggregates references without storing remote values. First-admin setup followed by atomic provisioning; explicit existing-instance adoption. No reseeding on restart and no claiming an unrelated tenant on a name collision.
- Instance-config is a mandatory conjunct for the bound hierarchy. Retain normal scoped capabilities and separate reveal/history grants, enforced by the owning instance; viewer-admin rights do not confer remote rights. The runtime uses a narrow registered internal system site, not a reusable bearer token or arbitrary tenant reader.
- First delivery: complete mail configuration plus update notification channel. External DB/root-key bootstrap and existing networking, authentication, backup/audit, HA identity and client controls remain outside. Managed mail file inputs are imported once as contents; remote apply cannot read arbitrary paths.
- Draft, publish and apply are separate. Exact-revision preparation precedes a durable generation commit. Atomic local bundle swaps, acknowledgements only from that owner’s HA replicas and stale-consumer fencing follow. Independent remotes never share variables, generations or apply jobs. Before-commit rejection preserves old target; after-commit failure is pending/partial and reconciles. No claim of instantaneous global atomicity and no automatic SMTP-triggered rollback.
- Bounded runtime retention roots protect target, active and recovery snapshots. Normal rollback restores and publishes a new revision. Host-local recovery is explicit and audited. Restore fences outbound use until reconciliation and explicit credential resumption.

## Implementation dependencies and sequence

Baseline inspected: `90b4ca6a5d22438e751cf9af83aa4fd077a6a61c`. [#608](https://github.com/Hikyo-Org/Hikyo/issues/608), the mailer/local sign-up ticket, was open on 2026-09-06; there was no mail library dependency in this checkout. Implementation adds the shared mail transport and test sink seam used by managed configuration. Local sign-up remains outside this change.

The five implementation milestones define the delivered scope and its verification:

1. Amend owning contracts and add the system-resource authority profile and fixtures.
2. Add both-engine durable binding/generation/retention storage and shared provisioning/adoption.
3. Integrate mail transport, immutable runtime activation, HA, status API, human CLI and host-local recovery.
4. Add matrix/settings workflow and discoverable operations documentation, with browser proof.
5. Verify failures, concurrent operations, revocation, GC, key rotation, restart, two-node HA, two independent remotes with different variables and restored-state fencing before normal exact-head delivery gates.

The existing ADRs remain canonical until amended. Preserve the multi-instance contract: owner-local secrets, direct browser-to-remote workspace calls, metadata-only directory credentials and no central main server. D26 records the explicit remote/HA distinction; D27 selects a root UI group rather than a shared physical organization. In particular, tenant-isolation's system mint sites are closed, normal pins have a maximum 365-day lifetime, and the current mailer spec assumes process-owned credentials that are not covered by managed-secret restore/re-encryption. The report selects explicit changes to these contracts; it does not pretend that existing code already supports them.

## Runtime acceptance

Use a TLS test SMTP sink, never live recipient mail, for automated proof. Through the browser: setup/adopt, edit SMTP, publish, test the exact selected revision, apply, observe every admitted node, and verify subsequent sends use the new configuration while process start times remain unchanged. Restart must resume the durable target. Reject invalid configuration, show failed delivery without automatic credential rollback, recover partial HA safely and preserve secret confidentiality throughout. Apply and rollback on one owner must leave all other remote projects untouched, even when names or revision numbers match. Loss of the viewing instance must not interrupt remote boot or runtime operation.
