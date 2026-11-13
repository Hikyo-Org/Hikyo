# AWS Secrets Manager acceptance coverage (#158)

This map assigns each of the 46 records of the retired real-AWS gate
(`TEST_NAMES` in `scripts/ci/aws-acceptance/runner.py`) to exactly one owner.
Coverage moves; none is dropped silently. The two rows marked **owner
decision** lose their only end-to-end proof once the live gate shrinks.

Destinations:

- **(a)** Floci CI lane: `TestAWSSecretsManagerEmulatorAcceptance` (same suite
  body as the real smoke), both engines, on GitHub-hosted runners via
  `scripts/ci/start-floci-emulator.sh`. Until Floci matches AWS's first-version
  staging (below), moto keeps running `TestAWSSecretsManagerEmulatorLifecycle`
  in required CI via `scripts/ci/start-aws-emulator.sh`.
- **(b)** Existing controlled or unit tests, named below.
- **(c)** Live real-AWS static-key smoke (`lifecycle` and `promoted-replay`).
- **(d)** Another issue.

Criteria are the #158 acceptance list: 1 configuration, 2 authentication,
3 lifecycle without `GetSecretValue`, 4 encoding and per-key outcomes,
5 loud failure modes, 6 end-to-end harness plus live smoke.

| Records (`TestAWSSecretsManagerRealSmoke/…`) | # | To | Covered by, and why | Crit. |
|---|---|---|---|---|
| suite root | 1 | (c) | Live smoke root. The lane runs the same body. | 6 |
| `lifecycle`, `/sqlite`, `/postgres` | 3 | (c) | Live smoke. Skipped on Floci (first-version gap below); the create, update and delete flow stays in required CI through moto's `TestAWSSecretsManagerEmulatorLifecycle`. | 3, 4, 6 |
| `promoted-replay`, `/finish-loss`, `/marker-refusal`, each `/sqlite`, `/postgres` | 7 | (c) | Live smoke. Also runs in (a), including the second-process replay child. | 3, 6 |
| `preflight-static` | 1 | (a) | Provisioned role session, identity bound through Floci STS. | 2, 6 |
| `iam-denial` | 1 | (a) | Floci enforces resource-ARN scoping (cross-writer `TagResource` deny) and refuses the writer's value read. No Allow grants that read, so the lane cannot tell the explicit `NeverReadValues` deny from an implicit one; (c) keeps that distinction. | 3, 6 |
| `adoption-witness`, its three scenarios, each `/sqlite`, `/postgres` | 10 | (a) | Static writer; metadata and version semantics match AWS. | 3, 5 |
| `auth-boundaries`, `/configured-identity-refusal` | 2 | (a) | Account-mismatch half on Floci. The region half needs an AWS regional origin: `TestRouteResolution`, `TestAccountAndRegionMovementIsRefused`. | 5 |
| `auth-boundaries/expired-static` | 1 | (b) | `TestFailureClassesFailLoud` (expired credentials), `TestClientRefreshesExpiredCredentials`, `TestAWSRealFixtureAuthExpiryClassification`. Floci and AWS STS refuse sessions under 900 s, so authentic expiry needs a 15-minute wait. | 2, 5 |
| `auth-boundaries/expired-{assume-role,web-identity,ambient}` | 3 | (b) | Same tests. Workload modes cannot reach an emulator (below). | 2, 5 |
| `role-auth`, `/assume-role`, `/web-identity`, each `/sqlite`, `/postgres`; `preflight-assume-role`, `preflight-web-identity` | 9 | (b) | `TestWorkloadIdentityRequiresInstanceOptIn`, `TestWorkloadIdentityAlwaysUsesAWSSTS`, `TestWorkloadIdentityRejectsNonAWSDestinations`, `TestTenantEndpointCannotReceiveWorkloadIdentity`, `TestDescriptorContract`. **Owner decision:** real STS AssumeRole, ExternalId trust and WebIdentity acquisition keep no end-to-end proof. | 2 |
| `role-auth/ambient`, `/sqlite`, `/postgres`; `preflight-ambient` | 4 | (d) #1185 | Real IMDS, ECS and IRSA discovery. Opt-in and routing stay in the (b) tests above. | 2 |
| `kms-denial`, `/sqlite`, `/postgres`; `preflight-kms-denied` | 4 | (b) | `TestFailureClassesFailLoud` (kms denial). Floci never authorizes the caller's KMS use (the lane asserts this). **Owner decision:** no end-to-end KMS denial remains. | 5 |
| **Total** | **46** | | | |

