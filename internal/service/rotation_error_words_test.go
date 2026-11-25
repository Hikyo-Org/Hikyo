package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// rotCodeValues reads every rotCode* constant of rotation_executor.go, so the test
// below sees a constant added tomorrow without anyone remembering to list it.
func rotCodeValues(t *testing.T) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "rotation_executor.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
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
				if !strings.HasPrefix(name.Name, "rotCode") {
					continue
				}
				if i >= len(vs.Values) {
					t.Fatalf("%s has an implicit value: give each rotCode constant a string literal", name.Name)
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					t.Fatalf("%s is not a string literal", name.Name)
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out[name.Name] = value
			}
		}
	}
	return out
}

// Every run error code the executor can write is a fixed word the views pass on; a
// new rotCode constant that is not in the word list would silently read as unknown.
func TestEveryRotCodeIsAFixedErrorWord(t *testing.T) {
	codes := rotCodeValues(t)
	if len(codes) < 10 {
		t.Fatalf("found only %d rotCode constants: the parser lost the block", len(codes))
	}
	for name, value := range codes {
		if got := rotationErrorWord(value); got != value {
			t.Errorf("%s = %q reads as %q: add it to rotationRunErrorWords (and the CLI's rotationRunErrorWords)", name, value, got)
		}
	}
	for _, word := range rotationRunErrorWords {
		if word == "abandoned" {
			continue
		}
		found := false
		for _, value := range codes {
			found = found || value == word
		}
		if !found {
			t.Errorf("rotationRunErrorWords lists %q, which no rotCode constant defines", word)
		}
	}
	if got := rotationErrorWord("retries_exhausted"); got != RotationErrorUnknown {
		t.Errorf("a halt reason is not a run error code: got %q", got)
	}
}
