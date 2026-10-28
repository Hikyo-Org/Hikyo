# #1033: CLI DEK rotation and re-encryption

Continues [PR #1042](https://github.com/Hikyo-Org/Hikyo/pull/1042), preserving
its original signed commit `4ff6b48fb4405a6b6e2a1cc95e5083937706bc03`.
Main was rechecked at `24e198b32a3ffd8d4013207b2f974d76ec49be6f` and merged
without rewriting the original history. The merge also restores current pins,
including Node 26.12.1.
Maintainer review and merge authorization remain separate gates.

## Executable contract

`--instance NAME` selects an existing trusted instance reference.
`--instance-scope` selects its instance encryption scope. Without that boolean,
both org and project must resolve through the normal per-dimension chain.
An explicit unknown instance is refused, without falling back to another one.

```sh
hikyo rotate-dek --instance NAME --instance-scope --yes
hikyo reencrypt --instance NAME --instance-scope
hikyo rotate-dek --instance NAME --org ORG_ID --project PROJECT_ID --yes
hikyo reencrypt --instance NAME --org ORG_ID --project PROJECT_ID
```

Replace NAME with the trusted reference and ORG_ID/PROJECT_ID with identifiers
on that instance. Use the same target and scope for rotation and re-encryption.
The #1039 containment runbook can consume these commands without duplicating
the CLI implementation.

Explicit `--org`/`--project` conflict with `--instance-scope`. Both commands
refuse explicit `--env`: a project DEK operation covers all environments and
retained history. Inherited tenant defaults still resolve project scope;
explicit instance scope ignores them. Missing project dimensions, unknown
flags and extra positionals are usage errors before HTTP access. Explicit
scope conflicts are checked before context lookup, including a missing context.

The old boolean `--instance` collided with the common string selector and
never executed successfully. This pre-stable repair uses the existing
`--instance-scope` collision spelling and updates the canonical spelling spec,
CLI help and JSON goldens, public CLI reference, HA reference and UI examples.
It does not change the encryption ADR, request routes or bodies, crypto formats,
capability formulas or re-encryption algorithms, and grants no ADR waiver.
The owner also explicitly authorized restoring the two existing capabilities'
MFA gates to the accepted human-auth ADR.
For that repair, main was rechecked at
`1c7a0d0bdb5f8e4f9bd9c71f0a67219cd8f72736`; its capability set still omitted
both gates. This repair retains the existing branch and signed history.

## API owner and completion

| Command scope | Method and route | Body |
| --- | --- | --- |
| Rotate instance | `POST /api/v1/instance/rotate-dek` | `{"scope":"instance"}` |
| Rotate project | `POST /api/v1/instance/rotate-dek` | `{"org":"ORG_ID","project":"PROJECT_ID","scope":"project"}` |
| Re-encrypt instance | `POST /api/v1/instance/reencrypt` | Empty |
| Re-encrypt project | `POST /api/v1/orgs/ORG_ID/projects/PROJECT_ID/reencrypt` | Empty |

The existing service remains the only mutation owner. Rotation leaves the old
DEK retiring and readable. A failed walk is incomplete and must retain that
key; retrying the same scope resumes committed chunks. A successful walk
retires superseded keys only after the service's ciphertext-reference gate.
A subsequent successful invocation moves zero rows.

Both scopes require adequate MFA assurance in the current human session.
A password-only session is refused even if the same account owns a passkey.
Complete a passkey login or another accepted MFA ceremony before retrying.
The server enforces this at the existing authorization chokepoint, rather than
trusting CLI state or an account's enrolled-factor inventory.

## Regression evidence

| Test | Property |
| --- | --- |
| `TestDEKHandlersRejectUnknownFlags` | Actual handlers reject bad syntax without duplicate registration or state access |
| `TestDEKRotationDispatch` | Top-level dispatch, exact POST route/body, bearer, both scopes and JSON goldens |
| `TestDEKRotationScopeRefusals` | Invalid or conflicting scopes send no HTTP request, including metadata |
| `TestDEKRotationInstanceTarget` | Explicit selection wins over another trusted ambient instance; typos reach neither server; both scopes |
| `TestDEKRotationAuthenticationAndConfirmation` | Required `--yes`; authentication, capability and incomplete-walk errors propagate without success output |
| `TestDEKRotationCredentialRefusals` | Logged-out, machine-only and origin-mismatched credentials cannot mutate |
| `TestDEKCLICompletesRotation` | Real CLI/API/service on SQLite and PostgreSQL; live password-only refusal and passkey success at both scopes, invalid bearer and wrong-chain refusal, interrupted/resumed project walk, both key retirements, zero-row re-run and readable data/login afterward |
| `TestDEKOperationsRequireMFA` | Password-only refusal, passkey acceptance and the existing session-less local-authority exemption for operations using either capability, including token-key and scanning-key rotation |
| `TestMFAMandatorySetMatchesTheADR` | Independent capability inventory includes both restored gates |

Focused local CLI regressions pass. A source overlay of current main reproduces
both original `flag redefined: instance` panics. New negative controls also
reproduced project-wide mutation despite explicit `--env` before its repair.

Both-engine completion validation passes on SQLite and PostgreSQL, including
the interrupted/resumed project walk. Consult the PR description for the final
tested head, pinned complete-diff feedback, full CLI package suite and hosted
check state. Compilation, local tests, hosted admission and full merge-group
qualification are separate evidence.

## Review and remaining gates

The old frontend failure was the Instance Admin sensitivity hash. The complete
component diff changes command copy only; mutation bodies, consequence dialogs,
sensitive custody, session handling and result lifetime are unchanged. The
review note and source hash are refreshed together. Independent maintainer
security review is still required by #1033.

Real service validation reproduced a pre-existing MFA gap on this PR and
current main: `rotate-dek` and `reencrypt` were absent from the mandatory-MFA
capability set. The owner explicitly authorized restoring both gates in this
PR, as required by the accepted [human-auth ADR](../adr/human-auth.md).
The repair adds only these two capabilities to the canonical MFA set and its
independent fixture. OpenAPI and generated Go/TypeScript clients declare the
existing forbidden response for all affected routes. Operations sharing
`rotate-dek`, including token-key and
scanning-key rotation, receive the same required human-session assurance gate.
The existing session-less local-authority exemption remains intact.

The real CLI regression retains two valid sessions for the same passkey-owning
account. At both project and instance scope, the password-only session receives
the existing forbidden response (CLI exit 4), without new keys, key-state
changes, ciphertext rewrites, walk chunks or completion events. The independent
passkey session then completes rotation and re-encryption. Refused re-encryption
is tested while old-key ciphertext is still present, so a no-op cannot hide an
authorization bypass. Before the gate repair, the negative control rotated
both scopes successfully on both engines. No ADR amendment or waiver is needed.

Cross-provider review is SKIPPED because the authorized Anthropic subscription
path is unavailable; no paid or alternate provider was substituted. This is
not a CLEAN review or an ADR waiver. No merge, release, publication, deployment,
provider acceptance, production mutation or external notification is authorized.

UI evidence is captured locally at matching desktop/mobile viewports. Before
views reconstruct only the changed command text from the pinned main source;
routes, fixture state, layout and styles match the after views. Screenshots and
the offline HTML report stay outside Git. PR attachments or an approved artifact
store are required before embedding the screenshots in the PR description.

## Re-run

Prepare the repository-pinned toolchain and frozen client/web dependencies.
Use an isolated PostgreSQL fixture with `HIKYO_TEST_POSTGRES_DSN` configured;
the isolation harness creates its sibling `_isolation` database.

```sh
go test -p 2 ./internal/cli -count=1
go test -p 2 ./internal/authz ./internal/server ./internal/service -count=1
go test -p 2 -parallel 2 ./internal/isolation -run '^TestDEKCLICompletesRotation$' -count=1
go test -p 2 -parallel 2 ./internal/isolation -run '^TestMFAMandatorySetMatchesTheADR$' -count=1
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree
scripts/ci/check-dco.sh hikyo-upstream/main HEAD
scripts/ci/check-commit-signatures.sh hikyo-upstream/main HEAD
```

Run broad checks through the shared heavy queue per the contributor environment
policy. Keep signing and the fail-closed pre-push hook enabled. Fetch and compare
the existing PR branch before a normal fast-forward push; another writer's work
must not be overwritten. The T3 workspace branch stays bound to this checkout.
