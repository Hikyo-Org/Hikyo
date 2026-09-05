# #527 roadmap truthfulness refresh

Base: `0d591366874ad525b60ca12938dfd0ef2efe12d4`. Worktree `/tmp/hikyo-roadmap`, branch `docs/one-zero-roadmap`. No commit/push; parent owns signed delivery and #527 body publication.

Canonical #527 body/comments read first. The body falsely said #79 alone/unblocked, optional browser parity and social registration in 1.0. Owner comment 5531906184 explicitly moves social registration post-1.0 and requires #617. Root's all-issue assessment informed required supported-product fixes; labels/milestones were not used as scope authority. The task is coordination/docs, not a missing runtime implementation.

Added `/docs/roadmap/` with navigation and dated merged/pending/release-evidence/post-1.0 distinctions. At the source snapshot #642/#643 are merged and #644-650 are open; #652/#653 are also recorded as pending delivery. the page labels that historical snapshot and links live PR state. Homepage stale HA/dynamic future clocks became implemented-on-main checks linked to actual guides; rotation is post-1.0 with no invented minor version. Competitor claims and their assessment date remain untouched.

Copied `/tmp/hikyo-mcp-ticket-audit.{md,html}` into `docs/reports/1.0/mcp-ticket-audit.{md,html}` and replaced draft-only narration with actual #631 revision/#651 creation. No OAuth implementation, public deployment or live-client proof is claimed. Removed internal parent/assistant wording and fixed the detailed-record link.

[Decision report](../reports/1.0/roadmap.html), [rendered assertions](../reports/1.0/roadmap-evidence/checks.json), and four desktop/mobile screenshots are included. Full `fnm exec --using 26.7.0 scripts/ci/verify-docs.sh` passed: ledger fixtures, peers, Astro 0 diagnostics, 40-page build, policy/PWA/offline browser and disclosure/fallback fixture gates. Additional real Chromium assertions for roadmap links/overflow and homepage capability labels passed desktop/mobile with no page errors. `git diff --check` passed. Package locks unchanged.

Publication-ready issue content is [527-roadmap-issue-body.md](527-roadmap-issue-body.md). Replace #527's stale body as part of this delivery; retain it as the ongoing execution-map tracker rather than treating roadmap maintenance as closure of #79/#41. If merge state changes before publication, update the dated snapshot or preserve its clearly labelled historical boundary. Do not invent final-candidate acceptance, migration-graph runtime or stable-tag proof.

Review R1 addressed: mandatory signed-upgrade foundations are explicit before 1.0, separate from governance/refusal; pending provider and client-skew PRs #652/#653 are included. Historical main/merge snapshot remains labelled.
