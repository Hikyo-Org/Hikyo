// Command qualification prepares an inventory. It never grants acceptance,
// dispatches workflows, or modifies release state.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type requirement struct {
	ID        string          `json:"id"`
	Authority string          `json:"authority"`
	Sources   []string        `json:"sources"`
	Gates     []string        `json:"gates"`
	Evidence  string          `json:"required_evidence"`
	Status    string          `json:"status"`
	Tests     []testReference `json:"tests"`
}

type gate struct {
	ID       string   `json:"id"`
	Workflow string   `json:"workflow,omitempty"`
	Jobs     []string `json:"jobs,omitempty"`
	Commands []string `json:"commands"`
	Evidence string   `json:"required_evidence"`
	Status   string   `json:"status"`
}

type specification struct {
	Requirements []requirement `json:"requirements"`
	Gates        []gate        `json:"gates"`
	Holds        []string      `json:"dependency_holds"`
	Exclusions   []string      `json:"exclusions"`
}

type testReference struct {
	Name                 string   `json:"name"`
	Path                 string   `json:"path"`
	Package              string   `json:"package"`
	ExecutionJobs        []string `json:"execution_jobs,omitempty"`
	IsolationRaceJob     string   `json:"isolation_race_job,omitempty"`
	BuildConstraint      string   `json:"build_constraint,omitempty"`
	DefaultRaceExclusion string   `json:"default_race_exclusion,omitempty"`
}

type criterion struct {
	ID          string          `json:"id"`
	Authority   string          `json:"authority"`
	Tests       []testReference `json:"tests"`
	LedgerLanes string          `json:"ledger_lanes"`
	Status      string          `json:"status"`
}

type job struct {
	Name     string    `yaml:"name" json:"name,omitempty"`
	Uses     string    `yaml:"uses" json:"uses,omitempty"`
	If       string    `yaml:"if" json:"if,omitempty"`
	Needs    yaml.Node `yaml:"needs" json:"-"`
	RunsOn   yaml.Node `yaml:"runs-on" json:"-"`
	Strategy struct {
		Matrix yaml.Node `yaml:"matrix"`
	} `yaml:"strategy" json:"-"`
	Steps []struct {
		Name string `yaml:"name"`
		Uses string `yaml:"uses"`
		Run  string `yaml:"run"`
		If   string `yaml:"if"`
		With struct {
			Name          string `yaml:"name"`
			Path          string `yaml:"path"`
			RetentionDays string `yaml:"retention-days"`
			IfNoFiles     string `yaml:"if-no-files-found"`
		} `yaml:"with"`
	} `yaml:"steps" json:"-"`
	NeedsYAML  string     `json:"needs_yaml,omitempty"`
	RunnerYAML string     `json:"runner_yaml,omitempty"`
	MatrixYAML string     `json:"matrix_yaml,omitempty"`
	Artifacts  []artifact `json:"artifact_requirements"`
}

type artifact struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	RetentionDays string `json:"retention_days,omitempty"`
	UploadIf      string `json:"upload_if,omitempty"`
	IfNoFiles     string `json:"if_no_files_found,omitempty"`
}

type migration struct {
	Head  string            `json:"highest_filename"`
	Files map[string]string `json:"file_sha256"`
}

