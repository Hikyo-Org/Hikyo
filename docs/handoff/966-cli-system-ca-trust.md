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

Omitted modes preserve legacy SPKI verification; explicit `spki` is equivalent.
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
go test ./internal/cli -count=1
go test -race ./internal/cli -run 'TestTrustModes|TestProvisionedTrustMode|TestEstablishmentTLSModeFlag|TestSystemCATrust|TestPinnedClient|TestTrustStore|TestFetchIdentity' -count=1
go test ./cmd/hikyo
go vet ./internal/cli
go build ./cmd/hikyo
scripts/ci/check-go-imports.sh
git diff --check
```

The final full CLI suite completed in 9.618 seconds and focused race checks in
6.171 seconds. The command-entry suite passed in 1.315 seconds. The API freeze
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

The parent reviewed the implementation and a separate same-provider reviewer
checked it against live #966 and the repository rules. No production
correctness/security finding remained. The reviewer identified a masked-test
weakness; malformed bundles now run against fresh stores as well as existing
stores, and the reviewer verified that correction. This is source review, not
cross-provider approval.

The [governance amendment procedure](../../GOVERNANCE.md#amendment-procedure)
requires reopening the originating ticket, adversarial cross-model review and
recording the amendment. The originating API/CLI ticket is
[#25](https://github.com/Hikyo-Org/Hikyo/issues/25), currently closed. The owner must approve the proposed contract and
authorize a qualifying review path or explicitly waive that review gate before
the amendment can be locked. Claude subscription review is unavailable under
the current working rules; another provider or paid route was not substituted.

No remote CI, Windows/Linux runtime, live managed-proxy rotation, artifact
publication/signature verification or downstream CI acceptance was performed.
The local unsigned development binary is not a signed CLI release. Keep #966
open through ADR approval, merge, verified signed publication and the documented
consumer update. Merge and release retain their separate authorization gates.
