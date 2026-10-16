# #527 execution and evidence reconciliation, 2026-10-16

Source snapshot: `6769ee55dcd4087eeaf42547e9abde01c4f0818c`.
Issue: [#527](https://github.com/Hikyo-Org/Hikyo/issues/527).
This is a documentation increment for an ongoing roadmap, not issue closure,
release approval or production deployment. #79 owns reconciliation of the
[release plan](../release/release-plan-1.0.md),
[acceptance ledger](../release/acceptance-1.0.md), self-hoster candidate and
API/CLI spellings. Until that separate change lands, their dated evidence
remains historical; this handoff does not claim those files are refreshed.

## Changes and boundaries

The former plan said no code-side work remained and scheduled delivered social
sign-in and Kubernetes reporting after the release. Current issue contracts,
source and verified merges contradict that execution order. This increment
separates implementation, accepted release membership and evidence still owed.
It identifies the old plan's blanket permission to leave mandatory MCP
profiles unverified for #79 to correct: #651 requires explicit disposition of
a narrower claim. The initial release-plan/acceptance draft was preserved as
an external handoff to #79; those files are excluded from this increment.

Only the public roadmap and this dated execution map change. No ADR, parity
row, historical criterion outcome or release-membership decision is changed.
#589 owns only the accepted mvp-boundary A7 relocation, amendment 4
and closed-scope uncharged wording. #15 owns the pending member-access
ADR/handoff/status correction. `docs/status/ledger.json` and generated status
surfaces are deliberately excluded from this increment until that correction
can be consumed. Existing dated handoffs remain historical evidence.

The governing social disposition is
[#589 comment 5531905677](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-5531905677).
Its #611 password-proof conflict is separately settled by
[comment 6199131864](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-6199131864).
The #612 RFC8628 amendment still needs completed review/merge closeout through
#32 even though its implementation is merged. During this increment the owner
approved its scoped review route and #15's scoped disposition; those approvals
are not completed acceptance. #589 PR #945 and #32 PR #946 were reported open,
not merged. #930 policy B and its scoped review route are recorded in
[comment 6220018904](https://github.com/Hikyo-Org/Hikyo/issues/930#issuecomment-6220018904).
The owner also approved corrected #805 A with scoped review and #631 no-OAuth A.
None of these decisions is a blanket waiver of other locked review gates.

## Acceptance and integration map

The following covers the 22 audit-queue issues in #527 plus the subsequently
found #941. Every implementation owner still owes complete applicable behavior
proof, signed+DCO commits, current review repairs, mandatory protected merge
validation and verified main ancestry. The table records obligations, not a
claim that pending branches have passed them. PR state is a dated snapshot;
consult the owning PR's current result before changing a disposition.

| Owner | Acceptance obligation | Dependencies or remaining boundary |
| --- | --- | --- |
| #930 | Approved retention B; count-independent ceiling, verified-copy exception, uncertainty/failure telemetry, service/config/doctor/docs and erasure agreement; old/under-count/clock/unlink tests and both-engine recovery | Scoped ops/encryption review; integrate #941 before final non-UTC PostgreSQL recovery proof. Independent of payload-ranking #933. |
| #934 | Atomic non-increasing renewal-period ceiling, zero/omitted/tightened/concurrent renewals, current authority/fencing, real provider VALID UNTIL, generated-owner and both-engine tests | PR #938 pending. Integrate shared conversion with #935; admission does not prove merge. |
| #935 | Safe TTL maximum at ingress/persistence/worker/provider arithmetic; request schema/client generation; accepted/next/MaxInt64/zero/negative/normal boundaries and both-engine/real PostgreSQL lifecycle | Integrate #934's final contract before combined acceptance; preserve historical response range. |
| #941 | Deterministic PostgreSQL catalog rendering, scoped connection settings, unchanged signed identities, actual drift negatives, admission/backup/restore and pool reuse; SQLite intact | PR #944 pending. Independent of #930's policy choice; UTC workaround is not the fix. |
| #931 | Recovery refresh of added/renamed/reordered/deleted environment topology, removed data retirement, remote isolation, first-connect and once-per-recovery behavior; desktop/mobile two-session proof | PR #940 pending. #819/#820 shared query/cache integration must consume the final change. |
| #932 | Distinct project-list pending/error/empty/nonempty in Overview and shell; accessible retry, recovery and one-project redirect; desktop/mobile and real-hook tests | Independent of #931; coordinate shared shell/query changes. |
| #933 | Real both-engine catch-up/scaling evidence; bounded ranking preserving sparse lineage, age AND last-N, pins, self-config roots and transactions; sqlc and conformance | Useful optimization; no audit SLO failure established. Does not replace backup-retention #930. |
| #936 | Failed-helper/output descendant cleanup, successful-helper behavior, Linux/macOS proof, explicit Windows limitation, timeout/output bounds and kube/Vault coverage | Implemented by merged #937 at this source. Its handoff and trusted merge proof are linked below. |
| #807 | Accepted bounded credential/loader/custody/current-authority/expiry/revocation/no-offline behavior; four project/policy browser admin operations | PR #942 pending; own credential lifecycle is CLI-only. Scoped review waiver does not extend to other issues. |
| #819 | Five file-target admin operations; binding/selection, generation check, live-credential delete refusal, accurate report age/state and parity | #164 merged. Client-local reporting excluded from browser scope; v1.1 membership retained. Coordinate shared query changes with #820. |
| #820 | All 15 Transit operations reachable; no key material, display-once unstored plaintext/data keys; list/empty/disabled/compromised browser cases and parity | #156 merged. Integrate #819 query compatibility and #931 topology before final browser proof; apply owning Transit amendment. |
| #805 | Corrected A durable transition records, bounded repeatable attempts/coalescing/no-op behavior, signatures, duplicate/restart-safe receiver admission, complete API/CLI/UI/verifier/docs and real host delivery | Scoped contract review and implementation still owed; serialized ops-spec edits with #930/#32. Existing pull delivery and sealed-value adapter are separate. |
| #631 | Accepted no-OAuth necessity decision, exact-client evidence or explicit unsupported/unverified reasons, custody/operator/attribution/offboarding comparison and revisit triggers | PR #943 pending; fresh client runtime evidence separately scheduled. Does not close #651. Positive human integration alone would require #613/#617 semantics. |
| #651 | Candidate source/image/deployment/public HTTPS identity; scoped real tools, invalid/revoked/replacement credentials, exact claimed clients; real two-process PostgreSQL cursor/auth/cancel/drain/concurrency and redacted support evidence | Reviewed candidate and isolated authorized fixtures required; independent of OAuth/#631. Unknown mandatory profiles require explicit disposition or remain open. |
| #589 | Narrow A7/amendment4/uncharged reconciliation, named login/device+mailer preconditions, review and verified merge | PR #945 open at handoff. Closure also depends on #32's scoped #612 closeout, not global #32 closure. |
| #32 | Qualifying RFC8628 +5s amendment review and recorded merged closeout, historical body retained, connection-timeout distinction | Review route approved; PR #946 open at handoff. Incoming #930/#805 ops amendments remain separate closure obligations. |
| #15 | Qualifying scoped member-access disposition/review and ADR/handoff/status reconciliation | Owner disposition approved; existing Members/WhoCan code remains operative. Consume its committed ledger correction without concurrent edits. |
| #148 | Durable PostgreSQL mint/validated publish/overlap/revoke, safe config and destructive manual confirmation, failure custody, normal Compose/K8s delivery, all surfaces/audit/health and restart/multi-node proof | #147/#63/#64 merged. Own amendment gate remains. Integrating #934/#935 before final rotation proof is an integration recommendation, not a new original issue dependency. |
| #158 | AWS surfaces/auth/no-read closure/ownership/lifecycle/replay, deterministic JSON/partial failure and negative cases, controlled harness plus real AWS smoke/delete-recreate/cleanup | #157 and #828 merged. Real authorized sandbox/custody and amendment8 closeout remain; emulator pass is not live AWS proof. |
| #161 | Cloudflare minimum permissions/protected token, secret-only Workers/Pages, complete lifecycle, batch/deployment/partial/ambiguous outcomes, real disposable preview/production isolation and cleanup | #157 and #822 merged. Real sandbox authority and own formal review remain; controlled tests do not certify the real provider. |
| #527 | Traceable execution/evidence map, coordinated current documentation, reviewed signed merge of this increment | Ongoing tracker stays open. Do not infer release success from merged dependencies. |
| #79 | Full owning criteria on both engines/all check classes, floor recovery, freeze/parity/skew, self-hoster/docs, providers/disclosure, stable authority and signed verified public artifacts | All 42 listed legacy issue blockers are closed; full candidate and external/release gates remain. Fix/disposition current correctness contracts before freeze. |
| #41 | Original user stories and operative corpus, culminating in #79's complete release act | Feature completion alone cannot close the release umbrella. |

## Source checks behind the map

- `api/parity.yaml:455-459`, `524-538` and `544-547` still track the five #819,
  fifteen #820 and four #807 operations at the snapshot.
- `docs/adr/member-access-rules.md:3-11` makes the amendment operative while
  preserving review debt; `web/src/routes/Members.tsx` uses the existing
  `accessRules/WhoCan.tsx`. The capability ledger's missing-surface and
  not-yet-operative wording is stale. #15 owns its correction.
- `api/acceptance_ledger_test.go:25-47` extracts criterion-shaped rows from all
  of mvp-boundary. A7 relocation can retain the deferred inventory entry;
  deleting its owning row or ledger row independently breaks closure. Do not
  weaken the inventory test or infer new criterion counts from relocation.
- `internal/service/backup_dr.go` retains newest-count archives regardless of
  age at this source; `internal/service/dynamic_worker.go` performs unchecked
  TTL multiplication; both-engine dynamic repository queries allow ceiling
  replacement. These source findings explain #930/#934/#935, not new runtime
  reproductions by this documentation owner.
- `internal/store/upgrade/schema.go` hashes PostgreSQL deparser output;
  #941 records the actual timezone reproduction. #527 did not rerun it.
- `internal/isolation/aws_external_e2e_test.go` and
  `internal/adapter/cloudflare/contract_external_test.go` explicitly gate
  real-provider execution. Merely finding those tests does not satisfy them.

## Verified historical merges and validation identities

GitHub mergedAt and merge SHA were read independently and each listed merge
was verified as an ancestor of the source snapshot. This establishes landed
source, not a fresh runtime pass for every criterion.

| Work | PR | Merge SHA |
| --- | --- | --- |
| Social data/policy/federated signup | #794/#798/#804 | `851e092270ab477087c70b6a40ed3981d962a32e` |
| Local signup/mail | #849 | `5c3f969f692268e5c0bc7f5b9acc860f3bff0f48` |
| OAuth2 | #848 | `4a22dad4796a7d4622d0477e1230ec741556cea8` |
| Invitation claim | #883 | `85ba9992a65b04f1bd79278af4fe609625a19955` |
| Credential establishment | #925 | `648a5a52b7abb403b69186bc496ce367bf117582` |
| CLI browser/device login | #924 | `ad4c7b124addae47d79bc715f70d51ba326cf271` |
| Social acceptance | #928 | `4af540bf377cc1e2a34163d4de150d395bbea5a9` |
| Pre-freeze identity slice | #646 | `9827af64962a36e2e31906b48906fd59c7a36c1c` |
| Condition-reporting design / real-cluster suite | #792 / #837 | `5eff97bed0d6f18db2cf6027368447e135250e0f` / `c9c809319da6baba5d8ad99ce9d34c87288cfc08` |
| Shared adapter / file destinations | #592 / #892 | `94d27a5955258c29eed559b5ddbf86eebdc52fbc` / `2149fa51e5762a2fe4dd0bd618293d87f609348f` |
| Transit / dynamic credentials | #826 / #593 | `ebd2d2f8c0cd154963fe9919a56e79c049c929e2` / `ec90c72498b829d88f6a97fe0a9994a572fe7674` |
| Compose / Kubernetes | #178 / #176 | `811d650f0c54f6f19090b4ae10eed8fb071f94ea` / `58b93aae1428b6f0dbbcd8ef00c3bc32c2e27971` |
| AWS / Cloudflare implementation | #828 / #822 | `20fb9b52c3f4e8383b256ed34442e5175a334dd0` / `4460e82a21c843194702236320cf85da7fd1dcb4` |
| Failed importer descendant cleanup | #937 | `6769ee55dcd4087eeaf42547e9abde01c4f0818c` |

#937 merged at 2026-10-16 16:40:31 UTC. Its
[behavior/evidence handoff](936-importer-descendant-cleanup.md) preserves the
Windows runtime limitation. Independently read workflow metadata reports
[trusted merge CI 38929885843](https://github.com/Hikyo-Org/Hikyo/actions/runs/38929885843)
success at exactly `6769ee55...`. The parent additionally verified all 48 jobs;
this documentation owner did not replay them.

Historical [full-main CI 38887776523](https://github.com/Hikyo-Org/Hikyo/actions/runs/38887776523)
at `4af540bf...`, [social candidate CI 38881592174](https://github.com/Hikyo-Org/Hikyo/actions/runs/38881592174)
at `4151c291...` and [docs deployment 38890803193](https://github.com/Hikyo-Org/Hikyo/actions/runs/38890803193)
at `4af540bf...` independently report success. #613's
[closure record](https://github.com/Hikyo-Org/Hikyo/issues/613#issuecomment-6215549123)
and #791's [runtime record](https://github.com/Hikyo-Org/Hikyo/issues/791#issuecomment-6216662194)
retain their exact-source limitations. #615 and #791 were independently read
as closed. None of this evidence claims official release or current external
provider acceptance. The 2026-10-16 stable-tag lookup returned 404.

## Validation and continuation

The owner authorized lightweight validation for this two-file documentation
increment while the shared build/browser lane remains occupied. On Node
26.7.0, both canonical read-only checks passed:

- `node scripts/ci/check-doc-status.mjs --check`: 35 ledger entries verified.
  The isolated checkout initially lacked Zod. The check ran through an existing
  checkout's byte-identical canonical script with the pinned Zod 4.4.3 and
  `--root` targeting this candidate. No dependency was installed and no status
  output was regenerated.
- `node docs/site/scripts/prepare-content.mjs --check`: passed without writes.
- Whitespace, relative-link/source-reference and portable-path checks passed.
  Files owned by #79, #15 and #589 are unchanged in this increment.

Independent Sol 6.1 high source review completed with no actionable findings
across the two-file draft and 23-issue map. The owner independently checked the
underlying source/evidence, relative links and excluded-file boundaries. This
is source consistency review only; no build, runtime, provider or release
acceptance is inferred. No full build, test suite, browser or server was run;
the applicable hosted required checks remain separate merge gates.

Continue through signed+DCO commit/range verification, GitHub Verified push,
registered PR, current-head review repairs and protected verified main merge.
Consume parent-routed #15/#589/#79 and merge evidence without editing their
in-flight files or polling parent-owned watches. Keep #527 ongoing and #79/#41
open; publication and deployment remain separately authorized work.
