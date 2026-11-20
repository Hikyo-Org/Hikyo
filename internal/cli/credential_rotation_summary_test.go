package cli_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

const rotationListPath = "/api/v1/orgs/org_70/projects/prj_70/environments/env_70/rotations"

func summaryRotation(id string, state apigen.CredentialRotationState, mutate func(*apigen.CredentialRotation)) apigen.CredentialRotation {
	r := apigen.CredentialRotation{Id: id, State: state}
	if mutate != nil {
		mutate(&r)
	}
	return r
}

func ptr[T any](v T) *T { return &v }

// list and show print the halt reason as a fixed word in the table and say, on
// stderr, in words why the server stopped the rotation. An unrecognised word is
// printed quoted and never explained away.
func TestRotationListNamesTheHaltReason(t *testing.T) {
	items := []apigen.CredentialRotation{
		summaryRotation("rot_a", apigen.CredentialRotationStateHalted, func(r *apigen.CredentialRotation) { r.HaltReason = ptr("retries_exhausted") }),
		summaryRotation("rot_b", apigen.CredentialRotationStateHalted, func(r *apigen.CredentialRotation) { r.HaltReason = ptr("authority_lost") }),
		summaryRotation("rot_c", apigen.CredentialRotationStateHalted, func(r *apigen.CredentialRotation) { r.HaltReason = ptr("gate_failed") }),
		summaryRotation("rot_d", apigen.CredentialRotationStateHalted, func(r *apigen.CredentialRotation) { r.HaltReason = ptr("unknown") }),
		summaryRotation("rot_e", apigen.CredentialRotationStateHalted, func(r *apigen.CredentialRotation) { r.HaltReason = ptr("brand_new\nword") }),
		summaryRotation("rot_f", apigen.CredentialRotationStateActive, nil),
	}
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		if r.URL.Path != rotationListPath {
			http.NotFound(rw, r)
			return
		}
		_ = jsonwire.Encode(rw, apigen.CredentialRotationList{Items: items})
	}))
	if code := cli.Run(t.Context(), ios, append([]string{"rotation", "list"}, rotationScope...)); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	for _, want := range []string{"HALT REASON", "retries_exhausted", "authority_lost", "gate_failed", `"brand_new\nword"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	out := stderr.String()
	for _, want := range []string{
		"HALTED: rotation rot_a is halted (retries_exhausted): the retry window ran out before a run could publish or extend the credential; it starts no run and does not extend the delivered credential until a person resumes it: hikyo rotation resume rot_a",
		"HALTED: rotation rot_b is halted (authority_lost): the recorded person no longer holds the access the rotation needs, or one of its keys or its environment is gone;",
		"HALTED: rotation rot_c is halted (gate_failed): a pre-publish gate refused: the environment became protected, a key changed or a draft appeared;",
		"HALTED: rotation rot_d is halted (unknown): the server halted the rotation without a reason it can name;",
		`HALTED: rotation rot_e is halted ("brand_new\nword"): the server recorded a reason this client does not know;`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(stdout.String(), "brand_new\nword") {
		t.Errorf("an unrecognised halt word reached the table raw:\n%s", stdout)
	}
	if strings.Contains(out, "rot_f") {
		t.Errorf("an active rotation was reported:\n%s", out)
	}
	if strings.Contains(out, "brand_new\nword") {
		t.Errorf("an unrecognised word reached the terminal raw:\n%s", out)
	}
}

// The rotation-level ATTENTION line counts the runs that owe a drop and points at
// the runs list; it never tells the operator to drop, and where a restore refused a
// drop it says not to.
func TestRotationListSummarisesOwedDrops(t *testing.T) {
	when := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	items := []apigen.CredentialRotation{
		summaryRotation("rot_none", apigen.CredentialRotationStateActive, func(r *apigen.CredentialRotation) {
			r.LastRun = &apigen.RotationLastRun{Id: "rrn_1", Phase: apigen.RotationRunPhaseDone, PhaseAt: when}
		}),
		summaryRotation("rot_owed", apigen.CredentialRotationStateActive, func(r *apigen.CredentialRotation) {
			r.OwedDropRuns = 2
			r.LastRun = &apigen.RotationLastRun{Id: "rrn_2", Phase: apigen.RotationRunPhaseRevokeRefused, ErrorCode: ptr("role_owns_objects"), PhaseAt: when}
		}),
		summaryRotation("rot_held", apigen.CredentialRotationStateActive, func(r *apigen.CredentialRotation) {
			r.RestoreHold = true
			r.OwedDropRuns = 1
		}),
		summaryRotation("rot_gone", apigen.CredentialRotationStateDisabled, func(r *apigen.CredentialRotation) { r.OwedDropRuns = 1 }),
		summaryRotation("rot_restore", apigen.CredentialRotationStateActive, func(r *apigen.CredentialRotation) {
			r.RestoreHold = true
			r.OwedDropRuns, r.OwedDropRunsVersionsChanged = 3, 1
			r.LastRun = &apigen.RotationLastRun{Id: "rrn_3", Phase: apigen.RotationRunPhaseRevokeRefused, ErrorCode: ptr("published_versions_changed"), PhaseAt: when}
		}),
	}
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_ = jsonwire.Encode(rw, apigen.CredentialRotationList{Items: items})
	}))
	if code := cli.Run(t.Context(), ios, append([]string{"rotation", "list"}, rotationScope...)); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	table := stdout.String()
	for _, want := range []string{"LAST RUN", "OWED DROPS", "done", "revoke_refused (role_owns_objects)", "revoke_refused (published_versions_changed)"} {
		if !strings.Contains(table, want) {
			t.Errorf("stdout lacks %q:\n%s", want, table)
		}
	}
	out := stderr.String()
	if strings.Contains(out, "rot_none") {
		t.Errorf("a rotation that owes nothing was flagged:\n%s", out)
	}
	owed := lineWith(out, "rot_owed")
	if !strings.Contains(owed, "ATTENTION: rotation rot_owed has 2 run(s) that owe a provider role a drop") || !strings.Contains(owed, "hikyo rotation runs rot_owed") {
		t.Errorf("owed line = %q", owed)
	}
	if strings.Contains(owed, "NOT") || strings.Contains(owed, "published_versions_changed") {
		t.Errorf("an ordinary owed drop was worded as a restore case: %q", owed)
	}
	// The key check the per-run line carries is on the rotation-level line too.
	const keyCheck = "check that neither key serves that role, because a revision restore can put it back"
	if !strings.Contains(owed, keyCheck) {
		t.Errorf("owed line lacks the key check: %q", owed)
	}
	// A restore hold that is still on: never a plain drop, whatever the error codes.
	held := lineWith(out, "ATTENTION: rotation rot_held")
	if !strings.Contains(held, "a restore holds the rotation") || !strings.Contains(held, "do NOT drop it at the provider until you have checked that neither key serves it") {
		t.Errorf("held line = %q", held)
	}
	// A deleted rotation says it is deleted; its warning is not lost with the delete.
	if gone := lineWith(out, "ATTENTION: rotation rot_gone"); !strings.Contains(gone, "rotation rot_gone (deleted) has 1 run(s)") || !strings.Contains(gone, keyCheck) {
		t.Errorf("deleted line = %q", gone)
	}
	restore := lineWith(out, "ATTENTION: rotation rot_restore")
	for _, want := range []string{"3 run(s) that owe a provider role a drop, 1 of them refused after a restore (published_versions_changed)", "do NOT drop it at the provider until the keys have moved off it", "hikyo rotation runs rot_restore"} {
		if !strings.Contains(restore, want) {
			t.Errorf("restore line lacks %q: %q", want, restore)
		}
	}
	// The per-run text lives in `rotation runs`, not in the list.
	if strings.Contains(out, "stays valid") || strings.Contains(out, "ATTENTION: run ") {
		t.Errorf("the per-run text was repeated in the list:\n%s", out)
	}
}

func lineWith(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

// show prints the rotation-level line once, before the per-run lines, and keeps the
// per-run lines (they carry the expiry the summary does not).
func TestRotationShowSummarisesOwedDrops(t *testing.T) {
	until := time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case rotationBase:
			_ = jsonwire.Encode(rw, apigen.CredentialRotation{Id: "rot_70", State: "halted", HaltReason: ptr("authority_lost"), OwedDropRuns: 1})
		case rotationBase + "/runs":
			_ = jsonwire.Encode(rw, apigen.RotationRunList{Items: []apigen.RotationRun{owedRun("rrn_9", apigen.RotationRunPhaseQuarantine, until, false)}})
		default:
			http.NotFound(rw, r)
		}
	}))
	args := append([]string{"rotation", "show", "rot_70"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	out := stderr.String()
	summary := strings.Index(out, "ATTENTION: rotation rot_70 has 1 run(s)")
	perRun := strings.Index(out, "ATTENTION: run rrn_9 is quarantine")
	if summary < 0 || perRun < 0 || summary > perRun || strings.Count(out, "ATTENTION: rotation rot_70") != 1 {
		t.Errorf("summary at %d, per-run at %d:\n%s", summary, perRun, out)
	}
	if !strings.Contains(out, "HALTED: rotation rot_70 is halted (authority_lost)") {
		t.Errorf("show lacks the halt line:\n%s", out)
	}
}

// A deleted rotation is `deleted` in the table, like its ATTENTION line, not the wire's
// `disabled`; a halted one always fills the HALT REASON cell (a missing reason reads as
// unknown, as on the HALTED line) and a live one never does; a run phase or error code
// outside the fixed words is printed quoted, never raw.
func TestRotationListStateHaltCellAndQuotedRunWords(t *testing.T) {
	items := []apigen.CredentialRotation{
		summaryRotation("rot_gone", apigen.CredentialRotationStateDisabled, nil),
		summaryRotation("rot_noreason", apigen.CredentialRotationStateHalted, nil),
		summaryRotation("rot_live", apigen.CredentialRotationStateActive, func(r *apigen.CredentialRotation) {
			r.HaltReason = ptr("authority_lost") // a live rotation's stale reason is not believed
			r.LastRun = &apigen.RotationLastRun{Id: "run_1", Phase: apigen.RotationRunPhase("done\x1b[31m"), ErrorCode: ptr("secret=hunter2")}
		}),
		summaryRotation("rot_known", apigen.CredentialRotationStateActive, func(r *apigen.CredentialRotation) {
			r.LastRun = &apigen.RotationLastRun{Id: "run_2", Phase: apigen.RotationRunPhaseRevokeRefused, ErrorCode: ptr("published_versions_changed")}
		}),
	}
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_ = jsonwire.Encode(rw, apigen.CredentialRotationList{Items: items})
	}))
	if code := cli.Run(t.Context(), ios, append([]string{"rotation", "list", "--include-disabled"}, rotationScope...)); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	out := stdout.String()
	if row := lineWith(out, "rot_gone"); !strings.Contains(row, "deleted") || strings.Contains(row, "disabled") {
		t.Errorf("a deleted rotation's row says %q, want deleted", row)
	}
	if row := lineWith(out, "rot_noreason"); !strings.Contains(row, "unknown") {
		t.Errorf("a halted rotation with no reason has no HALT REASON cell: %q", row)
	}
	if row := lineWith(out, "rot_live"); strings.Contains(row, "authority_lost") {
		t.Errorf("a live rotation printed a halt reason: %q", row)
	}
	if !strings.Contains(out, `"done\x1b[31m" ("secret=hunter2")`) {
		t.Errorf("an unknown phase and code were not quoted:\n%s", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("a control character reached stdout:\n%q", out)
	}
	if row := lineWith(out, "rot_known"); !strings.Contains(row, "revoke_refused (published_versions_changed)") {
		t.Errorf("a known phase and code were not printed bare: %q", row)
	}
}
