# Reusable trusted release policy for issue 158

**Proposed design, not executable or adopted.** The owner selected the reusable-policy direction on October 23, 2026. This package specifies the policy and its implementation requirements. It does not authorize a release, authenticate an original, run a loader, allocate resources or close issue 158.

## Outcome and scope

Replace release-specific current-source checks with one reviewed successor policy and explicitly approved release data. Keep the original G package, #1017 merge evidence, c03/c04 history, consumed allowance and original timestamps unchanged.

The first proposed release is main `14678ba6c8d5bb5673b13078d719a355d1faf4f9`, merged by PR #1031 and tested by queue run `39924627144`, attempt 1. Those values come from the prior read-only audit. They are a candidate for authentication and review, not an approved release or a current readiness claim.

The policy is deliberately scoped to the existing Hikyo issue 158 acceptance series. General support for other repositories, accounts, toolchains, actors or resource budgets is outside this change.

## Three distinct decisions

| Decision | Required authority | What it permits |
|---|---|---|
| Adopt the policy implementation | Independent exact-source review, then Root adoption of the successor's implementation manifest and configuration | Run the reviewed admission validators. The owner's choice to design this policy is not this adoption. |
| Approve a release | Owner's genuine explicit decision naming the exact release digest, policy digest and existing resource scope; Root authenticates that original and adopts the release configuration | Use that release as trusted-source evidence, subject to current admission checks. |
| Execute an attempt | All existing owner, identity, billing, budget, history, source, CI, lease, claim, physical-lock, human deployment and dispatch gates | Only the exact already-authorized next operation. A release approval supplies none of these other permissions. |

Root cannot manufacture the owner's release decision. A checksum, successful CI job, release-record field, Git merge event or this design document cannot substitute for it. No blanket approval of future main revisions is introduced.

## What is reusable

An approved release fixes a tested trusted-source vector, release merge episode and policy version. A later main revision can remain covered without another owner release approval only when all of the following hold:

1. It belongs to the same fixed repository and main branch.
2. Its complete bounded, signed first-parent chain reaches the approved release anchor. The retained original historical anchor and its required lineage remain independently checked.
3. All 15 trusted source files at the later main revision are regular Git blobs whose exact byte hashes equal the approved release vector. Root-tree proof still establishes absence of `go.work`.
4. The policy implementation, authority identities, toolchain and complete job contracts have not changed.
5. The release remains approved and unrevoked under the same resource authorization scope.
6. The current PR head and current main revision have fresh exact-pair candidate proof. Current main also has its own complete successful queue CI, authenticated tested commit/tree/ordered parents and merged-PR correspondence. The release anchor's older queue run cannot prove a later main tree. All other admission checks pass.

This is the same narrow reuse boundary as the old collector's unchanged-source comparison, with an approved current release anchor replacing the hard-coded current-source role of #1017. It is not a claim that unrelated repository files are trusted acceptance tooling. The full candidate CI and existing candidate-file rules remain required for every pair.

A changed trusted file, missing file, symlink, unrelated lineage, revoked release or changed contract holds admission. Changed trusted bytes require a new release candidate, change review, exact release approval and Root adoption. Do not silently select the newest main, newest run, newest passing attempt or another approved release.

## Policy constants and data

The implementation owns the fixed repository IDs, allowed owner actor, trusted paths, Go directive, absent paths, source helper pins, workflow identities, exact CI inventories and all existing resource/timing constraints. Release data cannot override these constants or name arbitrary paths, commands, validators or imports.

For version 1, preserve the current 15 trusted paths and seven frozen helper pins. Preserve literal Go `1.27.3`, exact candidate and queue workflow identities, same-repository PRs, signed public commits, author-matching DCO checks, candidate `go.mod` proof and root `go.work` absence. Preserve the adopted 49-job candidate and 48-job workflow contracts, including 3 isolation, 3 fuzz and 12 race shards; `vouch: skipped` is the sole intended candidate skip. Queue admission is not full CI proof.

