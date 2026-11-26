package ci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The seed leaves one superseded set per day or per commit behind. The prune job removes them so
// the repository cache budget holds one set per family. It is a separate job so that the one that
// compiles keeps a read-only token; these tests pin that boundary and run the real script against a
// fake `gh`, so the deletion prefix is checked by what it deletes, not by how it reads.

const pruneStepName = "Delete the superseded seeds of this family and entry"

const pruneTrustedRefs = "success() && (((github.event_name == 'workflow_dispatch' || github.event_name == 'schedule') && github.ref == 'refs/heads/main') || ((github.event_name == 'workflow_dispatch' || github.event_name == 'push') && github.ref == 'refs/heads/dev'))"

func requirePruneJob(workflow map[string]any) (string, error) {
	jobs, _ := workflow["jobs"].(map[string]any)
	prune, _ := jobs["prune"].(map[string]any)
	if prune == nil {
		return "", fmt.Errorf("the seed must prune what it supersedes")
	}
	if prune["needs"] != "seed" || prune["if"] != pruneTrustedRefs {
		return "", fmt.Errorf("prune must follow a successful seed and run only for trusted main and dev")
	}
	if !reflect.DeepEqual(prune["permissions"], map[string]any{"actions": "write"}) {
		return "", fmt.Errorf("prune must hold actions: write and nothing else")
	}
	for _, key := range []string{"container", "services", "continue-on-error", "defaults"} {
		if prune[key] != nil {
			return "", fmt.Errorf("prune must not set %s", key)
		}
	}
	if !reflect.DeepEqual(prune["strategy"], map[string]any{"fail-fast": false, "matrix": map[string]any{"entry": []any{"application", "cli", "tooling"}}}) {
		return "", fmt.Errorf("prune must visit exactly the seeded entries, one prefix each")
	}
	if !reflect.DeepEqual(prune["env"], map[string]any{
		"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}",
		"ABI": "${{ needs.seed.outputs.abi }}", "SEED_DAY": "${{ needs.seed.outputs.day }}", "ENTRY": "${{ matrix.entry }}",
	}) {
		return "", fmt.Errorf("prune must take its identity from the seed run and the workflow token only")
	}
	steps, _ := prune["steps"].([]any)
	if len(steps) != 1 {
		return "", fmt.Errorf("prune is a single script step: no checkout, no action, no repository code")
	}
	step, _ := steps[0].(map[string]any)
	run, _ := step["run"].(string)
	if step["name"] != pruneStepName || run == "" || step["uses"] != nil || step["working-directory"] != nil || step["shell"] != nil || step["continue-on-error"] != nil || step["env"] != nil || step["if"] != nil {
		return "", fmt.Errorf("prune must run one inline script with nothing from the repository")
	}
	seed, _ := jobs["seed"].(map[string]any)
	if !reflect.DeepEqual(seed["outputs"], map[string]any{
		"abi": "${{ steps.runner-cache-abi.outputs.value }}", "day": "${{ steps.seed-day.outputs.value }}",
	}) {
		return "", fmt.Errorf("the seed must hand its ABI and day to prune")
	}
	return run, nil
}

type fakeCache struct{ id, key, created, ref string }

