# Release collector integration API

This module is an offline, injected reader and pure integrity checker. It does
not authenticate originals, approve a release, adopt source, or grant runtime
or resource authority. It never imports or edits the frozen G baseline.

## Public entry points

- `Collector(m, transport, explicit_clock).collect_release(selection, change_review)`
  collects one explicitly selected immutable post-merge release episode. Selection
  is `{prNumber, mergeSHA, runId, attempt, historicalAnchorSHA}`. Review is whole
  structured data with exact previous/current file vectors, changed paths and
  evidence references. All referenced originals must be supplied in its index.
- `Collector(m, transport, explicit_clock).collect_pair(selection, manifest, release)`
  collects one fresh candidate/current-main pair against the explicit release
  collection. Selection is `{candidatePRNumber, candidateHeadSHA, candidateRunId,
  candidateAttempt, mainSHA, mainPRNumber, mainRunId, mainAttempt}`. No latest-run,
  latest-release, retry, or run discovery behavior exists.
- `release_checked(m, release)` validates whole captured release bytes and their
  projections, complete jobs/ranges/files, merge correspondence and change review.
  Returns the checked semantic release evidence, without authenticity claims.
- `pair_checked(m, proof, manifest, release, explicit_now)` checks the pair collection
  and returns exactly `{'candidate': headSHA, 'workflow': mainSHA}`. Admission
  status, owner approval, Root adoption and historical #1017 validation are called
  separately by the configured model. This checker does not call or replace the
  original historical affirmative checker.
- `ExternalOriginals.verify(collection)` is a fail-closed external seam; its base
  implementation raises. `require_external_originals(collection, verifier)` calls
  this seam with whole original bodies, requests, public headers, projections and
  timestamps. A successful integrity check or a subclass is not authenticity.

`m` supplies `canonical`, `exact`, `require`, `strict_json`, `sha`, `clock`,
`stamp`, `instant`, `public_commit_checked` and `pr_commit_range_checked` with
G-compatible semantics. It may also expose the separate historical affirmative
checker. Policy constants, inventories and frozen helper pins come exclusively
from `release_policy`; release data cannot replace them. The v4 source manifest
is created/validated by the record/model owner and supplied to `collect_pair`.

## Transport and evidence

The synchronous transport receives a closed request object
`{method, endpoint, accept, body, deadlineAt}`. REST reads use `GET`, fixed relative GitHub
endpoints and null body. The only POST is a fixed GraphQL read-only merged-event
query with explicit PR number and pagination cursor. The transport returns
`{status, headers, body}` where body is the complete response bytes and headers
contain only supported public response metadata. There is no live/default
transport or clock. Each instance is single-use and limited to 1,200 requests,
24,000,000 response bytes and 300 monotonic UTC seconds; first-parent lineage is
bounded at 200 commits. Original decoder, tree, blob, range and job bounds remain.

Each collection retains whole UTF-8 response bodies, byte counts, raw SHA256, canonical semantic SHA256,
requests, headers, sent/received UTC timestamps and exact semantic projection
provenance. Pure checking replays the deterministic collector over those whole
bytes and checks the entire resulting collection, not digest placeholders.
Change-review originals are retained separately and checked as whole bytes.
External custody authentication is mandatory outside this module.

Pair `code` preserves `candidateCommit`, `workflowCommit`, `candidateFiles`,
`workflowFiles`, `absentFiles`, `ciRuns` with candidate/workflow roles and
`prCommitRanges`. Additional closed fields retain candidate CI files, release
and main episodes, tested source bytes, first-parent lineage, selections and
stable readbacks. Every `projectionPath` resolves in the final `code` packet.
All collection outputs carry false `runtimeReady`,
`externalAuthenticityVerifiedByCollector`, and `executionAuthorityGranted` flags.

Static release collection is separate from fresh pair collection. Current main
has its own exact complete 48-job queue proof, even after unchanged-source
first-parent descendants. Candidate proof has exactly 49 jobs. Only candidate
vouch is skipped. Candidate go.mod is independent; all eight CI files at head
and tested merge equal release, and all fifteen trusted main/tested files equal
release. Queue/public SHA identity, tree and ordered `[base, head]` parents are
mandatory. Earlier timeline episodes are preserved without causal interpretation.

