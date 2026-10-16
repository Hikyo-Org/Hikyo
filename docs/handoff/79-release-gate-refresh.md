# #79 release-gate documentation refresh

Refs #79. Preparation source: `6769ee55dcd4087eeaf42547e9abde01c4f0818c`.
This handoff does not claim final candidate acceptance, a freeze, stable trust
activation, publication or deployment. #79's release checkbox remains open;
parent #41 is not modified or closed.

## Behavior and ownership

- The release plan now separates component merge evidence, combined-main
  acceptance, external proof, public trust preparation and human ceremonies.
  Current workflow names and the exact-main gate replace retired sequencing.
- Acceptance and self-hoster documents retain the September source/results
  under historical headings, with an explicit pending current-candidate layer.
  No criterion, test reference or assertion is removed or weakened.
- API/CLI documentation records revision 11, 395 operations, 12 extensible-enum
  declarations and completed source disposition of the two previously flagged
  response enums. Counts are as-of-source, pending #805 and final refresh;
  fixture presence is not execution proof. Engine migration source head is 75.
- #527's overlapping release-plan/acceptance draft was read and reconciled into
  this owner-controlled update; its original outside-Git patch is retained by
  the parent. #527 owns roadmap/sequencing, #15 capability ledger/generated
  README and #589 A7. Their files/criterion row were not edited here.
- The #651 owner supplies one exact-image deployment/client evidence package.
  #79 independently checks it against the common final candidate instead of
  launching another MCP fixture. No implicit OAuth or support waiver is added.

- #41's amended inventory is reconciled against current source. C-ACC is added
  as CURRENT-CANDIDATE-PENDING; all original 49 historical criterion rows remain
  unchanged. HA/dynamic/PKI/SSH/transit and provider-specific conditional scope
  now appear explicitly. AWS #158 authority conflict is sent to its owner;
  the recorded label correction flags conditional relevance and a decision,
  without resolving merge/review/title/milestone gates. No product implementation
  is duplicated.
- Parent extended this scope to the acceptance inventory test: both regexes
  now recognize uppercase named C-criteria while the owning ADR set constrains
  membership. The baseline-negative and repaired-positive Go regression passed
  on 2026-10-16; no release PASS follows from inventory closure.
- Full Node/web tests may build Go at fixture import unless compatible
  `HIKYO_E2E_OIDC_BINARY` is supplied. The prerequisite was relayed to the parent
  for future resource scheduling; no full web suite was run.

## Evidence and limits

GitHub independently reports PR #937 merged at 2026-10-16T16:40:31Z as
`6769ee55dcd4087eeaf42547e9abde01c4f0818c`, the branch base. Its
[run 38929885843](https://github.com/Hikyo-Org/Hikyo/actions/runs/38929885843)
has 48 successful jobs and event `merge_group`. This is not the separate
exact-main CI selected by `scripts/release/require-green-main.sh` and does not
refresh all external or artifact acceptance. Other owners' pending PRs are not
polled or declared merged by this refresh.

Source inspection checked operation/minimum-revision counts, migration filenames,
open-enum declarations/consumer fixtures, workflow names/inputs and ceremony
flags/ordering. Public policy proposal generation is described but not executed;
no private key was accessed. Source/docs editing is the authorized phase.
Heavy builds/tests, generators, browser execution and workflow dispatch remain
held. Only the explicitly allocated standalone stdlib Go microcheck ran. Static
diff/reference checks and independent source-review outcomes are recorded in
the PR; any unrun check stays explicitly pending.

Independent GPT-6.1 Sol high source review R2 found no actionable findings in
the full six-file draft. The owner independently inspected the named-criterion
regexes, expected owning-ADR membership and C-ACC source/ledger rows. This is
source review; executed regression evidence is recorded below. The parent subsequently
requested removal of temporary scheduling language from canonical operator
instructions; that editorial correction preserves the OIDC prerequisite and
all candidate/release gates. No full validation or release PASS is claimed.

## Executed inventory regression, 2026-10-16

From the `api` directory, ran only the standalone standard-library test file:

```sh
GOMAXPROCS=2 go test -p=1 acceptance_ledger_test.go -run '^TestReleaseAcceptanceLedgerReferences$' -count=1
```

The updated extractor against the base commit's historical ledger failed with
`owning criterion C-ACC missing from release ledger` (exit 1, package 0.445 s).
A `finally` block restored the exact current ledger bytes, then the same test
passed (exit 0, package 0.172 s). The per-invocation timeout remained 120 seconds;
no application packages or other Go checks ran. The allocated micro slot was
released immediately after completion at 2026-10-16T17:20:18Z.

| Evidence input | SHA-256 |
| --- | --- |
| Updated test file | `26df1b578b201c078afb1773bc3829c3fdf45d92a7a111d071a0763a43065902` |
| Historical missing-C-ACC ledger | `489d42eb90beefdb793e2d4b49d3ed9e0294a64cb2f3bd5f9b5fca3b7cc15ad1` |
| Restored current ledger | `0fac927ad7398bea1815c5e5d46190a5ab6b3574a32687b9b88f7ebb7b686904` |
| Baseline stdout | `0af877097fe289ac1b9aaaf232645815e1c9e64bd63c86b0879d50d8f3a1fcf7` |
| Repaired stdout | `6867be38136cdc2e025c323c640265e0a1b8ac14235649d930e926ff12c6e453` |

Raw stdout/stderr and hash-bound JSON are retained as ephemeral owner evidence;
there is no portable committed location for those local artifacts. Both stderr
streams were empty. This proves inventory closure and referenced Go declaration
names, not execution of those referenced acceptance suites or release readiness.

The read-only documentation status checker passed with 35 ledger entries.
Its first attempt could not load the absent local `zod` dependency; rerunning
with the already-installed exact pinned version 4.4.3 supplied through Node's
module search path passed, without installing or changing dependencies. This
was a status check only; the full docs build/browser suite was not run.
`git diff --check`, relative-link existence and added-text portable-path checks
also passed.

## Remaining delivery and release gates

Complete the scoped source review and permitted checks, then signed+DCO commit,
full-range signature/DCO and path-portability verification, push/GitHub Verified,
issue-linked PR, review repairs and protected independently verified main merge.
Parent owns watch notifications. No automatic issue-closing keyword is used.

After all required implementation and trust changes, select one clean combined
main SHA and execute the full release plan under scheduled resources. Reassert
native recovery/custody/resource proof, both-engine and browser/client coverage,
self-hoster journeys, live providers, #651 support evidence and real disclosure
receipts. Recovery-policy activation, the freeze/tag and publication require
separate explicit owner authorization even after this documentation PR merges.
