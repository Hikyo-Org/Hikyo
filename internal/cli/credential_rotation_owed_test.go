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

func owedRun(id string, phase apigen.RotationRunPhase, until time.Time, lowerBound bool) apigen.RotationRun {
	run := apigen.RotationRun{
		Id: id, Trigger: apigen.RotationRunTriggerSchedule, Phase: phase,
		StartedAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), PhaseAt: time.Date(2030, 1, 2, 4, 4, 5, 0, time.UTC),
	}
	if !until.IsZero() {
		run.CredentialValidUntil = &until
	}
	if lowerBound {
		run.CredentialValidUntilLowerBound = &lowerBound
	}
	return run
}

// show names every owed run across pages, with the lower-bound wording when an
// extend is unsettled, and stays silent about runs that owe nothing.
func TestRotationShowPagesThroughOwedRuns(t *testing.T) {
	until := time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)
	var runCalls []string
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == rotationBase:
			_ = jsonwire.Encode(rw, apigen.CredentialRotation{Id: "rot_70", State: "active"})
		case r.URL.Path == rotationBase+"/runs":
			runCalls = append(runCalls, r.URL.RawQuery)
			if r.URL.Query().Get("after") == "" {
				next := "rrn_2"
				_ = jsonwire.Encode(rw, apigen.RotationRunList{
					Items:     []apigen.RotationRun{owedRun("rrn_1", apigen.RotationRunPhaseDone, time.Time{}, false), owedRun("rrn_2", apigen.RotationRunPhaseRevokeRefused, until, false)},
					NextAfter: &next,
				})
				return
			}
			_ = jsonwire.Encode(rw, apigen.RotationRunList{Items: []apigen.RotationRun{
				owedRun("rrn_3", apigen.RotationRunPhaseQuarantine, until, true),
				owedRun("rrn_4", apigen.RotationRunPhaseQuarantine, time.Time{}, false),
			}})
		default:
			http.NotFound(rw, r)
		}
	}))
	args := append([]string{"rotation", "show", "rot_70"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if len(runCalls) != 2 || !strings.Contains(runCalls[1], "after=rrn_2") {
		t.Fatalf("runs requests = %v", runCalls)
	}
	out := stderr.String()
	for _, want := range []string{
		"ATTENTION: run rrn_2 is revoke_refused",
		"stays valid until 2030-02-03T04:05:06Z)",
		"ATTENTION: run rrn_3 is quarantine",
		"stays valid at least until 2030-02-03T04:05:06Z, and possibly longer",
		"ATTENTION: run rrn_4 is quarantine and a provider role it owes a drop is still live (no recorded expiry); before you drop it at the provider, check that neither key serves that role",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "rrn_1") {
		t.Errorf("a settled run was reported:\n%s", out)
	}
}

// A failure of only the runs read warns on stderr and never fails the show.
func TestRotationShowWarnsWhenRunsCannotBeRead(t *testing.T) {
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		if r.URL.Path == rotationBase {
			_ = jsonwire.Encode(rw, apigen.CredentialRotation{Id: "rot_70", State: "active"})
			return
		}
		http.Error(rw, "boom", http.StatusInternalServerError)
	}))
	args := append([]string{"rotation", "show", "rot_70"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("show failed on a runs error: exit %d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr.String(), "WARNING: the runs could not be read") || !strings.Contains(stdout.String(), "rot_70") {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

// A history longer than the page bound says so rather than implying it was all checked.
func TestRotationShowSaysWhenTheRunHistoryIsTruncated(t *testing.T) {
	calls := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		if r.URL.Path == rotationBase {
			_ = jsonwire.Encode(rw, apigen.CredentialRotation{Id: "rot_70", State: "active"})
			return
		}
		calls++
		next := "rrn_more"
		_ = jsonwire.Encode(rw, apigen.RotationRunList{Items: []apigen.RotationRun{}, NextAfter: &next})
	}))
	args := append([]string{"rotation", "show", "rot_70"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if calls != 20 || !strings.Contains(stderr.String(), "only the newest 10000 runs were checked") {
		t.Fatalf("calls=%d stderr=%q", calls, stderr)
	}
}

// A run refused because a restore put the superseded role back in service must
// not tell the operator to drop it; every other refusal may be dropped, but only
// after checking that neither key serves the role.
func TestRotationOwedRunRestoredRoleIsNotToBeDropped(t *testing.T) {
	until := time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)
	withCode := func(id string, phase apigen.RotationRunPhase, code string) apigen.RotationRun {
		run := owedRun(id, phase, until, false)
		run.ErrorCode = &code
		return run
	}
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case rotationBase:
			_ = jsonwire.Encode(rw, apigen.CredentialRotation{Id: "rot_70", State: "active"})
		case rotationBase + "/runs":
			_ = jsonwire.Encode(rw, apigen.RotationRunList{Items: []apigen.RotationRun{
				withCode("rrn_restored", apigen.RotationRunPhaseRevokeRefused, "published_versions_changed"),
				withCode("rrn_restored_q", apigen.RotationRunPhaseQuarantine, "published_versions_changed"),
				withCode("rrn_objects", apigen.RotationRunPhaseRevokeRefused, "role_owns_objects"),
				withCode("rrn_down", apigen.RotationRunPhaseRevokeRefused, "provider_unavailable"),
			}})
		default:
			http.NotFound(rw, r)
		}
	}))
	args := append([]string{"rotation", "show", "rot_70"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	lines := map[string]string{}
	for _, line := range strings.Split(stderr.String(), "\n") {
		for _, id := range []string{"rrn_restored_q", "rrn_restored ", "rrn_objects", "rrn_down"} {
			if strings.Contains(line, "run "+id) {
				lines[strings.TrimSpace(id)] = line
			}
		}
	}
	for _, id := range []string{"rrn_restored", "rrn_restored_q"} {
		line := lines[id]
		if !strings.Contains(line, "do NOT drop it at the provider until the keys have moved off it") || !strings.Contains(line, "may be serving") || strings.Contains(line, "before you drop it") {
			t.Errorf("%s: a restored role is advised wrongly: %q", id, line)
		}
	}
	for _, id := range []string{"rrn_objects", "rrn_down"} {
		line := lines[id]
		if !strings.Contains(line, "before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back") || strings.Contains(line, "do NOT") {
			t.Errorf("%s: an ordinary refusal lost its drop advice or its key check: %q", id, line)
		}
	}
}

// The hold status is a second read; when only it fails the runs still render
// and the operator is told the hold status is unknown.
func TestRotationRunsRenderWhenTheHoldStatusCannotBeRead(t *testing.T) {
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		if r.URL.Path == rotationBase+"/runs" {
			_ = jsonwire.Encode(rw, apigen.RotationRunList{Items: []apigen.RotationRun{
				owedRun("rrn_owed", apigen.RotationRunPhaseRevokeRefused, time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC), false),
			}})
			return
		}
		http.Error(rw, "boom", http.StatusInternalServerError)
	}))
	args := append([]string{"rotation", "runs", "rot_70"}, rotationScope...)
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("runs failed on a hold-status error: exit %d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "rrn_owed") {
		t.Errorf("the runs were discarded: stdout=%q", stdout)
	}
	for _, want := range []string{"WARNING: the rotation could not be read", "restore hold status is unknown", "ATTENTION: run rrn_owed"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, stderr)
		}
	}
}
