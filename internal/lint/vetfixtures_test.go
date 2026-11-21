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
	},
	{
		name: "forgeguard", analyzer: "forgeguard", patterns: []string{fixtureRoot + "badnil"},
	},
	{
		name: "txresult", analyzer: "txresult", patterns: []string{fixtureRoot + "badtxresult"},
	},
	{
		name: "fence", analyzer: "fence", patterns: []string{fixtureRoot + "badseal"},
		flags:   []string{"-fence.service=" + fixtureImportRoot + "badseal"},
		targets: []string{fixtureImportRoot + "badseal"},
	},
	{
		name: "serviceseams", analyzer: "serviceseams", patterns: []string{fixtureRoot + "badseam"},
		flags:   []string{"-serviceseams.service=" + fixtureImportRoot + "badseam", "-serviceseams.store=" + fixtureImportRoot + "badseam"},
		targets: []string{fixtureImportRoot + "badseam"},
	},
	{
		name: "handles", analyzer: "handles", patterns: []string{fixtureRoot + "badhandle"},
	},
	{
		name: "redactfmt", analyzer: "redactfmt", patterns: []string{fixtureRoot + "badredact"},
	},
	{
		name: "testobservers", analyzer: "testobservers", patterns: []string{fixtureRoot + "badobserver/..."},
		flags:   []string{"-testobservers.installers=" + observerOwner + "=SetQueryObserver,SetMutationFailureObserver,SetSCIMPhaseObserver"},
		targets: []string{observerOwner},
	},
	{
		name: "recoverylookup", analyzer: "recoverylookup", patterns: []string{fixtureRoot + "badrecovery"},
		flags:   []string{"-recoverylookup.rules=" + rulesFlag(badRecoveryRules)},
		targets: []string{fixtureImportRoot + "badrecovery"},
	},
	{
		name: "containmentsetters", analyzer: "containmentsetters", patterns: []string{fixtureRoot + "badsetter"},
		flags:   []string{"-containmentsetters.rules=" + rulesFlag(badSetterRules)},
		targets: []string{fixtureImportRoot + "badsetter"},
	},
	{
		name: "grantlock", analyzer: "grantlock", patterns: []string{fixtureRoot + "badgrant"},
		flags:      []string{"-grantlock.surface=" + fixtureImportRoot + "badgrant"},
		targets:    []string{fixtureImportRoot + "badgrant"},
		inputsRoot: ".",
	},
	{
		name: "denialwriter", analyzer: "denialwriter", patterns: []string{fixtureRoot + "badauthn"},
		flags:      []string{"-denialwriter.surface=" + fixtureImportRoot + "badauthn", "-denialwriter.writers=WriteDenial"},
		targets:    []string{fixtureImportRoot + "badauthn"},
		inputsRoot: ".",
	},
	{
		name: "containmentgates", analyzer: "containmentgates", patterns: []string{fixtureRoot + "badcontainmentgates"},
		flags:      []string{"-containmentgates.anchor=" + fixtureImportRoot + "badcontainmentgates", "-containmentgates.exempt=Exempted,ExemptedButGated,Vanished"},
		targets:    []string{fixtureImportRoot + "badcontainmentgates"},
		inputsRoot: "internal/lint/testdata/badcontainmentgates",
	},
	{
		name: "reencryptcoverage", analyzer: "reencryptcoverage", patterns: []string{fixtureRoot + "badreencrypt"},
		flags: []string{
			"-reencryptcoverage.anchor=" + fixtureImportRoot + "badreencrypt",
			"-reencryptcoverage.covered=sealed_things.payload_sealed,both_sets.ambiguous,gone_table.gone_column",
			"-reencryptcoverage.exempt=sealed_things.wrapped_key,both_sets.ambiguous",
		},
		targets:    []string{fixtureImportRoot + "badreencrypt"},
		inputsRoot: "internal/lint/testdata/badreencrypt",
	},
	{
		name: "sqlpredicate", analyzer: "sqlpredicate", patterns: []string{fixtureRoot + "badsqlpredicate"},
		flags:      []string{"-sqlpredicate.anchor=" + fixtureImportRoot + "badsqlpredicate", "-sqlpredicate.nopins"},
		targets:    []string{fixtureImportRoot + "badsqlpredicate"},
		inputsRoot: "internal/lint/testdata/badsqlpredicate",
	},
	{
		// The fixture tree also holds an ignore-tagged file and a testdata
		// directory: vet cannot see the first, which the driver's unanalyzed-file
		// check finds, and neither tool reads the second. internal/store/.gitkeep
		// keeps an empty internal/store: checkUnanalyzedGoFiles also runs the
		// synchronous_commit walk, which refuses a missing internal/store.
		name: "jsonv1", analyzer: "jsonv1", patterns: []string{fixtureRoot + "badjsonv1"},
		flags: []string{"-jsonv1.root=" + filepath.Join(moduleRoot(), "internal/lint/testdata/badjsonv1")},
		aggregate: func(out *vetOutcome) []string {
			files, exception := analyzedGoFiles(out)
			return checkUnanalyzedGoFiles(filepath.Join(moduleRoot(), "internal/lint/testdata/badjsonv1"), files, exception)
		},
		wantAggregate: func(string) []string {
			return []string{
				"jsonv1: api/apigen no longer imports encoding/json; remove jsonV1Exception",
				"jsonv1: ignored/ignored.go imports encoding/json; use encoding/json/v2 and encoding/json/jsontext",
			}
		},
	},
	{
		name: "stablecopies", analyzer: "stablecopies", patterns: []string{fixtureRoot + "badstablecopies"},
		flags:      []string{"-stablecopies.anchor=" + fixtureImportRoot + "badstablecopies"},
		targets:    []string{fixtureImportRoot + "badstablecopies"},
		inputsRoot: "internal/lint/testdata/badstablecopies",
	},
	{
		// The fixture tree holds an ignore-tagged Go file under internal/store,
		// which vet cannot see: the driver's unanalyzed-file check finds it.
		name: "appendonly", analyzer: "appendonly", patterns: []string{fixtureRoot + "badappendonly"},
		flags:      []string{"-appendonly.anchor=" + fixtureImportRoot + "badappendonly"},
		targets:    []string{fixtureImportRoot + "badappendonly"},
		inputsRoot: "internal/lint/testdata/badappendonly",
		aggregate: func(out *vetOutcome) []string {
			files, _ := analyzedGoFiles(out)
			return unanalyzedSyncCommit(filepath.Join(moduleRoot(), "internal/lint/testdata/badappendonly"), func(path string) bool { return files[canonicalPath(path)] })
		},
		wantAggregate: func(string) []string {
			return []string{"appendonly: internal/store/ignored.go issues SET synchronous_commit — durability is a boot-verified server setting, never a session downgrade (audit-model ADR CI invariant 7)"}
		},
	},
	{
		// RawSQL: a want comment per finding with a source line; the rest is
		// aggregated by the driver (wantAggregate).
		name: "rawsql", analyzer: "rawsql", patterns: []string{fixtureRoot + "badrawsql/..."},
		flags:      []string{"-rawsql.prefix=" + fixtureImportRoot + "badrawsql"},
		inputsRoot: "internal/lint/testdata/badrawsql",
		aggregate: func(out *vetOutcome) []string {
			findings, err := rawSQLAggregate(out, rawSQLFixtureRoot())
			if err != nil {
				panic(err)
			}
			return findings
		},
		wantAggregate: func(context string) []string {
			want := []string{
				"rawsql: owner/owner.go:DroppedDependencies helper dependency helper/helper.go:Gone is absent from analyzed production source",
				"rawsql: owner/owner.go:DroppedDependencies no longer references helper dependency helper/helper.go:Spare; retire its dependency pin",
				"rawsql: protocol owner owner/gone.go:Vanished is absent from analyzed production source",
				"rawsql: protocol owner/owner.go:Restricted no longer has reviewed caller owner/owner.go:OldCaller; retire its caller pin",
			}
			if context == "windows" {
				want = append(want, "rawsql: protocol owner owner/gone.go:WindowsOnly is absent from analyzed production source")
			}
			return want
		},
	},
	{
		name: "upgradebuild", analyzer: "upgradebuild", patterns: []string{fixtureRoot + "badupgradebuild"},
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

// inFixture reports whether file (testdata-relative) belongs to one of the
// fixture's own packages. Fixtures that share one run each see the others'
// packages, which other analyzers may flag.
func inFixture(f vetFixture, file string) bool {
	for _, dir := range fixtureDirs(f.patterns) {
		if strings.HasPrefix(file, fixtureKey(dir)+"/") {
			return true
		}
	}
	return false
}

// checkWants requires every want to match one diagnostic of the fixture's
// analyzer on its line, and every such diagnostic to match a want. Limit: the
// inFixture relaxation skips diagnostics in the other packages of a shared run,
// so a sibling fixture's package is judged only by that fixture's own entry;
// no entry checks the packages that no fixture lists.
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
		if !inFixture(f, key.file) {
			continue // a sibling fixture's package, judged by its own entry
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
					for _, finding := range targetFindings(f.analyzer, f.targets, out.Targets[f.analyzer]) {
						t.Error(finding)
					}
					if f.aggregate != nil {
						var want []string
						if f.wantAggregate != nil {
							want = f.wantAggregate(ctx.Name)
						}
						got, want := slices.Sorted(slices.Values(f.aggregate(out))), slices.Sorted(slices.Values(want))
						if !slices.Equal(got, want) {
							t.Errorf("aggregate findings = %q, want %q", got, want)
						}
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
			missing := targetFindings("testobservers", []string{observerOwner, observerOwner + "/missing"}, out.Targets["testobservers"])
			if len(missing) != 1 || !strings.Contains(missing[0], observerOwner+"/missing") {
				t.Errorf("target aggregation = %v, want exactly the absent owner package", missing)
			}
		})
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

// The surface analyzers are silent when the fixture packages are not their
// named surface: the same lockless and second-writer fixtures pass once the
// surface points at the real authn store.
func TestSurfaceAnalyzersStaySilentOutsideTheirSurface(t *testing.T) {
	bin := buildVetTool(t)
	authn := Module + "/internal/store/authn"
	flags := []string{"-grantlock", "-grantlock.surface=" + authn, "-denialwriter", "-denialwriter.surface=" + authn, "-denialwriter.writers=WriteDenial"}
	flags = append(flags, inputFlags(t, "grantlock", moduleRoot())...)
	flags = append(flags, inputFlags(t, "denialwriter", moduleRoot())...)
	out := runVet(t, bin, Contexts[0], flags, []string{fixtureRoot + "badgrant", fixtureRoot + "badauthn"})
	for _, name := range []string{"grantlock", "denialwriter"} {
		if got := findingMessages(out.Findings, name); len(got) != 0 {
			t.Errorf("%s fired outside its named surface: %v", name, got)
		}
	}
}
