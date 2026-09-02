package api_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/Hikyo-Org/hikyo/api"
	"gopkg.in/yaml.v3"
)

// The WebUI parity registry (#490).
//
// `parity.yaml` beside the contract gives every operation exactly one
// disposition: a browser surface, one member of the closed exception set, or
// an open implementation issue. These tests are what make the file executable
// rather than a table someone once filled in:
//
//   - the registry is keyed by the contract's operationIds, both directions, so
//     a new operation fails CI until it is dispositioned and a deleted one
//     cannot leave a stale row behind;
//   - a `webui` disposition is checked against the SPA's own source (the
//     generated `<id>Op` import, or a literal `/api/v1` path for the few plain
//     fetches) and against the router's surface list, so "we have a page for
//     that" is a claim the build verifies;
//   - the symmetry holds too: an operation the SPA reaches cannot sit under an
//     exception or an issue, so landing a surface forces the registry to say so;
//   - exception classes are a closed enum admitted by a predicate over the
//     contract's own metadata, never by the string alone.
//
// Issue liveness (a referenced issue must still be open) needs the network and
// lives in scripts/ci/check-parity-issues.sh.

const parityPath = "parity.yaml"

// Paths are relative to this package directory, which is where `go test` runs.
const (
	spaSourceRoot       = "../web/src"
	spaNavigationSource = "../web/src/app/navigation.ts"
	generatedOperations = "../clients/ts/src/generated/operations.gen.ts"
)

// The closed exception set. Membership is decided by the predicate, and the
// registry may only name a class listed here.
//
// Two of the four classes the parity programme names have no HTTP member by
// construction: host-local authority (init, migrate, restore reconcile,
// break-glass) acts on the server's own host and has no endpoint at all
// (api-cli-surface ADR, closed local-authority exception set), and the
// Kubernetes operator reconciles through the machine delivery wire, which is
// already the client-local class. They stay in the enum so the registry can
// say so in one place, and a row claiming either is a spec review event.
var parityExceptionClasses = map[string]struct {
	admits func(op api.Operation) bool
	rule   string
}{
	"identity-protocol": {
		admits: func(op api.Operation) bool {
			return strings.HasPrefix(op.Path, "/api/v1/auth/") || strings.Contains(op.Path, "/scim/v2/")
		},
		rule: "protocol-shaped paths under /api/v1/auth/ or the SCIM wire under /scim/v2/",
	},
	"client-local-delivery": {
		admits: func(op api.Operation) bool {
			return op.AdmitsArtifact(api.ArtifactMachineCredential) && !op.AdmitsArtifact(api.ArtifactHumanSession)
		},
		rule: "machine-credential wire a human session cannot present",
	},
	"host-local-authority": {
		admits: func(api.Operation) bool { return false },
		rule:   "no HTTP endpoint by ADR; a member here is a spec review event",
	},
	"k8s-controller-reconciliation": {
		admits: func(api.Operation) bool { return false },
		rule:   "the operator speaks only the machine delivery wire; a member here is a spec review event",
	},
}

type parityEntry struct {
	WebUI     string   `yaml:"webui"`
	Reach     string   `yaml:"reach"`
	Via       []string `yaml:"via"`
	Exception string   `yaml:"exception"`
	Issue     int      `yaml:"issue"`
	Note      string   `yaml:"note"`
}

type parityRegistry struct {
	Operations map[string]parityEntry `yaml:"operations"`
}

func (e parityEntry) kind() string {
	switch {
	case e.WebUI != "":
		return "webui"
	case e.Exception != "":
		return "exception"
	case e.Issue != 0:
		return "issue"
	}
	return ""
}

// direct reports whether the row claims the SPA calls this operation itself,
// as opposed to delivering the same outcome through other operations.
func (e parityEntry) direct() bool {
	return e.WebUI != "" && len(e.Via) == 0
}

