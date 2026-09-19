# #779: release-defined managed configuration defaults

Issue: https://github.com/Hikyo-Org/Hikyo/issues/779

Nightly 45 has a 27-setting managed catalogue. Nightly 49 introduced
`HIKYO_MCP_WRITE_ENABLED`, but candidate startup required the new catalogue
before adding its declaration or value. Unattended backup preparation could
therefore refuse before reaching schema migration.

## Contract

Each new managed setting has an explicit, versioned, release-defined default.
There is no universal default for new settings. The first migration adds
`HIKYO_MCP_WRITE_ENABLED` with `false`. A missing default, or a default rejected
by the setting's declaration, requires an error naming the setting and operator
input. Existing values, including valid false, zero and empty values, take
precedence and remain byte-for-byte unchanged.

Backup preparation and maintenance configuration may preview declared additive
migrations without changing the retained source. Durable migration runs under
the exact candidate's upgrade fence before health validation. Catalogue drift,
incompatible declarations and invalid stored values remain errors. The normal
exact-catalogue check still runs after migration.

The database schema records the managed migration version separately from the
configuration schema version. Configuration changes publish an encrypted
snapshot, advance configuration revision and generation, and emit
`self_config.migrated` in the instance security audit trail. The event records
the migration version, owner instance, revision and generation without values.

An unfinished Apply, suspended recovery, or a published revision awaiting Apply
must be resolved on the source release before a migration that publishes a new
configuration revision. This prevents an upgrade from activating pending
operator changes or replacing them with an older desired configuration.

## Delivery validation

The change requires PostgreSQL and SQLite regression coverage, including retry,
owner isolation, encrypted snapshot integrity and refusal cases. Development
compatibility metadata must be regenerated from real catalogs after migration
SQL changes. The signed nightly workflow must pass on the exact green main
commit before the release is considered published.

Final acceptance uses the signed nightly 45 image against the published fixed
nightly in an isolated PostgreSQL 18 rehearsal. It must preserve representative
configuration and secret values, retain machine access, and expose no MCP write
tools while MCP itself is enabled. Enabling MCP writes remains a separate
operator configuration change.

The self-contained [rehearsal harness](../../scripts/ci/managed-config-upgrade/README.md)
documents the exact source image and verification command. Its fixture tests run
in the supply-chain CI job. Cross-provider review was explicitly skipped for
this delivery; ordinary Standards and Spec reviews still apply.
