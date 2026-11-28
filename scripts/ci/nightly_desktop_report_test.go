package ci_test

import (
	"strings"
	"testing"
)

func reportScript(t *testing.T) string {
	t.Helper()
	steps := stepsOf(asMap(jobsOf(readYAMLMap(t, "nightly-desktop.yml"))["report"]))
	script, _ := steps[0]["run"].(string)
	return script
}

// Re-running the failed jobs of an older run reuses that attempt's results. A run that is not
// the newest nightly must never close an issue a newer red night keeps open.
func TestNightlyDesktopReportClosesOnlyFromTheNewestRun(t *testing.T) {
	script := reportScript(t)
	issues := `[{"number":7,"title":"ci: nightly desktop validation failing","author":{"login":"app/github-actions"},"body":"<!-- hikyo-nightly-desktop -->"}]`
	env := []string{"RUN_URL=u", "DEV_SHA=a", "MAIN_SHA=b", "DEV_RESULT=success", "MAIN_RESULT=success", "SHIM_RESULT=success", "SHIM_STATE=gone"}
	for name, tc := range map[string]struct {
		latest string
		closes bool
	}{"newest run": {"77", true}, "an older run": {"78", false}} {
		t.Run(name, func(t *testing.T) {
			run := runStubbed(t, script, map[string]string{"issues.ci-nightly-desktop.json": issues, "latest_run": tc.latest}, env...)
			if run.status != 0 {
				t.Fatalf("exit %d:\n%s", run.status, run.log)
			}
			if !strings.Contains(run.log, "run list --workflow nightly-desktop.yml --branch main --limit 1") {
				t.Fatalf("the newest run was never looked up:\n%s", run.log)
			}
			if closed := strings.Contains(run.log, "issue close 7 "); closed != tc.closes {
				t.Fatalf("closed=%v, want %v:\n%s", closed, tc.closes, run.log)
			}
		})
	}
}

// A failed desktop issue write must not hide a dead shim that night, and must still fail the run.
func TestNightlyDesktopReportReconcilesEachIssueIndependently(t *testing.T) {
	script := reportScript(t)
	run := runStubbed(t, script, map[string]string{"fail.create.ci-nightly-desktop": ""},
		"RUN_URL=u", "DEV_SHA=a", "MAIN_SHA=b", "DEV_RESULT=failure", "MAIN_RESULT=success", "SHIM_RESULT=success", "SHIM_STATE=dead")
	if run.status != 1 {
		t.Fatalf("exit %d, want 1:\n%s", run.status, run.log)
	}
	if !strings.Contains(run.log, "issue create --title ci: desktop-removal shim check (#1595)") {
		t.Fatalf("the shim issue was not created after the desktop write failed:\n%s", run.log)
	}
}
