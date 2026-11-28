package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// The CLI prints a run error code bare only when it is one of the server's fixed words:
// its list must be the executor's rotCode constants plus abandoned and unknown, or a
// real code would print quoted.
func TestRotationRunErrorWordsMatchTheExecutor(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../service/rotation_executor.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					t.Fatalf("%s has an implicit value: give each rotCode constant a string literal", name.Name)
				}
				lit, isLit := vs.Values[i].(*ast.BasicLit)
				if strings.HasPrefix(name.Name, "rotCode") && isLit {
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					codes = append(codes, value)
				}
			}
		}
	}
	want := append(codes, "abandoned", "unknown")
	slices.Sort(want)
	got := slices.Clone(rotationRunErrorWords)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("rotationRunErrorWords = %v, want %v", got, want)
	}
	for _, phase := range []apigen.RotationRunPhase{
		apigen.RotationRunPhaseDone, apigen.RotationRunPhaseFailed, apigen.RotationRunPhaseMinted, apigen.RotationRunPhaseMinting,
		apigen.RotationRunPhaseOverlap, apigen.RotationRunPhasePublished, apigen.RotationRunPhaseQuarantine,
		apigen.RotationRunPhaseRevokeRefused, apigen.RotationRunPhaseRevoking, apigen.RotationRunPhaseStaged,
	} {
		if !slices.Contains(rotationRunPhaseWords, string(phase)) {
			t.Errorf("phase %q prints quoted", phase)
		}
	}
	if len(rotationRunPhaseWords) != 10 {
		t.Errorf("rotationRunPhaseWords has %d phases, the API has 10", len(rotationRunPhaseWords))
	}
}
