package lint

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// fixtureRoot is relative to the module root, where vet runs: a wildcard
// pattern does not match testdata packages by import path.
const fixtureRoot = "./internal/lint/testdata/"

const fixtureImportRoot = Module + "/internal/lint/testdata/"

// vetFixtures are the negative fixtures of the ported analyzers. Each package
// keeps its place under testdata/ and states its expected diagnostics as
// `// want "regexp"` comments, checked by the vet driver below. analysistest
// is not used: it loads every dependency from source, the cost the vet driver
// removes.
var vetFixtures = []vetFixture{
	{
		name: "proofsig", analyzer: "proofsig", patterns: []string{fixtureRoot + "badstore"},
		flags:   []string{"-proofsig.store=" + fixtureImportRoot + "badstore"},
		targets: []string{fixtureImportRoot + "badstore"},
		old: func(pkgs []*packages.Package) []string {
			return CheckProofSignatures(pkgs, fixtureImportRoot+"badstore")
		},
	},
	{
		name: "forgeguard", analyzer: "forgeguard", patterns: []string{fixtureRoot + "badnil"},
		old: CheckProofForgery,
	},
	{
		name: "txresult", analyzer: "txresult", patterns: []string{fixtureRoot + "badtxresult"},
		old: CheckTransactionResults,
	},
	{
		name: "fence", analyzer: "fence", patterns: []string{fixtureRoot + "badseal"},
		flags:   []string{"-fence.service=" + fixtureImportRoot + "badseal"},
		targets: []string{fixtureImportRoot + "badseal"},
		old: func(pkgs []*packages.Package) []string {
			return CheckFenceCompleteness(pkgs, fixtureImportRoot+"badseal")
		},
	},
	{
		name: "serviceseams", analyzer: "serviceseams", patterns: []string{fixtureRoot + "badseam"},
		flags:   []string{"-serviceseams.service=" + fixtureImportRoot + "badseam", "-serviceseams.store=" + fixtureImportRoot + "badseam"},
		targets: []string{fixtureImportRoot + "badseam"},
		old: func(pkgs []*packages.Package) []string {
			return CheckServiceSeams(pkgs, fixtureImportRoot+"badseam", fixtureImportRoot+"badseam")
		},
	},
	{
		name: "handles", analyzer: "handles", patterns: []string{fixtureRoot + "badhandle"},
		old: CheckDriverHandles,
	},
	{
		name: "redactfmt", analyzer: "redactfmt", patterns: []string{fixtureRoot + "badredact"},
		old: CheckSensitiveFormatting,
	},
	{
		name: "testobservers", analyzer: "testobservers", patterns: []string{fixtureRoot + "badobserver/..."},
		flags:   []string{"-testobservers.installers=" + observerOwner + "=SetQueryObserver,SetMutationFailureObserver,SetSCIMPhaseObserver"},
		targets: []string{observerOwner},
		old: func(pkgs []*packages.Package) []string {
			return checkTestObserversIn(pkgs, map[string][]string{observerOwner: {"SetQueryObserver", "SetMutationFailureObserver", "SetSCIMPhaseObserver"}})
		},
	},
	{
		name: "recoverylookup", analyzer: "recoverylookup", patterns: []string{fixtureRoot + "badrecovery"},
		flags:   []string{"-recoverylookup.rules=" + rulesFlag(badRecoveryRules)},
		targets: []string{fixtureImportRoot + "badrecovery"},
		old: func(pkgs []*packages.Package) []string {
			return CheckRecoveryLookup(pkgs, badRecoveryRules)
		},
	},
	{
		name: "containmentsetters", analyzer: "containmentsetters", patterns: []string{fixtureRoot + "badsetter"},
		flags:   []string{"-containmentsetters.rules=" + rulesFlag(badSetterRules)},
		targets: []string{fixtureImportRoot + "badsetter"},
		old: func(pkgs []*packages.Package) []string {
			return CheckContainmentSetters(pkgs, badSetterRules)
		},
	},
	{
		name: "stablecopies", analyzer: "stablecopies", patterns: []string{fixtureRoot + "badstablecopies"},
		flags:      []string{"-stablecopies.anchor=" + fixtureImportRoot + "badstablecopies"},
		targets:    []string{fixtureImportRoot + "badstablecopies"},
		inputsRoot: "internal/lint/testdata/badstablecopies/root", messages: true,
		old: func([]*packages.Package) []string {
			return CheckStableCopies(filepath.Join(moduleRoot(), "internal/lint/testdata/badstablecopies/root"))
		},
	},
	{
		name: "upgradebuild", analyzer: "upgradebuild", patterns: []string{fixtureRoot + "badupgradebuild"},
		old: UpgradeBuildAuthority,
	},
}

var badRecoveryRules = []RecoveryRule{
	{Name: "RecoveryAccountByID", Allowed: map[string][]string{fixtureImportRoot + "badrecovery": {"ResetAllowed", "NeverCalled", "licensedType.Reset"}}},
}

