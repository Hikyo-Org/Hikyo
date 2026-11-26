package ci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// The repository's Actions cache has a 10 GB limit that cannot be raised, and GitHub evicts by last
// access once it is passed. ci-cache-prune.yml removes the dead entries first: superseded entries of
// the listed regenerable families on main and dev, and idle pull-request entries. These tests pin its
// trust boundary and run the real script, with the real jq, against a fake `gh` that serves realistic
// API pages, so what it deletes is checked by what it deletes.

const (
	cachePruneWorkflow = ".github/workflows/ci-cache-prune.yml"
	cachePruneStep     = "Delete superseded and idle pull request caches"
	cachePruneGuard    = "(github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || (github.event_name == 'workflow_run' && github.event.workflow_run.head_branch == 'main' && github.event.workflow_run.head_repository.full_name == github.repository)) && github.ref == 'refs/heads/main'"
)

// cachePruneWriters are the workflows whose completion on main triggers a prune: the writers of the
// listed families that either run PR code (ci-main reuses ci.yml) or are not ours to edit (CodeQL).
var cachePruneWriters = []any{"CodeQL", "ci-main", "cache-seed-lint", "cache-seed-race", "cache-seed-tools", "cache-seed-feedback"}

func requireCachePruneWorkflow(workflow map[string]any) (string, error) {
	if !reflect.DeepEqual(workflow["on"], map[string]any{
		"workflow_dispatch": nil,
		"schedule":          []any{map[string]any{"cron": "47 * * * *"}},
		"workflow_run":      map[string]any{"workflows": cachePruneWriters, "types": []any{"completed"}},
	}) {
		return "", fmt.Errorf("the prune runs only on a schedule, by hand or after a named cache writer finished, never on a pull request or a push")
	}
	if !reflect.DeepEqual(workflow["permissions"], map[string]any{}) {
		return "", fmt.Errorf("the workflow grants nothing by default")
	}
	if !reflect.DeepEqual(workflow["concurrency"], map[string]any{"group": "ci-cache-prune", "cancel-in-progress": false}) {
		return "", fmt.Errorf("runs must queue behind each other, not cancel a half-done prune")
	}
	jobs, _ := workflow["jobs"].(map[string]any)
	if len(jobs) != 1 {
		return "", fmt.Errorf("the workflow is one prune job")
	}
	job, _ := jobs["prune"].(map[string]any)
	if job == nil || job["if"] != cachePruneGuard {
		return "", fmt.Errorf("prune must run only from a schedule or a manual run on main")
	}
	if !reflect.DeepEqual(job["permissions"], map[string]any{"actions": "write"}) {
		return "", fmt.Errorf("prune must hold actions: write and nothing else")
	}
	for _, key := range []string{"needs", "container", "services", "strategy", "continue-on-error", "defaults"} {
		if job[key] != nil {
			return "", fmt.Errorf("prune must not set %s", key)
		}
	}
	if !reflect.DeepEqual(job["env"], map[string]any{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}"}) {
		return "", fmt.Errorf("prune takes the workflow token and repository only")
	}
	steps, _ := job["steps"].([]any)
	if len(steps) != 1 {
		return "", fmt.Errorf("prune is a single script step: no checkout, no action, no repository code")
	}
	step, _ := steps[0].(map[string]any)
	run, _ := step["run"].(string)
	if step["name"] != cachePruneStep || run == "" || step["uses"] != nil || step["working-directory"] != nil || step["shell"] != nil || step["continue-on-error"] != nil || step["env"] != nil || step["if"] != nil {
		return "", fmt.Errorf("prune must run one inline script with nothing from the repository")
	}
	return run, nil
}

type apiCacheEntry struct {
	ID       int    `json:"id"`
	Ref      string `json:"ref"`
	Key      string `json:"key"`
	Created  string `json:"created_at"`
	Accessed string `json:"last_accessed_at"`
	Size     int64  `json:"size_in_bytes"`
}

var cachePruneNow = time.Date(2026, 11, 25, 19, 10, 0, 0, time.UTC)

