# Signed managed-configuration upgrade rehearsal

Runs official signed source and candidate images on fresh local PostgreSQL 18,
with a fresh root key and operator custody. It never reads production credentials,
backups, or databases. Requires Docker with a local Unix socket and Linux arm64
support, cosign, OpenSSL, Python 3.11+, and uv. The executable fixture uses pinned
`pyotp==2.9.0`; it also verifies its separately pinned official machine CLI image.

From the repository root, set `CANDIDATE` to the fixed release's signed multiarch
index digest, then run:

```sh
python3 scripts/ci/managed-config-upgrade/rehearse-upgrade.py \
  --source ghcr.io/hikyo-org/hikyo@sha256:89c04b72b8c4b75b0bfd8518bbcd7c3b22654cc6b504f854de4f7efb90c28bea \
  --candidate "$CANDIDATE" \
  --fixture "$PWD/scripts/ci/managed-config-upgrade/rehearsal-fixture.py" \
  --output /tmp/hikyo-779-signed-45-to-fixed \
  --timeout 300
```

The output directory must not already exist. The source above is nightly 45
(`0.0.1-nightly.20260915.45.ge349e902`). Use a candidate with a signed compatibility
edge from that release. Same-image restarts are rejected as upgrade evidence;
`--bootstrap-only` provides a separate source-fixture preflight.

The source starts with MCP enabled and without `HIKYO_MCP_WRITE_ENABLED`. This
exercises migration of the missing declaration/default while proving MCP itself
remains active. Both fixture stages require a nonempty MCP tool catalogue with
`hikyo_stage_change` and `hikyo_validate_change` absent. The fixture seeds a
published config and random secret, creates a machine credential, and verifies
byte-for-byte retained values plus the machine export, environment clone, edit,
publish, pin-list, and delete lifecycle before and after the upgrade.

Success requires `status: synthetic-upgrade-passed` in `report.json`, including
both signed-image checks, source readiness, fixture seed, source stop, candidate
readiness, and fixture verification. This is synthetic local Docker evidence,
not production or Kubernetes deployment proof. Diagnostics and credentials stay
in the private output directory. Do not publish that directory.

Resources are intentionally retained for inspection. Remove only this run's
recorded, ownership-labelled Docker resources with:

```sh
python3 scripts/ci/managed-config-upgrade/cleanup-rehearsal.py \
  --report /tmp/hikyo-779-signed-45-to-fixed/report.json
```

The cleanup retains private evidence files. Machine credentials expire after two
hours. Keep the source-to-candidate run within that period.

Run trust-boundary and evidence tests without Docker:

```sh
uv run --with-requirements scripts/ci/managed-config-upgrade/requirements.txt \
  python -m unittest discover \
  -s scripts/ci/managed-config-upgrade -p 'test_*.py'
```

## Provenance

Adapted from the repository owner's local synthetic Hikyo rehearsal at
`Projects/pi-cluster.git`, commit
`16b5a0116804084dca39f2dcd3d6f802cc6e7282`, under
`apps/hikyo-dbugit/scripts/`. This copy is self-contained: the PostgreSQL image
pin is embedded rather than read from a Kubernetes manifest. Issue #779 adds
MCP-enabled bootstrap, explicit disabled-write catalogue assertions, and their
regression tests. Upstream Hikyo: <https://github.com/Hikyo-Org/Hikyo/issues/779>.
