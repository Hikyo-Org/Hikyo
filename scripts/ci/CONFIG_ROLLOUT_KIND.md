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
