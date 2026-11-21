package lint

import (
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// UpgradeBuild is the upgradebuild analyzer.
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