// runPrune executes the script with a fake gh. The fake serves the API's JSON page (actions_caches
// with id, key, ref and created_at) for every cache regardless of the requested key and ref, as a
// hostile or lagging server could, and applies the script's real --jq expression with jq.
func runPrune(script string, env map[string]string, caches []fakeCache, missing, broken string) (deleted []string, listed string, err error) {
	dir, err := os.MkdirTemp("", "prune-test")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	type apiCache struct {
		ID      int    `json:"id"`
		Ref     string `json:"ref"`
		Key     string `json:"key"`
		Created string `json:"created_at"`
	}
	page := struct {
		Total  int        `json:"total_count"`
		Caches []apiCache `json:"actions_caches"`
	}{Total: len(caches)}
	for _, c := range caches {
		id, err := strconv.Atoi(c.id)
		if err != nil {
			return nil, "", err
		}
		page.Caches = append(page.Caches, apiCache{ID: id, Ref: c.ref, Key: c.key, Created: c.created})
	}
	raw, err := json.Marshal(page)
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "page.json"), raw, 0o600); err != nil {
		return nil, "", err
	}
	gh := `#!/bin/sh
[ "$1" = api ] || exit 64
shift
if [ "$1" = --paginate ]; then
	printf '%s\n' "$2" >"$FAKE_DIR/listed"
	[ "$3" = --jq ] || exit 66
	exec jq -r "$4" "$FAKE_DIR/page.json"
fi
[ "$1" = -X ] && [ "$2" = DELETE ] || exit 65
id=${3##*/}
case "$id" in
	"$FAKE_MISSING") echo 'gh: Not Found (HTTP 404)' >&2; exit 1 ;;
	"$FAKE_BROKEN") echo 'gh: Forbidden (HTTP 403)' >&2; exit 1 ;;
esac
printf '%s\n' "$id" >>"$FAKE_DIR/deleted"
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0o700); err != nil {
		return nil, "", err
	}
	cmd := exec.CommandContext(context.Background(), "bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "FAKE_DIR="+dir, "FAKE_MISSING="+missing, "FAKE_BROKEN="+broken, "RUNNER_TEMP="+dir, "RUNNER_OS=Linux", "RUNNER_ARCH=X64")
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, runErr := cmd.CombinedOutput()
	if raw, readErr := os.ReadFile(filepath.Join(dir, "deleted")); readErr == nil {
		deleted = strings.Fields(string(raw))
	}
	if raw, readErr := os.ReadFile(filepath.Join(dir, "listed")); readErr == nil {
		listed = strings.TrimSpace(string(raw))
	}
	if runErr != nil {
		return deleted, listed, fmt.Errorf("%w: %s", runErr, out)
	}
	return deleted, listed, nil
}

func pruneCaches() []fakeCache {
	const base = "go-feedback-v2-Linux-X64-ubuntu24-"
	caches := []fakeCache{
		{"1", base + "main-application-h1-w1-shaNEW", "2026-11-25T03:40:00Z", ""},
		{"2", base + "main-application-h1-w1-shaOLD", "2026-11-24T03:40:00Z", ""},
		{"3", base + "main-application-h0-w0-shaOLDER", "2026-11-20T03:40:00Z", ""},
		{"4", base + "dev-application-h1-w1-2026-11-24", "2026-11-24T12:00:00Z", ""},
		{"5", base + "main-cli-h1-w1-shaOLD", "2026-11-24T03:40:00Z", ""},
		{"6", base + "main-tooling-h1-w1-shaOLD", "2026-11-24T03:40:00Z", ""},
		{"7", "go-feedback-v2-Linux-X64-ubuntu22-main-application-h1-w1-shaOLD", "2026-11-01T03:40:00Z", ""},
		{"8", "go-test-v2-Linux-X64-ubuntu24-hash", "2026-11-24T03:40:00Z", ""},
		{"9", base + "main-applicationX-h1-w1-shaOLD", "2026-11-24T03:40:00Z", ""},
		{"10", base + "main-application-h1-w1-shaOTHER-NEWER", "2026-11-25T05:00:00Z", ""},
		{"11", base + "dev-application-h1-w1-2026-11-25", "2026-11-25T08:00:00Z", ""},
		{"12", base + "dev-application-h1-w1-2026-11-23", "2026-11-23T08:00:00Z", ""},
		{"13", base + "dev-tooling-h1-w1-2026-11-23", "2026-11-23T08:00:00Z", ""},
		// The same prefix on a pull request's merge ref: not this run's ref, so never deleted, whatever
		// the server returns.
		{"14", base + "main-application-h1-w1-shaPR", "2026-11-23T03:40:00Z", "refs/pull/9/merge"},
		{"15", base + "dev-application-h1-w1-2026-11-22", "2026-11-22T08:00:00Z", "refs/pull/9/merge"},
	}
	for i := range caches {
		if caches[i].ref != "" {
			continue
		}
		caches[i].ref = "refs/heads/main"
		if strings.Contains(caches[i].key, "-dev-") {
			caches[i].ref = "refs/heads/dev"
		}
	}
	return caches
}

func pruneEnv(ref, entry string) map[string]string {
	return map[string]string{
		"GH_TOKEN": "t", "GH_REPO": "o/r", "ABI": "ubuntu24", "SEED_DAY": "2026-11-25", "ENTRY": entry,
		"GITHUB_REF": ref, "GITHUB_SHA": "shaNEW",
	}
}

// requirePruneBehavior is the contract: which ids the script may delete for each context.
func requirePruneBehavior(script string) error {
	type scenario struct {
		name, ref, entry, wantList string
		wantDeleted                []string
	}
	for _, sc := range []scenario{
		// Main keeps the key it just saved and the newest entry; the older main sets go. Nothing of the
		// dev family, another entry, another ABI or another cache family is touched.
		{"main application", "refs/heads/main", "application", "key=go-feedback-v2-Linux-X64-ubuntu24-main-application-", []string{"2", "3"}},
		{"main cli", "refs/heads/main", "cli", "key=go-feedback-v2-Linux-X64-ubuntu24-main-cli-", nil},
		// Dev keeps today's set and the newest, removes earlier days, and never touches a main entry.
		{"dev application", "refs/heads/dev", "application", "key=go-feedback-v2-Linux-X64-ubuntu24-dev-application-", []string{"4", "12"}},
		{"dev tooling", "refs/heads/dev", "tooling", "key=go-feedback-v2-Linux-X64-ubuntu24-dev-tooling-", nil},
	} {
		deleted, listed, err := runPrune(script, pruneEnv(sc.ref, sc.entry), pruneCaches(), "", "")
		if err != nil {
			return fmt.Errorf("%s: %w", sc.name, err)
		}
		if !strings.Contains(listed, sc.wantList) || !strings.Contains(listed, "ref="+sc.ref+"&") {
			return fmt.Errorf("%s: lists %q, want the exact prefix %q on its own ref", sc.name, listed, sc.wantList)
		}
		if !reflect.DeepEqual(deleted, sc.wantDeleted) {
			return fmt.Errorf("%s: deleted %v, want %v", sc.name, deleted, sc.wantDeleted)
		}
	}
	// An entry that is already gone is success; any other failure is not.
	if _, _, err := runPrune(script, pruneEnv("refs/heads/main", "application"), pruneCaches(), "2", ""); err != nil {
		return fmt.Errorf("a 404 on delete must count as deleted: %w", err)
	}
	if _, _, err := runPrune(script, pruneEnv("refs/heads/main", "application"), pruneCaches(), "", "2"); err == nil {
		return fmt.Errorf("a failed delete must fail the job")
	}
	// Missing identity from the seed fails closed instead of building a short prefix.
	env := pruneEnv("refs/heads/main", "application")
	env["ABI"] = ""
	if _, _, err := runPrune(script, env, pruneCaches(), "", ""); err == nil {
		return fmt.Errorf("prune without the seed's ABI must refuse")
	}
	return nil
}

func TestSeedPrunesSupersededEntriesOnly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), backendSeedWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	script, err := requirePruneJob(workflow)
	if err != nil {
		t.Fatal(err)
	}
	if err := requirePruneBehavior(script); err != nil {
		t.Fatal(err)
	}

	prune := func(w map[string]any) map[string]any { return w["jobs"].(map[string]any)["prune"].(map[string]any) }
	structural := map[string]func(map[string]any){
		"widened permissions":  func(w map[string]any) { prune(w)["permissions"].(map[string]any)["contents"] = "write" },
		"contents read added":  func(w map[string]any) { prune(w)["permissions"].(map[string]any)["contents"] = "read" },
		"no permissions block": func(w map[string]any) { delete(prune(w), "permissions") },
		"checks out the repository": func(w map[string]any) {
			prune(w)["steps"] = append([]any{map[string]any{"uses": "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"}}, prune(w)["steps"].([]any)...)
		},
		"runs repository code": func(w map[string]any) {
			prune(w)["steps"].([]any)[0].(map[string]any)["run"] = "./scripts/ci/prune.sh"
			prune(w)["steps"].([]any)[0].(map[string]any)["working-directory"] = "source"
		},
		"runs without the seed": func(w map[string]any) { delete(prune(w), "needs") },
		"runs for any ref":      func(w map[string]any) { prune(w)["if"] = "success()" },
		"runs after a failed seed": func(w map[string]any) {
			prune(w)["if"] = strings.Replace(pruneTrustedRefs, "success() && ", "always() && ", 1)
		},
		"extra entry": func(w map[string]any) {
			prune(w)["strategy"].(map[string]any)["matrix"].(map[string]any)["entry"] = []any{"application", "cli", "tooling", ""}
		},
		"seed keeps write token": func(w map[string]any) {
			w["jobs"].(map[string]any)["seed"].(map[string]any)["permissions"] = map[string]any{"actions": "write"}
		},
		"seed hands over no ABI":    func(w map[string]any) { delete(w["jobs"].(map[string]any)["seed"].(map[string]any), "outputs") },
		"entry taken from the repo": func(w map[string]any) { prune(w)["env"].(map[string]any)["ENTRY"] = "${{ github.head_ref }}" },
	}
	for name, mutate := range structural {
		t.Run("structure/"+name, func(t *testing.T) {
			var changed map[string]any
			if err := yaml.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(changed)
			_, pruneErr := requirePruneJob(changed)
			seedErr := requireBackendSeed(changed)
			if pruneErr == nil && seedErr == nil {
				t.Fatal("accepted a prune job outside its trust boundary")
			}
		})
	}
	scriptMutations := map[string]func(string) string{
		"prefix without the entry": func(s string) string { return strings.Replace(s, "-${family}-${ENTRY}-\"", "-${family}-\"", 1) },
		"prefix without the family": func(s string) string {
			return strings.Replace(s, "-${ABI}-${family}-${ENTRY}-", "-${ABI}-${ENTRY}-", 1)
		},
		"prefix without the ABI": func(s string) string { return strings.Replace(s, "${RUNNER_ARCH}-${ABI}-", "${RUNNER_ARCH}-", 1) },
		"server filter trusted": func(s string) string {
			return strings.Replace(s, "awk -F'\\t' -v p=\"$prefix\" 'index($2, p) == 1' | ", "", 1)
		},
		"loose prefix match": func(s string) string {
			return strings.Replace(s, "index($2, p) == 1", "index($2, \"-\" ENVIRON[\"ENTRY\"] \"-\") > 0", 1)
		},
		"ref not checked": func(s string) string { return strings.Replace(s, "select(.ref == env.GITHUB_REF) | ", "", 1) },
		"jq field renamed": func(s string) string {
			return strings.Replace(s, "[.id, .key, .created_at]", "[.id, .name, .created_at]", 1)
		},
		"jq output not tsv": func(s string) string { return strings.Replace(s, "| @tsv'", "| @csv'", 1) },
		"newest not kept":   func(s string) string { return strings.Replace(s, "[ \"$id\" != \"$newest\" ] || continue", "true", 1) },
		"just saved not kept": func(s string) string {
			return strings.Replace(s, "case \"$key\" in *\"-${suffix}\") continue ;; esac", "true", 1)
		},
		"not scoped to the ref": func(s string) string { return strings.Replace(s, "&ref=${GITHUB_REF}", "", 1) },
		"404 is failure": func(s string) string {
			return strings.Replace(s, "grep -q 'HTTP 404' \"${RUNNER_TEMP}/delete.err\" || ", "", 1)
		},
		"any failure is success": func(s string) string {
			return strings.Replace(s, "{ cat \"${RUNNER_TEMP}/delete.err\" >&2; exit 1; }", "true", 1)
		},
		"empty identity tolerated": func(s string) string {
			return strings.Replace(s, "[ -n \"$ABI\" ] && [ -n \"$SEED_DAY\" ] ||", "true ||", 1)
		},
		"deletes by day for main": func(s string) string { return strings.Replace(s, "suffix=$GITHUB_SHA", "suffix=$SEED_DAY", 1) },
	}
	for name, mutate := range scriptMutations {
		t.Run("script/"+name, func(t *testing.T) {
			changed := mutate(script)
			if changed == script {
				t.Fatal("mutation changed nothing; the script text it targets moved")
			}
			if requirePruneBehavior(changed) == nil {
				t.Fatal("accepted a prune script that can delete the wrong entries")
			}
		})
	}
}

// The cache-policy fixture compares each writer's whole `if:` guard. A guard that merely contains
// the trusted one, or a dev ref outside the seed workflow, fails it.
func TestCachePolicyWriterGuardIsExact(t *testing.T) {
	root, run := cachePolicyFixture(t)
	const mainGuard = "(github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'"
	for _, tc := range []struct{ name, file, from, to string }{
		{"seed guard widened with an alternative", "ci-feedback-backend-cache.yml", "github.ref == 'refs/heads/dev')\n        uses:", "github.ref == 'refs/heads/dev' || github.event_name == 'pull_request')\n        uses:"},
		{"main guard widened with an alternative", "ci-lint-cache.yml", mainGuard + "\n        uses:", mainGuard + " || github.event_name == 'pull_request'\n        uses:"},
		{"main guard with a trailing alternative", "ci-race-cache.yml", "github.ref == 'refs/heads/main'\n        uses:", "github.ref == 'refs/heads/main' || true\n        uses:"},
		{"dev ref outside the seed", "ci-tool-cache.yml", mainGuard + "\n        uses:", "(" + mainGuard + " || (github.event_name == 'workflow_dispatch' || github.event_name == 'push') && github.ref == 'refs/heads/dev')\n        uses:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, ".github/workflows", tc.file)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.WriteFile(path, original, 0o644) }()
			changed := strings.Replace(string(original), tc.from, tc.to, 1)
			if changed == string(original) {
				t.Fatal("mutation changed nothing; the guard text it targets moved")
			}
			if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := run()
			if err == nil || !strings.Contains(out, "cache writer without trusted-main exact-miss guard in "+tc.file) {
				t.Fatalf("accepted a cache writer guard that is not the exact trusted one: %v\n%s", err, out)
			}
		})
	}
}

// cachePolicyFixture copies what the cache-policy script reads into a temporary tree and returns
// its root with a runner for the script; the unmodified tree must pass.
func cachePolicyFixture(t *testing.T) (string, func() (string, error)) {
	t.Helper()
	repo := repositoryRoot(t)
	root := t.TempDir()
	files := []string{"scripts/ci/check-cache-policy_test.sh", "scripts/ci/go-tool-modules.txt", "scripts/ci/verify-docs.sh", "web/package.json", "docs/site/package.json"}
	entries, err := os.ReadDir(filepath.Join(repo, ".github/workflows"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".yml") || strings.HasSuffix(entry.Name(), ".yaml") {
			files = append(files, filepath.Join(".github/workflows", entry.Name()))
		}
	}
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func() (string, error) {
		out, err := exec.CommandContext(context.Background(), "sh", filepath.Join(root, "scripts/ci/check-cache-policy_test.sh")).CombinedOutput()
		return string(out), err
	}
	if out, err := run(); err != nil {
		t.Fatalf("unmodified workflows refused: %v\n%s", err, out)
	}
	return root, run
}

// The module cache path is part of the cache version: a drifted spelling is a silent miss on every
// restore, so each module reader must carry the exact path line.
func TestCachePolicyModuleCachePathIsExact(t *testing.T) {
	root, run := cachePolicyFixture(t)
	const exact = "path: ~/go/pkg/mod/cache/download\n"
	for name, drifted := range map[string]string{
		"parent directory":   "path: ~/go/pkg/mod/cache\n",
		"trailing slash":     "path: ~/go/pkg/mod/cache/download/\n",
		"extracted tree":     "path: ~/go/pkg/mod\n",
		"different sub-path": "path: ~/go/pkg/mod/cache/download-v2\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, ".github/workflows/ci-golangci.yml")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.WriteFile(path, original, 0o644) }()
			changed := strings.Replace(string(original), exact, drifted, 1)
			if changed == string(original) {
				t.Fatal("mutation changed nothing; ci-golangci.yml no longer restores the module cache this way")
			}
			if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := run()
			// The whole-tree spelling is refused by its own earlier check, the rest by the reader check.
			want := "incompatible Go cache reader in ci-golangci.yml"
			if name == "extracted tree" {
				want = "archives the whole module tree"
			}
			if err == nil || !strings.Contains(out, want) {
				t.Fatalf("accepted a drifted module cache path: %v\n%s", err, out)
			}
		})
	}
}
