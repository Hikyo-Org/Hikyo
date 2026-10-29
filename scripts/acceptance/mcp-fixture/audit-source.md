# Supported audit source seeding seam

Single source-fixture owner, shared with #651. This command provisions only the
separately named #1037 synthetic sibling. It does not deploy a source, select an
image, open ingress or delete any source copy. The original #651 PostgreSQL
guard still accepts only `hikyo_mcp_651`; this seam has independent narrow guards.

```sh
"$MCP651_FIXTURE" audit-seed --runtime "$AUDIT_SOURCE_RUNTIME"
```

The runtime is canonical, owner-only 0700. Stage `audit-settings.json` as 0600.
These are the supported fields, with placeholders requiring actual protected
operator input, not literal runnable credentials:

```json
{
  "engine": "postgres",
  "installation": "audit-pg-1037",
  "dsn": "PROTECTED_DEDICATED_AUDIT_POSTGRES_DSN",
  "sqlite_path": "",
  "root_key_file": "/run/audit-pg-1037/root-key",
  "state_directory": "/installation/operator-state",
  "development": false,
  "bundle_directory": "/public/bundle",
  "operator_public_key_file": "/public/operator.pub"
}
```

For PostgreSQL the role/database are both `hikyo_audit_1037`. Local development
requires loopback and a high port with `sslmode=disable`; production requires
`audit-db-1037.hikyo-mcp-651.svc.cluster.local:5432`, `sslmode=verify-full` and
the dedicated CA. DSN overrides and any MCP database are refused. A read-only
empty-organization check precedes admission/control/migration writes.

For SQLite set `engine=sqlite`, `installation=audit-sqlite-1037`, empty `dsn`
and an absolute `sqlite_path` ending in `audit-sqlite-1037.db`. Its immediate
parent must already be canonical owner-only 0700. The database file must not
exist. Root and authority directories belong to this separate installation.
Never reuse another installation's root, operator state, verifier floor or data.

Both engines use the same admission/keyring wiring. Development uses the
distinct supported development trust domain. Production uses genuine selected
build stamps, public bundle and installation-operator custody; no synthetic
production authority or verifier reset is provided. Missing final artifacts
remain HOLD. Actual artifact/platform/server binary and resource observation
must be bound later, before collector execution.

Supported service writers bootstrap the synthetic host custodian, create an
organization/project, create `audit-collector-1037` automation, grant only
project `audit-read`, mint a finite four-hour credential, and rename the project
to `audit-collector-1037`. A real failed `LocalLogin` generates the instance
operation. The seeder reads the rename through the same project bearer and
requires org/instance refusal. No SQL recreates grants. The collector's test
harness raw grants and test WebAuthn authenticator are not provisioning inputs.

## Private outputs and collection gate

All outputs are 0600 and never committed or printed:

| File | Recipient and purpose |
| --- | --- |
| `audit-project-bearer.curl` | Project poller only; contains the bearer header, HTTPS-only protocol and zero redirects; bearer never appears in argv or the seed receipt |
| `audit-bootstrap-authority.json` | Source custodian only; never transferred to the collector |
| `audit-seed-receipt.json` | Token-free source metadata for the collector owner to reconcile with live observations |

Receipt schema is `hikyo.dev/audit-source-seed/v1`. Closed fields are `schema`,
`status`, `engine`, `installation`, native `source_id`, `trust_mode`, `org_id`,
`project_id`, `principal_id`, `credential_id`, actual `credential_expires_at_utc`,
absolute protected-file `bearer_curl_config`, `project_receipt_path` (including
`receipt=v1`), `synthetic_tenant_event_id`, `synthetic_tenant_event_type`,
`failed_local_login_writer_return_at_utc`, and
`supervised_instance_event_observation`.

The following live-binding fields are deliberately null, so this seed receipt
is **not a collector-ready descriptor**:

- `actual_server_source_and_immutable_artifact`: later actual source SHA,
  immutable image/platform digests, measured server binary SHA-256 and trust mode.
- `verified_https_origin_and_trust`: later verified exact origin, optional
  local CA path, and verification receipt SHA-256. A proposed hostname is not proof.
- `actual_owned_resources_and_teardown_owner`: later observed namespace,
  workload, PVCs, dedicated database and teardown owner.

The collector owner combines these actual observations with the seed receipt
to bind the exact full receipt URL and collector config. Root keys, DSNs,
passwords, recovery custody and raw bearer values are excluded from that
descriptor. An instance/org export requires a separate supervised human and
current-MFA curl-config file for one approved window. This command creates no
unattended org/instance audit grant, session or MFA authority. The instance event
ID/export receipt remains HOLD until that human observation occurs.

Repeat seeding refuses existing custody/data. A partial failure preserves
authority for diagnosis; do not reinitialize it to retry. Destructive removal
of sibling synthetic copies requires its own explicit post-receipt permission.
Never delete shared MCP acceptance database/event rows or collector evidence.

## Local proof

An opt-in disposable test runs the supported seeder once on SQLite and once on
a bounded PostgreSQL18 container, then refuses a repeated seed, checks native
identity/trust, project event recovery, credential custody and null live bindings:

```sh
MCP651_AUDIT_REHEARSAL_DIR="$AUDIT_SEED_RECEIPTS" GOMAXPROCS=2 go test -p 2 -parallel 2 ./scripts/acceptance/mcp-fixture -run '^TestAuditSeedSupportedBothEngines$' -count=1
```

`AUDIT_SEED_RECEIPTS` must already be a canonical empty 0700 directory. Only
redacted per-engine summaries survive. Temporary roots, credentials, databases,
containers and network are removed. This is local supported-service seeding
proof, not remote source export, a published artifact or public acceptance.
