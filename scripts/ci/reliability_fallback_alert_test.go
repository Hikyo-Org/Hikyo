package ci_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The fallback-channel alert is one issue owned by the workflow's own bot. The
// owner is bound by its numeric user id and type (gh spells the login
// `app/github-actions`, REST spells it `github-actions[bot]`, and a person can
// write the same title and marker), and a listing that fails must stop the step
// instead of reading as "no issue".
func TestReliabilityFallbackAlertBindsTheBotById(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "reliability.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	steps := map[string]string{}
	for _, step := range workflow.Jobs["fallback-channel"].Steps {
		steps[step.Name] = step.Run
	}
	surface, closeStep := steps["Surface fallback-channel degradation"], steps["Close resolved fallback-channel alert"]
	if surface == "" || closeStep == "" {
		t.Fatalf("the fallback-channel job lost its alert steps: %v", steps)
	}
	for name, run := range map[string]string{"surface": surface, "close": closeStep} {
		if strings.Contains(run, ".author.login") || strings.Contains(run, "gh issue list") {
			t.Fatalf("%s must not match the author by login spelling", name)
		}
	}

	title := "Fallback security channel health check failed"
	marker := "<!-- hikyo-security-channel-alert -->"
	listing := func(id int, kind string, pullRequest bool) string {
		pr := "null"
		if pullRequest {
			pr = `{"url":"x"}`
		}
		return `[[{"number":7,"title":"` + title + `","body":"` + marker + `\ntext","user":{"id":` + strconv.Itoa(id) + `,"type":"` + kind + `"},"pull_request":` + pr + `}]]`
	}
	cases := []struct {
		name    string
		listing string
		failGH  bool
		want    []string // substrings of the gh calls, in order
		refuse  []string
		failed  bool
	}{
		{name: "the bot's own issue is updated", listing: listing(41898282, "Bot", false), want: []string{"issue comment 7"}, refuse: []string{"issue create"}},
		{name: "a person's issue with the same title is not ours", listing: listing(5, "User", false), want: []string{"issue create"}, refuse: []string{"issue comment"}},
		{name: "another app with the same title is not ours", listing: listing(12345, "Bot", false), want: []string{"issue create"}, refuse: []string{"issue comment"}},
		{name: "a user with the bot's id is not the bot", listing: listing(41898282, "User", false), want: []string{"issue create"}, refuse: []string{"issue comment"}},
		{name: "a pull request is never the alert", listing: listing(41898282, "Bot", true), want: []string{"issue create"}, refuse: []string{"issue comment"}},
		{name: "a listing that fails stops the step", failGH: true, refuse: []string{"issue create", "issue comment"}, failed: true},
	}
	for _, tc := range cases {
		t.Run("surface: "+tc.name, func(t *testing.T) {
			calls, err := runAlertStep(t, surface, tc.listing, tc.failGH)
			if (err != nil) != tc.failed {
				t.Fatalf("failure = %v, want %v\n%s", err, tc.failed, calls)
			}
			for _, want := range tc.want {
				if !strings.Contains(calls, want) {
					t.Fatalf("missing gh call %q:\n%s", want, calls)
				}
			}
			for _, refused := range tc.refuse {
				if strings.Contains(calls, refused) {
					t.Fatalf("unexpected gh call %q:\n%s", refused, calls)
				}
			}
		})
	}
	t.Run("close: only the bot's own alert is closed", func(t *testing.T) {
		calls, err := runAlertStep(t, closeStep, listing(41898282, "Bot", false), false)
		if err != nil || !strings.Contains(calls, "issue close 7") {
			t.Fatalf("the bot's alert must be closed: %v\n%s", err, calls)
		}
		calls, err = runAlertStep(t, closeStep, listing(5, "User", false), false)
		if err != nil || strings.Contains(calls, "issue close") {
			t.Fatalf("a person's issue must not be closed: %v\n%s", err, calls)
		}
		calls, err = runAlertStep(t, closeStep, listing(12345, "Bot", false), false)
		if err != nil || strings.Contains(calls, "issue close") {
			t.Fatalf("another app's issue must not be closed: %v\n%s", err, calls)
		}
		if _, err = runAlertStep(t, closeStep, "", true); err == nil {
			t.Fatal("a listing that fails must stop the step")
		}
	})
}

// runAlertStep runs one workflow step's script in a scratch directory with a fake
// gh (the REST listing comes from listing; every call is recorded) and returns the
// recorded gh calls.
func runAlertStep(t *testing.T, script, listing string, failListing bool) (string, error) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "listing.json"), []byte(listing), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
printf '%s\n' "$*" >>"$FIXTURE/calls"
case "$*" in
	'api --paginate --slurp repos/o/r/issues?state=open&labels=security-channel-alert&creator=github-actions%5Bbot%5D&per_page=100')
		[ -z "$FAIL_LISTING" ] || { echo 'gh: server error (HTTP 500)' >&2; exit 1; }
		cat "$FIXTURE/listing.json" ;;
	'label create '* | 'issue comment '* | 'issue create '* | 'issue close '*) ;;
	*) echo "unexpected gh call: $*" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "calls"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	failFlag := ""
	if failListing {
		failFlag = "1"
	}
	// GitHub runs a step with `bash -e`; do the same.
	command := exec.CommandContext(context.Background(), "bash", "-e", "-c", script)
	command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FIXTURE="+dir,
		"FAIL_LISTING="+failFlag, "GH_REPO=o/r", "GH_TOKEN=t", "RUN_URL=https://github.com/o/r/actions/runs/1")
	output, err := command.CombinedOutput()
	calls, readErr := os.ReadFile(filepath.Join(dir, "calls"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(calls) + string(output), err
}