Close the audited candidate-CI source gap explicitly. The policy fixes eight CI-definition/helper paths: `.github/workflows/ci-candidate.yml`, `.github/workflows/ci-merge-group.yml`, `.github/workflows/ci.yml`, `scripts/ci/ci-job-registry.json`, `scripts/ci/check-required-jobs.sh`, `scripts/ci/pr-gate/main.go`, `scripts/ci/pr-gate/queue.go` and `scripts/ci/start-ci-postgres.sh`. Their blobs at both candidate head and the actual candidate-tested merge must equal the approved release vector. Keep the existing separate candidate/tested-merge `go.mod` equality and Go directive checks. All 15 paths must match at release-tested and current-main roles. Acceptance executes the main checkout's pinned workflow/runner with candidate code in its separate checkout; do not substitute candidate acceptance tooling. This fixes a defined source-role gap and is not complete transitive dependency attestation.

Policy version 1 does not add squash, rebase, octopus or multi-PR merge-group support. It accepts the existing two-parent, single-PR shape only: the queue-tested commit and public merged commit must be identical, with ordered parents `[baseSHA, headSHA]`. This is stricter than tree equality alone. A different merge form requires a policy successor, not guessed correspondence.

Changes to these policy constants, the allowed authority, evidence transport, merge correspondence or custody rules require a new policy version, implementation review and Root adoption. A new release approval cannot relax them.

## Closed records and digest rules

`CONTRACTS.json` defines design-time closed field sets and cross-record predicates. It is a specification, not a production validator. Unknown fields, duplicate JSON keys, nonfinite numbers, invalid types and out-of-bound collections must be rejected by the successor. In particular, booleans cannot serve as integer IDs.

Use the existing model's bounded ASCII-escaped canonical JSON algorithm for semantic record digests. Retain raw SHA-256 separately for original files and whole native/provider envelopes. Never equate a semantic digest with a raw digest. Preserve transport-specific receipt canonicalization where it differs; do not change G's hash algorithm to accommodate transport Unicode.

Records form an acyclic chain: policy implementation manifest -> release candidate -> owner release approval -> Root release adoption -> attempt binding. A record never contains its own digest. The policy implementation manifest excludes later approval and runtime records. The release candidate excludes approval/adoption fields, so owner approval can bind its final bytes without a hash cycle.

### Release candidate

The immutable release candidate binds policy and original historical-history digests; fixed repository IDs; exact PR identity, source head, base, public merge commit/tree and ordered parents; the exact trusted file vector; workflow run ID, attempt and tested commit; complete job evidence; whole PR source range; change review; explicit post-merge release-evidence mode; evidence index; and original observation times. It carries no `approved: true` field.

The change review covers the complete trusted-source diff from the previous approved release, or from original #1017 for the first successor. It accounts for intervening accepted changes, not only the final PR #1031's two workflow updates. Each changed trusted file must have reviewed provenance. Dependency changes may require broader review and full CI even when only `go.mod` is in the fixed vector. Unknown or incomplete provenance holds.

Original #1017 evidence remains in its historical field and is validated with its original fixed checker. The new release merge episode is a separate record and validator. Do not reinterpret, relabel or freshen the historical episode to satisfy current-source approval.

### Owner release approval and Root adoption

The owner approval names an exact release digest, policy digest, unchanged resource-configuration digest, acceptance-series identity and explicit purpose `approve-trusted-release`. It explicitly approves the exact already-merged, fully tested source under mode `explicit-post-merge-release-approval`. The actual decision must occur after the candidate's full evidence/change review collection, merge and successful queue completion. Preserve the whole genuine owner message, its immutable native identity and original timestamp in the custody index. Root authenticates the correspondence between that message and the structured decision. A root-authenticated summary cannot invent missing consent.

Root release adoption binds the exact reviewed implementation manifest, release, approval, review and original-custody index. The append-only status genesis refers to that adoption; adoption does not contain a later status digest. Root's existing native-original verifier and approved transport establish external custody. Pure validators return checked structure with false authority markers; they cannot authenticate their own input or create an execution grant.

No new authentication system, signing scheme, key store or generic trust service is proposed. Use the existing native-original and provider-readback mechanisms. Any necessary transport adaptation is itself reviewed and pinned.

### Attempt binding

The successor has two differently named records: `releaseConfiguration` and `attemptTrustBinding`. The first is the configured model's composite configuration; it contains the original eight-field resource configuration byte-identical plus its digest, policy/implementation/release/approval/adoption/status-genesis/history digests. Its complete closed field set is in CONTRACTS.json. It excludes source manifests, candidate/main selections, CI selections, attempt bindings, preparations and claims. Root adoption refers only to the original resource-configuration digest, never to the composite configuration that later contains that adoption. This avoids an indirect hash cycle.

