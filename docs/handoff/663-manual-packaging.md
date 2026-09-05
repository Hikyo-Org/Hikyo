# Mandatory upgrade deployment packaging

Implements the packaging portion of #663 in the recovered combined worktree.
No commit/push performed by this worker. Parent owns integration review and CI.

## Changes

- Chart requires preprovisioned `upgrade.existingClaim` for read-only regular
  public files and `upgrade.stateExistingClaim` for persistent operator custody.
  Fixed env/mount paths include the bundle, operator public key, state directory
  and optional public receipt/attestation/ciphertext. Exact final manifest and
  legacy-writer assertion are explicit values. Separate claims are enforced.
- Server deployment uses `Recreate`, retaining full-stop default. This is not
  evidence that external/pre-gate writers have stopped; the runbook requires
  explicit process stop and one verified migrator.
- `install/compose/server.yaml` supplies equivalent rootless manual bind mounts
  with automatic host-directory creation disabled and `--auto-migrate=false`.
- `docs/operations/manual-upgrades.md` gives exact bundle names, custody,
  export/drill staging, native/Compose/Helm commands, rotation and recovery.
  Published installation, upgrade and backup pages correct obsolete optional
  backup and plain-restore-reactivation claims.
- `scripts/ci/chartfixture` generates ephemeral signed stable-profile public
  artifacts plus buildcompat linker inputs from reviewed source schema claims.
  Embedded migration bytes are checked before signing; wrong recovery key is
  rejected. `k8s-e2e.sh` builds the matching UI candidate; `chart-kind.sh` stages
  regular files and owner-mode persistent state into separate PVCs. It does not
  use development admission. Fixture source changes select the required kind
  job through the changed-path classifier.

## Validation

- `sh scripts/ci/check-chart.sh`: PASS across ordinary, TLS, MCP, populated
  upgrade, HA and refusal modes.
- `sh scripts/ci/check-chart_test.sh`: PASS, including all 16 targeted chart
  mutations and four new public-bundle/state/strategy mutations.
- `go test -count=1 ./scripts/ci/chartfixture`: PASS 0.599s; fixture export repeat
  PASS 0.558s. A binary stamped using its generated public trust completed real
  SQLite `migrate`, exact-target boot and HTTP readiness/liveness. SQL inspection
  confirmed `trust_domain=production`, `maintenance=0`. Result retained in the
  companion HTML report directory. No private fixture keys were exported.
- Parsed `docker compose ... config --format json`: fixed public input paths,
  read-only public mount, persistent writable state, rootless/read-only runtime,
  no private drill custody mounts and manual migration all passed.
- Shell syntax and ShellCheck passed for changed shell runners. Docs
  `fnm exec --using 26.7.0 pnpm --dir docs/site run check`: 0 errors/warnings/hints.
  `git diff --check` passed.

## Remaining parent acceptance

Run the full real `k8s-e2e` required job. The local Docker VM exposes 2.35GiB total
memory and concurrent database suites were active, so this worker did not start
a kind cluster. The script now provides authentic gate inputs; no full-cluster
pass is claimed from a render or native smoke test.

Production release publication still requires the actual recovery-signed upgrade
catalog and assembled offline bundle. The existing catalog generator produces
an unsigned ceremony artifact; ordinary release payload downloads are not the
runtime bundle layout. Parent explicitly owns the final producer. No production
signer or trust policy was created by this change.
