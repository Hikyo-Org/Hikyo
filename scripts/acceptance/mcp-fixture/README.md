# #651 isolated acceptance fixture

Entry point for source assets. The [preparation handoff](../../../../docs/handoff/651-public-fixture-preparation.md)
records scope and evidence boundaries. The owner receives the actual private
host/origin authorization packet separately. **Preparation is not deployment
authority or final 1.0 acceptance.**

This directory owns one isolated fixture. Existing production installations,
stable signing/recovery custody, #158, PR #969 and controller #1057 are excluded.
The separate #1037 source-sibling proposal is not implemented by this seeder.
Never reuse an existing namespace or reset admission/operator custody after a
refusal. `TestMCPDeploymentFixture` and its dedicated loopback guard are unchanged.

## What is executable

| Entry | Purpose | Authority boundary |
| --- | --- | --- |
| `local.sh NEW_ABSOLUTE_RUNTIME` | Frozen clients, current-source builds, disposable PostgreSQL 18, two actual Hikyo processes, verified private-CA TLS and a round-robin proxy | Local disposable rehearsal only; generates development custody, never production release authority |
| `mcp-fixture render --runtime DIR` | Render namespace resources from the selected binding and public bundle | Offline structural preparation only; no Kubernetes calls or signature claim |
| `mcp-fixture authorize --runtime DIR` | Refuse missing/expired resource approval, changed resources, helper bytes or verification receipt | A recorded owner decision is required; a locally written JSON file does not create that decision |
| `deploy.sh DIR FIXTURE_EXECUTABLE reserve` | Reserve a new namespace and shared installation-authority claim only | NOTRUN; final selection and resource-specific approval required |
| `prepare-installation.sh EMPTY_PV_DIRECTORY PVC_UID` | Storage owner's manual preparation of the verified fresh claim directory | NOTRUN; manual storage operator only, after exact PVC/PV mapping and approval |
| `check-installation.sh DIR FIXTURE_EXECUTABLE OPERATOR_RECEIPT_REF` | Two temporary cross-node pods check exclusive locks, private permissions and remount preservation | NOTRUN; approved resources only, removes its own diagnostic pods |
| `deploy.sh DIR FIXTURE_EXECUTABLE continue` | Stage protected inputs, supported seed, two servers and gateway after bound storage proof | NOTRUN under preparation; requires current resource approval |
| `mcp-fixture public --runtime DIR` | Public transport/client, committed-authority and lifecycle checks | NOTRUN under preparation; requires approved public origin, authenticated artifacts, independent process binding and mutation window |
| `seed`, `revoke`, `revoke-smoke`, `grant-remove`, `grant-restore` | Supported service writers on the dedicated fixture | Never point at another database or installation |
| `hold`, `observe` | Bounded catalogue lock and no-secret store/claim observations | Only the approved synthetic fixture; lock expires after 20 seconds |
| `gateway` | Verified HTTPS proxy with alternating backends and closed wire-only receipts | Fixture namespace only; no buffering or affinity and no detached tool work |

All runtime directories must be canonical absolute paths owned by the executing
user with mode 0700. Protected JSON, authority and token records use 0600;
canonical staged root keys also permit 0400, without relaxing JSON custody.
No bearer goes into client configuration or OS argv. Inspector receives its
transient header through its own `runCli` function in process memory. Codex uses
`bearer_token_env_var`, an isolated home, a loopback-only model-provider stub,
and no model turn. Its exact fixture hostname bypasses the otherwise dead
proxy; this does not enable a provider recipient. No Claude or Responses
execution is required or authorized.

## Local rehearsal

From the repository root with the pinned Go toolchain, Docker, Node and npm:

```sh
scripts/acceptance/mcp-fixture/local.sh "$MCP651_RUNTIME"
```

`MCP651_RUNTIME` must name a new absolute directory. The script refuses an
existing one. It installs the frozen npm lock with lifecycle scripts disabled,
resolves the actual Node executable rather than a version-manager shim, and
builds sequentially with Go `-p 2` / `GOMAXPROCS=2`. It selects the pinned native
Codex executable for the current supported platform, never a global version.

The launcher creates one labelled Docker network and one memory/cpu bounded
PostgreSQL container, with tmpfs data and a randomly allocated loopback-only
port. It uses `hikyo_mcp_651`, not a shared datastore. Credentials are random
and task-local. Root, TLS keys and private files are removed on success. No
image is built or pushed. The existing pinned PostgreSQL image is shared cache,
not task-owned release evidence.