// runCachePrune serves the pages as the cache listing and records deletes. The fake date is fixed so
// idle times are exact.
func runCachePrune(script string, pages [][]apiCacheEntry, missing, broken string) (deleted []int, listed []string, err error) {
	dir, err := os.MkdirTemp("", "cache-prune-test")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	var served strings.Builder
	for _, page := range pages {
		raw, err := json.Marshal(map[string]any{"total_count": len(page), "actions_caches": page})
		if err != nil {
			return nil, nil, err
		}
		served.Write(raw)
		served.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "pages.json"), []byte(served.String()), 0o600); err != nil {
		return nil, nil, err
	}
	gh := `#!/bin/sh
[ "$1" = api ] || exit 64
shift
printf '%s\n' "$*" >>"$FAKE_DIR/calls"
if [ "$1" = --paginate ]; then
	exec cat "$FAKE_DIR/pages.json"
fi
[ "$1" = -X ] && [ "$2" = DELETE ] || exit 65
id=${3##*/}
case "$id" in
	"$FAKE_MISSING") echo 'gh: Not Found (HTTP 404)' >&2; exit 1 ;;
	"$FAKE_BROKEN") echo 'gh: Forbidden (HTTP 403)' >&2; exit 1 ;;
esac
printf '%s\n' "$id" >>"$FAKE_DIR/deleted"
`
	date := "#!/bin/sh\n[ \"$*\" = '-u +%s' ] || exit 64\necho \"$FAKE_NOW\"\n"
	for name, body := range map[string]string{"gh": gh, "date": date} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
			return nil, nil, err
		}
	}
	cmd := exec.CommandContext(context.Background(), "bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "FAKE_DIR="+dir, "FAKE_MISSING="+missing, "FAKE_BROKEN="+broken,
		"FAKE_NOW="+strconv.FormatInt(cachePruneNow.Unix(), 10), "RUNNER_TEMP="+dir, "GH_TOKEN=t", "GH_REPO=o/r")
	out, runErr := cmd.CombinedOutput()
	if raw, readErr := os.ReadFile(filepath.Join(dir, "deleted")); readErr == nil {
		for _, field := range strings.Fields(string(raw)) {
			id, convErr := strconv.Atoi(field)
			if convErr != nil {
				return nil, nil, convErr
			}
			deleted = append(deleted, id)
		}
	}
	if raw, readErr := os.ReadFile(filepath.Join(dir, "calls")); readErr == nil {
		listed = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	if runErr != nil {
		return deleted, listed, fmt.Errorf("%w: %s", runErr, out)
	}
	return deleted, listed, nil
}

