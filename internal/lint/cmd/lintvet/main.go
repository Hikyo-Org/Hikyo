// Command lintvet is the repository's analyzer set as a go vet tool:
//
//	go vet -vettool=$(which lintvet) ./...
//
// It is run only through the driver test in internal/lint, which builds it and
// runs it once per analysis context (lint-analysis driver ADR). The driver
// decodes `go vet -json` and separates the reserved summary diagnostics
// (target presence, redaction pins) from findings; in text mode those summaries
// print as ordinary findings, so do not run the tool by hand. An analyzer that
// reads declared inputs also needs -<analyzer>.root and -<analyzer>.inputs (the
// repository root and the digest of its declared inputs); the driver passes
// them, and the analyzer refuses to run without them.
package main

import (
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/Hikyo-Org/hikyo/internal/lint"
)

func main() { unitchecker.Main(lint.Analyzers()...) }