Outside the 46 records:

| Test or property | To | Notes |
|---|---|---|
| `TestAWSSecretsManagerEmulatorLifecycle` | moto | Unchanged, on digest-pinned moto in required CI. Moves to Floci when the lane's first-version canary fails. |
| `TestAWSSecretsManagerReplayProcess` | (a), (c) | Child process launched by `promoted-replay`. |
| `TestAWSReal*` fixture guards (scope, budget, oracle, replay, metadata, convergence) | (b) | Run without a provider in the CI isolation shards. |
| Policy conditions: `aws:CurrentTime` windows, `secretsmanager:KmsKeyArn`, `RecoveryWindowInDays`, `ForceDeleteWithoutRecovery`, `kms:ViaService`, `kms:CallerAccount`, `kms:EncryptionContext:SecretARN` | (c) | Floci never populates these keys. The lane strips exactly these from Allow statements, fails on any other unknown key, and asserts that the unstripped writer template still cannot `CreateSecret`. Conditional denies on these keys never match on Floci (for example the writer's `NeverForceDelete`), so they too stay with (c). |

## Floci fidelity checks

Floci 2.4.0 (`floci/floci@sha256:5ee85db6…`), IAM enforcement on, account
`123456789012`, in-memory storage, TLS through `QUARKUS_HTTP_SSL_*` on the native
image.

| Check | Result | Consequence |
|---|---|---|
| KMS denial from an identity policy on the key ARN | A role with an explicit `Deny` on `kms:GenerateDataKey`/`kms:Decrypt` for the CMK still creates and writes secrets with that CMK. | Vacuous on Floci: `kms-denial` to (b). The lane pins this gap. |
| Web identity | Floci refuses an unknown-issuer token. Moot: the product sends workload credentials only to AWS regional STS (`awssm/auth.go`), and `sts_origin` is static-only. | All workload modes leave the lane. |
| Authentic credential expiry | Floci STS rejects `DurationSeconds` below 900 for `AssumeRole` and `GetSessionToken`, through the Go SDK too. A 900 s `AssumeRole` session is refused after expiry with `ExpiredTokenException` by both STS and Secrets Manager, which the adapter classifies as auth (`awssm/client.go`). | Authentic, but proving it takes a 15-minute wait per run: `expired-*` to (b); no waiting or timeout changes. |
| Ambient | Not reachable on any emulator (as web identity). | (d) #1185. |
| Pending deletion and restore | 30-day deletion sets `DeletedDate`; writes, tags, staging and same-name creates return `InvalidRequestException`; `ListSecrets` hides it unless asked; `RestoreSecret` clears it; force delete removes it. | Matches AWS. |
| Value-less `CreateSecret` | 2.4.0 creates an empty `AWSCURRENT` version (id = `ClientRequestToken` or a GUID). AWS creates none. Fixed on Floci `main` (#8039, nightly 2026-11-11), not yet released. | The adapter sees an untagged `AWSCURRENT` and reports `exists, unowned`. |
| First `PutSecretValue` with another stage | On nightly 2026-11-11, where the empty version is gone, the first version gets only the requested stage; Floci `main` has the same logic. On 2.4.0 the empty version hides this. AWS also attaches `AWSCURRENT` to a secret's first version, and `internal/adapter/awssm/module.go` (create flow, "AWS makes the first value AWSCURRENT") depends on it. | `lifecycle` cannot pass on Floci: it stays with (c) and moto. The lane's canary stages a first value after a value-less create and fails once that version carries `AWSCURRENT`, which needs both this row and the one above to match AWS. That is the signal to run `lifecycle` and `TestAWSSecretsManagerEmulatorLifecycle` on Floci and remove moto. |
| Condition keys | Only `aws:RequestedRegion` among the keys the sandbox policies use. Resource ARNs, action lists and unconditional denies are enforced. | Stripped as above; conditions stay with (c). |