func cachePrunePages() [][]apiCacheEntry {
	h := func(c string, n int) string { return strings.Repeat(c, n) }
	const (
		main = "refs/heads/main"
		dev  = "refs/heads/dev"
	)
	e := func(id int, ref, key, created, accessed string) apiCacheEntry {
		return apiCacheEntry{ID: id, Ref: ref, Key: key, Created: created, Accessed: accessed, Size: 1 << 20}
	}
	const live = "2026-11-25T18:50:06.706511Z"
	first := []apiCacheEntry{
		e(1, main, "go-lintdriver-v2-Linux-X64-ubuntu26-"+h("a", 64)+"-2026-W48", "2026-11-24T14:57:00.1Z", live),
		e(2, main, "go-lintdriver-v2-Linux-X64-ubuntu26-"+h("a", 64)+"-2026-W47", "2026-11-17T14:57:00.1Z", "2026-11-23T09:00:00Z"),
		e(3, main, "go-mod-v2-Linux-"+h("a", 64), "2026-11-15T19:40:11.2Z", "2026-11-22T09:00:00Z"),
		e(4, main, "go-mod-v2-Linux-"+h("b", 64), "2026-11-22T19:40:11.2Z", live),
		e(5, dev, "go-mod-v2-Linux-"+h("b", 64), "2026-11-22T19:40:11.2Z", live),
		e(6, main, "codeql-overlay-base-database-1-433570c23e836bda-go-2.27.6-"+h("a", 40)+"-111-1", "2026-11-25T10:23:00.3Z", "2026-11-25T11:36:00Z"),
		// The listing is paged: the newest of the stem above arrives on the second page.
	}
	second := []apiCacheEntry{
		e(7, main, "codeql-overlay-base-database-1-433570c23e836bda-go-2.27.6-"+h("b", 40)+"-222-1", "2026-11-25T14:34:34.579064Z", "2026-11-25T14:34:34.579064Z"),
		// Seeds are owned by the seed's own prune job: never listed here, so the older sets stay.
		e(8, main, "go-feedback-v2-Linux-X64-ubuntu26-main-application-"+h("c", 64)+"-"+h("d", 64)+"-"+h("e", 40), "2026-11-24T03:40:00Z", "2026-11-25T12:00:00Z"),
		e(9, main, "go-feedback-v2-Linux-X64-ubuntu26-main-application-"+h("c", 64)+"-"+h("d", 64)+"-"+h("f", 40), "2026-11-25T03:40:00Z", live),
		// An open pull request's fresh entry is kept, an idle one is not.
		e(10, "refs/pull/9/merge", "go-mod-v2-Linux-"+h("a", 64), "2026-11-24T08:00:00Z", "2026-11-25T18:00:00Z"),
		e(11, "refs/pull/7/merge", "go-test-v2-Linux-X64-ubuntu26-"+h("a", 64), "2026-11-23T08:00:00Z", "2026-11-24T10:00:00Z"),
		// Another branch's entries and families this job does not list are never touched, however old.
		e(12, "refs/heads/feature-x", "go-mod-v2-Linux-"+h("a", 64), "2026-11-01T08:00:00Z", "2026-11-01T08:00:00Z"),
		e(13, main, "custom-thing-v1-aaa", "2026-11-01T08:00:00Z", "2026-11-01T08:00:00Z"),
		e(14, main, "custom-thing-v1-bbb", "2026-11-20T08:00:00Z", "2026-11-20T08:00:00Z"),
		// Different stems (the shard number, the tool) each keep their newest.
		e(15, main, "go-fuzz-v2-shard-0-Linux-X64-ubuntu26-"+h("a", 64)+"-2026-W47", "2026-11-22T20:31:00Z", live),
		e(16, main, "go-fuzz-v2-shard-1-Linux-X64-ubuntu26-"+h("a", 64)+"-2026-W47", "2026-11-22T20:28:00Z", live),
		e(17, main, "pinned-tool-v1-Linux-helm-linux-amd64-"+h("a", 64), "2026-11-10T12:35:20.1Z", live),
		e(18, main, "pinned-tool-v1-Linux-helm-linux-amd64-"+h("b", 64), "2026-11-16T12:35:20.1Z", live),
		e(19, main, "buildx-dl-bin-0.36.1-linux-x64", "2026-11-25T12:28:00Z", live),
		// Two entries of one stem on a ref this job does not own (a branch, an open pull request): the
		// older one is not superseded by this job's rule, whatever its age.
		e(20, "refs/heads/feature-x", "go-mod-v2-Linux-"+h("b", 64), "2026-11-10T08:00:00Z", "2026-11-10T08:00:00Z"),
		e(21, "refs/pull/9/merge", "go-mod-v2-Linux-"+h("b", 64), "2026-11-20T08:00:00Z", "2026-11-25T18:00:00Z"),
		// A superseded pair on dev, kept apart from main's entries of the same key.
		e(22, dev, "go-test-v2-Linux-X64-ubuntu26-"+h("a", 64), "2026-11-20T08:00:00Z", "2026-11-22T08:00:00Z"),
		e(23, dev, "go-test-v2-Linux-X64-ubuntu26-"+h("b", 64), "2026-11-24T08:00:00Z", live),
		// An overlay saved by a rerun (attempt 2) is as superseded as one saved by attempt 1.
		e(24, main, "codeql-overlay-base-database-1-433570c23e836bda-go-2.27.6-"+h("c", 40)+"-111-2", "2026-11-25T09:00:00Z", "2026-11-25T09:30:00Z"),
		// A runner-image bump and a CodeQL bump change the lane's discriminators, not the lane: the old
		// image's newest entry and the old CLI's newest entry go with the rest.
		e(25, main, "go-test-v2-Linux-X64-ubuntu24-"+h("a", 64), "2026-11-10T08:00:00Z", "2026-11-12T08:00:00Z"),
		e(26, main, "codeql-overlay-base-database-1-0123456789abcdef-go-2.27.5-"+h("a", 40)+"-90-1", "2026-11-20T08:00:00Z", "2026-11-20T09:00:00Z"),
		e(27, main, "go-test-v2-Linux-X64-ubuntu26-"+h("b", 64), "2026-11-22T08:00:00Z", live),
		// The family match is anchored: a name that merely contains a listed one is not that family.
		e(28, main, "xgo-mod-v2-Linux-"+h("a", 64), "2026-11-10T08:00:00Z", "2026-11-10T08:00:00Z"),
		e(29, main, "xgo-mod-v2-Linux-"+h("b", 64), "2026-11-22T08:00:00Z", live),
	}
	return [][]apiCacheEntry{first, second}
}

