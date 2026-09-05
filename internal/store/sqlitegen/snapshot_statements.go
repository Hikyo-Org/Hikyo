package sqlitegen

// SnapshotInsertSlot identifies the two repeated publish inserts by their exact
// generated SQL. This keeps transaction-local statement reuse tied to SQLC's
// source, without matching comments, copying queries or broadening other SQL.
func SnapshotInsertSlot(query string) (int, bool) {
	switch query {
	case insertSnapshotEntry:
		return 0, true
	case insertRevisionKeyChange:
		return 1, true
	default:
		return 0, false
	}
}
