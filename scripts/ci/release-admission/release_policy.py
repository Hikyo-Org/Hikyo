"""Fixed policy, authenticated design inputs and immutable contract inventories."""
from types import MappingProxyType
import hashlib

from bounded import digest, require, strict_json

_DESIGN_PINS = {
    'POLICY.md': (32023, '1ba3622c6ccf641398e943036603a5f00d8eda371c5fe2ff1ad06489bc852745'),
    'CONTRACTS.json': (30360, '6e927f791d6ecfe32575296731c5287f379b122edeb945e069449e9d7b77ef04'),
    'CONFORMANCE-CASES.json': (12499, '859f4685e7eb21db5c08d1d7cae59ff3c1082b7c0978acff52741090ee8b0a82'),
}
_constants = {'repository': {'owner': 'Hikyo-Org',
                'name': 'Hikyo',
                'repositoryId': 1316165429,
                'ownerId': 316726515,
                'branch': 'main'},
 'ownerActor': {'login': 'Dunky13', 'databaseId': 991668},
 'trustedPaths': ['.github/workflows/aws-acceptance.yml',
                  '.github/workflows/ci-candidate.yml',
                  '.github/workflows/ci-merge-group.yml',
                  '.github/workflows/ci.yml',
                  'go.mod',
                  'scripts/ci/aws-acceptance/README.md',
                  'scripts/ci/aws-acceptance/historical_cleanup.py',
                  'scripts/ci/aws-acceptance/runner.py',
                  'scripts/ci/aws-acceptance/test_historical_cleanup.py',
                  'scripts/ci/aws-acceptance/test_runner.py',
                  'scripts/ci/check-required-jobs.sh',
                  'scripts/ci/ci-job-registry.json',
                  'scripts/ci/pr-gate/main.go',
                  'scripts/ci/pr-gate/queue.go',
                  'scripts/ci/start-ci-postgres.sh'],
 'requiredAbsentPaths': ['go.work'],
 'goDirective': '1.27.3',
 'requiredCIJobs': {'candidate': {'candidate-proof': 'success',
                                  'validation / analysis_shards': 'success',
                                  'validation / app-build': 'success',
                                  'validation / changes': 'success',
                                  'validation / ci-required': 'success',
                                  'validation / client': 'success',
                                  'validation / compose-demo': 'success',
                                  'validation / docs': 'success',
                                  'validation / floor-bench / measure': 'success',
                                  'validation / fuzz shard (0)': 'success',
                                  'validation / fuzz shard (1)': 'success',
                                  'validation / fuzz shard (2)': 'success',
                                  'validation / headline-guarantee': 'success',
                                  'validation / isolation shard (0)': 'success',
                                  'validation / isolation shard (1)': 'success',
                                  'validation / isolation shard (2)': 'success',
                                  'validation / k8s-e2e': 'success',
                                  'validation / lint': 'success',
                                  'validation / preflight': 'success',
                                  'validation / race shard (0)': 'success',
                                  'validation / race shard (1)': 'success',
                                  'validation / race shard (10)': 'success',
                                  'validation / race shard (11)': 'success',
                                  'validation / race shard (2)': 'success',
                                  'validation / race shard (3)': 'success',
                                  'validation / race shard (4)': 'success',
                                  'validation / race shard (5)': 'success',
                                  'validation / race shard (6)': 'success',
                                  'validation / race shard (7)': 'success',
                                  'validation / race shard (8)': 'success',
                                  'validation / race shard (9)': 'success',
                                  'validation / release-snapshot': 'success',
                                  'validation / scan-xplat (macos-latest)': 'success',
                                  'validation / scan-xplat (windows-latest)': 'success',
                                  'validation / source-artifact-security': 'success',
                                  'validation / storybook': 'success',
                                  'validation / supply-chain-checks': 'success',
                                  'validation / test_core': 'success',
                                  'validation / web (desktop, 1)': 'success',
                                  'validation / web (desktop, 2)': 'success',
                                  'validation / web (desktop, 3)': 'success',
                                  'validation / web (desktop, 4)': 'success',
                                  'validation / web (mobile, 1)': 'success',
                                  'validation / web (mobile, 2)': 'success',
                                  'validation / web (mobile, 3)': 'success',
                                  'validation / web (mobile, 4)': 'success',
                                  'validation / web-closure': 'success',
                                  'validation / web-go': 'success',
                                  'vouch': 'skipped'},
                    'workflow': {'merge-group-proof': 'success',
                                 'validation / analysis_shards': 'success',
                                 'validation / app-build': 'success',
                                 'validation / changes': 'success',
                                 'validation / ci-required': 'success',
                                 'validation / client': 'success',
                                 'validation / compose-demo': 'success',
                                 'validation / docs': 'success',
                                 'validation / floor-bench / measure': 'success',
                                 'validation / fuzz shard (0)': 'success',
                                 'validation / fuzz shard (1)': 'success',
                                 'validation / fuzz shard (2)': 'success',
                                 'validation / headline-guarantee': 'success',
                                 'validation / isolation shard (0)': 'success',
                                 'validation / isolation shard (1)': 'success',
                                 'validation / isolation shard (2)': 'success',
                                 'validation / k8s-e2e': 'success',
                                 'validation / lint': 'success',
                                 'validation / preflight': 'success',
                                 'validation / race shard (0)': 'success',
                                 'validation / race shard (1)': 'success',
                                 'validation / race shard (10)': 'success',
                                 'validation / race shard (11)': 'success',
                                 'validation / race shard (2)': 'success',
                                 'validation / race shard (3)': 'success',
                                 'validation / race shard (4)': 'success',
                                 'validation / race shard (5)': 'success',
                                 'validation / race shard (6)': 'success',
                                 'validation / race shard (7)': 'success',
                                 'validation / race shard (8)': 'success',
                                 'validation / race shard (9)': 'success',
                                 'validation / release-snapshot': 'success',
                                 'validation / scan-xplat (macos-latest)': 'success',
                                 'validation / scan-xplat (windows-latest)': 'success',
                                 'validation / source-artifact-security': 'success',
                                 'validation / storybook': 'success',
                                 'validation / supply-chain-checks': 'success',
                                 'validation / test_core': 'success',
                                 'validation / web (desktop, 1)': 'success',
                                 'validation / web (desktop, 2)': 'success',
                                 'validation / web (desktop, 3)': 'success',
                                 'validation / web (desktop, 4)': 'success',
                                 'validation / web (mobile, 1)': 'success',
                                 'validation / web (mobile, 2)': 'success',
                                 'validation / web (mobile, 3)': 'success',
                                 'validation / web (mobile, 4)': 'success',
                                 'validation / web-closure': 'success',
                                 'validation / web-go': 'success'}},
 'mergeForm': 'identical-queue-tested-and-public-merge-commit-two-ordered-parents-single-pr',
 'resourceConfiguration': 'retain exact accepted eight-field resource configuration as separately '
                          'hashed input',
 'hashAlgorithm': 'existing G canonical JSON: UTF-8, sorted keys, compact separators, '
                  'ensure_ascii=true; raw original SHA256 separate',
 'historicalAnchor': 'original #1017 and original c03/c04 immutable history; no release-field '
                     'substitution',
 'fixedHelperPins': 'all seven original G FROZEN entries, exact bytes',
 'collectionBounds': {'requests': 1200,
                      'rawResponseBytes': 24000000,
                      'seconds': 300,
                      'firstParentCommits': 200},
 'integerType': 'exact integer, excluding boolean',
 'sha256': '64 lowercase hex',
 'gitSHA': '40 lowercase hex',
 'utcTime': 'existing model instant() grammar; retain real timestamps',
 'rawRecordBytes': 2000000,
 'jsonDepth': 40,
 'jsonListItems': 2048,
 'candidateCIPaths': ['.github/workflows/ci-candidate.yml',
                      '.github/workflows/ci-merge-group.yml',
                      '.github/workflows/ci.yml',
                      'scripts/ci/ci-job-registry.json',
                      'scripts/ci/check-required-jobs.sh',
                      'scripts/ci/pr-gate/main.go',
                      'scripts/ci/pr-gate/queue.go',
                      'scripts/ci/start-ci-postgres.sh'],
 'releaseStatusAuthority': 'configured Root thread genuine owner-decision stream; complete bounded '
                           'high-watermark scan and authenticated current CAS head; '
                           'unavailable/gap/conflict refuses',
 'currentReleaseEvidenceMode': 'explicit-post-merge-release-approval',
 'currentReleaseAuthority': 'explicit owner approval after exact release evidence collection, '
                            'public merge and successful queue completion; never inferred from an '
                            'earlier enqueue/removal',
 'rootSafetyHoldReasons': ['confirmed-source-compromise',
                           'confirmed-evidence-integrity-failure',
                           'unresolved-critical-source-finding',
                           'contested-release-approval'],
 'transientRefusals': 'stale/unavailable observations, query/transport limits, pointer movement or '
                      'incomplete fresh captures refuse current attempt, without a durable release '
                      'transition',
 'recordDigestOrder': ['policy and static implementation',
                       'releaseCandidate',
                       'ownerReleaseApproval',
                       'rootReleaseAdoption',
                       'status genesis',
                       'releaseConfiguration',
                       'sourceManifest',
                       'attemptTrustBinding',
                       'preparation',
                       'claim'],
 'decisionReuse': 'Standing owner approval may be revalidated only for explicit '
                  'root-safety-recovery of unchanged unexpired subjects with no withdrawal; every '
                  'initial-adoption, recovery, reauthorization and status transition decision is '
                  'consumed once by atomic authoritative CAS. Historical reconstruction is not '
                  'consumption.',
 'statusTransitions': {'genesis': ['eligible'],
                       'eligible': ['held', 'revoked'],
                       'held': ['revoked'],
                       'revoked': []}}

