# AWS adapter real-AWS smoke runbook

Issue #158 closes with one local, credential-gated real-AWS smoke,
`TestAWSSecretsManagerRealSmoke`. The owner provisions a throwaway sandbox from
AWS CloudShell, runs the test from a local checkout, and tears the sandbox down.
Hosted workflows, approvals, OIDC and role provisioning are not part of this
path. History lives in [aws-acceptance-history.md](aws-acceptance-history.md).

## What the smoke covers

Criterion 6 of #158: value delivery to AWS, a production API with no value-read
operation, replay safety and cleanup. `preflight-static` binds the three
identities, `lifecycle` and `promoted-replay` run on SQLite and on PostgreSQL,
and `iam-denial` proves the writer's value read and force delete are refused by
the explicit denies in its policy. Every other group skips or is logged, and
[aws-acceptance-matrix.md](aws-acceptance-matrix.md) names its owner. The
smoke uses one static writer (an IAM user with a temporary session), a separate
oracle identity that alone may call `GetSecretValue` on synthetic secrets, and a
separate cleanup identity. All values are synthetic, and the production adapter
never reads one.

## 1. Provision in CloudShell

Sign in to the sandbox account as the owner with MFA, switch the console to
eu-central-1, and open CloudShell.

```sh
git clone https://github.com/Hikyo-Org/Hikyo.git && cd Hikyo
git checkout <REVIEWED_COMMIT_SHA>
bash docs/testing/aws-sandbox/smoke-sandbox.sh check
bash docs/testing/aws-sandbox/smoke-sandbox.sh provision
```

`check` is read-only. Review its output first: any customer KMS key that is not
`PendingDeletion` is marked for review, and it lists leftover `hikyo-e2e-*`
secrets and `hikyo-smoke-*` users and policies. `provision` and `teardown`
refuse to run unless the caller is the account root user. `provision` prints a
random `RUN` inside its teardown command. Pass your own `[a-z0-9]{8,12}` value as
the optional argument to choose it.

`provision` creates, all in eu-central-1:

- one symmetric customer KMS key with the default key policy, alias
  `alias/hikyo-smoke-RUN` and tag `hikyo-smoke=RUN`;
- IAM users `hikyo-smoke-RUN-writer`, `-oracle` and `-cleanup`, each with
  customer-managed policies rendered from the templates below (IAM caps inline
  user policies at 2048 characters, which the writer policy exceeds);
- per user, one 3-hour `GetSessionToken` session, plus a second 900-second writer
  session. Long-term access keys stay in shell variables and are never printed.

It prints the nonsecret manifest, then four single-line descriptors
(`static.json`, `static-expired.json`, `oracle.json`, `cleanup.json`), then the
teardown command. The descriptors are secrets: do not save the output to a file,
do not paste it into chat, and clear the CloudShell screen after copying.

## 2. Local custody

Run on the machine that has the repository and Go toolchain. Paste each block
with `cat > file` so no secret enters shell history.

```sh
umask 077
DIR=$(mktemp -d "$HOME/hikyo-smoke.XXXXXX")   # created 0700
cd "$DIR"
cat > manifest.json        # paste the manifest line, press Enter, then Ctrl-D
cat > static.json          # paste the descriptor line, Enter, Ctrl-D
cat > static-expired.json
cat > oracle.json
cat > cleanup.json
ls -l                      # expect -rw------- on all five files
```

The fixture refuses symlinks, non-regular files, group or world permissions,
files over 32 KiB, and a manifest past its `expires_at`. The first run writes a
single-use `.hikyo-aws-run-*.used` marker into this directory. Keep the marker
and do not delete it to reset the allowance.

## 3. Wait, then run

The `lifecycle` group needs an authentic expired static session for its
credential fence. STS issues no session shorter than 900 seconds, so the smoke
can start only after `static-expired.json` has expired. **Wait at least 16
minutes after `provision`** before `go test`, and finish within the 2-hour
window (`expires_at` in the manifest).

Start PostgreSQL with `scripts/ci/start-ci-postgres.sh`. It pulls a pinned image
and starts container `hikyo-ci-postgres` on `127.0.0.1:5432` with user `hikyo`,
password `hikyo` and database `hikyo_test`; it fails if that container name or
port is already in use. From the repository root:

```sh
set -o pipefail
HIKYO_TEST_AWS_REAL_MANIFEST="$DIR/manifest.json" HIKYO_TEST_AWS_REAL_REQUIRED=1 HIKYO_TEST_AWS_REAL_MUTATIONS=approved \
HIKYO_TEST_POSTGRES_DSN='postgres://hikyo:hikyo@127.0.0.1:5432/hikyo_test' \
  go test -json ./internal/isolation -run '^TestAWSSecretsManagerRealSmoke$' -count=1 -parallel=1 -timeout=30m |
  jq -r 'select(.Action == "pass" or .Action == "skip" or .Action == "fail" or .Action == "build-fail") | "\(.Action) \(.Test // .ImportPath // .Package)"'
```

The `jq` filter prints only outcomes with test or package names, which is the
evidence to post on #158: `pass` for `preflight-static`, `iam-denial`, and
`lifecycle` and `promoted-replay` with their subtests on `sqlite` and
`postgres`, and `skip` for `auth-boundaries` and `adoption-witness`. Do not publish `-v` or
raw `-json` output without checking it for sensitive material. A missing
manifest skips locally but fails with the required switch, and a present manifest
without the mutation switch fails before AWS use. Stop the database container
afterwards (`docker rm -f hikyo-ci-postgres`).

