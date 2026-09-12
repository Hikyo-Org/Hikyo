package parameters

import (
	"strings"
	"testing"
)

func TestValidatedSinglePassSubstitution(t *testing.T) {
	decl := map[string]string{"PR_NUMBER": "^[0-9]{1,6}$"}
	for _, values := range []map[string]string{nil, {"PR_NUMBER": "123x"}, {"PR_NUMBER": "x123"}, {"PR_NUMBER": "123", "EXTRA": "x"}} {
		if err := Validate(decl, values); err == nil {
			t.Fatalf("accepted invalid inputs %v", values)
		}
	}
	if err := Validate(decl, map[string]string{"PR_NUMBER": "123"}); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("https://pr-${PR_NUMBER}.preview.example.com", map[string]string{"PR_NUMBER": "123"}, 1024)
	if err != nil || got != "https://pr-123.preview.example.com" {
		t.Fatalf("resolve = %q, %v", got, err)
	}
	got, err = Resolve("${FIRST}", map[string]string{"FIRST": "${SECOND}", "SECOND": "evaluated"}, 1024)
	if err != nil || got != "${SECOND}" {
		t.Fatalf("recursive evaluation: %q %v", got, err)
	}
}

func TestTemplateGrammarAndBounds(t *testing.T) {
	for _, value := range []string{"${UNKNOWN}", "${NAME", "${NAME:-default}", "${NAME.other}", "${}"} {
		if err := CheckReferences(value, map[string]string{"NAME": ".*"}); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if _, err := Resolve("${NAME}${NAME}", map[string]string{"NAME": strings.Repeat("x", 256)}, 511); err == nil {
		t.Fatal("accepted expanded value over limit")
	}
	if err := CheckDeclaration("NAME", "["); err == nil {
		t.Fatal("accepted invalid regex")
	}
	if err := Validate(map[string]string{"NAME": "[0-9]+"}, map[string]string{"NAME": "x12"}); err == nil {
		t.Fatal("pattern was not full-match")
	}
	if err := CheckSupplied(map[string]string{"NAME": "a\nb"}); err == nil {
		t.Fatal("accepted newline")
	}
}

func TestDeclarationPatternHasPortableStorageEncoding(t *testing.T) {
	// A literal NUL is valid RE2, but PostgreSQL TEXT cannot store it. Refuse
	// it as a named validation error before either database is consulted.
	err := CheckDeclaration("PR_NUMBER", "[0-9]+\x00?")
	if err == nil || !strings.Contains(err.Error(), "PR_NUMBER") || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("nonportable pattern error = %v", err)
	}
	// An escaped RE2 sequence is ordinary text and remains representable.
	if err := CheckDeclaration("PR_NUMBER", `[0-9]+\x00?`); err != nil {
		t.Fatalf("escaped pattern rejected: %v", err)
	}
}