Supported writers bootstrap a fresh human custodian, two disjoint organizations,
projects and one production-named synthetic environment. They create three
definitions, publish two config values and one secret canary, create automation
identities and grant through `service.Grants.Create`. No SQL creates grants,
accounts, definitions or values. The read-only database refusal precedes seed;
an existing bootstrap or nonempty tenant database fails closed.

The campaign runs:

1. Tenant-free closed discovery and a nonempty three-page traversal alternating
   across independently observed OS processes, with no session IDs.
2. Absent/invalid uniform 401, wrong-scope/ungranted denial and an authorized
   missing-object twin, plus in-scope and disjoint canary exclusion.
3. A proven-live predecessor, supported committed revocation and its very first
   subsequent request on each process. Replacement calls still succeed.
4. Supported grant removal, first-call denial on both processes, supported
   restoration and recovery using the same bearer.
5. The existing public smoke checker using another predecessor, so its polling
   cannot consume the first-call probes. Its receipt remains `endpoint_smoke_only`.
6. Actual Go SDK v1.8.0, Inspector 2.11.0 Modern and native Codex 0.160.1 reads.
   Go/Inspector report `2026-07-28`; gateway observations capture Codex's
   requested/negotiated `2025-06-18` and authenticated call header separately.
7. A real admitted request held on the owned PostgreSQL `keys` table, proxy
   cancellation, disappearance of blocked work while the lock remains held,
   and shared claim release on each process.
8. SIGTERM with an admitted read, positively observed operational-listener
   closure while store work remains blocked, then lock release, successful exit,
   no claim and a surviving peer within the unchanged five-second drain budget.

`run/local-receipt.json`, `run/smoke-receipt.json` and
`run/wire-receipt.json` are redacted review artifacts. Wire receipts contain
only closed method/protocol names, status, session-presence and bearer-presence
booleans. They contain no token, request arguments, response bodies or tenant
identifiers. Direct TLS reads are recorded separately from proxy observations.
Bounded JSON is not open-stream proof.

Failure also stops owned processes and removes the exact container/network.
It retains private diagnostic scratch for diagnosis, not reuse. Inspect only
the failed owned run, retain a redacted failure receipt, then remove its private
scratch after repair. Never globally prune Docker or delete shared services.
The outer directory's pinned clients/builds are retained through review; remove
that exact directory after its review/qualification retention condition ends.

## Selected public artifact binding

Copy `binding.example.json` to a protected runtime `binding.json`. Its null
values are intentional HOLDs. The coordinator must separately select a clean
integrated source SHA/tree after required source/trust work. Fill:

- Published RepoDigest, selected linux/arm64 child manifest, image config and
  measured server binary SHA-256. A local image ID or tag is insufficient.
- Authenticated release manifest, bundle inventory, channel, installation
  operator public-key pin, verification receipt hash and owner selection reference.
- Reviewed linux/arm64 helper binary identity. Build it from the selected clean
  source with the **genuine selected release's existing public linker stamps**;
  measure the resulting helper separately. Do not generate test release keys,
  synthetic production declarations, replacement floors or new operator custody.
- The actual approved hostname, two observed nodes, cluster DNS IP and gateway
  trust CIDR. The renderer checks syntax, not domain ownership or capacity.

Follow [manual signed-bundle assembly](../../../../docs/operations/manual-upgrades.md)
and the [signed nightly procedure](../../../../docs/operations/signed-nightlies.md)
for the selected channel. Authenticate source/provenance, immutable image tree,
bundle and public operator pin independently. Store the no-secret verifier
output as `artifact-verification.json`. `authorize` checks its exact bytes;
its existence is not a substitute for the cryptographic verification.

Place only the public `bundle/` and `operator.pub` under runtime `public/`.
Bundle inventory is SHA-256 of compact JSON mapping each slash-separated path
relative to `public/` to its lowercase file SHA-256, with keys sorted
lexicographically and no trailing newline. The renderer computes and compares
this identity. Symlinks/nonregular files are refused. The reviewed ConfigMap
budget is 750 KiB; a larger real bundle is HOLD for a new storage packet.

```sh
"$MCP651_FIXTURE" render --runtime "$MCP651_RUNTIME"
```

Inspect `resources.yaml`, `reserve.json` and `storage-check.json`, and bind each
SHA-256 in the owner's concrete authorization record. The resources initially
keep server/gateway replicas at zero. The launcher scales them only after
supported seed completes. Projected public bundle files are copied into regular
files, with selected hashes checked before and after staging. Production
no-follow artifact readers remain unchanged.

## Protected inputs after approval

