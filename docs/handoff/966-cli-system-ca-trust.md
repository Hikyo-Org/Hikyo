# #966: explicit system-CA CLI trust

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/966>.
Baseline: `7bef4d670d2dce0defb963326f0e1e05cfdb6798`.
Implementation: [PR #967](https://github.com/Hikyo-Org/Hikyo/pull/967), merged on
2026-10-18 as `69e32a25a6431426e839cb463aafd78c781ef15d`.
Current inspection: 2026-10-28, canonical `Hikyo-Org/Hikyo` main
`24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
Historical preparation baseline: `eb991c991bcad853590cb0ab2c09367052a92c02`.

## Current disposition

**Governance HOLD.** The implementation and preparation PRs merged. The inspected
issue, PR and original owner conversation do not record final acceptance of
the exact five-clause contract and #966-only review exception. The contract
text is unchanged by this correction; its approval status remains proposed.
No general governance exception, merge authority or production upgrade authority
is inferred from the readiness-task approval.

The public record is internally inconsistent:

- [#25's amendment record](https://github.com/Hikyo-Org/Hikyo/issues/25#issuecomment-6262097215)
  reopened the ticket on October 19 and explicitly retained final owner approval,
  signed publication and isolated authenticated downstream acceptance.
- [#1004](https://github.com/Hikyo-Org/Hikyo/pull/1004) merged on October 19 at
  08:57:52 UTC as `f3581e7bb6ec65cb416d5213f7e55819ef19782f`, without the
  proposed text becoming owner-locked. #966 closed one second later, and #25
  also closed. Both implementation and preparation merges are current-main
  ancestors. Those facts establish merge and ticket state, not exact-contract
  approval or criterion 5 acceptance.
- [#1004's review record](https://github.com/Hikyo-Org/Hikyo/pull/1004#issuecomment-6262095983)
  records separate OpenAI Standards CLEAN and Spec SOUND results for
  `9f339981a0e931c6236fa296907f4c2dd954b491`, with cross-provider review
  **SKIPPED** and the scoped waiver still pending. These historical reviews do
  not become cross-provider review or a review of this correction.

The original owner conversation selected preparation of the recommended
amendment/exception route and ended with an unanswered final approval request.
It provides no additional final decision. Current T3 history was inspected
through its read tools; the documented legacy SQLite snapshot was queried
read-only and contained no matching October thread rows. The approval conclusion
is limited to those inspected records, rather than inferred from stale prose
or closed state alone. No private transcript or credential is included here.

## One owner decision

**Recommended: approve the unchanged [five-clause trust contract](../adr/api-cli-surface.md#proposed-explicit-https-trust-amendment-966)
and its #966-only cross-provider review exception.** That accepts explicit
system-CA verification, legacy SPKI compatibility, deliberate first contact,
protected origin/mode provisioning and fail-closed refusal boundaries. The
exception covers only this amendment's unavailable cross-provider review;
the separate same-provider review requirements, technical checks, CodeQL,
signed/DCO history, ordinary PR protections and maintainer security review
remain. Publication and consumer evidence are separate gates.

The alternative is to keep governance **HOLD** until an authorized qualifying
cross-provider review is available. No substitute provider, paid review or
general ADR waiver is authorized. A same-provider review cannot unlock this
gate by itself. The explicit final decision is required by the amendment's
[activation section](../adr/api-cli-surface.md#proposed-review-disposition-and-activation)
and the [governance amendment procedure](../../GOVERNANCE.md#amendment-procedure).

After explicit approval, record its durable reference in the TLS section,
change its banner and activation status to owner-locked and operative on the
corrective PR's merge, and reconcile #25's closeout with the decision and merge.
Keep the actual cross-provider result **SKIPPED** even if the owner accepts
the scoped exception. Prepare a #966 closeout entry only for gates supported
by evidence below. This packet changes no remote ticket state and leaves merge
to the maintainer.

## Evidence boundaries and closeout

| Gate | Evidence and remaining work |
| --- | --- |
| Source | #967 merged as `69e32a25a6431426e839cb463aafd78c781ef15d`; #1004 merged as `f3581e7bb6ec65cb416d5213f7e55819ef19782f`. Both are ancestors of the inspected main. No production TLS behavior changes in this correction. |
| Trusted merge validation | [Run 39144169996](https://github.com/Hikyo-Org/Hikyo/actions/runs/39144169996) completed successfully on that exact merge commit. |
| Contract | **HOLD**, pending the exact owner decision above. |
| Review | Historical #1004 same-provider reviews as recorded above. No independent current-correction review is claimed. Cross-provider **SKIPPED**, configured Anthropic subscription unavailable; no alternate or paid route substituted. |
| Hosted publication | [Nightly release](https://github.com/Hikyo-Org/Hikyo/releases/tag/v0.0.1-nightly.20261028.98.g24e198b3), release ID `426425469`, published 2026-10-28 at 08:41:04 UTC. [Nightly run 40651678580](https://github.com/Hikyo-Org/Hikyo/actions/runs/40651678580) succeeded for source `24e198b32a3ffd8d4013207b2f974d76ec49be6f`. This is hosted publication evidence; local trusted verification is recorded separately below. |
| Earlier pending nightly | [Run 39213654522](https://github.com/Hikyo-Org/Hikyo/actions/runs/39213654522), described as pending in the October 19 handoff, subsequently completed successfully. No consumer acceptance is inferred from that result. |
| Named downstream CI update | **NOTRUN/HOLD**. No approved isolated downstream target, paired protected credential/bundle or existing runner configuration was supplied for this lane. Synthetic fixture proof does not show that DBugIT CI's version and protected bundle were updated. |
| Production, stable 1.0 publication and deployment | **NOTRUN**, outside this lane's authority. A verified nightly is not stable-release qualification or production upgrade approval. |

A verified signed nightly containing #967 satisfies the publication component
of criterion 5 without waiting for 1.0. The
[operations closeout procedure](../operations/cli-tls-trust.md#signed-release-and-consumer-update)
also requires the consumer's exact version and bundle update plus an isolated
authenticated read against its approved origin. For the named downstream CI
gate, the next operator needs an explicitly authorized isolated runner/target,
its non-production read credential delivered through protected storage, and the
corresponding bundle. Inspect that protected storage before requesting a missing
secret through the private secret tool. Do not reuse production credentials or
run a production upgrade. Retain the prior runner configuration for rollback.

For an authorized isolated consumer with a verified `hikyo` on `PATH`, the
existing Bash procedure is:

```bash
set -euo pipefail
set +x
: "${HIKYO_TRUST_BUNDLE:?Protected system-ca bundle required}"
: "${HIKYO_TOKEN:?Paired non-production read credential required}"
: "${HIKYO_ORG:?Approved organisation required}"
: "${HIKYO_PROJECT:?Approved project required}"
: "${HIKYO_STATE_DIR:?Fresh private absolute state directory required}"
hikyo --version
hikyo context create ci --instance ci --trust-file "$HIKYO_TRUST_BUNDLE"
hikyo env list --auth machine --instance ci \
  --org "$HIKYO_ORG" --project "$HIKYO_PROJECT" -o json
```

Expect context creation to perform no network request and `env list` to return
JSON with `items`. Record exact release/source/digests, runner identity,
protected-bundle identity without its credential, origin, successful read and
rollback location. Do not print the token or collect an environment dump.

The dated validation and review sections below are historical snapshots.
Current gate disposition above takes precedence over their former open-ticket
instructions.

## Fresh signed-artifact and synthetic consumer evidence, 2026-10-28

The current-source `scripts/release/nightly` verifier authenticated the complete
24-file public release download, including all 22 signed inventory payloads,
against independently pinned repository public trust. It returned:

```json
{"profile":"nightly/v1","version":"0.0.1-nightly.20261028.98.g24e198b3","sequence":98,"commit":"24e198b32a3ffd8d4013207b2f974d76ec49be6f","compatibility_sha256":"e2fc5b672565e44ac6498ca004e5e862e6c8784c5257f17671dfebb4425de36e","manifest_sha256":"1cfc26f353602feaad139d88a81bff54a36b8c3afaa16a93da1649bc3d50ed45"}
```

| Verified identity | SHA-256 |
| --- | --- |
| Linux/arm64 archive, release asset `684368480` | `b2f26718ac02877d38f4d0055ccd1692ab4527b048af24a7f5b42e392b71e9c8` |
| Extracted Linux/arm64 CLI, matching signed binary provenance | `1b75ce36808c9a650b0839461829234218ca02d5c5872c816ff4cc65c89cb3cb` |
| Independently authorized nightly policy, matching current main | `afaeb19fc0299f700234a74aa0817f77ce0833853b327a9d9d9d126d25d5b04f` |

The signed CLI was executed only after verification. A disposable Linux/arm64
container used an existing Node 26.12.1 image
`sha256:5c4012d11d9857c42713ba511fbc19645cac1706bb710d51d42b0d301dff7f88`,
with networking disabled, read-only root and fixture mounts, no inherited
operator credentials, private temporary CLI state and a synthetic bearer. Its
private protected bundle selected `system-ca` for a loopback HTTPS fixture.
The runner's CA file contained only the disposable fixture root; Linux Go's
normal root loading through `SSL_CERT_FILE` performed verification. Host trust
and global state were unchanged. A second valid certificate had a different
SPKI at the same origin.

The fixture returned synthetic API metadata and an empty environment list,
requiring the expected synthetic bearer on every request. It exercised the
released consumer's transport and command path. It did not run a Hikyo server's
authorization service, a managed proxy or a named downstream CI pipeline.

| Released consumer action | Exit | HTTP requests received |
| --- | --- | --- |
| Protected bundle and context import | `0` | `0` |
| Machine `env list`, first valid certificate | `0` | `2`, metadata and environments |
| Machine `env list`, renewed certificate with a different key | `0` | `2`, metadata and environments |
| Expired, wrong-host or untrusted certificate | `6` each | `0` each |
| Redirect refusal | `4` | `1` at original origin, `0` at redirect sink |
| Repository mode substitution | `4` | `0` |

No request had a missing or unexpected bearer, and no CLI output exposed it.
This is **PASS: local signed-consumer synthetic transport evidence**. The
named downstream version/bundle update and live-provider acceptance remain
**NOTRUN/HOLD**. Production upgrade remains **NOTRUN**. The owned container,
downloaded payloads and fixture private keys are disposable; retain only the
public verification/consumer receipts and fixture source for review.

To repeat full public artifact verification from this inspected source with
Go 1.27.3 and an empty operator-selected download directory:

```bash
set -euo pipefail
: "${HIKYO_NIGHTLY_REVIEW_DIR:?Empty download directory required}"
gh release download v0.0.1-nightly.20261028.98.g24e198b3 \
  --repo Hikyo-Org/Hikyo --dir "$HIKYO_NIGHTLY_REVIEW_DIR"
go run -p 2 ./scripts/release/nightly verify --trust release/trust \
  --directory "$HIKYO_NIGHTLY_REVIEW_DIR"
```

Expect the identity JSON above. Recheck current public trust/revocations before
a later use; preserve its verification state where the selected verifier
requires one. Do not substitute release-page checksums for trusted verification.
The full download is disposable after its receipts and required review evidence
are retained.

## Fresh local source checks, 2026-10-28

Shared-queue job `2568` passed on Go 1.27.3/macOS, with the pinned Node 26.12.1
for the documentation test. Production source is unchanged from inspection
`24e198b32a3ffd8d4013207b2f974d76ec49be6f`:

```sh
go test -p 2 -parallel 2 ./internal/cli ./cmd/hikyo -count=1
go test -p 2 -parallel 2 -race ./internal/cli -run 'TestNewHTTPSInteractive|TestRemoteAdd|TestTLSIdentityInspection|TestExplicitSPKI|TestExistingAndProvisionedSPKI|TestContextCreateExplicitSPKI|TestSystemCATrust|TestTrustModes|TestProvisionedTrustMode|TestPinnedClient' -count=1
node --test docs/site/scripts/test-prepare-content.mjs
```

The CLI and command suites passed in 26.184 and 1.549 seconds; focused race
coverage passed in 10.173 seconds. No shipped tests were removed. Persistence
is unchanged; no new both-engine qualification is claimed. The shared
fast-feedback driver selects no application/frontend domains for the complete
two-Markdown-file correction. Final documentation/link checks and hosted
current-head checks belong to the corrective PR; local checks do not establish
merge-queue validation or final integrated 1.0 acceptance.

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