## 4. Rerunning one failed test

The run marker blocks a second run in the same directory and prefix. To rerun
one failed test, make a fresh 0700 directory (`umask 077; mktemp -d`) and copy
the same `static.json`, `static-expired.json`, `oracle.json` and `cleanup.json`
into it. Write a new `manifest.json` that is the original with only `prefix`
changed to `hikyo-e2e-<RUN>r1/` (then `r2`, and so on). The policy templates
match `hikyo-e2e-<RUN>*`, so these prefixes need no new IAM. Rerun inside the
2-hour window, narrowing with `-run`:

```sh
  go test ./internal/isolation -run '^TestAWSSecretsManagerRealSmoke$/^lifecycle$/^postgres$' -count=1 -parallel=1 -timeout=30m
```

with the same environment, pointing `HIKYO_TEST_AWS_REAL_MANIFEST` at the new
copy. A failed run's cleanup force-deletes the ARNs it recorded; `check` lists
anything left, and teardown removes it.

## 5. Teardown

Run in CloudShell as soon as the smoke ends or the window closes:

```sh
bash docs/testing/aws-sandbox/smoke-sandbox.sh teardown <RUN>
bash docs/testing/aws-sandbox/smoke-sandbox.sh check
```

Teardown first force-deletes any secret left under `hikyo-e2e-RUN/` or a rerun
prefix `hikyo-e2e-RUNrN/` (a test timeout skips the test's own cleanup), restoring
a pending deletion first. It then deletes the users' access keys, policies and
users, deletes the alias, and schedules the key for deletion in 7 days. The final
`check` must show no `hikyo-e2e-*` secrets, no `hikyo-smoke-*` users or policies,
and the key in `PendingDeletion`. Then delete
the local directory (`rm -rf "$DIR"`).

## Policy templates and tokens

The JSON files in [aws-sandbox](aws-sandbox/) are rendered by `smoke-sandbox.sh`,
which refuses any policy that still contains `${` or is not valid JSON. Do not
attach an unrendered template by hand.

| Template | Attached to | Purpose |
|---|---|---|
| `writer.policy.json` | writer | Create, describe, put, stage, tag and 30-day scheduled deletion inside the writer namespace, KMS use through Secrets Manager, explicit denies on value reads and force delete. |
| `static-lifecycle.policy.json` | writer | Regional `ListSecrets` for per-key planning, and `RestoreSecret` in the writer namespace. |
| `oracle.policy.json` | oracle | `GetSecretValue` and KMS decrypt on synthetic secrets only. No write or cleanup. |
| `cleanup.policy.json` | cleanup | External seed and edit, metadata, and force delete confined to the run prefix. No value read. |
| `kms-denied.policy.json` | none | Explicit KMS deny. Only the Floci emulator lane uses it. |

| Token | Value |
|---|---|
| `ACCOUNT` | From `aws sts get-caller-identity`. |
| `REGION` | `eu-central-1`. |
| `KMS_KEY_ARN` | ARN of the key created by `provision`. |
| `RUN_SECRET_ARN_PATTERN` | `arn:aws:secretsmanager:REGION:ACCOUNT:secret:hikyo-e2e-RUN*`. |
| `WRITER_SECRET_ARN_PATTERN` | `arn:aws:secretsmanager:REGION:ACCOUNT:secret:hikyo-e2e-RUN*/static/*`. The `*` after `RUN` deliberately covers rerun prefixes such as `hikyo-e2e-RUNr1/`. |
| `AUTH_EXPIRES_AT` | Provision time plus 2 hours, UTC. |
| `CLEANUP_EXPIRES_AT` | Provision time plus 3 hours, UTC. |

`ListSecrets` requires `Resource: "*"`, so IAM cannot restrict it to the prefix;
the test wrapper refuses names outside the run. This is why the account must hold
synthetic sandbox data only. The three sessions last 3 hours so cleanup can still
run after the 2-hour mutation window. See the
[AWS authorization reference](https://docs.aws.amazon.com/service-authorization/latest/reference/list_secretsmanager.html).

`smoke-sandbox.sh` also takes `--endpoint-url` and `--ca-bundle`, only to dry-run
the script against a local Floci emulator. It refuses any `amazonaws.com`
endpoint.

## Child-process replay custody

`lifecycle` launches the already-built test executable, not a new Go build.
A bounded anonymous stdin pipe carries the original test root and database DSN;
argv and environment contain neither. The writer descriptor stays sealed in SQL
until the child uses the normal loader. Oracle and cleanup credentials are not
passed. The child repeats the signed development admission in Boot mode with
migrations disabled, opens the existing store, and rebuilds its keyring and
runtime. It accepts only the exact target and job, uses a distinct worker
identity, and refuses every `CreateSecret` reservation.

The first child exits with a dedicated code right after AWS accepts the write,
bypassing journal Finish and process defers. The second process runs after the
lease and crash window on the fixture clock and must finish the same durable job.
The parent asserts dispatched custody, unfinished intent, unknown crash closure,
successful replay and one new AWS version. Child stdout and stderr are discarded
so library diagnostics cannot expose DSNs or keys; only the bounded exit status
reaches the test log. Each child has a 90-second parent deadline.

## Local emulator validation

Without AWS credentials, `TestAWSSecretsManagerEmulatorAcceptance` runs the same
suite body against Floci through `scripts/ci/start-floci-emulator.sh`, and the
`TestAWSReal*` fixture guards run in the ordinary isolation shards. These are not
provider acceptance; the real smoke above is.
