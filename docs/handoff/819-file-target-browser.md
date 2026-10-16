# File target browser administration (#819)

## Contract and scope

The project `adapters` surface now exposes the five existing file-target
administration operations. `reportFileTarget` remains a client-local delivery
operation. The accepted #164 amendment and #157 selection rules remain intact:
file targets bind an existing workload account and environment, contain no host
path or provider credential, and resolve selection to immutable key IDs at save.
No backend policy, API schema, release or deployment changes are included.

## Implementation

- `web/src/api/fileTargets.ts` uses generated operations and the shared
  session-aware request layer. Query keys include organisation and project.
  Mutation result delivery retires when its form unmounts or its session ends.
- `web/src/routes/FileTargets.tsx` lists environment, workload account, selected
  key names/classifications, target generation, and the last report's state,
  revision, generation, reported/received timestamps and live age. Detail uses
  `getFileTarget` and can be linked with `?file-target=<id>`.
- Reports are explicitly historical client assertions, never verified health.
  Missing reports show unknown health. Different report/target generations are
  called out. There is no invented freshness deadline: the separate Kubernetes
  reporting ADR's deadline depends on a report interval absent from the
  file-target API. The UI never gives file reports a healthy badge.
- Creation requires explicit environment and workload account choices. No
  account, credential, grant or client filesystem configuration is created.
- Editing captures the displayed generation. A 409 preserves the operator's
  draft and blocks resubmission until they cancel, refresh and review again.
  Pending saves block panel dismissal while navigation remains available.
  Each mounted create panel owns its busy guard. All mutation outcomes
  reconcile metadata in the originating browser epoch, even after navigation;
  retired surfaces cannot receive results or navigate on late success.
- Deletion explains why live credentials cause 409, links to Machine access,
  and distinguishes deleting the server target from modifying host files.
- `TargetKeySelection.tsx` is shared with push adapters. Exact names and checked
  IDs are unioned with the server's save-time pattern/classification selection;
  exclusions never remove explicit members. Future keys are not added silently.

## Acceptance evidence

The table records the current local checks. Browser acceptance and delivery
gates remain separate below.

Source inspection reproduced the original gap at base `c4c626a10`: the five
parity rows referenced #819 and the adapters route had no file-target hooks or
UI. All five rows now use `webui: adapters`; reporting retains its exception.

Behavioral tests are written for list/report states, creation, generation
conflicts, deletion refusal/success, project isolation, pending panel dismissal,
integer serialization, retired mutation results and sibling selection controls.
The existing machine-access Playwright flow contains a real-server file-target
lifecycle for desktop and mobile, including a machine report, concurrent edit,
credential refusal, revocation and deletion.

Validation used pinned Node 26.7.0 and pnpm 11.24.0, sequentially under the
parent's resource slot. Frozen client/web dependency installation passed.

| Check | Observed result |
| --- | --- |
| Full web unit suite, one worker | 207 files, 1,836 tests passed in 188.71 seconds after the aggregate repair |
| Expanded focused suite after full run | 6 files, 89 tests passed; includes every 401/403/404 denial followed by network failure and successful recovery |
| Web typecheck and lint | Passed after the final aggregate repair and regression additions |
| Design checks | Passed; 68 tokens matched, literal-size budgets and markup checks passed |
| Production SPA build | Passed, including demo isolation and precompression; existing large-chunk advisory retained |
| Generated client | Typecheck and 37 tests passed |
| Documentation content | Read-only preparation check and its one regression test passed |
| Real desktop/mobile browser and story tests | Desktop and mobile lifecycle passed (4.9s and 4.6s test bodies); Adapters stories 6 passed in each of dark and light modes |
| Go parity checks and structural graph refresh | All nine Go parity checks passed; embedded UI and OIDC fixture binaries built; structural refresh completed with one worker |

Failed attempts are preserved in the offline evidence package. Seven initial
actual-parent regressions failed before error classification, metadata denial
and late-success refresh repairs; two detail-only denial regressions also failed
before repair. Independent R2 review produced six further failing regressions,
and R3 produced five failing catalogue regressions. The first full unit run had
1,804 passing tests and one sensitivity-inventory failure. Only reviewed source
pins and custody notes were updated; the final full run passed. A TypeScript
updater-inference error was repaired without assertions or suppression.

The tests mount the actual `Adapters` route with production `makeQueryClient`
defaults and establish:

- Authoritative 401/403/404 denials retire file-target list/detail metadata.
  A later detail-pruning write cannot erase aggregate denial, and a later
  network failure cannot resurrect either cache. Successful reads restore data.
- Catalogue account, environment and key metadata follows the same retirement
  semantics at its query owner. The shared `metadataQuery.ts` helper returns a
  cache-only null marker; presentation treats it as unavailable. Regressions
  include both file-target and sibling push-adapter forms. No non-null cache
  writers were found for these catalogue keys. Audit and scope-name consumers
  use optional data access and retain unavailable/fallback behavior.
- A network failure without an earlier denial retains explicitly marked
  last-loaded target metadata and disables detail actions. Malformed success,
  unbound statuses, server errors and network errors use the canonical refusal
  distinctions, preserving safe domain validation detail.
- Confirmed and uncertain create/update/delete outcomes reconcile after
  navigation within the five-second stale window. Actual `AuthProvider`
  replacement tests prove that late results cannot populate the replacement
  session, restore the retired cache, or invalidate the replacement client's
  metadata. Sensitive result lifetime remains unchanged.
