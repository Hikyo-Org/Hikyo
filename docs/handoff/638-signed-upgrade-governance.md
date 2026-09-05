# Signed upgrade governance, #638

The user explicitly authorized recommended decisions, implementation and merge on green without further questions. The signed graph was already the maintainer-selected option in #638. The normative decision is [signed-upgrade-compatibility.md](../adr/signed-upgrade-compatibility.md); its amendments preserve every historical owning ADR body.

Native Codex `gpt-5.6-sol`, high effort, reviewed the affected Claude-authored locked decisions in three rounds. [Round 1](638-review/round-1.md) found legacy unsafe rollback, missing structural HA fencing, readable-backup custody, nightly/recovery trust and Flux authority gaps. [Round 2](638-review/round-2.md) verified fixes and identified recovery-edge precedence, signed nightly inventory and exact Flux patch admission gaps. [Round 3](638-review/round-3.md) returned SOUND, with legacy runtime retirement as the remaining prerequisite. Review paths reference the original research draft; its reviewed content is now the canonical ADR, with only its status and retirement wording changed.

The same PR retires that runtime before the decision becomes operative. The [retirement handoff](638-legacy-updater-retirement.md) records exact behavior and tests. This is governance and removal of unsafe execution, not completed compatibility implementation or a 1.0-ready claim.

## Owning issues and amendments

- #8: trust classes, target verification, restore custody, writer fencing and absence of deployment credentials.
- #18: external bounded host helper, retired legacy Compose scripts, full-stop schema migration and no automatic post-write restore.
- #19: separate Flux pull controller, exact repository/patch admission, full-stop sequencing, direct Helm automation future.
- #22: signed graph, manifest binding, target admission, durable applied/pending ledger on both engines.
- #26: compatibility foundations mandatory before 1.0; platform application stays disabled until acceptance.
- #32: mandatory readable backup evidence, durable maintenance, pre-write-only automatic rollback, explicit restore reconciliation.
- #33: stable/nightly/recovery release authorities, compatibility release gate and retirement prerequisite.

Each owning issue receives the exact scope and a link to #638. Implementation tickets are filed only once this governance PR merges. Their intended order is artifact/trust and route selection, datastore ledger and migration gate, maintenance/restore acceptance, then separately gated platform automation. The first three contain mandatory 1.0 foundations; platform automation is not enabled by their completion.

The [HTML report](../reports/1.0/upgrade-governance.html) records questions, alternatives and delegated decisions. Parent final checks and merge SHA are recorded in #638 after exact-head CI passes.

Parent canonical docs verification passed with required public analytics configuration: static build, policy, PWA/offline browser, analytics and fallback notification fixtures. Parent verified the seven amendment banners add no historical-body deletions.