type inventory struct {
	Schema        string                     `json:"schema"`
	Phase         string                     `json:"phase"`
	Verdict       string                     `json:"release_verdict"`
	Commit        string                     `json:"commit"`
	Tree          string                     `json:"committed_tree"`
	Dirty         bool                       `json:"working_tree_dirty"`
	APIRevision   int                        `json:"source_api_revision"`
	FullDomains   []string                   `json:"full_domains"`
	Migrations    map[string]migration       `json:"source_migrations_not_runtime"`
	FreezeRef     string                     `json:"local_freeze_ref"`
	FreezeCommit  string                     `json:"local_freeze_commit"`
	FreezeMode    string                     `json:"freeze_mode"`
	SkewMode      string                     `json:"skew_mode"`
	Inputs        map[string]string          `json:"input_sha256"`
	Criteria      []criterion                `json:"criteria"`
	Specification specification              `json:"qualification"`
	Workflows     map[string]map[string]job  `json:"workflow_graphs"`
	Plans         map[string]json.RawMessage `json:"shard_plans,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "qualification preparation: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("qualification", flag.ContinueOnError)
	root := flags.String("root", ".", "repository root")
	plans := flags.Bool("plans", false, "also run the existing complete shard planners (use the shared heavy queue)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	i, err := prepare(*root)
	if err != nil {
		return err
	}
	if *plans {
		i.Plans, err = planShards(*root, i.Workflows)
		if err != nil {
			return err
		}
		if err := bindPlans(i); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(i)
}

func git(root string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = root
	raw, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func prepare(root string) (*inventory, error) {
	i := &inventory{Schema: "hikyo.dev/qualification-preparation/v1", Phase: "PREPARATION", Verdict: "HOLD", Inputs: map[string]string{}, Workflows: map[string]map[string]job{}, Migrations: map[string]migration{}}
	read := func(path string) ([]byte, error) {
		if !filepath.IsLocal(path) {
			return nil, fmt.Errorf("nonportable input: %q", path)
		}
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err == nil {
			i.Inputs[path] = digest(raw)
		}
		return raw, err
	}
	var err error
	if i.Commit, err = git(root, "rev-parse", "HEAD"); err != nil {
		return nil, err
	}
	if i.Tree, err = git(root, "rev-parse", "HEAD^{tree}"); err != nil {
		return nil, err
	}
	status, err := git(root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}
	i.Dirty = status != ""
	for _, path := range []string{"scripts/ci/qualification/main.go", "scripts/ci/qualification/plans.go", "scripts/ci/analysis-shards-go/main.go", "scripts/ci/test-isolation-race.sh", "scripts/ci/check-api-freeze.sh", "scripts/ci/check-client-skew.sh", "scripts/release/require-green-main.sh", "scripts/ci/assert-doctor-findings.jq"} {
		if _, err := read(path); err != nil {
			return nil, err
		}
	}
	i.FreezeRef, err = git(root, "for-each-ref", "--format=%(refname)", "refs/tags/v1.0.0")
	if err != nil {
		return nil, err
	}
	i.FreezeMode, i.SkewMode = "DORMANT", "pre-freeze-rehearsal"
	if i.FreezeRef != "" {
		if i.FreezeRef != "refs/tags/v1.0.0" {
			return nil, fmt.Errorf("ambiguous local freeze ref: %s", i.FreezeRef)
		}
		i.FreezeCommit, err = git(root, "rev-parse", "--verify", "refs/tags/v1.0.0^{commit}")
		if err != nil {
			return nil, err
		}
		i.FreezeMode, i.SkewMode = "tag-present-NOTRUN", "frozen-v1.0.0-NOTRUN"
	}
	raw, err := read("scripts/ci/qualification/requirements.json")
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&i.Specification); err != nil {
		return nil, err
	}
	workflowPaths := []string{".github/workflows/ci-main.yml", ".github/workflows/ci.yml", ".github/workflows/reliability.yml", ".github/workflows/release-acceptance.yml", ".github/workflows/ops-floor.yml", ".github/workflows/floor-bench.yml"}
	for index := 0; index < len(workflowPaths); index++ {
		path := workflowPaths[index]
		if _, loaded := i.Workflows[path]; loaded {
			continue
		}
		raw, err := read(path)
		if err != nil {
			return nil, err
		}
		var workflow struct {
			Jobs map[string]job `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(workflow.Jobs) == 0 {
			return nil, fmt.Errorf("empty workflow: %s", path)
		}
		for id, j := range workflow.Jobs {
			if strings.HasPrefix(j.Uses, "./.github/workflows/") {
				workflowPaths = append(workflowPaths, strings.TrimPrefix(j.Uses, "./"))
			}
			j.NeedsYAML = nodeYAML(j.Needs)
			j.RunnerYAML = nodeYAML(j.RunsOn)
			j.MatrixYAML = nodeYAML(j.Strategy.Matrix)
			j.Artifacts = []artifact{}
			for _, s := range j.Steps {
				if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
					j.Artifacts = append(j.Artifacts, artifact{Name: s.With.Name, Path: s.With.Path, RetentionDays: s.With.RetentionDays, UploadIf: s.If, IfNoFiles: s.With.IfNoFiles})
				}
			}
			workflow.Jobs[id] = j
		}
		i.Workflows[path] = workflow.Jobs
	}
	raw, err = read("scripts/ci/ci-job-registry.json")
	if err != nil {
		return nil, err
	}
	var registry struct {
		Jobs        map[string]json.RawMessage `json:"jobs"`
		PathClasses struct {
			Full []string `json:"full"`
		} `json:"path_classes"`
	}
	if err := json.Unmarshal(raw, &registry); err != nil {
		return nil, err
	}
	i.FullDomains = registry.PathClasses.Full
	if len(i.FullDomains) == 0 {
		return nil, errors.New("full domain inventory missing")
	}
	graph := i.Workflows[".github/workflows/ci.yml"]
	if err := checkRegistry(registry.Jobs, graph); err != nil {
		return nil, err
	}
	gateIDs := map[string]bool{}
	for n := range i.Specification.Gates {
		g := &i.Specification.Gates[n]
		if g.ID == "" || gateIDs[g.ID] || len(g.Commands) == 0 || g.Evidence == "" {
			return nil, fmt.Errorf("invalid/duplicate gate: %s", g.ID)
		}
		gateIDs[g.ID], g.Status = true, "NOTRUN"
		for _, id := range g.Jobs {
			if _, ok := i.Workflows[g.Workflow][id]; !ok {
				return nil, fmt.Errorf("gate %s names missing workflow job %s", g.ID, id)
			}
		}
	}
	requirementIDs := map[string]bool{}
	for n := range i.Specification.Requirements {
		r := &i.Specification.Requirements[n]
		if r.ID == "" || requirementIDs[r.ID] || len(r.Gates) == 0 || r.Evidence == "" {
			return nil, fmt.Errorf("invalid/duplicate requirement: %s", r.ID)
		}
		requirementIDs[r.ID] = true
		r.Status = "NOTRUN"
		for _, id := range r.Gates {
			if !gateIDs[id] {
				return nil, fmt.Errorf("requirement %s names missing gate %s", r.ID, id)
			}
		}
		for _, path := range append([]string{r.Authority}, r.Sources...) {
			raw, err := read(path)
			if err != nil {
				return nil, err
			}
			if strings.HasSuffix(path, "_test.go") {
				file, err := parser.ParseFile(token.NewFileSet(), path, raw, 0)
				if err != nil {
					return nil, err
				}
				for _, declaration := range file.Decls {
					if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
						r.Tests = append(r.Tests, reference(fn.Name.Name, path, raw))
					}
				}
			}
		}
	}
	i.Criteria, err = criteria(read)
	if err != nil {
		return nil, err
	}
	raw, err = read("api/spec.go")
	if err != nil {
		return nil, err
	}
	m := regexp.MustCompile(`(?m)^const Revision = ([0-9]+)$`).FindSubmatch(raw)
	if m == nil {
		return nil, errors.New("source API revision not found")
	}
	i.APIRevision, err = strconv.Atoi(string(m[1]))
	if err != nil {
		return nil, err
	}
	for _, engine := range []string{"sqlite", "postgres"} {
		paths, err := filepath.Glob(filepath.Join(root, "internal/store/migrations", engine, "*.sql"))
		if err != nil || len(paths) == 0 {
			return nil, fmt.Errorf("missing %s migration inventory", engine)
		}
		sort.Strings(paths)
		m := migration{Head: filepath.Base(paths[len(paths)-1]), Files: map[string]string{}}
		for _, path := range paths {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil, err
			}
			raw, err := read(filepath.ToSlash(rel))
			if err != nil {
				return nil, err
			}
			m.Files[filepath.Base(path)] = digest(raw)
		}
		i.Migrations[engine] = m
	}
	return i, nil
}

