# Real configuration rollout fixture

`check-config-rollout-kind.py` runs the actual server and executor from one
candidate binary in a dedicated local, single-node Kubernetes 1.36 kind cluster.
It requires Docker, kind, kubectl, Helm, OpenSSL, Python 3 and the repository Go
version. Python uses only its standard library. No production credentials or
resources participate.

The harness validates the selected context against the local kind kubeconfig,
checks its loopback API endpoint and CA, derives the Docker node with matching
kind ownership, and checks the candidate Linux architecture. Every Kubernetes
call names the context. It creates a random owned namespace, local registry and
node-backed fixture volumes. It never creates or deletes the cluster.

## Build the signed candidate

Run from the repository root. Build the UI first with `bash scripts/ci/build-spa.sh`.
Create a fresh fixture path, because the signing test refuses to overwrite one:

```sh
fixture_parent=$(mktemp -d)
fixture="$fixture_parent/candidate"
HIKYO_CHART_FIXTURE_OUTPUT="$fixture" \
  HIKYO_CHART_FIXTURE_COMMIT="$(git rev-parse HEAD)" \
  GOMAXPROCS=2 go test -p 2 ./scripts/ci/chartfixture \
  -run '^TestWriteChartFixture$' -count=1
```

The test signs the reviewed source-owned schema declaration and exact embedded
migration manifest with ephemeral **test** keys. The generated binary embeds
that trust root and declaration; its normal production upgrade gate verifies the
matching bundle on every boot. This is production-mode admission using test
release authority, not a production release signature or development bypass.
The fixture exports only public trust material and linker input. It does not
export the ephemeral release signing private keys.

Use the Docker/kind node architecture (`arm64` on Apple Silicon, `amd64` on x86):

```sh
candidate_arch=arm64
GOMAXPROCS=2 GOOS=linux GOARCH="$candidate_arch" CGO_ENABLED=0 \
  go build -p 2 -tags ui -trimpath -ldflags "$(cat "$fixture/ldflags")" \
  -o "$fixture/hikyo" ./cmd/hikyo
```

## Execute

Choose an existing dedicated kind cluster, plus unused loopback ports:

```sh
python3 scripts/ci/check-config-rollout-kind.py \
  --context kind-config-rollout-test \
  --binary "$fixture/hikyo" --public-dir "$fixture/public" \
  --port 18188 --registry-port 56501
```

The full run proves:

1. Supported host setup creates the protected profile; a real password and TOTP
   ceremony establishes an instance administrator.
2. Publishing and applying an ordinary setting changes the actual update
   service channel while Pod UID, container ID, host PID and process start ticks
   stay identical.
3. Fresh exact TOTP authorizes database and root source aliases. The executor
   replaces the Pod, the replacement opens the same datastore with the new
   root, and an exact plan receipt records the actual application acknowledgement.
4. Holding the executor response after preparation keeps an actual replacement
   unresolved. Root verification succeeds, finalization refuses with conflict,
   and both active root wrappers survive. The hold continues through the real
   five-minute signed-command expiry. Delivery renews with a larger sequence,
   unchanged authorized command/target and unchanged MFA event count. Releasing
   the hold completes the same job.
5. A second held reply yields partial convergence. Distinct fresh TOTP authorizes
   Restore; the previous deployment returns while the desired revision remains
   fenced. A separately published and freshly authorized repair releases the
   fence without replacing that process. A later freshly authorized bootstrap
   plan replaces its Pod and completes normally.

The response hold is a namespaced-target admission refusal and proves real
transport-expiry renewal. It does **not** prove an executor process outage,
executor takeover, GitOps integration,
HA bootstrap, true database migration, or every managed setting. Database aliases
both refer to the same real PostgreSQL store; root aliases contain different
random keys. The harness never automatically finalizes the root key.

## Expanded singleton topology and upgrade custody proof

Use `--expanded` for the complete baseline chain plus singleton HA/NodeID and
all seven upgrade inputs. This requires `age`, `age-keygen`, maintained `cosign`
(validated with 3.1.3), a matching native operator CLI and a source manifest
captured before either build. The Linux server and native CLI must embed the
same signed linker input. Compile the native CLI with `CGO_ENABLED=0`, the same
`-tags ui -trimpath -ldflags` arguments and a separate output path.
Retain Go symbols in both binaries. Since `-trimpath` omits linker flags from
build metadata, the harness uses the Go standard-library ELF/Mach-O readers to
read each exact linked string symbol and compare all seven assignments with the
fixture's adjacent `ldflags` file. It records that input's hash and checks both
executable hashes again after the actual proof.

Capture the union of native and Linux compiled source/embed inputs before
building. The manifest is a JSON object mapping repository-relative paths to
lowercase SHA-256 digests. Include `GoFiles`, `CgoFiles`, `CFiles`, `CXXFiles`,
`HFiles`, `SFiles`, `SysoFiles` and `EmbedFiles` from
`go list -deps -json -tags ui ./cmd/hikyo` for both targets with
`CGO_ENABLED=0`, plus `go.mod`, `go.sum` and `Dockerfile.release`. Exclude files
outside this repository. Keep source and embedded UI assets frozen through the
build and run. The harness checks every supplied entry before and after the
proof, and verifies the actual running server ELF against the Linux candidate.

```sh
python3 scripts/ci/check-config-rollout-kind.py \
  --context kind-config-rollout-test \
  --binary "$fixture/hikyo" --public-dir "$fixture/public" \
  --expanded --operator-binary "$fixture/hikyo-native" \
  --source-manifest "$fixture/source-manifest.json" \
  --cosign cosign --database-port 15438 \
  --port 18188 --registry-port 56501
```