The successor per-attempt `sourceManifest` has a separate closed field set: schema; model/test pins; seven frozen helper pins; candidate `go.mod`, eight candidate CI-path and fifteen workflow file maps; exact jobs; composite configuration digest; resource-configuration, policy, release, owner-approval, Root-adoption and historical-history digests. It excludes its own digest, attempt-binding, preparation and claim digests. Model/test/implementation pins cover code, not runtime configuration files. The existing source manifest's `configurationSHA256` is migrated to mean `digest(releaseConfiguration)`; old manifests are not reinterpreted under the new schema.

The attempt binding includes the composite configuration digest and its constituent policy, implementation, release, owner approval, Root adoption, status genesis, history and resource-configuration digests; explicit candidate/main SHAs, release/current-main/candidate CI run selections, and source manifest digest. The complete digest order is: static implementation/policy -> release candidate -> owner approval -> Root adoption -> status genesis -> release configuration -> source manifest -> attempt binding -> preparation -> claim. Fresh current-status proofs accompany the immutable binding without changing it. Carry the binding through preparation, reserve, owner intent, CreateKey lock context, bind, execution and finalization. Every consumer validates it with the same configured successor model instance.

Code manifests pin the stable policy implementation. Per-release and per-attempt data digests flow through the configuration and evidence binding, not literal hashes embedded in every Python/JavaScript consumer. The one-time migration must remove only the obsolete current-release literals. All other code/source guards remain exact.

## Evidence and episode semantics

Source hashes prove byte identity, not provenance or approval. The release evidence must contain or resolve, within reviewed bounds, to whole genuine originals for these facts:

1. Exact repository/owner/PR identities and supported response schemas.
2. The complete ordered provider timeline and pagination. Preserve all earlier failed or removed episodes. No queue-removal event is selected as proof that it caused the public merge. Preserve real event IDs/order/times; do not infer causal links from adjacency, matching timestamps, a queue branch label or eventual success.
3. The sole current-release evidence mode is `explicit-post-merge-release-approval`. Mandatory originals are: complete REST PR/timeline pages with terminal pagination and supported public headers; supported GraphQL repository/PR/merged-event response; public commit/tree/blob/range originals; exact queue run/attempt and complete jobs originals; complete change review; and whole native custody for those captures plus the later explicit owner release approval. REST and GraphQL must agree on fixed repository IDs, PR node/number, final head/base, merged state/time and exact merged commit. Select exactly one merged event matching that PR and commit, with the policy's fixed owner actor. Public commit/tree/ordered-parent and source-range checks bind the tuple; the successful queue-tested SHA must be that identical public merged commit. Final fresh reads must retain the selected tuple and run/attempt. An ambiguous merge identity, wrong actor/commit, missing page/original, inconsistent source range, changed tuple or failed test refuses.
4. Pre/post-enqueue observations and queue-entry IDs are not required by this new current-release mode, and cannot replace its explicit later owner approval. This mode makes no claim about which enqueue or removal caused the merge, what revision the owner intended at an earlier click, or whether queue removal was completion or abandonment. It does not accept a historical causal grant. Earlier exit/reentry or a bot removal before/after merge is retained as actual history and neither grants nor invalidates this later exact release approval. Removal followed by an unrelated merge fails exact PR/commit/run correspondence; an unrelated removal cannot be used to prove causality even for a coincidentally adjacent valid merge. The separate fixed #1017 historical validator still requires all its original contemporary/causal observations. The current #1031 example remains held until every mandatory new-mode original, change review and explicit post-merge approval exists. No missing earlier capture is invented.
5. Exact completed successful queue run and attempt, correct event/path/name, tested commit, tree, ordered parents, complete unique jobs and full source PR range. Reread mutable metadata; changed attempt or run selection holds rather than automatically selecting another.
   Keep three explicit roles: approved-release queue proof, current-main queue proof and current-candidate proof. The first two may share one record only when current main is the exact release SHA. Otherwise fetch and validate the later main's own merged PR and complete 48-job queue proof; old anchor jobs cannot be relabelled. Candidate head/tested merge and its 49 jobs must bind to that current main base. Later unchanged-source main coverage does not imply historical owner enqueue approval of each descendant, and must not make that claim.