func requireCachePruneBehavior(script string) error {
	deleted, calls, err := runCachePrune(script, cachePrunePages(), "", "")
	if err != nil {
		return err
	}
	sort.Ints(deleted)
	// Superseded: 2 old week, 3 old go-mod, 6 and 24 old overlays (attempts 1 and 2), 17 old tool, 25 old
	// runner image, 26 old CodeQL CLI, 22 dev's older test entry. 11: idle pull request.
	if want := []int{2, 3, 6, 11, 17, 22, 24, 25, 26}; !reflect.DeepEqual(deleted, want) {
		return fmt.Errorf("deleted %v, want %v", deleted, want)
	}
	if len(calls) != 1+len(deleted) || !strings.Contains(calls[0], "repos/o/r/actions/caches?per_page=100") {
		return fmt.Errorf("api calls %v: want one paged listing and one delete each", calls)
	}
	// An entry that is already gone is success; any other failure is not.
	if got, _, err := runCachePrune(script, cachePrunePages(), "3", ""); err != nil || len(got) != 8 {
		return fmt.Errorf("a 404 on delete must count as deleted and the rest still go: %v %w", got, err)
	}
	if _, _, err := runCachePrune(script, cachePrunePages(), "", "3"); err == nil {
		return fmt.Errorf("a failed delete must fail the job")
	}
	// One run deletes at most 100 entries, so the API budget per run is bounded.
	var many []apiCacheEntry
	for i := 0; i < 131; i++ {
		many = append(many, apiCacheEntry{ID: 1000 + i, Ref: "refs/heads/main", Key: fmt.Sprintf("go-mod-v2-Linux-%064x", i),
			Created: fmt.Sprintf("2026-10-01T00:%02d:%02dZ", i/60, i%60), Accessed: "2026-11-25T18:00:00Z"})
	}
	if got, _, err := runCachePrune(script, [][]apiCacheEntry{many}, "", ""); err != nil || len(got) != 100 {
		return fmt.Errorf("one run must delete at most 100 entries, got %d: %w", len(got), err)
	}
	return nil
}

