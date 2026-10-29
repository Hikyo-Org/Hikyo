# #156: source-bound Transit amendment closeout

Source baseline: `ce5e85835383b6428676ff595e2a0c1a72f6a088`, tree
`0a15a814ecb0e17e98a519cc129950dd0e94b02c`, assessed 2026-10-29.

Status: **D3 conformance repair authorized; final-byte independent review
and formal closeout pending**. The coordinator clarified that the existing
implementation/fix authorization covers option A as preservation of the
already approved contract. This is not a new product-policy decision, ADR
exception or waiver. D3 and all five normative excerpts remain unchanged.

Independent R1 Standards and Spec reviews of head
`c69576b74e12791439e16d51666b821255bad863`, tree
`d2d1c258b99b4e06ebcf3d6fef95623e62a666ef`, both requested changes for the
same two P2 findings. The permission selector's adjacent #154 capture was
corrected in `274ee392f94f1df81e1acc170e970c4ab54ffb19`. The D3 consumer
repair and focused regressions now address the other finding. R1 remains
historically CHANGES REQUESTED; fresh review receipts must bind the changed
final bytes. Maintainer/security approval, scoped owner-ticket ceremony and
verified merge remain separate gates. Delivered #156/#820 stay closed.

## Exact disposition being reviewed

Retain the owner's September 26 bounded Transit promotion unchanged: software
custody; environment-scoped keys; the closed algorithm/operation set;
versioned fail-closed lifecycle; explicit `crypto-use`/`crypto-manage` grants;
decision-only audit; bounded API/CLI/generated-client/WebUI surfaces; test-only
external custody. No HSM provider, key import/export or Vault replacement is
promoted. #826 delivered implementation and #965 delivered the WebUI; #156
and #820 remain closed.

The outstanding item is the original amendment review and its formal
recording. October 28's T1 retained that gate; it did not prohibit completing
it. On October 29 the owner requested this actual review under the updated
independent Standards/Spec policy. The new review must cover the final bound
contract and its relevant consumers, rather than only the status correction
reviewed in #1071. That earlier ordinary documentation review is not reused
as the missing #156 amendment review.

This closeout does not alter the general locked-ADR procedure. It supplies a
precise review route and evidence for #156. Any finding that needs a policy
change, or any unresolved interpretation of the scoped review requirement,
must be reported explicitly for the owner; it is not silently waived.

## Authorship and policy provenance