6. All trusted blobs and required absence proofs at both the tested release and current main. Validate byte counts, Git blob IDs, modes, complete trees and UTF-8 as in the current collector.
7. Source-range signature/DCO checks, complete historical-to-release and release-to-current signed first-parent lineage, plus exact current PR candidate range.
8. Every selected command, response, public header, request endpoint, observation time and semantic projection must correspond to the retained whole original. No child stdout or audit JSON body becomes a native original by wrapping it.

Current mutable pointer, run/attempt and status reads retain their original five-minute freshness/stability and first-forward buffers. Static release evidence retains real historical times. Refreshing a pointer never refreshes a merge event, approval, resource lease or expired human readiness.

Retain the existing 1,200-request / 24 MB / five-minute collector bounds, complete pagination caps, 200-commit first-parent bounds and other existing decoder limits. Collect static release approval separately from the bounded fresh candidate capture; the fresh capture validates its exact adoption and current status. If a complete proof cannot fit, hold and separately review a bounded algorithm change. Do not truncate, silently cache mutable facts, extend deadlines or weaken source coverage.

## Lifecycle and failure behavior

| Stage | Required transition | Refusal behavior |
|---|---|---|
| Policy draft -> implementation adopted | Concrete successor, meaningful integrated tests, independent review, Root adoption | No release admission or AWS action |
| Release candidate -> approved release | Whole evidence/change review, explicit owner decision, Root authentication/adoption | Keep candidate non-authoritative |
| Approved release -> prepared attempt | Current canonical main covered by release, exact candidate/main CI, original history/resource config, fresh other preflights | No reserve or resource creation |
| Prepared -> reserved | Unchanged binding, fresh release status/source/pointers and existing reserve gates | Permanent charges/once records follow existing ledger rules; never refund by deleting a failed record |
| Reserved -> first provider effect | Same release and attempt binding, fresh final source/status/pointer checks and unchanged 60s/55s/2s gates | Hold, do not switch release or retry an uncertain effect |
| Effect observed or uncertain -> continuation/cleanup | Existing readback, custody and explicit operation tickets; each later protected step rechecks its required live gates | Stop new forward work on drift, revocation, expiry or uncertainty; retain original evidence and perform only authorized cleanup |

A new release record can be prepared while another attempt exists, but it cannot be substituted into that attempt. A failed reserved attempt retains its original release and consumed allowance. Any later attempt requires existing bank/claim rules and explicit human attempt selection; release policy grants no automatic retry.

### Release status and revocation

Distinguish a transient admission refusal from a recorded release hold. Stale or unavailable status/pointer reads, expired observation windows, incomplete fresh captures, ordinary transport failures or a moved candidate/main pair refuse the observation/preparation/attempt; they do not create a release-status transition. A later fresh complete capture can use the still-eligible release for a new pre-reserve preparation, subject to all other gates. Any reserved attempt remains held under its existing once/charge rules and cannot be revived or rebound.

Use a bounded append-only status chain rooted in each Root release adoption. Only an explicit authenticated owner revocation or a deliberate authenticated Root safety-hold decision changes an eligible release status. Root safety-hold reasons are the closed set `confirmed-source-compromise`, `confirmed-evidence-integrity-failure`, `unresolved-critical-source-finding` and `contested-release-approval`. A failed gate cannot implicitly write one. The decision names the release, affected adoption and evidence; every status entry names the previous status digest, original decision custody and actual time. Neither `held` nor `revoked` may return to eligibility within that adoption. An explicit authenticated owner withdrawal may append `held -> revoked` on the same chain, retaining the hold, reason and evidence. `revoked` has no outgoing transition. Root must read the authoritative current head and decision-stream watermark at preparation and each existing forward gate; old eligible records cannot hide a newer hold, revocation or adoption.