func TestCachePruneDeletesDeadEntriesOnly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), cachePruneWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	script, err := requireCachePruneWorkflow(workflow)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireCachePruneBehavior(script); err != nil {
		t.Fatal(err)
	}

	job := func(w map[string]any) map[string]any { return w["jobs"].(map[string]any)["prune"].(map[string]any) }
	structural := map[string]func(map[string]any){
		"runs on pull requests": func(w map[string]any) { w["on"].(map[string]any)["pull_request"] = map[string]any{} },
		"runs on pull_request_target": func(w map[string]any) {
			w["on"].(map[string]any)["pull_request_target"] = map[string]any{}
		},
		"runs on pushes":          func(w map[string]any) { w["on"].(map[string]any)["push"] = map[string]any{} },
		"runs after any workflow": func(w map[string]any) { w["on"].(map[string]any)["workflow_run"] = map[string]any{} },
		"runs after a PR workflow": func(w map[string]any) {
			run := w["on"].(map[string]any)["workflow_run"].(map[string]any)
			run["workflows"] = append(run["workflows"].([]any), "trusted-ci")
		},
		"runs when a writer starts": func(w map[string]any) {
			w["on"].(map[string]any)["workflow_run"].(map[string]any)["types"] = []any{"requested"}
		},
		"guard accepts any triggering branch": func(w map[string]any) {
			job(w)["if"] = strings.Replace(cachePruneGuard, " && github.event.workflow_run.head_branch == 'main'", "", 1)
		},
		"guard accepts a fork": func(w map[string]any) {
			job(w)["if"] = strings.Replace(cachePruneGuard, " && github.event.workflow_run.head_repository.full_name == github.repository", "", 1)
		},
		"guard accepts any ref": func(w map[string]any) { job(w)["if"] = "github.event_name == 'schedule'" },
		"guard accepts a pull":  func(w map[string]any) { job(w)["if"] = cachePruneGuard + " || github.event_name == 'pull_request'" },
		"guard accepts dev": func(w map[string]any) {
			job(w)["if"] = strings.Replace(cachePruneGuard, "refs/heads/main", "refs/heads/dev", 1)
		},
		"widened permissions":      func(w map[string]any) { job(w)["permissions"].(map[string]any)["contents"] = "write" },
		"workflow-wide token":      func(w map[string]any) { w["permissions"] = map[string]any{"actions": "write"} },
		"no job permissions block": func(w map[string]any) { delete(job(w), "permissions") },
		"checks out the repository": func(w map[string]any) {
			job(w)["steps"] = append([]any{map[string]any{"uses": "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"}}, job(w)["steps"].([]any)...)
		},
		"runs repository code": func(w map[string]any) {
			job(w)["steps"].([]any)[0].(map[string]any)["run"] = "./scripts/ci/prune.sh"
			job(w)["steps"].([]any)[0].(map[string]any)["working-directory"] = "source"
		},
		"takes input from the event": func(w map[string]any) { job(w)["env"].(map[string]any)["REF"] = "${{ github.head_ref }}" },
		"a second job": func(w map[string]any) {
			w["jobs"].(map[string]any)["other"] = map[string]any{"runs-on": "ubuntu-latest", "steps": []any{}}
		},
		"cancels a half-done prune": func(w map[string]any) { w["concurrency"].(map[string]any)["cancel-in-progress"] = true },
	}
	for name, mutate := range structural {
		t.Run("structure/"+name, func(t *testing.T) {
			var changed map[string]any
			if err := yaml.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(changed)
			if _, err := requireCachePruneWorkflow(changed); err == nil {
				t.Fatal("accepted a prune workflow outside its trust boundary")
			}
		})
	}
	scriptMutations := map[string]func(string) string{
		"newest of a stem deleted": func(s string) string { return strings.Replace(s, ".[1:][]", ".[]", 1) },
		"refs not restricted": func(s string) string {
			return strings.Replace(s, "(.ref == \"refs/heads/main\" or .ref == \"refs/heads/dev\") and ", "", 1)
		},
		"every family is regenerable": func(s string) string { return strings.Replace(s, " and (.key | regenerable)", "", 1) },
		"lanes ignore the ref": func(s string) string {
			return strings.Replace(s, "group_by([.ref, (.key | lane)])", "group_by([.key | lane])", 1)
		},
		"stem keeps the hash": func(s string) string {
			return strings.Replace(s, "sub(\"-[0-9a-f]{64}(-.*)?$\"; \"\") | ", "", 1)
		},
		"fresh pull request entries deleted": func(s string) string { return strings.Replace(s, "($now - 86400)", "($now + 86400)", 1) },
		"idle test inverted":                 func(s string) string { return strings.Replace(s, "| epoch) < ($now", "| epoch) > ($now", 1) },
		"any ref counts as a pull request": func(s string) string {
			return strings.Replace(s, "^refs/pull/[0-9]+/(merge|head)$", ".*", 1)
		},
		"lane keeps the runner image": func(s string) string {
			return strings.Replace(s, ` | gsub("-ubuntu[0-9]+"; "")`, "", 1)
		},
		"lane keeps the tool version": func(s string) string {
			return strings.Replace(s, ` | gsub("-[0-9]+(\\.[0-9]+)+"; "")`, "", 1)
		},
		"lane keeps the config hash": func(s string) string {
			return strings.Replace(s, ` | gsub("-[0-9a-f]{16}(?=-)"; "")`, "", 1)
		},
		"attempt suffix is only the first": func(s string) string {
			return strings.Replace(s, "(-[0-9]+-[0-9]+)?$", "(-[0-9]+-1)?$", 1)
		},
		"family match unanchored": func(s string) string { return strings.Replace(s, `test("^(go-mod-v2`, `test("(go-mod-v2`, 1) },
		"dev ref dropped": func(s string) string {
			return strings.Replace(s, ` or .ref == "refs/heads/dev"`, "", 1)
		},
		"only the first page": func(s string) string { return strings.Replace(s, "jq -s -r", "jq -r", 1) },
		"no delete cap": func(s string) string {
			return strings.Replace(s, "head -n 100 \"${RUNNER_TEMP}/victims.tsv\" |", "cat \"${RUNNER_TEMP}/victims.tsv\" |", 1)
		},
		"jq id renamed": func(s string) string { return strings.Replace(s, "[.id, .ref, .key]", "[.identifier, .ref, .key]", 1) },
		"404 is failure": func(s string) string {
			return strings.Replace(s, "grep -q 'HTTP 404' \"${RUNNER_TEMP}/delete.err\" || ", "", 1)
		},
		"any failure is success": func(s string) string {
			return strings.Replace(s, "{ cat \"${RUNNER_TEMP}/delete.err\" >&2; exit 1; }", "true", 1)
		},
	}
	for name, mutate := range scriptMutations {
		t.Run("script/"+name, func(t *testing.T) {
			changed := mutate(script)
			if changed == script {
				t.Fatal("mutation changed nothing; the script text it targets moved")
			}
			if requireCachePruneBehavior(changed) == nil {
				t.Fatal("accepted a prune script that can delete the wrong entries")
			}
		})
	}
}
