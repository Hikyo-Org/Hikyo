# #15 member-access amendment review reconciliation

Snapshot: 2026-10-16 at c4c626a105d0139fcc8a6f5444e585e7a4e0a761.

## Scope and review state

The owner made the member-access amendment operative with
[#838](https://github.com/Hikyo-Org/Hikyo/pull/838). The
[October 16 reopening](https://github.com/Hikyo-Org/Hikyo/issues/15#issuecomment-6216662798)
requires formal amendment review or an explicit scoped owner disposition,
reconciliation of the ADR/handoff/ledger, and a linked completion decision.
Reopening does not suspend the operative authorization policy.

## Exact scoped owner disposition

The owner explicitly approved this disposition on 2026-10-16:

> A accept completed Astra-high verification plus independent Sol6.1-high source
> review as #15-only formal review disposition. Preserve skipped quota-gated pass
> and all documented limitations. No new formal provider run, no permission-model
> or general quota-policy change.

This satisfies #15's review-disposition requirement by explicit owner acceptance.
It does not relabel either completed source pass as cross-provider or retroactively
CLEAN, and it does not waive another issue's review gate. No new formal provider
run was launched after that instruction.

The quota-gated formal pass remains skipped: native Codex telemetry at
2026-10-16T16:30:10.101Z showed 19% weekly remaining, below the configured 50%
minimum. Anthropic subscription review was unavailable, and no paid fallback was
used. The original ADR is Claude-authored, so an eligible OpenAI formal review
could have reviewed that target without a provider waiver; later OpenAI-authored
implementation would require separate author-aware routing. That distinction was
investigated, not used to relabel the ordinary source review.

### Completed source reviews and dispositions

Astra high independently inspected the current source, historical audit evidence,
accepted prototype and PR records. Independent GPT-6.1 Sol at high effort reviewed
the same source at `c4c626a105d0139fcc8a6f5444e585e7a4e0a761`. The Astra implementation owner checked
its findings against source and counterevidence:

| Finding | Disposition |
| --- | --- |
| P2: ledger claims Members/Who can are missing and amendment is not operative | Correct implemented behavior and operative status; preserve remaining target slices |
| P2: first-slice limits and Owed list obscure later implementation | Mark historical boundaries and link the October 5 delivery and this reconciliation |
| P3: exact-preset wording differs from shape-filtered labeling | Preserve the approved prototype's behavior and document the wording tension below |
| D11: people-only wording differs from current machine scope-wide cards | Record the later handoff/source qualification; do not redesign the roster |

Neither pass confirmed an exploitable authorization bypass in its inspected
scope. No full-codebase security verdict or new runtime verification follows
from that statement. The owner accepted these documented limitations as part of
the #15-only disposition.

## Current implementation

The [October 5 handoff](members-unified-access-cards.md) supersedes the original
first-slice limits for human rules:

- Members combines legacy scope-wide cards and narrowed human rules. It also
  retains machine scope-wide cards, as the October 5 handoff records. Narrowed
  machine rules remain refused.
- See ignores the key axis; Pin needs every key in the selected environment,
  and project powers require the whole project. Human Edit, Publish,
  Reveal/history, Define keys and Manage access support key selectors.
- Rule delegation proves management containment and independently proves the
  capability being granted, including future environments/keys and exceptions.
  Only legacy org/instance managers retain the unheld-capability exception.
  Assigning See needs management across every key in the selected environments.
- Card replacement authorizes every addition/removal against the original state
  before writing. Definite refusal keeps the prior card; uncertain browser
  results trigger authoritative refresh. Delegated listings show manageable
  rules only and do not present a global access census.
- Publish proves selected keys and coupled group members before publication;
  history/restore checks the addressed key. Existing shared definition changes
  require Define keys and Publish in every project environment. Selected-env
  Define may create an empty ungrouped key; first values require Define and
  Publish at each destination and are staged before explicit publication.
- Rule-based See participates in filtered metadata navigation. It does not
  become mutation authority. Who can...? offers Yes, local Except, missing See
  and collapsed no-rule outcomes over disclosed grants and rules.

Source owners: `internal/domain/rule.go`, `internal/domain/rule_containment.go`,
`internal/authz/rule_delegation.go`, `internal/authz/rule_navigation.go`,
`internal/service/rule_replace.go`, `internal/service/rules.go`,
`internal/service/publish.go`, `web/src/routes/Members.tsx` and
`web/src/routes/accessRules/`.

## Remaining target scope and wording qualifications

The implementation still uses explicit project/environment IDs and folder
paths or stable key IDs. It has no all/future-project rules or narrowed machine
rules. The resolver is client-side. These are existing target-slice boundaries,
not newly discovered missing work required by the October 16 closeout comment.
Legacy broad grants remain additive and retain their reach.

Folder-only selectors match exactly. Nonroot folder exceptions include
subfolders; the empty root exception removes root keys only. A widening move
requires the exact gaining-principal set and audit. Only legacy project member
managers may name/confirm it. A rules-only census may overcount gains when a
legacy grant already supplies access. No policy change is proposed here.

Two wording tensions are retained under the scoped owner disposition. D2's
exact-preset wording differs from the shape-filtered preset naming in approved prototype 6
and current code; omitted powers are disclosed in the editor and actual badges.
D11's people-only wording differs from the later handoff and current machine
scope-wide cards on Members. This reconciliation does not silently change the
approved UI or reinterpret those decisions as implemented without qualification.

## Evidence and limits

Source review inspected selector delegation, root exceptions, atomic changes,
per-key publication/history, widening confirmation and the current UI/test
contracts. No exploitable authorization bypass was confirmed in this limited
review. It is not a full security acceptance verdict.

The October 16 audit's issue comment records passing both-engine rule,
delegation/root, per-key publication/history and atomic-self-change checks plus
100,000 deterministic selector cases. The Astra implementation owner inspected the retained logs and
source: the selector run checked 20,404 affirmative containment implications.
Those local artifacts are not committed evidence and are not required for
navigation from this document. The issue comment is the durable public record;
the tests remain in `internal/domain/rule*_test.go` and
`internal/isolation/*rule*_test.go`. The temporary 100k overlay is not a
permanent committed test suite.

The assigned head differs from the audited 4af540bf377cc1e2a34163d4de150d395bbea5a9
only in documentation. Source/test continuity was checked; the audit was not
rerun in this source review. Documentation checks are recorded below; application
tests and browser flows were not rerun for this documentation-only change.
Historical #856 validation is recorded in its handoff and
[review comment](https://github.com/Hikyo-Org/Hikyo/pull/856#issuecomment-6003374332).
The new browser execution below validates the documentation PWA only. No new
application runtime, PR CI, merge or issue-closure proof is claimed.

## Documentation validation and delivery

Lightweight checks passed with the repository-pinned Node 26.7.0:

- `node scripts/ci/check-doc-status.mjs --write` regenerated both status views.
- `node scripts/ci/check-doc-status.mjs --check` verified all 35 ledger entries.
- `node docs/site/scripts/prepare-content.mjs --check` validated documentation
  content without generating site files.
- Relative links resolved, ledger JSON parsed, and `git diff --check` passed.

The initial status-generator attempt failed because this isolated worktree had
no installed `zod`. The micro-slot run reused an existing read-only installation
of the exact pinned `zod` 4.4.3 through `NODE_PATH`, without installing or building.

After the full docs slot was granted, frozen dependency installation passed with
Node 26.7.0, Corepack 0.35.0 and pnpm 11.24.0. Peer checks and
`pnpm --dir docs/site run verify` passed: Astro check reported zero errors, warnings or
hints; all seven content/PWA unit tests passed; 78 static pages built; policy,
PWA assets, CSP and LLM-index checks passed. The build emitted its Shiki inline-style
CSP advisory; the final CSP gate passed. The documentation-only Chromium
check loaded an unvisited precached route offline and closed its browser/server.
PostHog's check reported analytics disabled because deploy configuration was not
required; this is not live analytics verification. Documentation-status,
OSS-policy, docs-live and fallback-channel fixture suites also passed. The
live-site fixture suite uses controlled responses, not a deployment check.
All validation processes exited; no preview was left running.
The closeout still requires the signed DCO PR, current-head review/CI and a
verified main merge. Link the merged reconciliation on #15 before closing it.
Historical implementation merges and other issues' review exceptions are not
proof of this delivery.