Recovery is explicit and creates a new adoption, never an eligible entry on the old chain. For a Root safety hold only, Root may authenticate a recovery decision with exact prior adoption/hold digest, resolved reason, remediation evidence and independent review. It may reuse the original owner release approval only if the exact policy, implementation, release, resource scope and validity remain unchanged and a fresh complete decision-stream read proves there is no owner withdrawal or newer incompatible decision. The new adoption names `supersedesAdoptionSHA256` and `recoverySHA256`; its status genesis and future configuration/binding are new. If any approved subject changed, obtain new explicit owner approval under the changed reviewed policy/release first. Recovery does not restore or rebind an existing reserved attempt.

Owner revocation cannot use Root-only recovery. Reauthorization requires a genuinely later explicit owner approval naming the exact release, policy, scope and revoked adoption/status digest, followed by Root authentication and a new adoption. The authoritative per-release adoption/status head and owner-decision high-watermark cover all superseded chains; issuing another adoption cannot hide an earlier withdrawal or intentional hold. A duplicate adoption with an old approval and no qualifying recovery decision refuses.

Missing status custody, an unbounded chain, stale status observation or conflicting heads refuses the current admission without fabricating a durable release decision. The policy does not create a new global mutable JSON authority file. Implement the current per-release adoption/status head through the existing reviewed custody/claim mechanism and include collision/atomicity/recovery tests in the successor; do not promote a convenience local cache to authority.

For this series the authoritative decision scope is the configured Root thread's genuine owner-decision stream. A withdrawal received elsewhere takes effect when its genuine original is brought into that scope and authenticated; this policy cannot observe every external channel. Root consumes the complete bounded stream since its last authenticated high-water mark before issuing a status observation, includes the observed terminal sequence and status head, and applies changes with the existing compare-and-swap discipline. A consumer rejects a regressing watermark/head or unverifiable/gapped stream. Missing head, more than the existing 50,000-sequence query bound, exceeded five-second verification budget or ambiguous decision holds. Exact whole-byte native correspondence retains `LIMIT 2` and exactly one matching original. Do not interpret the absence of a file named revoked as consent.

Revocation stops future forward effects and attempts. It does not promise to undo an already accepted provider send or stop an in-flight GitHub job. Record that uncertainty, retain charges and invoke the existing abort/cleanup coordinator. Cleanup remains bound to the original attempt's exact resources, old-key exclusions and original cutoff. If the pinned cleanup path itself is unsafe or incompatible, hold forward work and obtain a separately reviewed exact cleanup remedy; never substitute new release tooling into a live attempt silently.

Make admission-now and reconstruct-original-for-cleanup explicit modes. Cleanup verifies that the attempt's release/status/source evidence was valid at its original action time, then obtains the existing genuinely current owner/MFA/quiescence/owned-survivor and exact-resource cleanup proof. It does not require canonical main or present forward-release eligibility to match the old attempt. A generic call to a new `ci_checked(now)` or current eligible-status gate must not accidentally strand cleanup. Historical reconstruction never grants a new forward action.

## Limits and other gates remain unchanged

Retain 176 consumed plus 32 once, 208 lifetime; one new KMS key; at most one acceptance dispatch; eight roles, one boundary and at most one OIDC provider; and the four old scheduled key deletions untouched. Release versioning never resets consumed amounts or permits an extra key.

Preserve the bank's below-$15 billable cap and every stricter existing forecast, gross, reserve, tax/fee and billing rule. Preserve the original horizon `2026-10-26T12:22:56Z`, lease/authentication windows, four-hour cleanup cutoff, provisioning/workflow limits and proof freshness. Preserve the reserve, arm/Finish, physical-lock and adjacent-effect limits, including 120s, 60s, 55s and 2s. Do not extend them to accommodate policy work.

Fresh setup-user CLI identity and sole MFA, private owner confirmation/control, native UI probes, actual budget/inventory originals, guided private policy paste, exact native analysis, one-write guards, protected deployment approval, all real AWS cases, exact cleanup/readback and protected PR merge remain separate. The current design selection and source tests authorize none of those operations.

## Required implementation, in dependency order

