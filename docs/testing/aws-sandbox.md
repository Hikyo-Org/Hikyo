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
refuse to run unless the caller is the account root user. Before it creates
anything, `provision` prints a random `RUN` (10 lowercase hex digits) and the
exact teardown command on stderr, and it prints them again if it fails or is
interrupted midway. Note the command before the run goes further. Pass your own
10-hex-digit value as the optional argument to choose `RUN`. The fixed length
keeps one run's `hikyo-e2e-RUN*` policy patterns from reaching another run.

`provision` creates, all in eu-central-1:

- one symmetric customer KMS key with the default key policy, alias
  `alias/hikyo-smoke-RUN` and tag `hikyo-smoke=RUN`;
- IAM users `hikyo-smoke-RUN-writer`, `-oracle` and `-cleanup`, each with
  customer-managed policies rendered from the templates below (IAM caps inline
  user policies at 2048 characters, which the writer policy exceeds);
- per user, one 3-hour `GetSessionToken` session, plus a second 900-second writer
  session. Long-term access keys stay in shell variables and are never printed.
  They stay active, with their policies attached, from `provision` until
  `teardown` deactivates them, so tear down as soon as the smoke ends.

It prints the nonsecret manifest, then four single-line descriptors
(`static.json`, `static-expired.json`, `oracle.json`, `cleanup.json`), then the
teardown command. The descriptors are secrets: do not save the output to a file,
do not paste it into chat, and clear the CloudShell screen after copying.

## 2. Local custody

Run on the machine that has the repository and Go toolchain. Do not paste a
descriptor into a terminal: macOS truncates a pasted terminal line at 1024
bytes, and a session token can come close to that. Copy one line in CloudShell,
then write it from the clipboard and check that it is complete JSON:

```sh
umask 077
DIR=$(mktemp -d "$HOME/hikyo-smoke.XXXXXX")   # created 0700
cd "$DIR"
pbpaste > manifest.json && jq -e . manifest.json >/dev/null && echo ok
# copy the next line in CloudShell before each command
pbpaste > static.json && jq -e . static.json >/dev/null && echo ok
pbpaste > static-expired.json && jq -e . static-expired.json >/dev/null && echo ok
pbpaste > oracle.json && jq -e . oracle.json >/dev/null && echo ok
pbpaste > cleanup.json && jq -e . cleanup.json >/dev/null && echo ok
pbcopy </dev/null          # clear the clipboard
ls -l                      # expect -rw------- on all five files
```

Each command must print `ok`; `jq` fails on a truncated or partial line, so copy
that line again. On Linux, use `wl-paste` or `xclip -selection clipboard -o`
instead of `pbpaste`.

The manifest has the layout of
[manifest.example.json](aws-sandbox/manifest.example.json): account, region,
`authorization_id`, `budget_usd`, `prefix`, `kms_key_arn`, `expires_at`, the
`static` writer (`credential_file`, `expired_credential_file`, `principal_arn`),
and the `oracle` and `cleanup` identities (`credential_file`, `principal_arn`).
File names are relative to the manifest's directory. Each descriptor is one JSON
line with `mode` `static`, `region`, `access_key_id`, `secret_access_key` and
`session_token`.

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
  jq -r 'select(.Action == "pass" or .Action == "skip" or .Action == "fail" or .Action == "build-fail") | "\(.Action) \(.Test // .ImportPath // .Package)"' |
  tee "$DIR/outcomes-0.txt"
```

The `jq` filter prints only outcomes with test or package names, and `tee` keeps
them in `outcomes-0.txt` (reruns use `outcomes-1.txt` and so on). That is the
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
2-hour window, narrowing with `-run` and keeping `preflight-static` so the
identities are checked first. A failed `lifecycle` engine:

```sh
  go test -json ./internal/isolation -run '^TestAWSSecretsManagerRealSmoke$/^(preflight-static|lifecycle)$/^postgres$' -count=1 -parallel=1 -timeout=30m
```

A failed `promoted-replay` case (here `finish-loss` on PostgreSQL):

```sh
  go test -json ./internal/isolation -run '^TestAWSSecretsManagerRealSmoke$/^(preflight-static|promoted-replay)$/^finish-loss$/^postgres$' -count=1 -parallel=1 -timeout=30m
```

`iam-denial` probes `static/sqlite/document`, which only `lifecycle` on SQLite
creates in the same prefix, so rerun it together with that engine (6 creates):

```sh
  go test -json ./internal/isolation -run '^TestAWSSecretsManagerRealSmoke$/^(preflight-static|lifecycle|iam-denial)$/^sqlite$' -count=1 -parallel=1 -timeout=30m
```

with the same environment and `jq` filter, pointing
`HIKYO_TEST_AWS_REAL_MANIFEST` at the new copy and teeing into the next
`"$DIR/outcomes-N.txt"`. A failed run's cleanup force-deletes the ARNs it
recorded; `check` lists anything left, and teardown removes it.

Before teardown, check that the first run and its reruns together passed every
required leaf and the suite root, which carries the cleanup and its
force-delete checks. This prints what never passed; empty output means every
part passed at least once:

```sh
{ echo TestAWSSecretsManagerRealSmoke
printf 'TestAWSSecretsManagerRealSmoke/%s\n' preflight-static iam-denial \
  lifecycle/sqlite lifecycle/postgres \
  promoted-replay/finish-loss/sqlite promoted-replay/finish-loss/postgres \
  promoted-replay/marker-refusal/sqlite promoted-replay/marker-refusal/postgres
} | sort > "$DIR/required.txt"
awk '$1 == "pass" { print $2 }' "$DIR"/outcomes-*.txt | sort -u |
  comm -23 "$DIR/required.txt" -
grep -h '^fail TestAWSSecretsManagerRealSmoke$' "$DIR"/outcomes-*.txt
```

A later rerun's root `pass` does not cancel an earlier root `fail`: the second
command prints every root failure, and each one needs its cause (usually a
cleanup the test reported) checked and recorded with the evidence. `check`
after teardown must still come back clean.

## 5. Teardown

Run in CloudShell as soon as the smoke ends or the window closes:

```sh
bash docs/testing/aws-sandbox/smoke-sandbox.sh teardown <RUN>
bash docs/testing/aws-sandbox/smoke-sandbox.sh check
```

Teardown revokes first: it deactivates the run users' access keys, detaches
every policy from them and deletes their inline policies. It then force-deletes
every secret whose name starts with `hikyo-e2e-RUN`, which is what the policies
grant and covers `hikyo-e2e-RUN/` and the rerun prefixes `hikyo-e2e-RUNrN/` (a
test timeout skips the test's own cleanup). A secret in a scheduled deletion is
restored first, because AWS refuses to delete it otherwise; the API cannot tell
that apart from a force deletion already under way, so a refused restore is
treated as one and the force delete is repeated, which AWS accepts.
A secret that still cannot be deleted is named, the rest of teardown runs, and
teardown exits non-zero: rerun it a few minutes later.
Then it deletes the access keys and users, deletes only the customer-managed
policies named `hikyo-smoke-RUN-*` (anything else it detached stays), deletes
the alias, and schedules the key tagged `hikyo-smoke=RUN` for deletion in 7
days. Any failed listing aborts teardown instead of reading as empty; rerun it
after fixing the cause. The final
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
the script against a local Floci emulator. `--endpoint-url` accepts only
`localhost`, `127.0.0.0/8`, `::1` and RFC1918 IPv4 addresses, and `--ca-bundle`
needs `--endpoint-url`. `TestAWSSandboxScript*` in `internal/isolation` checks
these refusals and the recovery output against a stub `aws`, with no AWS calls.

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
