# Reviewed trusted-source release admission

Issue: [#1046](https://github.com/Hikyo-Org/Hikyo/issues/1046).

This contract qualifies source for the existing issue 158 acceptance series.
It grants no resource, AWS, deployment, dispatch, retry or merge authority.
External originals must be authenticated separately through the existing Root
custody boundary. Semantic digests establish integrity, not authenticity.

## Implementation contract

The R2 proposal is identified by its raw design-manifest SHA256
`2701b4e6cb391a3b25aa4f4139d8e2979ff30cb6e892f1a6a7aabacf6f69fe7d`.
Its nine closed record contracts and acyclic digest order are retained:
policy/code, release candidate, owner approval, Root adoption, status genesis,
release configuration, source manifest, attempt binding, preparation, claim.
No record includes its own digest or a reference to a later record. Static
implementation manifests exclude runtime data. Raw original hashes remain
separate from ASCII-escaped canonical semantic digests.

The original eight-field resource configuration remains separately bound.
Release configuration contains its exact value and digest and the exact
policy, implementation, release, approval, adoption, genesis and history
references. It excludes pair selections, manifests, bindings and preparations.
Each attempt binds the configuration, source manifest, candidate/main pair
and selected CI runs. Reserve, owner intent, physical holder, paired driver,
bind, result, finalization and abort cleanup retain that same binding.

## Withdrawal after a safety hold

An adoption starts eligible. An authenticated deliberate Root safety decision
can append a held status with one of the four R2 reasons and its evidence.
An authenticated owner withdrawal can append a revoked status after either
eligible or held. Held-to-revoked escalation preserves the earlier hold,
reason, evidence and digest. Neither held nor revoked may become eligible.
Revoked is terminal. Repeated withdrawal cannot create another eligibility
path or erase the first withdrawal.

Recovery always creates a new adoption and genesis. Root-only recovery from
a hold requires remediation and independent review, the exact superseded
adoption/hold, unchanged approved subjects and a complete fresh authoritative
stream showing no later withdrawal or incompatible decision. Recovery after
withdrawal additionally requires a genuinely later explicit owner approval
referencing the actual withdrawal, affected adoption and prior approval.
That approval must name the exact release, policy and resource scope. A new
genesis or duplicate initial adoption cannot hide an earlier hold or withdrawal.
No recovery revives a reserved attempt.

Single-use consumption applies to transition decisions. Root-only recovery
may revalidate the unchanged, unexpired standing owner grant, but consumes a
new authenticated recovery decision exactly once under CAS. This narrow reuse
does not permit duplicate initial adoption or reuse after withdrawal.

The existing authenticated Root decision stream and compare-and-swap custody
mechanism own the current head and high-watermark. Consumers require complete,
contiguous, bounded observations, reject forks/regression/replay and refuse
when authority is unavailable. A local cache cannot assert current authority.
Transient capture, pointer and transport failures refuse that observation;
they never manufacture a durable safety hold.

## Release evidence and source roles

The fixed policy owns repository/owner IDs, authority, Go 1.27.3, root
`go.work` absence, the fifteen trusted regular Git blobs, seven historical
helper pins, exact 49 candidate and 48 queue jobs, and all original bounds.
The inventories include three isolation, three fuzz and twelve race shards.
Only candidate vouch may be skipped. Release data cannot override policy.

Current releases require explicit post-merge owner approval. Supported whole
REST PR/timeline and GraphQL merged-event originals must agree on repository,
PR/head/base/merge/time and owner. Retain complete pagination and earlier
episodes. Public and queue-tested commits must have identical SHA, tree and
ordered parents `[base, head]` for one PR. Equal trees alone do not suffice.
Complete signed source ranges, author-matching DCO, trusted-source change
review, raw-capture provenance and stable final rereads are mandatory.
Adjacent queue removals do not prove causality or prior owner intent.

The original #1017 causal checker, c03/c04 history, timestamps, raw custody
and consumed allowance remain literal. They cannot become current release
approval. Current release, current main and candidate proof are distinct
roles. Later signed first-parent main descendants may reuse approval only
when all fifteen trusted blobs match and the descendant has its own exact
full queue proof. Every candidate/main pair needs fresh proof. The eight CI
definition/helper paths must match approved bytes at candidate head and its
tested merge. Candidate `go.mod` is separately bound. Acceptance tooling
comes from main, with candidate code in its separate checkout.

Changed trusted bytes require separate complete source review, evidence,
owner approval and Root adoption. Changed toolchain, authority, source roles,
job topology or evidence semantics require reviewed policy/code changes.

## Cleanup and bounds

Admission-now and reconstruction-for-cleanup are distinct. Cleanup validates
the original action-time authority and exact immutable source, then requires
current exact-resource owner/MFA/quiescence and survivor safety evidence.
Current main movement or release withdrawal cannot strand valid original
cleanup. Unsafe cleanup code needs a separately reviewed remedy.

Preserve 176 consumed plus 32 once, 208 lifetime, one new key, one dispatch,
four old scheduled keys, every stricter $10/$3/reserve rule and strict gross
and billable totals below $15 including fees. Preserve the original horizon,
cutoffs, once markers and 120s/60s/55s/2s windows. Release approval renews none
of them. Collection remains at most 1,200 requests, 24 MB and five minutes,
with 200 first-parent commits and the original nested JSON/custody bounds.
Oversized evidence refuses without truncation or increased limits.

## Qualification

Two independently approved synthetic releases and two pairs must traverse
the real configured model, collector, normalizers, preparation, reserve,
holder/driver, bind, result/finalization and abort paths with identical
implementation/map bytes. Only external boundaries may be doubled. Negative
coverage must exercise source/pair substitution, incomplete provenance,
pagination/jobs/signature/DCO failures, forged authority, mixed configuration,
cycles, lifecycle replay and reserved rebinding. Source qualification,
independent review, genuine owner approval, Root adoption, resource authority,
deployment approval and protected merge are separate gates.