Missing user-supplied secrets must be requested directly through T3
`request_secret` with file delivery. Never ask for values in chat, invent a
secretRef, inspect unrelated credentials or access stable recovery/signing
material. Fixture-generated credentials stay within its protected custody.

The approved operator stages these 0600 files under runtime `custody/`:

| Files | Required content |
| --- | --- |
| `root-key` | Fresh fixture-only root, supported root-key format |
| `password`, `dsn` | Fresh dedicated PostgreSQL credential; DSN selects user `hikyo`, database `hikyo_mcp_651`, host `db.hikyo-mcp-651.svc.cluster.local:5432`, only `sslmode=verify-full` |
| `postgres.crt`, `postgres.key`, `postgres-ca.crt` | Fixture-only server certificate with the exact database DNS SAN; verified chain and validity |
| `backend.crt`, `backend.key`, `backend-ca.crt` | Fixture-only backend TLS certificate with the selected external hostname SAN; verified by gateway |
| `public.crt`, `public.key` | Dedicated hostname certificate from the owner-approved public issuer; normal system trust, no TLS bypass |
| `settings.json` | Protected fixture custody descriptor shown below |

```json
{
  "dsn": "PRIVATE_CUSTODY_VALUE_FROM_PROTECTED_FILE",
  "root_key_file": "/run/hikyo-root-key/root-key",
  "state_directory": "/installation/operator-state",
  "development": false,
  "bundle_directory": "/public/bundle",
  "operator_public_key_file": "/public/operator.pub"
}
```

The root and TLS paths are intentional container runtime paths. The descriptor
is credential custody, never a distributable client configuration. Protect the
outer directory and files before transfer. `helper-linux-arm64` must match the
selected helper hash. Its production gate authenticates the real bundle and
build stamps before any supported writer is used.

Copy `authorization.example.json` only **after** the actual owner authorizes
the exact packet. Record their decision/reference, expiration, approved origin,
rendered hash and precise allowed mutations. Template booleans default false.
Execution expires within 24 hours; the recommended packet uses four hours.
No resource approval substitutes for final source/artifact selection.

```sh
scripts/acceptance/mcp-fixture/deploy.sh "$MCP651_RUNTIME" "$MCP651_FIXTURE" reserve
```

The reservation creates exactly five resources: namespace, quota, service
account, default-deny policy and the 1 GiB `installation-authority` RWX claim
using the existing `nfs` provisioner. No datastore or server starts yet. Tooling
and both replicas must share this installation's verifier floor/operator state;
three independent state directories would fail genuine admission to one database.
The database uses a separate 2 GiB `local-path` claim, for two claims/3 GiB total.
Claim requests are accounting bounds, not physical filesystem quotas.

After verifying the new PVC UID, PV name and exact empty provisioner directory,
the storage owner manually runs `prepare-installation.sh` on its Linux storage
host. No agent executes NAS commands. The script refuses noncanonical, nonempty
or existing-installation directories. It prepares a 2770 root, group 65532 and
an owner-65532 0700 `operator-state` child. `OnRootMismatch` preserves that child
on remount. No dataset/export, shared tree or existing custody is changed. The
storage owner must validate installed GNU command flags before this manual step.

```sh
scripts/acceptance/mcp-fixture/check-installation.sh "$MCP651_RUNTIME" "$MCP651_FIXTURE" "$MCP651_STORAGE_OPERATOR_RECEIPT"
```

This creates only `custody-check-0/1`, pinned to the two selected nodes. Actual
exclusive contention, success after lock release, private-file ownership and
preservation after both pods remount must pass. The pods are deleted before
the resulting protected `storage-verification.json` is written. Failed checks
are HOLD; diagnostic marker/lock files may require the approved owner's cleanup.
Review this receipt and bind its SHA-256 as `shared_storage_verification_sha256`
in the existing approval record. Its fields are `pvc_uid`, `pv_name`, `root_mode`
(`2770`), `root_group` (65532), `private_child_mode` (`0700`),
`private_child_owner` (65532), `remount_preserves_private_child`,
`cross_node_exclusive_locks_passed`, and `operator_receipt_ref`.
`authorize-resume` checks the original namespace UID, claim identity and proof.

```sh
scripts/acceptance/mcp-fixture/deploy.sh "$MCP651_RUNTIME" "$MCP651_FIXTURE" continue
```

The initial launcher refuses an existing namespace instead of adopting it. A partial
failure is HOLD: preserve the owned namespace, root and verifier/operator state,
inspect the exact failed step, and recover within the same authorized fixture.
Do not rerun the fresh installer or erase admission custody to make it pass.
Ordinary namespace cleanup belongs to the approved teardown window.

