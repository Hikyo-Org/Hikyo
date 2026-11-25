package benchmark

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const controlWorkflowPath = "../../../.github/workflows/benchmark-control.yml"
const commentGroup = "perf-control-comment-${{ github.event.pull_request.number }}"

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

func TestDuplicateMarkerCommentsKeepLowestID(t *testing.T) {
	head := strings.Repeat("a", 40)
	for name, order := range map[string][]int64{"ascending": {20, 21, 22}, "descending": {22, 21, 20}, "mixed": {21, 20, 22}} {
		t.Run(name, func(t *testing.T) {
			var page []comment
			for _, id := range order {
				page = append(page, botCopy(t, id, head))
			}
			log := fakeGH(t, map[string]string{
				"repos/o/r/issues/7/comments?per_page=100": encode(t, [][]comment{page}),
				"repos/o/r/issues/comments/21":             `{}`,
				"repos/o/r/issues/comments/22":             `{}`,
			})
			c, err := botComment("o/r", 7)
			if err != nil || c.ID != 20 {
				t.Fatalf("kept=%d error=%v", c.ID, err)
			}
			raw, _ := os.ReadFile(log)
			calls := string(raw)
			if !strings.Contains(calls, "--method DELETE repos/o/r/issues/comments/21") ||
				!strings.Contains(calls, "--method DELETE repos/o/r/issues/comments/22") ||
				strings.Contains(calls, "DELETE repos/o/r/issues/comments/20") {
				t.Fatalf("must delete only the higher ids:\n%s", calls)
			}
		})
	}
}
