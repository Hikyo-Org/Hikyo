package ci_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStableSigningAndPublicationStaySeparate(t *testing.T) {
	type step struct {
		ID   string            `yaml:"id"`
		Run  string            `yaml:"run"`
		If   string            `yaml:"if"`
		Uses string            `yaml:"uses"`
		With map[string]string `yaml:"with"`
	}
	type job struct {
		If          string            `yaml:"if"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []step            `yaml:"steps"`
	}
	type releaseWorkflow struct {
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]job       `yaml:"jobs"`
	}
	read := func(name string) releaseWorkflow {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var workflow releaseWorkflow
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
			t.Fatal("workflow default must have only contents read permission")
		}
		return workflow
	}
	build := read("release.yml")
	if len(build.On) != 1 {
		t.Fatal("stable signing must only be tag push triggered")
	}
	if _, ok := build.On["push"]; !ok {
		t.Fatal("stable signing push trigger is missing")
	}
	var source, evidence *step
	for i := range build.Jobs["build-artifacts"].Steps {
		candidate := &build.Jobs["build-artifacts"].Steps[i]
		if candidate.ID == "release-source" {
			source = candidate
		}
		if candidate.With["name"] == "release-source-proof-${{ github.run_id }}-${{ github.run_attempt }}" {
			evidence = candidate
		}
	}
	if source == nil || !strings.Contains(source.Run, "./scripts/ci/source-proof") ||
		!strings.Contains(source.Run, `--mode main --commit "$GITHUB_SHA" --authority-commit "$GITHUB_SHA"`) ||
		strings.Contains(source.Run, "./scripts/release/test-source.sh") {
		t.Fatal("release source must revalidate complete exact-source proof without another ordinary source test pass")
	}
	if evidence == nil || evidence.If != "steps.release-source.outcome == 'success'" ||
		!strings.HasPrefix(evidence.Uses, "actions/upload-artifact@") || evidence.With["if-no-files-found"] != "error" ||
		evidence.With["path"] != "${{ runner.temp }}/release-source-proof.json" {
		t.Fatal("successful release source proof must be retained as exact small evidence")
	}
	signer := build.Jobs["build-signed-draft"]
	if signer.Permissions["id-token"] != "write" {
		t.Fatal("tag build requires OIDC")
	}
	var buildRun strings.Builder
	for _, step := range signer.Steps {
		buildRun.WriteString(step.Run)
		buildRun.WriteByte('\n')
	}
	for _, required := range []string{"require-signed-tag.sh", "stable preflight", "create-build-provenance.sh", "sign-stable-draft.sh", "--draft", "stable verify"} {
		if !strings.Contains(buildRun.String(), required) {
			t.Fatalf("stable draft gate missing: %s", required)
		}
	}
	for _, forbidden := range []string{"--draft=false", "--key ", "COSIGN_PASSWORD", "ceremony.sh"} {
		if strings.Contains(buildRun.String(), forbidden) {
			t.Fatalf("tag build must not publish or use private keys: %s", forbidden)
		}
	}
	if _, err := os.Stat(filepath.Join(repositoryRoot(t), ".github", "workflows", "release-publish.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the tag-ref build never publishes; unattended publication lives only in stable-release-reconciler.yml")
	}
	ceremony, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "release", "stable-ceremony.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(ceremony)
	start, end := strings.Index(text, "\n\tpublish)"), strings.Index(text, "\n\tsync-trust)")
	if start < 0 || end <= start {
		t.Fatal("local publication ceremony is missing")
	}
	publish := text[start:end]
	for _, required := range []string{"clean_main", "download_verify", "confirm ", "publish-stable-draft.sh", "HIKYO_RELEASE_TRUST_STATE"} {
		if !strings.Contains(publish, required) {
			t.Fatalf("local publication guard missing: %s", required)
		}
	}
	for _, forbidden := range []string{"workflow run", "sign-stable-draft.sh", "signer", "sign-blob"} {
		if strings.Contains(publish, forbidden) {
			t.Fatalf("local publication must not sign or delegate unsupported permissions: %s", forbidden)
		}
	}
}

// The unattended reconciler may publish, so it must run only from main, only
// when the owner has provisioned the App, and never from a tag-ref workflow.
func TestUnattendedReconcilerIsInertAndMainOnly(t *testing.T) {
	type step struct {
		Uses string            `yaml:"uses"`
		Run  string            `yaml:"run"`
		With map[string]string `yaml:"with"`
	}
	type job struct {
		If          string            `yaml:"if"`
		Env         string            `yaml:"environment"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []step            `yaml:"steps"`
	}
	var workflow struct {
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]job `yaml:"jobs"`
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "stable-release-reconciler.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.Permissions) != 0 {
		t.Fatal("the reconciler must grant no default workflow permissions")
	}
	if workflow.Concurrency.Group != "stable-release-reconciler" || workflow.Concurrency.Cancel {
		t.Fatal("the reconciler needs its own queued concurrency group, separate from the build")
	}
	if _, tags := workflow.On["create"]; tags {
		t.Fatal("the reconciler must not use a tag trigger")
	}
	var push struct {
		Branches []string `yaml:"branches"`
		Tags     []string `yaml:"tags"`
	}
	pushNode := workflow.On["push"]
	if err := pushNode.Decode(&push); err != nil || len(push.Tags) != 0 || len(push.Branches) != 1 || push.Branches[0] != "main" {
		t.Fatalf("the reconciler may push-trigger only on main: %v", err)
	}
	for name, j := range workflow.Jobs {
		for _, gate := range []string{"vars.STABLE_RELEASE_APP_CLIENT_ID != ''", "vars.STABLE_RELEASE_APP_BOT_ID != ''", "github.ref == 'refs/heads/main'"} {
			if !strings.Contains(j.If, gate) {
				t.Fatalf("job %s must stay inert until the owner provisions the App and run only on main: missing %s", name, gate)
			}
		}
		if name != "plan" && name != "issue" && !strings.Contains(j.If, "!inputs.dry_run") {
			t.Fatalf("job %s must not act on a dry run", name)
		}
		for key, level := range j.Permissions {
			if level == "write" && key != "issues" {
				t.Fatalf("job %s: the workflow token must stay read-only; App tokens do the writing", name)
			}
		}
		minted := false
		for _, s := range j.Steps {
			if strings.HasPrefix(s.Uses, "actions/create-github-app-token@") {
				minted = true
				if !regexp.MustCompile(`^actions/create-github-app-token@[0-9a-f]{40}$`).MatchString(s.Uses) {
					t.Fatalf("job %s: the token action must be pinned by SHA", name)
				}
				for _, required := range []string{"client-id", "private-key", "repositories"} {
					if s.With[required] == "" {
						t.Fatalf("job %s: the App token needs %s", name, required)
					}
				}
				hasPermission := false
				for key := range s.With {
					hasPermission = hasPermission || strings.HasPrefix(key, "permission-")
				}
				if !hasPermission {
					t.Fatalf("job %s: the App token must be down-scoped with permission-* inputs", name)
				}
			}
		}
		if minted && j.Env != "stable-release" {
			t.Fatalf("job %s mints a token and must use the main-only stable-release environment", name)
		}
		if !minted && j.Env != "" {
			t.Fatalf("job %s mints no token and needs no environment", name)
		}
	}
}
