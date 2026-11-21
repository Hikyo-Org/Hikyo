package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Shell fixtures of the desktop release lane (ADR D13), run from the workflow-test
// package because ci.yml is a protected CI definition.
func TestDesktopReleaseFixtures(t *testing.T) {
	for _, script := range []string{
		"build-desktop-app_test.sh", "check-desktop-rulesets_test.sh",
		"publish-desktop-draft_test.sh", "stage-desktop-draft_test.sh",
	} {
		t.Run(script, func(t *testing.T) {
			cmd := exec.Command("sh", filepath.Join(repositoryRoot(t), "scripts", "release", script))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}

type desktopStep struct {
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Uses string            `yaml:"uses"`
	If   string            `yaml:"if"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}

type desktopJob struct {
	Env         map[string]string `yaml:"env"`
	Needs       yaml.Node         `yaml:"needs"`
	If          string            `yaml:"if"`
	RunsOn      string            `yaml:"runs-on"`
	Environment string            `yaml:"environment"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []desktopStep     `yaml:"steps"`
}

func readDesktopWorkflow(t *testing.T, name string) map[string]desktopJob {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]desktopJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow.Jobs
}

func needsOf(t *testing.T, job desktopJob) []string {
	t.Helper()
	var needs []string
	if job.Needs.Kind == yaml.ScalarNode {
		return []string{job.Needs.Value}
	}
	if err := job.Needs.Decode(&needs); err != nil {
		t.Fatal(err)
	}
	return needs
}

// The desktop series has no SemVer v tag, so its release event is skipped before any step.
func TestSecurityReleaseSkipsDesktopReleases(t *testing.T) {
	resolve := readDesktopWorkflow(t, "security-release.yml")["resolve"]
	for _, required := range []string{"github.event_name != 'release'", "!startsWith(github.event.release.tag_name, 'desktop-v')"} {
		if !strings.Contains(resolve.If, required) {
			t.Fatalf("security-release resolve job must skip desktop-v releases at job level, missing %q in %q", required, resolve.If)
		}
	}
	validate := readDesktopWorkflow(t, "security-release.yml")["validate"]
	if got := needsOf(t, validate); len(got) != 1 || got[0] != "resolve" {
		t.Fatalf("validate must depend only on the guarded resolve job, got %v", got)
	}
}

// The only desktop steps that may hold credentials, each pinned to its exact
// command so a chained run: cannot add another stage (for example notarization)
// to a signing step. Credential groups never share a step.
var desktopCredentialSteps = map[string]string{
	"Sign the app with the hardened runtime": `./scripts/release/build-desktop-app.sh sign-app "$RUNNER_TEMP/desktop/Hikyo.app"`,
	"Notarize and staple the app":            `./scripts/release/with-apple-ticket-network.sh ./scripts/release/build-desktop-app.sh notarize "$RUNNER_TEMP/desktop/Hikyo.app"`,
	"Sign the disk image":                    `./scripts/release/build-desktop-app.sh sign-image "$RUNNER_TEMP/desktop-dmg/Hikyo-Desktop-$VERSION-$ARCH.dmg"`,
	"Notarize and staple the disk image":     `./scripts/release/with-apple-ticket-network.sh ./scripts/release/build-desktop-app.sh notarize "$RUNNER_TEMP/desktop-dmg/Hikyo-Desktop-$VERSION-$ARCH.dmg"`,
}

// Any mention of the secrets context, however spelled (secrets.X, secrets['X'],
// toJSON(secrets), format strings), anywhere an expression can appear.
var secretsWord = regexp.MustCompile(`\bsecrets\b`)

// A credentialed step may pass a secret only as its own, exactly named env value.
var ownSecret = regexp.MustCompile(`^\$\{\{ secrets\.([A-Za-z0-9_]+) \}\}$`)

// Which credential group an env key belongs to, by name, so no key escapes by
// being outside a hard-coded list.
func credentialGroup(key, value string) string {
	switch {
	case strings.HasPrefix(key, "APPLE_NOTARY_KEY_P8"):
		return "notary"
	case strings.HasPrefix(key, "APPLE_DEVELOPER_ID_"):
		return "signing"
	case secretsWord.MatchString(value):
		return "other"
	}
	return ""
}

func TestDesktopReleaseLaneStaysStableOnlyAndCredentialScoped(t *testing.T) {
	jobs := readDesktopWorkflow(t, "release.yml")
	for name, job := range jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Uses, "goreleaser/goreleaser-action@") && step.Env["GORELEASER_CURRENT_TAG"] != "${{ github.ref_name }}" {
				t.Fatalf("%s: GoReleaser must pin its tag, two tags share a commit", name)
			}
		}
	}
	app, draft := jobs["desktop-app"], jobs["desktop-draft"]
	const stableOnly = "needs.build-artifacts.outputs.channel == 'stable'"
	if app.If != stableOnly || draft.If != stableOnly {
		t.Fatal("desktop jobs must run for the stable channel only")
	}
	if app.RunsOn != "macos-15" || app.Environment != "apple-signing" || len(app.Permissions) != 1 || app.Permissions["contents"] != "read" {
		t.Fatal("desktop signing must use the apple-signing environment on macOS with read-only repository access")
	}
	if len(draft.Permissions) != 2 || draft.Permissions["contents"] != "write" || draft.Permissions["id-token"] != "write" {
		t.Fatal("desktop draft needs exactly contents write and OIDC")
	}
	if !contains(needsOf(t, draft), "desktop-app") || !contains(needsOf(t, app), "build-artifacts") {
		t.Fatal("desktop draft must wait for the desktop build, which waits for validation")
	}
	for _, dependent := range []string{"desktop-app", "desktop-draft"} {
		if contains(needsOf(t, jobs["build-signed-draft"]), dependent) {
			t.Fatalf("build-signed-draft must not depend on %s", dependent)
		}
	}
	for name, job := range jobs {
		for key, value := range job.Env {
			if secretsWord.MatchString(value) {
				t.Fatalf("%s: job-level env %s carries a secret to every step", name, key)
			}
		}
	}
	// Neither desktop job may mention secrets outside the pinned credentialed
	// steps' env: not in run, if, with, other env, or the job condition.
	for _, name := range []string{"desktop-app", "desktop-draft"} {
		if secretsWord.MatchString(jobs[name].If) {
			t.Fatalf("%s: job condition mentions secrets", name)
		}
		for _, step := range jobs[name].Steps {
			_, pinned := desktopCredentialSteps[step.Name]
			pinned = pinned && name == "desktop-app"
			for field, text := range map[string]string{"run": step.Run, "if": step.If, "name": step.Name, "uses": step.Uses} {
				if secretsWord.MatchString(text) {
					t.Fatalf("%s step %q mentions secrets in its %s", name, step.Name, field)
				}
			}
			for key, value := range step.With {
				if secretsWord.MatchString(value) {
					t.Fatalf("%s step %q passes secrets through with: %s", name, step.Name, key)
				}
			}
			for key, value := range step.Env {
				if !secretsWord.MatchString(value) {
					continue
				}
				match := ownSecret.FindStringSubmatch(value)
				if !pinned || match == nil || match[1] != key {
					t.Fatalf("%s step %q env %s must not carry secrets (only a pinned credentialed step's own named secret)", name, step.Name, key)
				}
			}
		}
	}
	var run strings.Builder
	for _, step := range app.Steps {
		run.WriteString(step.Run + "\n")
		groups := map[string]bool{}
		for key, value := range step.Env {
			if group := credentialGroup(key, value); group != "" {
				groups[group] = true
			}
		}
		for _, value := range step.With {
			if strings.Contains(value, "secrets.") {
				t.Fatalf("step %q passes a secret through with:", step.Name)
			}
		}
		if len(groups) > 1 {
			t.Fatalf("step %q holds signing and notary credentials together", step.Name)
		}
		if len(groups) == 1 {
			want, pinned := desktopCredentialSteps[step.Name]
			if !pinned || step.Run != want || groups["other"] {
				t.Fatalf("credentialed step %q is not one of the pinned credential steps with its exact command", step.Name)
			}
			if (strings.Contains(want, " notarize ") && !groups["notary"]) || (strings.Contains(want, " sign-") && !groups["signing"]) {
				t.Fatalf("step %q holds the wrong credential group", step.Name)
			}
		}
		if step.If != "" && strings.Contains(step.Run, "build-desktop-app.sh") {
			t.Fatalf("step %q makes a signing stage conditional", step.Name)
		}
	}
	for name, want := range desktopCredentialSteps {
		found := false
		for _, step := range app.Steps {
			found = found || (step.Name == name && step.Run == want)
		}
		if !found {
			t.Fatalf("pinned credential step %q is missing or changed", name)
		}
	}
	// The stages run in this order, ending with the Gatekeeper verification.
	order := []string{"Build the unsigned app", "Sign the app with the hardened runtime", "Notarize and staple the app",
		"Build the disk image from the stapled app", "Sign the disk image", "Notarize and staple the disk image", "Verify signature, ticket and Gatekeeper"}
	next := 0
	for _, step := range app.Steps {
		if next < len(order) && step.Name == order[next] {
			next++
		}
	}
	if next != len(order) {
		t.Fatalf("desktop-app stages out of order or missing at %q", order[next])
	}
	for _, forbidden := range []string{"--deep", "wails3", "task package", "codesign --force --sign -"} {
		if strings.Contains(run.String(), forbidden) {
			t.Fatalf("desktop release must not use %s", forbidden)
		}
	}
	// Artifacts are handed over by exact, attempt-qualified names with recorded digests.
	for _, step := range app.Steps {
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") && strings.HasPrefix(step.With["name"], "desktop-dmg-") &&
			step.With["name"] != "desktop-dmg-${{ matrix.arch }}-${{ github.run_attempt }}" {
			t.Fatalf("desktop disk images must be named by architecture and run attempt, got %q", step.With["name"])
		}
	}
	downloads := 0
	var stage strings.Builder
	for _, step := range draft.Steps {
		stage.WriteString(step.Run + "\n")
		if strings.HasPrefix(step.Uses, "actions/download-artifact@") {
			downloads++
			if step.With["pattern"] != "" || !strings.HasPrefix(step.With["name"], "${{ needs.desktop-app.outputs.artifact_") {
				t.Fatalf("desktop draft must download exact recorded artifact names, got %v", step.With)
			}
		}
		if strings.Contains(step.Run, "stage-desktop-draft.sh") && (step.Env["DESKTOP_SHA256_ARM64"] == "" || step.Env["DESKTOP_SHA256_X86_64"] == "") {
			t.Fatal("the staging step must receive both recorded disk image digests")
		}
	}
	if downloads != 2 {
		t.Fatalf("desktop draft must download exactly the two recorded artifacts, got %d", downloads)
	}
	if !strings.Contains(stage.String(), "stage-desktop-draft.sh") || strings.Contains(stage.String(), "--verify-tag") {
		t.Fatal("the draft job must stage through the script and must not require an existing tag")
	}
}

