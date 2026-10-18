# #966: explicit system-CA CLI trust

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/966>.
Baseline: `7bef4d670d2dce0defb963326f0e1e05cfdb6798`.
Delivery authorization: signed commit, branch push and pull-request creation.
The implementation is locally validated; hosted delivery and CI status belong
to the pull request. Merge, release and downstream changes remain outside the
current authorization.

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

## Local validation

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

## Review and remaining gates

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
[#25](https://github.com/Hikyo-Org/Hikyo/issues/25), currently closed. The owner must approve the proposed contract and
authorize a qualifying review path or explicitly waive that review gate before
the amendment can be locked. Claude subscription review is unavailable under
the current working rules; another provider or paid route was not substituted.

The initial PR head passed Go/frontend feedback and language analysis, but its
combined CodeQL security check failed on the alert above. CodeRabbit, Qodo and
MergeWatch did not produce reviews because of quota or billing limits; their
availability notices are not approval. No Windows/Linux runtime, live
managed-proxy rotation, artifact publication/signature verification or downstream
CI acceptance was performed.
The local unsigned development binary is not a signed CLI release. Keep #966
open through ADR approval, merge, verified signed publication and the documented
consumer update. Merge and release retain their separate authorization gates.