## Final adapter details

`pair_checked` takes the complete collection, whose `code` member contains the
G packet. `pair_code_checked(m, code, manifest, release, collection,
explicit_now)` additionally checks exact equality between G's packet and that
collection's `code`, then calls `pair_checked`. A configured model may instead
keep a content-addressed registry of authenticated whole pair collections and
perform that same equality check before calling `pair_checked`.

`collection_digest(m, collection)` produces a bounded semantic integrity index
for references and immutable-registry comparison. Call whole-byte validation
separately. The digest function is not an original-evidence validator.

The transport request also includes `deadlineAt`, the absolute explicit UTC
collection deadline. Injected I/O MUST honor it, including blocked reads; the
synchronous collector can reject an over-deadline return but cannot preempt an
arbitrary injected Python callback. The only supported GraphQL query is exported
as `GRAPHQL`. Public response headers are limited to `content-type`, `link`,
`date`, `etag`, `x-github-request-id`, and
`x-github-api-version-selected`. REST link pagination and GraphQL cursor
pagination must terminate within three pages; PR ranges remain at most 250
commits, jobs at most 200, trees at most 2,048 entries, and blobs at most 262,144
bytes. The fixed job inventories remain exactly 49/48.

`m.BOOTSTRAP_1017['mergeSHA']` supplies the unchanged literal historical anchor.
`historicalAnchorSHA` must equal it. The complete signed first-parent chain
reaches that anchor. The historical causal episode is never revalidated by the
new post-merge release mode; the caller separately invokes G's unchanged
historical affirmative checker with its original custody. The previous review
baseline must be an earlier commit on that chain, with its whole fifteen-file
vector collected and compared. Whether it is the previous approved release or
original #1017 remains a record/adoption check outside the collector.

Every original contains a raw `bodySHA256` and separate canonical
`semanticSHA256`. Both are recomputed from the retained whole body. Requests,
clock readings, headers, pagination, source roles and every semantic output are
reproduced during pure replay. Generated semantic code retains the original
2 MB, depth-40 and list-2,048 bounds. A larger capture refuses without truncation.

Policy integration reads only the exported immutable `REPOSITORY`,
`OWNER_ACTOR`, `TRUSTED_FILES`, `CANDIDATE_CI_FILES`, `REQUIRED_CI_JOBS`, `FROZEN`,
`EVIDENCE_MODE`, `COLLECTION_REQUESTS`, `COLLECTION_RAW_BYTES`,
`COLLECTION_SECONDS` and `FIRST_PARENT_COMMITS` constants. The collector validates
v4 structure, fixed jobs/helpers, and actual source vectors. The configured
record model separately validates all configuration/adoption/policy/approval
and implementation digest relationships. Manifest syntax supplies no authority.

Candidate CI runs must retain one exact PR association. Queue metadata may have
an empty `pull_requests` array; that supported case still requires the exact
single-PR queue branch, public/tested identical SHA/tree and ordered
`[baseSHA, headSHA]` parents. Any nonempty association must name exactly that
same PR and pair. Candidate ephemeral test merges need not carry a public Git
signature; public candidate heads, complete source ranges, public release/main
commits and first-parent lineage do. This preserves G's signature boundary.