_record_contracts = {'releaseCandidate': {'unknownFields': 'reject',
                      'fields': {'schema': 'literal hikyo-158/trusted-release-candidate/v1',
                                 'policySHA256': 'semantic digest of exact reviewed policy '
                                                 'constants and record contracts',
                                 'historicalHistorySHA256': 'unchanged original historical bundle '
                                                            'digest',
                                 'previousReleaseSHA256': 'prior approved release digest; null '
                                                          'only for first successor whose '
                                                          'comparison baseline is original #1017',
                                 'repository': 'exact policy repository identity',
                                 'releasePR': '{number, nodeId, headSHA, baseSHA, mergeSHA, '
                                              'mergedAt, sourceRangeSHA256}',
                                 'mergeCommit': '{sha, treeSHA, orderedParents}; exact '
                                                '[baseSHA,headSHA]',
                                 'trustedFileSHA256s': 'exact 15 policy paths, each lowercase '
                                                       'SHA256',
                                 'workflowRun': '{id, attempt, path, event, name, headSHA, '
                                                'testedCommitSHA, jobsEvidenceSHA256}',
                                 'changeReviewSHA256': 'review of complete trusted diff from '
                                                       'previous approved release or original '
                                                       '#1017',
                                 'evidenceIndexSHA256': 'raw/semantic, '
                                                        'request/response/original/projection and '
                                                        'chronology correspondence index',
                                 'collectedAt': 'actual collection completion timestamp, not merge '
                                                'timestamp',
                                 'evidenceMode': 'literal explicit-post-merge-release-approval',
                                 'mergeEvidenceSHA256': 'semantic digest of post-merge exact '
                                                        'PR/commit/queue/test evidence, not a '
                                                        'historical causal queue grant'},
                      'semanticPredicates': ['No approval/adoption/runtime flags in release '
                                             'candidate',
                                             'All trusted blobs at exact tested merge equal '
                                             'release vector',
                                             'Queue-tested SHA == merged public SHA == '
                                             'mergeCommit.sha; tree and ordered parents also equal',
                                             'Complete unique 48 workflow jobs on selected '
                                             'run/attempt/head, all success',
                                             'Source range complete, signed, author-matching DCO; '
                                             'no merge-form or authority override',
                                             'Original historical checker still validates original '
                                             '#1017 separately',
                                             'Evidence digests resolve to whole genuine originals; '
                                             'Root authentication required outside pure model',
                                             'Fresh pointer read does not rewrite original '
                                             'evidence time',
                                             'No owner enqueue intent, terminal-removal cause or '
                                             'earlier pre/post observations are asserted by this '
                                             'mode',
                                             'Exactly one public merged event matches fixed '
                                             'PR/commit/owner actor, cross-checked between '
                                             'supported REST and GraphQL originals',
                                             'Mandatory complete post-merge originals in '
                                             'mergeEvidence record plus genuine later explicit '
                                             'owner release approval; absent originals refuse',
                                             'Bot removal before or after merge and earlier queue '
                                             'exits are preserved as history and supply no causal '
                                             'approval',
                                             'Final PR head/base/merge tuple and exact run/attempt '
                                             'match selected whole public evidence; ambiguity or '
                                             'changes refuse']},
 'ownerReleaseApproval': {'unknownFields': 'reject',
                          'fields': {'schema': 'literal hikyo-158/owner-release-approval/v1',
                                     'purpose': 'literal approve-trusted-release',
                                     'policySHA256': 'exact policy digest',
                                     'releaseSHA256': 'exact release candidate semantic digest',
                                     'resourceConfigurationSHA256': 'existing resource '
                                                                    'configuration digest, '
                                                                    'unchanged',
                                     'acceptanceSeries': 'literal issue158 banked series, exact '
                                                         'identity set in successor policy',
                                     'actor': 'exact policy owner actor',
                                     'originalDecisionCustodySHA256': 'index of complete genuine '
                                                                      'owner original and truthful '
                                                                      'structured decision '
                                                                      'correspondence',
                                     'decidedAt': 'actual owner original timestamp',
                                     'validUntil': 'explicit, no later than existing resource '
                                                   'authorization/horizon',
                                     'evidenceMode': 'literal explicit-post-merge-release-approval',
                                     'supersedesRevocationSHA256': 'null for ordinary approval; '
                                                                   'exact prior owner revocation '
                                                                   'status digest for explicit '
                                                                   'later reauthorization',
                                     'supersedesAdoptionSHA256': 'null for ordinary approval; '
                                                                 'exact affected adoption for '
                                                                 'later owner reauthorization',
                                     'supersedesOwnerApprovalSHA256': 'null for ordinary approval; '
                                                                      'exact prior owner approval '
                                                                      'affected by the actual '
                                                                      'withdrawal'},
                          'semanticPredicates': ['Whole original explicitly approves exact release '
                                                 'and scope; no inference from CI, enqueue, design '
                                                 'choice or digest alone',
                                                 'No budget, deployment, retry, source '
                                                 'implementation or merge grant',
                                                 'Policy and release checks pass before Root '
                                                 'adopts this approval',
                                                 'Actual owner original ordering/time follows '
                                                 'release evidence collection, change review, '
                                                 'public merge and successful queue completion',
                                                 'Approval describes exact already-merged tested '
                                                 'release; it never retrospectively approves or '
                                                 'asserts an earlier enqueue action',
                                                 'Reauthorization original explicitly names prior '
                                                 'owner revocation and revoked adoption, exact '
                                                 'current subject and scope, and is genuinely '
                                                 'later in authoritative decision stream',
                                                 'Reauthorization binds actual withdrawal status, '
                                                 'its affected adoption and prior approval '
                                                 'explicitly; it follows all three in '
                                                 'authenticated stream order and actual '
                                                 'chronology']},
 'rootReleaseAdoption': {'unknownFields': 'reject',
                         'fields': {'schema': 'literal hikyo-158/root-release-adoption/v1',
                                    'policySHA256': 'same policy digest',
                                    'implementationManifestSHA256': 'exact independently reviewed '
                                                                    'and Root-adopted successor '
                                                                    'code manifest',
                                    'releaseSHA256': 'same candidate digest',
                                    'ownerApprovalSHA256': 'exact owner release approval digest',
                                    'reviewSHA256': 'complete independent review record including '
                                                    'limitations and resolved findings',
                                    'originalCustodyIndexSHA256': 'Root-authenticated whole '
                                                                  'originals correspondence',
                                    'resourceConfigurationSHA256': 'unchanged bank configuration '
                                                                   'digest',
                                    'adoptedAt': 'actual Root adoption timestamp',
                                    'adoptionKind': 'one of initial, root-safety-recovery, '
                                                    'owner-reauthorization',
                                    'supersedesAdoptionSHA256': 'null only for initial adoption; '
                                                                'otherwise exact current previous '
                                                                'adoption',
                                    'recoverySHA256': 'null for initial; exact recovery decision '
                                                      'for root-safety-recovery or exact '
                                                      'reauthorization owner-approval digest for '
                                                      'owner-reauthorization'},
                         'semanticPredicates': ['Not accepted because its fields are syntactically '
                                                'valid; authenticated existing Root custody '
                                                'required',
                                                'The status-chain genesis refers to this adoption; '
                                                'adoption excludes later status head to avoid a '
                                                'hash cycle',
                                                'This release-data adoption does not replace '
                                                'implementation source adoption',
                                                'No reference to releaseConfiguration, '
                                                'sourceManifest, attemptTrustBinding, preparation '
                                                'or claim',
                                                'Code implementation manifest excludes runtime '
                                                'configuration/approval/adoption/status artifacts '
                                                'and pins reviewed static bytes only',
                                                'An initial adoption cannot bypass an existing '
                                                'held/revoked/superseded adoption for the same '
                                                'release; current per-release head and complete '
                                                'owner stream checked',
                                                'Root-only recovery is permitted only by exact '
                                                'rootSafetyRecovery record; owner revocation '
                                                'always needs genuinely later explicit owner '
                                                'approval',
                                                'New adoption produces new '
                                                'genesis/configuration/bindings for future '
                                                'preparation; never revives or rebinds a reserved '
                                                'attempt']},
 'releaseStatus': {'unknownFields': 'reject',
                   'fields': {'schema': 'literal hikyo-158/trusted-release-status/v1',
                              'rootAdoptionSHA256': 'exact release adoption digest',
                              'releaseSHA256': 'same release digest',
                              'sequence': 'strict positive integer, contiguous',
                              'previousStatusSHA256': 'null only at authenticated genesis; else '
                                                      'exact prior digest',
                              'disposition': 'one of eligible, held, revoked',
                              'reason': 'closed reason code from successor, not executable input',
                              'decisionCustodySHA256': 'genuine owner/Root status decision and '
                                                       'current stream high-watermark observation '
                                                       'under configured custody mechanism',
                              'recordedAt': 'actual record time'},
                   'semanticPredicates': ['Genesis eligible only after valid release adoption',
                                          'held/revoked sticky for forward work under this '
                                          'adoption; no status entry can undo them',
                                          'Authoritative current head and freshness checked at '
                                          'existing gates; stale caller head, conflict or gap '
                                          'refuses',
                                          'Bounded status chain and current head use reviewed '
                                          'existing custody/claim mechanics, not arbitrary mutable '
                                          'file',
                                          'Authoritative current status observation includes '
                                          'terminal native stream sequence and monotonic '
                                          'high-watermark; bounded whole-stream decision coverage, '
                                          'not only equality for one selected event',
                                          'Admission-now and reconstruct-original-for-cleanup are '
                                          'separate; current forward revocation/main drift cannot '
                                          'masquerade as invalidity at original action time',
                                          'Ordinary stale/unavailable reads or candidate/main '
                                          'movement produce transient refusal, not held/revoked '
                                          'status',
                                          'Only deliberate authenticated Root decision with a '
                                          'closed safety reason or explicit owner revocation can '
                                          'make a terminal status transition',
                                          'A held adoption recovers only through exact '
                                          'rootSafetyRecovery and a new successor adoption; no '
                                          'in-chain unhold',
                                          'Owner revocation requires genuinely later explicit '
                                          'exact owner reauthorization and new adoption; Root '
                                          'recovery cannot override it',
                                          'Per-release current adoption/status head covers '
                                          'superseded chains so old approvals, duplicate initial '
                                          'adoptions or old stream high-watermarks cannot bypass a '
                                          'hold',
                                          'Allow authenticated owner held-to-revoked append-only '
                                          'escalation on same adoption, preserving prior '
                                          'hold/reason/evidence; revoked has no outgoing '
                                          'transition']},
 'attemptTrustBinding': {'unknownFields': 'reject',
                         'fields': {'schema': 'literal hikyo-158/attempt-trust-binding/v1',
                                    'policySHA256': 'same policy digest',
                                    'implementationManifestSHA256': 'same adopted successor '
                                                                    'manifest',
                                    'releaseSHA256': 'exact approved release candidate digest',
                                    'ownerApprovalSHA256': 'exact owner release approval',
                                    'rootAdoptionSHA256': 'exact Root adoption',
                                    'statusGenesisSHA256': 'adoption status genesis; later current '
                                                           'status proofs supplied separately',
                                    'resourceConfigurationSHA256': 'unchanged resource '
                                                                   'configuration',
                                    'historicalHistorySHA256': 'unchanged original history',
                                    'candidateHeadSHA': 'explicit candidate revision',
                                    'mainSHA': 'explicit current canonical main',
                                    'candidateRun': '{id, attempt}',
                                    'sourceManifestSHA256': 'exact successor source manifest '
                                                            'excluding '
                                                            'attempt-binding/preparation/claim '
                                                            'digest',
                                    'currentMainRun': '{id, attempt, mergedPRNumber, headSHA, '
                                                      'baseSHA, mergeSHA}; exact current main '
                                                      'queue proof',
                                    'releaseConfigurationSHA256': 'digest of closed '
                                                                  'releaseConfiguration, before '
                                                                  'sourceManifest'},
                         'semanticPredicates': ['Binding fixed before preparation; preparation '
                                                'hashes binding; reserve/claim hashes preparation, '
                                                'avoiding cycles',
                                                'No preparationSHA or claimSHA inside their own '
                                                'transitive input binding',
                                                'Main is covered by release by complete signed '
                                                'first-parent lineage and 15-file equality',
                                                'Fresh exact current pair/full 49-job candidate CI '
                                                'and unchanged source main proof required',
                                                'Same binding on every intent, holder, paired '
                                                'driver, bind, executor, normalization and '
                                                'finalizer consumer',
                                                'Changed release, code, candidate/base, resource '
                                                'config or history after reserve causes hold; no '
                                                'in-place rebinding',
                                                'Current status head is fresh external proof, not '
                                                'a mutable field changing immutable binding',
                                                'Release/current-main queue proof may be shared '
                                                'only if exact SHA roles identical; otherwise '
                                                'current main has independent complete 48-job '
                                                'queue and merged-PR proof',
                                                'Candidate head and candidate-tested merge eight '
                                                'fixed CI path hashes equal approved release; '
                                                'go.mod remains separately bound '
                                                'candidate/tested-merge source',
                                                'Fresh status proof carries current stream '
                                                'terminal sequence/high-watermark and current '
                                                'status head; regression or gap refuses',
                                                'All composite constituent digests equal '
                                                'releaseConfiguration exactly',
                                                'Source manifest configurationSHA256 == '
                                                'digest(releaseConfiguration); no attempt digest '
                                                'occurs in composite or sourceManifest',
                                                'Required integration computes '
                                                'releaseConfiguration then sourceManifest then '
                                                'binding under two separate approved releases with '
                                                'identical static implementation bytes']},
 'mergeEvidence': {'unknownFields': 'reject',
                   'fields': {'schema': 'literal hikyo-158/post-merge-release-evidence/v1',
                              'mode': 'literal explicit-post-merge-release-approval',
                              'repository': 'exact policy identity',
                              'pullRequest': '{number,nodeId,headSHA,baseSHA,mergeSHA,mergedAt,state}; '
                                             'state merged',
                              'mergedEvent': '{restId,nodeId,actor,createdAt,commitSHA}; exact '
                                             'supported REST/GraphQL correspondence',
                              'publicCommitSHA256': 'digest of exact public merged '
                                                    'commit/tree/ordered parent proof',
                              'queueRunEvidenceSHA256': 'exact selected successful merge_group '
                                                        'run/attempt plus complete 48-job proof',
                              'sourceRangeEvidenceSHA256': 'complete source commit range, '
                                                           'signatures and DCO',
                              'timelineEvidenceSHA256': 'whole complete ordered REST timeline '
                                                        'pages with headers/pagination and '
                                                        'supported GraphQL event crosslinks',
                              'captureIndexSHA256': 'complete genuine originals and exact '
                                                    'request/projection correspondence, with real '
                                                    'capture times',
                              'finalReadbackSHA256': 'stable exact PR head/base/merge tuple and '
                                                     'run/attempt readback'},
                   'semanticPredicates': ['Tested queue SHA == public merged SHA, identical '
                                          'tree/ordered parents [base,head], single PR form',
                                          'Repository, PR number/node ID, final head/base, merged '
                                          'state/time and merge commit match across all mandatory '
                                          'supported originals',
                                          'Exactly one selected matching merged event, policy '
                                          'owner actor, no conflicting merge identity',
                                          'No queue-entry/removal causal selection, enqueue intent '
                                          'claim or original contemporaneous bracket required or '
                                          'inferred',
                                          'Removal-before-merge alone cannot accept or reject a '
                                          'release; exact unrelated PR/commit/run correspondence '
                                          'fails',
                                          'Missing/ambiguous/inconsistent originals, pagination, '
                                          'timestamp or stable readback refuse',
                                          'Whole native custody and later explicit owner approval '
                                          'remain mandatory, never satisfied by a digest alone']},
 'rootSafetyRecovery': {'unknownFields': 'reject',
                        'fields': {'schema': 'literal hikyo-158/root-safety-recovery/v1',
                                   'releaseSHA256': 'same exact release',
                                   'priorAdoptionSHA256': 'exact held current adoption',
                                   'priorHeldStatusSHA256': 'exact authenticated deliberate Root '
                                                            'hold',
                                   'resolvedReason': 'one policy Root safety-hold reason',
                                   'remediationEvidenceSHA256': 'exact evidence resolving named '
                                                                'Root reason',
                                   'independentReviewSHA256': 'genuine independent review of that '
                                                              'remediation',
                                   'rootDecisionCustodySHA256': 'genuine explicit Root recovery '
                                                                'decision',
                                   'ownerStreamObservationSHA256': 'fresh complete owner-decision '
                                                                   'scan/high-watermark proving '
                                                                   'original approval still valid '
                                                                   'and no withdrawal/incompatible '
                                                                   'decision',
                                   'decidedAt': 'actual recovery decision time'},
                        'semanticPredicates': ['Only a deliberate Root hold qualifies; an owner '
                                               'revocation never qualifies',
                                               'Same exact policy, implementation, release, bank '
                                               'and unexpired owner approval; changed approved '
                                               'subjects require new owner approval',
                                               'No new adoption/genesis/configuration digest in '
                                               'recovery record; they are later records',
                                               'Prior held chain stays terminal; recovery cannot '
                                               'revive existing reserved binding',
                                               'An unchanged unexpired owner grant may be '
                                               'revalidated for this explicit recovery only if the '
                                               'complete authoritative decision stream proves no '
                                               'owner withdrawal. The new recovery decision is '
                                               'single-use; approval reuse never permits duplicate '
                                               'initial adoption or reserved-attempt revival.']},
 'releaseConfiguration': {'unknownFields': 'reject',
                          'fields': {'schema': 'literal hikyo-158/release-configuration/v1',
                                     'resourceConfiguration': 'exact original eight fields '
                                                              'approvalSHA256,directiveSHA256,contextSHA256,proposalRawSHA256,proposalMessageSHA256,namespace,ledgerFolder,cleanupFolder; '
                                                              'byte-identical accepted values',
                                     'resourceConfigurationSHA256': 'digest of exactly the '
                                                                    'original eight-field '
                                                                    'resourceConfiguration object',
                                     'policySHA256': 'fixed policy',
                                     'implementationManifestSHA256': 'static independently '
                                                                     'reviewed code manifest',
                                     'releaseSHA256': 'exact releaseCandidate',
                                     'ownerApprovalSHA256': 'exact ownerReleaseApproval',
                                     'rootAdoptionSHA256': 'exact rootReleaseAdoption; references '
                                                           'original resource digest only',
                                     'statusGenesisSHA256': 'exact status genesis for this Root '
                                                            'adoption',
                                     'historicalHistorySHA256': 'original immutable history'},
                          'semanticPredicates': ['The configured-model composite digest is digest '
                                                 'of exactly this closed complete object',
                                                 'No sourceManifest, candidate/main/CI selections, '
                                                 'attempt binding, preparation, claim or current '
                                                 'mutable status head',
                                                 'Root adoption and all earlier records cannot '
                                                 'reference this composite digest directly or '
                                                 'transitively',
                                                 'Runtime release configuration is injected data, '
                                                 'not baked into model/code manifest bytes']},
 'sourceManifest': {'unknownFields': 'reject',
                    'fields': {'schema': 'literal hikyo-158/future-fixture-source-manifest/v4',
                               'modelSHA256': 'exact static model bytes',
                               'testSHA256': 'exact static model test bytes',
                               'frozenNextHelpers': 'exact original seven helper pins',
                               'candidateFileSHA256s': 'exact map containing go.mod only',
                               'candidateCIFileSHA256s': 'exact fixed eight CI paths, equal '
                                                         'approved release hashes',
                               'workflowFileSHA256s': 'exact fifteen trusted paths, equal approved '
                                                      'release hashes',
                               'requiredCIJobs': 'exact fixed candidate/workflow job maps, 49/48',
                               'configurationSHA256': 'digest(releaseConfiguration), not '
                                                      'resource-only and not attemptTrustBinding',
                               'resourceConfigurationSHA256': 'same original eight-field digest',
                               'policySHA256': 'same policy',
                               'releaseSHA256': 'same approved release',
                               'ownerApprovalSHA256': 'same owner release approval',
                               'rootAdoptionSHA256': 'same Root adoption',
                               'historicalHistorySHA256': 'same original history'},
                    'semanticPredicates': ['Digest over exactly the complete closed sourceManifest '
                                           'object',
                                           'No own digest, attemptTrustBinding, preparation, claim '
                                           'or later digest',
                                           'All schema v3 values retain old meaning in historical '
                                           'lane; no implicit reinterpretation as schema v4',
                                           'Actual source-role evidence and complete jobs checked '
                                           'independently; matching manifest data alone is '
                                           'insufficient']}}

