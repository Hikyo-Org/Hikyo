# Handoff: #158 AWS Secrets Manager synchronization adapter

Issue: https://github.com/Hikyo-Org/Hikyo/issues/158. Built on the #157
multi-target seam (targets, outbox, health, pause/resume, retain-or-prune).

## Current status, 2026-11-12

- **Implementation:** the adapter merged in #828 (2026-09-28) and its repairs
  in #969 (2026-10-30). Controlled tests cover acceptance criteria 1 to 5; the
  [coverage map](../testing/aws-acceptance-matrix.md) names the owner of each
  former real-AWS record.
- **Option A (accepted by the owner):** the live gate is criterion 6's four
  items only: value delivery, no-read API closure, replay safety and cleanup.
  It is one local, credential-gated `go test` of
  `TestAWSSecretsManagerRealSmoke` with a smoke manifest: one static writer
  (plus its authentic expired session for the credential fence), separate
  oracle and cleanup IAM users, and one customer KMS key. That manifest runs
  `preflight-static`, `lifecycle` and `promoted-replay` on SQLite and
  PostgreSQL, and `iam-denial`, which proves the writer's value read and force
  delete are refused by explicit identity-policy denies. It caps creates at 17
  (15 nominal) and skips or logs every other group with its owner from the
  map. The real lane accepts no other manifest shape. Individual failed tests may be rerun under a new prefix.
  Only the oracle identity may call `GetSecretValue`; the writer policy
  denies it explicitly.