## Public ingress and campaign

The owner applies only the explicit new-host edge route from the private packet.
The namespace's Traefik TCP route passes TLS through to the fixture gateway.
The public edge must verify its HTTPS upstream hostname/chain, preserve Host,
disable buffering and retries for MCP POSTs, propagate disconnects and allow
the protocol/bearer headers without logging them. No new port-forward, tunnel,
public registry push or production alias change is implicit. The operational
listener, database and toolbox have no public service.

Before sending any bearer, retain the actual edge route/certificate readback,
anonymous metadata, public TLS/no-redirect evidence, deployed revision and
effective enforcing-CNI positive/negative matrix. A wildcard DNS answer or an
empty HTTP 200 is not a fixture. Verify the gateway's backend socket identities
against independently observed pod UID/IP/start/image data. The campaign refuses
an image index where the selected platform digest is required. If the Kubernetes
API reports only the index, obtain authorized runtime measurement; do not waive
the check or infer the selected child from architecture alone.

Write protected `target.json` with the exact HTTPS `/mcp` URL, empty `ca_file`
for normal public system trust and two process labels. Public execution replaces
the labels with the independently observed pod socket identities.

Also stage a protected `ingress-verification.json` and put its exact SHA-256 in
`authorization.json` as `ingress_verification_sha256`. Its closed fields are:
`origin`, `source_commit`, `image`, ordered `pod_uids`,
`edge_configuration_sha256`, `public_TLS_and_upstream_TLS_verified`,
`no_redirects`, `effective_CNI_positive_negative_controls_passed`, and
`operator_receipt_ref`. The three boolean observations must be true and refer
to real approved observations, not a fixture. Before the first bearer, the
campaign independently requires both anonymous metadata responses to match
the bound `server_version`, a positive API revision and the observed backend
socket identities. Empty or wrong-version HTTP 200 responses fail closed.

```json
{"url":"https://fixture.example/mcp","ca_file":"","processes":["observed-a","observed-b"]}
```

Load the same frozen client locations as `local.sh`, including actual Node and
the native Codex executable, then run:

```sh
"$MCP651_FIXTURE" public --runtime "$MCP651_RUNTIME"
```

The command touches only the approved fixture. It uses the supported writer
for revocation/grants, holds only its catalogue table for bounded lifecycle
checks, and gracefully deletes one UID-preconditioned server pod to obtain
SIGTERM. It forbids forced/zero-grace deletion. It requires old-pod removal,
shared-claim release and a surviving peer within the existing five-second
application budget. The toolbox alone may connect to the two observed operational
TCP sockets to prove shutdown has begun before the held query is released.
Network timeouts do not establish listener closure. Record underlying
kubelet/container termination evidence as well; API removal is an upper-bound
observation and leaves the runtime-termination-record gate HOLD. Recheck recovery
on the replacement pod under the same artifact binding.

The public receipt is a campaign result, not release PASS. Attach the direct TLS
comparison, independent public vantage, ingress/CNI observations, exact artifact
receipts, named-client support table and final cleanup. If a failure interrupts
grant removal, run the supported `grant-restore` helper and verify both servers;
do not fix grants with SQL. Revoked predecessors cannot be reused. Mint a fresh
dedicated predecessor through the supported identity writer and update protected
custody before a new campaign; retain the failed run separately.

An interrupted public campaign writes a redacted failure receipt and attempts
supported grant restoration within the still-current approved window, including
when a removal command might have committed before its acknowledgement was lost.
Recovery is checked on both processes, and a failed command remains a failure.
It reports
a failed/expired recovery as HOLD for the named cleanup owner. It never writes
a passing receipt for a partial campaign or silently resets credential custody.

## Cleanup and adjacent ownership

Local cleanup is automatic and exact. For remote cleanup, first close only the
new-host edge route, then remove only this labelled namespace and its two
dedicated PVCs/PVs through the approved storage owner. Verify the exact PV paths
are reclaimed, task processes/pods/services/secrets are absent, and no public
fixture route remains. Retain only redacted receipts/client pins for the approved
window. Never delete production namespaces, the shared MCP event rows or #1037
collector storage. If audit siblings are later separately authorized, their
custody-copy removal needs its own explicit permission and terminal receipts.

The owner-approved 1.0 mandatory matrix is Go v1.8.0, Inspector 2.11.0 and Codex
0.160.1 only. Claude Code and Responses remain not verified for 1.0. Native Codex
proof does not establish T3 desktop acceptance. #631's managed-credential,
no-OAuth ruling and its lifecycle limitations remain unchanged.
