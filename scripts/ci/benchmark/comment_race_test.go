package benchmark

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const controlWorkflowPath = "../../../.github/workflows/benchmark-control.yml"
const commentGroup = "perf-control-comment-${{ github.event.pull_request.number || github.run_id }}"

type controlWorkflow struct {
	Jobs map[string]struct {
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
	} `yaml:"jobs"`
}

func loadControlWorkflow(t *testing.T) controlWorkflow {
	t.Helper()
	raw, err := os.ReadFile(controlWorkflowPath)
	if err != nil {
		t.Fatal(err)
	}
	var w controlWorkflow
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func checkControlConcurrency(w controlWorkflow) string {
	for _, name := range []string{"automatic", "request"} {
		if w.Jobs[name].Concurrency.Group != "codspeed-walltime" {
			return name + " must stay in the global codspeed-walltime group"
		}
	}
	c := w.Jobs["comment"].Concurrency
	if c.Group != commentGroup || !c.CancelInProgress {
		return "comment must use a cancelling per-PR group"
	}
	return ""
}

func setGroup(w *controlWorkflow, job, group string, cancel bool) {
	j := w.Jobs[job]
	j.Concurrency.Group = group
	j.Concurrency.CancelInProgress = cancel
	w.Jobs[job] = j
}

func TestControlWorkflowConcurrencyGroups(t *testing.T) {
	if msg := checkControlConcurrency(loadControlWorkflow(t)); msg != "" {
		t.Fatal(msg)
	}
	for name, mutate := range map[string]func(*controlWorkflow){
		"comment back in global group": func(w *controlWorkflow) { setGroup(w, "comment", "codspeed-walltime", false) },
		"comment not cancelling":       func(w *controlWorkflow) { setGroup(w, "comment", commentGroup, false) },
		"automatic leaves global":      func(w *controlWorkflow) { setGroup(w, "automatic", "other", false) },
		"request leaves global":        func(w *controlWorkflow) { setGroup(w, "request", "other", false) },
	} {
		t.Run(name, func(t *testing.T) {
			m := loadControlWorkflow(t)
			mutate(&m)
			if checkControlConcurrency(m) == "" {
				t.Fatal("mutation was not detected")
			}
		})
	}
}

func botCopy(t *testing.T, id int64, head string) comment {
	t.Helper()
	c := testComment(t, head, request{head: head})
	c.ID = id
	return c
}

func TestPrepareUpdatesAnExistingMarkerCommentInsteadOfCreating(t *testing.T) {
	head, old := strings.Repeat("a", 40), strings.Repeat("b", 40)
	log := fakeGH(t, map[string]string{
		"repos/o/r/pulls/7":                        `{"number":7,"state":"open","head":{"sha":"` + head + `"}}`,
		"repos/o/r/issues/7/comments?per_page=100": encode(t, [][]comment{{botCopy(t, 20, old)}}),
		"repos/o/r/issues/comments/20":             `{}`,
	})
	if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(log)
	if strings.Contains(string(raw), "--method POST") || !strings.Contains(string(raw), "--method PATCH") {
		t.Fatalf("existing marker comment must be updated, not duplicated:\n%s", raw)
	}
}

// sequenceGH serves each listing in order (the last repeats), a PR, and
// answers every other call with {}. DELETE of the notFound id returns HTTP 404.
func sequenceGH(t *testing.T, head string, listings []string, notFound string) string {
	t.Helper()
	return sequenceGHFailingDelete(t, head, listings, notFound, "Not Found (HTTP 404)")
}

// sequenceGHFailingDelete is sequenceGH with the failing DELETE's gh message.
func sequenceGHFailingDelete(t *testing.T, head string, listings []string, notFound, message string) string {
	t.Helper()
	dir := t.TempDir()
	for i, l := range listings {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("list-%d", i)), []byte(l), 0600); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(dir, "calls")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>'%[1]s'
case "$*" in
*"--method DELETE"*comments/%[3]s) echo "gh: %[6]s" >&2; exit 1;;
*issues/7/comments?per_page=100*)
 n=$(cat '%[2]s/n' 2>/dev/null || echo 0); last=%[4]d
 [ "$n" -gt "$last" ] && n=$last
 echo $((n+1)) >'%[2]s/n'; cat "%[2]s/list-$n";;