def _immutable(value):
    if type(value) is dict:
        return MappingProxyType({key: _immutable(item) for key, item in value.items()})
    if type(value) is list:
        return tuple(_immutable(item) for item in value)
    return value


REPOSITORY = _immutable(_constants['repository'])
OWNER_ACTOR = _immutable(_constants['ownerActor'])
TRUSTED_FILES = frozenset(_constants['trustedPaths'])
CANDIDATE_CI_FILES = frozenset(_constants['candidateCIPaths'])
REQUIRED_CI_JOBS = _immutable(_constants['requiredCIJobs'])
ROOT_HOLD_REASONS = frozenset(_constants['rootSafetyHoldReasons'])
EVIDENCE_MODE = 'explicit-post-merge-release-approval'
ACCEPTANCE_SERIES = 'hikyo-158-banked-series-20261022'
HORIZON = '2026-10-26T12:22:56Z'
GO_DIRECTIVE = '1.27.3'
REQUIRED_ABSENT_PATHS = frozenset({'go.work'})
COLLECTION_REQUESTS = 1200
COLLECTION_RAW_BYTES = 24_000_000
COLLECTION_SECONDS = 300
FIRST_PARENT_COMMITS = 200
STREAM_SEQUENCES = 50_000
VERIFIER_SECONDS = 5

