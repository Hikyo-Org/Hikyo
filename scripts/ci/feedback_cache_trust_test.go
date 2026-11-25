package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The feedback compiler cache has two trust families. Entries seeded from main are read only
// by feedback runs on PRs that do not target dev; entries seeded from dev are read only by
// runs on PRs that target dev. These tests evaluate the real key templates of the seed and the
// reader in each context and replay GitHub's restore order (exact key, then each restore key
// as a prefix, newest first), so a prefix that spans families fails, not just a changed string.

const (
	backendSeedWorkflow = ".github/workflows/ci-feedback-backend-cache.yml"
	feedbackWorkflow    = ".github/workflows/ci-feedback.yml"
	seedLookupName      = "Look up this seed without restoring old entries"
)

type seededEntry struct {
	key    string
	family string // the ref that published it: ground truth, not parsed from the key
	scope  string
}

var feedbackCacheTernary = regexp.MustCompile(`^(\S+) == '([^']*)' && ('[^']*'|\S+) \|\| ('[^']*'|\S+)$`)

// evalFeedbackExpr models only the expression forms the cache keys use. Anything else is an
// error, so a key built from an unreviewed (for example PR-author controlled) value cannot pass.
func evalFeedbackExpr(expr string, vars map[string]string) (string, error) {
	expr = strings.TrimSpace(expr)
	operand := func(v string) (string, error) {
		if strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") && len(v) >= 2 {
			return v[1 : len(v)-1], nil
		}
		got, ok := vars[v]
		if !ok {
			return "", fmt.Errorf("unmodelled cache key expression %q", v)
		}
		return got, nil
	}
	if strings.HasPrefix(expr, "hashFiles(") {
		return fmt.Sprintf("hash%x", len(expr)), nil
	}
	if m := feedbackCacheTernary.FindStringSubmatch(expr); m != nil {
		lhs, ok := vars[m[1]]
		if !ok {
			return "", fmt.Errorf("unmodelled cache key expression %q", m[1])
		}
		if lhs == m[2] {
			return operand(m[3])
		}
		return operand(m[4])
	}
	return operand(expr)
}

var feedbackExprPattern = regexp.MustCompile(`\$\{\{\s*(.*?)\s*\}\}`)

func evalFeedbackTemplate(template string, vars map[string]string) (string, error) {
	var failure error
	out := feedbackExprPattern.ReplaceAllStringFunc(template, func(match string) string {
		got, err := evalFeedbackExpr(feedbackExprPattern.FindStringSubmatch(match)[1], vars)
		if err != nil {
			failure = err
		}
		return got
	})
	return out, failure
}

func feedbackStepWith(steps []any, name string) map[string]any {
	with, _ := seedStepByName(steps, name)["with"].(map[string]any)
	return with
}

func feedbackBaseVars() map[string]string {
	return map[string]string{
		"runner.os": "Linux", "runner.arch": "X64",
		"steps.runner-cache-abi.outputs.value": "ubuntu24",
	}
}

