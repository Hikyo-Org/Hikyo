package lint

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// UpgradeBuildAuthority confines the empty-database build primitive to the
// release generator. Resolve symbols through Go types so import aliases and
// captured function values cannot turn this into a runtime migration bypass.
func UpgradeBuildAuthority(pkgs []*packages.Package) []string {
	var findings []string
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for identifier, object := range pkg.TypesInfo.Uses {
			if object.Pkg() == nil || object.Pkg().Path() != Module+"/internal/store/upgrade" || object.Name() != "BuildScratchSchema" {
				continue
			}
			position := pkg.Fset.Position(identifier.Pos())
			if strings.HasSuffix(position.Filename, "_test.go") {
				continue
			}
			if pkg.PkgPath == Module+"/internal/app" && filepath.Base(position.Filename) == "releasecompat.go" {
				continue
			}
			findings = append(findings, fmt.Sprintf("%s: scratch schema generation is confined to app/releasecompat.go", position))
		}
	}
	return findings
}
