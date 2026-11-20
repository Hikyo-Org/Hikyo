package lint

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestTransportCannotConstructALocalPrincipal(t *testing.T) {
	eachRepoContext(t, func(t *testing.T, pkgs []*packages.Package) {
		for _, finding := range CheckTransportLocalPrincipal(pkgs) {
			t.Error(finding)
		}
	})
}
