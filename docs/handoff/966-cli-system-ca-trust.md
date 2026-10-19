# #966: explicit system-CA CLI trust

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/966>.
Baseline: `7bef4d670d2dce0defb963326f0e1e05cfdb6798`.
Implementation: [PR #967](https://github.com/Hikyo-Org/Hikyo/pull/967), merged on
2026-10-18 as `69e32a25a6431426e839cb463aafd78c781ef15d`.
Closeout baseline: `eb991c991bcad853590cb0ab2c09367052a92c02`.

## Amendment preparation, 2026-10-19

The owner selected preparation of the exact trust-contract amendment and the
scoped cross-provider-waiver route, with fresh adversarial OpenAI review. The
authorized endpoint for this preparation is a signed, DCO-compliant amendment
PR ready for final owner approval. This selection does not itself approve the
final contract text or make its proposed waiver operative. Amendment merge,
release and downstream acceptance remain separate gates.

| Gate | Evidence and remaining work |
| --- | --- |
| Implementation merge | #967 merged as `69e32a25a6431426e839cb463aafd78c781ef15d`, an ancestor of the closeout baseline. |
| Trusted merge validation | [Run 39144169996](https://github.com/Hikyo-Org/Hikyo/actions/runs/39144169996) completed successfully on that exact merge commit. |
| Contract and review disposition | The [exact amendment](../adr/api-cli-surface.md#proposed-explicit-https-trust-amendment-966) is prepared for owner approval. Its waiver is limited to #966's cross-provider requirement; same-provider review, technical checks and ordinary PR gates remain required. |
| Fresh OpenAI review | Required on the amendment's exact revision: separate Standards and Spec reviews of this amendment and the implemented trust boundary. Their revision and results belong to the amendment PR; the implementation review history below remains historical evidence. |
| Signed publication | At the closeout inspection, latest published nightly `v0.0.1-nightly.20261018.81.g35db63eb` predated #967. [Run 39213654522](https://github.com/Hikyo-Org/Hikyo/actions/runs/39213654522) targeted the closeout baseline but had not completed or published. Recheck its eventual result; no artifact verification is claimed. |
| Consumer acceptance | Pending: exact verified CLI version plus protected `system-ca` bundle, fresh isolated runner state and authenticated read-only `env list` against the approved origin. No consumer update or production rollout was performed. |

Reopen [#25](https://github.com/Hikyo-Org/Hikyo/issues/25) for the amendment
record. After final owner approval, mark this PR's amendment owner-locked and
operative on its merge. On merge, re-close #25 with that evidence. Keep #966 open through
verified signed publication and documented consumer acceptance. A signed
nightly may satisfy publication before 1.0; the contract must be dispositioned
before freezing a 1.0 candidate that includes #967. Use the
[operations closeout procedure](../operations/cli-tls-trust.md#signed-release-and-consumer-update)
for the release identity, verification and consumer evidence to record.

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