// GoReleaser ignores the desktop series in every run, snapshot or tagged.
func TestGoReleaserIgnoresDesktopTags(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Git struct {
			IgnoreTags []string `yaml:"ignore_tags"`
		} `yaml:"git"`
	}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if !contains(config.Git.IgnoreTags, "desktop-v*") {
		t.Fatalf("git.ignore_tags must hold desktop-v*, got %v", config.Git.IgnoreTags)
	}
}

// Publication of the desktop draft follows the CLI release in the ceremony.
func TestCeremonyPublishesDesktopAfterTheCLIRelease(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "release", "stable-ceremony.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start, end := strings.Index(text, "\n\tpublish)"), strings.Index(text, "\n\tsync-trust)")
	if start < 0 || end <= start {
		t.Fatal("publish ceremony is missing")
	}
	publish := text[start:end]
	cli, desktop := strings.Index(publish, "publish-stable-draft.sh"), strings.LastIndex(publish, "publish-desktop-draft.sh")
	gate := strings.Index(publish, "desktop_gate")
	if gate < 0 || gate > cli || !strings.Contains(text, "publish-desktop-draft.sh\" --check-only") {
		t.Fatal("the desktop draft must be checked before the CLI release is published")
	}
	if cli < 0 || desktop < cli || !strings.Contains(publish, "release-channel.sh") {
		t.Fatal("the desktop draft must be published after publish-stable-draft.sh, for the stable channel only")
	}
}
