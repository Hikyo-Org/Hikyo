# #966: explicit system-CA CLI trust

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/966>.
Current source baseline: `d02242217502de6f23f528396be572072c34c085`, inspected
2026-10-29. Implementation [#967](https://github.com/Hikyo-Org/Hikyo/pull/967)
merged as `69e32a25a6431426e839cb463aafd78c781ef15d`; preparation
[#1004](https://github.com/Hikyo-Org/Hikyo/pull/1004) merged as
`f3581e7bb6ec65cb416d5213f7e55819ef19782f`; reconciliation
[#1073](https://github.com/Hikyo-Org/Hikyo/pull/1073) merged on 2026-10-28 at
10:50:15 UTC as `970ac0db7c6024f91dedcefe34db7ca560b247e3`.
These are ancestors of this baseline. Merge and closed issue state do not
establish the missing acceptance gates.

## Current disposition

**Reviewable outcome, exact-contract and operational downstream acceptance HOLD.**
This continuation adds a repeatable consumer test against real Hikyo machine
authentication and grants, independently verifies the current signed nightly,
and supplies a reproduced and tested downstream adapter repair. It does not
change production TLS code, operational credentials or runner configuration.
The five normative clauses in the [proposed amendment](../adr/api-cli-surface.md#proposed-explicit-https-trust-amendment-966)
remain unchanged.

The owner explicitly requested `gpt-6.1-sol` with maximum reasoning and a fresh
`gpt-6-astra` outcome validator with `xhigh` reasoning for #966 on 2026-10-29.
That route is authorized; no repeat permission request is needed. It remains
OpenAI same-provider validation. Cross-provider review is **SKIPPED**, because
the configured Anthropic subscription reviewer is unavailable. No paid or
alternate provider, general ADR waiver, Transit or Vault exception is inferred.
The fresh validator must inspect the frozen base/head/tree and blob inventory,
this contract, the actual runtime receipts and every outstanding constraint.
Its exact candidate and result belong in the PR's review record, separately
from historical reviews. A requested or running review is not CLEAN.

The inspected original owner conversation selected preparation of the proposal
and ended with an unanswered exact-contract approval request. The documented
legacy SQLite snapshot has no matching thread rows. The latest explicit
request selects the reviewer but does not expressly accept the five clauses.
Neither #1004/#1073's merges nor closed #25/#966 supplies that missing decision.
No private transcript is published in this packet.

## Evidence by gate

| Gate | Current evidence and limit |
| --- | --- |
| Source | #967, #1004 and #1073 merged. This candidate adds tests and Markdown evidence/operations changes; production trust behavior is unchanged. |
| Contract | **HOLD**: explicit acceptance of the exact five clauses is still required. The current review route is authorized. |
| Fresh review | Fresh Astra `xhigh` outcome validation is required for this exact candidate. See the PR review record for its completed verdict and any re-check. Historical same-provider reviews are not reused as current proof. Cross-provider **SKIPPED**. |
| Hosted publication | [Nightly release](https://github.com/Hikyo-Org/Hikyo/releases/tag/v0.0.1-nightly.20261029.99.gd0224221), release ID `427637314`, published 2026-10-29 at 08:56:29 UTC. [Run 40837498647](https://github.com/Hikyo-Org/Hikyo/actions/runs/40837498647) succeeded for source `d02242217502de6f23f528396be572072c34c085`. |
| Independent signed verification | **PASS**: complete 24-file download authenticated against independently pinned repository public trust; 22 signed inventory payloads verified. Exact identity and CLI provenance below. |
| Real authorization fixture | **PASS**: source CLI and the verified released Linux/arm64 CLI read a real fixture environment through Hikyo API authentication, machine credential minting and scoped grants. Certificate, redirect, protected identity, sibling-project and revoked-credential refusals passed. Disposable SQLite fixture, not an operational service or pipeline. |
| Actual consumer helper | **Reproduced and repair validated**: fresh DBugIT source rejects `system-ca` because it requires a pin. The minimal repair passes the actual helper and existing owner/consumer tests. [Exact patch, source/blob identities and apply procedure](966-dbugit-system-ca-repair.md). It has not been applied to DBugIT's operational source or CI. |
| Named downstream version/bundle update and approved consumer run | **NOTRUN/HOLD**: discovery found the operational legacy-SPKI bundle and preview/staging/release credential names, not a #966-approved isolated target/credential/runner configuration. No secret value was read. Local fixture proof does not close this gate. |
| Merge | **NOTRUN**: this continuation is authorized through a signed/DCO PR and review, without merge. Head feedback, CodeQL and full merge-group validation remain distinct evidence. |
| Production, stable 1.0 publication and deployment | **NOTRUN**, outside this task. No production upgrade, stable-policy activation, tag, Apple submission, release or image publication is authorized. |

## Verified released CLI, 2026-10-29

The repository's current `scripts/release/nightly` verifier returned:

```json
{"profile":"nightly/v1","version":"0.0.1-nightly.20261029.99.gd0224221","sequence":99,"commit":"d02242217502de6f23f528396be572072c34c085","compatibility_sha256":"2e5c1d26fe48b64abec24c40c8b806ed5ad0caaad15f7823ccea6cdb98eb6c9d","manifest_sha256":"776012927e0fad1c021d5a192324d57301c2715c8115698e45ccc87405470711"}
```

| Verified material | SHA-256 |
| --- | --- |
| Linux/arm64 archive, asset `687941779` | `44eb39091af0dee0ed62f7d0ae1d8d4564ea4120f4c8369ea58965d9e0732e76` |
| Extracted Linux/arm64 CLI, matching signed binary provenance | `134c8aa1a9dfd68c2489cef8979ccd61c4e1d301f8634fe0f353ab676c15186b` |
| Independently authorized nightly policy | `afaeb19fc0299f700234a74aa0817f77ce0833853b327a9d9d9d126d25d5b04f` |

The released CLI was executed only after full verification and binary digest
comparison. A disposable container used the existing Linux/arm64 Node image
`sha256:5c4012d11d9857c42713ba511fbc19645cac1706bb710d51d42b0d301dff7f88`,
network disabled, read-only root and source mounts, dropped capabilities,
private temporary state, two CPUs and a 2 GiB memory bound. No operator
credential or proxy environment was inherited. Go loaded only the disposable
fixture root through the child's `SSL_CERT_FILE` and empty `SSL_CERT_DIR`.
Host trust was unchanged. Fixture keys and credentials lived in temporary
storage, and no operational database was loaded.

The current-source Go fixture ran the real API handler, authentication service,
credential mint/revoke service and SQLite authorization store. It granted one
fresh automation identity project read access and returned the existing fixture
environment. An existing sibling project was denied, and revoking the credential
caused the next read to fail authentication. No canned environment response
or fake bearer comparison supplied these authorization results.

| Exercised behavior | Observed result |
| --- | --- |
| Protected bundle/context import | Exit `0`, zero HTTP requests. |
| Valid leaf and renewed leaf with a different SPKI | Environment reads succeeded, including the actual repaired DBugIT helper on each leaf. |
| Expired, not-yet-valid, wrong-host, untrusted leaf | Exit `6`, zero HTTP requests on each refusal. |
| Cross-origin and same-origin redirects | Exit `4`, exactly one original request each, zero forwarded requests. |
| Protected origin/mode/pin conflict and repository origin/mode/pin injection | Exit `4`, zero HTTP requests. |
| Legacy and explicit SPKI; changed key; missing pin | Original key accepted, changed key and missing pin refused; no fallback. |
| Deliberate SPKI to system-CA re-establishment | Explicit deletion and reimport succeeded. |
| Sibling project; revoked credential | Real API returned not-found and authentication refusal respectively. |
| CLI/helper diagnostics | No fixture bearer appeared in stdout or stderr. |

This is **PASS: isolated signed-consumer transport and real fixture authorization**.
It is distinct from the earlier synthetic HTTP fixture, operational DBugIT CI,
a managed proxy rotation or production acceptance.

To repeat public verification from this source with Go 1.27.3, use a new empty
operator-selected download directory:

```bash
set -euo pipefail
: "${HIKYO_NIGHTLY_REVIEW_DIR:?Set a new empty download directory}"
gh release download v0.0.1-nightly.20261029.99.gd0224221 \
  --repo Hikyo-Org/Hikyo --dir "$HIKYO_NIGHTLY_REVIEW_DIR"
GOMAXPROCS=2 go run -p 2 ./scripts/release/nightly verify --trust release/trust \
  --directory "$HIKYO_NIGHTLY_REVIEW_DIR"
```

Expect the identity above. Recheck public trust and revocations before a later
use. Release-page checksums alone are insufficient. See the
[isolated authorization command](../operations/cli-tls-trust.md#repeat-the-isolated-authorization-check)
for source and released-consumer execution.

## Discovery and executable remaining work

Discovery used current DBugIT main, its actual CI helper/workflow, authenticated
Forgejo repository/variable/secret-name metadata, runner metadata and retained
local upgrade-rehearsal inventories. Forgejo's protected bundle currently names
`https://hikyo.dbugit.io` with a legacy SPKI pin. Preview, staging and release
credential names exist, but their values were not fetched. Repository runner
registration returned an empty list; this does not prove no inherited runner
exists. Recent job metadata did not identify an isolated TLS runner. Retained
upgrade rehearsals are historical, have no running owned server, and do not
supply current #966 acceptance authority. Protected recovery/admin files were
not opened, and no production credential was reused.

1. **Owner contract decision:** approve the unchanged five clauses with the
   explicitly requested #966 Astra review disposition after reading the concrete
   result, or keep contract **HOLD**. No additional permission is needed to run
   the requested review. If approved, record the durable decision, mark the
   amendment owner-locked and operative on the corrective PR's authorized merge,
   and reconcile #25 with the decision/review/merge evidence. Keep the actual
   cross-provider review **SKIPPED**.
2. **Named downstream gate:** select the authorized isolated runner and target,
   paired non-production project-read credential, protected system-CA bundle,
   exact CLI version and prior configuration retained for rollback. Apply the
   reviewed consumer repair through the downstream repository's normal source
   review. Inspect protected storage first; if the selected credential is still
   missing, obtain it through the private secret tool. Do not ask for a token in
   chat, reuse an operational credential, change a shared protected bundle or
   upgrade production to satisfy this gate.
3. **Record acceptance:** import the approved file bundle into fresh private
   state and run the [documented authenticated read](../operations/cli-tls-trust.md#protected-provisioning-for-ci).
   Record exact source/release/digests, runner and protected-bundle identities
   without credentials, origin, read result and rollback reference. The local
   DBugIT wrapper receives JSON text; the CLI receives a file path. A signed
   nightly satisfies publication without stable 1.0, but not the downstream gate.

#966 and #25 are already closed. This packet prepares their correction rather
than treating closed state as proof or sending external issue messages. Do not
claim #966 complete or close a new reconciliation record until all required
gates above are evidenced. Merge requires a later explicit instruction.

## Current validation and custody

Go 1.27.3 and Node 26.12.1 were used, following the current direct-command
resource policy with at most two Go workers. Source CLI/command owner suites,
focused TLS race checks and the new real-authorization source fixture passed.
The Linux fixture runner was cross-compiled from this candidate; the released
consumer executable was the unchanged signed binary, not a rebuild. DBugIT's
existing preview, teardown and release-script tests passed on the minimally
repaired source snapshot. These script tests use their established mocks;
the separate signed-consumer fixture supplies real authentication evidence.

The complete-diff fast-feedback plan selects the Go owner, with no frontend,
workflow, shell or docs-site domains. Its checks passed, together with the explicit docs preparation test, targeted
owner vet, the new fixture under the race detector, 44 local link-path checks
and unchanged-contract verification. Record the exact head/tree,
source and blob coverage, local receipts, hosted checks and Astra result in the
PR review record. A passing feedback compilation is not execution of these
runtime tests or full merge-group validation.

Retain public receipts, the failure witness and reviewed patch while review is
active. Disposable containers use automatic removal; remove owned downloads,
extracted executables, test binaries and source snapshots after the validator
finishes. Keep local Markdown/HTML reports ignored. No shared service, image,
cache, rehearsal archive or user worktree belongs to this task's cleanup.

The following sections are historical validation and implementation records.
The current gate disposition above takes precedence.

## Amendment validation, 2026-10-19

Fresh checks on Go 1.27.2/macOS passed:

```sh
go test -p 2 -parallel 2 ./internal/cli ./cmd/hikyo -count=1
go test -p 2 -parallel 2 -race ./internal/cli -run 'TestNewHTTPSInteractive|TestRemoteAdd|TestTLSIdentityInspection|TestExplicitSPKI|TestExistingAndProvisionedSPKI|TestContextCreateExplicitSPKI|TestSystemCATrust|TestTrustModes|TestProvisionedTrustMode|TestPinnedClient' -count=1
node --test docs/site/scripts/test-prepare-content.mjs
git diff --check
```

The CLI suite completed in 8.889 seconds, the command-entry suite in 1.211
seconds, and focused race checks in 10.555 seconds. The documentation preparation
test passed, and all 39 relative Markdown links in the changed documents resolved.

The initial full CLI run exposed a fixture assumption in
`TestTrustStoreRejectsUncontrolledFilesystemObjects/shared_file`: the validation
runner's `0077` umask created the requested `0644` fixture as `0600`, so the
loader correctly accepted it. A focused rerun reproduced that failure. The
fixture now explicitly sets `0644` after creation; the focused test, full CLI
suite and race checks passed under the same `0077` umask. Production permission
checks and runner policy are unchanged.

These are fresh local checks. Hosted checks belong to the amendment PR. No
signed CLI artifact or live downstream consumer was exercised by these tests.

## Implemented behavior

The CLI trust entry and provisioned bundle accept `tls_mode: "system-ca"` for
HTTPS. That mode uses normal Go TLS chain, validity and hostname verification
against system trust. Leaf-key rotation is acceptable when the new certificate
passes that verification. Production leaves `RootCAs` unset and never enables
`InsecureSkipVerify` or a pin callback in this mode.

Omitted modes in stored entries and provisioned bundles preserve legacy SPKI
verification; explicit `spki` is equivalent. New interactive HTTPS establishment
requires an explicit `--tls-mode spki` or `--tls-mode system-ca` selection before
connecting. This is the owner's selected policy following the CodeQL review.
Missing pins never select system CA verification. Unknown, empty or null JSON
modes and unknown or empty explicit flags are refused. Conflicting mode, pin
and socket combinations are refused. Existing local-socket policy is retained.

`login` and `context create` accept `--tls-mode spki|system-ca`. Interactive
system-CA establishment requires a controlling terminal, verifies TLS before
recording, and names the origin, mode and key-rotation tradeoff in confirmation.
Provisioning imports the fixed origin and mode through the credential's
protected channel. An explicitly named URL or instance reference must agree
with the bundle. An established origin, effective mode, pin or socket cannot
be replaced without deliberate deletion and re-establishment.

Trust stores, bundles and repository `.hikyo.json` files now reject unknown
fields. A repository may reference established trust but cannot add an origin,
pin or mode. Redirects remain rejected without a second credential-bearing
request. No server, artifact admission, grants, login eligibility or reveal
policy changed.

The [operations guide](../operations/cli-tls-trust.md) contains the proposed
schema, compatible legacy behavior, interactive and protected CI commands,
migration/refusal rules, release verification and downstream update procedure.
The [API/CLI ADR](../adr/api-cli-surface.md) carries a clearly proposed,
non-operative amendment. It does not claim approval or a review waiver.

## Implementation validation, 2026-10-18

All checks below passed against the final implementation on Go 1.27.2/macOS:

```sh
go test ./internal/cli ./cmd/hikyo -count=1
go test -race ./internal/cli -run 'TestNewHTTPSInteractive|TestRemoteAdd|TestTLSIdentityInspection|TestExplicitSPKI|TestExistingAndProvisionedSPKI|TestContextCreateExplicitSPKI|TestSystemCATrust|TestTrustModes|TestProvisionedTrustMode|TestPinnedClient' -count=1
go vet ./internal/cli
go build ./cmd/hikyo
scripts/ci/check-go-imports.sh
node --test docs/site/scripts/test-prepare-content.mjs
git diff --check
```

The corrected full CLI suite completed in 9.711 seconds, the command-entry suite
in 1.175 seconds and focused race checks in 9.006 seconds. Before the consent
correction, head `7dc30f973` had passed the earlier CLI, focused race and command
checks in 9.618, 6.171 and 1.315 seconds respectively; those earlier passes did
not cover the first-contact permission gap. The API freeze
guard reported dormant because the `v1.0.0` tag does not exist; that is not an
active compatibility verification.

The certificate lifecycle test uses an isolated child process with Go's
supported fallback-root mechanism and `x509usefallbackroots=1`. It exercises the
production configuration with unset `RootCAs`, including valid renewal with a
different key at the same origin. Untrusted, expired and wrong-host certificates
and a changed SPKI key produce no HTTP request. Redirects produce no forwarded
request. The test also drives actual CLI context creation and a machine
environment read, and proves repository mode introduction sends no credential.
The host trust store is unchanged.

An independent smoke check executed the built binary with isolated state and
no inherited credentials. It verified CA import, malformed modes/fields on
fresh stores, missing pins, mode conflicts, non-TTY and empty-flag refusal,
deliberate migration, and command help. Its initial harness checked multicall
overview help instead of command help and failed that assertion; the corrected
`context --help` check passed. No application change was needed for that failure.

Relative documentation targets were checked. Structural Graphify refresh is
local and ignored by Git; documentation semantics were not regenerated.

## Implementation review history, 2026-10-18

The initial parent review and a separate same-provider review checked the
implementation against live #966 and the repository rules without identifying
the first-contact consent gap later reported by CodeQL. That reviewer identified a masked-test
weakness; malformed bundles now run against fresh stores as well as existing
stores, and the reviewer verified that correction. This is source review, not
cross-provider approval.

The current-head CodeQL review of `7dc30f973` subsequently reported
[alert 186](https://github.com/Hikyo-Org/Hikyo/security/code-scanning/186):
the SPKI inspection handshake disabled CA checks before terminal confirmation.
The owner selected explicit mode choice for new HTTPS establishment on
2026-10-18. The correction refuses omitted modes before network access, preserves
stored/provisioned pin compatibility, and requires explicit pre-probe consent
in the shared SPKI-only remote-add ceremony. Fingerprint confirmation remains
separate from permission to inspect. The inspection API has no implicit
unverified default. The alert is not dismissed and no new scan suppression is
introduced.

The new regression tests failed before the correction: missing-mode login
opened two connections, sent one HTTP request and persisted a pin; context
creation opened one connection and persisted a pin; remote inspection without
a terminal or after declined consent still connected once. Empty/unknown
inspection policies also dialed. The corrected tests pass with zero accepted
connections on those refusal paths. Positive tests retain explicit self-signed
SPKI establishment and separately enforce fingerprint acceptance before trust
is stored. The shared remote ceremony separately enforces fingerprint
confirmation before any metadata request or credential prompt.

The parent and independent same-provider reviewer found no remaining production
issue in the correction. The reviewer caught stale validation commands/timings
in this handoff; those now describe the corrected test run above. A fresh built
binary smoke check verified missing-mode and non-TTY refusal plus legacy bundle
and stored-pin compatibility. Hosted CodeQL and review state must be checked
against the exact pushed head; local tests and source review alone do not
establish alert closure.

The [governance amendment procedure](../../GOVERNANCE.md#amendment-procedure)
requires reopening the originating ticket, adversarial cross-model review and
recording the amendment. The originating API/CLI ticket is
[#25](https://github.com/Hikyo-Org/Hikyo/issues/25), which was closed at the implementation handoff. The owner must approve the proposed contract and
authorize a qualifying review path or explicitly waive that review gate before
the amendment can be locked. Claude subscription review is unavailable under
the current working rules; another provider or paid route was not substituted.

The initial PR head passed Go/frontend feedback and language analysis, but its
combined CodeQL security check failed on the alert above. CodeRabbit, Qodo and
MergeWatch did not produce reviews because of quota or billing limits; their
availability notices are not approval. No Windows/Linux runtime, live
managed-proxy rotation, artifact publication/signature verification or downstream
CI acceptance was performed.
The local unsigned development binary was not a signed CLI release. The
implementation subsequently merged as recorded above. Keep #966 open through
final ADR approval and amendment merge, verified signed publication and the
documented consumer update. Release and downstream rollout retain their separate
authorization gates.
