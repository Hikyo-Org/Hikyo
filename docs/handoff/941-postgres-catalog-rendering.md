# PostgreSQL catalog rendering independence (#941)

PostgreSQL renders stored expressions using session settings. The same
`workspace_handoffs.authenticated_at` default was rendered as midnight UTC or
01:00 Amsterdam time. Hashing these equivalent expressions rejected unchanged
schema with `candidate schema differs from signed target`.

## Implementation

The catalog owner now scopes PostgreSQL rendering to UTC, ISO dates, PostgreSQL
intervals, full float precision, hexadecimal bytea, stable identifier/string
quoting, and an explicit catalog/public/temporary search order. Independent
PostgreSQL probes demonstrated changes from each corresponding setting; the
regression fixture includes typed defaults that exercise all eight settings.
Temporary types cannot shadow public types during inspection.

One runner covers full catalog and control-only reads. It adapts reserved
`sql.Conn` handles through the configured pgx driver, handles `sql.Tx`, and
accepts the narrow transaction capability used by runtime and recovery wrappers.
It does not unwrap transaction wrappers or regain authority from closed handles.

For an idle connection, inspection owns a read-only repeatable-read transaction
and rolls it back. For an existing transaction, it creates a savepoint, captures
settings, renders the catalog, restores settings, and releases the savepoint.
Successful reads retain acquired catalog locks. Errors roll back to the
savepoint. Cleanup uses a bounded context independent of request cancellation;
unrecoverable cleanup aborts the transaction or closes the connection and never
returns a usable catalog.

Catalog SQL, object coverage, digest serialization, migration bytes, signed
compatibility declarations, and legacy genesis pins are unchanged. No operator
or pool-wide setting changes are required. SQLite follows its existing path.

## Acceptance evidence

The pre-fix catalog regression failed independently for each setting, and the
authorized signed production fixture reproduced the exact admission error.
The test fixture uses the shared production gate with a test signing root; it
does not substitute development trust for production trust.

The focused regression checks cover fresh migration, boot, restart in another
timezone, production/development trust separation, rejection of a changed
timestamp default and an added table, caller writes, catalog-lock retention,
all-setting restoration, two concurrently reserved pool connections and reuse,
closed transactions, SQL errors, cancelled readers, recoverable in-flight
cancellation, failed cleanup, and rows-close errors.

On PostgreSQL 18.6 with the owned test server configured for Europe/Amsterdam,
these existing checks passed with their SQLite siblings:

- `TestLegacyGenesisAndMigrationTamper`
- `TestRestoreControlSchemaIsExactEmptyAndTransactional`
- `TestSameArchiveRestoresNewIncarnationsBeforePublication`
- `TestFreshGatePersistsSchemaOnlyAndRefusesDifferentResume`
- `TestReleaseCompatibilityMatchesActualBothEngineSchemas`
- `TestOrdinaryBackupPublicAdmissionAndDataOnlyRestore`
- `TestUpgradeDrillActualBothEngineRecoveryAndConfigOnlyEscrow`

The release-compatibility check compares the generated declaration bytes with
the committed declaration. Backup checks exercise actual encrypted export,
restore, invalidation, recovery authority, and secret/config-only recovery.

Run the owner packages with `HIKYO_TEST_POSTGRES_DSN` pointing to an isolated
PostgreSQL 18 server whose role can create and drop test databases:

```sh
GOMAXPROCS=2 GOFLAGS='-p=1' go test -p 1 ./internal/store/upgrade ./internal/upgradegate -count=1
GOMAXPROCS=2 GOFLAGS='-p=1' go test -p 1 ./internal/app -run '^(TestReleaseCompatibilityMatchesActualBothEngineSchemas|TestOrdinaryBackupPublicAdmissionAndDataOnlyRestore|TestUpgradeDrillActualBothEngineRecoveryAndConfigOnlyEscrow)$' -count=1
```

The complete storage-upgrade package passed in 52.250 seconds and the complete
upgrade-gate package in 184.113 seconds, including CLI/process fixtures. Vet,
formatting, diff whitespace, and documentation-status checks passed. Independent
source review completed two rounds with no actionable findings in the final
patch. These reviews inspected source; the owner ran the recorded tests. The
final app identity and encrypted backup/restore checks passed in 16.785 seconds.

## Limits

No released production binary, release publication, or deployed service was
exercised. Production verification is proven through the shared gate with an
authorized signed fixture. Connection loss or cancellation of a transaction's
lifetime context can make that transaction unrecoverable; the reader fails
closed rather than promising transaction survival. Canonical inspection does
not redefine migration execution semantics under arbitrary operator settings.