1. Build a new owned successor directory. Copy only reviewed required source; record complete origin diffs and a new map/manifest. Keep G and all retained history read-only. Define closed records, policy constants and configuration binding before the collector.
2. Separate the model's historical #1017 checker from the new current-release checker. Preserve the former bytes/behavior; introduce reusable release/approval/status checks and call them from every current CI, prepare, reserve, bind and first-forward path.
3. Implement bounded post-merge release collection and exact PR/merge/run correspondence, plus the fresh explicit candidate/main collector. Retain raw originals, projection provenance, final pointer/status rereads and all limits. Keep the historical causal checker separate and unchanged. Refuse incomplete current #1031 evidence rather than invent old-style capture IDs.
4. Migrate the entrypoint, runtime bridge, loader/maps, normalizers, ledger/executor and Chrome A/H/N/D/K consumers to the successor's honest model pin and composite binding. Follow the direct consumer map from the companion audit. Do not permit imports or masks claiming a new model is old G.
5. Exercise synthetic full configured-model paths from release adoption through CI collection, normalization, preparation, reserve, holder/paired-driver checks, bind, abort and finalization. Use two different release vectors with independent approvals, and two later candidate/main pairs under unchanged implementation/map bytes, to prove reuse. Use real successor validators and real historical fixtures where required. Double only external boundaries. Confirm all unchanged original historical/cap/claim/lock regressions and new conformance cases fail under targeted mutants. Test new source-sized evidence against existing nested record limits, not only small stub records.
6. Independently review the complete successor and every pin/contract migration. Correct confirmed findings, then Root authenticates genuine new originals and adopts exact implementation/configuration. Cross-provider review is unavailable and must be reported skipped, never CLEAN; no unperformed review is recorded as passed.
7. Present the concrete current release and change review for explicit owner release approval. Root authenticates/adopts that exact release, then freshly selects eligible current candidate/main proof. Only after the other existing preflights and human steps are ready may AWS work resume.

A design review cannot substitute for step 6's exact-source implementation review. Merely editing the collector, accepting any merged main, changing bootstrap hashes or repinning historical timestamps is not an implementation of this policy.

## Effort and completion impact

The one-time successor/model/consumer migration and its integrated validation are the largest remaining engineering item. Current-release evidence/approval is medium effort and may be blocked by missing causal originals. Future unchanged-source main advances should be small fresh-capture work; future changed-source releases require review and approval data, with policy code reused.

The prior end-of-day estimate cannot be supported while this larger design, implementation and real AWS execution remain open. Completion requires all seven steps above plus the already-outstanding live acceptance and cleanup. No new CI spend or AWS allocation is needed to finish this design.

## Evidence and verification limits

Primary source: the immutable G model and collector, the adopted Root loader/capture wrapper, and the prior trusted-source audit. Exact file/line migration references are in the companion contract audit retained outside Git (identified in issue #1046). It records 14 G production files with the old model hash, plus maps/loaders and A/H/N/D/K contracts. It is a scoped static audit, not full dependency verification. The design's current-release example uses audit snapshots, not new provider authentication.

This package's structural checks cover report/contract consistency and artifact integrity only. Its conformance cases specify required future implementation tests; they are not executed production-validator tests. No repository files, G files, runtime source, native store, browser/profile, AWS resource, ledger, claim or admission proof is modified by this design.


## R3 reconciliation, reviewed before implementation

The authoritative state consumes transition decisions once, not a standing
owner grant once. Initial adoption consumes its Root adoption decision. A
Root-only safety recovery consumes a NEW authenticated recovery decision,
requires unchanged and unexpired policy/implementation/release/resource scope,
remediation and independent review, and complete fresh stream proof of no owner
withdrawal. Only this route may revalidate the original standing owner grant.
It cannot create a duplicate initial adoption, skip a hold, or revive a reserved
attempt. The fresh recovery decision, reauthorization decision and every status
transition decision may authorize exactly one state transition under CAS.

Owner withdrawal after a hold appends revoked on that same adoption, preserving
the held predecessor. Later owner reauthorization explicitly references the
actual withdrawal, affected adoption AND prior owner approval, is genuinely
later by stream order and chronology, and requires remediation review before a
new adoption/genesis. Root-only recovery after any withdrawal refuses. Old
chains and reservations never return to eligibility.

Required decisive cases are held-to-withdrawn-to-reauthorized new adoption;
Root-only recovery after withdrawal refused; pre-withdrawal approval replay
refused; Root-only recovery without withdrawal allowed only with a fresh
single-use recovery decision; reusing that recovery decision refused; cleanup
under exact valid original authority after withdrawal remains possible with
current exact-resource safety proof.