The expanded fixture additionally proves:

1. Fresh exact MFA applies then clears the enrolled next-root selector live.
   Clearing omits the optional selector from the complete node document; an
   empty string is not a valid managed node setting.
   The actual node acknowledges each revision while its process, current root
   mount and full active wrapper inventory stay exact. Selection alone does not
   perform or authorize root preparation/finalization.
2. Fresh exact MFA authorizes `ha=true` with a new enrolled node identity and
   later returns to the original singleton identity with `ha=false`. Both
   replace the Pod and receive the exact application acknowledgement.
3. While HA is enabled, source-only Restore keeps the desired target fenced.
   Fresh ordinary repair resumes the same process. The harness requires a new
   real HA heartbeat and a live scheduler lease before permitting the later
   bootstrap replacement, so a reboot cannot hide broken coordination recovery.
4. A separately generated operator public key is installed before first boot.
   Supported `backup upgrade-export` exports the actual populated source;
   supported `backup upgrade-drill` restores an independently empty PostgreSQL
   database, proves existing hierarchy/secret readability, reconciles the real
   administrator and mints/revokes a credential before cosign signs the result.
5. All seven explicit upgrade inputs switch to an enrolled alternate tuple.
   A held reply leads to Restore of the initial tuple, a separately authorized
   ordinary repair and a later successful seven-input Apply. The actual server
   validates the signed artifact proof and acknowledges the exact rollout.
   Image, volumes and mounts remain exact; both state paths in the replacement
   resolve to the same device/inode through preinstalled PVC subPath mounts.

Only a byte-identical snapshot of existing public operator custody is copied
into a private host directory for standalone drill pin verification. A pending
operator rotation refuses the drill, and both live and copied custody must
remain unchanged afterward. This snapshot is never selected by the server or
treated as a replacement state directory. Private age, root escrow and signing
keys remain outside server mounts. Public archive, receipt and attestation bytes
come directly from the supported CLI operations.
The identity custody file contains one encoded X25519 identity as the Hikyo CLI
expects; the original commented `age-keygen` output is retained privately, and
both forms must derive the same public recipient.

The scratch database connection retains end-to-end `verify-full` TLS. A bounded
loopback relay gives each TCP connection its own automatically allocated
`kubectl port-forward` tunnel to the exact owned PostgreSQL Pod. This isolates
local kind runtimes that terminate a whole tunnel when one PostgreSQL stream
closes. It neither retries nor replays traffic and does not terminate TLS.
The relay permits eight concurrent connections, 64 total connections and a
180-second connection lifetime, and cleans up every child after the drill.

The fixture temporarily applies a 60-second disclosure window through the
normal exact-MFA configuration flow, then restores the original zero window.
Its final negative check keeps a live disclosure window in one human session
and performs the policy change from a separately authenticated session. The
original session must retain its unexpired window yet receive a disclosure
refusal under the newly restored zero-window policy, without a process change.

This remains a singleton topology proof. It does not demonstrate multiple HA
replicas, node failure/takeover, a real database move, or a release/schema upgrade.
The alternate bundle contains the same authenticated candidate; the test proves
changing custody inputs and processing actual recovery evidence for that build.
Use `--topology-probe` or `--upgrade-probe` with an expanded retained fixture
only to investigate an interrupted run. A fresh full `--expanded` run is the
acceptance command. The upgrade probe requires fresh artifact destinations and
an empty scratch database; it deliberately refuses silently reusing a drill.
`--expanded-probes` can resume immediately after the complete baseline chain,
only when its exact generation, desired/latest revision and running process
still match the retained checkpoint and no added probe has completed. Expanded
resumes require an explicit matching chart-input record. Secret node documents
use the supported reveal endpoint and its real disclosure TOTP ceremony; a
redacted ordinary read is never treated as plaintext or replaced with a bypass.
The production default disclosure window is zero. Before the added probes the
fixture records that TOTP is unavailable for disclosure, then applies a
60-second window through separately authorized managed configuration and checks
the same process now offers TOTP. After the probes it separately authorizes
restoring zero, checks the same process, and confirms an ordinary secret read
is still redacted. Immediately before returning to zero it performs a successful
TOTP-authorized disclosure, then requires the same reveal endpoint to refuse
with HTTP 403 before that positive window's original expiry. The fixture refuses silently repeating this setup during
resume. Product defaults and protected-environment caps remain in force.

## Evidence and retained custody

The command prints a private `0700` work directory and the owned namespace.
`evidence.json` contains metadata only: resource/process identities, generation
states, source alias names, digests, root epoch numbers and refusal codes.
Review that file before copying it into a report. Other files contain live test
passwords, TOTP seeds, database/TLS keys, kubeconfig, or encrypted/private command
artifacts. Keep them private and do not commit them or expose the directory over
HTTP.

Resume a retained fixture with the same binary and bundle:

```sh
python3 scripts/ci/check-config-rollout-kind.py \
  --context kind-config-rollout-test \
  --binary "$fixture/hikyo" --public-dir "$fixture/public" \
  --resume /path/to/private/hikyo-rollout-kind-RANDOM \
  --namespace hikyo-rollout-01234567 \
  --port 18188 --registry-port 56501
```

A resume refuses a namespace without the fixture ownership label, a different
binary hash, or a different deployed image. `--root-guard-probe` and
`--restore-probe` run only that probe in a retained fixture. A full fresh run is
the acceptance command; resume is for inspecting or recovering an interrupted
fixture. The fixture removes temporary hold policies even on failure. Other
resources remain available for inspection and require explicit cleanup in the
same dedicated cluster once evidence has been collected.
