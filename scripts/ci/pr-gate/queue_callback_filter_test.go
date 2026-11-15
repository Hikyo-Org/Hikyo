package main

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The publish job's `if:` and the Go callback check must accept the same
// ci-feedback workflow paths. A path Go accepts but the filter skips would
// silently drop that callback (admission would wait for the 15-minute cron); a
// path the filter lets through but Go refuses is a red run on the default
// branch's tip. dev's queue.go also accepts ci-feedback-retarget.yml, so this
// fails on the first dev backmerge or promotion until the filter is widened.
func TestPublishFilterAcceptsExactlyTheCallbackPathsTheQueueAccepts(t *testing.T) {
	raw, err := os.ReadFile("../../../.github/workflows/ci-queue-gate.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs struct {
			Publish struct {
				If string `yaml:"if"`
			} `yaml:"publish"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
	filter := map[string]bool{}
	for _, m := range regexp.MustCompile(`github\.event\.workflow_run\.path == '([^']*)'`).FindAllStringSubmatch(wf.Jobs.Publish.If, -1) {
		filter[m[1]] = true
	}
	if len(filter) == 0 {
		t.Fatalf("publish.if names no workflow_run.path: %q", wf.Jobs.Publish.If)
	}
	// Every path either side names, plus both known feedback workflows.
	universe := map[string]bool{feedbackWorkflowPath: true, retargetWorkflowPath: true}
	for p := range filter {
		universe[p] = true
	}
	for path := range universe {
		f := newQueueFixture(t)
		e := f.event
		e.Run = f.feedbackRun
		e.Run.Path = path
		err := f.q.execute(context.Background(), "workflow_run", e)
		accepted := err == nil || !strings.Contains(err.Error(), "invalid feedback callback")
		if accepted != filter[path] {
			t.Errorf("path %q: gate-queue accepts it = %t, publish.if lists it = %t; keep both in step", path, accepted, filter[path])
		}
	}
}
