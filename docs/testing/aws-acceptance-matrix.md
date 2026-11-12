# AWS Secrets Manager acceptance coverage (#158)

This map assigns each of the 46 records of the hosted real-AWS gate (the
`TEST_NAMES` list of `scripts/ci/aws-acceptance/runner.py` at commit
`a8f3cf477`) to exactly one owner. The change that adds this live smoke (#1338)
also deletes that workflow and runner; their history is in
[aws-acceptance-history.md](aws-acceptance-history.md). Coverage moves; none is
dropped silently. The rows marked **accepted (option A, 2026-11-12)** lose their
only end-to-end proof with the smaller live gate; the owner accepted that.

**Status:** the live smoke (c) has not run yet. Rows assigned to (c) state what
that run must prove; they become evidence only once the owner posts its result
on #158.

Destinations:

- **(a)** Floci CI lane: `TestAWSSecretsManagerEmulatorAcceptance` (same suite
  body as the real smoke), both engines, on GitHub-hosted runners via
  `scripts/ci/start-floci-emulator.sh`. Until Floci matches AWS's first-version
  staging (below), moto keeps running `TestAWSSecretsManagerEmulatorLifecycle`
  in required CI via `scripts/ci/start-aws-emulator.sh`.
- **(b)** Existing controlled or unit tests, named below.
- **(c)** Live real-AWS static-key smoke (`preflight-static`, `lifecycle`,
  `iam-denial` and `promoted-replay`): `TestAWSSecretsManagerRealSmoke` with a
  smoke manifest holding only the static writer, run locally per
  [aws-sandbox.md](aws-sandbox.md). `auth-boundaries` and `adoption-witness`
  skip with their owner from this map; `role-auth` and `kms-denial` no longer
  exist as groups and the run logs their owner.
- **(d)** Another issue.

Criterion 6's no-read API closure does not depend on any record below. Two
required unit tests in `internal/adapter/awssm/client_test.go` own it:
`TestNoValueReadPathExists` pins the adapter API to nine value-blind methods and
its wire registry to eight Secrets Manager operations, none a read, and
`TestProductionSourceNeverNamesAValueRead` fails if production source names
`GetSecretValue`, `BatchGetSecretValue` or another value-read operation. The
live `iam-denial` group adds AWS's own explicit refusal on top.

Criteria are the #158 acceptance list: 1 configuration, 2 authentication,
3 lifecycle without `GetSecretValue`, 4 encoding and per-key outcomes,
5 loud failure modes, 6 end-to-end harness plus live smoke.

| Records (`TestAWSSecretsManagerRealSmoke/…`) | # | To | Covered by, and why | Crit. |
|---|---|---|---|---|
| suite root | 1 | (c) | Live smoke root (pending its first run). The lane runs the same body. | 6 |
| `lifecycle`, `/sqlite`, `/postgres` | 3 | (c) | Assigned to the live smoke, pending its first run. Skipped on Floci (first-version gap below); the create, update and delete flow stays in required CI through moto's `TestAWSSecretsManagerEmulatorLifecycle`. | 3, 4, 6 |
| `promoted-replay`, `/finish-loss`, `/marker-refusal`, each `/sqlite`, `/postgres` | 7 | (c) | Assigned to the live smoke, pending its first run. Also runs in (a). | 3, 6 |
| `preflight-static` | 1 | (a) | Provisioned role session, identity bound through Floci STS. The live smoke also runs it first, with its IAM users. | 2, 6 |
| `iam-denial` | 1 | (c), (a) | Assigned to the live smoke (pending its first run) with the static writer: cross-writer `TagResource` deny, then the writer's `GetSecretValue` on its own secret. The writer policy has no Allow for that read, so the denial code alone would not show which policy refused it. The explicit/implicit distinction comes only from the deny kind the test classifies from AWS's access-denied message (`classifyAWSDeny`): AWS reports "explicit deny in an identity-based policy" whenever a Deny statement matches, here only `NeverReadValues`, and "no identity-based policy allows" when nothing does. The live run will pass only on the explicit identity-policy kind; an implicit, other-policy or missing deny kind fails it. A writer force delete must also be refused with the explicit identity-policy kind (`NeverForceDelete`), and the secret must stay live. Floci runs the same group but checks only the code of the value-read deny (it grants no read, so explicit and implicit look alike) and skips the force-delete probe (no condition evaluation, so it would really delete). | 3, 6 |
| `adoption-witness`, its three scenarios, each `/sqlite`, `/postgres` | 10 | (a) | Static writer; metadata and version semantics match AWS. | 3, 5 |
| `auth-boundaries`, `/configured-identity-refusal` | 2 | (a) | Account-mismatch half on Floci. The region half needs an AWS regional origin: `TestRouteResolution`, `TestAccountAndRegionMovementIsRefused`. | 5 |
| `auth-boundaries/expired-static` | 1 | (c), (b) | Assigned to the live smoke, pending its first run: the `lifecycle` credential fence installs `static-expired.json`, an authentic 900 s session that has lapsed (the owner waits 16 minutes), and the real AWS call must fail the durable job with no mutation. **Asserts `ErrorClassAuth` only**, not STS's `ExpiredTokenException` code, so any auth-class refusal satisfies it. Controlled: `TestFailureClassesFailLoud` (expired credentials) and `TestClientRefreshesExpiredCredentials`. | 2, 5 |
| `auth-boundaries/expired-{assume-role,web-identity,ambient}` | 3 | (b) | `TestFailureClassesFailLoud` (expired credentials) only injects a pre-classified error (`ErrProviderAuth` joined with an `ExpiredTokenException` response) into the fake API and checks the class; `TestClientRefreshesExpiredCredentials` checks credential refresh with a fake provider. Neither talks to STS. Workload modes cannot reach an emulator (below). **Accepted (option A, 2026-11-12):** authentic workload-mode STS expiry keeps no end-to-end proof. | 2, 5 |
| `role-auth`, `/assume-role`, `/web-identity`, each `/sqlite`, `/postgres`; `preflight-assume-role`, `preflight-web-identity` | 9 | (b) | `TestWorkloadIdentityRequiresInstanceOptIn`, `TestWorkloadIdentityAlwaysUsesAWSSTS`, `TestWorkloadIdentityRejectsNonAWSDestinations`, `TestTenantEndpointCannotReceiveWorkloadIdentity`, `TestDescriptorContract`. Acquisition: `TestSTSCredentialAcquisition` serves AWS's STS wire format from an in-process handler on the regional STS host and asserts that `AssumeRole` sends `RoleArn`, `ExternalId`, `DurationSeconds` and `RoleSessionName`, signed with the source credentials, and that `AssumeRoleWithWebIdentity` sends the token file's contents unsigned. `TestSTSCredentialAccessDeniedIsAuthClass` maps an STS `AccessDenied` to the auth class for both modes. **Accepted (option A, 2026-11-12):** AWS's own trust-policy evaluation (ExternalId match, OIDC issuer trust) keeps no end-to-end proof. | 2 |
| `role-auth/ambient`, `/sqlite`, `/postgres`; `preflight-ambient` | 4 | (d) #1185 | Real IMDS, ECS and IRSA discovery. Opt-in and routing stay in the (b) tests above. | 2 |
| `kms-denial`, `/sqlite`, `/postgres`; `preflight-kms-denied` | 4 | (b) | `TestFailureClassesFailLoud` (kms denial), which only injects a pre-classified error (`ErrProviderAuth` joined with an `EncryptionFailure` response) into the fake API and checks the class and outcome. Floci never authorizes the caller's KMS use (the lane asserts this). **Accepted (option A, 2026-11-12):** no end-to-end KMS denial remains. | 5 |
| **Total** | **46** | | | |

Outside the 46 records:

| Test or property | To | Notes |
|---|---|---|
| `TestAWSSecretsManagerEmulatorLifecycle` | moto | Unchanged, on digest-pinned moto in required CI. Moves to Floci when the lane's first-version canary fails. |
| `TestAWSSecretsManagerReplayProcess` | (c) | Child process launched by `lifecycle`, so only the live smoke runs it (Floci skips `lifecycle`). |
| `TestAWSReal*` fixture guards (scope, budget, oracle, replay, metadata, convergence, deny classification) | (b) | Run without a provider in the CI isolation shards. |
| Policy conditions: `aws:CurrentTime` windows, `secretsmanager:KmsKeyArn`, `RecoveryWindowInDays`, `ForceDeleteWithoutRecovery`, `kms:ViaService`, `kms:CallerAccount`, `kms:EncryptionContext:SecretARN` | (c) | Floci never populates these keys. The lane strips exactly these from Allow statements, fails on any other unknown key, and asserts that the unstripped writer template still cannot `CreateSecret`. The live smoke's creates, writes and KMS use must satisfy these conditions, and its `iam-denial` force-delete probe is to prove the conditional `NeverForceDelete` deny (pending its first run). Floci never matches conditional denies. |

## Floci fidelity checks

Floci 2.4.0 (`floci/floci@sha256:5ee85db6…`), IAM enforcement on, account
`123456789012`, in-memory storage, TLS through `QUARKUS_HTTP_SSL_*` on the native
image.

| Check | Result | Consequence |
|---|---|---|
| KMS denial from an identity policy on the key ARN | A role with an explicit `Deny` on `kms:GenerateDataKey`/`kms:Decrypt` for the CMK still creates and writes secrets with that CMK. | Vacuous on Floci: `kms-denial` to (b). The lane pins this gap. |
| Web identity | Floci refuses an unknown-issuer token. Moot: the product sends workload credentials only to AWS regional STS (`awssm/auth.go`), and `sts_origin` is static-only. | All workload modes leave the lane. |
| Authentic credential expiry | Floci STS rejects `DurationSeconds` below 900 for `AssumeRole` and `GetSessionToken`, through the Go SDK too. A 900 s `AssumeRole` session is refused after expiry with `ExpiredTokenException` by both STS and Secrets Manager, which the adapter classifies as auth (`awssm/client.go`). | Authentic, but proving it takes a 15-minute wait per run. The live smoke takes that wait once for `expired-static` (the `lifecycle` credential fence); workload `expired-*` stays with (b). No CI waits or timeout changes. |
| Ambient | Not reachable on any emulator (as web identity). | (d) #1185. |
| Pending deletion and restore | 30-day deletion sets `DeletedDate`; writes, tags, staging and same-name creates return `InvalidRequestException`; `ListSecrets` hides it unless asked; `RestoreSecret` clears it; force delete removes it. | Matches AWS. |
| Value-less `CreateSecret` | 2.4.0 creates an empty `AWSCURRENT` version (id = `ClientRequestToken` or a GUID). AWS creates none. Fixed on Floci `main` (#8039, nightly 2026-11-11), not yet released. | The adapter sees an untagged `AWSCURRENT` and reports `exists, unowned`. |
| First `PutSecretValue` with another stage | On nightly 2026-11-11, where the empty version is gone, the first version gets only the requested stage; Floci `main` has the same logic. On 2.4.0 the empty version hides this. AWS also attaches `AWSCURRENT` to a secret's first version, and `internal/adapter/awssm/module.go` (create flow, "AWS makes the first value AWSCURRENT") depends on it. | `lifecycle` cannot pass on Floci: it stays with (c) and moto. The lane's canary stages a first value after a value-less create and fails once that version carries `AWSCURRENT`, which needs both this row and the one above to match AWS. That is the signal to run `lifecycle` and `TestAWSSecretsManagerEmulatorLifecycle` on Floci and remove moto. |
| Condition keys | Only `aws:RequestedRegion` among the keys the sandbox policies use. Resource ARNs, action lists and unconditional denies are enforced. | Stripped as above; conditions stay with (c). |
