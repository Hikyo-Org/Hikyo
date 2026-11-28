package ci_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// static-invariants.sh selects tests by name pattern, and go test passes when a
// pattern selects nothing. These tests keep the selection honest: the named
// registry and boundary checks exist and are selected, and no selected
// isolation test is anything but a source-only invariant.

const staticInvariantsScript = "static-invariants.sh"

var staticRunPattern = regexp.MustCompile(`(?m)^run_in (\S+) (\S+)(?: -test\.run '([^']+)')?$`)

type staticSuite struct{ dir, binary, pattern string }

func staticInvariantSuites(t *testing.T) []staticSuite {
	t.Helper()
	raw, err := os.ReadFile(staticInvariantsScript)
	if err != nil {
		t.Fatal(err)
	}
	var suites []staticSuite
	for _, m := range staticRunPattern.FindAllStringSubmatch(string(raw), -1) {
		suites = append(suites, staticSuite{dir: m[1], binary: m[2], pattern: m[3]})
	}
	if len(suites) != 3 {
		t.Fatalf("static-invariants.sh runs %d suites, want boundary, isolation and lint", len(suites))
	}
	return suites
}

// topLevelTests maps each Test function of the package to its file.
func topLevelTests(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", dir, "*_test.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no test files in %s: %v", dir, err)
	}
	tests := map[string]string{}
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				tests[fn.Name.Name] = filepath.Base(file)
			}
		}
	}
	return tests
}

func TestStaticInvariantsSelectTheRegistryAndBoundaryChecks(t *testing.T) {
	suites := staticInvariantSuites(t)
	// The cache registry breakage of #1515 (invariant 12), the package fences, and the
	// per-package lint analyzers that missed #1522 and #1523.
	required := map[string][]string{
		"internal/boundary":  {"TestForbiddenEdges"},
		"internal/lint":      {"TestVetDriverChangedPackages"},
		"internal/isolation": {"TestInvariant12CacheDiscipline", "TestCacheDeclarationFindingsChecksExternalTestPackages", "TestInvariant01ClassificationTotality", "TestInvariant13AllowlistPinning"},
	}
	for _, suite := range suites {
		want := required[suite.dir]
		if want == nil {
			t.Fatalf("unexpected static suite %s", suite.dir)
		}
		tests := topLevelTests(t, suite.dir)
		selects := func(name string) bool { return true }
		if suite.pattern != "" {
			// go test splits -test.run on "/"; the first element selects top-level tests.
			re := regexp.MustCompile(strings.SplitN(suite.pattern, "/", 2)[0])
			selects = re.MatchString
		}
		for _, name := range want {
			if _, ok := tests[name]; !ok {
				t.Errorf("%s: %s no longer exists; static-invariants.sh would silently skip it", suite.dir, name)
			} else if !selects(name) {
				t.Errorf("%s: pattern %q does not select %s", suite.dir, suite.pattern, name)
			}
		}
	}
}

func TestStaticInvariantsNeverSelectDatabaseSuites(t *testing.T) {
	for _, suite := range staticInvariantSuites(t) {
		tests := topLevelTests(t, suite.dir)
		switch suite.dir {
		case "internal/isolation":
			re := regexp.MustCompile(suite.pattern)
			selected := 0
			for name, file := range tests {
				if !re.MatchString(name) {
					continue
				}
				selected++
				// invariants_test.go holds the source-only invariants; every
				// database-backed isolation test lives in another file.
				if file != "invariants_test.go" {
					t.Errorf("%s (%s) is selected for feedback but is not a source-only invariant", name, file)
				}
			}
			if selected == 0 {
				t.Error("the isolation pattern selects no test")
			}
		case "internal/lint":
			// Only the changed-package form; the whole-module pass stays in the full run.
			if suite.pattern != "^TestVetDriverChangedPackages$" {
				t.Errorf("lint pattern %q must select only TestVetDriverChangedPackages", suite.pattern)
			}
			raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "lint", "lint.go"))
			if err != nil {
				t.Fatal(err)
			}
			if !regexp.MustCompile(`(?s)var Contexts = \[\]BuildContext\{\s*\{Name: "default"\}`).Match(raw) {
				t.Error(`lint.Contexts no longer begins with the "default" context`)
			}
		}
	}
}

func TestFeedbackPlansStaticInvariantsOnTheToolingRunnerOnly(t *testing.T) {
	raw, err := os.ReadFile("feedback-plan.sh")
	if err != nil {
		t.Fatal(err)
	}
	plan := string(raw)
	// The application lane is the feedback critical path (go compile, 79 to 161 s of a 220 s
	// deadline); the check sits in the block that excludes that scope.
	start := strings.LastIndex(plan, `if [ "$scope" != application ]; then`)
	end := strings.Index(plan[start+1:], "\n\t\t\tfi\n\t\t\t;;")
	if start < 0 || end < 0 || !strings.Contains(plan[start:start+1+end], "feedback_checks+=(static-invariants)") {
		t.Fatal("static-invariants must be selected inside the scope != application block")
	}
	if strings.Count(plan, "feedback_checks+=(static-invariants)") != 1 {
		t.Error("static-invariants must be selected exactly once")
	}
	if !strings.Contains(plan, "static-invariants) feedback_check_needs=(go) ;;") {
		t.Error("static-invariants must declare its Go prerequisite")
	}
}