The introducing [#826](https://github.com/Hikyo-Org/Hikyo/pull/826) merge
`ebd2d2f8c0cd154963fe9919a56e79c049c929e2` records Claude Opus 5.5 authorship
and Claude sessions in its retained commit message. It also contains later
repairs and bot contributions, so the entire implementation is not labeled
exclusively Anthropic-authored. The original normative Transit body from
`## Context` through EOF is 15,023 bytes, SHA-256
`f2ef7f9fa679a79b6da905abba96724f83210249ac7fc3f656e3194d31c56591`.

Current Git history has three later edits to the Transit ADR: #965's WebUI
paragraph and #1071's two status-record commits. Removing only the seven-line
WebUI paragraph from the current normative body reproduces the introducing
body byte for byte. The September normative contract therefore has evidenced
Claude provenance; the later WebUI and governance portions are reported
separately as OpenAI/mixed continuation. Commit trailers alone do not prove
every line's provider, so unproven line-level authorship remains unknown.

The owner's October 29 policy removes mandatory cross-model and daily-choice
prompts while retaining independent adversarial Standards and Spec review.
The configured available reviewer is OpenAI `gpt-6.1-sol/high`; Anthropic
subscription access remains unavailable. OpenAI review of evidenced
Claude-authored text is provider-diverse review of that portion. OpenAI review
of later OpenAI text is same-provider independent review. Neither distinction
can be inferred from the current branch author's provider alone.

Review the complete effective amendment and report those coverage classes
honestly. No whole-feature cross-provider CLEAN claim is made for mixed or
unknown provenance. The September and October 28 skipped passes stay
historically SKIPPED. The updated policy is not relabeled as a retrospective
successful review, and no exception from #1014, #966, #589 or #162 transfers.

## Frozen normative excerpts

These SHA-256 values bind UTF-8 bytes, including the terminating newline.
They remain unchanged by the closeout's status notes. The pointer selector
below extracts only the #156 paragraph, not unrelated amendments sharing a
file. The full review also covers all new closeout text at the final PR head.

| Path | Selector | Bytes | SHA-256 |
| --- | --- | ---: | --- |
| `docs/adr/transit.md` | `## Context` through EOF | 15488 | `d324ce5d11b37acbb65d98d25d7132f0ad4f566be89d918915a10e1f49745ce4` |
| `docs/adr/mvp-boundary.md` | September 26 #156 blockquote | 867 | `1a0591e0a5115a7e5b5d00f4331ef93e6e81f11fe9a4f915bd2fbd5f76af7536` |
| `docs/adr/encryption-model.md` | September 26 #156 blockquote | 1380 | `48eb8d135801b62d835277f91c3feb2ebf01f5281d5a803856ca3c4e2cce800d` |
| `docs/adr/permission-model.md` | September 26 #156 blockquote | 865 | `f6dcc0f69c8766056dd610058501ee52642cea02ee0f1d96907aefaef78617f5` |
| `docs/adr/tenant-isolation.md` | September 26 #156 blockquote | 584 | `beb616ed61c93684e88ea9fa06168fc4d290391aaae99046b8075421f70ed508` |

From the actual review checkout, this read-only command emits the identities:

```sh
git rev-parse HEAD 'HEAD^{tree}'
python3 - <<'PY'
from pathlib import Path
import hashlib

paths = [
    'docs/adr/transit.md',
    'docs/adr/mvp-boundary.md',
    'docs/adr/encryption-model.md',
    'docs/adr/permission-model.md',
    'docs/adr/tenant-isolation.md',
]
for name in paths:
    raw = Path(name).read_bytes()
    if name == paths[0]:
        part = raw[raw.index(b'## Context\n'):]
    else:
        lines = raw.splitlines(keepends=True)
        starts = [i for i, line in enumerate(lines) if line.startswith(
            b'> **Declared amendment (2026-09-26, [#156]')]
        if len(starts) != 1:
            raise SystemExit(f'{name}: expected one #156 amendment')
        start = starts[0]
        end = start + 1
        while (end < len(lines) and lines[end].startswith(b'>')
               and lines[end].strip() != b'>'
               and not lines[end].startswith(b'> **')):
            end += 1
        part = b''.join(lines[start:end])
    print(name, len(part), hashlib.sha256(part).hexdigest())
PY
```

Review receipts must record these values, the actual source head/tree, their
Standards or Spec axis, inspected consumers, findings and final disposition.
A changed covered excerpt or closeout text needs a new scoped review/recheck;
a later main SHA cannot inherit review merely because its PR title matches.

## Required substantive coverage

- D1-D4: environment identity, immutable purpose/allowed operations, project
  DEK custody, HKDF/AAD binding, strict wire/version parsing and no fallback.
- D5-D6: version floors and retained-version bounds, concurrent rotation,
  compromise recovery, cancellation/destruction fences and backup limits.
- D7-D9: grant narrowing, machine/human capability split, scoped concealment,
  decision-only audit and installation-wide admission. The scheduler gains
  only the declared rotation/purge doors.
- D10/invariants: generated API/CLI parity, non-export, display-once WebUI
  state and the existing known-answer, fuzz, both-engine and custody tests.

Relevant source owners include `internal/crypto/transit.go`,
`internal/transit/`, `internal/service/transit.go`, `transit_ops.go`,
`internal/store/transit.go`, the Transit queries/migrations on both engines,
`internal/authz/registry.go`, `internal/audit/registry.go`, API/CLI handlers
and the WebUI Transit owners. A source review must identify which consumers
it inspected; this list does not assert a fresh test execution or full
implementation security audit.

## D3 conformance repair, preserving the approved rule

Both independent configured OpenAI `gpt-6.1-sol/high` R1 reviews found the
same pre-existing mismatch: D3 requires every operation to refuse unavailable
or unregistered custody, but `Transit.Verify` read the stored public key and
verified locally without resolving/checking its provider. The first source
packet offered an A/B policy choice. The coordinator corrected that scope:
A is a conformance repair already authorized by the original implementation
and fix instruction and the 1.0 completion goal. No additional product-policy
approval is needed to preserve D3. B is not selected; D3 is not weakened.

The repair shares the existing provider-kind resolution, `Available` check,
`ErrTransitCustodyUnavailable` error and durable `custody-unavailable` failure
audit in `transitUse.availableCustody`. The existing material-bearing path
uses the same helper. Public verification calls it only after the ordinary
authorization, state, version/compromise and public-version-row checks, then
runs `crypto.VerifyTransitSignature` locally. It does not call `material`,
request unsealing or signing, or fall back to another provider.

`TestTransitVerifyCustodyAvailability` uses the existing `forEngines` harness
and software/mock external providers. Its immediate matrix covers available,
down, missing and recovered custody; valid and mismatched signatures; unknown
and below-floor versions; disabled and compromised keys; cross-org/sibling
scope concealment; and one durable failure audit per unavailable verification
with no result returned. Guarded providers reject every material-bearing
operation and record calls to the wrong provider. The verification service
has no keyring. Both available and recovered checks must still verify locally,
while preceding policy refusals must not contact custody at all. Existing
`assertTransitAuditClean` checks exclude input/signature material from audit.

No live external provider, new infrastructure, schema, wire contract or UI is
introduced. Both-engine execution uses isolated fixture databases on an
existing local PostgreSQL 18.6 test service plus the normal temporary SQLite
fixture. Only task-owned databases may be dropped; the existing service and
other databases remain protected. Exact red/green, consumer and race commands
and results are recorded in the accompanying PR, tied to its source head.

The R1 OpenAI receipts are provider-diverse for evidenced Claude-origin
contract portions and independent same-provider review for later OpenAI
portions. Their source-only limitations and historical skipped passes remain
intact. Fresh Standards/Spec rechecks cover the changed helper, verification
consumer, regressions, selector and closeout text, inheriting unchanged R1
coverage only where the frozen excerpt/source identities still match.

## Focused validation evidence

The new regression was run before the fix. On both SQLite and PostgreSQL,
for software and mock external custody, down and missing providers incorrectly
returned `Valid=true` with no error. The available/recovered cases also proved
that availability was never checked. Existing preceding policy refusals passed.

After the fix, the new regression and existing Transit end-to-end consumer
suite passed on both engines. The focused service owner tests, new both-engine
race test and owner-package vet passed. No assertion, deadline, fixture skip
or security boundary was weakened. With `HIKYO_TEST_POSTGRES_DSN` configured
for an isolated fixture database, the executed commands were:

```sh
GOMAXPROCS=2 go test -p 2 -parallel 2 ./internal/isolation -run '^TestTransit(VerifyCustodyAvailability|EndToEnd)$' -count=1 -v
GOMAXPROCS=2 go test -p 2 -parallel 2 ./internal/service -run '^TestTransit' -count=1
GOMAXPROCS=2 go test -race -p 2 -parallel 2 ./internal/isolation -run '^TestTransitVerifyCustodyAvailability$' -count=1 -v
GOMAXPROCS=2 go vet -p 2 ./internal/service ./internal/isolation
```

The complete PR diff still requires its shared feedback plan/run and exact
final-head hosted readback. Frozen normative identities and Markdown paths
were checked without changing the contract. These fixture executions do not
establish real HSM/provider, deployed instance or integrated #79 acceptance.

## Formal closeout and exact remaining action

1. Complete the authorized D3 conformance repair and its immediate regression
   evidence, then obtain fresh independent Standards and Spec rechecks of the
   changed final bytes. Preserve R1's changes-requested receipts, the selector
   correction and provenance classification. Do not relabel R1 CLEAN or invent
   a new owner-policy approval requirement for preserving D3.
2. Record the #156-scoped ceremony on the owning ADR tickets: MVP #26,
   encryption #14, permission #15 and tenant isolation #23. Preserve every
   unrelated obligation on shared tickets. Do not reopen delivered #156/#820
   as if implementation were missing. The parent/maintainer owns the public
   owner-ticket ceremony and recorded #156 review disposition; this
   preparation posts no issue messages and requests no new product-policy
   approval for preserving D3.
3. Obtain the existing human merge authorization for this new source/docs PR,
   then use the protected queue. Verify actual merge, ancestry and unchanged
   reviewed excerpts before declaring the amendment debt closed. Do not
   close shared tickets globally merely because the Transit slice is complete.
4. Return the exact review/merge links to #79's coordinator. #79 retains
   integrated final candidate, both-engine/runtime, freeze, release trust,
   tag, publication and deployment acceptance.

No new feature, provider run, production mutation, payment, release, tag or
deployment is authorized. Vault #162's approved membership and reviewed
PR #1091 remain with their owner. Historical social A7 is not reopened.
Source validation includes the complete-diff feedback driver, Markdown links,
frozen excerpt comparison and the immediate both-engine/consumer/race checks
recorded against the final source. Live provider and integrated release
acceptance remain NOTRUN by this lane; historical or fixture tests do not
establish those gates. HTML companions are ignored local chat artifacts only.
