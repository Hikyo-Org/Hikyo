package lint

import (
	"strings"
	"testing"
)

func TestReviewedPinsShareHashesWithoutGrantingAuthority(t *testing.T) {
	source := `{"helpers":{"helpers.go:Build":"pin","helpers.go:build":"other"},"protocols":{"protocol.go:execute":{"hash":"owner","reason":"engine protocol","dependencies":["helpers.go:Build","helpers.go:build"]}}}`
	inventory, err := parseReviewedPins([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	protocols, err := inventory.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	if len(protocols) != 1 || protocols["helpers.go:Build"].Hash != "" || len(protocols["protocol.go:execute"].Dependencies) != 2 {
		t.Fatalf("shared helper gained protocol authority: %v", protocols)
	}
	for _, change := range []struct{ old, new string }{
		{`"helpers.go:Build":"pin"`, `"helpers.go:Build":"pin","helpers.go:Build":"changed"`},
		{`"hash":"owner"`, `"hash":"owner","HASH":"changed"`},
		{`"reason":"engine protocol"`, `"reason":"engine protocol","unexpected":1`},
		{`"helpers.go:Build","helpers.go:build"`, `"helpers.go:Build","helpers.go:Build"`},
		{`"helpers.go:Build","helpers.go:build"`, `"helpers.go:Missing","helpers.go:build"`},
		{`"helpers.go:Build","helpers.go:build"`, `"helpers.go:Build"`},
	} {
		if _, err := parseReviewedPins([]byte(strings.Replace(source, change.old, change.new, 1))); err == nil {
			t.Fatalf("invalid pins accepted: %s", change.new)
		}
	}
}

func TestSharedHelperBuildVariantsRemainExact(t *testing.T) {
	source := `{"helpers":{"helpers.go:Build":"common"},"build_helpers":{"windows":{"helpers.go:Build":"windows"}},"protocols":{"protocol.go:execute":{"hash":"owner","reason":"engine protocol","build_dependencies":{"default":["helpers.go:Build"],"windows":["helpers.go:Build"]}}}}`
	inventory, err := parseReviewedPins([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	protocols, err := inventory.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	for context, expected := range map[string]string{"default": "common", "windows": "windows"} {
		if got := rawSQLProtocolDependencies(protocols["protocol.go:execute"], context)["helpers.go:Build"]; got != expected {
			t.Fatalf("%s hash = %s", context, got)
		}
	}
	for _, modified := range []string{strings.Replace(source, `"build_helpers":{"windows"`, `"build_helpers":{"unknown"`, 1), strings.Replace(source, `"default":["helpers.go:Build"],"windows":["helpers.go:Build"]`, `"default":["helpers.go:Build"]`, 1)} {
		if _, err := parseReviewedPins([]byte(modified)); err == nil {
			t.Fatal("unused/unknown build hash accepted")
		}
	}
}

func TestFailedReviewedPinDecodeExposesNoExceptions(t *testing.T) {
	source := `{"contract_differences":{"Query":{"authority":"reviewed","sql_hash":{"sqlite":"sql","postgres":"sql"},"api_hash":{"sqlite":"api","postgres":"api"}}},"unknown":true}`
	inventory, err := parseReviewedPins([]byte(source))
	if err == nil || len(inventory.ContractDifferences) != 1 {
		t.Fatal("fixture did not reach partially decoded metadata")
	}
	differences, engines := queryContractPins(inventory, err)
	if differences != nil || engines != nil {
		t.Fatal("failed metadata exposed query exceptions")
	}
}
