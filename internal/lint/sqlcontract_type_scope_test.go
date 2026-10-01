package lint

import "testing"

func TestRuntimeTypeCompatibilityIsQueryScoped(t *testing.T) {
	for _, test := range []struct{ query, field, sqlite, postgres string }{
		{"PKICreateIssuer", "NotAfter", "string", "pgtype.Timestamptz"},
		{"RuntimeDynamicClaimLease", "Until", "sql.NullString", "pgtype.Timestamptz"},
		{"AdapterWorkerFinishJobQuery", "Due", "string", "pgtype.Timestamptz"},
		{"AdapterWorkerActivateApplyTarget", "SelectedRaw", "string", "[]byte"},
	} {
		if !compatibleType(test.query, test.field, test.sqlite, test.postgres) {
			t.Errorf("reviewed pair refused: %+v", test)
		}
		if compatibleType("UnreviewedQuery", test.field, test.sqlite, test.postgres) {
			t.Errorf("pair leaked outside reviewed query: %+v", test)
		}
	}
	if !compatibleType("AdapterWorkerLoadExecutionQuery", "VariableHidden", "int64", "int32") {
		t.Fatal("0/1 CASE carrier refused")
	}
	if compatibleType("UnreviewedQuery", "VariableHidden", "int64", "int32") {
		t.Fatal("CASE carrier exception leaked")
	}
}