func nodeYAML(n yaml.Node) string {
	if n.Kind == 0 {
		return ""
	}
	raw, _ := yaml.Marshal(&n) // Parsed nodes contain only YAML-supported values.
	return strings.TrimSpace(string(raw))
}

func checkRegistry(registry map[string]json.RawMessage, graph map[string]job) error {
	if len(graph) == 0 || len(graph) != len(registry) {
		return errors.New("full workflow/registry job inventory differs")
	}
	for id := range registry {
		if _, ok := graph[id]; !ok {
			return fmt.Errorf("registered job missing: %s", id)
		}
	}
	return nil
}

var criterionRow = regexp.MustCompile(`^\| (C-[A-Z]+|[CAKSMO][0-9]+|S[ACS][0-9]+|GH-(?:E2E|STUB|CONTRACT)) \|`)
var testLink = regexp.MustCompile(`\[(Test[A-Za-z0-9_]+)\]\(../../([^()]+_test\.go)\)`)

func criteria(read func(string) ([]byte, error)) ([]criterion, error) {
	expected := map[string]string{"GH-E2E": "docs/adr/github-adapter.md", "GH-STUB": "docs/adr/github-adapter.md", "GH-CONTRACT": "docs/adr/github-adapter.md"}
	for _, owner := range []string{"mvp-boundary", "saml-sp", "scim-provisioning", "secret-scanning"} {
		path := "docs/adr/" + owner + ".md"
		raw, err := read(path)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if m := criterionRow.FindStringSubmatch(line); m != nil {
				expected[m[1]] = path
			}
		}
	}
	raw, err := read("docs/release/acceptance-1.0.md")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	result := []criterion{}
	for _, line := range strings.Split(string(raw), "\n") {
		m := criterionRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := m[1]
		if seen[id] || expected[id] == "" {
			return nil, fmt.Errorf("duplicate/unowned ledger criterion %s", id)
		}
		seen[id] = true
		cells := strings.Split(line, " | ")
		if len(cells) < 6 {
			return nil, fmt.Errorf("malformed criterion %s", id)
		}
		c := criterion{ID: id, Authority: expected[id], Tests: []testReference{}, LedgerLanes: cells[3], Status: "NOTRUN"}
		for _, ref := range testLink.FindAllStringSubmatch(cells[2], -1) {
			raw, err := read(ref[2])
			if err != nil {
				return nil, err
			}
			file, err := parser.ParseFile(token.NewFileSet(), ref[2], raw, 0)
			if err != nil {
				return nil, err
			}
			found := false
			for _, declaration := range file.Decls {
				if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == ref[1] {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("%s names missing test %s in %s", id, ref[1], ref[2])
			}
			c.Tests = append(c.Tests, reference(ref[1], ref[2], raw))
		}
		result = append(result, c)
	}
	for id := range expected {
		if !seen[id] {
			return nil, fmt.Errorf("missing owning criterion %s", id)
		}
	}
	return result, nil
}

func reference(name, path string, raw []byte) testReference {
	r := testReference{Name: name, Path: path, Package: "./" + filepath.ToSlash(filepath.Dir(path))}
	if m := regexp.MustCompile(`(?m)^//go:build (.+)$`).FindSubmatch(raw); m != nil {
		r.BuildConstraint = string(m[1])
	}
	return r
}