var badSetterRules = []RecoveryRule{
	{Name: "ContainPrincipal", Allowed: map[string][]string{fixtureImportRoot + "badsetter": {"Licensed", "NeverCalled"}}},
	{Name: "ReleasePrincipalContainment", Allowed: map[string][]string{}},
}

func rulesFlag(rules []RecoveryRule) string { return (*confinementFlag)(&rules).String() }

const observerOwner = fixtureImportRoot + "badobserver/owner"

var wantQuoted = regexp.MustCompile(`//\s*want\s+(.+)$`)

type wantKey struct {
	file string // path from testdata/, slash separated
	line int
}

// fixtureKey normalizes a diagnostic or source path to its testdata-relative
// form, so the vet tool's absolute paths and the scanned paths compare equal.
func fixtureKey(file string) string {
	file = filepath.ToSlash(file)
	index := strings.LastIndex(file, "/testdata/")
	if index < 0 {
		return file
	}
	return file[index+1:]
}

// splitPosn splits "file:line:col".
func splitPosn(posn string) (file string, line int, err error) {
	parts := strings.Split(posn, ":")
	if len(parts) < 3 {
		return "", 0, fmt.Errorf("position %q is not file:line:col", posn)
	}
	line, err = strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		return "", 0, fmt.Errorf("position %q has no line: %w", posn, err)
	}
	return strings.Join(parts[:len(parts)-2], ":"), line, nil
}

// fixtureDirs resolves import-path patterns to the directories whose files
// carry the fixture's want comments.
func fixtureDirs(patterns []string) []string {
	var dirs []string
	for _, pattern := range patterns {
		rel := strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/...")
		dirs = append(dirs, filepath.Join(moduleRoot(), filepath.FromSlash(rel)))
	}
	return dirs
}