// publishedFeedbackEntries replays the seed's lookup key for every ref that may publish and every
// matrix entry.
func publishedFeedbackEntries(seedRaw []byte) ([]seededEntry, error) {
	var seed struct {
		Jobs struct {
			Seed struct {
				Strategy struct {
					Matrix struct {
						Include []struct {
							Entry string `yaml:"entry"`
						} `yaml:"include"`
					} `yaml:"matrix"`
				} `yaml:"strategy"`
				Steps []any `yaml:"steps"`
			} `yaml:"seed"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(seedRaw, &seed); err != nil {
		return nil, err
	}
	keyTemplate, _ := feedbackStepWith(seed.Jobs.Seed.Steps, seedLookupName)["key"].(string)
	if keyTemplate == "" || len(seed.Jobs.Seed.Strategy.Matrix.Include) == 0 {
		return nil, fmt.Errorf("seed lost its lookup key or entries")
	}
	var published []seededEntry
	for _, family := range []string{"main", "dev"} {
		for _, include := range seed.Jobs.Seed.Strategy.Matrix.Include {
			vars := feedbackBaseVars()
			vars["github.ref"] = "refs/heads/" + family
			vars["matrix.entry"] = include.Entry
			vars["github.sha"] = "sha-" + family
			vars["steps.seed-day.outputs.value"] = "2026-11-25"
			key, err := evalFeedbackTemplate(keyTemplate, vars)
			if err != nil {
				return nil, err
			}
			published = append(published, seededEntry{key: key, family: family, scope: include.Entry})
		}
	}
	return published, nil
}

// restoreFeedbackEntry mirrors actions/cache restore: the exact key, then each restore key as a
// prefix in order, taking the newest (last published) match.
func restoreFeedbackEntry(store []seededEntry, key, restoreKeys string) *seededEntry {
	for i := range store {
		if store[i].key == key {
			return &store[i]
		}
	}
	for _, prefix := range strings.Split(restoreKeys, "\n") {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		for i := len(store) - 1; i >= 0; i-- {
			if strings.HasPrefix(store[i].key, prefix) {
				return &store[i]
			}
		}
	}
	return nil
}

func requireFeedbackCacheTrust(seedRaw, readerRaw []byte) error {
	store, err := publishedFeedbackEntries(seedRaw)
	if err != nil {
		return err
	}
	var reader struct {
		Jobs struct {
			Feedback struct {
				Steps []any `yaml:"steps"`
			} `yaml:"feedback"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(readerRaw, &reader); err != nil {
		return err
	}
	laneReader := feedbackStepWith(reader.Jobs.Feedback.Steps, "Restore bounded feedback Go cache")
	cliStep := seedStepByName(reader.Jobs.Feedback.Steps, "Restore bounded feedback CLI Go cache")
	cliReader, _ := cliStep["with"].(map[string]any)
	if cliStep["if"] != feedbackCLIReaderCondition {
		return fmt.Errorf("the CLI entry is read by the application lane only")
	}
	type readerKeys struct{ key, restore string }
	templates := func(with map[string]any) (readerKeys, error) {
		key, _ := with["key"].(string)
		restore, _ := with["restore-keys"].(string)
		if key == "" || restore == "" {
			return readerKeys{}, fmt.Errorf("feedback restore lost its key or restore prefix")
		}
		return readerKeys{key, restore}, nil
	}
	laneKeys, err := templates(laneReader)
	if err != nil {
		return err
	}
	cliKeys, err := templates(cliReader)
	if err != nil {
		return err
	}
	// Every base branch a PR can name must land in exactly one family: only a PR whose base is
	// literally dev reads dev entries. Other names, including look-alikes, read main entries.
	bases := map[string]string{
		"dev": "dev", "main": "main", "release/1.0": "main", "hotfix/x": "main",
		"dev-evil": "main", "DEV": "main", "dev/x": "main", "refs/heads/dev": "main", "": "main",
	}
	for base, family := range bases {
		// The application lane reads its entry and the CLI entry; the tooling lane reads its own.
		for scope, entries := range map[string][]string{"application": {"application", "cli"}, "tooling": {"tooling"}} {
			vars := feedbackBaseVars()
			vars["github.event.pull_request.base.ref"] = base
			vars["fromJSON(steps.plan.outputs.plan).lanes[matrix.lane].goScope"] = scope
			vars["env.FAST_FEEDBACK_BASE"] = "base-sha"
			readers := []readerKeys{laneKeys}
			if scope == "application" {
				readers = append(readers, cliKeys)
			}
			for i, r := range readers {
				key, err := evalFeedbackTemplate(r.key, vars)
				if err != nil {
					return err
				}
				restoreKeys, err := evalFeedbackTemplate(r.restore, vars)
				if err != nil {
					return err
				}
				got := restoreFeedbackEntry(store, key, restoreKeys)
				if got == nil || got.family != family || got.scope != entries[i] {
					return fmt.Errorf("a PR targeting %q on the %s lane restores %+v, want the %s family's %s entry", base, scope, got, family, entries[i])
				}
			}
		}
		// A lane without a Go scope must never reach any entry through the family prefix.
		vars := feedbackBaseVars()
		vars["github.event.pull_request.base.ref"] = base
		vars["fromJSON(steps.plan.outputs.plan).lanes[matrix.lane].goScope"] = "all"
		vars["env.FAST_FEEDBACK_BASE"] = "base-sha"
		key, _ := evalFeedbackTemplate(laneKeys.key, vars)
		restoreKeys, _ := evalFeedbackTemplate(laneKeys.restore, vars)
		if got := restoreFeedbackEntry(store, key, restoreKeys); got != nil {
			return fmt.Errorf("a lane with no Go scope restored %+v", got)
		}
	}
	return nil
}

// requireNoOtherFeedbackV2Reader fails when a workflow other than the seed and the feedback
// workflow names the v2 family, which no main, merge-queue or release context may read.
func requireNoOtherFeedbackV2Reader(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return err
	}
	for _, file := range files {
		switch filepath.Base(file) {
		case "ci-feedback.yml", "ci-feedback-backend-cache.yml":
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "go-feedback-v2-") {
			return fmt.Errorf("%s reads the feedback v2 cache family outside the trust-split reader", filepath.Base(file))
		}
	}
	return nil
}

func TestFeedbackCacheFamiliesStaySeparate(t *testing.T) {
	root := repositoryRoot(t)
	seedRaw, err := os.ReadFile(filepath.Join(root, backendSeedWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	readerRaw, err := os.ReadFile(filepath.Join(root, feedbackWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireFeedbackCacheTrust(seedRaw, readerRaw); err != nil {
		t.Fatal(err)
	}
	if err := requireNoOtherFeedbackV2Reader(filepath.Join(root, ".github/workflows")); err != nil {
		t.Fatal(err)
	}

	const (
		devFamily     = "${{ github.event.pull_request.base.ref == 'dev' && 'dev' || 'main' }}"
		seedFamily    = "${{ github.ref == 'refs/heads/dev' && 'dev' || 'main' }}"
		restoreKeysLn = "restore-keys: go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-" + devFamily + "-${{ fromJSON(steps.plan.outputs.plan).lanes[matrix.lane].goScope }}-"
	)
	reader := map[string]func(string) string{
		// A prefix that spans both families: main PRs would restore the newest dev entry.
		"main prefix matches dev entries": func(s string) string {
			return strings.Replace(s, restoreKeysLn, "restore-keys: go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-", 1)
		},
		"extra bare fallback": func(s string) string {
			return strings.Replace(s, restoreKeysLn, restoreKeysLn+"\n            go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-", 1)
		},
		"every PR reads dev":  func(s string) string { return strings.ReplaceAll(s, devFamily, "dev") },
		"every PR reads main": func(s string) string { return strings.ReplaceAll(s, devFamily, "main") },
		"families swapped": func(s string) string {
			return strings.ReplaceAll(s, "base.ref == 'dev' && 'dev' || 'main'", "base.ref == 'dev' && 'main' || 'dev'")
		},
		"author-controlled family": func(s string) string {
			return strings.ReplaceAll(s, "github.event.pull_request.base.ref == 'dev'", "github.head_ref == 'dev'")
		},
		"loose branch match": func(s string) string {
			return strings.ReplaceAll(s, "github.event.pull_request.base.ref == 'dev' && 'dev' || 'main'", "startsWith(github.event.pull_request.base.ref, 'dev') && 'dev' || 'main'")
		},
		// The CLI reader is a second path to the same entries: its prefix must hold the family too.
		"cli prefix matches dev entries": func(s string) string {
			return strings.Replace(s, "restore-keys: go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-"+devFamily+"-cli-", "restore-keys: go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-", 1)
		},
		"cli entry read by every lane": func(s string) string {
			return strings.ReplaceAll(s, " && fromJSON(steps.plan.outputs.plan).lanes[matrix.lane].goScope == 'application'", "")
		},
		"entry dropped from prefix": func(s string) string {
			return strings.Replace(s, restoreKeysLn, "restore-keys: go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-"+devFamily+"-", 1)
		},
	}
	for name, mutate := range reader {
		t.Run("reader/"+name, func(t *testing.T) {
			changed := mutate(string(readerRaw))
			if changed == string(readerRaw) {
				t.Fatal("mutation changed nothing; the workflow text it targets moved")
			}
			if requireFeedbackCacheTrust(seedRaw, []byte(changed)) == nil {
				t.Fatal("accepted a reader that can restore across the trust families")
			}
		})
	}
	seed := map[string]func(string) string{
		"dev entries named main": func(s string) string {
			return strings.Replace(s, seedFamily+"-${{ matrix.entry }}", "main-${{ matrix.entry }}", 1)
		},
		"main entries named dev": func(s string) string {
			return strings.Replace(s, seedFamily+"-${{ matrix.entry }}", "dev-${{ matrix.entry }}", 1)
		},
		"family dropped from key": func(s string) string {
			return strings.Replace(s, seedFamily+"-${{ matrix.entry }}", "${{ matrix.entry }}", 1)
		},
		"entry dropped from key": func(s string) string {
			return strings.Replace(s, seedFamily+"-${{ matrix.entry }}-", seedFamily+"-", 1)
		},
		"author-controlled family": func(s string) string {
			return strings.Replace(s, "github.ref == 'refs/heads/dev' && 'dev' || 'main'", "github.head_ref == 'dev' && 'dev' || 'main'", 1)
		},
	}
	for name, mutate := range seed {
		t.Run("seed/"+name, func(t *testing.T) {
			changed := mutate(string(seedRaw))
			if changed == string(seedRaw) {
				t.Fatal("mutation changed nothing; the workflow text it targets moved")
			}
			if requireFeedbackCacheTrust([]byte(changed), readerRaw) == nil {
				t.Fatal("accepted a seed whose entries can reach the wrong family")
			}
		})
	}
	t.Run("another workflow reads the v2 family", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "ci.yml"), []byte("restore-keys: go-feedback-v2-${{ runner.os }}-\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if requireNoOtherFeedbackV2Reader(dir) == nil {
			t.Fatal("accepted a main or merge-queue reader of the v2 family")
		}
	})
}

func requireBackendSeed(workflow map[string]any) error {
	triggers := workflow["on"].(map[string]any)
	if len(triggers) != 3 || !reflect.DeepEqual(triggers["push"], map[string]any{"branches": []any{"dev"}}) {
		return fmt.Errorf("seed must run only daily, manually, or on pushes to dev")
	}
	if _, ok := triggers["workflow_dispatch"]; !ok {
		return fmt.Errorf("missing manual seed")
	}
	if _, ok := triggers["schedule"]; !ok {
		return fmt.Errorf("missing daily seed")
	}
	if !reflect.DeepEqual(workflow["permissions"], map[string]any{"contents": "read"}) {
		return fmt.Errorf("seed must be read-only")
	}
	if !reflect.DeepEqual(workflow["concurrency"], map[string]any{"group": "ci-feedback-backend-cache-${{ github.ref }}", "cancel-in-progress": false}) {
		return fmt.Errorf("each ref must serialize its own seed without cancelling a running one")
	}
	jobs := workflow["jobs"].(map[string]any)
	job := jobs["seed"].(map[string]any)
	if len(jobs) != 1 || job["runs-on"] != "ubuntu-latest" || job["timeout-minutes"] != 15 || job["continue-on-error"] != nil || job["permissions"] != nil || job["container"] != nil || job["services"] != nil || !reflect.DeepEqual(job["defaults"], map[string]any{"run": map[string]any{"working-directory": "source"}}) {
		return fmt.Errorf("seed must retain bounded native execution without masked or privileged overrides")
	}
	const trustedRefs = "((github.event_name == 'workflow_dispatch' || github.event_name == 'schedule') && github.ref == 'refs/heads/main') || ((github.event_name == 'workflow_dispatch' || github.event_name == 'push') && github.ref == 'refs/heads/dev')"
	if job["if"] != trustedRefs {
		return fmt.Errorf("only trusted main and dev may populate the seed")
	}
	if !reflect.DeepEqual(job["strategy"], map[string]any{"fail-fast": false, "matrix": map[string]any{"include": []any{
		map[string]any{"entry": "application", "roots": "./internal/app"},
		map[string]any{"entry": "cli", "roots": "./internal/cli"},
		map[string]any{"entry": "tooling", "roots": "./scripts/ci ./internal/boundary ./internal/isolation ./internal/lint"},
	}}}) {
		return fmt.Errorf("each lane needs its own bounded entry with the reviewed roots")
	}
	if !reflect.DeepEqual(job["env"], map[string]any{"GOCACHE": "${{ github.workspace }}/.ci-cache/go-feedback-compact", "GOFLAGS": "-gcflags=all=-dwarf=false"}) {
		return fmt.Errorf("seed must use the consumer directory and compact compiler ABI")
	}
	steps := job["steps"].([]any)
	names := []string{
		"Check out the immutable pushed commit without credentials", "", "Select runner cache ABI", "Name the UTC day for dev seeds",
		seedLookupName, "Compile a fresh feedback seed without running tests", "Compile the pinned workflow linter",
		"Check the seed against its size and file bounds", "Save bounded feedback seed from a trusted branch only",
	}
	if len(steps) != len(names) {
		return fmt.Errorf("unexpected seed preparation or publication")
	}
	for i, name := range names {
		if got, _ := steps[i].(map[string]any)["name"].(string); got != name {
			return fmt.Errorf("seed step %d is %q, want %q", i, got, name)
		}
	}
	checkout := steps[0].(map[string]any)["with"].(map[string]any)
	if checkout["ref"] != "${{ github.sha }}" || checkout["path"] != "source" || checkout["persist-credentials"] != false {
		return fmt.Errorf("seed must use the immutable pushed SHA without credentials")
	}
	if !reflect.DeepEqual(steps[1].(map[string]any)["env"], map[string]any{"GOCACHE": "${{ runner.temp }}/feedback-bootstrap"}) {
		return fmt.Errorf("setup diagnostics must use a separate temporary bootstrap cache")
	}
	setup := steps[1].(map[string]any)["with"].(map[string]any)
	if setup["cache"] != false || setup["go-version-file"] != "source/go.mod" {
		return fmt.Errorf("seed must select pinned Go without implicit caches")
	}
	if steps[3].(map[string]any)["run"] != `echo "value=$(date -u +%F)" >> "$GITHUB_OUTPUT"` {
		return fmt.Errorf("dev seeds must be named by UTC day, so a push storm cannot evict other caches")
	}
	lookup := steps[4].(map[string]any)
	wantKey := "go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ github.ref == 'refs/heads/dev' && 'dev' || 'main' }}-${{ matrix.entry }}-${{ hashFiles('source/go.mod', 'source/go.sum', 'source/scripts/ci/go-tool-modules.txt') }}-${{ hashFiles('source/.github/workflows/ci-feedback-backend-cache.yml') }}-${{ github.ref == 'refs/heads/dev' && steps.seed-day.outputs.value || github.sha }}"
	if lookup["id"] != "feedback-reuse" || lookup["uses"] != "actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9" || !reflect.DeepEqual(lookup["with"], map[string]any{
		"path":        ".ci-cache/go-feedback-compact",
		"key":         wantKey,
		"lookup-only": true,
	}) {
		return fmt.Errorf("seed lookup must never restore historical entries or lose family, entry, compatibility and source identity")
	}
	const missing = "steps.feedback-reuse.outputs.cache-hit != 'true'"
	compile := steps[5].(map[string]any)
	if compile["run"] != "test ! -e \"$GOCACHE\"\nmkdir -p \"$GOCACHE\"\ngo list -p 2 -deps -test -export ${{ matrix.roots }} >/dev/null\n" || compile["if"] != missing || compile["continue-on-error"] != nil || compile["env"] != nil {
		return fmt.Errorf("fresh seed must compile without executing tests")
	}
	linter := steps[6].(map[string]any)
	if linter["run"] != "./scripts/ci/run-go-tool.sh actionlint -version" || linter["if"] != missing+" && matrix.entry == 'tooling'" || linter["continue-on-error"] != nil {
		return fmt.Errorf("the tooling entry must compile the pinned linter")
	}
	size := steps[7].(map[string]any)
	if size["run"] != `go run ./scripts/ci/feedback-reuse-size "$GOCACHE"` || size["if"] != missing || size["continue-on-error"] != nil {
		return fmt.Errorf("every entry must fail closed over the size and file bounds")
	}
	save := steps[8].(map[string]any)
	wantSave := "success() && steps.feedback-reuse.outputs.cache-hit != 'true' && ((github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main' || (github.event_name == 'workflow_dispatch' || github.event_name == 'push') && github.ref == 'refs/heads/dev')"
	if save["uses"] != "actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9" || save["if"] != wantSave || save["continue-on-error"] != nil || !reflect.DeepEqual(save["with"], map[string]any{
		"path": ".ci-cache/go-feedback-compact",
		"key":  "${{ steps.feedback-reuse.outputs.cache-primary-key }}",
	}) {
		return fmt.Errorf("only a successful bounded main or dev seed may publish its own lookup key")
	}
	return nil
}

func TestBackendSeedWorkflowTrustAndBounds(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), backendSeedWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if err := requireBackendSeed(workflow); err != nil {
		t.Fatal(err)
	}
	job := func(w map[string]any) map[string]any { return w["jobs"].(map[string]any)["seed"].(map[string]any) }
	step := func(w map[string]any, i int) map[string]any { return job(w)["steps"].([]any)[i].(map[string]any) }
	for name, mutation := range map[string]func(map[string]any){
		"PR trigger": func(w map[string]any) { w["on"].(map[string]any)["pull_request"] = nil },
		"push to any branch": func(w map[string]any) {
			w["on"].(map[string]any)["push"] = map[string]any{"branches": []any{"dev", "main"}}
		},
		"write token":                   func(w map[string]any) { w["permissions"] = map[string]any{"contents": "write"} },
		"job write permission":          func(w map[string]any) { job(w)["permissions"] = map[string]any{"contents": "write"} },
		"untrusted ref":                 func(w map[string]any) { job(w)["if"] = "true" },
		"untrusted container":           func(w map[string]any) { job(w)["container"] = "untrusted/image:latest" },
		"masked seed job":               func(w map[string]any) { job(w)["continue-on-error"] = true },
		"wrong compiler ABI":            func(w map[string]any) { job(w)["env"].(map[string]any)["GOFLAGS"] = "-gcflags=all=-dwarf=true" },
		"widened save":                  func(w map[string]any) { step(w, 8)["if"] = "success()" },
		"save under another key":        func(w map[string]any) { step(w, 8)["with"].(map[string]any)["key"] = "go-feedback-v2-x" },
		"fail open bounds":              func(w map[string]any) { step(w, 7)["continue-on-error"] = true },
		"unbounded publisher":           func(w map[string]any) { step(w, 7)["run"] = "true" },
		"tests instead of compile":      func(w map[string]any) { step(w, 5)["run"] = "go test ./..." },
		"bootstrap inherits seed cache": func(w map[string]any) { delete(step(w, 1), "env") },
		"reuse oversized archive":       func(w map[string]any) { delete(step(w, 4)["with"].(map[string]any), "lookup-only") },
		"drop last required seed root": func(w map[string]any) {
			include := job(w)["strategy"].(map[string]any)["matrix"].(map[string]any)["include"].([]any)
			tooling := include[2].(map[string]any)
			tooling["roots"] = strings.TrimSuffix(tooling["roots"].(string), " ./internal/lint")
		},
		"third entry without a bound": func(w map[string]any) {
			m := job(w)["strategy"].(map[string]any)["matrix"].(map[string]any)
			m["include"] = append(m["include"].([]any), map[string]any{"entry": "all", "roots": "./..."})
		},
		"linter dropped from tooling entry": func(w map[string]any) { step(w, 6)["if"] = "false" },
		"shared concurrency":                func(w map[string]any) { w["concurrency"].(map[string]any)["group"] = "ci-feedback-backend-cache" },
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			if err := yaml.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutation(changed)
			if requireBackendSeed(changed) == nil {
				t.Fatal("accepted unsafe seed publisher")
			}
		})
	}
}
