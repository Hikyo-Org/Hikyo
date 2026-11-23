package badappendonly // want "appendonly\\(sqlite\\): TamperTenantAudit: statement on an audit table" "appendonly\\(postgres\\): TamperInstanceAudit: statement on an audit table" "appendonly: internal/store/migrations/downgrade.sql issues SET synchronous_commit"

// The anchor the appendonly analyzer runs on; the files it reads sit under this
// directory, mirroring the repository layout: audit-table statements in the
// query files, a synchronous_commit downgrade in a .sql file, and an
// ignore-tagged Go file that vet never analyzes (the driver's unanalyzed-file
// check finds it).