*repos/o/r/pulls/7) echo '{"number":7,"state":"open","head":{"sha":"%[5]s"}}';;
*) echo '{}';;
esac
`, log, dir, notFound, len(listings)-1, head, message)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func recorded(t *testing.T, log string) string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestVerifyRequestWithDuplicatesIsReadOnly(t *testing.T) {
	head := strings.Repeat("a", 40)
	log := sequenceGH(t, head, []string{encode(t, [][]comment{{botCopy(t, 22, head), botCopy(t, 20, head), botCopy(t, 21, head)}})}, "none")
	c, dups, err := botComments("o/r", 7)
	if err != nil || c.ID != 20 || len(dups) != 2 {
		t.Fatalf("kept=%d dups=%d error=%v", c.ID, len(dups), err)
	}
	e := controlEvent{}
	e.Pull.Number = 7
	e.Pull.Head.SHA = head
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_RUN_ID", "10")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	output := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(output, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_OUTPUT", output)
	if err := verifyRequest("o/r", e); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorded(t, log), "DELETE") {
		t.Fatal("read-only paths must never delete")
	}
}

func TestPrepareDeletesHigherDuplicatesAtEntry(t *testing.T) {
	head := strings.Repeat("a", 40)
	log := sequenceGH(t, head, []string{encode(t, [][]comment{{botCopy(t, 21, head), botCopy(t, 20, head)}})}, "none")
	if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
		t.Fatal(err)
	}
	c := recorded(t, log)
	if !strings.Contains(c, "--method DELETE repos/o/r/issues/comments/21") || strings.Contains(c, "DELETE repos/o/r/issues/comments/20") {
		t.Fatalf("must delete only the higher id:\n%s", c)
	}
}

func TestPrepareRereadsAfterPostAndDeletesRacedDuplicate(t *testing.T) {
	head := strings.Repeat("a", 40)
	log := sequenceGH(t, head, []string{`[[]]`, encode(t, [][]comment{{botCopy(t, 20, head), botCopy(t, 21, head)}})}, "none")
	if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
		t.Fatal(err)
	}
	c := recorded(t, log)
	if !strings.Contains(c, "--method POST") || !strings.Contains(c, "--method DELETE repos/o/r/issues/comments/21") || strings.Contains(c, "DELETE repos/o/r/issues/comments/20") {
		t.Fatalf("post-create re-read must delete the higher id only:\n%s", c)
	}
}

func TestPrepareToleratesAlreadyDeletedDuplicate(t *testing.T) {
	head := strings.Repeat("a", 40)
	log := sequenceGH(t, head, []string{encode(t, [][]comment{{botCopy(t, 20, head), botCopy(t, 21, head)}})}, "21")
	if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
		t.Fatalf("404 on a duplicate DELETE must succeed: %v", err)
	}
	if !strings.Contains(recorded(t, log), "DELETE repos/o/r/issues/comments/21") {
		t.Fatal("expected a delete attempt")
	}
}

func TestPrepareFailsOnAForbiddenDuplicateDelete(t *testing.T) {
	head := strings.Repeat("a", 40)
	sequenceGHFailingDelete(t, head, []string{encode(t, [][]comment{{botCopy(t, 20, head), botCopy(t, 21, head)}})}, "21", "Resource not accessible by integration (HTTP 403)")
	err := prepare("o/r", controlEvent{Number: 7})
	if err == nil || errors.Is(err, errNotFound) {
		t.Fatalf("a 403 on a duplicate DELETE must fail prepare, got %v", err)
	}
	if strings.Contains(err.Error(), "Resource not accessible") {
		t.Fatalf("gh stderr must stay out of error text (decline posts it): %v", err)
	}
}

func TestPrepareReconcilesARacedSurvivorFromAnOlderHead(t *testing.T) {
	live, old := strings.Repeat("a", 40), strings.Repeat("b", 40)
	survivor := botCopy(t, 20, old)
	log := sequenceGH(t, live, []string{`[[]]`, encode(t, [][]comment{{survivor, botCopy(t, 21, live)}})}, "none")
	if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
		t.Fatal(err)
	}
	c := recorded(t, log)
	if !strings.Contains(c, "--method DELETE repos/o/r/issues/comments/21") || !strings.Contains(patchLine(c, 20), "head="+live) {
		t.Fatalf("an older-head survivor must be reset to the live head:\n%s", c)
	}
}

// patchLine returns the recorded PATCH call for comment id, or "".
func patchLine(calls string, id int) string {
	for _, line := range strings.Split(calls, "\n") {
		if strings.Contains(line, fmt.Sprintf("--method PATCH repos/o/r/issues/comments/%d ", id)) {
			return line
		}
	}
	return ""
}

// laterPullRead makes every PR read after the first answer with later, a
// shell case body (for example a JSON echo or a failing exit).
func laterPullRead(t *testing.T, log, live, later string) {
	t.Helper()
	gh := filepath.Join(filepath.Dir(log), "gh")
	script, err := os.ReadFile(gh)
	if err != nil {
		t.Fatal(err)
	}
	first := fmt.Sprintf(`echo '{"number":7,"state":"open","head":{"sha":"%s"}}'`, live)
	moved := fmt.Sprintf(`*repos/o/r/pulls/7) if [ -f '%[1]s.pr' ]; then %[2]s; else : >'%[1]s.pr'; %[3]s; fi;;`, log, later, first)
	lines := strings.Split(string(script), "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "*repos/o/r/pulls/7)") {
			lines[i], replaced = moved, true
		}
	}
	if !replaced {
		t.Fatal("fake gh has no PR case to replace")
	}
	if err := os.WriteFile(gh, []byte(strings.Join(lines, "\n")), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareDoesNotResetASurvivorOnceTheHeadMoved(t *testing.T) {
	live, old, newer := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	for name, later := range map[string]string{
		"moved":  fmt.Sprintf(`echo '{"number":7,"state":"open","head":{"sha":"%s"}}'`, newer),
		"closed": fmt.Sprintf(`echo '{"number":7,"state":"closed","head":{"sha":"%s"}}'`, live),
	} {
		t.Run(name, func(t *testing.T) {
			log := sequenceGH(t, live, []string{`[[]]`, encode(t, [][]comment{{botCopy(t, 20, old), botCopy(t, 21, live)}})}, "none")
			laterPullRead(t, log, live, later)
			if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
				t.Fatal(err)
			}
			if c := recorded(t, log); patchLine(c, 20) != "" {
				t.Fatalf("a reset must not land once the PR %s:\n%s", name, c)
			}
		})
	}
}

func TestPrepareReportsAFailedLiveHeadRead(t *testing.T) {
	live, old := strings.Repeat("a", 40), strings.Repeat("b", 40)
	log := sequenceGH(t, live, []string{`[[]]`, encode(t, [][]comment{{botCopy(t, 20, old), botCopy(t, 21, live)}})}, "none")
	laterPullRead(t, log, live, `echo 'gh: Server Error (HTTP 502)' >&2; exit 1`)
	if err := prepare("o/r", controlEvent{Number: 7}); err == nil {
		t.Fatal("a failed live-head read must fail prepare, not report success")
	}
	if c := recorded(t, log); patchLine(c, 20) != "" {
		t.Fatalf("no reset may land without a confirmed live head:\n%s", c)
	}
}

func TestPrepareKeepsARacedSurvivorOnTheLiveHead(t *testing.T) {
	live := strings.Repeat("a", 40)
	admitted := testComment(t, live, request{head: live, run: 10, attempt: 2})
	admitted.ID = 20
	log := sequenceGH(t, live, []string{`[[]]`, encode(t, [][]comment{{admitted, botCopy(t, 21, live)}})}, "none")
	if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
		t.Fatal(err)
	}
	if c := recorded(t, log); strings.Contains(c, "--method PATCH") {
		t.Fatalf("an admitted request on the live head must be preserved:\n%s", c)
	}
}

func TestPrepareToleratesADuplicateDeletedBeforeALaterOne(t *testing.T) {
	head := strings.Repeat("a", 40)
	log := sequenceGH(t, head, []string{encode(t, [][]comment{{botCopy(t, 20, head), botCopy(t, 21, head), botCopy(t, 22, head)}})}, "21")
	if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
		t.Fatalf("a 404 on the first duplicate must not stop cleanup: %v", err)
	}
	if !strings.Contains(recorded(t, log), "DELETE repos/o/r/issues/comments/22") {
		t.Fatal("the duplicate after an already-deleted one must still be deleted")
	}
}

func TestPrepareToleratesAResetOfAnAlreadyDeletedSurvivor(t *testing.T) {
	live, old := strings.Repeat("a", 40), strings.Repeat("b", 40)
	log := sequenceGH(t, live, []string{encode(t, [][]comment{{botCopy(t, 20, old)}})}, "none")
	gh := filepath.Join(filepath.Dir(log), "gh")
	script, err := os.ReadFile(gh)
	if err != nil {
		t.Fatal(err)
	}
	fail := "case \"$*\" in\n*\"--method PATCH\"*comments/20*) echo 'gh: Not Found (HTTP 404)' >&2; exit 1;;\n"
	if err := os.WriteFile(gh, []byte(strings.Replace(string(script), "case \"$*\" in\n", fail, 1)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := prepare("o/r", controlEvent{Number: 7}); err != nil {
		t.Fatalf("a 404 resetting a comment another cleaner deleted must succeed: %v", err)
	}
	if patchLine(recorded(t, log), 20) == "" {
		t.Fatal("expected a reset attempt")
	}
}
