package lint

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// This guard runs in the existing CI lint matrix. An exception grants one
// reviewed engine protocol owner; ordinary data queries never enter this file.
func TestRawSQLProtocolsRepo(t *testing.T) {
	root := repoRoot(t)
	if reviewedPinsError != nil {
		t.Fatal(reviewedPinsError)
	}
	protocols, err := reviewedPins.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	eachRepoContext(t, func(t *testing.T, pkgs []*packages.Package) {
		name := t.Name()
		context := name[strings.LastIndex(name, "/")+1:]
		for _, finding := range CheckRawSQL(pkgs, root, Module, protocols, context) {
			t.Error(finding)
		}
	})
}
