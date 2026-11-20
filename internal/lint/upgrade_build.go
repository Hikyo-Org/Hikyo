package lint

import (
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// UpgradeBuildAuthority confines the empty-database build primitive to the
// release generator. Resolve symbols through Go types so import aliases and
// captured function values cannot turn this into a runtime migration bypass.
func UpgradeBuildAuthority(pkgs []*packages.Package) []string {
	return RunOver(Check{Analyzer: UpgradeBuild}, pkgs).Findings
}

// UpgradeBuild is UpgradeBuildAuthority as an analyzer.
var UpgradeBuild = &analysis.Analyzer{
	Name: "upgradebuild",
	Doc:  "BuildScratchSchema is confined to app/releasecompat.go",
	Run: func(pass *analysis.Pass) (any, error) {
		for identifier, object := range pass.TypesInfo.Uses {
			if object.Pkg() == nil || object.Pkg().Path() != Module+"/internal/store/upgrade" || object.Name() != "BuildScratchSchema" {
				continue
			}
			position := pass.Fset.Position(identifier.Pos())
			if strings.HasSuffix(position.Filename, "_test.go") {
				continue
			}
			if pass.Pkg.Path() == Module+"/internal/app" && filepath.Base(position.Filename) == "releasecompat.go" {
				continue
			}
			pass.Reportf(identifier.Pos(), "scratch schema generation is confined to app/releasecompat.go")
		}
		return nil, nil
	},
}