// scanWants collects the want expectations of every .go file under dirs.
func scanWants(t *testing.T, dirs []string) map[wantKey][]*regexp.Regexp {
	t.Helper()
	wants := map[wantKey][]*regexp.Regexp{}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for number, line := range strings.Split(string(source), "\n") {
				match := wantQuoted.FindStringSubmatch(line)
				if match == nil {
					continue
				}
				rest := strings.TrimSpace(match[1])
				for rest != "" {
					quoted, err := strconv.QuotedPrefix(rest)
					if err != nil {
						return fmt.Errorf("%s:%d: want expects quoted regular expressions: %w", path, number+1, err)
					}
					pattern, err := strconv.Unquote(quoted)
					if err != nil {
						return fmt.Errorf("%s:%d: %w", path, number+1, err)
					}
					re, err := regexp.Compile(pattern)
					if err != nil {
						return fmt.Errorf("%s:%d: %w", path, number+1, err)
					}
					key := wantKey{fixtureKey(path), number + 1}
					wants[key] = append(wants[key], re)
					rest = strings.TrimSpace(strings.TrimPrefix(rest, quoted))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return wants
}

// checkWants requires every want to match one diagnostic of the fixture's
// analyzer on its line, and every such diagnostic to match a want.
func checkWants(t *testing.T, f vetFixture, findings []string) {
	t.Helper()
	wants := scanWants(t, fixtureDirs(f.patterns))
	if len(wants) == 0 {
		t.Fatalf("fixture %s declares no want comments", f.name)
	}
	prefix := ": " + f.analyzer + ": "
	for _, finding := range findings {
		cut := strings.Index(finding, prefix)
		if cut < 0 {
			continue // another analyzer's diagnostic
		}
		file, line, err := splitPosn(finding[:cut])
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		key := wantKey{fixtureKey(file), line}
		if !strings.Contains(key.file, "testdata/") {
			continue
		}
		message := finding[cut+len(prefix):]
		matched := false
		for i, re := range wants[key] {
			if re.MatchString(message) {
				wants[key] = slices.Delete(wants[key], i, i+1)
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("unexpected diagnostic: %s", finding)
		}
	}
	for _, key := range slices.SortedFunc(maps.Keys(wants), func(a, b wantKey) int {
		return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
	}) {
		for _, re := range wants[key] {
			t.Errorf("%s:%d: no diagnostic matched want %q", key.file, key.line, re)
		}
	}
}

// loadPatterns are the patterns relative to this package's directory, where the
// old go/packages harness runs.
func (f vetFixture) loadPatterns() []string {
	out := make([]string, len(f.patterns))
	for i, pattern := range f.patterns {
		out[i] = "./" + strings.TrimPrefix(pattern, "./internal/lint/")
	}
	return out
}

func fixtureFlagsAndPatterns(t *testing.T, fixtures []vetFixture) (flags, patterns []string) {
	t.Helper()
	for _, f := range fixtures {
		flags = append(flags, "-"+f.analyzer)
		flags = append(flags, f.flags...)
		if f.inputsRoot != "" {
			flags = append(flags, inputFlags(t, f.analyzer, filepath.Join(moduleRoot(), filepath.FromSlash(f.inputsRoot)))...)
		}
		patterns = append(patterns, f.patterns...)
	}
	return flags, patterns
}

// Each negative fixture runs through the vet driver under every context; the
// analyzers are enabled together and each fixture is judged on its own
// analyzer's diagnostics.
func TestVetDriverFixtures(t *testing.T) {
	bin := buildVetTool(t)
	flags, patterns := fixtureFlagsAndPatterns(t, vetFixtures)
	for _, ctx := range Contexts {
		t.Run(ctx.Name, func(t *testing.T) {
			out := runVet(t, bin, ctx, flags, patterns)
			for _, f := range vetFixtures {
				t.Run(f.name, func(t *testing.T) {
					checkWants(t, f, out.Findings)
					for _, finding := range MissingTargets(f.analyzer, f.targets, out.Targets[f.analyzer]) {
						t.Error(finding)
					}
				})
			}
		})
	}
}

// A renamed installer and an absent owner package are reported: the first by
// the analyzer, the second by target aggregation in the driver.
func TestVetDriverReportsMissingObserverTargets(t *testing.T) {
	bin := buildVetTool(t)
	flags := []string{
		"-testobservers",
		"-testobservers.installers=" + observerOwner + "=RenamedObserver;" + observerOwner + "/missing=SetQueryObserver",
	}
	for _, ctx := range Contexts {
		t.Run(ctx.Name, func(t *testing.T) {
			out := runVet(t, bin, ctx, flags, []string{fixtureRoot + "badobserver/..."})
			joined := strings.Join(out.Findings, "\n")
			if want := "installer " + observerOwner + ".RenamedObserver not found"; !strings.Contains(joined, want) {
				t.Errorf("findings do not contain %q:\n%s", want, joined)
			}
			missing := MissingTargets("testobservers", []string{observerOwner, observerOwner + "/missing"}, out.Targets["testobservers"])
			if len(missing) != 1 || !strings.Contains(missing[0], observerOwner+"/missing") {
				t.Errorf("target aggregation = %v, want exactly the absent owner package", missing)
			}
		})
	}
}

// Every old finding must also be reported by the vet driver, on the real
// repository and on every negative fixture, under every context. The old
// harness hides in-package test files, so the driver may report more.
func TestVetDriverParity(t *testing.T) {
	bin := buildVetTool(t)
	flags, patterns := fixtureFlagsAndPatterns(t, vetFixtures)
	for _, ctx := range Contexts {
		t.Run(ctx.Name, func(t *testing.T) {
			repo := runVet(t, bin, ctx, repoInputFlags(t), []string{Module + "/..."})
			pkgs, err := LoadRepoIn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range Checks() {
				requireSubset(t, c.Analyzer.Name+" on the repository", RunOver(c.For(moduleRoot()), pkgs).Findings, repo.Findings)
			}
			fixtures := runVet(t, bin, ctx, flags, patterns)
			for _, f := range vetFixtures {
				loaded, err := LoadIn(ctx, f.loadPatterns()...)
				if err != nil {
					t.Fatal(err)
				}
				old := f.old(loaded)
				if len(old) == 0 {
					t.Errorf("old harness found nothing in fixture %s: the parity check would be vacuous", f.name)
				}
				current := fixtures.Findings
				if f.messages {
					current = findingMessages(fixtures.Findings, f.analyzer)
				}
				requireSubset(t, "fixture "+f.name, old, current)
			}
		})
	}
}

func requireSubset(t *testing.T, what string, old, current []string) {
	t.Helper()
	have := map[string]bool{}
	for _, finding := range current {
		have[finding] = true
	}
	for _, finding := range old {
		if !have[finding] {
			t.Errorf("%s: the old harness reports a finding the vet driver does not: %s", what, finding)
		}
	}
}

// A `//line` directive cannot move a production file into the test-file
// deferral or into fence's wholesale reencrypt.go exemption.
func TestVetDriverIgnoresLineDirectives(t *testing.T) {
	bin := buildVetTool(t)
	flags := []string{"-fence", "-fence.service=" + fixtureImportRoot + "badline", "-handles"}
	for _, ctx := range Contexts {
		t.Run(ctx.Name, func(t *testing.T) {
			out := runVet(t, bin, ctx, flags, []string{fixtureRoot + "badline"})
			joined := strings.Join(out.Findings, "\n")
			for _, want := range []string{
				"fence: seal seals ciphertext but never calls the writer fence",
				"handles: ",
			} {
				if !strings.Contains(joined, want) {
					t.Errorf("findings lack %q:\n%s", want, joined)
				}
			}
			if len(out.Deferred) != 0 {
				t.Errorf("a production file claiming a test name was deferred: %v", out.Deferred)
			}
		})
	}
}
