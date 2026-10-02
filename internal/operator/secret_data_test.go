package operator

import "testing"

func TestDataEqualRequiresIdenticalKeysForEmptyValues(t *testing.T) {
	t.Parallel()

	if dataEqual(map[string][]byte{"EXPECTED": {}}, map[string][]byte{"REPLACEMENT": {}}) {
		t.Fatal("different empty-valued keys compared equal")
	}
	if !dataEqual(map[string][]byte{"EXPECTED": {}}, map[string][]byte{"EXPECTED": nil}) {
		t.Fatal("the same empty-valued key should compare equal")
	}
}