- Same-route navigation can replace a pending creation form. Its old busy
  state cannot block an idle replacement or release a new pending panel.
- Explicit key IDs/names remain unioned with the save-time automatic selection;
  neither exclusions nor classification silently remove explicit members.

Independent R2 also caught missing CSRF companion headers on direct E2E writes.
Each write now reads the current cookie. R3 caught an invalid empty report stamp;
the optional field is omitted. The generated request schema independently
rejects the old empty field and accepts omission, matching the server contract
and request-validation middleware. These source/schema checks do not substitute
for the separately executed real-server lifecycle.

The complete R2 through R7 reviews are preserved in the offline evidence package.
R4 found cancelled query denial handlers could write null after a newer successful
read and refreshed reports could compare timestamps against an older page clock.
Four actual-parent regressions failed before repair and then passed. Manual
retirement, detail pruning and invalidation now check the query's cancellation
signal; generated reads receive the same signal. The executing query's client
owns the write. Report ages sample the clock on successful-read remount and
periodic timer events, with timer cleanup; no freshness policy is introduced.

The environment catalogue's canonical owner is `environmentListQueryOptions`
in `hierarchyQueries.ts`. Settings, values, adapters and org topology share its
nullable result and transport-aware cancellation. ProjectSettings guards nullable
dereferences; OrgTopology, MachineAccess and Matrix refuse readiness from null.
An independent sibling owner reviewed the Matrix and MachineAccess patch and
confirmed it applies with their pending work. Audit and scope-name readers use
optional access. Shared-reader tests demonstrate observer compatibility through
401/403/404, network failure and successful recovery; request deduplication means
these tests do not independently exercise each reader's transport.

R5 found no additional source-supported defect. Lint subsequently rejected the
render-time clock read; it was replaced with the initialized, keyed report clock.
The revised clock, shared readers and ProjectSettings passed 69 tests. Typecheck,
lint and design checks passed. A new test initially lacked AuthProvider; another
asserted topology readiness before its dependent settings query settled. Both
failed logs are retained, and the harness now waits for the actual readiness
condition. R6 independently reviewed the final clock and whole-module mutation custody and
found no new code defect. The additional unchanged-report refresh, periodic age
update and timer-cleanup regression passed in the final full run.

The final full suite, corrected bundle, Go parity and embedded fixture builds
passed. Node package and Go compile slots are released. Desktop and mobile browser
lifecycles passed against independent genuine-auth instances. Six affected
Adapters stories passed in each of dark and light modes. Source review
and mocked-fetch tests are not runtime acceptance.

Integration warning: PR #940 adds an environment cache-retirement effect after
this branch base. When combining it with nullable metadata, its successful-read
guard must also reject null before dereferencing items. Null must never be
treated as an authoritative empty topology or trigger purging all environment
caches. This combined branch has not yet been tested.

## Aggregate inventory repair

Independent sibling review found that a retired environment catalogue produces
zero child lease/certificate queries, falsely presenting zero inventory. Three
actual-parent regressions failed before repair. Machine access now qualifies
both complete aggregate views with the environment read's readiness: denied or
pending topology yields unknown counts, unavailable/loading copy, and an unknown
provider-deletion lease summary. A successful empty topology still permits zero.
Hooks remain unconditional and mutation/session ownership is unchanged.

The regressions use production `makeQueryClient` and real generated environment,
lease and certificate hooks. They cover populated inventory followed by 403/404,
network failure, server failure, successful empty recovery, provider deletion,
and actual navigation to another project's pending then empty inventory.
The seven affected test files passed all 140 tests; final typecheck, lint,
1,836-test full suite and rebuilt production bundle passed. The full suite used
a compatible prebuilt OIDC fixture, without starting another Go compilation.
R7 independently inspected the exact repair and found no additional actionable
source defect. The earlier desktop/mobile and story evidence predates this narrow
aggregate repair; it is not represented as a new combined browser run.

## Remaining delivery gates

1. Integrate current main when needed, preserving the PR #940 null guard and sibling Machine access ownership, and run affected integration checks.
2. Local Node, Go parity, structural graph, desktop/mobile lifecycle and sibling story checks have passed; retain their evidence.
3. Preserve the passed real-server desktop/mobile lifecycle screenshots and
   final local evidence; hosted CI still needs to validate the pushed head.
4. Review the final diff, update this handoff with actual evidence, commit with
   signing and DCO, verify the entire PR range and staged path portability,
   push, confirm GitHub verification, and open/link the self-contained PR.
5. Parent owns PR watching. Repair valid findings and failures, pass protected
   merge-group checks, then verify mergedAt, merge SHA and current main ancestry.

No commit, PR, remote CI or merge exists for this work yet. These are outstanding
requirements, not completed issue acceptance or an authorization request.

## Browser evidence

The real-server flow creates an account and credential through Machine access,
creates a file target with explicit ID and name selections, receives a three-day
old bearer-authenticated report, verifies generation conflict/reload/save, then
observes credential-bound deletion refusal before revoking and deleting. The
reporting request uses a separate cookie-free client. Direct administrator
writes use the current CSRF companion. Both desktop and Pixel 5 runs passed.

Screenshots were inspected at matching report states: IDs wrap on mobile, the
historical disclaimer and age remain visible, and actions fit the available
width. The files remain offline chat evidence, not repository artifacts. The
filtered lifecycle intentionally does not claim the entire browser-flow suite.