- **Isolated harness:** the Floci CI lane `TestAWSSecretsManagerEmulatorAcceptance`
  (#1300) runs the same suite body on both engines; moto keeps
  `TestAWSSecretsManagerEmulatorLifecycle` until Floci matches AWS first-version
  staging.
- **Accepted gaps (option A, 2026-11-12):** real STS AssumeRole/WebIdentity and
  end-to-end KMS denial keep only controlled-test coverage. Ambient discovery
  moves to #1185.
- **Retired:** the hosted `aws-acceptance.yml` workflow, its runner, evidence
  schema, failure-text assertions, SHA-pinned approvals, historical cleanup,
  five-writer provisioning and OIDC roles. The release-admission package
  (#1057) remains; its pinned spec still names those paths, and retargeting or
  retiring it belongs to #1057.
- **Review disposition:** main records the owner-approved route-B disposition
  for declared amendment 8 in `docs/adr/mvp-boundary.md` (the "#158 review
  disposition" paragraph) and under Open items below. Both arrived with #969
  (commit `0ff27e3d6`).
- **Cloud authority:** agents perform no AWS operation. The owner provisions,
  runs and tears down the one live smoke. Nothing here authorizes a release,
  deployment or further spending, and 1.0 still excludes AWS per
  `docs/release/acceptance-1.0.md`.
- **Remaining step:** the owner's one live run, following
  [aws-sandbox.md](../testing/aws-sandbox.md):
  1. In CloudShell, run `smoke-sandbox.sh check`, then `provision`.
  2. Store the manifest and four descriptors locally in a private directory.
  3. After the 900-second expired session lapses, run the smoke command.
  4. Rerun any individual failure under a new prefix within the window.
  5. Run `teardown`, then `check` again, and record the result on #158.
- **Cloud leftovers:** seven earlier KMS keys had deletion windows ending by
  2026-11-03, so they should be gone; `check` flags any customer key that is
  not pending deletion, and any leftover `hikyo-e2e-*` secret. The historical IAM user
  `hikyo-158-setup` remains for the owner to decide.

## Authorization record

Current task authorization includes implementation of formerly post-1.0
issues. The ADR claims owner authorization on 2026-09-26, but the live #158
comment list contains no September 26 authorization comment;
that specific authorization reference is not independently corroborated there.
The August 24 deferral and unchanged v1.1 milestone remain visible alongside
the conditional promotion. Owner comment 6220607066 corrected the audit labels
to release:1.0 plus release:decision on October 16; this records conditional
relevance, not final promotion or acceptance. The parent/#79 owner retains
that conditional status in the release ledger. No review waiver is implied; the only cloud
operation now planned is the owner-run live smoke above. The ADR amendment is declared in `docs/adr/mvp-boundary.md` (declared amendment 8 and
the §4.1 row) with a banner on `docs/adr/deployment-adapter.md`; the
owner-approved review disposition below now governs both AWS portions.
Historical cross-provider SKIP remains; no additional formal pass is required.

Dated history before 2026-11-12, including the hosted-run results, merge-queue
repairs and integration notes, is in
[aws-acceptance-history.md](../testing/aws-acceptance-history.md#158-handoff-history-moved-2026-11-12).

## Shape

- **Provider** `aws-secrets-manager` (`internal/adapter/provider.go`), third in
  the closed set. `internal/adapter/adapter_test.go` pins the provider set and
  now also pins which destination kinds each provider accepts.
- **Destination kinds** `json-object` and `per-key` (`internal/adapter/aws_names.go`).
  Stored columns keep their generic meaning:

  | Column | AWS meaning |
  |---|---|
  | `destination_owner` | 12-digit account id |
  | `destination_name` | json-object: secret name. per-key: optional path prefix ending in `/` |
  | `destination_environment` | optional customer KMS key (applied on create) |
  | `destination_id` | the account number, from STS; ledger namespace |

  Because the KMS key, mode, name, and path live in the routing columns, any
  change is a destination move and gets scrub-before-switch for free.
- **Origin** is the regional endpoint `https://secretsmanager.<region>.amazonaws.com`
  (also `-fips` and `.amazonaws.com.cn`). Any other https origin (VPC endpoint,
  emulator) must carry `region` in the descriptor. Workload-identity modes
  always use AWS's regional STS (it receives the node's projected token or a
  replayable signed AssumeRole); only `static` may set `sts_origin`.
- **Errors**: origin, descriptor, and destination refusals are
  `awssm.ConfigError` (safe detail); the app factory joins `domain.ErrInvalid`
  (API 400 with the detail) and, for a disabled workload identity, also
  `adapter.ErrProviderAuth` so the worker fails the job instead of retrying.
- **Credential** is a sealed JSON access descriptor (`internal/adapter/awssm/auth.go`):
  `ambient`, `assume-role` (role ARN, external id, 900-3600 s), `web-identity`
  (role ARN; token file comes only from the server's `AWS_WEB_IDENTITY_TOKEN_FILE`),
  `static` (access key id + secret key; the only secret-bearing mode). Strict
  decoding; decode errors never echo input.
- **Node policy** `HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY=allow|deny` (default deny,
  node-scoped, managed-node key). Modes that borrow the server's AWS identity
  are refused without it, so a project admin cannot act as the server's role.

## Client and closure (`internal/adapter/awssm/client.go`)

Hand-rolled awsJson1.1 over a vetted `http.Client` (public-egress dialer with
the operator CIDR policy, TLS 1.2+, no proxy, no redirects, 1 MiB response cap,
deadline below the lease). SigV4 via `aws-sdk-go-v2/aws/signer/v4`; credential
sources via `config`, `credentials`, `credentials/stscreds`, `service/sts`
(promoted from indirect). `service/secretsmanager` is deliberately not a
dependency. Closure is pinned three ways in `client_test.go`: the exact `API`
method set, the exact signed-operation registry, and an AST scan of the
production package refusing any identifier or string literal naming
`GetSecretValue`, `BatchGetSecretValue`, `GetRandomPassword`,
`ForceDeleteWithoutRecovery`, or `ListSecretVersionIds`.

Error mapping: AccessDenied/KMS Encryption/DecryptionFailure/expired or invalid
tokens -> `ErrProviderAuth` (class `auth`); Throttling/TooManyRequests/
RequestLimitExceeded -> `ErrRateLimited` with a `RetryAt` from `Retry-After`
(default 5 s), class `provider_limit`. `LimitExceededException` is service-quota
exhaustion: a definite refusal with no throttle retry deadline. Other 4xx are
definite failures; 5xx and transport
errors ambiguous. Provider messages are dropped; only the closed error code is
kept.

## Module semantics (`internal/adapter/awssm/module.go`)

- Every attempt first proves the account (STS) and every DescribeSecret ARN
  proves account and region; mismatch is `ErrDestinationID`.
- Create: `CreateSecret` without a value but with `MANAGED_BY_HIKYO=<target>`
  and the KMS key, then `PutSecretValue(ClientRequestToken, VersionStages=[HIKYO_PENDING])`.
  `UpdateSecretVersionStage(AWSCURRENT, MoveToVersionId=<token>, RemoveFromVersionId=<observed current>)`
  conditionally promotes the staged value. Only after promotion does Hikyo move
  `HIKYO_CURRENT` and record `TagResource(HIKYO_VERSION=<token>)`. AWS automatically
  makes its first-ever value current even with explicit custom stages; a missing
  predecessor still refuses promotion if a concurrent writer became current.
- Concurrent drift between metadata inspection and either staging or promotion
  fails the conditional promotion, preserving the external value and previous
  Hikyo ownership markers. This requires the metadata-only
  `secretsmanager:UpdateSecretVersionStage` permission. There is no value read.
- Token = sha256(job id, target, generation, name): stable across retries of
  one outbox job (`SyncRequest.JobID`, set by the worker), new per job. A
  landed replay is detected (AWSCURRENT == token with HIKYO_CURRENT,
  HIKYO_VERSION, or this exact job's HIKYO_PENDING marker) and skips the write,
  repairing only missing ownership metadata. A staged but unpromoted version
  does not authorize overwriting a newer external current version. An ambiguous
  first adoption of an existing foreign value can require a fresh version-bound
  consent rather than assuming the current foreign version is still the one reviewed.
- Drift: only the labels of the version holding AWSCURRENT are consulted, plus
  the `HIKYO_VERSION` tag. moto drops custom labels from superseded versions
  while AWS keeps them; the rule holds under both (tested both ways). Consent to
  overwrite an external edit is `HIKYO_VERSION=<that version id>`, bound to that
  version only.
- Ownership: untagged + Reserved -> `Refuse` (conflict artifact, adoptable);
  untagged + Owned (adopted) -> tag, then write; tagged for another target ->
  conflict; pending deletion and ours -> `RestoreSecret`.
- Per-key outcomes are independent: conflicts, definite failures, and
  ambiguous outcomes are reported per name and the loop continues; auth, rate
  limit, destination move, and journal/gate errors stop it. Prune runs only
  after every desired name converged.
- Prune/teardown: `DeleteSecret` with `RecoveryWindowInDays=30`; already
  deleted or missing is success. A secret tagged for another target is never
  deleted.

## Store and migration

`00067_aws_secrets_manager_adapter.sql` widens the provider CHECK and the
destination-kind CHECKs on `adapter_targets`, `adapter_route_move_targets`,
`adapter_route_move_claims`, `adapter_ledger`. SQLite rebuilds five tables
under `legacy_alter_table=ON` with every current column (including the 00040
multi-target columns) and recreates `adapters_active_origin` and
`adapter_ledger_active_provider_name`. `internal/store/migrate/aws_adapter_migration_test.go`
seeds pre-00067 rows and proves they, the partial unique index, and the child
FKs survive on both engines.

`internal/buildcompat/development.json` was regenerated against the CI-pinned
`postgres:18@sha256:06cad38a...` (the diff is exactly the version-67 entry and
`schema_sha256` per engine). Regenerate it again after any change to 00067.

AWS-aware name collision (`refuseAWSNameCollision`, `reserveAWSMoveClaims`):
one account and region is one namespace across both kinds, so claims are
computed per target (`adapter.ClaimedNames`) and compared case-insensitively.
AWS kinds never reuse the CI sentinel pseudo-names, so two json-object targets
with the same (empty) prefix in one account are allowed.

## Surfaces

- API: `AdapterDestinationKind` += `json-object`, `per-key`; `AdapterProvider`
  x-extensible-enum += `aws-secrets-manager`; field descriptions updated. No
  new fields or routes. `apigen` and `clients/ts` regenerated.
- CLI: `--kind json-object|per-key`, `--secret`, `--kms-key`, and `--aws-auth`
  with `--aws-role-arn`, `--aws-external-id`, `--aws-session-seconds`,
  `--aws-region`, `--aws-sts-origin` on `adapter create`,
  `adapter update --origin`, and `adapter credential set` for ambient,
  AssumeRole, and WebIdentity. Static authentication uses the complete one-line
  JSON descriptor with `mode: static`, access-key ID, secret access key, optional
  session token and region through the no-echo prompt, `--stdin`, or
  `--value-file`, without any `--aws-*` flags. The removed identifier flag and
  `--aws-auth static` return a safe migration error before input or HTTP.
  Destination flags also apply to `adapter target add`. Help golden updated.
- Web: provider option, `AwsAccessFields` (descriptor in sensitive state),
  AWS kinds in `TargetForm`, AWS-aware target labels. Sensitivity inventory
  re-pinned with a review note.
- `adapter target show --format workflow` renders a names-only consumption
  snippet for AWS (`adapter.ConsumptionForTarget`).

## Tests

- `internal/adapter/awssm`: module behaviour against an in-memory AWS double,
  wire lifecycle against `awssmtest` (in-process signed Secrets Manager + STS
  emulator), error classification, descriptor contract, closure.
- `internal/isolation/adapter_aws_e2e_test.go` (both engines, always runs):
  real service/store/outbox/worker, sealed descriptor, snapshot decryption;
  both modes delivered byte-exact, cross-kind collision refused, value-blind
  plan, one new version per publish, external edit refused with drift
  attention, teardown with recovery window, no value-read on the wire,
  value-blind audit, INTENT/OUTCOME linkage.
- `internal/isolation/aws_external_e2e_test.go`: the existing emulator
  lifecycle, concurrency, replacement, and interrupted-adoption contracts.
- `internal/isolation/aws_real_durable_test.go` and `aws_real_fixture_test.go`:
  real AWS service/outbox tests with separate writer, synthetic oracle and
  cleanup identities. Both datastore engines are mandatory. A smoke manifest
  (static writer only, the only real-lane shape) caps creates at 17; the
  Floci lane keeps 32. A persistent local single-use marker refuses
  same-namespace reruns. See the
  [smoke runbook](../testing/aws-sandbox.md).
- `internal/isolation/aws_emulator_acceptance_test.go`: the Floci lane.

## Open items

- The owner explicitly approved route B for #158 on 2026-10-16: independent
  GPT-6.1 Sol high substantive source review plus Astra-high verification for
  BOTH original Anthropic AWS amendment `0171da3f1` and OpenAI #844 AWS
  concurrency/IAM/replay/custody/adoption changes introduced by `6b1e8552`.
  No further quota-gated formal run is required. Preserve historical formal
  SKIP and provider provenance; resolve findings and recheck the candidate.
  Substantive R1 found an initial-consent race and a fixture deadline bug;
  R2 found a replay regression in the consent repair. Source fixes and
  regression drafts are present; R3 independently verified all 38 pinned
  files and returned SOUND for the scoped source review. Astra independently
  rechecked the hashes and repair paths. No runtime acceptance is claimed.
  Package-review SOUND is not substantive or runtime acceptance.
  Real AWS acceptance, CI, signing/DCO and verified merge remain mandatory.
  (Under option A, 2026-11-12, that real AWS acceptance is the owner's one
  live smoke.) This #158-only disposition does not authorize resources, spending, release,
  Cloudflare, other ADRs or a global quota/provider-policy change.

- The owner's live smoke run under option A (see the current status above).
- The dev fake provider delegates AWS kinds to the production AWS module over
  the metadata-aware peer in `internal/app/adapter_fake_aws_dev.go`; other
  providers retain the generic fake. Authentication and the remote API remain
  simulated, with no SDK client, STS call or network transport. AWS browser
  drafts are registered through `machine-access.spec.ts`; this dev peer does
  not establish AWS-specific pinned-surface or real-provider runtime proof.
- `ListSecrets` cannot be resource-scoped in IAM; per-key planning and
  connection tests need it.