FROZEN = MappingProxyType({'setup_version_operations.source-only.mjs': '7c04cb1c5e4e1d9e4f423f49b08ec1aaef62a4a36e0e262d1b90ea55e9bab454', 'native_setup_version_operations_guard.source-only.js': '667bd009a82cb13fe293537665c18f91fd74ab8dae2672a3d043f54874c49d45', 'test_setup_version_operations.source-only.mjs': '2da0ee7fadc828ac6112bf85f3fec2432c5b2aa9530ee78e3c6251f20dfccb97', 'native_setup_whole_read_observer.source-only.js': '0bfeb00db2473fccb7e220ac6045ffb6e2c0de3dd8a20fe58760f8fbdc8815f7', 'normalize_setup_whole_reads.source-only.py': '6fa2472c5bdf9242c7d28db57b87874e471b8974887c09eb976b252fa8d09558', 'test_native_setup_whole_read_observer.source-only.mjs': 'f0d81145b2b08584347b20c3696f9efa61d31103aea9eb2a643a423676e65755', 'test_normalize_setup_whole_reads.py': '86cc96633378f9bf8207852332643f6c39a7b0d6190ffc01d3e1e12eea8bc113'})

SCHEMAS = MappingProxyType({
    name: contract['fields']['schema'].removeprefix('literal ')
    for name, contract in _record_contracts.items()
})
RECORD_FIELDS = MappingProxyType({
    name: frozenset(contract['fields']) for name, contract in _record_contracts.items()
})
POLICY_SHA256 = digest({
    'policyConstants': _constants, 'records': _record_contracts,
    'frozenHelpers': dict(FROZEN), 'acceptanceSeries': ACCEPTANCE_SERIES,
    'horizon': HORIZON,
})
require(len(TRUSTED_FILES) == 15 and len(CANDIDATE_CI_FILES) == 8
        and len(REQUIRED_CI_JOBS['candidate']) == 49
        and len(REQUIRED_CI_JOBS['workflow']) == 48 and len(FROZEN) == 7,
        'fixed-policy-inventories')


DESIGN_RAW_PINS = MappingProxyType(dict(_DESIGN_PINS))


def design_originals_checked(originals):
    """Explicit pure check for loader-supplied bytes; imports never open files."""
    require(type(originals) is dict and set(originals) == set(DESIGN_RAW_PINS), 'whole-design-originals')
    for name, (size, pin) in DESIGN_RAW_PINS.items():
        raw = originals[name]
        require(type(raw) is bytes and len(raw) == size and hashlib.sha256(raw).hexdigest() == pin,
                'design-original-integrity')
    parsed = strict_json(originals['CONTRACTS.json'])
    require(parsed['policyConstants'] == _constants and parsed['records'] == _record_contracts,
            'embedded-reviewed-contract-correspondence')
    return POLICY_SHA256