func loadParity(t *testing.T) parityRegistry {
	t.Helper()
	raw, err := os.ReadFile(parityPath)
	if err != nil {
		t.Fatalf("read %s: %v", parityPath, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var reg parityRegistry
	if err := dec.Decode(&reg); err != nil {
		t.Fatalf("decode %s: %v", parityPath, err)
	}
	if len(reg.Operations) == 0 {
		t.Fatalf("%s declares no operations", parityPath)
	}
	return reg
}

func loadOperations(t *testing.T) map[string]api.Operation {
	t.Helper()
	ops, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// generatedOperationName spells the `@hikyo/operations` export for an
// operationId the way the generator does: words split at case boundaries,
// acronyms folded to title case (`rotateDEK` becomes `rotateDekOp`,
// `startCLIReauth` becomes `startCliReauthOp`). The rule is pinned against the
// committed generated module below, so a generator change fails here rather
// than silently emptying the evidence.
func generatedOperationName(id string) string {
	runes := []rune(id)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		boundary := false
		if unicode.IsUpper(cur) {
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				boundary = true
			} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				boundary = true
			}
		}
		if boundary {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	words = append(words, string(runes[start:]))
	var b strings.Builder
	for i, w := range words {
		lower := strings.ToLower(w)
		if i == 0 {
			b.WriteString(lower)
			continue
		}
		b.WriteString(strings.ToUpper(lower[:1]))
		b.WriteString(lower[1:])
	}
	b.WriteString("Op")
	return b.String()
}

var identifierPattern = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

// spaSource is the browser application's runtime source: identifiers and
// `/api/v1` path literals from every non-test module under web/src. Tests and
// the test kit are excluded so a mock cannot count as browser reach.
type spaSource struct {
	identifiers map[string]bool
	paths       map[string]bool
}

var (
	apiPathLiteral   = regexp.MustCompile("/api/v1/[^'\"`\\s]*")
	templateSegment  = regexp.MustCompile(`\$\{[^}]*\}`)
	specPathTemplate = regexp.MustCompile(`\{[^}]*\}`)
)

func normalisePath(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = templateSegment.ReplaceAllString(p, "{}")
	return specPathTemplate.ReplaceAllString(p, "{}")
}

func isSPARuntimeSource(path string, d fs.DirEntry) bool {
	if d.IsDir() {
		return false
	}
	name := d.Name()
	if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx") {
		return false
	}
	if strings.Contains(name, ".test.") || strings.Contains(name, ".test-d.") {
		return false
	}
	return true
}

func loadSPASource(t *testing.T) spaSource {
	t.Helper()
	src := spaSource{identifiers: map[string]bool{}, paths: map[string]bool{}}
	files := 0
	err := filepath.WalkDir(spaSourceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testkit" {
			return filepath.SkipDir
		}
		if !isSPARuntimeSource(path, d) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for _, id := range identifierPattern.FindAll(raw, -1) {
			src.identifiers[string(id)] = true
		}
		for _, p := range apiPathLiteral.FindAll(raw, -1) {
			src.paths[normalisePath(string(p))] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", spaSourceRoot, err)
	}
	if files == 0 {
		t.Fatalf("no SPA source under %s", spaSourceRoot)
	}
	return src
}

var surfaceID = regexp.MustCompile(`id: '([^']+)'`)

// loadSurfaces reads the router's closed surface list. It is the same list the
// flow registry closes over, so a surface named here is one the build serves.
func loadSurfaces(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(spaNavigationSource)
	if err != nil {
		t.Fatalf("read %s: %v", spaNavigationSource, err)
	}
	const open = "export const SURFACES = defineSurfaceRegistry(["
	start := strings.Index(string(raw), open)
	if start < 0 {
		t.Fatalf("%s: SURFACES block not found", spaNavigationSource)
	}
	body := string(raw)[start+len(open):]
	end := strings.Index(body, "\n]);")
	if end < 0 {
		t.Fatalf("%s: SURFACES block is not closed", spaNavigationSource)
	}
	surfaces := map[string]bool{}
	for _, m := range surfaceID.FindAllStringSubmatch(body[:end], -1) {
		surfaces[m[1]] = true
	}
	if len(surfaces) == 0 {
		t.Fatalf("%s: no surfaces parsed", spaNavigationSource)
	}
	return surfaces
}

func loadGeneratedExports(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(generatedOperations)
	if err != nil {
		t.Fatalf("read %s: %v", generatedOperations, err)
	}
	exports := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^export const ([A-Za-z0-9]+Op)\b`).FindAllStringSubmatch(string(raw), -1) {
		exports[m[1]] = true
	}
	if len(exports) == 0 {
		t.Fatalf("%s: no operation exports parsed", generatedOperations)
	}
	return exports
}

func TestParityRegistryCoversEveryOperationExactlyOnce(t *testing.T) {
	reg := loadParity(t)
	ops := loadOperations(t)

	for _, id := range sortedKeys(ops) {
		if _, ok := reg.Operations[id]; !ok {
			t.Errorf("%s %s (%s) has no disposition in %s: add a webui surface, a closed exception, or an open issue", ops[id].Method, ops[id].Path, id, parityPath)
		}
	}
	for _, id := range sortedKeys(reg.Operations) {
		if _, ok := ops[id]; !ok {
			t.Errorf("%s names %q, which is not an operationId in the contract", parityPath, id)
		}
	}
}

func TestParityDispositionsAreWellFormed(t *testing.T) {
	reg := loadParity(t)

	for _, id := range sortedKeys(reg.Operations) {
		e := reg.Operations[id]
		set := 0
		for _, present := range []bool{e.WebUI != "", e.Exception != "", e.Issue != 0} {
			if present {
				set++
			}
		}
		if set != 1 {
			t.Errorf("%s: exactly one of webui, exception, issue must be set (got %d)", id, set)
			continue
		}
		if e.WebUI == "" && (e.Reach != "" || len(e.Via) != 0) {
			t.Errorf("%s: reach and via belong to a webui disposition only", id)
		}
		if e.Reach != "" && e.Reach != "path" {
			t.Errorf("%s: reach %q is not a known evidence kind (only \"path\")", id, e.Reach)
		}
		if e.Reach != "" && len(e.Via) != 0 {
			t.Errorf("%s: a via row delivers the outcome through other operations and carries no reach of its own", id)
		}
		if e.Issue < 0 {
			t.Errorf("%s: issue %d is not an issue number", id, e.Issue)
		}
		for _, target := range e.Via {
			other, ok := reg.Operations[target]
			switch {
			case !ok:
				t.Errorf("%s: via names %q, which is not in the registry", id, target)
			case target == id:
				t.Errorf("%s: via names itself", id)
			case !other.direct():
				t.Errorf("%s: via target %q must itself be a direct webui row, not %s", id, target, describe(other))
			}
		}
	}
}

func describe(e parityEntry) string {
	switch e.kind() {
	case "webui":
		if len(e.Via) != 0 {
			return fmt.Sprintf("webui %s via %v", e.WebUI, e.Via)
		}
		return "webui " + e.WebUI
	case "exception":
		return "exception " + e.Exception
	case "issue":
		return fmt.Sprintf("issue #%d", e.Issue)
	}
	return "an empty row"
}

func TestParityWebUIRowsNameServedSurfaces(t *testing.T) {
	reg := loadParity(t)
	surfaces := loadSurfaces(t)

	for _, id := range sortedKeys(reg.Operations) {
		e := reg.Operations[id]
		if e.WebUI == "" {
			continue
		}
		if !surfaces[e.WebUI] {
			t.Errorf("%s: surface %q is not in the router's SURFACES list (%s)", id, e.WebUI, spaNavigationSource)
		}
	}
}

func TestParityGeneratedNameRuleMatchesTheClient(t *testing.T) {
	ops := loadOperations(t)
	exports := loadGeneratedExports(t)

	// The generator skips a handful of protocol-only operations. Every other
	// operation must resolve to an export under this test's spelling rule, or
	// the evidence scan would be looking for names that do not exist.
	missing := 0
	for _, id := range sortedKeys(ops) {
		if !exports[generatedOperationName(id)] {
			missing++
			if !parityExceptionClasses["identity-protocol"].admits(ops[id]) {
				t.Errorf("%s: computed export %q is not in %s; the spelling rule or the generator changed", id, generatedOperationName(id), generatedOperations)
			}
		}
	}
	if missing > len(ops)/10 {
		t.Errorf("%d of %d operations have no generated export; the spelling rule is broken", missing, len(ops))
	}
}

func TestParityWebUIRowsAreReachedByTheSPA(t *testing.T) {
	reg := loadParity(t)
	ops := loadOperations(t)
	src := loadSPASource(t)

	for _, id := range sortedKeys(reg.Operations) {
		e := reg.Operations[id]
		op, ok := ops[id]
		if !ok || !e.direct() {
			continue
		}
		switch e.Reach {
		case "":
			if !src.identifiers[generatedOperationName(id)] {
				t.Errorf("%s: claims surface %q but no runtime module under %s imports %s", id, e.WebUI, spaSourceRoot, generatedOperationName(id))
			}
		case "path":
			if !src.paths[normalisePath(op.Path)] {
				t.Errorf("%s: claims surface %q by path, but no runtime module under %s carries a %s literal", id, e.WebUI, spaSourceRoot, op.Path)
			}
		}
	}
}

func TestParityRowsTheSPAReachesAreWebUIRows(t *testing.T) {
	reg := loadParity(t)
	ops := loadOperations(t)
	src := loadSPASource(t)

	for _, id := range sortedKeys(ops) {
		e, ok := reg.Operations[id]
		if !ok {
			continue
		}
		if src.identifiers[generatedOperationName(id)] && !e.direct() {
			t.Errorf("%s: the SPA imports %s, so the row must be a direct webui surface, not %s", id, generatedOperationName(id), describe(e))
		}
	}
}

func TestParityExceptionsAreClosedAndAdmitted(t *testing.T) {
	reg := loadParity(t)
	ops := loadOperations(t)

	for _, id := range sortedKeys(reg.Operations) {
		e := reg.Operations[id]
		if e.Exception == "" {
			continue
		}
		class, ok := parityExceptionClasses[e.Exception]
		if !ok {
			t.Errorf("%s: exception %q is not one of the closed classes %v", id, e.Exception, sortedKeys(parityExceptionClasses))
			continue
		}
		op, ok := ops[id]
		if !ok {
			continue
		}
		if !class.admits(op) {
			t.Errorf("%s (%s %s): does not qualify for %q: %s", id, op.Method, op.Path, e.Exception, class.rule)
		}
	}
}

func TestParityExceptionClassPredicatesAreDisjointOnTheContract(t *testing.T) {
	// A row must have one honest home. If two admissible classes ever overlap
	// on a real operation the registry author gets to choose, which is exactly
	// the discretion the closed set is meant to remove.
	ops := loadOperations(t)
	classes := sortedKeys(parityExceptionClasses)
	for _, id := range sortedKeys(ops) {
		var admitted []string
		for _, class := range classes {
			if parityExceptionClasses[class].admits(ops[id]) {
				admitted = append(admitted, class)
			}
		}
		if len(admitted) > 1 {
			t.Errorf("%s is admissible to more than one exception class: %v", id, admitted)
		}
	}
}

func TestParityGeneratedNameRuleSpellsAcronyms(t *testing.T) {
	cases := map[string]string{
		"whoami":                    "whoamiOp",
		"rotateDEK":                 "rotateDekOp",
		"startCLIReauth":            "startCliReauthOp",
		"samlACS":                   "samlAcsOp",
		"listScimDirectoryUsers":    "listScimDirectoryUsersOp",
		"scimServiceProviderConfig": "scimServiceProviderConfigOp",
	}
	for in, want := range cases {
		if got := generatedOperationName(in); got != want {
			t.Errorf("generatedOperationName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := normalisePath("/api/v1/orgs/${encodeURIComponent(org)}/audit/export?x=1"); got != "/api/v1/orgs/{}/audit/export" {
		t.Errorf("normalisePath does not fold template segments: %q", got)
	}
	if normalisePath("/api/v1/orgs/{org}/audit/export") != "/api/v1/orgs/{}/audit/export" {
		t.Errorf("normalisePath does not fold contract parameters")
	}
}