GraphQL event correspondence uses the merged event's global node ID against
REST `node_id`, plus exact actor, commit and time. `MergedEvent.databaseId`
is not requested because it is absent from GitHub's supported
[merged-event schema](https://docs.github.com/en/graphql/reference/pulls#mergedevent).
Timeline rows are heterogeneous; whole committed/reviewed/comment/queue rows are
preserved in provider order. Missing native times or IDs are never invented.
Final REST timeline and GraphQL pages, PR tuples, selected attempt metadata,
current run attempt, and main pointer are reread without cache or reselection.

## Supported native timeline identity and time

Collection and whole-original replay apply the same explicit variant checks to
both release and current-main episodes. The supported identity/time shapes are:

| Native `event` | Required native identity | Required native time |
| --- | --- | --- |
| `committed` | Forty lowercase hexadecimal `sha` and nonempty `node_id` | Top-level `author.date` and `committer.date` from the Git commit object |
| `reviewed` | Positive exact integer review `id`, nonempty `node_id`, forty-character lowercase hexadecimal `commit_id` | `submitted_at` |
| `commented` | Positive exact integer issue-comment `id` and nonempty `node_id` | `created_at` |
| Supported issue-event variants below | Positive exact integer issue-event `id` and nonempty `node_id` | `created_at` |

Supported issue-event names are exactly `added_to_merge_queue`,
`removed_from_merge_queue`, `labeled`, `unlabeled`, `mentioned`,
`review_dismissed`, `renamed`, `merged`, and `closed`. A supplied non-null
`commit_id` must be a forty-character lowercase hexadecimal SHA. The exact
owner/commit/time REST and GraphQL correspondence for the single merged event
remains separately mandatory.

Every required time must parse and be no later than collection start. Git
author/committer dates may carry an explicit numeric ISO 8601 timezone offset;
other required event times retain the existing explicit UTC format. Comparison
does not rewrite captured dates or order rows by timestamps. A top-level
`created_at` never substitutes for `submitted_at` or a native Git date; if one
is supplied on a commit/review row, its original time bound also applies. An
extra numeric `id` on a commit row cannot substitute for its required `sha`.

Duplicate checks span the entire episode across all pages. All supported
issue-event names share one numeric ID namespace, so changing the event name
does not hide a duplicate. Commit SHAs, review IDs and issue-comment IDs have
their distinct native namespaces; equal numeric review/comment IDs are valid.
Duplicate global `node_id` values also refuse, even if another native ID differs.
The original provider order, complete response bytes, additional payload fields,
provenance, pagination checks and stable Link rereads are retained.

All other variants refuse closed, including `cross-referenced`,
`line-commented`, and `commit-commented`: their distinct native identity/time
shapes are not implemented here. An arbitrary event with apparently valid
common fields does not become supported. Supporting another variant requires
a reviewed source change; release data cannot expand the set. These checks
validate identity/time fields of supported rows, not external authenticity or
the meaning of uninterpreted payload fields. Queue entries/exits remain
preserved observations, with no inferred enqueue intent or merge causality.

Primary schema references are GitHub's
[timeline endpoint](https://docs.github.com/en/rest/issues/timeline),
[issue-event types](https://docs.github.com/en/rest/using-the-rest-api/issue-event-types)
(common properties and the separate committed, reviewed and commented shapes),
[Git commit objects](https://docs.github.com/en/rest/git/commits#get-a-commit-object),
and the official
[REST OpenAPI timeline schemas](https://github.com/github/rest-api-description/blob/main/descriptions/api.github.com/api.github.com.json).
The queue event names are described by GitHub's
[pull-request GraphQL objects](https://docs.github.com/en/graphql/reference/pulls#removedfrommergequeueevent);
their REST `id`/`node_id`/`created_at` shape is also present in the pinned original
historical #1017 timeline. The GraphQL description is not a substitute for that
REST-shape evidence, and neither supplies current-release authenticity.

## Focused qualification

Run from the repository root:

```sh
python3 -m unittest discover -s scripts/ci/release-admission -p test_release_collector.py
python3 -m py_compile scripts/ci/release-admission/release_collector.py scripts/ci/release-admission/collector_test_support.py scripts/ci/release-admission/test_release_collector.py
```

The 34 tests use the real collector and the parent's portable G successor's
public commit/range checkers. Two distinct synthetic release vectors and pairs
pass with identical collector source. Focused negative tests cover source
change under old release, same-tree wrong SHA, reversed parents, omitted
source commits/pages/jobs, signature/DCO, CI substitution, stale main proof,
run/pointer drift, original/projection forgery, review provenance and bounds.
Timeline regressions exercise both release/current-main collection and complete
whole-original replay: unknown and identityless rows, missing/wrong native
IDs/times/SHAs, malformed Git dates, duplicate native identities within/across
pages and issue-event names, and complete heterogeneous positive controls.
No actual provider captures, native authentication, owner release approval,
Root source adoption, lifecycle integration, AWS actions, CI, commit or push
are established by this focused suite. Parent-owned integrated qualification
must still traverse the configured lifecycle and actual downstream consumers.
