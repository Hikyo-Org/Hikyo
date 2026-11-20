// Command lintvet is the repository's analyzer set as a go vet tool:
//
//	go vet -vettool=$(which lintvet) ./...
//
// The driver test in internal/lint builds it and runs it once per analysis
// context (lint-analysis driver ADR).
package main

import (
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/Hikyo-Org/hikyo/internal/lint"
)

func main() { unitchecker.Main(lint.Analyzers()...) }
